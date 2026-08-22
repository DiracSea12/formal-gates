package whitebox_qa

import (
	"sort"
	"testing"

	"formal-gates/internal/engine/runtime"
)

// 用例：RunPhase 迁移表编码流水线主线、修复回环与分片/合并支路；终止闭包
// 给每个非终态 phase 一条 TERMINAL 出边；非法边（跳步、回退、自环、TERMINAL
// 出边）一律拒绝；返回的表是副本，篡改副本不影响静态权威。
func TestPhaseTransitionTableEncodesPipelineAndClosure(t *testing.T) {
	legal := [][2]runtime.RunPhase{
		{runtime.PhaseIntakeRegistered, runtime.PhaseProductReview},
		{runtime.PhaseProductReview, runtime.PhaseTechnicalReview},
		{runtime.PhaseTechnicalReview, runtime.PhaseStartReadiness},
		{runtime.PhaseStartReadiness, runtime.PhaseTopologyAndRoute},
		{runtime.PhaseTopologyAndRoute, runtime.PhaseDevelopmentParallel},
		{runtime.PhaseDevelopmentParallel, runtime.PhaseSnapshotReady},
		{runtime.PhaseSnapshotReady, runtime.PhasePostReview},
		{runtime.PhasePostReview, runtime.PhaseRepair},
		{runtime.PhaseRepair, runtime.PhaseSnapshotReady},
		{runtime.PhasePostReview, runtime.PhaseSnapshotReady},
		{runtime.PhasePostReview, runtime.PhaseSliceReady},
		{runtime.PhasePostReview, runtime.PhaseTerminal},
		{runtime.PhaseSliceReady, runtime.PhaseTerminal},
		{runtime.PhaseDevelopmentParallel, runtime.PhaseMainlineIntegration},
		{runtime.PhaseMainlineIntegration, runtime.PhaseMergeValidation},
		{runtime.PhaseMergeValidation, runtime.PhaseRepair},
		{runtime.PhaseRepair, runtime.PhaseMainlineIntegration},
		{runtime.PhaseMergeValidation, runtime.PhaseTerminal},
	}
	for _, e := range legal {
		if !runtime.PhaseCanTransition(e[0], e[1]) {
			t.Errorf("pipeline edge %s -> %s must be allowed", e[0], e[1])
		}
	}

	nonTerminal := []runtime.RunPhase{
		runtime.PhaseIntakeRegistered, runtime.PhaseProductReview, runtime.PhaseTechnicalReview,
		runtime.PhaseStartReadiness, runtime.PhaseTopologyAndRoute, runtime.PhaseDevelopmentParallel,
		runtime.PhaseSnapshotReady, runtime.PhasePostReview, runtime.PhaseRepair, runtime.PhaseSliceReady,
		runtime.PhaseMainlineIntegration, runtime.PhaseMergeValidation,
	}
	for _, p := range nonTerminal {
		if !runtime.PhaseCanTransition(p, runtime.PhaseTerminal) {
			t.Errorf("termination closure: %s -> TERMINAL must be allowed", p)
		}
		if !p.Valid() {
			t.Errorf("phase %s must be in the closed set", p)
		}
	}
	if runtime.PhaseTerminal.Valid() != true {
		t.Error("TERMINAL must be a valid phase value")
	}

	illegal := [][2]runtime.RunPhase{
		{runtime.PhaseIntakeRegistered, runtime.PhaseTechnicalReview}, // 跳步
		{runtime.PhaseProductReview, runtime.PhaseIntakeRegistered},   // 回退
		{runtime.PhaseProductReview, runtime.PhaseProductReview},      // 自环
		{runtime.PhaseTerminal, runtime.PhaseIntakeRegistered},        // TERMINAL 无出边
		{runtime.PhaseDevelopmentParallel, runtime.PhasePostReview},   // 未列出点对
		{runtime.PhaseSnapshotReady, runtime.PhaseDevelopmentParallel},
		{runtime.RunPhase("MADE_UP"), runtime.PhaseTerminal}, // 非法枚举值
	}
	for _, e := range illegal {
		if runtime.PhaseCanTransition(e[0], e[1]) {
			t.Errorf("edge %s -> %s must be rejected", e[0], e[1])
		}
		if err := runtime.PhaseTransition(e[0], e[1]); err == nil {
			t.Errorf("PhaseTransition(%s -> %s) must error", e[0], e[1])
		} else {
			wantErrContaining(t, err, "illegal phase transition")
		}
	}

	// 表是稳定 golden（(From, To) 升序）且为副本。
	tbl := runtime.PhaseTransitionTable()
	if !sort.SliceIsSorted(tbl, func(i, j int) bool {
		if tbl[i].From != tbl[j].From {
			return tbl[i].From < tbl[j].From
		}
		return tbl[i].To < tbl[j].To
	}) {
		t.Error("transition table must be sorted by (from, to)")
	}
	first := tbl[0]
	tbl[0].To = runtime.PhaseIntakeRegistered // 篡改副本
	if !runtime.PhaseCanTransition(first.From, first.To) {
		t.Error("static authority must be unaffected by mutating the returned copy")
	}
	if runtime.RunPhase("FREE_FORM").Valid() {
		t.Error("unknown phase value must be invalid")
	}
}

