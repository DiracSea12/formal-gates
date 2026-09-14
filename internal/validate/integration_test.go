//go:build integration

package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPortableCanaryPassesAgainstRepoRoot runs the full portable canary against
// the real repository root. It exercises package validation, installs, hook
// decisions, and a quick end-to-end workflow against the working checkout, so it
// is an integration check — it lives here, behind the integration build tag,
// outside the unit suite (`go test ./...`). CI runs the same canary as a separate
// CLI step (`canary portable --root .`) after building the native CLI.
func TestPortableCanaryPassesAgainstRepoRoot(t *testing.T) {
	root := repoRootForCanaryTest(t)
	buildRepoRootBinary(t, root)
	report, result := PortableCanary(PortableCanaryOptions{Root: root})
	if !result.OK() {
		t.Fatalf("expected portable canary to pass, report=%#v failures=%#v", report, result.Failures)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected canary checks")
	}
	for _, check := range report.Checks {
		if check.Status != "PASS" {
			t.Fatalf("expected all checks to pass, got %#v", check)
		}
	}
}

// buildRepoRootBinary builds the native CLI into the repository root's bin/
// directory so package validation and source-install checks always exercise
// current source rather than a stale ignored binary. bin/ is gitignored, so the
// build does not dirty the working tree.
func buildRepoRootBinary(t *testing.T, root string) {
	t.Helper()
	binPath := filepath.Join(root, "bin", nativeBinaryName())
	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/formal-gates")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v: %s", binPath, err, output)
	}
}

// TestWordingCoversRegularPlanAndReviewBoundaries verifies the supported stage 4
// no-split contract in its current documentation owners.
func TestWordingCoversRegularPlanAndReviewBoundaries(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"SKILL.md", []string{"planDigest", "no-split", "逐条核实", "未接入 split"}},
		{"references/formal-flow.md", []string{"no-split", "ADJUDICATE", "逐 finding", "PREPARATION_RESULT"}},
		{"references/sliced-runs.md", []string{"Codex/Git/no-split", "按后续阶段接入", "development-plan.md", "engine-regular.md"}},
		{"references/development-plan.md", []string{"REQUEST(topology, planDigest, reason)", "Subtask", "计划契约变化", "重新技术审"}},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join("..", "..", check.path))
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		content := string(data)
		for _, want := range check.want {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q", check.path, want)
			}
		}
	}
}
