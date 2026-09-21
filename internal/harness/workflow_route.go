package harness

// The transitions: everything that happens BETWEEN kernel runs. Each one is
// a pure condition over durable facts (outcome records, disposition notes,
// the kernel's question and request surfaces) plus one explicit action
// (ask, revise, request a repair round) — the flow's conditional edges and
// interrupt points. Transitions run in a fixed order
// and are idempotent: a crash between a kernel write and its ledger line
// costs one re-derivation of the same action, never a duplicate question or
// a second host call.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/runner"
)

// route applies the transition table once. It returns a stop when the run
// waits for a user input or has reached a terminal, and (false, nil) when
// the kernel may advance again.
func (c *Compose) route(ctx context.Context) (ComposeState, bool) {
	// 1. A refused answer anywhere is the run's ABORTED terminal: the
	// kernel reports a refused gate through ErrDecisionRefused, and a
	// refused request (the repair-limit ask) is read from the answer
	// ledger — both are the user's own durable choice.
	if state, stop := c.abortedIfRefused(); stop {
		return state, true
	}
	// 2. Open dispositions become questions, and answered ones move their
	// stage's retry forward.
	for _, task := range c.disposedTasks() {
		state, stop := c.routeDisposition(ctx, task)
		if stop {
			return state, true
		}
	}
	// 4. The plan approval, once answered, re-arms the plan task so the
	// chain continues (the gate itself is placed by the plan executor,
	// exactly when its review inputs stand).
	if state, stop := c.routePlanAdvance(ctx); stop {
		return state, true
	}
	// 5. The repair loop: evaluate's repair route drives one allowance-
	// bounded revision of development with the failed-verification brief.
	if state, stop := c.routeRepair(ctx); stop {
		return state, true
	}
	// 6. Check briefs follow the round and their own retry attempts.
	if state, stop := c.routeCheckBriefs(ctx); stop {
		return state, true
	}
	return ComposeState{}, false
}

// abortedIfRefused derives the ABORTED terminal from the answer ledger: any
// answered question whose choice was not its allowing one, or a refused
// repair-limit request, ended this run by the user's own decision.
func (c *Compose) abortedIfRefused() (ComposeState, bool) {
	for _, answer := range c.readAnswers() {
		allow, known := allowingChoice(answer.Task, answer.Options)
		if known {
			allow = answer.allowOr(allow)
		}
		if known && answer.Choice != allow && answer.Choice != answer.Alternate {
			current := c.readRecord()
			current.Terminal = statusAborted
			current.Stopped = fmt.Sprintf("user declined at a decision boundary (%s chose %q)", answer.Task, answer.Choice)
			_ = c.writeRecord(current)
			return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
		}
	}
	return ComposeState{}, false
}

// allowOr falls back to the static map when the entry carries no allow.
func (a answerEntry) allowOr(fallback string) string {
	if a.Allow != "" {
		return a.Allow
	}
	return fallback
}

// allowingChoice names the allowing choice of one question. The vocabulary
// is the composition's own: every question it places on a task has exactly
// one allowing choice, so the map is static — requirement and plan approve,
// every retry question allows retry, and the repair-allowance request on
// development allows continue.
func allowingChoice(task string, options []string) (string, bool) {
	switch {
	case offersRestartSession(options) && !strings.HasSuffix(task, askSuffix):
		// A kernel restart request: its own allowing choice is the session
		// replacement (the repair-limit request below offers continue).
		return "restart-session", true
	case task == taskRequirement, task == taskPlan, task == askTask(taskPlan):
		return "approve", true
	case task == taskDevelopment:
		// The repair-allowance request (a kernel user_request, not a gate).
		return "continue", true
	case strings.HasSuffix(task, askSuffix):
		return "retry", true
	default:
		return "", false
	}
}

// offersRestartSession names the restart requests' question shape.
func offersRestartSession(options []string) bool {
	for _, option := range options {
		if option == "restart-session" {
			return true
		}
	}
	return false
}

