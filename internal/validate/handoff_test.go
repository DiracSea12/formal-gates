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
	ref, composed := ComposeHandoff(HandoffComposeOptions{
		Root: dir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
		RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
		ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user",
		TaskType: "small-feature",
		MaxNet:   250, MaxNewProdFiles: 0, MaxProdInsertions: 300,
	})
	if !composed.OK() {
		t.Fatal(composed.Failures)
	}
	result := Handoff(HandoffOptions{Root: dir, File: filepath.Join(".claude", "gates", "runs", "wf", filepath.FromSlash(ref.Path)), WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatalf("expected handoff to pass, got %#v", result.Failures)
	}
}

func TestComposeHandoffPreservesExplicitRunDir(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "custom-run")
	ref, composed := ComposeHandoff(HandoffComposeOptions{
		Root: dir, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
		RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
		ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user", TaskType: "bugfix",
		MaxNet: 100, MaxNewProdFiles: 0, MaxProdInsertions: 100,
	})
	if !composed.OK() {
		t.Fatalf("explicit run-dir handoff composition failed: %#v", composed.Failures)
	}
	file := filepath.Join(".claude", "gates", "runs", "custom-run", filepath.FromSlash(ref.Path))
	if result := Handoff(HandoffOptions{Root: dir, File: file, WorkflowID: "wf", ChangeSnapshot: "snap"}); !result.OK() {
		t.Fatalf("handoff validation did not infer the explicit run directory: %#v", result.Failures)
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
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "custom-run")
	caseSet, designReview, err := writeCanaryDesignReviewClosure(dir, runDir, "wf", "design-snap")
	if err != nil {
		t.Fatal(err)
	}
	ref, composed := ComposeHandoff(HandoffComposeOptions{
		Root: dir, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "design-snap", Output: "restricted/handoff.md",
		RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
		ForbiddenContext: "prior findings", FormalFlowMode: "four-gate", TriggerSource: "user",
		TaskType:  "small-feature",
		QACaseSet: caseSet.Path, DesignReview: designReview.Path,
		MaxNet: 250, MaxNewProdFiles: 0, MaxProdInsertions: 300,
	})
	if !composed.OK() {
		t.Fatal(composed.Failures)
	}
	file := filepath.Join(".claude", "gates", "runs", "custom-run", filepath.FromSlash(ref.Path))
	options := HandoffOptions{Root: dir, File: file, WorkflowID: "wf", ChangeSnapshot: "design-snap"}
	if result := Handoff(options); !result.OK() {
		t.Fatalf("accepted Design Review chain was rejected: %#v", result.Failures)
	}

	path := filepath.Join(runDir, filepath.FromSlash(ref.Path))
	data, _ := os.ReadFile(path)
	text := strings.Replace(string(data), "Approved QA case set: path="+caseSet.Path, "Approved QA case set: path=copied-cases.md", 1)
	mustWrite(t, path, text)
	if result := Handoff(options); result.OK() || !strings.Contains(resultSummary(result), "same exact EvidenceRef") {
		t.Fatalf("different approved-case reference was accepted: %#v", result.Failures)
	}
}

func TestComposeHandoffRejectsUnsupportedTaskTypeBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "wf")
	output := filepath.Join(runDir, "restricted", "handoff.md")
	_, result := ComposeHandoff(HandoffComposeOptions{
		Root: dir, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
		RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
		ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user",
		TaskType: "code-implementation", MaxNet: 250, MaxNewProdFiles: 0, MaxProdInsertions: 300,
	})
	if result.OK() {
		t.Fatal("unsupported handoff task type passed")
	}
	assertFailureContains(t, result, "--task-type must be one of: bugfix, delete-or-consolidate, new-system, refactor, small-feature")
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("rejected handoff composition left output: %v", err)
	}
	proofs, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(proofs) != 0 {
		t.Fatalf("rejected handoff composition left proof: %v", proofs)
	}
}

