package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"slices"
	"testing"
)

// TestPrewaveReviewFollowsArtifactResponsibility keeps transport facts while
// requiring independent approval of each replacement test artifact.
func TestPrewaveReviewFollowsArtifactResponsibility(t *testing.T) {
	for _, kind := range []string{"THAW", "QA_ARTIFACT_REPAIR"} {
		for _, delivery := range []string{"receipted", "result-first", "terminal-first", "retry"} {
			t.Run(kind+"/"+delivery, func(t *testing.T) {
				d := candidateSourceDriver(t, "whitebox", "quality")
				trees := map[string]map[string]string{}
				testBytes := "func TestNormal() { initializedInput() }; func TestInvalid() { rejectEmptyInput() }"
				d.fixtureCandidateObserver = func(intent protocol.HostActionIntent, params protocol.CandidateParameters) protocol.Event {
					t.Helper()
					tree := map[string]string{}
					if trees[params.Base] == nil {
						trees[params.Base] = candidateSourceTree(t, d, params.Base)
					}
					for path, bytes := range trees[params.Base] {
						tree[path] = bytes
					}
					stage := params.Stage
					if stage == "CHECK_SOURCE" {
						stage = "QA_ARTIFACT"
						if slices.Contains(params.Paths, "app.go") {
							stage = "DEVELOPMENT"
						}
					}
					switch stage {
					case "DEVELOPMENT":
						tree["app.go"] = "func Valid(s string) bool { return s != empty }"
					case "AUTHORING":
					case "QA_ARTIFACT":
						if len(params.Paths) != 1 || params.Paths[0] != "app_test.go" {
							t.Fatal("wrong test path input")
						}
						tree["app_test.go"] = testBytes
					case "REVIEW_READY":
						for _, artifact := range params.QAArtifacts {
							if trees[artifact.Identity] == nil {
								t.Fatal("unknown immutable artifact")
							}
							for _, path := range artifact.Paths {
								if bytes, ok := trees[artifact.Identity][path]; ok {
									tree[path] = bytes
								} else {
									delete(tree, path)
								}
							}
						}
					default:
						t.Fatal("unknown candidate stage")
					}
					identity, digest := freezeCandidateSource(t, d, params, tree)
					trees[identity] = tree
					evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: digest, WorkspaceID: params.WorkspaceID, Paths: params.Paths, TestOnly: true, ProductionViewEqual: true}
					evidence.GateView = gateReviewView(params)
					raw, _ := json.Marshal(evidence)
					var values map[string]any
					json.Unmarshal(raw, &values)
					ev, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, "EXECUTED", "", values)
					if err != nil {
						t.Fatal(err)
					}
					return ev
				}
				materialize := func() {
					t.Helper()
					d.drive()
					if len(d.run.AdapterIntents) != 1 {
						t.Fatal("one candidate operation required")
					}
					d.materialize()
				}
				id, in := d.action("engine.development.batch.v4")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input validation"}})
				materialize()
				materialize()
				id, in = d.action("engine.qa.design.v3")
				m := in.Manifest
				m.Cases = []coverage.AcceptanceCase{{CaseID: "normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestNormal", Oracle: "valid input accepted"}, {CaseID: "invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input rejected"}}
				for _, c := range m.Cases {
					m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
				}
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
				materialize()
				oldID, old := d.action("engine.qa.review.rationale.v2")
				if delivery != "result-first" {
					d.receipt(oldID)
				}
				if delivery == "retry" {
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: oldID, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: authoring.FailureTransientEngine, PayloadDigest: "review-runtime-error"}})
					oldID, old = d.action("engine.qa.review.rationale.v2")
					d.receipt(oldID)
				}
				if delivery == "terminal-first" {
					attempt := d.run.PendingActions[oldID].AttemptID
					for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
						ev, err := protocol.NewTaskEvent(d.eventID(), old.Binding.Task, attempt, status)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(ev)
					}
				}
				t.Logf("original review action=%s task=%v candidate=%s", oldID, old.Binding.Task, old.Binding.Candidate)
				d.chooseValidation(protocol.ValidationRequest{Kind: kind, Mode: coverage.ReviewWhitebox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal", "invalid"}}, "")
				testBytes = "func TestNormal() { changedInputSetup() }; func TestInvalid() { rejectEmptyInput() }"
				id, in = d.action("engine.qa.design.v3")
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}, Summary: "initialize shared test fixture; same TestRef and oracle"}})
				materialize()
				now := d.run.Validation.Sets[coverage.ReviewWhitebox]
				t.Logf("replacement design=%v current candidate=%s; old candidate=%s", now.DesignTask, now.Candidate, old.Binding.Candidate)
				if now.Candidate == old.Binding.Candidate {
					t.Fatal("probe must use different immutable candidates")
				}
				r := approvedReview(old.Manifest)
				ev := protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: oldID, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "original-review-pass", Validation: &protocol.ValidationResult{Binding: old.Binding, Status: "PASS", Review: &r}}}
				a := d.submit(ev)
				if delivery == "result-first" {
					d.reopen()
					d.receipt(oldID)
				}
				d.reopen()
				d.drive()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("old result acceptance=%s current set status=%s history=%+v inputs=%+v", a.Status, d.run.Validation.Sets[coverage.ReviewWhitebox].Status, d.run.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory, d.run.ValidationInputs)
				if d.run.Validation.Sets[coverage.ReviewWhitebox].Status == "FROZEN" {
					t.Fatal("old review froze replacement test artifact without reviewing its candidate")
				}
				if len(snap.State.ObsoleteResults[oldID]) != 1 || snap.State.ObsoleteResults[oldID][0].Validation.Binding != old.Binding || snap.State.ObsoleteResults[oldID][0].PayloadDigest != "original-review-pass" {
					t.Fatal("original superseded evidence not retained exactly")
				}
				if len(d.run.Validation.Sets[coverage.ReviewWhitebox].ReviewHistory) != 0 {
					t.Fatal("obsolete review consumed a semantic round")
				}
				fresh, input := d.action("engine.qa.review.rationale.v2")
				if fresh == oldID || input.Binding.Candidate != now.Candidate {
					t.Fatal("replacement review did not bind its own artifact")
				}
				rr := approvedReview(input.Manifest)
				d.business(fresh, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &rr})
				materialize()
				qa, qi := d.action("engine.qa.execution.v2")
				execution := executionFor(qi)
				d.business(qa, nativeExecutionResult(qi, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &execution}))
				gate, gi := d.gateAction("quality")
				d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
				if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
					t.Fatal("new approved artifact did not complete validation")
				}
				revision := d.run.Revision
				d.reopen()
				d.drive()
				if d.run.Revision != revision {
					t.Fatal("recovery duplicated work")
				}
			})
		}
	}
}

