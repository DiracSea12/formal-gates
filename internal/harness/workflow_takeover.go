package harness

// The takeover consultation (the kernel's narrow recovery seam, wired the
// K5 way): when an owner opens a run a previous owner left with a call in
// flight, the call's fate is judged from its job directory's verifiable
// facts, in this order:
//
//  1. a task holding a pending question — the call parked for an ask this
//     owner will surface — stays parked (nothing adopts or replaces it),
//     whether or not a supervised call ever started: a parked stage's
//     retry revision (or the plan approval's re-arm) voids the invocation
//     itself;
//  2. a session the host explicitly refused (SESSION_UNAVAILABLE) is the
//     zero outcome: the kernel's restart request asks, nothing is voided
//     or re-run behind the user's back;
//  3. a turn that ended exit-0 with a framed answer the worker contract
//     rejects is its own classification (R9): a runtime question —
//     retry / restart-session / abort — opened here through OpenQuestion
//     (consultations run inside a Run, where no revision is possible, and
//     OpenQuestion takes no advance lock), with the call parked until the
//     answer moves it;
//  4. a verifiable terminal result is adopted under the original call
//     identity through the SAME per-type admission chain the live
//     executors use (adoptTurn: checks through settleCheckResult, gates
//     through finishGate, workers through settleWorkerResult) — unless
//     this stage's own flow must still judge it (a pre-review FAIL,
//     unresolved development findings, a check that could not execute),
//     which parks with its disposition instead, exactly as the live
//     executor would;
//  5. a supervisor still alive under its recorded identity is observed;
//  6. anything else — no receipt, nothing alive, no session refusal — is
//     the zero outcome too: whether the call ever executed cannot be
//     proven, so nothing is blindly restarted and the user disposes.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/runner"
)

// consult is Runner.Takeover for the composition.
func (c *Compose) consult(ctx context.Context, inv runner.Invocation) (runner.TakeoverOutcome, error) {
	// The pending-question check comes first: a stage this composition
	// deliberately parked for an ask (a disposition retry, the plan gate)
	// holds its question on the ask task, and its invocation is voided by
	// the answer's revision — not by a restart decision.
	if c.pendingQuestionOn(ctx, inv.TaskID) || c.pendingQuestionOn(ctx, askTask(inv.TaskID)) {
		c.noteObservation(inv.TaskID, true)
		return runner.TakeoverOutcome{Observe: true}, nil
	}
	// The consultation reads THE invocation's own turns: a task holds one
	// job directory per generation, and the newest evidence is the newest
	// turn of exactly the invocation being judged.
	turn := latestTurnOf(filepath.Join(c.dir, "jobs", pathSafe(inv.TaskID), inv.InvocationID))
	if turn == "" {
		// A standing disposition with no call at all is a deliberate park
		// that never reached the host: the retry revision owns it.
		if c.dispositionStands(inv.TaskID) {
			c.noteObservation(inv.TaskID, true)
			return runner.TakeoverOutcome{Observe: true}, nil
		}
		// The invocation never spawned a supervised call — provably nothing
		// ran. Still not a blind restart: the zero outcome asks.
		return runner.TakeoverOutcome{}, nil
	}
	if refusedSession(turn) {
		// The zero fact asks through the kernel's restart request; the
		// marker that held the stage for this consultation ends here.
		c.clearSessionRefusal(inv.TaskID)
		return runner.TakeoverOutcome{}, nil
	}
	if outcome, ok := c.adoptTurn(inv, turn); ok {
		c.noteObservation(inv.TaskID, false)
		return outcome, nil
	}
	if c.contractRejected(inv, turn) {
		// R9: a rejected answer is a runtime question of its own, never a
		// disguised session failure.
		if err := c.openRuntimeQuestion(ctx, inv.TaskID, turn); err == nil {
			c.noteObservation(inv.TaskID, true)
			return runner.TakeoverOutcome{Observe: true}, nil
		}
	}
	if supervisorAlive(turn) {
		c.noteObservation(inv.TaskID, true)
		return runner.TakeoverOutcome{Observe: true}, nil
	}
	c.noteObservation(inv.TaskID, false)
	return runner.TakeoverOutcome{}, nil
}

// openRuntimeQuestion opens the runtime disposition's question on the
// stage's question task through the kernel's mid-flight seam. It is
// idempotent: a standing pending question returns as-is. The body carries
// the disposition id through the same encoding as placeQuestion, so the
// ask-once read (askRecorded) recognizes this spelling too. The park row
// lands FIRST — an owner dying between the two writes leaves the row the
// route re-derives the ask from, never a question the route cannot see.
func (c *Compose) openRuntimeQuestion(ctx context.Context, taskID, turnDir string) error {
	reason := "the recorded answer failed the worker contract"
	if body, err := readJSONFile(filepath.Join(turnDir, "native-error.json")); err == nil {
		if text := textOf(getOf(body, "error")); text != "" {
			reason = text
		}
	}
	d := runner.Disposition{TaskID: taskID, Kind: "runtime", Reason: reason}
	if err := c.store.SetDisposition(ctx, d); err != nil {
		return err
	}
	// The same encoding boundary placeQuestion uses: question bodies cross
	// as plain decoded values, never typed Go fields.
	body, err := canonicalBody(questionBody(&d, dispositionIDOf(taskID, &d)))
	if err != nil {
		return err
	}
	_, err = c.store.OpenQuestion(ctx, askTask(taskID), &runner.Confirmation{
		Options: questionOptions(&d), Allow: questionAllow(&d),
		Revision: c.spec.revision, Body: body,
	})
	return err
}

