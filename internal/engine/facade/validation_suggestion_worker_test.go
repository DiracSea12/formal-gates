package facade

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestApprovedQASuggestionsHaveIssuedSemanticDuty(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.qa.design.v5")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, in = d.action("engine.qa.review.rationale.v2")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify optional preparation"}}})
	d.reopen()
	d.drive()
	d.action("engine.validation.observation.v2")
}

func TestQASuggestionWorkerPreservesApprovalAndResumesOnce(t *testing.T) {
	for _, affected := range []bool{false, true} {
		t.Run(map[bool]string{false: "defer", true: "accepted-case-change"}[affected], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.qa.design.v5")
			manifest := simpleManifest(in)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
			d.drive()
			id, in = d.action("engine.qa.review.rationale.v2")
			approved := approvedReview(manifest)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify optional setup"}, {Severity: "P2", Message: "Clarify optional output"}}})
			id, in = d.action("engine.validation.observation.v2")
			if len(in.Observation.Suggestions) != 2 || in.Observation.WaveID != "" {
				t.Fatal("approved pre-wave suggestions have no finite original duty")
			}
			issued := in.Observation.Suggestions
			if issued[0].Workspace == nil || issued[0].Workspace.Path == "" || !reflect.DeepEqual(issued[0].Manifest, manifest) || !reflect.DeepEqual(issued[0].Review, approved) {
				t.Fatal("suggestion omitted the actual original case, review or resource carrier")
			}
			d.reopen()
			d.drive()
			repeatedID, _ := d.action("engine.validation.observation.v2")
			if repeatedID != id {
				t.Fatal("unchanged recovery issued another semantic choice")
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			original := snapshot.State.WorkerInputs(d.f.definition)[id]
			judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{}, GateImpacts: []worker.ImpactJudgment{}, Suggestions: []worker.SuggestionJudgment{{Decision: "DEFER", Reason: "Optional setup is outside the confirmed acceptance", AffectedCases: []int{}}, {Decision: "DECLINE", Reason: "Approved output already has the specified oracle", AffectedCases: []int{}}}}
			if affected {
				index := slices.IndexFunc(issued[0].Manifest.Cases, func(c coverage.AcceptanceCase) bool { return c.CaseID == "invalid" })
				if index < 0 {
					t.Fatal("fixture does not contain the original invalid-input case")
				}
				judgment.Suggestions[0] = worker.SuggestionJudgment{Decision: "ACCEPT", Reason: "Clarify this case's executable preparation", AffectedCases: []int{index + 1}}
				bad := judgment
				bad.Suggestions = append([]worker.SuggestionJudgment{}, judgment.Suggestions...)
				bad.Suggestions[0].AffectedCases = []int{len(issued[0].Manifest.Cases) + 1}
				raw, _ := json.Marshal(bad)
				if _, err := worker.DecodeResult(original, d.eventID(), raw, "out-of-range-suggestion-case"); err == nil {
					t.Fatal("unknown affected case selection accepted outside the original manifest")
				}

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
				d.action("engine.qa.design.v5")
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
				if input.Handler == "engine.validation.observation.v2" {
					t.Fatal("disposed suggestions created another semantic obligation")
				}
			}
		})
	}
}

