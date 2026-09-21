package runner

// K2 acceptance tests for the §13.2 decision sample: task a is gated
// behind a confirmation (approve/reject, approve allows) and task b
// depends on a.  The first three scenarios re-exec this binary through
// TestMain (RUNNER_K2_SCENARIO) and really exit — one before the
// decision is answered, one right after an allowing Answer committed but
// before any successor ran, one right after a refusing Answer — the
// parent then reopens and observes the run.  The remaining §6.1 refusal
// classes (old ids, replays, terminal answers) and the unit behaviours
// are covered by the in-process tests below.  The K1 crash machinery in
// crash_test.go keeps its env, injection points and exit codes
// untouched.

import (
	"context"
	"database/sql"
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
	envK2Scenario = "RUNNER_K2_SCENARIO"
	envK2Dir      = "RUNNER_K2_DIR"
	k2RunID       = "k2-sample"
)

// K2 child exit codes: 90, 91 and 93 are the three on-purpose exits
// (waiting point, answered point, refused point); 92+ mark wiring bugs
// in the child itself.
const (
	exitK2Waiting  = 90
	exitK2Answered = 91
	exitK2Wiring   = 92
	exitK2Refused  = 93
)

// gatedSpecs is the §13.2 K2 sample: a gated a (answering "approve"
// allows execution), and b behind it.
func gatedSpecs() []TaskSpec {
	return []TaskSpec{
		{
			TaskID: "a", Name: "task-a", Input: []byte("input-a"),
			Confirm: &Confirmation{
				Options:  []string{"approve", "reject"},
				Allow:    "approve",
				Revision: "rev-1",
				Body:     []byte("approve plan revision rev-1?"),
			},
		},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("input-b")},
	}
}

// k2Child drives the gated sample in RUNNER_K2_DIR.  All scenarios stop
// at the same durable point — Run returned ErrWaitingDecision, so one
// pending decision exists and nothing after a's gate has executed — and
// differ only in what the child's Answer did to it: nothing (waiting),
// consume it with the allowing choice (answered), or consume it with a
// refusing one (refused).
func k2Child(scenario string) {
	dir := os.Getenv(envK2Dir)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "child: missing", envK2Dir)
		os.Exit(exitK2Wiring)
	}
	ctx := context.Background()
	st, err := Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "child open:", err)
		os.Exit(exitK2Wiring)
	}
	specs := gatedSpecs()
	if err := st.CreateRun(ctx, k2RunID, specs); err != nil {
		fmt.Fprintln(os.Stderr, "child create:", err)
		os.Exit(exitK2Wiring)
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, specs); !errors.Is(err, ErrWaitingDecision) {
		fmt.Fprintln(os.Stderr, "child run:", err)
		os.Exit(exitK2Wiring)
	}
	switch scenario {
	case "waiting":
		// Durable state: one pending decision, zero executions, exit.
		os.Exit(exitK2Waiting)
	case "answered":
		// The user approves; the successor deliberately does not start
		// in this process — the parent must pick the work up from the
		// consumed gate alone.
		pending, err := st.PendingDecisions(ctx)
		if err != nil || len(pending) != 1 {
			fmt.Fprintln(os.Stderr, "child pending:", err, len(pending))
			os.Exit(exitK2Wiring)
		}
		if _, err := st.Answer(ctx, pending[0].DecisionID, "approve"); err != nil {
			fmt.Fprintln(os.Stderr, "child answer:", err)
			os.Exit(exitK2Wiring)
		}
		os.Exit(exitK2Answered)
	case "refused":
		// The user answers with a choice that does NOT allow execution;
		// the durable refusal is the point the parent must observe.
		pending, err := st.PendingDecisions(ctx)
		if err != nil || len(pending) != 1 {
			fmt.Fprintln(os.Stderr, "child pending:", err, len(pending))
			os.Exit(exitK2Wiring)
		}
		if _, err := st.Answer(ctx, pending[0].DecisionID, "reject"); err != nil {
			fmt.Fprintln(os.Stderr, "child answer:", err)
			os.Exit(exitK2Wiring)
		}
		os.Exit(exitK2Refused)
	default:
		fmt.Fprintln(os.Stderr, "child: unknown scenario:", scenario)
		os.Exit(exitK2Wiring)
	}
}

