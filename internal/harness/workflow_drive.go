package harness

// The drive loop: Drive runs the kernel until it stops, applies the
// transitions between runs, and classifies each stop. While a Run is in flight it also watches
// for held native interactions; one is returned immediately and the run
// keeps advancing in the background.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"formal-gates/internal/runner"
)

// composeMaxIterations bounds one Drive call's run/route cycles. Every
// cycle either settles work or surfaces a user input, so the bound is a
// runaway guard, not a mechanism.
const composeMaxIterations = 96

// Drive advances the run until a user input is needed or a terminal
// judgment is reached. Transitions run before every kernel run, so an
// answered question moves its stage forward before any consultation could
// misread the parked call it left behind. A persisted terminal comes
// before all of that: a completed or aborted run is a non-advancing
// boundary — no route, revision or launch follows it, so reopening or
// driving again returns the terminal it already holds.
func (c *Compose) Drive(ctx context.Context) ComposeState {
	if terminal, _, err := c.store.Terminal(ctx); err == nil && terminal != "" {
		if terminal == statusAborted {
			return ComposeState{Status: StatusAborted, Terminal: terminal}
		}
		return ComposeState{Status: StatusComplete, Terminal: terminal}
	}
	for iteration := 0; iteration < composeMaxIterations; iteration++ {
		c.runMu.Lock()
		idle := c.runDone == nil
		c.runMu.Unlock()
		if idle {
			// Apply the transition table.
			if state, stop := c.route(ctx); stop {
				return state
			}
		}
		done := c.ensureRun(ctx)
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
			continue
		case <-ctx.Done():
			return ComposeState{Status: StatusFailed, Err: ctx.Err()}
		}
	}
	return ComposeState{Status: StatusFailed,
		Err: fmt.Errorf("compose: drive stopped after %d cycles without settling", composeMaxIterations)}
}

// ensureRun starts the kernel Run if none is advancing and returns its
// completion channel.
func (c *Compose) ensureRun(ctx context.Context) <-chan error {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.runDone != nil {
		return c.runDone
	}
	specs, err := c.specSet()
	done := make(chan error, 1)
	if err != nil {
		done <- err
		c.runDone = done
		return done
	}
	c.runDone = done
	go func() { done <- c.runner.Run(c.baseCtx, specs) }()
	return done
}

// finishRun clears the in-flight slot.
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

