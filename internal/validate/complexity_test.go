package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComplexityReportRequiresEveryClosedField(t *testing.T) {
	report := ComplexityReport{
		Status: "PASS", VCS: "git", Worktree: t.TempDir(), TaskType: "refactor",
		BudgetSource: "none", BudgetOverrides: ComplexityBudgetOverride{},
		Summary:  ComplexitySummary{Insertions: 1, Net: 1, ChangedFiles: 1},
		Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{},
		LargestFiles: []ComplexityFileChange{{Path: "feature.go", Insertions: 1, Status: "M", Category: "production"}},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	checkMissing := func(name string, mutate func(map[string]any)) {
		t.Helper()
		copyBytes, _ := json.Marshal(original)
		var value map[string]any
		_ = json.Unmarshal(copyBytes, &value)
		mutate(value)
		candidate, _ := json.Marshal(value)
		if _, err := decodeComplexityReport(candidate); err == nil || !strings.Contains(err.Error(), "missing required field") {
			t.Fatalf("missing %s was accepted: %v", name, err)
		}
	}
	for _, field := range []string{"status", "vcs", "worktree", "task_type", "budget_source", "budget_overrides", "summary", "failures", "review_required", "warnings", "largest_files"} {
		field := field
		checkMissing(field, func(value map[string]any) { delete(value, field) })
	}
	for _, field := range []string{"max_net", "max_new_prod_files", "max_prod_insertions"} {
		field := field
		checkMissing("budget_overrides."+field, func(value map[string]any) { delete(value["budget_overrides"].(map[string]any), field) })
	}
	for _, field := range []string{"max_net", "max_new_prod_files", "max_prod_insertions"} {
		field := field
		checkMissing("budget."+field, func(value map[string]any) {
			value["budget"] = map[string]any{"max_net": float64(1), "max_new_prod_files": float64(1), "max_prod_insertions": float64(1)}
			value["budget_source"] = "explicit"
			value["budget_overrides"] = map[string]any{"max_net": true, "max_new_prod_files": true, "max_prod_insertions": true}
			delete(value["budget"].(map[string]any), field)
		})
	}
	for _, field := range []string{"insertions", "deletions", "net", "production_insertions", "new_production_files", "untracked_production_files", "untracked_production_insertions", "changed_files", "untracked_files"} {
		field := field
		checkMissing("summary."+field, func(value map[string]any) { delete(value["summary"].(map[string]any), field) })
	}
	for _, field := range []string{"path", "insertions", "deletions", "status", "category", "suspicious_name"} {
		field := field
		checkMissing("largest_files[0]."+field, func(value map[string]any) {
			delete(value["largest_files"].([]any)[0].(map[string]any), field)
		})
	}
}

func TestComplexityStatisticsRequiresGeneratedCompleteFreshReport(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".claude", "gates", "runs", "wf")
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(runDir, "restricted", "partial.json")
	mustWrite(t, partialPath, "{\"budget_source\":\"none\"}\n")
	partialRef := EvidenceRef{Path: "restricted/partial.json", SHA256: sha256File(partialPath)}
	if err := validateComplexityStatisticsEvidence(root, runDir, "wf", "snap", partialRef); err == nil || !strings.Contains(err.Error(), "missing required field") {
		t.Fatalf("partial complexity report was accepted: %v", err)
	}

	report, result := Complexity(ComplexityOptions{Worktree: root, VCS: "none", TaskType: "refactor"})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	manualPath := filepath.Join(runDir, "restricted", "manual.json")
	manualData, _ := ComplexityJSON(report)
	mustWrite(t, manualPath, string(manualData)+"\n")
	manualRef := EvidenceRef{Path: "restricted/manual.json", SHA256: sha256File(manualPath)}
	if err := validateComplexityStatisticsEvidence(root, runDir, "wf", "snap", manualRef); err == nil || !strings.Contains(err.Error(), "composition proof is missing") {
		t.Fatalf("complete handwritten complexity report was accepted: %v", err)
	}

	generated, _, generatedResult := ComplexityStatistics(ComplexityStatisticsOptions{
		ComplexityOptions: ComplexityOptions{Worktree: root, VCS: "none", TaskType: "refactor"},
		RunDir:            runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: "restricted/generated.json",
	})
	if !generatedResult.OK() {
		t.Fatal(generatedResult.Failures)
	}
	if err := validateComplexityStatisticsEvidence(root, runDir, "wf", "snap", generated); err != nil {
		t.Fatalf("generated complexity statistics did not validate: %v", err)
	}
	if err := validateComplexityStatisticsEvidence(root, runDir, "wf", "other-snapshot", generated); err == nil || !strings.Contains(err.Error(), "composition proof is invalid") {
		t.Fatalf("stale-snapshot complexity proof was accepted: %v", err)
	}
}

