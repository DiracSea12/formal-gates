package worker

import (
	"encoding/json"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
	"path/filepath"
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
		"engine.development.batch.v6": {"summary"}, "engine.repair.v7": {"summary", "granularityReview"}, "engine.merge.resolve.v3": {"summary"}, "engine.qa.design.v6": {"cases", "coverage", "sharedSteps", "replace", "removeCases", "removeSharedSteps", "checkpoint"}, "engine.qa.review.rationale.v2": {"sourceDecisions", "pointDecisions", "caseDecisions", "sharedStepDecisions", "suggestions"}, "engine.qa.execution.v4": {"records", "findings"}, "engine.gate.v3": {"findings"},
	}
	for handler, names := range roles {
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
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

func TestFindingSchemasPublishSeverityChoices(t *testing.T) {
	for _, input := range []protocol.WorkerInput{
		{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.review.worker.v3", Review: &protocol.WorkerReviewInput{Action: review.ProductReview}},
		{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.gate.v3", Validation: &protocol.ValidationInput{}},
		{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.qa.execution.v4", Validation: &protocol.ValidationInput{}},
	} {
		t.Run(input.Handler, func(t *testing.T) {
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, branch := range schema["anyOf"].([]any) {
				fields := branch.(map[string]any)["properties"].(map[string]any)
				if fields["findings"] == nil {
					continue
				}
				finding := fields["findings"].(map[string]any)["items"].(map[string]any)
				severity := finding["properties"].(map[string]any)["severity"].(map[string]any)
				if !reflect.DeepEqual(severity["enum"], []string{"P0", "P1", "P2", "P3"}) || !slices.Contains(finding["required"].([]any), any("severity")) {
					t.Fatal("finding severity is not a required choice among P0-P3", severity)
				}
			}
		})
	}
}

func TestDesignSchemasPublishScenarioChoicesForNewAndExistingCases(t *testing.T) {
	for _, kind := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox} {
		for _, scope := range []string{"initial", "full", "affected"} {
			t.Run(string(kind)+"/"+scope, func(t *testing.T) {
				input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.qa.design.v6", Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: kind}}}}
				if scope != "initial" {
					input.Validation.Manifest.Cases = []coverage.AcceptanceCase{{CaseID: "approved", Scenario: "SUCCESS", Oracle: "approved oracle"}}
				}
				if scope == "affected" {
					input.Validation.AffectedCases = []string{"approved"}
				}
				schema, err := ResultSchema(input)
				if err != nil {
					t.Fatal(err)
				}
				cases := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["cases"].(map[string]any)
				items := []any{cases["items"]}
				if scope == "affected" {
					items = cases["prefixItems"].([]any)
				} else if scope == "full" {
					items = cases["items"].(map[string]any)["anyOf"].([]any)
				}
				for index, raw := range items {
					item := raw.(map[string]any)
					scenario := item["properties"].(map[string]any)["scenario"].(map[string]any)
					if !reflect.DeepEqual(scenario["enum"], coverage.Scenarios()) {
						t.Fatal("scenario choices differ from acceptance coverage", scenario)
					}
					newCase := scope != "affected" && index == 0
					if slices.Contains(item["required"].([]any), any("scenario")) != newCase {
						t.Fatal("new-case scenario duty or existing-case omission changed")
					}
				}
				stored, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				var recovered protocol.WorkerInput
				if err := json.Unmarshal(stored, &recovered); err != nil {
					t.Fatal(err)
				}
				replay, err := ResultSchema(recovered)
				if err != nil || !reflect.DeepEqual(schema, replay) {
					t.Fatal("recovery changed scenario choices or omission", err)
				}
			})
		}
	}
}

func TestObservationSchemaRequiresOrdinarySeverityAndExcludesExecutionSeverity(t *testing.T) {
	input := observationWorkerInput()
	input.InputCodec, input.OutputCodec = protocol.WorkerInputCodec, OutputCodec
	input.Validation.Observation.Findings[0].Finding.Raw = &review.Finding{Severity: "P1"}
	input.Validation.Observation.Findings[1].Finding.ExecutionFailure = &protocol.ExecutionFailure{CaseID: "failed-case"}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "validation-observation"}}}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	findings := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["prefixItems"].([]any)
	ordinary := findings[0].(map[string]any)
	properties := ordinary["properties"].(map[string]any)
	for _, decision := range []string{"NEW", "SAME_AS", "DISMISSED"} {
		t.Run(decision, func(t *testing.T) {
			if !slices.Contains(properties["decision"].(map[string]any)["enum"].([]string), decision) {
				t.Fatal("ordinary decision lost its severity duty")
			}
			for _, name := range []string{"severity", "severityRationale"} {
				if !slices.Contains(ordinary["required"].([]any), any(name)) || properties[name].(map[string]any)["minLength"] != 1 {
					t.Fatalf("ordinary %s does not require non-empty %s", decision, name)
				}
			}
			verification := properties["verification"].(map[string]any)
			if !slices.Contains(verification["required"].([]any), any("severity")) || verification["properties"].(map[string]any)["severity"].(map[string]any)["minLength"] != 1 {
				t.Fatalf("ordinary %s does not require severity verification", decision)
			}
		})
	}
	if !reflect.DeepEqual(properties["severity"].(map[string]any)["enum"], []string{"P0", "P1", "P2", "P3"}) {
		t.Fatal("ordinary severity was derived from the source grade")
	}
	execution := findings[1].(map[string]any)
	executionFields := execution["properties"].(map[string]any)
	for _, name := range []string{"severity", "severityRationale"} {
		if executionFields[name] != nil || slices.Contains(execution["required"].([]any), any(name)) {
			t.Fatalf("execution failure exposes %s", name)
		}
	}
	verification := executionFields["verification"].(map[string]any)
	if verification["properties"].(map[string]any)["severity"] != nil || slices.Contains(verification["required"].([]any), any("severity")) || execution["additionalProperties"] != false || verification["additionalProperties"] != false {
		t.Fatal("execution failure accepts a severity judgment")
	}
	stored, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	var recovered Material
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := Render(recovered.Input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	originalSchema, _ := json.Marshal(material.ResultSchema)
	replaySchema, _ := json.Marshal(replay.ResultSchema)
	if string(originalSchema) != string(replaySchema) {
		t.Fatal("recovery changed the issued severity duties")
	}
}

func TestReviewMaterialCannotAuthorizeIndependentVerification(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "product-review"}, {ID: "start-readiness"}}}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		handler := "engine.review.worker.v3"
		if action == review.StartReadiness {
			handler = "engine.readiness.plan.v6"
		}
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "requirement"}, Review: &protocol.WorkerReviewInput{Action: action, RequirementRevision: "revision"}}
		material, err := Render(input, catalog)
		if err != nil {
			t.Fatal(err)
		}
		for _, internal := range []string{"inputCodec", "outputCodec"} {
			if strings.Contains(material.Prompt, internal) {
				t.Fatalf("model prompt exposes internal I/O binding %s", internal)
			}
		}
		fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		for _, key := range []string{"action", "dispatchId", "requirementRevision", "authorizationId"} {
			if fields[key] != nil {
				t.Fatalf("model owns %s", key)
			}
		}
		if fields["verification"] != nil {
			t.Fatal("reviewer owns independent Operator verification")
		}
		value := Review{Findings: []Finding{}}
		body, _ := json.Marshal(value)
		event, err := DecodeResultWithPaths(input, "event", body, "original", []string{})
		if err != nil {
			t.Fatal(err)
		}
		if event.Result.Review.RequirementRevision != "revision" {
			t.Fatal("reviewer authorized independent verification or lost the original binding")
		}
		forged := append(body[:len(body)-1], []byte(`,"verification":["reviewer self-approval"]}`)...)
		if _, err := DecodeResultWithPaths(input, "event", forged, "original", []string{}); err == nil {
			t.Fatal("reviewer self-approval field was accepted")
		}

	}
}

