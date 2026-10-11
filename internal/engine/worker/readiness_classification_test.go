package worker

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestReadinessMaterialAndSemanticBodyCarryTechnicalClassification(t *testing.T) {
	input := protocol.WorkerInput{RunID: "run", Handler: "engine.readiness.plan.v4", Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "requirement"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "revision"}}
	material, err := Render(input, prompts.PromptCatalog{Actions: []prompts.PromptDefinition{{ID: "start-readiness"}}})
	if err != nil {
		t.Fatal(err)
	}
	item := material.ResultSchema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["findings"].(map[string]any)["items"].(map[string]any)
	if !slices.Contains(item["required"].([]any), any("category")) || !strings.Contains(material.Prompt, "implementation-detail has severity P2 or P3") {
		t.Fatal("material lost required classification")
	}
	choices := item["anyOf"].([]any)
	if len(choices) != 4 {
		t.Fatal("technical categories not published")
	}
	published := map[string][]string{}
	for _, choice := range choices {
		fields := choice.(map[string]any)["properties"].(map[string]any)
		category := fields["category"].(map[string]any)["const"].(string)
		published[category] = fields["severity"].(map[string]any)["enum"].([]string)
	}
	if !reflect.DeepEqual(published["implementation-detail"], []string{"P2", "P3"}) {
		t.Fatal("schema allows implementation blockers", published)
	}
	for _, category := range []string{"", "unknown", "requirement-conflict", "responsibility-dependency", "acceptance-entry", "implementation-detail"} {
		for _, severity := range []string{"P0", "P1", "P2", "P3"} {
			t.Run(category+"/"+severity, func(t *testing.T) {
				body := Review{Findings: []Finding{{Category: category, Severity: severity, Message: "Actual documented path concern"}}, Verification: []string{}}
				for range review.RequiredVerificationChecks() {
					body.Verification = append(body.Verification, "Inspected original requirement and observed evidence")
				}
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				valid := category != "" && category != "unknown" && !(category == "implementation-detail" && (severity == "P0" || severity == "P1"))
				err = ValidateSemanticBody(input, raw)
				if (err == nil) != valid {
					t.Fatalf("tool body accepted=%v want=%v: %v", err == nil, valid, err)
				}
				event, err := DecodeResultWithPaths(input, "event", raw, "original", []string{})
				if (err == nil) != valid {
					t.Fatalf("terminal body accepted=%v want=%v: %v", err == nil, valid, err)
				}
				if valid {
					got := event.Result.Review.Findings[0]
					if got.Category != category || got.Severity != severity || event.Result.Review.Action != review.StartReadiness {
						t.Fatal("program lost original classification or issued action")
					}
					if !slices.Contains(published[category], severity) {
						t.Fatal("valid semantic body missing from schema")
					}
				}
			})
		}
	}
}