func TestQASuggestionsShareCompletedModeBarrier(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	dev, development := d.action("engine.development.batch.v4")
	id, in := qaModeAction(d, "engine.qa.design.v5", coverage.ReviewBlackbox)
	black := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &black})
	d.drive()
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	approved := approvedReview(black)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional public-entry clarification"}}})
	d.drive()
	for _, input := range d.run.ValidationInputs {
		if input.Handler == "engine.validation.observation.v2" {
			t.Fatal("unfinished mode changed an already issued suggestion duty")
		}
	}
	d.business(dev, protocol.ValidationResult{Binding: development.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement the confirmed command"}})
	d.materialize()
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.design.v5", coverage.ReviewWhitebox)
	white := in.Manifest
	white.Cases = []coverage.AcceptanceCase{{CaseID: "white-normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input rejected"}}
	for _, c := range white.Cases {
		white.Edges = append(white.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &white, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	id, in = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewWhitebox)
	approved = approvedReview(white)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional test explanation"}}})
	id, in = d.action("engine.validation.observation.v2")
	suggestions := in.Observation.Suggestions
	if len(suggestions) != 2 || suggestions[0].Mode != coverage.ReviewBlackbox || suggestions[1].Mode != coverage.ReviewWhitebox || suggestions[0].Workspace.Path == suggestions[1].Workspace.Path || suggestions[1].Workspace.Path == "" || !reflect.DeepEqual(suggestions[1].Manifest, white) {
		t.Fatal("multi-mode duty lost its independent source resource or input order")
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{}, GateImpacts: []worker.ImpactJudgment{}, Suggestions: []worker.SuggestionJudgment{{Decision: "DEFER", Reason: "Confirmed public-entry cases already cover their acceptance", AffectedCases: []int{}}, {Decision: "DECLINE", Reason: "Approved test explanations already preserve the intended oracle", AffectedCases: []int{}}}}
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
	id, in := d.action("engine.qa.execution.v3")
	execution := executionFor(in)
	d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &execution}))
	id, in = d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
	d.drive()
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.reopen()
	d.drive()
	id, in = d.action("engine.qa.design.v5")
	manifest := in.Manifest
	manifest.Cases[0].Preconditions += "; initialize the executable input"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest})
	id, in = d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Optional clarified setup wording"}}})
	d.reopen()
	d.drive()
	_, in = d.action("engine.validation.observation.v2")
	if len(in.Observation.Suggestions) != 1 || !reflect.DeepEqual(in.Observation.Suggestions[0].Manifest, manifest) || len(d.run.Validation.Waves) == 0 {
		t.Fatal("existing wave hid the newly approved case suggestion")
	}
}

func TestAcceptedSuggestionReachesIssuedCaseRevision(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, input := d.action("engine.qa.design.v5")
	manifest := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	id, input = d.action("engine.qa.review.rationale.v2")
	approved := approvedReview(manifest)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Specify the rejecting diagnostic in the invalid oracle"}}})
	suggestion := d.run.Validation.Sets[coverage.ReviewBlackbox].Suggestions[0]
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "case correction", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewBlackbox, SuggestionID: suggestion.ID, Decision: "ACCEPT", Reason: "The corrected oracle must state the actual diagnostic", AffectedCases: []string{"invalid"}}}})
	d.drive()
	id, input = d.action("engine.qa.design.v5")
	if len(input.AcceptedSuggestions) != 1 || input.AcceptedSuggestions[0].ID != suggestion.ID || input.AcceptedSuggestions[0].Message != suggestion.Message || input.AcceptedSuggestions[0].Decision != "ACCEPT" || !reflect.DeepEqual(input.AffectedCases, []string{"invalid"}) {
		t.Fatal("issued revision omitted its original accepted instruction or exact affected cases")
	}
	d.reopen()
	d.drive()
	repeatedID, repeatedInput := d.action("engine.qa.design.v5")
	if repeatedID != id || !reflect.DeepEqual(input, repeatedInput) {
		t.Fatal("recovery changed the issued correction or created another design duty")
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResult(snapshot.State.WorkerInputs(d.f.definition)[id], d.eventID(), []byte(`{"cases":[{"oracle":"rejects invalid input with its specified diagnostic"}]}`), "original-case-correction")
	if err != nil {
		t.Fatal(err)
	}
	d.receipt(id)
	if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
		t.Fatalf("case revision rejected: %+v", accepted)
	}
	d.drive()
	id, input = d.action("engine.qa.review.rationale.v2")
	if !reflect.DeepEqual(input.Manifest.Cases[0], manifest.Cases[0]) || input.Manifest.Cases[1].Oracle != "rejects invalid input with its specified diagnostic" || len(input.InheritableReviews) != 1 || input.InheritableReviews[0].ID != "normal" || len(input.AcceptedSuggestions) != 1 || input.AcceptedSuggestions[0].ID != suggestion.ID || !reflect.DeepEqual(input.AffectedCases, []string{"invalid"}) {
		t.Fatal("revision changed an unaffected case or inherited the affected approval")
	}
	approved = approvedReview(input.Manifest)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved})
	d.drive()
	if set := d.run.Validation.Sets[coverage.ReviewBlackbox]; set.Status != "FROZEN" || len(set.ReviewHistory) != 2 || len(set.AffectedCases) != 0 {
		t.Fatal("independently approved correction did not finish in the original owner")
	}
}
