package runner

// K4 rework 2 acceptance tests — one formal test per second re-review
// probe scenario: dead requests must not hold the task's repair entry
// nor the terminal judgment, a dead grant must raise no allowance, the
// nil-consult recovery must tell constrained calls from free ones, and
// the durable restart ask must outlive an owner that omits the optional
// Takeover consult — including a real os.Exit.  Every test drives the
// public API over real durable state with deterministic executors.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// deadRequestSetup finishes a→b, asks b at a zero allowance, ages the
// request one generation through a raw upstream ReviseTask, reopens,
// refuses the old id as stale and runs the new generation to
// completion — leaving one dead request row behind and the task's
// current generation fully executed.  state picks the shape: "pending"
// leaves the ask unanswered, "granted" allows it first.
func deadRequestSetup(t *testing.T, runID, state string) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a1")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
	}
	st := mustCreate(t, dir, runID, specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	oldID := reworkAsk(t, st, "b", []byte("b2"))
	if state == reqGranted {
		if _, err := st.Answer(ctx, oldID, "allow"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("a2")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Answer(ctx, oldID, "allow"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("dead request answer err = %v, want ErrDecisionStale", err)
	}
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a2")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, revised); err != nil {
		t.Fatalf("new-generation run err = %v, want nil", err)
	}
	return st, ctx
}

// 失效请求不再占住返修入口: a dead pending or granted request neither
// blocks the current generation's new ask nor surfaces as recoverable
// work — OpenRequests lists nothing, and the new ask is a fresh row the
// allowance decides on.
func TestK4Rework2StaleRequestsDoNotBlockNewGeneration(t *testing.T) {
	for _, state := range []string{reqPending, reqGranted} {
		t.Run(state, func(t *testing.T) {
			st, ctx := deadRequestSetup(t, "k4-r2-open", state)
			open, err := st.OpenRequests(ctx)
			if err != nil || len(open) != 0 {
				t.Fatalf("open requests = %v (err %v), want none (dead binding is history)", open, err)
			}
			err = st.RequestRevision(ctx, "b",
				TaskSpec{TaskID: "b", Input: []byte("b2")},
				reworkExhaustion(), repairLimitPolicy, 0)
			if !errors.Is(err, ErrWaitingDecision) {
				t.Fatalf("new-generation ask err = %v, want ErrWaitingDecision", err)
			}
			pending, err := st.PendingDecisions(ctx)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending after the new ask = %v (err %v), want the new question", pending, err)
			}
			// The dead row stays as history, unmutated, at its old
			// generation — the new ask is a fresh row.
			var dead int
			if err := st.db.QueryRow(
				"SELECT COUNT(*) FROM user_requests WHERE run_id=? AND task_id=? AND kind=? AND target_generation=? AND state=?",
				st.runID, "b", kindRevision, 1, state).Scan(&dead); err != nil {
				t.Fatal(err)
			}
			if dead != 1 {
				t.Fatalf("dead rows in state %q at generation 1 = %d, want 1 kept as history", state, dead)
			}
		})
	}
}

// 失效 granted 不计入额度: with the current generation executed, the
// dead grant raises nothing — round 0 of the current generation asks
// against the real allowance instead of riding the phantom grant to a
// direct approval.
func TestK4Rework2StaleGrantRaisesNoAllowance(t *testing.T) {
	st, ctx := deadRequestSetup(t, "k4-r2-grant", reqGranted)
	if gen := taskGeneration(t, st, "b"); gen != 2 {
		t.Fatalf("task b generation before the ask = %d, want 2", gen)
	}
	if err := st.RequestRevision(ctx, "b",
		TaskSpec{TaskID: "b", Input: []byte("b2")},
		reworkExhaustion(), repairLimitPolicy, 0); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("ask after a dead grant err = %v, want ErrWaitingDecision (the grant raised nothing)", err)
	}
	if gen := taskGeneration(t, st, "b"); gen != 2 {
		t.Fatalf("task a generation after the ask = %d, want 2 (nothing executed on the dead grant)", gen)
	}
	if n := countRequests(t, st, "b", reqGranted); n != 1 {
		t.Fatalf("dead granted rows = %d, want 1 kept as history", n)
	}
}

