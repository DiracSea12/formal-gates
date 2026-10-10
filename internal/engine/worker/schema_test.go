package worker

import (
	"encoding/json"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestResultSchemaUsesSemanticRoleContract(t *testing.T) {
	roles := map[string][]string{
		"engine.development.batch": {"paths", "summary", "affectedCases"}, "engine.repair": {"paths", "summary", "affectedCases", "granularityReview"}, "engine.merge.resolve": {"summary"}, "engine.qa.design": {"cases", "coverage", "sharedSteps", "retainCases", "retainSharedSteps", "checkpoint"}, "engine.qa.review.rationale": {"sourceDecisions", "pointDecisions", "caseDecisions", "sharedStepDecisions", "suggestions"}, "engine.qa.execution": {"records", "findings"}, "engine.gate": {"status", "findings"},
	}
	for handler, names := range roles {
		input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		outer := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		for _, key := range []string{"actionId", "provider", "payloadDigest"} {
			if outer[key] != nil {
				t.Fatalf("model owns %s", key)
			}
		}
		fields := outer
		if len(fields) != len(names) {
			t.Fatalf("wrong role fields: %v", fields)
		}
		for _, key := range names {
			if fields[key] == nil {
				t.Fatalf("missing %s", key)
			}
		}
		if fields["binding"] != nil {
			t.Fatal("model owns binding")
		}
	}
}
func TestReviewMaterialRequiresEveryVerificationEvidence(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "product-review"}, {ID: "start-readiness"}}}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		handler := "engine.review.worker"
		if action == review.StartReadiness {
			handler = "engine.readiness.plan"
		}
		input := protocol.WorkerInput{RunID: "run", Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "requirement"}, Review: &protocol.WorkerReviewInput{Action: action, RequirementRevision: "revision"}}
		material, err := Render(input, catalog)
		if err != nil {
			t.Fatal(err)
		}
		fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		for _, key := range []string{"action", "dispatchId", "requirementRevision", "authorizationId"} {
			if fields[key] != nil {
				t.Fatalf("model owns %s", key)
			}
		}
		checks := fields["verification"].(map[string]any)
		if !reflect.DeepEqual(checks["required"], review.RequiredVerificationChecks()) {
			t.Fatal("lost individual checks")
		}
		value := Review{Status: "PASS", Findings: []Finding{}, Verification: map[string]string{}}
		for _, key := range review.RequiredVerificationChecks() {
			value.Verification[key] = "observed " + key
		}
		body, _ := json.Marshal(value)
		event, err := DecodeResult(input, "event", body, "original")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(event.Result.Review.Verification.Checks, review.RequiredVerificationChecks()) || event.Result.Review.RequirementRevision != "revision" {
			t.Fatal("wrong original binding")
		}
		for _, key := range review.RequiredVerificationChecks() {
			delete(value.Verification, key)
			body, _ = json.Marshal(value)
			if _, err := DecodeResult(input, "event", body, "original"); err == nil {
				t.Fatalf("missing %s accepted", key)
			}
			value.Verification[key] = "observed " + key
		}
	}
}

