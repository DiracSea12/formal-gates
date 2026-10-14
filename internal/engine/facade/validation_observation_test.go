package facade

import (
	"crypto/sha256"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestSuggestionFactsWaitForRequirementResolution(t *testing.T) {
	for _, decision := range []string{"ACCEPT", "DEFER"} {
		t.Run(decision, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design.v6")
			m := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
			d.drive()
			id, in = d.action("engine.qa.review.rationale.v2")
			r := approvedReview(m)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "clarify setup"}}})
			set := d.run.Validation.Sets[coverage.ReviewBlackbox]
			if set.Status != "SUGGESTIONS" {
				t.Fatalf("bad precondition %s", set.Status)
			}
			suggestion := set.Suggestions[0].ID
			version := set.Version
			req, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise requirement"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(req)
			obs := protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: suggestion, Decision: decision, Reason: "explicit disposition"}
			if decision == "ACCEPT" {
				obs.AffectedCases = []string{"normal"}
			}
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "QA suggestions", Validation: &obs}}
			d.submit(event)
			d.submit(event)
			d.reopen()
			d.drive()
			held := d.run.Validation.Sets[coverage.ReviewBlackbox]
			if held.Status != "SUGGESTIONS" || held.Review == nil || held.Version != version {
				t.Fatalf("observation advanced case state under barrier: %+v", held)
			}
			ask := d.run.PendingRequirementAsk
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "abort"}})
			d.reopen()
			d.drive()
			next := d.run.Validation.Sets[coverage.ReviewBlackbox]
			if decision == "DEFER" {
				if next.Status != "FROZEN" {
					t.Fatalf("deferred suggestion did not freeze after barrier %s", next.Status)
				}
			} else {
				if next.Status != "DRAFT" || next.Review != nil {
					t.Fatalf("affected suggestion did not reopen after barrier %+v", next)
				}
				id, in = d.action("engine.qa.design.v6")
				m = in.Manifest
				m.Cases[0].Preconditions += "; use an empty directory"
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
				d.drive()
				id, in = d.action("engine.qa.review.rationale.v2")
				r = approvedReview(m)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
				d.drive()
				if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
					t.Fatal("old affected cases reopened a successful review")
				}
			}
			rev := d.run.Revision
			d.drive()
			d.reopen()
			if d.run.Revision != rev {
				t.Fatal("unchanged Drive repeated suggestion transition")
			}
		})
	}
}

func TestFindingFactsResumeOnceAfterAllRequirementAsksResolve(t *testing.T) {
	d, finding, waveID := validationWithGateFinding(t)
	for i := 0; i < 2; i++ {
		event, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise requirement"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(event)
	}
	resolveGateFinding(d, finding, waveID)
	for i := 0; i < 2; i++ {
		d.reopen()
		d.drive()
		held := d.run.Validation.Waves[0]
		if held.Status != "ADJUDICATING" || held.Counted || held.Repair != nil {
			t.Fatal("unresolved requirement change advanced the wave")
		}
		if len(d.run.Validation.Findings[finding].History) != 1 {
			t.Fatal("adjudication fact was lost or duplicated")
		}
		for _, input := range d.run.ValidationInputs {
			if input.Handler == "engine.repair.v7" {
				t.Fatal("requirement barrier issued repair")
			}
		}
		ask := d.run.PendingRequirementAsk
		d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "abort"}})
	}
	d.reopen()
	d.drive()
	resumed := d.run.Validation.Waves[0]
	if resumed.Status != "REPAIR" || !resumed.Counted || resumed.Repair == nil || len(resumed.Repair.Problems) != 1 {
		t.Fatal("held finding did not produce one repair obligation")
	}
	count := 0
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.repair.v7" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("repair dispatch count=%d", count)
	}
	revision := d.run.Revision
	d.drive()
	d.reopen()
	if d.run.Revision != revision {
		t.Fatal("unchanged drive repeated adjudication settlement")
	}
}

func TestConfirmedInventoryRebindsOnlyAfterRequirementBarrier(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, input := d.action("engine.qa.design.v6")
	manifest := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, input = d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved})
	document := validationDocument + "\n"
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	work := []protocol.RequirementWorkDecision{}
	for _, item := range d.run.RequirementWork {
		work = append(work, protocol.RequirementWorkDecision{Work: item, Disposition: protocol.RequirementWorkCarry, Reason: "same execution obligations"})
	}
	receipt := driverState(t, d).CurrentIntakeConfirmation()
	if err := os.WriteFile(filepath.Join(d.f.root, receipt.RequirementSource), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := prepareConfirmedIntake(d.f.root, receipt, true)
	if err != nil {
		t.Fatal(err)
	}
	pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	ask := d.run.PendingRequirementAsk
	d.reopen()
	d.drive()
	held := d.run.Validation.Sets[coverage.ReviewBlackbox]
	if held.Status != "FROZEN" || held.Manifest.Binding.RequirementRevision != d.revision || held.Review.Binding.RequirementRevision != d.revision {
		t.Fatal("inventory observation rebound cases during barrier")
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "confirmed editorial change"}}}.WithRequirementDocument(document))
	d.reopen()
	d.drive()
	carried := d.run.Validation.Sets[coverage.ReviewBlackbox]
	if carried.Status != "FROZEN" || carried.Manifest.Binding.RequirementRevision != revision || carried.Review.Binding.RequirementRevision != revision {
		t.Fatal("confirmed inventory did not rebind after barrier")
	}
	if carried.ReviewHistory[0].Manifest.Binding.RequirementRevision != d.revision {
		t.Fatal("rebind changed original review evidence")
	}
	current := d.run.Revision
	d.drive()
	d.reopen()
	if d.run.Revision != current {
		t.Fatal("unchanged drive repeated inventory rebind")
	}
}

func TestRequirementRedoDiscardsPendingWaveSettlement(t *testing.T) {
	d, finding, waveID := validationWithGateFinding(t)
	document := validationDocument + "\n"
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	target := d.run.Validation.Waves[0].Targets[d.run.Validation.Findings[finding].Target].Task
	work := []protocol.RequirementWorkDecision{}
	redone := false
	for _, item := range d.run.RequirementWork {
		disposition := protocol.RequirementWorkCarry
		reason := "same inputs and obligation"
		if item.Task == target {
			disposition = protocol.RequirementWorkRedo
			reason = "user requires a fresh gate result"
			redone = true
		}
		work = append(work, protocol.RequirementWorkDecision{Work: item, Disposition: disposition, Reason: reason})
	}
	if !redone {
		t.Fatal("gate result missing from selectable requirement work")
	}
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:fresh-gate", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm fresh review"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	resolveGateFinding(d, finding, waveID)
	d.reopen()
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "confirm exact work carry and gate redo"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	d.reopen()
	d.drive()
	old := d.run.Validation.Waves[0]
	if old.Status != "INVALIDATED" || old.Counted || old.Repair != nil {
		t.Fatal("superseded wave consumed a repair or wave allowance")
	}
	if len(d.run.Validation.Findings[finding].History) != 1 {
		t.Fatal("requirement redo erased original observation evidence")
	}
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.repair.v7" {
			t.Fatal("held finding issued repair after its gate was redone")
		}
	}
}
