package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

func TestCLIWorkflowStartPrepareRecordShow(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	run := func(args ...string) (int, string, string) {
		var out, err bytes.Buffer
		code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &err})
		return code, out.String(), err.String()
	}
	code, _, stderr := run("workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "cli-run", "--requirement", "requirements.md", "--confirmed", "--vcs", "git", "--base-snapshot", "base", "--current-snapshot", "current")
	if code != 0 {
		t.Fatalf("start failed: %s", stderr)
	}
	state, err := validate.LoadRunState(root, "cli-run")
	if err != nil {
		t.Fatal(err)
	}
	code, prompt, stderr := run("workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", "cli-run", "--gate", "quality", "--live-snapshot", "current")
	if code != 0 || !strings.Contains(prompt, "[Shared reviewer contract]") {
		t.Fatalf("prepare failed code=%d err=%s prompt=%s", code, stderr, prompt)
	}
	code, _, stderr = run("workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", "cli-run", "--gate", "quality", "--status", "FAIL", "--finding", "broken behavior", "--location", "internal/x.go:10", "--source-revision", state.RequirementRevision, "--source-catalog-revision", state.CatalogRevision, "--source-snapshot", "current", "--live-snapshot", "current")
	if code != 0 {
		t.Fatalf("record failed: %s", stderr)
	}
	code, shown, stderr := run("workflow", "show", "--root", root, "--run-id", "cli-run")
	if code != 0 {
		t.Fatalf("show failed: %s", stderr)
	}
	if err := json.Unmarshal([]byte(shown), &state); err != nil {
		t.Fatal(err)
	}
	result := state.Gates["quality"]
	if result.Status != "FAIL" || len(result.Findings) != 1 || len(result.Findings[0].Locations) != 1 {
		t.Fatalf("semantic result lost: %#v", result)
	}
}

func TestCLIQADesignGeneratesCaseIDsAndRejectsMisorderedFields(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "qa-cli")
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", "qa-cli", "--source-revision", state.RequirementRevision, "--source-catalog-revision", state.CatalogRevision, "--procedure", "before case"}, IO{Stderr: &stderr}); code == 0 {
		t.Fatal("misordered case field accepted")
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", "qa-cli", "--source-revision", state.RequirementRevision, "--source-catalog-revision", state.CatalogRevision, "--case", "behavior", "--procedure", "exercise", "--oracle", "observed", "--case", "second behavior", "--procedure", "exercise again", "--oracle", "second observation"}, IO{Stderr: &stderr}); code != 0 {
		t.Fatalf("QA design failed: %s", stderr.String())
	}
	state, err := validate.LoadRunState(root, "qa-cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QACases) != 2 || state.QACases[0].ID != "CASE-001" || state.QACases[1].ID != "CASE-002" {
		t.Fatalf("case ID was not generated: %#v", state.QACases)
	}
}

func TestCLIGroupedSemanticFieldsRejectDuplicatesWithoutStateMutation(t *testing.T) {
	tests := []struct {
		name    string
		command string
		group   []string
		flag    string
	}{
		{name: "QA Design procedure", command: "qa-design", group: []string{"--case", "behavior"}, flag: "procedure"},
		{name: "QA Design oracle", command: "qa-design", group: []string{"--case", "behavior"}, flag: "oracle"},
		{name: "QA Execution outcome", command: "qa-execution", group: []string{"--case-result", "CASE-001"}, flag: "outcome"},
		{name: "QA Execution procedure", command: "qa-execution", group: []string{"--case-result", "CASE-001"}, flag: "procedure"},
		{name: "QA Execution observation", command: "qa-execution", group: []string{"--case-result", "CASE-001"}, flag: "observation"},
		{name: "QA Execution oracle", command: "qa-execution", group: []string{"--case-result", "CASE-001"}, flag: "oracle-result"},
		{name: "Carry decision", command: "carry", group: []string{"--gate", "quality"}, flag: "decision"},
		{name: "Carry reason", command: "carry", group: []string{"--gate", "quality"}, flag: "reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, pkg := cliWorkflowFixture(t)
			state := startCLIWorkflow(t, root, pkg, "duplicate-field")
			statePath := validate.RunStatePath(root, state.RunID)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"workflow", test.command, "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--source-revision", state.RequirementRevision, "--source-catalog-revision", state.CatalogRevision}
			args = append(args, test.group...)
			args = append(args, "--"+test.flag, "first", "--"+test.flag, "second")
			var stderr bytes.Buffer
			if code := Run("formal-gates", args, IO{Stderr: &stderr}); code == 0 {
				t.Fatalf("duplicate --%s was accepted", test.flag)
			}
			if !strings.Contains(stderr.String(), "duplicate --"+test.flag) {
				t.Fatalf("unexpected error for duplicate --%s: %s", test.flag, stderr.String())
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("duplicate --%s mutated workflow state", test.flag)
			}
		})
	}
}

func TestCLIRemovedHeavyCommandsAreUnavailable(t *testing.T) {
	for _, command := range []string{"artifact", "handoff", "prompt", "receipt", "policy", "gate"} {
		var stderr bytes.Buffer
		if code := Run("formal-gates", []string{command}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "unknown command") {
			t.Fatalf("removed command %s remains available: code=%d stderr=%s", command, code, stderr.String())
		}
	}
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "repair"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "unknown workflow subcommand") {
		t.Fatalf("removed repair alias remains available: code=%d stderr=%s", code, stderr.String())
	}
}

func cliWorkflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), "requirement\n")
	pkg := t.TempDir()
	mustWriteCLI(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "shared contract\n")
	for _, id := range []string{"requirements-clarification", "start-readiness", "qa-design", "qa-execution", "carry", "development-worker"} {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	mustWriteCLI(t, filepath.Join(pkg, "gates", "quality.md"), "quality checks\n")
	return root, pkg
}
func startCLIWorkflow(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	var stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--confirmed", "--vcs", "git", "--base-snapshot", "base", "--current-snapshot", "current"}, IO{Stderr: &stderr})
	if code != 0 {
		t.Fatalf("start failed: %s", stderr.String())
	}
	if _, err := os.Stat(validate.RunStatePath(root, id)); err != nil {
		t.Fatal(err)
	}
	state, err := validate.LoadRunState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