// classify maps one Run outcome to a Drive stop.
func (c *Compose) classify(ctx context.Context, err error) (ComposeState, bool) {
	switch {
	case err == nil:
		return c.terminalJudgment()
	case errors.Is(err, runner.ErrWaitingDecision):
		pending, perr := c.store.PendingDecisions(ctx)
		if perr != nil {
			return ComposeState{Status: StatusFailed, Err: perr}, true
		}
		// A premature gate wait must not mask the user's refusal.
		if state, stop := c.abortedIfRefused(ctx); stop {
			return state, true
		}
		// A standing park row whose transition has not run yet is work in
		// hand: place its question (or re-arm its continuation) before
		// stopping, so one waiting stop carries EVERY askable question —
		// a parked result's disposal is never hidden behind a different
		// pending ask.
		if c.unplacedDisposition(ctx) {
			return ComposeState{}, false
		}
		// A user question outranks an observation notice: the user can act
		// on the question while the observed call keeps settling.
		if len(pending) == 0 {
			if observations := c.liveObservations(); len(observations) > 0 {
				return ComposeState{Status: StatusObserved,
					Err: fmt.Errorf("compose: %s still in flight under observation", strings.Join(observations, ", "))}, true
			}
		}
		return ComposeState{Status: StatusWaitingDecision, Pending: pending}, true
	case errors.Is(err, runner.ErrDecisionRefused), errors.Is(err, runner.ErrCallAborted):
		// The kernel reports any non-allowing answer as refused; only the
		// composition's own refusal test decides which refusal actually ends
		// the run — restart-session continues (a replacement session, not
		// consent to stop), and its revision moves the stage again.
		if state, stop := c.abortedIfRefused(ctx); stop {
			return state, true
		}
		if c.progressed() {
			return ComposeState{}, false
		}
		// A sealed run is complete: a stale non-allowing answer row on a
		// retired question cannot unseal it.
		if state, stop := c.terminalJudgment(); stop {
			return state, true
		}
		_ = c.store.SetTerminal(ctx, statusAborted, "user declined at a decision boundary")
		return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
	case errors.Is(err, runner.ErrCallObserved), errors.Is(err, runner.ErrTakeoverRequired):
		// An observed call stops the drive only when it is genuinely in
		// flight. A parked observation — its stage's standing disposition
		// holds it, a question to place or a continuation to re-arm —
		// still has transitions to apply, so the loop advances and the
		// next run surfaces the question (or runs the re-armed work)
		// instead of reporting a wait nothing can move.
		if c.unplacedDisposition(ctx) {
			return ComposeState{}, false
		}
		return ComposeState{Status: StatusObserved}, true
	case errors.Is(err, runner.ErrRevisionGranted):
		// A repair round the user allowed but nobody completed yet: the
		// repair transition re-derives its brief and completes it.
		return ComposeState{}, false
	default:
		// Executor failures and anything else: the next transition cycle
		// settles the parked invocation (a question, a revision) or the
		// run is stuck and the failure is surfaced. A session refusal is
		// not surfaced: the next run's recovery consult turns it into the
		// kernel's restart request, which is the ask the user answers.
		if errors.Is(err, errSessionRefused) {
			return ComposeState{}, false
		}
		if c.progressed() {
			return ComposeState{}, false
		}
		if state, stop := c.abortedIfRefused(ctx); stop {
			return state, true
		}
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
}

// progressed compares a cheap signature of the observable state with the
// previous cycle's, so the drive loop cannot spin on an unchanging failure.
func (c *Compose) progressed() bool {
	parts := []string{}
	for _, task := range c.disposedTasks() {
		parts = append(parts, "d:"+task)
	}
	if pending, err := c.store.PendingDecisions(context.Background()); err == nil {
		for _, question := range pending {
			parts = append(parts, "q:"+question.DecisionID)
		}
	}
	if requests, err := c.store.OpenRequests(context.Background()); err == nil {
		for _, request := range requests {
			parts = append(parts, "r:"+request.RequestID+":"+request.State)
		}
	}
	for _, task := range c.allTasks() {
		if gens := c.outcomeGenerations(task); len(gens) > 0 {
			parts = append(parts, fmt.Sprintf("o:%s:%d", task, gens[len(gens)-1]))
		}
	}
	signature := strings.Join(parts, ";")
	c.mu.Lock()
	last := c.lastSignature
	c.lastSignature = signature
	c.mu.Unlock()
	return signature != last
}

// allTasks lists every task id of the run.
func (c *Compose) allTasks() []string {
	specs, err := c.creationSpecs()
	if err != nil {
		return nil
	}
	var tasks []string
	for _, spec := range specs {
		tasks = append(tasks, spec.TaskID)
	}
	return tasks
}

// execute is the kernel Executor: one invocation of one task, dispatched by
// the task's stage kind.
func (c *Compose) execute(ctx context.Context, inv runner.Invocation) (runner.Result, error) {
	switch {
	case inv.TaskID == taskRequirement:
		if err := c.writeOutcome(outcomeRecord{
			Task: taskRequirement, Generation: inv.Generation, Action: "approved",
		}); err != nil {
			return runner.Result{}, err
		}
		return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
	case inv.TaskID == taskPlan:
		return c.executePlanGate(ctx, inv)
	case inv.TaskID == taskCandidate:
		return c.executeCandidate(inv)
	case inv.TaskID == taskEvaluate:
		return c.executeEvaluate(inv)
	case inv.TaskID == taskSeal:
		return c.executeSeal(inv)
	case c.isQuestionTask(inv.TaskID):
		// A question task's execution IS its gate having been answered:
		// record the authorization and finish, unblocking the chain.
		if err := c.writeOutcome(outcomeRecord{
			Task: inv.TaskID, Generation: inv.Generation, Action: "answered",
		}); err != nil {
			return runner.Result{}, err
		}
		return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
	case strings.HasPrefix(inv.TaskID, "check:"):
		check, err := c.checkOf(inv.TaskID)
		if err != nil {
			return runner.Result{}, err
		}
		round, attempt := c.checkCounters(inv.TaskID)
		return c.executeCheck(ctx, inv, check, round, attempt)
	case strings.HasPrefix(inv.TaskID, "gate:"):
		return c.executeGate(ctx, inv, strings.TrimPrefix(inv.TaskID, "gate:"))
	default:
		var brief workerBrief
		if err := jsonUnmarshal(inv.Input, &brief); err != nil {
			return runner.Result{}, fmt.Errorf("compose: task %q has an unreadable brief: %w", inv.TaskID, err)
		}
		if brief.Role == "" {
			brief.Role = c.roleOf(inv.TaskID)
		}
		return c.executeWorker(ctx, inv.TaskID, inv, brief)
	}
}

// executePlanGate is the plan-approval stage: the task parks its
// approval question as a wait-state row exactly when it dispatches —
// which is exactly when its materials (both standing reviews) exist, so
// the question never precedes its evidence. The route asks the
// linked question; the approve answer resolves by re-arming this task,
// and the re-run records the approval outcome — the re-arm's
// idempotence fact is kernel-sourced: only the approval's resolve ever
// revises the plan task, so a persisted revision input row for it means
// the re-arm landed (with its bump, atomically).
func (c *Compose) executePlanGate(ctx context.Context, inv runner.Invocation) (runner.Result, error) {
	if c.latestOutcome(taskPlan) != nil {
		return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
	}
	// An unreadable park row reads as absent here (an unreadable fact
	// decides nothing): only a row that provably stands parks the
	// invocation.
	if _, stands, err := c.readDisposition(taskPlan); err == nil && stands {
		// The row stands: its question is the route's to place and the
		// user's to answer — this invocation stays unsettled until the
		// approve answer's resolve supersedes it.
		return runner.Result{}, fmt.Errorf("compose: the plan approval is pending: %w", errParked)
	}
	if c.taskRevised(taskPlan) {
		// The approval resolved and re-armed this task: record it.
		if err := c.writeOutcome(outcomeRecord{
			Task: taskPlan, Generation: inv.Generation, Action: "approved",
		}); err != nil {
			return runner.Result{}, err
		}
		return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
	}
	product := c.latestOutcome(taskProduct)
	technical := c.latestOutcome(taskTechnical)
	if product == nil || technical == nil || product.Worker == nil || technical.Worker == nil {
		return runner.Result{}, fmt.Errorf(
			"compose: the plan approval dispatched before its review inputs stand")
	}
	return runner.Result{}, c.park(taskPlan, inv.Generation, 0, runner.Disposition{
		Kind:   "plan",
		Reason: "Approve the reviewed requirement/plan and exact check commands before development",
	})
}

// outcomeFileRef addresses one outcome record as a path relative to the
// outcomes root (writeOutcome's layout), so a question body can point the
// user at a file that actually exists. The reference is built from the
// record's own task and generation, which binds it to exactly the review
// round it was taken from — a later round lands a different file and never
// rewrites this one.
func outcomeFileRef(outcome *outcomeRecord) string {
	return fmt.Sprintf("%s/%d", pathSafe(outcome.Task), outcome.Generation)
}

// checkOf finds one check task's authorized check.
func (c *Compose) checkOf(taskID string) (Check, error) {
	id := strings.TrimPrefix(taskID, "check:")
	for _, check := range c.spec.spec.Checks {
		if check.ID == id {
			return check, nil
		}
	}
	return Check{}, fmt.Errorf("compose: task %q names no authorized check", taskID)
}

// checkCounters reads a check brief's round and attempt.
func (c *Compose) checkCounters(taskID string) (round, attempt int) {
	var current struct {
		Round   int `json:"round"`
		Attempt int `json:"attempt"`
	}
	if err := jsonUnmarshal(c.lastBrief(taskID), &current); err != nil {
		return 0, 0
	}
	return current.Round, current.Attempt
}

// ---- user answers ----------------------------------------------------------

// Answer settles one question with the user's choice: the kernel's own
// Answer, the single write path for every question family (decision gates
// and user requests alike — it refuses unknown ids, off-option choices
// and stale bindings, and replays an already-settled one idempotently).
// A refusing choice derives the run's ABORTED terminal from the kernel's
// answered rows (abortedIfRefused), so no second record of the answer
// exists anywhere: what the kernel row says is the whole fact.
func (c *Compose) Answer(ctx context.Context, id, choice string) (runner.AnswerReceipt, error) {
	return c.store.Answer(ctx, id, choice)
}

// PendingDecisions lists the run's unanswered questions in the kernel's
// stable order.
func (c *Compose) PendingDecisions(ctx context.Context) ([]runner.DecisionView, error) {
	return c.store.PendingDecisions(ctx)
}

// ---- supervised jobs -------------------------------------------------------

// runSupervised executes one supervised job: the request names the command,
// the supervisor (the accepted component) claims the job directory, records
// its identity, spawns and reaps the child and writes exit.json. This
// process only waits — and on cancellation signals the supervisor's process
// group, the SIGTERM path the supervisor settles deterministically.
func (c *Compose) runSupervised(ctx context.Context, taskID, jobDir, kind string, argv []string, cwd string, environment map[string]string) (map[string]any, error) {
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return nil, err
	}
	request := map[string]any{
		"kind":        kind,
		"argv":        argvOf(argv),
		"cwd":         cwd,
		"stdin":       "",
		"environment": envOf(environment),
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "request.json"), request); err != nil {
		return nil, err
	}
	c.event("job-start", map[string]any{"operation": jobDir, "kind": kind})
	cmd := exec.Command(c.binary, "harness", "supervise", jobDir)
	cmd.SysProcAttr = supervisorSysProcAttr()
	cmd.Env = append(os.Environ(), "FORMAL_GATES_HARNESS_BINARY="+c.binary)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("compose: cannot start the supervisor for %s: %w", jobDir, err)
	}
	job := &liveJob{taskID: taskID, dir: jobDir, cmd: cmd, pgid: jobProcessGroup(cmd)}
	c.trackJob(job, true)
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		terminateJob(job)
		err = <-waitErr
	}
	c.trackJob(job, false)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("compose: supervisor for %s exited %d without a receipt", jobDir, ee.ExitCode())
		}
		return nil, fmt.Errorf("compose: supervisor for %s failed: %w", jobDir, err)
	}
	receiptValue, err := readJSONFile(filepath.Join(jobDir, "exit.json"))
	if err != nil {
		return nil, fmt.Errorf("compose: supervisor for %s wrote no exit.json: %w", jobDir, err)
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: exit.json of %s is not an object", jobDir)
	}
	return receipt, nil
}

