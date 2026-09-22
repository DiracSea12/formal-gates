package runner

// K4 acceptance tests for the §13.2 sample — 局部代次、返修额度、失效范围
// 及调用接回: a failed task is repaired through Store.ReviseTask, the
// unaffected branch keeps its finished result, the failure is never
// overwritten by the later PASS, an old-generation result or answer never
// touches the current generation, gates carry across generations, and a
// crashed session's work is taken over by a revision.  The takeover
// scenario re-executes this binary through TestMain (RUNNER_K4_SCENARIO)
// and really exits at K1's after-intent injection point; everything else
// runs in-process.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	envK4Scenario = "RUNNER_K4_SCENARIO"
	envK4Dir      = "RUNNER_K4_DIR"
	k4RunID       = "k4-takeover"
)

// K4 child exit codes: 96 is the on-purpose exit right after the second
// task's intent registered (K1 injection point ①); 97+ mark wiring bugs
// in the child itself.  Codes stay clear of the 70–95 range K1–K3 use.
const (
	exitK4AfterIntent = 96
	exitK4Wiring      = 97
)

// taskGeneration returns the task's current generation.
func taskGeneration(t *testing.T, st *Store, taskID string) int64 {
	t.Helper()
	var gen int64
	if err := st.db.QueryRow(
		"SELECT generation FROM tasks WHERE run_id=? AND task_id=?",
		st.runID, taskID).Scan(&gen); err != nil {
		t.Fatal(err)
	}
	return gen
}

// pendingDecisionFor returns the currently pending decision id of taskID.
func pendingDecisionFor(t *testing.T, st *Store, taskID string) string {
	t.Helper()
	pending, err := st.PendingDecisions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range pending {
		if d.TaskID == taskID {
			return d.DecisionID
		}
	}
	t.Fatalf("no pending decision for task %s", taskID)
	return ""
}

// decisionRowAtGeneration loads one decisions row by task and generation.
func decisionRowAtGeneration(t *testing.T, st *Store, taskID string, generation int64) (decisionID, revision, body, optionsJSON, allow, state string) {
	t.Helper()
	if err := st.db.QueryRow(`
		SELECT decision_id, revision, body, options_json, allow, state
		  FROM decisions
		 WHERE run_id=? AND task_id=? AND generation=?`,
		st.runID, taskID, generation).
		Scan(&decisionID, &revision, &body, &optionsJSON, &allow, &state); err != nil {
		t.Fatal(err)
	}
	return
}

// k4Child drives the takeover sample in RUNNER_K4_DIR: two INDEPENDENT
// tasks, a and b.  Sequential dispatch runs a to a committed admission
// (the finished partial work), then b's generation-1 intent registers —
// and the process really exits at K1's after-intent point: an invalidated
// session whose in-flight intent will never settle, plus retained work
// the parent must keep.
func k4Child() {
	dir := os.Getenv(envK4Dir)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "child: missing", envK4Dir)
		os.Exit(exitK4Wiring)
	}
	st, err := Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child open:", err)
		os.Exit(exitK4Wiring)
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
	}
	if err := st.CreateRun(context.Background(), k4RunID, specs); err != nil {
		fmt.Fprintln(os.Stderr, "child create:", err)
		os.Exit(exitK4Wiring)
	}
	r := NewRunner(st, markerExecutor(dir))
	registrations := 0
	// K1 injection point ①, on the SECOND registration (b's): a's
	// registration passed through and its admission already committed.
	r.hooks.afterRegisterIntent = func() {
		registrations++
		if registrations == 2 {
			os.Exit(exitK4AfterIntent)
		}
	}
	if err := r.Run(context.Background(), specs); err != nil {
		fmt.Fprintln(os.Stderr, "child run:", err)
		os.Exit(exitK4Wiring)
	}
	fmt.Fprintln(os.Stderr, "child: run completed without exiting at the injection point")
	os.Exit(exitK4Wiring)
}

