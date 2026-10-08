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
)

func TestResultSchemaUsesSemanticRoleContract(t *testing.T) {
	roles := map[string][]string{
		"engine.development.batch": {"status", "development"}, "engine.repair": {"status", "development", "granularityReview"}, "engine.merge.resolve": {"status", "resolution"}, "engine.qa.design": {"status", "manifest", "development", "checkpoint"}, "engine.qa.review.rationale": {"status", "review", "suggestions"}, "engine.qa.execution": {"status", "execution", "tool", "environment", "checkpoint", "findings"}, "engine.gate": {"status", "findings"},
	}
	for handler, names := range roles {
		input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		outer := schema["properties"].(map[string]any)
		for _, key := range []string{"actionId", "provider", "payloadDigest"} {
			if outer[key] != nil {
				t.Fatalf("model owns %s", key)
			}
		}
		fields := outer["validation"].(map[string]any)["properties"].(map[string]any)
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
		fields := material.ResultSchema["properties"].(map[string]any)["review"].(map[string]any)["properties"].(map[string]any)
		for _, key := range []string{"action", "dispatchId", "requirementRevision", "authorizationId"} {
			if fields[key] != nil {
				t.Fatalf("model owns %s", key)
			}
		}
		checks := fields["verification"].(map[string]any)
		if !reflect.DeepEqual(checks["required"], review.RequiredVerificationChecks()) {
			t.Fatal("lost individual checks")
		}
		value := material.ResultTemplate
		value.Outcome = "PASS"
		value.Review.Status = "PASS"
		value.Review.Findings = []Finding{}
		for _, key := range review.RequiredVerificationChecks() {
			value.Review.Verification[key] = "observed " + key
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
			delete(value.Review.Verification, key)
			body, _ = json.Marshal(value)
			if _, err := DecodeResult(input, "event", body, "original"); err == nil {
				t.Fatalf("missing %s accepted", key)
			}
			value.Review.Verification[key] = "observed " + key
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
			for _, contract := range []string{"Declare only the files actually delivered as artifacts", "Temporary build and test outputs are not delivery artifacts", "remove your own temporary validation outputs", "produce them outside the workspace"} {
				if strings.Contains(material.Prompt, contract) != role.author {
					t.Fatalf("role %s delivery contract mismatch: %s", role.handler, contract)
				}
			}
			if role.handler == "engine.qa.execution" && !strings.Contains(material.Prompt, "normal test runtime outputs may be produced") {
				t.Fatal("execution runtime contract lost")
			}
		})
	}
}

func TestQASchemaExcludesIssuedFacts(t *testing.T) {
	for _, handler := range []string{"engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution"} {
		input := protocol.WorkerInput{Handler: handler, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}, Sources: []string{"source"}, Points: []coverage.AcceptancePoint{{PointID: "point"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}, PendingCases: []string{"case"}, RequiredCases: []string{"case"}}}
		schema, err := ResultSchema(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)
		if fields["manifest"] != nil {
			m := fields["manifest"].(map[string]any)["properties"].(map[string]any)
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
		if fields["review"] != nil {
			r := fields["review"].(map[string]any)["properties"].(map[string]any)
			for _, key := range []string{"binding", "scope", "setStatus", "unboundSources"} {
				if r[key] != nil {
					t.Fatalf("review owns %s", key)
				}
			}
		}
		if fields["execution"] != nil {
			e := fields["execution"].(map[string]any)["properties"].(map[string]any)
			if e["binding"] != nil || e["notExecuted"] != nil {
				t.Fatal("model owns execution facts")
			}
			r := e["records"].(map[string]any)["properties"].(map[string]any)["case"].(map[string]any)["properties"].(map[string]any)
			for _, key := range []string{"caseId", "reviewKind", "sourceId", "pointId", "provenance", "reuseEvidence", "inheritedFromCandidate"} {
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
			fields := material.ResultSchema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)["development"].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)["properties"].(map[string]any)
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
			development := schema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)["development"].(map[string]any)["properties"].(map[string]any)
			if name == "unfrozen-repair" {
				completion := development["completion"].(map[string]any)["properties"].(map[string]any)
				if _, ok := completion["dod"].(map[string]any)["additionalProperties"].(map[string]any); !ok {
					t.Fatal("single proposed repair batch cannot supply authored DoD evidence")
				}
			} else if development["completion"] != nil {
				t.Fatal("non-batch duty exposed unused completion claims")
			}
		})
	}
}

func TestValidationRoleStatusUsesOnlyOwnedFiniteValues(t *testing.T) {
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "development-worker", Content: "Develop"}, {ID: "qa-design", Content: "Design"}, {ID: "qa-review", Content: "Review"}, {ID: "qa-execution", Content: "Execute"}}, Gates: []prompts.PromptDefinition{{ID: "quality", Content: "Judge"}}}
	for _, role := range []struct {
		handler  string
		statuses []string
	}{{"engine.development.batch", []string{"PASS"}}, {"engine.repair", []string{"PASS"}}, {"engine.merge.resolve", []string{"PASS"}}, {"engine.qa.design", []string{"PASS"}}, {"engine.gate", []string{"PASS", "FAIL"}}, {"engine.qa.review.rationale", nil}, {"engine.qa.execution", nil}} {
		t.Run(role.handler, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Handler: role.handler, Provider: "host", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed"}, Validation: &protocol.ValidationInput{Manifest: coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewBlackbox}}, Gate: &protocol.GateInput{Gate: "quality"}}}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			status := material.ResultSchema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)["status"].(map[string]any)
			if status["type"] != "string" {
				t.Fatal("status wire type changed")
			}
			if role.statuses == nil {
				if _, exists := status["enum"]; exists {
					t.Fatal("role without a core status restriction acquired one")
				}
			} else {
				if !reflect.DeepEqual(status["enum"], role.statuses) {
					t.Fatalf("wrong role status domain: %v", status)
				}
				values, _ := json.Marshal(role.statuses)
				if !strings.Contains(material.Prompt, "validation.status must use exactly one of these machine values: "+string(values)) {
					t.Fatal("prompt differs from schema's finite domain")
				}
			}
			if material.ResultTemplate.Validation.Status != "" {
				t.Fatal("template fabricated success")
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
