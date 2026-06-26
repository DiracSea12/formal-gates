package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComplexityGitPassReviewAndFail(t *testing.T) {
	dir := initComplexityGitRepo(t)
	mustWrite(t, filepath.Join(dir, "feature.go"), strings.Repeat("package main\n", 20))

	maxNet := 100
	maxFiles := 2
	maxProd := 100
	report, result := Complexity(ComplexityOptions{
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
		BudgetSource: "explicit-overrides",
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
