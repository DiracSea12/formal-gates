package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"reflect"
	"slices"
	"testing"
)

func TestDesignRetentionPassesOriginalAdmissionAndKeepsReviewInheritance(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design.v6")
	m := simpleManifest(in)
	m.SharedSteps = []coverage.SharedStep{{ID: "setup", Procedure: "prepare the existing fixture", Operations: []coverage.Operation{{Kind: coverage.OperationShell, Command: "printf fixture"}}}}
	m.Cases[1].SharedSteps = []string{"setup"}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	id, in = d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	r.SharedStepDecisions = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "The fixed fixture preparation supplies the referenced negative case"}}
	r.CaseDecisions[0].Status, r.PointDecisions[0].Status, r.SourceDecisions[0].Status = coverage.StatusFail, coverage.StatusFail, coverage.StatusFail
	r.SetStatus = coverage.StatusFail
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &r})
	id, in = d.action("engine.qa.design.v6")
	d.reopen()
	d.drive()
	in = d.run.ValidationInputs[id]
	changed := m.Cases[0]
	changed.Oracle = "exit zero and print accepted input"
	coverageChoices := make([][]int, len(in.Manifest.Points))
	for index, point := range in.Manifest.Points {
		for _, edge := range m.Edges {
			if edge.PointID == point.PointID {
				selected := 1
				if edge.CaseID == m.Cases[1].CaseID {
					selected = 2
				}
				if !slices.Contains(coverageChoices[index], selected) {
					coverageChoices[index] = append(coverageChoices[index], selected)
				}
			}
		}
	}
	value := worker.Design{DesignManifest: worker.DesignManifest{Cases: []worker.DesignedCase{{Case: 1, Scenario: changed.Scenario, PublicEntry: changed.PublicEntry, Preconditions: changed.Preconditions, Steps: changed.Steps, Oracle: changed.Oracle, Operations: changed.Operations}}, Coverage: coverageChoices}}
	missingShared := value
	missingShared.RemoveSharedSteps = []int{1}
	invalidRaw, err := json.Marshal(missingShared)
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.qa.design.v6", Provider: DefaultProvider, Pending: protocol.PendingAction{ActionID: id}, Validation: &in}, d.eventID(), invalidRaw, "original-native-answer")
	if err != nil {
		t.Fatal(err)
	}
	contract := *in.Contract
	contract.Manifests = []coverage.AcceptanceManifest{*invalid.Result.Validation.Manifest}
	if err := contract.Validate(); err == nil {
		t.Fatal("core accepted retained case referencing a deleted shared step")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.qa.design.v6", Provider: DefaultProvider, Pending: protocol.PendingAction{ActionID: id}, Validation: &in}, d.eventID(), raw, "original-native-answer")
	if err != nil {
		t.Fatal(err)
	}
	contract.Manifests = []coverage.AcceptanceManifest{*event.Result.Validation.Manifest}
	if err := contract.Validate(); err != nil {
		t.Fatalf("retained complete contract rejected: %v", err)
	}
	d.receipt(id)
	a := d.submit(event)
	if a.Status == "REJECTED_INVALID_RESULT" {
		t.Fatal("retained issued content failed original admission")
	}
	_, current := d.action("engine.qa.review.rationale.v2")
	if len(current.Manifest.Cases) != 2 || !reflect.DeepEqual(current.Manifest.Cases[1], m.Cases[1]) || len(current.InheritableReviews) != 1 || current.InheritableReviews[0].ID != m.Cases[1].CaseID || len(current.Manifest.Edges) != len(m.Edges) {
		t.Fatalf("continued design lost retained responsibility or its unchanged review: cases=%#v original=%#v inherited=%#v edges=%#v originalEdges=%#v", current.Manifest.Cases, m.Cases[1], current.InheritableReviews, current.Manifest.Edges, m.Edges)
	}
}

func TestDesignCheckpointsPreserveDutyAndResumeRecordedCases(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "reopen", true: "native-resume"}[resume], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design.v6")
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
				if next.Handler == "engine.qa.review.rationale.v2" {
					t.Fatal("partial design prepared review")
				}
			}
			if resume {
				plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
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
			review, ri := d.action("engine.qa.review.rationale.v2")
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
	id, in := d.action("engine.qa.design.v6")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	id, in = d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	r.CaseDecisions[0].Status = coverage.StatusFail
	r.PointDecisions[0].Status = coverage.StatusFail
	r.SourceDecisions[0].Status = coverage.StatusFail
	r.SetStatus = coverage.StatusFail
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &r})
	id, in = d.action("engine.qa.design.v6")
	part := m
	part.Cases = append([]coverage.AcceptanceCase(nil), m.Cases[:1]...)
	part.Edges = nil
	for _, e := range m.Edges {
		if e.CaseID == part.Cases[0].CaseID {
			part.Edges = append(part.Edges, e)
		}
	}
	d.receipt(id)
	a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "repeat original failed case with no actual repair", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &part, Checkpoint: true}}})
	if a.Status != "REJECTED_INVALID_RESULT" {
		t.Fatal("no-op checkpoint repair was accepted")
	}
	retry, ri := d.action("engine.qa.design.v6")
	if ri.Binding.Task != in.Binding.Task {
		t.Fatal("invalid checkpoint reset duty")
	}
	m.Cases[0].Oracle = "exit zero and print accepted input"
	d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Manifest: &m})
	_, ri = d.action("engine.qa.review.rationale.v2")
	if len(ri.InheritableReviews) != 1 || ri.InheritableReviews[0].ID != "invalid" {
		t.Fatal("retained approval was lost or changed case inherited")
	}
}

