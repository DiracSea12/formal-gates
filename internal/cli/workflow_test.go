package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIWorkflowUsesDispatchesKindsAndNativeSnapshots(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-run")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, _ = validate.LoadRunState(root, state.RunID)
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")

	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules", "--kind", "STATIC", "--procedure", "go test ./...", "--oracle", "tests pass",
		"--case", "public workflow", "--kind", "LIVE", "--procedure", "run documented CLI", "--oracle", "observable success")
	reviewDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch, "--reviewer", "qa-session")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch,
		"--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS")

	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID)

	executionDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-execution")
	runCLI(t, "workflow", "qa-execution", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", executionDispatch,
		"--case-result", "CASE-001", "--outcome", "PASS", "--procedure", "ran tests", "--observation", "passed", "--oracle-result", "matched",
		"--case-result", "CASE-002", "--outcome", "PASS", "--procedure", "ran CLI", "--observation", "succeeded", "--oracle-result", "matched")

	gateDispatch := cliPrepareGate(t, root, pkg, state.RunID, "quality")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", gateDispatch, "--reviewer", "gate-session")
	runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", gateDispatch, "--status", "PASS")

	out := runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID)
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != cliGit(t, root, "rev-parse", "HEAD") || state.QAExecution.Cases[0].Kind != "STATIC" || state.Gates["quality"].DispatchID != gateDispatch {
		t.Fatalf("CLI state lost native bindings: %s", out)
	}
	summary := runCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
	if !strings.Contains(summary, `"status": "SEALED"`) {
		t.Fatalf("seal output=%s", summary)
	}
}

func TestCLIRejectsRemovedSourceAndLiveSnapshotFlags(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "removed-bindings")
	dispatchID := cliPrepareAction(t, root, pkg, state.RunID, "requirements-clarification")
	before := cliStateBytes(t, root, state.RunID)
	for _, args := range [][]string{
		{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "requirements-clarification", "--dispatch", dispatchID, "--status", "PASS", "--source-revision", state.RequirementRevision},
		{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "requirements-clarification", "--live-snapshot", state.CurrentSnapshot},
	} {
		var stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("removed flag was accepted: code=%d err=%s", code, stderr.String())
		}
	}
	if cliStateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected removed binding flag changed state")
	}
}

func TestCLIRequirementArtifactsAreRegisteredAndSorted(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(root, "z-solution.md"), "solution\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "solution")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "artifact-cli", "--requirement", "requirements.md", "--requirement-artifact", "z-solution.md", "--vcs", "git"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("start failed: %s", stderr.String())
	}
	var state validate.RunState
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RequirementArtifacts) != 2 || state.RequirementArtifacts[0].Path != "requirements.md" || state.RequirementArtifacts[1].Path != "z-solution.md" {
		t.Fatalf("artifacts=%#v", state.RequirementArtifacts)
	}
}

func TestCLIGroupedQAReviewFieldsRequireCurrentCase(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "qa-review", "--outcome", "PASS"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "must follow --case") {
		t.Fatalf("misordered QA Review field accepted: code=%d err=%s", code, stderr.String())
	}
}

func TestCLIHelpListsDispatchAndQAReviewCommands(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	for _, want := range []string{"claim-dispatch", "qa-review"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help omitted %s: %s", want, stdout.String())
		}
	}
}

func startCLIWorkflow(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	out := runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git")
	var state validate.RunState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func cliRecordAction(t *testing.T, root, pkg string, state validate.RunState, action, status string) validate.RunState {
	t.Helper()
	dispatchID := cliPrepareAction(t, root, pkg, state.RunID, action)
	out := runCLI(t, "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", action, "--dispatch", dispatchID, "--status", status)
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func cliPrepareAction(t *testing.T, root, pkg, runID, action string) string {
	t.Helper()
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", action)
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	id := cliOpenDispatch(state, "action", action)
	if id == "" || !strings.Contains(prompt, id) {
		t.Fatalf("prepared prompt missing dispatch: %s", prompt)
	}
	return id
}

func cliPrepareGate(t *testing.T, root, pkg, runID, gate string) string {
	t.Helper()
	prompt := runCLI(t, "workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", runID, "--gate", gate)
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	id := cliOpenDispatch(state, "gate", gate)
	if id == "" || !strings.Contains(prompt, id) {
		t.Fatalf("prepared gate missing dispatch: %s", prompt)
	}
	return id
}

func cliOpenDispatch(state validate.RunState, kind, target string) string {
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == kind && dispatch.Target == target && (dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED") {
			return id
		}
	}
	return ""
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("%s failed: %s", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

func cliWorkflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), "requirement\n")
	mustWriteCLI(t, filepath.Join(root, "design.md"), "design\n")
	cliGit(t, root, "init")
	cliGit(t, root, "config", "user.email", "tests@example.invalid")
	cliGit(t, root, "config", "user.name", "Formal Gates CLI Tests")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "base")
	pkg := t.TempDir()
	mustWriteCLI(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "shared contract\n")
	for _, id := range []string{"requirements-clarification", "start-readiness", "qa-design", "qa-review", "qa-execution", "carry", "development-worker"} {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	mustWriteCLI(t, filepath.Join(pkg, "gates", "quality.md"), "quality checks\n")
	return root, pkg
}

func cliGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func cliStateBytes(t *testing.T, root, runID string) string {
	t.Helper()
	data, err := os.ReadFile(validate.RunStatePath(root, runID))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
