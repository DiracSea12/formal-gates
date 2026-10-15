package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"formal-gates/internal/coordination"
)

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
		canonical[key] = record[field].(string)
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

// ConfigureNativePaths isolates path-freezing scenarios from installed model
// hosts. Native host arguments fail against the Go executable; transport tests
// provide their own protocol peers instead.
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
		switch provider {
		case "claude", "claude-code":
			values["claudeExecutable"], values["claudeHome"] = executable, t.TempDir()
		case "codex":
			values["codexExecutable"], values["codexHome"] = executable, t.TempDir()
		case "zcode":
			home := t.TempDir()
			script := filepath.Join(home, "unavailable.cjs")
			if err := os.WriteFile(script, []byte(`throw new Error("path-only fixture cannot start a model host");`), 0600); err != nil {
				t.Fatal(err)
			}
			values["zcodeExecutable"], values["zcodeScript"], values["zcodeHome"] = executable, script, home
		default:
			t.Fatalf("unsupported path fixture provider %q", provider)
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