// runK2Child executes the re-exec'd child and asserts it exited with
// exactly wantExit.
func runK2Child(t *testing.T, dir, scenario string, wantExit int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), envK2Scenario+"="+scenario, envK2Dir+"="+dir)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != wantExit {
		t.Fatalf("child exit = %v (want code %d); output:\n%s", err, wantExit, out)
	}
}

// countDecisions counts the run's decision rows in the given state ("" =
// all states).
func countDecisions(t *testing.T, st *Store, state string) int {
	t.Helper()
	query := "SELECT COUNT(*) FROM decisions WHERE run_id=?"
	args := []any{st.runID}
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

// decisionAnswer reads one decision row's answer and answered_at ("" when
// still NULL).
func decisionAnswer(t *testing.T, st *Store, decisionID string) (answer, answeredAt string) {
	t.Helper()
	if err := st.db.QueryRow(
		"SELECT COALESCE(answer,''), COALESCE(answered_at,'') FROM decisions WHERE decision_id=?",
		decisionID).Scan(&answer, &answeredAt); err != nil {
		t.Fatal(err)
	}
	return answer, answeredAt
}

// eventKinds returns the run's event kinds in seq order.
func eventKinds(t *testing.T, st *Store) []string {
	t.Helper()
	var out []string
	for _, e := range loadEvents(t, st) {
		out = append(out, e.kind)
	}
	return out
}

// invocationTotal counts all invocation rows of the run.
func invocationTotal(t *testing.T, st *Store) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM invocations WHERE run_id=?", st.runID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// reachPendingGate runs once in-process and returns the single pending
// decision, asserting Run reported the wait.
func reachPendingGate(t *testing.T, st *Store) DecisionView {
	t.Helper()
	ctx := context.Background()
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, gatedSpecs()); !errors.Is(err, ErrWaitingDecision) {
		t.Fatalf("gated Run err = %v, want ErrWaitingDecision", err)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending decisions = %d, want 1", len(pending))
	}
	return pending[0]
}

// §13.2 scenario 1 — exit while the decision is pending: the reopened
// owner sees exactly the spec's question, nothing has executed, and the
// run only completes after a real answer.
func TestK2ExitBeforeDecisionWaitsThenCompletes(t *testing.T) {
	dir := t.TempDir()
	runK2Child(t, dir, "waiting", exitK2Waiting)
	ctx := context.Background()

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.runID != k2RunID {
		t.Fatalf("reopened run id = %q, want %q", st.runID, k2RunID)
	}
	// Exactly one pending decision, carrying the spec's question verbatim.
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending decisions = %d, want 1", len(pending))
	}
	got := pending[0]
	if got.TaskID != "a" || got.Revision != "rev-1" || got.Body != "approve plan revision rev-1?" {
		t.Fatalf("pending decision = %+v, want the spec's gate for task a", got)
	}
	if !slices.Equal(got.Options, []string{"approve", "reject"}) {
		t.Fatalf("options = %v, want [approve reject]", got.Options)
	}
	if len(got.DecisionID) != 32 {
		t.Fatalf("decision id %q is not a 128-bit hex id", got.DecisionID)
	}
	// No user decision yet: no execution, no invocation, no admission —
	// the successor did not run.
	if n := markerLines(t, dir, "a"); n != 0 {
		t.Errorf("task a executed %d times before the decision, want 0", n)
	}
	if n := markerLines(t, dir, "b"); n != 0 {
		t.Errorf("task b executed %d times before the decision, want 0", n)
	}
	if n := invocationTotal(t, st); n != 0 {
		t.Errorf("invocations = %d before the decision, want 0", n)
	}

	// The user approves; only now does the chain run, each task exactly
	// once, and the gate never re-asks.
	if _, err := st.Answer(ctx, got.DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, gatedSpecs()); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "b"} {
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
			t.Errorf("task %s state = (%q,%q), want finished with effective result", task, s, eff)
		}
	}
	// One decision row total, answered with the approved choice, and the
	// full event set: one creation, one answer, one admission per task.
	if n := countDecisions(t, st, ""); n != 1 {
		t.Errorf("decision rows = %d, want exactly 1", n)
	}
	if n := countDecisions(t, st, decisionAnswered); n != 1 {
		t.Errorf("answered decisions = %d, want exactly 1", n)
	}
	if answer, at := decisionAnswer(t, st, got.DecisionID); answer != "approve" || at == "" {
		t.Errorf("stored answer = (%q,%q), want (approve, timestamp)", answer, at)
	}
	wantKinds := []string{eventKindDecisionCreated, eventKindDecisionAnswered,
		eventKindAdmitted, eventKindAdmitted}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantKinds) {
		t.Errorf("event kinds = %v, want %v", kinds, wantKinds)
	}
}

