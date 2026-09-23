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
	// 2. The wait-state rows: each parked stage's question is placed, and
	// each answered one drives its resolve — the plan approval (kind
	// "plan") lives here with every other park kind.
	for _, task := range c.disposedTasks() {
		state, stop := c.routeDisposition(ctx, task)
		if stop {
			return state, true
		}
	}
	// 3. The repair loop: evaluate's repair route drives one allowance-
	// bounded revision of development with the failed-verification brief.
	if state, stop := c.routeRepair(ctx); stop {
		return state, true
	}
	// 4. Check briefs follow the round and their own retry attempts.
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
			// A write failure is tolerated: the answered question still
			// stands, so the next route pass re-derives the same terminal.
			_ = c.store.SetTerminal(ctx, statusAborted, reason)
			return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
		}
	}
	return ComposeState{}, false
}

// disposedTasks lists the tasks with standing park rows, in the kernel's
// stable task-id order. The row is the whole fact: its task_id IS the
// logical TaskID, so nothing is guessed back from a storage spelling. An
// unreadable list reads empty — an unreadable fact decides nothing, and the
// transitions that must read a row surface its failure.
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

// readDisposition loads a task's standing park row from the kernel: ok is
// false when none stands, err when the read failed. The two are distinct
// facts and every caller states its own handling of each.
func (c *Compose) readDisposition(taskID string) (runner.Disposition, bool, error) {
	return c.store.Disposition(context.Background(), taskID)
}

// clearDisposition removes a task's park row (its question's answer has
// been applied; the row has served its purpose). A removal that fails is
// tolerated and dropped: the transitions are idempotent, so the row left
// standing costs one re-derivation of the same clear on the next pass.
func (c *Compose) clearDisposition(taskID string) {
	_ = c.store.ClearDisposition(context.Background(), taskID)
}

// routeDisposition walks one parked stage's wait-state row: a
// continuation re-arms its stage without a question (unfinished work is
// continued, never asked about); every other row lives the park→ask→
// answer→resolve cycle off the row's own link — no question yet, the
// transition asks; asked and unanswered, the user is deciding; answered,
// the answer drives its revision (the retry brief, or the other stage's
// brief when the question retried a different stage, as the QA loop
// does; a restart-session additionally drops the stage's session).
func (c *Compose) routeDisposition(ctx context.Context, task string) (ComposeState, bool) {
	d, stands, readErr := c.readDisposition(task)
	if readErr != nil {
		// The row is this transition's necessary fact — every branch below
		// (the ask, the continuation re-arm, the answer's resolve) is derived
		// from it — so its read failure is this layer's failure stop, never
		// a flattened "no row" the later transitions would route around.
		return ComposeState{Status: StatusFailed, Err: readErr}, true
	}
	if !stands {
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
		// the stage on the SAME next-turn input the live loop builds — the
		// unfinished-work feedback over the parked CONTINUE result (its
		// recorded outcome), under the recorded session identity — and no
		// question is asked. The resolve lands the re-arm revision and the
		// row's removal in one kernel transaction; the generation guard
		// keeps a crash between them from double-arming (the row parks the
		// generation the re-arm moved past).
		if c.taskGeneration(task) <= d.Generation {
			if err := c.store.ResolveDisposition(ctx, task, task, runner.TaskSpec{
				TaskID: task, Input: c.continueReArmBrief(task, d.Generation),
			}); err != nil {
				return ComposeState{Status: StatusFailed, Err: err}, true
			}
		} else {
			c.clearDisposition(task)
		}
		return ComposeState{}, false
	}
	target := c.dispositionTarget(d, task)
	question, asked := c.standingQuestion(ctx, task)
	if !asked {
		if err := c.askDisposition(ctx, task, target, d); err != nil {
			return ComposeState{Status: StatusFailed, Err: err}, true
		}
		return ComposeState{}, false
	}
	if !question.Answered {
		return ComposeState{}, false // the user is being asked
	}
	switch question.Choice {
	case "abort":
		return ComposeState{Status: StatusAborted, Terminal: statusAborted}, true
	case "restart-session":
		c.clearSession(target)
	}
	if err := c.applyRetry(ctx, d, task); err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	return ComposeState{}, false
}

// standingQuestion reads one parked task's linked question from the
// kernel: found=false when the park has not been asked yet. An unreadable
// question reads the same — the ask path re-derives the question from the
// standing row, so the tolerance costs one re-derivation, never a skipped
// decision.
func (c *Compose) standingQuestion(ctx context.Context, task string) (runner.StandingQuestion, bool) {
	question, found, err := c.store.DispositionQuestion(ctx, task)
	if err != nil || !found {
		return runner.StandingQuestion{}, false
	}
	return question, true
}

