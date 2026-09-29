package facade

import (
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"testing"
)

// A custom route executes exactly its selected ordinary gates without QA obligations.
func TestCustomGateRouteCompletesWithoutQA(t *testing.T) {
	d := startValidationDriver(t, nil)
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		var id string
		for key, p := range d.run.PendingActions {
			if p.ReviewSeries == action {
				id = key
			}
		}
		d.receipt(id)
		var plan *protocol.GranularityReview
		if action == review.StartReadiness {
			plan = commandPlan()
		}
		event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, DefaultProvider, string(d.eventID()), protocol.ReviewResultEvidence{Action: action, DispatchID: id, RequirementRevision: d.revision, Status: "PASS", GranularityReview: plan, Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "Checked exact current requirement and all acceptance conditions"}})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(event)
	}
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	d.confirmRoute(protocol.RouteDecision{Mode: "custom", Gates: []string{"blackbox", "whitebox", "quality"}, Selected: []string{"quality"}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: d.revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument}}})
	d.drive()
	id, in := d.action("engine.development.batch")
	t.Logf("accepted route=%s selected=%v development=%s", d.run.Review.RouteMode, d.run.Review.SelectedGates, id)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement command", Build: []string{"go build passed"}, Tests: []string{"input validation tests passed"}}})
	d.materialize()
	d.materialize()
	d.reopen()
	d.drive()
	if len(d.run.Validation.Sets) != 0 {
		t.Fatal("gate-only route created QA sets")
	}
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	if len(wave.Targets) != 1 {
		t.Fatalf("unexpected validation targets: %+v", wave.Targets)
	}
	for _, target := range wave.Targets {
		if target.Kind != "GATE" || target.Gate != "quality" {
			t.Fatal("unconfirmed QA obligation")
		}
	}
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.reopen()
	d.drive()
	if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
		t.Fatal("selected gate did not complete")
	}
	before := d.run.Revision
	d.drive()
	d.reopen()
	if d.run.Revision != before || len(d.run.ValidationInputs) != 0 || len(d.run.AdapterIntents) != 0 {
		t.Fatal("completed route generated more work")
	}
}
