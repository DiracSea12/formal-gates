package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestWhiteboxSemanticRevisionReusesExactAuthoringMaterials(t *testing.T) {
	t.Parallel()
	d, runner := startNativeProviderDriver(t, "svn")
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	d.confirmRoute(routeDecisionForGates([]string{"whitebox", "quality"}))
	d.drive()
	dev, di := d.action("engine.development.batch.v6")
	if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) > 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
	d.drive()
	design, in := d.action("engine.qa.design.v6")
	manifest := in.Manifest
	manifest.Cases = []coverage.AcceptanceCase{{CaseID: "valid", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "Valid accepts the actual word", Files: []string{"app_test.go"}}, {CaseID: "invalid", Mode: coverage.CaseWhitebox, Scenario: "MISUSE", TestRef: "app_test.go::TestInvalid", Oracle: "Valid rejects empty input", Files: []string{"app_test.go"}}}
	manifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "valid"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "invalid"}}
	body := "package command\nimport \"testing\"\nfunc TestValid(t *testing.T) { if !Valid(\"word\") { t.Fatal(\"valid input\") } }\nfunc TestInvalid(t *testing.T) { if Valid(\"\") { t.Fatal(\"invalid input\") } }\n"
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app_test.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	d.business(design, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.drive()
	review, before := d.action("engine.qa.review.rationale.v2")
	if before.ReviewArtifact == nil {
		t.Fatal("first actual test artifact is absent")
	}
	approved := approvedReview(manifest)
	d.business(review, protocol.ValidationResult{Binding: before.Binding, Status: "PASS", Review: &approved, Suggestions: []protocol.QASuggestion{{Severity: "P3", Message: "Clarify the word in the oracle"}}})
	suggestion := d.run.Validation.Sets[coverage.ReviewWhitebox].Suggestions[0]
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: "oracle clarification", Validation: &protocol.ValidationObservation{Kind: "SUGGESTION", Mode: coverage.ReviewWhitebox, SuggestionID: suggestion.ID, Decision: "ACCEPT", Reason: "Specify the actual tested word", AffectedCases: []string{"valid"}}}})
	d.drive()
	design, in = d.action("engine.qa.design.v6")
	if in.Workspace.ID != before.Workspace.ID || in.Workspace.Source != before.ReviewBase || in.Workspace.Identity != before.ReviewArtifact.Identity {
		t.Fatalf("semantic reopening discarded actual source/material identity: %+v; original=%+v", in.Workspace, before.Workspace)
	}
	actual, err := os.ReadFile(filepath.Join(in.Workspace.Path, "app_test.go"))
	if err != nil || string(actual) != body {
		t.Fatal("issued revision lacks original test bytes", err)
	}
	reopenNativeProviderDriver(t, d, "svn", runner)
	sameDesign, reopened := d.action("engine.qa.design.v6")
	if sameDesign != design || !reflect.DeepEqual(reopened, in) {
		t.Fatal("recovery changed issued revision materials")
	}
	paths, err := d.f.vcs.At(in.Workspace.Path, in.Workspace.Handle).ChangedPathsFrom(in.Workspace.Source)
	if err != nil || !reflect.DeepEqual(paths, []string{"app_test.go"}) {
		t.Fatal("semantic native facts lost actual material", paths, err)
	}
	manifest = in.Manifest
	manifest.Cases[0].Oracle += ": word"
	d.business(design, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: paths}})
	d.drive()
	review, after := d.action("engine.qa.review.rationale.v2")
	if after.ReviewArtifact == nil || after.Binding.Candidate != before.Binding.Candidate || after.Workspace.ID != before.Workspace.ID || after.ReviewBase != before.ReviewBase || !reflect.DeepEqual(after.ReviewArtifact.Changes, before.ReviewArtifact.Changes) {
		t.Fatalf("semantic revision lost exact candidate test diff: %+v", after)
	}
	failed := approvedReview(manifest)
	failed.Scope = after.ReviewScope
	failed.SetStatus = coverage.StatusFail
	failed.CaseDecisions[1] = after.InheritableReviews[0]
	failed.CaseDecisions[1].Inherited = true
	failed.CaseDecisions[0].Status = coverage.StatusFail
	failed.CaseDecisions[0].Reason = "Clarify this oracle once more while retaining its actual assertion"
	failed.PointDecisions[0].Status = coverage.StatusFail
	failed.PointDecisions[0].Reason = "The bound positive case needs its actual input stated in the oracle"
	failed.SourceDecisions[0].Status = coverage.StatusFail
	failed.SourceDecisions[0].Reason = "The bound acceptance point includes the unresolved positive oracle"
	d.business(review, protocol.ValidationResult{Binding: after.Binding, Status: "FAIL", Review: &failed})
	if len(d.run.RejectedResults) != 0 {
		t.Fatalf("bound failed review was rejected: %+v", d.run.RejectedResults)
	}
	d.drive()
	_, unapproved := d.action("engine.qa.design.v6")
	if unapproved.Workspace.ID != after.Workspace.ID || unapproved.Workspace.Identity != after.ReviewArtifact.Identity {
		t.Fatal("failed review discarded written test material source", unapproved.Workspace)
	}
	actual, err = os.ReadFile(filepath.Join(unapproved.Workspace.Path, "app_test.go"))
	if err != nil || string(actual) != body {
		t.Fatal("unapproved material bytes were replaced", err)
	}
}
