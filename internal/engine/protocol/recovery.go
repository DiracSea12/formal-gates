package protocol

import (
	"fmt"
	"slices"
	"sort"
	"strconv"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// RecoveryAction 是 engine 对已知失败或中断事实作出的封闭处置。它是
// 纯协议决定，不代表宿主已经执行了对应动作；真正的宿主动作仍须经过
// HostAction intent/receipt 协议。
type RecoveryAction string

const (
	RecoveryResumeAttempt RecoveryAction = "RESUME_ATTEMPT"
	RecoveryNewAttempt    RecoveryAction = "NEW_ATTEMPT"
	RecoveryAsk           RecoveryAction = "ASK"
	RecoveryReconcile     RecoveryAction = "RECONCILE"
	RecoveryWait          RecoveryAction = "WAIT"
	RecoveryFail          RecoveryAction = "FAIL"
	RecoveryOperator      RecoveryAction = "OPERATOR"
	RecoveryAgent         RecoveryAction = "AGENT"
	RecoveryAttachReceipt RecoveryAction = "ATTACH_RECEIPT"
)

// FailureRoute 是 failure-class 到 engine 处置边的机械映射。特别地，
// engine/adapter 故障只落在 engine 的恢复边，不会隐式变成 AGENT。
type FailureRoute struct {
	Class  authoring.FailureClass
	Action RecoveryAction
}

// RouteFailure 返回封闭 failure-class 的唯一基础路由。
func RouteFailure(class authoring.FailureClass) FailureRoute {
	switch class {
	case authoring.FailureTransientEngine:
		return FailureRoute{Class: class, Action: RecoveryResumeAttempt}
	case authoring.FailureBusinessReject:
		return FailureRoute{Class: class, Action: RecoveryFail}
	case authoring.FailureUserActionRequired:
		return FailureRoute{Class: class, Action: RecoveryAsk}
	case authoring.FailureSideEffectUnknown:
		return FailureRoute{Class: class, Action: RecoveryReconcile}
	case authoring.FailureInvariantViolation, authoring.FailureBlockedBug:
		return FailureRoute{Class: class, Action: RecoveryFail}
	case authoring.FailureAgentRecoverable:
		return FailureRoute{Class: class, Action: RecoveryAgent}
	default:
		return FailureRoute{Class: class, Action: RecoveryOperator}
	}
}

// Interruption 描述一次没有可靠终态的中断。CauseKnown=false 且绑定不变
// 时，engine 无法安全猜测恢复语义，必须产生 Ask。
type Interruption struct {
	InterruptionReason string
	Class              authoring.FailureClass
	CauseKnown         bool
	ReceiptUnknown     bool
	LifecycleMatches   int
}

// RecoveryPlan carries the original recovery request identity. ASK exposes it
// for user decisions; automatic recovery uses it for its native host effect.
// LifecycleMatches makes unknown receipt reconciliation observable.
type RecoveryPlan struct {
	Class            authoring.FailureClass
	Action           RecoveryAction
	RequestID        string
	Options          []AskOption
	LifecycleMatches int
	Detail           string
}

// DecideRecovery 按草案 §9.1 的优先级计算恢复处置：receipt UNKNOWN 先
// 查 lifecycle；唯一匹配才自动 attach；其余进入 Operator。对于普通中断，
// 只有明确的客观瞬态且 bindings 未变才自动 resume；未知原因进入 Ask。
func DecideRecovery(interruption Interruption) RecoveryPlan {
	return decideRecovery(interruption, false)
}

// decideRecovery receives bindingsChanged only from Engine after comparing
// the current durable Attempt bindings with the current write transaction.
// Callers cannot select the recovery branch with a trusted boolean.
func decideRecovery(interruption Interruption, bindingsChanged bool) RecoveryPlan {
	if interruption.ReceiptUnknown {
		if interruption.LifecycleMatches == 1 {
			return RecoveryPlan{
				Class:            authoring.FailureSideEffectUnknown,
				Action:           RecoveryAttachReceipt,
				LifecycleMatches: 1,
				Detail:           "exactly one lifecycle identity matches the unknown receipt",
			}
		}
		return RecoveryPlan{
			Class:            authoring.FailureSideEffectUnknown,
			Action:           RecoveryOperator,
			LifecycleMatches: interruption.LifecycleMatches,
			Detail:           "unknown receipt has zero or multiple lifecycle matches; operator reconciliation required",
		}
	}

	base := RouteFailure(interruption.Class)
	if interruption.Class == "" {
		if bindingsChanged || interruption.CauseKnown {
			return RecoveryPlan{Action: RecoveryNewAttempt, Detail: "bindings changed or the interruption cause is known"}
		}
		return RecoveryPlan{
			Action:  RecoveryAsk,
			Options: recoveryAskOptions(),
			Detail:  "bindings are unchanged but the interruption cause is unknown",
		}
	}
	if base.Action == RecoveryResumeAttempt && (bindingsChanged || interruption.CauseKnown) {
		return RecoveryPlan{Class: interruption.Class, Action: RecoveryNewAttempt, Detail: "transient failure cannot reuse changed or known-invalid bindings"}
	}
	return RecoveryPlan{Class: interruption.Class, Action: base.Action, Detail: base.ActionDetail()}
}

func (r FailureRoute) ActionDetail() string {
	if r.Action == RecoveryAgent {
		return "only explicit AGENT_RECOVERABLE_SEMANTIC_ERROR permits the agent edge"
	}
	if r.Class == authoring.FailureInvariantViolation || r.Class == authoring.FailureBlockedBug {
		return fmt.Sprintf("failure class %s is terminal; run diagnose and rebuild the definition before another execution", r.Class)
	}
	return fmt.Sprintf("failure class %s routes to %s", r.Class, r.Action)
}

func recoveryAskOptions() []AskOption {
	return []AskOption{
		{ID: "resume", Label: "resume"},
		{ID: "fresh", Label: "fresh"},
		{ID: "abort", Label: "abort"},
	}
}

// lifecycleMatches derives candidate identities exclusively from the durable
// lifecycle buffer. One correlation may legitimately have multiple paired
// identities; preserving that cardinality is what makes the Operator branch
// observable instead of collapsing it into a caller-provided string set.
func (s *State) lifecycleMatches(provider, correlation string) []string {
	if correlation == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, record := range s.LifecycleEvents {
		if record.Provider == provider && record.Correlation == correlation && s.lifecyclePaired(provider, correlation, record.Identity) {
			seen[record.Identity] = true
		}
	}
	matches := make([]string, 0, len(seen))
	for identity := range seen {
		matches = append(matches, identity)
	}
	sort.Strings(matches)
	return matches
}

func (s *State) appendRecovery(record RecoveryRecord) {
	s.RecoveryRecords = append(s.RecoveryRecords, record)
}

// recordObsoleteAction preserves the issued execution until its stop is observed.
func (s *State) recordObsoleteAction(pending PendingAction, attempt Attempt, replacedBy, reason string) {
	obsolete := s.ObsoleteActions[pending.ActionID]
	obsolete.ActionID, obsolete.Task = pending.ActionID, pending.Task
	if pending.AttemptID != "" {
		obsolete.AttemptID = pending.AttemptID
	}
	if attempt.ActionID == pending.ActionID {
		obsolete.AttemptID, obsolete.Bindings, obsolete.Plan = attempt.ID, attempt.Bindings, attempt.Plan
	}
	if replacedBy != "" {
		obsolete.ReplacedBy = replacedBy
	}
	if obsolete.Reason == "" {
		obsolete.Reason = reason
	}
	s.ObsoleteActions[pending.ActionID] = obsolete
}

// retireAction removes an execution from current acceptance indexes while
// retaining its issued identity and received evidence.
func (s *State) retireAction(actionID, replacedBy, reason string) (PendingAction, bool) {
	pending, attempt, ok := s.workerActionTarget(actionID)
	if !ok {
		if _, obsolete := s.ObsoleteActions[actionID]; obsolete {
			return PendingAction{}, false
		}
		receipt, recorded := s.SpawnReceipts[actionID]
		if !recorded {
			return PendingAction{}, false
		}
		pending = PendingAction{ActionID: actionID, Task: receipt.Task, Step: string(receipt.Task.Step)}
		if ended, retained := s.EndedAttempts[receipt.Task]; retained && ended.ActionID == actionID {
			attempt = ended
		}
	}
	if pending.AttemptID == "" && attempt.ActionID == actionID {
		pending.AttemptID = attempt.ID
	}
	delete(s.PendingActions, actionID)
	delete(s.Attempts, pending.Task)
	if result, staged := s.StagedResults[actionID]; staged {
		s.archiveObsoleteResult(result)
		delete(s.StagedResults, actionID)
	}
	s.recordObsoleteAction(pending, attempt, replacedBy, reason)
	if ended, awaiting := s.EndedAttempts[pending.Task]; awaiting && ended.ActionID == actionID && ended.AwaitingResult {
		delete(s.EndedAttempts, pending.Task)
	}
	return pending, true
}

// installReplacement installs a new Attempt for the same logical task. The
// task status remains the status of the current task projection; recovery
// replaces the execution instance, so it must not manufacture an illegal
// TERMINAL -> ISSUED runtime transition.
func (s *State) installReplacement(pending PendingAction, nextRevision uint64, bindings AttemptBindings, plan PlanIdentity, attempts, maxAttempts int) (Attempt, error) {
	if pending.Task == (runtime.TaskKey{}) {
		return Attempt{}, fmt.Errorf("protocol: replacement task is empty")
	}
	if _, exists := s.Attempts[pending.Task]; exists {
		return Attempt{}, &RejectedError{Code: CodeDuplicateAction, Detail: fmt.Sprintf("task %s already has a current attempt", pending.Task.String())}
	}
	if !s.expectedContains(pending.Task) {
		s.Expected = append(s.Expected, pending.Task)
	}
	actionID := "retry:" + pending.ActionID + ":" + strconv.FormatUint(nextRevision, 10)
	attempt := Attempt{
		IssuedAction: decision.IssuedAction{ActionID: actionID, Task: pending.Task, Step: authoring.StepID(pending.Step)},
		ID:           "att:" + pending.Task.String() + ":" + strconv.FormatUint(nextRevision, 10),
		Bindings:     bindings,
		Plan:         plan,
		Attempts:     attempts,
		MaxAttempts:  maxAttempts,
	}
	// 与 issueInto 同源的签发代数落账：重签发即新派发，floor 比对以它
	// 为准（终态清理不删）。
	s.DispatchIssueRevisions[actionID] = nextRevision
	s.Attempts[pending.Task] = attempt
	s.PendingActions[actionID] = PendingAction{
		ActionID: actionID, Task: pending.Task, Step: pending.Step, AttemptID: attempt.ID,
		// 系列身份是步骤的签发时事实，重签发随步继承。
		ReviewSeries: pending.ReviewSeries,
	}
	// 换新继承承载被换新旧派发的原授权槽：旧派发已退位不在途，其槽即干净
	// 的背书者身份；多槽时不抢别的干净槽。旧派发未绑定（容量自洽的签出不
	// 绑槽）时换新保持未绑定——换新不过闸，放行承诺随任务代际延续。
	if pending.ReviewSeries != "" {
		s.ReviewSeries = review.BindReplacementDispatch(s.ReviewSeries, s.RunID, pending.ReviewSeries, pending.ActionID, actionID)
	}
	return attempt, nil
}

// RecoverAttempt applies the durable part of a recovery decision for a
// current Attempt. Resume/reconcile/operator/agent decisions retain the old
// action until its declared protocol step. Fresh execution reconciles the old
// session before replacement; ASK persists resume/fresh/abort.
func (e *Engine) RecoverAttempt(actionID string, interruption Interruption, input Observation) (RecoveryPlan, uint64, error) {
	e = e.observing(input)
	revision, state, err := e.load()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	if state == nil {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// 屏障闸（FA）：恢复决定是权威写入——重试派发、terminalize 与恢复
	// Ask 的创建在未决需求变化期间冻结（解除后照常）。
	if state.abortRequested() {
		return RecoveryPlan{}, revision, validationError("abort does not permit a replacement attempt")
	}
	if err := state.errWriteBarrier("attempt recovery"); err != nil {
		return RecoveryPlan{}, 0, err
	}
	pending, ok := state.PendingActions[actionID]
	if !ok {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("recovery references unknown action %q", actionID)}
	}
	attempt, ok := state.Attempts[pending.Task]
	if !ok || attempt.ID != pending.AttemptID {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeStaleAttempt, Detail: fmt.Sprintf("recovery action %q has no matching current Attempt", actionID)}
	}
	fingerprint, err := e.sourceFacts.Digest()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	currentBindings := attempt.Bindings
	currentBindings.Task, currentBindings.Snapshot = pending.Task, fingerprint
	plan := decideRecovery(interruption, !attempt.Bindings.Equal(currentBindings))
	if pending, exists := state.pendingAttemptRecovery(actionID, attempt.ID); exists {
		return pending, revision, nil
	}
	nextRevision := revision + 1
	plan.RequestID = "recover:" + actionID + ":" + strconv.FormatUint(nextRevision, 10)
	record := RecoveryRecord{ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID, Class: plan.Class, Action: plan.Action, RequestID: plan.RequestID, Detail: plan.Detail, InterruptionReason: interruption.InterruptionReason, LifecycleMatches: plan.LifecycleMatches, Revision: nextRevision}
	if plan.Action == RecoveryAsk {
		state.registerRecoveryAsk(&record, nextRevision)
		plan.RequestID, plan.Options = record.RequestID, recoveryAskOptions()
	}
	if plan.Action == RecoveryFail {
		if err := state.terminalizeTask(pending.Task); err != nil {
			return RecoveryPlan{}, 0, err
		}
	}
	state.appendRecovery(record)
	commitRevision, _, err := e.commitEffects(state, revision, input, nil)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}

