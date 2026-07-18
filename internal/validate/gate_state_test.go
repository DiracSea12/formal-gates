package validate

import (
	"bytes"
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
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	statePath := filepath.Join(runDir, "restricted", "gate-state.json")
	if !isFile(statePath) {
		t.Fatal("expected missing state file to be initialized")
	}

	state, show := GateShow(GateShowOptions{Worktree: dir, StatePath: relativePath(dir, statePath)})
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

func TestGateRecordDesignReviewRequiresRequirementsAndStoresReceiptClosure(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	_, _, err := writeCanaryDesignReviewClosure(dir, runDir, "wf", "design-snap")
	if err != nil {
		t.Fatal(err)
	}
	artifact := relativePath(dir, filepath.Join(runDir, "restricted", "design-review.json"))
	options := GateRecordOptions{Worktree: dir, RunDir: runDir, Gate: "qa-test-gate", Stage: "Design Review", Mode: "pre-development", Verdict: "PASS", Artifact: artifact, WorkflowID: "wf", ChangeSnapshot: "design-snap"}
	if result := GateRecord(options); result.OK() || !strings.Contains(resultSummary(result), "requirements-clarification-gate") {
		t.Fatalf("Design Review without requirements PASS was accepted: %#v", result.Failures)
	}
	writeV2RequirementsFixture(t, runDir, "wf", "design-snap")
	requirementsArtifact := relativePath(dir, filepath.Join(runDir, "restricted", "requirements.json"))
	if result := GateRecord(GateRecordOptions{Worktree: dir, RunDir: runDir, Gate: "requirements-clarification-gate", Verdict: "PASS", Artifact: requirementsArtifact, WorkflowID: "wf", ChangeSnapshot: "design-snap"}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if result := GateRecord(options); !result.OK() {
		t.Fatalf("valid Design Review record failed: %#v", result.Failures)
	}
	state, _ := GateShow(GateShowOptions{Worktree: dir, StatePath: relativePath(dir, filepath.Join(runDir, "restricted", "gate-state.json"))})
	entry := state.Gates["qa-test-gate"]
	if entry.Mode != "pre-development" || entry.Stage != "Design Review" {
		t.Fatalf("unexpected Design Review state entry: %#v", entry)
	}
	data, _ := os.ReadFile(resolvePath(dir, entry.Artifact))
	var closure EvidenceClosure
	if err := json.Unmarshal(data, &closure); err != nil || closure.RootRole != "QA_REVIEW" || closure.Receipt == "" {
		t.Fatalf("Design Review did not store a receipt-bound closure: %#v err=%v", closure, err)
	}
}

func TestGateRecordTransitionStoresReceiptClosureAndRejectsConflict(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)
	missingReceipt := GateRecordTransition(WorkflowRecordTransitionOptions{Worktree: dir, RunDir: fixture.RunDir, Artifact: fixture.Artifact, WorkflowID: "wf", ChangeSnapshot: "target"})
	if missingReceipt.OK() || !strings.Contains(resultSummary(missingReceipt), "matching finalized receipt is missing") {
		t.Fatalf("receipt-free Carry transition was accepted: %#v", missingReceipt.Failures)
	}
	if err := os.Remove(resolvePath(dir, fixture.Artifact)); err != nil {
		t.Fatal(err)
	}
	finalizeCarryTestArtifact(t, dir, fixture)
	statePath := relativePath(dir, filepath.Join(fixture.RunDir, "restricted", "gate-state.json"))
	options := WorkflowRecordTransitionOptions{Worktree: dir, StatePath: statePath, RunDir: fixture.RunDir, Artifact: fixture.Artifact, WorkflowID: "wf", ChangeSnapshot: "target"}
	if result := GateRecordTransition(options); !result.OK() {
		t.Fatalf("valid Carry transition failed: %#v", result.Failures)
	}
	state, _ := GateShow(GateShowOptions{Worktree: dir, StatePath: statePath})
	if len(state.Transitions) != 1 || len(state.Gates) != 0 || len(state.History) != 0 {
		t.Fatalf("Carry must create one non-gate transition record: %#v", state)
	}
	transition := state.Transitions[0]
	data, err := os.ReadFile(resolvePath(dir, transition.ArbiterClosure))
	if err != nil {
		t.Fatal(err)
	}
	var closure EvidenceClosure
	if strictContractJSON(data, &closure) != nil || closure.RootRole != "CARRY_ARBITER" || closure.Receipt == "" || transition.ArbiterClosureHash != sha256File(resolvePath(dir, transition.ArbiterClosure)) {
		t.Fatalf("transition is not bound to an Arbiter receipt closure: %#v", transition)
	}
	before, _ := os.ReadFile(resolvePath(dir, statePath))
	if result := GateRecordTransition(options); !result.OK() {
		t.Fatalf("identical transition was not reusable: %#v", result.Failures)
	}
	after, _ := os.ReadFile(resolvePath(dir, statePath))
	if !bytes.Equal(before, after) {
		t.Fatal("identical transition rewrote authoritative state")
	}

	conflict := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	conflict.Artifact = relativePath(dir, filepath.Join(conflict.RunDir, "restricted", "target", "carry-conflict.json"))
	conflict.Payload.Decisions[0].Reason = "A different accepted judgment."
	finalizeCarryTestArtifact(t, dir, conflict)
	options.Artifact = conflict.Artifact
	if result := GateRecordTransition(options); result.OK() || !strings.Contains(resultSummary(result), "conflicting Carry transition") {
		t.Fatalf("conflicting transition was accepted: %#v", result.Failures)
	}
	after, _ = os.ReadFile(resolvePath(dir, statePath))
	if !bytes.Equal(before, after) {
		t.Fatal("conflicting transition changed authoritative state")
	}

	next := newCarryTestFixture(t, dir, "wf", "source", "target-2", postDevelopmentGateOrder[:1])
	finalizeCarryTestArtifact(t, dir, next)
	options.Artifact, options.ChangeSnapshot = next.Artifact, "target-2"
	if result := GateRecordTransition(options); !result.OK() {
		t.Fatalf("new target transition failed: %#v", result.Failures)
	}
	state, _ = GateShow(GateShowOptions{Worktree: dir, StatePath: statePath})
	if len(state.Transitions) != 2 || state.Transitions[1].ChangeSnapshot != "target-2" {
		t.Fatalf("new target did not get a fresh transition: %#v", state.Transitions)
	}
}

func TestGateRecordTransitionReportsMachineDerivedRerunWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:2])
	fixture.Envelope.Verdict = "REVIEW"
	fixture.Payload.Decisions[1].Decision, fixture.Payload.Decisions[1].RerunFromGate = "RERUN_REQUIRED", "complexity-gate"
	writeEnvelopeTest(t, resolvePath(dir, fixture.Artifact), fixture.Envelope, fixture.Payload)
	result := GateRecordTransition(WorkflowRecordTransitionOptions{Worktree: dir, RunDir: fixture.RunDir, Artifact: fixture.Artifact, WorkflowID: "wf", ChangeSnapshot: "target"})
	if result.OK() || !strings.Contains(resultSummary(result), "earliestRerunGate=complexity-gate") {
		t.Fatalf("derived rerun was not returned: %#v", result.Failures)
	}
	if isFile(filepath.Join(fixture.RunDir, "restricted", "gate-state.json")) {
		t.Fatal("rejected Carry decision changed state")
	}
}

