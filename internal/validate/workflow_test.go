package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowFileHashSnapshotIsStable(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha\n")
	mustWrite(t, filepath.Join(dir, "nested", "b.txt"), "beta\n")

	first, result := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: dir, VCS: "file-hash"})
	if !result.OK() {
		t.Fatalf("expected snapshot to pass, got %#v", result.Failures)
	}
	second, result := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: dir, VCS: "file-hash"})
	if !result.OK() {
		t.Fatalf("expected second snapshot to pass, got %#v", result.Failures)
	}
	if first.ChangeSnapshot != second.ChangeSnapshot || first.RangeHash != second.RangeHash {
		t.Fatalf("expected stable snapshot, first=%#v second=%#v", first, second)
	}
	if !strings.HasPrefix(first.ChangeSnapshot, "files.") {
		t.Fatalf("expected files snapshot id, got %q", first.ChangeSnapshot)
	}
	if first.WorkingTreeHash != first.RangeHash || !first.IncludeWorkingTree {
		t.Fatalf("unexpected file-hash fields: %#v", first)
	}
}

func TestWorkflowFileHashSnapshotIgnoresGateAndTempDirs(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src.txt"), "source\n")

	before, result := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: dir, VCS: "file-hash"})
	if !result.OK() {
		t.Fatalf("expected snapshot to pass, got %#v", result.Failures)
	}
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "gate-state.json"), `{"schemaVersion":1}`)
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "review.md"), "artifact\n")
	mustWrite(t, filepath.Join(dir, ".artifacts", "tmp", "scratch.txt"), "tmp\n")
	mustWrite(t, filepath.Join(dir, ".artifacts", "scratch", "scratch.txt"), "scratch\n")
	mustWrite(t, filepath.Join(dir, ".artifacts", "cleanup", "old.txt"), "cleanup\n")

	after, result := WorkflowSnapshot(WorkflowSnapshotOptions{Worktree: dir, VCS: "file-hash"})
	if !result.OK() {
		t.Fatalf("expected snapshot to pass, got %#v", result.Failures)
	}
	if before.ChangeSnapshot != after.ChangeSnapshot {
		t.Fatalf("ignored directories changed snapshot: before=%#v after=%#v", before, after)
	}
}

func TestWorkflowRecordStageCallsGateState(t *testing.T) {
	dir := t.TempDir()
	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")

	result := WorkflowRecordStage(WorkflowRecordStageOptions{
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
		t.Fatalf("expected workflow record-stage to pass, got %#v", result.Failures)
	}
	state, show := GateShow(GateShowOptions{Worktree: dir})
	if !show.OK() {
		t.Fatalf("expected show to pass, got %#v", show.Failures)
	}
	if state.Gates["qa-test-gate"].Actor != "qa" {
		t.Fatalf("expected gate-state entry from workflow record-stage, got %#v", state.Gates["qa-test-gate"])
	}
}

func TestWorkflowVerifyAdmissionPositiveAndNegative(t *testing.T) {
	dir := t.TempDir()
	blocked := WorkflowVerifyAdmission(WorkflowVerifyAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if blocked.OK() {
		t.Fatal("expected missing QA prerequisite to block")
	}

	writeGateArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	record := WorkflowRecordStage(WorkflowRecordStageOptions{
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
		t.Fatalf("expected record-stage to pass, got %#v", record.Failures)
	}

	allowed := WorkflowVerifyAdmission(WorkflowVerifyAdmissionOptions{
		Worktree:       dir,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !allowed.OK() {
		t.Fatalf("expected admission to pass, got %#v", allowed.Failures)
	}
}

func TestWorkflowFinalVerificationAcceptedAttempt(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "final-run.json"), `{"ok":true}`+"\n")
	attempts := `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-run.json","contextBundle":"bundle.zip sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`
	output := ".claude/gates/artifacts/final-verification.json"

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:       dir,
		AttemptsJSON:   attempts,
		OutputArtifact: output,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected final verification to pass, got %#v", result.Failures)
	}
	if artifact.Status != "PASS" || len(artifact.AcceptedAttempts) != 1 || len(artifact.Attempts) != 1 {
		t.Fatalf("unexpected aggregate: %#v", artifact)
	}
	var written WorkflowFinalVerificationArtifact
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(output)))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatal(err)
	}
	if written.WorkflowID != "wf" || written.ChangeSnapshot != "snap" || written.Status != "PASS" {
		t.Fatalf("unexpected written artifact: %#v", written)
	}
	if strings.Contains(string(data), "generatedAt") {
		t.Fatalf("final verification artifact must be deterministic, got %s", string(data))
	}
}