// 用例：TaskKey 稳定键——node 与 step 必填；String() 是 "node/step[/scope]"
// 规范形态且对同一键恒定。
func TestTaskKeyStringAndValidity(t *testing.T) {
	if _, err := runtime.NewTaskKey("", "s.x", ""); err == nil {
		t.Error("empty node must be rejected")
	}
	if _, err := runtime.NewTaskKey("n", "", ""); err == nil {
		t.Error("empty step must be rejected")
	}
	k, err := runtime.NewTaskKey("n1", "s.x", "")
	if err != nil {
		t.Fatalf("NewTaskKey: %v", err)
	}
	if got, want := k.String(), "n1/s.x"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	scoped, err := runtime.NewTaskKey("n1", "s.x", "case-2")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scoped.String(), "n1/s.x/case-2"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if scoped.String() != scoped.String() {
		t.Fatal("String() must be stable for the same key")
	}
	if (runtime.TaskKey{Node: "n", Step: "s"}).Valid() != true {
		t.Error("node+step key must be valid")
	}
	if (runtime.TaskKey{Node: "n"}).Valid() {
		t.Error("key without step must be invalid")
	}
}

// 用例：任务状态机只前进——合法边恰为七条前向边；回退、跳步、自环、
// TERMINAL 出边拒绝；表返回副本。
func TestTaskTransitionTableOnlyMovesForward(t *testing.T) {
	want := map[runtime.TaskEdge]bool{
		{From: runtime.TaskQueued, To: runtime.TaskIssued}:       true,
		{From: runtime.TaskQueued, To: runtime.TaskTerminal}:     true,
		{From: runtime.TaskIssued, To: runtime.TaskRunning}:      true,
		{From: runtime.TaskIssued, To: runtime.TaskTerminal}:     true,
		{From: runtime.TaskRunning, To: runtime.TaskValidating}:  true,
		{From: runtime.TaskRunning, To: runtime.TaskTerminal}:    true,
		{From: runtime.TaskValidating, To: runtime.TaskTerminal}: true,
	}
	tbl := runtime.TaskTransitionTable()
	if len(tbl) != len(want) {
		t.Fatalf("transition table has %d edges, want %d: %v", len(tbl), len(want), tbl)
	}
	for _, e := range tbl {
		if !want[e] {
			t.Errorf("unexpected edge %s -> %s", e.From, e.To)
		}
	}
	for e := range want {
		if !runtime.TaskCanTransition(e.From, e.To) {
			t.Errorf("edge %s -> %s must be allowed", e.From, e.To)
		}
		if err := runtime.TaskTransition(e.From, e.To); err != nil {
			t.Errorf("TaskTransition(%s -> %s): %v", e.From, e.To, err)
		}
	}

	illegal := []runtime.TaskEdge{
		{From: runtime.TaskIssued, To: runtime.TaskQueued},      // 回退
		{From: runtime.TaskQueued, To: runtime.TaskRunning},     // 跳过 ISSUED
		{From: runtime.TaskQueued, To: runtime.TaskValidating},  // 跳步
		{From: runtime.TaskRunning, To: runtime.TaskIssued},     // 回退
		{From: runtime.TaskValidating, To: runtime.TaskRunning}, // 回退
		{From: runtime.TaskTerminal, To: runtime.TaskQueued},    // 终态无出边
		{From: runtime.TaskQueued, To: runtime.TaskQueued},      // 自环
		{From: runtime.TaskIssued, To: runtime.TaskIssued},
	}
	for _, e := range illegal {
		if runtime.TaskCanTransition(e.From, e.To) {
			t.Errorf("edge %s -> %s must be rejected", e.From, e.To)
		}
		err := runtime.TaskTransition(e.From, e.To)
		if err == nil {
			t.Errorf("TaskTransition(%s -> %s) must error", e.From, e.To)
		} else {
			wantErrContaining(t, err, "illegal task transition")
		}
	}

	// 副本：篡改返回表不影响权威。
	first := tbl[0]
	tbl[0].To = runtime.TaskQueued
	if !runtime.TaskCanTransition(first.From, first.To) {
		t.Error("static task authority must be unaffected by mutating the returned copy")
	}
	for _, s := range []runtime.TaskStatus{runtime.TaskQueued, runtime.TaskIssued, runtime.TaskRunning,
		runtime.TaskValidating, runtime.TaskTerminal} {
		if !s.Valid() {
			t.Errorf("status %s must be valid", s)
		}
	}
	if runtime.TaskStatus("PAUSED").Valid() {
		t.Error("unknown status must be invalid")
	}
}

// 用例：Batch 完成状态从成员 task 状态派生——全部 TERMINAL 即完成；任一
// 未终态或状态未知即未完成；空成员批视为完成。
func TestBatchCompleteDerivesFromMemberStatuses(t *testing.T) {
	k1 := runtime.TaskKey{Node: "n1", Step: "s.a"}
	k2 := runtime.TaskKey{Node: "n1", Step: "s.b"}
	empty := runtime.Batch{ID: "b0"}
	if !empty.Complete(nil) {
		t.Error("empty batch must be complete (vacuous truth)")
	}
	b := runtime.Batch{ID: "b1", Tasks: []runtime.TaskKey{k1, k2}}
	if b.Complete(nil) {
		t.Error("batch with unregistered members must not be complete (unknown status is not terminal)")
	}
	if !b.Complete(map[runtime.TaskKey]runtime.TaskStatus{k1: runtime.TaskTerminal, k2: runtime.TaskTerminal}) {
		t.Error("all-terminal batch must be complete")
	}
	if b.Complete(map[runtime.TaskKey]runtime.TaskStatus{k1: runtime.TaskTerminal, k2: runtime.TaskRunning}) {
		t.Error("member RUNNING must keep batch incomplete")
	}
	if b.Complete(map[runtime.TaskKey]runtime.TaskStatus{k1: runtime.TaskTerminal}) {
		t.Error("unknown member status must count as not terminal")
	}
}
