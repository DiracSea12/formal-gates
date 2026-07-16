package validate

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyV2ExportIsExactAndDeterministic(t *testing.T) {
	first, err := PolicyJSON(Policy())
	if err != nil {
		t.Fatal(err)
	}
	second, err := PolicyJSON(Policy())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("policy output is not deterministic")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(first, &top); err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top["schemaVersion"] == nil || top["postDevelopmentGateOrder"] == nil || top["artifactPolicies"] == nil {
		t.Fatalf("unexpected policy fields: %s", first)
	}
	if len(Policy().ArtifactPolicies) != 8 {
		t.Fatalf("expected eight policies, got %d", len(Policy().ArtifactPolicies))
	}
	for i := 1; i < len(Policy().ArtifactPolicies); i++ {
		if Policy().ArtifactPolicies[i-1].ID >= Policy().ArtifactPolicies[i].ID {
			t.Fatal("policies are not sorted")
		}
	}
	qa, ok := policyByID("qa.execution.v2")
	if !ok || qa.ArtifactRole != "QA_EXECUTION" || qa.ReceiptRequired || !qa.Mechanical || len(qa.RequiredCheckIDs) != 0 {
		t.Fatalf("unexpected QA Execution policy: %#v", qa)
	}
}

func TestRecordingSelectionUsesExportedArtifactPolicies(t *testing.T) {
	for _, policy := range Policy().ArtifactPolicies {
		mode := map[string]string{
			"requirements":     "requirements",
			"start-readiness":  "start-readiness",
			"post-development": "formal",
			"finalization":     "formal",
		}[policy.Flow]
		selected, ok := recordingPolicy(policy.Gate, policy.Stage, mode)
		if !ok || selected.ID != policy.ID {
			t.Fatalf("recording did not select policy %s: selected=%#v ok=%v", policy.ID, selected, ok)
		}
	}
}

func TestStrictJSONRejectsDuplicateUnknownNullAndTrailing(t *testing.T) {
	dir := t.TempDir()
	valid := writeV2RequirementsFixture(t, dir, "wf", "snap")
	cases := map[string]string{
		"duplicate": strings.Replace(valid, `"schemaVersion": 2,`, `"schemaVersion": 2, "schemaVersion": 2,`, 1),
		"unknown":   strings.Replace(valid, `"artifactRole":`, `"futureRole": true, "artifactRole":`, 1),
		"null":      strings.Replace(valid, `"requirementSource": "brief"`, `"requirementSource": null`, 1),
		"trailing":  valid + `{}`,
		"schema-v1": strings.Replace(valid, `"schemaVersion": 2`, `"schemaVersion": 1`, 1),
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			mustWrite(t, filepath.Join(dir, "requirements.json"), text)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "requirements.json", Gate: "requirements-clarification-gate", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "requirements"})
			if result.OK() {
				t.Fatalf("expected %s to fail", name)
			}
		})
	}
}

