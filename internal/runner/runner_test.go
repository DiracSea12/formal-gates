package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mustCreate opens dir and creates a run with the given specs.
func mustCreate(t *testing.T, dir, runID string, specs []TaskSpec) *Store {
	t.Helper()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateRun(context.Background(), runID, specs); err != nil {
		t.Fatal(err)
	}
	return st
}

// countInvocations counts invocation rows of taskID in the given state.
func countInvocations(t *testing.T, st *Store, taskID, state string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM invocations WHERE run_id=? AND task_id=? AND state=?",
		st.runID, taskID, state).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// invocationState returns the invocation row's state and stored result
// JSON ("" when absent).
func invocationState(t *testing.T, st *Store, invocationID string) (state, resultJSON string) {
	t.Helper()
	if err := st.db.QueryRow(
		"SELECT state, COALESCE(result_json,'') FROM invocations WHERE invocation_id=?",
		invocationID).Scan(&state, &resultJSON); err != nil {
		t.Fatal(err)
	}
	return state, resultJSON
}

// taskState returns the task's state and effective result invocation id.
func taskState(t *testing.T, st *Store, taskID string) (state, resultInvocationID string) {
	t.Helper()
	if err := st.db.QueryRow(
		"SELECT state, COALESCE(result_invocation_id,'') FROM tasks WHERE run_id=? AND task_id=?",
		st.runID, taskID).Scan(&state, &resultInvocationID); err != nil {
		t.Fatal(err)
	}
	return state, resultInvocationID
}

type eventRow struct {
	seq         int64
	kind        string
	payloadJSON string
}

// loadEvents returns the run's events in seq order.
func loadEvents(t *testing.T, st *Store) []eventRow {
	t.Helper()
	rows, err := st.db.Query(
		"SELECT seq, kind, payload_json FROM events WHERE run_id=? ORDER BY seq", st.runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		if err := rows.Scan(&e.seq, &e.kind, &e.payloadJSON); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// recordingExecutor answers with a fixed payload per task and appends each
// executed task id to calls (when non-nil).  probe, when set, runs before
// the result is produced — used to observe store state mid-flight.
func recordingExecutor(calls *[]string, probe func(inv Invocation)) Executor {
	return func(_ context.Context, inv Invocation) (Result, error) {
		if probe != nil {
			probe(inv)
		}
		if calls != nil {
			*calls = append(*calls, inv.TaskID)
		}
		return Result{Payload: json.RawMessage(`"done-` + inv.TaskID + `"`)}, nil
	}
}

func TestDependencyGatingRunsTasksInOrder(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "A"},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}},
		{TaskID: "c", Name: "C", DependsOn: []string{"b"}},
	}
	st := mustCreate(t, dir, "r-gating", specs)
	var calls []string
	// Mid-flight probe: while A executes (A admitted nothing yet), B must
	// be untouched — no invocation row, state still waiting_deps.
	exec := recordingExecutor(&calls, func(inv Invocation) {
		if inv.TaskID != "a" {
			return
		}
		if n := countInvocations(t, st, "b", invPending); n != 0 {
			t.Errorf("b has %d pending intents before a was admitted", n)
		}
		if s, _ := taskState(t, st, "b"); s != taskWaitingDeps {
			t.Errorf("b state = %q before a admitted, want %q", s, taskWaitingDeps)
		}
	})
	if err := NewRunner(st, exec).Run(context.Background(), specs); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "a,b,c" {
		t.Fatalf("execution order = %v, want [a b c]", calls)
	}
	for _, id := range []string{"a", "b", "c"} {
		if n := countInvocations(t, st, id, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want 1", id, n)
		}
		if s, res := taskState(t, st, id); s != taskFinished || res == "" {
			t.Errorf("task %s state = (%q,%q), want finished with effective result", id, s, res)
		}
	}
}

