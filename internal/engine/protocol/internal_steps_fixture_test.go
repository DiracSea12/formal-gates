package protocol

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// Entry nodes check registered state; other fixture nodes exercise dependent protocol work.
func fixtureInternalSteps() map[authoring.HandlerID]InternalStep {
	bindings := map[authoring.HandlerID]InternalStep{
		"engine.entry.parse":   {Runner: authoring.RunnerEngineLocal, Run: entryRegistered},
		"engine.entry.persist": {Runner: authoring.RunnerDurableActivity, Run: entryRegistered},
	}
	for _, id := range []authoring.HandlerID{"engine.test.prep", "engine.test.parse", "engine.cost.parse", "engine.funnel.parse", "engine.fan.slice", "engine.fan.join", "engine.report.cost"} {
		bindings[id] = InternalStep{Runner: authoring.RunnerEngineLocal, Run: func(*State, compiler.CompiledStep) (bool, error) { return true, nil }}
	}
	return bindings
}