func TestClosedV2RequiredZeroValueFieldPresenceMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{name: "envelope stage", mutate: func(t *testing.T, dir string) {
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) { delete(root, "stage") })
		}},
		{name: "droppedQuestionApproval false", mutate: func(t *testing.T, dir string) {
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) { delete(jsonObject(root, "payload"), "droppedQuestionApproval") })
		}},
		{name: "scopePreservation PASS message", mutate: func(t *testing.T, dir string) {
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) {
				delete(jsonObject(jsonObject(root, "payload"), "scopePreservation"), "message")
			})
		}},
		{name: "taskProof PASS message", mutate: func(t *testing.T, dir string) {
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) { delete(jsonObject(jsonObject(root, "payload"), "taskProof"), "message") })
		}},
		{name: "COVERED dimension message", mutate: func(t *testing.T, dir string) {
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) {
				delete(jsonObject(jsonArray(jsonObject(root, "payload"), "dimensionCoverage")[0], ""), "message")
			})
		}},
		{name: "approvedDroppedIds empty array", mutate: func(t *testing.T, dir string) {
			decisionPath := filepath.Join(dir, "decision.json")
			mutateJSONObject(t, decisionPath, func(root map[string]any) { delete(root, "approvedDroppedIds") })
			mutateJSONObject(t, filepath.Join(dir, "requirements.json"), func(root map[string]any) {
				jsonObject(jsonObject(root, "payload"), "decision")["sha256"] = sha256File(decisionPath)
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeV2RequirementsFixture(t, dir, "wf", "snap")
			valid := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "requirements.json", Gate: "requirements-clarification-gate", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "requirements"})
			if !valid.OK() {
				t.Fatalf("explicit legal zero value was rejected: %#v", valid.Failures)
			}
			test.mutate(t, dir)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "requirements.json", Gate: "requirements-clarification-gate", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "requirements"})
			if result.OK() || !strings.Contains(resultSummary(result), "missing required field") {
				t.Fatalf("missing required field was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestRequirementsCoveredTargetsMatrix(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
		accept bool
	}{
		{name: "nested document", target: "docs/requirements.md", accept: true},
		{name: "top-level document", target: "README.md", accept: true},
		{name: "empty", target: ""},
		{name: "dot root", target: "."},
		{name: "slash root", target: "/"},
		{name: "parent root", target: ".."},
		{name: "parent traversal", target: "../README.md"},
		{name: "embedded traversal", target: "docs/../README.md"},
		{name: "dot segment", target: "docs/./README.md"},
		{name: "empty segment", target: "docs//README.md"},
		{name: "broad top-level directory", target: "docs"},
		{name: "trailing slash", target: "docs/"},
		{name: "wildcard", target: "*.md"},
		{name: "POSIX absolute", target: "/tmp/requirements.md"},
		{name: "drive absolute", target: "C:/repo/requirements.md"},
		{name: "drive relative", target: "C:requirements.md"},
		{name: "forward-slash UNC", target: "//server/share/requirements.md"},
		{name: "backslash UNC", target: `\\server\share\requirements.md`},
		{name: "URI scheme", target: "https://example.test/requirements.md"},
		{name: "backslash", target: `docs\requirements.md`},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			writeV2RequirementsFixture(t, runDir, "wf", "snap")
			mutateJSONObject(t, filepath.Join(runDir, "requirements.json"), func(root map[string]any) {
				jsonObject(root, "payload")["coveredTargets"] = []any{test.target}
			})
			artifact := relativePath(dir, filepath.Join(runDir, "requirements.json"))
			statePath := filepath.Join(runDir, "gate-state.json")
			result := WorkflowRecordStage(WorkflowRecordStageOptions{Worktree: dir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: artifact, WorkflowID: "wf", ChangeSnapshot: "snap"})
			if result.OK() != test.accept {
				t.Fatalf("target=%q accept=%v failures=%#v", test.target, test.accept, result.Failures)
			}
			_, stateErr := os.ReadFile(statePath)
			if !test.accept && !os.IsNotExist(stateErr) {
				t.Fatalf("rejected target changed authoritative state: %v", stateErr)
			}
			if test.accept && stateErr != nil {
				t.Fatalf("accepted target did not record authoritative state: %v", stateErr)
			}
		})
	}
}

func TestRequirementsContinuityUsesActualRecordedPriorPass(t *testing.T) {
	for _, test := range []struct {
		name      string
		withPrior bool
		previous  string
		accept    bool
	}{
		{name: "first run omits previous", accept: true},
		{name: "first run cannot invent previous", previous: "substitute"},
		{name: "later run exact prior", withPrior: true, previous: "actual", accept: true},
		{name: "later run cannot omit prior", withPrior: true},
		{name: "later run cannot substitute alias", withPrior: true, previous: "substitute"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			_, actual := writeV2RequirementsFixtureAt(t, dir, "wf", "snap", "first-", nil)
			if test.withPrior {
				result := GateRecord(GateRecordOptions{Worktree: dir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: "first-requirements.json", WorkflowID: "wf", ChangeSnapshot: "snap"})
				if !result.OK() {
					t.Fatalf("first requirements PASS failed: %#v", result.Failures)
				}
			}
			var previous *EvidenceRef
			switch test.previous {
			case "actual":
				previous = &actual
			case "substitute":
				alias := "prior-alias.json"
				data, err := os.ReadFile(filepath.Join(dir, actual.Path))
				if err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(dir, alias), string(data))
				ref := testRef(t, dir, alias)
				previous = &ref
			}
			writeV2RequirementsFixtureAt(t, dir, "wf", "snap", "second-", previous)
			statePath := filepath.Join(dir, ".claude", "gates", "gate-state.json")
			before, beforeErr := os.ReadFile(statePath)
			result := GateRecord(GateRecordOptions{Worktree: dir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: "second-requirements.json", WorkflowID: "wf", ChangeSnapshot: "snap"})
			if result.OK() != test.accept {
				t.Fatalf("accept=%v failures=%#v", test.accept, result.Failures)
			}
			if !test.accept {
				after, afterErr := os.ReadFile(statePath)
				if !os.IsNotExist(beforeErr) && (afterErr != nil || string(after) != string(before)) {
					t.Fatalf("continuity rejection changed prior state: beforeErr=%v afterErr=%v", beforeErr, afterErr)
				}
				if os.IsNotExist(beforeErr) && !os.IsNotExist(afterErr) {
					t.Fatalf("first-run continuity rejection created state: %v", afterErr)
				}
			}
		})
	}
}

