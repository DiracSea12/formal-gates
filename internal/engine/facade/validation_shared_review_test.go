package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"testing"
)

func TestSharedApprovalRetainsItsOwnContentAndRationale(t *testing.T) {
	for _, variation := range []string{"unchanged", "changed-instructions", "changed-reason"} {
		t.Run(variation, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design")
			m := simpleManifest(in)
			// The shared ID deliberately overlaps a case ID; their approvals differ.
			m.SharedSteps = []coverage.SharedStep{{ID: "normal", Procedure: "Create a clean project and install the documented fixture"}}
			for i := range m.Cases {
				m.Cases[i].SharedSteps = []string{"normal"}
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			d.drive()
			id, in = d.action("engine.qa.review.rationale")
			r := approvedReview(m)
			reason := "共同前置可用。\nThe fixture creates the required clean state.  "
			r.SharedStepDecisions = []coverage.ReviewDecision{{ID: "normal", Status: coverage.StatusPass, Reason: reason}}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify the invalid diagnostic"}}})
			d.drive()
			sg := d.run.Validation.Sets[coverage.ReviewBlackbox].Suggestions[0]
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "diagnostic", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: sg.ID, Decision: "ACCEPT", Reason: "Specify the exact invalid diagnostic", AffectedCases: []string{"invalid"}}}})
			d.drive()
			id, in = d.action("engine.qa.design")
			m = in.Manifest
			m.Cases[1].Oracle = "rejects invalid input with its documented diagnostic"
			if variation == "changed-instructions" {
				m.SharedSteps[0].Procedure = "Create a different clean project with the revised fixture"
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			d.reopen()
			d.drive()
			id, in = d.action("engine.qa.review.rationale")
			r = approvedReview(m)
			r.Scope = coverage.ScopeAffected
			if variation == "changed-instructions" {
				if len(in.InheritableSharedReviews) != 0 {
					t.Fatal("changed shared content retained approval")
				}
			} else if len(in.InheritableSharedReviews) != 1 || in.InheritableSharedReviews[0].Reason != reason {
				t.Fatal("shared approval input lost its original rationale")
			}
			inherited := coverage.ReviewDecision{ID: "normal", Status: coverage.StatusPass, Reason: reason, Inherited: true}
			if variation == "changed-reason" {
				inherited.Reason = "A rewritten claim is not the original approval"
			}
			r.SharedStepDecisions = []coverage.ReviewDecision{inherited}
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "shared-approval", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r}}})
			if variation == "unchanged" {
				if a.Status != "ACCEPTED" {
					t.Fatalf("legal shared inheritance: %s", a.Status)
				}
				d.reopen()
				history := d.run.Validation.Sets[coverage.ReviewBlackbox].ReviewHistory
				if history[len(history)-1].Review.SharedStepDecisions[0].Reason != reason {
					t.Fatal("accepted rationale text changed")
				}
			} else {
				if a.Status != "REJECTED_INVALID_RESULT" {
					t.Fatalf("unsupported inheritance: %s", a.Status)
				}
				d.drive()
				next, nextInput := d.action("engine.qa.review.rationale")
				if next == id || nextInput.Binding.Task != in.Binding.Task {
					t.Fatal("invalid inheritance did not retain the finite logical review duty")
				}
				r.SharedStepDecisions[0].Inherited = false
				r.SharedStepDecisions[0].Reason = "Independently reviewed the actual current instructions"
				d.business(next, protocol.ValidationResult{Binding: nextInput.Binding, Status: "PASS", Review: &r})
				d.drive()
				if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
					t.Fatal("fresh shared review failed to freeze")
				}
			}
		})
	}
}
