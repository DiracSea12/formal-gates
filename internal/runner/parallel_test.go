package runner

// K3 acceptance tests for the §13.2 sample — parallel, join and retained
// success results: a failing or waiting task must not stop independent
// branches, two confirmations may be answered in any order, and a join
// only ever runs on complete inputs (§3.1).  The crash scenario re-executes
// this binary through TestMain (RUNNER_K3_SCENARIO) and really exits
// mid-run, like the K1/K2 children; everything else runs in-process with
// real concurrency guards under -race.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	envK3Scenario = "RUNNER_K3_SCENARIO"
	envK3Dir      = "RUNNER_K3_DIR"
	k3RunID       = "k3-crash"
)

// K3 child exit codes: 94 is the on-purpose exit right after the first
// admission committed; 95+ mark wiring bugs in the child itself.
const (
	exitK3Crash  = 94
	exitK3Wiring = 95
)

// k3Specs is the crash sample's shape: independent a and b, c joining both.
func k3Specs() []TaskSpec {
	return []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
		{TaskID: "c", Name: "task-c", DependsOn: []string{"a", "b"}, Input: []byte("input-c")},
	}
}

// k3Child drives the crash sample in RUNNER_K3_DIR: a finishes fast, b's
// executor blocks forever without side effects, c joins both.  The
// afterAdmit hook exits right after the FIRST admission (a's) commits, so
// the process dies while b is in flight — registered or not, both timings
// are possible and the parent must recover from either.
func k3Child() {
	dir := os.Getenv(envK3Dir)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "child: missing", envK3Dir)
		os.Exit(exitK3Wiring)
	}
	st, err := Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child open:", err)
		os.Exit(exitK3Wiring)
	}
	specs := k3Specs()
	if err := st.CreateRun(context.Background(), k3RunID, specs); err != nil {
		fmt.Fprintln(os.Stderr, "child create:", err)
		os.Exit(exitK3Wiring)
	}
	base := markerExecutor(dir)
	r := NewRunner(st, func(ctx context.Context, inv Invocation) (Result, error) {
		if inv.TaskID == "b" {
			<-make(chan struct{}) // b never settles in the child
		}
		return base(ctx, inv)
	})
	r.Parallelism = 2
	r.hooks.afterAdmit = func() { os.Exit(exitK3Crash) }
	err = r.Run(context.Background(), specs)
	fmt.Fprintln(os.Stderr, "child run returned:", err)
	os.Exit(exitK3Wiring) // unreachable in a correct child: b never settles
}

// §13.2 sample 1 — a fails, independent b succeeds, c joins both: the run
// reports the failure but b's admission is durable; after a restart with a
// healthy executor, b is not re-executed (its success is retained), a is
// retried, and the join runs exactly once on the now-complete inputs.
func TestK3FailureDoesNotBlockIndependentTasks(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := k3Specs()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateRun(ctx, "k3-fail", specs); err != nil {
		t.Fatal(err)
	}
	failA := true
	base := markerExecutor(dir)
	exec := func(ctx context.Context, inv Invocation) (Result, error) {
		if inv.TaskID == "a" && failA {
			// The attempt really ran (marker counts it) but returns no
			// receipt, exactly an executor that died mid-call.
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
		t.Fatalf("run err = %v, want the executor failure", err)
	}
	// The independent branch completed and its admission is durable.
	if s, eff := taskState(t, st, "b"); s != taskFinished || eff == "" {
		t.Errorf("task b = (%q,%q), want finished with effective result", s, eff)
	}
	if n := countInvocations(t, st, "b", invAdmitted); n != 1 {
		t.Errorf("task b admitted %d times, want 1", n)
	}
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Errorf("task b executed %d times, want 1", n)
	}
	// The failed task keeps K1 semantics: one unsettled intent, nothing
	// admitted.
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Errorf("task a pending intents = %d, want 1", n)
	}
	if n := countInvocations(t, st, "a", invAdmitted); n != 0 {
		t.Errorf("task a admitted %d times, want 0", n)
	}
	// The join never ran: c waits for complete inputs.
	if n := invocationTotal(t, st); n != 2 {
		t.Errorf("invocations = %d, want 2 (a and b only, c never started)", n)
	}
	if n := markerLines(t, dir, "c"); n != 0 {
		t.Errorf("task c executed %d times, want 0", n)
	}
	_, effB := taskState(t, st, "b")

	// A restarted owner with a healthy executor finishes the run; b's
	// admitted result is retained, not recomputed.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	failA = false
	r2 := NewRunner(st2, exec)
	r2.Parallelism = 2
	if err := r2.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	if n := markerLines(t, dir, "a"); n != 2 {
		t.Errorf("task a executed %d times, want 2 (failed attempt plus retry)", n)
	}
	if n := markerLines(t, dir, "b"); n != 1 {
		t.Errorf("task b executed %d times after restart, want still 1 (success retained)", n)
	}
	if n := markerLines(t, dir, "c"); n != 1 {
		t.Errorf("task c executed %d times, want exactly 1", n)
	}
	for _, task := range []string{"a", "b", "c"} {
		if n := countInvocations(t, st2, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
		if s, eff := taskState(t, st2, task); s != taskFinished || eff == "" {
			t.Errorf("task %s = (%q,%q), want finished with effective result", task, s, eff)
		}
	}
	if _, eff := taskState(t, st2, "b"); eff != effB {
		t.Errorf("task b effective result replaced on restart: %q, want retained %q", eff, effB)
	}
}