// §13.2 K4 sample 1 — 失败项返修、无影响项保留: a fails without a receipt,
// independent b succeeds, c joins both.  Revising a's input re-runs exactly
// a and its dependent c at the new generation; b's finished result is
// retained untouched; a's failed generation-1 invocation row survives as
// superseded history — the later PASS never overwrites the failure.
func TestK4FailureRepairKeepsUnaffectedWork(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
		{TaskID: "c", Name: "task-c", DependsOn: []string{"a", "b"}, Input: []byte("input-c")},
	}
	st := mustCreate(t, dir, "k4-repair", specs)
	failA := true
	base := markerExecutor(dir)
	exec := func(ctx context.Context, inv Invocation) (Result, error) {
		if inv.TaskID == "a" && failA {
			// The attempt really ran (its marker line counts) but returns
			// no receipt — exactly a failed execution.
			if _, err := base(ctx, inv); err != nil {
				return Result{}, err
			}
			return Result{}, errors.New("boom")
		}
		return base(ctx, inv)
	}
	r := NewRunner(st, exec)
	r.Parallelism = 2
	if err := r.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("first run err = %v, want the executor failure", err)
	}
	// a failed without a receipt, b is durably admitted, c never ran.
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Fatalf("task a pending intents after failure = %d, want 1", n)
	}
	_, effB := taskState(t, st, "b")
	if effB == "" {
		t.Fatal("task b has no effective result after the first run")
	}
	if n := markerLines(t, dir, "c"); n != 0 {
		t.Fatalf("task c executed %d times on incomplete inputs, want 0", n)
	}

	// The repair: revise a's input between Runs.
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}); err != nil {
		t.Fatal(err)
	}
	failA = false
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
		{TaskID: "c", Name: "task-c", DependsOn: []string{"a", "b"}, Input: []byte("input-c")},
	}
	if err := r.Run(ctx, revised); err != nil {
		t.Fatal(err)
	}

	// a: the failed attempt plus the repaired one — two executions, one
	// admitted row at generation 2, and the failed generation-1 row kept
	// (superseded, result-less): the failure is not overwritten by the
	// later PASS.
	if n := markerLines(t, dir, "a"); n != 2 {
		t.Errorf("task a executed %d times, want 2 (failed attempt plus repair)", n)
	}
	invA, genA, _ := admittedInvocation(t, st, "a")
	if genA != 2 {
		t.Errorf("task a admitted at generation %d, want 2", genA)
	}
	if _, eff := taskState(t, st, "a"); eff != invA {
		t.Errorf("task a effective result %q, want the generation-2 call %q", eff, invA)
	}
	if n := countInvocations(t, st, "a", invSuperseded); n != 1 {
		t.Fatalf("task a superseded rows = %d, want the failed attempt preserved", n)
	}
	var failedID string
	if err := st.db.QueryRow(`
		SELECT invocation_id FROM invocations
		 WHERE run_id=? AND task_id=? AND state=?`,
		st.runID, "a", invSuperseded).Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	if state, res := invocationState(t, st, failedID); state != invSuperseded || res != "" {
		t.Errorf("failed generation-1 row = (%q,%q), want (superseded, no result)", state, res)
	}
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Errorf("task a generation = %d, want 2", gen)
	}

	// b: zero re-execution — marker, admitted rows and effective result
	// all unchanged by the revision of its sibling branch (§117).
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Errorf("task b executed %d times, want still 1 (unaffected work retained)", n)
	}
	if n := countInvocations(t, st, "b", invAdmitted); n != 1 {
		t.Errorf("task b admitted %d times, want exactly 1", n)
	}
	var rowsB int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM invocations WHERE run_id=? AND task_id=?",
		st.runID, "b").Scan(&rowsB); err != nil {
		t.Fatal(err)
	}
	if rowsB != 1 {
		t.Errorf("task b invocation rows = %d, want its single original call", rowsB)
	}
	if _, eff := taskState(t, st, "b"); eff != effB {
		t.Errorf("task b effective result replaced: %q, want retained %q", eff, effB)
	}
	if gen := taskGeneration(t, st, "b"); gen != 1 {
		t.Errorf("task b generation = %d, want 1 (not in the revision scope)", gen)
	}

	// c: exactly one execution, invalidated to generation 2 by a's
	// revision, admitted once.
	if n := markerLines(t, dir, "c"); n != 1 {
		t.Errorf("task c executed %d times, want exactly 1", n)
	}
	if gen := taskGeneration(t, st, "c"); gen != 2 {
		t.Errorf("task c generation = %d, want 2 (dependent of the revised task)", gen)
	}
	if n := countInvocations(t, st, "c", invAdmitted); n != 1 {
		t.Errorf("task c admitted %d times, want exactly 1", n)
	}

	// Events: b's first admission, the scope's revisions in task order
	// (a then c — no gate on either), then a's and c's admissions.
	wantKinds := []string{eventKindAdmitted, eventKindTaskRevised, eventKindTaskRevised,
		eventKindAdmitted, eventKindAdmitted}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantKinds) {
		t.Errorf("event kinds = %v, want %v", kinds, wantKinds)
	}
	var bumps []taskRevisedEvent
	for _, e := range loadEvents(t, st) {
		if e.kind != eventKindTaskRevised {
			continue
		}
		var p taskRevisedEvent
		if err := json.Unmarshal([]byte(e.payloadJSON), &p); err != nil {
			t.Fatalf("task_revised payload: %v", err)
		}
		bumps = append(bumps, p)
	}
	wantBumps := []taskRevisedEvent{
		{TaskID: "a", OldGeneration: 1, NewGeneration: 2},
		{TaskID: "c", OldGeneration: 1, NewGeneration: 2},
	}
	if !slices.Equal(bumps, wantBumps) {
		t.Errorf("task_revised payloads = %+v, want %+v", bumps, wantBumps)
	}
}

