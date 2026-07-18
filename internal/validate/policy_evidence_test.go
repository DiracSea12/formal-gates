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
	if len(Policy().ArtifactPolicies) != 10 {
		t.Fatalf("expected ten policies, got %d", len(Policy().ArtifactPolicies))
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
	carry, ok := policyByID("carry.arbiter.v2")
	if !ok || carry.ArtifactRole != "CARRY_ARBITER" || carry.Gate != "qa-test-gate" || carry.Stage != "Carry" || carry.Flow != "carry" || !carry.ReceiptRequired || carry.Mechanical || len(carry.RequiredCheckIDs) != 0 {
		t.Fatalf("unexpected Carry Arbiter policy: %#v", carry)
	}
}

func TestRecordingSelectionUsesExportedArtifactPolicies(t *testing.T) {
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Flow == "carry" {
			if selected, ok := recordingPolicy(policy.Gate, policy.Stage, "formal"); ok || selected.ID != "" {
				t.Fatalf("generic record-stage selected Carry policy: %#v", selected)
			}
			continue
		}
		mode := map[string]string{
			"requirements":     "requirements",
			"start-readiness":  "start-readiness",
			"post-development": "formal",
			"pre-development":  "pre-development",
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
			mutateJSONObject(t, filepath.Join(runDir, "restricted", "requirements.json"), func(root map[string]any) {
				jsonObject(root, "payload")["coveredTargets"] = []any{test.target}
			})
			artifact := relativePath(dir, filepath.Join(runDir, "restricted", "requirements.json"))
			statePath := filepath.Join(runDir, "restricted", "gate-state.json")
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
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			_, actual := writeV2RequirementsFixtureAt(t, runDir, "wf", "snap", "first-", nil)
			firstArtifact := relativePath(dir, filepath.Join(runDir, "restricted", "first-requirements.json"))
			if test.withPrior {
				result := GateRecord(GateRecordOptions{Worktree: dir, RunDir: runDir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: firstArtifact, WorkflowID: "wf", ChangeSnapshot: "snap"})
				if !result.OK() {
					t.Fatalf("first requirements PASS failed: %#v", result.Failures)
				}
			}
			var previous *EvidenceRef
			switch test.previous {
			case "actual":
				previous = &actual
			case "substitute":
				alias := "restricted/prior-alias.json"
				data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(actual.Path)))
				if err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(runDir, filepath.FromSlash(alias)), string(data))
				ref := testRef(t, runDir, alias)
				previous = &ref
			}
			writeV2RequirementsFixtureAt(t, runDir, "wf", "snap", "second-", previous)
			statePath := filepath.Join(runDir, "restricted", "gate-state.json")
			before, beforeErr := os.ReadFile(statePath)
			secondArtifact := relativePath(dir, filepath.Join(runDir, "restricted", "second-requirements.json"))
			result := GateRecord(GateRecordOptions{Worktree: dir, RunDir: runDir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: secondArtifact, WorkflowID: "wf", ChangeSnapshot: "snap"})
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
	restricted := filepath.Join(dir, "restricted")
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/input.txt")}}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), bundle)
	policy, _ := policyByID("architecture.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, "restricted/bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "restricted/changed.txt")), Verification: ptrRef(testRef(t, dir, "restricted/verification.txt"))}
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