// disposedTasks lists the tasks with open disposition notes, in a stable
// order.
func (c *Compose) disposedTasks() []string {
	entries, err := os.ReadDir(filepath.Join(c.dir, "dispositions"))
	if err != nil {
		return nil
	}
	var tasks []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		tasks = append(tasks, strings.TrimSuffix(entry.Name(), ".json"))
	}
	if len(tasks) > 1 {
		for i := 1; i < len(tasks); i++ {
			for j := i; j > 0 && tasks[j] < tasks[j-1]; j-- {
				tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
			}
		}
	}
	return tasks
}

// routeDisposition walks one parked stage's disposition: no question yet —
// place it; answered retry — revise the stage (its retry brief, or the
// other stage's brief when the question retried a different stage, as the
// QA loop does); answered restart-session — drop the stage's session and
// revise.
func (c *Compose) routeDisposition(ctx context.Context, task string) (ComposeState, bool) {
	d := c.readDisposition(task)
	if d == nil {
		c.clearDisposition(task)
		return ComposeState{}, false
	}
	if d.Kind == "route-guard" {
		// Inert marker (see executeSeal): no question, no retry — the
		// repair route's revision supersedes it, and it clears once the
		// task moved past the generation it parked at.
		if c.taskGeneration(task) > d.Generation {
			c.clearDisposition(task)
		}
		return ComposeState{}, false
	}
	question := askTask(c.dispositionTarget(d, task))
	dispositionID := fmt.Sprintf("%s@%d:%d:%s", task, d.Generation, d.Attempt, d.Kind)
	pending := c.pendingQuestionOn(ctx, question)
	if pending {
		return ComposeState{}, false // the user is being asked
	}
	if !c.askRecorded(question, dispositionID) {
		if err := c.placeQuestion(ctx, question, dispositionID, d); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	}
	// The question was placed and answered (a refusal returned above).
	choice := c.answeredChoice(question, dispositionID)
	switch choice {
	case "":
		return ComposeState{}, false // answered but the ledger has not seen it yet
	case "abort":
		return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
	}
	// retry (or restart-session): move the stage forward.
	if choice == "restart-session" {
		c.clearSession(c.dispositionTarget(d, task))
	}
	if err := c.applyRetry(ctx, d, task); err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	c.clearDisposition(task)
	return ComposeState{}, false
}

// dispositionTarget names the stage a disposition's retry revises: the QA
// loop retries the DESIGN with the review's findings; a worker-blocked or
// runtime retry re-runs the stage itself.
func (c *Compose) dispositionTarget(d *disposition, task string) string {
	if d.Kind == "qa-design" {
		return taskQADesign
	}
	return task
}

// pendingQuestionOn reports whether a task holds a pending (unanswered)
// kernel question.
func (c *Compose) pendingQuestionOn(ctx context.Context, task string) bool {
	pending, err := c.store.PendingDecisions(ctx)
	if err != nil {
		return false
	}
	for _, question := range pending {
		if question.TaskID == task {
			return true
		}
	}
	return false
}

// placeQuestion revises the question task with the fresh question as its
// gate (the question task's only revisions are questions, so the kernel's
// gate-carry never resurrects an answered one), then records the marker.
// For a runtime disposition raised mid-run the executor may already have
// opened the question at the current generation through OpenQuestion; that
// path lands the same marker, so both spellings ask exactly once.
func (c *Compose) placeQuestion(ctx context.Context, questionTask, dispositionID string, d *disposition) error {
	body, err := CanonicalJSON(questionBody(d))
	if err != nil {
		return err
	}
	confirmation := &runner.Confirmation{
		Options: questionOptions(d), Allow: questionAllow(d),
		Revision: c.spec.revision, Body: body,
	}
	brief, err := c.initialBrief(questionTask)
	if err != nil {
		return err
	}
	spec := runner.TaskSpec{TaskID: questionTask, Input: brief, Confirm: confirmation}
	if err := c.store.ReviseTask(ctx, questionTask, spec); err != nil {
		return err
	}
	return appendLedger(filepath.Join(c.dir, "asks.jsonl"), askEntry{
		Task: questionTask, Disposition: dispositionID, Generation: 0,
	})
}

// questionBody is the question's displayed body: the kind, the reason and
// the evidence reference the user needs to decide.
func questionBody(d *disposition) map[string]any {
	body := map[string]any{
		"kind":   d.Kind,
		"reason": d.Reason,
		"task":   d.Task,
	}
	if d.Evidence != "" {
		body["evidence"] = d.Evidence
	}
	if len(d.Reasons) > 0 {
		body["reasons"] = d.Reasons
	}
	return body
}

