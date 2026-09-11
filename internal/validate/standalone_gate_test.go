package validate

import (
	"strings"
	"testing"
)

// TestComposeStandaloneGatePrompt covers detached gate prompt: it reuses
// the shared reviewer contract (contamination check applies), the gate block, and
// a working-tree-vs-head change block with no requirement or dispatch binding.
func TestComposeStandaloneGatePrompt(t *testing.T) {
	root, pkg := workflowFixture(t)
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := ComposeStandaloneGatePrompt(catalog, "quality", root, "git", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range []string{"[Shared reviewer contract]", "[Gate: quality]", "[Current change]", "[Result contract]"} {
		if !strings.Contains(prompt, block) {
			t.Fatalf("standalone prompt missing %s: %s", block, prompt)
		}
	}
	if strings.Contains(prompt, "[Current requirement]") || strings.Contains(prompt, "[Dispatch]") {
		t.Fatalf("standalone prompt must not carry requirement or dispatch blocks: %s", prompt)
	}
	// 单跑明示无需求块属设计意图，零上下文审查者不应因此报
	// RUNTIME_ERROR。
	if !strings.Contains(prompt, "no requirement block") || !strings.Contains(prompt, "must not be reported as RUNTIME_ERROR") {
		t.Fatalf("standalone prompt must state the missing requirement block is by design: %s", prompt)
	}
	if !strings.Contains(prompt, "base snapshot: "+gitHead(t, root)) || !strings.Contains(prompt, "git status + git diff") {
		t.Fatalf("standalone prompt missing the working-tree comparison: %s", prompt)
	}
	// 复用共享契约块使污染检查自动生效（契约文本本身由
	// TestContaminationCheckCoversReviewersButNotExecutors 对真实包校验）。
	if !strings.Contains(prompt, "[Shared reviewer contract]") {
		t.Fatalf("standalone prompt must reuse the shared reviewer contract: %s", prompt)
	}
	if _, err := ComposeStandaloneGatePrompt(catalog, "missing", root, "git", ""); err == nil {
		t.Fatal("unknown standalone gate was accepted")
	}
	if _, err := ComposeStandaloneGatePrompt(catalog, "quality", root, "mercurial", ""); err == nil {
		t.Fatal("unsupported standalone VCS was accepted")
	}
	scoped, err := ComposeStandaloneGatePrompt(catalog, "quality", root, "git", "internal/cli")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "Review scope (user-specified): internal/cli") {
		t.Fatalf("standalone scope was not injected: %s", scoped)
	}
}

// TestValidateStandaloneGateResult covers result-contract validation:
// the same semantic rules as a gate result, with no run-state writes.
func TestValidateStandaloneGateResult(t *testing.T) {
	for _, item := range []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "pass", raw: `{"status":"PASS","findings":[]}`},
		{name: "p2 pass", raw: `{"status":"PASS","findings":[{"severity":"P2","message":"suggestion"}]}`},
		{name: "p3 pass", raw: `{"status":"PASS","findings":[{"severity":"P3","message":"suggestion"}]}`},
		{name: "fail", raw: `{"status":"FAIL","findings":[{"severity":"P1","message":"blocker"}]}`},
		{name: "runtime", raw: `{"status":"RUNTIME_ERROR","message":"VCS unavailable"}`},
		{name: "pass with p1", raw: `{"status":"PASS","findings":[{"severity":"P1","message":"blocker"}]}`, wantErr: "P2"},
		{name: "fail without p0p1", raw: `{"status":"FAIL","findings":[{"severity":"P2","message":"minor"}]}`, wantErr: "P0 or P1"},
		{name: "runtime without message", raw: `{"status":"RUNTIME_ERROR"}`, wantErr: "message"},
		{name: "bad json", raw: `{`, wantErr: "not valid JSON"},
	} {
		result, err := ValidateStandaloneGateResult([]byte(item.raw))
		if item.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", item.name, err)
			}
			if result.Status == "" {
				t.Fatalf("%s: empty status", item.name)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), item.wantErr) {
			t.Fatalf("%s: want error containing %q, got %v", item.name, item.wantErr, err)
		}
	}
}
