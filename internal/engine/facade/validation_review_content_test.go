package facade

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// Actual test bytes and execution establish the unchanged/changed content controls.
func TestWhiteboxApprovalInheritanceUsesCandidateContent(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("content-changed-%v", changed), func(t *testing.T) {

			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := nativeMasterAt(t, root, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
			d.confirmRoute(routeDecisionForGates([]string{"whitebox", "quality"}))
			d.drive()
			testBytes := "package main\nimport \"testing\"\nfunc sharedInput() string { return \"\" }\nfunc TestPositive(t *testing.T) { if !Validate(sharedInput()) { t.Fatal(\"valid input rejected\") } }\nfunc TestNegative(t *testing.T) { if Validate(sharedInput()) { t.Fatal(\"invalid input accepted\") } }\n"
			read := func(identity, path string) string {
				cmd := exec.Command("git", "-C", root, "show", identity+":"+path)
				content, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				return string(content)
			}
			negative := func(input protocol.ValidationInput, shouldFail bool) {
				cmd := exec.Command("go", "test", "-timeout=0", "-run", "^TestNegative$", "-count=1", ".")
				cmd.Dir = input.Workspace.Path
				output, err := cmd.CombinedOutput()
				t.Logf("negative test on actual candidate %s: error=%v output=%s", input.Binding.Candidate, err, output)
				if (err != nil) != shouldFail {
					t.Fatal("fixture did not establish the original negative-test behavior")
				}
			}
			id, input := d.action("engine.development.batch.v4")
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() {}\nfunc Validate(input string) bool { return input != \"\" }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input validation"}})
			d.materialize()
			d.materialize()
			id, input = d.action("engine.qa.design.v5")
			m := input.Manifest
			m.Cases = []coverage.AcceptanceCase{{CaseID: "positive", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestPositive", Oracle: "valid input accepted", Files: []string{"app_test.go"}}, {CaseID: "negative", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestNegative", Oracle: "invalid input rejected", Files: []string{"app_test.go"}}}
			for _, c := range m.Cases {
				m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
			}
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app_test.go"), []byte(testBytes), 0600); err != nil {
				t.Fatal(err)
			}
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
			d.materialize()
			id, input = d.action("engine.qa.review.rationale.v2")
			oldCandidate := input.Binding.Candidate
			negative(input, false)
			r := approvedReview(m)
			r.SetStatus = coverage.StatusFail
			r.SourceDecisions[0].Status = coverage.StatusFail
			r.PointDecisions[0].Status = coverage.StatusFail
			r.CaseDecisions[0].Status = coverage.StatusFail
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Review: &r})
			d.drive()
			if changed {
				testBytes = "package main\nimport \"testing\"\nfunc sharedInput() string { return \"two\" }\nfunc TestPositive(t *testing.T) { if !Validate(sharedInput()) { t.Fatal(\"valid input rejected\") } }\nfunc TestNegative(t *testing.T) { if Validate(sharedInput()) { t.Fatal(\"invalid input accepted\") } }\n"
			}
			id, input = d.action("engine.qa.design.v5")
			if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app_test.go"), []byte(testBytes), 0600); err != nil {
				t.Fatal(err)
			}
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "Changed shared setup in app_test.go used by both tests"}})
			d.materialize()
			id, input = d.action("engine.qa.review.rationale.v2")
			if (read(oldCandidate, "app_test.go") == read(input.Binding.Candidate, "app_test.go")) == changed || read(oldCandidate, "app.go") != read(input.Binding.Candidate, "app.go") {
				t.Fatal("fixture did not isolate a real test-only shared-helper content change")
			}
			negative(input, changed)
			t.Logf("test content before=%q after=%q", read(oldCandidate, "app_test.go"), read(input.Binding.Candidate, "app_test.go"))
			t.Logf("old candidate=%s new=%s inheritable=%v modified=%v", oldCandidate, input.Binding.Candidate, input.InheritableReviews, d.run.Validation.Sets[coverage.ReviewWhitebox].Paths)
			r = approvedReview(m)
			r.CaseDecisions[1].Inherited = true
			if !changed {
				r.SetStatus = coverage.StatusFail
				r.SourceDecisions[0].Status = coverage.StatusFail
				r.PointDecisions[0].Status = coverage.StatusFail
				r.CaseDecisions[0].Status = coverage.StatusFail
			}
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: d.host(), Outcome: protocol.OutcomePass, PayloadDigest: "new-review", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r}}})
			t.Logf("inherited review=%s final status=%s", a.Status, d.run.Validation.Sets[coverage.ReviewWhitebox].Status)
			if changed {
				if a.Status != "REJECTED_INVALID_RESULT" {
					t.Fatal("changed test implementation inherited stale approval")
				}
				d.drive()
				id, input = d.action("engine.qa.review.rationale.v2")
				fresh := approvedReview(m)
				fresh.SetStatus = coverage.StatusFail
				fresh.SourceDecisions[0].Status = coverage.StatusFail
				fresh.PointDecisions[0].Status = coverage.StatusFail
				fresh.CaseDecisions[1].Status = coverage.StatusFail
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Review: &fresh})
			} else if a.Status != "ACCEPTED" {
				t.Fatal("identical content under a different immutable identity lost legal approval inheritance")
			}
			set := d.run.Validation.Sets[coverage.ReviewWhitebox]
			if len(set.ReviewHistory) != 2 || set.ReviewHistory[0].Candidate != oldCandidate {
				t.Fatal("review history lost its candidate binding or counted invalid evidence")
			}

		})
	}
}
