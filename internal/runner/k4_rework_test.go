package runner

// K4 rework acceptance tests — one formal test per re-review probe
// scenario (F1, F2, F3, F6, F7, F8 here; F4 and F5 live in
// takeover_test.go, whose nil-consult and dead-session cases carry
// them).  Every test drives the public API over real durable state with
// deterministic executors, mirroring the independent probes the rework
// was gated on.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// reworkExhaustion is the shared ask of the revision tests.
func reworkExhaustion() *Confirmation {
	return &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-2",
		Body:     []byte("allow one more repair round?"),
	}
}

// mustReopen closes nothing and just opens an existing directory.
func mustReopen(t *testing.T, dir string) *Store {
	t.Helper()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// reworkAsk reaches a pending revision request for taskID with a zero
// allowance (round 0 >= 0 asks immediately) and returns its id.
func reworkAsk(t *testing.T, st *Store, taskID string, input []byte) string {
	t.Helper()
	if err := st.RequestRevision(context.Background(), taskID,
		TaskSpec{TaskID: taskID, Input: input},
		reworkExhaustion(), repairLimitPolicy, 0); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("ask err = %v, want ErrWaitingDecision", err)
	}
	pending, err := st.PendingDecisions(context.Background())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending after the ask = %v (err %v), want the request", pending, err)
	}
	return pending[0].DecisionID
}

// F1 — 初始额度持久绑定: the first use binds the initial allowance on
// the request rows; a reconstructed (or same-owner) caller passing a
// different value is refused by name with zero residue, and re-sending
// the bound value proceeds.  Raising the allowance goes through a user
// grant, never a larger recovery argument.
func TestK4ReworkAllowanceInitialBindsAcrossReconstruction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("v1")}}
	st := mustCreate(t, dir, "k4-f1", specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	// Round 0 against allowance 2: direct, and the value binds.
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v2")},
		reworkExhaustion(), repairLimitPolicy, 2); err != nil {
		t.Fatalf("first revision err = %v, want nil (round 0 < 2)", err)
	}
	// Same owner, drifted argument: named refusal, zero residue.
	err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v3")},
		reworkExhaustion(), repairLimitPolicy, 3)
	if !errors.Is(err, ErrAllowanceDrift) ||
		!strings.Contains(err.Error(), "increases go through the user") {
		t.Fatalf("drifted same-owner err = %v, want ErrAllowanceDrift naming the rule", err)
	}
	if n := countRequests(t, st, "a", ""); n != 1 {
		t.Fatalf("request rows after the drift refusal = %d, want still 1 (zero residue)", n)
	}
	// Reconstruction drift: reopen and pass a larger initial — the
	// persisted binding, not the argument, decides.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st = mustReopen(t, dir)
	defer st.Close()
	err = st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v3")},
		reworkExhaustion(), repairLimitPolicy, 3)
	if !errors.Is(err, ErrAllowanceDrift) {
		t.Fatalf("reconstructed drift err = %v, want ErrAllowanceDrift", err)
	}
	if n := countRequests(t, st, "a", ""); n != 1 {
		t.Fatalf("request rows after the reconstructed drift = %d, want still 1", n)
	}
	// The bound value still works: round 1 < 2 direct-approves.
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v3")},
		reworkExhaustion(), repairLimitPolicy, 2); err != nil {
		t.Fatalf("bound-value revision err = %v, want nil (round 1 < 2)", err)
	}
	if gen := taskGeneration(t, st, "a"); gen != 3 {
		t.Fatalf("task a generation = %d, want 3 (two direct rounds)", gen)
	}
}

// F2 — 跨代问询失效: a revision request binds the task's real generation
// at creation.  A normal upstream revision that moves the task into a
// new generation invalidates the open request by itself — answering it
// is ErrDecisionStale, it leaves the pending list, and it never holds
// the run in a wait.
func TestK4ReworkInquiryExpiresWithTaskGeneration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a1")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
	}
	st := mustCreate(t, dir, "k4-f2", specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	reqID := reworkAsk(t, st, "b", []byte("b2"))
	// A normal upstream revision also changes B's scope/generation.
	if err := st.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("a2")}); err != nil {
		t.Fatal(err)
	}
	if gen := taskGeneration(t, st, "b"); gen != 2 {
		t.Fatalf("task b generation after the upstream revision = %d, want 2", gen)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Answer(ctx, reqID, "allow"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("old-generation request answer err = %v, want ErrDecisionStale", err)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending after the scope moved on = %d, want 0 (dead request not askable)", len(pending))
	}
	if n := countRequests(t, st, "b", reqPending); n != 1 {
		t.Fatalf("dead request rows = %d pending, want 1 kept as history", n)
	}
	// The dead request never waits the run: the revised scope finishes.
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("a2")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("b1")},
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, revised); err != nil {
		t.Fatalf("run with a dead request err = %v, want nil", err)
	}
}

