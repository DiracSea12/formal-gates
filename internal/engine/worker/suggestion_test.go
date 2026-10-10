package worker

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestSuggestionJudgmentsAssembleOnlyIssuedIdentities(t *testing.T) {
	input := observationWorkerInput()
	input.Validation.Observation = &protocol.ObservationInput{Suggestions: []protocol.ObservationSuggestion{{Mode: coverage.ReviewBlackbox, Suggestion: protocol.QASuggestion{ID: "public", Severity: "P3", Message: "Optional wording"}, Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "normal"}}}}, {Mode: coverage.ReviewWhitebox, Suggestion: protocol.QASuggestion{ID: "test", Severity: "P3", Message: "Optional explanation"}, Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "white"}}}}}}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	field := properties["suggestions"].(map[string]any)
	if field["minItems"] != 2 || field["maxItems"] != 2 || field["items"] != false {
		t.Fatal("suggestions are not the exact issued tuple")
	}
	for _, raw := range field["prefixItems"].([]any) {
		item := raw.(map[string]any)["properties"].(map[string]any)
		for _, mechanical := range []string{"suggestionId", "kind", "mode", "subject", "binding", "verdict", "status"} {
			if item[mechanical] != nil {
				t.Fatalf("model still copies %s", mechanical)
			}
		}
	}
	judgment := ObservationJudgments{Findings: []FindingJudgment{}, Scopes: []ScopeJudgment{}, GateImpacts: []ImpactJudgment{}, Suggestions: []SuggestionJudgment{{Decision: "DEFER", Reason: "Optional wording is outside the confirmed acceptance", AffectedCases: []string{}}, {Decision: "ACCEPT", Reason: "Change only the executable preparation", AffectedCases: []string{"white"}}}}
	raw, _ := json.Marshal(judgment)
	event, err := DecodeResult(input, "result", raw, "original-output")
	if err != nil {
		t.Fatal(err)
	}
	observations := event.Result.Validation.Observations
	if len(observations) != 2 || observations[0].Validation.Kind != "SUGGESTION" || observations[0].Validation.Mode != coverage.ReviewBlackbox || observations[0].Validation.SuggestionID != "public" || observations[1].Validation.Mode != coverage.ReviewWhitebox || observations[1].Validation.SuggestionID != "test" || event.Result.PayloadDigest != "original-output" {
		t.Fatal("original semantic choices lost their issued identities or raw evidence")
	}
	judgment.Suggestions = judgment.Suggestions[:1]
	raw, _ = json.Marshal(judgment)
	if _, err := DecodeResult(input, "missing", raw, "original-output"); err == nil {
		t.Fatal("missing suggested duty admitted")
	}
	if _, err := DecodeResult(input, "copied", []byte(`{"findings":[],"scopes":[],"gateImpacts":[],"suggestions":[],"suggestionId":"public"}`), "original-output"); err == nil {
		t.Fatal("model-copied identity admitted")
	}
}
