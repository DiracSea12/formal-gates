package decision

import (
	"fmt"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/runtime"
)

// Admission 是 SelectIssued 的容量输入。真实容量（在途任务上限等）由
// 后续批次的容量收集器计算；本批只承载机械值 C。
type Admission struct {
	Capacity int
}

// IssuedAction 是单个已签发动作：确定性 actionID + 任务薄指针。actionID
// 不含随机数与当前时间——phase 1 由 TaskKey 决定性地派生
// （"act:" + 规范键形态）；重试/Attempt 身份随阶段 2 的 Attempt 模型扩展。
type IssuedAction struct {
	ActionID string           `json:"actionId"`
	Task     runtime.TaskKey  `json:"task"`
	Step     authoring.StepID `json:"step"`
}

// IssuedSet 是一次签发的完整集合：容量 C、可签发 N 时恰好 min(C,N) 个，
// 顺序与 Plan 固定顺序一致。主代理必须原样整批搬运，不得挑子集
// （master-requirements §5.12、§8.2.3）。
type IssuedSet []IssuedAction

// ActionStore 是签发持久化接口：SelectIssued 分配 actionID 后、返回前
// 必须落账（阶段 2 接入 state.json 的 pendingActions 与 revision/CAS；
// 本批实现方自行保证持久化语义）。
type ActionStore interface {
	PersistIssued(IssuedSet) error
}

// SelectIssued 按 admission 机械裁剪 Ready 计划（master-requirements
// §8.2.2）：可签发 N、容量 C 时签发恰好 min(C,N) 个，不留空容量、不
// 选择性忽略、不把并行组退化为主代理决定的串行。只有 Kind=Ready 的
// 计划可签发；签发顺序固定为 Plan 顺序，actionID 派生后经 store 持久化。
func SelectIssued(plan *Plan, adm Admission, store ActionStore) (IssuedSet, error) {
	if plan == nil {
		return nil, fmt.Errorf("decision: select issued: nil plan")
	}
	if store == nil {
		return nil, fmt.Errorf("decision: select issued: nil action store")
	}
	if plan.Next.Kind != KindReady || plan.Next.Ready == nil {
		return nil, fmt.Errorf("decision: select issued: plan kind %s is not READY", plan.Next.Kind)
	}
	if adm.Capacity < 0 {
		return nil, fmt.Errorf("decision: select issued: negative capacity %d", adm.Capacity)
	}
	n := len(plan.Next.Ready.Tasks)
	k := n
	if adm.Capacity < k {
		k = adm.Capacity
	}
	issued := make(IssuedSet, 0, k)
	for _, t := range plan.Next.Ready.Tasks[:k] {
		issued = append(issued, IssuedAction{
			ActionID: "act:" + t.Task.String(),
			Task:     t.Task,
			Step:     t.Step,
		})
	}
	if err := store.PersistIssued(issued); err != nil {
		return nil, fmt.Errorf("decision: select issued: persist: %w", err)
	}
	return issued, nil
}
