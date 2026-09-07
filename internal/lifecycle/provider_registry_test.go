package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProviderRegistryRepeatedPathsHaveConstantFileProbes(t *testing.T) {
	const childKey = "FORMAL_GATES_TEST_REGISTRY_PROBE"
	if os.Getenv(childKey) == "1" {
		launcher := filepath.Join(os.Getenv("HOME"), "bin", "formal-gates")
		if provider, ambiguous := providerFromRegistryDetailed(launcher); provider != ProviderClaude || ambiguous {
			t.Fatalf("registry provider = %q, ambiguous = %v", provider, ambiguous)
		}
		return
	}

	// Canonical fixture paths make the logged stat names stable even when the
	// system temporary directory itself is reached through a symlink.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "workspace")
	workingDirectory := filepath.Join(root, "project")
	launcher := filepath.Join(home, "bin", "formal-gates")
	for _, directory := range []string{workingDirectory, filepath.Dir(launcher)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(launcher, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"existing", "missing"} {
		t.Run(state, func(t *testing.T) {
			if state == "missing" {
				if err := os.Remove(launcher); err != nil {
					t.Fatal(err)
				}
			}
			baseline := map[string]int{}
			for _, count := range []int{1, 128} {
				records := make([]registryProviderRecord, count)
				for i := range records {
					records[i] = registryProviderRecord{LauncherPath: launcher, Host: ProviderClaude, ProjectRoot: root, Status: "active"}
				}
				writeProviderRegistry(t, home, records)
				logPath := filepath.Join(t.TempDir(), "testlog-"+strconv.Itoa(count))
				command := exec.Command(testBinary, "-test.run=^TestProviderRegistryRepeatedPathsHaveConstantFileProbes$", "-test.testlogfile="+logPath)
				command.Dir = workingDirectory
				command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, childKey+"=1")
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("provider probe: %v\n%s", err, output)
				}
				data, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{launcher, root} {
					probes := strings.Count(string(data), "\nstat "+path+"\n")
					if count == 1 {
						if probes == 0 {
							t.Fatalf("testlog did not observe path %q", path)
						}
						baseline[path] = probes
					} else if probes != baseline[path] {
						t.Errorf("%d repeated records caused %d probes of %q; one record caused %d", count, probes, path, baseline[path])
					}
					t.Logf("%d records: %d probes of %s", count, probes, path)
				}
			}
		})
	}
}

func TestCurrentProviderResolvesExecutableOnce(t *testing.T) {
	const childKey = "FORMAL_GATES_TEST_EXECUTABLE_PROBE"
	cases := []struct {
		path, provider, agent string
	}{
		{"Source/Formal-Gates", ProviderDefault, ""},
		{".claude/skills/formal-gates/bin/formal-gates", ProviderClaude, ""},
		{".codex/skills/formal-gates/bin/formal-gates", ProviderCodex, ""},
		{".cursor/formal-gates/bin/formal-gates", ProviderCursor, ""},
		{".dsh/skills/formal-gates/bin/formal-gates", ProviderDeepSeek, ""},
		{".zcode/skills/formal-gates/bin/formal-gates", ProviderZCode, ""},
		{"project/.dsh/skills/formal-gates/bin/formal-gates", ProviderDefault, "codex"},
		{"project/.zcode/skills/formal-gates/bin/formal-gates", ProviderDefault, "codex"},
	}
	if os.Getenv(childKey) == "1" {
		home := os.Getenv("HOME")
		launcher := filepath.Join(home, "launcher")
		for _, key := range ProviderEnvironmentKeys() {
			t.Setenv(key, "")
		}
		previousExecutable := executablePath
		executablePath = func() (string, error) { return launcher, nil }
		t.Cleanup(func() { executablePath = previousExecutable })
		for _, test := range cases {
			if err := os.Remove(launcher); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(home, filepath.FromSlash(test.path)), launcher); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AI_AGENT", test.agent)
			if provider, err := currentProvider(); err != nil || provider != test.provider {
				t.Fatalf("launcher -> %s: provider = %q, error = %v; want %q", test.path, provider, err, test.provider)
			}
		}
		return
	}

	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		path := filepath.Join(home, filepath.FromSlash(test.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "testlog")
	command := exec.Command(testBinary, "-test.run=^TestCurrentProviderResolvesExecutableOnce$", "-test.testlogfile="+logPath)
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, childKey+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("executable probe: %v\n%s", err, output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	probes := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line == "stat "+filepath.Join(home, "launcher") {
			probes++
		}
	}
	if probes != len(cases) {
		t.Fatalf("%d provider detections caused %d launcher probes; want one per detection", len(cases), probes)
	}
	t.Logf("%d provider detections with symlink changes: %d launcher probes", len(cases), probes)
}

