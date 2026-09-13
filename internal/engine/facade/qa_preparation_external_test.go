package facade

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func TestExternalShellQAPreparationIgnoresUnrelatedGo(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		command   coverage.Command
		wantError string
	}{
		{"shell", coverage.Command{Executable: "sh", Args: []string{"-c", "mkdir -p bin; cp entry bin/hello"}}, ""},
		{"declared-command-fails", coverage.Command{Executable: "sh", Args: []string{"-c", "exit 7"}}, "exit 7, expected 0"},
		{"declared-artifact-missing", coverage.Command{Executable: "sh", Args: []string{"-c", "exit 0"}}, "artifact bin/hello"},
		{"declared-tool-missing", coverage.Command{Executable: "formal-gates-fixture-missing-tool"}, "preparation tool formal-gates-fixture-missing-tool"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			document := strings.ReplaceAll(b1Document, "命令输出字符串 hello。", "执行 sh entry，stdout hello，stderr 空，退出码 0。")
			f := facadePreparationTask(t, []string{"blackbox"}, 1, "Implement shell entry", document)
			r, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			writePreparationFile(t, r.Regular.Preparation.Resources["development"], "entry", "printf hello")
			facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"entry"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "construct shell fixture", Observation: "entry written", Result: "PASS"}}, Handoff: "fixture"})
			r, err = f.Drive()
			if err != nil {
				t.Fatal(err)
			}
			d := facadeDesign(r, "blackbox")
			d.Manifest.SharedSteps = []coverage.SharedStep{{ID: "build", Procedure: "prepare shell entry", Command: &scenario.command, Artifacts: []string{"bin/hello"}}}
			d.Manifest.Cases[0].SharedSteps = []string{"build"}
			d.Manifest.Cases[0].Command = &coverage.Command{Executable: "sh", Args: []string{"-c", `sh "$FORMAL_GATES_QA_SHARED/bin/hello"`}}
			facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &d})
			r, err = f.Show()
			if err != nil {
				t.Fatal(err)
			}
			review := facadeReview(r, "blackbox")
			review.SharedStepDecisions = append(review.SharedStepDecisions, coverage.ReviewDecision{ID: "build", Status: coverage.StatusPass})
			facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: review})
			if _, err := f.Drive(); err != nil {
				t.Fatal(err)
			}
			x := facadeExecution(t, f, "blackbox-execution")
			nativeFixtureClaim(t, f, x)
			t.Setenv("GOTOOLCHAIN", "unavailable-unrelated-toolchain")
			if out, err := exec.Command("go", "env", "-json", "GOOS").CombinedOutput(); err == nil {
				t.Fatalf("fixture must reproduce broken unrelated Go environment: %s", out)
			} else {
				t.Logf("unrelated go env: %s (%v)", out, err)
			}
			receipt, err := f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, "qa.prepare", "prepare"), false)
			if scenario.wantError != "" {
				if err != nil || receipt.Status != "FAILED" || !strings.Contains(receipt.Message, scenario.wantError) {
					t.Fatalf("expected %q: %+v %v", scenario.wantError, receipt, err)
				}
				t.Log(receipt.Message)
				return
			}
			if err != nil || receipt.Status != "COMPLETED" {
				t.Fatalf("qa.prepare: %+v %v", receipt, err)
			}
			if _, err := os.Stat(filepath.Join(receipt.SharedWorkspace, "go.mod")); !os.IsNotExist(err) {
				t.Fatalf("expected non-Go workspace: %v", err)
			}
			for _, kind := range []string{"qa.target", "qa.collect"} {
				receipt, err = f.ExecuteTaskOperation(operationRequest(t, f, x.Claim.ActionID, kind, kind), false)
				if err != nil || receipt.Status != "COMPLETED" {
					t.Fatalf("%s: %+v %v", kind, receipt, err)
				}
			}
			if receipt.Output != "hello" || receipt.Stderr != "" || receipt.ExitCode != 0 {
				t.Fatalf("shell oracle: %+v", receipt)
			}
			t.Logf("qa.prepare → qa.target → qa.collect: stdout=%q stderr=%q exit=%d", receipt.Output, receipt.Stderr, receipt.ExitCode)
		})
	}
}
