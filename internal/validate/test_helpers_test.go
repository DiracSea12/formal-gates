package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	// Workflow unit tests exercise the workflow logic against the native
	// lifecycle in a clean host environment: the test process may itself run
	// inside a real host shell (e.g. `go test` from Claude Code), and host
	// environment detection must not turn the lenient default provider into a
	// required provider that rejects lifecycle-less unit flow.
	for _, key := range hostProviderEnvKeys {
		os.Setenv(key, "")
	}
	os.Exit(m.Run())
}

func repoRootForCanaryTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