func TestCurrentProviderPreservesRegistryPathCase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AI_AGENT", "codex")
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(home, "MixedCase", "Formal-Gates")
	writeProviderRegistry(t, home, []registryProviderRecord{
		{LauncherPath: launcher, Host: ProviderClaude, ProjectRoot: root, Status: "active"},
	})
	previousExecutable := executablePath
	executablePath = func() (string, error) { return launcher, nil }
	t.Cleanup(func() { executablePath = previousExecutable })
	if provider, err := currentProvider(); err != nil || provider != ProviderClaude {
		t.Fatalf("mixed-case registry launcher: provider = %q, error = %v; want %q", provider, err, ProviderClaude)
	}
}

func TestProviderRegistryAliasesAndRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := filepath.Join(home, "workspace")
	project := filepath.Join(workspace, "project")
	other := filepath.Join(home, "other")
	for _, directory := range []string{project, other} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	previousDirectory, err := os.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = previousDirectory.Chdir()
		_ = previousDirectory.Close()
	})
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	firstBinary := filepath.Join(home, "first-binary")
	secondBinary := filepath.Join(home, "second-binary")
	for _, path := range []string{firstBinary, secondBinary} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	launcher := filepath.Join(home, "launcher")
	rootAlias := filepath.Join(home, "root-alias")
	if err := os.Symlink(firstBinary, launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, rootAlias); err != nil {
		t.Fatal(err)
	}
	records := []registryProviderRecord{
		{LauncherPath: firstBinary, Host: ProviderCodex, ProjectRoot: workspace, Status: "active"},
		{Host: ProviderClaude, Status: "active", Canonical: map[string]string{"launcher": launcher, "projectRoot": rootAlias}},
	}
	writeProviderRegistry(t, home, records)
	assertProvider := func(executable, want string, wantAmbiguous bool) {
		t.Helper()
		if provider, ambiguous := providerFromRegistryDetailed(executable); provider != want || ambiguous != wantAmbiguous {
			t.Fatalf("provider for %q = %q, ambiguous = %v; want %q, %v", executable, provider, ambiguous, want, wantAmbiguous)
		}
	}
	assertProvider(firstBinary, ProviderClaude, false)
	// Canonical-field fallback and direct aliases of the same root must still
	// compete at the same specificity rather than suppressing ambiguity.
	conflict := registryProviderRecord{LauncherPath: firstBinary, Host: ProviderCursor, ProjectRoot: project, Status: "active"}
	writeProviderRegistry(t, home, append(records, conflict))
	assertProvider(firstBinary, ProviderClaude, true)
	writeProviderRegistry(t, home, records)
	assertProvider(firstBinary, ProviderClaude, false)

	// Both successful resolutions and missing-path fallbacks expire after a
	// detection, so changing or recreating an installation is visible at once.
	if err := os.Remove(launcher); err != nil {
		t.Fatal(err)
	}
	assertProvider(firstBinary, ProviderCodex, false)
	if err := os.Symlink(secondBinary, launcher); err != nil {
		t.Fatal(err)
	}
	assertProvider(firstBinary, ProviderCodex, false)
	assertProvider(secondBinary, ProviderClaude, false)
	if err := os.Remove(rootAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, rootAlias); err != nil {
		t.Fatal(err)
	}
	assertProvider(secondBinary, "", false)
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}
	assertProvider(secondBinary, ProviderClaude, false)
}

func writeProviderRegistry(t *testing.T, home string, records []registryProviderRecord) {
	t.Helper()
	path := filepath.Join(home, ".formal-gates", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(registryProviderDocument{Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