func TestAuthorMaterialRequiresCleanArtifactDelivery(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct {
		handler string
		kind    coverage.ReviewKind
		author  bool
	}{
		{"engine.development.batch.v6", "", true},
		{"engine.repair.v7", "", true},
		{"engine.merge.resolve.v3", "", true},
		{"engine.qa.design.v6", coverage.ReviewWhitebox, true},
		{"engine.qa.design.v6", coverage.ReviewBlackbox, false},
		{"engine.qa.execution.v4", "", false},
		{"engine.qa.review.rationale.v2", "", false},
	} {
		t.Run(role.handler+string(role.kind), func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: role.handler, Provider: "zcode", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: role.kind}}}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			contracts := []string{"Deliver only actual authorized artifacts", "Temporary build and test outputs are not delivery artifacts", "remove your own temporary validation outputs", "produce them outside the workspace"}
			if role.handler == "engine.qa.design.v6" && role.author {
				contracts = []string{"Associate only actual test material files", "Remove temporary build and test outputs", "produce them outside the workspace", "Do not return a separate development path declaration"}
			}
			if role.handler == "engine.merge.resolve.v3" {
				contracts = []string{"Report only the semantic resolution summary", "program supplies the exact original conflict paths", "Remove your temporary build and test outputs", "produce them outside the workspace"}
			}
			for _, contract := range contracts {
				if strings.Contains(material.Prompt, contract) != role.author {
					t.Fatalf("role %s delivery contract mismatch: %s", role.handler, contract)
				}
			}
			if role.handler == "engine.qa.design.v6" && role.author {
				fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
				if fields["paths"] != nil || strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") {
					t.Fatal("QA author material retained a duplicate path-reporting contract")
				}
			} else if role.handler == "engine.merge.resolve.v3" {
				if strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") || strings.Contains(material.Prompt, "exact changed paths") {
					t.Fatal("conflict resolution requests issued paths")
				}
			} else if role.author && !strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") {
				t.Fatal("development author lost its artifact declaration")
			}
			if role.handler == "engine.qa.execution.v4" && !strings.Contains(material.Prompt, "Compare the issued complete native execution facts") {
				t.Fatal("execution runtime contract lost")
			}
		})
	}
}