func TestPrewaveRequirementDecisionsRetainExactDuty(t *testing.T) {
	for _, role := range []string{"engine.qa.design.v3", "engine.qa.review.rationale.v2"} {
		for _, redo := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/redo=%t", role, redo), func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, input := d.action("engine.qa.design.v3")
				manifest := simpleManifest(input)
				if role == "engine.qa.review.rationale.v2" {
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
					id, input = d.action(role)
				}
				old := id
				selected := ""
				if redo {
					selected = id
				}
				confirmValidationWork(d, selected)
				d.reopen()
				d.drive()
				if redo && role == "engine.qa.review.rationale.v2" {
					id, input = d.action("engine.qa.design.v3")
					manifest = input.Manifest
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
				}
				id, input = d.action(role)
				if redo == (id == old) {
					t.Fatal("requirement work disposition was not honored")
				}
				if role == "engine.qa.design.v3" {
					manifest = simpleManifest(input)
					d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
					id, input = d.action("engine.qa.review.rationale.v2")
				}
				review := approvedReview(input.Manifest)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &review})
				d.reopen()
				d.drive()
				if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
					t.Fatal("current requirement-authorized review failed to freeze")
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.State.Results[id].Validation.Binding != input.Binding {
					t.Fatal("signed original result identity changed")
				}
			})
		}
	}
}

func TestPrewaveReopeningPreservesUnobservedDesignTransport(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		for _, replacement := range []bool{false, true} {
			t.Run(fmt.Sprintf("unknown=%t/requirement-replacement=%t", unknown, replacement), func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, input := d.action("engine.qa.design.v3")
				manifest := simpleManifest(input)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
				d.drive()
				d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"normal"}}, "")
				old, oldInput := d.action("engine.qa.design.v3")
				if replacement {
					confirmValidationWork(d, old)
					d.drive()
					old, oldInput = d.action("engine.qa.design.v3")
					manifest = oldInput.Manifest
				}
				if unknown {
					e, err := protocol.NewSpawnReceiptWithFailureClass(d.eventID(), old, DefaultProvider, "unobserved-design", protocol.SpawnStatusUnknown, "")
					if err != nil {
						t.Fatal(err)
					}
					d.submit(e)
				}
				d.chooseValidation(protocol.ValidationRequest{Kind: "THAW", Mode: coverage.ReviewBlackbox, Category: "CASE_EXECUTABILITY", AffectedCases: []string{"invalid"}}, "")
				d.reopen()
				d.drive()
				if _, found := d.run.ValidationInputs[old]; found {
					t.Fatal("superseded design exposed as current input")
				}
				if _, pending := d.run.PendingActions[old]; !pending {
					t.Fatal("unobserved external work retired before identity receipt")
				}
				fresh, freshInput := d.action("engine.qa.design.v3")
				if fresh == old || freshInput.Binding.Task == oldInput.Binding.Task {
					t.Fatal("explicit new responsibility reused old design")
				}
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: old, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "superseded-design", Validation: &protocol.ValidationResult{Binding: oldInput.Binding, Status: "PASS", Manifest: &manifest}}})
				if unknown {
					for _, event := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
						life, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), DefaultProvider, "unobserved-design", "original-design-agent", event)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(life)
					}
					if _, _, err := d.f.engine.ReconcileUnknownReceipt(old, emptyFingerprint()); err != nil {
						t.Fatal(err)
					}
					d.drive()
				} else {
					d.receipt(old)
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.State.ObsoleteResults[old]) != 1 || snapshot.State.ObsoleteResults[old][0].PayloadDigest != "superseded-design" {
					t.Fatal("old legal fact was not retained")
				}
				manifest = freshInput.Manifest
				d.business(fresh, protocol.ValidationResult{Binding: freshInput.Binding, Status: "PASS", Manifest: &manifest})
				id, input = d.action("engine.qa.review.rationale.v2")
				review := approvedReview(input.Manifest)
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &review})
				if d.run.Validation.Sets[coverage.ReviewBlackbox].Status != "FROZEN" {
					t.Fatal("replacement design did not reach fresh approval")
				}
			})
		}
	}
}
