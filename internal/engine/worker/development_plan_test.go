package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestDevelopmentPlanPreparationPreservesOriginalChecksWithoutReviewEvidence(t *testing.T) {
	command := "./project-check --label '原命令 two words'\n"
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.development.plan.v1", RunID: "project", Provider: "codex", Pending: protocol.PendingAction{ActionID: "prepare", AttemptID: "issued"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed behavior"}, Validation: &protocol.ValidationInput{}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "project-edit", Purpose: protocol.CheckEdit, Command: command}}}
	proposal := &Granularity{Reason: "One public behavior", VerificationTier: "FINAL", Batches: []Batch{{NewArtifacts: []string{"app.go"}, Subtasks: []Subtask{{Description: "Implement the confirmed behavior"}}, DoD: []Criterion{{Description: "The public behavior holds"}}, Deliverable: "Confirmed behavior", PublicEntry: "app run", Interfaces: "Public command", StateBoundary: "Project files", Risk: "Local change", Rollback: "Revert the change", Recovery: "Resume the original task", Handoff: "Ready for validation", ContextCost: "One duty", Checks: []Check{{Reuse: 1}, {Goal: "Complete project build", Purpose: protocol.CheckFullBuild}, {Goal: "Complete project tests", Purpose: protocol.CheckFullTests}}}}}
	raw, err := json.Marshal(DevelopmentPlan{GranularityReview: proposal})
	if err != nil {
		t.Fatal(err)
	}
	original, err := DecodeResultWithFacts(input, "result", raw, "verified-native-source", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if original.Result.Review != nil || original.Result.Validation.Development != nil || len(original.Result.Validation.Findings) != 0 {
		t.Fatal("preparation claimed review or implementation evidence")
	}
	plan := original.Result.Validation.GranularityReview
	if err := protocol.ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Batches[0].Checks[0], input.ApprovedChecks[0]) || plan.Batches[0].Checks[1].Command != "" || plan.Batches[0].Checks[1].Goal != "Complete project build" {
		t.Fatal("preparation changed the original invocation or filled a missing call")
	}
	stored, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var recovered protocol.WorkerInput
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithFacts(recovered, "result", raw, "verified-native-source", nil, nil)
	if err != nil || !reflect.DeepEqual(replay, original) {
		t.Fatal("recovery changed original proposal or invocation bytes", err)
	}
	material, err := Render(input, prompts.PromptCatalog{Base: "independent-review-only", Actions: []prompts.PromptDefinition{{ID: "development-plan", Content: "Prepare the confirmed development contract"}}})
	if err != nil || !material.ReadOnly || strings.Contains(material.Prompt, "independent-review-only") {
		t.Fatal("preparation inherited an independent review duty or writable workspace", err)
	}
	for _, generate := range []func(protocol.WorkerInput) (map[string]any, error){ResultSchema, StableResultSchema} {
		schema, err := generate(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if len(fields) != 1 || fields["granularityReview"] == nil {
			t.Fatal("preparation exposes unrelated semantic evidence", fields)
		}
		plan := fields["granularityReview"].(map[string]any)["properties"].(map[string]any)
		batch := plan["batches"].(map[string]any)["items"].(map[string]any)
		if batch["properties"] == nil {
			batch = batch["anyOf"].([]any)[0].(map[string]any)
		}
		checks := batch["properties"].(map[string]any)["checks"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
		for _, choice := range checks {
			if choice.(map[string]any)["properties"].(map[string]any)["command"] != nil {
				t.Fatal("preparation schema allows copying an invocation")
			}
		}
	}
	proposal.Batches[0].Checks = []Check{{Command: command, Purpose: protocol.CheckEdit}}
	raw, err = json.Marshal(DevelopmentPlan{GranularityReview: proposal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeResult(input, "copied-command", raw, "verified-native-source"); err == nil {
		t.Fatal("preparation copied an invocation instead of selecting its original check")
	}
	if _, err := DecodeResult(input, "review-claim", []byte(`{"granularityReview":{},"findings":[]}`), "verified-native-source"); err == nil {
		t.Fatal("preparation returned review findings")
	}
}

func TestDisabledDevelopmentKeepsTechnicalReviewWithoutPlan(t *testing.T) {
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.readiness.plan.v6", DevelopmentDisabled: true, RunID: "project", Provider: "codex", Pending: protocol.PendingAction{ActionID: "technical", AttemptID: "issued"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed behavior"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	for _, generate := range []func(protocol.WorkerInput) (map[string]any, error){ResultSchema, StableResultSchema} {
		schema, err := generate(input)
		if err != nil {
			t.Fatal(err)
		}
		fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
		if fields["granularityReview"] != nil || fields["findings"] == nil {
			t.Fatal("technical review did not respect the selected duty", fields)
		}
	}
	event, err := DecodeResult(input, "technical-result", []byte(`{"findings":[]}`), "verified-native-source")
	if err != nil || event.Result.Review == nil || event.Result.Review.GranularityReview != nil {
		t.Fatal("technical review without development required a plan", err)
	}
	if _, err := DecodeResult(input, "unselected-plan", []byte(`{"findings":[],"granularityReview":{}}`), "verified-native-source"); err == nil {
		t.Fatal("technical review prepared unselected development")
	}
	material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}}})
	if err != nil || strings.Contains(material.Prompt, "approvedPlan") || strings.Contains(material.Prompt, "Each proposed batch") {
		t.Fatal("technical material retained a development preparation duty", err)
	}
	catalog := prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness", Content: "technical-review-duty"}, {ID: "development-plan", Content: "shared-execution-plan-contract"}}}
	material, err = Render(input, catalog)
	if err != nil || !strings.Contains(material.Prompt, "technical-review-duty") || strings.Contains(material.Prompt, "shared-execution-plan-contract") {
		t.Fatal("disabled development changed technical review or retained its plan contract", err)
	}
	input.DevelopmentDisabled = false
	material, err = Render(input, catalog)
	if err != nil || strings.Count(material.Prompt, "shared-execution-plan-contract") != 1 {
		t.Fatal("technical review did not receive the shared development contract", err)
	}
	input.Handler, input.Review, input.Validation = "engine.development.plan.v1", nil, &protocol.ValidationInput{}
	material, err = Render(input, catalog)
	if err != nil || strings.Count(material.Prompt, "shared-execution-plan-contract") != 1 || strings.Contains(material.Prompt, "technical-review-duty") {
		t.Fatal("selected development preparation did not receive its exact shared contract", err)
	}
}
