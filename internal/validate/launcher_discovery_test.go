//go:build qastructure

package validate

// These tests pin launcher-driven admission discovery: a workflow start from
// the fixed stable launcher discovers its registry binding, while the same
// invocation from a candidate copy of the executable is fenced before any run
// state exists. They use fresh fixtures independent of the shared helpers.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func launcherWriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func launcherRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func launcherCopyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	launcherWriteFile(t, destination, string(data), 0o700)
}

func launcherRepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func launcherChildEnvironment(overrides map[string]string) []string {
	skip := map[string]bool{}
	for key := range overrides {
		skip[key] = true
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key := item
		if index := strings.IndexByte(item, '='); index >= 0 {
			key = item[:index]
		}
		if !skip[key] {
			environment = append(environment, item)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func TestStableLauncherDiscoveryFencesCandidateWrites(t *testing.T) {
	root := t.TempDir()
	launcherWriteFile(t, filepath.Join(root, "requirements.md"), "sample requirement\n", 0600)
	home := t.TempDir()
	source := copyPackageFixture(t)
	binaryPath := filepath.Join(source, "bin", nativeBinaryName())
	if err := os.Remove(binaryPath); err != nil {
		t.Fatal(err)
	}
	launcherRun(t, launcherRepoRoot(t), "go", "build", "-o", binaryPath, "./cmd/formal-gates")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	candidate := filepath.Join(home, "candidate", nativeBinaryName())
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	options := InstallOptions{Source: source, Host: "codex", Scope: "project", Project: root, RegistryPath: registry, BinaryTarget: launcher, Force: true, SkipHooks: true}
	report, err := Install(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Bootstrap = true
	if _, err = Install(options); err != nil {
		t.Fatal(err)
	}
	document, err := LoadRegistry(registry)
	if err != nil {
		t.Fatal(err)
	}
	record := admissionRecordByID(t, document, report.Targets[0].RegistryRecordID)
	packageRoot := record.Target
	launcherCopyExecutable(t, launcher, candidate)
	raw, err := os.ReadFile(filepath.Join(root, "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	revision := fmt.Sprintf("%x", sha256.Sum256(raw))
	receipt := protocol.IntakeConfirmationReceipt{Source: facade.DefaultIntakeSource, Authority: facade.DefaultIntakeAuthority, Transport: facade.DefaultIntakeTransport, RequirementSource: "requirements.md", RequirementRevision: revision, Artifacts: []protocol.IntakeArtifact{{Path: "requirements.md", Revision: revision}}, SolutionRevision: revision, SolutionDigest: "sha256:" + revision}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(t.TempDir(), "confirmation.json")
	launcherWriteFile(t, receiptPath, string(receiptBytes), 0600)
	baseEnvironment := map[string]string{
		"HOME": home, "USERPROFILE": home,
		"LAUNCHER_DISCOVERY_ROOT":    root,
		"LAUNCHER_DISCOVERY_PACKAGE": packageRoot,
		"AI_AGENT":                   "", "CLAUDE_CODE_ENTRYPOINT": "", "CODEX_HOME": "", "CODEX_CLI_PATH": "",
		"CURSOR_TRACE_ID": "", "CURSOR_RUNTIME": "", "DSH_HOME": "", "DSH_PROJECT_DIR": "",
	}
	for _, invocation := range []struct {
		mode       string
		executable string
	}{
		{mode: "stable", executable: launcher},
		{mode: "candidate", executable: candidate},
	} {
		t.Run(invocation.mode, func(t *testing.T) {
			environment := make(map[string]string, len(baseEnvironment)+1)
			for key, value := range baseEnvironment {
				environment[key] = value
			}
			runID := "launcher-" + invocation.mode
			command := exec.Command(invocation.executable, "workflow", "start", "--root", root, "--run-id", runID, "--route", "lightweight", "--intake-receipt", receiptPath)
			command.Dir = root
			command.Env = launcherChildEnvironment(environment)
			output, childErr := command.CombinedOutput()
			if invocation.mode == "stable" {
				if childErr != nil {
					t.Fatalf("stable launcher failed: %v\n%s", childErr, output)
				}
				persisted := admissionCurrentRequest(t, root, runID)
				if persisted.AdmissionRegistryPath != registry || persisted.InstalledTargetIdentity != record.ID || persisted.AdmissionRoot != root || persisted.AdmissionTarget != packageRoot || persisted.AdmissionGeneration != record.Generation || persisted.AdmissionLease != record.Lease || persisted.AdmissionToken != record.Token {
					t.Fatalf("persisted exact launcher binding lost: %+v", persisted)
				}
				after, err := LoadRegistry(registry)
				if err != nil || after.Epoch != document.Epoch {
					t.Fatalf("launcher start changed registry epoch: %+v %v", after, err)
				}
			} else {
				if childErr == nil || !strings.Contains(string(output), "UNREGISTERED_INSTALL:") || !strings.Contains(string(output), "no active registry record matches candidate package root") {
					t.Fatalf("candidate not fenced by actual launcher owner: %v\n%s", childErr, output)
				}
				if _, err := os.Stat(admissionEngineDirectory(root, runID)); !os.IsNotExist(err) {
					t.Fatalf("candidate rejection created authority: %v", err)
				}
			}
		})
	}
}
