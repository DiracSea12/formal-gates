package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateTransitionAllowsRerunFromCodeQuality(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local correctness repair")

	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local correctness repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}

	admission := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if !admission.OK() {
		t.Fatalf("expected transition-backed code-quality admission, got %#v", admission.Failures)
	}

	writeGateArtifact(t, dir, "code-quality-gate", "", "wf", "new")
	recordCodeQuality := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		Verdict:        "PASS",
		Artifact:       "code-quality-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if !recordCodeQuality.OK() {
		t.Fatalf("expected code-quality record-stage to accept transition-backed prerequisites, got %#v", recordCodeQuality.Failures)
	}
}

func TestGateRecordTransitionPersistsAllowedModes(t *testing.T) {
	for _, workflowMode := range []string{"four-gate", "release", "seal"} {
		t.Run(workflowMode, func(t *testing.T) {
			dir := t.TempDir()
			recordTransitionSourcePrerequisites(t, dir, "wf", "old", "complexity-gate")
			writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "complexity-gate", workflowMode, "local repair")

			transition := GateRecordTransition(GateRecordTransitionOptions{
				Worktree:         dir,
				WorkflowID:       "wf",
				FromSnapshot:     "old",
				ToSnapshot:       "new",
				RerunFromGate:    "complexity-gate",
				FlowMode:         "post-development",
				WorkflowMode:     workflowMode,
				DecisionArtifact: "transition.md",
				Reason:           "local repair",
			})
			if !transition.OK() {
				t.Fatalf("expected %s transition to record, got %#v", workflowMode, transition.Failures)
			}

			state, show := GateShow(GateShowOptions{Worktree: dir})
			if !show.OK() {
				t.Fatalf("expected state show to pass, got %#v", show.Failures)
			}
			if len(state.Transitions) != 1 {
				t.Fatalf("expected one transition, got %#v", state.Transitions)
			}
			got := state.Transitions[0]
			if got.FlowMode != "post-development" || got.WorkflowMode != workflowMode {
				t.Fatalf("unexpected persisted modes: %#v", got)
			}
		})
	}
}

func TestGateTransitionAllowsFinalExecutionAfterRerunGateRecordsCurrentPass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "final-run.json"), `{"ok":true}`+"\n")
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "seal", "local correctness repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "seal",
		DecisionArtifact: "transition.md",
		Reason:           "local correctness repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}
	writeGateArtifact(t, dir, "code-quality-gate", "", "wf", "new")
	recordCodeQuality := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		Verdict:        "PASS",
		Artifact:       "code-quality-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if !recordCodeQuality.OK() {
		t.Fatalf("expected code-quality record to pass, got %#v", recordCodeQuality.Failures)
	}
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "final-verification.json"), finalVerificationJSONForGateState("wf", "new"))
	mustWrite(t, filepath.Join(dir, "final-execution.md"), finalExecutionArtifactTextForGateState("wf", "new", ".claude/gates/artifacts/final-verification.json"))

	recordFinal := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "FinalExecution",
		Artifact:       "final-execution.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if !recordFinal.OK() {
		t.Fatalf("expected FinalExecution to accept transition-backed prerequisites, got %#v", recordFinal.Failures)
	}
}

