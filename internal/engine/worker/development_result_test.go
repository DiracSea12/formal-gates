package worker

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
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
		if properties["caseRuns"] != nil || properties["build"] != nil || properties["tests"] != nil || properties["completion"].(map[string]any)["properties"].(map[string]any)["checks"] != nil {
			t.Fatal("model execution claims remain in the contract")
		}
		for _, claim := range []string{"none", "build", "tests", "checks", "caseRuns"} {
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

func TestRepairComparisonDerivesExecutionFactsAndStatus(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair.verify", Provider: "host", Pending: protocol.PendingAction{ActionID: "compare"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "exact-source"}, VerificationPaths: []string{"app.txt"}, VerificationCases: []string{"case-a", "case-b"}}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Cases: []coverage.AcceptanceCase{{CaseID: "case-a"}, {CaseID: "case-b"}}}}}}}
	for _, id := range input.Validation.Repair.VerificationCases {
		input.Validation.ExecutionFacts = append(input.Validation.ExecutionFacts, protocol.ExecutionFact{CaseID: id, Operations: []protocol.ExecutionOperationFact{{ActionID: "operation:" + id, Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:" + id, Operation: coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"fixture-tool", id}}}}, Evidence: protocol.CheckEvidence{StdoutText: id, Tool: "fixture-tool", Environment: "fixture-environment"}}}})
	}
	for _, scenario := range []string{"all-pass", "case-fail", "status-claim"} {
		t.Run(scenario, func(t *testing.T) {
			records := map[string]Observation{"case-a": {Result: coverage.ExecutedPass, Comparison: "output matches case-a"}, "case-b": {Result: coverage.ExecutedPass, Comparison: "output matches case-b"}}
			if scenario == "case-fail" {
				records["case-b"] = Observation{Result: coverage.ExecutedFail, Comparison: "actual output differs from oracle"}
			}
			value := Result{Outcome: protocol.OutcomePass, Validation: &Validation{RepairVerification: &Execution{Records: records}}}
			if scenario == "status-claim" {
				value.Validation.Status = protocol.OutcomePass
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			event, err := DecodeResult(input, "result", raw, "sha256:semantic")
			if scenario == "status-claim" {
				if err == nil {
					t.Fatal("model repeated a program-derived status")
				}
				return
			}
			expected := protocol.OutcomePass
			if scenario == "case-fail" {
				expected = protocol.OutcomeFail
			}
			if err != nil || event.Result.Validation.Status != expected || len(event.Result.Validation.Development.CaseRuns) != 2 {
				t.Fatalf("comparison projection lost semantic/factual values: %v %+v", err, event)
			}
			for i, fact := range input.Validation.ExecutionFacts {
				procedure, observation, err := fact.Render()
				if err != nil {
					t.Fatal(err)
				}
				row := event.Result.Validation.Development.CaseRuns[i]
				if row.Procedure != procedure || row.Observation != observation {
					t.Fatal("model replaced native facts")
				}
			}
		})
	}
}
