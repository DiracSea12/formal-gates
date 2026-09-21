package harness

// The takeover consult (plan §8.2, the K5 wiring of the kernel's narrow
// seam): when a new owner opens a run whose previous owner left a call in
// flight, whether that call is dead is judged here, from the job directory's
// verifiable facts only, in the reference classification order:
//
//   - a session the host explicitly refused (SESSION_UNAVAILABLE) is the zero
//     outcome: the fact creates a pending restart request (Runner.Restart's
//     question) and nothing is voided or re-run without the user's answer;
//   - a verifiable result — the supervisor's receipt checks out and the
//     framed answer is a terminal worker outcome — is Adopted under the
//     original call identity. A CONTINUE answer is deliberately NOT
//     adoptable: the work is unfinished, and adopting it would finish the
//     task on an intermediate state. It falls to the disposition below.
//   - a supervisor still alive under its recorded identity — the PID plus
//     the creation time the process record carries (§8.1: an integer alone
//     proves nothing) — is Observed: the call stays pending, its task out of
//     the ready set, no second executor starts;
//   - anything else — no receipt, nothing alive, no session error — is also
//     the zero outcome: whether the call ever executed cannot be proven, so
//     nothing is blindly restarted and the user disposes through the restart
//     question.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/runner"
)

// consult is Runner.Takeover for the composition: judge one pending
// invocation from its job directory's evidence. Consultations run outside any
// transaction and may read files.
func (c *Compose) consult(ctx context.Context, inv runner.Invocation) (runner.TakeoverOutcome, error) {
	base := filepath.Join(c.cfg.Dir, "jobs", inv.TaskID, inv.InvocationID)
	turn, err := latestTurnDir(base)
	if err != nil {
		return runner.TakeoverOutcome{}, err
	}
	if turn == "" {
		// The invocation never spawned a supervised call — provably nothing
		// ran. Still not a blind restart: the user disposes.
		return runner.TakeoverOutcome{}, nil
	}
	if refusedSession(turn) {
		c.noteObservation(inv.TaskID, "")
		return runner.TakeoverOutcome{}, nil
	}
	if result, ok := adoptableResult(c, inv, turn); ok {
		c.noteObservation(inv.TaskID, "")
		return result, nil
	}
	if supervisorAlive(turn) {
		c.noteObservation(inv.TaskID, inv.InvocationID)
		return runner.TakeoverOutcome{Observe: true}, nil
	}
	c.noteObservation(inv.TaskID, "")
	return runner.TakeoverOutcome{}, nil
}

// noteObservation records or clears one task's live-observation fact for
// classify; an empty invocation settles the entry.
func (c *Compose) noteObservation(taskID, invocationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if invocationID == "" {
		delete(c.observed, taskID)
	} else {
		c.observed[taskID] = invocationID
	}
}

// liveObservations reports the tasks whose observed invocations are still
// unsettled (no exit receipt yet) — the drives that must stop as observed.
func (c *Compose) liveObservations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for task, invocation := range c.observed {
		turn, err := latestTurnDir(filepath.Join(c.cfg.Dir, "jobs", task, invocation))
		if err != nil || turn == "" {
			delete(c.observed, task)
			continue
		}
		if _, err := os.Stat(filepath.Join(turn, "exit.json")); err == nil {
			// Settled: the next consult adopts or asks; not an observation.
			delete(c.observed, task)
			continue
		}
		out = append(out, task)
	}
	sort.Strings(out)
	return out
}

// latestTurnDir picks the highest turn directory of one invocation's job
// base; "" when no turn ever started.
func latestTurnDir(base string) (string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	best, bestN := "", -1
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !strings.HasPrefix(name, "turn-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "turn-"))
		if err != nil {
			continue
		}
		if n > bestN {
			best, bestN = filepath.Join(base, name), n
		}
	}
	if bestN < 0 {
		return "", nil
	}
	return best, nil
}