func TestAuthorMaterialRequiresCleanArtifactDelivery(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker"}, {ID: "qa-design"}, {ID: "qa-execution"}, {ID: "qa-review"}}}
	for _, role := range []struct {
		handler string
		kind    coverage.ReviewKind
		author  bool
	}{
		{"engine.development.batch", "", true},
		{"engine.repair", "", true},
		{"engine.merge.resolve", "", true},
		{"engine.qa.design", coverage.ReviewWhitebox, true},
		{"engine.qa.design", coverage.ReviewBlackbox, false},
		{"engine.qa.execution", "", false},
		{"engine.qa.review.rationale", "", false},
	} {
		t.Run(role.handler+string(role.kind), func(t *testing.T) {
			input := protocol.WorkerInput{Handler: role.handler, Provider: "zcode", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: role.kind}}}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			contracts := []string{"Declare only the files actually delivered as artifacts", "Temporary build and test outputs are not delivery artifacts", "remove your own temporary validation outputs", "produce them outside the workspace"}
			if role.handler == "engine.qa.design" && role.author {
				contracts = []string{"Associate only actual test material files", "Remove temporary build and test outputs", "produce them outside the workspace", "Do not return a separate development path declaration"}
			}
			if role.handler == "engine.merge.resolve" {
				contracts = []string{"Report only the semantic resolution summary", "program supplies the exact original conflict paths", "Remove your temporary build and test outputs", "produce them outside the workspace"}
			}
			for _, contract := range contracts {
				if strings.Contains(material.Prompt, contract) != role.author {
					t.Fatalf("role %s delivery contract mismatch: %s", role.handler, contract)
				}
			}
			if role.handler == "engine.qa.design" && role.author {
				if !strings.Contains(material.Prompt, "associates their test material using case files or referenced shared-step files") || strings.Contains(material.Prompt, "Declare only the files actually delivered as artifacts") {
					t.Fatal("QA author material retained a duplicate path-reporting contract")
				}
			} else if role.handler == "engine.merge.resolve" {
				if strings.Contains(material.Prompt, "Declare only the files actually delivered as artifacts") || strings.Contains(material.Prompt, "exact changed paths") {
					t.Fatal("conflict resolution requests issued paths")
				}
			} else if role.author && !strings.Contains(material.Prompt, "Declare only the files actually delivered as artifacts") {
				t.Fatal("development author lost its artifact declaration")
			}
			if role.handler == "engine.qa.execution" && !strings.Contains(material.Prompt, "Compare the issued complete native execution facts") {
				t.Fatal("execution runtime contract lost")
			}
		})
	}
}

func TestQASchemaExcludesIssuedFacts(t *testing.T) {
	for _, handler := range []string{"engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution"} {
		input := protocol.WorkerInput{Handler: handler, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}, PendingCases: []string{"case"}, RequiredCases: []string{"case"}}}
		input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "case"}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if handler == "engine.qa.design" {
			m := fields
			for _, key := range []string{"binding", "sources", "points", "edges"} {
				if m[key] != nil {
					t.Fatalf("manifest owns %s", key)
				}
			}
			c := m["cases"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
			if c["mode"] != nil || !reflect.DeepEqual(c["scenario"].(map[string]any)["enum"], coverage.Scenarios()) {
				t.Fatal("case semantics differ")
			}
		}
		if handler == "engine.qa.review.rationale" {
			r := fields
			for _, key := range []string{"binding", "scope", "setStatus", "unboundSources"} {
				if r[key] != nil {
					t.Fatalf("review owns %s", key)
				}
			}
		}
		if handler == "engine.qa.execution" {
			e := fields
			if e["binding"] != nil || e["notExecuted"] != nil {
				t.Fatal("model owns execution facts")
			}
			r := e["records"].(map[string]any)["properties"].(map[string]any)["case"].(map[string]any)["properties"].(map[string]any)
			for _, key := range []string{"caseId", "reviewKind", "sourceId", "pointId", "provenance", "reuseEvidence", "inheritedFromCandidate", "procedure", "observation", "tool", "environment"} {
				if r[key] != nil {
					t.Fatalf("model owns %s", key)
				}
			}
		}
	}
}