func TestQASchemaExcludesIssuedFacts(t *testing.T) {
	for _, handler := range []string{"engine.qa.design.v6", "engine.qa.review.rationale.v2", "engine.qa.execution.v4"} {
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: handler, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}, PendingCases: []string{"case"}, RequiredCases: []string{"case"}}}
		input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "case"}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if handler == "engine.qa.design.v6" {
			m := fields
			for _, key := range []string{"binding", "sources", "points", "edges"} {
				if m[key] != nil {
					t.Fatalf("manifest owns %s", key)
				}
			}
			c := m["cases"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
			if c["mode"] != nil || !reflect.DeepEqual(c["scenario"].(map[string]any)["enum"], coverage.Scenarios()) {
				t.Fatal("case semantics differ")
			}
		}
		if handler == "engine.qa.review.rationale.v2" {
			r := fields
			for _, key := range []string{"binding", "scope", "setStatus", "unboundSources"} {
				if r[key] != nil {
					t.Fatalf("review owns %s", key)
				}
			}
		}
		if handler == "engine.qa.execution.v4" {
			e := fields
			if e["binding"] != nil || e["notExecuted"] != nil {
				t.Fatal("model owns execution facts")
			}
			r := e["records"].(map[string]any)["prefixItems"].([]any)[0].(map[string]any)["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
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
	for _, role := range []string{"engine.development.batch.v6", "engine.repair.v7"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Handler: role, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)
			tuple := fields["dod"].(map[string]any)
			if tuple["minItems"] != len(batch.DoD) || tuple["items"] != false {
				t.Fatal("completion does not use issued criteria order")
			}
			for _, raw := range tuple["prefixItems"].([]any) {
				item := raw.(map[string]any)
				if item["type"] != "string" || item["minLength"] != 1 {
					t.Fatal("semantic evidence lost")
				}
			}
			if !strings.Contains(material.Prompt, "do not copy IDs") || !strings.Contains(material.Prompt, "Approved command execution") {
				t.Fatal("completion owner missing")
			}

			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("schema/material mutated issued input")
			}
		})
	}
	for _, name := range []string{"no-batch", "empty-batch", "unfrozen-repair", "final-verification", "other-role"} {
		t.Run(name, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.repair.v7", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
			switch name {
			case "empty-batch":
				input.Validation.Development = &protocol.DevelopmentInput{}
			case "unfrozen-repair":
				input.Validation.Repair = &protocol.RepairInput{}
			case "final-verification":
				input.Validation.Development = &protocol.DevelopmentInput{Batch: batch}
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "final"}}
			case "other-role":
				input.Handler = "engine.qa.design.v6"
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
				for _, rule := range []string{"issued development batch or a proposed single batch", "Without either, omit completion", "report summary"} {
					if !strings.Contains(material.Prompt, rule) {
						t.Fatalf("repair output instruction missing %q", rule)
					}
				}
				if development["completion"] != nil {
					t.Fatal("unplanned repair exposed completion without a batch")
				}
				proposed := schema["anyOf"].([]any)[3].(map[string]any)["properties"].(map[string]any)
				completion := proposed["completion"].(map[string]any)["properties"].(map[string]any)
				if completion["dod"].(map[string]any)["type"] != "array" {
					t.Fatal("single proposed repair batch cannot supply authored DoD evidence")
				}
			} else if development["completion"] != nil {
				t.Fatal("non-batch duty exposed unused completion claims")
			}
		})
	}
}

