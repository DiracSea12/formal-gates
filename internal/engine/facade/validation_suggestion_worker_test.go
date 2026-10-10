package facade

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestApprovedQASuggestionsHaveIssuedSemanticDuty(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, in = d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify optional preparation"}}})
	d.reopen()
	d.drive()
	d.action("engine.validation.observation")
}

func TestQASuggestionWorkerPreservesApprovalAndResumesOnce(t *testing.T) {
	for _, affected := range []bool{false, true} {
		t.Run(map[bool]string{false: "defer", true: "accepted-case-change"}[affected], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design")
			manifest := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
			d.drive()
			id, in = d.action("engine.qa.review.rationale")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify optional setup"}, {Severity: "P2", Message: "Clarify optional output"}}})
			id, in = d.action("engine.validation.observation")
			if len(in.Observation.Suggestions) != 2 || in.Observation.WaveID != "" {
				t.Fatal("approved pre-wave suggestions have no finite original duty")
			}
			issued := in.Observation.Suggestions
			if issued[0].Workspace == nil || issued[0].Workspace.Path == "" || !reflect.DeepEqual(issued[0].Manifest, manifest) || !reflect.DeepEqual(issued[0].Review, approved) {
				t.Fatal("suggestion omitted the actual original case, review or resource carrier")
			}
			d.reopen()
			d.drive()
			repeatedID, _ := d.action("engine.validation.observation")
			if repeatedID != id {
				t.Fatal("unchanged recovery issued another semantic choice")
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			original := snapshot.State.WorkerInputs(d.f.definition)[id]
			judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{}, GateImpacts: []worker.ImpactJudgment{}, Suggestions: []worker.SuggestionJudgment{{Decision: "DEFER", Reason: "Optional setup is outside the confirmed acceptance", AffectedCases: []string{}}, {Decision: "DECLINE", Reason: "Approved output already has the specified oracle", AffectedCases: []string{}}}}
			if affected {
				judgment.Suggestions[0] = worker.SuggestionJudgment{Decision: "ACCEPT", Reason: "Clarify this case's executable preparation", AffectedCases: []string{"invalid"}}
			}
			raw, _ := json.Marshal(judgment)
			event, err := worker.DecodeResult(original, d.eventID(), raw, "original-suggestion-judgment")
			if err != nil {
				t.Fatal(err)
			}
			d.receipt(id)
			accepted := d.submit(event)
			if accepted.Status != "ACCEPTED" {
				t.Fatalf("suggestion rejected: %+v", accepted)
			}
			d.reopen()
			d.drive()
			set := d.run.Validation.Sets[coverage.ReviewBlackbox]
			if len(set.ReviewHistory) != 1 || set.Suggestions[0].Decision != judgment.Suggestions[0].Decision || set.Suggestions[1].Decision != "DECLINE" {
				t.Fatal("disposition lost original approval or exact independent choices")
			}
			if affected {
				if set.Status != "DRAFT" || set.Review != nil {
					t.Fatal("accepted impact bypassed redesign")
				}
				d.action("engine.qa.design")
			} else if set.Status != "FROZEN" || !reflect.DeepEqual(set.Review, &approved) {
				t.Fatal("optional disposition changed original approval")
			}
			result, _, err := d.f.Submit(event)
			if err != nil || !reflect.DeepEqual(result, accepted) {
				t.Fatal("result replay changed the original acceptance")
			}
			d.reopen()
			d.drive()
			for _, input := range d.run.ValidationInputs {
				if input.Handler == "engine.validation.observation" {
					t.Fatal("disposed suggestions created another semantic obligation")
				}
			}
		})
	}
}

func TestQASuggestionsShareCompletedModeBarrier(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	dev, development := d.action("engine.development.batch")
	id, in := qaModeAction(d, "engine.qa.design", coverage.ReviewBlackbox)
	black := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &black})
	d.drive()
	id, in = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewBlackbox)
	approved := approvedReview(black)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional public-entry clarification"}}})
	d.drive()
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.validation.observation" {
			t.Fatal("unfinished mode changed an already issued suggestion duty")
		}
	}
	d.business(dev, protocol.ValidationResult{Binding: development.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement the confirmed command"}})
	d.materialize()
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.design", coverage.ReviewWhitebox)
	white := in.Manifest
	white.Cases = []coverage.AcceptanceCase{{CaseID: "white-normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input rejected"}}
	for _, c := range white.Cases {
		white.Edges = append(white.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &white, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.review.rationale", coverage.ReviewWhitebox)
	approved = approvedReview(white)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional test explanation"}}})
	id, in = d.action("engine.validation.observation")
	suggestions := in.Observation.Suggestions
	if len(suggestions) != 2 || suggestions[0].Mode != coverage.ReviewBlackbox || suggestions[1].Mode != coverage.ReviewWhitebox || suggestions[0].Workspace.Path == suggestions[1].Workspace.Path || suggestions[1].Workspace.Path == "" || !reflect.DeepEqual(suggestions[1].Manifest, white) {
		t.Fatal("multi-mode duty lost its independent source resource or input order")
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{}, GateImpacts: []worker.ImpactJudgment{}, Suggestions: []worker.SuggestionJudgment{{Decision: "DEFER", Reason: "Confirmed public-entry cases already cover their acceptance", AffectedCases: []string{}}, {Decision: "DECLINE", Reason: "Approved test explanations already preserve the intended oracle", AffectedCases: []string{}}}}
	raw, _ := json.Marshal(judgment)
	event, err := worker.DecodeResult(snapshot.State.WorkerInputs(d.f.definition)[id], d.eventID(), raw, "original-multi-mode-judgment")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(id)
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("multi-mode disposition: %+v", accepted)
	}
	d.reopen()
	d.drive()
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox} {
		if set := d.run.Validation.Sets[mode]; set.Status != "FROZEN" || len(set.ReviewHistory) != 1 {
			t.Fatal("suggestions failed to freeze without re-review")
		}
	}
}

func TestQASuggestionDutyFollowsConfirmedCaseThaw(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	id, in := d.action("engine.qa.execution")
	execution := executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &execution}))
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	id, in = d.action("engine.qa.design")
	manifest := in.Manifest
	manifest.Cases[0].Preconditions += "; initialize the executable input"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
	id, in = d.action("engine.qa.review.rationale")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional clarified setup wording"}}})
	d.reopen()
	d.drive()
	_, in = d.action("engine.validation.observation")
	if len(in.Observation.Suggestions) != 1 || !reflect.DeepEqual(in.Observation.Suggestions[0].Manifest, manifest) || len(d.run.Validation.Waves) == 0 {
		t.Fatal("existing wave hid the newly approved case suggestion")
	}
}