func TestActiveWorkflowRejectsNestedEvidenceOutsideRestricted(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "wf")
	policy, _ := policyByID("architecture.post-development.v2")
	envelope, payload := reviewerPolicyFixture(t, runDir, policy)
	mustWrite(t, filepath.Join(runDir, "outside.txt"), "outside restricted")
	outsideRef := testRef(t, runDir, "outside.txt")
	writeJSONTest(t, filepath.Join(runDir, "restricted", "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{outsideRef}})
	payload.ContextBundle = testRef(t, runDir, "restricted/bundle.json")
	artifactPath := filepath.Join(runDir, "restricted", "review.json")
	writeEnvelopeTest(t, artifactPath, envelope, payload)
	result := Artifact(ArtifactOptions{Root: dir, RunDir: runDir, File: relativePath(dir, artifactPath), Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "restricted directory") {
		t.Fatalf("nested evidence outside restricted was accepted: %#v", result.Failures)
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

func TestPostDevelopmentComplexityRejectsBudgetReportsOutsideStatisticsCheck(t *testing.T) {
	policy, _ := policyByID("complexity.post-development.v2")
	for _, location := range []string{"context bundle", "wrapped handoff", "other check"} {
		t.Run(location, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := reviewerPolicyFixture(t, dir, policy)
			budgetReport := ComplexityReport{
				Status: "PASS", VCS: "git", Worktree: dir, TaskType: "bugfix",
				Budget:       &ComplexityBudget{MaxNet: -600, MaxNewProdFiles: 5, MaxProdInsertions: 2400},
				BudgetSource: "explicit", BudgetOverrides: ComplexityBudgetOverride{MaxNet: true, MaxNewProdFiles: true, MaxProdInsertions: true},
				Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{},
			}
			budgetName := "budget-report.json"
			budgetPath := filepath.Join(dir, "restricted", budgetName)
			writeJSONTest(t, budgetPath, budgetReport)
			if location == "wrapped handoff" {
				budgetName = "handoff.json"
				budgetPath = filepath.Join(dir, "restricted", budgetName)
				writeJSONTest(t, budgetPath, map[string]any{"handoff": map[string]any{"developmentTimeComplexityBudget": map[string]any{"maxNet": 100}}})
			}
			budgetRef := testRef(t, dir, "restricted/"+budgetName)
			if location == "context bundle" || location == "wrapped handoff" {
				writeJSONTest(t, filepath.Join(dir, "restricted", "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{budgetRef}})
				payload.ContextBundle = testRef(t, dir, "restricted/bundle.json")
			} else {
				for i := range payload.Checks {
					if payload.Checks[i].ID == "complexity.diff-shape" {
						payload.Checks[i].EvidenceRefs = []EvidenceRef{budgetRef}
					}
				}
			}
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
			if result.OK() || !strings.Contains(resultSummary(result), "development-time budget material") {
				t.Fatalf("budget-bearing report outside statistics check was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestCarryArbiterRequiresFixedEnvelopeContract(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	fixture.Envelope.Gate = "complexity-gate"
	writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)
	result := Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: fixture.Artifact, Gate: "complexity-gate", Stage: "Carry", WorkflowID: "wf", ChangeSnapshot: "target"})
	if result.OK() || !strings.Contains(resultSummary(result), "Carry Arbiter must use artifactRole") {
		t.Fatalf("Carry artifact with mismatched fixed contract was accepted: %#v", result.Failures)
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
		options := ArtifactOptions{Root: dir, RunDir: dir, File: "restricted/final-execution.json", Gate: "qa-test-gate", Stage: "FinalExecution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "finalization"}
		if result := Artifact(options); !result.OK() {
			t.Fatalf("FinalExecution policy positive control failed: %#v", result.Failures)
		}
		options.Flow = "post-development"
		if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "flow") {
			t.Fatalf("FinalExecution flow mismatch was accepted: %#v", result.Failures)
		}
	})
}

func TestFinalExecutionRejectsOldAndMismatchedFreshRows(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "old row shape", mutate: func(row map[string]any) {
			delete(row, "resultKind")
			delete(row, "sourceSnapshot")
			delete(row, "targetSnapshot")
		}},
		{name: "wrong source", mutate: func(row map[string]any) { row["sourceSnapshot"] = "older" }},
		{name: "wrong target", mutate: func(row map[string]any) { row["targetSnapshot"] = "other" }},
		{name: "wrong result kind", mutate: func(row map[string]any) { row["resultKind"] = "CARRIED_PASS" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFinalExecutionPolicyFixture(t, dir)
			mutateJSONObject(t, filepath.Join(dir, "restricted", "final-execution.json"), func(root map[string]any) {
				payload := root["payload"].(map[string]any)
				row := payload["gateMatrix"].([]any)[0].(map[string]any)
				test.mutate(row)
			})
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "restricted/final-execution.json", Gate: "qa-test-gate", Stage: "FinalExecution", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "finalization"})
			if result.OK() {
				t.Fatal("invalid FinalExecution row was accepted")
			}
		})
	}
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
			mustWrite(t, filepath.Join(dir, "restricted", "approved-cases.md"), "tampered\n")
		}},
		{name: "missing Design Review", mutate: func(_ *testing.T, _ string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			payload.DesignReview = EvidenceRef{}
		}},
		{name: "case changed after Design Review", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mustWrite(t, filepath.Join(dir, "restricted", "approved-cases.md"), "# Cases\n\nCase ID: P1-001\n\nOracle: weakened oracle\n")
			rebindApprovedCases(t, dir, payload)
		}},
		{name: "copied case set", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			data, _ := os.ReadFile(filepath.Join(dir, "restricted", "approved-cases.md"))
			mustWrite(t, filepath.Join(dir, "restricted", "copied-cases.md"), string(data))
			payload.ApprovedCaseSet = testRef(t, dir, "restricted/copied-cases.md")
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) { root["approvedCaseSet"] = payload.ApprovedCaseSet })
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
		}},
		{name: "stale results snapshot", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-results.json"), func(root map[string]any) { root["changeSnapshot"] = "old" })
			rebindQAResults(t, dir, payload)
		}},
		{name: "failed case", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-results.json"), func(root map[string]any) {
				root["overallOutcome"] = "FAIL"
				jsonObject(jsonArray(root, "caseResults")[0], "")["status"] = "FAIL"
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "wrong result pointer", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["resultPointer"] = "/caseResults/1"
			})
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
		}},
		{name: "missing result oracle", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-results.json"), func(root map[string]any) {
				delete(jsonObject(jsonArray(root, "caseResults")[0], ""), "oracle")
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "changed binding oracle", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["oracle"] = "changed oracle"
			})
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
		}},
		{name: "missing binding procedures", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) {
				delete(jsonObject(jsonArray(root, "bindings")[0], ""), "procedures")
			})
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
		}},
		{name: "changed binding procedures", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["procedures"] = []string{"E-002"}
			})
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
		}},
		{name: "unknown result field", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-results.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "caseResults")[0], "")["unexpected"] = true
			})
			rebindQAResults(t, dir, payload)
		}},
		{name: "removed oracleBound field is unknown", mutate: func(t *testing.T, dir string, _ *FormalGateEvidence, payload *QAExecutionPayload) {
			mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) {
				jsonObject(jsonArray(root, "bindings")[0], "")["oracleBound"] = true
			})
			payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
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

func TestQADesignReviewCaseBindingRejectsMissingOrMismatchedDesignerReceipt(t *testing.T) {
	policy, _ := policyByID("qa.design-review.v2")
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, *ReviewerPayload)
	}{
		{name: "missing receipt", mutate: func(_ *testing.T, _ string, payload *ReviewerPayload) {
			for i := range payload.Checks {
				if payload.Checks[i].ID == "qa.design.case-set-binding" {
					payload.Checks[i].EvidenceRefs = payload.Checks[i].EvidenceRefs[:1]
				}
			}
		}},
		{name: "stale receipt hash", mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
			mustWrite(t, filepath.Join(dir, "restricted", "proofs", "fixtures", "design-receipt.json"), "tampered\n")
		}},
		{name: "duplicate case ID", mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
			mustWrite(t, filepath.Join(dir, "restricted", "approved-cases.md"), "Case ID: P1-001\nCase ID: P1-001\n")
			for i := range payload.Checks {
				if payload.Checks[i].ID == "qa.design.case-set-binding" {
					payload.Checks[i].EvidenceRefs[0] = testRef(t, dir, "restricted/approved-cases.md")
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := reviewerPolicyFixture(t, dir, policy)
			test.mutate(t, dir, &payload)
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, Stage: policy.Stage, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
			if result.OK() {
				t.Fatal("invalid Design Review case binding was accepted")
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
			mustWrite(t, filepath.Join(dir, "restricted", "approved-cases.md"), test.cases)
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
	payload.QAOwnedResults = testRef(t, dir, "restricted/qa-results.json")
	mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) { root["qaOwnedResults"] = payload.QAOwnedResults })
	payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
}

func rebindApprovedCases(t *testing.T, dir string, payload *QAExecutionPayload) {
	t.Helper()
	payload.ApprovedCaseSet = testRef(t, dir, "restricted/approved-cases.md")
	mutateJSONObject(t, filepath.Join(dir, "restricted", "qa-binding.json"), func(root map[string]any) { root["approvedCaseSet"] = payload.ApprovedCaseSet })
	payload.CaseResultBinding = testRef(t, dir, "restricted/qa-binding.json")
}

func TestReviewerV2ContextBundleRejectsSymlinkAliases(t *testing.T) {
	dir := t.TempDir()
	restricted := filepath.Join(dir, "restricted")
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	if err := os.Symlink("input.txt", filepath.Join(restricted, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	bundle := ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/input.txt"), testRef(t, dir, "restricted/alias.txt")}}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), bundle)
	policy, _ := policyByID("architecture.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, "restricted/bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "restricted/changed.txt")), Verification: ptrRef(testRef(t, dir, "restricted/verification.txt"))}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}, payload)

	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "must not resolve to the same file") {
		t.Fatalf("symlink aliases were accepted: %#v", result.Failures)
	}
}

