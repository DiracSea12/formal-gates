package validate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"formal-gates/internal/lifecycle"
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

// stubLifecycle replaces the global workflowLifecycle verifier with an isolated
// stub for the duration of the test and restores the prior verifier on cleanup.
// It is the single converged stub for the workflow's lifecycle seam; tests that
// stub the lifecycle swap global state, so they must stay serial (no t.Parallel)
// — the suite currently holds that, keep it.
func stubLifecycle(t *testing.T, verification lifecycle.Verification, transcript, interruptionReason string) *workflowLifecycleStub {
	t.Helper()
	stub := &workflowLifecycleStub{verification: verification, transcript: transcript, interruptionReason: interruptionReason}
	prior := workflowLifecycle
	workflowLifecycle = stub
	t.Cleanup(func() { workflowLifecycle = prior })
	return stub
}

// stubParallelCooldown pins the global parallel-reminder cooldown for the
// duration of the test and restores the production value on cleanup.
func stubParallelCooldown(t *testing.T, duration time.Duration) {
	t.Helper()
	prior := parallelCooldown
	parallelCooldown = duration
	t.Cleanup(func() { parallelCooldown = prior })
}
