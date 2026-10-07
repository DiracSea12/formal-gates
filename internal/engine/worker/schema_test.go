package worker

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func TestResultSchemaUsesRoleWireContract(t *testing.T) {
	binding := protocol.WorkBinding{Task: runtime.TaskKey{Node: "candidate", Step: "work"}, RequirementRevision: "confirmed-content", Generation: 1}
	roles := []struct {
		handler string
		fields  []string
		result  protocol.ValidationResult
	}{
		{"engine.development.batch", []string{"binding", "status", "development", "granularityReview"}, protocol.ValidationResult{Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "implemented behavior", Build: []string{"go build"}, Tests: []string{"go test"}, Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"behavior": "verified"}, Checks: map[string]string{"build": "passed"}, Handoff: "app.go"}}}},
		{"engine.repair", []string{"binding", "status", "development", "granularityReview"}, protocol.ValidationResult{GranularityReview: &protocol.GranularityReview{}}},
		{"engine.merge.resolve", []string{"binding", "status", "resolution"}, protocol.ValidationResult{Resolution: &protocol.ConflictResolution{}}},
		{"engine.qa.design", []string{"binding", "status", "development", "manifest", "suggestions"}, protocol.ValidationResult{Manifest: &coverage.AcceptanceManifest{}}},
		{"engine.qa.review.rationale", []string{"binding", "status", "review"}, protocol.ValidationResult{Review: &coverage.ReviewResult{}}},
		{"engine.qa.execution", []string{"binding", "status", "execution", "tool", "environment", "checkpoint", "findings"}, protocol.ValidationResult{Execution: &coverage.ExecutionReport{}, Tool: "go test", Environment: "isolated candidate"}},
		{"engine.gate", []string{"binding", "status", "findings"}, protocol.ValidationResult{Findings: []review.Finding{{Severity: "P3", Message: "evidence"}}}},
	}
	for _, role := range roles {
		t.Run(role.handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: role.handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued-action"}, Validation: &protocol.ValidationInput{Binding: binding}}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			outer := schema["properties"].(map[string]any)
			if _, present := outer["payloadDigest"]; present {
				t.Fatal("source digest is a worker-controlled schema field")
			}
			fields := outer["validation"].(map[string]any)["properties"].(map[string]any)
			if len(fields) != len(role.fields) {
				t.Fatalf("unrelated role union leaked: %v", fields)
			}
			for _, field := range role.fields {
				if fields[field] == nil {
					t.Fatalf("role field %s omitted", field)
				}
			}
			if fields["binding"].(map[string]any)["properties"].(map[string]any)["task"].(map[string]any)["type"] != "string" {
				t.Fatal("TaskKey does not use its text wire identity")
			}
			role.result.Binding, role.result.Status = binding, "PASS"
			payload := protocol.WorkerResultPayload{ActionID: input.Pending.ActionID, Provider: input.Provider, Outcome: protocol.OutcomePass, Validation: &role.result}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var sample map[string]json.RawMessage
			if err := json.Unmarshal(body, &sample); err != nil {
				t.Fatal(err)
			}
			if sample["actionId"] == nil || sample["ActionID"] != nil || sample["payloadDigest"] != nil {
				t.Fatal("actual payload encoding differs from source-owned schema")
			}
			event, err := DecodeResult(input, "result", body, "sha256:original-native-bytes")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(event.Result.Validation, &role.result) || event.Result.PayloadDigest != "sha256:original-native-bytes" {
				t.Fatal("complete typed role result changed during transport")
			}
			t.Logf("actual role wire: %s", body)
		})
	}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		input := protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "review"}, Review: &protocol.WorkerReviewInput{Action: action}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		if schema["properties"].(map[string]any)["validation"] != nil {
			t.Fatal("review exposes the validation union")
		}
	}
}