// argvOf widens a string slice for the canonical request encoder.
func argvOf(argv []string) []any {
	out := make([]any, 0, len(argv))
	for _, arg := range argv {
		out = append(out, arg)
	}
	return out
}

// envOf widens an environment map for the canonical request encoder.
func envOf(environment map[string]string) map[string]any {
	out := map[string]any{}
	for key, value := range environment {
		out[key] = value
	}
	return out
}

// settleNativeReceipt is the receipt→admission gate: the supervisor's
// receipt must still describe the request it was launched with
// (VerifyProcess, the accepted evidence contract), and the framed business
// answer must be present. Anything else is no result.
func settleNativeReceipt(jobDir string, receipt map[string]any) (map[string]any, error) {
	requestValue, err := readJSONFile(filepath.Join(jobDir, "request.json"))
	if err != nil {
		return nil, err
	}
	request, ok := requestValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: request.json of %s is not an object", jobDir)
	}
	if _, refusal := VerifyProcess(jobDir, receipt, receipt, request, receiptProofs(receipt)); refusal != "" {
		return nil, fmt.Errorf("compose: %s: %s", jobDir, refusal)
	}
	if !valuesEqual(getOf(receipt, "exit_code"), 0) {
		detail := ""
		if body, err := readJSONFile(filepath.Join(jobDir, "native-error.json")); err == nil {
			detail = textOf(getOf(body, "error"))
		}
		if detail == "" {
			detail = "native job exited " + textOf(getOf(receipt, "exit_code"))
		}
		return nil, fmt.Errorf("compose: %s: %s", jobDir, detail)
	}
	answerValue, err := readJSONFile(filepath.Join(jobDir, "answer.json"))
	if err != nil {
		return nil, fmt.Errorf("compose: %s produced no framed answer: %w", jobDir, err)
	}
	answer, ok := answerValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: answer.json of %s is not an object", jobDir)
	}
	return answer, nil
}

