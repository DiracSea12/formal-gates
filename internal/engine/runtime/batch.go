package runtime

// BatchID 是开发批次的稳定标识（ADR-002：批次计划在拆分决定与路线确认时
// 产生并留痕，允许"单批 + 理由"退化形态）。
type BatchID string

// Batch 是 TaskKey 上的分组与批间依赖信息（ADR-002 决策 7、总需求
// §8.5.6）：batch 不是状态机——没有独立状态、receipt 或生命周期，批次
// 完成状态从成员 task 状态派生；批间依赖（DependsOn）只是登记信息，
// 依赖链串行/零耦合并行的语义判断由主代理/Operator 建议、用户确认，
// 引擎不做语义分类器。
//
// 依赖链必须串行、共享工作面不并行等批次纪律由批次计划（受理后的用户
// 确认产物）保证，本结构只承载机械信息。
type Batch struct {
	ID        BatchID
	Tasks     []TaskKey
	DependsOn []BatchID
}

// Complete 报告批是否完成：全部成员 task 均为 TERMINAL 即完成；任一成员
// 未终态、或状态未知（不在 statuses 中——按未终态处理）即未完成。空成员
// 批视为完成（全称命题在空集上成立）。
func (b Batch) Complete(statuses map[TaskKey]TaskStatus) bool {
	for _, k := range b.Tasks {
		if statuses[k] != TaskTerminal {
			return false
		}
	}
	return true
}
