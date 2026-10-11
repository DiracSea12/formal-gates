package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestDevelopmentResultOwnsOnlyIssuedBatchEvidence(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.development.batch.v3", Provider: "host", Pending: protocol.PendingAction{ActionID: "edit"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "command", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "Public behavior"}}}}}}
	raw := []byte(`{"summary":"Implemented public behavior","completion":{"dod":["Public behavior implemented"],"handoff":"Ready for program checks"}}`)
	for _, resumed := range []bool{false, true} {
		if resumed {
			input.Validation.Development.Result = &protocol.WorkerResult{Digest: "accepted-progress", Validation: &protocol.ValidationResult{Development: &protocol.DevelopmentEvidence{Paths: []string{"library.txt"}, RemainingWork: "Implement the public command"}}}
		}
		original, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{"command.txt"})
		if err != nil {
			t.Fatal(err)
		}
		stored, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var recovered protocol.WorkerInput
		if err := json.Unmarshal(stored, &recovered); err != nil {
			t.Fatal(err)
		}
		replay, err := DecodeResultWithPaths(recovered, "result", raw, "original-output", []string{"command.txt"})
		if err != nil || !reflect.DeepEqual(original, replay) {
			t.Fatalf("recovery changed original acceptance: %v", err)
		}
		result := original.Result
		development := result.Validation.Development
		if result.ActionID != input.Pending.ActionID || result.PayloadDigest != "original-output" || development.Completion.DoD["behavior"] != "Public behavior implemented" || len(development.AffectedCases) != 0 {
			t.Fatalf("issued batch or original source changed: %+v", result)
		}
		if resumed && (result.SourceResult != "accepted-progress" || !reflect.DeepEqual(development.Paths, []string{"command.txt", "library.txt"})) {
			t.Fatal("continuation lost original source or delivered paths")
		}
		mixed := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"affectedCases":[1,2,3]}`)...)
		if _, err := DecodeResultWithPaths(recovered, "invalid", mixed, "original-invalid-output", []string{}); err == nil || !strings.Contains(err.Error(), `unknown field "affectedCases"`) {
			t.Fatalf("development accepted repair-only evidence: %v", err)
		}
	}
}

func TestRepairAffectedCasesUseOriginalContractChoices(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair.v3", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Cases: []coverage.AcceptanceCase{{CaseID: "first"}, {CaseID: "second"}}}, {Cases: []coverage.AcceptanceCase{{CaseID: "first"}, {CaseID: "third"}}}}}}}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	choices := fields["affectedCases"].(map[string]any)["items"].(map[string]any)["enum"]
	if !reflect.DeepEqual(choices, []int{1, 2, 3}) {
		t.Fatalf("repair choices do not follow the original unique contract order: %v", choices)
	}
	raw := []byte(`{"summary":"Repaired public behavior","affectedCases":[3,1]}`)
	stored, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	original, err := DecodeResultWithPaths(input, "result", raw, "original-output", []string{"command.txt"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithPaths(recovered, "result", raw, "original-output", []string{"command.txt"})
	if err != nil || !reflect.DeepEqual(original, replay) || !reflect.DeepEqual(original.Result.Validation.Development.AffectedCases, []string{"third", "first"}) {
		t.Fatalf("repair impact or original recovery acceptance changed: %v %+v", err, replay)
	}
	for _, invalid := range []string{`{"summary":"Repaired","affectedCases":[4]}`, `{"summary":"Repaired","affectedCases":[1,1]}`} {
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(invalid), "invalid-output", []string{}); err == nil {
			t.Fatalf("repair admitted an unissued or repeated choice: %s", invalid)
		}
	}
	input.Validation.Contract = nil
	schema, err = ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	fields = schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if fields["affectedCases"] != nil {
		t.Fatal("repair without case duties exposed a case field")
	}
	for _, raw := range []string{`{"summary":"Repaired","affectedCases":[]}`, `{"summary":"Repaired","affectedCases":[1]}`} {
		if _, err := DecodeResultWithPaths(input, "invalid", []byte(raw), "invalid-output", []string{}); err == nil || !strings.Contains(err.Error(), `unknown field "affectedCases"`) {
			t.Fatalf("repair accepted evidence outside its duties: %v", err)
		}
	}
	if _, err := DecodeResultWithPaths(input, "result", []byte(`{"summary":"Repaired"}`), "original-output", []string{}); err != nil {
		t.Fatal(err)
	}
}

func TestCompletedRepairPlanUsesNativeArtifactsAndApprovedChecks(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair.v3", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "project-build"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "project-tests"}, {ID: "local", Command: "local-oracle"}}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	value := Repair{Summary: "Repaired the original behavior", Completion: &Completion{DoD: []string{"Behavior restored"}, Handoff: "Ready for program checks"}, GranularityReview: &Granularity{Reason: "One cohesive repair", VerificationTier: "FINAL", Batches: []Batch{{DoD: []Criterion{{Description: "Behavior restored"}}, Checks: []Check{{Reuse: 3}, {Command: "new-oracle"}}}}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"language/source.ext"}
	original, err := DecodeResultWithFacts(input, "result", raw, "verified-source", paths, nil)
	if err != nil {
		t.Fatal(err)
	}
	batch := original.Result.Validation.GranularityReview.Batches[0]
	if !reflect.DeepEqual(batch.Artifacts, paths) || !reflect.DeepEqual(batch.Checks[:3], input.ApprovedChecks) || batch.Checks[3].Command != "new-oracle" || batch.Checks[3].Purpose != "" {
		t.Fatalf("native artifacts or approved command identities were changed: %+v", batch)
	}
	stored, _ := json.Marshal(input)
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithFacts(recovered, "result", raw, "verified-source", paths, nil)
	if err != nil || !reflect.DeepEqual(original, replay) {
		t.Fatalf("recovery changed original repair facts: %v", err)
	}
	input.ApprovedChecks = append(input.ApprovedChecks, protocol.DevelopmentCheck{ID: "independent:local", Command: "independent-local-oracle"})
	value.GranularityReview.Batches[0].Checks = []Check{{Reuse: 3}, {Reuse: 4}}
	selected, _ := json.Marshal(value)
	joined, err := DecodeResultWithFacts(input, "joined", selected, "verified-source", paths, nil)
	if err != nil || !reflect.DeepEqual(joined.Result.Validation.GranularityReview.Batches[0].Checks, input.ApprovedChecks) {
		t.Fatalf("single repair lost independent original local checks: %v", err)
	}
	value.GranularityReview.Batches[0].Checks = []Check{{Reuse: 3}, {Command: "new-oracle"}}
	for _, field := range []string{"artifacts", "newArtifacts"} {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		body["granularityReview"].(map[string]any)["batches"].([]any)[0].(map[string]any)[field] = []any{}
		mixed, _ := json.Marshal(body)
		if _, err := DecodeResultWithFacts(input, "invalid", mixed, "verified-source", paths, nil); err == nil {
			t.Fatalf("completed repair accepted an artifact declaration: %s", field)
		}
	}
	value.GranularityReview.Batches[0].Checks = []Check{{Command: "project-build", Purpose: protocol.CheckFullBuild}}
	raw, _ = json.Marshal(value)
	if _, err := DecodeResultWithFacts(input, "invalid", raw, "verified-source", paths, nil); err == nil {
		t.Fatal("copied approved command was accepted as original authorship")
	}
}

func TestDevelopmentResultRejectsExecutionClaims(t *testing.T) {
	batch := protocol.DevelopmentBatch{ID: "command", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "Public behavior"}}, Checks: []protocol.DevelopmentCheck{{ID: "actual", Command: "false"}}}
	for _, role := range []string{"engine.development.batch.v3", "engine.repair.v3"} {
		input := protocol.WorkerInput{Handler: role, Provider: "host", Pending: protocol.PendingAction{ActionID: "edit"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		properties := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if properties["caseRuns"] != nil || properties["build"] != nil || properties["tests"] != nil || properties["completion"].(map[string]any)["properties"].(map[string]any)["checks"] != nil {
			t.Fatal("model execution claims remain in the contract")
		}
		for _, claim := range []string{"none", "build", "tests", "checks", "caseRuns"} {
			development := map[string]any{"summary": "Implemented public behavior", "completion": map[string]any{"dod": []string{"Public behavior implemented"}, "handoff": "Ready for program checks"}}
			if claim == "checks" {
				development["completion"].(map[string]any)[claim] = map[string]string{"actual": "passed"}
			} else if claim != "none" {
				development[claim] = []string{"passed"}
			}
			raw, err := json.Marshal(development)
			if err != nil {
				t.Fatal(err)
			}
			event, err := DecodeResultWithPaths(input, "result", raw, "sha256:original", []string{})
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
	input := protocol.WorkerInput{Handler: "engine.repair.verify.v2", Provider: "host", Pending: protocol.PendingAction{ActionID: "compare"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "exact-source"}, VerificationPaths: []string{"app.txt"}, VerificationCases: []string{"case-a", "case-b"}}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Cases: []coverage.AcceptanceCase{{CaseID: "case-a"}, {CaseID: "case-b"}}}}}}}
	for _, id := range input.Validation.Repair.VerificationCases {
		input.Validation.ExecutionFacts = append(input.Validation.ExecutionFacts, protocol.ExecutionFact{CaseID: id, Operations: []protocol.ExecutionOperationFact{{ActionID: "operation:" + id, Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:" + id, Operation: coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"fixture-tool", id}}}}, Evidence: protocol.CheckEvidence{StdoutText: id, Tool: "fixture-tool", Environment: "fixture-environment"}}}})
	}
	for _, scenario := range []string{"all-pass", "case-fail", "status-claim"} {
		t.Run(scenario, func(t *testing.T) {
			records := []*Observation{{Result: coverage.ExecutedPass, Comparison: "output matches case-a"}, {Result: coverage.ExecutedPass, Comparison: "output matches case-b"}}
			if scenario == "case-fail" {
				records[1] = &Observation{Result: coverage.ExecutedFail, Comparison: "actual output differs from oracle"}
			}
			value := Execution{Records: records}

			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "status-claim" {
				raw = append(raw[:len(raw)-1], []byte(`,"status":"PASS"}`)...)
			}
			event, err := DecodeResultWithPaths(input, "result", raw, "sha256:semantic", []string{})
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
