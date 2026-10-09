package facade

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Byte-sensitive candidate fixtures retain real source snapshots while the
// semantic driver supplies the independently signed operation observations.
func candidateSourceDriver(t *testing.T, gates ...string) *validationDriver {
	t.Helper()
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	d := plannedValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil, commandPlan(), gates...)
	d.mockNative = true
	return d
}

func candidateSourceTree(t *testing.T, d *validationDriver, base string) map[string]string {
	t.Helper()
	entries, err := d.git.TreeEntries(base)
	if err != nil {
		t.Fatal(err)
	}
	tree := map[string]string{}
	for path := range entries {
		body, err := (vcs.ExecRunner{}).RunRaw(d.f.root, "git", nil, "show", base+":"+path)
		if err != nil {
			t.Fatal(err)
		}
		tree[path] = string(body)
	}
	return tree
}

func freezeCandidateSource(t *testing.T, d *validationDriver, p protocol.CandidateParameters, tree map[string]string) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "candidate")
	if err := d.git.CreateWorkspace(dir, p.Base, "candidate fixture source", vcs.WorkspaceHandle{}); err != nil {
		t.Fatal(err)
	}
	for path := range candidateSourceTree(t, d, p.Base) {
		if _, retained := tree[path]; !retained {
			if err := os.Remove(filepath.Join(dir, path)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for path, body := range tree {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	native := vcs.New(dir, nil)
	paths, err := native.ChangedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := native.Track(paths); err != nil {
		t.Fatal(err)
	}
	if len(paths) > 0 {
		if err := native.Commit("candidate fixture source"); err != nil {
			t.Fatal(err)
		}
	}
	identity, err := native.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := native.TreeDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidateSourceTree(t, d, identity), tree) {
		t.Fatal("immutable candidate differs from its declared source bytes")
	}
	return identity, digest
}

// Identical manifests can name different test implementations. A completed
// candidate belongs to the accepted design that supplied its actual test bytes.
func TestCandidateCompletionRetainsDesignSource(t *testing.T) {
	for _, stage := range []string{"QA_ARTIFACT", "REVIEW_READY"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/held=%t", stage, held), func(t *testing.T) {
				d := candidateSourceDriver(t, "whitebox", "quality")
				trees := map[string]map[string]string{}
				testBytes := "func fixture() string { return uninitializedInput }"
				sideEffects := 0
				currentIntent := func() protocol.HostActionIntent {
					t.Helper()
					if len(d.run.AdapterIntents) != 1 {
						t.Fatalf("want one candidate intent: %+v", d.run.AdapterIntents)
					}
					for _, intent := range d.run.AdapterIntents {
						return intent
					}
					panic("unreachable")
				}
				observe := func(intent protocol.HostActionIntent) protocol.Event {
					t.Helper()
					if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
						t.Fatal("unexpected external method")
					}
					raw, _ := json.Marshal(intent.Adapter.Params)
					var params protocol.CandidateParameters
					if err := json.Unmarshal(raw, &params); err != nil {
						t.Fatal(err)
					}
					tree := map[string]string{}
					if trees[params.Base] == nil {
						trees[params.Base] = candidateSourceTree(t, d, params.Base)
					}
					for p, b := range trees[params.Base] {
						tree[p] = b
					}
					stage := params.Stage
					if stage == "CHECK_SOURCE" {
						stage = "QA_ARTIFACT"
						if reflect.DeepEqual(params.Paths, []string{"app.go"}) {
							stage = "DEVELOPMENT"
						}
					}
					switch stage {
					case "DEVELOPMENT":
						tree["app.go"] = "func valid(input string) bool { return input != \"\" }"
					case "AUTHORING":
					case "QA_ARTIFACT":
						if !reflect.DeepEqual(params.Paths, []string{"app_test.go"}) {
							t.Fatal("wrong test paths")
						}
						tree["app_test.go"] = testBytes
					case "REVIEW_READY":
						if !params.Finalize {
							t.Fatal("final commit organization omitted")
						}
						for _, artifact := range params.QAArtifacts {
							if trees[artifact.Identity] == nil {
								t.Fatal("unknown QA source")
							}
							for _, p := range artifact.Paths {
								if b, ok := trees[artifact.Identity][p]; ok {
									tree[p] = b
								} else {
									delete(tree, p)
								}
							}
						}
					default:
						t.Fatal("unknown candidate stage")
					}
					if params.Stage == "QA_ARTIFACT" || params.Stage == "REVIEW_READY" {
						if params.DesignTasks[coverage.ReviewWhitebox] != d.run.Validation.Sets[coverage.ReviewWhitebox].DesignTask {
							t.Fatal("issued intent has wrong design source")
						}
						if tree["app.go"] != trees[params.Base]["app.go"] {
							t.Fatal("test materialization changed production content")
						}
					}
					identity, digest := freezeCandidateSource(t, d, params, tree)
					trees[identity] = tree
					if params.Stage != "CHECK_SOURCE" {
						sideEffects++
					}
					evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: digest, WorkspaceID: params.WorkspaceID, Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
					evidence.GateView = gateReviewView(params)
					raw, _ = json.Marshal(evidence)
					var values map[string]any
					_ = json.Unmarshal(raw, &values)
					ev, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, "EXECUTED", "", values)
					if err != nil {
						t.Fatal(err)
					}
					return ev
				}
				d.fixtureCandidateObserver = func(intent protocol.HostActionIntent, _ protocol.CandidateParameters) protocol.Event {
					return observe(intent)
				}
				materialize := func() { t.Helper(); d.submit(observe(currentIntent())); d.drive() }
				id, in := d.action("engine.development.batch")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input contract"}})
				materialize()
				materialize()
				id, in = d.action("engine.qa.design")
				manifest := in.Manifest
				manifest.Cases = []coverage.AcceptanceCase{{CaseID: "normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestNormal", Oracle: "accept valid input"}, {CaseID: "invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "reject invalid input"}}
				for _, c := range manifest.Cases {
					manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
				}
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "test fixture implementation"}})
				originalDesign := d.run.Validation.Sets[coverage.ReviewWhitebox].DesignTask
				if stage == "REVIEW_READY" {
					materialize()
					id, in = d.action("engine.qa.review.rationale")
					r := approvedReview(in.Manifest)
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
					d.drive()
				}
				old := currentIntent()
				if old.Adapter.Params["stage"] != stage {
					t.Fatal("wrong in-flight stage")
				}
				oldReceipt := observe(old)
				oldIdentity := oldReceipt.HostAction.AdapterEvidence.Values["identity"].(string)
				d.chooseValidation(protocol.ValidationRequest{Kind: "QA_ARTIFACT_REPAIR", Mode: coverage.ReviewWhitebox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal", "invalid"}}, "")
				testBytes = "func fixture() string { return initializedInput() }"
				id, in = d.action("engine.qa.design")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &manifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "initialize shared fixture without changing TestRef or oracle"}})
				current := d.run.Validation.Sets[coverage.ReviewWhitebox]
				if current.DesignTask == originalDesign || !reflect.DeepEqual(current.Manifest, manifest) {
					t.Fatal("setup must change only design source and implementation")
				}
				if held {
					request, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
					if err != nil {
						t.Fatal(err)
					}
					d.submit(request)
				}
				if stage == "QA_ARTIFACT" {
					badValues := maps.Clone(oldReceipt.HostAction.AdapterEvidence.Values)
					badValues["workspaceId"] = "different-workspace"
					bad, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), old.ActionID, "op.candidate", DefaultProvider, old.Correlation, old.PayloadDigest, "EXECUTED", "", badValues)
					if err != nil {
						t.Fatal(err)
					}
					revision := d.run.Revision
					if _, _, err := d.f.Submit(bad); err == nil {
						t.Fatal("receipt for a different workspace was accepted")
					}
					d.reopen()
					if d.run.Revision != revision {
						t.Fatal("invalid original evidence changed state")
					}
				}
				d.submit(oldReceipt)
				d.submit(oldReceipt)
				d.reopen()
				d.drive()
				if held {
					if _, ok := d.run.AdapterIntents[old.ActionID]; !ok {
						t.Fatal("barrier prematurely settled external fact")
					}
					ask := d.run.PendingRequirementAsk
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "abort"}})
				}
				d.reopen()
				d.drive()
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				recorded := snapshot.State.HostActionReceipts[old.ActionID]
				if recorded.AdapterEvidence == nil || !reflect.DeepEqual(recorded.AdapterEvidence.Values, oldReceipt.HostAction.AdapterEvidence.Values) || recorded.PayloadDigest != old.PayloadDigest {
					t.Fatal("original receipt content changed")
				}
				if d.run.Validation.Candidate != nil || d.run.Validation.Sets[coverage.ReviewWhitebox].Candidate != "" || len(d.run.Validation.Waves) != 0 {
					t.Fatal("obsolete test artifact was promoted")
				}
				replacement := currentIntent()
				if replacement.ActionID == old.ActionID || replacement.Adapter.Params["stage"] != "QA_ARTIFACT" {
					t.Fatal("obsolete intent not replaced exactly once")
				}
				before := sideEffects
				d.submit(oldReceipt)
				d.reopen()
				d.drive()
				if currentIntent().ActionID != replacement.ActionID || sideEffects != before {
					t.Fatal("duplicate receipt repeated external work")
				}
				materialize()
				id, in = d.action("engine.qa.review.rationale")
				if in.Binding.Candidate == oldIdentity || trees[in.Binding.Candidate]["app_test.go"] != testBytes {
					t.Fatal("review uses old test implementation")
				}
				r := approvedReview(in.Manifest)
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
				d.drive()
				materialize()
				d.reopen()
				candidate := d.run.Validation.Candidate
				if candidate == nil || trees[candidate.Identity]["app_test.go"] != testBytes {
					t.Fatal("final candidate lost approved test bytes")
				}
				id, in = d.action("engine.qa.execution")
				execution := executionFor(in)
				d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &execution}))
				id, in = d.gateAction("quality")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
				d.reopen()
				d.drive()
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("replacement candidate cannot complete")
				}
				want := 5
				if stage == "REVIEW_READY" {
					want = 6
				}
				if sideEffects != want {
					t.Fatalf("external effects=%d want=%d", sideEffects, want)
				}
			})
		}
	}
}
