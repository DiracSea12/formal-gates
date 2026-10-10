package worker

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestInterruptionPromptKeepsOnlySemanticFeedback(t *testing.T) {
	input := semanticInput()
	input.Pending.AttemptID = "current-attempt"
	input.RequirementDocument.Content = "Confirmed requirement"
	input.Validation.Interruption = &protocol.RecoveryRecord{ActionID: "private-action", AttemptID: "private-attempt", RequestID: "private-request", Revision: 93, Action: protocol.RecoveryAgent, Detail: "The current point has no boundary case", InterruptionReason: "The source tool exited before returning evidence"}
	before, _ := json.Marshal(input)
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	material, err := Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(material.Prompt, input.Validation.Interruption.Detail) || !strings.Contains(material.Prompt, input.Validation.Interruption.InterruptionReason) {
		t.Fatal("semantic interruption evidence was omitted")
	}
	for _, identity := range []string{"private-action", "private-attempt", "private-request", `"revision": 93`, `"action": "AGENT"`} {
		if strings.Contains(material.Prompt, identity) {
			t.Fatal("prompt exposed recovery mechanics", identity)
		}
	}
	after, _ := json.Marshal(material.Input)
	if !bytes.Equal(before, after) {
		t.Fatal("prompt projection changed original input")
	}
}

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

func TestChildMaterialsCarryConfirmedResponsibilityWithoutNarrowingRequirement(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.development.batch", "engine.gate", "engine.validation.observation", "engine.repair", "engine.repair.verify"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Complete confirmed requirement, including sibling and merge obligations."
			input.Scope = &protocol.WorkerScope{Owner: "slice-2", AcceptanceOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge"}}
			if handler == "engine.gate" {
				input.Validation.Gate = &protocol.GateInput{Gate: "implementation-quality-gate"}
			}
			if handler == "engine.validation.observation" {
				input.Validation.Observation = &protocol.ObservationInput{}
			}
			if handler == "engine.repair" || handler == "engine.repair.verify" {
				input.Validation.Repair = &protocol.RepairInput{}
			}
			if handler == "engine.repair" {
				input.AvailableRouteCandidates = catalog.RouteCandidates()
				input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
			}
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(material.Prompt, input.RequirementDocument.Content) != 1 || !strings.Contains(material.Prompt, "another owner's unfinished work is not a defect of this child") {
				t.Fatal("child lost the whole requirement or its review boundary")
			}
			actionInput := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var projection protocol.WorkerInput
			if err := json.Unmarshal([]byte(actionInput), &projection); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(projection.Scope, input.Scope) || !reflect.DeepEqual(material.Input, input) {
				t.Fatal("material changed responsibility facts or issued input")
			}
			stored, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(stored, &recovered); err != nil {
				t.Fatal(err)
			}
			replay, err := Render(recovered, catalog)
			if err != nil || !reflect.DeepEqual(material, replay) {
				t.Fatalf("recovery changed role materials: %v", err)
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
			v.Inherited = []coverage.ExecutionRecord{{CaseID: "inherited", Comparison: "accepted inherited comparison", Result: coverage.ExecutedPass}}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"program-only-", "issued-workspace", "unrelated-development-duty", "unrelated-route-choice", "Available route choices", "Design retainCases"} {
				if strings.Contains(material.Prompt, forbidden) {
					t.Fatalf("role received unrelated control facts or duties: %s", forbidden)
				}
			}
			raw := strings.Split(strings.Split(material.Prompt, "[Action input]\n")[1], "\n\n[Dispatch]")[0]
			var context protocol.WorkerInput
			if err := json.Unmarshal([]byte(raw), &context); err != nil {
				t.Fatal(err)
			}
			if context.Validation.Workspace != nil || context.Validation.Binding.Task != (protocol.WorkBinding{}).Task {
				t.Fatal("role received a physical workspace or dispatch binding")
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

func TestMaterialKeepsDevelopmentFeedbackWithoutTransportBindings(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	result := protocol.WorkerResult{SourceResult: "opaque-source", ActionID: "opaque-action", Provider: "opaque-provider", PayloadDigest: "opaque-payload", Digest: "opaque-digest", Outcome: "FAILED", InterruptionReason: "original execution interruption", InvalidReason: "original result rejection", Validation: &protocol.ValidationResult{Binding: protocol.WorkBinding{WaveID: "opaque-wave"}, Status: "CONTINUE", Development: &protocol.DevelopmentEvidence{Summary: "author text contains actionId and opaque-user-body", RemainingWork: "finish the original behavior", Paths: []string{"app.go"}, Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"criterion": "original evidence"}, Handoff: "original handoff"}}}}
	for _, handler := range []string{"engine.development.batch", "engine.repair"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Validation.Development = &protocol.DevelopmentInput{Result: &result, Dependencies: map[string]protocol.WorkerResult{"batch": result}}
			if handler == "engine.repair" {
				input.Validation.Repair = &protocol.RepairInput{Result: &result, MemberResults: []protocol.WorkerResult{result}}
				input.AvailableRouteCandidates = catalog.RouteCandidates()
				input.AvailableRouteCandidates = slices.DeleteFunc(input.AvailableRouteCandidates, func(id string) bool { return id == "blackbox" || id == "whitebox" })
			}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, transport := range []string{"opaque-source", "opaque-action", "opaque-provider", "opaque-payload", "opaque-digest", "opaque-wave"} {
				if strings.Contains(material.Prompt, transport) {
					t.Fatalf("role prompt leaked result transport %s", transport)
				}
			}
			for _, feedback := range []string{"original execution interruption", "original result rejection", "FAILED", "CONTINUE", "author text contains actionId and opaque-user-body", "finish the original behavior", "app.go", "original evidence", "original handoff"} {
				if !strings.Contains(material.Prompt, feedback) {
					t.Fatalf("role prompt lost business feedback %s", feedback)
				}
			}
			after, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) {
				t.Fatal("prompt projection changed original issued API input")
			}
			var recovered protocol.WorkerInput
			if err := json.Unmarshal(before, &recovered); err != nil {
				t.Fatal(err)
			}
			replayed, err := Render(recovered, catalog)
			if err != nil || !reflect.DeepEqual(material, replayed) {
				t.Fatalf("recovery changed role prompt or original input: %v", err)
			}
		})
	}
}

func TestMaterialRestrictsComparisonAndConflictResponsibilities(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, handler := range []string{"engine.repair.verify", "engine.merge.resolve"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "attempt"
			input.RequirementDocument.Content = "Confirmed requirement"
			input.Validation.Development = nil
			if handler == "engine.repair.verify" {
				input.Validation.Repair = &protocol.RepairInput{FinalCandidate: &coverage.ValidationCandidate{Identity: "candidate"}, VerificationCases: []string{"case"}}
			} else {
				input.Validation.Conflict = &protocol.ConflictInput{}
			}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, instruction := range []string{"# 开发工作者", "需分块时提出完整", "逐项 DoD 证据与 handoff"} {
				if strings.Contains(material.Prompt, instruction) {
					t.Fatalf("role %s received development duties", handler)
				}
			}
			if handler == "engine.repair.verify" && (!material.ReadOnly || !strings.Contains(material.Prompt, "# QA 判据比较")) {
				t.Fatal("repair comparison lost its existing comparison contract")
			}
			if handler == "engine.merge.resolve" && (material.ReadOnly || !strings.Contains(material.Prompt, "Report only the semantic resolution summary")) {
				t.Fatal("conflict resolver lost its exact current duty")
			}
			after, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) {
				t.Fatal("role instructions changed original issued input")
			}
		})
	}
}