// §4.2/§9.2 — a late OLD-generation result never overrides the current
// generation's: the admission identity tuple carries the generation, so
// the revision-superseded row records it and the effective result stays
// with the new generation's call.
func TestK4LateOldGenerationResultIsRecordedNotEffective(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("input")}}
	st := mustCreate(t, dir, "k4-late", specs)
	r := NewRunner(st, recordingExecutor(nil, nil))

	// A generation-1 intent is registered and never settled — the state
	// an owner that died right after registration leaves behind.
	t1, err := r.nextReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if t1.generation != 1 {
		t.Fatalf("fresh task generation = %d, want 1", t1.generation)
	}
	inv1, err := r.registerIntent(ctx, t1, []byte("input"))
	if err != nil {
		t.Fatal(err)
	}
	// The revision bumps the task and voids the intent; the row stays.
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "A", Input: []byte("input2")}); err != nil {
		t.Fatal(err)
	}
	if state, _ := invocationState(t, st, inv1.InvocationID); state != invSuperseded {
		t.Fatalf("generation-1 intent state after revision = %q, want superseded", state)
	}

	// The new generation executes and admits.
	t2, err := r.nextReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if t2.generation != 2 {
		t.Fatalf("revised task generation = %d, want 2", t2.generation)
	}
	inv2, err := r.registerIntent(ctx, t2, []byte("input2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.admit(ctx, inv2, Result{Payload: json.RawMessage(`"second"`)}); err != nil {
		t.Fatal(err)
	}

	// The old generation's result arrives late: recorded on its row, the
	// effective result untouched.
	if err := r.admit(ctx, inv1, Result{Payload: json.RawMessage(`"late"`)}); err != nil {
		t.Fatalf("recording late old-generation result: %v", err)
	}
	if state, res := invocationState(t, st, inv1.InvocationID); state != invSuperseded || res != `{"payload":"late"}` {
		t.Fatalf("superseded row = (%q,%q), want (superseded, late result)", state, res)
	}
	if _, eff := taskState(t, st, "a"); eff != inv2.InvocationID {
		t.Fatalf("effective result invocation = %q, want the generation-2 call %q", eff, inv2.InvocationID)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("admitted rows = %d, want exactly 1", n)
	}
	wantKinds := []string{eventKindTaskRevised, eventKindAdmitted}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantKinds) {
		t.Errorf("event kinds = %v, want %v", kinds, wantKinds)
	}
}