// trackJob registers/unregisters a live supervised call for cancellation
// and held-interaction watching.
func (c *Compose) trackJob(job *liveJob, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.live[job.dir] = job
	} else {
		delete(c.live, job.dir)
	}
}

// snapshotLive returns the currently in-flight supervised calls, ordered by
// job directory.
func (c *Compose) snapshotLive() []*liveJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	jobs := make([]*liveJob, 0, len(c.live))
	for _, job := range c.live {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].dir < jobs[j].dir })
	return jobs
}

// liveJob is one supervised call this process started and has not yet
// reaped: the supervisor command (for cancellation) and its process group.
type liveJob struct {
	taskID string
	dir    string
	cmd    *exec.Cmd
	pgid   int
}

// heldNotice is one held native interaction the watcher surfaced.
type heldNotice struct {
	taskID    string
	jobDir    string
	requestID string
}

// watchedDirs is the watcher's scan set: the live jobs of this process plus
// the job directories of calls a takeover consultation kept under
// observation — a recovered owner watches the predecessor's call too. The
// observed entries are pinned to the invocation the consultation judged:
// the watcher follows that invocation's own newest turn, never a
// "highest turn" guess across a task's every call (turn numbers order
// turns within one invocation only, so an old call's turn-02 is not newer
// than the current call's turn-01).
func (c *Compose) watchedDirs() map[string]string {
	dirs := map[string]string{}
	for _, job := range c.snapshotLive() {
		dirs[job.dir] = job.taskID
	}
	c.mu.Lock()
	for task, invocation := range c.observed {
		// observed is keyed by the logical TaskID; the job tree's directory
		// is its pathSafe form (see the executors' jobDir), so the watcher
		// must cross at the same point or a recovered "gate:x" call's held
		// interactions are never found.
		if turn := latestTurnOf(filepath.Join(c.dir, "jobs", pathSafe(task), invocation)); turn != "" {
			dirs[turn] = task
		}
	}
	c.mu.Unlock()
	return dirs
}

