package harness

// The takeover consultation (the kernel's narrow recovery seam): when
// an owner opens a run a previous owner left with a call in
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
//  3. a verifiable receipt is settled under the original call identity
//     through the SAME per-type admission chain the live executors use
//     (adoptTurn: checks through settleCheckResult, gates through
//     finishGate, workers through settleWorkerResult), and the chain's
//     classification decides what recovery means: an admissible result is
//     adopted, while a result the stage itself must still judge (a
//     BLOCKED answer, a pre-review FAIL, a check that could not execute,
//     an unfinished CONTINUE session) parks with its disposition exactly
//     as the live executor parks it — the invocation stays under
//     observation and the transitions between runs surface its question
//     or re-arm its continuation;
//  4. a turn that ended without an acceptable business answer parks
//     the runtime disposition bound to the failed invocation and asks
//     its linked question — retry / restart-session / abort — never a
//     disguised session failure;
//  5. a supervisor still alive under its recorded identity is observed;
//  6. anything else — no receipt, nothing alive, no session refusal — is
//     the zero outcome too: whether the call ever executed cannot be
//     proven, so nothing is blindly restarted and the user disposes.

import (
	"context"
	"errors"
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
		c.noteObservation(inv.TaskID, inv.InvocationID)
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
			c.noteObservation(inv.TaskID, inv.InvocationID)
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
	if outcome, verdict := c.adoptTurn(inv, turn); verdict != admissionNone {
		if verdict == admissionAdopted {
			c.clearObservation(inv.TaskID)
			return outcome, nil
		}
		// admissionParked: the shared admission chain classified the
		// result and landed its standing disposition — the invocation
		// stays parked under observation, and the transitions between
		// runs surface the question (or re-arm the continuation) exactly
		// as the live executor's park would.
		c.noteObservation(inv.TaskID, inv.InvocationID)
		return runner.TakeoverOutcome{Observe: true}, nil
	}
	if c.contractRejected(inv, turn) {
		// A rejected answer is a runtime question of its own, never a
		// disguised session failure. The park row is bound to the failed
		// invocation's own identity, so a different failure asks afresh
		// and the same failure replayed reuses the same answer.
		if c.openRuntimeQuestion(ctx, inv, turn) {
			c.noteObservation(inv.TaskID, inv.InvocationID)
			return runner.TakeoverOutcome{Observe: true}, nil
		}
	}
	if supervisorAlive(turn) {
		c.noteObservation(inv.TaskID, inv.InvocationID)
		return runner.TakeoverOutcome{Observe: true}, nil
	}
	c.clearObservation(inv.TaskID)
	return runner.TakeoverOutcome{}, nil
}

// openRuntimeQuestion parks the runtime disposition for one
// contract-rejected turn and asks its question. The park lands through
// the same park() every live park uses, stamped with the failed
// invocation's own identity — generation and turn. The ask goes through
// the kernel's one disposition-bound Ask: it creates the pending
// question on the stage's ask task and links it to the standing row in
// the same transaction, so the consultation's ask and a later route
// pass re-deriving it are the same ask (the idempotent link returns the
// same decision id). An ask that fails to land still leaves the parked
// row standing: the route's next pass performs it.
func (c *Compose) openRuntimeQuestion(ctx context.Context, inv runner.Invocation, turnDir string) bool {
	reason := "the recorded answer failed the worker contract"
	if body, err := readJSONFile(filepath.Join(turnDir, "native-error.json")); err == nil {
		if text := textOf(getOf(body, "error")); text != "" {
			reason = text
		}
	}
	if err := c.park(inv.TaskID, inv.Generation, turnNumberOf(turnDir), runner.Disposition{
		Kind: "runtime", Reason: reason,
	}); !errors.Is(err, errParked) {
		return false // the park row itself could not land: not this fact's ask
	}
	d, stands, readErr := c.readDisposition(inv.TaskID)
	if readErr != nil || !stands {
		// The park landed above; a row that cannot be read back is the
		// route's to ask from on its own pass, exactly like the not-yet-
		// asked row below.
		return true // parked; the route asks from the row
	}
	body, err := canonicalBody(questionBody(d))
	if err != nil {
		return true // parked; the route asks from the row
	}
	_, _ = c.store.Ask(ctx, inv.TaskID, askTask(inv.TaskID), &runner.Confirmation{
		Options: questionOptions(d), Allow: questionAllow(d),
		Revision: c.spec.revision, Body: body,
	})
	return true
}

// noteObservation records one task's live-observation fact, pinned to the
// invocation the consultation judged: the watcher and the status display
// derive the exact job from that identity (the newest turn OF THAT
// invocation), never from a guess across a task's every call.
func (c *Compose) noteObservation(taskID, invocationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observed[taskID] = invocationID
}

// clearObservation drops one task's observation fact.
func (c *Compose) clearObservation(taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.observed, taskID)
}