func (s *State) pendingAttemptRecovery(actionID, attemptID string) (RecoveryPlan, bool) {
	for _, record := range s.RecoveryRecords {
		if record.ActionID != actionID || record.AttemptID != attemptID || record.RequestID == "" {
			continue
		}
		if record.Action == RecoveryAsk && !s.PendingAsks[record.RequestID].Resolved {
			return RecoveryPlan{Action: RecoveryAsk, RequestID: record.RequestID, Options: recoveryAskOptions(), Detail: record.Detail}, true
		}
		completed := false
		for _, effect := range s.ReconciledEffects {
			if effect.Intent != nil && effect.Intent.Correlation == record.RequestID && (effect.Status == "WITHDRAWN" || effect.Status == "FULFILLED") {
				completed = true
			}
		}
		for _, receipt := range s.HostActionReceipts {
			if receipt.Correlation == record.RequestID && receipt.Status == HostActionStatusExecuted {
				completed = true
			}
		}
		if !completed && (record.Action == RecoveryAsk || record.Action == RecoveryResumeAttempt || record.Action == RecoveryNewAttempt) {
			return RecoveryPlan{Action: RecoveryWait, RequestID: record.RequestID, Detail: "original recovery awaits its native effect receipt"}, true
		}
	}
	return RecoveryPlan{}, false
}

