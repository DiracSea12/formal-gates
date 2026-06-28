package validate

import (
	"path/filepath"
	"testing"
)

func TestHandoffRequiresDevelopmentTimeComplexityBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.md")
	mustWrite(t, path, `Gate Handoff Request
WorkflowId: wf
Change snapshot: snap
Worktree: repo
Requirement document target or OpenSpec change: openspec/changes/example
Verification requirements: go test ./...
Forbidden context: no prior findings
`)

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected handoff without development-time complexity budget to fail")
	}
}

func TestHandoffAcceptsBudgetedComplexityCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.md")
	mustWrite(t, path, `Gate Handoff Request
WorkflowId: wf
Change snapshot: snap
Worktree: repo
Requirement document target or OpenSpec change: openspec/changes/example
Verification requirements: go test ./...
Development-time complexity budget: max-net 250, max-new-prod-files 0, max-prod-insertions 300
Complexity check command: bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto
Budget stop triggers: stop on non-zero complexity check or new unbudgeted concepts
Budget expansion approval path: .claude/gates/artifacts/anti-complexity-approval.md
Forbidden context: no prior findings
`)

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected handoff to pass, got %#v", result.Failures)
	}
}
