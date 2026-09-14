package facade

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// Role judgments are protocol fixtures; Git, the external project's assertion,
// result acceptance, finalization and the dispatched gate diff are real.
func TestMixedWhiteboxFileRemainsInImplementationDiff(t *testing.T) {
	mixedWhiteboxDiff(t, false)
}

func TestMixedWhiteboxProductionEditRequiresRevision(t *testing.T) {
	mixedWhiteboxDiff(t, true)
}

func mixedWhiteboxDiff(t *testing.T, productionEdit bool) {
	t.Helper()
	document := strings.ReplaceAll(b1Document, "命令输出字符串 hello。", "在项目根目录执行 python3 entry，stdout 恰好为 hello，stderr 为空，退出码为 0。")
	document = strings.ReplaceAll(document, "运行后输出恰好是 hello。", "执行 python3 entry 后 stdout 恰好为 hello，stderr 为空，退出码为 0。")
	f := facadePreparationTask(t, []string{"whitebox"}, 1, "Implement the Python entry and its assertion in an external project", document)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	dev := run.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "entry", mixedProduction)
	writePreparationFile(t, dev, "probe", "import subprocess\nr = subprocess.run(['python3', 'entry'], capture_output=True)\nassert (r.returncode, r.stdout, r.stderr) == (0, b'hello', b'')\n")
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
	writePreparationFile(t, wb, "entry", mixedProduction+mixedAssertion)
	cmd := exec.Command("python3", "entry", "--test")
	cmd.Dir = wb
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("existing assertion: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
	}
	cmd = exec.Command("python3", "probe")
	cmd.Dir = wb
	if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("developer assertion: %q %v", output, err)
	}
	design := facadeDesign(run, "whitebox")
	design.TestFiles = []string{"probe"}
	if productionEdit {
		writePreparationFile(t, wb, "entry", strings.Replace(mixedProduction, "hello", "wrong", 1)+mixedAssertion)
	}
	// The public contract permits symbolic references, not only path::symbol.
	design.Manifest.Cases[0].TestRef = "exact command output assertion"
	design.Manifest.Cases[0].Command = &coverage.Command{Executable: "python3", Args: []string{"probe"}}
	result := protocol.PreparationResult{
		ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider,
		Identity: src.Identity, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate,
		Status: "DRAFT", Manifest: []string{"entry"}, Design: &design,
		Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: design.Manifest.Cases[0].TestRef,
			CoverageJudgment: "probe asserts entry stdout hello, empty stderr and exit 0",
			Procedure:        "python3 entry --test", Observation: "stdout empty; stderr empty; exit 0",
			Comparison: "new in-file assertion executed before review fixture", Result: "EXECUTED_PASS"}},
	}
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &result})
	nativeFixtureStop(t, f, src)
	run, err = f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	q := run.Regular.Preparation.QA["whitebox"]
	if q.Snapshot == run.Regular.Preparation.DevelopmentSnapshot || len(q.Manifest) != 1 {
		t.Fatal("mixed test edit was not snapshotted")
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

	args := input["testChanges"].(map[string]any)["diffArgs"].([]string)
	whiteboxDiff, err := gitprep.Git(wb, args...)
	if err != nil || !strings.Contains(whiteboxDiff, "+    assert greeting()") {
		t.Fatalf("independent review lacks mixed-file test content: %s %v", whiteboxDiff, err)
	}
	if productionEdit {
		if !strings.Contains(whiteboxDiff, `+    return "wrong"`) {
			t.Fatal("review cannot see production mistake")
		}
		src := nativeFixtureClaim(t, f, reviewTask)
		failed := facadeReview(run, "whitebox")
		failed.SourceDecisions[0].Status = coverage.StatusFail
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &protocol.PreparationResult{
			ActionID: reviewTask.Claim.ActionID, AttemptID: reviewTask.Claim.AttemptID, Provider: reviewTask.Claim.Provider,
			Identity: src.Identity, RequirementRevision: reviewTask.Claim.RequirementRevision, Candidate: reviewTask.Claim.Candidate,
			Status: "FAIL", Review: failed, Message: "Independent review fixture: entry changed production greeting from hello to wrong; undo production edit, retain in-file assertion",
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
		// The ordinary design revision fixes only the mistaken edit in the
		// existing mixed file, preserving the developer implementation.
		writePreparationFile(t, wb, "entry", mixedProduction+mixedAssertion)
		cmd = exec.Command("python3", "entry", "--test")
		cmd.Dir = wb
		if output, err := cmd.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("repaired assertion: %q %v", output, err)
		}
		design = facadeDesign(run, "whitebox")
		design.TestFiles = []string{"probe"}
		design.Manifest.Cases[0].TestRef = "entry in-file assertion"
		design.Manifest.Cases[0].Command = &coverage.Command{Executable: "python3", Args: []string{"entry", "--test"}}
		facadeResult(t, f, facadeExecution(t, f, "whitebox-design"), protocol.PreparationResult{
			Manifest: []string{"entry"}, Design: &design,
			Reuse: []protocol.TestReuse{{CaseID: "WHITEBOX:C1", TestRef: design.Manifest.Cases[0].TestRef,
				CoverageJudgment: "in-file assertion checks production output", Procedure: "python3 entry --test",
				Observation: "stdout empty; stderr empty; exit 0", Comparison: "exact assertion passed", Result: "EXECUTED_PASS"}},
		})
		run, err = f.Drive()
		if err != nil {
			t.Fatal(err)
		}
	}
	approved := facadeReview(run, "whitebox")
	approved.Scope = run.Regular.Preparation.QA["whitebox"].ReviewScope
	facadeResult(t, f, facadeExecution(t, f, "whitebox-review"), protocol.PreparationResult{
		Review:  approved,
		Message: "Independent review fixture: probe is test-only; entry contains unchanged production and an in-file assertion, so entry must remain in implementation review; complete whitebox diff changes only test content",
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
	args = material.input["implementation"].(map[string]any)["diffArgs"].([]string)
	diff, err := gitprep.Git(material.workspace, args...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "diff --git a/entry b/entry") || !strings.Contains(diff, `+    return "hello"`) {
		t.Errorf("production implementation missing from gate diff: %s", diff)
	}
	if !strings.Contains(diff, "+    assert greeting()") || strings.Contains(diff, `+    return "wrong"`) {
		t.Fatalf("final mixed file lost tests or retained production mistake: %s", diff)
	}
	if strings.Contains(diff, "diff --git a/probe b/probe") {
		t.Errorf("reviewed, executed, unchanged developer test remains in implementation diff; TestOwned=%v\n%s", run.Regular.Preparation.Candidate.TestOwned, diff)
	}
}

const mixedProduction = `import sys

def greeting():
    return "hello"

if __name__ == "__main__" and "--test" not in sys.argv:
    sys.stdout.write(greeting())
`

const mixedAssertion = `
if __name__ == "__main__" and "--test" in sys.argv:
    assert greeting() == "hello"
`

// Exercise the same workspace restoration used after incremental development.
// Whole-file retention is safe only while the file still matches either bound
// snapshot. A new production edit in a mixed file must stop restoration.
func TestMixedWhiteboxRetentionPreservesNewProduction(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := gitprep.Git(root, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "-q")
	git("config", "user.email", "fixture@example.invalid")
	git("config", "user.name", "fixture")
	writePreparationFile(t, root, "entry", mixedProduction)
	writePreparationFile(t, root, "other", "old production")
	git("add", ".")
	git("commit", "-qm", "development")
	base := git("rev-parse", "HEAD")
	writePreparationFile(t, root, "entry", mixedProduction+mixedAssertion)
	git("add", "entry")
	git("commit", "-qm", "whitebox test content")
	reviewed := git("rev-parse", "HEAD")
	for _, changedMixed := range []bool{false, true} {
		t.Run(fmt.Sprint(changedMixed), func(t *testing.T) {
			git("checkout", "--detach", base)
			writePreparationFile(t, root, "other", "new production")
			production := mixedProduction
			if changedMixed {
				production = strings.Replace(mixedProduction, "hello", "new hello", 1)
				writePreparationFile(t, root, "entry", production)
			}
			git("add", ".")
			git("commit", "-qm", "increment")
			current := git("rev-parse", "HEAD")
			f := &Facade{root: root, stateDir: t.TempDir()}
			p := &protocol.Preparation{BindingDigest: "mixed-retention", Resources: map[string]string{}, QA: map[string]*protocol.PreparedQA{
				"whitebox": {TestArtifactsBase: base, TestArtifactsFrom: reviewed, Manifest: []string{"entry"}},
			}}
			run := Run{RunID: "mixed-retention", Regular: &protocol.RegularState{Preparation: p}}
			op := protocol.GitPreparation{Kind: "whitebox-workspace", Before: current, ID: "restore"}
			for replay := 0; replay < 2; replay++ {
				receipt, err := f.performPreparationGit(run, op)
				workspace := filepath.Join(f.stateDir, "resources", p.BindingDigest, "whitebox-restore")
				if changedMixed {
					if err == nil || !strings.Contains(err.Error(), "ARTIFACT_CONFLICT") {
						t.Fatalf("new production not protected: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					workspace = receipt.Resources["whitebox"]
				}
				got, err := os.ReadFile(filepath.Join(workspace, "entry"))
				want := production
				if !changedMixed {
					want += mixedAssertion
				}
				if err != nil || string(got) != want {
					t.Fatalf("production overwritten: %q %v", got, err)
				}
				got, err = os.ReadFile(filepath.Join(workspace, "other"))
				if err != nil || string(got) != "new production" {
					t.Fatalf("unrelated production overwritten: %q %v", got, err)
				}
				cmd := exec.Command("python3", "entry")
				cmd.Dir = workspace
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				wantOutput := "hello"
				if changedMixed {
					wantOutput = "new hello"
				}
				if err := cmd.Run(); err != nil || stdout.String() != wantOutput || stderr.Len() != 0 {
					t.Fatalf("production oracle: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
				}
			}
		})
	}
}
