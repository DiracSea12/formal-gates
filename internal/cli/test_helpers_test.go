package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
)

func clearHostEnv(t *testing.T) {
	t.Helper()
	for _, key := range lifecycle.ProviderEnvironmentKeys() {
		t.Setenv(key, "")
	}
}

func isHostEnvKey(key string) bool {
	for _, candidate := range lifecycle.ProviderEnvironmentKeys() {
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
