package runtime

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
)

// goldenTaskEdges 是任务状态机合法转移全表（只前进；重跑走新实例）。
var goldenTaskEdges = []TaskEdge{
	{TaskIssued, TaskRunning},
	{TaskIssued, TaskTerminal},
	{TaskQueued, TaskIssued},
	{TaskQueued, TaskTerminal},
	{TaskRunning, TaskTerminal},
	{TaskRunning, TaskValidating},
	{TaskValidating, TaskTerminal},
}

func TestTaskTransitionTableGolden(t *testing.T) {
	if got := TaskTransitionTable(); !reflect.DeepEqual(got, goldenTaskEdges) {
		t.Fatalf("task transition table mismatch:\n got  %v\n want %v", got, goldenTaskEdges)
	}
}

// TestTaskTransitionsExhaustive 在全部点对上核对合法/非法：正常链
// QUEUED→ISSUED→RUNNING→VALIDATING→TERMINAL、两条提前终止边合法；
// 一切回退、跳步与非法枚举值拒绝。
func TestTaskTransitionsExhaustive(t *testing.T) {
	legal := make(map[TaskEdge]bool, len(goldenTaskEdges))
	for _, e := range goldenTaskEdges {
		legal[e] = true
	}
	all := append(append([]TaskStatus{}, taskStatuses...), "NOT_A_STATUS", "")
	for _, from := range all {
		for _, to := range all {
			e := TaskEdge{From: from, To: to}
			err := TaskTransition(from, to)
			if legal[e] {
				if err != nil {
					t.Errorf("TaskTransition(%s -> %s) = %v, want nil", from, to, err)
				}
				if !TaskCanTransition(from, to) {
					t.Errorf("TaskCanTransition(%s -> %s) = false, want true", from, to)
				}
			} else if err == nil || TaskCanTransition(from, to) {
				t.Errorf("TaskTransition(%s -> %s) accepted illegal edge", from, to)
			}
		}
	}
	// TERMINAL 无出边。
	for _, to := range taskStatuses {
		if TaskCanTransition(TaskTerminal, to) {
			t.Errorf("TERMINAL must have no outgoing edge, got TERMINAL -> %s", to)
		}
	}
}

func TestTaskStatusValid(t *testing.T) {
	for _, s := range taskStatuses {
		if !s.Valid() {
			t.Errorf("task status %q should be valid", s)
		}
	}
	for _, bad := range []TaskStatus{"", "QUEUED_", "queued"} {
		if bad.Valid() {
			t.Errorf("task status %q should be invalid", bad)
		}
	}
}

func TestTaskKey(t *testing.T) {
	k, err := NewTaskKey("dev", "impl", "child-2")
	if err != nil {
		t.Fatalf("NewTaskKey: %v", err)
	}
	if got, want := k.String(), "dev/impl/child-2"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	// scope 为空时省略尾段：静态步骤键形态。
	plain, err := NewTaskKey("review", "product", "")
	if err != nil {
		t.Fatalf("NewTaskKey: %v", err)
	}
	if got, want := plain.String(), "review/product"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
	// node/step 必填；scope 不参与合法性。
	for _, tc := range []struct{ node, step string }{
		{"", ""}, {"", "impl"}, {"dev", ""},
	} {
		if _, err := NewTaskKey(authoring.NodeID(tc.node), authoring.StepID(tc.step), ""); err == nil {
			t.Errorf("NewTaskKey(%q, %q) accepted empty node/step", tc.node, tc.step)
		}
	}
	// 不同 scope 是不同键；相同三元组是相同键。
	if k == plain {
		t.Fatal("distinct keys must not be equal")
	}
	k2, _ := NewTaskKey("dev", "impl", "child-2")
	if k != k2 {
		t.Fatal("same triple must produce the same key")
	}
}