// askDisposition places one parked stage's question: one kernel Ask
// that creates the pending decisions row on the stage's ask task and
// links it to the standing row, so the ask record and the park are one
// fact. The plan kind's body carries the review evidence and the exact
// approved check commands; every other kind shows its park's own
// kind, reason and evidence.
func (c *Compose) askDisposition(ctx context.Context, task, target string, d runner.Disposition) error {
	body, err := canonicalBody(questionBody(d))
	if err != nil {
		return err
	}
	if d.Kind == "plan" {
		body, err = c.planQuestionBody()
		if err != nil {
			return err
		}
	}
	_, err = c.store.Ask(ctx, task, askTask(target), &runner.Confirmation{
		Options: questionOptions(d), Allow: questionAllow(d),
		Revision: c.spec.revision, Body: body,
	})
	return err
}

// dispositionTarget names the stage a disposition's retry revises: the QA
// loop retries the DESIGN with the review's findings; a worker-blocked or
// runtime retry re-runs the stage itself.
func (c *Compose) dispositionTarget(d runner.Disposition, task string) string {
	if d.Kind == "qa-design" {
		return taskQADesign
	}
	return task
}

// pendingQuestionOn reports whether a task holds a pending (unanswered)
// kernel question. An unreadable decisions list reports no pending
// question — the same tolerance every kernel read here carries: an
// unreadable fact decides nothing, and the transitions that must read the
// question surface its failure.
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

// questionBody is the question's displayed body: the kind, the reason
// and the evidence reference the user needs to decide. The body is
// display material only — the ask record is the row's structural link.
func questionBody(d runner.Disposition) map[string]any {
	body := map[string]any{
		"kind":   d.Kind,
		"reason": d.Reason,
		"task":   d.TaskID,
	}
	if d.Evidence != "" {
		body["evidence"] = d.Evidence
	}
	return body
}

// planQuestionBody is the plan approval's displayed body: both standing
// reviews as readable references, the outcomes root that holds them and
// the exact approved check commands — the user approves concrete
// materials, never a summary.
func (c *Compose) planQuestionBody() ([]byte, error) {
	product := c.latestOutcome(taskProduct)
	technical := c.latestOutcome(taskTechnical)
	if product == nil || technical == nil || product.Worker == nil || technical.Worker == nil {
		return nil, fmt.Errorf(
			"compose: the plan approval was asked before its review inputs stand")
	}
	return canonicalBody(map[string]any{
		"product":   outcomeFileRef(product),
		"technical": outcomeFileRef(technical),
		"artifacts": filepath.Join(c.dir, "outcomes"),
		"checks":    c.spec.spec.Checks,
		"reason":    "Approve the reviewed requirement/plan and exact check commands before development",
	})
}

// questionOptions/Allow: every retry question allows retry; a runtime
// question additionally offers restart-session; the plan approval
// offers approve/abort and allows approve.
func questionOptions(d runner.Disposition) []string {
	switch d.Kind {
	case "plan":
		return []string{"approve", "abort"}
	case "runtime":
		return []string{"retry", "restart-session", "abort"}
	default:
		return []string{"retry", "abort"}
	}
}

func questionAllow(d runner.Disposition) string {
	if d.Kind == "plan" {
		return "approve"
	}
	return "retry"
}

