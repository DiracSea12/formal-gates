package protocol

import (
	"formal-gates/internal/cost"
)

// CostSourceState 是 typed source 引用的三值解析结果。
const (
	// CostSourceReady：stop 证据在账且 transcript 可解析，Usage 携带精确用量。
	CostSourceReady = "READY"
	// CostSourcePending：派发有 lifecycle 绑定但 stop 尚未到达——engine 把结果
	// 留在回执边界并等待该证据，然后才完成与补位。
	CostSourcePending = "PENDING"
	// CostSourceUnavailable：来源明确不可用（无绑定、provider 不符、stop 无
	// transcript、解析失败或不支持的宿主）——记 UNAVAILABLE 后继续使用
	// request/attempt guard。
	CostSourceUnavailable = "UNAVAILABLE"
)

// StatusCostPending 是结果滞留回执边界的 acceptance 状态：计量来源的 lifecycle
// stop 证据未到，完成与补位挂起至来源落定的下一事务。
const StatusCostPending = "COST_PENDING"

// CostSourceRef 是派发计量来源的 typed 引用：(runID, actionID, provider,
// correlation) 绑定权威回执身份，防跨 run/跨派发误归属。
type CostSourceRef struct {
	RunID       string `json:"runId"`
	ActionID    string `json:"actionId"`
	Provider    string `json:"provider"`
	Correlation string `json:"correlation,omitempty"`
}

// ResolvedCostSource 是一次解析结果：State 三值＋READY 时的精确用量。
type ResolvedCostSource struct {
	State CostSourceState
	Usage cost.Usage
}

// CostSourceState 是 typed source 引用的三值解析结果。
type CostSourceState = string

// CostSourceResolver 负责 lifecycle-sidecar 查找（typed 引用→READY/PENDING/
// UNAVAILABLE）。transcript 解析归 internal/cost；engine 只消费投影。nil 表示
// 本引擎无宿主计量集成，cost 维度不参与判定。
type CostSourceResolver func(ref CostSourceRef) ResolvedCostSource

// dispatchCostHeld 在结果完成前解析派发的计量来源：READY/UNAVAILABLE 即刻把
// 条目记入 State.Cost（幂等由 cost.Record 持有）；PENDING 报告滞留（调用方把
// 结果留在回执边界，acceptance 记 COST_PENDING）。无 bridge（nil）时不参与
// 判定，行为与未集成时一致。
func (e *Engine) dispatchCostHeld(state *State, actionID string) bool {
	if e.cfg.CostSource == nil {
		return false
	}
	receipt, receipted := state.SpawnReceipts[actionID]
	if !receipted {
		return false
	}
	resolved := e.cfg.CostSource(CostSourceRef{
		RunID: state.RunID, ActionID: actionID,
		Provider: state.RunProvider, Correlation: receipt.Correlation,
	})
	if resolved.State != CostSourcePending {
		e.recordDispatchCost(state, actionID, resolved)
	} else {
		// 持留中的派发先建空 cost 投影（零数字、不预记条目）——该派发的精
		// 确用量在 stop 证据落定的下一事务回填，持留期投影可见为零值。
		state.costLedger()
	}
	return resolved.State == CostSourcePending
}

// recordDispatchCost 把解析结果写入 run 的 cost 投影（唯一写入点经
// cost.Record；重复完成不重复累计）。
func (e *Engine) recordDispatchCost(state *State, actionID string, resolved ResolvedCostSource) {
	pending, known := state.PendingActions[actionID]
	if !known {
		if receipt, receipted := state.SpawnReceipts[actionID]; receipted {
			pending = PendingAction{Task: receipt.Task}
		}
	}
	entry := cost.DispatchCost{
		Target: pending.Task.String(),
		Kind:   "action",
		Source: cost.SourceUnavailable,
	}
	if state.validationRole(pending.Task, e.cfg.Definition) == "engine.gate" {
		entry.Kind = "gate"
	}
	if resolved.State == CostSourceReady {
		entry.Source = cost.SourceTranscript
		entry.InputCacheHitTokens = resolved.Usage.InputCacheHitTokens
		entry.InputCacheMissTokens = resolved.Usage.InputCacheMissTokens
		entry.OutputTokens = resolved.Usage.OutputTokens
		entry.TotalInputTokens = resolved.Usage.InputCacheHitTokens + resolved.Usage.InputCacheMissTokens
	}
	cost.Record(state.costLedger(), actionID, entry)
}

// costLedger 返回（必要时初始化）run 的 cost 投影。
func (s *State) costLedger() *cost.RunCost {
	if s.Cost == nil {
		s.Cost = &cost.RunCost{}
	}
	return s.Cost
}