// liveObservations reports the tasks whose observed invocations are still
// unsettled (no exit receipt yet), following each recorded invocation's
// own newest turn.
func (c *Compose) liveObservations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for task, invocation := range c.observed {
		// The job tree stores the pathSafe form of the logical TaskID; the
		// consult reads it through the same conversion.
		turn := latestTurnOf(filepath.Join(c.dir, "jobs", pathSafe(task), invocation))
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

// admission is what adopting one recovered receipt through the shared
// per-type admission chain produced. The consultation tells the cases
// apart because they mean different things to the scheduler: an admitted
// result finishes the call under its original identity; a parked one is a
// CLASSIFIED result whose standing disposition owns the next move (its
// question, or its continuation); "none" is a genuinely unclassifiable
// call — the only fact the kernel's restart ask is for.
type admission int

const (
	admissionNone    admission = iota // no verifiable, classifiable result
	admissionParked                   // classified; a standing disposition owns the next move
	admissionAdopted                  // admitted under the original call identity
)

// admissionOf maps one settle-chain outcome to the consultation's verdict.
func admissionOf(result runner.Result, err error) (runner.TakeoverOutcome, admission) {
	switch {
	case err == nil:
		return runner.TakeoverOutcome{Adopt: true, Result: result}, admissionAdopted
	case errors.Is(err, errParked):
		return runner.TakeoverOutcome{}, admissionParked
	default:
		return runner.TakeoverOutcome{}, admissionNone
	}
}

// adoptTurn settles one verifiable receipt under the original call
// identity — the recovery half of the single admission chain. How the
// receipt was obtained is the ONLY difference from the live executors:
// they wait for the supervisor they started, recovery locates the turn a
// crashed owner left behind. From here the per-type verification,
// classification and construction are the live ones, by explicit
// task-type branch: checks go through the check executor's own receipt
// rules and settleCheckResult, gate verdicts through finishGate, worker
// results through settleWorkerResult — never a generic worker shape
// standing in for a domain result.
func (c *Compose) adoptTurn(inv runner.Invocation, turnDir string) (runner.TakeoverOutcome, admission) {
	receiptValue, err := readJSONFile(filepath.Join(turnDir, "exit.json"))
	if err != nil {
		return runner.TakeoverOutcome{}, admissionNone
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
		return runner.TakeoverOutcome{}, admissionNone
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
// session question, because a check has no native session. A receipt the
// check itself must still judge (it could not execute) parks on the same
// check-runtime disposition the live executor parks on.
func (c *Compose) adoptCheckTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, admission) {
	check, err := c.checkOf(inv.TaskID)
	if err != nil {
		return runner.TakeoverOutcome{}, admissionNone
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.TakeoverOutcome{}, admissionNone
	}
	parkRuntime := func(err error) (runner.TakeoverOutcome, admission) {
		_ = c.park(inv.TaskID, inv.Generation, turnNumberOf(turnDir)-1, runner.Disposition{
			Kind: "check-runtime", Reason: err.Error(),
		})
		return runner.TakeoverOutcome{}, admissionParked
	}
	if err := checkReceiptRan(receipt); err != nil {
		return parkRuntime(err)
	}
	workspace := turnWorkspace(turnDir)
	if workspace == "" {
		return runner.TakeoverOutcome{}, admissionNone
	}
	if err := verifyCandidateIntact(workspace, candidate); err != nil {
		return parkRuntime(err)
	}
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.TakeoverOutcome{}, admissionNone
	}
	binding, err := scopeBinding(bindingInput{Root: root, Candidate: candidate, Revision: c.spec.revision, Check: check})
	if err != nil {
		return runner.TakeoverOutcome{}, admissionNone
	}
	return admissionOf(c.settleCheckResult(inv, check, binding, candidate, intOf(getOf(receipt, "exit_code")), receipt))
}

// framedTurnOutput verifies one recovered native turn and frames its
// answer: the receipt against its request, the answer under the worker
// contract of the invocation's own brief, and the worker evidence record
// built exactly as the live loop builds it (same series key, same turn
// and job references). What the output MEANS is not decided here: the
// settle functions apply the same worker-outcome branches the live loops
// apply, so a CONTINUE frames like any other answer and is classified by
// the chain that receives it.
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
	return brief, series, output, true
}

// adoptGateTurn adopts one recovered gate turn through finishGate — the
// SAME admission the live gate loop settles through — so a BLOCKED answer
// parks on the same worker-blocked disposition the live loop parks on,
// and an admitted verdict carries its candidate binding for the seal to
// later prove the same-candidate evidence. The read-only clone the
// verdict's own request names must still hold the candidate, the proof
// the live loop demands before admitting a verdict.
func (c *Compose) adoptGateTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, admission) {
	_, _, output, ok := c.framedTurnOutput(inv, turnDir, receipt)
	if !ok {
		return runner.TakeoverOutcome{}, admissionNone
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.TakeoverOutcome{}, admissionNone
	}
	if workspace := turnWorkspace(turnDir); workspace != "" {
		if err := verifyCandidateIntact(workspace, candidate); err != nil {
			return runner.TakeoverOutcome{}, admissionNone
		}
	}
	return admissionOf(c.finishGate(inv.TaskID, inv.Generation, candidate, output))
}

// adoptWorkerTurn adopts one recovered worker result through
// settleWorkerResult — the SAME admission chain the live invoke loop uses
// for fresh answers, receipt replays and recovered receipts alike. A
// result the stage itself must still judge parks with its disposition;
// an unfinished CONTINUE session parks to be re-armed.
func (c *Compose) adoptWorkerTurn(inv runner.Invocation, turnDir string, receipt map[string]any) (runner.TakeoverOutcome, admission) {
	brief, series, output, ok := c.framedTurnOutput(inv, turnDir, receipt)
	if !ok {
		return runner.TakeoverOutcome{}, admissionNone
	}
	return admissionOf(c.settleWorkerResult(inv.TaskID, inv.Generation, series, brief, output))
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

// contractRejected reports a turn that ended without an acceptable
// business answer and without the host's session refusal. The accepted
// validators decide, the same ones the live
// executor parks a runtime disposition on: any turn whose receipt never
// frames an answer, or whose framed answer the stage's worker contract
// refuses, is a runtime question here too.
func (c *Compose) contractRejected(inv runner.Invocation, turnDir string) bool {
	receiptValue, err := readJSONFile(filepath.Join(turnDir, "exit.json"))
	if err != nil {
		return false
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
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
