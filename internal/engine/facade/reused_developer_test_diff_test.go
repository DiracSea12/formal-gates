package facade

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// Role judgments are protocol fixtures; Git, the external project's assertion,
// result acceptance, finalization and the dispatched gate diff are real.
func TestReusedDeveloperTestExcludedFromImplementationDiff(t *testing.T) {
	reusedDeveloperTestDiff(t, false)
}

func TestDeclaredProductionFileNeedsIndependentWhiteboxReview(t *testing.T) {
	reusedDeveloperTestDiff(t, true)
}

func reusedDeveloperTestDiff(t *testing.T, declareProduction bool) {
	t.Helper()
	document := strings.ReplaceAll(b1Document, "命令输出字符串 hello。", "在项目根目录执行 sh entry，stdout 恰好为 hello，stderr 为空，退出码为 0。")
	document = strings.ReplaceAll(document, "运行后输出恰好是 hello。", "执行 sh entry 后 stdout 恰好为 hello，stderr 为空，退出码为 0。")
	f := facadePreparationTask(t, []string{"whitebox"}, 1, "Implement the shell entry and its assertion in an external project", document)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	dev := run.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "entry", "printf hello")
	writePreparationFile(t, dev, "probe", "import subprocess\nr = subprocess.run(['sh', 'entry'], capture_output=True)\nassert (r.returncode, r.stdout, r.stderr) == (0, b'hello', b'')\n")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{
		Manifest:   []string{"entry", "probe"},
		Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "construct external production and developer test fixture", Observation: "entry and probe written", Result: "PASS"}},
		Handoff:    "No unresolved decisions",
	})
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	x := facadeExecution(t, f, "whitebox-design")
	// Persist independent coverage before executing the existing test.
	src := nativeFixtureClaim(t, f, x)
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	wb := run.Regular.Preparation.Resources["whitebox"]
	cmd := exec.Command("python3", "probe")
	cmd.Dir = wb
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("existing assertion: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
	design := facadeDesign(run, "whitebox")
	design.TestFiles = []string{"probe"}
	if declareProduction {
		design.TestFiles = append(design.TestFiles, "entry")
	}
	// The public contract permits symbolic references, not only path::symbol.
	design.Manifest.Cases[0].TestRef = "exact command output assertion"
	design.Manifest.Cases[0].Command = &coverage.Command{Executable: "python3", Args: []string{"probe"}}
	result := protocol.PreparationResult{
		ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider,
		Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate,
		Status: "DRAFT", Manifest: []string{}, Design: &design,
		Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: design.Manifest.Cases[0].TestRef,
			CoverageJudgment: "probe asserts entry stdout hello, empty stderr and exit 0",
			Procedure:        "python3 probe", Observation: "stdout empty; stderr empty; exit 0",
			Comparison: "actual assertion passed without changing either file", Result: "EXECUTED_PASS"}},
	}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	nativeFixtureStop(t, f, src)
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	q := run.Regular.Preparation.QA["whitebox"]
	if q.Snapshot != run.Regular.Preparation.DevelopmentSnapshot || len(q.Manifest) != 0 {
		t.Fatal("reuse changed the developer snapshot")
	}
	if protocol.WhiteboxPromotionReady(run.Regular.Preparation) {
		t.Fatal("testFiles declaration alone authorized promotion")
	}
	if _, err := f.performPreparationGit(run, protocol.GitPreparation{Kind: "finalize", Before: q.Snapshot, ID: "unreviewed-declaration"}); err == nil || !strings.Contains(err.Error(), "WHITEBOX_REVIEW_REQUIRED") {
		t.Fatalf("unreviewed declaration reached finalization: %v", err)
	}
	reviewTask := facadeExecution(t, f, "whitebox-review")
	input, _, err := preparationInput(run, reviewTask.Claim)
	if err != nil || input["design"].(*protocol.QADesign).TestFiles[0] != "probe" {
		t.Fatal("independent review did not receive testFiles", err)
	}
	prompt, _, err := preparationPrompt(run, reviewTask.Claim)
	if err != nil || !strings.Contains(prompt, "design.testFiles") || !strings.Contains(prompt, "零改动直接复用") {
		t.Fatal("actual review prompt omits declared-file review", err)
	}
	if declareProduction {
		src := nativeFixtureClaim(t, f, reviewTask)
		failed := facadeReview(run, "whitebox")
		failed.SourceDecisions[0].Status = coverage.StatusFail
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &protocol.PreparationResult{
			ActionID: reviewTask.Claim.ActionID, AttemptID: reviewTask.Claim.AttemptID, Provider: reviewTask.Claim.Provider,
			Identity: src.Identity, RequirementRevision: reviewTask.Claim.RequirementRevision, Candidate: reviewTask.Claim.Candidate,
			Status: "FAIL", Review: failed, Message: "Independent review fixture: entry is production invoked by probe; remove entry from testFiles",
		}})
		nativeFixtureStop(t, f, src)
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		if run.Regular.Preparation.Candidate != nil || protocol.WhiteboxPromotionReady(run.Regular.Preparation) {
			t.Fatal("failed independent review allowed production exclusion")
		}
		if _, err := f.performPreparationGit(run, protocol.GitPreparation{Kind: "finalize", Before: q.Snapshot, ID: "failed-declaration"}); err == nil {
			t.Fatal("failed declaration reached candidate")
		}
		return
	}
	facadeResult(t, f, facadeExecution(t, f, "whitebox-review"), protocol.PreparationResult{
		Review:  facadeReview(run, "whitebox"),
		Message: "Independent review fixture: probe contains only the exact subprocess assertion; entry is production; no whitebox edits",
	})
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	gate := facadeExecution(t, f, "implementation-quality-gate")
	material, err := prepareVerificationPrompt(run, gate.Claim)
	if err != nil {
		t.Fatal(err)
	}
	args := material.input["implementation"].(map[string]any)["diffArgs"].([]string)
	diff, err := gitprep.Git(material.workspace, args...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "diff --git a/entry b/entry") || !strings.Contains(diff, "+printf hello") {
		t.Errorf("production implementation missing from gate diff: %s", diff)
	}
	if strings.Contains(diff, "diff --git a/probe b/probe") {
		t.Errorf("reviewed, executed, unchanged developer test remains in implementation diff; TestOwned=%v\n%s", run.Regular.Preparation.Candidate.TestOwned, diff)
	}
}