// questionOptions/Allow: every retry question allows retry; a runtime
// question additionally offers restart-session.
func questionOptions(d *disposition) []string {
	if d.Kind == "runtime" {
		return []string{"retry", "restart-session", "abort"}
	}
	return []string{"retry", "abort"}
}

func questionAllow(d *disposition) string { return "retry" }

// answeredChoice reads the ledgered choice for the question covering one
// disposition (the latest answer on its question task).
func (c *Compose) answeredChoice(questionTask, dispositionID string) string {
	var choice string
	for _, answer := range c.readAnswers() {
		if answer.Task == questionTask {
			choice = answer.Choice
		}
	}
	return choice
}

// applyRetry revises the retried stage with the brief its retry needs —
// derived, at this moment, from the durable facts the disposition names.
func (c *Compose) applyRetry(ctx context.Context, d *disposition, task string) error {
	target := c.dispositionTarget(d, task)
	switch d.Kind {
	case "pre-review":
		brief, err := c.retryBrief(target, "prior-review-findings")
		if err != nil {
			return err
		}
		return c.revise(ctx, target, brief, "pre-review retry")
	case "qa-design":
		brief, err := c.qaDesignRetryBrief()
		if err != nil {
			return err
		}
		return c.revise(ctx, taskQADesign, brief, "qa design retry")
	case "development-findings":
		brief, err := c.retryBrief(taskDevelopment, "unresolved-findings")
		if err != nil {
			return err
		}
		return c.revise(ctx, taskDevelopment, brief, "development findings retry")
	case "worker-blocked", "runtime":
		// The blocked and runtime retries re-run the stage on its current
		// brief; a restart-session additionally dropped the session above.
		return c.revise(ctx, target, c.lastBrief(target), d.Kind+" retry")
	case "check-runtime":
		brief, err := c.checkRetryBrief(target)
		if err != nil {
			return err
		}
		return c.revise(ctx, target, brief, "check runtime retry")
	default:
		return fmt.Errorf("compose: unknown disposition kind %q", d.Kind)
	}
}

// retryBrief rebuilds a worker brief with the feedback wrapper of one
// purpose over the stage's latest outcome.
func (c *Compose) retryBrief(taskID, purpose string) ([]byte, error) {
	outcome := c.latestOutcome(taskID)
	if outcome == nil {
		return nil, fmt.Errorf("compose: retry of %s has no recorded outcome", taskID)
	}
	feedback, err := c.evidenceFeedback(outcome, purpose)
	if err != nil {
		return nil, err
	}
	role := c.roleOf(taskID)
	if outcome.Output != nil && outcome.Output.Role != "" {
		role = outcome.Output.Role
	}
	return CanonicalJSON(map[string]any{
		"stage": taskID, "role": role, "revision": c.spec.revision, "feedback": feedback,
	})
}

// qaDesignRetryBrief is the QA loop's design brief: the review's findings
// paired with the prior design, so the retried design answers both.
func (c *Compose) qaDesignRetryBrief() ([]byte, error) {
	review := c.latestOutcome(taskQAReview)
	design := c.latestOutcome(taskQADesign)
	if review == nil || design == nil {
		return nil, fmt.Errorf("compose: qa design retry is missing the review or design outcome")
	}
	reviewFeedback, err := c.evidenceFeedback(review, "qa-review-findings")
	if err != nil {
		return nil, err
	}
	designFeedback, err := c.evidenceFeedback(design, "prior-qa-design")
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(map[string]any{
		"stage": taskQADesign, "role": "qa-design", "revision": c.spec.revision,
		"feedback": map[string]any{"review": reviewFeedback, "prior_design": designFeedback},
	})
}

// checkRetryBrief bumps one check's attempt counter in its brief.
func (c *Compose) checkRetryBrief(taskID string) ([]byte, error) {
	brief := c.lastBrief(taskID)
	var current struct {
		Round   int `json:"round"`
		Attempt int `json:"attempt"`
	}
	if err := unmarshalBrief(brief, &current); err != nil {
		return nil, err
	}
	return CanonicalJSON(map[string]any{
		"stage": taskID, "revision": c.spec.revision,
		"round": current.Round, "attempt": current.Attempt + 1,
	})
}

