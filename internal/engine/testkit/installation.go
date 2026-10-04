package testkit

import (
	"encoding/json"
	"os"
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
		"status": "active", "packageDigest": packageDigest, "installedDigest": packageDigest,
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