func TestGateVerifyAdmissionUsesOnlyAcceptedCurrentTargetCarry(t *testing.T) {
	dir := t.TempDir()
	fixture := newCarryTestFixture(t, dir, "wf", "source", "target", postDevelopmentGateOrder[:1])
	finalizeCarryTestArtifact(t, dir, fixture)
	statePath := relativePath(dir, filepath.Join(fixture.RunDir, "restricted", "gate-state.json"))
	admission := GateAdmissionOptions{Worktree: dir, StatePath: statePath, RunDir: fixture.RunDir, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "target"}
	if result := GateVerifyAdmission(admission); result.OK() {
		t.Fatal("admission passed before the accepted Carry transition was recorded")
	}
	if result := GateRecordTransition(WorkflowRecordTransitionOptions{Worktree: dir, StatePath: statePath, RunDir: fixture.RunDir, Artifact: fixture.Artifact, WorkflowID: "wf", ChangeSnapshot: "target"}); !result.OK() {
		t.Fatalf("accepted Carry transition failed: %#v", result.Failures)
	}
	if result := GateVerifyAdmission(admission); !result.OK() {
		t.Fatalf("accepted Carry did not satisfy admission: %#v", result.Failures)
	}
	admission.ChangeSnapshot = "next-target"
	if result := GateVerifyAdmission(admission); result.OK() {
		t.Fatal("stale-target Carry satisfied admission")
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
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	statePath := filepath.Join(runDir, "restricted", "gate-state.json")
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
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	writeRequirementsArtifact(t, runDir, "wf", "snap")
	requirementsArtifact := relativePath(dir, filepath.Join(runDir, "restricted", "requirements.md"))
	recordRequirements := GateRecord(GateRecordOptions{
		Worktree:       dir,
		RunDir:         runDir,
		Gate:           "requirements-clarification-gate",
		Verdict:        "PASS",
		Artifact:       requirementsArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordRequirements.OK() {
		t.Fatalf("expected requirements record to pass, got %#v", recordRequirements.Failures)
	}
	complexityArtifact := writeGateArtifactMode(t, dir, "complexity-gate", "", "wf", "snap", "start-readiness")

	recordComplexity := GateRecord(GateRecordOptions{
		Worktree:       dir,
		RunDir:         runDir,
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
		RunDir:         runDir,
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
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	writeRequirementsArtifact(t, runDir, "wf", "snap")
	requirementsArtifact := relativePath(dir, filepath.Join(runDir, "restricted", "requirements.md"))
	recordRequirements := GateRecord(GateRecordOptions{
		Worktree:       dir,
		RunDir:         runDir,
		Gate:           "requirements-clarification-gate",
		Verdict:        "PASS",
		Artifact:       requirementsArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordRequirements.OK() {
		t.Fatalf("expected requirements record to pass, got %#v", recordRequirements.Failures)
	}
	qaArtifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	recordQA := GateRecord(GateRecordOptions{
		Worktree:       dir,
		RunDir:         runDir,
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
		RunDir:         runDir,
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
		RunDir:         runDir,
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
	qaArtifact := writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "source-snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       qaArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "source-snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	complexityArtifact := writeGateArtifact(t, dir, "complexity-gate", "", "wf", "target-snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Artifact:       complexityArtifact,
		WorkflowID:     "wf",
		ChangeSnapshot: "target-snap",
	})
	if result.OK() {
		t.Fatal("expected mismatched workflow prerequisite to block")
	}
	if !strings.Contains(result.Failures[0].Message, "changeSnapshot=target-snap") {
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
	for _, dependency := range []string{"dispatch", "start", "stop", "prompt"} {
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
			runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
			statePath := relativePath(dir, filepath.Join(runDir, "restricted", "gate-state.json"))
			state, shown := GateShow(GateShowOptions{Worktree: dir, StatePath: statePath})
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
			if len(receiptEntry.References) != 4 || contains(receiptEntry.References, closure.RootArtifact) {
				t.Fatalf("receipt dependencies are incomplete or cyclic: %#v", receiptEntry.References)
			}
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
			path := map[string]string{"dispatch": receipt.DispatchRegistrationArtifact, "start": receipt.StartEventArtifact, "stop": receipt.StopEventArtifact, "prompt": receipt.PromptArtifact}[dependency]
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

func TestArtifactValidationRevalidatesReceiptBoundPrompt(t *testing.T) {
	dir := t.TempDir()
	artifact := writeGateArtifact(t, dir, "complexity-gate", "", "wf", "snap")
	options := ArtifactOptions{Root: dir, File: artifact, Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap", Flow: "post-development"}
	if result := Artifact(options); !result.OK() {
		t.Fatalf("clean receipt-bound artifact failed validation: %#v", result.Failures)
	}
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	entries, err := os.ReadDir(receiptProofDir(dir, runDir, "dispatch"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("missing dispatch registration: %v", err)
	}
	var promptPath string
	for _, entry := range entries {
		dispatch, ok := decodeDispatch(filepath.Join(receiptProofDir(dir, runDir, "dispatch"), entry.Name()))
		if ok && dispatch.Gate == "complexity-gate" {
			promptPath = resolvePath(dir, dispatch.PromptArtifact)
		}
	}
	if promptPath == "" {
		t.Fatal("missing receipt-bound prompt")
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, promptPath, string(prompt)+"Directed focus: repaired path\n")
	if result := Artifact(options); result.OK() || !strings.Contains(resultSummary(result), "final-send prompt") {
		t.Fatalf("artifact validation accepted changed prompt: %#v", result.Failures)
	}
}

func writeRequirementsArtifact(t *testing.T, dir, workflowID, snapshot string) {
	t.Helper()
	writeV2RequirementsFixture(t, dir, workflowID, snapshot)
	baseDir := dir
	if strings.Contains(filepath.ToSlash(dir), "/.claude/gates/runs/") {
		baseDir = filepath.Join(dir, "restricted")
	}
	data, err := os.ReadFile(filepath.Join(baseDir, "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(baseDir, "requirements.md"), string(data))
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
	restricted := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	if gate == "qa-test-gate" {
		envelope, payload := qaExecutionPolicyFixture(t, runDir, workflowID, snapshot)
		path := filepath.Join(restricted, gate+".md")
		writeEnvelopeTest(t, path, envelope, payload)
		return relativePath(dir, path)
	}
	bundleName := prefix + "-bundle.json"
	for name, text := range map[string]string{prefix + "-input.txt": "input", prefix + "-changed.txt": "changed", prefix + "-verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), text)
	}
	writeJSONTest(t, filepath.Join(restricted, bundleName), ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []EvidenceRef{testRef(t, runDir, logical(prefix+"-input.txt"))}})
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
	writeJSONTest(t, filepath.Join(restricted, statisticsName), ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{}, Summary: ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []ComplexityFileChange{}})
	for _, id := range policy.RequiredCheckIDs {
		check := ReviewCheck{ID: id, Status: "PASS", Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}}
		if id == "complexity.statistics" && flow == "start-readiness" {
			check.Status = "NOT_APPLICABLE"
			check.Message = "not needed before development"
		}
		if id == "complexity.statistics" && flow == "post-development" {
			check.EvidenceRefs = []EvidenceRef{testRef(t, runDir, logical(statisticsName))}
		}
		checks = append(checks, check)
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, runDir, logical(bundleName)), ReviewPolicyID: policy.ID, Checks: checks}
	if policy.ChangedFilesRequired {
		payload.ChangedFiles = ptrRef(testRef(t, runDir, logical(prefix+"-changed.txt")))
		payload.Verification = ptrRef(testRef(t, runDir, logical(prefix+"-verification.txt")))
	}
	role := policy.ArtifactRole
	path := filepath.Join(restricted, gate+".md")
	artifact := relativePath(dir, path)
	register, rr := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Artifact: artifact, ContextBundle: logical(bundleName)}))
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

func finalizeCarryTestArtifact(t *testing.T, root string, fixture carryTestFixture) {
	t.Helper()
	registration, result := ReceiptRegisterDispatch(withReceiptBundle(t, ReceiptRegisterOptions{Worktree: root, RunDir: fixture.RunDir, Provider: "codex", WorkflowID: fixture.Envelope.WorkflowID, ChangeSnapshot: fixture.Envelope.ChangeSnapshot, Gate: "qa-test-gate", Stage: "Carry", Artifact: fixture.Artifact, ContextBundle: fixture.Payload.ContextBundle.Path}))
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload, _ := json.Marshal(map[string]any{"workflowId": fixture.Envelope.WorkflowID, "changeSnapshot": fixture.Envelope.ChangeSnapshot, "gate": "qa-test-gate", "stage": "Carry", "subagentId": "carry-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact, "status": "completed"})
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: root, RunDir: fixture.RunDir, Provider: "codex", Event: "SubagentStart", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	writeEnvelopeTest(t, resolvePath(root, fixture.Artifact), fixture.Envelope, fixture.Payload)
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: root, RunDir: fixture.RunDir, Provider: "codex", Event: "SubagentStop", Payload: payload}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := ReceiptFinalize(ReceiptFinalizeOptions{Worktree: root, RunDir: fixture.RunDir, Provider: "codex", WorkflowID: fixture.Envelope.WorkflowID, Gate: "qa-test-gate", Stage: "Carry", Artifact: fixture.Artifact}); !result.OK() {
		t.Fatal(result.Failures)
	}
}