// F3 — granted 防假完成＋重放非 ErrRunFinished: a granted-but-unexecuted
// revision is unfinished business — the drained run reports
// ErrRevisionGranted instead of nil, executes nothing, exposes the
// recoverable identity through OpenRequests, replays its answer as the
// original receipt (not ErrRunFinished), refuses a different answer as a
// conflict, and only the completion call consumes it.  A grant whose
// scope an upstream revision moved past is dead: the completion call is
// refused by name and the run is not held.
func TestK4ReworkGrantedRevisionPreventsFalseCompletion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("v1")}}
	st := mustCreate(t, dir, "k4-f3", specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	reqID := reworkAsk(t, st, "a", []byte("v2"))
	first, err := st.Answer(ctx, reqID, "allow")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// The reopened owner runs the OLD specs and must not be told "done".
	calls := 0
	exec := func(context.Context, Invocation) (Result, error) { calls++; return Result{}, nil }
	if err := NewRunner(st, exec).Run(ctx, specs); !errors.Is(err, ErrRevisionGranted) {
		t.Fatalf("reopened run err = %v, want ErrRevisionGranted", err)
	}
	if calls != 0 {
		t.Fatalf("executions while the grant waits for material = %d, want 0", calls)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending decisions = %d, want 0 (the grant is not a question)", len(pending))
	}
	open, err := st.OpenRequests(ctx)
	if err != nil || len(open) != 1 {
		t.Fatalf("open requests = %v (err %v), want the granted one", open, err)
	}
	if open[0].RequestID != reqID || open[0].Kind != kindRevision || open[0].State != reqGranted {
		t.Fatalf("open request = %+v, want the granted revision %s", open[0], reqID)
	}
	// The same answer replays as the original receipt — the run is not
	// terminal while the granted work is unexecuted.
	replay, err := st.Answer(ctx, reqID, "allow")
	if err != nil {
		t.Fatalf("replay on the nonterminal run err = %v, want nil (original receipt)", err)
	}
	if replay != first {
		t.Fatalf("replay receipt = %+v, want the original %+v", replay, first)
	}
	if _, err := st.Answer(ctx, reqID, "refuse"); !errors.Is(err, ErrDecisionConflict) {
		t.Fatalf("different answer err = %v, want ErrDecisionConflict", err)
	}
	// The completion call consumes the grant and the run finishes.
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v2")},
		reworkExhaustion(), repairLimitPolicy, 0); err != nil {
		t.Fatalf("completion call err = %v, want nil", err)
	}
	if n := countRequests(t, st, "a", reqExecuted); n != 1 {
		t.Fatalf("executed requests after completion = %d, want 1", n)
	}
	if err := NewRunner(st, exec).Run(ctx, []TaskSpec{{TaskID: "a", Input: []byte("v2")}}); err != nil {
		t.Fatalf("run after completion err = %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("executions after completion = %d, want 1", calls)
	}

	// Dead grant: an upstream revision moves the scope past the grant.
	st2 := mustCreate(t, t.TempDir(), "k4-f3dead", specs)
	if err := NewRunner(st2, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	deadID := reworkAsk(t, st2, "a", []byte("v2"))
	if _, err := st2.Answer(ctx, deadID, "allow"); err != nil {
		t.Fatal(err)
	}
	if err := st2.ReviseTask(ctx, "a", TaskSpec{TaskID: "a", Input: []byte("v2")}); err != nil {
		t.Fatal(err)
	}
	err = st2.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Input: []byte("v3")},
		reworkExhaustion(), repairLimitPolicy, 0)
	if !errors.Is(err, ErrRevisionGrantStale) {
		t.Fatalf("completion after the scope moved err = %v, want ErrRevisionGrantStale", err)
	}
	if n := countRequests(t, st2, "a", reqGranted); n != 1 {
		t.Fatalf("dead grant state rows = %d granted, want 1 (kept for the domain)", n)
	}
	// The dead grant holds nothing: the revised scope runs to nil.
	if err := NewRunner(st2, recordingExecutor(nil, nil)).Run(ctx,
		[]TaskSpec{{TaskID: "a", Input: []byte("v2")}}); err != nil {
		t.Fatalf("run with a dead grant err = %v, want nil", err)
	}
}