func TestRequirementsV2RecordsClosureAndPreservesStateOnRejection(t *testing.T) {
	dir := t.TempDir()
	valid := writeV2RequirementsFixture(t, dir, "wf", "snap")
	result := GateRecord(GateRecordOptions{Worktree: dir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: "requirements.json", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("valid requirements failed: %#v", result.Failures)
	}
	statePath := filepath.Join(dir, ".claude", "gates", "gate-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	state, show := GateShow(GateShowOptions{Worktree: dir})
	if !show.OK() {
		t.Fatal(show.Failures)
	}
	entry := state.Gates["requirements-clarification-gate"]
	if !strings.Contains(entry.Artifact, "closures/") || entry.Mode != "requirements" {
		t.Fatalf("state did not bind requirements closure: %#v", entry)
	}
	mustWrite(t, filepath.Join(dir, "requirements.json"), strings.Replace(valid, `"openBlockers": []`, `"openBlockers": ["blocked"]`, 1))
	rejected := GateRecord(GateRecordOptions{Worktree: dir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: "requirements.json", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if rejected.OK() {
		t.Fatal("open blocker was accepted")
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected artifact changed authoritative state")
	}
}

func TestReviewerV2ChecksAndContextBundle(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"dispatch.txt": "dispatch", "input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "input.txt")}}
	writeJSONTest(t, filepath.Join(dir, "bundle.json"), bundle)
	policy, _ := policyByID("architecture.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}
	payload := ReviewerPayload{Dispatch: testRef(t, dir, "dispatch.txt"), ContextBundle: testRef(t, dir, "bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "changed.txt")), Verification: ptrRef(testRef(t, dir, "verification.txt"))}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}, payload)
	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if !result.OK() {
		t.Fatalf("valid reviewer failed: %#v", result.Failures)
	}
	checks[0].ID = checks[1].ID
	payload.Checks = checks
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}, payload)
	if result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow}); result.OK() {
		t.Fatal("duplicate/missing check was accepted")
	}
}

func TestReviewerFindingLocationsPresence(t *testing.T) {
	dir := t.TempDir()
	policy, _ := policyByID("architecture.post-development.v2")
	envelope, payload := reviewerPolicyFixture(t, dir, policy)
	payload.Checks[0].Findings = []Finding{{Message: "not tied to a line", Locations: []Location{}}}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
	valid := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if !valid.OK() {
		t.Fatalf("explicit empty locations was rejected: %#v", valid.Failures)
	}
	mutateJSONObject(t, filepath.Join(dir, "review.json"), func(root map[string]any) {
		finding := jsonObject(jsonArray(jsonObject(jsonArray(jsonObject(root, "payload"), "checks")[0], ""), "findings")[0], "")
		delete(finding, "locations")
	})
	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "missing required field") {
		t.Fatalf("missing finding locations was accepted: %#v", result.Failures)
	}
}

func TestExportedReviewerPolicyCheckBehaviorMatrix(t *testing.T) {
	for _, policy := range Policy().ArtifactPolicies {
		if !strings.HasSuffix(policy.ArtifactRole, "_REVIEW") {
			continue
		}
		t.Run(policy.ID, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := reviewerPolicyFixture(t, dir, policy)
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
			options := ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, Stage: policy.Stage, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow}
			if result := Artifact(options); !result.OK() {
				t.Fatalf("exported policy positive control failed: %#v", result.Failures)
			}
			wrongFlow := options
			wrongFlow.Flow = "unsupported-flow"
			if result := Artifact(wrongFlow); result.OK() || !strings.Contains(resultSummary(result), "flow") {
				t.Fatalf("policy flow mismatch was accepted: %#v", result.Failures)
			}
			for i, check := range payload.Checks {
				t.Run(check.ID, func(t *testing.T) {
					missing := payload
					missing.Checks = append(append([]ReviewCheck{}, payload.Checks[:i]...), payload.Checks[i+1:]...)
					writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, missing)
					if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "every policy check") {
						t.Fatalf("missing exported check %s was accepted: %#v", check.ID, result.Failures)
					}
					notApplicable := payload
					notApplicable.Checks = append([]ReviewCheck{}, payload.Checks...)
					notApplicable.Checks[i].Status = "NOT_APPLICABLE"
					notApplicable.Checks[i].Message = "not applicable for this flow"
					writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, notApplicable)
					allowed := stringSet(policy.AllowedNotApplicableCheckIDs)[check.ID]
					if result := Artifact(options); result.OK() != allowed {
						t.Fatalf("NOT_APPLICABLE allowed=%v failures=%#v", allowed, result.Failures)
					}
				})
			}
		})
	}
}