// applyRetry resolves the parked stage with the brief its transition
// needs — derived, at this moment, from the durable facts the
// disposition names. One kernel ResolveDisposition lands the revision
// (with its invalidation closure and the parked invocation's
// supersession), the question lineage's retirement and the row's
// removal in a single transaction: the answer and the transition it
// drove cannot part ways. Two retry kinds, two session rules: a
// run-condition retry (blocked, runtime, findings, continuation)
// re-runs the SAME work and its call resumes the recorded identity; a
// business review FAIL starts an independent new round, so the retried
// reviewer's recorded identity is dropped and the round opens a fresh
// session — the two are never conflated into one treatment.
func (c *Compose) applyRetry(ctx context.Context, d runner.Disposition, task string) error {
	target := c.dispositionTarget(d, task)
	// resolveBrief lands one retry's resolve: fail on the brief builder's
	// error, else hand its bytes to the parked task's resolve — so each
	// case below reads as its own rule.
	resolveBrief := func(parked, targetTask string, brief []byte, err error) error {
		if err != nil {
			return err
		}
		return c.store.ResolveDisposition(ctx, parked, targetTask, runner.TaskSpec{
			TaskID: targetTask, Input: brief,
		})
	}
	switch d.Kind {
	case "pre-review":
		// A new review round: the failed result's session does not carry
		// into it (the reference starts each round's invoke_worker fresh).
		c.clearSession(target)
		brief, err := c.retryBrief(target, "prior-review-findings")
		return resolveBrief(task, target, brief, err)
	case "qa-design":
		// A new design-and-review round: both stages' recorded identities
		// are spent with the round they judged.
		c.clearSession(taskQADesign)
		c.clearSession(taskQAReview)
		brief, err := c.qaDesignRetryBrief()
		return resolveBrief(task, taskQADesign, brief, err)
	case "development-findings":
		brief, err := c.retryBrief(taskDevelopment, "unresolved-findings")
		return resolveBrief(task, taskDevelopment, brief, err)
	case "worker-blocked", "runtime":
		// The blocked and runtime retries re-run the stage on its current
		// brief; a restart-session additionally dropped the session above.
		return resolveBrief(task, target, c.lastBrief(target), nil)
	case "check-runtime":
		brief, err := c.checkRetryBrief(target)
		return resolveBrief(task, target, brief, err)
	case "plan":
		// The approved plan re-arms the plan task on its creation brief;
		// the re-run records the approval outcome.
		brief, err := c.initialBrief(taskPlan)
		return resolveBrief(task, taskPlan, brief, err)
	default:
		return fmt.Errorf("compose: unknown disposition kind %q", d.Kind)
	}
}

// continueReArmBrief is the re-armed stage's brief after a parked
// CONTINUE: the same next-turn input the live loop would run — the
// continuation feedback (continuationFeedback) over the parked result's
// recorded outcome — on the brief the parked generation ran with. The
// bytes persist with the revision, so a crash after the re-arm replays
// exactly this input.
func (c *Compose) continueReArmBrief(task string, generation int64) []byte {
	briefBytes := c.lastBrief(task)
	var brief workerBrief
	if jsonUnmarshal(briefBytes, &brief) != nil || brief.Role == "" {
		return briefBytes
	}
	outcome := c.readOutcome(task, generation)
	if outcome == nil || outcome.Output == nil {
		return briefBytes // no recorded CONTINUE fact: re-arm the bound brief unchanged
	}
	body, err := CanonicalJSON(map[string]any{
		"stage": brief.Stage, "role": brief.Role, "revision": brief.Revision,
		"feedback": continuationFeedback(task, generation, brief, outcome.Output, c.spec.revision),
	})
	if err != nil {
		return briefBytes
	}
	return body
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
	if err := jsonUnmarshal(brief, &current); err != nil {
		return nil, err
	}
	return CanonicalJSON(map[string]any{
		"stage": taskID, "revision": c.spec.revision,
		"round": current.Round, "attempt": current.Attempt + 1,
	})
}

// isWaiting recognizes the kernel's durable-ask return.
func isWaiting(err error) bool { return err != nil && err == runner.ErrWaitingDecision }

// advanceCheckRounds brings every check brief to the current business
// round: a new repair round moves the round counter so a fresh attempt
// is counted from zero — attempts are per round. It runs at the moment
// the round lands (routeRepair), inside the active flow, so the briefs
// never lag behind the round the rest of the run already committed to.
func (c *Compose) advanceCheckRounds(ctx context.Context) error {
	round := c.repairRounds()
	for _, check := range c.spec.spec.Checks {
		task := checkTaskID(check.ID)
		stands, err := c.dispositionStands(task)
		if err != nil {
			return err
		}
		if stands {
			continue
		}
		var current struct {
			Round   int `json:"round"`
			Attempt int `json:"attempt"`
		}
		if jsonUnmarshal(c.lastBrief(task), &current) != nil {
			continue
		}
		if current.Round >= round {
			continue
		}
		next, err := CanonicalJSON(map[string]any{
			"stage": task, "revision": c.spec.revision, "round": round, "attempt": 0,
		})
		if err != nil {
			return err
		}
		if err := c.revise(ctx, task, next); err != nil {
			return err
		}
	}
	return nil
}

// routeCheckBriefs keeps every check brief at its current round — the
// route's own pass over the same rule advanceCheckRounds applies when the
// round lands, a no-op re-check while the two stay in step.
func (c *Compose) routeCheckBriefs(ctx context.Context) (ComposeState, bool) {
	if err := c.advanceCheckRounds(ctx); err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	}
	return ComposeState{}, false
}

