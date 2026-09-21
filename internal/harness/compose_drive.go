package harness

// The drive loop and the domain routing (plan §3, §8.2, §13.2). Drive runs
// the kernel's Run and, between runs, applies the composition's routing over
// durable outcomes only — completing granted repair revisions and driving the
// repair loop a candidate refusal asks for — then classifies each stop. It
// never schedules anything itself: after routing, the kernel alone decides
// what may run next.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/runner"
)

// composeMaxIterations bounds one Drive call's run/route cycles. Every cycle
// either settles work or surfaces a user input, so the bound is a runaway
// guard, not a mechanism.
const composeMaxIterations = 64

// Drive advances the run until a user input is needed or the run reaches a
// classified stop. Routing (repair revisions, granted completions) runs
// between kernel runs, when no Run is advancing. While a Run is in flight
// the call also watches for held native interactions: one is returned
// immediately, the run keeps advancing in the background, and the next Drive
// attaches to it (a second Run on the same store would refuse with
// ErrAdvancing).
func (c *Compose) Drive(ctx context.Context) ComposeState {
	for iteration := 0; iteration < composeMaxIterations; iteration++ {
		c.runMu.Lock()
		idle := c.runDone == nil
		c.runMu.Unlock()
		if idle {
			if state, stop := c.route(ctx); stop {
				return state
			}
		}
		done := c.ensureRun()
		select {
		case err := <-done:
			c.finishRun(err)
			if state, stop := c.classify(ctx, err); stop {
				return state
			}
		case notice := <-c.heldCh:
			if held, ok := c.heldInteraction(notice); ok {
				return ComposeState{Status: StatusWaitingInteraction, Held: []HeldInteraction{held}}
			}
			// Already answered while buffered — keep waiting on the run.
			continue
		case <-ctx.Done():
			return ComposeState{Status: StatusFailed, Err: ctx.Err()}
		}
	}
	err := fmt.Errorf("compose: drive stopped after %d cycles without settling", composeMaxIterations)
	return ComposeState{Status: StatusFailed, Err: err}
}

// ensureRun starts the kernel Run if none is advancing and returns its
// completion channel, or nil when it just routed (the caller calls it again).
// A Run already in flight is returned as-is.
func (c *Compose) ensureRun() <-chan error {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.runDone != nil {
		return c.runDone
	}
	specs := c.specsSnapshot()
	done := make(chan error, 1)
	c.runDone = done
	go func() {
		done <- c.runner.Run(c.baseCtx, specs)
	}()
	return done
}

// finishRun clears the in-flight slot and remembers the outcome for
// Cancel/Close reporting.
func (c *Compose) finishRun(err error) {
	c.runMu.Lock()
	c.runDone = nil
	c.runMu.Unlock()
}

// waitRunDone waits up to bound for an in-flight Run goroutine to end.
func (c *Compose) waitRunDone(bound time.Duration) {
	c.runMu.Lock()
	done := c.runDone
	c.runMu.Unlock()
	if done == nil {
		return
	}
	select {
	case err := <-done:
		c.finishRun(err)
	case <-time.After(bound):
	}
}