func TestComplexityStatisticsRollsBackOutputWhenProofCannotCommit(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".claude", "gates", "runs", "wf")
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	output := "restricted/statistics.json"
	outputPath := filepath.Join(runDir, filepath.FromSlash(output))
	if _, _, result := ComplexityStatistics(ComplexityStatisticsOptions{
		ComplexityOptions: ComplexityOptions{Worktree: root, VCS: "none", TaskType: "refactor"},
		RunDir:            runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: output,
	}); !result.OK() {
		t.Fatal(result.Failures)
	}
	proofPath, err := compositionProofPath(root, runDir, "complexity-statistics.v1", outputPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeProof, _ := os.ReadFile(proofPath)
	if err := os.Remove(outputPath); err != nil {
		t.Fatal(err)
	}
	if _, _, result := ComplexityStatistics(ComplexityStatisticsOptions{
		ComplexityOptions: ComplexityOptions{Worktree: root, VCS: "none", TaskType: "refactor"},
		RunDir:            runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Output: output,
	}); result.OK() {
		t.Fatal("complexity statistics succeeded despite occupied proof path")
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("failed proof commit left statistics output: %v", err)
	}
	afterProof, _ := os.ReadFile(proofPath)
	if string(beforeProof) != string(afterProof) {
		t.Fatal("failed statistics generation changed the existing proof")
	}
}

func TestComplexityGitPassReviewAndFail(t *testing.T) {
	dir := initComplexityGitRepo(t)
	mustWrite(t, filepath.Join(dir, "feature.go"), strings.Repeat("package main\n", 20))

	report, result := Complexity(ComplexityOptions{Worktree: dir, VCS: "git", TaskType: "small-feature"})
	if !result.OK() || report.Budget != nil || report.BudgetSource != "none" || len(report.Warnings) == 0 {
		t.Fatalf("expected statistics-only report without budget, got report=%#v result=%#v", report, result.Failures)
	}
	partialMaxNet := 10
	if _, result = Complexity(ComplexityOptions{Worktree: dir, VCS: "git", TaskType: "small-feature", MaxNet: &partialMaxNet}); result.OK() {
		t.Fatal("expected partial budget to fail")
	}

	maxNet := 100
	maxFiles := 2
	maxProd := 100
	report, result = Complexity(ComplexityOptions{
		Worktree:          dir,
		VCS:               "git",
		TaskType:          "small-feature",
		MaxNet:            &maxNet,
		MaxNewProdFiles:   &maxFiles,
		MaxProdInsertions: &maxProd,
	})
	if !result.OK() {
		t.Fatalf("expected complexity to run, got %#v", result.Failures)
	}
	if report.Status != "PASS" {
		t.Fatalf("expected pass, got %#v", report)
	}

	maxProd = 10
	report, result = Complexity(ComplexityOptions{
		Worktree:          dir,
		VCS:               "git",
		TaskType:          "small-feature",
		MaxNet:            &maxNet,
		MaxNewProdFiles:   &maxFiles,
		MaxProdInsertions: &maxProd,
	})
	if !result.OK() {
		t.Fatalf("expected complexity to run, got %#v", result.Failures)
	}
	if report.Status != "REVIEW" || len(report.ReviewRequired) == 0 {
		t.Fatalf("expected review, got %#v", report)
	}

	maxNewFiles := 0
	report, result = Complexity(ComplexityOptions{
		Worktree:          dir,
		VCS:               "git",
		TaskType:          "small-feature",
		MaxNet:            &maxNet,
		MaxNewProdFiles:   &maxNewFiles,
		MaxProdInsertions: &maxProd,
	})
	if !result.OK() {
		t.Fatalf("expected complexity to run, got %#v", result.Failures)
	}
	if report.Status != "FAIL" || len(report.Failures) == 0 {
		t.Fatalf("expected fail, got %#v", report)
	}
}

func TestComplexityNoVCSRequiresManualReview(t *testing.T) {
	dir := t.TempDir()
	report, result := Complexity(ComplexityOptions{Worktree: dir, VCS: "none", TaskType: "bugfix"})
	if !result.OK() {
		t.Fatalf("expected no-vcs report to run, got %#v", result.Failures)
	}
	if report.Status != "REVIEW" {
		t.Fatalf("expected manual review, got %#v", report)
	}
}

func initComplexityGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForComplexityTest(t, dir, "init")
	runGitForComplexityTest(t, dir, "config", "user.email", "test@example.com")
	runGitForComplexityTest(t, dir, "config", "user.name", "Test User")
	mustWrite(t, filepath.Join(dir, "README.md"), "initial\n")
	runGitForComplexityTest(t, dir, "add", "README.md")
	runGitForComplexityTest(t, dir, "commit", "-m", "initial")
	return dir
}

func runGitForComplexityTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func TestComplexityTextIncludesLargestFiles(t *testing.T) {
	report := ComplexityReport{
		Status:       "PASS",
		BudgetSource: "explicit",
		Summary:      ComplexitySummary{Insertions: 1, ChangedFiles: 1},
		LargestFiles: []ComplexityFileChange{{Path: "a.go", Insertions: 1, Category: "production"}},
	}
	text := ComplexityText(report)
	if !strings.Contains(text, "Complexity Gate: PASS") || !strings.Contains(text, "a.go [production]") {
		t.Fatalf("unexpected text: %s", text)
	}
}

func TestComplexityCountFileLinesMissing(t *testing.T) {
	dir := t.TempDir()
	if count := countFileLines(filepath.Join(dir, "missing.go")); count != 0 {
		t.Fatalf("expected missing file count 0, got %d", count)
	}
	empty := filepath.Join(dir, "empty.go")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if count := countFileLines(empty); count != 0 {
		t.Fatalf("expected empty file count 0, got %d", count)
	}
	oneLine := filepath.Join(dir, "one.go")
	if err := os.WriteFile(oneLine, []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	if count := countFileLines(oneLine); count != 1 {
		t.Fatalf("expected one line, got %d", count)
	}
}
