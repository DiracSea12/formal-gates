package protocol

import (
	"fmt"
	"maps"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// InternalStep runs a deterministic state transformation or checks an existing
// business fact inside the engine's Store transaction. Run must not perform
// external effects: those belong to HostAction intent/receipt handling. A false
// result must leave State unchanged and leaves the step open; an error rejects
// the whole transaction. State and CompiledStep carry run identity, business facts, inputs and dependency identity.
// Local and Durable share this path during ordinary commits and recovery.
type InternalStep struct {
	Runner authoring.RunnerKind
	Run    func(*State, compiler.CompiledStep) (bool, error)
}

const reviewResultPredicate authoring.PredicateID = "pred.review.post"

// ValidateDefinitionConditions binds the closed condition references to the
// typed worker-result contract; other condition implementations are unavailable.
func ValidateDefinitionConditions(cd *compiler.CompiledDefinition) error {
	for _, step := range cd.Steps {
		_, agent := step.Payload.(compiler.CompiledAgentStep)
		if len(step.IO.Preconditions) != 0 || !agent && len(step.IO.Postconditions) != 0 || agent && len(step.IO.Postconditions) == 0 {
			return &compiler.Error{Class: authoring.FailureBlockedBug, Msg: fmt.Sprintf("protocol: step %s predicate position has no implementation", step.Header.ID)}
		}
		for _, ref := range step.IO.Postconditions {
			if ref.ID != reviewResultPredicate || ref.Negated {
				return &compiler.Error{Class: authoring.FailureBlockedBug, Msg: fmt.Sprintf("protocol: step %s predicate %q (negated=%t) has no implementation", step.Header.ID, ref.ID, ref.Negated)}
			}
		}
	}
	return nil
}

func internalStepHandler(step compiler.CompiledStep) authoring.HandlerID {
	switch payload := step.Payload.(type) {
	case compiler.CompiledLocalStep:
		return payload.Handler
	case compiler.CompiledDurableStep:
		return payload.Handler
	}
	return ""
}

func bindInternalSteps(cd *compiler.CompiledDefinition, registry *compiler.Registry, supplied map[authoring.HandlerID]InternalStep) (map[authoring.HandlerID]InternalStep, error) {
	bindings := map[authoring.HandlerID]InternalStep{
		"engine.entry.registered": {Runner: authoring.RunnerEngineLocal, Run: entryRegistered},
		"engine.children.prepare": {Runner: authoring.RunnerEngineLocal, Run: childrenCreated},
		"engine.children.publish": {Runner: authoring.RunnerEngineLocal, Run: childrenCreated},
	}
	maps.Copy(bindings, supplied)
	for _, step := range cd.Steps {
		id := internalStepHandler(step)
		if id == "" {
			continue
		}
		binding, ok := bindings[id]
		if !ok || binding.Run == nil {
			return nil, fmt.Errorf("protocol: internal handler %q has no implementation", id)
		}
		runner, err := registry.ResolveHandler(id)
		if err != nil {
			return nil, err
		}
		if binding.Runner != runner || binding.Runner != step.Header.Runner {
			return nil, fmt.Errorf("protocol: internal handler %q runner %q does not match %q", id, binding.Runner, step.Header.Runner)
		}
	}
	return bindings, nil
}

// Entry registration is the current transaction's initialized State value.
func entryRegistered(s *State, _ compiler.CompiledStep) (bool, error) {
	return s.RunProvider != "", nil
}

func childrenCreated(s *State, step compiler.CompiledStep) (bool, error) {
	return s.creationStepSettled(step.Header.ID), nil
}