func TestSecondAdmissionIsIdempotentlyRefused(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "A"},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}},
	}
	st := mustCreate(t, dir, "r-once", specs)
	r := NewRunner(st, recordingExecutor(nil, nil))
	ctx := context.Background()
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	invA, _, digest := admittedInvocation(t, st, "a")
	eventsBefore := len(loadEvents(t, st))
	_, resultBefore := invocationState(t, st, invA)

	// The same identity tuple presented again must be refused, with no
	// row, result or event changed by the refusal.
	replay := Invocation{InvocationID: invA, TaskID: "a", Generation: 1, InputDigest: digest}
	if err := r.admit(ctx, replay, Result{Payload: json.RawMessage(`"tampered"`)}); !errors.Is(err, ErrAlreadyAdmitted) {
		t.Fatalf("second admission err = %v, want ErrAlreadyAdmitted", err)
	}
	if _, resultAfter := invocationState(t, st, invA); resultAfter != resultBefore {
		t.Fatalf("admitted result changed on refused replay: %q -> %q", resultBefore, resultAfter)
	}
	if n := len(loadEvents(t, st)); n != eventsBefore {
		t.Fatalf("event count changed on refused replay: %d -> %d", eventsBefore, n)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("task a admitted rows = %d after replay, want 1", n)
	}
}

// admittedInvocation loads a task's single admitted invocation: its id,
// generation and input digest.
func admittedInvocation(t *testing.T, st *Store, taskID string) (invocationID string, generation int64, digest string) {
	t.Helper()
	if err := st.db.QueryRow(`
		SELECT invocation_id, generation, input_digest FROM invocations
		 WHERE run_id=? AND task_id=? AND state=?`,
		st.runID, taskID, invAdmitted).Scan(&invocationID, &generation, &digest); err != nil {
		t.Fatal(err)
	}
	return invocationID, generation, digest
}

func TestLateSupersededResultIsRecordedNotEffective(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("input")}}
	st := mustCreate(t, dir, "r-late", specs)
	r := NewRunner(st, recordingExecutor(nil, nil))
	ctx := context.Background()

	// An intent is registered but never settled — exactly the state an
	// owner that died right after registration leaves behind.
	t1, err := r.nextReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inv1, err := r.registerIntent(ctx, t1, []byte("input"))
	if err != nil {
		t.Fatal(err)
	}
	// Recovery supersedes the orphaned intent and preserves the row.
	if err := r.recoverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countInvocations(t, st, "a", invSuperseded); n != 1 {
		t.Fatalf("superseded rows after recovery = %d, want 1", n)
	}
	// A fresh attempt is registered, executed and admitted.
	t2, err := r.nextReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inv2, err := r.registerIntent(ctx, t2, []byte("input"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.admit(ctx, inv2, Result{Payload: json.RawMessage(`"second"`)}); err != nil {
		t.Fatal(err)
	}
	// The superseded call's result now arrives late.
	if err := r.admit(ctx, inv1, Result{Payload: json.RawMessage(`"late"`)}); err != nil {
		t.Fatalf("recording late result: %v", err)
	}
	// Recorded on the old row, state unchanged ...
	if state, res := invocationState(t, st, inv1.InvocationID); state != invSuperseded || res != `{"payload":"late"}` {
		t.Fatalf("superseded row = (%q,%q), want (superseded, late result)", state, res)
	}
	// ... but the effective result still belongs to the newer call.
	if _, res := taskState(t, st, "a"); res != inv2.InvocationID {
		t.Fatalf("effective result invocation = %q, want the newer call %q", res, inv2.InvocationID)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("admitted rows = %d, want exactly 1", n)
	}
	if n := len(loadEvents(t, st)); n != 1 {
		t.Fatalf("events = %d, want 1 (one admission)", n)
	}
}

func TestAdmissionRejectsMismatchedIdentity(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("input")}}
	st := mustCreate(t, dir, "r-mismatch", specs)
	r := NewRunner(st, recordingExecutor(nil, nil))
	ctx := context.Background()

	tp, err := r.nextReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := r.registerIntent(ctx, tp, []byte("input"))
	if err != nil {
		t.Fatal(err)
	}
	// A tampered digest does not match any registered call: refused,
	// nothing settled, no event.
	forged := inv
	forged.InputDigest = strings.Repeat("0", 64)
	if err := r.admit(ctx, forged, Result{}); !errors.Is(err, ErrInvocationMismatch) {
		t.Fatalf("mismatched admission err = %v, want ErrInvocationMismatch", err)
	}
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Fatalf("pending rows = %d after refused mismatch, want 1", n)
	}
	if n := len(loadEvents(t, st)); n != 0 {
		t.Fatalf("events = %d after refused mismatch, want 0", n)
	}
	// The genuine tuple still settles normally.
	if err := r.admit(ctx, inv, Result{Payload: json.RawMessage(`"ok"`)}); err != nil {
		t.Fatal(err)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("admitted rows = %d, want 1", n)
	}
}