func TestGateVerifyAdmissionReportsMissingTransition(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected missing transition to block")
	}
	message := result.Failures[0].Message
	if !strings.Contains(message, "current-pass-missing") || !strings.Contains(message, "transition-missing") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateRecordTransitionRejectsWorkflowModeStartReadinessOnly(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "complexity-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "complexity-gate", "start-readiness-only", "local repair")

	result := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "complexity-gate",
		WorkflowMode:     "start-readiness-only",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if result.OK() {
		t.Fatal("expected start-readiness-only transition to be rejected")
	}
	if !strings.Contains(result.Failures[0].Message, "start-readiness-only") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateRecordTransitionRejectsConflictingTransition(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old-a", "complexity-gate")
	writeTransitionDecisionArtifact(t, dir, "transition-a.md", "wf", "old-a", "new", "complexity-gate", "release", "repair a")
	first := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old-a",
		ToSnapshot:       "new",
		RerunFromGate:    "complexity-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition-a.md",
		Reason:           "repair a",
	})
	if !first.OK() {
		t.Fatalf("expected first transition to record, got %#v", first.Failures)
	}

	recordTransitionSourcePrerequisites(t, dir, "wf", "old-b", "complexity-gate")
	writeTransitionDecisionArtifact(t, dir, "transition-b.md", "wf", "old-b", "new", "complexity-gate", "release", "repair b")
	second := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old-b",
		ToSnapshot:       "new",
		RerunFromGate:    "complexity-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition-b.md",
		Reason:           "repair b",
	})
	if second.OK() {
		t.Fatal("expected conflicting transition to be rejected")
	}
	if !strings.Contains(second.Failures[0].Path, "transition-conflict") {
		t.Fatalf("unexpected failure: %#v", second.Failures)
	}
}

func TestGateRecordTransitionRejectsMissingSourcePass(t *testing.T) {
	dir := t.TempDir()
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local repair")

	result := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if result.OK() {
		t.Fatal("expected missing source PASS to be rejected")
	}
	if !strings.Contains(result.Failures[0].Message, "source-pass-missing") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateRecordTransitionRejectsSourcePassNotReal(t *testing.T) {
	dir := t.TempDir()
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "old")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "CONDITIONAL_PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "old",
	})
	if !record.OK() {
		t.Fatalf("expected non-PASS source record to be stored, got %#v", record.Failures)
	}
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "complexity-gate", "release", "local repair")

	result := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "complexity-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if result.OK() {
		t.Fatal("expected non-PASS source to be rejected")
	}
	if !strings.Contains(result.Failures[0].Message, "source-pass-not-real") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionReportsDecisionArtifactHashMismatch(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}
	mustWrite(t, filepath.Join(dir, "transition.md"), "tampered")

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected tampered decision artifact to block")
	}
	if !strings.Contains(result.Failures[0].Message, "transition-artifact-hash-mismatch") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionReportsDecisionArtifactMissing(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}
	if err := os.Remove(filepath.Join(dir, "transition.md")); err != nil {
		t.Fatal(err)
	}

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected missing decision artifact to block")
	}
	if !strings.Contains(result.Failures[0].Message, "transition-artifact-missing") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionReportsSourceArtifactHashMismatch(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}
	mustWrite(t, filepath.Join(dir, "qa-test-gate.md"), "tampered")

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected tampered source artifact to block")
	}
	if !strings.Contains(result.Failures[0].Message, "source-pass-artifact-invalid") || !strings.Contains(result.Failures[0].Message, "artifactHashMismatch") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionReportsSourceArtifactMissing(t *testing.T) {
	dir := t.TempDir()
	recordTransitionSourcePrerequisites(t, dir, "wf", "old", "code-quality-gate")
	writeTransitionDecisionArtifact(t, dir, "transition.md", "wf", "old", "new", "code-quality-gate", "release", "local repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: "transition.md",
		Reason:           "local repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}
	if err := os.Remove(filepath.Join(dir, "qa-test-gate.md")); err != nil {
		t.Fatal(err)
	}

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected missing source artifact to block")
	}
	if !strings.Contains(result.Failures[0].Message, "source-pass-artifact-invalid") || !strings.Contains(result.Failures[0].Message, "artifactMissing") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestGateVerifyAdmissionReportsRunDirArtifactOutOfBounds(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "wf")
	statePath := filepath.Join(runDir, "gate-state.json")
	recordTransitionSourcePrerequisitesWithState(t, dir, statePath, "wf", "old", "code-quality-gate")
	decisionArtifact := ".claude/gates/runs/wf/transition.md"
	writeTransitionDecisionArtifact(t, dir, decisionArtifact, "wf", "old", "new", "code-quality-gate", "release", "local repair")
	transition := GateRecordTransition(GateRecordTransitionOptions{
		Worktree:         dir,
		StatePath:        statePath,
		WorkflowID:       "wf",
		FromSnapshot:     "old",
		ToSnapshot:       "new",
		RerunFromGate:    "code-quality-gate",
		WorkflowMode:     "release",
		DecisionArtifact: decisionArtifact,
		Reason:           "local repair",
	})
	if !transition.OK() {
		t.Fatalf("expected transition to record, got %#v", transition.Failures)
	}

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		StatePath:      statePath,
		RunDir:         runDir,
		Gate:           "code-quality-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "new",
	})
	if result.OK() {
		t.Fatal("expected source artifact outside run-dir to block")
	}
	if !strings.Contains(result.Failures[0].Message, "artifactOutOfBounds") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func recordTransitionSourcePrerequisites(t *testing.T, dir, workflowID, snapshot, rerunFromGate string) {
	t.Helper()
	recordTransitionSourcePrerequisitesWithState(t, dir, "", workflowID, snapshot, rerunFromGate)
}