// registerRecoveryAsk binds the current execution to its existing recovery
// request. An unfinished native recovery effect settles before another request.
func (s *State) registerRecoveryAsk(record *RecoveryRecord, revision uint64) bool {
	if pending, exists := s.pendingAttemptRecovery(record.ActionID, record.AttemptID); exists {
		if pending.Action != RecoveryAsk {
			return false
		}
		record.RequestID = pending.RequestID
		return true
	}
	if record.RequestID == "" {
		record.RequestID = "recover:" + record.ActionID + ":" + strconv.FormatUint(revision, 10)
	}
	s.PendingAsks[record.RequestID] = PendingAsk{RequestID: record.RequestID, Control: ControlRecovery, Options: recoveryAskOptions()}
	return true
}

// ReconcileUnknownReceipt resolves a SpawnReceipt whose status is UNKNOWN.
// Candidate lifecycle identities are derived from the persisted lifecycle
// buffer. Exactly one match allows an automatic attach; zero or multiple
// matches become an Operator route and never trigger a blind respawn.
func (e *Engine) ReconcileUnknownReceipt(actionID string, input Observation) (RecoveryPlan, uint64, error) {
	e = e.observing(input)
	revision, state, err := e.load()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	if state == nil {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	receipt, ok := state.SpawnReceipts[actionID]
	if !ok {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("receipt %q does not exist", actionID)}
	}
	if receipt.Status == SpawnStatusAttached {
		return RecoveryPlan{
			Class:            authoring.FailureSideEffectUnknown,
			Action:           RecoveryAttachReceipt,
			LifecycleMatches: 1,
			Detail:           "unknown receipt was already attached to lifecycle identity " + receipt.LifecycleIdentity,
		}, revision, nil
	}
	if receipt.Status != SpawnStatusUnknown {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("receipt %q is not UNKNOWN", actionID)}
	}
	plan := e.reconcileUnknownReceipt(state, actionID, revision+1)

	commitRevision, _, err := e.commitEffects(state, revision, input, nil)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}

