//go:build phase0whitebox

package validate

import (
	"strings"
	"testing"
)

// 本文件是白盒 QA 设计者（结构测试交付物）独立编写的结构测试代码，覆盖
// 「修复现有实现缺陷」run 的三项已确认需求：缺陷 1（白盒测试代码进快照）、
// 缺陷 4（未跟踪交付文件检测）、缺陷 3（删除 legacy "qa" 兼容）。用例独立设计，
// 不引用、不复用 development-worker 交付的既有测试与 fixture。

// whiteboxScriptedNativeRunner 是自包含的原生命令 runner，用于驱动 VerifyReady
// 的 git 脏检查。它独立于 development-worker 的 scriptedNativeRunner：每次 Run 返回
// 下一个脚本化输出，模仿 VerifyReady 驱动的 git 子进程。
type whiteboxScriptedNativeRunner struct {
	outputs []string
	calls   [][]string
}

func (r *whiteboxScriptedNativeRunner) Run(_ string, command string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	idx := len(r.calls) - 1
	if idx >= len(r.outputs) {
		return "", nil
	}
	return r.outputs[idx], nil
}

func whiteboxGitResolver(t *testing.T, outputs ...string) nativeVCSResolver {
	t.Helper()
	resolver, err := resolverForVCS("git", &whiteboxScriptedNativeRunner{outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

// 缺陷 4：快照脏检查改为 porcelain normal（检测未跟踪且未忽略文件），新增交付文件
// 漏 git add 必须在快照前明确报错。

func TestWhiteboxVerifyReadyRejectsUntrackedDeliveryFile(t *testing.T) {
	resolver := whiteboxGitResolver(t, "/repo", "?? delivery-new.txt")
	err := resolver.VerifyReady("/repo")
	if err == nil || !strings.Contains(err.Error(), "unsubmitted git changes") || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked non-ignored delivery file was accepted by the snapshot dirty check: %v", err)
	}
}

func TestWhiteboxVerifyReadyAcceptsCleanWorktree(t *testing.T) {
	resolver := whiteboxGitResolver(t, "/repo", "")
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("clean worktree was rejected: %v", err)
	}
}

func TestWhiteboxVerifyReadyIgnoresUntrackedRuntimeDirectories(t *testing.T) {
	runtimeOnly := "?? .gates/tmp/run-1/state.json\n?? .gates/qa-isolation/run-1/\n?? .gates/slices/run-1/\n?? .gates/results/old-run.json"
	resolver := whiteboxGitResolver(t, "/repo", runtimeOnly)
	if err := resolver.VerifyReady("/repo"); err != nil {
		t.Fatalf("untracked formal-gates runtime directories blocked a snapshot: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsMixedUntracked(t *testing.T) {
	mixed := "?? .gates/tmp/run-1/state.json\n?? delivery-new.txt"
	resolver := whiteboxGitResolver(t, "/repo", mixed)
	if err := resolver.VerifyReady("/repo"); err == nil || !strings.Contains(err.Error(), "untracked") {
		t.Fatalf("untracked delivery file was accepted alongside runtime directories: %v", err)
	}
}

func TestWhiteboxVerifyReadyRejectsTrackedRuntimeModification(t *testing.T) {
	// 豁免只针对「未跟踪」的运行期产物；运行期目录下被跟踪文件的修改（" M " 行）仍须拦截。
	resolver := whiteboxGitResolver(t, "/repo", " M .gates/tmp/run-1/state.json")
	if err := resolver.VerifyReady("/repo"); err == nil {
		t.Fatalf("tracked modification under a runtime directory was accepted: %v", err)
	}
}

func TestWhiteboxIsRuntimeGitStatusPath(t *testing.T) {
	for _, path := range []string{".gates/tmp", ".gates/tmp/run-1/state.json", ".gates/qa-isolation/run-1/", ".gates/slices/run-1/", ".gates/results/old-run.json"} {
		if !isRuntimeGitStatusPath(path) {
			t.Fatalf("runtime path %q not recognized as a reserved runtime directory", path)
		}
	}
	for _, path := range []string{"delivery-new.txt", ".gates/other/run.json", "internal/code.go", ".gates/tmp-notes.md"} {
		if isRuntimeGitStatusPath(path) {
			t.Fatalf("non-runtime path %q misclassified as a reserved runtime directory", path)
		}
	}
}

// 缺陷 1：白盒测试代码推进路径（方案 A）。whiteboxTestCodeAdvancement 仅在开发完成、
// 白盒已选中、白盒 per-mode 设计已记录且派发绑当前快照时为真；推进后一次性关闭。

func whiteboxAdvancementState(developmentStatus string) RunState {
	return RunState{
		Actions: map[string]ActionResult{
			"development-worker": {Status: developmentStatus},
		},
		SelectedGates: []string{whiteboxQAID},
		QADesignByMode: map[string]ActionResult{
			whiteboxQAID: {Status: "PASS", DispatchID: "wb-design-1"},
		},
		Dispatches: map[string]PreparedDispatch{
			"wb-design-1": {Target: "qa-design", Mode: whiteboxQAID, SourceSnapshot: "snap-S2"},
		},
		CurrentSnapshot: "snap-S2",
	}
}

func TestWhiteboxTestCodeAdvancementEnabled(t *testing.T) {
	if !whiteboxTestCodeAdvancement(whiteboxAdvancementState(developmentComplete)) {
		t.Fatal("whitebox test-code advancement did not open on the completed-development whitebox-design state")
	}
}

func TestWhiteboxTestCodeAdvancementRequiresDevelopmentComplete(t *testing.T) {
	for _, status := range []string{developmentPending, developmentPrepared, developmentRepairPrepared, developmentVerified} {
		if whiteboxTestCodeAdvancement(whiteboxAdvancementState(status)) {
			t.Fatalf("advancement opened before development completed (status %q)", status)
		}
	}
}

func TestWhiteboxTestCodeAdvancementRequiresWhiteboxSelected(t *testing.T) {
	state := whiteboxAdvancementState(developmentComplete)
	state.SelectedGates = []string{blackboxQAID}
	if whiteboxTestCodeAdvancement(state) {
		t.Fatal("advancement opened when whitebox QA was not selected")
	}
}

func TestWhiteboxTestCodeAdvancementRequiresWhiteboxDesignPass(t *testing.T) {
	state := whiteboxAdvancementState(developmentComplete)
	state.QADesignByMode[whiteboxQAID] = ActionResult{Status: "PENDING", DispatchID: "wb-design-1"}
	if whiteboxTestCodeAdvancement(state) {
		t.Fatal("advancement opened while the whitebox design was still PENDING")
	}
}

func TestWhiteboxTestCodeAdvancementOneShot(t *testing.T) {
	state := whiteboxAdvancementState(developmentComplete)
	// 快照推进后 CurrentSnapshot 前进到新提交，而白盒设计派发的 SourceSnapshot 仍钉在旧
	// 快照；第二次提交不得再经此路径推进（修复快照走正常修复门）。
	state.CurrentSnapshot = "snap-S3"
	if whiteboxTestCodeAdvancement(state) {
		t.Fatal("advancement remained open after the snapshot advanced (one-shot path re-opened)")
	}
}

func TestWhiteboxTestCodeAdvancementRequiresPerModeDesign(t *testing.T) {
	state := whiteboxAdvancementState(developmentComplete)
	// full 路线开发前把黑盒设计记入合并 "" 键；按合并键回退读取会误判白盒设计已就绪、
	// 让任意提交绕过修复门。推进路径只能由白盒自己的 per-mode 设计打开。
	state.QADesignByMode = map[string]ActionResult{
		"": {Status: "PASS", DispatchID: "blackbox-design-1"},
	}
	state.Dispatches = map[string]PreparedDispatch{
		"blackbox-design-1": {Target: "qa-design", Mode: blackboxQAID, SourceSnapshot: "snap-S2"},
	}
	if whiteboxTestCodeAdvancement(state) {
		t.Fatal("advancement opened from the merged blackbox design key instead of the whitebox per-mode design")
	}
}

func TestWhiteboxSnapshotTransitionBypassesDevelopmentPrepare(t *testing.T) {
	state := whiteboxAdvancementState(developmentComplete)
	if err := requireSnapshotTransition(state); err != nil {
		t.Fatalf("whitebox test-code snapshot transition was blocked: %v", err)
	}
	// 对比：开发未完成且无白盒设计时，快照过渡仍要求开发工作者已准备。
	state.Actions["development-worker"] = ActionResult{Status: developmentPending}
	state.SelectedGates = []string{blackboxQAID}
	state.QADesignByMode = map[string]ActionResult{}
	state.Dispatches = map[string]PreparedDispatch{}
	if err := requireSnapshotTransition(state); err == nil || !strings.Contains(err.Error(), "development worker must be prepared") {
		t.Fatalf("non-whitebox snapshot transition did not require a prepared development worker: %v", err)
	}
}

// 缺陷 3：删除 legacy "qa" 合并态识别。新 run 正常路线只返回 blackbox / whitebox；
// "qa" 不再被当作内置 QA 模式，也不再让仅选 "qa" 的 run 被当作 QA 选中。

func TestWhiteboxIsQAModeRejectsLegacyQA(t *testing.T) {
	if isQAMode("qa") {
		t.Fatal(`legacy "qa" id is still recognized as a built-in QA mode`)
	}
	for _, id := range []string{blackboxQAID, whiteboxQAID, mergeQAID} {
		if !isQAMode(id) {
			t.Fatalf("real QA mode %q was not recognized", id)
		}
	}
}

func TestWhiteboxLegacyQANotQAselected(t *testing.T) {
	state := RunState{SelectedGates: []string{"qa"}}
	if isSelectedQA(state) {
		t.Fatal(`a run selecting only the legacy "qa" id is still treated as QA-selected`)
	}
	state = RunState{SelectedGates: []string{whiteboxQAID}}
	if !isSelectedQA(state) {
		t.Fatal("a run selecting whitebox QA was not treated as QA-selected")
	}
}
