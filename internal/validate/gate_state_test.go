package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateRecordInitializesStateAndStoresPass(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		Actor:          "qa",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected record to pass, got %#v", result.Failures)
	}
	if !isFile(filepath.Join(dir, ".claude", "gates", "gate-state.json")) {
		t.Fatal("expected missing state file to be initialized")
	}

	state, show := GateShow(GateShowOptions{Worktree: dir})
	if !show.OK() {
		t.Fatalf("expected show to pass, got %#v", show.Failures)
	}
	entry := state.Gates["qa-test-gate"]
	if entry.Verdict != "PASS" || entry.WorkflowID != "wf" || entry.ChangeSnapshot != "snap" {
		t.Fatalf("unexpected state entry: %#v", entry)
	}
	if entry.ArtifactHash == "" {
		t.Fatal("expected artifact hash to be recorded")
	}
	data, err := os.ReadFile(resolvePath(dir, entry.Artifact))
	if err != nil {
		t.Fatal(err)
	}
	var closure EvidenceClosure
	if err := json.Unmarshal(data, &closure); err != nil {
		t.Fatal(err)
	}
	if closure.RootRole != "QA_EXECUTION" || closure.Receipt != "" {
		t.Fatalf("QA Execution closure must not contain a reviewer receipt: %#v", closure)
	}
}

func TestGateRecordAllowsRequirementsClarificationPass(t *testing.T) {
	dir := t.TempDir()
	writeRequirementsArtifact(t, dir, "wf", "snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "requirements-clarification-gate",
		Verdict:        "PASS",
		Artifact:       "requirements.md",
		Actor:          "requirements",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected requirements clarification record to pass, got %#v", result.Failures)
	}
	state, show := GateShow(GateShowOptions{Worktree: dir})
	if !show.OK() {
		t.Fatalf("expected show to pass, got %#v", show.Failures)
	}
	entry := state.Gates["requirements-clarification-gate"]
	if entry.Verdict != "PASS" || !strings.Contains(entry.Artifact, "closures/") {
		t.Fatalf("unexpected requirements entry: %#v", entry)
	}
}

func TestGateVerifyAdmissionBlocksMissingPrerequisite(t *testing.T) {
	dir := t.TempDir()
	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected missing QA prerequisite to block")
	}
	if !strings.Contains(result.Failures[0].Message, "missing prerequisite gate=qa-test-gate") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionAllowsSameWorkflowSnapshotPrerequisite(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected complexity admission to pass, got %#v", result.Failures)
	}
	result = GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Mode:           "formal",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected formal mode to select post-development policy, got %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionRejectsUnsupportedFlowWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	statePath := filepath.Join(dir, ".claude", "gates", "gate-state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Mode:           "start-readiness",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() || !strings.Contains(result.Failures[0].Message, "unsupported admission policy gate=qa-test-gate flow=start-readiness") {
		t.Fatalf("unsupported flow was not rejected: %#v", result.Failures)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unsupported admission mutated authoritative state")
	}
}

func TestGateRecordModeMatrix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "artifact.json"), "{}\n")
	for _, test := range []struct {
		gate   string
		stage  string
		mode   string
		accept bool
	}{
		{gate: "requirements-clarification-gate", mode: "", accept: true},
		{gate: "requirements-clarification-gate", mode: "requirements", accept: true},
		{gate: "requirements-clarification-gate", mode: "formal"},
		{gate: "qa-test-gate", stage: "Execution", mode: "formal", accept: true},
		{gate: "qa-test-gate", stage: "Execution", mode: ""},
		{gate: "qa-test-gate", stage: "Execution", mode: "post-development"},
		{gate: "complexity-gate", mode: "", accept: true},
		{gate: "complexity-gate", mode: "formal", accept: true},
		{gate: "complexity-gate", mode: "start-readiness", accept: true},
		{gate: "complexity-gate", mode: "arbitrary"},
		{gate: "architecture-health-gate", mode: "", accept: true},
		{gate: "architecture-health-gate", mode: "formal", accept: true},
		{gate: "architecture-health-gate", mode: "start-readiness", accept: true},
		{gate: "architecture-health-gate", mode: "arbitrary"},
		{gate: "code-quality-gate", mode: "", accept: true},
		{gate: "code-quality-gate", mode: "formal", accept: true},
		{gate: "code-quality-gate", mode: "start-readiness"},
		{gate: "code-quality-gate", mode: "arbitrary"},
	} {
		name := test.gate + "_" + test.stage + "_" + test.mode
		t.Run(name, func(t *testing.T) {
			var result Result
			err := validateGateRecordOptions(dir, GateRecordOptions{Gate: test.gate, Stage: test.stage, Mode: test.mode, Verdict: "PASS", Artifact: "artifact.json", WorkflowID: "wf", ChangeSnapshot: "snap"}, &result)
			accepted := err == nil && result.OK()
			if accepted != test.accept {
				t.Fatalf("accept=%v err=%v failures=%#v", test.accept, err, result.Failures)
			}
		})
	}
}