// noteObservation records or clears one task's live-observation fact for
// classify and the interaction watcher.
func (c *Compose) noteObservation(taskID string, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.observed[taskID] = true
	} else {
		delete(c.observed, taskID)
	}
}

// liveObservations reports the tasks whose observed calls are still
// unsettled (no exit receipt yet).
func (c *Compose) liveObservations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for task := range c.observed {
		// The job tree stores the pathSafe form of the logical TaskID; the
		// consult reads it through the same conversion.
		turn := latestTurnDir(filepath.Join(c.dir, "jobs", pathSafe(task)))
		if turn == "" {
			delete(c.observed, task)
			continue
		}
		if _, err := os.Stat(filepath.Join(turn, "exit.json")); err == nil {
			delete(c.observed, task) // settled: the next consult classifies it
			continue
		}
		out = append(out, task)
	}
	sortStrings(out)
	return out
}

// adoptTurn settles one verifiable receipt under the original call
// identity — the recovery half of the single admission chain. How the
// receipt was obtained is the ONLY difference from the live executors:
// they wait for the supervisor they started, recovery locates the turn a
// crashed owner left behind. From here the per-type verification and
// construction are the live ones, by explicit task-type branch: checks go
// through the check executor's own receipt rules and settleCheckResult,
// gate verdicts through finishGate, worker results through
// settleWorkerResult — never a generic worker shape standing in for a
// domain result. A receipt the stage itself must still judge (a blocked
// answer, a pre-review FAIL, a check that could not execute) parks with
// its disposition and reports no adoption: the standing question owns
// the next move, exactly as it would live.
func (c *Compose) adoptTurn(inv runner.Invocation, turnDir string) (runner.TakeoverOutcome, bool) {
	receiptValue, err := readJSONFile(filepath.Join(turnDir, "exit.json"))
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
		return runner.TakeoverOutcome{}, false
	}
	switch {
	case strings.HasPrefix(inv.TaskID, "check:"):
		return c.adoptCheckTurn(inv, turnDir, receipt)
	case strings.HasPrefix(inv.TaskID, "gate:"):
		return c.adoptGateTurn(inv, turnDir, receipt)
	default:
		return c.adoptWorkerTurn(inv, turnDir, receipt)
	}
}

// adoptCheckTurn adopts one recovered check receipt through the check
// executor's own rules: the receipt must describe a run that actually
// executed (checkReceiptRan) inside a clone that still holds the frozen
// candidate (verifyCandidateIntact), and the record lands through
// settleCheckResult with the binding re-derived exactly as the live
// executor derives it — a completed check is never read as a native
// session question, because a check has no native session.
func (c *Compose) adoptCheckTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, bool) {
	check, err := c.checkOf(inv.TaskID)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.TakeoverOutcome{}, false
	}
	parkRuntime := func(err error) (runner.TakeoverOutcome, bool) {
		_ = c.park(inv.TaskID, inv.Generation, turnNumberOf(turnDir)-1, runner.Disposition{
			Kind: "check-runtime", Reason: err.Error(),
		})
		return runner.TakeoverOutcome{}, false
	}
	if err := checkReceiptRan(receipt); err != nil {
		return parkRuntime(err)
	}
	workspace := turnWorkspace(turnDir)
	if workspace == "" {
		return runner.TakeoverOutcome{}, false
	}
	if err := verifyCandidateIntact(workspace, candidate); err != nil {
		return parkRuntime(err)
	}
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	binding, err := scopeBinding(bindingInput{Root: root, Candidate: candidate, Revision: c.spec.revision, Check: check})
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	result, err := c.settleCheckResult(inv, check, binding, candidate, intOf(getOf(receipt, "exit_code")), receipt)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	return runner.TakeoverOutcome{Adopt: true, Result: result}, true
}