// §13.2 K4 sample 3 — 失效会话与部分工作接手: a real child process admits
// the independent task a, registers task b's generation-1 intent and dies
// at K1's after-intent point.  The parent takes over through a revision of
// b: the dead session's intent is void (superseded, row kept), b executes
// exactly once at the new generation, and a's finished result is retained.
func TestK4CrashedSessionTakenOverByRevision(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), envK4Scenario+"=1", envK4Dir+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitK4AfterIntent {
		t.Fatalf("child exit = %v (want code %d); output:\n%s", err, exitK4AfterIntent, out)
	}
	ctx := context.Background()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.runID != k4RunID {
		t.Fatalf("reopened run id = %q, want %q", st.runID, k4RunID)
	}
	// The durable state the child left: a admitted exactly once; b holding
	// one generation-1 pending intent that will never settle.
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("task a admitted %d times in the child, want 1", n)
	}
	_, effA := taskState(t, st, "a")
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Fatalf("task a executed %d times in the child, want 1", n)
	}
	if n := countInvocations(t, st, "b", invPending); n != 1 {
		t.Fatalf("task b pending intents after the crash = %d, want 1", n)
	}
	if n := markerLines(t, dir, "b"); n != 0 {
		t.Fatalf("task b executed %d times in the child, want 0 (died before execution)", n)
	}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, []string{eventKindAdmitted}) {
		t.Fatalf("event kinds after the crash = %v, want one admission (a's)", kinds)
	}

	// The takeover: revise b's input.  The revision voids the dead
	// session's generation-1 intent (superseded, row kept) and rebinds
	// b's definition to the new input.
	if err := st.ReviseTask(ctx, "b",
		TaskSpec{TaskID: "b", Name: "task-b", Input: []byte("input-b2")}); err != nil {
		t.Fatal(err)
	}
	if n := countInvocations(t, st, "b", invSuperseded); n != 1 {
		t.Fatalf("task b superseded rows after the revision = %d, want the dead intent kept", n)
	}
	if gen := taskGeneration(t, st, "b"); gen != 2 {
		t.Fatalf("task b generation after the revision = %d, want 2", gen)
	}
	if gen := taskGeneration(t, st, "a"); gen != 1 {
		t.Fatalf("task a generation after the revision = %d, want 1 (unaffected)", gen)
	}

	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b2")},
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	// a retained: no re-execution, effective result unchanged.
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Errorf("task a executed %d times after takeover, want still 1", n)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Errorf("task a admitted %d times after takeover, want 1", n)
	}
	if _, eff := taskState(t, st, "a"); eff != effA {
		t.Errorf("task a effective result replaced on takeover: %q, want %q", eff, effA)
	}
	// b: exactly one execution, at the new generation, admitted once.
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Errorf("task b executed %d times, want exactly 1 (new generation)", n)
	}
	_, genB, _ := admittedInvocation(t, st, "b")
	if genB != 2 {
		t.Errorf("task b admitted at generation %d, want 2", genB)
	}
	if n := countInvocations(t, st, "b", invAdmitted); n != 1 {
		t.Errorf("task b admitted %d times, want exactly 1", n)
	}
	// Events: a's admission from the child, the revision of b, b's
	// admission — nothing else.
	wantKinds := []string{eventKindAdmitted, eventKindTaskRevised, eventKindAdmitted}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantKinds) {
		t.Errorf("event kinds = %v, want %v", kinds, wantKinds)
	}
}