func TestUnplannedRepairCompletionRequiresSingleProposal(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.repair.v7", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Cases: []coverage.AcceptanceCase{{CaseID: "normal-input"}}}}}}}
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
	batch := Batch{DoD: []Criterion{{Description: "Normal input"}}}
	for _, scenario := range []string{"small-repair", "single-proposal", "split-plan", "unplanned-completion", "split-completion"} {
		t.Run(scenario, func(t *testing.T) {
			result := Repair{Summary: "Implemented the confirmed command", AffectedCases: []int{1}}
			if scenario != "small-repair" && scenario != "unplanned-completion" {
				result.GranularityReview = &Granularity{Reason: "One command boundary", VerificationTier: "FINAL", Batches: []Batch{batch}}
			}
			if scenario == "split-plan" || scenario == "split-completion" {
				result.GranularityReview.Batches = append(result.GranularityReview.Batches, Batch{})
			}
			if scenario == "split-plan" {
				result.Summary, result.AffectedCases = "", nil
			} else if scenario != "small-repair" {
				result.Completion = &Completion{DoD: []string{"Implemented"}, Handoff: "Ready for approved checks"}
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			event, err := DecodeResultWithPaths(input, "result", raw, "original-native-output", []string{})
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

func TestRepairPlanSchemaPublishesOnlyOriginalChoicesOrNewSemanticContent(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.repair.v7", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "approved-build"}}}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	branches := schema["anyOf"].([]any)
	proposal := branches[0].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if proposal["artifacts"].(map[string]any)["items"].(map[string]any)["type"] != "object" {
		t.Fatal("proposal still asks for existing raw paths")
	}
	checks := proposal["checks"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(checks) != 2 || checks[0].(map[string]any)["properties"].(map[string]any)["reuse"] == nil {
		t.Fatal("approved commands have no numeric selection")
	}
	for index, choice := range checks {
		branch := choice.(map[string]any)
		if branch["additionalProperties"] != false || len(branch["properties"].(map[string]any)) != []int{1, 2}[index] {
			t.Fatal("check modes accept fields from another selection mode")
		}
	}
	authored := checks[1].(map[string]any)["properties"].(map[string]any)
	if !reflect.DeepEqual(checks[1].(map[string]any)["required"], []string{"command"}) || authored["read"] != nil || authored["parts"] != nil || !reflect.DeepEqual(authored["purpose"].(map[string]any)["enum"], []string{""}) {
		t.Fatal("new repair invocation lost its authorship or local-command policy")
	}
	command := authored["command"].(map[string]any)
	if !reflect.DeepEqual(command["not"].(map[string]any)["enum"], []string{"approved-build"}) {
		t.Fatal("known command can pass as newly authored content")
	}
	completed := branches[3].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
	fields := completed["properties"].(map[string]any)
	if fields["artifacts"] != nil || fields["newArtifacts"] != nil || completed["anyOf"] != nil {
		t.Fatal("completed repair publishes a duplicate artifact declaration")
	}
	input.ApprovedChecks = nil
	schema, err = ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	proposal = schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	choices := proposal["checks"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(choices) != 1 || choices[0].(map[string]any)["properties"].(map[string]any)["command"] == nil {
		t.Fatal("initial repair invocation must be developer-authored")
	}
}

func TestPlanRouteSchemaSeparatesFullAndCustomChoices(t *testing.T) {
	for _, handler := range []string{"engine.readiness.plan.v6", "engine.repair.v7"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "plan", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed public behavior"}, AvailableRouteCandidates: []string{"quality", "complexity"}}
			if handler == "engine.readiness.plan.v6" {
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			} else {
				input.Validation = &protocol.ValidationInput{Repair: &protocol.RepairInput{}}
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, body := range schema["anyOf"].([]any) {
				plan, ok := body.(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)
				if !ok {
					continue
				}
				batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
				route := batch["properties"].(map[string]any)["route"].(map[string]any)
				if route["properties"] != nil {
					t.Fatal("route publishes an unqualified mode/subset combination")
				}
				branches := route["anyOf"].([]any)
				if len(branches) != 2 {
					t.Fatal("route must separate complete and subset selection")
				}
				full := branches[0].(map[string]any)
				fullFields := full["properties"].(map[string]any)
				if full["additionalProperties"] != false || len(fullFields) != 1 || !reflect.DeepEqual(fullFields["mode"].(map[string]any)["enum"], []string{"full"}) || !reflect.DeepEqual(full["required"], []string{"mode"}) {
					t.Fatal("full route asks the model for a gate subset")
				}
				custom := branches[1].(map[string]any)
				customFields := custom["properties"].(map[string]any)
				selected := customFields["selected"].(map[string]any)
				if custom["additionalProperties"] != false || len(customFields) != 2 || !reflect.DeepEqual(customFields["mode"].(map[string]any)["enum"], []string{"custom"}) || !reflect.DeepEqual(custom["required"], []string{"mode", "selected"}) || selected["minItems"] != 1 || selected["uniqueItems"] != true || !reflect.DeepEqual(selected["items"].(map[string]any)["enum"], []int{1, 2}) {
					t.Fatal("custom route permits missing, repeated or unissued gate choices")
				}
			}
			input.AvailableRouteCandidates = nil
			material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}, {ID: "development-worker"}}})
			if err != nil {
				t.Fatal(err)
			}
			schema = material.ResultSchema
			for _, body := range schema["anyOf"].([]any) {
				if plan, ok := body.(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any); ok {
					batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
					if batch["properties"].(map[string]any)["route"] != nil {
						t.Fatal("empty gate inventory publishes an unusable route choice")
					}
				}
			}
		})
	}
}

func TestRepairCompletionSchemaRequiresSummaryWithoutChangingAlternatives(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.repair.v7", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	for _, scenario := range []string{"proposed", "issued"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "issued" {
				input.Validation.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "issued", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "approved behavior"}}}}
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(wire, &schema); err != nil {
				t.Fatal(err)
			}
			completed := false
			for _, raw := range schema["anyOf"].([]any) {
				branch := raw.(map[string]any)
				fields := branch["properties"].(map[string]any)
				required := branch["required"].([]any)
				switch {
				case fields["completion"] != nil:
					completed = true
					if !slices.Contains(required, any("summary")) || fields["summary"].(map[string]any)["minLength"] != float64(1) {
						t.Fatal("repair completion permits an absent or empty summary")
					}
				case fields["granularityReview"] != nil:
					if slices.Contains(required, any("summary")) {
						t.Fatal("pure repair proposal requires completion summary")
					}
				case fields["remainingWork"] != nil:
					for _, name := range []string{"summary", "remainingWork"} {
						if !slices.Contains(required, any(name)) || fields[name].(map[string]any)["minLength"] != float64(1) {
							t.Fatalf("continuation lost required %s", name)
						}
					}
				case fields["interruptionReason"] != nil:
					if fields["summary"] != nil || !slices.Contains(required, any("interruptionReason")) {
						t.Fatal("inability requires completion summary")
					}
				}
			}
			if !completed {
				t.Fatal("repair completion alternative lost")
			}
		})
	}
}

