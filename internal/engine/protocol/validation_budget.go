package protocol

import (
	"fmt"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
)

// KindDispatchBudgetDisposition 是派发预算边界的唯一 typed control
// （额度或 retry 耗尽、token cap 到达后返回适用于当前边界的 typed Ask，
// 不新增生命周期或公开旁路）。经 ControlValidation
// 的既有 Ask/DECIDE 机制落账与授权，不是新的 ControlKind。
const KindDispatchBudgetDisposition = "DISPATCH_BUDGET_DISPOSITION"

// budgetDispositionTarget 是无单一逻辑动作的预算边界（如 token cap 到达）使用
// 的 run 级 Ask 目标身份；「retry」选项需要逻辑动作目标，对其拒绝。
const budgetDispositionTarget = "engine-dispatch-budget"

// dispatchBudgetOptions retains each choice's bounded semantics. A finite retry
// boundary requires its single extension; continue unlocks the current canonical
// Ready batch once within admission capacity and cannot change the retry policy.
func (s *State) dispatchBudgetOptions(target string) []AskOption {
	attempt, known := s.dispatchBudgetAttempt(target)
	retryBlocked := false
	if known && attempt.MaxAttempts > 0 {
		_, _, owed := s.workerActionTarget(attempt.ActionID)
		if owed {
			_, retryBlocked = s.effectRetryBudget(attempt)
		} else {
			retryBlocked = attempt.Attempts >= attempt.MaxAttempts
		}
	}
	var options []AskOption
	if !retryBlocked {
		options = append(options, AskOption{ID: "continue", Label: "Unlock the current Ready batch once"})
	}
	if retryBlocked && !s.retryExtensionGranted(target) {
		options = append(options, AskOption{ID: "retry", Label: "Add one retry slot to the boundary action"})
	}
	return append(options, AskOption{ID: "skip", Label: "Keep the boundary blocked"}, AskOption{ID: "abort", Label: "Abort this run"})
}

func (s *State) dispatchBudgetAttempt(target string) (Attempt, bool) {
	var task runtime.TaskKey
	if task.UnmarshalText([]byte(target)) != nil {
		return Attempt{}, false
	}
	if attempt, ok := s.Attempts[task]; ok {
		return attempt, true
	}
	attempt, ok := s.EndedAttempts[task]
	return attempt, ok
}

// ensureDispatchBudgetAsk 落账预算处置 Ask（幂等：同 kind/target 且摘要未变时
// 不重复创建）。target 是边界绑定的逻辑动作 TaskKey 字符串，或 run 级
// budgetDispositionTarget。
func (s *State) ensureDispatchBudgetAsk(target string, nextRevision uint64) bool {
	return s.ensureValidationAsk(ValidationRequest{Kind: KindDispatchBudgetDisposition, Target: target}, nextRevision)
}

// hasPendingBudgetAsk 报告是否存在未决的预算处置 Ask（预算边界的 Ask 判据：
// guard 零签发时外部边界是未决 Ask，不是错误）。
func (s *State) hasPendingBudgetAsk() bool {
	for _, ask := range s.PendingAsks {
		if !ask.Resolved && ask.Validation != nil && ask.Validation.Kind == KindDispatchBudgetDisposition {
			return true
		}
	}
	return false
}

// budgetContinueGranted 从决定账本推导「continue」的一次性继续授权：存在已决
// 的预算处置 Ask 选择 continue，且其后（含同事务）无任何授权效果落账。消耗凭
// 据全部从既有持久账本派生（授权效果的落账事实，不新建授权账本）：治理签发
// （含换新 install 的签发代数）、resume intent 的建立与其 EXECUTED 回执的接纳
// （接纳代数只在 Events 账本）——首个授权效果即消耗，不累积。
func (s *State) budgetContinueGranted(cd *compiler.CompiledDefinition) bool {
	granted := false
	var grantRevision uint64
	for id, decision := range s.Decisions {
		ask := s.PendingAsks[id]
		if decision.Choice != "continue" || ask.Validation == nil || ask.Validation.Kind != KindDispatchBudgetDisposition {
			continue
		}
		if decision.Revision > grantRevision {
			granted, grantRevision = true, decision.Revision
		}
	}
	if !granted {
		return false
	}
	for action, revision := range s.DispatchIssueRevisions {
		if revision < grantRevision {
			continue
		}
		if task, known := s.issuedTaskOf(action); known && s.validationRole(task, cd) != "" {
			return false
		}
	}
	for _, intent := range s.PendingHostActions {
		if intent.Operation == HostActionResumeAgent && intent.Revision >= grantRevision {
			return false
		}
	}
	for _, record := range s.Events {
		acceptance := record.Acceptance
		if acceptance.Kind != string(KindHostActionReceipt) || acceptance.Revision < grantRevision {
			continue
		}
		if receipt, ok := s.HostActionReceipts[acceptance.ActionID]; ok &&
			receipt.Operation == HostActionResumeAgent && receipt.Status == HostActionStatusExecuted {
			return false
		}
	}
	return true
}

// issuedTaskOf 从持久派发簿记还原 actionID 的任务身份（签发代数账本不随
// terminal 清理删除；任务身份经 DispatchBindings/SpawnReceipts/ObsoleteActions
// 三处账本定位）。
func (s *State) issuedTaskOf(actionID string) (runtime.TaskKey, bool) {
	if binding, ok := s.DispatchBindings[actionID]; ok {
		return binding.Task, true
	}
	if receipt, ok := s.SpawnReceipts[actionID]; ok && receipt.Task.Valid() {
		return receipt.Task, true
	}
	if obsolete, ok := s.ObsoleteActions[actionID]; ok {
		return obsolete.Task, true
	}
	return runtime.TaskKey{}, false
}

// extendBudgetRetry 对预算处置 Ask 的目标逻辑动作一次性追加一个 retry slot
// （MaxAttempts+1、解除耗尽标记；镜像 RETRY Ask 的既有处置，不累积、不改基础
// 槽位）。目标不是逻辑动作（run 级边界）、账本已不在、或该逻辑任务的一次追加
// 已被任一通道授予（retryExtensionGranted——RETRY approve 与本通道授出同一
// 个 slot）时拒绝。
func (s *State) extendBudgetRetry(target string) error {
	var key runtime.TaskKey
	if err := key.UnmarshalText([]byte(target)); err != nil {
		return validationError("budget retry requires the boundary's logical action target")
	}
	if s.retryExtensionGranted(target) {
		return validationError("the boundary action's single retry extension was already granted")
	}
	found := false
	if attempt, ok := s.Attempts[key]; ok {
		attempt.MaxAttempts++
		attempt.RetryExhausted = false
		s.Attempts[key] = attempt
		found = true
	}
	if attempt, ok := s.EndedAttempts[key]; ok {
		attempt.MaxAttempts++
		attempt.RetryExhausted = false
		s.EndedAttempts[key] = attempt
		found = true
	}
	if !found {
		return validationError(fmt.Sprintf("budget retry target %s has no attempt ledger entry", target))
	}
	return nil
}
