package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/coordination"
	"formal-gates/internal/host"
)

// ShortRoot keeps native worktree paths independent of test names.
func ShortRoot(t testing.TB) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "engine-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return root
}

// Installation supplies the registry identity and accepted bootstrap material
// for an isolated project. Its files live outside the project so native VCS
// fixtures see only the files their business scenarios create.
func Installation(t testing.TB, root, recordID, packageDigest, provider string) (registryPath, target string, generation uint64, lease, token string) {
	t.Helper()
	dir := t.TempDir()
	registryPath, target = filepath.Join(dir, "registry.json"), filepath.Join(dir, "target")
	generation, lease, token = 1, "lease-1", "token-1"
	record := map[string]any{
		"id": recordID, "target": target, "host": provider, "scope": "project", "projectRoot": root,
		"launcherPath": filepath.Join(dir, "launcher"), "stateRoot": filepath.Join(root, ".gates"),
		"resourceRoot": filepath.Join(root, ".formal-gates-resources"), "runtimeSibling": filepath.Join(dir, "runtime"),
		"status": "active", "packageDigest": packageDigest, "installedDigest": packageDigest, "vcsIdentity": "fixture-native",
		"generation": generation, "lease": lease, "token": token,
	}
	canonical := map[string]string{}
	for key, field := range map[string]string{"target": "target", "launcher": "launcherPath", "projectRoot": "projectRoot", "stateRoot": "stateRoot", "resourceRoot": "resourceRoot", "runtimeSibling": "runtimeSibling"} {
		canonical[key] = canonicalInstallPath(record[field].(string))
	}
	record["canonicalPaths"] = canonical
	for path, value := range map[string]any{
		registryPath:                     map[string]any{"schemaVersion": 1, "epoch": 1, "records": []any{record}},
		registryPath + ".bootstrap.json": map[string]any{"accepted": true, "stateCreated": false, "packageDigest": packageDigest, "records": []any{record}},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release, err := coordination.AcquirePath(registryPath+".lock", "registry")
	if err != nil {
		t.Fatal(err)
	}
	release()
	return
}

// canonicalInstallPath resolves aliases in the nearest existing ancestor: the
// fixture's target paths do not exist yet, while its registry record must carry
// the canonical roots an installer writes for the same layout.
func canonicalInstallPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	for current := abs; ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			rel, err := filepath.Rel(current, abs)
			if err != nil || rel == "." {
				return filepath.Clean(resolved)
			}
			return filepath.Join(resolved, rel)
		}
		if filepath.Dir(current) == current {
			return abs
		}
	}
}

// ConfigureNativePaths isolates path-freezing scenarios from installed model
// hosts: each platform binds the Go executable into its own configuration
// fields, so a path-only freeze resolves without an installed host. Native host
// arguments fail against the Go executable; transport tests provide their own
// protocol peers instead.
func ConfigureNativePaths(t testing.TB, root string, providers ...string) {
	t.Helper()
	executable, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	values := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatal(err)
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, provider := range providers {
		adapter, err := host.LookupAdapter(provider)
		if err != nil {
			t.Fatalf("native path fixture %q: %v", provider, err)
		}
		fixture, ok := adapter.(host.NativeFixtureProvider)
		if !ok {
			t.Fatalf("native path fixture %q is not a registered platform", provider)
		}
		native, err := fixture.FixtureNativePaths(host.NativeFixture{Executable: executable, ScriptDir: t.TempDir(), Home: t.TempDir()})
		if err != nil {
			t.Fatalf("native path fixture %q: %v", provider, err)
		}
		for field, value := range native {
			values[field] = value
		}
	}
	data, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
