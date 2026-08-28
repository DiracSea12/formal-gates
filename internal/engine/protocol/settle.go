package protocol

import (
	"fmt"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
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
//   - AGENT steps are never settled here — only a PASS worker result through
//     completeResult completes them.
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
			if !s.dependenciesSatisfied(step) {
				continue
			}
			settle := false
			switch step.Payload.(type) {
			case compiler.CompiledHumanAskStep:
				settle = s.hasDecisionForStep(step.Header.ID)
			case compiler.CompiledHostActionStep:
				settle = s.hasSettledHostActionForStep(step.Header.ID)
			case compiler.CompiledLocalStep, compiler.CompiledDurableStep, compiler.CompiledParallelStep:
				settle = true
			case compiler.CompiledAgentStep:
				settle = false
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

func (s *State) nextHumanAskStep(compiled *compiler.CompiledDefinition) authoring.StepID {
	for i := range compiled.Steps {
		step := &compiled.Steps[i]
		if s.stepCompleted(step.Header.ID) || !s.dependenciesSatisfied(step) {
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
		if s.stepCompleted(step.Header.ID) || !s.dependenciesSatisfied(step) {
			continue
		}
		payload, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || payload.Operation != operation || s.hostActionStepBound(step.Header.ID) {
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

func (s *State) hostActionStepBound(stepID authoring.StepID) bool {
	for _, intent := range s.PendingHostActions {
		if intent.Step == stepID {
			return true
		}
	}
	for _, receipt := range s.HostActionReceipts {
		if receipt.Step == stepID {
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

func (s *State) dependenciesSatisfied(step *compiler.CompiledStep) bool {
	for _, dep := range step.Header.Dependencies {
		done := false
		for _, id := range s.Completed {
			if id == dep {
				done = true
				break
			}
		}
		if !done {
			return false
		}
	}
	return true
}