// §13.2 sample 1, waiting side — the formal reversal of K2's disclosed
// boundary: a gated task's pending decision does not block the independent
// b; the wait is reported only after b has run and been admitted.
func TestK3WaitingDoesNotBlockIndependentTasks(t *testing.T) {
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
	st := mustCreate(t, dir, "k3-wait", specs)
	var calls []string
	r := NewRunner(st, recordingExecutor(&calls, nil))
	if err := r.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("run with pending gate err = %v, want ErrWaitingDecision", err)
	}
	if strings.Join(calls, ",") != "b" {
		t.Fatalf("executions = %v, want only the independent task b", calls)
	}
	// a never even registered an intent: the gate precedes registration.
	if n := invocationTotal(t, st); n != 1 {
		t.Fatalf("invocations = %d, want 1 (b only)", n)
	}
	if n := countInvocations(t, st, "b", invAdmitted); n != 1 {
		t.Fatalf("task b admitted %d times, want 1", n)
	}
	// The wait is durable; answering it completes the run.
	pending, err := st.PendingDecisions(ctx)
	if err != nil || len(pending) != 1 || pending[0].TaskID != "a" {
		t.Fatalf("pending decisions = %v (err %v), want a's gate", pending, err)
	}
	if _, err := st.Answer(ctx, pending[0].DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "b,a" {
		t.Errorf("executions = %v, want [b a]", calls)
	}
	for _, task := range []string{"a", "b"} {
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
	}
}

// §13.2 sample 2 — two confirmations answered out of order: with both
// gates pending nothing executes; answering b alone completes b while the
// run keeps waiting for a; answering a afterwards lets the join run
// exactly once on both results.
func TestK3DecisionsAnsweredOutOfOrder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gate := func() *Confirmation {
		return &Confirmation{
			Options:  []string{"approve", "reject"},
			Allow:    "approve",
			Revision: "rev-1",
			Body:     []byte("approve?"),
		}
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a"), Confirm: gate()},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b"), Confirm: gate()},
		{TaskID: "c", Name: "task-c", DependsOn: []string{"a", "b"}, Input: []byte("input-c")},
	}
	st := mustCreate(t, dir, "k3-order", specs)
	var calls []string
	r := NewRunner(st, recordingExecutor(&calls, nil))
	if err := r.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("first run err = %v, want ErrWaitingDecision", err)
	}
	if len(calls) != 0 || invocationTotal(t, st) != 0 {
		t.Fatalf("executions %v / invocations %d with both gates pending, want none", calls, invocationTotal(t, st))
	}
	decisionID := func(task string) string {
		t.Helper()
		pending, err := st.PendingDecisions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range pending {
			if d.TaskID == task {
				return d.DecisionID
			}
		}
		t.Fatalf("no pending decision for task %s", task)
		return ""
	}
	// Out of order: b first.  Its branch completes; the run still waits
	// for a, and the join has not run on half-complete inputs.
	if _, err := st.Answer(ctx, decisionID("b"), "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("run after answering b err = %v, want ErrWaitingDecision for a", err)
	}
	if strings.Join(calls, ",") != "b" {
		t.Fatalf("executions after answering b = %v, want only b", calls)
	}
	if n := countInvocations(t, st, "c", invPending); n != 0 {
		t.Fatalf("task c registered %d intents on half-complete inputs", n)
	}
	// Then a: the join runs exactly once, on both results.
	if _, err := st.Answer(ctx, decisionID("a"), "approve"); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "b,a,c" {
		t.Errorf("executions = %v, want [b a c]", calls)
	}
	for _, task := range []string{"a", "b", "c"} {
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
	}
}