// unmarshalBrief decodes a brief's JSON.
func unmarshalBrief(brief []byte, into any) error {
	return jsonUnmarshal(brief, into)
}

// isWaiting recognizes the kernel's durable-ask return.
func isWaiting(err error) bool { return err != nil && err == runner.ErrWaitingDecision }

// routeCheckBriefs keeps every check brief at its current round: a new
// repair round moves the round counter so a fresh attempt is counted from
// zero — attempts are per round.
func (c *Compose) routeCheckBriefs(ctx context.Context) (ComposeState, bool) {
	round := c.repairRounds()
	for _, check := range c.spec.spec.Checks {
		task := checkTaskID(check.ID)
		if c.dispositionStands(task) {
			continue
		}
		brief := c.lastBrief(task)
		var current struct {
			Round   int `json:"round"`
			Attempt int `json:"attempt"`
		}
		if unmarshalBrief(brief, &current) != nil {
			continue
		}
		if current.Round >= round {
			continue
		}
		next, err := CanonicalJSON(map[string]any{
			"stage": task, "revision": c.spec.revision, "round": round, "attempt": 0,
		})
		if err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		if err := c.revise(ctx, task, next, "new repair round"); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	}
	return ComposeState{}, false
}

// jsonUnmarshal is json.Unmarshal, spelled once.
func jsonUnmarshal(body []byte, into any) error { return json.Unmarshal(body, into) }

// ---- the plan gate and the repair loop --------------------------------------

// planQuestion names the plan approval's question task and its ledger
// marker (the ask is once per run: the plan is approved exactly one time).
func planQuestion() (question, dispositionID string) {
	return askTask(taskPlan), taskPlan + "@1:plan"
}

// sessionRefusalStands reports whether one task holds a session-refusal
// marker whose restart request has not been created yet.
func (c *Compose) sessionRefusalStands(taskID string) bool {
	_, err := os.Stat(filepath.Join(c.dir, "session-refused", pathSafe(taskID)+".json"))
	return err == nil
}

// clearSessionRefusal retires the marker (the restart ask owns the stage).
func (c *Compose) clearSessionRefusal(taskID string) {
	_ = os.Remove(filepath.Join(c.dir, "session-refused", pathSafe(taskID)+".json"))
}

// pendingRequestOn reports whether one task holds a pending — asked,
// unanswered — kernel request.
func (c *Compose) pendingRequestOn(ctx context.Context, taskID string) bool {
	requests, err := c.store.OpenRequests(ctx)
	if err != nil {
		return false
	}
	for _, request := range requests {
		if request.TaskID == taskID && request.State == "pending" {
			return true
		}
	}
	return false
}

// taskGeneration derives a task's current generation: its creation
// generation plus one per accepted revision.
func (c *Compose) taskGeneration(taskID string) int64 {
	var revisions int64
	for _, entry := range c.readRevisions() {
		if entry.Task == taskID {
			revisions++
		}
	}
	return 1 + revisions
}

// dispositionStands reports whether one task's disposition note still owns
// its stage — an open question or an answer not yet applied.
func (c *Compose) dispositionStands(task string) bool {
	return c.readDisposition(task) != nil
}

// briefBound reports whether the task's current brief is already exactly
// these bytes (a transition must not re-bind what it already bound).
func (c *Compose) briefBound(task string, brief []byte) bool {
	return bytesEqual(c.lastBrief(task), brief)
}

