package runner

// Revision-allowance acceptance tests — 返修额度控制链 over the unified
// request model: Store.RequestRevision builds "allowance reached →
// durable wait → exact answer → only the granted revision executes" over
// ReviseTask, with every count derived from user_requests rows and the
// domain's ask rule injected as RepairPolicy.  Rounds are 0-BASED — the
// completed-rounds number the accepted graph.py repair() hands its
// repair-allowance — so initial=1 permits the first revision directly
// and asks on the second (the F6 rework of the previous first-round-ask
// expectation).  PendingDecisions lists current-generation questions
// plus valid-binding pending requests, so an invalidated old question is
// never presented as askable.  Everything here runs in-process over real
// durable state.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// repairLimitPolicy mirrors domain.repairAllowance's boundary (internal/
// harness/domain.go, read-only reference — runner must not import it): a
// round equal to the allowance already asks.
func repairLimitPolicy(round, allowance int) bool { return round >= allowance }

// countRequests counts the task's revision request rows in the given
// state ("" = all states).
func countRequests(t *testing.T, st *Store, taskID, state string) int {
	t.Helper()
	query := "SELECT COUNT(*) FROM user_requests WHERE run_id=? AND task_id=? AND kind=?"
	args := []any{st.runID, taskID, kindRevision}
	if state != "" {
		query += " AND state=?"
		args = append(args, state)
	}
	var n int
	if err := st.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The full allowance control chain on 0-based rounds: initial=1
// direct-approves round 0 and asks on round 1; the ask
// persists across Close/Open and is listed; a refusing answer refuses
// the request with the task untouched (generation, zero invalidation,
// original specs still run — and no ErrDecisionRefused: declining a
// revision is not the run refusing); the allowing answer grants, the
// completion call executes exactly the granted revision; round 2 asks
// again against allowance 2 (initial 1 + one grant).
func TestRequestRevisionAllowanceControlChain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	specs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "d", Name: "task-d", DependsOn: []string{"a"}, Input: []byte("input-d")},
	}
	if err := st.CreateRun(ctx, "revision-allowance", specs); err != nil {
		t.Fatal(err)
	}
	failA := true
	r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		if inv.TaskID == "a" && failA {
			return Result{}, errors.New("boom")
		}
		return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
	})
	if err := r.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failed run err = %v, want the executor failure", err)
	}

	exhaustion := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-2",
		Body:     []byte("allow one more repair round of task a?"),
	}
	reviseA := func(input string) TaskSpec {
		return TaskSpec{TaskID: "a", Name: "task-a", Input: []byte(input)}
	}

	// Round 0 against allowance 1: completed rounds 0 < 1 — the accepted
	// graph.py boundary — direct approval, no question.
	if err := st.RequestRevision(ctx, "a", reviseA("input-a2"), exhaustion, repairLimitPolicy, 1); err != nil {
		t.Fatalf("round-0 RequestRevision err = %v, want nil (direct approval)", err)
	}
	if n := countRequests(t, st, "a", reqExecuted); n != 1 {
		t.Fatalf("executed requests after round 0 = %d, want 1", n)
	}
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Fatalf("task a generation after round 0 = %d, want 2", gen)
	}
	if gen := taskGeneration(t, st, "d"); gen != 2 {
		t.Fatalf("dependent d generation after round 0 = %d, want 2 (scope invalidated)", gen)
	}
	failA = false
	if err := r.Run(ctx, []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "d", Name: "task-d", DependsOn: []string{"a"}, Input: []byte("input-d")},
	}); err != nil {
		t.Fatal(err)
	}

	// Round 1 against allowance 1: 1 >= 1 asks.
	if err := st.RequestRevision(ctx, "a", reviseA("input-a3"), exhaustion, repairLimitPolicy, 1); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("round-1 RequestRevision err = %v, want ErrWaitingDecision", err)
	}
	if n := countRequests(t, st, "a", reqPending); n != 1 {
		t.Fatalf("pending requests after the ask = %d, want 1", n)
	}
	// One open inquiry per task: a duplicate call is a named refusal.
	if err := st.RequestRevision(ctx, "a", reviseA("input-a3"), exhaustion, repairLimitPolicy, 1); !errors.Is(err, ErrRevisionPending) {
		t.Fatalf("duplicate RequestRevision err = %v, want ErrRevisionPending", err)
	}
	if n := countRequests(t, st, "a", ""); n != 2 {
		t.Fatalf("request rows after the duplicate = %d, want still 2", n)
	}
	// The task is untouched while the question waits.
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Fatalf("task a generation while the inquiry waits = %d, want 2", gen)
	}

	// The inquiry persists across Close/Open and stays listed.
	pendingBefore, err := st.PendingDecisions(ctx)
	if err != nil || len(pendingBefore) != 1 {
		t.Fatalf("pending decisions after the ask = %v (err %v), want the one inquiry", pendingBefore, err)
	}
	inquiry := pendingBefore[0]
	if inquiry.TaskID != "a" || inquiry.Revision != "rev-2" || inquiry.Body != "allow one more repair round of task a?" {
		t.Fatalf("inquiry = %+v, want the exhaustion question for task a", inquiry)
	}
	if !containsChoice(inquiry.Options, "allow") || !containsChoice(inquiry.Options, "refuse") {
		t.Fatalf("inquiry options = %v, want [allow refuse]", inquiry.Options)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pendingReopened, err := st.PendingDecisions(ctx)
	if err != nil || len(pendingReopened) != 1 || pendingReopened[0].DecisionID != inquiry.DecisionID {
		t.Fatalf("inquiry did not survive reopen: %v (err %v)", pendingReopened, err)
	}

	// Refuse: the request is refused and nothing else moves.
	if _, err := st.Answer(ctx, inquiry.DecisionID, "refuse"); err != nil {
		t.Fatal(err)
	}
	if n := countRequests(t, st, "a", reqRefused); n != 1 {
		t.Fatalf("refused requests after the decline = %d, want 1", n)
	}
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Fatalf("task a generation after the refusal = %d, want 2 (untouched)", gen)
	}
	if gen := taskGeneration(t, st, "d"); gen != 2 {
		t.Fatalf("dependent d generation after the refusal = %d, want 2 (zero invalidation)", gen)
	}
	// The current-generation specs still run to completion — and the
	// refused request must not turn the run into ErrDecisionRefused.
	gen2 := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "d", Name: "task-d", DependsOn: []string{"a"}, Input: []byte("input-d")},
	}
	if err := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		return Result{Payload: json.RawMessage(`"ok-` + inv.TaskID + `"`)}, nil
	}).Run(ctx, gen2); err != nil {
		t.Fatalf("run on current specs after the refusal: %v", err)
	}

	// Ask again (the refusal consumed no round: round 1 >= 1 once more),
	// then grant and complete.
	if err := st.RequestRevision(ctx, "a", reviseA("input-a3"), exhaustion, repairLimitPolicy, 1); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("second ask err = %v, want ErrWaitingDecision", err)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending decisions at the second ask = %v (err %v), want 1", pending, err)
	}
	if pending[0].DecisionID == inquiry.DecisionID {
		t.Fatal("the second ask reused the answered request row")
	}
	if _, err := st.Answer(ctx, pending[0].DecisionID, "allow"); err != nil {
		t.Fatal(err)
	}
	if n := countRequests(t, st, "a", reqGranted); n != 1 {
		t.Fatalf("granted requests after the allow = %d, want 1", n)
	}
	// The grant executes nothing by itself (input bytes stay external);
	// OpenRequests exposes the recoverable identity and the completion
	// call supplies the input and lands both in one transaction.
	open, err := st.OpenRequests(ctx)
	if err != nil || len(open) != 1 {
		t.Fatalf("open requests after the grant = %v (err %v), want the granted one", open, err)
	}
	if open[0].RequestID != pending[0].DecisionID || open[0].Kind != kindRevision ||
		open[0].TaskID != "a" || open[0].Generation != 2 || open[0].State != reqGranted {
		t.Fatalf("open request = %+v, want the granted revision of task a at generation 2", open[0])
	}
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Fatalf("task a generation after the grant = %d, want still 2 (not yet executed)", gen)
	}
	if err := st.RequestRevision(ctx, "a", reviseA("input-a3"), exhaustion, repairLimitPolicy, 1); err != nil {
		t.Fatalf("completion call err = %v, want nil", err)
	}
	if n := countRequests(t, st, "a", reqExecuted); n != 2 {
		t.Fatalf("executed requests after the completion = %d, want 2", n)
	}
	if gen := taskGeneration(t, st, "a"); gen != 3 {
		t.Fatalf("task a generation after the completion = %d, want 3", gen)
	}
	if gen := taskGeneration(t, st, "d"); gen != 3 {
		t.Fatalf("dependent d generation after the completion = %d, want 3 (scope invalidated)", gen)
	}

	// Round 2 against allowance 2 (initial 1 + one grant): asks again.
	if err := st.RequestRevision(ctx, "a", reviseA("input-a4"), exhaustion, repairLimitPolicy, 1); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("round-2 ask err = %v, want ErrWaitingDecision (round 2 >= allowance 2)", err)
	}
	if n := countRequests(t, st, "a", reqPending); n != 1 {
		t.Fatalf("pending requests at round 2 = %d, want 1", n)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 1 {
		t.Fatalf("pending decisions at round 2 = %d, want the new inquiry", len(pending))
	}
}