// §6.1 — gates carry across generations: a revision copies the latest
// gate's frozen definition to the new generation, pending again; the old
// request is refused as stale, answered or not; only the new gate's
// answer authorizes the new generation's work — for the revised task and
// for its gated dependent alike.  A task that was never gated stays
// ungated.
func TestK4GatesCarryAcrossGenerations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gate := func(body string) *Confirmation {
		return &Confirmation{
			Options:  []string{"approve", "reject"},
			Allow:    "approve",
			Revision: "rev-1",
			Body:     []byte(body),
		}
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a"), Confirm: gate("approve a?")},
		{TaskID: "d", Name: "task-d", DependsOn: []string{"a"}, Input: []byte("input-d"), Confirm: gate("approve d?")},
		{TaskID: "u", Name: "task-u", Input: []byte("input-u")},
	}
	st := mustCreate(t, dir, "k4-gates", specs)
	r := NewRunner(st, markerExecutor(dir))

	// Generation 1: both gates answered, everything completes once.
	if err := r.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("gated first run err = %v, want ErrWaitingDecision", err)
	}
	oldA := pendingDecisionFor(t, st, "a")
	oldD := pendingDecisionFor(t, st, "d")
	if _, err := st.Answer(ctx, oldA, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Answer(ctx, oldD, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "d", "u"} {
		if n := markerLines(t, dir, task); n != 1 {
			t.Fatalf("task %s executed %d times at generation 1, want 1", task, n)
		}
	}

	// Revise a: scope {a, d}; both carry pending generation-2 gates; u is
	// untouched.
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}); err != nil {
		t.Fatal(err)
	}
	// The carried gate is the same frozen question, pending again, under
	// a fresh id at generation 2 — for the revised task and its gated
	// dependent.
	newA, rev, body, optionsJSON, allow, state := decisionRowAtGeneration(t, st, "a", 2)
	if newA == oldA {
		t.Fatal("carried gate reused the old decision id")
	}
	if rev != "rev-1" || body != "approve a?" || allow != "approve" || state != decisionPending {
		t.Fatalf("carried gate for a = (%q,%q,%q,%q), want the frozen (rev-1, approve a?, approve, pending)", rev, body, allow, state)
	}
	var options []string
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(options, []string{"approve", "reject"}) {
		t.Fatalf("carried options = %v, want [approve reject]", options)
	}
	if _, _, _, _, _, dState := decisionRowAtGeneration(t, st, "d", 2); dState != decisionPending {
		t.Fatalf("dependent d's carried gate state = %q, want pending", dState)
	}

	// The revised run waits on the new gates and executes nothing; the
	// Confirm-free specs meet the same carried gates (the rows rule).
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "d", Name: "task-d", DependsOn: []string{"a"}, Input: []byte("input-d")},
		{TaskID: "u", Name: "task-u", Input: []byte("input-u")},
	}
	if err := r.Run(ctx, revised); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("revised run err = %v, want ErrWaitingDecision", err)
	}
	for _, task := range []string{"a", "d", "u"} {
		if n := markerLines(t, dir, task); n != 1 {
			t.Errorf("task %s executed %d times while the new gates wait, want still 1", task, n)
		}
	}

	// The OLD request is refused as stale: answered or not, it belongs to
	// generation 1 and authorizes nothing of generation 2.
	if _, err := st.Answer(ctx, oldA, "approve"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("old-generation answer err = %v, want ErrDecisionStale", err)
	}
	if _, err := st.Answer(ctx, oldD, "approve"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("dependent's old-generation answer err = %v, want ErrDecisionStale", err)
	}

	// The new gates authorize the new generation's work.
	if _, err := st.Answer(ctx, newA, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Answer(ctx, pendingDecisionFor(t, st, "d"), "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "d"} {
		if n := markerLines(t, dir, task); n != 2 {
			t.Errorf("task %s executed %d times, want 2 (once per generation)", task, n)
		}
		if n := countInvocations(t, st, task, invAdmitted); n != 2 {
			t.Errorf("task %s admitted %d times, want 2 (one per generation)", task, n)
		}
		if gen := taskGeneration(t, st, task); gen != 2 {
			t.Errorf("task %s generation = %d, want 2", task, gen)
		}
	}
	if n := markerLines(t, dir, "u"); n != 1 {
		t.Errorf("task u executed %d times, want still 1 (outside the scope)", n)
	}

	// u was never gated: revising it creates no gate, and it re-runs
	// straight through with its new input.
	if err := st.ReviseTask(ctx, "u",
		TaskSpec{TaskID: "u", Name: "task-u", Input: []byte("input-u2")}); err != nil {
		t.Fatal(err)
	}
	var gatesU int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM decisions WHERE run_id=? AND task_id=?",
		st.runID, "u").Scan(&gatesU); err != nil {
		t.Fatal(err)
	}
	if gatesU != 0 {
		t.Fatalf("ungated task u accumulated %d decision rows, want 0", gatesU)
	}
	revised[2].Input = []byte("input-u2")
	if err := r.Run(ctx, revised); err != nil {
		t.Fatalf("run after revising the ungated task: %v", err)
	}
	if n := markerLines(t, dir, "u"); n != 2 {
		t.Errorf("task u executed %d times after its revision, want 2", n)
	}
}

