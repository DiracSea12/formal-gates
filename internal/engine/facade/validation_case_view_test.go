package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Review inheritance consumes both original complete dependency views.
func TestWhiteboxReuseRequiresCompleteCaseViews(t *testing.T) {
	for _, variant := range []string{"unchanged", "held", "missing-proof", "baseline-missing", "shared-input-changed", "config-changed", "different-test-ref"} {
		t.Run(variant, func(t *testing.T) {
			d := candidateSourceDriver(t, "whitebox")
			trees := map[string]map[string]string{}
			reviewIntents := map[string]protocol.HostActionIntent{}
			changed := false
			negativeRef := "negative_test.go::TestNegative"
			observe := func(intent protocol.HostActionIntent) protocol.Event {
				t.Helper()
				raw, _ := json.Marshal(intent.Adapter.Params)
				var params protocol.CandidateParameters
				json.Unmarshal(raw, &params)
				tree := map[string]string{}
				if trees[params.Base] == nil {
					trees[params.Base] = candidateSourceTree(t, d, params.Base)
				}
				for p, b := range trees[params.Base] {
					tree[p] = b
				}
				stage := params.Stage
				if stage == "CHECK_SOURCE" {
					stage, _ = candidateEditSource(t, d, params)
				}
				switch stage {
				case "DEVELOPMENT":
					tree["app.go"] = "package app\nfunc Positive() bool { return true }; func Negative() bool { return false }\n"
				case "AUTHORING":
				case "QA_ARTIFACT":
					tree["positive_test.go"] = "package app\nimport \"testing\"\nfunc TestPositive(t *testing.T) { if !FixtureReady() { t.Fatal(\"fixture\") }; if !Positive() { t.Fatal(\"positive rejected\") } }\n"
					if changed {
						tree["positive_test.go"] = "package app\nimport \"testing\"\nfunc TestPositive(t *testing.T) { if !FixtureReady() { t.Fatal(\"fixture\") }; got := Positive(); if got != true { t.Fatalf(\"got %v\",got) } }\n"
					}
					tree["negative_test.go"] = "package app\nimport \"testing\"\nfunc TestNegative(t *testing.T) { if !FixtureReady() { t.Fatal(\"fixture\") }; if Negative() { t.Fatal(\"negative accepted\") } }\nfunc TestNegativeAlternate(t *testing.T) { TestNegative(t) }\n"
				case "REVIEW_READY":
					for _, artifact := range params.QAArtifacts {
						for _, path := range artifact.Paths {
							if content, ok := trees[artifact.Identity][path]; ok {
								tree[path] = content
							} else {
								delete(tree, path)
							}
						}
					}
				default:
					t.Fatalf("unexpected candidate stage %s", params.Stage)
				}
				if stage == "QA_ARTIFACT" {
					tree["fixture_test.go"] = "package app\nfunc FixtureReady() bool { return true }\n"
					if changed && variant == "shared-input-changed" {
						tree["fixture_test.go"] = "package app\nfunc FixtureReady() bool { ready := true; return ready }\n"
					}
				}
				tree["go.mod"] = "module app\ngo 1.24\n"
				if changed && variant == "config-changed" {
					tree["go.mod"] = "module app\ngo 1.25\n"
				}
				identity, digest := freezeCandidateSource(t, d, params, tree)
				trees[identity] = tree
				folder := t.TempDir()
				os.MkdirAll(folder, 0700)
				os.WriteFile(filepath.Join(folder, "go.mod"), []byte("module app\ngo 1.24\n"), 0600)
				for p, b := range tree {
					os.WriteFile(filepath.Join(folder, p), []byte(b), 0600)
				}
				proof := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: digest, WorkspaceID: params.WorkspaceID, Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
				proof.GateView = gateReviewView(params)
				raw, _ = json.Marshal(proof)
				var values map[string]any
				json.Unmarshal(raw, &values)
				if intent.Adapter.Operation != "op.candidate" {
					t.Fatal("wrong adapter method")
				}
				if params.Stage == "QA_ARTIFACT" || params.Stage == "REVIEW_READY" {
					if params.DesignTasks[coverage.ReviewWhitebox] != d.run.Validation.Sets[coverage.ReviewWhitebox].DesignTask {
						t.Fatal("issued intent has wrong design source")
					}
					if params.Stage == "QA_ARTIFACT" {
						reviewIntents[intent.ActionID] = intent
					}
					rawParams, _ := json.Marshal(intent.Adapter.Params)
					var shape map[string]any
					if err := json.Unmarshal(rawParams, &shape); err != nil {
						t.Fatal(err)
					}
					targets, ok := shape["reviewCases"].(map[string]any)
					if !ok || len(targets) != 2 || targets["positive"] != "positive_test.go::TestPositive" || targets["negative"] != negativeRef {
						t.Fatalf("signed review targets incomplete: %v", shape["reviewCases"])
					}
					if variant != "missing-proof" && !(variant == "baseline-missing" && !changed) && negativeRef == "negative_test.go::TestNegative" {
						views := map[string]any{}
						for caseID, ref := range targets {
							inputs := map[string]string{}
							for _, path := range []string{"app.go", caseID + "_test.go", "fixture_test.go", "go.mod"} {
								content, ok := tree[path]
								if !ok {
									t.Fatalf("missing actual dependency %s", path)
								}
								inputs[path] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
							}
							views[caseID] = map[string]any{"testRef": ref, "inputs": inputs}
						}
						values["reviewViews"] = views
					}
				}
				ev, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, "EXECUTED", "", values)
				if err != nil {
					t.Fatal(err)
				}

				if params.Stage == "QA_ARTIFACT" || params.Stage == "REVIEW_READY" {
					cmd := exec.Command("go", "test", "-timeout=0", "-count=1", "./...")
					cmd.Dir = folder
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("native candidate tests: %v\n%s", err, output)
					}
				}
				return ev
			}
			d.fixtureCandidateObserver = func(intent protocol.HostActionIntent, _ protocol.CandidateParameters) protocol.Event {
				return observe(intent)
			}
			materialize := func() {
				d.drive()
				intent := protocol.HostActionIntent{}
				for _, candidate := range d.run.AdapterIntents {
					intent = candidate
				}
				ev := observe(intent)
				values := ev.HostAction.AdapterEvidence.Values
				var request protocol.Event
				if variant == "held" {
					request = holdRequirement(t, d)
				}
				d.submit(ev)
				if variant == "held" {
					d.reopen()
					releaseRequirement(t, d, request)
				}
				d.drive()
				before := d.run.Revision
				d.submit(ev)
				d.reopen()
				d.drive()
				if before != d.run.Revision {
					t.Fatal("candidate completion replay repeated business work")
				}
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				rawStored, _ := json.Marshal(snap.State.HostActionReceipts[intent.ActionID].AdapterEvidence.Values)
				rawSent, _ := json.Marshal(values)
				if string(rawStored) != string(rawSent) {
					t.Fatal("raw candidate evidence was changed")
				}

			}
			id, in := d.action("engine.development.batch.v6")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "two independent predicates"}})
			materialize()
			materialize()
			id, in = d.action("engine.qa.design.v6")
			m := in.Manifest
			m.Cases = []coverage.AcceptanceCase{{CaseID: "positive", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "positive_test.go::TestPositive", Oracle: "positive true"}, {CaseID: "negative", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "negative_test.go::TestNegative", Oracle: "negative false"}}
			for _, c := range m.Cases {
				m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
			}
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"positive_test.go", "negative_test.go", "fixture_test.go"}}})
			materialize()
			id, in = d.action("engine.qa.review.rationale.v2")
			old := in.Binding.Candidate
			r := approvedReview(m)
			r.SetStatus = coverage.StatusFail
			r.CaseDecisions[0].Status = coverage.StatusFail
			r.PointDecisions[0].Status = coverage.StatusFail
			r.SourceDecisions[0].Status = coverage.StatusFail
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "FAIL", Review: &r})
			changed = true
			id, in = d.action("engine.qa.design.v6")
			m.Cases[0].Oracle = "positive equals true with actual value reported"
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"positive_test.go", "negative_test.go", "fixture_test.go"}}})
			materialize()
			id, in = d.action("engine.qa.review.rationale.v2")
			now := in.Binding.Candidate
			if trees[old]["negative_test.go"] != trees[now]["negative_test.go"] || trees[old]["app.go"] != trees[now]["app.go"] {
				t.Fatal("negative control was affected")
			}
			r = approvedReview(m)
			r.Scope = coverage.ScopeAffected
			r.CaseDecisions[1].Inherited = true
			d.receipt(id)
			a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "incremental review", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r}}})
			s, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("negative test unchanged with independent production function; old=%s current=%s inheritable=%v acceptance=%s", old, now, in.InheritableReviews, a.Status)
			if variant != "unchanged" && variant != "held" && variant != "different-test-ref" {
				if a.Status != "REJECTED_INVALID_RESULT" {
					t.Fatal("missing or changed dependency proof authorized inheritance")
				}
				if len(d.run.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory) != 1 {
					t.Fatal("invalid inheritance entered semantic history")
				}
				rejected := s.State.RejectedResults[id]
				if rejected.Payload.Validation == nil || !rejected.Payload.Validation.Review.CaseDecisions[1].Inherited {
					t.Fatal("raw rejected inheritance lost")
				}
				return
			}
			if a.Status != "ACCEPTED" {
				t.Fatal("unmodified independent whitebox case could not retain prior approval")
			}
			set := d.run.Validation.Sets[coverage.ReviewWhitebox]
			if set.Status != "FROZEN" || len(set.ReviewHistory) != 2 || !set.ReviewHistory[1].Review.CaseDecisions[1].Inherited || set.ReviewHistory[1].Review.CaseDecisions[0].Inherited {
				t.Fatal("precise review history not retained")
			}
			for _, candidate := range []string{old, now} {
				found := false
				for action, intent := range reviewIntents {
					receipt, recorded := s.State.HostActionReceipts[action]
					if !recorded || receipt.ActionID != action || receipt.PayloadDigest != intent.PayloadDigest || receipt.Correlation != intent.Correlation || receipt.AdapterOperation != intent.Adapter.Operation {
						t.Fatal("original QA candidate receipt binding was lost")
					}
					if receipt.AdapterEvidence == nil || receipt.AdapterEvidence.Values["identity"] != candidate {
						continue
					}
					raw, _ := json.Marshal(receipt.AdapterEvidence.Values["reviewViews"])
					var views map[string]struct {
						TestRef string            `json:"testRef"`
						Inputs  map[string]string `json:"inputs"`
					}
					if err := json.Unmarshal(raw, &views); err != nil {
						t.Fatal(err)
					}
					v := views["negative"]
					if v.TestRef != "negative_test.go::TestNegative" || len(v.Inputs) != 4 {
						t.Fatal("original dependency view was lost")
					}
					inputs := map[string]string{}
					for _, path := range []string{"app.go", "negative_test.go", "fixture_test.go", "go.mod"} {
						content, exists := trees[candidate][path]
						if !exists {
							t.Fatalf("original dependency bytes missing: %s", path)
						}
						inputs[path] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content)))
					}
					if !maps.Equal(v.Inputs, inputs) {
						t.Fatal("stored view differs from original dependency paths and bytes")
					}
					found = true
				}
				if !found {
					t.Fatal("original candidate proof absent")
				}
			}
			if variant == "different-test-ref" {
				// Reuse both real native trees under a different test target, without proof for that target.
				materialize()
				negativeRef = "negative_test.go::TestNegativeAlternate"
				for _, affected := range []string{"negative", "positive"} {
					d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Category: "CASE_EXECUTABILITY", Mode: coverage.ReviewWhitebox, AffectedCases: []string{affected}}, "")
					design, di := d.action("engine.qa.design.v6")
					m = di.Manifest
					m.Cases[1].TestRef = negativeRef
					if affected == "positive" {
						changed = false
						m.Cases[0].Oracle = "positive true"
					}
					d.business(design, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"positive_test.go", "negative_test.go", "fixture_test.go"}}})
					materialize()
					review, ri := d.action("engine.qa.review.rationale.v2")
					verdict := approvedReview(ri.Manifest)
					verdict.Scope = coverage.ScopeAffected
					if affected == "positive" {
						verdict.CaseDecisions[1].Inherited = true
						d.receipt(review)
						accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: review, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "unproved-alternate-test", Validation: &protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &verdict}}})
						if accepted.Status != "REJECTED_INVALID_RESULT" {
							t.Fatalf("views for another signed TestRef granted approval: %s", accepted.Status)
						}
					} else {
						d.business(review, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &verdict})
						materialize()
					}
				}
				return
			}
			materialize()
			qa, qi := d.action("engine.qa.execution.v4")
			execution := executionFor(qi)
			d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &execution}))
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("approved candidate did not complete full validation")
			}
			revision := d.run.Revision
			d.reopen()
			d.drive()
			if revision != d.run.Revision {
				t.Fatal("recovery repeated validation")
			}
		})
	}
}
