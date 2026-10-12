package worker

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestRequirementReadFindingLocationsUseFrozenDocumentForEveryRole(t *testing.T) {
	for _, handler := range []string{"engine.review.worker.v2", "engine.readiness.plan.v4", "engine.gate.v2", "engine.qa.execution.v3"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Path = "requirements.md"
			input.RequirementDocument.Content = "# Frozen requirement\r\nRequired behavior.  \r\n"
			finding := Finding{Severity: "P3", Message: "The frozen requirement needs clarification", Locations: []Location{{Read: 1, Line: 2}, {Read: 1}, {RequirementLine: 2}}}
			var body any = Gate{Findings: []Finding{finding}}
			if handler == "engine.review.worker.v2" || handler == "engine.readiness.plan.v4" {
				input.Validation = nil
				input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
				if handler == "engine.readiness.plan.v4" {
					input.Review.Action = review.StartReadiness
					for _, category := range review.TechnicalCategories() {
						if review.ValidateFindingClassification(review.StartReadiness, category, "P3") == nil {
							finding.Category = category
							break
						}
					}
				}
				result := Review{Findings: []Finding{finding}, Verification: []string{}}
				for range review.RequiredVerificationChecks() {
					result.Verification = append(result.Verification, "The original frozen text was checked")
				}
				body = result
			} else if handler == "engine.qa.execution.v3" {
				input.Validation.Execution = &coverage.ExecutionBinding{Candidate: coverage.ValidationCandidate{Identity: "original-candidate"}, Scope: coverage.ScopeFull}
				input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
				body = ExecutionJudgments{Execution: Execution{Records: []*Observation{}}, Findings: []Finding{finding}}
			}
			facts := []LocationFact{{Kind: "requirement", Content: []byte(input.RequirementDocument.Content)}}
			before, _ := json.Marshal(facts)
			raw, _ := json.Marshal(body)
			for range 2 {
				if err := ValidateSemanticBody(input, raw, facts); err != nil {
					t.Fatal("tool validation rejected original frozen read", err)
				}
				event, err := DecodeResultWithFacts(input, "result", raw, "original-output", nil, facts)
				if err != nil {
					t.Fatal("assembler disagreed with tool validation", err)
				}
				var actual []string
				if event.Result.Review != nil {
					actual = event.Result.Review.Findings[0].Locations
				} else {
					actual = event.Result.Validation.Findings[0].Locations
				}
				if strings.Join(actual, "|") != "requirements.md:2|requirements.md|requirements.md:2" {
					t.Fatal("program did not bind original document positions", actual)
				}
				encoded, _ := json.Marshal(input)
				if err := json.Unmarshal(encoded, &input); err != nil {
					t.Fatal(err)
				}
			}
			after, _ := json.Marshal(facts)
			if !bytes.Equal(before, after) || facts[0].Kind != "requirement" || len(facts[0].Paths) != 0 {
				t.Fatal("finding projection granted a physical path to original facts")
			}
			mismatched := []LocationFact{{Kind: "requirement", Content: []byte("other frozen revision\n")}}
			if err := ValidateSemanticBody(input, raw, mismatched); err == nil {
				t.Fatal("different original requirement content bound current document path")
			}
			if _, err := DecodeResultWithFacts(input, "mismatched", raw, "original-output", nil, mismatched); err == nil {
				t.Fatal("assembler accepted a different frozen revision")
			}
			if handler == "engine.qa.execution.v3" {
				partial, _ := json.Marshal(ExecutionInterruption{InterruptionReason: "The remaining approved fixture is unavailable", Records: []*Observation{}, Findings: []Finding{finding}})
				if err := ValidateSemanticBody(input, partial, facts); err != nil {
					t.Fatal("partial execution lost original finding location", err)
				}
				if _, err := DecodeResultWithFacts(input, "partial", partial, "original-output", nil, facts); err != nil {
					t.Fatal("partial execution assembly disagreed with tool publication", err)
				}
			}
		})
	}
}

func TestRequirementReadNeverSelectsPhysicalDeliveryIdentity(t *testing.T) {
	input := semanticInput()
	input.RequirementDocument.Path = "requirements.md"
	input.RequirementDocument.Content = "Original frozen text\n"
	facts := []LocationFact{{Kind: "requirement", Content: []byte(input.RequirementDocument.Content)}}
	selection := []Location{{Read: 1}}
	if _, err := LocationPaths(selection, facts, input.RequirementDocument); err == nil {
		t.Fatal("requirement read became a physical file selection")
	}
	if _, err := selectedTestFiles(selection, facts); err == nil {
		t.Fatal("requirement read became a structural test file")
	}
	plan := &Granularity{Batches: []Batch{{Artifacts: selection}}}
	if _, err := DecodeGranularity(input, plan, nil, facts, false); err == nil || !strings.Contains(err.Error(), "original published read") {
		t.Fatal("requirement read became a development artifact", err)
	}
	if facts[0].Kind != "requirement" || len(facts[0].Paths) != 0 || string(facts[0].Content) != input.RequirementDocument.Content {
		t.Fatal("physical negative controls mutated original source facts")
	}
}

func TestConfirmedMaterialReadsKeepSemanticAndPhysicalAuthoritySeparate(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.review.worker.v2"
	input.Validation = nil
	input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
	input.Materials = []protocol.IntakeMaterial{{Path: "design.md", Content: "# Plan\nUse original command.\n", Revision: "original-plan"}}
	facts := []LocationFact{{Kind: "material", Paths: []string{"design.md"}, Content: []byte(input.Materials[0].Content)}}
	body := Review{Findings: []Finding{{Severity: "P3", Message: "The confirmed plan needs clarification", Locations: []Location{{Read: 1, Line: 2}}}}}
	for range review.RequiredVerificationChecks() {
		body.Verification = append(body.Verification, "Inspected the original confirmed plan")
	}
	raw, _ := json.Marshal(body)
	before, _ := json.Marshal(facts)
	if err := ValidateSemanticBody(input, raw, facts); err != nil {
		t.Fatal("published original material finding rejected", err)
	}
	event, err := DecodeResultWithFacts(input, "material-result", raw, "original-source", nil, facts)
	if err != nil || event.Result.Review.Findings[0].Locations[0] != "design.md:2" {
		t.Fatal("finding did not bind the confirmed material source", err)
	}
	command, err := sourceReadText(facts, 1, 2, 2, nil, nil)
	if err != nil || command != "Use original command.\n" {
		t.Fatal("semantic value lost original material bytes", err)
	}
	if _, err = LocationPaths([]Location{{Read: 1}}, facts, input.RequirementDocument); err == nil {
		t.Fatal("virtual confirmed material granted physical delivery selection")
	}
	changed := append([]LocationFact(nil), facts...)
	changed[0].Content = []byte("different unconfirmed plan\n")
	if err = ValidateSemanticBody(input, raw, changed); err == nil {
		t.Fatal("unconfirmed material bytes obtained the source path")
	}
	after, _ := json.Marshal(facts)
	if !bytes.Equal(before, after) || facts[0].Kind != "material" {
		t.Fatal("semantic projection mutated original fact authority")
	}
}