// Old-generation decision rows are history: while the current
// generation's gate decides the wait, a still-pending generation-1 row
// must not hold settle in a permanent wait once the current gate settled
// — decisionStatus counts only rows at the task's current generation,
// matching the ready query's predicate.
func TestK4OldGenerationPendingGateDoesNotMisreportSettle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{
		{
			TaskID: "a", Name: "task-a", Input: []byte("input-a"),
			Confirm: &Confirmation{
				Options:  []string{"approve", "reject"},
				Allow:    "approve",
				Revision: "rev-1",
				Body:     []byte("approve a?"),
			},
		},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
	}
	st := mustCreate(t, dir, "k4-oldgate", specs)
	r := NewRunner(st, markerExecutor(dir))
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
	}

	// Generation 1: the gate is pending, the run waits, b runs.
	if err := r.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("first run err = %v, want ErrWaitingDecision", err)
	}
	oldGate := pendingDecisionFor(t, st, "a")
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Fatalf("task b executed %d times, want 1", n)
	}
	if n := markerLines(t, dir, "a"); n != 0 {
		t.Fatalf("task a executed %d times behind its gate, want 0", n)
	}

	// The revision (no new Confirm) copies the gate pending to
	// generation 2; settle still reports the wait — counting the
	// generation-2 row.
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}); err != nil {
		t.Fatal(err)
	}
	waiting, refused, err := st.decisionStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !waiting || refused {
		t.Fatalf("decisionStatus after revision = (waiting=%v, refused=%v), want the generation-2 wait", waiting, refused)
	}
	if err := r.Run(ctx, revised); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("revised run err = %v, want ErrWaitingDecision", err)
	}
	// Two pending rows exist in the raw table now (generations 1 and 2).
	var rawPending int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM decisions WHERE run_id=? AND state=?",
		st.runID, decisionPending).Scan(&rawPending); err != nil {
		t.Fatal(err)
	}
	if rawPending != 2 {
		t.Fatalf("raw pending decision rows = %d, want 2 (both generations kept)", rawPending)
	}

	// Answer the GENERATION-2 gate only.
	var newGate string
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range pending {
		if d.DecisionID != oldGate {
			newGate = d.DecisionID
		}
	}
	if newGate == "" {
		t.Fatal("no generation-2 gate found among the pending decisions")
	}
	if _, err := st.Answer(ctx, newGate, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Errorf("task a executed %d times, want exactly 1 (generation 2)", n)
	}
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Errorf("task b executed %d times, want still 1", n)
	}

	// The generation-1 row is STILL pending in the table, yet the
	// finished run settles to nil — no permanent wait from dead history.
	if _, _, _, _, _, s := decisionRowAtGeneration(t, st, "a", 1); s != decisionPending {
		t.Fatalf("generation-1 row state = %q, want still pending (history kept)", s)
	}
	waiting, refused, err = st.decisionStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if waiting || refused {
		t.Fatalf("decisionStatus with only an old-generation pending row = (waiting=%v, refused=%v), want neither", waiting, refused)
	}
	if err := r.Run(ctx, revised); err != nil {
		t.Fatalf("no-op run err = %v, want nil (old pending row must not wait)", err)
	}
}