// watchHeld routes held native interactions: while a supervised call runs,
// any client-directed request its host holds for the user is surfaced once,
// until it is answered through native-respond and the original turn
// continues.
func (c *Compose) watchHeld() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.baseCtx.Done():
			return
		case <-ticker.C:
		}
		for jobDir, taskID := range c.watchedDirs() {
			records, err := readNativePendings(jobDir)
			if err != nil {
				continue
			}
			for _, raw := range records {
				record := objectOrEmpty(raw)
				if textOf(getOf(record, "status")) != "pending" {
					continue
				}
				requestID := textOf(getOf(record, "request_id"))
				key := jobDir + "\x00" + requestID
				c.mu.Lock()
				seen := c.announced[key]
				c.announced[key] = true
				c.mu.Unlock()
				if seen {
					continue
				}
				select {
				case c.heldCh <- heldNotice{taskID: taskID, jobDir: jobDir, requestID: requestID}:
				case <-c.baseCtx.Done():
					return
				}
			}
		}
	}
}

// heldInteraction builds the user-facing record for one surfaced notice,
// re-checking the request is still pending.
func (c *Compose) heldInteraction(notice heldNotice) (HeldInteraction, bool) {
	records, err := readNativePendings(notice.jobDir)
	if err != nil {
		return HeldInteraction{}, false
	}
	for _, raw := range records {
		record := objectOrEmpty(raw)
		if textOf(getOf(record, "status")) != "pending" ||
			!valuesEqual(getOf(record, "request_id"), notice.requestID) {
			continue
		}
		return HeldInteraction{
			TaskID:    notice.taskID,
			JobDir:    notice.jobDir,
			RequestID: notice.requestID,
			Method:    textOf(getOf(record, "method")),
			Summary:   textOf(getOf(record, "summary")),
			Options:   getOf(record, "options"),
		}, true
	}
	return HeldInteraction{}, false
}

// latestTurnOf picks one invocation directory's highest turn.
func latestTurnOf(base string) string {
	turns, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	best, bestN := "", -1
	for _, turn := range turns {
		if !turn.IsDir() || !strings.HasPrefix(turn.Name(), "turn-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(turn.Name(), "turn-"))
		if err != nil {
			continue
		}
		if n > bestN {
			best, bestN = filepath.Join(base, turn.Name()), n
		}
	}
	return best
}

// latestTurnDir picks the highest turn directory (numerically) under a
// task's job tree, searching every invocation. Turn numbers order turns
// within ONE invocation only, so this cross-invocation maximum is NOT a
// "current call" selector — the recovery paths select the invocation
// first (latestTurnOf); this listing is kept for diagnostics that want
// the whole task's highest-numbered turn.
func latestTurnDir(base string) string {
	invocations, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	best, bestN := "", -1
	for _, invocation := range invocations {
		if !invocation.IsDir() {
			continue
		}
		turns, err := os.ReadDir(filepath.Join(base, invocation.Name()))
		if err != nil {
			continue
		}
		for _, turn := range turns {
			if !turn.IsDir() || !strings.HasPrefix(turn.Name(), "turn-") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(turn.Name(), "turn-"))
			if err != nil {
				continue
			}
			if n > bestN {
				best, bestN = filepath.Join(base, invocation.Name(), turn.Name()), n
			}
		}
	}
	return best
}

