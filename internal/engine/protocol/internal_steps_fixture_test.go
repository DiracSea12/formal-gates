package protocol

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
)

// Fixture-only nodes have no state work; they exercise dependent protocol work.
func fixtureInternalSteps() map[authoring.HandlerID]InternalStep {
	bindings := make(map[authoring.HandlerID]InternalStep)
	for _, id := range []authoring.HandlerID{"engine.test.prep", "engine.test.parse", "engine.cost.parse", "engine.funnel.parse", "engine.fan.slice", "engine.fan.join", "engine.report.cost"} {
		bindings[id] = InternalStep{Runner: authoring.RunnerEngineLocal, Run: func(*State, compiler.CompiledStep) (bool, error) { return true, nil }}
	}
	return bindings
}