// Boundary behaviour of ReviseTask: refused while a Run is mid-advance,
// a named refusal for an unknown task, zero residue for a malformed
// confirmation, and the verifySpecs refusal for the OLD input after the
// revision took effect.
func TestK4ReviseTaskBoundaries(t *testing.T) {
	ctx := context.Background()

	t.Run("refused while a Run is in flight", func(t *testing.T) {
		dir := t.TempDir()
		specs := []TaskSpec{{TaskID: "a", Name: "A"}}
		st := mustCreate(t, dir, "k4-adv", specs)
		started := make(chan struct{})
		release := make(chan struct{})
		exec := func(_ context.Context, inv Invocation) (Result, error) {
			close(started)
			<-release
			return Result{Payload: json.RawMessage(`"ok"`)}, nil
		}
		done := make(chan error, 1)
		go func() { done <- NewRunner(st, exec).Run(ctx, specs) }()
		<-started
		// The revision joins Run's and Close's advance guard: refused,
		// never blocking, nothing changed.
		if err := st.ReviseTask(ctx, "a",
			TaskSpec{TaskID: "a", Input: []byte("in2")}); !errors.Is(err, ErrAdvancing) {
			t.Fatalf("in-flight ReviseTask err = %v, want ErrAdvancing", err)
		}
		if gen := taskGeneration(t, st, "a"); gen != 1 {
			t.Errorf("generation after refused revision = %d, want 1", gen)
		}
		if n := len(loadEvents(t, st)); n != 0 {
			t.Errorf("events after refused revision = %d, want 0", n)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		// After the Run, the same revision succeeds.
		if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("in2")}); err != nil {
			t.Fatal(err)
		}
		if gen := taskGeneration(t, st, "a"); gen != 2 {
			t.Errorf("generation after the revision = %d, want 2", gen)
		}
	})

	t.Run("unknown task is a named refusal", func(t *testing.T) {
		dir := t.TempDir()
		st := mustCreate(t, dir, "k4-ghost", []TaskSpec{{TaskID: "a", Name: "A"}})
		err := st.ReviseTask(ctx, "ghost", TaskSpec{TaskID: "ghost", Input: []byte("x")})
		if err == nil || !strings.Contains(err.Error(), `no task "ghost"`) || !strings.Contains(err.Error(), "k4-ghost") {
			t.Fatalf("unknown task err = %v, want a refusal naming the task and the run", err)
		}
		if n := len(loadEvents(t, st)); n != 0 {
			t.Errorf("events after the refusal = %d, want 0 (zero residue)", n)
		}
		if gen := taskGeneration(t, st, "a"); gen != 1 {
			t.Errorf("task a generation after the refusal = %d, want 1", gen)
		}
	})

	t.Run("malformed confirmation leaves zero residue", func(t *testing.T) {
		dir := t.TempDir()
		st := mustCreate(t, dir, "k4-malformed", []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in")}})
		spec := TaskSpec{
			TaskID: "a", Input: []byte("in2"),
			Confirm: &Confirmation{Options: []string{"approve"}, Allow: "maybe"},
		}
		err := st.ReviseTask(ctx, "a", spec)
		if err == nil || !strings.Contains(err.Error(), "outside its confirmation options") {
			t.Fatalf("malformed ReviseTask err = %v, want the validateConfirmation refusal", err)
		}
		if gen := taskGeneration(t, st, "a"); gen != 1 {
			t.Errorf("generation after refused declaration = %d, want 1", gen)
		}
		if n := countDecisions(t, st, ""); n != 0 {
			t.Errorf("decision rows after refused declaration = %d, want 0", n)
		}
		if n := len(loadEvents(t, st)); n != 0 {
			t.Errorf("events after refused declaration = %d, want 0", n)
		}
	})

	t.Run("old input after the revision is refused by name", func(t *testing.T) {
		dir := t.TempDir()
		original := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in-1")}}
		st := mustCreate(t, dir, "k4-oldinput", original)
		if err := st.ReviseTask(ctx, "a",
			TaskSpec{TaskID: "a", Input: []byte("in-2")}); err != nil {
			t.Fatal(err)
		}
		executed := 0
		r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
			executed++
			return Result{}, nil
		})
		// The pre-revision input no longer matches the rebound
		// definition: refused, pointing at ReviseTask, nothing executed.
		err := r.Run(ctx, original)
		if err == nil || !strings.Contains(err.Error(), `task "a"`) || !strings.Contains(err.Error(), "ReviseTask") {
			t.Fatalf("old-input run err = %v, want a refusal naming task a and pointing at ReviseTask", err)
		}
		if executed != 0 {
			t.Fatalf("executed %d tasks on refused specs, want 0", executed)
		}
		// The revised input runs at the new generation.
		revised := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in-2")}}
		if err := r.Run(ctx, revised); err != nil {
			t.Fatal(err)
		}
		if executed != 1 {
			t.Fatalf("executed %d times with the revised input, want 1", executed)
		}
	})
}