func TestPostDevelopmentComplexityStatisticsChecksEveryDecodableReport(t *testing.T) {
	dir := t.TempDir()
	policy, _ := policyByID("complexity.post-development.v2")
	envelope, payload := reviewerPolicyFixture(t, dir, policy)
	budgetReport := ComplexityReport{
		Status: "PASS", VCS: "git", Worktree: dir, TaskType: "bugfix",
		Budget:       &ComplexityBudget{MaxNet: -600, MaxNewProdFiles: 5, MaxProdInsertions: 2400},
		BudgetSource: "explicit", BudgetOverrides: ComplexityBudgetOverride{MaxNet: true, MaxNewProdFiles: true, MaxProdInsertions: true},
		Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{},
	}
	writeJSONTest(t, filepath.Join(dir, "budget-report.json"), budgetReport)
	for i := range payload.Checks {
		if payload.Checks[i].ID == "complexity.statistics" {
			payload.Checks[i].EvidenceRefs = append(payload.Checks[i].EvidenceRefs, testRef(t, dir, "budget-report.json"))
		}
	}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)

	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "post-development complexity evidence must be statistics-only") {
		t.Fatalf("later budget-bearing report was accepted: %#v", result.Failures)
	}
}

func TestExportedOperationalPolicyFlowBehaviorMatrix(t *testing.T) {
	t.Run("requirements.pass.v2", func(t *testing.T) {
		dir := t.TempDir()
		writeV2RequirementsFixture(t, dir, "wf", "snap")
		options := ArtifactOptions{Root: dir, RunDir: dir, File: "requirements.json", Gate: "requirements-clarification-gate", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "requirements"}
		if result := Artifact(options); !result.OK() {
			t.Fatalf("requirements policy positive control failed: %#v", result.Failures)
		}
		options.Flow = "post-development"
		if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "flow") {
			t.Fatalf("requirements flow mismatch was accepted: %#v", result.Failures)
		}
	})
	t.Run("qa.execution.v2", func(t *testing.T) {
		dir := t.TempDir()
		envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
		writeEnvelopeTest(t, filepath.Join(dir, "qa-execution.json"), envelope, payload)
		options := ArtifactOptions{Root: dir, RunDir: dir, File: "qa-execution.json", Gate: "qa-test-gate", Stage: "Execution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"}
		if result := Artifact(options); !result.OK() {
			t.Fatalf("QA Execution policy positive control failed: %#v", result.Failures)
		}
		options.Flow = "start-readiness"
		if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "flow") {
			t.Fatalf("QA Execution flow mismatch was accepted: %#v", result.Failures)
		}
	})
	t.Run("final-execution.v2", func(t *testing.T) {
		dir := t.TempDir()
		writeFinalExecutionPolicyFixture(t, dir)
		options := ArtifactOptions{Root: dir, RunDir: dir, File: "final-execution.json", Gate: "qa-test-gate", Stage: "FinalExecution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "finalization"}
		if result := Artifact(options); !result.OK() {
			t.Fatalf("FinalExecution policy positive control failed: %#v", result.Failures)
		}
		options.Flow = "post-development"
		if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "flow") {
			t.Fatalf("FinalExecution flow mismatch was accepted: %#v", result.Failures)
		}
	})
}

func TestQAExecutionRejectsIncompleteOrMismatchedEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, *FormalGateEvidence, *QAExecutionPayload)
	}{
		{name: "review role", mutate: func(_ *testing.T, _ string, envelope *FormalGateEvidence, _ *QAExecutionPayload) {
			envelope.ArtifactRole = "QA_REVIEW"
		}},
		{name: "non-pass verdict", mutate: func(_ *testing.T, _ string, envelope *FormalGateEvidence, _ *QAExecutionPayload) {
			envelope.Verdict = "FAIL"
		}},
		{name: "wrong evidence hash", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, _ *QAExecutionPayload) {
			mustWrite(t, filepath.Join(dir, "approved-cases.md"), "tampered\n")
		}},
		{name: "stale results snapshot", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-results.json"), func(root map[string]any) { root["changeSnapshot"] = "old" })
			rebindQAResults(t, dir, payload)
		}},
		{name: "failed case", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-results.json"), func(root map[string]any) {
				root["overallOutcome"] = "FAIL"
				jsonObject(jsonArray(root, "caseResults")[0], "")["status"] = "FAIL"
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "wrong result pointer", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["resultPointer"] = "/caseResults/1"
			})
			payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
		}},
		{name: "missing result oracle", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-results.json"), func(root map[string]any) {
				delete(jsonObject(jsonArray(root, "caseResults")[0], ""), "oracle")
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "changed binding oracle", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["oracle"] = "changed oracle"
			})
			payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
		}},
		{name: "missing binding procedures", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) {
				delete(jsonObject(jsonArray(root, "bindings")[0], ""), "procedures")
			})
			payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
		}},
		{name: "changed binding procedures", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["procedures"] = []string{"E-002"}
			})
			payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
		}},
		{name: "unknown result field", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-results.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "caseResults")[0], "")["unexpected"] = true
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "removed oracleBound field is unknown", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["oracleBound"] = true
			})
			payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
			test.mutate(t, dir, &envelope, &payload)
			writeEnvelopeTest(t, filepath.Join(dir, "qa-execution.json"), envelope, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "qa-execution.json", Gate: "qa-test-gate", Stage: "Execution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"})
			if result.OK() {
				t.Fatal("invalid QA Execution evidence was accepted")
			}
		})
	}
}

