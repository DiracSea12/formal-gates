package worker

import (
	"encoding/json"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
	"reflect"
	"strings"
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
		{"engine.development.batch", []string{"binding", "status", "development"}, protocol.ValidationResult{Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "implemented behavior", Build: []string{"go build"}, Tests: []string{"go test"}, Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"behavior": "verified"}, Checks: map[string]string{"build": "passed"}, Handoff: "app.go"}}}},
		{"engine.repair", []string{"binding", "status", "development", "granularityReview"}, protocol.ValidationResult{GranularityReview: &protocol.GranularityReview{}}},
		{"engine.merge.resolve", []string{"binding", "status", "resolution"}, protocol.ValidationResult{Resolution: &protocol.ConflictResolution{}}},
		{"engine.qa.design", []string{"binding", "status", "development", "manifest", "checkpoint"}, protocol.ValidationResult{Manifest: &coverage.AcceptanceManifest{}, Checkpoint: true}},
		{"engine.qa.review.rationale", []string{"binding", "status", "review", "suggestions"}, protocol.ValidationResult{Review: &coverage.ReviewResult{}, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "observed optional improvement"}}}},
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

func TestReviewMaterialCarriesClosedVerificationContract(t *testing.T) {
	catalog := prompts.PromptCatalog{Base: "Independent review", Actions: []prompts.PromptDefinition{{ID: "product-review", Content: "Product review"}, {ID: "start-readiness", Content: "Readiness review"}}}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		t.Run(string(action), func(t *testing.T) {
			handler := "engine.review.worker"
			if action == review.StartReadiness {
				handler = "engine.readiness.plan"
			}
			input := protocol.WorkerInput{RunID: "run", Provider: "host", Handler: handler, Pending: protocol.PendingAction{ActionID: "review", AttemptID: "attempt"}, Review: &protocol.WorkerReviewInput{Action: action, RequirementRevision: "confirmed"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed requirement"}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			wanted := review.RequiredVerificationChecks()
			if !reflect.DeepEqual(material.ResultTemplate.Review.Verification.Checks, wanted) {
				t.Fatal("template lost closed check IDs")
			}
			fields := material.ResultSchema["properties"].(map[string]any)["review"].(map[string]any)["properties"].(map[string]any)
			verification := fields["verification"].(map[string]any)
			expected := []any{}
			for _, id := range wanted {
				expected = append(expected, id)
				if !strings.Contains(material.Prompt, id) {
					t.Fatalf("prompt omits %s", id)
				}
			}
			got := verification["properties"].(map[string]any)["checks"].(map[string]any)["items"].(map[string]any)["enum"]
			if !reflect.DeepEqual(got, expected) || !reflect.DeepEqual(verification["required"], []any{"checks", "evidence"}) {
				t.Fatal("schema lost exact verification contract")
			}
			if !strings.Contains(material.Prompt, "narrative support in verification.evidence") || material.ResultTemplate.Review.Verification.Evidence != "" {
				t.Fatal("template invents a verification verdict")
			}
			wanted[0] = "unrecognized prose"
			if review.RequiredVerificationChecks()[0] == wanted[0] {
				t.Fatal("consumer mutated owner check set")
			}
		})
	}
}