func TestValidationRoleStatusUsesOnlyOwnedFiniteValues(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog.Gates = []prompts.PromptDefinition{{ID: "quality", Content: "Judge"}}
	for _, role := range []struct {
		handler  string
		statuses []string
	}{{"engine.development.batch.v6", nil}, {"engine.repair.v7", nil}, {"engine.merge.resolve.v3", nil}, {"engine.qa.design.v6", nil}, {"engine.gate.v3", nil}, {"engine.qa.review.rationale.v2", nil}, {"engine.qa.execution.v4", nil}} {
		t.Run(role.handler, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Handler: role.handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}}, Gate: &protocol.GateInput{Gate: "quality"}}}
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
			if !material.ReadOnly && !strings.Contains(material.Prompt, "Do not execute any VCS write operation; all VCS writes belong to engine adapters.") {
				t.Fatal("writable role lost adapter ownership or read-only inspection")
			}
			if strings.Contains(material.Prompt, "Do not stage, commit, submit or push") {
				t.Fatal("partial VCS prohibition remained")
			}
		})
	}
	t.Run("whitebox-design-vcs-owner", func(t *testing.T) {
		input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, RunID: "run", Handler: "engine.qa.design.v6", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}}
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
	input.Handler = "engine.qa.execution.v4"
	input.RequirementDocument.Content = "confirmed requirement"
	input.Validation.Execution = &coverage.ExecutionBinding{Candidate: coverage.ValidationCandidate{Identity: "candidate"}}
	input.Validation.RequiredCases = []string{"new", "prior"}
	input.Validation.Completed = []coverage.ExecutionRecord{
		{CaseID: "new", Result: coverage.ExecutedFail},
		{CaseID: "prior", Result: coverage.ExecutedPass},
	}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Execution: *input.Validation.Execution, CaseID: "new", Source: "case:new", Ordinal: 1, Operation: coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"actual-tool", "--negative"}}, Directory: "."}}, Evidence: protocol.CheckEvidence{Tool: "actual-tool", Environment: "actual-environment"}}}}}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{"program derives checkpoint and overall status", "Completed FAIL judgments are complete", "return records []", "Missing native facts never establish PASS"} {
		if !strings.Contains(material.Prompt, instruction) {
			t.Fatalf("final report instruction missing: %s", instruction)
		}
	}
	fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	if fields["status"] != nil || fields["checkpoint"] != nil {
		t.Fatal("model owns derived execution summaries")
	}
	records := fields["records"].(map[string]any)
	if _, present := records["prefixItems"]; present || records["items"] != false || records["minItems"] != 0 || records["maxItems"] != 0 {
		t.Fatal("completed judgments were requested again")
	}
	body := decodeSemantic(t, input, ExecutionJudgments{Execution: Execution{Records: []*Observation{}}}).Validation
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