// containsChoice reports whether the option list offers the choice.
func containsChoice(options []string, choice string) bool {
	for _, o := range options {
		if o == choice {
			return true
		}
	}
	return false
}

// F1/F6, direct approval — a round below the allowance never asks: the
// request is born executed with no answer and no question, the revision
// lands bound to the creation-time generation, and a larger initial
// (增额) reaches the same path.
func TestRequestRevisionDirectApprovalBelowAllowance(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "revision-direct", specs)
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	exhaustion := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-2",
		Body:     []byte("allow one more repair round of task a?"),
	}
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		exhaustion, repairLimitPolicy, 2); err != nil {
		t.Fatalf("direct approval err = %v, want nil (round 0 < allowance 2)", err)
	}
	if n := countRequests(t, st, "a", reqExecuted); n != 1 {
		t.Fatalf("executed requests after direct approval = %d, want 1", n)
	}
	// The row was never a question: bound to the creation generation,
	// no answer, the initial allowance carried, no ask event.
	var answer, answeredAt string
	var initial, target int64
	if err := st.db.QueryRow(`
		SELECT COALESCE(answer,''), COALESCE(answered_at,''), initial, target_generation
		  FROM user_requests WHERE run_id=? AND task_id=? AND kind=?`,
		st.runID, "a", kindRevision).Scan(&answer, &answeredAt, &initial, &target); err != nil {
		t.Fatal(err)
	}
	if answer != "" || answeredAt != "" {
		t.Fatalf("direct approval row = (%q,%q), want no answer (no question was asked)", answer, answeredAt)
	}
	if initial != 2 || target != 1 {
		t.Fatalf("direct approval row initial/target = (%d,%d), want (2,1: bound allowance, creation generation)", initial, target)
	}
	if n := countEvents(t, st, eventKindRequestCreated); n != 0 {
		t.Fatalf("request_created events after direct approval = %d, want 0", n)
	}
	if gen := taskGeneration(t, st, "a"); gen != 2 {
		t.Fatalf("task a generation after direct approval = %d, want 2", gen)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending decisions after direct approval = %d, want 0", len(pending))
	}
	revised := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	if n := markerLines(t, dir, "a"); n != 2 {
		t.Errorf("task a executed %d times, want 2 (once per generation)", n)
	}
}