// F4 — 观察模式跨 owner 保留 (the durable half; the nil-consult recovery
// behaviour is TestK4NilTakeoverSupersedesOnlyUnobservedCalls): the
// Observe fact lands in call_observations, survives Close/Open, and is
// still there after the observed invocation settles — read only while
// the invocation is pending.
func TestK4ReworkObservationFactIsDurable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "k4-f4fact", specs)
	calls := 0
	execute := func(context.Context, Invocation) (Result, error) {
		calls++
		if calls == 1 {
			return Result{}, errors.New("owner disconnected while the original call still ran")
		}
		return Result{}, nil
	}
	observing := NewRunner(st, execute)
	observing.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
		return TakeoverOutcome{Observe: true}, nil
	}
	if err := observing.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("first run err = %v, want the executor failure", err)
	}
	if err := observing.Run(ctx, specs); !errors.Is(err, ErrCallObserved) {
		t.Fatalf("observing run err = %v, want ErrCallObserved", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var rows int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM call_observations").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("observation rows after reopen = %d, want 1 (the fact survived)", rows)
	}
	// A nil-consult owner is refused, not blind.
	if err := NewRunner(st, execute).Run(ctx, specs); !errors.Is(err, ErrTakeoverRequired) {
		t.Fatalf("nil-consult run err = %v, want ErrTakeoverRequired", err)
	}
	if calls != 1 {
		t.Fatalf("total executions = %d, want 1", calls)
	}
}

// F5 — 失效需决定 (the nil-Restart half; the decision flow itself is
// TestK4TakeoverDeadSessionRequiresReplacementDecision): a correctly
// configured consult reporting the dead-session fact without a Restart
// question is a named refusal with zero residue — the fact alone never
// replaces the execution identity.
func TestK4ReworkDeadSessionFactNeedsRestartQuestion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "k4-f5nil", specs)
	calls := 0
	execute := func(context.Context, Invocation) (Result, error) {
		calls++
		if calls == 1 {
			return Result{}, errors.New("session unavailable")
		}
		return Result{}, nil
	}
	if err := NewRunner(st, execute).Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("first run err = %v, want the missing receipt", err)
	}
	r := NewRunner(st, execute)
	r.Takeover = func(context.Context, Invocation) (TakeoverOutcome, error) {
		return TakeoverOutcome{}, nil // the documented dead/invalidated fact
	}
	if err := r.Run(ctx, specs); !errors.Is(err, ErrRestartUnavailable) {
		t.Fatalf("dead-session run without Restart err = %v, want ErrRestartUnavailable", err)
	}
	if calls != 1 {
		t.Fatalf("executions = %d, want 1 (the fact authorized nothing)", calls)
	}
	var reqs int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM user_requests").Scan(&reqs); err != nil {
		t.Fatal(err)
	}
	if reqs != 0 {
		t.Fatalf("request rows after the refusal = %d, want 0 (zero residue)", reqs)
	}
	if n := countInvocations(t, st, "a", invPending); n != 1 {
		t.Fatalf("pending invocations = %d, want still 1 (nothing voided)", n)
	}
}

