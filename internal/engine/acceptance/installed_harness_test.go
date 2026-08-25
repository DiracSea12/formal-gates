package acceptance_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/validate"
)

type installedHarnessReport struct {
	Status   string                     `json:"status"`
	Phase    string                     `json:"phase"`
	Recovery persistence.RecoveryReport `json:"recovery"`
	Summary  testkit.StateSummary       `json:"summary"`
	Paths    []string                   `json:"paths"`
	Terminal *testkit.TerminalSummary   `json:"terminal"`
	Next     []testkit.NextRecord       `json:"next"`
	Effects  map[string]int             `json:"sideEffects"`
}

func buildInstalledHarnessBinary(t *testing.T, root, packagePath, name string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), name)
	buildInstalledBinaryAt(t, root, packagePath, bin)
	return bin
}

func buildInstalledBinaryAt(t *testing.T, root, packagePath, bin string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, packagePath)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, output)
	}
}

func decodeHarnessReport(t *testing.T, output []byte) installedHarnessReport {
	t.Helper()
	var report installedHarnessReport
	if err := json.Unmarshal(bytes.TrimSpace(output), &report); err != nil {
		t.Fatalf("decode harness output %q: %v", output, err)
	}
	return report
}

// TestAcceptanceInstalledProtocolHarness proves the phase-2 test-only entry
// is runnable from independently built binaries and that its writes remain in
// the project state namespace across a real process restart.
func TestAcceptanceInstalledProtocolHarness(t *testing.T) {
	root, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	candidate := filepath.Join(home, ".local", "bin", "formal-gates")
	buildInstalledBinaryAt(t, root, "./cmd/formal-gates", candidate)
	harness := buildInstalledHarnessBinary(t, root, "./internal/engine/testkit/cmd/harness", "engine-harness")
	project, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatalf("new isolated project: %v", err)
	}

	before, err := project.Snapshot()
	if err != nil {
		t.Fatalf("snapshot before candidate: %v", err)
	}
	if output, err := project.RunInstalled(candidate, "--help"); err != nil {
		t.Fatalf("candidate public entry smoke: %v\n%s", err, output)
	}
	afterSmoke, err := project.Snapshot()
	if err != nil {
		t.Fatalf("snapshot after candidate: %v", err)
	}
	if changes := (&testkit.FakeVCS{Root: project.Root}).Diff(before, afterSmoke); len(changes) != 0 {
		t.Fatalf("candidate public smoke wrote isolated project: %+v", changes)
	}

	firstOutput, err := project.RunInstalled(harness)
	if err != nil {
		t.Fatalf("installed harness first process: %v\n%s", err, firstOutput)
	}
	first := decodeHarnessReport(t, firstOutput)
	if first.Status != "PASS" || first.Phase != "initial" || first.Summary.Revision == 0 {
		t.Fatalf("first harness report = %+v", first)
	}

	secondOutput, err := project.RunInstalled(harness)
	if err != nil {
		t.Fatalf("installed harness restart process: %v\n%s", err, secondOutput)
	}
	second := decodeHarnessReport(t, secondOutput)
	if second.Status != "PASS" || second.Phase != "restart" || second.Recovery.Outcome != persistence.RecoveryClean {
		t.Fatalf("second harness report = %+v", second)
	}
	if !reflect.DeepEqual(first.Summary, second.Summary) || !reflect.DeepEqual(first.Paths, second.Paths) {
		t.Fatalf("restart changed durable report: first=%+v second=%+v", first, second)
	}
	if len(second.Paths) != 3 || second.Paths[0] != "engine-state/state.json" || second.Paths[1] != "workspace/.fake-vcs-operations.json" || !strings.HasPrefix(second.Paths[2], "workspace/fake-host/spawn/") {
		t.Fatalf("harness paths = %v", second.Paths)
	}

	decisionProject, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	decisionOutput, err := decisionProject.RunInstalled(harness, "--scenario", "next-sequence")
	if err != nil {
		t.Fatalf("installed decision scenarios: %v\n%s", err, decisionOutput)
	}
	decisionReport := decodeHarnessReport(t, decisionOutput)
	wantKinds := []decision.Kind{decision.KindReady, decision.KindAsk, decision.KindWait, decision.KindHostAction, decision.KindOperator, decision.KindComplete}
	if len(decisionReport.Next) != len(wantKinds) {
		t.Fatalf("installed NextResult sequence = %+v", decisionReport.Next)
	}
	for index, want := range wantKinds {
		if decisionReport.Next[index].Kind != want {
			t.Fatalf("installed NextResult[%d] = %s, want %s", index, decisionReport.Next[index].Kind, want)
		}
	}

	fullProject, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fullOutput, err := fullProject.RunInstalled(harness, "--scenario", "full")
	if err != nil {
		t.Fatalf("installed full scenario: %v\n%s", err, fullOutput)
	}
	full := decodeHarnessReport(t, fullOutput)
	if full.Terminal == nil || full.Terminal.Status != "COMPLETE" || len(full.Next) == 0 || full.Next[len(full.Next)-1].Kind != decision.KindComplete || len(full.Effects) != 3 {
		t.Fatalf("installed full terminal report = %+v", full)
	}
	for name, count := range full.Effects {
		if count != 1 {
			t.Fatalf("installed full side effect %s count=%d", name, count)
		}
	}
	queryOutput, err := fullProject.RunInstalled(harness, "--scenario", "query-terminal")
	if err != nil {
		t.Fatalf("installed terminal query: %v\n%s", err, queryOutput)
	}
	replayOutput, err := fullProject.RunInstalled(harness, "--scenario", "terminal-replay")
	if err != nil {
		t.Fatalf("installed terminal replay: %v\n%s", err, replayOutput)
	}
	if query := decodeHarnessReport(t, queryOutput); query.Terminal == nil || query.Terminal.Revision != full.Terminal.Revision {
		t.Fatalf("installed terminal query = %+v", query)
	}
	if replay := decodeHarnessReport(t, replayOutput); len(replay.Next) != 1 || replay.Next[0].Kind != decision.KindComplete {
		t.Fatalf("installed terminal replay = %+v", replay)
	}

	runInstalledLegacyRegression(t, candidate, home, root)

	final, err := project.Snapshot()
	if err != nil {
		t.Fatalf("final project snapshot: %v", err)
	}
	changes := (&testkit.FakeVCS{Root: project.Root}).Diff(before, final)
	if len(changes) != 3 || changes[0].Path != "engine-state/state.json" || changes[1].Path != "workspace/.fake-vcs-operations.json" || !strings.HasPrefix(changes[2].Path, "workspace/fake-host/spawn/") {
		t.Fatalf("installed harness namespace diff = %+v", changes)
	}
	for _, stablePath := range []string{project.StableState, project.StableRun} {
		entries, err := os.ReadDir(stablePath)
		if err != nil {
			t.Fatalf("read stable namespace %s: %v", stablePath, err)
		}
		if len(entries) != 0 {
			t.Fatalf("stable namespace %s was modified: %v", stablePath, entries)
		}
	}
}

