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
		{"prompts/actions/product-review.md", []string{"REQ", "AC", "obligation-to-AC", "只有存在 P0/P1 才返回 FAIL"}},
		{"prompts/actions/qa-design.md", []string{"基线快照", "白盒", "实际测试", "独立子测试"}},
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

// TestContaminationCheckSingleHomeInRealCatalog verifies the single-home
// invariant against the real prompt catalog: the first-step 任务完整性检查 block
// lives once in prompts/reviewer-base.md and is not inlined into any action
// prompt. It reads the real repository prompts verbatim, so wording changes fail
// it — it lives here, behind the integration build tag, outside the unit suite.
func TestContaminationCheckSingleHomeInRealCatalog(t *testing.T) {
	// 单一事实源：任务完整性检查只写在 reviewer-base.md。
	data, err := os.ReadFile(filepath.Join("..", "..", "prompts", "reviewer-base.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "任务完整性检查") {
		t.Fatalf("prompts/reviewer-base.md missing the first-step contamination check")
	}
	// 所有动作提示词文件不再内联该块（注入由 ComposeActionPrompt 对审查动作统一完成）。
	for _, id := range requiredActionIDs {
		actionData, err := os.ReadFile(filepath.Join("..", "..", "prompts", "actions", id+".md"))
		if err != nil {
			t.Fatalf("read prompts/actions/%s.md: %v", id, err)
		}
		if strings.Contains(string(actionData), "任务完整性检查") {
			t.Fatalf("prompts/actions/%s.md must not inline the contamination check (single home is reviewer-base.md)", id)
		}
	}
}