func TestGateRecordAllowsStartReadinessComplexityAfterRequirements(t *testing.T) {
	dir := t.TempDir()
	writeRequirementsArtifact(t, dir, "wf", "snap")
	recordRequirements := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "requirements-clarification-gate",
		Verdict:        "PASS",
		Artifact:       "requirements.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordRequirements.OK() {
		t.Fatalf("expected requirements record to pass, got %#v", recordRequirements.Failures)
	}
	complexityArtifact := writeGateArtifactMode(t, dir, "complexity-gate", "", "wf", "snap", "start-readiness")

	recordComplexity := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Mode:           "start-readiness",
		Artifact:       complexityArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordComplexity.OK() {
		t.Fatalf("expected start-readiness complexity record to pass, got %#v", recordComplexity.Failures)
	}

	admission := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "architecture-health-gate",
		Mode:           "start-readiness",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !admission.OK() {
		t.Fatalf("expected start-readiness architecture admission to pass, got %#v", admission.Failures)
	}
}

func TestGateRecordBlocksStartReadinessArchitectureAfterFormalComplexity(t *testing.T) {
	dir := t.TempDir()
	writeRequirementsArtifact(t, dir, "wf", "snap")
	recordRequirements := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "requirements-clarification-gate",
		Verdict:        "PASS",
		Artifact:       "requirements.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordRequirements.OK() {
		t.Fatalf("expected requirements record to pass, got %#v", recordRequirements.Failures)
	}
	qaArtifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	recordQA := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       qaArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordQA.OK() {
		t.Fatalf("expected QA record to pass, got %#v", recordQA.Failures)
	}
	complexityArtifact := writeGateArtifact(t, dir, "complexity-gate", "", "wf", "snap")
	recordComplexity := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Artifact:       complexityArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordComplexity.OK() {
		t.Fatalf("expected formal complexity record to pass, got %#v", recordComplexity.Failures)
	}

	admission := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "architecture-health-gate",
		Mode:           "start-readiness",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if admission.OK() {
		t.Fatal("expected start-readiness architecture to require start-readiness complexity")
	}
	if !strings.Contains(admission.Failures[0].Message, "requiredMode=start-readiness") {
		t.Fatalf("unexpected failure: %#v", admission.Failures)
	}
}