func runInstalledLegacyRegression(t *testing.T, candidate, home, source string) {
	t.Helper()
	project := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(project, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("requirements.md", "requirement\n")
	write("design.md", "design\n")
	write(".gitignore", ".codex/\n.gates/tmp/\n.gates/results\n")
	for _, args := range [][]string{{"init"}, {"config", "user.email", "acceptance@example.invalid"}, {"config", "user.name", "Acceptance"}, {"add", "."}, {"commit", "-m", "baseline"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	runCandidate(t, candidate, home, project, "install", "--source", source, "--host", "codex", "--scope", "project", "--project", project, "--skip-hooks", "--force")
	runCandidate(t, candidate, home, project, "install", "--source", source, "--host", "codex", "--scope", "project", "--project", project, "--binary-target", candidate, "--bootstrap", "--force")
	installedPackage := filepath.Join(project, ".codex", "skills", "formal-gates")
	runID := "installed-legacy-regression"
	runCandidate(t, candidate, home, project, "workflow", "start", "--root", project, "--package-root", installedPackage, "--run-id", runID, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git", "--split", "no")
	runCandidate(t, candidate, home, project, "workflow", "prepare-action", "--root", project, "--package-root", installedPackage, "--run-id", runID, "--action", "requirements-clarification")
	state := installedLegacyState(t, candidate, home, project, runID)
	dispatchID := ""
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == "action" && dispatch.Target == "requirements-clarification" && dispatch.Status == "OPEN" {
			dispatchID = id
		}
	}
	if dispatchID == "" {
		t.Fatal("installed legacy requirements-clarification dispatch was not prepared")
	}
	runCandidate(t, candidate, home, project, "workflow", "record-action", "--root", project, "--package-root", installedPackage, "--run-id", runID, "--action", "requirements-clarification", "--dispatch", dispatchID, "--status", "PASS")
	runCandidate(t, candidate, home, project, "workflow", "requirement", "--root", project, "--package-root", installedPackage, "--run-id", runID, "--confirmed")
	state = installedLegacyState(t, candidate, home, project, runID)
	if state.Actions["requirements-clarification"].Status != "PASS" || !state.RequirementConfirmed {
		t.Fatalf("installed legacy requirement flow = %+v", state)
	}
	runCandidate(t, candidate, home, project, "workflow", "abort", "--root", project, "--run-id", runID, "--user-confirm")
}

func installedLegacyState(t *testing.T, candidate, home, project, runID string) validate.RunState {
	t.Helper()
	output := runCandidate(t, candidate, home, project, "workflow", "show", "--root", project, "--run-id", runID)
	var state validate.RunState
	if err := json.Unmarshal(output, &state); err != nil {
		t.Fatalf("decode installed legacy state: %v\n%s", err, output)
	}
	return state
}

func runCandidate(t *testing.T, candidate, home, workdir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(candidate, args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"AI_AGENT=",
		"CLAUDE_CODE_ENTRYPOINT=",
		"CODEX_HOME=",
		"CODEX_CLI_PATH=",
		"CURSOR_TRACE_ID=",
		"CURSOR_RUNTIME=",
		"DSH_HOME=",
		"DSH_PROJECT_DIR=",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("candidate %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}