func TestHandoffRejectsUnsupportedComplexityTaskType(t *testing.T) {
	dir := t.TempDir()
	writeHandoffForTest(t, dir,
		"max-net 250, max-new-prod-files 0, max-prod-insertions 300",
		"bin/formal-gates complexity check --task-type code-implementation --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto",
	)
	result := Handoff(HandoffOptions{Root: dir, File: "handoff.md", WorkflowID: "wf", ChangeSnapshot: "snap"})
	assertFailureContains(t, result, "Complexity check command has unsupported --task-type: code-implementation")
}

func TestHandoffEvidenceRefKeepsRunLocalPathSpaces(t *testing.T) {
	hash := strings.Repeat("a", 64)
	ref, ok := handoffEvidenceRef("path=qa design/candidate cases.md sha256=" + hash)
	if !ok || ref.Path != "qa design/candidate cases.md" || ref.SHA256 != hash {
		t.Fatalf("run-local path with spaces was not preserved: %#v ok=%v", ref, ok)
	}
}

func TestComposeHandoffRejectsCRLFInEveryLineScalar(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*HandoffComposeOptions)
	}{
		{"workflow-id", func(options *HandoffComposeOptions) { options.WorkflowID = "wf\ncontinued" }},
		{"change-snapshot", func(options *HandoffComposeOptions) { options.ChangeSnapshot = "snap\rcontinued" }},
		{"requirement-target", func(options *HandoffComposeOptions) { options.RequirementTarget = "requirements\ncurrent.md" }},
		{"verification-requirements", func(options *HandoffComposeOptions) { options.VerificationRequirements = "go test ./...\r" }},
		{"budget-stop-triggers", func(options *HandoffComposeOptions) { options.BudgetStopTriggers = "stop\nnow" }},
		{"budget-expansion-approval-path", func(options *HandoffComposeOptions) {
			options.BudgetExpansionApprovalPath = "agents\r/anti-complexity-review.md"
		}},
		{"forbidden-context", func(options *HandoffComposeOptions) { options.ForbiddenContext = "prior\nfindings" }},
		{"formal-flow-mode", func(options *HandoffComposeOptions) { options.FormalFlowMode = "none\r" }},
		{"trigger-source", func(options *HandoffComposeOptions) { options.TriggerSource = "user\nrequest" }},
		{"task-type", func(options *HandoffComposeOptions) { options.TaskType = "bugfix\r" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			options := HandoffComposeOptions{
				Root: dir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
				RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
				BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
				ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user",
				TaskType: "small-feature", MaxNet: 250, MaxNewProdFiles: 0, MaxProdInsertions: 300,
			}
			test.apply(&options)
			_, result := ComposeHandoff(options)
			if result.OK() {
				t.Fatal("line-breaking scalar was accepted")
			}
			assertFailureContains(t, result, "--"+test.name+" must not contain CR/LF")
			if _, err := os.Stat(filepath.Join(dir, ".claude", "gates", "runs", "wf", "restricted", "handoff.md")); !os.IsNotExist(err) {
				t.Fatalf("rejected handoff left output: %v", err)
			}
		})
	}
}

func TestComposeHandoffQuotesWorktreePathWithSpaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	ref, result := ComposeHandoff(HandoffComposeOptions{
		Root: root, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/handoff.md",
		RequirementTarget: "openspec/changes/example", VerificationRequirements: "go test ./...",
		BudgetStopTriggers: "stop on non-zero complexity check", BudgetExpansionApprovalPath: "agents/anti-complexity-review.md",
		ForbiddenContext: "prior findings", FormalFlowMode: "none", TriggerSource: "user", TaskType: "small-feature",
		MaxNet: 250, MaxNewProdFiles: 0, MaxProdInsertions: 300,
	})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	path := filepath.Join(root, ".claude", "gates", "runs", "wf", filepath.FromSlash(ref.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	command := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Complexity check command: ") {
			command = strings.TrimPrefix(line, "Complexity check command: ")
			break
		}
	}
	want := `--worktree "` + filepath.ToSlash(root) + `"`
	if !strings.Contains(command, want) {
		t.Fatalf("generated command does not quote worktree path: %q", command)
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
