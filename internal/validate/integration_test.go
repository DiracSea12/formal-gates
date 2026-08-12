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
// directory so package validation and source-install checks pass. bin/ is
// gitignored, so the build does not dirty the working tree.
func buildRepoRootBinary(t *testing.T, root string) {
	t.Helper()
	binPath := filepath.Join(root, "bin", nativeBinaryName())
	if isFile(binPath) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(binPath), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/formal-gates")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v: %s", binPath, err, output)
	}
}

// TestWordingCoversSplitSuggestionAndOverallReviewInheritance checks the
// user-visible wording required by the acceptance evidence: the split suggestion
// (including the 改拆后果说明) is mandated in SKILL.md and the product-review
// prompt, and the overall-level review inheritance is documented in SKILL.md and
// formal-flow.md. It reads the repository documentation verbatim, so any wording
// change fails it — it lives here, behind the integration build tag, outside the
// unit suite (`go test ./...`).
func TestWordingCoversSplitSuggestionAndOverallReviewInheritance(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"SKILL.md", []string{"改拆后果说明", "整体级产品审/技术审足够", "切片继承整体审查结果"}},
		{"references/formal-flow.md", []string{"改拆后果说明", "整体级产品审/技术审足够", "切片继承整体审查结果"}},
		{"references/sliced-runs.md", []string{"改拆后果说明"}},
		{"prompts/actions/product-review.md", []string{"改拆后果说明"}},
		{"prompts/actions/qa-design.md", []string{"开发前", "开发后"}},
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