// refusedSession reports whether the turn's native error record carries the
// host's explicit persistent-identity refusal (the sessionUnavailableError
// family the accepted adapters record as error_kind SESSION_UNAVAILABLE).
func refusedSession(turnDir string) bool {
	value, err := readJSONFile(filepath.Join(turnDir, "native-error.json"))
	if err != nil {
		return false
	}
	return textOf(getOf(value, "error_kind")) == "SESSION_UNAVAILABLE"
}

// adoptableResult verifies the turn's receipt and framed answer and, when
// the answer is a terminal worker outcome, returns the Adopt verdict with
// the result under the original call identity. The outcome record is written
// here too, so routing and the terminal judgment see adopted results exactly
// like executed ones.
func adoptableResult(c *Compose, inv runner.Invocation, turnDir string) (runner.TakeoverOutcome, bool) {
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
		// Unverifiable or failed: not adoptable, whatever else it says.
		return runner.TakeoverOutcome{}, false
	}
	stage, found := composeStageByID(inv.TaskID)
	if !found || stage.Role == "" {
		return runner.TakeoverOutcome{}, false
	}
	worker, err := validateWorkerAnswer(stage.Role, answer)
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	action := textOf(workerOutcome(workerOutcomeInput{
		Role: stage.Role, Status: worker.Status, NeedsRepair: NeedsRepair(*worker)})["action"])
	if action == "continue" {
		// Unfinished work is not a result; fall through to observation or
		// the restart disposition.
		return runner.TakeoverOutcome{}, false
	}
	attempt := turnNumberOf(turnDir)
	provider := ""
	if source, err := readJSONFile(filepath.Join(turnDir, "source.json")); err == nil {
		provider = textOf(getOf(source, "provider"))
	}
	if err := writeOutcome(c.cfg.Dir, outcomeRecord{
		TaskID:       inv.TaskID,
		Generation:   inv.Generation,
		InvocationID: inv.InvocationID,
		Attempt:      attempt,
		Action:       action,
		Worker:       worker,
		Provider:     provider,
		Job:          relJobPath(inv.TaskID, inv.InvocationID, attempt),
	}); err != nil {
		return runner.TakeoverOutcome{}, false
	}
	result, err := evidenceResult(composeEvidence{
		TaskID:     inv.TaskID,
		Generation: inv.Generation,
		Attempt:    attempt,
		Role:       stage.Role,
		Action:     action,
		Provider:   provider,
		Worker:     worker,
		Job:        relJobPath(inv.TaskID, inv.InvocationID, attempt),
	})
	if err != nil {
		return runner.TakeoverOutcome{}, false
	}
	return runner.TakeoverOutcome{Adopt: true, Result: result}, true
}

// turnNumberOf parses a turn directory's sequence.
func turnNumberOf(turnDir string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(turnDir), "turn-"))
	if err != nil {
		return 1
	}
	return n
}

// supervisorAlive applies §8.1's identity rule to the supervisor the process
// record names: the PID alone is nothing — pid reuse would let a stranger
// answer for the dead owner — so the live process's creation time must match
// the recorded started_at (a small tolerance covers scheduling delay between
// process creation and the record write).
func supervisorAlive(turnDir string) bool {
	value, err := readJSONFile(filepath.Join(turnDir, "process.json"))
	if err != nil {
		return false
	}
	pidNumber, ok := getOf(value, "pid").(json.Number)
	if !ok {
		return false
	}
	pid, err := pidNumber.Int64()
	if err != nil || pid <= 0 {
		return false
	}
	if !coordination.ProcessAlive(int(pid)) {
		return false
	}
	started, ok := getOf(value, "started_at").(json.Number)
	if !ok {
		return false
	}
	startedAt, err := started.Float64()
	if err != nil {
		return false
	}
	created, ok := processCreationTime(int(pid))
	if !ok {
		// Alive but unverifiable creation time: an integer alone never
		// authorizes observing.
		return false
	}
	delta := created.Unix() - int64(startedAt)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2
}
