package validate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHandoffRequiresDevelopmentTimeComplexityBudget(t *testing.T) {
	dir := t.TempDir()
	writeHandoffForTest(t, dir, "", "")

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected handoff without development-time complexity budget to fail")
	}
}

func TestHandoffAcceptsBudgetedComplexityCheck(t *testing.T) {
	dir := t.TempDir()
	writeHandoffForTest(t, dir,
		"max-net 250, max-new-prod-files 0, max-prod-insertions 300",
		"bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
	)

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected handoff to pass, got %#v", result.Failures)
	}
}

func TestHandoffRejectsQualitativeComplexityBudget(t *testing.T) {
	dir := t.TempDir()
	writeHandoffForTest(t, dir,
		"only scripts/dev/ainpc.py and scripts/dev/ainpc_tool/**; no runtime C++",
		"bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
	)

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected qualitative-only budget to fail")
	}
	assertFailureContains(t, result, "Development-time complexity budget missing numeric max-net")
}

func TestHandoffRejectsBudgetCommandMismatch(t *testing.T) {
	dir := t.TempDir()
	writeHandoffForTest(t, dir,
		"max-net 100, max-new-prod-files 0, max-prod-insertions 300",
		"bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
	)

	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if result.OK() {
		t.Fatal("expected mismatched budget and command to fail")
	}
	assertFailureContains(t, result, "Development-time complexity budget max-net=100 does not match Complexity check command --max-net=250")
}

func writeHandoffForTest(t *testing.T, dir, budget, command string) {
	t.Helper()
	lines := []string{
		"Gate Handoff Request",
		"WorkflowId: wf",
		"Change snapshot: snap",
		"Worktree: repo",
		"Requirement document target or OpenSpec change: openspec/changes/example",
		"Verification requirements: go test ./...",
	}
	if budget != "" {
		lines = append(lines, "Development-time complexity budget: "+budget)
	}
	if command != "" {
		lines = append(lines, "Complexity check command: "+command)
	}
	lines = append(lines,
		"Budget stop triggers: stop on non-zero complexity check or new unbudgeted concepts",
		"Budget expansion approval path: .claude/gates/artifacts/anti-complexity-approval.md",
		"Forbidden context: no prior findings",
	)
	mustWrite(t, filepath.Join(dir, "handoff.md"), strings.Join(lines, "\n")+"\n")
}

func assertFailureContains(t *testing.T, result Result, want string) {
	t.Helper()
	for _, failure := range result.Failures {
		if failure.Message == want {
			return
		}
	}
	t.Fatalf("expected failure %q, got %#v", want, result.Failures)
}