// framedTurnOutput verifies one recovered native turn and frames its
// answer: the receipt against its request, the answer under the worker
// contract of the invocation's own brief, and the worker evidence record
// built exactly as the live loop builds it (same series key, same turn
// and job references). A CONTINUE answer is not a result — its next turn
// never started, the owner died — and reports false.
func (c *Compose) framedTurnOutput(inv runner.Invocation, turnDir string, receipt map[string]any) (workerBrief, string, *workerOutput, bool) {
	answer, err := settleNativeReceipt(turnDir, receipt)
	if err != nil {
		return workerBrief{}, "", nil, false
	}
	brief := c.briefOf(inv)
	worker, err := validateWorkerAnswer(brief.Role, answer, c.spec.spec.Checks)
	if err != nil {
		return workerBrief{}, "", nil, false
	}
	series := c.seriesKey(inv.TaskID, inv.Generation, brief)
	output := &workerOutput{
		Result: *worker, NeedsRepair: NeedsRepair(*worker), Role: brief.Role,
		Revision: c.spec.revision, Generation: inv.Generation,
		Series: series, Turn: turnNumberOf(turnDir), Job: jobRelOf(c.dir, turnDir),
		Identity: turnIdentity(turnDir),
	}
	if textOf(workerOutcome(workerOutcomeInput{
		Role: brief.Role, Status: worker.Status, NeedsRepair: output.NeedsRepair})["action"]) == "continue" {
		return workerBrief{}, "", nil, false
	}
	return brief, series, output, true
}

// adoptGateTurn adopts one recovered gate verdict through finishGate —
// the SAME entry the live gate loop admits through — so the outcome
// carries its candidate binding and the seal can later prove the
// same-candidate evidence. The read-only clone the verdict's own request
// names must still hold the candidate, the proof the live loop demands
// before admitting a verdict.
func (c *Compose) adoptGateTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, bool) {
	_, _, output, ok := c.framedTurnOutput(inv, turnDir, receipt)
	if !ok {
		return runner.TakeoverOutcome{}, false
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.TakeoverOutcome{}, false
	}
	if workspace := turnWorkspace(turnDir); workspace != "" {
		if err := verifyCandidateIntact(workspace, candidate); err != nil {
			return runner.TakeoverOutcome{}, false
		}
	}
	result, err := c.finishGate(inv.TaskID, inv.Generation, candidate, output)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	return runner.TakeoverOutcome{Adopt: true, Result: result}, true
}

// adoptWorkerTurn adopts one recovered worker result through
// settleWorkerResult — the SAME admission chain the live invoke loop uses
// for both fresh answers and receipt replays. A result the stage itself
// must still judge parks with its disposition and reports no adoption.
func (c *Compose) adoptWorkerTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, bool) {
	brief, series, output, ok := c.framedTurnOutput(inv, turnDir, receipt)
	if !ok {
		return runner.TakeoverOutcome{}, false
	}
	result, err := c.settleWorkerResult(inv.TaskID, inv.Generation, series, brief, output)
	if err != nil {
		return runner.TakeoverOutcome{}, false // parked: the question owns the next move
	}
	return runner.TakeoverOutcome{Adopt: true, Result: result}, true
}

// turnWorkspace is the workspace a completed turn's request names — where
// its command actually ran, recoverable after the owner that knew it died.
func turnWorkspace(turnDir string) string {
	value, err := readJSONFile(filepath.Join(turnDir, "request.json"))
	if err != nil {
		return ""
	}
	return textOf(getOf(value, "cwd"))
}

// contractRejected reports a turn that exited 0 with a framed answer the
// worker contract refuses — the R9 classification. The accepted validator
// itself decides: any answer this stage's live executor would park on is a
// runtime question here too.
func (c *Compose) contractRejected(inv runner.Invocation, turnDir string) bool {
	receiptValue, err := readJSONFile(filepath.Join(turnDir, "exit.json"))
	if err != nil {
		return false
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok || !valuesEqual(getOf(receipt, "exit_code"), 0) {
		return false
	}
	if _, err := os.Stat(filepath.Join(turnDir, "answer.json")); err != nil {
		return false
	}
	answer, err := settleNativeReceipt(turnDir, receipt)
	if err != nil {
		// The receipt itself is broken: a runtime question covers that too.
		return true
	}
	var brief workerBrief
	if jsonUnmarshal(inv.Input, &brief) != nil || brief.Role == "" {
		brief.Role = c.roleOf(inv.TaskID)
	}
	_, err = validateWorkerAnswer(brief.Role, answer, c.spec.spec.Checks)
	return err != nil
}

// turnNumberOf parses a turn directory's sequence.
func turnNumberOf(turnDir string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(turnDir), "turn-"))
	if err != nil {
		return 1
	}
	return n
}

// jobRelOf renders a job directory relative to the run directory.
func jobRelOf(dir, jobDir string) string {
	rel, err := filepath.Rel(dir, jobDir)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// supervisorAlive applies the identity rule to the supervisor the process
// record names: the PID alone is nothing — pid reuse would let a stranger
// answer for the dead owner — so the live process's creation time must
// match the recorded started_at.
func supervisorAlive(turnDir string) bool {
	value, err := readJSONFile(filepath.Join(turnDir, "process.json"))
	if err != nil {
		return false
	}
	pid := intOf(getOf(value, "pid"))
	if pid <= 0 {
		return false
	}
	if !coordination.ProcessAlive(pid) {
		return false
	}
	started := floatOf(getOf(value, "started_at"))
	if started == 0 {
		return false
	}
	created, ok := processCreationTime(pid)
	if !ok {
		return false
	}
	delta := created.Unix() - int64(started)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2
}

// sortStrings sorts in place (a tiny local helper keeps the import set
// honest).
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
