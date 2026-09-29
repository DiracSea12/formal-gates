package facade

import (
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestBlackboxInputsRemainIsolatedAfterThaw(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	assertValidationInputModes(t, d)
	id, in := d.action("engine.qa.design")
	m := simpleManifest(in)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	assertValidationInputModes(t, d)
	id, in = d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
	id, in = d.action("engine.development.batch")
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement input validation", Build: []string{"build succeeds"}, Tests: []string{"input tests succeed"}}})
	d.materialize()
	d.materialize()
	assertValidationInputModes(t, d)
	id, in = d.action("engine.qa.design")
	m = in.Manifest
	m.Cases = []coverage.AcceptanceCase{{CaseID: "white-ok", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestPrivateCacheBranch", Oracle: "private cache branch yields the accepted value"}, {CaseID: "white-error", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestPrivateCacheError", Oracle: "cache error branch returns rejection"}}
	for _, c := range m.Cases {
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	assertValidationInputModes(t, d)
	id, in = d.action("engine.qa.review.rationale")
	r = approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
	d.materialize()
	assertValidationInputModes(t, d)
	d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
	d.drive()
	_, in = d.action("engine.qa.design")
	for _, manifest := range in.Contract.Manifests {
		for _, c := range manifest.Cases {
			if manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				t.Errorf("blackbox design received whitebox case %s with test=%s oracle=%s", c.CaseID, c.TestRef, c.Oracle)
			}
		}
	}
	d.reopen()
	assertValidationInputModes(t, d)
	id, in = d.action("engine.qa.design")
	m = in.Manifest
	m.Cases[0].Preconditions += "; initialize a clean input history"
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	_, in = d.action("engine.qa.review.rationale")
	for _, manifest := range in.Contract.Manifests {
		if manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
			t.Errorf("blackbox review after reopen received %d whitebox cases", len(manifest.Cases))
		}
	}

}

func assertValidationInputModes(t *testing.T, d *validationDriver) {
	t.Helper()
	for _, input := range d.run.ValidationInputs {
		switch input.Handler {
		case "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution":
			mode := input.Manifest.Binding.ReviewKind
			if input.Contract == nil || len(input.Contract.SelectedKinds) != 1 || input.Contract.SelectedKinds[0] != mode || len(input.Contract.RequiredSources.Sources) != 1 || len(input.Manifest.Points) != 1 || input.Manifest.Points[0].PointID != "AC-001" {
				t.Fatalf("QA lost its mode/source/point contract: %+v", input)
			}
			for _, manifest := range input.Contract.Manifests {
				if manifest.Binding.ReviewKind != mode {
					t.Fatalf("%s received %s manifest", mode, manifest.Binding.ReviewKind)
				}
			}
			if input.Handler == "engine.qa.review.rationale" && (len(input.Contract.Manifests) != 1 || len(input.Contract.Manifests[0].Cases) != 2) {
				t.Fatal("review lost own cases")
			}
			if input.Handler == "engine.qa.execution" && (input.Execution == nil || input.Whitelist == nil || len(input.Execution.ExpectedCaseIDs) != 2) {
				t.Fatal("execution lost approved cases")
			}
		case "engine.gate", "engine.development.batch":
			if input.Contract != nil || len(input.Manifest.Cases) != 0 || len(input.Completed) != 0 || input.Execution != nil {
				t.Fatal("non-QA role received QA materials")
			}
		}
	}
}
