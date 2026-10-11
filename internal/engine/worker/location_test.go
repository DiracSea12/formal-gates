package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestFindingLocationsUseOriginalSourceFacts(t *testing.T) {
	for _, handler := range []string{"engine.review.worker.v2", "engine.gate.v2", "engine.qa.execution.v2"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.RequirementDocument.Path = "requirements.md"
			input.RequirementDocument.Content = "# Original requirement\nRequired behavior.\n"
			finding := Finding{Severity: "P3", Message: "The original source needs clarification", Locations: []Location{{Read: 2, Line: 2}, {Listing: 1, Entry: 1}, {RequirementLine: 2}}}
			var value any = Gate{Findings: []Finding{finding}}
			if handler == "engine.review.worker.v2" {
				input.Validation = nil
				input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
				body := Review{Findings: []Finding{finding}, Verification: []string{}}
				for range review.RequiredVerificationChecks() {
					body.Verification = append(body.Verification, "Original requirement evidence inspected")
				}
				value = body
			} else if handler == "engine.qa.execution.v2" {
				input.Validation.Execution = &coverage.ExecutionBinding{RequiredSourcesDigest: "sources", ManifestDigest: "manifest", MapDigest: "map", WhitelistDigest: "whitelist", Candidate: coverage.ValidationCandidate{Identity: "candidate"}, Scope: coverage.ScopeFull}
				input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
				value = ExecutionJudgments{Execution: Execution{Records: []*Observation{}}, Findings: []Finding{finding}}
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			facts := []LocationFact{{Kind: "listing", Paths: []string{"source.go", "other.go"}}, {Kind: "read", Paths: []string{"source.go"}, Content: []byte("package app\nfunc Main() {}\n")}}
			event, err := DecodeResultWithFacts(input, "result", raw, "original-output", nil, facts)
			if err != nil {
				t.Fatal(err)
			}
			locations := event.Result.Validation
			actual := []string{}
			if event.Result.Review != nil {
				actual = event.Result.Review.Findings[0].Locations
			} else {
				actual = locations.Findings[0].Locations
			}
			if !reflect.DeepEqual(actual, []string{"source.go:2", "source.go", "requirements.md:2"}) {
				t.Fatalf("original paths/lines were not assembled: %v", actual)
			}
			copied := strings.Replace(string(raw), `[{"read":2,"line":2},{"listing":1,"entry":1},{"requirementLine":2}]`, `["source.go:2"]`, 1)
			if copied == string(raw) {
				t.Fatal("raw-location negative control did not replace the field")
			}
			if _, err := DecodeResultWithFacts(input, "copied", []byte(copied), "original-output", nil, facts); err == nil {
				t.Fatal("raw string location wire was accepted")
			}
		})
	}
}
