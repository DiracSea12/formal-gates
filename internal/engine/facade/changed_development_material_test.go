package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestRequirementDevelopmentRedoPreservesActualWhiteboxMaterials(t *testing.T) {
	for _, approvedArtifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("approved=%t", approvedArtifact), func(t *testing.T) {
			d, _ := startNativeProviderDriver(t, "svn")
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
			d.confirmRoute(routeDecisionForGates([]string{"whitebox", "quality"}))
			d.drive()
			dev, di := d.action("engine.development.batch.v3")
			if err := os.WriteFile(filepath.Join(di.Workspace.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) > 1 }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: batchEvidence(di)})
			d.drive()
			design, in := d.action("engine.qa.design.v2")
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

			if approvedArtifact {
				approved := approvedReview(manifest)
				d.business(review, protocol.ValidationResult{Binding: before.Binding, Status: "PASS", Review: &approved})
			}
			var choices []protocol.RequirementWorkDecision
			for _, work := range d.run.RequirementWork {
				disposition := protocol.RequirementWorkCarry
				if work.ActionID == dev || work.ActionID == design || work.ActionID == review {
					disposition = protocol.RequirementWorkRedo
				}
				choices = append(choices, protocol.RequirementWorkDecision{Work: work, Disposition: disposition, Reason: "Rebuild implementation and its retained test responsibility against the confirmed document"})
			}
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			impact := &protocol.RequirementImpact{ChangedItems: []string{"document formatting"}, TransitiveImpacts: []string{"implementation and test source rebinding"}, CarryReasons: []string{"public valid/invalid input meaning remains the same"}}
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:source-rebinding", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: choices, Impact: impact}
			event, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(event)
			ask := d.run.PendingRequirementAsk
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: choices, Impact: impact, Reason: "Confirm each retained and rebuilt responsibility"}}})
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			d.drive()
			nextDev, newInput := d.action("engine.development.batch.v3")
			devEntry, statErr := os.ReadFile(filepath.Join(newInput.Workspace.Path, "app_test.go"))
			if err := os.WriteFile(filepath.Join(newInput.Workspace.Path, "app.go"), []byte("package command\nfunc Valid(s string) bool { return len(s) >= 2 }\n"), 0644); err != nil {
				t.Fatal(err)
			}
			d.business(nextDev, protocol.ValidationResult{Binding: newInput.Binding, Status: "PASS", Development: batchEvidence(newInput)})
			d.drive()
			_, after := d.action("engine.qa.design.v2")
			actual, materialErr := os.ReadFile(filepath.Join(after.Workspace.Path, "app_test.go"))
			facts := map[string]any{"approvedArtifact": approvedArtifact, "oldDevelopment": before.ReviewBase, "oldArtifact": before.ReviewArtifact, "oldWorkspace": before.Workspace, "newDevelopment": d.run.Validation.DevelopmentSnapshot, "newDevelopmentInput": newInput, "newDeveloperTestBody": string(devEntry), "newDeveloperTestError": fmt.Sprint(statErr), "newAuthoring": after, "newAuthoringTestBody": string(actual), "newAuthoringTestError": fmt.Sprint(materialErr), "confirmedRequirementChoices": choices, "rejected": d.run.RejectedResults}
			if evidence := os.Getenv("NATIVE_VCS_EVIDENCE"); evidence != "" {
				data, err := json.MarshalIndent(facts, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(d.f.root), "whitebox-material-facts.json"), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("old workspace %s; new developer %s (test error %v); new QA %s (test error %v); retained cases=%d", before.Workspace.Path, newInput.Workspace.Path, statErr, after.Workspace.Path, materialErr, len(after.Manifest.Cases))
			if after.Workspace.Source != d.run.Validation.DevelopmentSnapshot {
				t.Fatal("new author is not bound to the actual current development")
			}
			if materialErr != nil || string(actual) != body {
				t.Fatal("confirmed development redo lost retained actual whitebox material", materialErr)
			}
			production, err := os.ReadFile(filepath.Join(after.Workspace.Path, "app.go"))
			if err != nil || string(production) != "package command\nfunc Valid(s string) bool { return len(s) >= 2 }\n" {
				t.Fatal("authoring material assembly restored obsolete production", string(production), err)
			}
			if after.ReviewBase != d.run.Validation.DevelopmentSnapshot {
				t.Fatal("authoring terminal facts lack their actual development baseline", after.ReviewBase)
			}
			native := d.f.vcs.At(after.Workspace.Path, after.Workspace.Handle)
			paths, err := native.ChangedPathsFrom(after.ReviewBase)
			if err != nil || len(paths) != 1 || paths[0] != "app_test.go" {
				t.Fatal("prepared source does not expose its complete actual test diff", paths, err)
			}
			originalInput := after
			d.reopen()
			_, restored := d.action("engine.qa.design.v2")
			if !reflect.DeepEqual(originalInput, restored) {
				t.Fatal("reopened authoring changed its original source binding", restored)
			}
		})
	}
}
