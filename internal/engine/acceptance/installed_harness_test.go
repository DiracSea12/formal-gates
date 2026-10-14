package acceptance_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
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
}

func buildInstalledHarnessBinary(t *testing.T, root, packagePath, name string) string {
	t.Helper()
	bin := executablePath(filepath.Join(t.TempDir(), name))
	buildInstalledBinaryAt(t, root, packagePath, bin)
	return bin
}

func executablePath(path string) string {
	if runtime.GOOS == "windows" && strings.ToLower(filepath.Ext(path)) != ".exe" {
		return path + ".exe"
	}
	return path
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

// TestAcceptanceInstalledProtocolHarness proves the test-only protocol
// entry is runnable from independently built binaries and that its writes
// remain in the project state namespace across a real process restart.
func TestAcceptanceInstalledProtocolHarness(t *testing.T) {
	root, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	candidate := executablePath(filepath.Join(home, ".local", "bin", "formal-gates"))
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
	// 持久化工件按序核对：state.json、持久的锁工件（write.lock，内核文
	// 件锁永不删除）、workspace 副作用与 spawn 标记。
	if len(second.Paths) != 4 || second.Paths[0] != "engine-state/state.json" || second.Paths[1] != "engine-state/write.lock" || second.Paths[2] != "workspace/.fake-vcs-operations.json" || !strings.HasPrefix(second.Paths[3], "workspace/fake-host/spawn/") {
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

	terminalProject, err := testkit.NewIsolatedProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	terminalOutput, err := terminalProject.RunInstalled(harness, "--scenario", "terminal")
	if err != nil {
		t.Fatalf("installed terminal scenario: %v\n%s", err, terminalOutput)
	}
	terminal := decodeHarnessReport(t, terminalOutput)
	if terminal.Terminal == nil || terminal.Terminal.Status != "ABORTED" || len(terminal.Next) == 0 || terminal.Next[len(terminal.Next)-1].Kind != decision.KindComplete {
		t.Fatalf("installed terminal report = %+v", terminal)
	}
	queryOutput, err := terminalProject.RunInstalled(harness, "--scenario", "query-terminal")
	if err != nil {
		t.Fatalf("installed terminal query: %v\n%s", err, queryOutput)
	}
	replayOutput, err := terminalProject.RunInstalled(harness, "--scenario", "terminal-replay")
	if err != nil {
		t.Fatalf("installed terminal replay: %v\n%s", err, replayOutput)
	}
	if query := decodeHarnessReport(t, queryOutput); query.Terminal == nil || query.Terminal.Revision != terminal.Terminal.Revision {
		t.Fatalf("installed terminal query = %+v", query)
	}
	if replay := decodeHarnessReport(t, replayOutput); len(replay.Next) != 1 || replay.Next[0].Kind != decision.KindComplete {
		t.Fatalf("installed terminal replay = %+v", replay)
	}

	final, err := project.Snapshot()
	if err != nil {
		t.Fatalf("final project snapshot: %v", err)
	}
	changes := (&testkit.FakeVCS{Root: project.Root}).Diff(before, final)
	// 引擎命名空间只产出持久化状态与锁工件；write.lock 是持久的目录互
	// 斥工件（内核文件锁永不删除）。
	if len(changes) != 4 || changes[0].Path != "engine-state/state.json" || changes[1].Path != "engine-state/write.lock" || changes[2].Path != "workspace/.fake-vcs-operations.json" || !strings.HasPrefix(changes[3].Path, "workspace/fake-host/spawn/") {
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

// The source tree's bin/ holds the caller-built candidate binary, so an
// install from this source cannot silently substitute a stale checkout
// bin as the candidate executable.
func isolatedInstallSource(t *testing.T, root, candidate string) string {
	t.Helper()
	receipt, err := validate.PackageReceipt(root)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	copyFile := func(from, to string, mode os.FileMode) {
		t.Helper()
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range receipt.Entries {
		rel := filepath.FromSlash(entry.Path)
		copyFile(filepath.Join(root, rel), filepath.Join(source, rel), os.FileMode(entry.Mode))
	}
	copyFile(candidate, filepath.Join(source, "bin", filepath.Base(candidate)), 0o700)
	return source
}
