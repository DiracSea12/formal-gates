package facade

import (
	"crypto/sha256"
	"encoding/json"
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

			d := newValidationDriver(t, "whitebox", "quality")
			trees := map[string]map[string]string{}
			testBytes := "package app\nimport \"testing\"\nfunc sharedInput() string { return \"\" }\nfunc TestPositive(t *testing.T) { if !Validate(sharedInput()) { t.Fatal(\"valid input rejected\") } }\nfunc TestNegative(t *testing.T) { if Validate(sharedInput()) { t.Fatal(\"invalid input accepted\") } }\n"
			fixtureCount := 0
			materialize := func() {
				var intent protocol.HostActionIntent
				for _, value := range d.run.AdapterIntents {
					intent = value
				}
				if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
					t.Fatal("missing typed candidate operation")
				}
				data, _ := json.Marshal(intent.Adapter.Params)
				var params protocol.CandidateParameters
				if err := json.Unmarshal(data, &params); err != nil {
					t.Fatal(err)
				}
				tree := map[string]string{}
				if params.Base != "" {
					if trees[params.Base] == nil {
						t.Fatalf("unknown requested base %s", params.Base)
					}
				}
				for path, value := range trees[params.Base] {
					tree[path] = value
				}
				switch params.Stage {
				case "DEVELOPMENT":
					tree["app.go"] = "package app\nfunc Validate(input string) bool { return input != \"\" }\n"
				case "AUTHORING":
				case "QA_ARTIFACT":
					if len(params.Paths) != 1 || params.Paths[0] != "app_test.go" {
						t.Fatalf("unexpected declared QA paths %v", params.Paths)
					}
					tree["app_test.go"] = testBytes
				default:
					t.Fatalf("unexpected stage %s", params.Stage)
				}
				if params.Stage == "QA_ARTIFACT" {
					dir := t.TempDir()
					for path, content := range tree {
						if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module app\ngo 1.24\n"), 0600); err != nil {
						t.Fatal(err)
					}
					cmd := exec.Command("go", "test", "-run", "^TestNegative$", "-count=1")
					cmd.Dir = dir
					output, err := cmd.CombinedOutput()
					t.Logf("negative test on QA artifact %d: error=%v output=%s", fixtureCount, err, output)
					if fixtureCount == 0 && err != nil || fixtureCount == 1 && ((changed && err == nil) || (!changed && err != nil)) {
						t.Fatal("fixture did not change actual negative-test behavior")
					}
					fixtureCount++
				}
				bytes, _ := json.Marshal(tree)
				sum := sha256.Sum256(bytes)
				identity := fmt.Sprintf("candidate-%d", len(trees)+1)
				trees[identity] = tree
				evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: fmt.Sprintf("sha256:%x", sum), Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
				if params.Stage == "QA_ARTIFACT" && tree["app.go"] != trees[params.Base]["app.go"] {
					t.Fatal("production bytes changed")
				}
				data, _ = json.Marshal(evidence)
				values := map[string]any{}
				_ = json.Unmarshal(data, &values)
				event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(event)
				d.drive()
			}
			id, input := d.action("engine.development")
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "input validation", Build: []string{"go build passed"}, Tests: []string{"input rejection passed"}}})
			materialize()
			materialize()
			id, input = d.action("engine.qa.design")
			m := input.Manifest
			m.Cases = []coverage.AcceptanceCase{{CaseID: "positive", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go#TestPositive", Oracle: "valid input accepted"}, {CaseID: "negative", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go#TestNegative", Oracle: "invalid input rejected"}}
			for _, c := range m.Cases {
				m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
			}
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
			materialize()
			id, input = d.action("engine.qa.review")
			oldCandidate := input.Binding.Candidate
			r := approvedReview(m)
			r.SetStatus = coverage.StatusFail
			r.SourceDecisions[0].Status = coverage.StatusFail
			r.PointDecisions[0].Status = coverage.StatusFail
			r.CaseDecisions[0].Status = coverage.StatusFail
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Review: &r})
			d.drive()
			if changed {
				testBytes = "package app\nimport \"testing\"\nfunc sharedInput() string { return \"two\" }\nfunc TestPositive(t *testing.T) { if !Validate(sharedInput()) { t.Fatal(\"valid input rejected\") } }\nfunc TestNegative(t *testing.T) { if Validate(sharedInput()) { t.Fatal(\"invalid input accepted\") } }\n"
			}
			id, input = d.action("engine.qa.design")
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "Changed shared setup in app_test.go used by both tests"}})
			materialize()
			id, input = d.action("engine.qa.review")
			if (trees[oldCandidate]["app_test.go"] == trees[input.Binding.Candidate]["app_test.go"]) == changed || trees[oldCandidate]["app.go"] != trees[input.Binding.Candidate]["app.go"] {
				t.Fatal("fixture did not isolate a real test-only shared-helper content change")
			}
			t.Logf("test content before=%q after=%q", trees[oldCandidate]["app_test.go"], trees[input.Binding.Candidate]["app_test.go"])
			t.Logf("old candidate=%s new=%s inheritable=%v modified=%v", oldCandidate, input.Binding.Candidate, input.InheritableCases, d.run.Validation.Sets[coverage.ReviewWhitebox].Paths)
			r = approvedReview(m)
			r.CaseDecisions[1].Inherited = true
			if !changed {
				r.SetStatus = coverage.StatusFail
				r.SourceDecisions[0].Status = coverage.StatusFail
				r.PointDecisions[0].Status = coverage.StatusFail
				r.CaseDecisions[0].Status = coverage.StatusFail
			}
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "new-review", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r}}})
			t.Logf("inherited review=%s final status=%s", a.Status, d.run.Validation.Sets[coverage.ReviewWhitebox].Status)
			if changed {
				if a.Status != "REJECTED_INVALID_RESULT" {
					t.Fatal("changed test implementation inherited stale approval")
				}
				d.drive()
				id, input = d.action("engine.qa.review")
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