// 失效请求不拖住终态: with every task finished and only dead history
// left open, the run is terminal and refuses even the idempotent replay
// of an already accepted gate answer.
func TestK4Rework2StaleRequestDoesNotKeepRunAnswerable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gate := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-1",
		Body:     []byte("run c?"),
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a1")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
		{TaskID: "c", Name: "task-c", Input: []byte("c1"), Confirm: gate},
	}
	st := mustCreate(t, dir, "k4-r2-term", specs)
	qs, err := st.PendingDecisions(ctx)
	if err != nil || len(qs) != 1 {
		t.Fatalf("gates = %v (err %v), want c's", qs, err)
	}
	gateID := qs[0].DecisionID
	if _, err := st.Answer(ctx, gateID, "allow"); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	reworkAsk(t, st, "b", []byte("b2"))
	if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("a2")}); err != nil {
		t.Fatal(err)
	}
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a2")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
		{TaskID: "c", Name: "task-c", Input: []byte("c1"), Confirm: gate},
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Answer(ctx, gateID, "allow"); !errors.Is(err, ErrRunFinished) {
		t.Fatalf("terminal replay err = %v, want ErrRunFinished", err)
	}
}

// 已落盘的 restart 询问在漏配 Takeover 的恢复下存活 (Close/Open form):
// the ask's invocation is neither superseded nor replaced — zero
// executions, the question still listed, the run still waiting.
func TestK4Rework2PendingRestartSurvivesNilTakeoverReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "k4-r2-keep", specs)
	var original string
	if err := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		original = inv.InvocationID
		return Result{}, errors.New("owner disconnected before the receipt")
	}).Run(ctx, specs); err == nil {
		t.Fatal("expected the missing receipt")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	asking := NewRunner(st, func(context.Context, Invocation) (Result, error) {
		t.Error("execution while the ask waits")
		return Result{}, nil
	})
	asking.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
		return TakeoverOutcome{}, nil // the dead-session fact
	}
	asking.Restart = restartQuestion()
	if err := asking.Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("asking run err = %v, want ErrWaitingDecision", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	// The new owner omits the optional consult: nothing it can do may
	// replace the call the unanswered ask is about.
	executions := 0
	err = NewRunner(st, func(context.Context, Invocation) (Result, error) {
		executions++
		return Result{}, nil
	}).Run(ctx, specs)
	if executions != 0 {
		t.Fatalf("nil-consult executions = %d, want 0 (the ask constrains the call)", executions)
	}
	if !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("nil-consult run err = %v, want ErrWaitingDecision (the ask survived)", err)
	}
	if state, _ := invocationState(t, st, original); state != invPending {
		t.Fatalf("original call state = %q, want pending (nothing superseded)", state)
	}
	if s, _ := taskState(t, st, "a"); s != taskRunning {
		t.Fatalf("task a state = %q, want running (out of the ready set)", s)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("asks after the nil-consult run = %v (err %v), want the restart ask", pending, err)
	}
}

// rework2ExitEnv marks the child mode of the process-exit form below.
const rework2ExitEnv = "K4_REWORK2_EXIT_DIR"

