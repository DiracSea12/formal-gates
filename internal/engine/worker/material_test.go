package worker

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

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
			if !reflect.DeepEqual(projection.RequirementDocument, wantProjection) || projection.Validation != nil && !reflect.DeepEqual(projection.Validation.RequirementDocument, wantProjection) {
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