// §13.2 scenario 2 — exit right after the answer committed: the reopened
// owner finds no open question, the gate is not asked again, and the
// successor runs on the consumed decision alone.
func TestK2ExitAfterDecisionRunsSuccessorWithoutReAsk(t *testing.T) {
	dir := t.TempDir()
	runK2Child(t, dir, "answered", exitK2Answered)
	ctx := context.Background()

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// The question is gone, and it is never re-created: still exactly one
	// answered row and the two events from the child.
	if pending, err := st.PendingDecisions(ctx); err != nil || len(pending) != 0 {
		t.Fatalf("pending decisions = %v (err %v), want none", pending, err)
	}
	if n := countDecisions(t, st, ""); n != 1 {
		t.Fatalf("decision rows = %d, want exactly 1", n)
	}
	if n := countDecisions(t, st, decisionAnswered); n != 1 {
		t.Fatalf("answered decisions = %d, want exactly 1", n)
	}
	wantBefore := []string{eventKindDecisionCreated, eventKindDecisionAnswered}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantBefore) {
		t.Fatalf("event kinds before resume = %v, want %v", kinds, wantBefore)
	}
	if n := invocationTotal(t, st); n != 0 {
		t.Fatalf("invocations = %d after answer, want 0 (successor not started)", n)
	}

	// Resume: a and b each run and are admitted exactly once, with no
	// second gate row or event.
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, gatedSpecs()); err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"a", "b"} {
		if n := markerLines(t, dir, task); n != 1 {
			t.Errorf("task %s executed %d times, want exactly 1", task, n)
		}
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
	}
	if n := countDecisions(t, st, ""); n != 1 {
		t.Errorf("decision rows after resume = %d, want still exactly 1", n)
	}
	wantAfter := append(append([]string{}, wantBefore...),
		eventKindAdmitted, eventKindAdmitted)
	if kinds := eventKinds(t, st); !slices.Equal(kinds, wantAfter) {
		t.Errorf("event kinds after resume = %v, want %v", kinds, wantAfter)
	}
}

// §13.2 refusal sample — the child reaches the wait, answers "reject"
// and really exits; the reopened parent's Run is refused from the stored
// answer alone: no re-ask, no execution, no invocation.
func TestK2ExitAfterRefusalRefusesOnReopen(t *testing.T) {
	dir := t.TempDir()
	runK2Child(t, dir, "refused", exitK2Refused)
	ctx := context.Background()

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := NewRunner(st, markerExecutor(dir)).Run(ctx, gatedSpecs()); !errors.Is(err, ErrDecisionRefused) {
		t.Fatalf("reopened run err = %v, want ErrDecisionRefused", err)
	}
	for _, task := range []string{"a", "b"} {
		if n := markerLines(t, dir, task); n != 0 {
			t.Errorf("task %s executed %d times after the refusal, want 0", task, n)
		}
	}
	if n := invocationTotal(t, st); n != 0 {
		t.Errorf("invocations after the refusal = %d, want 0", n)
	}
	// The question is not asked again either.
	if pending, err := st.PendingDecisions(ctx); err != nil || len(pending) != 0 {
		t.Errorf("pending decisions after refusal = %v (err %v), want none", pending, err)
	}
	if kinds := eventKinds(t, st); !slices.Equal(kinds,
		[]string{eventKindDecisionCreated, eventKindDecisionAnswered}) {
		t.Errorf("event kinds = %v, want [decision_created decision_answered]", kinds)
	}
}

