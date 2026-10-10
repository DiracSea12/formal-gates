package worker

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestActionMaterialUsesSemanticResultFields(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.development.batch", "engine.repair", "engine.repair.verify", "engine.qa.design", "engine.readiness.plan"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Pending.AttemptID = "attempt"
			if handler == "engine.repair.verify" {
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "issued-candidate"}, VerificationCases: []string{"new"}}
			}
			if handler == "engine.readiness.plan" {
				input.Validation = nil
				input.Review = &protocol.WorkerReviewInput{Action: review.StartReadiness}
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"报告实际修改的精确路径", "提交原 `DevelopmentEvidence`", "保留 affectedCases 及逐项 caseRuns", "返回语义字段（mode", "`test` =", "返回的 `manifest.sharedSteps`", "`granularity_review`"} {
				if strings.Contains(material.Prompt, forbidden) {
					t.Fatalf("action requests a removed result field: %s", forbidden)
				}
			}
			if !strings.Contains(material.Prompt, "Issued identities, bindings") || !strings.Contains(material.Prompt, "Do not return status, paths") {
				t.Fatal("action material lost program ownership of mechanical facts")
			}
		})
	}
}

func TestMaterialProvidesRequirementBodyOnceWithoutChangingIssuedInput(t *testing.T) {
	document := requirement.FrozenRequirementProjection{Path: "requirements.md", ContentDigest: "confirmed-digest", Content: "Complete confirmed requirement body.", Requirements: []requirement.FrozenRequirement{{ID: "REQ-001", Title: "behavior", Requirement: "required behavior"}}}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "product-review"}, {ID: "qa-design"}}}
	for _, handler := range []string{"engine.review.worker", "engine.qa.design"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: document}
			if handler == "engine.review.worker" {
				input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
			} else {
				input.Validation = &protocol.ValidationInput{RequirementDocument: document}
			}
			original, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(material.Prompt, document.Content) != 1 || !strings.Contains(material.Prompt, "[Current requirement]\n"+document.Content) {
				t.Fatal("model material must supply the complete requirement body exactly once")
			}
			actionInput := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var projection protocol.WorkerInput
			if err := json.Unmarshal([]byte(actionInput), &projection); err != nil {
				t.Fatal(err)
			}
			wantProjection := document
			wantProjection.Content = ""
			if !reflect.DeepEqual(projection.RequirementDocument, wantProjection) || projection.Validation != nil && projection.Validation.RequirementDocument.Content != "" {
				t.Fatal("model action context must retain requirement identity and coverage without repeating its body")
			}
			for _, value := range []protocol.WorkerInput{input, material.Input} {
				body, err := json.Marshal(value)
				if err != nil || !bytes.Equal(body, original) {
					t.Fatalf("render changed original issued input: %s %v", body, err)
				}
			}
		})
	}
}

func TestReadinessUsesOneOrdinaryGateChoiceOrder(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan", Provider: "host", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}}, Gates: []prompts.PromptDefinition{{ID: "quality"}}}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
	var context protocol.WorkerInput
	if err := json.Unmarshal([]byte(raw), &context); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(context.AvailableRouteCandidates, material.Input.AvailableRouteCandidates) || !reflect.DeepEqual(context.AvailableRouteCandidates, []string{"quality"}) {
		t.Fatal("model choice order differs from original decoder input")
	}
}

func TestMaterialKeepsRoleEvidenceWithoutExposingOtherDuties(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.gate", "engine.qa.execution"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Content = "Confirmed semantic content"
			input.RunID, input.Provider = "program-only-run", "program-only-provider"
			input.Pending.ActionID, input.Pending.AttemptID = "program-only-action", "program-only-attempt"
			input.AvailableRouteCandidates = []string{"unrelated-route-choice"}
			v := input.Validation
			v.Workspace = &protocol.NativeResource{Path: "issued-workspace", Owner: "program-only-resource-owner", Ref: "program-only-ref", Status: "program-only-status"}
			v.Development = &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{Deliverable: "unrelated-development-duty"}}
			v.Gate = &protocol.GateInput{Gate: catalog.Gates[0].ID, BaseSnapshot: "issued-base", Candidate: coverage.ValidationCandidate{Identity: "issued-candidate"}}
			v.RequiredCases, v.PendingCases = []string{"new", "prior"}, []string{"prior", "new"}
			v.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "prior"}, {CaseID: "new"}}
			v.Completed = []coverage.ExecutionRecord{{CaseID: "done", Comparison: "accepted comparison", Result: coverage.ExecutedFail}}
			v.Inherited = []coverage.ExecutionRecord{{CaseID: "inherited", Observation: "original inherited observation", Result: coverage.ExecutedPass}}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"program-only-", "unrelated-development-duty", "unrelated-route-choice", "Available route choices", "Design retainCases"} {
				if strings.Contains(material.Prompt, forbidden) {
					t.Fatalf("role received unrelated control facts or duties: %s", forbidden)
				}
			}
			raw := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var context protocol.WorkerInput
			if err := json.Unmarshal([]byte(raw), &context); err != nil {
				t.Fatal(err)
			}
			if context.Validation.Workspace.Path != v.Workspace.Path || context.Validation.Binding.Task != (protocol.WorkBinding{}).Task {
				t.Fatal("role lost its workspace or received dispatch binding")
			}
			if handler == "engine.gate" {
				if !reflect.DeepEqual(context.Validation.Gate, v.Gate) || len(context.Validation.Manifest.Cases) != 0 || len(context.Validation.ExecutionFacts) != 0 {
					t.Fatal("gate lost its native range or received QA content")
				}
			} else if context.Validation.Gate != nil || !reflect.DeepEqual(context.Validation.Manifest, v.Manifest) || !reflect.DeepEqual(context.Validation.ExecutionFacts, v.ExecutionFacts) || !reflect.DeepEqual(context.Validation.RequiredCases, v.RequiredCases) || !reflect.DeepEqual(context.Validation.PendingCases, v.PendingCases) || !reflect.DeepEqual(context.Validation.Completed, v.Completed) || !reflect.DeepEqual(context.Validation.Inherited, v.Inherited) {
				t.Fatal("comparison lost oracle, original order or accepted evidence, or received a gate duty")
			}
			after, _ := json.Marshal(input)
			internal, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) || !bytes.Equal(before, internal) {
				t.Fatal("prompt projection changed program admission/recovery facts")
			}
		})
	}
}
