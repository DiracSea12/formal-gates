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
//   - HUMAN_ASK step: complete when a two-phase decision is durably recorded.
//   - HOST_ACTION step: complete when an EXECUTED adapter receipt exists for
//     the operation bound in the compiled payload.
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
		executedOperations := make(map[authoring.OperationID]bool, len(s.HostActionReceipts))
		for _, receipt := range s.HostActionReceipts {
			if receipt.Status == HostActionStatusExecuted && receipt.AdapterOperation != "" {
				executedOperations[receipt.AdapterOperation] = true
			}
		}
		askResolved := len(s.Decisions) > 0
		for i := range compiled.Steps {
			step := &compiled.Steps[i]
			if completedSet[step.Header.ID] {
				continue
			}
			if !s.dependenciesSatisfied(step) {
				continue
			}
			settle := false
			switch payload := step.Payload.(type) {
			case compiler.CompiledHumanAskStep:
				settle = askResolved
			case compiler.CompiledHostActionStep:
				settle = executedOperations[payload.Operation]
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