// reconcileUnknownReceipt derives attachment from durable lifecycle facts.
// Attachment is an observation; result acceptance remains in the transaction epilogue.
func (e *Engine) reconcileUnknownReceipt(state *State, actionID string, nextRevision uint64) RecoveryPlan {
	receipt := state.SpawnReceipts[actionID]
	matches := state.lifecycleMatches(receipt.Provider, receipt.Correlation)
	plan := DecideRecovery(Interruption{ReceiptUnknown: true, LifecycleMatches: len(matches)})
	if len(matches) == 1 {
		receipt.Status = SpawnStatusAttached
		receipt.LifecycleIdentity = matches[0]
		state.SpawnReceipts[actionID] = receipt
		plan.Detail = "unknown receipt attached to unique lifecycle identity " + matches[0]
		// 唯一匹配即身份可判定：同一事务内以解析出的真实身份建立 lifecycle
		// 绑定（claim 以观察到的身份解析，不信任回执的追踪键——与 SPAWNED
		// 回执同一回调）。claim 失败只降级计量来源（UNAVAILABLE）留痕，不否
		// 决挂接；零/多匹配不 claim（身份不可判定即不可计量）。
		if e.cfg.ClaimDispatch != nil {
			if claimErr := e.cfg.ClaimDispatch(state.RunID, actionID, matches[0], receipt.Provider); claimErr != nil {
				state.appendRecovery(RecoveryRecord{
					ActionID: actionID,
					Detail:   fmt.Sprintf("lifecycle dispatch claim failed; the dispatch cost source degrades to unavailable: %v", claimErr),
					Revision: nextRevision,
				})
			}
		}
		// 挂接是观察事实照常落账；暂存结果的配对接纳属事务收尾单点
		//（commitEffects）——屏障在位时收尾不配对，解除事务扫清。
		if state.requirementChangePending() {
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: actionID,
				Detail:   "unknown receipt attached while the requirement change barrier holds; the staged result stays suspended until the barrier lifts",
				Revision: nextRevision,
			})
		}
	} else {
		plan.Detail = fmt.Sprintf("unknown receipt has %d lifecycle matches; operator reconciliation required", len(matches))
	}
	state.appendRecovery(RecoveryRecord{ActionID: actionID, Class: authoring.FailureSideEffectUnknown, Action: plan.Action, Detail: plan.Detail, LifecycleMatches: len(matches), Revision: nextRevision})
	return plan
}