// F1/F6, CONTINUE/查询不计业务轮数 — executor-failure retries (the
// no-receipt path) and PendingDecisions queries create zero request
// rows, so the first business revision is still round 0 and
// direct-approves under initial=1; only the second round asks.  An ask
// is answerable even on an all-finished run: the open question is
// unfinished business, not a terminal run.
func TestFailureRetriesAndQueriesConsumeNoRepairRounds(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specs := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a")}}
	st := mustCreate(t, dir, "revision-noround", specs)
	fail := true
	calls := 0
	r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		calls++
		if fail {
			return Result{}, errors.New("boom")
		}
		return Result{Payload: json.RawMessage(`"ok"`)}, nil
	})
	// Two failed executions (two Runs, two no-receipt intents) ...
	for i := 0; i < 2; i++ {
		if err := r.Run(ctx, specs); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("failed run %d err = %v, want the executor failure", i, err)
		}
		if _, err := st.PendingDecisions(ctx); err != nil {
			t.Fatal(err)
		}
	}
	fail = false
	if err := r.Run(ctx, specs); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("executor ran %d times, want 3 (two failures plus the retry)", calls)
	}
	if n := countRequests(t, st, "a", ""); n != 0 {
		t.Fatalf("revision request rows after failures and queries = %d, want 0", n)
	}
	// Every run is finished now; the first business revision is still
	// round 0 — direct approval, proving the retries consumed no round.
	exhaustion := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-2",
		Body:     []byte("allow one more repair round of task a?"),
	}
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		exhaustion, repairLimitPolicy, 1); err != nil {
		t.Fatalf("first business revision err = %v, want nil (round 0, retries consumed nothing)", err)
	}
	gen2 := []TaskSpec{{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, gen2); err != nil {
		t.Fatal(err)
	}
	// The second round asks on the finished run.
	if err := st.RequestRevision(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a3")},
		exhaustion, repairLimitPolicy, 1); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("ask on finished run err = %v, want ErrWaitingDecision (round 1 >= 1)", err)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending decisions on finished run = %v (err %v), want the inquiry", pending, err)
	}
	// Answering it must not hit ErrRunFinished: the inquiry keeps the
	// run answerable.  Decline it; the untouched run settles to nil.
	if _, err := st.Answer(ctx, pending[0].DecisionID, "refuse"); err != nil {
		t.Fatalf("answering the inquiry on a finished run: %v", err)
	}
	if n := countRequests(t, st, "a", reqRefused); n != 1 {
		t.Fatalf("refused requests = %d, want 1", n)
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, gen2); err != nil {
		t.Fatalf("settled run after declining: %v", err)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending decisions after declining = %d, want 0", len(pending))
	}
}