// jsonUnmarshal is json.Unmarshal, spelled once.
func jsonUnmarshal(body []byte, into any) error { return json.Unmarshal(body, into) }

// ---- the repair loop ---------------------------------------------------------

// sessionRefusalStands reports whether one task holds a session-refusal
// marker whose restart request has not been created yet.
func (c *Compose) sessionRefusalStands(taskID string) bool {
	_, err := os.Stat(filepath.Join(c.dir, "session-refused", pathSafe(taskID)+".json"))
	return err == nil
}

// clearSessionRefusal retires the marker (the restart ask owns the stage).
// A removal that fails is tolerated: the marker's reader only stats the
// file, so a marker left behind holds the same guard one more pass.
func (c *Compose) clearSessionRefusal(taskID string) {
	_ = os.Remove(filepath.Join(c.dir, "session-refused", pathSafe(taskID)+".json"))
}

// pendingRequestOn reports whether one task holds a pending — asked,
// unanswered — kernel request. An unreadable requests list reports none:
// the kernel's own revision-pending refusal (RequestRevision) stops a
// second request on the task, so the tolerance never grants a second
// round.
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
// invalidation closures alike, which a revision-input count would miss. An
// unreadable generation reads zero, so the guards comparing it against the
// parked generation take their not-moved branch (resolve or keep the row)
// and no clear is derived from a guess.
func (c *Compose) taskGeneration(taskID string) int64 {
	generation, err := c.store.TaskGeneration(context.Background(), taskID)
	if err != nil {
		return 0
	}
	return generation
}

// dispositionStands reports whether one task's disposition note still owns
// its stage — an open question or an answer not yet applied. The row is the
// fact every caller's decision turns on (skipping a check round, holding
// the repair entry, keeping a recovered call parked), so an unreadable row
// is an error the caller maps to its own failure, never "not standing".
func (c *Compose) dispositionStands(task string) (bool, error) {
	_, stands, err := c.readDisposition(task)
	if err != nil {
		return false, err
	}
	return stands, nil
}

// unplacedDisposition reports whether any standing park row still has
// transition work: a continuation not yet re-armed, a park not yet
// asked, or an answer whose resolve has not landed. The drive loop
// keeps advancing while one does, so a single waiting stop carries
// every askable question (a parked result's disposal and a pending
// restart ask surface together instead of one hiding the other). Inert
// rows (route-guard) are no work. Each row is a necessary fact of the
// report — an unreadable one is an error the caller maps to its own
// failure, never a skipped row the stop would silently drop.
func (c *Compose) unplacedDisposition(ctx context.Context) (bool, error) {
	for _, task := range c.disposedTasks() {
		d, stands, err := c.readDisposition(task)
		if err != nil {
			return false, err
		}
		if !stands || d.Kind == "route-guard" {
			continue
		}
		if d.Kind == "continue" {
			return true, nil
		}
		question, asked := c.standingQuestion(ctx, task)
		if !asked || question.Answered {
			return true, nil
		}
	}
	return false, nil
}

// routeRepair drives the repair loop: evaluate's repair route, the
// allowance boundary (the accepted repair-allowance judgement through the
// kernel's request), and the repair brief derived — at this moment — from
// the persistent failed-verification bundle (never a pre-staged cache).
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
	if stands, err := c.dispositionStands(taskDevelopment); err != nil {
		return ComposeState{Status: StatusFailed, Err: err}, true
	} else if stands {
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
		return repairAllowance(repairAllowanceInput{
			Round: round, Allowance: &allowance, RepairLimit: c.spec.spec.RepairLimit,
		}).AskUser
	}
	err = c.store.RequestRevision(ctx, taskDevelopment,
		runner.TaskSpec{TaskID: taskDevelopment, Input: brief}, ask, policy, c.spec.spec.RepairLimit)
	switch {
	case err == nil:
		// Direct approval (or the granted completion): the revision and
		// its input bytes are bound in the kernel's transaction. The new
		// round's synchronous effects land here, in the active flow: the
		// gates re-review a new candidate — an independent review round
		// opens fresh sessions — and every check brief moves to the round
		// it will run in, so nothing is left for a later pass to catch up
		// across a terminal.
		for _, gate := range c.spec.spec.Gates {
			c.clearSession(gateTaskID(gate))
		}
		if err := c.advanceCheckRounds(ctx); err != nil {
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