func TestQAExecutionReadsDocumentedCaseIDFields(t *testing.T) {
	for _, test := range []struct {
		name     string
		cases    string
		wantPass bool
	}{
		{name: "heading disagreement and unrelated heading", cases: "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## Login flow\n\nCase ID: P1-001\n\n## Execution notes\n", wantPass: true},
		{name: "heading without Case ID", cases: "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## P1-001 Case\n"},
		{name: "duplicate Case ID", cases: "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## First\n\nCase ID: P1-001\n\n## Second\n\nCase ID: P1-001\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
			mustWrite(t, filepath.Join(dir, "approved-cases.md"), test.cases)
			rebindApprovedCases(t, dir, &payload)
			writeEnvelopeTest(t, filepath.Join(dir, "qa-execution.json"), envelope, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "qa-execution.json", Gate: "qa-test-gate", Stage: "Execution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"})
			if result.OK() != test.wantPass {
				t.Fatalf("accept=%v failures=%#v", test.wantPass, result.Failures)
			}
		})
	}
}

func TestQAExecutionRejectsReviewerPayloadFields(t *testing.T) {
	dir := t.TempDir()
	envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
	path := filepath.Join(dir, "qa-execution.json")
	writeEnvelopeTest(t, path, envelope, payload)
	mutateJSONObject(t, path, func(root map[string]any) {
		jsonObject(root, "payload")["checks"] = []any{}
	})
	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "qa-execution.json", Gate: "qa-test-gate", Stage: "Execution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"})
	if result.OK() || !strings.Contains(resultSummary(result), "unknown field") {
		t.Fatalf("reviewer field was accepted in QA Execution: %#v", result.Failures)
	}
}

func rebindQAResults(t *testing.T, dir string, payload *QAExecutionPayload) {
	t.Helper()
	payload.QAOwnedResults = testRef(t, dir, "qa-results.json")
	mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) { root["qaOwnedResults"] = payload.QAOwnedResults })
	payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
}

func rebindApprovedCases(t *testing.T, dir string, payload *QAExecutionPayload) {
	t.Helper()
	payload.ApprovedCaseSet = testRef(t, dir, "approved-cases.md")
	mutateJSONObject(t, filepath.Join(dir, "qa-binding.json"), func(root map[string]any) { root["approvedCaseSet"] = payload.ApprovedCaseSet })
	payload.CaseResultBinding = testRef(t, dir, "qa-binding.json")
}

func TestReviewerV2ContextBundleRejectsSymlinkAliases(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"dispatch.txt": "dispatch", "input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	if err := os.Symlink("input.txt", filepath.Join(dir, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "input.txt"), testRef(t, dir, "alias.txt")}}
	writeJSONTest(t, filepath.Join(dir, "bundle.json"), bundle)
	policy, _ := policyByID("architecture.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}
	payload := ReviewerPayload{Dispatch: testRef(t, dir, "dispatch.txt"), ContextBundle: testRef(t, dir, "bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "changed.txt")), Verification: ptrRef(testRef(t, dir, "verification.txt"))}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}, payload)

	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "must not resolve to the same file") {
		t.Fatalf("symlink aliases were accepted: %#v", result.Failures)
	}
}

func TestVerifyClosureRejectsResolvedPathAliases(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "root.json"), "{}\n")
	mustWrite(t, filepath.Join(dir, "evidence.txt"), "evidence\n")
	if err := os.Symlink("evidence.txt", filepath.Join(dir, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	closure := EvidenceClosure{
		SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Verdict: "PASS", RootRole: "COMPLEXITY_REVIEW", RootArtifact: "root.json", Receipt: "evidence.txt",
		Entries: []ClosureEntry{
			{Path: "root.json", SHA256: sha256File(filepath.Join(dir, "root.json")), References: []string{"alias.txt", "evidence.txt"}},
			{Path: "alias.txt", SHA256: sha256File(filepath.Join(dir, "alias.txt")), References: []string{}},
			{Path: "evidence.txt", SHA256: sha256File(filepath.Join(dir, "evidence.txt")), References: []string{}},
		},
	}
	if err := verifyClosure(ArtifactOptions{Root: dir}, dir, closure); err == nil || !strings.Contains(err.Error(), "resolve to the same file") {
		t.Fatalf("resolved closure aliases were accepted: %v", err)
	}
}

func TestBuildClosureKeepsIdenticalOutputBytesAtDistinctPathsIndependent(t *testing.T) {
	dir := t.TempDir()
	envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
	firstPath := filepath.Join(dir, "first-qa-execution.json")
	writeEnvelopeTest(t, firstPath, envelope, payload)
	data, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(dir, "second-qa-execution.json")
	mustWrite(t, secondPath, string(data))

	build := func(file string) EvidenceRef {
		options := ArtifactOptions{Root: dir, RunDir: dir, File: file, Gate: "qa-test-gate", Stage: "Execution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"}
		var result Result
		decoded := decodeArtifact(options, data, &result)
		if !result.OK() {
			t.Fatalf("valid QA output %s failed: %#v", file, result.Failures)
		}
		closure, err := buildClosure(options, decoded, nil)
		if err != nil {
			t.Fatalf("build closure for %s: %v", file, err)
		}
		return closure
	}

	first := build(filepath.Base(firstPath))
	firstBytes, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(first.Path)))
	if err != nil {
		t.Fatal(err)
	}
	second := build(filepath.Base(secondPath))
	if first.Path == second.Path || first.SHA256 == second.SHA256 {
		t.Fatalf("distinct root paths shared closure identity: first=%#v second=%#v", first, second)
	}
	after, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(first.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, after) || sha256Bytes(after) != first.SHA256 {
		t.Fatal("second closure replaced the first closure bytes")
	}
}

