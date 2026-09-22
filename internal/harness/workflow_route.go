package harness

// The transitions: everything that happens BETWEEN kernel runs. Each one is
// a pure condition over durable facts (outcome records, the kernel's park
// rows, question and request surfaces) plus one explicit action
// (ask, revise, request a repair round) — the flow's conditional edges and
// interrupt points. Transitions run in a fixed order
// and are idempotent: every fact they read and write lives in the kernel
// alone, so a crash after an action costs one re-derivation of the same
// action, never a duplicate question or a second host call.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/runner"
)

// route applies the transition table once. It returns a stop when the run
// waits for a user input or has reached a terminal, and (false, nil) when
// the kernel may advance again.
func (c *Compose) route(ctx context.Context) (ComposeState, bool) {
	// 1. A refused answer anywhere is the run's ABORTED terminal: the
	// kernel reports a refused gate through ErrDecisionRefused, and a
	// refused request (the repair-limit ask) is read from the kernel's
	// answered rows — both are the user's own durable choice.
	if state, stop := c.abortedIfRefused(ctx); stop {
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
	// 3. The plan approval, once answered, re-arms the plan task so the
	// chain continues (the gate itself is placed by the plan executor,
	// exactly when its review inputs stand).
	if state, stop := c.routePlanAdvance(ctx); stop {
		return state, true
	}
	// 4. The repair loop: evaluate's repair route drives one allowance-
	// bounded revision of development with the failed-verification brief.
	if state, stop := c.routeRepair(ctx); stop {
		return state, true
	}
	// 5. Check briefs follow the round and their own retry attempts.
	if state, stop := c.routeCheckBriefs(ctx); stop {
		return state, true
	}
	return ComposeState{}, false
}

// abortedIfRefused derives the ABORTED terminal from the kernel's settled
// questions (Store.AnsweredQuestions): any answered gate, or refused
// request, whose choice was neither its allowing one nor the run-wide
// continuation choice ended this run by the user's own decision. The
// allow column on the kernel row IS the allowing choice — the kernel
// froze it from the same vocabulary this composition declared the
// question with — and "restart-session" is a continuation wherever it is
// offered (a valid answer is always an offered option, so an answer of
// restart-session proves it was offered): a replacement session, never
// consent to stop.
func (c *Compose) abortedIfRefused(ctx context.Context) (ComposeState, bool) {
	answered, err := c.store.AnsweredQuestions(ctx)
	if err != nil {
		return ComposeState{}, false // unreadable facts decide nothing yet; the failure surfaces through the transitions that must read them
	}
	for _, answer := range answered {
		if answer.Choice != answer.Allow && answer.Choice != "restart-session" {
			reason := fmt.Sprintf("user declined at a decision boundary (%s chose %q)", answer.TaskID, answer.Choice)
			_ = c.store.SetTerminal(ctx, statusAborted, reason)
			return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
		}
	}
	return ComposeState{}, false
}

// disposedTasks lists the tasks with standing park rows, in the kernel's
// stable task-id order. The row is the whole fact: its task_id IS the
// logical TaskID, so nothing is guessed back from a storage spelling.
func (c *Compose) disposedTasks() []string {
	rows, err := c.store.Dispositions(context.Background())
	if err != nil {
		return nil
	}
	var tasks []string
	for _, row := range rows {
		tasks = append(tasks, row.TaskID)
	}
	return tasks
}

// readDisposition loads a task's standing park row from the kernel, or
// nil when none stands (or the read fails — an unreadable fact decides
// nothing, the tolerance every kernel read here carries).
func (c *Compose) readDisposition(taskID string) *runner.Disposition {
	row, ok, err := c.store.Disposition(context.Background(), taskID)
	if err != nil || !ok {
		return nil
	}
	return &row
}

// clearDisposition removes a task's park row (its question's answer has
// been applied; the row has served its purpose).
func (c *Compose) clearDisposition(taskID string) {
	_ = c.store.ClearDisposition(context.Background(), taskID)
}

// dispositionIDOf names one parked disposition's identity — the marker the
// question body carries, so "was THIS disposition asked" is read from the
// kernel's own question row and a re-parked round (a later generation or
// attempt of the same stage and kind) asks afresh instead of replaying
// the previous round's answer. Its inputs are exactly the park row's
// columns, stamped in one place (park).
func dispositionIDOf(task string, d *runner.Disposition) string {
	return fmt.Sprintf("%s@%d:%d:%s", task, d.Generation, d.Attempt, d.Kind)
}

// routeDisposition walks one parked stage's disposition row: a
// continuation re-arms its stage without a question (unfinished work is
// continued, never asked about); every other row becomes a question —
// no question yet, place it; answered retry — revise the stage (its
// retry brief, or the other stage's brief when the question retried a
// different stage, as the QA loop does); answered restart-session — drop
// the stage's session and revise.
func (c *Compose) routeDisposition(ctx context.Context, task string) (ComposeState, bool) {
	d := c.readDisposition(task)
	if d == nil {
		return ComposeState{}, false // no row stands: the kernel row is the whole fact, nothing to clean up
	}
	if d.Kind == "route-guard" {
		// Inert row (see executeSeal): no question, no retry — the
		// repair route's revision supersedes it, and it clears once the
		// task moved past the generation it parked at.
		if c.taskGeneration(task) > d.Generation {
			c.clearDisposition(task)
		}
		return ComposeState{}, false
	}
	if d.Kind == "continue" {
		// The live loop would simply start the next turn: recovery re-arms
		// the stage on its current brief — the remembered session identity
		// carries the work under the SAME session — and no question is
		// asked. The row is spent once the task moved past the parked
		// generation (a crash between the revision and this clear re-reads
		// the moved generation and only cleans up).
		if c.taskGeneration(task) <= d.Generation {
			if err := c.revise(ctx, task, c.lastBrief(task)); err != nil {
				return ComposeState{Status: StatusFailed, Err: err}, true
			}
		}
		c.clearDisposition(task)
		return ComposeState{}, false
	}
	question := askTask(c.dispositionTarget(d, task))
	dispositionID := dispositionIDOf(task, d)
	pending := c.pendingQuestionOn(ctx, question)
	if pending {
		return ComposeState{}, false // the user is being asked
	}
	if !c.askRecorded(ctx, question, dispositionID) {
		if err := c.placeQuestion(ctx, question, dispositionID, d); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	}
	// The question was placed and answered (a refusal returned above).
	choice := c.answeredChoice(ctx, question)
	switch choice {
	case "":
		return ComposeState{}, false // not settled on the kernel row yet
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
func (c *Compose) dispositionTarget(d *runner.Disposition, task string) string {
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
// gate-carry never resurrects an answered one). The question body carries
// the disposition id, which — with the kernel row's generation binding —
// IS the ask-once marker: a reopened owner reads it back off the row
// itself. A runtime disposition the recovery consult already asked
// through OpenQuestion carries the same id in its body, so both
// spellings ask exactly once; a later disposition on the same ask task
// is a new revision with a fresh decision id, never a replay of the
// answered row.
func (c *Compose) placeQuestion(ctx context.Context, questionTask, dispositionID string, d *runner.Disposition) error {
	// canonicalBody is the encoding boundary for values that may carry typed
	// Go fields: the body must cross as plain decoded values.
	body, err := canonicalBody(questionBody(d, dispositionID))
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
	return c.store.ReviseTask(ctx, questionTask, spec)
}

// questionBody is the question's displayed body: the kind, the reason and
// the evidence reference the user needs to decide, plus the disposition id
// — the binding the ask-once read (askRecorded) matches against the
// kernel row's own frozen body.
func questionBody(d *runner.Disposition, dispositionID string) map[string]any {
	body := map[string]any{
		"kind":        d.Kind,
		"reason":      d.Reason,
		"task":        d.TaskID,
		"disposition": dispositionID,
	}
	if d.Evidence != "" {
		body["evidence"] = d.Evidence
	}
	return body
}

// questionOptions/Allow: every retry question allows retry; a runtime
// question additionally offers restart-session.
func questionOptions(d *runner.Disposition) []string {
	if d.Kind == "runtime" {
		return []string{"retry", "restart-session", "abort"}
	}
	return []string{"retry", "abort"}
}

func questionAllow(d *runner.Disposition) string { return "retry" }

// answeredChoice reads the settled choice of one question task's standing
// kernel question (the decisions row at its current generation). Empty
// means not answered — or no row standing — so callers gate it behind
// askRecorded and treat empty as "not yet".
func (c *Compose) answeredChoice(ctx context.Context, questionTask string) string {
	question, ok, err := c.store.TaskQuestion(ctx, questionTask)
	if err != nil || !ok || !question.Answered {
		return ""
	}
	return question.Choice
}

// askRecorded reports whether one disposition's question stands on the
// kernel: the question task's current-generation decisions row carrying
// this disposition's id in its body. The row IS the ask record — created
// pending by the placement (placeQuestion's revision or OpenQuestion) and
// answered in place — so a reopened owner re-derives "asked" from the
// same row the answer will land on, and a different disposition on the
// same task asks afresh.
func (c *Compose) askRecorded(ctx context.Context, questionTask, disposition string) bool {
	question, ok, err := c.store.TaskQuestion(ctx, questionTask)
	if err != nil || !ok {
		return false
	}
	var body struct {
		Disposition string `json:"disposition"`
	}
	if json.Unmarshal([]byte(question.Body), &body) != nil {
		return false
	}
	return body.Disposition == disposition
}

// applyRetry revises the retried stage with the brief its retry needs —
// derived, at this moment, from the durable facts the disposition names.
func (c *Compose) applyRetry(ctx context.Context, d *runner.Disposition, task string) error {
	target := c.dispositionTarget(d, task)
	switch d.Kind {
	case "pre-review":
		brief, err := c.retryBrief(target, "prior-review-findings")
		if err != nil {
			return err
		}
		return c.revise(ctx, target, brief)
	case "qa-design":
		brief, err := c.qaDesignRetryBrief()
		if err != nil {
			return err
		}
		return c.revise(ctx, taskQADesign, brief)
	case "development-findings":
		brief, err := c.retryBrief(taskDevelopment, "unresolved-findings")
		if err != nil {
			return err
		}
		return c.revise(ctx, taskDevelopment, brief)
	case "worker-blocked", "runtime":
		// The blocked and runtime retries re-run the stage on its current
		// brief; a restart-session additionally dropped the session above.
		return c.revise(ctx, target, c.lastBrief(target))
	case "check-runtime":
		brief, err := c.checkRetryBrief(target)
		if err != nil {
			return err
		}
		return c.revise(ctx, target, brief)
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
		if err := c.revise(ctx, task, next); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	}
	return ComposeState{}, false
}

// jsonUnmarshal is json.Unmarshal, spelled once.
func jsonUnmarshal(body []byte, into any) error { return json.Unmarshal(body, into) }

// ---- the plan gate and the repair loop --------------------------------------

// planQuestion names the plan approval's question task and its body
// binding (the ask is once per run: the plan is approved exactly one
// time).
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

// taskGeneration reads the task's current generation from the kernel —
// authoritative for bumps from its own revisions AND from upstream
// invalidation closures alike, which a revision-input count would miss.
func (c *Compose) taskGeneration(taskID string) int64 {
	generation, err := c.store.TaskGeneration(context.Background(), taskID)
	if err != nil {
		return 0
	}
	return generation
}

// dispositionStands reports whether one task's disposition note still owns
// its stage — an open question or an answer not yet applied.
func (c *Compose) dispositionStands(task string) bool {
	return c.readDisposition(task) != nil
}

// unplacedDisposition reports whether any standing park row still has
// transition work: a continuation not yet re-armed, or a question not yet
// placed on its ask task. The drive loop keeps advancing while one does,
// so a single waiting stop carries every askable question (a parked
// result's disposal and a pending restart ask surface together instead of
// one hiding the other). Inert rows (route-guard) are no work.
func (c *Compose) unplacedDisposition(ctx context.Context) bool {
	for _, task := range c.disposedTasks() {
		d := c.readDisposition(task)
		if d == nil || d.Kind == "route-guard" {
			continue
		}
		if d.Kind == "continue" {
			return true
		}
		if !c.pendingQuestionOn(ctx, askTask(c.dispositionTarget(d, task))) {
			return true
		}
	}
	return false
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
	if !c.askRecorded(ctx, question, dispositionID) {
		return ComposeState{}, false // not asked yet (or its inputs do not stand)
	}
	switch c.answeredChoice(ctx, question) {
	case "approve":
		// The re-arm's idempotence fact is kernel-sourced: only the re-arm
		// ever revises the plan task itself, so a persisted revision input
		// row for it means the re-arm landed (with its bump, atomically).
		if c.taskRevised(taskPlan) {
			return ComposeState{}, false // the re-arm already happened
		}
		brief, err := c.initialBrief(taskPlan)
		if err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		if err := c.revise(ctx, taskPlan, brief); err != nil {
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
	// One verdict drives at most its own round, by the kernel's own
	// generation fact: latestOutcome reads the kernel-current generation,
	// and this round's revision bumps evaluate with the whole dependent
	// closure the moment it lands — a spent verdict therefore has no
	// outcome at the moved generation (latestOutcome is nil), and the
	// next verdict only exists after the chain re-verified. While the
	// round's ask or repair is still in flight, the guards below hold the
	// entry.
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
		// Direct approval (or the granted completion): the revision and
		// its input bytes are bound in the kernel's transaction.
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
