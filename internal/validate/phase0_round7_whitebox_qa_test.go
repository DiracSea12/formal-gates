//go:build phase0whitebox

package validate

// These tests are the independently authored whitebox QA increment for the
// stage-0 admission/install refactor. They intentionally use fresh fixtures
// instead of depending on the delivery's existing test helpers.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func round7WriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func round7Run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func round7CopyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	round7WriteFile(t, destination, string(data), 0o700)
}

func round7RepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(sourceFile) {
		t.Fatal("could not locate the whitebox test source as an absolute path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func round7ChildEnvironment(overrides map[string]string) []string {
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

func round7LauncherDiscoveryChild(t *testing.T) bool {
	mode := os.Getenv("PHASE0_ROUND7_LAUNCHER_CHILD")
	if mode == "" {
		return false
	}
	root := os.Getenv("PHASE0_ROUND7_WORKFLOW_ROOT")
	packageRoot := os.Getenv("PHASE0_ROUND7_PACKAGE_ROOT")
	runID := "round7-" + mode
	_, err := Start(StartOptions{
		Root: root, PackageRoot: packageRoot, RunID: runID, Flow: "formal",
		RequirementSource: "requirements.md", VCS: "git", Split: "no",
	})
	if mode == "stable" {
		if err != nil {
			t.Fatalf("fixed launcher did not discover its admission binding: %v", err)
		}
		persisted, loadErr := LoadRunState(root, runID)
		if loadErr != nil {
			t.Fatalf("started run could not be reloaded from persisted state: %v", loadErr)
		}
		expectedRegistry := filepath.Join(os.Getenv("HOME"), ".formal-gates", "registry.json")
		if persisted.AdmissionRegistry != expectedRegistry || persisted.AdmissionRecordID != "round7-project" ||
			persisted.AdmissionRoot != root || persisted.AdmissionTarget != packageRoot ||
			persisted.AdmissionEpoch != 7 || persisted.AdmissionGeneration != 7 ||
			persisted.AdmissionLease != "round7-lease" || persisted.AdmissionToken != "round7-token" {
			t.Fatalf("persisted run lost the exact admission identity: %+v", persisted)
		}
		return true
	}
	if err == nil || !strings.Contains(err.Error(), "UNREGISTERED_INSTALL") || !strings.Contains(err.Error(), "stable launcher") {
		t.Fatalf("direct candidate executable was not rejected by launcher admission: %v", err)
	}
	if _, statErr := os.Stat(RunDir(root, runID)); !os.IsNotExist(statErr) {
		t.Fatalf("candidate rejection occurred after run-state creation: %v", statErr)
	}
	return true
}

func TestWhiteboxPhase0Round7StableLauncherDiscoveryFencesCandidateWrites(t *testing.T) {
	if round7LauncherDiscoveryChild(t) {
		return
	}
	root := t.TempDir()
	round7WriteFile(t, filepath.Join(root, "requirements.md"), "stage zero\n", 0o600)
	round7Run(t, root, "git", "init")
	round7Run(t, root, "git", "config", "user.email", "round7@example.invalid")
	round7Run(t, root, "git", "config", "user.name", "Round Seven QA")
	round7Run(t, root, "git", "add", "requirements.md")
	round7Run(t, root, "git", "commit", "-m", "baseline")

	packageRoot := round7RepoRoot(t)
	home := t.TempDir()
	launcher := filepath.Join(home, ".local", "bin", nativeBinaryName())
	candidate := filepath.Join(home, "candidate", nativeBinaryName())
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	round7CopyExecutable(t, testExecutable, launcher)
	round7CopyExecutable(t, testExecutable, candidate)

	canonicalTarget := canonicalRegistryPath(packageRoot)
	canonicalLauncher := canonicalRegistryPath(launcher)
	canonicalRoot := canonicalRegistryPath(root)
	stateRoot := canonicalRegistryPath(filepath.Join(root, ".gates"))
	resourceRoot := canonicalRegistryPath(filepath.Join(root, ".formal-gates-resources"))
	runtimeSibling := canonicalRegistryPath(filepath.Dir(packageRoot))
	record := RegistryRecord{
		ID: "round7-project", Target: canonicalTarget, LauncherPath: canonicalLauncher,
		Scope: "project", Host: "codex", ProjectRoot: canonicalRoot,
		StateRoot: stateRoot, ResourceRoot: resourceRoot, RuntimeSibling: runtimeSibling,
		Status: "active", Generation: 7, Lease: "round7-lease", Token: "round7-token",
		CanonicalPaths: map[string]string{
			"target": canonicalTarget, "launcher": canonicalLauncher, "projectRoot": canonicalRoot,
			"stateRoot": stateRoot, "resourceRoot": resourceRoot, "runtimeSibling": runtimeSibling,
		},
	}
	registry := filepath.Join(home, ".formal-gates", "registry.json")
	registryData, err := json.MarshalIndent(RegistryDocument{SchemaVersion: RegistrySchemaVersion, Epoch: 7, Records: []RegistryRecord{record}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	round7WriteFile(t, registry, string(registryData)+"\n", 0o600)
	// 文档化首启边界：发现式准入的 workflow start 同样要求已提交的 bootstrap
	// receipt（fixture 直接构造 registry，须补写该 receipt）。
	phase0WriteBootstrapReceipt(t, registry)

	baseEnvironment := map[string]string{
		"HOME": home, "USERPROFILE": home,
		"PHASE0_ROUND7_WORKFLOW_ROOT": root,
		"PHASE0_ROUND7_PACKAGE_ROOT":  packageRoot,
		"AI_AGENT":                    "", "CLAUDE_CODE_ENTRYPOINT": "", "CODEX_HOME": "", "CODEX_CLI_PATH": "",
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
			environment["PHASE0_ROUND7_LAUNCHER_CHILD"] = invocation.mode
			command := exec.Command(invocation.executable, "-test.run", "^TestWhiteboxPhase0Round7StableLauncherDiscoveryFencesCandidateWrites$", "-test.v")
			command.Dir = root
			command.Env = round7ChildEnvironment(environment)
			output, childErr := command.CombinedOutput()
			if childErr != nil {
				t.Fatalf("%s child failed: %v\n%s", invocation.mode, childErr, output)
			}
		})
	}
}