func TestReviewerRejectsInputsOutsideActiveRestrictedDirectory(t *testing.T) {
	policy, _ := policyByID("architecture.post-development.v2")
	tests := []struct {
		name   string
		mutate func(*testing.T, string, *ReviewerPayload)
	}{
		{
			name: "direct context bundle",
			mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
				mustWrite(t, filepath.Join(dir, "outside-input.txt"), "input")
				writeJSONTest(t, filepath.Join(dir, "outside-bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "outside-input.txt")}})
				payload.ContextBundle = testRef(t, dir, "outside-bundle.json")
			},
		},
		{
			name: "transitive context input",
			mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
				mustWrite(t, filepath.Join(dir, "outside-input.txt"), "{}")
				writeJSONTest(t, filepath.Join(dir, "restricted", "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "outside-input.txt")}})
				payload.ContextBundle = testRef(t, dir, "restricted/bundle.json")
			},
		},
		{
			name: "check evidence",
			mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
				mustWrite(t, filepath.Join(dir, "outside-evidence.txt"), "evidence")
				payload.Checks[0].EvidenceRefs = []EvidenceRef{testRef(t, dir, "outside-evidence.txt")}
			},
		},
		{
			name: "resolved symlink",
			mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
				mustWrite(t, filepath.Join(dir, "outside.txt"), "outside")
				if err := os.Symlink(filepath.Join("..", "outside.txt"), filepath.Join(dir, "restricted", "alias.txt")); err != nil {
					t.Fatal(err)
				}
				writeJSONTest(t, filepath.Join(dir, "restricted", "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/alias.txt")}})
				payload.ContextBundle = testRef(t, dir, "restricted/bundle.json")
			},
		},
		{
			name: "another run",
			mutate: func(t *testing.T, dir string, payload *ReviewerPayload) {
				logical := ".claude/gates/runs/older/restricted/verdict.txt"
				mustWrite(t, filepath.Join(dir, filepath.FromSlash(logical)), "PASS")
				writeJSONTest(t, filepath.Join(dir, "restricted", "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, logical)}})
				payload.ContextBundle = testRef(t, dir, "restricted/bundle.json")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			envelope, payload := reviewerPolicyFixture(t, dir, policy)
			test.mutate(t, dir, &payload)
			writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
			if result.OK() || !strings.Contains(resultSummary(result), "active run restricted directory") {
				t.Fatalf("outside input was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestReviewerPayloadRejectsLegacyDispatchField(t *testing.T) {
	dir := t.TempDir()
	policy, _ := policyByID("architecture.post-development.v2")
	envelope, payload := reviewerPolicyFixture(t, dir, policy)
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), envelope, payload)
	mutateJSONObject(t, filepath.Join(dir, "review.json"), func(root map[string]any) {
		jsonObject(root, "payload")["dispatch"] = map[string]any{"path": "restricted/dispatch.txt", "sha256": strings.Repeat("0", 64)}
	})
	result := Artifact(ArtifactOptions{Root: dir, RunDir: dir, File: "review.json", Gate: policy.Gate, WorkflowID: "wf", ChangeSnapshot: "snap", Flow: policy.Flow})
	if result.OK() || !strings.Contains(resultSummary(result), "unknown field") {
		t.Fatalf("legacy reviewer dispatch field was accepted: %#v", result.Failures)
	}
}

func TestFinalSendPromptRequiresCurrentContextAndRejectsHistoricalFields(t *testing.T) {
	root := t.TempDir()
	writeJSONTest(t, filepath.Join(root, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	valid := finalSendPromptFixture(root, "architecture-health-gate", ".claude/gates/runs/wf/restricted/review.json")
	for _, test := range []struct {
		name   string
		prompt string
	}{
		{name: "missing current requirement", prompt: strings.Replace(valid, "Current requirement: requirements/current.md\n", "", 1)},
		{name: "context bundle supplied", prompt: valid + "Context bundle: other-bundle.json\n"},
		{name: "prior findings field", prompt: valid + "Previous findings: result.md\n"},
		{name: "repair narrative field", prompt: valid + "Repair narrative: repair.md\n"},
		{name: "target conclusion field", prompt: valid + "Target conclusion: PASS\n"},
		{name: "directed focus field", prompt: valid + "Directed focus: validator\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, _ := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: test.prompt, FinalSend: true})
			if result.OK() {
				t.Fatal("invalid final-send prompt was accepted")
			}
		})
	}
}

func TestVerifyClosureRejectsResolvedPathAliases(t *testing.T) {
	dir := t.TempDir()
	restricted := filepath.Join(dir, "restricted")
	mustWrite(t, filepath.Join(restricted, "root.json"), "{}\n")
	mustWrite(t, filepath.Join(restricted, "evidence.txt"), "evidence\n")
	if err := os.Symlink("evidence.txt", filepath.Join(restricted, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	closure := EvidenceClosure{
		SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Verdict: "PASS", RootRole: "COMPLEXITY_REVIEW", RootArtifact: "restricted/root.json", Receipt: "restricted/evidence.txt",
		Entries: []ClosureEntry{
			{Path: "restricted/root.json", SHA256: sha256File(filepath.Join(restricted, "root.json")), References: []string{"restricted/alias.txt", "restricted/evidence.txt"}},
			{Path: "restricted/alias.txt", SHA256: sha256File(filepath.Join(restricted, "alias.txt")), References: []string{}},
			{Path: "restricted/evidence.txt", SHA256: sha256File(filepath.Join(restricted, "evidence.txt")), References: []string{}},
		},
	}
	if err := verifyClosure(ArtifactOptions{Root: dir}, dir, closure); err == nil || !strings.Contains(err.Error(), "resolve to the same file") {
		t.Fatalf("resolved closure aliases were accepted: %v", err)
	}
}

func TestBuildClosureKeepsIdenticalOutputBytesAtDistinctPathsIndependent(t *testing.T) {
	dir := t.TempDir()
	envelope, payload := qaExecutionPolicyFixture(t, dir, "wf", "snap")
	firstPath := filepath.Join(dir, "restricted", "first-qa-execution.json")
	writeEnvelopeTest(t, firstPath, envelope, payload)
	data, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(dir, "restricted", "second-qa-execution.json")
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

	first := build("restricted/" + filepath.Base(firstPath))
	firstBytes, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(first.Path)))
	if err != nil {
		t.Fatal(err)
	}
	second := build("restricted/" + filepath.Base(secondPath))
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
	restricted := filepath.Join(dir, "restricted")
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/input.txt")}})
	policy, _ := policyByID("architecture.post-development.v2")
	baseChecks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		baseChecks = append(baseChecks, ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
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
			payload := ReviewerPayload{ContextBundle: testRef(t, dir, "restricted/bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "restricted/changed.txt")), Verification: ptrRef(testRef(t, dir, "restricted/verification.txt"))}
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
	restricted := filepath.Join(dir, "restricted")
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/input.txt")}})
	policy, _ := policyByID("architecture.post-development.v2")
	baseChecks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		baseChecks = append(baseChecks, ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
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
			payload := ReviewerPayload{ContextBundle: testRef(t, dir, "restricted/bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "restricted/changed.txt")), Verification: ptrRef(testRef(t, dir, "restricted/verification.txt"))}
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

func TestCarryArtifactAcceptsRestrictedMultiHopChain(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:3])
	writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)

	result := Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: fixture.Artifact, Gate: "qa-test-gate", Stage: "Carry", WorkflowID: "wf", ChangeSnapshot: "target"})
	if !result.OK() {
		t.Fatalf("valid Carry artifact failed: %#v", result.Failures)
	}
	fixture.Envelope.Verdict = "BLOCKED"
	fixture.Payload.Decisions[2].Decision = "BLOCKED"
	writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)
	if result = Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: fixture.Artifact, Gate: "qa-test-gate", Stage: "Carry", WorkflowID: "wf", ChangeSnapshot: "target"}); !result.OK() {
		t.Fatalf("valid BLOCKED Carry artifact failed: %#v", result.Failures)
	}

	data, err := os.ReadFile(resolvePath(dir, fixture.Artifact))
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"reviewPolicyId":`), []byte(`"checks": [], "reviewPolicyId":`), 1)
	if err := os.WriteFile(resolvePath(dir, fixture.Artifact), data, 0o600); err != nil {
		t.Fatal(err)
	}
	result = Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: fixture.Artifact, Gate: "qa-test-gate", Stage: "Carry", WorkflowID: "wf", ChangeSnapshot: "target"})
	if result.OK() || !strings.Contains(resultSummary(result), "unknown field") {
		t.Fatalf("Carry payload accepted reviewer checks: %#v", result.Failures)
	}
}

func TestCarryArtifactRejectsRepairHistoryInReviewerPrompt(t *testing.T) {
	root := t.TempDir()
	writeJSONTest(t, filepath.Join(root, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	prompt := finalSendPromptFixture(root, "carry-forward-arbiter", ".claude/gates/runs/wf/restricted/carry.json") + "Repair history: restricted/target/repair-history.txt\n"
	result, _ := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: prompt, FinalSend: true})
	if result.OK() {
		t.Fatalf("Carry reviewer prompt accepted repair history: %#v", result.Failures)
	}
}

func TestCarryArtifactRejectsBrokenChainDecisionAndSourceClosure(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*carryTestFixture)
		want   string
	}{
		{
			name: "non-prefix gates",
			mutate: func(f *carryTestFixture) {
				f.Chain.ProposedCarriedGates[0] = "complexity-gate"
				f.Payload.Decisions[0].Gate = "complexity-gate"
			},
			want: "unique prefix",
		},
		{
			name: "discontinuous hop",
			mutate: func(f *carryTestFixture) {
				f.Chain.Hops[1].FromSnapshot = "omitted-intermediate"
			},
			want: "contiguous",
		},
		{
			name: "source outside chain",
			mutate: func(f *carryTestFixture) {
				f.Payload.Decisions[0].SourceSnapshot = "other-source"
			},
			want: "sourceSnapshot must identify a source hop",
		},
		{
			name: "closure gate mismatch",
			mutate: func(f *carryTestFixture) {
				f.Payload.Decisions[0].SourceGateEvidence = f.Payload.Decisions[1].SourceGateEvidence
			},
			want: "required source-snapshot PASS closure",
		},
		{
			name: "rerun later than rejected gate",
			mutate: func(f *carryTestFixture) {
				f.Envelope.Verdict = "REVIEW"
				f.Payload.Decisions[0].Decision = "RERUN_REQUIRED"
				f.Payload.Decisions[0].RerunFromGate = "complexity-gate"
			},
			want: "same or an earlier fixed gate",
		},
		{
			name: "verdict mismatch",
			mutate: func(f *carryTestFixture) {
				f.Envelope.Verdict = "PASS"
				f.Payload.Decisions[1].Decision = "RERUN_REQUIRED"
				f.Payload.Decisions[1].RerunFromGate = "complexity-gate"
			},
			want: "contradicts Carry decision aggregation",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:3])
			test.mutate(&fixture)
			writeJSONTest(t, filepath.Join(fixture.RunDir, filepath.FromSlash(fixture.ChainPath)), fixture.Chain)
			fixture.Payload.TransitionChain = testRef(t, fixture.RunDir, fixture.ChainPath)
			writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)
			result := Artifact(ArtifactOptions{Root: dir, RunDir: fixture.RunDir, File: fixture.Artifact, Gate: "qa-test-gate", Stage: "Carry", WorkflowID: "wf", ChangeSnapshot: "target"})
			if result.OK() || !strings.Contains(resultSummary(result), test.want) {
				t.Fatalf("invalid Carry artifact was accepted: %#v", result.Failures)
			}
		})
	}
}

func TestCarryDerivesEarliestRerunAndBlockedAggregation(t *testing.T) {
	decisions := []CarryDecision{
		{Decision: "ACCEPT_CARRY"},
		{Decision: "RERUN_REQUIRED", RerunFromGate: "complexity-gate"},
		{Decision: "RERUN_REQUIRED", RerunFromGate: "qa-test-gate"},
	}
	if got := deriveEarliestCarryRerun(decisions); got != "qa-test-gate" {
		t.Fatalf("earliest rerun was not machine-derived: %q", got)
	}
	decisions = append(decisions, CarryDecision{Decision: "BLOCKED"})
	if got := carryAggregateVerdict(decisions); got != "BLOCKED" {
		t.Fatalf("BLOCKED did not dominate Carry aggregation: %q", got)
	}
}

type carryTestFixture struct {
	RunDir, Artifact, ChainPath string
	Envelope                    FormalGateEvidence
	Payload                     CarryPayload
	Chain                       TransitionChain
}

func newCarryTestFixture(t *testing.T, root, workflowID, sourceSnapshot, targetSnapshot string, proposed []string) carryTestFixture {
	t.Helper()
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	base := "restricted/" + targetSnapshot
	if err := os.MkdirAll(filepath.Join(runDir, filepath.FromSlash(base)), 0o700); err != nil {
		t.Fatal(err)
	}
	closures := map[string]EvidenceRef{}
	for _, gate := range proposed {
		closures[gate] = writeCarrySourceClosure(t, root, runDir, workflowID, sourceSnapshot, gate)
	}
	for name, text := range map[string]string{
		base + "/repair-history.txt":       "normal repair history",
		base + "/hop-one-files.txt":        "first change set",
		base + "/hop-one-verification.txt": "first verification",
		base + "/hop-one-repair.txt":       "first repair evidence",
		base + "/hop-two-files.txt":        "second change set",
		base + "/hop-two-verification.txt": "second verification",
		base + "/hop-two-repair.txt":       "second repair evidence",
	} {
		mustWrite(t, filepath.Join(runDir, filepath.FromSlash(name)), text)
	}
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(base), "carry-context.json"), ContextBundle{
		BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: targetSnapshot,
		Inputs: []EvidenceRef{testRef(t, runDir, base+"/repair-history.txt")},
	})
	chain := TransitionChain{
		SchemaVersion: 2, WorkflowID: workflowID, TargetSnapshot: targetSnapshot,
		ProposedCarriedGates: append([]string{}, proposed...),
		Hops: []TransitionHop{
			{FromSnapshot: sourceSnapshot, ToSnapshot: "middle-" + targetSnapshot, ChangedFiles: testRef(t, runDir, base+"/hop-one-files.txt"), Verification: testRef(t, runDir, base+"/hop-one-verification.txt"), RepairEvidence: testRef(t, runDir, base+"/hop-one-repair.txt")},
			{FromSnapshot: "middle-" + targetSnapshot, ToSnapshot: targetSnapshot, ChangedFiles: testRef(t, runDir, base+"/hop-two-files.txt"), Verification: testRef(t, runDir, base+"/hop-two-verification.txt"), RepairEvidence: testRef(t, runDir, base+"/hop-two-repair.txt")},
		},
	}
	chainPath := base + "/transition-chain.json"
	writeJSONTest(t, filepath.Join(runDir, filepath.FromSlash(chainPath)), chain)
	decisions := make([]CarryDecision, 0, len(proposed))
	for _, gate := range proposed {
		decisions = append(decisions, CarryDecision{Gate: gate, SourceSnapshot: sourceSnapshot, SourceGateEvidence: closures[gate], Decision: "ACCEPT_CARRY", RerunFromGate: "", Reason: "The complete chain does not invalidate this gate."})
	}
	payload := CarryPayload{
		ContextBundle:  testRef(t, runDir, base+"/carry-context.json"),
		ReviewPolicyID: "carry.arbiter.v2", TransitionChain: testRef(t, runDir, chainPath), Decisions: decisions,
	}
	artifact := relativePath(root, filepath.Join(runDir, filepath.FromSlash(base), "carry.json"))
	return carryTestFixture{
		RunDir: runDir, Artifact: artifact, ChainPath: chainPath,
		Envelope: FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "CARRY_ARBITER", WorkflowID: workflowID, ChangeSnapshot: targetSnapshot, Gate: "qa-test-gate", Stage: "Carry", Verdict: "PASS"},
		Payload:  payload, Chain: chain,
	}
}

func writeCarrySourceClosure(t *testing.T, root, runDir, workflowID, snapshot, gate string) EvidenceRef {
	t.Helper()
	stage, role := sourceGateContract(gate)
	artifactName := "source-" + gate + ".json"
	restricted := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	artifactPath := filepath.Join(restricted, artifactName)
	var receipt *EvidenceRef
	if gate == "qa-test-gate" {
		envelope, payload := qaExecutionPolicyFixture(t, runDir, workflowID, snapshot)
		writeEnvelopeTest(t, artifactPath, envelope, payload)
	} else {
		policyID := map[string]string{
			"complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2",
		}[gate]
		policy, _ := policyByID(policyID)
		prefix := strings.TrimSuffix(gate, "-gate")
		for name, text := range map[string]string{prefix + "-input.txt": "current requirements", prefix + "-changed.txt": "changed files", prefix + "-verification.txt": "verification"} {
			mustWrite(t, filepath.Join(restricted, name), text)
		}
		writeJSONTest(t, filepath.Join(restricted, prefix+"-bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{testRef(t, runDir, logical(prefix+"-input.txt"))}})
		checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
		for _, id := range policy.RequiredCheckIDs {
			check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
			if id == "complexity.statistics" {
				report := ComplexityReport{Status: "PASS", VCS: "git", Worktree: root, TaskType: "small-feature", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}}
				writeJSONTest(t, filepath.Join(restricted, prefix+"-statistics.json"), report)
				check.EvidenceRefs = []EvidenceRef{testRef(t, runDir, logical(prefix+"-statistics.json"))}
			}
			checks = append(checks, check)
		}
		payload := ReviewerPayload{ContextBundle: testRef(t, runDir, logical(prefix+"-bundle.json")), ReviewPolicyID: policyID, Checks: checks, ChangedFiles: refPtr(testRef(t, runDir, logical(prefix+"-changed.txt"))), Verification: refPtr(testRef(t, runDir, logical(prefix+"-verification.txt")))}
		writeEnvelopeTest(t, artifactPath, FormalGateEvidence{SchemaVersion: 2, ArtifactRole: role, WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS"}, payload)
		value := writeProofReceiptFixture(t, runDir, workflowID, snapshot, gate, stage, logical(artifactName), prefix+"-source")
		receipt = &value
	}
	options := ArtifactOptions{Root: root, RunDir: runDir, File: relativePath(root, artifactPath), Gate: gate, Stage: stage, Flow: "post-development", WorkflowID: workflowID, ChangeSnapshot: snapshot}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	decoded := decodeArtifact(options, data, &result)
	if !result.OK() {
		t.Fatalf("source gate fixture is invalid: %#v", result.Failures)
	}
	closure, err := buildClosure(options, decoded, receipt)
	if err != nil {
		t.Fatal(err)
	}
	return closure
}

func refPtr(ref EvidenceRef) *EvidenceRef { return &ref }

func reviewerPolicyFixture(t *testing.T, dir string, policy ArtifactPolicy) (FormalGateEvidence, ReviewerPayload) {
	t.Helper()
	restricted := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, logical("input.txt"))}})
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		switch id {
		case "complexity.statistics":
			if policy.ID == "complexity.post-development.v2" {
				report := ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}}
				writeJSONTest(t, filepath.Join(restricted, "statistics.json"), report)
				check.EvidenceRefs = []EvidenceRef{testRef(t, dir, logical("statistics.json"))}
			}
		case "qa.design.case-set-binding":
			mustWrite(t, filepath.Join(restricted, "approved-cases.md"), "# Cases\n\nCase ID: P1-001\n\nOracle: approved behavior\n")
			caseSet := testRef(t, dir, logical("approved-cases.md"))
			designReceipt := writeProofReceiptFixture(t, dir, "wf", "snap", "qa-test-gate", "Design", logical("approved-cases.md"), "design")
			check.EvidenceRefs = []EvidenceRef{caseSet, designReceipt}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, logical("bundle.json")), ReviewPolicyID: policy.ID, Checks: checks}
	if policy.ChangedFilesRequired {
		payload.ChangedFiles = ptrRef(testRef(t, dir, logical("changed.txt")))
	}
	if policy.VerificationRequired {
		payload.Verification = ptrRef(testRef(t, dir, logical("verification.txt")))
	}
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS"}
	return envelope, payload
}

func qaExecutionPolicyFixture(t *testing.T, dir, workflowID, snapshot string) (FormalGateEvidence, QAExecutionPayload) {
	t.Helper()
	restricted := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	for name, text := range map[string]string{"qa-changed.txt": "changed", "qa-verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	mustWrite(t, filepath.Join(restricted, "approved-cases.md"), "# Cases\n\nStatus: APPROVED_FOR_EXECUTION\n\n## Login flow\n\nCase ID: P1-001\n\n## Execution notes\n")
	approved := testRef(t, dir, logical("approved-cases.md"))
	designReview := writeDesignReviewClosureFixture(t, dir, workflowID, snapshot+"-design", approved)
	writeJSONTest(t, filepath.Join(restricted, "qa-results.json"), map[string]any{
		"owner": "QA", "workflowId": workflowID, "changeSnapshot": snapshot, "stage": "Execution", "status": "COMPLETE", "overallOutcome": "PASS",
		"executions":  []any{map[string]any{"id": "E-001", "outcome": "PASS", "procedure": "Run the approved case", "result": "The case passed"}},
		"caseResults": []any{map[string]any{"caseId": "P1-001", "status": "PASS", "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	results := testRef(t, dir, logical("qa-results.json"))
	writeJSONTest(t, filepath.Join(restricted, "qa-binding.json"), map[string]any{
		"workflowId": workflowID, "changeSnapshot": snapshot, "approvedCaseSet": approved, "qaOwnedResults": results, "complete": true,
		"bindings": []any{map[string]any{"caseId": "P1-001", "resultPointer": "/caseResults/0", "status": "PASS", "executionRefs": []string{"E-001"}, "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	payload := QAExecutionPayload{
		ApprovedCaseSet: approved, DesignReview: designReview, QAOwnedResults: results, CaseResultBinding: testRef(t, dir, logical("qa-binding.json")),
		ChangedFiles: testRef(t, dir, logical("qa-changed.txt")), Verification: testRef(t, dir, logical("qa-verification.txt")),
	}
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS"}
	return envelope, payload
}

func writeDesignReviewClosureFixture(t *testing.T, dir, workflowID, snapshot string, caseSet EvidenceRef) EvidenceRef {
	t.Helper()
	root := testWorktreeForRunDir(dir)
	restricted := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	for name, text := range map[string]string{"design-review-input.txt": "requirements", "design-review-changed.txt": "unused"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{testRef(t, dir, logical("design-review-input.txt"))}})
	designReceipt := writeProofReceiptFixture(t, dir, workflowID, snapshot, "qa-test-gate", "Design", caseSet.Path, "design")
	policy, _ := policyByID("qa.design-review.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "qa.design.case-set-binding" {
			check.EvidenceRefs = []EvidenceRef{caseSet, designReceipt}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, logical("bundle.json")), ReviewPolicyID: policy.ID, Checks: checks}
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_REVIEW", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS"}
	writeEnvelopeTest(t, filepath.Join(restricted, "design-review.json"), envelope, payload)
	reviewReceipt := writeProofReceiptFixture(t, dir, workflowID, snapshot, "qa-test-gate", "Design Review", logical("design-review.json"), "design-review")
	data, err := os.ReadFile(filepath.Join(restricted, "design-review.json"))
	if err != nil {
		t.Fatal(err)
	}
	options := ArtifactOptions{Root: root, RunDir: dir, File: relativePath(root, filepath.Join(restricted, "design-review.json")), Gate: "qa-test-gate", Stage: "Design Review", Flow: "pre-development", WorkflowID: workflowID, ChangeSnapshot: snapshot}
	var result Result
	decoded := decodeArtifact(options, data, &result)
	if !result.OK() {
		t.Fatalf("Design Review fixture is invalid: %#v", result.Failures)
	}
	closure, err := buildClosure(options, decoded, &reviewReceipt)
	if err != nil {
		t.Fatal(err)
	}
	return closure
}

func TestDesignReviewRejectsDesignerAsReviewer(t *testing.T) {
	dir := t.TempDir()
	root := testWorktreeForRunDir(dir)
	restricted := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	mustWrite(t, filepath.Join(restricted, "cases.md"), "# Cases\n\nCase ID: P2-001\n")
	caseSet := testRef(t, dir, logical("cases.md"))
	designReceipt := writeProofReceiptFixtureWithSubagent(t, dir, "wf", "snap", "qa-test-gate", "Design", caseSet.Path, "design-same", "same-agent")
	mustWrite(t, filepath.Join(restricted, "input.txt"), "requirements")
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, logical("input.txt"))}})
	policy, _ := policyByID("qa.design-review.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "qa.design.case-set-binding" {
			check.EvidenceRefs = []EvidenceRef{caseSet, designReceipt}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, logical("bundle.json")), ReviewPolicyID: policy.ID, Checks: checks}
	path := filepath.Join(restricted, "review.json")
	writeEnvelopeTest(t, path, FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_REVIEW", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS"}, payload)
	reviewReceipt := writeProofReceiptFixtureWithSubagent(t, dir, "wf", "snap", "qa-test-gate", "Design Review", logical("review.json"), "review-same", "same-agent")
	options := ArtifactOptions{Root: root, RunDir: dir, File: relativePath(root, path), Gate: "qa-test-gate", Stage: "Design Review", Flow: "pre-development", WorkflowID: "wf", ChangeSnapshot: "snap"}
	data, _ := os.ReadFile(path)
	var result Result
	decoded := decodeArtifact(options, data, &result)
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	if err := validateDesignReviewIndependence(options, decoded, reviewReceipt); err == nil || !strings.Contains(err.Error(), "different subagents") {
		t.Fatalf("same designer/reviewer was accepted: %v", err)
	}
}

func writeProofReceiptFixture(t *testing.T, dir, workflowID, snapshot, gate, stage, artifact, prefix string) EvidenceRef {
	return writeProofReceiptFixtureWithSubagent(t, dir, workflowID, snapshot, gate, stage, artifact, prefix, prefix+"-agent")
}

func writeProofReceiptFixtureWithSubagent(t *testing.T, dir, workflowID, snapshot, gate, stage, artifact, prefix, subagentID string) EvidenceRef {
	t.Helper()
	root := testWorktreeForRunDir(dir)
	proofDir := filepath.Join(dir, "restricted", "proofs", "fixtures")
	logical := func(name string) string {
		return filepath.ToSlash(filepath.Join("restricted", "proofs", "fixtures", name))
	}
	dispatchName := prefix + "-dispatch-registration.json"
	receiptName := prefix + "-receipt.json"
	startName := prefix + "-start.json"
	stopName := prefix + "-stop.json"
	dispatchPath := relativePath(root, filepath.Join(proofDir, dispatchName))
	receiptPath := relativePath(root, filepath.Join(proofDir, receiptName))
	startPath := relativePath(root, filepath.Join(proofDir, startName))
	stopPath := relativePath(root, filepath.Join(proofDir, stopName))
	artifactPath := relativePath(root, filepath.Join(dir, filepath.FromSlash(artifact)))
	writeJSONTest(t, filepath.Join(root, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	promptPath, promptHash := "", ""
	if reviewJudgmentLifecycle(gate, stage) {
		promptName := prefix + "-final-send.txt"
		promptAbs := filepath.Join(proofDir, promptName)
		mustWrite(t, promptAbs, finalSendPromptFixture(root, expectedDispatchRole(gate, stage), artifactPath))
		promptPath, promptHash = relativePath(root, promptAbs), sha256File(promptAbs)
	}
	dispatchID := prefix + "-dispatch-id"
	dispatch := dispatchRegistration{ProofVersion: 1, DispatchID: dispatchID, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, ReviewArtifact: artifactPath, PromptArtifact: promptPath, PromptSha256: promptHash, ReceiptArtifact: receiptPath, Status: "finalized"}
	writeJSONTest(t, filepath.Join(proofDir, dispatchName), dispatch)
	for name, event := range map[string]string{startName: "subagent_start", stopName: "subagent_stop"} {
		writeJSONTest(t, filepath.Join(proofDir, name), receiptEventRecord{Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, NormalizedEvent: event, RawEventName: map[string]string{"subagent_start": "SubagentStart", "subagent_stop": "SubagentStop"}[event], SubagentID: subagentID, Status: "completed", DispatchID: dispatchID, DispatchRegistrationArtifact: dispatchPath, CapturedAtUTC: "2026-07-17T00:00:00Z"})
	}
	receipt := reviewerProofReceipt{ProofVersion: 1, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, DispatchID: dispatchID, DispatchRegistrationArtifact: dispatchPath, DispatchRegistrationSha256: sha256File(filepath.Join(proofDir, dispatchName)), SubagentID: subagentID, NormalizedEvents: []string{"subagent_start", "subagent_stop"}, StartEventArtifact: startPath, StartEventSha256: sha256File(filepath.Join(proofDir, startName)), StopEventArtifact: stopPath, StopEventSha256: sha256File(filepath.Join(proofDir, stopName)), ReviewArtifact: artifactPath, ReviewArtifactSha256: sha256File(filepath.Join(dir, filepath.FromSlash(artifact))), PromptArtifact: promptPath, PromptSha256: promptHash}
	writeJSONTest(t, filepath.Join(proofDir, receiptName), receipt)
	return testRef(t, dir, logical(receiptName))
}

func testWorktreeForRunDir(dir string) string {
	current := filepath.Clean(dir)
	if filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(current)))) == ".claude" {
		return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(current))))
	}
	return current
}

func writeFinalExecutionPolicyFixture(t *testing.T, dir string) {
	t.Helper()
	restricted := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	matrix := make([]FinalGateRow, 0, len(postDevelopmentGateOrder))
	for _, gate := range postDevelopmentGateOrder {
		root := gate + "-root.json"
		mustWrite(t, filepath.Join(restricted, root), "{}\n")
		closure := EvidenceClosure{
			SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: gate, Stage: map[string]string{"qa-test-gate": "Execution"}[gate], Verdict: "PASS",
			RootRole:     map[string]string{"qa-test-gate": "QA_EXECUTION", "complexity-gate": "COMPLEXITY_REVIEW", "architecture-health-gate": "ARCHITECTURE_REVIEW", "code-quality-gate": "CODE_QUALITY_REVIEW"}[gate],
			RootArtifact: logical(root), Entries: []ClosureEntry{{Path: logical(root), SHA256: sha256File(filepath.Join(restricted, root)), References: []string{}}},
		}
		if gate != "qa-test-gate" {
			receipt := gate + "-receipt.json"
			mustWrite(t, filepath.Join(restricted, receipt), "{}\n")
			closure.Receipt = logical(receipt)
			closure.Entries = append(closure.Entries, ClosureEntry{Path: logical(receipt), SHA256: sha256File(filepath.Join(restricted, receipt)), References: []string{}})
		}
		closurePath := gate + "-closure.json"
		writeJSONTest(t, filepath.Join(restricted, closurePath), closure)
		matrix = append(matrix, FinalGateRow{Gate: gate, ResultKind: "FRESH_PASS", SourceSnapshot: "snap", TargetSnapshot: "snap", GateEvidence: testRef(t, dir, logical(closurePath))})
	}
	attempt := WorkflowFinalVerificationAttempt{"name": "go test", "status": "PASS"}
	writeJSONTest(t, filepath.Join(restricted, "final-verification.json"), WorkflowFinalVerificationArtifact{SchemaVersion: 2, WorkflowID: "wf", ChangeSnapshot: "snap", Status: "PASS", Attempts: []WorkflowFinalVerificationAttempt{attempt}, AcceptedAttempts: []WorkflowFinalVerificationAttempt{attempt}})
	payload := FinalExecutionPayload{Mode: "MECHANICAL_CLOSEOUT", GateMatrix: matrix, FinalVerification: testRef(t, dir, logical("final-verification.json")), ReleaseJudgment: "SEAL"}
	writeEnvelopeTest(t, filepath.Join(restricted, "final-execution.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "FINAL_EXECUTION", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "FinalExecution", Verdict: "PASS"}, payload)
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
	baseDir := dir
	logicalPrefix := ""
	if strings.Contains(filepath.ToSlash(dir), "/.claude/gates/runs/") {
		baseDir = filepath.Join(dir, "restricted")
		logicalPrefix = "restricted/"
	}
	alignmentPath := logicalPrefix + prefix + "alignment.json"
	decisionPath := logicalPrefix + prefix + "decision.json"
	alignment := AlignmentArtifact{SchemaVersion: 2, WorkflowID: workflow, ChangeSnapshot: snapshot, Items: []AlignmentItem{{ID: "RQ-001", RequirementOrQuestion: "What", Source: "user", WhyItMatters: "value", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "implement", DocumentImpact: "docs/requirements.md", EvidenceNeeded: "tests"}}}
	writeJSONTest(t, filepath.Join(baseDir, prefix+"alignment.json"), alignment)
	alignmentRef := testRef(t, dir, alignmentPath)
	decision := RequirementsDecision{SchemaVersion: 2, WorkflowID: workflow, ChangeSnapshot: snapshot, DecisionType: "USER_CONFIRMATION", UserConfirmation: true, UserOriginal: "approved", Alignment: alignmentRef, ApprovedAlignmentIDs: []string{"RQ-001"}, ApprovedDroppedIDs: []string{}, ApprovalScope: "requirements-clarification-gate"}
	writeJSONTest(t, filepath.Join(baseDir, prefix+"decision.json"), decision)
	dimensions := make([]DimensionCoverage, 0, len(dimensionIDs))
	for _, id := range dimensionIDs {
		dimensions = append(dimensions, DimensionCoverage{ID: id, Status: "COVERED", AlignmentIDs: []string{"RQ-001"}, Message: ""})
	}
	payload := RequirementsPayload{RequirementSource: "brief", Alignment: alignmentRef, TotalAlignmentItems: 1, PreviousAlignment: previous, OpenQuestionIDs: []string{}, OpenBlockers: []string{}, DroppedQuestionIDs: []string{}, DroppedQuestionApproval: false, UserConfirmation: true, CoverageScan: "PASS", ScopePreservation: PassOrNA{Status: "PASS", Message: ""}, TaskProof: PassOrNA{Status: "PASS", Message: ""}, DimensionCoverage: dimensions, Decision: testRef(t, dir, decisionPath), CoveredTargets: []string{"docs/requirements.md"}, DownstreamPermission: "READY_TO_DRAFT"}
	path := filepath.Join(baseDir, prefix+"requirements.json")
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