func TestOrderedSchemaPreservesExactEmptyAndNonemptyDuties(t *testing.T) {
	for _, items := range [][]any{nil, {}, {map[string]any{"type": "string"}}} {
		schema := orderedSchema(items)
		if schema["type"] != "array" || schema["items"] != false || schema["minItems"] != len(items) || schema["maxItems"] != len(items) {
			t.Fatalf("ordered duty bounds changed: %#v", schema)
		}
		prefix, present := schema["prefixItems"]
		if len(items) == 0 {
			if present {
				t.Fatalf("empty duty emitted prefixItems: %#v", prefix)
			}
		} else if !present || !reflect.DeepEqual(prefix, items) {
			t.Fatalf("ordered duty items changed: %#v", prefix)
		}
	}
}

func TestStableResultSchemaRetainsOnlyRoleShapesAcrossContinuations(t *testing.T) {
	commands := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.qa.execution.v4", Validation: &protocol.ValidationInput{CommandCases: []string{"first"}}}
	judgments := commands
	judgments.Validation = &protocol.ValidationInput{PendingCases: []string{"first", "second"}}
	first, err := StableResultSchema(commands)
	if err != nil {
		t.Fatal(err)
	}
	second, err := StableResultSchema(judgments)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("same role changed its persistent tool contract", err)
	}
	branches := first["anyOf"].([]any)
	if len(branches) != 3 {
		t.Fatal("execution omitted command, judgment or inability shape")
	}
	for _, branch := range branches {
		item := branch.(map[string]any)
		if item["additionalProperties"] != false {
			t.Fatal("role exposes unknown fields")
		}
		for _, identity := range []string{"actionId", "task", "binding", "candidate", "provider", "granularityReview"} {
			if item["properties"].(map[string]any)[identity] != nil {
				t.Fatal("execution exposes another role or machine identity", identity)
			}
		}
	}
	fields := branches[1].(map[string]any)["properties"].(map[string]any)
	if fields["records"].(map[string]any)["prefixItems"] != nil || fields["records"].(map[string]any)["maxItems"] != nil {
		t.Fatal("first turn froze later execution cardinality")
	}
	if fields["records"].(map[string]any)["type"] != "array" {
		t.Fatal("judgment record type is not constrained")
	}
	development := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.development.batch.v6", Validation: &protocol.ValidationInput{}}
	stable, err := StableResultSchema(development)
	if err != nil {
		t.Fatal(err)
	}
	if len(stable["anyOf"].([]any)) != 3 {
		t.Fatal("development omitted completion, continuation or inability")
	}
	changed := semanticInput()
	changed.Validation.AffectedCases = nil
	original, err := StableResultSchema(changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.Validation.AffectedCases = []string{"new"}
	revision, err := StableResultSchema(changed)
	if err != nil || !reflect.DeepEqual(original, revision) {
		t.Fatal("case revisions replaced the registered role", err)
	}
}