func TestGateRecordRejectsWorkflowSnapshotMismatch(t *testing.T) {
	dir := t.TempDir()
	qaArtifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf-a", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       qaArtifact,
		WorkflowID:     "wf-a",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	complexityArtifact := writeGateArtifact(t, dir, "complexity-gate", "", "wf-b", "snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Artifact:       complexityArtifact,
		WorkflowID:     "wf-b",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected mismatched workflow prerequisite to block")
	}
	if !strings.Contains(result.Failures[0].Message, "missing route gate=qa-test-gate") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionBlocksArtifactHashMismatch(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       artifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	mustWrite(t, resolvePath(dir, artifact), "tampered")

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected artifact hash mismatch to block")
	}
	if !strings.Contains(result.Failures[0].Message, "closure entry hash mismatch") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionRejectsReceiptDependencyTampering(t *testing.T) {
	for _, dependency := range []string{"dispatch", "start", "stop"} {
		t.Run(dependency, func(t *testing.T) {
			dir := t.TempDir()
			qaArtifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
			if record := GateRecord(GateRecordOptions{Worktree: dir, Gate: "qa-test-gate", Verdict: "PASS", Mode: "formal", Stage: "Execution", Artifact: qaArtifact, WorkflowID: "wf", ChangeSnapshot: "snap"}); !record.OK() {
				t.Fatal(record.Failures)
			}
			complexityArtifact := writeGateArtifact(t, dir, "complexity-gate", "", "wf", "snap")
			if record := GateRecord(GateRecordOptions{Worktree: dir, Gate: "complexity-gate", Verdict: "PASS", Mode: "formal", Artifact: complexityArtifact, WorkflowID: "wf", ChangeSnapshot: "snap"}); !record.OK() {
				t.Fatal(record.Failures)
			}
			state, shown := GateShow(GateShowOptions{Worktree: dir})
			if !shown.OK() {
				t.Fatal(shown.Failures)
			}
			data, err := os.ReadFile(resolvePath(dir, state.Gates["complexity-gate"].Artifact))
			if err != nil {
				t.Fatal(err)
			}
			var closure EvidenceClosure
			if err := json.Unmarshal(data, &closure); err != nil {
				t.Fatal(err)
			}
			var receiptEntry ClosureEntry
			for _, entry := range closure.Entries {
				if entry.Path == closure.Receipt {
					receiptEntry = entry
				}
			}
			if len(receiptEntry.References) != 3 || contains(receiptEntry.References, closure.RootArtifact) {
				t.Fatalf("receipt dependencies are incomplete or cyclic: %#v", receiptEntry.References)
			}
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			receiptPath, err := safeEvidencePath(runDir, closure.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			receiptData, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			var receipt reviewerProofReceipt
			if err := json.Unmarshal(receiptData, &receipt); err != nil {
				t.Fatal(err)
			}
			path := map[string]string{"dispatch": receipt.DispatchRegistrationArtifact, "start": receipt.StartEventArtifact, "stop": receipt.StopEventArtifact}[dependency]
			logical, err := logicalPathInRun(runDir, resolvePath(dir, path))
			if err != nil || !contains(receiptEntry.References, logical) {
				t.Fatalf("closure is missing %s dependency %q: %v", dependency, logical, err)
			}
			mustWrite(t, resolvePath(dir, path), "tampered\n")
			admission := GateVerifyAdmission(GateAdmissionOptions{Worktree: dir, Gate: "architecture-health-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
			if admission.OK() || !strings.Contains(resultSummary(admission), "closure entry hash mismatch") {
				t.Fatalf("%s tampering passed downstream admission: %#v", dependency, admission.Failures)
			}
		})
	}
}

func writeRequirementsArtifact(t *testing.T, dir, workflowID, snapshot string) {
	t.Helper()
	writeV2RequirementsFixture(t, dir, workflowID, snapshot)
	data, err := os.ReadFile(filepath.Join(dir, "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "requirements.md"), string(data))
}

func writeGateArtifact(t *testing.T, dir, gate, stage, workflowID, snapshot string) string {
	return writeGateArtifactMode(t, dir, gate, stage, workflowID, snapshot, "post-development")
}

func writeGateArtifactMode(t *testing.T, dir, gate, stage, workflowID, snapshot, flow string) string {
	t.Helper()
	runDir, err := resolveWorkflowRunDir(dir, workflowID, "")
	if err != nil {
		t.Fatal(err)
	}
	prefix := strings.ReplaceAll(gate, "-gate", "")
	if gate == "qa-test-gate" {
		envelope, payload := qaExecutionPolicyFixture(t, runDir, workflowID, snapshot)
		path := filepath.Join(runDir, gate+".md")
		writeEnvelopeTest(t, path, envelope, payload)
		return relativePath(dir, path)
	}
	for name, text := range map[string]string{prefix + "-input.txt": "input", prefix + "-dispatch.txt": "dispatch", prefix + "-changed.txt": "changed", prefix + "-verification.txt": "verified"} {
		mustWrite(t, filepath.Join(runDir, name), text)
	}
	bundleName := prefix + "-bundle.json"
	writeJSONTest(t, filepath.Join(runDir, bundleName), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{testRef(t, runDir, prefix+"-input.txt")}})
	policyID := map[string]string{"qa-test-gate": "qa.execution.v2", "complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2"}[gate]
	if flow == "start-readiness" {
		policyID = map[string]string{"complexity-gate": "complexity.start-readiness.v2", "architecture-health-gate": "architecture.start-readiness.v2"}[gate]
	}
	policy, ok := policyByID(policyID)
	if !ok {
		t.Fatal("missing test policy")
	}
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	statisticsName := prefix + "-statistics.json"
	writeJSONTest(t, filepath.Join(runDir, statisticsName), ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}})
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: "checked", EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "complexity.statistics" && flow == "start-readiness" {
			check.Status = "NOT_APPLICABLE"
			check.Message = "not needed before development"
		}
		if id == "complexity.statistics" && flow == "post-development" {
			check.EvidenceRefs = []EvidenceRef{testRef(t, runDir, statisticsName)}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{Dispatch: testRef(t, runDir, prefix+"-dispatch.txt"), ContextBundle: testRef(t, runDir, bundleName), ReviewPolicyID: policy.ID, Checks: checks}
	if policy.ChangedFilesRequired {
		payload.ChangedFiles = ptrRef(testRef(t, runDir, prefix+"-changed.txt"))
		payload.Verification = ptrRef(testRef(t, runDir, prefix+"-verification.txt"))
	}
	role := policy.ArtifactRole
	path := filepath.Join(runDir, gate+".md")
	artifact := relativePath(dir, path)
	register, rr := ReceiptRegisterDispatch(ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Artifact: artifact, ContextBundle: bundleName})
	if !rr.OK() {
		t.Fatal(rr.Failures)
	}
	eventPayload := func(event string) []byte {
		data, _ := json.Marshal(map[string]any{"provider": "codex", "eventName": event, "workflowId": workflowID, "changeSnapshot": snapshot, "gate": gate, "stage": stage, "subagentId": prefix + "-agent", "dispatchId": register.DispatchID, "dispatchRegistrationArtifact": register.DispatchRegistrationArtifact, "status": "completed"})
		return data
	}
	if _, captured := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: eventPayload("SubagentStart")}); !captured.OK() {
		t.Fatal(captured.Failures)
	}
	writeEnvelopeTest(t, path, FormalGateEvidence{SchemaVersion: 2, ArtifactRole: role, WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS"}, payload)
	if _, captured := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: eventPayload("SubagentStop")}); !captured.OK() {
		t.Fatal(captured.Failures)
	}
	if _, finalized := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, Gate: gate, Stage: stage, Artifact: artifact}); !finalized.OK() {
		t.Fatal(finalized.Failures)
	}
	return artifact
}