func TestReviewerV2FindingLocationPathsAreRepositoryRelative(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"dispatch.txt": "dispatch", "input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	writeJSONTest(t, filepath.Join(dir, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "input.txt")}})
	policy, _ := policyByID("architecture.post-development.v2")
	baseChecks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		baseChecks = append(baseChecks, ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}

	for _, test := range []struct {
		name   string
		path   string
		accept bool
	}{
		{name: "repository relative", path: "internal/validate/evidence.go", accept: true},
		{name: "URI", path: "https://example.test/x"},
		{name: "Windows volume absolute", path: "C:/repo/file.go"},
		{name: "Windows volume relative", path: "C:file.go"},
		{name: "UNC", path: `\\server\share\file.go`},
		{name: "backslash", path: `internal\validate\evidence.go`},
		{name: "absolute", path: "/repo/file.go"},
		{name: "parent traversal", path: "../file.go"},
		{name: "embedded traversal", path: "internal/../file.go"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks := append([]ReviewCheck(nil), baseChecks...)
			checks[0].Findings = []Finding{{Message: "finding", Locations: []Location{{Path: test.path, StartLine: 1, EndLine: 1}}}}
			payload := ReviewerPayload{Dispatch: testRef(t, dir, "dispatch.txt"), ContextBundle: testRef(t, dir, "bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "changed.txt")), Verification: ptrRef(testRef(t, dir, "verification.txt"))}
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}, payload)

			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
			if result.OK() != test.accept {
				t.Fatalf("accept=%v failures=%#v", test.accept, result.Failures)
			}
		})
	}
}

func TestReviewerVerdictExactlyMatchesCheckAggregate(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"dispatch.txt": "dispatch", "input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	writeJSONTest(t, filepath.Join(dir, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "input.txt")}})
	policy, _ := policyByID("architecture.post-development.v2")
	baseChecks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		baseChecks = append(baseChecks, ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}

	for _, test := range []struct {
		name           string
		envelope       string
		checkAggregate string
		accept         bool
	}{
		{name: "pass matches pass", envelope: "PASS", checkAggregate: "PASS", accept: true},
		{name: "review matches review", envelope: "REVIEW", checkAggregate: "REVIEW", accept: true},
		{name: "fail matches fail", envelope: "FAIL", checkAggregate: "FAIL", accept: true},
		{name: "blocked matches blocked", envelope: "BLOCKED", checkAggregate: "BLOCKED", accept: true},
		{name: "review does not match fail", envelope: "REVIEW", checkAggregate: "FAIL"},
		{name: "fail does not match blocked", envelope: "FAIL", checkAggregate: "BLOCKED"},
		{name: "blocked does not match review", envelope: "BLOCKED", checkAggregate: "REVIEW"},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks := append([]ReviewCheck(nil), baseChecks...)
			checks[0].Status = test.checkAggregate
			payload := ReviewerPayload{Dispatch: testRef(t, dir, "dispatch.txt"), ContextBundle: testRef(t, dir, "bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "changed.txt")), Verification: ptrRef(testRef(t, dir, "verification.txt"))}
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: test.envelope}, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
			if result.OK() != test.accept {
				t.Fatalf("accept=%v failures=%#v", test.accept, result.Failures)
			}
			if !test.accept && !strings.Contains(resultSummary(result), "top-level verdict contradicts check aggregation") {
				t.Fatalf("expected aggregation mismatch, got %#v", result.Failures)
			}
		})
	}
}

