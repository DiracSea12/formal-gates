package protocol

import (
	"fmt"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/review"
)

// settleFrontierSteps completes every frontier step whose external boundary
// has just been satisfied by a durable protocol fact, then mechanically
// completes engine-internal steps (LOCAL / DURABLE / PARALLEL) that became
// eligible. The canonical rule set:
//
//   - HUMAN_ASK step: complete when its own two-phase decision is durably
//     recorded.
//   - HOST_ACTION step: complete when its own intent has an EXECUTED or
//     RECONCILED receipt.
//   - AGENT steps: a step carrying a review-series mark (carrier step)
//     completes iff its series' first-pass latch is set—a monotonic fact, so
//     sibling carriers of a concluded series complete in dependency order
//     without their own dispatches and a later PASS transaction idempotently
//     skips already-completed carriers. Non-carrier agent steps are never
//     settled here: their completion fact is a recorded PASS result and
//     completeResult completes them directly.
//   - LOCAL / DURABLE steps execute inside the engine's controller boundary;
//     once eligible they complete mechanically in dependency order.
//   - PARALLEL steps are pure scheduling markers; once eligible they complete
//     immediately and unlock their children.
//
// Every completion still goes through CompleteStep, so version binding,
// duplicate, and dependency-order checks remain authoritative.
func (s *State) settleFrontierSteps(compiled *compiler.CompiledDefinition) error {
	for pass := 0; pass < len(compiled.Steps); pass++ {
		progressed := false
		completedSet := make(map[authoring.StepID]bool, len(s.Completed))
		for _, id := range s.Completed {
			completedSet[id] = true
		}
		for i := range compiled.Steps {
			step := &compiled.Steps[i]
			if completedSet[step.Header.ID] {
				continue
			}
			if !s.dependenciesSatisfied(step, compiled) {
				continue
			}
			settle := false
			switch payload := step.Payload.(type) {
			case compiler.CompiledHumanAskStep:
				settle = s.hasDecisionForStep(step.Header.ID)
			case compiler.CompiledHostActionStep:
				if payload.Operation == "op.candidate" {
					continue
				}
				settle = s.hasSettledHostActionForStep(step.Header.ID)
			case compiler.CompiledLocalStep, compiler.CompiledDurableStep, compiler.CompiledParallelStep:
				settle = true
			case compiler.CompiledAgentStep:
				settle = s.carrierLatched(payload)
			default:
				return fmt.Errorf("protocol: settle: unknown compiled payload %T", step.Payload)
			}
			if !settle {
				continue
			}
			if err := s.CompleteStep(step.Header.ID, compiled); err != nil {
				return fmt.Errorf("protocol: settle step %s: %w", step.Header.ID, err)
			}
			completedSet[step.Header.ID] = true
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return nil
}

// carrierLatched 报告载体步（携带账本审查系列标记的 agent 步）的完成条
// 件：所属系列首过闩已置位（首次 effective PASS 的同一事务内由内核
// refresh 写入）。闩单调：置位后纠正/重走只改 effective 投影，不回退步
// 完成。非载体 agent 步不经 settle 完成——其完成事实＝入账 PASS 结果，
// 由 completeResult 直接完成。
func (s *State) carrierLatched(payload compiler.CompiledAgentStep) bool {
	series, ok := s.ReviewSeries.Series(review.Action(payload.ReviewSeries))
	return ok && series.FirstPass
}

// passResultCompletesStep 报告一条入账 PASS 结果是否完成其步骤：非载体
// agent 步直接完成；载体步完成 ⟺ 所属系列首过闩——语义 FAIL 的证据不置
// 闩，载体步保持开放等待下一轮。已完成的步幂等跳过（后续轮次的 PASS 不
// 重复完成）。
func (s *State) passResultCompletesStep(stepID authoring.StepID, seriesOf func(authoring.StepID) review.Action) bool {
	if s.stepCompleted(stepID) {
		return false
	}
	series, ok := s.ReviewSeries.Series(seriesOf(stepID))
	if !ok {
		return true
	}
	return series.FirstPass
}

func (s *State) nextHumanAskStep(compiled *compiler.CompiledDefinition) authoring.StepID {
	for i := range compiled.Steps {
		step := &compiled.Steps[i]
		if s.stepCompleted(step.Header.ID) || !s.dependenciesSatisfied(step, compiled) {
			continue
		}
		if _, ok := step.Payload.(compiler.CompiledHumanAskStep); !ok || s.askStepBound(step.Header.ID) {
			continue
		}
		return step.Header.ID
	}
	return ""
}

func (s *State) nextHostActionStep(compiled *compiler.CompiledDefinition, operation authoring.OperationID) authoring.StepID {
	for i := range compiled.Steps {
		step := &compiled.Steps[i]
		if s.stepCompleted(step.Header.ID) || !s.dependenciesSatisfied(step, compiled) {
			continue
		}
		payload, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || payload.Operation != operation || s.hostActionStepBound(step.Header.ID, payload.Handler != "engine.candidate") {
			continue
		}
		return step.Header.ID
	}
	return ""
}

func (s *State) askStepBound(stepID authoring.StepID) bool {
	for _, ask := range s.PendingAsks {
		if ask.Step == stepID {
			return true
		}
	}
	return false
}

func (s *State) hostActionStepBound(stepID authoring.StepID, includeReceipts bool) bool {
	for _, intent := range s.PendingHostActions {
		if intent.Step == stepID {
			return true
		}
	}
	for _, receipt := range s.HostActionReceipts {
		if includeReceipts && receipt.Step == stepID {
			return true
		}
	}
	return false
}

func (s *State) hasDecisionForStep(stepID authoring.StepID) bool {
	for _, recorded := range s.Decisions {
		if recorded.Step == stepID {
			return true
		}
	}
	return false
}

func (s *State) hasSettledHostActionForStep(stepID authoring.StepID) bool {
	for _, receipt := range s.HostActionReceipts {
		if receipt.Step == stepID && (receipt.Status == HostActionStatusExecuted || receipt.Status == HostActionStatusReconciled) {
			return true
		}
	}
	return false
}

func (s *State) stepCompleted(stepID authoring.StepID) bool {
	for _, completed := range s.Completed {
		if completed == stepID {
			return true
		}
	}
	return false
}

func (s *State) dependenciesSatisfied(step *compiler.CompiledStep, compiled *compiler.CompiledDefinition) bool {
	view := s.decisionView(compiled)
	for _, dep := range step.Header.Dependencies {
		if !view.StepSatisfied(dep, compiled) {
			return false
		}
	}
	return true
}