func TestReadinessExposesOnlyApprovedReuseAndVerificationGoals(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.readiness.plan.v6", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "edit", Purpose: protocol.CheckEdit, Command: "project-edit"}}}
	for _, generate := range []func(protocol.WorkerInput) (map[string]any, error){ResultSchema, StableResultSchema} {
		schema, err := generate(input)
		if err != nil {
			t.Fatal(err)
		}
		body := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		plan := body["granularityReview"].(map[string]any)
		batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
		batches := []any{batch}
		if alternatives, ok := batch["anyOf"].([]any); ok && batch["properties"] == nil {
			batches = alternatives
		}
		for _, batch := range batches {
			fields := batch.(map[string]any)["properties"].(map[string]any)
			checks := fields["checks"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
			if len(checks) != 2 {
				t.Fatal("review check choices must be approved reuse and a verification goal", checks)
			}
			for _, check := range checks {
				branch := check.(map[string]any)
				properties := branch["properties"].(map[string]any)
				for _, field := range []string{"command", "parts", "read", "from", "to", "fromColumn", "toColumn"} {
					if properties[field] != nil {
						t.Fatalf("review still selects command text through %s", field)
					}
				}
				if properties["reuse"] != nil {
					if len(properties) != 1 || !reflect.DeepEqual(branch["required"], []string{"reuse"}) {
						t.Fatal("approved reuse exposes authored command fields", branch)
					}
				} else if properties["goal"] == nil || len(properties) != 2 || !reflect.DeepEqual(properties["purpose"].(map[string]any)["enum"], protocol.DevelopmentCheckPurposes()) || !reflect.DeepEqual(branch["required"], []string{"goal"}) {
					t.Fatal("review goal lost its ordinary or full verification purposes", branch)
				}
			}
		}
	}
}

func TestNewBatchSchemaRequiresDeliveryAndVerification(t *testing.T) {
	for _, handler := range []string{"engine.readiness.plan.v6", "engine.repair.v7"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: handler, Validation: &protocol.ValidationInput{}}
			if handler == "engine.readiness.plan.v6" {
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			plan := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)
			batch := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
			fields := batch["properties"].(map[string]any)
			for _, name := range []string{"subtasks", "dod", "checks"} {
				if fields[name].(map[string]any)["minItems"] != 1 {
					t.Errorf("new batch permits empty %s", name)
				}
				if !slices.Contains(batch["required"].([]any), any(name)) {
					t.Errorf("new batch permits omitted %s", name)
				}
			}
			delivery, ok := batch["anyOf"].([]any)
			if !ok || len(delivery) != 2 {
				t.Error("new batch permits both original and new delivery artifacts to be absent")
				return
			}
			for i, name := range []string{"artifacts", "newArtifacts"} {
				choice := delivery[i].(map[string]any)
				if !reflect.DeepEqual(choice["required"], []string{name}) || choice["properties"].(map[string]any)[name].(map[string]any)["minItems"] != 1 {
					t.Errorf("new batch permits an empty delivery choice: %s", name)
				}
			}
		})
	}
}

func TestNewBatchDeliverySourcesSatisfyGranularity(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v6", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	facts := []LocationFact{{Kind: "read", Paths: []string{"existing.py"}}}
	for _, sample := range []struct {
		name         string
		artifacts    []Location
		newArtifacts []string
		want         []string
	}{
		{"original-file", []Location{{Read: 1}}, nil, []string{"existing.py"}},
		{"new-file", nil, []string{"created.py"}, []string{"created.py"}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			proposal := &Granularity{Reason: "One public behavior", VerificationTier: "FINAL", Batches: []Batch{{Subtasks: []Subtask{{Description: "Implement the public behavior"}}, Deliverable: "Public behavior", PublicEntry: "Product command", Artifacts: sample.artifacts, NewArtifacts: sample.newArtifacts, Interfaces: "Command output", StateBoundary: "Project files", Risk: "Local change", DoD: []Criterion{{Description: "Public behavior holds"}}, Checks: []Check{{Goal: "Verify public behavior"}}, Rollback: "Restore base", Recovery: "Resume the duty", Handoff: "Ready for validation", ContextCost: "One cohesive duty"}}}
			plan, err := DecodeGranularity(input, proposal, nil, facts, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateGranularity(plan); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(plan.Batches[0].Artifacts, sample.want) {
				t.Fatal("delivery selection did not retain the original file or new path", plan.Batches[0].Artifacts)
			}
		})
	}
}

