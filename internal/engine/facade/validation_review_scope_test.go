package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"testing"
)

// A baseline is established by a valid FULL review, including after invalid incremental evidence.
func TestFirstReviewRequiresFullBaseline(t *testing.T) {
	for _, order := range []string{"normal", "result-first", "held"} {
		t.Run(order, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			id, in := d.action("engine.qa.design.v2")
			m := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			id, in = d.action("engine.qa.review.rationale.v2")
			r := approvedReview(m)
			r.Scope = coverage.ScopeAffected
			var request protocol.Event
			if order == "held" {
				request = holdRequirement(t, d)
			}
			if order != "result-first" {
				d.receipt(id)
			}
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "original-incremental-without-baseline", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r}}})
			if order == "result-first" {
				d.reopen()
				d.receipt(id)
			}
			if order == "held" {
				d.reopen()
				releaseRequirement(t, d, request)
			}
			d.reopen()
			d.drive()
			s, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			set := d.run.Validation.Sets[coverage.ReviewBlackbox]
			t.Logf("first review acceptance=%s status=%s count=%d", a.Status, set.Status, len(set.ReviewHistory))
			if order == "normal" && a.Status != "REJECTED_INVALID_RESULT" || set.Status == "FROZEN" || len(set.ReviewHistory) != 0 {
				t.Fatal("initial AFFECTED review established a baseline")
			}
			rejected := s.State.RejectedResults[id]
			if rejected.Payload.Validation == nil || rejected.Payload.Validation.Review.Scope != coverage.ScopeAffected || rejected.Payload.PayloadDigest != "original-incremental-without-baseline" {
				t.Fatal("invalid original review facts were lost")
			}
			next, input := d.action("engine.qa.review.rationale.v2")
			if next == id || input.Binding.Task != in.Binding.Task {
				t.Fatal("invalid scope did not retain finite original task")
			}
			full := approvedReview(input.Manifest)
			d.business(next, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &full})
			d.reopen()
			d.drive()
			set = d.run.Validation.Sets[coverage.ReviewBlackbox]
			if set.Status != "FROZEN" || len(set.ReviewHistory) != 1 || set.ReviewHistory[0].Review.Scope != coverage.ScopeFull {
				t.Fatal("valid full retry did not establish the original baseline")
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if snap.State.Results[next].Validation.Binding != input.Binding || snap.State.RejectedResults[id].Payload.Validation.Review.Scope != coverage.ScopeAffected {
				t.Fatal("retry rewrote original input or rejection")
			}
			revision := d.run.Revision
			d.reopen()
			d.drive()
			if d.run.Revision != revision {
				t.Fatal("reopen repeated review work")
			}
		})
	}
}