// TestV5RevisionInputsPersistWithTheBump: the byte carrier behind
// tasks.input_digest.  ReviseTask persists the TARGET's input in the same
// transaction as its bump; a dependent the invalidation closure bumped
// writes no row (its input did not change — that asymmetry is what lets
// readers tell "revised itself" from "bumped upstream"); RequestRevision's
// direct approval persists through the same reviseScope; and a reopened
// owner re-sends exactly the persisted bytes, which the run's digest
// binding accepts.
func TestV5RevisionInputsPersistWithTheBump(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "A", Input: []byte("in-a-1")},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}, Input: []byte("in-b-1")},
	}
	st := mustCreate(t, dir, "v5-rev-inputs", specs)

	// A fresh run carries no revision inputs: creation inputs are
	// re-derived from the run's binding, never stored.
	if rows, err := st.RevisionInputs(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("fresh run revision inputs = %v (err %v), want none", rows, err)
	}
	if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("in-a-2")}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.RevisionInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TaskID != "a" || rows[0].Generation != 2 || string(rows[0].Input) != "in-a-2" {
		t.Fatalf("revision inputs after one revision = %+v, want exactly a@2 with its bytes", rows)
	}
	// The closure bumped the dependent without new bytes: no row for b,
	// though its generation moved.
	if gen, err := st.TaskGeneration(ctx, "b"); err != nil || gen != 2 {
		t.Fatalf("dependent generation after the revision = %d (err %v), want bumped to 2", gen, err)
	}
	for _, row := range rows {
		if row.TaskID == "b" {
			t.Fatalf("dependent revision row = %+v, want none (closure bumps write no rows)", row)
		}
	}
	// RequestRevision below the allowance approves directly: the revision
	// and its input bytes land through the same write path.
	ask := &Confirmation{
		Options: []string{"continue", "abort"}, Allow: "continue",
		Revision: "r1", Body: []byte("allowance reached"),
	}
	if err := st.RequestRevision(ctx, "b",
		TaskSpec{TaskID: "b", Input: []byte("in-b-2")}, ask,
		func(_, _ int) bool { return false }, 3); err != nil {
		t.Fatal(err)
	}
	rows, err = st.RevisionInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("revision inputs after the direct approval = %+v, want one row per revision target", rows)
	}
	last := rows[len(rows)-1]
	if last.TaskID != "b" || last.Generation != 3 || string(last.Input) != "in-b-2" {
		t.Fatalf("direct-approval row = %+v, want b@3 with its bytes", last)
	}

	// A reopened owner re-sends the persisted bytes and the digest binding
	// accepts them — the whole point of the carrier.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	rows, err = st2.RevisionInputs(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("reopened revision inputs = %v (err %v), want both rows", rows, err)
	}
	revised := []TaskSpec{
		{TaskID: "a", Name: "A", Input: []byte("in-a-2")},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}, Input: []byte("in-b-2")},
	}
	if err := NewRunner(st2, recordingExecutor(nil, nil)).Run(ctx, revised); err != nil {
		t.Fatalf("run on the persisted revision inputs: %v", err)
	}
}

// TestV5StepsV4Forward: an accepted v4 file — user_requests and
// call_observations but neither v5 table — opens forward additively at
// v5: the version stamps, every v4 fact survives, and both v5
// capabilities work on the stepped database.  (A pre-v5 revision's bytes
// are simply absent, exactly as they were in v4: the digest binding, not
// a compatibility read, decides what a stepped run accepts.)
func TestV5StepsV4Forward(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "A", Input: []byte("in-a-1")}}
	st := mustCreate(t, dir, "v5-step", specs)
	if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("in-a-2")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Rewind the file to the v4 shape: drop the v5 delta tables and stamp 4.
	db, err := sql.Open("sqlite", filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"DROP TABLE task_revisions",
		"DROP TABLE task_sessions",
		"PRAGMA user_version = 4",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after the v4 rewind: %v", err)
	}
	defer st2.Close()
	var version int
	if err := st2.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version after the v4 step = %d, want %d", version, schemaVersion)
	}
	if st2.runID != "v5-step" {
		t.Fatalf("run id after the step = %q, want v5-step", st2.runID)
	}
	// The v4 facts survive: the task, its bumped generation, and the
	// revision inputs the v4 shape never had.
	if gen, err := st2.TaskGeneration(ctx, "a"); err != nil || gen != 2 {
		t.Fatalf("generation after the step = %d (err %v), want the v4 bump preserved", gen, err)
	}
	if rows, err := st2.RevisionInputs(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("revision inputs on the stepped file = %v (err %v), want none (v4 carried no bytes)", rows, err)
	}
	if identity, err := st2.Session(ctx, "a"); err != nil || identity != "" {
		t.Fatalf("session on the stepped file = %q (err %v), want empty", identity, err)
	}
	// Both v5 capabilities work on the stepped database.
	if err := st2.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("in-a-3")}); err != nil {
		t.Fatal(err)
	}
	rows, err := st2.RevisionInputs(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("post-step revision inputs = %v (err %v), want the new row", rows, err)
	}
	if rows[0].TaskID != "a" || rows[0].Generation != 3 || string(rows[0].Input) != "in-a-3" {
		t.Fatalf("post-step row = %+v, want a@3 with its bytes", rows[0])
	}
	if err := st2.SetSession(ctx, "a", "s-1"); err != nil {
		t.Fatal(err)
	}
	if identity, err := st2.Session(ctx, "a"); err != nil || identity != "s-1" {
		t.Fatalf("session round-trip on the stepped file = %q (err %v), want s-1", identity, err)
	}
	if err := st2.ClearSession(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if identity, err := st2.Session(ctx, "a"); err != nil || identity != "" {
		t.Fatalf("session after the clear = %q (err %v), want empty", identity, err)
	}
}