// classify maps one Run outcome to a Drive stop, applying the terminal
// judgment when the kernel drained cleanly. Errors that the next cycle can
// settle (a no-receipt invocation the takeover consult will classify, a
// granted revision awaiting its completion call) return stop=false after a
// progress check.
func (c *Compose) classify(ctx context.Context, err error) (ComposeState, bool) {
	switch {
	case err == nil:
		return c.terminalJudgment()
	case errors.Is(err, runner.ErrWaitingDecision):
		pending, perr := c.store.PendingDecisions(ctx)
		if perr != nil {
			return ComposeState{Status: StatusFailed, Err: perr}, true
		}
		// A premature gate wait must not mask the user's own refusal: the
		// kernel's settle order reports any pending gate before a refused
		// answer, so the composition's abort judgment reads its own
		// presentation record here.
		c.rememberSurfaced(pending)
		if c.refusedAnswer() {
			return c.abortedState(), true
		}
		// Nor may it mask a live observation: presenting the approval gate
		// while a previous owner's call is still in flight would ask the
		// user to approve work that has not happened.
		if observations := c.liveObservations(); len(observations) > 0 {
			return ComposeState{Status: StatusObserved, Pending: pending,
				Err: fmt.Errorf("compose: %s still in flight under observation", strings.Join(observations, ", "))}, true
		}
		return ComposeState{Status: StatusWaitingDecision, Pending: c.presentable(pending)}, true
	case errors.Is(err, runner.ErrDecisionRefused):
		// A gate was answered without the allowing choice: the user aborted.
		return c.abortedState(), true
	case errors.Is(err, runner.ErrCallAborted):
		return c.abortedState(), true
	case errors.Is(err, runner.ErrCallObserved):
		return ComposeState{Status: StatusObserved}, true
	case errors.Is(err, runner.ErrTakeoverRequired):
		return ComposeState{Status: StatusObserved}, true
	case errors.Is(err, runner.ErrRevisionGranted):
		// A repair the user allowed but nobody completed yet: route()
		// completes it on the next cycle.
		return ComposeState{}, false
	case isSpecMismatch(err):
		// The adopted spec cache is ahead of the database (a crash between
		// the cache write and its revision). Re-derive from the outcome trail
		// once; the retry re-verifies against the bound digests.
		c.specsMu.Lock()
		rebuilt := c.specsRebuilt
		c.specsMu.Unlock()
		if !rebuilt {
			c.deriveSpecsFromOutcomes()
			return ComposeState{}, false
		}
		return ComposeState{Status: StatusFailed, Err: err}, true
	default:
		// Executor failures (no receipt) and anything else: the next cycle's
		// recovery consult settles the pending invocation (adopt, observe or
		// a restart ask), which changes the observable state. Without
		// progress the failure is surfaced rather than spun on.
		if c.progressSinceLastCycle() {
			return ComposeState{}, false
		}
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
}

// abortedState records and returns the ABORTED terminal.
func (c *Compose) abortedState() ComposeState {
	c.terminalStatus = statusAborted
	return ComposeState{Status: StatusAborted, Terminal: statusAborted}
}

// presentable filters the kernel's pending questions to the ones whose stage
// the chain has actually reached. A gate materializes at run creation, so
// the approval question exists before any candidate does; presenting it
// there would ask the user to approve work that does not exist yet, so a
// gate question waits until every dependency of its task has executed.
// Request-side asks (repair, restart) always belong to the frontier and are
// never filtered. If the filter would present nothing, the raw list wins —
// presenting early beats presenting nothing.
func (c *Compose) presentable(pending []runner.DecisionView) []runner.DecisionView {
	var out []runner.DecisionView
	for _, question := range pending {
		stage, found := composeStageByID(question.TaskID)
		if !found || stage.Gate == nil {
			out = append(out, question) // an ask, or a foreign task's gate
			continue
		}
		mature := true
		for _, dep := range stage.Depends {
			if c.maxOutcomeGeneration(dep) == 0 {
				mature = false
				break
			}
		}
		if mature {
			out = append(out, question)
		}
	}
	if len(out) == 0 {
		return pending
	}
	return out
}

// isSpecMismatch recognizes the kernel's input-digest refusal — the one error
// whose cure (re-deriving the spec set) is known. The message text is pinned
// by the kernel's verifySpecs and asserted by the composition's tests.
func isSpecMismatch(err error) bool {
	return err != nil && strings.Contains(err.Error(), "differs from the run's definition")
}

// progressSinceLastCycle compares a cheap signature of the observable state
// (outcome records, open requests, pending decisions) with the previous
// cycle's, so the drive loop cannot spin on an unchanging failure.
func (c *Compose) progressSinceLastCycle() bool {
	signature := c.stateSignature()
	c.specsMu.Lock()
	last := c.lastSignature
	c.lastSignature = signature
	c.specsMu.Unlock()
	return signature != last
}

// stateSignature renders the observable-state signature.
func (c *Compose) stateSignature() string {
	sig := ""
	entries, err := os.ReadDir(filepath.Join(c.cfg.Dir, "outcomes"))
	if err == nil {
		for _, entry := range entries {
			sig += "o:" + entry.Name() + ";"
		}
	}
	if requests, err := c.store.OpenRequests(context.Background()); err == nil {
		for _, request := range requests {
			sig += fmt.Sprintf("r:%s:%s;", request.RequestID, request.State)
		}
	}
	if pending, err := c.store.PendingDecisions(context.Background()); err == nil {
		for _, decision := range pending {
			sig += "d:" + decision.DecisionID + ";"
		}
	}
	return sig
}

// route applies the domain routing between runs, from durable outcomes only:
//
//  1. complete every granted repair revision (the user allowed the round;
//     the grant executes nothing by itself — the revised input travels in
//     this call);
//  2. drive the repair loop a candidate refusal asks for: the target is
//     development for an unresolved development result, qa-design for an
//     unapproved QA review (plan §3's per-branch local repair), and the ask
//     at the allowance goes through Store.RequestRevision with the domain
//     repairAllowance bound as the kernel's RepairPolicy (0-based rounds).
//
// Routing keyed on generations read from the outcome records is naturally
// idempotent across reopens: a refusal whose target generation has already
// moved on has been repaired.
func (c *Compose) route(ctx context.Context) (ComposeState, bool) {
	// A presented question answered with anything but its allowing choice is
	// the user's refusal — of a gate, a repair round or a restart: the run
	// cannot complete and must not claim success.
	if c.refusedAnswer() {
		return c.abortedState(), true
	}
	requests, err := c.store.OpenRequests(ctx)
	if err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	completed := false
	for _, request := range requests {
		if request.Kind != "revision" || request.State != "granted" {
			continue
		}
		if state, stop := c.completeRevision(ctx, request.TaskID); stop {
			return state, true
		}
		completed = true
	}
	// A completion this cycle re-derives the world (generations moved); the
	// refusal routing re-reads it on the next cycle.
	if completed {
		return ComposeState{}, false
	}
	if state, stop := c.routeCandidateRefusal(ctx, requests); stop {
		return state, true
	}
	return ComposeState{}, false
}

// completeRevision finishes a user-allowed repair round: re-send the revised
// input through RequestRevision, which executes the revision and moves the
// request to executed in one transaction.
func (c *Compose) completeRevision(ctx context.Context, taskID string) (ComposeState, bool) {
	stage, ok := composeStageByID(taskID)
	if !ok {
		return ComposeState{Status: StatusFailed,
			Err: fmt.Errorf("compose: granted revision names unknown task %q", taskID)}, true
	}
	specs := c.specsSnapshot()
	for _, spec := range specs {
		if spec.TaskID == taskID {
			if err := c.requestRevision(ctx, stage, spec.Input); err != nil {
				return ComposeState{Status: StatusFailed, Err: err}, true
			}
			return ComposeState{}, false
		}
	}
	return ComposeState{Status: StatusFailed,
		Err: fmt.Errorf("compose: no spec for granted revision of %q", taskID)}, true
}

// routeCandidateRefusal inspects the candidate gate's latest verdict: a
// refusal drives one repair revision of its target branch — directly below
// the allowance, as a durable ask at it.
func (c *Compose) routeCandidateRefusal(ctx context.Context, requests []runner.RequestView) (ComposeState, bool) {
	candidate := c.latestOutcome(stageCandidate)
	if candidate == nil || candidate.Admission == nil || candidate.Admission.OK {
		return ComposeState{}, false
	}
	fact := candidate.Admission
	// development first — candidateAdmission's own order: an unresolved
	// development result is the primary refusal.
	target := ""
	targetGen := int64(0)
	findingsTask := ""
	if fact.DevelopmentRepair {
		target, targetGen, findingsTask = stageDevelopment, fact.DevelopmentGen, stageDevelopment
	} else if fact.QAReviewRepair {
		// The QA branch repairs its design with the review's findings.
		target, targetGen, findingsTask = stageQADesign, fact.QAReviewGen, stageQAReview
	} else {
		return ComposeState{}, false
	}
	// Already asked: an active revision request for the target means the
	// refusal is being handled — its answer grants (route completes it) or
	// refuses (route aborts); asking again would collide with itself.
	for _, request := range requests {
		if request.Kind == "revision" && request.TaskID == target && request.State == "pending" {
			return ComposeState{}, false
		}
	}
	// Already routed, two readings of the same fact: the durable marker
	// (compose-routed.json) records the generation this task's last routed
	// refusal was taken at — a revision directly executed below the allowance
	// leaves no pending request, so the marker is what keeps the next cycle
	// from reading the same refusal twice — and a target generation already
	// past the refusal's means the repair happened even if the marker was
	// lost (a crash between the revision and its save): several repair rounds
	// each route exactly once, and a reopened owner reads the same fact.
	c.specsMu.Lock()
	routedAt := c.routedMarkers[target]
	c.specsMu.Unlock()
	if routedAt >= targetGen || c.maxOutcomeGeneration(target) > targetGen {
		return ComposeState{}, false
	}
	stage, _ := composeStageByID(target)
	source := c.readOutcome(findingsTask, targetGen)
	round := int(c.maxOutcomeGeneration(target)) + 1
	input, err := repairBrief(stage, c.cfg.Requirement, round, findingsOf(source))
	if err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	if err := c.requestRevision(ctx, stage, input); err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	c.markRouted(target, targetGen)
	return ComposeState{}, false
}

// markRouted records and persists one task's routed generation. It lands
// after the revision call, not before: a marker without its revision would
// silence a refusal forever, while a revision without its marker is caught by
// the pending request and the advanced generation above until this save.
func (c *Compose) markRouted(taskID string, generation int64) {
	c.specsMu.Lock()
	c.routedMarkers[taskID] = generation
	markers := c.routedMarkers
	c.specsMu.Unlock()
	saveRoutedMarkers(filepath.Join(c.cfg.Dir, "compose-routed.json"), markers)
}

// requestRevision is the single spelling of a repair round: replace the spec
// input first (the cache must never be behind the database), then hand the
// revision to the kernel with the exhaustion question and the domain
// allowance as policy. ErrWaitingDecision means the ask is durable and
// surfaces with the next Run's settle; the user's answer travels through
// Answer, and a refusing choice reaches classify through the surfaced
// presentation record.
func (c *Compose) requestRevision(ctx context.Context, stage composeStage, input []byte) error {
	if err := c.replaceSpecInput(stage.TaskID, input); err != nil {
		return err
	}
	spec := runner.TaskSpec{TaskID: stage.TaskID, Name: stage.Name, Input: input}
	ask := &runner.Confirmation{
		Options: []string{"continue-repair", "abort"},
		Allow:   "continue-repair",
		Body:    []byte("The repair allowance for this task is exhausted. Allow one more repair round, or abort the run."),
	}
	err := c.store.RequestRevision(ctx, stage.TaskID, spec, ask, c.repairPolicy(), c.cfg.RepairLimit)
	if errors.Is(err, runner.ErrWaitingDecision) {
		return nil // surfaced by the next Run's settle as ErrWaitingDecision
	}
	return err
}

// repairPolicy wraps domain.repairAllowance as the kernel's RepairPolicy:
// the effective allowance is the bound initial plus the user's grants, and a
// round at or past it asks (the accepted >= boundary, 0-based rounds).
func (c *Compose) repairPolicy() runner.RepairPolicy {
	return func(round, allowance int) bool {
		verdict := repairAllowance(repairAllowanceInput{
			Round:       round,
			Allowance:   &allowance,
			RepairLimit: c.cfg.RepairLimit,
		})
		return verdict["ask_user"] == true
	}
}

// terminalJudgment is the §3.2 completeness check over the durable outcomes:
// every role's latest result must be acceptable (workerOutcome accept), no
// BLOCKED fact may stand, the candidate must have been admitted, and
// evaluateRoute must route the final gate review to seal.
func (c *Compose) terminalJudgment() (ComposeState, bool) {
	for _, stage := range composeGraph() {
		if stage.Role == "" {
			continue
		}
		outcome := c.latestOutcome(stage.TaskID)
		if outcome == nil {
			// Not all worker stages executed — cannot happen after a clean
			// drain, but a judgment never guesses.
			return ComposeState{Status: StatusNeedsRepair,
				Err: fmt.Errorf("compose: no outcome for %s", stage.TaskID)}, true
		}
		if outcome.Action == "blocked" {
			if outcome.Worker != nil {
				return ComposeState{Status: StatusBlocked,
					Err: fmt.Errorf("compose: %s reported BLOCKED: %s", stage.TaskID, outcome.Worker.Summary)}, true
			}
			return ComposeState{Status: StatusBlocked}, true
		}
		if outcome.Action != "accept" {
			return ComposeState{Status: StatusNeedsRepair,
				Err: fmt.Errorf("compose: %s holds an unresolved result (%s)", stage.TaskID, outcome.Action)}, true
		}
	}
	candidate := c.latestOutcome(stageCandidate)
	if candidate == nil || candidate.Admission == nil || !candidate.Admission.OK {
		return ComposeState{Status: StatusNeedsRepair,
			Err: errors.New("compose: no admitted candidate")}, true
	}
	gate := c.latestOutcome(stageGateReview)
	if gate == nil || gate.Worker == nil {
		return ComposeState{Status: StatusNeedsRepair,
			Err: errors.New("compose: no gate review outcome")}, true
	}
	verdict := evaluateRoute(evaluateRouteInput{
		Checks: []routeCheck{},
		Gates:  []routeGate{{Gate: stageGateReview, Result: *gate.Worker}},
	})
	if textOf(verdict["route"]) != "seal" {
		return ComposeState{Status: StatusNeedsRepair,
			Err: fmt.Errorf("compose: route judgement is %s", textOf(verdict["route"]))}, true
	}
	c.terminalStatus = statusHarnessComplete
	return ComposeState{Status: StatusComplete, Terminal: statusHarnessComplete}, true
}

// Cancel stops the run and settles its in-flight supervised calls (§8.1):
// dispatching stops first (the base context cancels the kernel's Run), then
// every live supervisor's process group gets the SIGTERM the supervisor turns
// into a cancellation receipt, and only once every receipt has landed — and
// the in-flight Run goroutine has returned — is the settlement reported.
// Until then, and for any job that never settles, cancellation is not
// claimed to be complete.
func (c *Compose) Cancel(ctx context.Context) ([]JobSettlement, error) {
	c.baseCancel()
	jobs := c.snapshotLive()
	for _, job := range jobs {
		terminateJob(job)
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var settled []JobSettlement
	var unsettled []string
	for _, job := range jobs {
		receipt := waitForReceipt(job.dir, deadline)
		if receipt == nil {
			unsettled = append(unsettled, job.dir)
			continue
		}
		settled = append(settled, JobSettlement{
			TaskID:   job.taskID,
			JobDir:   job.dir,
			ExitCode: int(rationalInt(getOf(receipt, "exit_code"))),
			Error:    textOf(getOf(receipt, "error")),
		})
	}
	c.waitRunDone(30 * time.Second)
	if len(unsettled) > 0 {
		return settled, fmt.Errorf("compose: cancellation incomplete; no settled receipt for %s",
			strings.Join(unsettled, ", "))
	}
	return settled, nil
}

// waitForReceipt polls one job directory for its exit receipt until the
// deadline; nil means it never settled.
func waitForReceipt(jobDir string, deadline time.Time) map[string]any {
	for time.Now().Before(deadline) {
		if value, err := readJSONFile(filepath.Join(jobDir, "exit.json")); err == nil {
			if receipt, ok := value.(map[string]any); ok {
				return receipt
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// rationalInt renders a decoded JSON number as an int when it is one.
func rationalInt(value any) int {
	if number, ok := value.(json.Number); ok {
		if n, err := number.Int64(); err == nil {
			return int(n)
		}
	}
	return -1
}

// composeStageByID finds one stage of the fixed graph.
func composeStageByID(taskID string) (composeStage, bool) {
	for _, stage := range composeGraph() {
		if stage.TaskID == taskID {
			return stage, true
		}
	}
	return composeStage{}, false
}
