//go:build !windows

package harness

// Run-lifecycle behaviors of the composition: an upstream FAIL holds the
// chain while its mandatory findings stand, the user's abort survives
// Close/Open, each repair round carries its own failure materials, a
// completed run archives to a real verified artifact, a reopen that
// changes the run is refused by name, a recovered owner's Cancel settles
// the predecessor's live call, a recovered held interaction surfaces for
// the user, gate findings enter the executable repair loop, and before
// anything exists only the requirement gate is answerable.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/runner"
)

// openWorkflowForReopen opens a workflow like openWorkflow but also returns the config a
// reopen needs.
func openWorkflowForReopen(t *testing.T, runID string, scripts composeScripts, repairLimit int) (*Compose, ComposeConfig) {
	t.Helper()
	c := openWorkflow(t, runID, scripts, repairLimit)
	return c, c.cfg
}

// TestUpstreamFAILHoldsTheChain: a blocking FAIL at the product review, the
// technical review or the QA design must hold the chain — no route to
// HARNESS_COMPLETE exists while mandatory findings stand.
func TestUpstreamFAILHoldsTheChain(t *testing.T) {
	for _, stage := range []string{taskProduct, taskTechnical, taskQADesign} {
		t.Run(stage, func(t *testing.T) {
			c, _ := openWorkflowForReopen(t, "review-fail-"+pathSafe(stage), composeScripts{
				stage: {Status: "FAIL", Summary: "blocking issue", Findings: []Finding{
					{ID: "blocker", Severity: "P1", Message: "requirement or plan invalid", Paths: []string{"main.go"}},
				}},
			}, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			// The review's ask is answered with its non-allowing choice: the
			// user's own abort of the mandatory-findings question.
			got := answerAllowing(t, c, ctx, map[string]string{askTask(stage): "abort"})
			t.Logf("status=%s terminal=%s err=%v", got.Status, got.Terminal, got.Err)
			if got.Status == StatusComplete {
				t.Errorf("blocking %s FAIL reached HARNESS_COMPLETE", stage)
			}
		})
	}
}

// TestAbortSurvivesReopen: the user's abort survives Close/Open — the reopened
// owner still classifies the run ABORTED and asks nothing.
func TestAbortSurvivesReopen(t *testing.T) {
	c, cfg := openWorkflowForReopen(t, "review-abort-reopen", composeScripts{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := c.Drive(ctx)
	if len(state.Pending) == 0 {
		t.Fatalf("no gate: %+v", state)
	}
	if _, err := c.Answer(ctx, state.Pending[0].DecisionID, "abort"); err != nil {
		t.Fatal(err)
	}
	if state = c.Drive(ctx); state.Status != StatusAborted {
		t.Fatalf("before reopen: %+v", state)
	}
	c.Close()
	reopened, err := ComposeHarnessV1(ComposeConfig{
		Dir: c.dir, RunID: cfg.RunID, Spec: cfg.Spec, Binary: cfg.Binary, Binding: cfg.Binding, Parallelism: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got := reopened.Drive(ctx)
	t.Logf("after reopen: %+v", got)
	if got.Status != StatusAborted {
		t.Errorf("user abort lost after reopening")
	}
}

// TestRepairBriefCarriesItsRoundsFindings: each approved repair round must carry THAT round's
// failure materials — round 1 works from the first gate defect, round 2 from
// the second, and the final repair brief references only the newest bundle.
// A development FAIL raises the development-findings ask inside the worker
// loop, so the allowance-bounded rounds here are driven by gate findings —
// the repair loop's own material.
func TestRepairBriefCarriesItsRoundsFindings(t *testing.T) {
	c, _ := openWorkflowForReopen(t, "review-repair-brief", composeScripts{
		// Development lands nothing on its first two calls: the candidate
		// stays broken and the verification route keeps repairing.
		"development#1": {Status: "PASS", Summary: "green", NoWrite: true},
		"development#2": {Status: "PASS", Summary: "green", NoWrite: true},
		"gate:complexity-gate#1": {Status: "FAIL", Summary: "first defect", Findings: []Finding{
			{ID: "first-defect", Severity: "P1", Message: "first distinct defect A", Paths: []string{"main.go"}},
		}},
		"gate:complexity-gate#2": {Status: "FAIL", Summary: "second defect", Findings: []Finding{
			{ID: "second-defect", Severity: "P1", Message: "second distinct defect B", Paths: []string{"second.go"}},
		}},
	}, 1) // spec floor: allowance 1 — round 1 is direct, round 2 asks
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	got := answerAllowing(t, c, ctx, nil) // "continue" is the allowing choice
	t.Logf("final: %+v", got)
	if got.Status != StatusComplete {
		t.Fatalf("repair chain did not seal: %+v", got)
	}
	// The evaluate outcomes' feedback bundles: round 1 carries defect A,
	// round 2 defect B, and each repair revision's brief references the
	// bundle of ITS round — the newest, never a stale one.
	refs := map[string]string{}
	for _, gen := range c.outcomeGenerations(taskEvaluate) {
		outcome := c.readOutcome(taskEvaluate, gen)
		if outcome != nil && outcome.Feedback != "" {
			body, err := os.ReadFile(filepath.Join(c.dir, "artifacts", outcome.Feedback+".json"))
			if err != nil {
				t.Fatal(err)
			}
			refs[string(body)] = outcome.Feedback
		}
	}
	var refA, refB string
	for body, ref := range refs {
		if strings.Contains(body, "first distinct defect A") {
			refA = ref
		}
		if strings.Contains(body, "second distinct defect B") {
			refB = ref
		}
	}
	if refA == "" || refB == "" || refA == refB {
		t.Fatalf("feedback bundles not distinguished: A=%q B=%q", refA, refB)
	}
	var repairBriefs []string
	// The repair rounds' briefs, kernel-sourced: revision inputs of the
	// development task carrying the repair role — the shape a repair
	// round's brief always has.
	for _, row := range c.kernelRevisionInputs() {
		if row.TaskID == taskDevelopment && strings.Contains(string(row.Input), `"role":"repair"`) {
			repairBriefs = append(repairBriefs, string(row.Input))
		}
	}
	if len(repairBriefs) < 2 {
		t.Fatalf("repair rounds = %d, want at least 2", len(repairBriefs))
	}
	last := repairBriefs[len(repairBriefs)-1]
	if !strings.Contains(last, refB) {
		t.Errorf("the approved final repair brief does not reference the newest findings bundle %q", refB)
	}
	if strings.Contains(last, refA) {
		t.Errorf("the approved final repair brief still references the stale round-1 bundle %q", refA)
	}
}

// TestArchiveCreatesVerifiedArtifact: a completed run archives to a real,
// verified artifact and has published its result record.
func TestArchiveCreatesVerifiedArtifact(t *testing.T) {
	c, _ := openWorkflowForReopen(t, "review-archive", composeScripts{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got := answerAllowing(t, c, ctx, nil)
	if got.Status != StatusComplete {
		t.Fatalf("not complete: %+v", got)
	}
	dest := filepath.Join(t.TempDir(), "run.zip")
	report, err := c.Archive(dest)
	t.Logf("archive: status=%s files=%d error=%v", report.Status, report.FileCount, err)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dest); err != nil {
		t.Errorf("successful Archive created no artifact: %v", err)
	}
	// The archive must admit the run's real inventory, never degrade to
	// the database backup alone.
	if report.FileCount <= 1 {
		t.Errorf("archive admitted %d members, want the run's real inventory", report.FileCount)
	}
	if _, err = os.Stat(filepath.Join(c.dir, "harness-result.json")); err != nil {
		t.Errorf("HARNESS_COMPLETE produced no harness-result.json: %v", err)
	}
}

// TestReopenWithChangedBindingIsRefused: a reopen that changes the run id, the
// requirement or the project is refused by name, never silently adopted.
func TestReopenWithChangedBindingIsRefused(t *testing.T) {
	c, cfg := openWorkflowForReopen(t, "review-binding-original", composeScripts{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c.Drive(ctx)
	c.Close()
	cfg.RunID = "different-run"
	cfg.Spec.Requirement = "different requirement"
	other, _ := sampleProject(t)
	cfg.Spec.Project = other
	reopened, err := ComposeHarnessV1(cfg)
	if err != nil {
		t.Logf("correctly refused: %v", err)
		return
	}
	defer reopened.Close()
	t.Errorf("reopen silently accepted changed RunID, requirement and project")
}

// TestCancelSettlesRecoveredInFlightCall: a recovered owner's Cancel settles the
// predecessor's still-live supervisor — success is never reported while the
// recorded process is alive without a receipt.
func TestCancelSettlesRecoveredInFlightCall(t *testing.T) {
	c := openWorkflow(t, "review-recovered-cancel", composeScripts{}, 1)
	job := fabricatePredecessor(t, c, taskDevelopment, "inv-recovered", 10)
	defer signalTurn(t, job)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The consultation classifies the predecessor's call as observed.
	outcome, err := c.consult(ctx, fabricatedInvocation(taskDevelopment, "inv-recovered"))
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Observe {
		t.Fatalf("consult = %+v, want observed", outcome)
	}
	_, err = c.Cancel(ctx)
	// The settlement evidence is the receipt: the supervisor's own record
	// that the call was signalled and reaped. (The fabricated supervisor is
	// Process.Release'd, so after exiting it lingers as an unreaped zombie
	// whose kill(pid,0) still answers — a process-state artifact of the
	// fixture, not a running call.)
	var receipt map[string]any
	if value, rerr := readJSONFile(filepath.Join(job, "exit.json")); rerr == nil {
		receipt, _ = value.(map[string]any)
	}
	t.Logf("Cancel err=%v exitExists=%v exitCode=%v", err, receipt != nil, intOf(getOf(receipt, "exit_code")))
	if err != nil && receipt == nil {
		t.Fatalf("Cancel failed and settled nothing: %v", err)
	}
	if err == nil {
		if receipt == nil {
			t.Errorf("Cancel reported success with no settled receipt")
		}
		if code := intOf(getOf(receipt, "exit_code")); code != 130 {
			t.Errorf("settled receipt exit = %d, want the supervisor's cancellation 130", code)
		}
	}
}

// TestRecoveredHeldInteractionIsSurfaced: a predecessor's observed call that holds a
// pending native request is in the interaction watcher's scan set, and the
// watcher surfaces it as a user-answerable interaction.
// The assertion runs over a real live supervisor; the consult-level
// observation and the cancel-level settlement are covered by the workflow
// crash tests.
func TestRecoveredHeldInteractionIsSurfaced(t *testing.T) {
	c := openWorkflow(t, "review-recovered-held", composeScripts{}, 1)
	job := fabricatePredecessor(t, c, taskDevelopment, "inv-held", 10)
	defer signalTurn(t, job)
	pending := []any{map[string]any{
		"status":     "pending",
		"request_id": "41",
		"method":     "interaction/requestPermission",
		"summary":    "allow file write?",
		"options":    []any{map[string]any{"optionId": "allow_once"}},
	}}
	if err := writeAtomicJSON(filepath.Join(job, "interaction-pending.json"), pending); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if outcome, err := c.consult(ctx, fabricatedInvocation(taskDevelopment, "inv-held")); err != nil || !outcome.Observe {
		t.Fatalf("consult = %+v (%v), want observed", outcome, err)
	}
	// The scan set must contain the observed call's directory, and the
	// watcher must surface its pending request as a held interaction.
	dirs := c.watchedDirs()
	if dirs[job] != taskDevelopment {
		t.Fatalf("watched dirs = %v, want the observed %s", dirs, job)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case notice := <-c.heldCh:
			held, ok := c.heldInteraction(notice)
			if ok && held.RequestID == "41" && held.Method == "interaction/requestPermission" {
				t.Logf("surfaced: %+v", held)
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Errorf("recovered held interaction not surfaced")
}

// TestGateFindingsEnterTheRepairLoop: a gate review FAIL enters the executable repair
// loop through the composition's own surfaces — a repair question the user
// answers, a new repair round, and a seal once the gate passes.
// Allowance 1 grants round 1 directly — the spec floor — so the ask is
// round 2's.
func TestGateFindingsEnterTheRepairLoop(t *testing.T) {
	c, _ := openWorkflowForReopen(t, "review-gate-repair", composeScripts{
		"gate:complexity-gate#1": {Status: "FAIL", Summary: "gate defect", Findings: []Finding{
			{ID: "gate-defect", Severity: "P1", Message: "needs development repair", Paths: []string{"main.go"}},
		}},
		"gate:complexity-gate#2": {Status: "FAIL", Summary: "gate defect again", Findings: []Finding{
			{ID: "gate-defect-2", Severity: "P1", Message: "still needs repair", Paths: []string{"main.go"}},
		}},
	}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Round 1 is granted directly (within the allowance); the round-2 ask is
	// the loop's user-facing entry — stop at it.
	state := answerAllowing(t, c, ctx, nil, taskDevelopment)
	found := false
	for _, question := range state.Pending {
		if question.TaskID == taskDevelopment {
			found = true
		}
	}
	t.Logf("stop at the repair ask: status=%s found=%v pending=%d", state.Status, found, len(state.Pending))
	if !found {
		t.Fatalf("gate FAIL did not enter the available repair loop: %+v", state)
	}
	got := answerAllowing(t, c, ctx, nil)
	if got.Status != StatusComplete {
		t.Errorf("repair loop did not complete after the allowance: %+v", got)
	}
}

// TestOnlyTheRequirementGateIsAskableFirst: before any review, development or candidate
// exists, the only answerable question is the requirement confirmation — no
// approval of not-yet-existing materials can be given through the API.
// The plan question opens only when its materials stand; this asserts the
// early question surface directly.
func TestOnlyTheRequirementGateIsAskableFirst(t *testing.T) {
	c, _ := openWorkflowForReopen(t, "review-early-approve", composeScripts{}, 1)
	ctx := context.Background()
	c.Drive(ctx)
	pending, err := c.PendingDecisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ask := range pending {
		t.Logf("early question: task=%s options=%v", ask.TaskID, ask.Options)
		if ask.TaskID != taskRequirement {
			t.Errorf("question on %s is answerable before the requirement is confirmed", ask.TaskID)
		}
	}
	if len(pending) != 1 {
		t.Errorf("early question surface = %d, want exactly the requirement gate", len(pending))
	}
}

// TestUnreadableParkRowFailsItsNecessaryReaders: the necessary-control
// park-row reads surface a failed store read as their own layer's failure
// stop, never as a flattened "no row" — an unreadable row read as absent
// could revise past a standing park (routeRepair's hold), re-ask a parked
// stage, or move a check brief whose stage is parked. latestOutcome's
// degraded read order is NOT under test here: it keeps its established
// design (see its doc comment). The row-loop read inside unplacedDisposition
// sits behind the Dispositions listing and is not reachable by a whole-store
// failure alone; its error propagation is carried by the signature the
// classify callers map to their failure stops.
func TestUnreadableParkRowFailsItsNecessaryReaders(t *testing.T) {
	c := openWorkflow(t, "review-unreadable-park", composeScripts{}, 1)
	ctx := context.Background()
	// A real standing row first — the fact the flattened reads would have
	// decided around.
	if err := c.store.SetDisposition(ctx, runner.Disposition{
		TaskID: taskDevelopment, Kind: "runtime", Generation: 1, Reason: "fixture park",
	}); err != nil {
		t.Fatal(err)
	}
	// The read failure itself: a closed store fails every kernel read.
	if err := c.store.Close(); err != nil {
		t.Fatal(err)
	}

	// routeDisposition: the row is this transition's necessary fact.
	if state, stop := c.routeDisposition(ctx, taskDevelopment); !stop || state.Status != StatusFailed || state.Err == nil {
		t.Fatalf("routeDisposition = (%+v, %v), want a failed stop on an unreadable park row", state, stop)
	}
	// dispositionStands: the error form of the same necessary fact.
	if _, err := c.dispositionStands(taskDevelopment); err == nil {
		t.Fatal("dispositionStands flattened an unreadable park row into not-standing")
	}
	// The route layer surfaces it through the check-brief round pass, whose
	// skip decision needs each check stage's row.
	if state, stop := c.route(ctx); !stop || state.Status != StatusFailed || state.Err == nil {
		t.Fatalf("route = (%+v, %v), want a failed stop when a necessary park-row read fails", state, stop)
	}
	// executePlanGate: the executor cannot decide parked / record / ask anew
	// without the row.
	if _, err := c.executePlanGate(ctx, fabricatedInvocation(taskPlan, "inv-plan")); err == nil {
		t.Fatal("executePlanGate decided through an unreadable park row")
	}
}

// fabricatedInvocation builds the invocation a fabricated predecessor left.
func fabricatedInvocation(taskID, invocation string) runner.Invocation {
	return runner.Invocation{InvocationID: invocation, TaskID: taskID, Generation: 1}
}
