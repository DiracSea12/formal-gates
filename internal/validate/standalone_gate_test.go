package validate

import (
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/prompts"
)

// TestComposeStandaloneGatePrompt covers detached gate prompt: it reuses
// the shared reviewer contract (contamination check applies), the gate block, and
// a working-tree-vs-head change block with no requirement or dispatch binding.
func TestComposeStandaloneGatePrompt(t *testing.T) {
	root, pkg := standaloneGateFixture(t)
	catalog, err := prompts.LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	context, err := CaptureStandaloneGateContext(root, "git", "")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := ComposeStandaloneGatePrompt(catalog, "quality", context)
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
	if !strings.Contains(prompt, "base snapshot: "+standaloneGateHead(t, root)) || !strings.Contains(prompt, "git status + git diff") {
		t.Fatalf("standalone prompt missing the working-tree comparison: %s", prompt)
	}
	// 复用共享契约块使污染检查自动生效（契约文本本身由
	// TestContaminationCheckCoversReviewersButNotExecutors 对真实包校验）。
	if !strings.Contains(prompt, "[Shared reviewer contract]") {
		t.Fatalf("standalone prompt must reuse the shared reviewer contract: %s", prompt)
	}
	if _, err := ComposeStandaloneGatePrompt(catalog, "missing", context); err == nil {
		t.Fatal("unknown standalone gate was accepted")
	}
	if _, err := CaptureStandaloneGateContext(root, "mercurial", ""); err == nil {
		t.Fatal("unsupported standalone VCS was accepted")
	}
	context.Scope = "internal/cli"
	scoped, err := ComposeStandaloneGatePrompt(catalog, "quality", context)
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
	for _, item := range []struct{ name, raw, status, wantErr string }{
		{"empty completed review", `{"findings":[]}`, "PASS", ""},
		{"advisory findings", `{"findings":[{"severity":"P2","message":"suggestion"},{"severity":"P3","message":"minor"}]}`, "PASS", ""},
		{"blocking finding", `{"findings":[{"severity":"P1","message":"blocker"}]}`, "FAIL", ""},
		{"highest severity", `{"findings":[{"severity":"P0","message":"blocking error"}]}`, "FAIL", ""},
		{"actual inability", `{"interruptionReason":"VCS unavailable"}`, "RUNTIME_ERROR", ""},
		{"forbidden status", `{"status":"PASS","findings":[]}`, "", "unknown field"},
		{"inability with findings", `{"interruptionReason":"VCS unavailable","findings":[{"severity":"P2","message":"suggestion"}]}`, "", "cannot include"},
		{"blank inability", `{"interruptionReason":" "}`, "", "actual reason"},
		{"missing judgment", `{}`, "", "requires findings"},
		{"bad json", `{`, "", "not valid JSON"},
		{"trailing object", `{"findings":[]} {}`, "", "one JSON object"},
	} {
		result, err := ValidateStandaloneGateResult([]byte(item.raw), StandaloneGateContext{})
		if item.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), item.wantErr) {
				t.Fatalf("%s: want %q, got %v", item.name, item.wantErr, err)
			}
			continue
		}
		if err != nil || result.Status != item.status {
			t.Fatalf("%s: status %q, want %q: %v", item.name, result.Status, item.status, err)
		}
	}
}

// Typed worker material carries the shared contract for readonly judgments;
// implementation and whitebox authors receive their scoped author contract.
func TestSharedReviewerContractInjectedIntoReviewersOnly(t *testing.T) {
	root := promptPackage(t, map[string]string{"quality": "checks"})
	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	roles := []struct {
		handler  string
		action   review.Action
		readOnly bool
	}{
		{"engine.review.worker", review.ProductReview, true}, {"engine.readiness.plan", review.StartReadiness, true}, {"engine.qa.review.rationale", "", true}, {"engine.development.batch", "", false}, {"engine.qa.execution", "", true}, {"engine.repair.verify", "", true}, {"engine.qa.design", "", false},
	}
	for _, role := range roles {
		input := protocol.WorkerInput{RunID: "contract", Provider: "codex", VCS: "git", Handler: role.handler, Pending: protocol.PendingAction{ActionID: "contract:" + role.handler, AttemptID: "attempt"}}
		input.RequirementDocument.Content = "Confirmed requirement"
		if role.action != "" {
			input.Review = &protocol.WorkerReviewInput{Action: role.action, RequirementRevision: "revision"}
		} else {
			input.Validation = &protocol.ValidationInput{Handler: role.handler}
			input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
			if role.handler == "engine.repair.verify" {
				input.Validation.Repair = &protocol.RepairInput{}
			}
		}
		material, err := worker.Render(input, catalog)
		if err != nil {
			t.Fatalf("%s: %v", role.handler, err)
		}
		injected := strings.HasPrefix(material.Prompt, "[Shared reviewer contract]\nshared contract\n")
		if injected != role.readOnly {
			t.Fatalf("%s shared-contract scope=%t, want %t", role.handler, injected, role.readOnly)
		}
	}
}

func standaloneGateFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	for _, args := range [][]string{{"init"}, {"config", "user.email", "fixture@example.invalid"}, {"config", "user.name", "Gate fixture"}, {"add", "."}, {"commit", "--no-verify", "--no-gpg-sign", "-m", "Delivery"}} {
		runGit(t, root, args...)
	}
	return root, promptPackage(t, map[string]string{"quality": "quality checks"})
}

func standaloneGateHead(t *testing.T, root string) string {
	t.Helper()
	return runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	value, err := (execNativeCommandRunner{}).Run(root, "git", args...)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