// attachObservedReceipts settles uniquely observed dispatches without replaying spawn.
func (e *Engine) attachObservedReceipts(state *State, nextRevision uint64) bool {
	ids := []string{}
	for id, receipt := range state.SpawnReceipts {
		if receipt.Status == SpawnStatusUnknown && state.liveDispatch(id) && len(state.lifecycleMatches(receipt.Provider, receipt.Correlation)) == 1 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		e.reconcileUnknownReceipt(state, id, nextRevision)
	}
	return len(ids) > 0
}

// ReconcileHostAction settles an UNKNOWN local side effect from observed
// external facts. fulfilled=true commits only the reconciliation result and
// never executes the side effect again; conflict routes to Operator; an
// unfulfilled but non-conflicting observation remains WAIT/pending.
type HostActionObservation struct {
	Promotion *PromotionObservation `json:"promotion,omitempty"`
	Resource  *ResourceObservation  `json:"resource,omitempty"`
}

func (o HostActionObservation) Digest() string {
	if o.Promotion != nil && o.Resource == nil {
		return o.Promotion.Digest()
	}
	return digestOfCanonical(o)
}

func (e *Engine) ReconcileHostAction(actionID, observationDigest string, fulfilled, conflict bool, input Observation, observations ...HostActionObservation) (RecoveryPlan, uint64, error) {
	e = e.observing(input)
	revision, state, err := e.load()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	if state == nil {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	// 屏障闸（FA）：host action 对账是权威写入——intent 结算、frontier
	// settle 与补位签发在未决需求变化期间冻结（解除后照常）。
	if err := state.errWriteBarrier("host action reconciliation"); err != nil && !state.resetPending() {
		return RecoveryPlan{}, 0, err
	}
	if effect, reconciled := state.ReconciledEffects[actionID]; reconciled {
		return RecoveryPlan{
			Class:  authoring.FailureSideEffectUnknown,
			Action: RecoveryReconcile,
			Detail: "host action was reconciled as " + effect.Status,
		}, revision, nil
	}
	if intent, pending := state.PendingHostActions[actionID]; pending && (intent.Resume != nil || intent.Terminate != nil) && e.cfg.WorkerExecutionStopped != nil {
		target := intent.Resume
		if target == nil {
			target = intent.Terminate
		}
		worker, attempt, ok := state.workerActionTarget(target.WorkerActionID)
		if ok {
			stopped, err := e.cfg.WorkerExecutionStopped(worker, attempt)
			if err != nil {
				return RecoveryPlan{}, 0, err
			}
			if stopped {
				digest, err := e.facts().Digest()
				if err != nil {
					return RecoveryPlan{}, 0, err
				}
				status := "WITHDRAWN"
				if intent.Terminate != nil {
					status = "FULFILLED"
				}
				state.retireNative(intent, status, digest, nil, revision+1)
				plan := RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryReconcile, Detail: "original native invocation stopped: " + status}
				committed, _, err := e.commitEffects(state, revision, input, nil)
				return plan, committed, err
			}
			if !state.abortRequested() && !state.resetPending() && !state.NodeContinuationWithdraws(intent) {
				return RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryWait, Detail: "original native invocation is not confirmed stopped"}, revision, nil
			}
		}
	}
	if intent, pending := state.PendingHostActions[actionID]; pending && intent.Resume != nil {
		_, obsolete := state.ObsoleteActions[intent.Resume.WorkerActionID]
		if state.abortRequested() || state.resetPending() || obsolete || state.NodeContinuationWithdraws(intent) {
			for _, fact := range e.facts().Facts {
				if fact.Source == decision.SourceHost && fact.Key == "native-invocation:"+actionID && fact.Value == "UNCLAIMED" {
					digest, err := e.sourceFacts.Digest()
					if err != nil || digest != observationDigest {
						return RecoveryPlan{}, 0, validationError("resume withdrawal requires its original invocation observation")
					}
					state.retireNative(intent, "WITHDRAWN", observationDigest, nil, revision+1)
					plan := RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryReconcile, Detail: "unclaimed resume withdrawn from its original native invocation"}
					committed, _, err := e.commitEffects(state, revision, input, nil)
					return plan, committed, err
				}
			}
		}
	}
	if len(observations) == 1 && (state.abortRequested() || state.resetPending() || state.NodeContinuationWithdraws(state.PendingHostActions[actionID]) || state.WorkspaceMirrorSuperseded(state.PendingHostActions[actionID], e.cfg.Definition)) && observations[0].Resource != nil && observations[0].Promotion == nil {
		intent, ok := state.PendingHostActions[actionID]
		observation := observations[0]
		if !ok || observationDigest != observation.Digest() {
			return RecoveryPlan{}, 0, validationError("resource withdrawal requires an exact original observation")
		}
		if err := state.validateResourceObservation(intent, *observation.Resource); err != nil {
			return RecoveryPlan{}, 0, err
		}
		state.retireNative(intent, "WITHDRAWN", observationDigest, &observation, revision+1)
		plan := RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryReconcile, Detail: "native remainder withdrawn from its exact resource prefix"}
		committed, _, err := e.commitEffects(state, revision, input, nil)
		return plan, committed, err
	}
	if len(observations) == 1 && (state.abortRequested() || state.resetPending() || state.NodeContinuationWithdraws(state.PendingHostActions[actionID])) && observations[0].Promotion != nil && observations[0].Resource == nil {
		intent, ok := state.PendingHostActions[actionID]
		params, parseErr := candidateParameters(intent)
		observation := *observations[0].Promotion
		if !ok || parseErr != nil || params.Promotion == nil || observation.Head == "" || observation.Tree == "" || observation.IndexTree == "" && observation.PendingFingerprint == "" || !observation.Clean && !observation.StagedOnly || observationDigest != digestOfCanonical(observation) {
			return RecoveryPlan{}, 0, validationError("promotion withdrawal requires the original intent and exact native observation")
		}
		chain, err := state.PromotionChain(intent)
		if err != nil {
			return RecoveryPlan{}, 0, err
		}
		for _, original := range chain {
			state.retireNative(original, "WITHDRAWN", observationDigest, &HostActionObservation{Promotion: &observation}, revision+1)
		}
		plan := RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryReconcile, Detail: "aborted promotion remainder withdrawn from the exact native prefix"}
		committed, _, err := e.commitEffects(state, revision, input, nil)
		return plan, committed, err
	}
	receipt, hasReceipt := state.HostActionReceipts[actionID]
	if !hasReceipt || receipt.Status != HostActionStatusUnknown {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("host action UNKNOWN receipt %q does not exist", actionID)}
	}
	intent, ok := state.PendingHostActions[actionID]
	if !ok {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("host action intent %q does not exist", actionID)}
	}
	nextRevision := revision + 1
	plan := RecoveryPlan{Class: authoring.FailureSideEffectUnknown, Action: RecoveryReconcile}
	if conflict {
		plan.Action = RecoveryOperator
		plan.Detail = "external observation conflicts with the pending host action intent"
	} else if fulfilled && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
		plan.Action = RecoveryWait
		plan.Detail = "native completion requires the original typed adapter evidence; submit its EXECUTED receipt"
	} else if fulfilled {
		plan.Detail = "external observation already satisfies the intent; result committed without re-execution"
		var adapterOperation authoring.OperationID
		if intent.Adapter != nil {
			adapterOperation = intent.Adapter.Operation
		}
		state.ReconciledEffects[actionID] = ReconciledEffect{ActionID: actionID, Operation: intent.Operation, AdapterOperation: adapterOperation, ObservationDigest: observationDigest, Status: "FULFILLED", Revision: nextRevision}
		state.HostActionReceipts[actionID] = HostActionReceipt{ActionID: actionID, Operation: intent.Operation, Step: intent.Step, Provider: state.hostActionProvider(intent), Correlation: "reconcile", PayloadDigest: intent.PayloadDigest, Status: HostActionStatusReconciled, Digest: digestOfCanonical(map[string]any{"actionId": actionID, "observationDigest": observationDigest, "status": HostActionStatusReconciled})}
		// 对账即外部事实已满足（与 EXECUTED 回执接纳同一条 settle 语义，
		// 不再次执行）：intent 清算入同一清算账，由事务收尾单点
		//（commitEffects）与屏障期挂起的清算共用一条执行路径。
		if !slices.Contains(state.HeldHostSettles, actionID) {
			state.HeldHostSettles = append(state.HeldHostSettles, actionID)
		}
	} else {
		plan.Action = RecoveryWait
		plan.Detail = "external observation does not yet satisfy the intent; keep pending and wait"
	}
	state.appendRecovery(RecoveryRecord{ActionID: actionID, Class: plan.Class, Action: plan.Action, Detail: plan.Detail, Revision: nextRevision})
	commitRevision, _, err := e.commitEffects(state, revision, input, nil)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}