func TestApprovedBatchSchemaRetainsOmittedContract(t *testing.T) {
	original := nativePlanForInheritance()
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.readiness.plan.v6", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}, ApprovedPlan: original}
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	plan := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)
	changed := plan["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)[1].(map[string]any)
	if !reflect.DeepEqual(changed["required"], []any{"reuse"}) || changed["anyOf"] != nil {
		t.Fatal("approved batch change requires a new contract", changed)
	}
	for _, name := range []string{"subtasks", "dod", "checks"} {
		if changed["properties"].(map[string]any)[name].(map[string]any)["minItems"] != nil {
			t.Errorf("approved batch change inherited a new-batch constraint: %s", name)
		}
	}
	var proposal Granularity
	if err := json.Unmarshal([]byte(`{"batches":[{"reuse":1}]}`), &proposal); err != nil {
		t.Fatal(err)
	}
	retained, err := DecodeGranularity(input, &proposal, nil, nil, false)
	if err != nil || !reflect.DeepEqual(retained, original) {
		t.Fatal("minimal batch reuse did not retain the approved contract", err)
	}
	if err := protocol.ValidateGranularity(retained); err != nil {
		t.Fatal(err)
	}
}

func TestNewBatchSchemaMatchesInheritedRepairChecks(t *testing.T) {
	build := protocol.DevelopmentCheck{ID: "build", Purpose: protocol.CheckFullBuild, Command: "project-build 'two words'\n"}
	tests := protocol.DevelopmentCheck{ID: "tests", Purpose: protocol.CheckFullTests, Command: "project-tests --all"}
	edit := protocol.DevelopmentCheck{ID: "edit", Purpose: protocol.CheckEdit, Command: "project-edit --source"}
	ordinary := protocol.DevelopmentCheck{ID: "regression", Command: "project-regression"}
	for _, sample := range []struct {
		name, handler string
		approved      []protocol.DevelopmentCheck
		inherited     []protocol.DevelopmentCheck
	}{
		{"readiness-approved", "engine.readiness.plan.v6", []protocol.DevelopmentCheck{build, tests, edit}, nil},
		{"repair-no-approved", "engine.repair.v7", nil, nil},
		{"repair-ordinary-only", "engine.repair.v7", []protocol.DevelopmentCheck{ordinary}, nil},
		{"repair-full-build", "engine.repair.v7", []protocol.DevelopmentCheck{build}, []protocol.DevelopmentCheck{build}},
		{"repair-full-tests", "engine.repair.v7", []protocol.DevelopmentCheck{tests}, []protocol.DevelopmentCheck{tests}},
		{"repair-edit", "engine.repair.v7", []protocol.DevelopmentCheck{edit}, []protocol.DevelopmentCheck{edit}},
		{"repair-project-checks", "engine.repair.v7", []protocol.DevelopmentCheck{build, tests, edit}, []protocol.DevelopmentCheck{build, tests, edit}},
		{"repair-project-and-ordinary", "engine.repair.v7", []protocol.DevelopmentCheck{ordinary, build, tests, edit}, []protocol.DevelopmentCheck{build, tests, edit}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: sample.handler, ApprovedChecks: sample.approved, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
			if sample.handler == "engine.readiness.plan.v6" {
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			planSchema := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["granularityReview"].(map[string]any)
			batchSchema := planSchema["properties"].(map[string]any)["batches"].(map[string]any)["items"].(map[string]any)
			needsChecks := len(sample.inherited) == 0
			if slices.Contains(batchSchema["required"].([]any), any("checks")) != needsChecks {
				t.Errorf("schema check omission differs from inherited verification: %s", sample.name)
			}
			minimum := batchSchema["properties"].(map[string]any)["checks"].(map[string]any)["minItems"]
			if needsChecks && minimum != 1 || !needsChecks && minimum != nil {
				t.Errorf("schema empty check array differs from inherited verification: %s", sample.name)
			}
			for _, checks := range []string{"", `,"checks":[]`} {
				var proposal Granularity
				raw := `{"reason":"One cohesive repair","verificationTier":"FINAL","batches":[{"subtasks":[{"description":"Restore public behavior"}],"dod":[{"description":"Public behavior holds"}],"deliverable":"Public behavior","publicEntry":"Product command","newArtifacts":["created.py"],"interfaces":"Command output","stateBoundary":"Project files","risk":"Local change","rollback":"Restore base","recovery":"Resume original duty","handoff":"Ready for validation","contextCost":"One cohesive duty"` + checks + `}]}`
				if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
					t.Fatal(err)
				}
				plan, err := DecodeGranularity(input, &proposal, nil, nil, false)
				if err != nil || !reflect.DeepEqual(plan.Batches[0].Checks, sample.inherited) {
					t.Fatal("original check IDs, purposes or command bytes changed during inheritance", err)
				}
				if err := protocol.ValidateGranularity(plan); (err == nil) != !needsChecks {
					t.Fatalf("check omission does not match the accepted batch invariant: %v", err)
				}
			}
		})
	}
}