// F1 — the confirmation requirement is persisted by CreateRun, so a
// reopened owner that omits the Confirm field from its recovery specs
// still meets the same gate: both when it reopens before any Run and
// when it reopens from the waiting state.  Answering through the store
// then completes the chain with the Confirm-free specs alone.
func TestK2OmittedConfirmFieldStillGatesAfterReopen(t *testing.T) {
	// Same task ids, dependencies and byte inputs as gatedSpecs, minus
	// the Confirm declaration entirely.
	plainSpecs := []TaskSpec{
		{TaskID: "a", Name: "task-a", Input: []byte("input-a")},
		{TaskID: "b", Name: "task-b", DependsOn: []string{"a"}, Input: []byte("input-b")},
	}
	for _, tc := range []struct {
		name     string
		runFirst bool
	}{
		{"reopen before the first Run", false},
		{"reopen from the waiting state", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			st, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.CreateRun(ctx, "r-f1", gatedSpecs()); err != nil {
				t.Fatal(err)
			}
			if tc.runFirst {
				if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, gatedSpecs()); !errors.Is(err, ErrWaitingDecision) {
					t.Fatalf("first Run err = %v, want ErrWaitingDecision", err)
				}
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st2, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer st2.Close()

			// Recovery without the Confirm field: the persisted gate
			// still stops the run, and nothing executes.
			var calls []string
			exec := recordingExecutor(&calls, nil)
			if err := NewRunner(st2, exec).Run(ctx, plainSpecs); !errors.Is(err, ErrWaitingDecision) {
				t.Fatalf("Confirm-free Run err = %v, want ErrWaitingDecision", err)
			}
			if len(calls) != 0 {
				t.Fatalf("Confirm-free run executed %v, want nothing", calls)
			}
			if n := invocationTotal(t, st2); n != 0 {
				t.Fatalf("Confirm-free run registered %d invocations, want 0", n)
			}

			// The user approves through the store; the very same
			// Confirm-free specs then drive the whole chain, each task
			// exactly once.
			pending, err := st2.PendingDecisions(ctx)
			if err != nil || len(pending) != 1 {
				t.Fatalf("pending decisions = %d (err %v), want 1", len(pending), err)
			}
			if _, err := st2.Answer(ctx, pending[0].DecisionID, "approve"); err != nil {
				t.Fatal(err)
			}
			if err := NewRunner(st2, exec).Run(ctx, plainSpecs); err != nil {
				t.Fatal(err)
			}
			for _, task := range []string{"a", "b"} {
				if n := countInvocations(t, st2, task, invAdmitted); n != 1 {
					t.Errorf("task %s admitted %d times, want exactly 1", task, n)
				}
			}
			if strings.Join(calls, ",") != "a,b" {
				t.Errorf("executions = %v, want [a b]", calls)
			}
		})
	}
}

