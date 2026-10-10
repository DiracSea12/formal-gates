package protocol

import (
	"fmt"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
)

// KindDispatchBudgetDisposition 是派发预算边界的唯一 typed control
// （额度或 retry 耗尽、token cap 到达后返回 typed Ask；固定四选项
// 有限继续/重试/跳过/abort，不新增生命周期或公开旁路）。经 ControlValidation
// 的既有 Ask/DECIDE 机制落账与授权，不是新的 ControlKind。
const KindDispatchBudgetDisposition = "DISPATCH_BUDGET_DISPOSITION"

// budgetDispositionTarget 是无单一逻辑动作的预算边界（如 token cap 到达）使用
// 的 run 级 Ask 目标身份；「retry」选项需要逻辑动作目标，对其拒绝。
const budgetDispositionTarget = "engine-dispatch-budget"

// dispatchBudgetOptions 是预算处置 Ask 的固定四选项（不接受自由数字，也不能
// 预先累积）：continue 固定解锁当前 canonical Ready batch 一次（仍按既有
// admission capacity 裁剪）；retry 固定增加目标逻辑动作的一个 retry slot；
// skip 只留痕不解锁；abort 经通用 abort 分支终止 run。
func dispatchBudgetOptions() []AskOption {
	return []AskOption{
		{ID: "continue", Label: "Unlock the current Ready batch once"},
		{ID: "retry", Label: "Add one retry slot to the boundary action"},
		{ID: "skip", Label: "Keep the boundary blocked"},
		{ID: "abort", Label: "Abort this run"},
	}
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
