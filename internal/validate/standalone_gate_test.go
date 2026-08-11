package validate

import (
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
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

// TestSharedReviewerContractInjectedIntoReviewersOnly verifies the
// contamination-check composition scope: ComposeActionPrompt injects the shared
// reviewer contract (which carries the first-step 任务完整性检查 block) into the
// three zero-context reviewer actions — product-review、qa-review、start-readiness
// — while the non-reviewer actions（development-worker、carry、qa-execution、
// requirements-clarification、qa-design）do not get the injection. qa-design is
// excluded because it is now a design-writer (whitebox writes test code, blackbox
// writes case documents), so a "don't edit repository files"
// contract would contradict its write role. This fixture-based check stays in
// the unit suite; the real-catalog single-home invariant (任务完整性检查 lives once
// in prompts/reviewer-base.md and no action prompt inlines it) is verified under
// the integration tag.
func TestSharedReviewerContractInjectedIntoReviewersOnly(t *testing.T) {
	// 组装层面：审查动作注入共享契约、非审查动作不注入。
	root := promptPackage(t, map[string]string{"quality": "checks"})
	catalog, err := LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	route := PromptRoute{RequirementSource: "requirements.md", RequirementRevision: "rev", CatalogRevision: catalog.CatalogRevision, Worktree: "/repo", VCS: "git", BaseSnapshot: "a", CurrentSnapshot: "b", PreRepairSnapshot: "old"}
	reviewerActions := []string{"product-review", "qa-review", "start-readiness"}
	for _, id := range reviewerActions {
		prompt, err := ComposeActionPrompt(catalog, id, route, "input")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(prompt, "[Shared reviewer contract]\nshared contract\n") {
			t.Fatalf("%s prompt did not inject the shared reviewer contract:\n%s", id, prompt)
		}
	}
	for _, id := range []string{"requirements-clarification", "development-worker", "qa-execution", "carry", "qa-design"} {
		prompt, err := ComposeActionPrompt(catalog, id, route, "input")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(prompt, "[Shared reviewer contract]") {
			t.Fatalf("%s (non-reviewer) prompt wrongly injected the shared reviewer contract:\n%s", id, prompt)
		}
	}
}

// TestResumeGuardForcesResumeOrUserAuthorizationForGates covers the
// gate prepare path: an unchanged claimed dispatch is forced to resume, while a
// user-authorized fresh dispatch is allowed and its source recorded.
func TestResumeGuardForcesResumeOrUserAuthorizationForGates(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "resume-gate", "custom", []string{"quality"})
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "interrupted-quality")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "resume the original agent") {
		t.Fatalf("unchanged claimed dispatch was not forced to resume: %v", err)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", true, "user reopened the gate"); err != nil {
		t.Fatalf("user-authorized fresh dispatch was rejected: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewOverrides["quality"] == "" {
		t.Fatalf("gate reopen was not recorded in ReviewOverrides: %#v", state.ReviewOverrides)
	}
	// prepare SHALL NOT 作废任何派发——用户授权的新派发生成后，旧 CLAIMED 派发仍
	// 在途；认领新派发时若读得前子代理的 stop 事件（手动终止例外）才把旧派发标 STALE。
	if state.Dispatches[dispatchID].Status != "CLAIMED" {
		t.Fatalf("prepare must not stale the claimed dispatch: %#v", state.Dispatches[dispatchID])
	}
	fresh := openDispatchID(state, "gate", "quality")
	if fresh == "" || fresh == dispatchID {
		t.Fatalf("fresh gate dispatch was not prepared: %#v", state.Dispatches)
	}
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "user abort")
	if _, err := ClaimDispatch(root, pkg, state.RunID, fresh, "fresh-gate-reviewer"); err != nil {
		t.Fatalf("claim of the fresh gate dispatch after manual termination was rejected: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.Dispatches[dispatchID].Status != "STALE" || state.Dispatches[fresh].Status != "CLAIMED" {
		t.Fatalf("manual-terminated claimed dispatch was not staled at claim: %#v", state.Dispatches)
	}
}

// TestResumeGuardAllowsNewDispatchWhenTaskContentMoved covers task-changed
// exception: editing a gate prompt makes the interrupted dispatch's task content
// differ, so a fresh dispatch is allowed without a user authorization.
func TestResumeGuardAllowsNewDispatchWhenTaskContentMoved(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "resume-task-changed", "custom", []string{"quality"})
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "interrupted-quality")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "resume the original agent") {
		t.Fatalf("unchanged claimed dispatch was not forced to resume: %v", err)
	}
	writeTestFile(t, filepath.Join(pkg, "gates", "quality.md"), "new quality checks\n")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatalf("task-moved dispatch was not allowed a fresh dispatch: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	// prepare 不再作废——任务内容移动放行的新派发生成后，旧派发仍 CLAIMED。
	if state.Dispatches[dispatchID].Status != "CLAIMED" {
		t.Fatalf("prepare must not stale the claimed dispatch: %#v", state.Dispatches[dispatchID])
	}
	fresh := openDispatchID(state, "gate", "quality")
	if fresh == "" || fresh == dispatchID {
		t.Fatalf("fresh gate dispatch was not prepared: %#v", state.Dispatches)
	}
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "user abort")
	if _, err := ClaimDispatch(root, pkg, state.RunID, fresh, "fresh-gate-reviewer"); err != nil {
		t.Fatalf("claim of the task-moved fresh dispatch was rejected: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.Dispatches[dispatchID].Status != "STALE" || state.Dispatches[fresh].Status != "CLAIMED" {
		t.Fatalf("manual-terminated claimed dispatch was not staled at claim: %#v", state.Dispatches)
	}
}
