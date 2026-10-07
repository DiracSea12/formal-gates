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
		"engine.entry.parse":      {Runner: authoring.RunnerEngineLocal, Run: entryRegistered},
		"engine.entry.persist":    {Runner: authoring.RunnerDurableActivity, Run: entryRegistered},
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

// Entry's parsed and persisted state is the current transaction's State value.
func entryRegistered(s *State, _ compiler.CompiledStep) (bool, error) {
	return s.RunProvider != "", nil
}

func childrenCreated(s *State, step compiler.CompiledStep) (bool, error) {
	return s.creationStepSettled(step.Header.ID), nil
}
