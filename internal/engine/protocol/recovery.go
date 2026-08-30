package protocol

import (
	"fmt"
	"sort"
	"strconv"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
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
	Class            authoring.FailureClass
	CauseKnown       bool
	ReceiptUnknown   bool
	LifecycleMatches int
}

// RecoveryPlan 是恢复决策的持久化前结果。RequestID 只在 ASK 路由生成，
// LifecycleMatches 用于让 receipt UNKNOWN 的 Operator 结果可观察。
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
func (s *State) lifecycleMatches(correlation string) []string {
	if correlation == "" {
		return nil
	}
	seen := map[string]bool{}
	for _, record := range s.LifecycleEvents {
		if record.Correlation == correlation && s.lifecyclePaired(correlation, record.Identity) {
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

// retireAction 把当前 action 从可接纳索引移到 obsolete 索引。已落账的
// receipt/result 是审计事实，保留；未消费的 result-before-receipt 暂存
// 结果属于失败/被替换的派发边界，随 action 一并清除——否则它没有正常
// 消费路径，会在 state 中永久残留（审阅 P2）。
func (s *State) retireAction(actionID, replacedBy, reason string) (PendingAction, bool) {
	pending, ok := s.PendingActions[actionID]
	if !ok {
		return PendingAction{}, false
	}
	attempt := s.Attempts[pending.Task]
	delete(s.PendingActions, actionID)
	delete(s.Attempts, pending.Task)
	delete(s.StagedResults, actionID)
	s.ObsoleteActions[actionID] = ObsoleteAction{
		ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID,
		ReplacedBy: replacedBy, Reason: reason, Bindings: attempt.Bindings, Plan: attempt.Plan,
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
	s.Attempts[pending.Task] = attempt
	s.PendingActions[actionID] = PendingAction{ActionID: actionID, Task: pending.Task, Step: pending.Step, AttemptID: attempt.ID}
	return attempt, nil
}

// RecoverAttempt applies the durable part of a recovery decision for a
// current Attempt. Resume/reconcile/operator/agent decisions retain the old
// action until their declared next protocol step; NEW_ATTEMPT retires the old
// action before installing a fresh one; ASK persists resume/fresh/abort.
func (e *Engine) RecoverAttempt(actionID string, interruption Interruption, expectedFingerprint string) (RecoveryPlan, uint64, error) {
	revision, state, err := e.load()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	if state == nil {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	pending, ok := state.PendingActions[actionID]
	if !ok {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("recovery references unknown action %q", actionID)}
	}
	attempt, ok := state.Attempts[pending.Task]
	if !ok || attempt.ID != pending.AttemptID {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeStaleAttempt, Detail: fmt.Sprintf("recovery action %q has no matching current Attempt", actionID)}
	}
	currentBindings := AttemptBindings{
		Task: pending.Task, Snapshot: expectedFingerprint, Responsibility: state.RunProvider,
	}
	plan := decideRecovery(interruption, !attempt.Bindings.Equal(currentBindings))
	if interruption.Class == authoring.FailureTransientEngine && attempt.RetryExhausted {
		plan = RecoveryPlan{
			Class:  interruption.Class,
			Action: RecoveryWait,
			Detail: fmt.Sprintf("transient retry attempts exhausted (%d/%d); wait for terminal handling", attempt.Attempts, attempt.MaxAttempts),
		}
	}
	nextRevision := revision + 1
	if plan.Action == RecoveryAsk {
		plan.RequestID = "recover:" + actionID + ":" + strconv.FormatUint(nextRevision, 10)
		plan.Options = recoveryAskOptions()
		state.PendingAsks[plan.RequestID] = PendingAsk{RequestID: plan.RequestID, Control: ControlRecovery, Options: append([]AskOption(nil), plan.Options...)}
	}
	if plan.Action == RecoveryNewAttempt {
		retired, _ := state.retireAction(actionID, "", "recovery replaced the interrupted Attempt")
		replacement, err := state.installReplacement(retired, nextRevision, currentBindings, attempt.Plan, attempt.Attempts+1, attempt.MaxAttempts)
		if err != nil {
			return RecoveryPlan{}, 0, err
		}
		plan.Detail = plan.Detail + "; replacement attempt " + replacement.ID
		obsolete := state.ObsoleteActions[actionID]
		obsolete.ReplacedBy = replacement.ActionID
		state.ObsoleteActions[actionID] = obsolete
	}
	if plan.Action == RecoveryFail {
		if err := state.TransitionTask(pending.Task, runtime.TaskTerminal); err != nil {
			return RecoveryPlan{}, 0, &RejectedError{Code: CodeIllegalTransition, Detail: fmt.Sprintf("recover %s: %v", pending.Task.String(), err)}
		}
		state.removeTaskBookkeeping(pending.Task)
	}
	state.appendRecovery(RecoveryRecord{ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID, Class: plan.Class, Action: plan.Action, RequestID: plan.RequestID, Detail: plan.Detail, LifecycleMatches: plan.LifecycleMatches, Revision: nextRevision})
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}

// ReconcileUnknownReceipt resolves a SpawnReceipt whose status is UNKNOWN.
// Candidate lifecycle identities are derived from the persisted lifecycle
// buffer. Exactly one match allows an automatic attach; zero or multiple
// matches become an Operator route and never trigger a blind respawn.
func (e *Engine) ReconcileUnknownReceipt(actionID, expectedFingerprint string) (RecoveryPlan, uint64, error) {
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
	matches := state.lifecycleMatches(receipt.Correlation)
	plan := DecideRecovery(Interruption{ReceiptUnknown: true, LifecycleMatches: len(matches)})
	nextRevision := revision + 1
	if len(matches) == 1 {
		receipt.Status = SpawnStatusAttached
		receipt.LifecycleIdentity = matches[0]
		state.SpawnReceipts[actionID] = receipt
		plan.Detail = "unknown receipt attached to unique lifecycle identity " + matches[0]
		if staged, waiting := state.StagedResults[actionID]; waiting {
			refill, err := e.completeResult(state, staged, nextRevision, expectedFingerprint)
			if err != nil {
				return RecoveryPlan{}, 0, err
			}
			delete(state.StagedResults, actionID)
			_ = refill
		}
	} else {
		plan.Detail = fmt.Sprintf("unknown receipt has %d lifecycle matches; operator reconciliation required", len(matches))
	}
	state.appendRecovery(RecoveryRecord{ActionID: actionID, Class: authoring.FailureSideEffectUnknown, Action: plan.Action, Detail: plan.Detail, LifecycleMatches: len(matches), Revision: nextRevision})
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}

// ReconcileHostAction settles an UNKNOWN local side effect from observed
// external facts. fulfilled=true commits only the reconciliation result and
// never invokes ExecuteHostAction again; conflict routes to Operator; an
// unfulfilled but non-conflicting observation remains WAIT/pending.
func (e *Engine) ReconcileHostAction(actionID, observationDigest string, fulfilled, conflict bool, expectedFingerprint string) (RecoveryPlan, uint64, error) {
	revision, state, err := e.load()
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	if state == nil {
		return RecoveryPlan{}, 0, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if effect, reconciled := state.ReconciledEffects[actionID]; reconciled {
		return RecoveryPlan{
			Class:  authoring.FailureSideEffectUnknown,
			Action: RecoveryReconcile,
			Detail: "host action was already reconciled as " + effect.Status,
		}, revision, nil
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
	} else if fulfilled {
		plan.Detail = "external observation already satisfies the intent; result committed without re-execution"
		delete(state.PendingHostActions, actionID)
		var adapterOperation authoring.OperationID
		if intent.Adapter != nil {
			adapterOperation = intent.Adapter.Operation
		}
		state.ReconciledEffects[actionID] = ReconciledEffect{ActionID: actionID, Operation: intent.Operation, AdapterOperation: adapterOperation, ObservationDigest: observationDigest, Status: "FULFILLED", Revision: nextRevision}
		state.HostActionReceipts[actionID] = HostActionReceipt{ActionID: actionID, Operation: intent.Operation, Step: intent.Step, Provider: state.RunProvider, Correlation: "reconcile", PayloadDigest: intent.PayloadDigest, Status: HostActionStatusReconciled, Digest: digestOfCanonical(map[string]any{"actionId": actionID, "observationDigest": observationDigest, "status": HostActionStatusReconciled})}
		// 对账即外部事实已满足：完成对应 HOST_ACTION frontier 步骤并补位
		// 签发（与 EXECUTED 回执接纳同一条 settle 语义，不再次执行）。
		if err := state.settleFrontierSteps(e.cfg.Definition); err != nil {
			return RecoveryPlan{}, 0, err
		}
		if _, refillErr := e.refill(state, nextRevision, expectedFingerprint); refillErr != nil {
			return RecoveryPlan{}, 0, refillErr
		}
	} else {
		plan.Action = RecoveryWait
		plan.Detail = "external observation does not yet satisfy the intent; keep pending and wait"
	}
	state.appendRecovery(RecoveryRecord{ActionID: actionID, Class: plan.Class, Action: plan.Action, Detail: plan.Detail, Revision: nextRevision})
	commitRevision, err := e.commit(state, revision, expectedFingerprint)
	if err != nil {
		return RecoveryPlan{}, 0, err
	}
	return plan, commitRevision, nil
}