func reviewerPolicyFixture(t *testing.T, dir string, policy ArtifactPolicy) (FormalGateEvidence, ReviewerPayload) {
	t.Helper()
	for name, text := range map[string]string{"dispatch.txt": "dispatch", "input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	writeJSONTest(t, filepath.Join(dir, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "input.txt")}})
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		switch id {
		case "complexity.statistics":
			if policy.ID == "complexity.post-development.v2" {
				report := ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}}
				writeJSONTest(t, filepath.Join(dir, "statistics.json"), report)
				check.EvidenceRefs = []EvidenceRef{testRef(t, dir, "statistics.json")}
			}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{Dispatch: testRef(t, dir, "dispatch.txt"), ContextBundle: testRef(t, dir, "bundle.json"), ReviewPolicyID: policy.ID, Checks: checks}
	if policy.ChangedFilesRequired {
		payload.ChangedFiles = ptrRef(testRef(t, dir, "changed.txt"))
	}
	if policy.VerificationRequired {
		payload.Verification = ptrRef(testRef(t, dir, "verification.txt"))
	}
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}
	return envelope, payload
}

func qaExecutionPolicyFixture(t *testing.T, dir, workflowID, snapshot string) (FormalGateEvidence, QAExecutionPayload) {
	t.Helper()
	for name, text := range map[string]string{"qa-changed.txt": "changed", "qa-verification.txt": "verified"} {
		mustWrite(t, filepath.Join(dir, name), text)
	}
	mustWrite(t, filepath.Join(dir, "approved-cases.md"), "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## Login flow\n\nCase ID: P1-001\n\n## Execution notes\n")
	approved := testRef(t, dir, "approved-cases.md")
	writeJSONTest(t, filepath.Join(dir, "qa-results.json"), map[string]any{
		"owner": "QA", "workflowId": workflowID, "changeSnapshot": snapshot, "stage": "Execution", "status": "COMPLETE", "overallOutcome": "PASS",
		"executions":  []any{map[string]any{"id": "E-001", "outcome": "PASS", "procedure": "Run the approved case", "result": "The case passed"}},
		"caseResults": []any{map[string]any{"caseId": "P1-001", "status": "PASS", "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	results := testRef(t, dir, "qa-results.json")
	writeJSONTest(t, filepath.Join(dir, "qa-binding.json"), map[string]any{
		"workflowId": workflowID, "changeSnapshot": snapshot, "approvedCaseSet": approved, "qaOwnedResults": results, "complete": true,
		"bindings": []any{map[string]any{"caseId": "P1-001", "resultPointer": "/caseResults/0", "status": "PASS", "executionRefs": []string{"E-001"}, "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	payload := QAExecutionPayload{
		ApprovedCaseSet: approved, QAOwnedResults: results, CaseResultBinding: testRef(t, dir, "qa-binding.json"),
		ChangedFiles: testRef(t, dir, "qa-changed.txt"), Verification: testRef(t, dir, "qa-verification.txt"),
	}
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS"}
	return envelope, payload
}

func writeFinalExecutionPolicyFixture(t *testing.T, dir string) {
	t.Helper()
	matrix := make([]FinalGateRow, 0, len(postDevelopmentGateOrder))
	for _, gate := range postDevelopmentGateOrder {
		root := gate + "-root.json"
		mustWrite(t, filepath.Join(dir, root), "{}\n")
		closure := EvidenceClosure{
			SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: gate, Stage: map[string]string{"qa-test-gate": "Execution"}[gate], Verdict: "PASS",
			RootRole:     map[string]string{"qa-test-gate": "QA_EXECUTION", "complexity-gate": "COMPLEXITY_REVIEW", "architecture-health-gate": "ARCHITECTURE_REVIEW", "code-quality-gate": "CODE_QUALITY_REVIEW"}[gate],
			RootArtifact: root, Entries: []ClosureEntry{{Path: root, SHA256: sha256File(filepath.Join(dir, root)), References: []string{}}},
		}
		if gate != "qa-test-gate" {
			receipt := gate + "-receipt.json"
			mustWrite(t, filepath.Join(dir, receipt), "{}\n")
			closure.Receipt = receipt
			closure.Entries = append(closure.Entries, ClosureEntry{Path: receipt, SHA256: sha256File(filepath.Join(dir, receipt)), References: []string{}})
		}
		closurePath := gate + "-closure.json"
		writeJSONTest(t, filepath.Join(dir, closurePath), closure)
		matrix = append(matrix, FinalGateRow{Gate: gate, GateEvidence: testRef(t, dir, closurePath)})
	}
	attempt := WorkflowFinalVerificationAttempt{"name": "go test", "status": "PASS"}
	writeJSONTest(t, filepath.Join(dir, "final-verification.json"), WorkflowFinalVerificationArtifact{SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Status: "PASS", Attempts: []WorkflowFinalVerificationAttempt{attempt}, AcceptedAttempts: []WorkflowFinalVerificationAttempt{attempt}})
	payload := FinalExecutionPayload{Mode: "MECHANICAL_CLOSEOUT", GateMatrix: matrix, FinalVerification: testRef(t, dir, "final-verification.json"), ReleaseJudgment: "SEAL"}
	writeEnvelopeTest(t, filepath.Join(dir, "final-execution.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "FINAL_EXECUTION", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "FinalExecution", Verdict: "PASS"}, payload)
}

func mutateJSONObject(t *testing.T, path string, mutate func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	mutate(root)
	writeJSONTest(t, path, root)
}

func jsonObject(value any, key string) map[string]any {
	if key != "" {
		value = value.(map[string]any)[key]
	}
	return value.(map[string]any)
}

func jsonArray(object map[string]any, key string) []any {
	return object[key].([]any)
}

func writeV2RequirementsFixture(t *testing.T, dir, workflow, snapshot string) string {
	text, _ := writeV2RequirementsFixtureAt(t, dir, workflow, snapshot, "", nil)
	return text
}

func writeV2RequirementsFixtureAt(t *testing.T, dir, workflow, snapshot, prefix string, previous *EvidenceRef) (string, EvidenceRef) {
	t.Helper()
	alignmentPath := prefix + "alignment.json"
	decisionPath := prefix + "decision.json"
	requirementsPath := prefix + "requirements.json"
	alignment := AlignmentArtifact{SchemaVersion: 2, WorkflowID: workflow, ChangeSnapshot: snapshot, Items: []AlignmentItem{{ID: "RQ-001", RequirementOrQuestion: "What", Source: "user", WhyItMatters: "value", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "implement", DocumentImpact: "docs/requirements.md", EvidenceNeeded: "tests"}}}
	writeJSONTest(t, filepath.Join(dir, alignmentPath), alignment)
	alignmentRef := testRef(t, dir, alignmentPath)
	decision := RequirementsDecision{SchemaVersion: 2, WorkflowID: workflow, ChangeSnapshot: snapshot, DecisionType: "USER_CONFIRMATION", UserConfirmation: true, UserOriginal: "approved", Alignment: alignmentRef, ApprovedAlignmentIDs: []string{"RQ-001"}, ApprovedDroppedIDs: []string{}, ApprovalScope: "requirements-clarification-gate"}
	writeJSONTest(t, filepath.Join(dir, decisionPath), decision)
	dimensions := make([]DimensionCoverage, 0, len(dimensionIDs))
	for _, id := range dimensionIDs {
		dimensions = append(dimensions, DimensionCoverage{ID: id, Status: "COVERED", AlignmentIDs: []string{"RQ-001"}, Message: ""})
	}
	payload := RequirementsPayload{RequirementSource: "brief", Alignment: alignmentRef, TotalAlignmentItems: 1, PreviousAlignment: previous, OpenQuestionIDs: []string{}, OpenBlockers: []string{}, DroppedQuestionIDs: []string{}, DroppedQuestionApproval: false, UserConfirmation: true, CoverageScan: "PASS", ScopePreservation: PassOrNA{Status: "PASS", Message: ""}, TaskProof: PassOrNA{Status: "PASS", Message: ""}, DimensionCoverage: dimensions, Decision: testRef(t, dir, decisionPath), CoveredTargets: []string{"docs/requirements.md"}, DownstreamPermission: "READY_TO_DRAFT"}
	path := filepath.Join(dir, requirementsPath)
	writeEnvelopeTest(t, path, FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "REQUIREMENTS_PASS", WorkflowID: workflow, ChangeSnapshot: snapshot, Gate: "requirements-clarification-gate", Stage: "", Verdict: "PASS"}, payload)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), alignmentRef
}

func writeEnvelopeTest(t *testing.T, path string, envelope FormalGateEvidence, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope.Payload = data
	writeJSONTest(t, path, envelope)
}
func writeJSONTest(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(data)+"\n")
}
func testRef(t *testing.T, dir, path string) EvidenceRef {
	t.Helper()
	return EvidenceRef{Path: path, SHA256: sha256FileForTest(t, filepath.Join(dir, path))}
}
func ptrRef(ref EvidenceRef) *EvidenceRef { return &ref }
