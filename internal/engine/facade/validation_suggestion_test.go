package facade

import (
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestSuggestionDispositionPreservesCurrentApprovalAndOpenImpacts(t *testing.T) {
	for _, test := range []struct {
		name    string
		choices [2]string
		impacts [2][]string
	}{
		{name: "defer-after-reopen", choices: [2]string{"ACCEPT", "DEFER"}, impacts: [2][]string{{"invalid"}, nil}},
		{name: "combine-accepted-impacts", choices: [2]string{"ACCEPT", "ACCEPT"}, impacts: [2][]string{{"invalid"}, {"normal"}}},
		{name: "no-case-changes", choices: [2]string{"DEFER", "DECLINE"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, input := d.action("engine.qa.design")
			manifest := simpleManifest(input)
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
			d.drive()
			id, input = d.action("engine.qa.review.rationale")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify invalid input preparation"}, {Severity: "P3", Message: "Clarify successful input preparation"}}})
			suggestions := d.run.Validation.Sets[coverage.ReviewBlackbox].Suggestions
			expected := []string{}
			var openTask string
			for i, choice := range test.choices {
				event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "QA suggestions", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: suggestions[i].ID, Decision: choice, Reason: "Reviewed the specific optional case preparation change", AffectedCases: test.impacts[i]}}}
				d.submit(event)
				d.submit(event)
				d.drive()
				expected = append(expected, test.impacts[i]...)
				if len(expected) > 0 {
					_, design := d.action("engine.qa.design")
					if openTask != "" && design.Binding.Task.String() != openTask {
						t.Fatal("suggestion disposition replaced an open design obligation")
					}
					openTask = design.Binding.Task.String()
				}
			}
			d.reopen()
			d.drive()
			set := d.run.Validation.Sets[coverage.ReviewBlackbox]
			if len(set.ReviewHistory) != 1 {
				t.Fatal("suggestion handling consumed a semantic review")
			}
			if len(expected) == 0 {
				if set.Status != "FROZEN" || set.Review == nil {
					t.Fatal("optional suggestions without case changes did not freeze")
				}
				return
			}
			if set.Status != "DRAFT" || set.Review != nil {
				t.Fatal("historical approval became effective while redesign remains open")
			}
			if len(set.AffectedCases) != len(expected) {
				t.Fatalf("affected cases=%v want %v", set.AffectedCases, expected)
			}
			for _, id := range expected {
				if !slices.Contains(set.AffectedCases, id) {
					t.Fatalf("lost accepted impact %s", id)
				}
			}
			id, input = d.action("engine.qa.design")
			for _, caseID := range expected {
				if slices.ContainsFunc(input.InheritableReviews, func(d coverage.ReviewDecision) bool { return d.ID == caseID }) {
					t.Fatalf("affected case %s inherited old approval", caseID)
				}
			}
			manifest = input.Manifest
			for i := range manifest.Cases {
				if slices.Contains(expected, manifest.Cases[i].CaseID) {
					manifest.Cases[i].Preconditions += "; initialize an empty input history"
				}
			}
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
			d.drive()
			if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "REVIEW" {
				t.Fatal("redesign froze without fresh review")
			}
			id, input = d.action("engine.qa.review.rationale")
			approved = approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved})
			d.drive()
			set = d.run.Validation.Sets[coverage.ReviewBlackbox]
			if set.Status != "FROZEN" || len(set.ReviewHistory) != 2 || len(set.AffectedCases) != 0 {
				t.Fatal("fresh review failed to consume the open impacts and freeze")
			}
			if set.ReviewHistory[0].Manifest.Cases[0].Preconditions != "clean project" {
				t.Fatal("original review evidence was mutated")
			}
		})
	}
}
