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
		"engine.development.batch.v3": {"summary"}, "engine.repair.v3": {"summary", "granularityReview"}, "engine.merge.resolve.v2": {"summary"}, "engine.qa.design.v2": {"cases", "coverage", "sharedSteps", "replace", "removeCases", "removeSharedSteps", "checkpoint"}, "engine.qa.review.rationale.v2": {"sourceDecisions", "pointDecisions", "caseDecisions", "sharedStepDecisions", "suggestions"}, "engine.qa.execution.v2": {"records", "findings"}, "engine.gate.v2": {"findings"},
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

func TestObservationSchemaRequiresOrdinarySeverityAndExcludesExecutionSeverity(t *testing.T) {
	input := observationWorkerInput()
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

func TestReviewMaterialRequiresEveryVerificationEvidence(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "product-review"}, {ID: "start-readiness"}}}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		handler := "engine.review.worker.v2"
		if action == review.StartReadiness {
			handler = "engine.readiness.plan.v3"
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
		if checks["minItems"] != len(review.RequiredVerificationChecks()) || checks["items"] != false {
			t.Fatal("lost individual checks")
		}
		value := Review{Findings: []Finding{}, Verification: []string{}}
		for _, key := range review.RequiredVerificationChecks() {
			value.Verification = append(value.Verification, "observed "+key)
		}
		body, _ := json.Marshal(value)
		event, err := DecodeResultWithPaths(input, "event", body, "original", []string{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(event.Result.Review.Verification.Checks, review.RequiredVerificationChecks()) || event.Result.Review.RequirementRevision != "revision" {
			t.Fatal("wrong original binding")
		}
		for index, key := range review.RequiredVerificationChecks() {
			original := slices.Clone(value.Verification)
			value.Verification = append(value.Verification[:index], value.Verification[index+1:]...)
			body, _ = json.Marshal(value)
			if _, err := DecodeResultWithPaths(input, "event", body, "original", []string{}); err == nil {
				t.Fatalf("missing %s accepted", key)
			}
			value.Verification = original
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
		{"engine.development.batch.v3", "", true},
		{"engine.repair.v3", "", true},
		{"engine.merge.resolve.v2", "", true},
		{"engine.qa.design.v2", coverage.ReviewWhitebox, true},
		{"engine.qa.design.v2", coverage.ReviewBlackbox, false},
		{"engine.qa.execution.v2", "", false},
		{"engine.qa.review.rationale.v2", "", false},
	} {
		t.Run(role.handler+string(role.kind), func(t *testing.T) {
			input := protocol.WorkerInput{Handler: role.handler, Provider: "zcode", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: role.kind}}}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			contracts := []string{"Deliver only actual authorized artifacts", "Temporary build and test outputs are not delivery artifacts", "remove your own temporary validation outputs", "produce them outside the workspace"}
			if role.handler == "engine.qa.design.v2" && role.author {
				contracts = []string{"Associate only actual test material files", "Remove temporary build and test outputs", "produce them outside the workspace", "Do not return a separate development path declaration"}
			}
			if role.handler == "engine.merge.resolve.v2" {
				contracts = []string{"Report only the semantic resolution summary", "program supplies the exact original conflict paths", "Remove your temporary build and test outputs", "produce them outside the workspace"}
			}
			for _, contract := range contracts {
				if strings.Contains(material.Prompt, contract) != role.author {
					t.Fatalf("role %s delivery contract mismatch: %s", role.handler, contract)
				}
			}
			if role.handler == "engine.qa.design.v2" && role.author {
				fields := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
				if fields["paths"] != nil || strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") {
					t.Fatal("QA author material retained a duplicate path-reporting contract")
				}
			} else if role.handler == "engine.merge.resolve.v2" {
				if strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") || strings.Contains(material.Prompt, "exact changed paths") {
					t.Fatal("conflict resolution requests issued paths")
				}
			} else if role.author && !strings.Contains(material.Prompt, "Deliver only actual authorized artifacts") {
				t.Fatal("development author lost its artifact declaration")
			}
			if role.handler == "engine.qa.execution.v2" && !strings.Contains(material.Prompt, "Compare the issued complete native execution facts") {
				t.Fatal("execution runtime contract lost")
			}
		})
	}
}

func TestQASchemaExcludesIssuedFacts(t *testing.T) {
	for _, handler := range []string{"engine.qa.design.v2", "engine.qa.review.rationale.v2", "engine.qa.execution.v2"} {
		input := protocol.WorkerInput{Handler: handler, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}, PendingCases: []string{"case"}, RequiredCases: []string{"case"}}}
		input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "case"}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if handler == "engine.qa.design.v2" {
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
		if handler == "engine.qa.execution.v2" {
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
	for _, role := range []string{"engine.development.batch.v3", "engine.repair.v3"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Handler: role, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
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
			input := protocol.WorkerInput{Handler: "engine.repair.v3", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
			switch name {
			case "empty-batch":
				input.Validation.Development = &protocol.DevelopmentInput{}
			case "unfrozen-repair":
				input.Validation.Repair = &protocol.RepairInput{}
			case "final-verification":
				input.Validation.Development = &protocol.DevelopmentInput{Batch: batch}
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "final"}}
			case "other-role":
				input.Handler = "engine.qa.design.v2"
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
	input := protocol.WorkerInput{Handler: "engine.repair.v3", Provider: "host", Pending: protocol.PendingAction{ActionID: "repair"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Cases: []coverage.AcceptanceCase{{CaseID: "normal-input"}}}}}}}
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
	input := protocol.WorkerInput{Handler: "engine.repair.v3", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "approved-build"}}}
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
	command := checks[1].(map[string]any)["properties"].(map[string]any)["command"].(map[string]any)
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
	check := proposal["checks"].(map[string]any)["items"].(map[string]any)
	if check["anyOf"] != nil || check["properties"].(map[string]any)["reuse"] != nil {
		t.Fatal("empty approved inventory publishes an unusable selection")
	}
}

func TestPlanRouteSchemaSeparatesFullAndCustomChoices(t *testing.T) {
	for _, handler := range []string{"engine.readiness.plan.v3", "engine.repair.v3"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "plan", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed public behavior"}, AvailableRouteCandidates: []string{"quality", "complexity"}}
			if handler == "engine.readiness.plan.v3" {
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
	input := protocol.WorkerInput{Handler: "engine.repair.v3", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
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
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker", Content: "Develop"}, {ID: "qa-design", Content: "Design"}, {ID: "qa-review", Content: "Review"}, {ID: "qa-execution", Content: "Execute"}}, Gates: []prompts.PromptDefinition{{ID: "quality", Content: "Judge"}}}
	for _, role := range []struct {
		handler  string
		statuses []string
	}{{"engine.development.batch.v3", nil}, {"engine.repair.v3", nil}, {"engine.merge.resolve.v2", nil}, {"engine.qa.design.v2", nil}, {"engine.gate.v2", nil}, {"engine.qa.review.rationale.v2", nil}, {"engine.qa.execution.v2", nil}} {
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
		input := protocol.WorkerInput{RunID: "run", Handler: "engine.qa.design.v2", Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}}}}
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
	input.Handler = "engine.qa.execution.v2"
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
	material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "qa-execution"}}})
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
	if len(records["prefixItems"].([]any)) != 0 || records["items"] != false {
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
