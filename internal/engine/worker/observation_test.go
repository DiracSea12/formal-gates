package worker

import (
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func observationWorkerInput() protocol.WorkerInput {
	return protocol.WorkerInput{RunID: "run", Provider: "host", Handler: "engine.validation.observation", Pending: protocol.PendingAction{ActionID: "semantic", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Observation: &protocol.ObservationInput{WaveID: "wave", Candidate: coverage.ValidationCandidate{Identity: "candidate"}, Findings: []protocol.ObservationFinding{{Finding: protocol.ValidationFinding{ID: "first"}}, {Finding: protocol.ValidationFinding{ID: "second"}}}, Scopes: []protocol.ObservationScope{{Mode: coverage.ReviewBlackbox, ApprovedCases: []string{"broken", "stable"}, MustExecute: []string{"broken"}}}, GateImpacts: []protocol.ObservationGateImpact{{Gate: "quality"}}}}}
}
func TestObservationJudgmentAssemblesOrderedIssuedIdentities(t *testing.T) {
	input := observationWorkerInput()
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	for name, count := range map[string]int{"findings": 2, "scopes": 1, "gateImpacts": 1} {
		field := properties[name].(map[string]any)
		if field["minItems"] != count || field["maxItems"] != count || field["items"] != false {
			t.Fatalf("%s is not the original ordered tuple", name)
		}
		for _, raw := range field["prefixItems"].([]any) {
			item := raw.(map[string]any)["properties"].(map[string]any)
			for _, mechanical := range []string{"findingId", "kind", "mode", "gate", "subject", "supersedes", "acceptedRevision", "problemId", "status"} {
				if item[mechanical] != nil {
					t.Fatalf("model still copies %s", mechanical)
				}
			}
		}
	}
	value := ObservationJudgments{Findings: []FindingJudgment{{Decision: "SAME_AS", SameAs: "second", Reason: "same actual root cause"}, {Decision: "NEW", Reason: "new actual root cause"}}, Scopes: []ScopeJudgment{{Cases: []string{"broken"}, Reason: "changed input behavior"}}, GateImpacts: []ImpactJudgment{{Decision: "RERUN", Reason: "implementation changes this gate range"}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	event, err := DecodeResult(input, "result", raw, "actual-native-output")
	if err != nil {
		t.Fatal(err)
	}
	observations := event.Result.Validation.Observations
	if len(observations) != 3 || event.Result.Outcome != protocol.OutcomePass || event.Result.Validation.Status != "PASS" || event.Result.PayloadDigest != "actual-native-output" || string(raw) != original {
		t.Fatal("semantic/raw output was not retained")
	}
	rows := observations[0].Validation.Findings
	if observations[0].Subject != "wave" || observations[0].Validation.Kind != "ADJUDICATE" || rows[0].FindingID != "second" || rows[1].FindingID != "first" || rows[1].ProblemID != protocol.FindingProblemID(input.Validation.Observation.Findings[1].Finding) {
		t.Fatal("issued identity or newly related problem was not assembled programmatically")
	}
	if observations[1].Validation.Kind != "EXECUTION_SCOPE" || observations[1].Validation.Mode != coverage.ReviewBlackbox || observations[2].Validation.Kind != "GATE_IMPACT" || observations[2].Validation.Gate != "quality" {
		t.Fatal("semantic worker copied/selects fixed duties")
	}
	value.Findings = value.Findings[:1]
	raw, _ = json.Marshal(value)
	if _, err := DecodeResult(input, "missing", raw, "raw"); err == nil {
		t.Fatal("omitted duty admitted")
	}
	if _, err := DecodeResult(input, "copied", []byte(`{"findings":[],"scopes":[],"gateImpacts":[],"kind":"ADJUDICATE"}`), "raw"); err == nil {
		t.Fatal("copied observation kind admitted")
	}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "validation-observation", Content: "Judge actual evidence"}}}
	material, err := Render(input, catalog)
	if err != nil || !material.ReadOnly || !strings.Contains(material.Prompt, "exactly their issued input order") || !strings.Contains(material.Prompt, "preserve real P0/P1") {
		t.Fatalf("material lost duty/permission contract: %v", err)
	}
}