func TestDesignCompletionRequiresCoverageAndLocksOnlyItsMode(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	id, in := d.action("engine.development.batch.v6")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	d.materialize()
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.design.v6", coverage.ReviewBlackbox)
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
	retry, ri := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewBlackbox)
	if ri.Binding.Task != in.Binding.Task || d.run.Attempts[ri.Binding.Task].Attempts != 1 {
		t.Fatal("invalid final design reset original allowance")
	}
	d.business(retry, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Manifest: &full})
	_, white := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewWhitebox)
	if white.Binding.Task == ri.Binding.Task {
		t.Fatal("mode responsibilities folded")
	}
	review, reviewInput := qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	r := approvedReview(full)
	d.business(review, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &r})
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	design, di := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewBlackbox)
	replacement := full
	replacement.Cases = append([]coverage.AcceptanceCase(nil), full.Cases[1:]...)
	replacement.Edges = nil
	for _, edge := range full.Edges {
		if edge.CaseID == replacement.Cases[0].CaseID {
			replacement.Edges = append(replacement.Edges, edge)
		}
	}
	d.business(design, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Manifest: &replacement, DesignChange: &protocol.QADesignChange{Replace: true}})
	review, reviewInput = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	if len(reviewInput.Manifest.Cases) != 1 {
		t.Fatal("explicit final replacement could not remove obsolete case")
	}
	r = approvedReview(replacement)
	d.business(review, protocol.ValidationResult{Binding: reviewInput.Binding, Status: "PASS", Review: &r})
	d.reopen()
	_, afterWhite := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewWhitebox)
	if afterWhite.Binding != white.Binding {
		t.Fatal("blackbox final edits replaced independent whitebox duty")
	}
	if len(d.run.Validation.Sets[coverage.ReviewBlackbox].ReviewHistory) != 2 {
		t.Fatal("explicit replacement erased original approval")
	}
}

func TestThawRetiresCheckpointedDesignWithoutLosingLateEvidence(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design.v6")
	m := simpleManifest(in)
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "record draft", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Checkpoint: true}}})
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	next, ni := d.action("engine.qa.design.v6")
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
	_, ri := d.action("engine.qa.review.rationale.v2")
	if !reflect.DeepEqual(ri.Manifest, m) {
		t.Fatal("replacement did not prepare actual complete design")
	}
}

func TestDesignIncrementExplicitRemovalSurvivesReplayAndRecovery(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	design, input := d.action("engine.qa.design.v6")
	manifest := simpleManifest(input)
	obsolete := manifest.Cases[0]
	obsolete.CaseID, obsolete.Steps = "obsolete", "repeat the superseded public example"
	manifest.Cases = append(manifest.Cases, obsolete)
	for _, link := range input.SourcePoints {
		manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: link.SourceID, PointID: link.PointID, CaseID: obsolete.CaseID})
	}
	d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	review, ri := d.action("engine.qa.review.rationale.v2")
	judgment := approvedReview(manifest)
	judgment.CaseDecisions[2].Status = coverage.StatusFail
	judgment.PointDecisions[0].Status, judgment.SourceDecisions[0].Status, judgment.SetStatus = coverage.StatusFail, coverage.StatusFail, coverage.StatusFail
	d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "FAIL", Review: &judgment})
	design, input = d.action("engine.qa.design.v6")
	value := worker.Design{DesignManifest: worker.DesignManifest{RemoveCases: []int{3}, Cases: []worker.DesignedCase{}, Coverage: make([][]int, len(input.Manifest.Points))}}
	raw, _ := json.Marshal(value)
	event, err := worker.DecodeResult(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: input.Handler, Provider: d.host(), Pending: d.run.PendingActions[design], Validation: &input}, d.eventID(), raw, "original-explicit-removal")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(design)
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("explicit removal rejected: %+v", accepted)
	}
	before, _ := d.f.engine.Load()
	d.submit(event)
	after, _ := d.f.engine.Load()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("removal replay changed the retained projection")
	}
	d.reopen()
	_, current := d.action("engine.qa.review.rationale.v2")
	if !reflect.DeepEqual(current.Manifest.Cases, manifest.Cases[:2]) || len(current.InheritableReviews) != 2 || slices.ContainsFunc(current.Manifest.Edges, func(edge coverage.CoverageEdge) bool { return edge.CaseID == "obsolete" }) {
		t.Fatal("explicit removal lost retained IDs, approval or coverage after recovery")
	}
}