func TestCompletionSchemaUsesIssuedBatchKeys(t *testing.T) {
	batch := protocol.DevelopmentBatch{ID: "owned", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "declared behavior"}, {ID: "interface", Description: "declared interface"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Command: "go build ./..."}, {ID: "behavior-test", Command: "go test ./..."}}}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker", Content: "Develop the issued duty"}}}
	for _, role := range []string{"engine.development.batch", "engine.repair"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Handler: role, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)
			for name, ids := range map[string][]string{"dod": {"behavior", "interface"}} {
				schema := fields[name].(map[string]any)
				properties := schema["properties"].(map[string]any)
				if schema["additionalProperties"] != false || !reflect.DeepEqual(schema["required"], ids) || len(properties) != len(ids) {
					t.Fatalf("%s is not exactly the issued key set: %v", name, schema)
				}
				for _, id := range ids {
					field := properties[id].(map[string]any)
					if field["type"] != "string" || field["minLength"] != 1 {
						t.Fatal("completion evidence lost non-empty string contract")
					}
				}
			}
			if !strings.Contains(material.Prompt, "do not omit or add keys") || !strings.Contains(material.Prompt, "Approved command execution and its evidence belong to the program") {
				t.Fatal("additional evidence destination missing")
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("schema/material mutated issued input")
			}
		})
	}
	for _, name := range []string{"no-batch", "empty-batch", "unfrozen-repair", "final-verification", "other-role"} {
		t.Run(name, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: "engine.repair", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
			switch name {
			case "empty-batch":
				input.Validation.Development = &protocol.DevelopmentInput{}
			case "unfrozen-repair":
				input.Validation.Repair = &protocol.RepairInput{}
			case "final-verification":
				input.Validation.Development = &protocol.DevelopmentInput{Batch: batch}
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "final"}}
			case "other-role":
				input.Handler = "engine.qa.design"
				input.Validation.Development = &protocol.DevelopmentInput{Batch: batch}
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			if name == "other-role" {
				if schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["development"] != nil {
					t.Fatal("QA role exposed a duplicate development declaration")
				}
				return
			}
			development := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
			if name == "unfrozen-repair" {
				input.RunID, input.Pending.AttemptID = "run", "attempt"
				input.RequirementDocument = requirement.FrozenRequirementProjection{Content: "confirmed requirement"}
				material, err := Render(input, catalog)
				if err != nil {
					t.Fatal(err)
				}
				for _, rule := range []string{"issued development batch or a proposed single batch", "Without either, omit completion", "paths, summary and affectedCases"} {
					if !strings.Contains(material.Prompt, rule) {
						t.Fatalf("repair output instruction missing %q", rule)
					}
				}
				if development["completion"] != nil {
					t.Fatal("unplanned repair exposed completion without a batch")
				}
				proposed := schema["anyOf"].([]any)[3].(map[string]any)["properties"].(map[string]any)
				completion := proposed["completion"].(map[string]any)["properties"].(map[string]any)
				if _, ok := completion["dod"].(map[string]any)["additionalProperties"].(map[string]any); !ok {
					t.Fatal("single proposed repair batch cannot supply authored DoD evidence")
				}
			} else if development["completion"] != nil {
				t.Fatal("non-batch duty exposed unused completion claims")
			}
		})
	}
}