func TestExecutorFailureLeavesPendingUntilNextRunSupersedes(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "A"}}
	st := mustCreate(t, dir, "r-fail", specs)
	ctx := context.Background()
	fail := true
	calls := 0
	r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		calls++
		if fail {
			return Result{}, errors.New("boom")
		}
		return Result{Payload: json.RawMessage(`"ok"`)}, nil
	})
	if err := r.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("first run err = %v, want executor failure", err)
	}
	// No receipt: the intent stays pending rather than being guessed at.
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Fatalf("pending rows after failure = %d, want 1", n)
	}
	fail = false
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("executor ran %d times, want 2 (failed attempt retried)", calls)
	}
	if n := countInvocations(t, st, "a", invSuperseded); n != 1 {
		t.Fatalf("superseded rows = %d, want the failed attempt preserved", n)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("admitted rows = %d, want exactly 1", n)
	}
}

func TestOpenIsExclusivePerDirectory(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// flock conflicts are per open file description, so even this same
	// process cannot own the directory twice.
	if _, err := Open(dir); !errors.Is(err, ErrRunBusy) {
		t.Fatalf("second Open err = %v, want ErrRunBusy", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st2.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEventsMatchAdmissionsExactly(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "A"},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}},
	}
	st := mustCreate(t, dir, "r-events", specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(context.Background(), specs); err != nil {
		t.Fatal(err)
	}
	events := loadEvents(t, st)
	if len(events) != 2 {
		t.Fatalf("events = %d, want one per admission (2)", len(events))
	}
	seen := map[string]bool{}
	var lastSeq int64
	for i, e := range events {
		if e.kind != eventKindAdmitted {
			t.Errorf("event %d kind = %q, want %q", i, e.kind, eventKindAdmitted)
		}
		if e.seq <= lastSeq {
			t.Errorf("event %d seq %d not increasing after %d", i, e.seq, lastSeq)
		}
		lastSeq = e.seq
		var p admittedEvent
		if err := json.Unmarshal([]byte(e.payloadJSON), &p); err != nil {
			t.Fatalf("event %d payload: %v", i, err)
		}
		// Every event references a really admitted invocation, and the
		// event's result equals that row's stored result.
		state, resultJSON := invocationState(t, st, p.InvocationID)
		if state != invAdmitted {
			t.Errorf("event %d references invocation in state %q", i, state)
		}
		var stored Result
		if err := json.Unmarshal([]byte(resultJSON), &stored); err != nil {
			t.Fatalf("invocation %d result unmarshal: %v", i, err)
		}
		if string(stored.Payload) != string(p.Result.Payload) {
			t.Errorf("event %d result %q != stored %q", i, p.Result.Payload, stored.Payload)
		}
		if _, eff := taskState(t, st, p.TaskID); eff != p.InvocationID {
			t.Errorf("task %s effective result %q != event invocation %q", p.TaskID, eff, p.InvocationID)
		}
		if seen[p.TaskID] {
			t.Errorf("duplicate admission event for task %s", p.TaskID)
		}
		seen[p.TaskID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("admission events covered tasks %v, want both a and b", seen)
	}
}

func TestSchemaConstraintsHold(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// A dependency on an unknown task is refused by the foreign key.
	if err := st.CreateRun(ctx, "r", []TaskSpec{{TaskID: "a", DependsOn: []string{"ghost"}}}); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("ghost dependency err = %v, want FOREIGN KEY constraint failure", err)
	}
	// Duplicate task ids hit the tasks primary key.
	if err := st.CreateRun(ctx, "r", []TaskSpec{{TaskID: "a"}, {TaskID: "a"}}); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate task err = %v, want UNIQUE constraint failure", err)
	}
	// A duplicated edge hits the task_deps primary key.
	if err := st.CreateRun(ctx, "r", []TaskSpec{{TaskID: "a"}, {TaskID: "b", DependsOn: []string{"a", "a"}}}); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate edge err = %v, want UNIQUE constraint failure", err)
	}
	if err := st.CreateRun(ctx, "r", []TaskSpec{{TaskID: "a"}}); err != nil {
		t.Fatal(err)
	}
	// A directory holds exactly one run.
	if err := st.CreateRun(ctx, "other", []TaskSpec{{TaskID: "x"}}); err == nil || !strings.Contains(err.Error(), "already holds run") {
		t.Fatalf("second CreateRun err = %v, want one-run-per-directory refusal", err)
	}
	// Direct writes respect the foreign keys too (constraint enforcement
	// is on for every connection).
	if _, err := st.db.Exec(`
		INSERT INTO invocations
			(invocation_id, run_id, task_id, generation, input_digest, registered_at, state)
		VALUES ('i1','r','nope',1,'d','t','pending')`); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("orphan invocation err = %v, want FOREIGN KEY constraint failure", err)
	}
	// Reopening keeps the schema version stamped and rediscovers the run.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	var version int
	if err := st2.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d after reopen, want %d", version, schemaVersion)
	}
	// The durability settings from the DSN are live on every connection.
	var journal string
	if err := st2.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}
	if st2.runID != "r" {
		t.Fatalf("reopened run id = %q, want %q", st2.runID, "r")
	}
}