// ---- cancellation ----------------------------------------------------------

// Cancel stops the run and settles its in-flight supervised calls:
// dispatching stops first (the base context cancels the kernel's Run), then
// every supervisor of this run still without a receipt — including a
// predecessor's, found by the recorded process identity — is signalled on
// its process group, and only once every receipt has landed is the
// settlement reported. Until then, and for any job that never settles,
// cancellation is not claimed to be complete.
func (c *Compose) Cancel(ctx context.Context) ([]JobSettlement, error) {
	c.baseCancel()
	var jobs []*liveJob
	for _, job := range c.snapshotLive() {
		jobs = append(jobs, job)
	}
	// A predecessor's live supervisors: recorded pid and creation time in
	// the job tree, still without a receipt.
	for _, job := range recordedLiveJobs(c.dir) {
		if _, tracked := c.lookupLive(job.dir); !tracked {
			jobs = append(jobs, job)
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var settled []JobSettlement
	var unsettled []string
	for _, job := range jobs {
		terminateJob(job)
	}
	for _, job := range jobs {
		if receipt := waitForReceipt(job.dir, deadline); receipt != nil {
			settled = append(settled, JobSettlement{
				TaskID:   job.taskID,
				JobDir:   job.dir,
				ExitCode: intOf(getOf(receipt, "exit_code")),
				Error:    textOf(getOf(receipt, "error")),
			})
			continue
		}
		unsettled = append(unsettled, job.dir)
	}
	c.waitRunDone(30 * time.Second)
	if len(unsettled) > 0 {
		return settled, fmt.Errorf("compose: cancellation incomplete; no settled receipt for %s",
			strings.Join(unsettled, ", "))
	}
	return settled, nil
}

// lookupLive reports whether this process tracks one job directory.
func (c *Compose) lookupLive(dir string) (*liveJob, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job, ok := c.live[dir]
	return job, ok
}

// recordedLiveJobs scans the run's job tree for recorded supervisors that
// are still alive under their recorded identity and have written no
// receipt yet — the cancellation set of a recovered owner.
func recordedLiveJobs(dir string) []*liveJob {
	var jobs []*liveJob
	root := filepath.Join(dir, "jobs")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || !strings.HasPrefix(entry.Name(), "turn-") {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "exit.json")); err == nil {
			return nil // settled
		}
		job := recordedJobOf(path)
		if job != nil {
			jobs = append(jobs, job)
		}
		return nil
	})
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].dir < jobs[j].dir })
	return jobs
}

// recordedJobOf reads one job directory's recorded process identity and
// verifies it against the live process table (a bare pid proves nothing).
func recordedJobOf(jobDir string) *liveJob {
	value, err := readJSONFile(filepath.Join(jobDir, "process.json"))
	if err != nil {
		return nil
	}
	pid := intOf(getOf(value, "pid"))
	if pid <= 0 {
		return nil
	}
	started := floatOf(getOf(value, "started_at"))
	if started == 0 {
		return nil
	}
	created, ok := processCreationTime(pid)
	if !ok {
		return nil // unverifiable identity: never signal on it
	}
	if delta := created.Unix() - int64(started); delta < -2 || delta > 2 {
		return nil // a recycled pid: not our supervisor
	}
	task := taskOfJobDir(jobDir)
	return &liveJob{taskID: task, dir: jobDir, pgid: pid}
}

// taskOfJobDir derives the owning task from a job directory path.
func taskOfJobDir(jobDir string) string {
	rel, err := filepath.Rel(filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(jobDir)))), jobDir)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
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

// floatOf renders a decoded JSON number as a float (0 when it is not one).
func floatOf(value any) float64 {
	if number, ok := value.(json.Number); ok {
		if f, err := number.Float64(); err == nil {
			return f
		}
	}
	return 0
}
