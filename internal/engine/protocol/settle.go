package protocol

import (
	"fmt"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
)

// settleFrontierSteps executes eligible transaction-local handlers and settles
// external boundaries from their durable facts. CompleteStep owns completion,
// version binding and dependency order; parallel steps are scheduling markers.
func (s *State) settleFrontierSteps(compiled *compiler.CompiledDefinition, bindings map[authoring.HandlerID]InternalStep) error {
	// Business completion is frozen by the same control facts used by write
	// admission. Control requests and signed external effects retain their own
	// reconciliation paths; they do not authorize ordinary frontier execution.
	if s.requirementChangePending() || s.abortRequested() {
		return nil
	}
	for pass := 0; pass < len(compiled.Steps); pass++ {
		progressed := false
		var view *decision.State
		completedSet := make(map[authoring.StepID]bool, len(s.Completed))
		for _, id := range s.Completed {
			completedSet[id] = true
		}
		for i := range compiled.Steps {
			step := &compiled.Steps[i]
			if completedSet[step.Header.ID] {
				continue
			}
			if len(step.Header.Dependencies) != 0 && view == nil {
				current := s.decisionView(compiled)
				view = &current
			}
			if !dependenciesSatisfied(step, compiled, view) {
				continue
			}
			settle := false
			switch payload := step.Payload.(type) {
			case compiler.CompiledHumanAskStep:
				settle = s.hasDecisionForStep(step.Header.ID)
			case compiler.CompiledHostActionStep:
				if step.Header.ID == "children.workspace" {
					settle = s.creationStepSettled(step.Header.ID)
				} else if payload.Operation == "op.workspace" || payload.Operation == "op.cleanup" || payload.Operation == "op.check" {
					settle = s.DeliveryComplete(compiled) && (payload.Operation != "op.cleanup" || s.SliceChild != nil || s.ResourcesCleaned())
				} else if payload.Operation == "op.candidate" {
					continue
				}
				if !nativeOperation(payload.Operation) {
					settle = s.hasSettledHostActionForStep(step.Header.ID)
				}
			case compiler.CompiledLocalStep, compiler.CompiledDurableStep:
				binding := bindings[internalStepHandler(*step)]
				var err error
				settle, err = binding.Run(s, *step)
				view = nil
				if err != nil {
					return fmt.Errorf("protocol: execute step %s: %w", step.Header.ID, err)
				}
			case compiler.CompiledParallelStep:
				settle = true
			case compiler.CompiledAgentStep:
				if node := flowStepNode(*step); node != "" && node != "repair" && !s.FlowNodeEnabled(node) {
					settle = true
				} else if payload.Handler == "engine.development.plan.v1" {
					settle = s.readinessPreparationReady()
				} else if validationHandler(*step) != "" {
					settle = s.DeliveryComplete(compiled)
				} else {
					settle = s.carrierLatched(payload)
				}
			default:
				return fmt.Errorf("protocol: settle: unknown compiled payload %T", step.Payload)
			}
			if !settle {
				continue
			}
			if err := s.CompleteStep(step.Header.ID, compiled); err != nil {
				return fmt.Errorf("protocol: settle step %s: %w", step.Header.ID, err)
			}
			if view != nil {
				view.Completed = s.Completed
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
// 完成。child 不自行跑审查轮，其载体步的完成事实来自继承结论（判据与
// effectivePass 的 child 分支同构：inheritedReviewPass）。
func (s *State) carrierLatched(payload compiler.CompiledAgentStep) bool {
	series, ok := s.ReviewSeries.Series(review.Action(payload.ReviewSeries))
	if !ok {
		return false
	}
	if s.SliceChild != nil {
		return s.inheritedReviewPass(series)
	}
	return series.FirstPass
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
	var view *decision.State
	for i := range compiled.Steps {
		step := &compiled.Steps[i]
		if s.stepCompleted(step.Header.ID) {
			continue
		}
		if _, ok := step.Payload.(compiler.CompiledHumanAskStep); !ok || s.askStepBound(step.Header.ID) {
			continue
		}
		if len(step.Header.Dependencies) != 0 && view == nil {
			current := s.decisionView(compiled)
			view = &current
		}
		if !dependenciesSatisfied(step, compiled, view) {
			continue
		}
		return step.Header.ID
	}
	return ""
}

func (s *State) nextHostActionStep(compiled *compiler.CompiledDefinition, operation authoring.OperationID) authoring.StepID {
	var view *decision.State
	for i := range compiled.Steps {
		step := &compiled.Steps[i]
		if s.stepCompleted(step.Header.ID) {
			continue
		}
		payload, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || payload.Operation != operation || s.hostActionStepBound(step.Header.ID, payload.Operation != "op.candidate" && !nativeOperation(payload.Operation)) {
			continue
		}
		if len(step.Header.Dependencies) != 0 && view == nil {
			current := s.decisionView(compiled)
			view = &current
		}
		if !dependenciesSatisfied(step, compiled, view) {
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

func dependenciesSatisfied(step *compiler.CompiledStep, compiled *compiler.CompiledDefinition, view *decision.State) bool {
	for _, dep := range step.Header.Dependencies {
		if !view.StepSatisfied(dep, compiled) {
			return false
		}
	}
	return true
}
