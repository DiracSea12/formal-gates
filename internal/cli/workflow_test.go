package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	state := startCLIWorkflow(t, root, pkg, "cli-run")
	var err error
	state, err = validate.RecordAction(root, pkg, state.RunID, "start-readiness", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = validate.RecordQADesign(root, pkg, state.RunID, []validate.QACaseInput{{Description: "behavior", Procedure: "exercise", Oracle: "observed"}}, "", state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = validate.RecordAction(root, pkg, state.RunID, "qa-review", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validate.PrepareAction(root, pkg, state.RunID, "development-worker", "base"); err != nil {
		t.Fatal(err)
	}
	state, err = validate.AdvanceSnapshot(root, pkg, state.RunID, "current", "current")
	if err != nil {
		t.Fatal(err)
	}
	code, prompt, stderr := run("workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", "cli-run", "--gate", "quality", "--live-snapshot", "current")
	if code != 0 || !strings.Contains(prompt, "[Shared reviewer contract]") {
		t.Fatalf("prepare failed code=%d err=%s prompt=%s", code, stderr, prompt)
	}
	code, _, stderr = run("workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", "cli-run", "--gate", "quality", "--status", "FAIL", "--finding", "broken behavior", "--severity", "P1", "--location", "internal/x.go:10", "--source-revision", state.RequirementRevision, "--source-catalog-revision", state.CatalogRevision, "--source-snapshot", "current", "--live-snapshot", "current")
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

func TestCLIRouteCommandsExposeOrderedCandidatesAndPersistSelection(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "route-cli")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "route-candidates", "--root", root, "--package-root", pkg, "--run-id", state.RunID}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("route candidates failed: %s", stderr.String())
	}
	var candidates []string
	if err := json.Unmarshal(stdout.Bytes(), &candidates); err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0] != "qa" || candidates[1] != "quality" {
		t.Fatalf("candidates=%v", candidates)
	}
	// startCLIWorkflow records a full route for parser setup; use a fresh run for route mutation.
	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "custom-cli", "--requirement", "requirements.md", "--vcs", "git", "--base-snapshot", "base"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("start failed: %s", stderr.String())
	}
	custom, err := validate.LoadRunState(root, "custom-cli")
	if err != nil {
		t.Fatal(err)
	}
	custom, err = validate.RecordAction(root, pkg, custom.RunID, "requirements-clarification", "PASS", "", nil, custom.RequirementRevision, custom.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	custom, err = validate.UpdateRequirement(root, pkg, custom.RunID, "", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", custom.RunID, "--mode", "custom", "--gate", "quality"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("custom route failed: %s", stderr.String())
	}
	custom, err = validate.LoadRunState(root, custom.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if custom.RouteMode != "custom" || len(custom.SelectedGates) != 1 || custom.SelectedGates[0] != "quality" {
		t.Fatalf("custom route=%#v", custom)
	}

	none, err := validate.Start(validate.StartOptions{Root: root, PackageRoot: pkg, RunID: "none-cli", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", BaseSnapshot: "base"})
	if err != nil {
		t.Fatal(err)
	}
	none, err = validate.RecordAction(root, pkg, none.RunID, "requirements-clarification", "PASS", "", nil, none.RequirementRevision, none.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	none, err = validate.UpdateRequirement(root, pkg, none.RunID, "", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", none.RunID, "--mode", "none"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("none route failed: %s", stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &none); err != nil {
		t.Fatal(err)
	}
	if none.SelectedGates == nil {
		t.Fatalf("none route response encoded selectedGates as null: %s", stdout.String())
	}
	persisted, err := validate.LoadRunState(root, none.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SelectedGates == nil || !reflect.DeepEqual(none.SelectedGates, persisted.SelectedGates) {
		t.Fatalf("none route response and persisted state disagree: response=%#v persisted=%#v", none.SelectedGates, persisted.SelectedGates)
	}
}

func TestCLIPackageRouteCandidatesIsStateless(t *testing.T) {
	_, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(pkg, "gates", "architecture.md"), "architecture checks\n")
	before, err := os.ReadDir(pkg)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"package", "route-candidates", "--root", pkg}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("package route candidates failed: %s", stderr.String())
	}
	var candidates []string
	if err := json.Unmarshal(stdout.Bytes(), &candidates); err != nil {
		t.Fatal(err)
	}
	if got, want := candidates, []string{"qa", "architecture", "quality"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates=%v want=%v", got, want)
	}

	after, err := os.ReadDir(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(directoryEntryNames(before), directoryEntryNames(after)) {
		t.Fatalf("package query changed package entries: before=%v after=%v", directoryEntryNames(before), directoryEntryNames(after))
	}
}

func directoryEntryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestCLIRequirementRevisionRequiresAndRebindsLiveSnapshot(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "requirement-cli")
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), "updated wording\n")
	var stdout, stderr bytes.Buffer
	args := []string{"workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--meaning", "preserved"}
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "live VCS snapshot") {
		t.Fatalf("requirement revision was rebound without live snapshot: code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	args = append(args, "--live-snapshot", "updated-requirement-snapshot")
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("requirement revision rebind failed: %s", stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != "updated-requirement-snapshot" {
		t.Fatalf("requirement response kept a stale snapshot: %#v", state)
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
		{name: "Gate finding severity", command: "record-gate", group: []string{"--finding", "problem"}, flag: "severity"},
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
	for _, id := range []string{"requirements-clarification", "start-readiness", "qa-design", "qa-review", "qa-execution", "carry", "development-worker"} {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	mustWriteCLI(t, filepath.Join(pkg, "gates", "quality.md"), "quality checks\n")
	return root, pkg
}
func startCLIWorkflow(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	var stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--vcs", "git", "--base-snapshot", "base"}, IO{Stderr: &stderr})
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
	state, err = validate.RecordAction(root, pkg, id, "requirements-clarification", "PASS", "", nil, state.RequirementRevision, state.CatalogRevision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = validate.UpdateRequirement(root, pkg, id, "", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = validate.SetRoute(root, pkg, id, "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