// F2 — a refusing answer denies execution as durably as an allowing one
// grants it: across a close/reopen the same stored answer refuses again
// and again, the gated task and its successor never execute, and the row
// and events stay exactly as the refusal left them.  The approve path on
// an identical fresh run completes normally, proving the choice — not
// the mechanism — makes the difference.
func TestK2RefusedDecisionSurvivesReopenAndBlocksExecution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-refuse", gatedSpecs())
	gate := reachPendingGate(t, st)
	if _, err := st.Answer(ctx, gate.DecisionID, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()

	var calls []string
	r := NewRunner(st2, recordingExecutor(&calls, nil))
	for i := 0; i < 2; i++ {
		if err := r.Run(ctx, gatedSpecs()); !errors.Is(err, ErrDecisionRefused) {
			t.Fatalf("refused run %d err = %v, want ErrDecisionRefused", i, err)
		}
		if len(calls) != 0 {
			t.Fatalf("refused run %d executed %v, want nothing", i, calls)
		}
	}
	if n := invocationTotal(t, st2); n != 0 {
		t.Fatalf("refused runs registered %d invocations, want 0", n)
	}
	// The row carries the refusal verbatim; the events are exactly the
	// creation and the answer — no admission ever happened.
	if answer, at := decisionAnswer(t, st2, gate.DecisionID); answer != "reject" || at == "" {
		t.Fatalf("stored decision = (%q,%q), want (reject, timestamp)", answer, at)
	}
	if n := countDecisions(t, st2, decisionAnswered); n != 1 {
		t.Fatalf("answered rows = %d, want exactly 1", n)
	}
	if kinds := eventKinds(t, st2); !slices.Equal(kinds,
		[]string{eventKindDecisionCreated, eventKindDecisionAnswered}) {
		t.Fatalf("event kinds = %v, want [decision_created decision_answered]", kinds)
	}

	// Contrast: the allowing choice on an identical fresh run completes
	// the chain normally.
	dirOK := t.TempDir()
	stOK := mustCreate(t, dirOK, "r-allow", gatedSpecs())
	gateOK := reachPendingGate(t, stOK)
	if _, err := stOK.Answer(ctx, gateOK.DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(stOK, recordingExecutor(nil, nil)).Run(ctx, gatedSpecs()); err != nil {
		t.Fatalf("allowing run err = %v, want nil", err)
	}
	for _, task := range []string{"a", "b"} {
		if n := countInvocations(t, stOK, task, invAdmitted); n != 1 {
			t.Errorf("contrast: task %s admitted %d times, want exactly 1", task, n)
		}
	}
}

// F3 — the three malformed gate declarations are refused before anything
// is written: no decisions row, no run row (a reopen finds no run), and
// the corrected declaration then creates normally in the same directory.
func TestK2MalformedConfirmationsRefusedBeforeAnyWrite(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*Confirmation)
		want   string
	}{
		{"no options", func(c *Confirmation) { c.Options = nil }, "without options"},
		{"no allowing choice", func(c *Confirmation) { c.Allow = "" }, "without an allowing choice"},
		{"allowing choice not offered", func(c *Confirmation) { c.Allow = "maybe" }, "outside its confirmation options"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			specs := gatedSpecs()
			tc.mutate(specs[0].Confirm)
			err = st.CreateRun(ctx, "r-f3", specs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("malformed CreateRun err = %v, want refusal containing %q", err, tc.want)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}

			// Zero residue: reopening finds no run at all.
			st2, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer st2.Close()
			if st2.runID != "" {
				t.Fatalf("refused creation left run %q behind, want none", st2.runID)
			}
			var nRuns, nDecisions int
			if err := st2.db.QueryRow("SELECT COUNT(*) FROM runs").Scan(&nRuns); err != nil {
				t.Fatal(err)
			}
			if err := st2.db.QueryRow("SELECT COUNT(*) FROM decisions").Scan(&nDecisions); err != nil {
				t.Fatal(err)
			}
			if nRuns != 0 || nDecisions != 0 {
				t.Fatalf("residue after refusal: runs=%d decisions=%d, want 0/0", nRuns, nDecisions)
			}
			// The corrected declaration creates normally in the same
			// directory.
			if err := st2.CreateRun(ctx, "r-f3", gatedSpecs()); err != nil {
				t.Fatalf("corrected CreateRun after refusal: %v", err)
			}
		})
	}
}

