package runtime

import (
	"reflect"
	"testing"
)

// goldenPhaseEdges 是合法迁移边全表的显式期望：流水线边（含修复回环与
// 分片/合并支路）+ 每个非 TERMINAL phase 的终止闭包边，按 (From, To)
// 排序。表结构变化必须以有意识的黄金表更新留痕。
var goldenPhaseEdges = []PhaseEdge{
	{PhaseDevelopmentParallel, PhaseMainlineIntegration},
	{PhaseDevelopmentParallel, PhaseSnapshotReady},
	{PhaseDevelopmentParallel, PhaseTerminal},
	{PhaseIntakeRegistered, PhaseProductReview},
	{PhaseIntakeRegistered, PhaseTerminal},
	{PhaseMainlineIntegration, PhaseMergeValidation},
	{PhaseMainlineIntegration, PhaseTerminal},
	{PhaseMergeValidation, PhaseRepair},
	{PhaseMergeValidation, PhaseTerminal},
	{PhasePostReview, PhaseRepair},
	{PhasePostReview, PhaseSliceReady},
	{PhasePostReview, PhaseSnapshotReady},
	{PhasePostReview, PhaseTerminal},
	{PhaseProductReview, PhaseTechnicalReview},
	{PhaseProductReview, PhaseTerminal},
	{PhaseRepair, PhaseMainlineIntegration},
	{PhaseRepair, PhaseSnapshotReady},
	{PhaseRepair, PhaseTerminal},
	{PhaseSliceReady, PhaseTerminal},
	{PhaseSnapshotReady, PhasePostReview},
	{PhaseSnapshotReady, PhaseTerminal},
	{PhaseStartReadiness, PhaseTerminal},
	{PhaseStartReadiness, PhaseTopologyAndRoute},
	{PhaseTechnicalReview, PhaseStartReadiness},
	{PhaseTechnicalReview, PhaseTerminal},
	{PhaseTopologyAndRoute, PhaseDevelopmentParallel},
	{PhaseTopologyAndRoute, PhaseTerminal},
}

func TestPhaseTransitionTableGolden(t *testing.T) {
	got := PhaseTransitionTable()
	if want := goldenPhaseEdges; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase transition table mismatch:\n got  %v\n want %v", got, want)
	}
}

// TestPhaseTransitionsExhaustive 在全部 (from, to) 点对上核对：只有黄金表
// 中的边合法，其余（回退、跳步、自环、非法枚举值）一律拒绝。
func TestPhaseTransitionsExhaustive(t *testing.T) {
	legal := make(map[PhaseEdge]bool, len(goldenPhaseEdges))
	for _, e := range goldenPhaseEdges {
		legal[e] = true
	}
	all := append(append([]RunPhase{}, phases...), "NOT_A_PHASE", "")
	for _, from := range all {
		for _, to := range all {
			e := PhaseEdge{From: from, To: to}
			err := PhaseTransition(from, to)
			if legal[e] {
				if err != nil {
					t.Errorf("PhaseTransition(%s -> %s) = %v, want nil", from, to, err)
				}
				if !PhaseCanTransition(from, to) {
					t.Errorf("PhaseCanTransition(%s -> %s) = false, want true", from, to)
				}
			} else if err == nil || PhaseCanTransition(from, to) {
				t.Errorf("PhaseTransition(%s -> %s) accepted illegal edge", from, to)
			}
		}
	}
	// TERMINAL 无出边（穷举已覆盖，这里显式断言语义）。
	for _, to := range phases {
		if PhaseCanTransition(PhaseTerminal, to) {
			t.Errorf("TERMINAL must have no outgoing edge, got TERMINAL -> %s", to)
		}
	}
}

func TestPhaseValid(t *testing.T) {
	if len(phases) != 13 {
		t.Fatalf("phase count = %d, want 13", len(phases))
	}
	for _, p := range phases {
		if !p.Valid() {
			t.Errorf("phase %q should be valid", p)
		}
	}
	for _, bad := range []RunPhase{"", "TERMINAL ", "terminal", "INTAKE"} {
		if bad.Valid() {
			t.Errorf("phase %q should be invalid", bad)
		}
	}
}

// TestPhaseTransitionTableCopy 确认访问器返回副本：调用方改写不得污染
// 静态权威表。
func TestPhaseTransitionTableCopy(t *testing.T) {
	got := PhaseTransitionTable()
	got[0] = PhaseEdge{From: PhaseTerminal, To: PhaseTerminal}
	again := PhaseTransitionTable()
	if again[0] == got[0] {
		t.Fatal("mutating returned slice leaked into the static table")
	}
}
