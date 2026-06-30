package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGateRecordInitializesStateAndStoresPass(t *testing.T) {
	dir := t.TempDir()
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
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
}

func TestGateRecordAllowsRequirementsClarificationPass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "requirements.md"), `Requirement source: user brief
Alignment table artifact: .claude/gates/artifacts/alignment.md
Total alignment items: 1
Open question IDs: none
User confirmation: YES
Dimension coverage:
  DIM-01 Goal: covered | RQ-001
Decision record: .claude/gates/artifacts/requirements-user-decision.md
Covered formal targets: openspec/changes/example/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf
  change_snapshot: snap
  next_action: proceed
`)

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
	if entry.Verdict != "PASS" || entry.Artifact != "requirements.md" {
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
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
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
	writeGateArtifact(t, dir, "complexity-gate", "", "wf", "snap")

	recordComplexity := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Mode:           "start-readiness",
		Artifact:       "complexity-gate.md",
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
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	recordQA := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !recordQA.OK() {
		t.Fatalf("expected QA record to pass, got %#v", recordQA.Failures)
	}
	writeGateArtifact(t, dir, "complexity-gate", "", "wf", "snap")
	recordComplexity := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Artifact:       "complexity-gate.md",
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
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf-a", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
		WorkflowID:     "wf-a",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	writeGateArtifact(t, dir, "complexity-gate", "", "wf-b", "snap")

	result := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		Verdict:        "PASS",
		Artifact:       "complexity-gate.md",
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
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := GateRecord(GateRecordOptions{
		Worktree:       dir,
		Gate:           "qa-test-gate",
		Verdict:        "PASS",
		Mode:           "formal",
		Stage:          "Execution",
		Artifact:       "qa-test-gate.md",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !record.OK() {
		t.Fatalf("expected QA record to pass, got %#v", record.Failures)
	}
	mustWrite(t, filepath.Join(dir, "qa-test-gate.md"), "tampered")

	result := GateVerifyAdmission(GateAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected artifact hash mismatch to block")
	}
	if !strings.Contains(result.Failures[0].Message, "artifactHashMismatch") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func writeRequirementsArtifact(t *testing.T, dir, workflowID, snapshot string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "requirements.md"), `Requirement source: user brief
Alignment table artifact: .claude/gates/artifacts/alignment.md
Total alignment items: 1
Open question IDs: none
User confirmation: YES
Dimension coverage:
  DIM-01 Goal: covered | RQ-001
Decision record: .claude/gates/artifacts/requirements-user-decision.md
Covered formal targets: openspec/changes/example/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: `+workflowID+`
  change_snapshot: `+snapshot+`
  next_action: proceed
`)
}

func writeGateArtifact(t *testing.T, dir, gate, stage, workflowID, snapshot string) {
	t.Helper()
	text := gateArtifactText(gate, stage, workflowID, snapshot)
	if err := os.WriteFile(filepath.Join(dir, gate+".md"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gateArtifactText(gate, stage, workflowID, snapshot string) string {
	lines := []string{
		"Review mode: ZERO_CONTEXT_FORMAL",
		"Prompt contamination check: PASS",
		"Semantic anti-anchor check: PASS",
		"Prompt source: " + expectedPromptSource(gate),
		"Zero-context reviewer: YES",
		"Independent agent: YES",
		"Context bundle: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"Dispatch prompt artifact: prompt.md sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"No-anchor prompt: YES",
	}
	switch gate {
	case "qa-test-gate":
		lines = append(lines,
			"Approved case set: cases.md",
			"QA-owned evidence: qa-evidence.md",
			"Case-to-artifact binding: bound",
		)
	case "complexity-gate":
		lines = append(lines,
			"Script result: PASS",
			"Diff shape judgment: focused",
			"Budget/expansion status: development-time budget history reviewed; no expansion approval used",
			"Impact surface health: bounded",
			"Public/config surface: none",
			"New concepts: none",
			"Minimum sufficient implementation: yes",
			"Shrink opportunities: none",
			"Decision evidence: diff",
		)
	}
	lines = append(lines,
		"gate_route:",
		"  workflow_id: "+workflowID,
		"  change_snapshot: "+snapshot,
		"  next_action: proceed",
		"  rework_owner: none",
		"  rerun_from: none",
	)
	if stage == "FinalExecution" {
		lines[len(lines)-3] = "  next_action: seal"
	}
	return strings.Join(lines, "\n") + "\n"
}