// §13.2 scenario 3 — old ids: a request that never existed, and a valid
// id belonging to another run, are both stale; neither touches any row.
func TestK2StaleDecisionIDsAreRefused(t *testing.T) {
	ctx := context.Background()
	dirOne := t.TempDir()
	stOne := mustCreate(t, dirOne, "r-one", gatedSpecs())
	gateOne := reachPendingGate(t, stOne)

	// An id no run ever created.
	if _, err := stOne.Answer(ctx, strings.Repeat("0", 32), "approve"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("nonexistent id err = %v, want ErrDecisionStale", err)
	}
	// A real, currently-pending decision — of a different run.  Two
	// parallel confirmations never substitute for each other (§6.1).
	dirTwo := t.TempDir()
	stTwo := mustCreate(t, dirTwo, "r-two", gatedSpecs())
	gateTwo := reachPendingGate(t, stTwo)
	if _, err := stOne.Answer(ctx, gateTwo.DecisionID, "approve"); !errors.Is(err, ErrDecisionStale) {
		t.Fatalf("other run's id err = %v, want ErrDecisionStale", err)
	}
	// Both refusals left both gates exactly as they were.
	for _, st := range []*Store{stOne, stTwo} {
		if n := countDecisions(t, st, decisionPending); n != 1 {
			t.Errorf("pending decisions after stale refusals = %d, want 1", n)
		}
		if n := countDecisions(t, st, decisionAnswered); n != 0 {
			t.Errorf("answered decisions after stale refusals = %d, want 0", n)
		}
	}
	if gateOne.DecisionID == gateTwo.DecisionID {
		t.Fatal("two gates minted the same decision id")
	}
}

// §13.2 scenario 4 — replays: the same answer returns the original
// receipt with zero side effects; a different answer on the same id is a
// conflict and the stored answer stands.
func TestK2AnswerReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-replay", gatedSpecs())
	gate := reachPendingGate(t, st)

	first, err := st.Answer(ctx, gate.DecisionID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	storedAnswer, storedAt := decisionAnswer(t, st, gate.DecisionID)
	eventsAfterAnswer := eventKinds(t, st)

	// Same id, same choice: the original receipt, byte for byte, and no
	// new transition or event.
	replay, err := st.Answer(ctx, gate.DecisionID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	if replay.DecisionID != first.DecisionID || replay.Choice != first.Choice {
		t.Fatalf("replay receipt = %+v, want %+v", replay, first)
	}
	if !replay.AnsweredAt.Equal(first.AnsweredAt) {
		t.Fatalf("replay answered_at = %v, want the original %v", replay.AnsweredAt, first.AnsweredAt)
	}
	if answer, at := decisionAnswer(t, st, gate.DecisionID); answer != storedAnswer || at != storedAt {
		t.Fatalf("row changed on replay: (%q,%q), want (%q,%q)", answer, at, storedAnswer, storedAt)
	}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, eventsAfterAnswer) {
		t.Fatalf("events changed on replay: %v, want %v", kinds, eventsAfterAnswer)
	}

	// Same id, different choice: refused as a conflict, original answer
	// untouched.
	if _, err := st.Answer(ctx, gate.DecisionID, "reject"); !errors.Is(err, ErrDecisionConflict) {
		t.Fatalf("conflicting answer err = %v, want ErrDecisionConflict", err)
	}
	if answer, at := decisionAnswer(t, st, gate.DecisionID); answer != "approve" || at != storedAt {
		t.Fatalf("row after conflict = (%q,%q), want (approve, original timestamp)", answer, at)
	}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, eventsAfterAnswer) {
		t.Fatalf("events changed on conflict: %v, want %v", kinds, eventsAfterAnswer)
	}
}

// §13.2 scenario 5 — terminal answers: once every task is finished, even
// the idempotent replay of the accepted answer is refused.
func TestK2TerminalRunRefusesAnyAnswer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-terminal", gatedSpecs())
	gate := reachPendingGate(t, st)
	if _, err := st.Answer(ctx, gate.DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(ctx, gatedSpecs()); err != nil {
		t.Fatal(err)
	}
	eventsAtTerminal := eventKinds(t, st)
	storedAnswer, storedAt := decisionAnswer(t, st, gate.DecisionID)

	// The run is finished: re-sending the very answer that was accepted
	// is a terminal refusal, not a replay receipt.
	if _, err := st.Answer(ctx, gate.DecisionID, "approve"); !errors.Is(err, ErrRunFinished) {
		t.Fatalf("terminal same-choice err = %v, want ErrRunFinished", err)
	}
	// Nothing changed by the refusal.
	if answer, at := decisionAnswer(t, st, gate.DecisionID); answer != storedAnswer || at != storedAt {
		t.Fatalf("row after terminal refusal = (%q,%q), want unchanged (%q,%q)", answer, at, storedAnswer, storedAt)
	}
	if kinds := eventKinds(t, st); !slices.Equal(kinds, eventsAtTerminal) {
		t.Fatalf("events after terminal refusal = %v, want %v", kinds, eventsAtTerminal)
	}
}

