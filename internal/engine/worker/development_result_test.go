package worker

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestDevelopmentResultRejectsExecutionClaims(t *testing.T) {
	batch := protocol.DevelopmentBatch{ID: "command", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "Public behavior"}}, Checks: []protocol.DevelopmentCheck{{ID: "actual", Command: "false"}}}
	for _, role := range []string{"engine.development.batch", "engine.repair"} {
		input := protocol.WorkerInput{Handler: role, Provider: "host", Pending: protocol.PendingAction{ActionID: "edit"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		properties := schema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)["development"].(map[string]any)["properties"].(map[string]any)
		if properties["build"] != nil || properties["tests"] != nil || properties["completion"].(map[string]any)["properties"].(map[string]any)["checks"] != nil {
			t.Fatal("model execution claims remain in the contract")
		}
		for _, claim := range []string{"none", "build", "tests", "checks"} {
			development := map[string]any{"paths": []string{"command.go"}, "summary": "Implemented public behavior", "completion": map[string]any{"dod": map[string]string{"behavior": "Public behavior implemented"}, "handoff": "Ready for program checks"}}
			if claim == "checks" {
				development["completion"].(map[string]any)[claim] = map[string]string{"actual": "passed"}
			} else if claim != "none" {
				development[claim] = []string{"passed"}
			}
			raw, err := json.Marshal(map[string]any{"outcome": "PASS", "validation": map[string]any{"status": "PASS", "development": development}})
			if err != nil {
				t.Fatal(err)
			}
			event, err := DecodeResult(input, "result", raw, "sha256:original")
			if claim == "none" {
				if err != nil || event.Result.Validation.Development.Completion.DoD["behavior"] != "Public behavior implemented" || event.Result.Validation.Binding != input.Validation.Binding {
					t.Fatalf("semantic DoD/handoff/binding was lost %v %#v", err, event)
				}
			} else if err == nil {
				t.Fatalf("model %s claim was admitted for %s", claim, role)
			}
		}
	}
}
