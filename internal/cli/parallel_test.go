package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

// buildCLIPostSnapshot drives the full CLI flow to the post-development
// snapshot with a full route (blackbox + whitebox QA + the quality gate), where
// the post-development stage expects parallel dispatch.
func buildCLIPostSnapshot(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	state := startCLIWorkflow(t, root, pkg, id)
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, _ = validate.LoadRunState(root, state.RunID)

	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules", "--mode", "whitebox", "--procedure", "go test ./...", "--oracle", "tests pass", "--test", "whitebox_delivered_test.go::TestWhiteboxDirectRules",
		"--case", "public workflow", "--mode", "blackbox", "--procedure", "run documented CLI", "--oracle", "observable success")
	reviewDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch, "--reviewer", "qa-session")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch,
		"--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS")

	developmentDispatch := cliPrepareAction(t, root, pkg, state.RunID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", developmentDispatch)
	state, _ = validate.LoadRunState(root, state.RunID)
	return state
}

// TestCLIParallelReminderGoesToStderr covers reminder channel: a
// state-changing workflow command in an under-parallelized stage emits the
// parallel reminder on stderr only, never polluting stdout's machine JSON.
func TestCLIParallelReminderGoesToStderr(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := buildCLIPostSnapshot(t, root, pkg, "cli-parallel")
	// 清掉冷却标记（snapshot 命令已写入一份同签名提醒），确保本次命令真正触发提醒。
	if err := os.Remove(filepath.Join(validate.RunDir(root, state.RunID), "parallel.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("prepare-gate failed: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "可并行") {
		t.Fatalf("stderr missing the parallel reminder: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "可并行") {
		t.Fatalf("parallel reminder polluted stdout machine output: %q", stdout.String())
	}
}
