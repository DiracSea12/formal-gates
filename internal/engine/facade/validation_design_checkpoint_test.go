package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestDesignCheckpointsPreserveDutyAndResumeRecordedCases(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "reopen", true: "native-resume"}[resume], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design")
			full := simpleManifest(in)
			part := full
			part.Cases = append([]coverage.AcceptanceCase(nil), full.Cases[:1]...)
			part.Edges = nil
			for _, e := range full.Edges {
				if e.CaseID == part.Cases[0].CaseID {
					part.Edges = append(part.Edges, e)
				}
			}
			d.receipt(id)
			start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
			d.submit(start)
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "record first designed case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &part, Checkpoint: true}}}
			accepted := d.submit(event)
			before, _ := d.f.engine.Load()
			again := d.submit(event)
			after, _ := d.f.engine.Load()
			if !reflect.DeepEqual(accepted, again) || !reflect.DeepEqual(before, after) {
				t.Fatal("checkpoint replay changed original receipt or state")
			}
			d.reopen()
			d.drive()
			current, ok := d.run.ValidationInputs[id]
			if !ok || current.Binding != in.Binding || !reflect.DeepEqual(current.Manifest, part) {
				t.Fatal("checkpoint changed duty or lost recorded design")
			}
			for _, next := range d.run.ValidationInputs {
				if next.Handler == "engine.qa.review.rationale" {
					t.Fatal("partial design prepared review")
				}
			}
			if resume {
				plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint())
				if err != nil {
					t.Fatal(err)
				}
				snapshot, _ := d.f.engine.Load()
				var intent protocol.HostActionIntent
				for _, p := range snapshot.State.PendingHostActions {
					if p.Correlation == plan.RequestID {
						intent = p
					}
				}
				if intent.Resume == nil || intent.Resume.Input == nil || !reflect.DeepEqual(intent.Resume.Input.Manifest, part) || intent.Resume.Input.Binding != in.Binding {
					t.Fatal("native resume lost recorded design")
				}
				receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: protocol.LifecycleStart})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(receipt)
			}
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "completed all designed cases", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &full}}})
			review, ri := d.action("engine.qa.review.rationale")
			if !reflect.DeepEqual(ri.Manifest, full) {
				t.Fatal("final completion lost cumulative cases")
			}
			r := approvedReview(full)
			d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
			snapshot, _ := d.f.engine.Load()
			if !reflect.DeepEqual(*snapshot.State.Results[id].Validation.Manifest, full) {
				t.Fatal("final original result lost content")
			}
		})
	}
}

func TestDesignCheckpointCannotOmitPreviouslyApprovedCase(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	id, in = d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	r.CaseDecisions[0].Status = coverage.StatusFail
	r.PointDecisions[0].Status = coverage.StatusFail
	r.SourceDecisions[0].Status = coverage.StatusFail
	r.SetStatus = coverage.StatusFail
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &r})
	id, in = d.action("engine.qa.design")
	part := m
	part.Cases = append([]coverage.AcceptanceCase(nil), m.Cases[:1]...)
	part.Edges = nil
	for _, e := range m.Edges {
		if e.CaseID == part.Cases[0].CaseID {
			part.Edges = append(part.Edges, e)
		}
	}
	d.receipt(id)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "omit approved negative case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &part, Checkpoint: true}}})
	if a.Status != "REJECTED_INVALID_RESULT" {
		t.Fatal("checkpoint dropped approved case")
	}
	retry, ri := d.action("engine.qa.design")
	if ri.Binding.Task != in.Binding.Task {
		t.Fatal("invalid checkpoint reset duty")
	}
	m.Cases[0].Oracle = "exit zero and print accepted input"
	d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Manifest: &m})
	_, ri = d.action("engine.qa.review.rationale")
	if len(ri.InheritableReviews) != 1 || ri.InheritableReviews[0].ID != "invalid" {
		t.Fatal("retained approval was lost or changed case inherited")
	}
}

func TestDesignCompletionRequiresCoverageAndLocksOnlyItsMode(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	id, in := d.action("engine.development.batch")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	d.materialize()
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
	full := simpleManifest(in)
	part := full
	part.Cases = append([]coverage.AcceptanceCase(nil), full.Cases[:1]...)
	part.Edges = nil
	for _, edge := range full.Edges {
		if edge.CaseID == part.Cases[0].CaseID {
			part.Edges = append(part.Edges, edge)
		}
	}
	d.receipt(id)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "incomplete final design", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &part}}})
	if a.Status != "REJECTED_INVALID_RESULT" {
		t.Fatal("partial final design skipped full scenario contract")
	}
	retry, ri := qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
	if ri.Binding.Task != in.Binding.Task || d.run.Attempts[ri.Binding.Task].Attempts != 1 {
		t.Fatal("invalid final design reset original allowance")
	}
	d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Manifest: &full})
	_, white := qaModeAction(d, "engine.qa.design", coverage.ReviewWhitebox)
	if white.Binding.Task == ri.Binding.Task {
		t.Fatal("mode responsibilities folded")
	}
	review, reviewInput := qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewBlackbox)
	r := approvedReview(full)
	d.business(review, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &r})
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	design, di := qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
	replacement := full
	replacement.Cases = append([]coverage.AcceptanceCase(nil), full.Cases[1:]...)
	replacement.Edges = nil
	for _, edge := range full.Edges {
		if edge.CaseID == replacement.Cases[0].CaseID {
			replacement.Edges = append(replacement.Edges, edge)
		}
	}
	d.business(design, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Manifest: &replacement})
	review, reviewInput = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewBlackbox)
	if len(reviewInput.Manifest.Cases) != 1 {
		t.Fatal("explicit final replacement could not remove obsolete case")
	}
	r = approvedReview(replacement)
	d.business(review, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &r})
	d.reopen()
	_, afterWhite := qaModeAction(d, "engine.qa.design", coverage.ReviewWhitebox)
	if afterWhite.Binding != white.Binding {
		t.Fatal("blackbox final edits replaced independent whitebox duty")
	}
	if len(d.run.Validation.Sets[coverage.ReviewBlackbox].ReviewHistory) != 2 {
		t.Fatal("explicit replacement erased original approval")
	}
}

func TestThawRetiresCheckpointedDesignWithoutLosingLateEvidence(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "record draft", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Checkpoint: true}}})
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	next, ni := d.action("engine.qa.design")
	if next == id || ni.Binding.Task == in.Binding.Task {
		t.Fatal("THAW retained superseded design duty")
	}
	late := protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m}
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "late completed design", Validation: &late}})
	snapshot, _ := d.f.engine.Load()
	if len(snapshot.State.ObsoleteResults[id]) != 1 || !reflect.DeepEqual(snapshot.State.ObsoleteResults[id][0].Validation, &late) {
		t.Fatal("late design evidence lost its original content")
	}
	if _, ok := snapshot.State.Results[id]; ok {
		t.Fatal("superseded design became effective")
	}
	d.business(next, protocol.ValidationResult{Binding: ni.Binding, Status: "PASS", Manifest: &m})
	_, ri := d.action("engine.qa.review.rationale")
	if !reflect.DeepEqual(ri.Manifest, m) {
		t.Fatal("replacement did not prepare actual complete design")
	}
}
