package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactRequirementsClarification(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "requirements.md")
	text := `Requirement source: user brief
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
  rework_owner: none
  rerun_from: none
`
	if err := os.WriteFile(artifact, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	result := Artifact(ArtifactOptions{
		Root:           dir,
		File:           "requirements.md",
		Gate:           "requirements-clarification-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected valid artifact, got %#v", result.Failures)
	}
}

func TestArtifactRejectsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "complexity.md")
	text := `Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: <reviewer>
Context bundle: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Dispatch prompt artifact: prompt.md sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: focused
Impact surface health: bounded
Public/config surface: none
New concepts: none
Shrink opportunities: none
Decision evidence: diff
gate_route:
  workflow_id: wf
  change_snapshot: snap
  next_action: proceed
`
	if err := os.WriteFile(artifact, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	result := Artifact(ArtifactOptions{Root: dir, File: "complexity.md", Gate: "complexity-gate", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected placeholder reviewer to fail")
	}
}