func recordTransitionSourcePrerequisitesWithState(t *testing.T, dir, statePath, workflowID, snapshot, rerunFromGate string) {
	t.Helper()
	rerunIndex, ok := postDevelopmentGateIndex[rerunFromGate]
	if !ok {
		t.Fatalf("unknown rerun gate %q", rerunFromGate)
	}
	for _, item := range requirementsBeforeGate(rerunIndex) {
		writeGateArtifact(t, dir, item.gate, item.stage, workflowID, snapshot)
		record := GateRecord(GateRecordOptions{
			Worktree:       dir,
			StatePath:      statePath,
			Gate:           item.gate,
			Verdict:        "PASS",
			Mode:           item.mode,
			Stage:          item.stage,
			Artifact:       item.gate + ".md",
			Actor:          item.gate,
			WorkflowID:     workflowID,
			ChangeSnapshot: snapshot,
		})
		if !record.OK() {
			t.Fatalf("expected source prerequisite %s to record, got %#v", item.gate, record.Failures)
		}
	}
}

func writeTransitionDecisionArtifact(t *testing.T, dir, path, workflowID, fromSnapshot, toSnapshot, rerunFromGate, workflowMode, reason string) {
	t.Helper()
	text := strings.Join([]string{
		"Rerun Scope Decision",
		"Workflow ID: " + workflowID,
		"From snapshot: " + fromSnapshot,
		"To snapshot: " + toSnapshot,
		"New change snapshot: " + toSnapshot,
		"Rerun from gate: " + rerunFromGate,
		"Earliest gate to rerun: " + rerunFromGate,
		"Flow mode: post-development",
		"Workflow mode: " + workflowMode,
		"Transition reason: " + reason,
		"Reason skipped gates still apply: current repair does not alter skipped gate judgment surfaces",
		"Full-scope review confirmed: YES",
	}, "\n") + "\n"
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(path)), text)
}

func finalVerificationJSONForGateState(workflowID, snapshot string) string {
	return `{"schemaVersion":1,"workflowId":"` + workflowID + `","changeSnapshot":"` + snapshot + `","status":"PASS","attempts":[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-run.json","contextBundle":"bundle.zip sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"acceptedAttempts":[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-run.json","contextBundle":"bundle.zip sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}` + "\n"
}

func finalExecutionArtifactTextForGateState(workflowID, snapshot, finalVerification string) string {
	lines := []string{
		"FinalExecution mode: MECHANICAL_CLOSEOUT",
		"Mechanical closeout: YES",
		"Final verification artifact: " + finalVerification,
		"Existing gate records: qa-test-gate Execution, complexity-gate, architecture-health-gate, code-quality-gate",
		"Release judgment: YES",
		"gate_route:",
		"  workflow_id: " + workflowID,
		"  change_snapshot: " + snapshot,
		"  next_action: seal",
		"  rework_owner: none",
		"  rerun_from: none",
	}
	return strings.Join(lines, "\n") + "\n"
}