// Waiting is reported as waiting, never as a dependency cycle, and one
// gate stays one row with one creation event however often Run reaches
// it.
func TestK2RunReportsWaitingNotCycleError(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-waiting", gatedSpecs())
	r := NewRunner(st, recordingExecutor(nil, nil))
	for i := 0; i < 2; i++ {
		err := r.Run(ctx, gatedSpecs())
		if !errors.Is(err, ErrWaitingDecision) {
			t.Fatalf("run %d err = %v, want ErrWaitingDecision", i, err)
		}
		if strings.Contains(err.Error(), "cycle") {
			t.Fatalf("run %d misreported the wait as a cycle: %v", i, err)
		}
	}
	if n := countDecisions(t, st, ""); n != 1 {
		t.Errorf("decision rows after %d runs = %d, want exactly 1", 2, n)
	}
	if n := countEvents(t, st, eventKindDecisionCreated); n != 1 {
		t.Errorf("decision_created events = %d, want exactly 1", n)
	}
	if n := invocationTotal(t, st); n != 0 {
		t.Errorf("invocations while waiting = %d, want 0", n)
	}
}

// A choice outside the offered options is refused descriptively and the
// question stays pending.
func TestK2ChoiceOutsideOptionsIsRefused(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-choice", gatedSpecs())
	gate := reachPendingGate(t, st)

	_, err := st.Answer(ctx, gate.DecisionID, "maybe")
	if err == nil || errors.Is(err, ErrDecisionStale) ||
		errors.Is(err, ErrDecisionConflict) || errors.Is(err, ErrRunFinished) {
		t.Fatalf("out-of-option choice err = %v, want a descriptive refusal", err)
	}
	if !strings.Contains(err.Error(), "maybe") || !strings.Contains(err.Error(), gate.DecisionID) {
		t.Fatalf("refusal %v does not name the choice and the decision", err)
	}
	if n := countDecisions(t, st, decisionPending); n != 1 {
		t.Fatalf("pending rows after refused choice = %d, want 1", n)
	}
	if n := countEvents(t, st, eventKindDecisionAnswered); n != 0 {
		t.Fatalf("decision_answered events = %d after refused choice, want 0", n)
	}
	// The gate still accepts the real options afterwards.
	if _, err := st.Answer(ctx, gate.DecisionID, "reject"); err != nil {
		t.Fatalf("valid choice after refusal: %v", err)
	}
}

