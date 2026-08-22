package decision

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/runtime"
)

func TestNewState(t *testing.T) {
	if _, err := NewState("", runtime.PhaseIntakeRegistered); err == nil {
		t.Error("empty definition version must be rejected")
	}
	if _, err := NewState(testDefVersion, "NOT_A_PHASE"); err == nil {
		t.Error("invalid phase must be rejected")
	}
	s, err := NewState(testDefVersion, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if s.TaskStatusOf(runtime.TaskKey{Node: "n", Step: "s"}) != runtime.TaskQueued {
		t.Error("unregistered task key must read as QUEUED")
	}
}

// TestCompleteStepBoundary 校验运行时边界接口（§5.7：拒绝乱序、遗漏和
// 重复 step）。
func TestCompleteStepBoundary(t *testing.T) {
	cd := testDefinition(t)

	s := newTestState(t)
	// 乱序：依赖未完成先完成下游步骤。
	if err := s.CompleteStep("work.agent", cd); err == nil || !strings.Contains(err.Error(), "out-of-order") {
		t.Errorf("out-of-order completion err = %v, want out-of-order rejection", err)
	}
	// 遗漏：join 步骤只补齐部分依赖。
	if err := s.CompleteStep("boot.local", cd); err != nil {
		t.Fatalf("boot.local: %v", err)
	}
	if err := s.CompleteStep("work.agent", cd); err != nil {
		t.Fatalf("work.agent: %v", err)
	}
	if err := s.CompleteStep("fin.report", cd); err == nil || !strings.Contains(err.Error(), "gate.durable") {
		t.Errorf("skipped-dependency completion err = %v, want missing dependency named", err)
	}
	// 重复：已完成步骤再次完成。
	if err := s.CompleteStep("boot.local", cd); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("duplicate completion err = %v, want duplicate rejection", err)
	}
	// 遗漏/未知：不在定义中的步骤。
	if err := s.CompleteStep("no.such.step", cd); err == nil || !strings.Contains(err.Error(), "not in definition") {
		t.Errorf("unknown step err = %v, want not-in-definition rejection", err)
	}
	// 版本绑定与 nil 定义。
	if err := s.CompleteStep("work.ask", cd); err != nil {
		t.Fatalf("work.ask: %v", err)
	}
	if err := s.CompleteStep("gate.host", nil); err == nil {
		t.Error("nil definition must be rejected")
	}
	other, err := NewState("other-version", runtime.PhasePostReview)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := other.CompleteStep("boot.local", cd); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("version mismatch err = %v, want version rejection", err)
	}
}

// TestStateCanonicalBytesOrderIndependent 核对 sibling 步骤以不同顺序完成
// 时 canonical 字节一致（Completed 有序维护，字节只是状态内容的函数）。
func TestStateCanonicalBytesOrderIndependent(t *testing.T) {
	cd := testDefinition(t)
	s1 := newTestState(t)
	complete(t, s1, cd, "boot.local", "work.agent", "work.ask")
	s2 := newTestState(t)
	complete(t, s2, cd, "boot.local", "work.ask", "work.agent")
	b1, err := s1.CanonicalBytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	b2, err := s2.CanonicalBytes()
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("same state content must encode identically:\n%s\n%s", b1, b2)
	}
	d1, _ := s1.Digest()
	d2, _ := s2.Digest()
	if d1 != d2 {
		t.Fatal("state digest must be content-only")
	}
	// 状态变化 → digest 变化。
	complete(t, s1, cd, "gate.host")
	d1b, _ := s1.Digest()
	if d1b == d2 {
		t.Fatal("state change must change digest")
	}
	// 任务状态进入 canonical 字节（有序，不依赖 map 遍历序）。
	sa, sb := newTestState(t), newTestState(t)
	for _, k := range []runtime.TaskKey{
		{Node: "work", Step: "work.agent"}, {Node: "work", Step: "work.ask"},
	} {
		if err := sa.TransitionTask(k, runtime.TaskIssued); err != nil {
			t.Fatalf("transition: %v", err)
		}
	}
	if err := sb.TransitionTask(runtime.TaskKey{Node: "work", Step: "work.ask"}, runtime.TaskIssued); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if err := sb.TransitionTask(runtime.TaskKey{Node: "work", Step: "work.agent"}, runtime.TaskIssued); err != nil {
		t.Fatalf("transition: %v", err)
	}
	ba, _ := sa.CanonicalBytes()
	bb, _ := sb.CanonicalBytes()
	if string(ba) != string(bb) {
		t.Fatal("task map insertion order must not leak into canonical bytes")
	}
}

func TestStateTransitions(t *testing.T) {
	s := newTestState(t)
	// phase：合法推进 + 非法回退。
	if err := s.TransitionPhase(runtime.PhaseSnapshotReady); err != nil {
		t.Fatalf("legal phase transition: %v", err)
	}
	if err := s.TransitionPhase(runtime.PhaseDevelopmentParallel); err == nil {
		t.Error("phase backward transition must be rejected")
	}
	// task：QUEUED→ISSUED 合法；QUEUED→RUNNING 跳步非法；终态无出边。
	key := runtime.TaskKey{Node: "work", Step: "work.agent"}
	if err := s.TransitionTask(key, runtime.TaskIssued); err != nil {
		t.Fatalf("legal task transition: %v", err)
	}
	if s.TaskStatusOf(key) != runtime.TaskIssued {
		t.Fatalf("status = %s, want ISSUED", s.TaskStatusOf(key))
	}
	if err := s.TransitionTask(key, runtime.TaskTerminal); err != nil {
		t.Fatalf("ISSUED→TERMINAL: %v", err)
	}
	if err := s.TransitionTask(key, runtime.TaskQueued); err == nil {
		t.Error("terminal task must not go back to QUEUED")
	}
	bad := runtime.TaskKey{Node: "", Step: "x"}
	if err := s.TransitionTask(bad, runtime.TaskIssued); err == nil {
		t.Error("invalid task key must be rejected")
	}
}