// 已落盘的 restart 询问在真实进程退出后存活: a child process dies
// (os.Exit 87) with the ask unanswered; the parent reopens without the
// optional consult and must still execute nothing and still wait.
func TestK4Rework2RestartAskSurvivesProcessExit(t *testing.T) {
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	if dir := os.Getenv(rework2ExitEnv); dir != "" {
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.CreateRun(context.Background(), "k4-r2-exit", specs); err != nil {
			t.Fatal(err)
		}
		r := NewRunner(st, func(context.Context, Invocation) (Result, error) {
			return Result{}, errors.New("session unavailable, no final receipt")
		})
		if err = r.Run(context.Background(), specs); err == nil {
			t.Fatal("expected the missing receipt")
		}
		r.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
			return TakeoverOutcome{}, nil
		}
		r.Restart = restartQuestion()
		if err = r.Run(context.Background(), specs); !errors.Is(err, ErrWaitingDecision) {
			t.Fatal(err)
		}
		pending, err := st.PendingDecisions(context.Background())
		if err != nil || len(pending) != 1 {
			t.Fatal(pending, err)
		}
		os.Exit(87) // the process dies with its ask unanswered
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestK4Rework2RestartAskSurvivesProcessExit$")
	cmd.Env = append(os.Environ(), rework2ExitEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 87 {
		t.Fatalf("child exit = %v (want code 87); output:\n%s", err, out)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pending, err := st.PendingDecisions(context.Background())
	if err != nil || len(pending) != 1 {
		t.Fatalf("asks after the exit = %v (err %v), want the durable one", pending, err)
	}
	executions := 0
	err = NewRunner(st, func(context.Context, Invocation) (Result, error) {
		executions++
		return Result{}, nil
	}).Run(context.Background(), specs)
	if executions != 0 || !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("recovery after the exit: executions = %d err = %v, want 0 and ErrWaitingDecision", executions, err)
	}
}

// nil 恢复按调用区分: a dies without receipt behind b's still-closed
// gate; the next owner answers the gate, reports a's session dead (the
// ask lands) and loses b's receipt too; the third owner omits the
// consult — a's constrained call is kept for its ask while b's free
// intent is superseded and re-run exactly as in K1.
func TestK4Rework2NilRecoverySeparatesConstrainedFromFreeCalls(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gate := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-1",
		Body:     []byte("run b?"),
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b"), Confirm: gate},
	}
	st := mustCreate(t, dir, "k4-r2-mixed", specs)
	// Owner 1: a's call dies without receipt; the gate keeps b
	// undispatched, so only a's intent is pending for the next owner.
	if err := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		return Result{}, errors.New("owner disconnected")
	}).Run(ctx, specs); err == nil {
		t.Fatal("expected the missing receipt")
	}
	qs, err := st.PendingDecisions(ctx)
	if err != nil || len(qs) != 1 {
		t.Fatalf("gates = %v (err %v), want b's", qs, err)
	}
	if _, err := st.Answer(ctx, qs[0].DecisionID, "allow"); err != nil {
		t.Fatal(err)
	}
	// Owner 2: the consult reports a's session dead and asks; b then
	// runs once and itself dies without receipt.
	asking := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		if inv.TaskID != "b" {
			t.Errorf("unexpected execution of task %s while asking", inv.TaskID)
		}
		return Result{}, errors.New("b lost its receipt too")
	})
	asking.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
		return TakeoverOutcome{}, nil
	}
	asking.Restart = restartQuestion()
	if err := asking.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "b lost its receipt too") {
		t.Fatalf("asking run err = %v, want b's missing receipt riding along", err)
	}
	// Owner 3 omits the consult: a's call is kept for its ask, b's
	// unconstrained intent takes the K1 supersede and re-runs.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	origA := pendingInvocationID(t, st, "a")
	origB := pendingInvocationID(t, st, "b")
	var ran []string
	err = NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		ran = append(ran, inv.TaskID)
		return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
	}).Run(ctx, specs)
	if !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("owner-3 run err = %v, want ErrWaitingDecision on a's ask", err)
	}
	if len(ran) != 1 || ran[0] != "b" {
		t.Fatalf("owner-3 executed %v, want exactly b (a waits for its ask)", ran)
	}
	if state, _ := invocationState(t, st, origA); state != invPending {
		t.Errorf("a's constrained call state = %q, want pending (kept)", state)
	}
	if state, _ := invocationState(t, st, origB); state != invSuperseded {
		t.Errorf("b's unconstrained call state = %q, want superseded (the K1 half)", state)
	}
	if s, _ := taskState(t, st, "a"); s != taskRunning {
		t.Errorf("task a state = %q, want running (kept out of the ready set)", s)
	}
	if s, _ := taskState(t, st, "b"); s != taskFinished {
		t.Errorf("task b state = %q, want finished by the replacement", s)
	}
	if n := countInvocations(t, st, "b", invAdmitted); n != 1 {
		t.Errorf("task b admitted %d times, want 1 (the replacement)", n)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil || len(pending) != 1 || pending[0].TaskID != "a" {
		t.Fatalf("asks after owner 3 = %v (err %v), want a's restart ask", pending, err)
	}
}