// F6 — 0/1/2 轮边界: the policy receives the 0-BASED count of executed
// rounds and the bound initial plus user grants — the boundary the
// accepted graph.py repair() defines.  initial=1 permits round 0 and
// asks on round 1; initial=2 permits rounds 0 and 1 and asks on round 2
// (the grant then raises the allowance, so round 3 asks again at
// allowance 3).
func TestK4ReworkAllowanceBoundaryZeroOneTwoRounds(t *testing.T) {
	ctx := context.Background()
	// walk drives n consecutive revisions, granting and completing every
	// ask, and returns the rounds the policy saw.
	walk := func(initial, n int) []int {
		dir := t.TempDir()
		specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("v1")}}
		st := mustCreate(t, dir, "k4-f6", specs)
		if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
			t.Fatal(err)
		}
		var rounds []int
		policy := func(round, allowance int) bool {
			rounds = append(rounds, round)
			return round >= allowance
		}
		for i := 0; i < n; i++ {
			input := []byte{118, 50 + byte(i)} // v2, v3, ...
			err := st.RequestRevision(ctx, "a", TaskSpec{TaskID: "a", Input: input},
				reworkExhaustion(), policy, initial)
			if errors.Is(err, ErrWaitingDecision) {
				pending, perr := st.PendingDecisions(ctx)
				if perr != nil || len(pending) != 1 {
					t.Fatalf("pending at the ask = %v (err %v)", pending, perr)
				}
				if _, aerr := st.Answer(ctx, pending[0].DecisionID, "allow"); aerr != nil {
					t.Fatal(aerr)
				}
				err = st.RequestRevision(ctx, "a", TaskSpec{TaskID: "a", Input: input},
					reworkExhaustion(), policy, initial)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		return rounds
	}
	if got, want := walk(1, 2), []int{0, 1}; !slices.Equal(got, want) {
		t.Errorf("initial=1 policy rounds = %v, want %v (round 0 permitted, round 1 asks)", got, want)
	}
	if got, want := walk(1, 3), []int{0, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("initial=1 with a grant policy rounds = %v, want %v (grant raised allowance 1→2)", got, want)
	}
	if got, want := walk(2, 4), []int{0, 1, 2, 3}; !slices.Equal(got, want) {
		t.Errorf("initial=2 policy rounds = %v, want %v (rounds 0 and 1 permitted, round 2 asks)", got, want)
	}
}

// F7 — 未知任务零残留: a mistyped task id is a named refusal before any
// branch writes — no request row, no decision row, no event, nothing
// presented as askable, and the real run is unaffected.
func TestK4ReworkUnknownRevisionTaskLeavesNoResidue(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "k4-f7", specs)
	err := st.RequestRevision(ctx, "typo",
		TaskSpec{TaskID: "typo", Input: []byte("x")},
		reworkExhaustion(), repairLimitPolicy, 1)
	if err == nil || !strings.Contains(err.Error(), `no task "typo"`) || !strings.Contains(err.Error(), "k4-f7") {
		t.Fatalf("unknown task err = %v, want a refusal naming the task and the run", err)
	}
	if errors.Is(err, ErrWaitingDecision) {
		t.Fatal("the unknown task persisted a question")
	}
	var reqs, events int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM user_requests").Scan(&reqs); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow("SELECT COUNT(*) FROM events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if reqs != 0 || events != 0 || countDecisions(t, st, "") != 0 {
		t.Fatalf("residue: requests=%d decisions=%d events=%d, want 0/0/0",
			reqs, countDecisions(t, st, ""), events)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending after the refusal = %d, want 0", len(pending))
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, specs); err != nil {
		t.Fatalf("run after the refused unknown task: %v", err)
	}
}

// F8 — refused 重放/冲突: a refused request is a settled answer, not a
// vanishing id — the same choice replays the original receipt with zero
// new transitions or events, and a different choice is a conflict that
// leaves the stored answer standing.
func TestK4ReworkRefusedRequestReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("v1")}}
	st := mustCreate(t, dir, "k4-f8", specs)
	reqID := reworkAsk(t, st, "a", []byte("v2"))
	first, err := st.Answer(ctx, reqID, "refuse")
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter := eventKinds(t, st)
	second, err := st.Answer(ctx, reqID, "refuse")
	if err != nil {
		t.Fatalf("same-answer replay err = %v, want the original receipt", err)
	}
	if second != first {
		t.Fatalf("replay receipt = %+v, want the original %+v", second, first)
	}
	if kinds := eventKinds(t, st); len(kinds) != len(eventsAfter) {
		t.Fatalf("events changed on replay: %v, want %v", kinds, eventsAfter)
	}
	if _, err := st.Answer(ctx, reqID, "allow"); !errors.Is(err, ErrDecisionConflict) {
		t.Fatalf("different answer err = %v, want ErrDecisionConflict", err)
	}
	if n := countRequests(t, st, "a", reqRefused); n != 1 {
		t.Fatalf("refused rows after the conflict = %d, want still 1", n)
	}
	var answer string
	if err := st.db.QueryRow(
		"SELECT COALESCE(answer,'') FROM user_requests WHERE request_id=?", reqID).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	if answer != "refuse" {
		t.Fatalf("stored answer after the conflict = %q, want refuse (standing)", answer)
	}
}