func TestUnplannedRepairCompletionRequiresSingleProposal(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	branches := schema["anyOf"].([]any)
	if len(branches) != 4 {
		t.Fatalf("repair lost a semantic alternative: %v", schema)
	}
	for _, branch := range branches {
		body := branch.(map[string]any)
		fields := body["properties"].(map[string]any)
		if fields["completion"] == nil {
			continue
		}
		for _, name := range []string{"granularityReview", "completion"} {
			if !slices.Contains(body["required"].([]any), any(name)) {
				t.Fatalf("completion alternative does not require %s", name)
			}
		}
		batches := fields["granularityReview"].(map[string]any)["properties"].(map[string]any)["batches"].(map[string]any)
		if batches["minItems"] != 1 || batches["maxItems"] != 1 {
			t.Fatal("completion alternative permits a missing or multiple-batch proposal")
		}
	}
	batch := Batch{DevelopmentBatch: protocol.DevelopmentBatch{ID: "command"}}
	for _, scenario := range []string{"small-repair", "single-proposal", "split-plan", "unplanned-completion", "split-completion"} {
		t.Run(scenario, func(t *testing.T) {
			result := Repair{Paths: []string{"command.py"}, Summary: "Implemented the confirmed command", AffectedCases: []string{"normal-input"}}
			if scenario != "small-repair" && scenario != "unplanned-completion" {
				result.GranularityReview = &Granularity{Reason: "One command boundary", VerificationTier: "FINAL", Batches: []Batch{batch}}
			}
			if scenario == "split-plan" || scenario == "split-completion" {
				result.GranularityReview.Batches = append(result.GranularityReview.Batches, Batch{DevelopmentBatch: protocol.DevelopmentBatch{ID: "output"}})
			}
			if scenario == "split-plan" {
				result.Paths, result.Summary, result.AffectedCases = nil, "", nil
			} else if scenario != "small-repair" {
				result.Completion = &protocol.DevelopmentCompletion{DoD: map[string]string{"normal-input": "Implemented"}, Handoff: "Ready for approved checks"}
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			event, err := DecodeResult(input, "result", raw, "original-native-output")
			invalid := scenario == "unplanned-completion" || scenario == "split-completion"
			if (err != nil) != invalid {
				t.Fatalf("repair completion boundary changed: %v", err)
			}
			if !invalid && (event.Result.PayloadDigest != "original-native-output" || event.Result.Validation.Binding != input.Validation.Binding) {
				t.Fatal("repair lost its original source or issued identity")
			}
		})
	}
}

func TestValidationRoleStatusUsesOnlyOwnedFiniteValues(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker", Content: "Develop"}, {ID: "qa-design", Content: "Design"}, {ID: "qa-review", Content: "Review"}, {ID: "qa-execution", Content: "Execute"}}, Gates: []prompts.PromptDefinition{{ID: "quality", Content: "Judge"}}}
	for _, role := range []struct {
		handler  string
		statuses []string
	}{{"engine.development.batch", nil}, {"engine.repair", nil}, {"engine.merge.resolve", nil}, {"engine.qa.design", nil}, {"engine.gate", []string{"PASS", "FAIL"}}, {"engine.qa.review.rationale", nil}, {"engine.qa.execution", nil}} {
		t.Run(role.handler, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Handler: role.handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}}, Gate: &protocol.GateInput{Gate: "quality"}}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
			if role.statuses == nil {
				if fields["status"] != nil {
					t.Fatal("model owns a derived status")
				}
			} else if !reflect.DeepEqual(fields["status"].(map[string]any)["enum"], role.statuses) {
				t.Fatal("semantic gate verdict domain changed")
			}
			encoded, _ := json.Marshal(material)
			if strings.Contains(string(encoded), "resultTemplate") {
				t.Fatal("mechanical template retained")
			}
			if !material.ReadOnly && (!strings.Contains(material.Prompt, "Do not execute any VCS write operation; all VCS writes belong to engine adapters.") || !strings.Contains(material.Prompt, "Read-only VCS inspection remains permitted.")) {
				t.Fatal("writable role lost adapter ownership or read-only inspection")
			}
			if strings.Contains(material.Prompt, "Do not stage, commit, submit or push") {
				t.Fatal("partial VCS prohibition remained")
			}
		})
	}
	t.Run("whitebox-design-vcs-owner", func(t *testing.T) {
		input := protocol.WorkerInput{RunID: "run", Handler: "engine.qa.design", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}}
		material, err := Render(input, catalog)
		if err != nil {
			t.Fatal(err)
		}
		if material.ReadOnly || !strings.Contains(material.Prompt, "all VCS writes belong to engine adapters") {
			t.Fatal("test authoring acquired VCS write ownership")
		}
	})
}

func TestExecutionMaterialExplainsFinalCompletedReport(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution"
	input.RequirementDocument.Content = "confirmed requirement"
	input.Validation.Execution = &coverage.ExecutionBinding{Candidate: coverage.ValidationCandidate{Identity: "candidate"}}
	input.Validation.RequiredCases = []string{"new", "prior"}
	input.Validation.Completed = []coverage.ExecutionRecord{
		{CaseID: "new", Result: coverage.ExecutedFail},
		{CaseID: "prior", Result: coverage.ExecutedPass},
	}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{Evidence: protocol.CheckEvidence{Tool: "actual-tool", Environment: "actual-environment"}}}}}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "qa-execution"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{"program derives checkpoint and overall status", "Completed FAIL judgments are complete", "return records {}", "Missing native facts never establish PASS"} {
		if !strings.Contains(material.Prompt, instruction) {
			t.Fatalf("final report instruction missing: %s", instruction)
		}
	}
	fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if fields["status"] != nil || fields["checkpoint"] != nil {
		t.Fatal("model owns derived execution summaries")
	}
	records := fields["records"].(map[string]any)
	if len(records["properties"].(map[string]any)) != 0 || records["additionalProperties"] != false {
		t.Fatal("completed judgments were requested again")
	}
	body := decodeSemantic(t, input, ExecutionJudgments{Execution: Execution{Records: map[string]Observation{}}}).Validation
	if body.Status != "FAIL" || body.Checkpoint || len(body.Execution.Records) != 0 || len(body.Execution.NotExecuted) != 0 {
		t.Fatal("completed facts were resubmitted or their FAIL lost")
	}
	after, err := json.Marshal(material.Input)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("render changed the issued input")
	}
}