func TestSpecInputChangesAreRefusedThroughoutLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("before any execution", func(t *testing.T) {
		dir := t.TempDir()
		original := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("revision-1")}}
		st := mustCreate(t, dir, "r-fresh", original)
		changed := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("revision-2")}}
		executed := 0
		r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
			executed++
			return Result{}, nil
		})
		err := r.Run(ctx, changed)
		if err == nil || !strings.Contains(err.Error(), `task "a"`) {
			t.Fatalf("changed input err = %v, want refusal naming task a", err)
		}
		if executed != 0 {
			t.Fatalf("executed %d tasks on refused specs, want 0", executed)
		}
		if n := countInvocations(t, st, "a", invPending); n != 0 {
			t.Fatalf("refused run left %d intents, want 0", n)
		}
		// A spec naming a task the run never had is refused too.
		stray := append([]TaskSpec{}, original...)
		stray = append(stray, TaskSpec{TaskID: "zz", Input: []byte("x")})
		if err := r.Run(ctx, stray); err == nil || !strings.Contains(err.Error(), "not part of run") {
			t.Fatalf("unknown task err = %v, want refusal", err)
		}
	})

	t.Run("after partial completion and on recovery", func(t *testing.T) {
		dir := t.TempDir()
		original := []TaskSpec{
			{TaskID: "a", Name: "A", Input: []byte("in-a")},
			{TaskID: "b", Name: "B", DependsOn: []string{"a"}, Input: []byte("in-b")},
		}
		st := mustCreate(t, dir, "r-resume", original)
		var gotInputs []string
		interruptB := true
		r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
			gotInputs = append(gotInputs, inv.TaskID+":"+string(inv.Input))
			if inv.TaskID == "b" && interruptB {
				return Result{}, errors.New("interrupted")
			}
			return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
		})
		// First advance settles a, loses the receipt for b.
		if err := r.Run(ctx, original); err == nil || !strings.Contains(err.Error(), "interrupted") {
			t.Fatalf("first run err = %v, want interruption", err)
		}
		_, effA := taskState(t, st, "a")

		// Reopening with a changed input for b must refuse before any
		// recovery write: b's unsettled intent stays pending, nothing
		// runs with the wrong input, a's effective result is untouched.
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		st2, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer st2.Close()
		r2 := NewRunner(st2, r.exec)
		changedB := []TaskSpec{
			{TaskID: "a", Name: "A", Input: []byte("in-a")},
			{TaskID: "b", Name: "B", DependsOn: []string{"a"}, Input: []byte("tampered")},
		}
		if err := r2.Run(ctx, changedB); err == nil || !strings.Contains(err.Error(), `task "b"`) {
			t.Fatalf("changed recovery input err = %v, want refusal naming task b", err)
		}
		if n := countInvocations(t, st2, "b", invPending); n != 1 {
			t.Fatalf("b pending intents after refusal = %d, want 1 (no recovery write)", n)
		}
		for _, got := range gotInputs {
			if strings.HasSuffix(got, ":tampered") {
				t.Fatalf("task executed with tampered input: %q", got)
			}
		}
		if s, eff := taskState(t, st2, "a"); s != taskFinished || eff != effA {
			t.Fatalf("a effective result changed by refused run: (%q,%q), want (%q,%q)", s, eff, taskFinished, effA)
		}

		// Unchanged specs resume cleanly: b is superseded by a fresh
		// call and admitted exactly once.
		interruptB = false
		if err := r2.Run(ctx, original); err != nil {
			t.Fatal(err)
		}
		if n := countInvocations(t, st2, "b", invSuperseded); n != 1 {
			t.Fatalf("b superseded rows = %d, want the interrupted attempt preserved", n)
		}
		if n := countInvocations(t, st2, "b", invAdmitted); n != 1 {
			t.Fatalf("b admitted rows = %d, want exactly 1", n)
		}
		if n := len(loadEvents(t, st2)); n != 2 {
			t.Fatalf("events = %d, want one per admitted task (2)", n)
		}
	})

	t.Run("schema v1 database is refused", func(t *testing.T) {
		dir := t.TempDir()
		// Seed a file that claims version 1 — the shape K1's first
		// draft wrote, before task input binding existed.
		db, err := sql.Open("sqlite", filepath.Join(dir, "run.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "predates task input binding") {
			t.Fatalf("v1 open err = %v, want explicit refusal", err)
		}
	})
}

