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

// ResolvedCostSource carries exact known usage; UNAVAILABLE also carries the
// reason that the complete dispatch consumption cannot be measured.
type ResolvedCostSource struct {
	State  CostSourceState
	Usage  cost.Usage
	Reason string
}

// CostSourceState 是 typed source 引用的三值解析结果。
type CostSourceState = string

// CostSourceResolver 负责 lifecycle-sidecar 查找（typed 引用→READY/PENDING/
// UNAVAILABLE）。transcript 解析归 internal/cost；engine 只消费投影。nil 表示
// 本引擎无宿主计量集成，cost 维度不参与判定。
type CostSourceResolver func(ref CostSourceRef) ResolvedCostSource

// dispatchCostHeld 在结果完成前解析派发的计量来源：READY/UNAVAILABLE 即刻把
// 条目记入 State.Cost；未完派发更新原累计值，已完成派发保持幂等。PENDING 报告滞留（调用方把
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

// recordDispatchCost 通过原执行 owner 区分累计更新与完成后冻结。
// 同 action 的继续与恢复使用同一条 cost；重复完成不重复累计。
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
		Reason: resolved.Reason,
	}
	if state.validationRole(pending.Task, e.cfg.Definition) == "engine.gate.v2" {
		entry.Kind = "gate"
	}
	if resolved.State == CostSourceReady {
		entry.Source = cost.SourceTranscript
	}
	if resolved.State == CostSourceReady || resolved.State == CostSourceUnavailable {
		entry.InputCacheHitTokens = resolved.Usage.InputCacheHitTokens
		entry.InputCacheMissTokens = resolved.Usage.InputCacheMissTokens
		entry.OutputTokens = resolved.Usage.OutputTokens
		entry.TotalInputTokens = resolved.Usage.InputCacheHitTokens + resolved.Usage.InputCacheMissTokens
	}
	if _, _, current := state.workerActionTarget(actionID); current {
		cost.RecordCumulative(state.costLedger(), actionID, entry)
	} else {
		cost.Record(state.costLedger(), actionID, entry)
	}
}

// costLedger 返回（必要时初始化）run 的 cost 投影。
func (s *State) costLedger() *cost.RunCost {
	if s.Cost == nil {
		s.Cost = &cost.RunCost{}
	}
	return s.Cost
}