// §2 — one admission never waits for its batch: with parallelism 2, a's
// executor blocks while b finishes; b is admitted and durable while a is
// still in flight, and the join runs exactly once after both complete.
func TestK3AdmissionDoesNotWaitForTheBatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := k3Specs()
	st := mustCreate(t, dir, "k3-batch", specs)
	releaseA := make(chan struct{})
	aStarted := make(chan struct{})
	bAdmitted := make(chan struct{})
	var once sync.Once
	base := markerExecutor(dir)
	exec := func(ctx context.Context, inv Invocation) (Result, error) {
		if inv.TaskID == "a" {
			close(aStarted) // a is inside its executor until released
			<-releaseA
		}
		return base(ctx, inv)
	}
	r := NewRunner(st, exec)
	r.Parallelism = 2
	// afterAdmit fires inside admit, after the commit: the first — and
	// at that moment only possible — admission is b's.
	r.hooks.afterAdmit = func() { once.Do(func() { close(bAdmitted) }) }
	runDone := make(chan error, 1)
	go func() { runDone <- r.Run(ctx, specs) }()
	<-aStarted
	<-bAdmitted
	// b is durably admitted while a is still in flight.
	if n := countEvents(t, st, eventKindAdmitted); n != 1 {
		t.Errorf("admission events while a in flight = %d, want 1 (b's)", n)
	}
	if s, eff := taskState(t, st, "b"); s != taskFinished || eff == "" {
		t.Errorf("task b = (%q,%q) while a in flight, want finished with effective result", s, eff)
	}
	if s, _ := taskState(t, st, "a"); s != taskRunning {
		t.Errorf("task a state = %q while in flight, want running", s)
	}
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Errorf("task a pending intents = %d while in flight, want 1", n)
	}
	if n := countInvocations(t, st, "c", invPending); n != 0 {
		t.Errorf("task c registered %d intents before its join was complete", n)
	}
	close(releaseA)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "b", "c"} {
		if n := markerLines(t, dir, task); n != 1 {
			t.Errorf("task %s executed %d times, want exactly 1", task, n)
		}
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
	}
}

// §4.1 — the local bound is real: with Parallelism 2 two executors are
// inside at once (they rendezvous); with the zero value — and anything
// below one — a re-entrancy guard proves they never overlap.
func TestK3ParallelismIsBounded(t *testing.T) {
	ctx := context.Background()

	t.Run("parallelism two overlaps", func(t *testing.T) {
		dir := t.TempDir()
		specs := []TaskSpec{
			{TaskID: "a", Name: "A"},
			{TaskID: "b", Name: "B"},
		}
		st := mustCreate(t, dir, "k3-par2", specs)
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		exec := func(_ context.Context, inv Invocation) (Result, error) {
			entered <- struct{}{}
			<-release
			return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
		}
		r := NewRunner(st, exec)
		r.Parallelism = 2
		runDone := make(chan error, 1)
		go func() { runDone <- r.Run(ctx, specs) }()
		for i := 0; i < 2; i++ {
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("second executor never entered — no overlap with Parallelism 2")
			}
		}
		close(release)
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		for _, task := range []string{"a", "b"} {
			if n := countInvocations(t, st, task, invAdmitted); n != 1 {
				t.Errorf("task %s admitted %d times, want 1", task, n)
			}
		}
	})

	for _, tc := range []struct {
		name  string
		value int
	}{
		{"zero value stays sequential", 0},
		{"values below one stay sequential", -3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specs := []TaskSpec{
				{TaskID: "a", Name: "A"},
				{TaskID: "b", Name: "B"},
			}
			st := mustCreate(t, dir, "k3-seq", specs)
			var inflight atomic.Int32
			var overlapped atomic.Bool
			exec := func(_ context.Context, inv Invocation) (Result, error) {
				if inflight.Add(1) > 1 {
					overlapped.Store(true)
				}
				time.Sleep(2 * time.Millisecond) // widen the would-be overlap window
				inflight.Add(-1)
				return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
			}
			r := NewRunner(st, exec)
			r.Parallelism = tc.value
			if err := r.Run(ctx, specs); err != nil {
				t.Fatal(err)
			}
			if overlapped.Load() {
				t.Error("executors overlapped below the parallelism bound")
			}
			for _, task := range []string{"a", "b"} {
				if n := countInvocations(t, st, task, invAdmitted); n != 1 {
					t.Errorf("task %s admitted %d times, want 1", task, n)
				}
			}
		})
	}
}