func TestWorkflowFinalVerificationRecordsFinalQA(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "final-run.json"), `{"ok":true}`+"\n")
	writeGateArtifact(t, dir, "qa-test-gate", "FinalExecution", "wf", "snap")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:        dir,
		AttemptsJSON:    `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-run.json"}]`,
		OutputArtifact:  ".claude/gates/artifacts/final-verification.json",
		FinalQAArtifact: "qa-test-gate.md",
		RecordFinalQA:   true,
		Actor:           "qa-reviewer",
		WorkflowID:      "wf",
		ChangeSnapshot:  "snap",
	})
	if !result.OK() {
		t.Fatalf("expected final QA record to pass, got %#v", result.Failures)
	}
	if artifact.Status != "PASS" {
		t.Fatalf("expected PASS aggregate, got %#v", artifact)
	}
	state, show := GateShow(GateShowOptions{Worktree: dir})
	if !show.OK() {
		t.Fatalf("expected gate state to show, got %#v", show.Failures)
	}
	entry := state.Gates["qa-test-gate"]
	if entry.Stage != "FinalExecution" || entry.Actor != "qa-reviewer" || entry.Artifact != "qa-test-gate.md" {
		t.Fatalf("unexpected final QA gate entry: %#v", entry)
	}
}

func TestWorkflowFinalVerificationRecordFinalQARequiresExistingArtifact(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "final-run.json"), `{"ok":true}`+"\n")

	_, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:        dir,
		AttemptsJSON:    `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-run.json"}]`,
		OutputArtifact:  ".claude/gates/artifacts/final-verification.json",
		FinalQAArtifact: ".claude/gates/artifacts/final-qa-execution.md",
		RecordFinalQA:   true,
		WorkflowID:      "wf",
		ChangeSnapshot:  "snap",
	})
	if result.OK() {
		t.Fatal("expected missing final QA artifact to fail")
	}
	if !strings.Contains(result.Failures[len(result.Failures)-1].Message, "does not exist") {
		t.Fatalf("unexpected failure: %#v", result.Failures)
	}
}

func TestWorkflowFinalVerificationMissingAcceptedArtifactFails(t *testing.T) {
	dir := t.TempDir()
	output := ".claude/gates/artifacts/final-verification.json"

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:       dir,
		AttemptsJSON:   `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/missing.json"}]`,
		OutputArtifact: output,
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected missing accepted artifact to fail")
	}
	if artifact.Status != "FAIL" {
		t.Fatalf("expected failed aggregate, got %#v", artifact)
	}
	if !strings.Contains(result.Failures[0].Message, "does not exist") {
		t.Fatalf("expected missing artifact failure, got %#v", result.Failures)
	}
	if !isFile(filepath.Join(dir, filepath.FromSlash(output))) {
		t.Fatal("expected deterministic failure artifact to be written")
	}
}

func TestWorkflowFinalVerificationNoAcceptedFails(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifacts", "run.json"), `{"ok":false}`+"\n")

	artifact, result := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree:       dir,
		AttemptsJSON:   `[{"status":"FAIL","accepted":false,"artifact":".claude/gates/artifacts/run.json"}]`,
		OutputArtifact: ".claude/gates/artifacts/final-verification.json",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if result.OK() {
		t.Fatal("expected no accepted attempts to fail")
	}
	if artifact.Status != "FAIL" || len(artifact.AcceptedAttempts) != 0 {
		t.Fatalf("unexpected aggregate: %#v", artifact)
	}
}

func TestWorkflowCleanupDryRunExecuteAndDeny(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, ".artifacts", "tmp", "run", "scratch.txt")
	mustWrite(t, tmpFile, "scratch\n")
	mustWrite(t, filepath.Join(dir, ".claude", "gates", "artifact.md"), "evidence\n")

	dryRun, result := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected cleanup dry-run to pass, got %#v", result.Failures)
	}
	if !dryRun.DryRun || len(dryRun.Paths) != 1 || dryRun.Paths[0].Status != "would-remove" {
		t.Fatalf("unexpected dry-run report: %#v", dryRun)
	}
	if !isFile(tmpFile) {
		t.Fatal("dry-run removed scratch file")
	}

	_, denied := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, Paths: []string{".claude/gates/artifact.md"}})
	if denied.OK() {
		t.Fatal("expected .claude/gates cleanup to be denied")
	}

	_, deniedRoot := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, Paths: []string{"."}})
	if deniedRoot.OK() {
		t.Fatal("expected repo root cleanup to be denied")
	}

	executed, result := WorkflowCleanup(WorkflowCleanupOptions{Worktree: dir, Paths: []string{".artifacts/tmp/run/scratch.txt"}, Execute: true})
	if !result.OK() {
		t.Fatalf("expected cleanup execute to pass, got %#v", result.Failures)
	}
	if executed.DryRun || len(executed.Paths) != 1 || executed.Paths[0].Status != "removed" {
		t.Fatalf("unexpected execute report: %#v", executed)
	}
	if exists(tmpFile) {
		t.Fatal("execute did not remove scratch file")
	}
}
