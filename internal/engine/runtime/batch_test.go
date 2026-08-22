package runtime

import "testing"

func batchKeys() (TaskKey, TaskKey) {
	a, err := NewTaskKey("dev", "impl", "")
	if err != nil {
		panic(err)
	}
	b, err := NewTaskKey("dev", "qa-blackbox", "")
	if err != nil {
		panic(err)
	}
	return a, b
}

// TestBatchCompleteDerivation 核对 ADR-002 决策 7 的派生规则：全部成员
// TERMINAL → 批完成；任一成员未终态或状态未知 → 未完成。
func TestBatchCompleteDerivation(t *testing.T) {
	impl, qa := batchKeys()
	b := Batch{ID: "batch-2a", Tasks: []TaskKey{impl, qa}, DependsOn: []BatchID{"batch-1"}}

	if b.Complete(map[TaskKey]TaskStatus{impl: TaskTerminal, qa: TaskTerminal}) != true {
		t.Error("all-terminal members must derive batch complete")
	}
	// 任一成员未终态（各非终态状态逐一验证）→ 未完成。
	for _, s := range []TaskStatus{TaskQueued, TaskIssued, TaskRunning, TaskValidating} {
		statuses := map[TaskKey]TaskStatus{impl: TaskTerminal, qa: s}
		if b.Complete(statuses) {
			t.Errorf("member status %s must keep batch incomplete", s)
		}
	}
	// 状态未知（缺成员登记）按未终态处理 → 未完成。
	if b.Complete(map[TaskKey]TaskStatus{impl: TaskTerminal}) {
		t.Error("missing member status must keep batch incomplete")
	}
	if b.Complete(nil) {
		t.Error("nil statuses must keep batch incomplete")
	}
	// 空成员批视为完成（全称命题在空集上成立）。
	if !(Batch{ID: "empty"}).Complete(nil) {
		t.Error("task-less batch must derive complete")
	}
	// 批间依赖只是登记信息，不参与完成派生（成员状态才是唯一输入）。
	if b.DependsOn[0] != "batch-1" {
		t.Errorf("dependency info = %q, want batch-1", b.DependsOn[0])
	}
}
