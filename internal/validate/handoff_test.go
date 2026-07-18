package validate

import (
	"fmt"
	"os"
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

func TestHandoffRejectsMissingOrUnsupportedFormalFlowMode(t *testing.T) {
	for name, modeLine := range map[string]string{
		"missing":     "",
		"unsupported": "Formal flow mode: four_gate\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeHandoffForTest(t, dir,
				"max-net 250, max-new-prod-files 0, max-prod-insertions 300",
				"bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
			)
			path := filepath.Join(dir, "handoff.md")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(data), "Formal flow mode: none\n", modeLine, 1)
			mustWrite(t, path, text)

			result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
			assertFailureContains(t, result, "Formal flow mode must be one of: none, four-gate, release, seal")
		})
	}
}

func TestHandoffRequiresAcceptedDesignReviewChainForFormalQAFlow(t *testing.T) {
	dir := t.TempDir()
	runDir, _ := resolveWorkflowRunDir(dir, "wf", "")
	caseSet, designReview, err := writeCanaryDesignReviewClosure(dir, runDir, "wf", "design-snap")
	if err != nil {
		t.Fatal(err)
	}
	writeFormalHandoffForTest(t, dir, caseSet, designReview)
	options := HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "design-snap"}
	if result := Handoff(options); !result.OK() {
		t.Fatalf("accepted Design Review chain was rejected: %#v", result.Failures)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "handoff.md"))
	text := strings.Replace(string(data), "Approved QA case set: path="+caseSet.Path, "Approved QA case set: path=copied-cases.md", 1)
	mustWrite(t, filepath.Join(dir, "handoff.md"), text)
	if result := Handoff(options); result.OK() || !strings.Contains(resultSummary(result), "same exact EvidenceRef") {
		t.Fatalf("different approved-case reference was accepted: %#v", result.Failures)
	}
}

func TestHandoffEvidenceRefKeepsRunLocalPathSpaces(t *testing.T) {
	hash := strings.Repeat("a", 64)
	ref, ok := handoffEvidenceRef("path=qa design/candidate cases.md sha256=" + hash)
	if !ok || ref.Path != "qa design/candidate cases.md" || ref.SHA256 != hash {
		t.Fatalf("run-local path with spaces was not preserved: %#v ok=%v", ref, ok)
	}
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
		"Formal flow mode: none",
	)
	mustWrite(t, filepath.Join(dir, "handoff.md"), strings.Join(lines, "\n")+"\n")
}

func writeFormalHandoffForTest(t *testing.T, dir string, caseSet, designReview EvidenceRef) {
	t.Helper()
	writeHandoffForTest(t, dir,
		"max-net 250, max-new-prod-files 0, max-prod-insertions 300",
		"bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
	)
	data, err := os.ReadFile(filepath.Join(dir, "handoff.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "Change snapshot: snap", "Change snapshot: design-snap", 1)
	text = strings.Replace(text, "Formal flow mode: none", "Formal flow mode: four-gate", 1) + strings.Join([]string{
		fmt.Sprintf("QA case design artifact: path=%s sha256=%s", caseSet.Path, caseSet.SHA256),
		fmt.Sprintf("Approved QA case set: path=%s sha256=%s", caseSet.Path, caseSet.SHA256),
		fmt.Sprintf("Accepted Design Review closure: path=%s sha256=%s", designReview.Path, designReview.SHA256),
	}, "\n") + "\n"
	mustWrite(t, filepath.Join(dir, "handoff.md"), text)
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
