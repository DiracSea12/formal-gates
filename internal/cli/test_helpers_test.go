package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostEnvKeys are the environment variables the lifecycle host provider reads
// to detect the driving host. CLI tests clear them so the CLI under test
// observes the lenient default provider instead of the host shell that happens
// to run `go test`.
var hostEnvKeys = []string{"AI_AGENT", "CLAUDE_CODE_ENTRYPOINT", "CODEX_HOME", "CODEX_CLI_PATH", "CURSOR_TRACE_ID", "CURSOR_RUNTIME", "DSH_HOME", "DSH_PROJECT_DIR"}

func clearHostEnv(t *testing.T) {
	t.Helper()
	for _, key := range hostEnvKeys {
		t.Setenv(key, "")
	}
}

func isHostEnvKey(key string) bool {
	for _, candidate := range hostEnvKeys {
		if key == candidate {
			return true
		}
	}
	return false
}

// hostFilteredEnv strips host lifecycle keys from the inherited environment so
// a subprocess under test observes the lenient default provider.
func hostFilteredEnv(environment []string) []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if key := strings.SplitN(kv, "=", 2)[0]; !isHostEnvKey(key) {
			filtered = append(filtered, kv)
		}
	}
	return append(filtered, environment...)
}

func mustWriteCLI(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
