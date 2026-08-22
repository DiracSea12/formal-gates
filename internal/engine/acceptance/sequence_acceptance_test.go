// sequence_acceptance_test.go 覆盖批次 3 验收的 step 乱序/遗漏/重复判据，
// 比批 2b 更完整：用真实 definition.Workflow() 的编译产物驱动
// State.CompleteStep——合法完成序列 golden + 三类非法序列全拒 + 对 golden
// 每个前缀的穷举走查（每个未满足依赖的步骤、每个已完成步骤、每个未知步骤
// 在每个前缀上都必须被拒，且拒绝不得改变状态）。
package acceptance_test

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
)

// workflowGoldenOrder 是一个合法完成序列（拓扑序；sibling 先后任意，此处
// 取编译 ordinal 的稳定参考序）：parse → persist → split → slice →
// transport → join → ask → review → cost。
var workflowGoldenOrder = []string{
	"entry.parse", "entry.persist", "fan.split", "fan.slice",
	"fan.transport", "fan.join", "ask.decide", "review.worker", "report.cost",
}

func newRunState(t *testing.T, cd *compiler.CompiledDefinition) *decision.State {
	t.Helper()
	s, err := decision.NewState(cd.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	return s
}

// replay 重放 golden 的前 k 步，返回独立状态（穷举走查的每个探针都在
// 干净状态上执行，探针之间互不污染）。
func replay(t *testing.T, cd *compiler.CompiledDefinition, k int) *decision.State {
	t.Helper()
	s := newRunState(t, cd)
	for _, id := range workflowGoldenOrder[:k] {
		if err := s.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatalf("replay %q: %v", id, err)
		}
	}
	return s
}

// TestAcceptanceLegalCompletionSequenceGolden：golden 序列逐步合法完成；全部
// 完成后 Decide 投影 COMPLETE、frontier 清空。
func TestAcceptanceLegalCompletionSequenceGolden(t *testing.T) {
	cd, _, _ := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))
	s := newRunState(t, cd)
	for i, id := range workflowGoldenOrder {
		if err := s.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatalf("step %d %q: %v", i, id, err)
		}
		if len(s.Completed) != i+1 {
			t.Fatalf("after %q: completed = %d, want %d", id, len(s.Completed), i+1)
		}
	}
	plan, err := decision.Decide(s, decision.Observation{}, cd)
	if err != nil {
		t.Fatalf("decide at terminal state: %v", err)
	}
	if plan.Next.Kind != decision.KindComplete {
		t.Fatalf("next kind = %s, want COMPLETE after all steps done", plan.Next.Kind)
	}
	if len(plan.Frontier) != 0 {
		t.Fatalf("frontier after completion = %v, want empty", plan.Frontier)
	}
}

// TestAcceptanceIllegalCompletionSequencesRejected：三类非法序列全拒。
func TestAcceptanceIllegalCompletionSequencesRejected(t *testing.T) {
	cd, _, _ := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))

	t.Run("out-of-order", func(t *testing.T) {
		s := newRunState(t, cd)
		err := s.CompleteStep("entry.persist", cd)
		wantErr(t, err, `dependency "entry.parse" not completed (out-of-order or skipped)`)
		if len(s.Completed) != 0 {
			t.Fatalf("rejected completion mutated state: %v", s.Completed)
		}
	})

	t.Run("skipped dependency", func(t *testing.T) {
		// 合法推进到只差 ask.decide 的位置，report.cost 仍必须被拒（遗漏依赖）。
		s := replay(t, cd, 6) // parse..fan.join 完成，report.cost 尚差 ask.decide
		err := s.CompleteStep("report.cost", cd)
		wantErr(t, err, `dependency "ask.decide" not completed`)
		if len(s.Completed) != 6 {
			t.Fatalf("rejected completion mutated state: %d completed", len(s.Completed))
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		s := replay(t, cd, 1)
		err := s.CompleteStep("entry.parse", cd)
		wantErr(t, err, `already completed (duplicate)`)
		if len(s.Completed) != 1 {
			t.Fatalf("rejected completion mutated state: %d completed", len(s.Completed))
		}
	})

	// 穷举走查：golden 每个前缀 k 上，对定义中每一步骤探针一次——
	//   - 已完成 → 重复拒绝；
	//   - 依赖未全满足 → 乱序/遗漏拒绝（错误点名缺失依赖）；
	//   - 其余（依赖满足且未完成）→ 必须可完成；
	// 未知步骤在每个前缀上恒拒；所有拒绝都不改变状态。
	t.Run("exhaustive walk over golden prefixes", func(t *testing.T) {
		deps := map[authoring.StepID][]authoring.StepID{}
		for _, cs := range cd.Steps {
			deps[cs.Header.ID] = cs.Header.Dependencies
		}
		for k := 0; k < len(workflowGoldenOrder); k++ {
			done := map[authoring.StepID]bool{}
			for _, id := range workflowGoldenOrder[:k] {
				done[authoring.StepID(id)] = true
			}
			for _, cs := range cd.Steps {
				sid := cs.Header.ID
				s := replay(t, cd, k)
				err := s.CompleteStep(sid, cd)
				if done[sid] {
					if err == nil || !strings.Contains(err.Error(), "duplicate") {
						t.Fatalf("prefix %d: re-complete %q err = %v, want duplicate rejection", k, sid, err)
					}
				} else {
					var missing []string
					for _, dep := range deps[sid] {
						if !done[dep] {
							missing = append(missing, string(dep))
						}
					}
					if len(missing) > 0 {
						if err == nil {
							t.Fatalf("prefix %d: %q completed with unmet deps %v", k, sid, missing)
						}
						// CompleteStep 点名第一个未满足依赖（依赖集合有序）；
						// 断言错误至少命中缺失依赖之一且归类乱序/遗漏。
						named := false
						for _, dep := range missing {
							if strings.Contains(err.Error(), dep) {
								named = true
							}
						}
						if !named || !strings.Contains(err.Error(), "out-of-order") {
							t.Fatalf("prefix %d: %q rejection %q must name a missing dependency of %v as out-of-order", k, sid, err, missing)
						}
					} else if err != nil {
						t.Fatalf("prefix %d: dep-satisfied %q rejected: %v", k, sid, err)
					}
				}
				if err != nil && len(s.Completed) != k {
					t.Fatalf("prefix %d: rejected probe on %q changed state (%d -> %d)", k, sid, k, len(s.Completed))
				}
			}
			// 未知步骤在前缀 k 上恒拒。
			s := replay(t, cd, k)
			wantErr(t, s.CompleteStep("no.such.step", cd), "not in definition")
			if len(s.Completed) != k {
				t.Fatalf("prefix %d: unknown-step probe changed state", k)
			}
		}
	})
}