// F3 regression — 修订后恰一个当前问题；当前任务完成后待答列表为空:
// an invalidated old-generation question is history an answer can no
// longer reach, so it must not be presented as askable.
func TestPendingDecisionsListOnlyCurrentGeneration(t *testing.T) {
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
	st := mustCreate(t, dir, "revision-generation-list", specs)
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("gated run err = %v, want ErrWaitingDecision", err)
	}
	old, err := st.PendingDecisions(ctx)
	if err != nil || len(old) != 1 {
		t.Fatalf("pending before revision = %v (err %v), want 1", old, err)
	}
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}); err != nil {
		t.Fatal(err)
	}
	// Exactly one current question; the generation-1 row is history.
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending after revision = %d, want exactly the current question", len(pending))
	}
	current := pending[0].DecisionID
	if current == old[0].DecisionID {
		t.Fatal("the listed question is the invalidated generation-1 row")
	}
	if _, err := st.Answer(ctx, old[0].DecisionID, "approve"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("answering the invalidated question err = %v, want ErrDecisionStale", err)
	}
	if _, err := st.Answer(ctx, current, "approve"); err != nil {
		t.Fatal(err)
	}
	revised := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, revised); err != nil {
		t.Fatal(err)
	}
	// The finished run presents no unanswerable leftover.
	after, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("pending after the run finished = %d, want 0 (old row must not resurface)", len(after))
	}
}

// F3×F1 coexistence — the current-generation rule and the revision
// request share one list: a carried generation-2 gate is listed while
// its invalidated generation-1 predecessor is not, a live revision
// request is listed alongside it, and a REFUSED request neither waits
// nor refuses the run.
func TestPendingDecisionsShowRevisionRequestAlongsideCurrentGate(t *testing.T) {
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
	st := mustCreate(t, dir, "revision-colist", specs)
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("gated run err = %v, want ErrWaitingDecision", err)
	}
	oldGate, err := st.PendingDecisions(ctx)
	if err != nil || len(oldGate) != 1 {
		t.Fatalf("pending gates before = %v (err %v), want 1", oldGate, err)
	}
	if err := st.ReviseTask(ctx, "a",
		TaskSpec{TaskID: "a", Name: "task-a", Input: []byte("input-a2")}); err != nil {
		t.Fatal(err)
	}
	exhaustion := &Confirmation{
		Options:  []string{"allow", "refuse"},
		Allow:    "allow",
		Revision: "rev-2",
		Body:     []byte("allow one more repair round of task b?"),
	}
	// Round 0 against a zero allowance asks immediately.
	if err := st.RequestRevision(ctx, "b",
		TaskSpec{TaskID: "b", Name: "task-b", Input: []byte("input-b2")},
		exhaustion, repairLimitPolicy, 0); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("request err = %v, want ErrWaitingDecision", err)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2 (carried gate plus live request)", len(pending))
	}
	seen := map[string]bool{}
	for _, d := range pending {
		seen[d.DecisionID] = true
	}
	if seen[oldGate[0].DecisionID] {
		t.Fatal("the invalidated generation-1 gate is still listed")
	}

	// Decline the request: the run keeps waiting on the gate, and a
	// refused request is neither a wait nor a run refusal.
	var requestID string
	for _, d := range pending {
		if d.TaskID == "b" {
			requestID = d.DecisionID
		}
	}
	if requestID == "" {
		t.Fatal("no request for task b in the pending list")
	}
	if _, err := st.Answer(ctx, requestID, "refuse"); err != nil {
		t.Fatal(err)
	}
	if n := countRequests(t, st, "b", reqRefused); n != 1 {
		t.Fatalf("refused requests for b = %d, want 1", n)
	}
	waiting, refused, err := st.decisionStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !waiting || refused {
		t.Fatalf("decisionStatus after declining = (waiting=%v, refused=%v), want the gate's wait only", waiting, refused)
	}
	// The declined revision never executed: task b's definition is still
	// the ORIGINAL input, and that is what runs while the gate waits.
	originalB := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a2")},
		{TaskID: "b", Name: "task-b", Input: []byte("input-b")},
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, originalB); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("run after declining err = %v, want ErrWaitingDecision on the carried gate", err)
	}
	// Answering the gate completes everything; the declined request is
	// gone from the list.
	gateID, _, _, _, _, _ := decisionRowAtGeneration(t, st, "a", 2)
	if _, err := st.Answer(ctx, gateID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, originalB); err != nil {
		t.Fatal(err)
	}
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 0 {
		t.Fatalf("pending after completion = %d, want 0", len(pending))
	}
}