// An executor failure after the gate was answered costs one retry, never a
// second question: the answer stands and the re-run goes straight
// through.
func TestK2FailedExecutionDoesNotReAskDecision(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-retry", gatedSpecs())
	gate := reachPendingGate(t, st)
	if _, err := st.Answer(ctx, gate.DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	fail := true
	r := NewRunner(st, func(_ context.Context, inv Invocation) (Result, error) {
		calls[inv.TaskID]++
		if fail && inv.TaskID == "a" {
			return Result{}, errors.New("boom")
		}
		return Result{Payload: []byte(`"ok"`)}, nil
	})
	if err := r.Run(ctx, gatedSpecs()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failed run err = %v, want executor failure", err)
	}
	// The failure asked nothing: one row, still answered, no new events.
	if n := countDecisions(t, st, ""); n != 1 {
		t.Fatalf("decision rows after failed execution = %d, want 1", n)
	}
	if n := countDecisions(t, st, decisionAnswered); n != 1 {
		t.Fatalf("answered rows after failed execution = %d, want 1", n)
	}
	if n := countEvents(t, st, eventKindDecisionCreated); n != 1 {
		t.Fatalf("decision_created events after failed execution = %d, want 1", n)
	}
	// The retry executes the settled work and finishes the run.
	fail = false
	if err := r.Run(ctx, gatedSpecs()); err != nil {
		t.Fatal(err)
	}
	if calls["a"] != 2 {
		t.Fatalf("task a ran %d times, want 2 (one failed attempt, one retry)", calls["a"])
	}
	if calls["b"] != 1 {
		t.Fatalf("task b ran %d times, want 1", calls["b"])
	}
	for _, task := range []string{"a", "b"} {
		if n := countInvocations(t, st, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted %d times, want exactly 1", task, n)
		}
	}
	if n := countDecisions(t, st, ""); n != 1 {
		t.Errorf("decision rows after retry = %d, want still 1", n)
	}
	if n := countEvents(t, st, eventKindDecisionCreated); n != 1 {
		t.Errorf("decision_created events after retry = %d, want still 1", n)
	}
}

// Answered decisions are history and never surface as pending.
func TestK2PendingDecisionsExcludeAnswered(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := mustCreate(t, dir, "r-list", gatedSpecs())
	gate := reachPendingGate(t, st)
	if pending, _ := st.PendingDecisions(ctx); len(pending) != 1 {
		t.Fatalf("pending before answer = %d, want 1", len(pending))
	}
	if _, err := st.Answer(ctx, gate.DecisionID, "approve"); err != nil {
		t.Fatal(err)
	}
	pending, err := st.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after answer = %v, want none", pending)
	}
}

// A v2 file (the accepted K1 baseline) is migrated forward, not refused:
// the decisions table is rebuilt, the version stamped 3, and every K1
// fact survives untouched.
func TestK2MigratesV2ToV3(t *testing.T) {
	dir := t.TempDir()
	specs := []TaskSpec{
		{TaskID: "a", Name: "A", Input: []byte("in-a")},
		{TaskID: "b", Name: "B", DependsOn: []string{"a"}, Input: []byte("in-b")},
	}
	st := mustCreate(t, dir, "r-mig", specs)
	if err := NewRunner(st, recordingExecutor(nil, nil)).Run(context.Background(), specs); err != nil {
		t.Fatal(err)
	}
	effA := func() string { _, eff := taskState(t, st, "a"); return eff }()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// Rewind the file to the v2 shape: drop the v3 delta and stamp the
	// old version — exactly what a K1-accepted baseline looks like.
	db, err := sql.Open("sqlite", filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE decisions"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: the migration runs, and the K1 facts are all still there.
	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after v2 rewind: %v", err)
	}
	defer st2.Close()
	var version int
	if err := st2.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("user_version = %d after migration, want 3", version)
	}
	if n := countDecisions(t, st2, ""); n != 0 {
		t.Fatalf("decisions rows after migration = %d, want 0 (empty table)", n)
	}
	if st2.runID != "r-mig" {
		t.Fatalf("run id after migration = %q, want r-mig", st2.runID)
	}
	for _, task := range []string{"a", "b"} {
		if s, eff := taskState(t, st2, task); s != taskFinished || eff == "" {
			t.Errorf("task %s after migration = (%q,%q), want finished with effective result", task, s, eff)
		}
		if n := countInvocations(t, st2, task, invAdmitted); n != 1 {
			t.Errorf("task %s admitted rows after migration = %d, want 1", task, n)
		}
	}
	if _, eff := taskState(t, st2, "a"); eff != effA {
		t.Errorf("task a effective result changed across migration: %q, want %q", eff, effA)
	}
	if n := len(loadEvents(t, st2)); n != 2 {
		t.Errorf("events after migration = %d, want 2", n)
	}
	// The migrated run is usable: a no-op advance reports success.
	if err := NewRunner(st2, recordingExecutor(nil, nil)).Run(context.Background(), specs); err != nil {
		t.Fatalf("run on migrated database: %v", err)
	}
}

// countEvents counts the run's events of one kind.
func countEvents(t *testing.T, st *Store, kind string) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM events WHERE run_id=? AND kind=?", st.runID, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
