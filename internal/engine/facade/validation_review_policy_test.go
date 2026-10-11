package facade

import (
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestCaseReviewPassKeepsFailureAllowanceAcrossReopening(t *testing.T) {
	for _, reopen := range []string{"THAW", "SUGGESTION"} {
		t.Run(reopen, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			for index := 0; index < 4; index++ {
				id, in := d.action("engine.qa.design.v2")
				m := in.Manifest
				if len(m.Cases) == 0 {
					m = simpleManifest(in)
				}
				m.Cases[0].Preconditions += "; initialize current input fixture"
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				d.drive()
				for _, ask := range d.run.PendingAsks {
					if ask.Validation != nil && ask.Validation.Kind == "QA_REVIEW" {
						t.Fatal("successful reviews exhausted failure allowance")
					}
				}
				id, in = d.action("engine.qa.review.rationale.v2")
				approved := approvedReview(m)
				result := protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved}
				if reopen == "SUGGESTION" && index < 3 {
					result.Suggestions = []protocol.QASuggestion{{Severity: "P3", Message: "Clarify the fixture initialization"}}
				}
				d.business(id, result)
				d.reopen()
				set := d.run.Validation.Sets[coverage.ReviewBlackbox]
				if len(set.ReviewHistory) != index+1 || len(d.run.RejectedResults) != 0 {
					t.Fatal("current review was not accepted exactly once")
				}
				if reopen == "SUGGESTION" && index < 3 {
					suggestion := set.Suggestions[len(set.Suggestions)-1]
					event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "QA suggestions", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: suggestion.ID, Decision: "ACCEPT", Reason: "clarify executable setup", AffectedCases: []string{"normal"}}}}
					d.submit(event)
					d.submit(event)
				} else {
					if set.Status != "FROZEN" {
						t.Fatalf("accepted review did not freeze: %s", set.Status)
					}
					if index < 3 {
						d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
					}
				}
				d.drive()
			}
		})
	}
}

func TestCaseReviewFailureStreakUsesOneBoundGrant(t *testing.T) {
	for _, diagnostic := range []bool{true, false} {
		t.Run(fmt.Sprintf("diagnostic=%t", diagnostic), func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			preparation := [][]string{
				{"", "initialize the valid input fixture before invocation", "clear captured output before invocation", "invoke from the clean project root"},
				{"restore the valid input fixture after prior execution", "resolve the app executable from the project root", "provide empty standard input", "use the isolated project environment"},
			}
			failureReasons := [][]string{
				{"Valid input fixture initialization is unspecified", "Captured output from earlier execution can contaminate the oracle", "The invocation working directory is unspecified"},
				{"Executable resolution is unspecified", "Standard input is unspecified", "Execution environment is unspecified"},
			}
			for sequence := 0; sequence < 2; sequence++ {
				for index := 0; index < 4; index++ {
					if index == 3 {
						ask := d.validationAsk("QA_REVIEW")
						d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "redesign", Validation: &protocol.ValidationDecision{Reason: "review the corrected cases once", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}}})
						d.reopen()
						d.drive()
					}
					id, in := d.action("engine.qa.design.v2")
					m := in.Manifest
					if len(m.Cases) == 0 {
						m = simpleManifest(in)
					}
					previousPreconditions := m.Cases[0].Preconditions
					unaffected := m.Cases[1]
					if preparation[sequence][index] != "" {
						m.Cases[0].Preconditions += "; " + preparation[sequence][index]
					}
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
					set := d.run.Validation.Sets[coverage.ReviewBlackbox]
					if len(d.run.RejectedResults) != 0 || len(set.Manifest.Cases) != 2 || set.Manifest.Cases[0].CaseID != "normal" || set.Manifest.Cases[0].Preconditions != m.Cases[0].Preconditions || !reflect.DeepEqual(set.Manifest.Cases[1], unaffected) {
						t.Fatal("accepted revision changed case identity or untouched rejection case", d.run.RejectedResults, set.Manifest)
					}
					if preparation[sequence][index] != "" && set.Manifest.Cases[0].Preconditions == previousPreconditions {
						t.Fatal("reopened design did not correct its executable preparation")
					}
					d.drive()

					for _, ask := range d.run.PendingAsks {
						if ask.Validation != nil && ask.Validation.Kind == "QA_REVIEW" {
							t.Fatalf("only %d consecutive failures", index)
						}
					}
					id, in = d.action("engine.qa.review.rationale.v2")
					r := approvedReview(in.Manifest)
					if sequence == 0 && index == 3 {
						d.carryFormattingChange()
					}
					if index < 3 {
						r.SetStatus = coverage.StatusFail
						r.CaseDecisions[0].Status = coverage.StatusFail
						r.CaseDecisions[0].Reason = failureReasons[sequence][index]
						r.PointDecisions[0].Status = coverage.StatusFail
						r.SourceDecisions[0].Status = coverage.StatusFail
					}
					status := r.SetStatus
					if !diagnostic {
						r.SetStatus = ""
					}
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: string(status), Review: &r})
					d.reopen()
					d.drive()
					set = d.run.Validation.Sets[coverage.ReviewBlackbox]
					if len(set.ReviewHistory) != sequence*4+index+1 || len(d.run.RejectedResults) != 0 {
						t.Fatal("review history differs from accepted results")
					}
					if !diagnostic && set.ReviewHistory[len(set.ReviewHistory)-1].Review.SetStatus != "" {
						t.Fatal("optional diagnostic was synthesized into raw evidence")
					}
				}
				set := d.run.Validation.Sets[coverage.ReviewBlackbox]
				if set.Status != "FROZEN" || set.Review.Binding.RequirementRevision != d.revision {
					t.Fatal("successful authorized review did not freeze the current requirement")
				}
				if sequence == 0 {
					d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
					d.drive()
				}
			}

		})
	}
}
