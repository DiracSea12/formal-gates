package worker

import (
	"encoding/json"
	"reflect"
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

func TestObservationSameAsSelectsOnlyIssuedRelatedCandidates(t *testing.T) {
	input := observationWorkerInput()
	input.Validation.Observation.Problems = []protocol.ObservationProblem{{ValidationProblem: protocol.ValidationProblem{ID: "sha256:original-problem", FindingIDs: []string{"original-finding"}}, Findings: []protocol.ObservationFinding{{Finding: protocol.ValidationFinding{ID: "original-finding", History: []protocol.FindingResolution{{RootCause: "original verified root cause", ProblemID: "sha256:original-problem"}}}, Evidence: protocol.WorkerResult{PayloadDigest: "original-source-digest"}}}}}
	selected := 1
	value := ObservationJudgments{Findings: []FindingJudgment{{Decision: "SAME_AS", SameAs: &selected}, {Decision: "NEW"}}, Scopes: []ScopeJudgment{{Cases: []int{1}}}, GateImpacts: []ImpactJudgment{{Decision: "RERUN"}}}
	decode := func() ([]protocol.OperatorObservationPayload, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		event, err := DecodeResultWithPaths(input, "related", raw, "original-native-digest", []string{})
		if err != nil {
			return nil, err
		}
		return event.Result.Validation.Observations, err
	}
	observations, err := decode()
	if err != nil || observations[0].Validation.Findings[1].ProblemID != "sha256:original-problem" {
		t.Fatalf("existing problem was not supplied from original input: %v", err)
	}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "validation-observation", Content: "Judge actual evidence"}}}
	material, err := Render(input, catalog)
	if err != nil || !strings.Contains(material.Prompt, "original verified root cause") || !strings.Contains(material.Prompt, "original-source-digest") {
		t.Fatalf("existing candidate lacks its original semantic/source evidence: %v", err)
	}
	selected = 3
	observations, err = decode()
	if err != nil || observations[0].Validation.Findings[1].ProblemID != protocol.FindingProblemID(input.Validation.Observation.Findings[1].Finding) {
		t.Fatalf("later issued NEW finding was not selected: %v", err)
	}
	for _, invalid := range []int{-1, 0, 2, 4} {
		selected = invalid
		if _, err := decode(); err == nil {
			t.Fatalf("self, nonexistent or invalid candidate %d admitted", invalid)
		}
	}
	selected = 3
	value.Findings[1].Decision = "DISMISSED"
	if _, err := decode(); err == nil {
		t.Fatal("related finding not judged NEW admitted")
	}
	value.Findings[0].SameAs = nil
	if _, err := decode(); err == nil {
		t.Fatal("SAME_AS without candidate admitted")
	}
	value.Findings[0].SameAs = &selected
	value.Findings[0].Decision = "NEW"
	if _, err := decode(); err == nil {
		t.Fatal("candidate attached to non-SAME_AS decision admitted")
	}
	if _, err := DecodeResultWithPaths(input, "raw-id", []byte(`{"findings":[{"decision":"SAME_AS","sameAs":"sha256:original-problem"},{"decision":"NEW"}],"scopes":[{"cases":["broken"],"reason":"impact"}],"gateImpacts":[{"decision":"RERUN","reason":"impact"}]}`), "original-native-digest", []string{}); err == nil {
		t.Fatal("raw-ID fallback admitted")
	}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	items := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["prefixItems"].([]any)
	selection := items[0].(map[string]any)["properties"].(map[string]any)["sameAs"].(map[string]any)
	if selection["type"] != "integer" || !reflect.DeepEqual(selection["enum"], []int{1, 3}) || !strings.Contains(selection["description"].(string), "1 = existing problem") || !strings.Contains(selection["description"].(string), "3 = issued finding") {
		t.Fatal("selection schema lost the exact issued choices or original evidence source")
	}
	input.Validation.Observation.Problems = nil
	input.Validation.Observation.Findings = input.Validation.Observation.Findings[:1]
	schema, err = ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	item := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if item["sameAs"] != nil || !reflect.DeepEqual(item["decision"].(map[string]any)["enum"], []string{"NEW", "DISMISSED"}) {
		t.Fatal("sole finding permits an impossible related selection")
	}
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
	selected := 2
	value := ObservationJudgments{Findings: []FindingJudgment{{Decision: "SAME_AS", SameAs: &selected, Reason: "same actual root cause"}, {Decision: "NEW", Reason: "new actual root cause"}}, Scopes: []ScopeJudgment{{Cases: []int{1}, Reason: "changed input behavior"}}, GateImpacts: []ImpactJudgment{{Decision: "RERUN", Reason: "implementation changes this gate range"}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	original := string(raw)
	event, err := DecodeResultWithPaths(input, "result", raw, "actual-native-output", []string{})
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
	if strings.Contains(string(raw), "second") || strings.Contains(string(raw), "sha256:") {
		t.Fatal("model result still copies a finding or problem identity")
	}
	value.Findings = value.Findings[:1]
	raw, _ = json.Marshal(value)
	if _, err := DecodeResultWithPaths(input, "missing", raw, "raw", []string{}); err == nil {
		t.Fatal("omitted duty admitted")
	}
	if _, err := DecodeResultWithPaths(input, "copied", []byte(`{"findings":[],"scopes":[],"gateImpacts":[],"kind":"ADJUDICATE"}`), "raw", []string{}); err == nil {
		t.Fatal("copied observation kind admitted")
	}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "validation-observation", Content: "Judge actual evidence"}}}
	material, err := Render(input, catalog)
	if err != nil || !material.ReadOnly || !strings.Contains(material.Prompt, "exactly their issued input order") || !strings.Contains(material.Prompt, "preserve real P0/P1") {
		t.Fatalf("material lost duty/permission contract: %v", err)
	}
}