// §13.2 crash sample, parallel form — the child dies (real os.Exit) right
// after the first admission commits while a sibling task is in flight;
// the reopened owner recovers with no missed execution and no double
// admission.  At the crash point b was either registered-but-unsettled or
// not yet registered — nothing asserts which; both must land on the same
// outcome.
func TestK3CrashDuringParallelRunRecovers(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), envK3Scenario+"=1", envK3Dir+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != exitK3Crash {
		t.Fatalf("child exit = %v (want code %d); output:\n%s", err, exitK3Crash, out)
	}
	ctx := context.Background()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.runID != k3RunID {
		t.Fatalf("reopened run id = %q, want %q", st.runID, k3RunID)
	}
	// The crash point is durable: a admitted exactly once, a's side
	// effect on disk; b's state is the two-timings case and asserted
	// only through the recovered outcome below.
	if n := countInvocations(t, st, "a", invAdmitted); n != 1 {
		t.Fatalf("task a admitted %d times before recovery, want 1", n)
	}
	if n := markerLines(t, dir, "a"); n != 1 {
		t.Fatalf("task a executed %d times before recovery, want 1", n)
	}

	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, k3Specs()); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "b", "c"} {
		if n := markerLines(t, dir, task); n != 1 {
			t.Errorf("task %s executed %d times, want exactly 1", task, n)
		}
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
		if n := countInvocations(t, st, task, invPending); n != 0 {
			t.Errorf("task %s left %d unsettled intents", task, n)
		}
		if s, eff := taskState(t, st, task); s != taskFinished || eff == "" {
			t.Errorf("task %s = (%q,%q), want finished with effective result", task, s, eff)
		}
	}
	// Events agree with the admissions: one admitted event per task,
	// each matching its task's effective invocation.
	events := loadEvents(t, st)
	if len(events) != 3 {
		t.Fatalf("events = %d, want one per admitted task (3)", len(events))
	}
	seen := map[string]bool{}
	for i, e := range events {
		if e.kind != eventKindAdmitted {
			t.Errorf("event %d kind = %q, want %q", i, e.kind, eventKindAdmitted)
		}
		var p admittedEvent
		if err := json.Unmarshal([]byte(e.payloadJSON), &p); err != nil {
			t.Fatalf("event %d payload: %v", i, err)
		}
		if _, eff := taskState(t, st, p.TaskID); eff != p.InvocationID {
			t.Errorf("event %d (%s) does not match task %s effective result", i, p.InvocationID, p.TaskID)
		}
		seen[p.TaskID] = true
	}
	if len(seen) != 3 {
		t.Errorf("admission events covered tasks %v, want all of a, b and c", seen)
	}
}

// The K3 review's P3 performance guard: the ready query asks the store
// for exactly the free slots' worth of dispatchable work in one LIMITed
// scan, instead of re-reading every remaining ready task and re-checking
// its gate on each completion.  Under the old loop, 1,000 ungated
// independent tasks in the default sequential mode meant ~500,500 gate
// checks (6.341s measured on an idle machine, against a 0.381s
// baseline); the batched predicate runs the same run in well under a
// second (0.44s here).  The timing guard is therefore armed explicitly
// with RUNNER_SCALE_TIMING=1: under `go test ./...` this package runs
// concurrently with the multi-minute validate suite, whose bursty CPU
// contention inflates even the fixed loop to ~6s of wall clock — no
// constant, and no same-load reference run, separates that from the
// idle-machine defect dependably (measured: fixed 5.7s contended vs
// 6.3s defect idle).  Armed, the run fails only when it is BOTH past
// the 3s absolute floor (far above the fixed ~0.5s, far below the
// broken 6.3s) AND past 15x a same-shape 200-task reference measured
// right after — the shape term absorbs uniform load, the floor anchors
// the defect.  Unarmed (and under -race, where the pure-Go SQLite
// driver slows far past any honest clock; raceBuild) the test asserts
// correctness alone: Run completes and every task is admitted exactly
// once.
func TestK3ScaleThousandIndependentTasks(t *testing.T) {
	ctx := context.Background()
	run := func(n int) time.Duration {
		dir := t.TempDir()
		specs := make([]TaskSpec, 0, n)
		for i := 0; i < n; i++ {
			specs = append(specs, TaskSpec{
				TaskID: fmt.Sprintf("t%04d", i),
				Name:   "bulk",
			})
		}
		st := mustCreate(t, dir, "k3-scale", specs)
		start := time.Now()
		if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		var admitted int
		if err := st.db.QueryRow(
			"SELECT COUNT(*) FROM invocations WHERE run_id=? AND state=?",
			st.runID, invAdmitted).Scan(&admitted); err != nil {
			t.Fatal(err)
		}
		if admitted != n {
			t.Fatalf("admitted %d of %d tasks, want every task admitted exactly once", admitted, n)
		}
		return elapsed
	}
	elapsed := run(1000)
	reference := run(200)
	if os.Getenv("RUNNER_SCALE_TIMING") == "1" && !raceBuild &&
		elapsed >= 3*time.Second && elapsed >= 15*reference {
		t.Fatalf("run of 1000 independent tasks took %v (200-task reference %v), want a linear-shape run under 3s", elapsed, reference)
	}
	t.Logf("1000-task run: %v; 200-task reference: %v", elapsed, reference)
}
