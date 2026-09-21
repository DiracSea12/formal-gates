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
//  4. a verifiable terminal worker result is adopted under the original
//     call identity — unless this stage's own flow must still judge it
//     (a pre-review FAIL, unresolved development findings), which parks
//     with its disposition instead, exactly as the live executor would;
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
	if outcome, ok := c.adoptable(inv, turn); ok {
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
// idempotent: a standing pending question returns as-is.
func (c *Compose) openRuntimeQuestion(ctx context.Context, taskID, turnDir string) error {
	reason := "the recorded answer failed the worker contract"
	if body, err := readJSONFile(filepath.Join(turnDir, "native-error.json")); err == nil {
		if text := textOf(getOf(body, "error")); text != "" {
			reason = text
		}
	}
	d := disposition{Task: taskID, Kind: "runtime", Reason: reason}
	body, err := CanonicalJSON(questionBody(&d))
	if err != nil {
		return err
	}
	if _, err := c.store.OpenQuestion(ctx, askTask(taskID), &runner.Confirmation{
		Options: questionOptions(&d), Allow: questionAllow(&d),
		Revision: c.spec.revision, Body: body,
	}); err != nil {
		return err
	}
	// The note the transitions read: without it a reopened owner would not
	// know this question covers a runtime disposition of this stage.
	return c.writeDisposition(d)
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
		turn := latestTurnDir(filepath.Join(c.dir, "jobs", task))
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

// adoptable verifies the turn's receipt and framed answer and, when the
// answer is a result this stage admits, returns the Adopt verdict with the
// result under the original call identity. Results whose own flow must
// still judge them (a pre-review FAIL, unresolved development findings)
// park with their disposition instead — adopting them would finish the
// stage on a result its own review loop must still ask about.
func (c *Compose) adoptable(inv runner.Invocation, turnDir string) (runner.TakeoverOutcome, bool) {
	receiptValue, err := readJSONFile(filepath.Join(turnDir, "exit.json"))
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
		return runner.TakeoverOutcome{}, false
	}
	answer, err := settleNativeReceipt(turnDir, receipt)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	var brief workerBrief
	if err := jsonUnmarshal(inv.Input, &brief); err != nil || brief.Role == "" {
		brief.Role = c.roleOf(inv.TaskID)
	}
	worker, err := validateWorkerAnswer(brief.Role, answer, c.spec.spec.Checks)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	output := &workerOutput{
		Result: *worker, NeedsRepair: NeedsRepair(*worker), Role: brief.Role,
		Revision: c.spec.revision, Generation: inv.Generation,
		Series: c.seriesKey(inv.TaskID, inv.Generation, brief),
		Turn:   turnNumberOf(turnDir), Job: jobRelOf(c.dir, turnDir),
		Identity: turnIdentity(turnDir),
	}
	action := textOf(workerOutcome(workerOutcomeInput{
		Role: brief.Role, Status: worker.Status, NeedsRepair: output.NeedsRepair})["action"])
	switch action {
	case "continue", "blocked", "unresolved-findings":
		// Unfinished or asking work: never adopt. A blocked or findings
		// result parks with its disposition (the transitions ask); a
		// CONTINUE with a dead supervisor has no session refusal and no
		// adoptable result — it falls through to the zero outcome, whose
		// restart question is the kernel's only void-and-rerun path.
		if action != "continue" {
			_ = c.recordWorkerTurn(output)
			_ = c.park(inv.TaskID, inv.Generation, output.Turn, disposition{
				Task: inv.TaskID, Kind: map[string]string{
					"blocked": "worker-blocked", "unresolved-findings": "development-findings",
				}[action],
				Reason:   blockedReason(action, worker.Summary),
				Evidence: turnEvidenceRef(inv.TaskID, inv.Generation, output.Series, output.Turn),
			})
		}
		return runner.TakeoverOutcome{}, false
	}
	// A result the stage itself must still judge parks rather than admits.
	if parking := c.parkOnResult(inv.TaskID, inv.Generation, output.Series, brief, output); parking != nil {
		return runner.TakeoverOutcome{}, false
	}
	_ = c.recordWorkerTurn(output)
	if err := c.writeOutcome(outcomeRecord{
		Task: inv.TaskID, Generation: inv.Generation, Action: "accepted",
		Worker: &output.Result, Output: output,
	}); err != nil {
		return runner.TakeoverOutcome{}, false
	}
	result, err := resultOf(output)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	return runner.TakeoverOutcome{Adopt: true, Result: result}, true
}

// blockedReason words the parked dispositions.
func blockedReason(action, summary string) string {
	if action == "blocked" {
		return summary
	}
	return "Unresolved development findings cannot be discarded"
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