// routePlanAdvance continues the chain after the plan approval was
// answered: approve re-arms the plan task (its errored invocation WAS the
// interrupt — the executor asked through the kernel's OpenQuestion seam and
// returned without completing), abort is the run's ABORTED terminal. The
// gate itself is placed by the plan executor exactly when both reviews
// stand, so the question never precedes its materials (R3).
func (c *Compose) routePlanAdvance(ctx context.Context) (ComposeState, bool) {
	if c.latestOutcome(taskPlan) != nil {
		return ComposeState{}, false
	}
	question, dispositionID := planQuestion()
	if !c.askRecorded(question, dispositionID) {
		return ComposeState{}, false // not asked yet (or its inputs do not stand)
	}
	switch c.answeredChoice(question, dispositionID) {
	case "approve":
		for _, entry := range c.readRevisions() {
			if entry.Task == taskPlan && entry.Why == "plan approval" {
				return ComposeState{}, false // the re-arm already happened
			}
		}
		brief, err := c.initialBrief(taskPlan)
		if err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		if err := c.revise(ctx, taskPlan, brief, "plan approval"); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	case "abort":
		return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
	}
	return ComposeState{}, false // asked, not yet answered
}

// routeRepair drives the repair loop: evaluate's repair route, the
// allowance boundary (the accepted repair-allowance judgement through the
// kernel's request), and the repair brief derived — at this moment — from
// the persistent failed-verification bundle (R4: never a pre-staged cache).
func (c *Compose) routeRepair(ctx context.Context) (ComposeState, bool) {
	outcome := c.latestOutcome(taskEvaluate)
	if outcome == nil || outcome.Route != "repair" {
		return ComposeState{}, false
	}
	if c.dispositionStands(taskDevelopment) {
		return ComposeState{}, false
	}
	// A standing ask is the user's to answer: re-entering the kernel's
	// request while it is pending is refused, and a granted one is this
	// transition's own completion call on its next pass.
	if c.pendingRequestOn(ctx, taskDevelopment) {
		return ComposeState{}, false
	}
	// A session the host refused waits for its restart request (the
	// recovery consult creates it): revising past it here would re-run the
	// call under a fresh session without the user's answer.
	if c.sessionRefusalStands(taskDevelopment) {
		return ComposeState{}, false
	}
	brief, err := c.repairBrief()
	if err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	ask := &runner.Confirmation{
		Options: []string{"continue", "abort"}, Allow: "continue",
		Revision: c.spec.revision,
		Body:     []byte("Configured repair rounds reached; continue authorizes one more round. Feedback: " + outcome.Feedback),
	}
	policy := func(round, allowance int) bool {
		verdict := repairAllowance(repairAllowanceInput{
			Round: round, Allowance: &allowance, RepairLimit: c.spec.spec.RepairLimit,
		})
		return verdict["ask_user"] == true
	}
	err = c.store.RequestRevision(ctx, taskDevelopment,
		runner.TaskSpec{TaskID: taskDevelopment, Input: brief}, ask, policy, c.spec.spec.RepairLimit)
	switch {
	case err == nil:
		// Direct approval (or the granted completion): the revision is
		// bound, and the ledger carries its bytes.
		if err := appendLedger(filepath.Join(c.dir, "revisions.jsonl"), revisionEntry{
			Task: taskDevelopment, Brief: json.RawMessage(brief), Why: "repair round",
		}); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	case isWaiting(err):
		// The durable allowance ask: nothing is bound yet; the user's
		// continue re-enters this transition, which re-derives the same
		// brief and executes it as the grant's completion call.
		return ComposeState{}, false
	default:
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
}

// repairBrief derives the next repair round's brief from the durable
// failed-verification bundle: role repair, the failed-verification feedback
// wrapper, the development session carried by the record.
func (c *Compose) repairBrief() ([]byte, error) {
	ref := c.evaluateFeedbackRef()
	if ref == "" {
		return nil, fmt.Errorf("compose: repair route without a feedback bundle")
	}
	body, err := os.ReadFile(filepath.Join(c.dir, "artifacts", ref+".json"))
	if err != nil {
		return nil, err
	}
	_ = body
	wrapper, err := planEvidenceFeedback(evidenceFeedbackInput{
		Ref:      ref,
		Purpose:  "failed-verification",
		Revision: c.spec.revision,
		Evidence: body,
	})
	if err != nil {
		return nil, err
	}
	if ok, _ := wrapper["ok"].(bool); !ok {
		return nil, fmt.Errorf("compose: failed-verification feedback refused")
	}
	content, _ := wrapper["wrapper"].(map[string]any)
	return CanonicalJSON(map[string]any{
		"stage": taskDevelopment, "role": "repair", "revision": c.spec.revision,
		"feedback": content,
	})
}