func TestConcurrentRunsOnOneStoreDoNotDoubleAdvance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sameRunner bool
	}{
		{"two runners sharing one store", false},
		{"same runner re-entered", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specs := []TaskSpec{{TaskID: "a", Name: "A"}}
			st := mustCreate(t, dir, "r-serial", specs)
			ctx := context.Background()
			started := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			exec := func(_ context.Context, inv Invocation) (Result, error) {
				calls.Add(1)
				close(started)
				<-release
				return Result{Payload: json.RawMessage(`"only"`)}, nil
			}
			first := NewRunner(st, exec)
			second := first
			if !tc.sameRunner {
				second = NewRunner(st, exec) // separate instance over the same store
			}
			firstDone := make(chan error, 1)
			go func() { firstDone <- first.Run(ctx, specs) }()
			<-started // the first advance is executing task a

			// The second advance must neither execute anything nor
			// wait for the in-flight one: it is refused with
			// ErrAdvancing because the holder already owns the loop.
			secondDone := make(chan error, 1)
			go func() { secondDone <- second.Run(ctx, specs) }()
			select {
			case err := <-secondDone:
				if !errors.Is(err, ErrAdvancing) {
					t.Fatalf("in-flight second Run err = %v, want ErrAdvancing", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("second Run blocked on the in-flight advance")
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("executed %d times while in flight, want 1", n)
			}
			close(release)
			if err := <-firstDone; err != nil {
				t.Fatalf("first Run err = %v, want nil", err)
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("executed %d times total, want exactly 1", n)
			}
			if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
				t.Fatalf("admitted rows = %d, want exactly 1", n)
			}
			// After completion a further advance takes the lock
			// normally — here a no-op that reports nil.
			if err := second.Run(ctx, specs); err != nil {
				t.Fatalf("post-completion Run err = %v, want nil", err)
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("executed %d times after completion, want 1", n)
			}
		})
	}
}

func TestDirectoriesWithURICharactersAreIsolated(t *testing.T) {
	root := t.TempDir()
	// Concurrent opens of two directories whose names contain a URI
	// fragment character — without path encoding both silently resolve
	// to the same database file.
	openAndCreate := func(name, runID, taskID string) error {
		st, err := Open(filepath.Join(root, name))
		if err != nil {
			return err
		}
		defer st.Close()
		return st.CreateRun(context.Background(), runID, []TaskSpec{{TaskID: taskID}})
	}
	errs := make(chan error, 2)
	go func() { errs <- openAndCreate("run#one", "one", "a") }()
	go func() { errs <- openAndCreate("run#two", "two", "b") }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent open/create: %v", err)
		}
	}
	// Each directory sees exactly its own run and nothing of the other.
	for _, tc := range []struct{ dir, runID, taskID string }{
		{"run#one", "one", "a"},
		{"run#two", "two", "b"},
	} {
		st, err := Open(filepath.Join(root, tc.dir))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if st.runID != tc.runID {
			t.Errorf("%s run id = %q, want %q", tc.dir, st.runID, tc.runID)
		}
		var n int
		if err := st.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE run_id=?", tc.runID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s holds %d tasks for run %q, want its own single task", tc.dir, n, tc.runID)
		}
	}
}
