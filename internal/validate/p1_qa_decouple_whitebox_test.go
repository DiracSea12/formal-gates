package validate

// 白盒结构测试（RQ-013 白盒设计者交付）：本文件由白盒 QA 设计者在开发后独立设计并编写，
// 覆盖 P1 QA 彻底解耦与 Carry 继承修复（RQ-001~013）的关键确定性规则。每条白盒用例的
// Test 绑定 = 文件定位的测试引用 "<文件路径>::<函数名>"（如
// "whitebox_delivered_test.go::TestWhiteboxDirectRules"），CLI 记录时只校验引用非空、
// 且同一引用不被两条白盒用例共用；存在性与对应性由 qa-review 与 qa-execution 验证。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWhiteboxBindingRejectsMissingTest 覆盖 RQ-013 的 caseId↔测试绑定：白盒用例必须写明
// 实现该用例的测试引用（--test <文件>::<函数>），缺 Test 字段的记录被 CLI 拒绝——"测 A 的
// 测试给 B 用例标 PASS"必须可被发现，不能只做任意文本非空校验。
func TestWhiteboxBindingRejectsMissingTest(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-binding-missing"), "custom", []string{whiteboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	_, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure", Procedure: "run the delivered structure test", Oracle: "the test passes"}}, "")
	if err == nil || !strings.Contains(err.Error(), "requires a --test") {
		t.Fatalf("whitebox case without --test was accepted: %v", err)
	}
}

// TestWhiteboxBindingRejectsSharedReference 覆盖 RQ-013 的 1:1 校验：同一个测试引用
// （<文件>::<函数>）不能被两条白盒用例共用——一个测试实现一个用例；本次记录内撞引用即
// 拒绝记录（与既有用例撞引用同理）。
func TestWhiteboxBindingRejectsSharedReference(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-binding-shared"), "custom", []string{whiteboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	_, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{
		{Mode: "whitebox", Description: "structure", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"},
		{Mode: "whitebox", Description: "duplicate direct coverage", Procedure: "run the delivered duplicate test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "one test implements one case") {
		t.Fatalf("whitebox cases sharing a test reference were accepted: %v", err)
	}
}

// TestWhiteboxBindingDefersExistence 覆盖 RQ-013 的存在性由 review/execution 验证：CLI 记录
// 时只校验引用非空/1:1，不解析代码、不校验引用指向的测试是否已交付——格式合法的引用（哪怕
// 指向尚未交付的文件/函数）即可记录，存在性与对应性留给 qa-review 读代码核对、qa-execution
// 实际运行验证。
func TestWhiteboxBindingDefersExistence(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-binding-deferred"), "custom", []string{whiteboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "not_delivered_test.go::TestNotDeliveredYet"}}, "")
	if err != nil {
		t.Fatalf("a well-formed but not-yet-delivered test reference was rejected by the CLI: %v", err)
	}
	if got := state.qaCases("whitebox"); len(got) != 1 || got[0].Test != "not_delivered_test.go::TestNotDeliveredYet" {
		t.Fatalf("recorded whitebox case reference mismatch: %#v", got)
	}
}

// TestDesignRuntimeErrorPerModeIndependent 覆盖 RQ-001 的 qa-design 权威结果按 mode 独立：
// 黑盒设计轮 RUNTIME_ERROR 只把黑盒 mode 的设计结果记为 RUNTIME_ERROR、只重置黑盒执行结果，
// 白盒设计权威结果与白盒执行结果不受影响（黑盒失败不得把白盒设计重置为 PENDING）。
func TestDesignRuntimeErrorPerModeIndependent(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "design-runtime-error"), "custom", []string{blackboxQAID, whiteboxQAID, "quality"})
	worktree := createQAWorktree(t, root, state)
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	bbDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, bbDesign, []QACaseInput{{Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	wbDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, wbDesign, []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("blackbox").Status != "PASS" || state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("per-mode designs not PASS before runtime error: bb=%#v wb=%#v", state.qaDesign("blackbox"), state.qaDesign("whitebox"))
	}
	runtimeDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, runtimeDispatch, nil, "design agent failed")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("blackbox").Status != "RUNTIME_ERROR" {
		t.Fatalf("blackbox design RUNTIME_ERROR not recorded: %#v", state.qaDesign("blackbox"))
	}
	if state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("blackbox design RUNTIME_ERROR reset the whitebox design: %#v", state.qaDesign("whitebox"))
	}
	if state.qaExecution("blackbox").Status != "PENDING" {
		t.Fatalf("blackbox design RUNTIME_ERROR did not reset blackbox execution: %#v", state.qaExecution("blackbox"))
	}
	if wb := state.qaExecution("whitebox"); wb.Status != "PENDING" && wb.Status != "" {
		t.Fatalf("blackbox design RUNTIME_ERROR touched whitebox execution: %#v", wb)
	}
}

// TestQAModeReviewDesignMergedFallback 覆盖 RQ-001 的读取回退语义（最低层）：qaReview/qaDesign
// 的 per-mode 键非空即用、否则回退合并 "" 键（单派发/legacy 合并形态）；空状态键（Status==""）
// 不遮蔽回退。recorder 写回与读取用同一存储键。
func TestQAModeReviewDesignMergedFallback(t *testing.T) {
	empty := RunState{}
	if got := empty.qaReview("whitebox"); got.Status != "" {
		t.Fatalf("empty state qaReview: %#v", got)
	}
	if got := empty.qaDesign("blackbox"); got.Status != "" {
		t.Fatalf("empty state qaDesign: %#v", got)
	}
	merged := RunState{}
	merged.setQAReview("", ActionResult{Status: "PASS"})
	merged.setQADesign("", ActionResult{Status: "PASS"})
	if got := merged.qaReview("whitebox"); got.Status != "PASS" {
		t.Fatalf("qaReview(whitebox) did not fall back to the merged key: %#v", got)
	}
	if got := merged.qaDesign("blackbox"); got.Status != "PASS" {
		t.Fatalf("qaDesign(blackbox) did not fall back to the merged key: %#v", got)
	}
	merged.setQAReview("whitebox", ActionResult{Status: "FAIL"})
	merged.setQADesign("blackbox", ActionResult{Status: "PENDING"})
	if got := merged.qaReview("whitebox"); got.Status != "FAIL" {
		t.Fatalf("per-mode whitebox review not preferred: %#v", got)
	}
	if got := merged.qaReview("blackbox"); got.Status != "PASS" {
		t.Fatalf("blackbox review should still read the merged key: %#v", got)
	}
	if got := merged.qaDesign("blackbox"); got.Status != "PENDING" {
		t.Fatalf("per-mode blackbox design not preferred: %#v", got)
	}
	if got := merged.qaDesign("whitebox"); got.Status != "PASS" {
		t.Fatalf("whitebox design should still read the merged key: %#v", got)
	}
	merged.setQAReview("blackbox", ActionResult{})
	if got := merged.qaReview("blackbox"); got.Status != "PASS" {
		t.Fatalf("empty-status per-mode key should fall back to the merged key: %#v", got)
	}
}

// TestCarryReadsPriorWhenResultResetToPending 覆盖 RQ-002 的 priorQAExecution 回退路径：修复
// 快照/重设计把 per-mode 执行结果重置为 PENDING 后，main-agent Carry 直取该 mode 时回退到
// 保留的上一轮权威结果（任意快照读取，不要求 current snapshot）；per-mode 权威结果优先于
// prior；无记录时回退合并 "" 键。
func TestCarryReadsPriorWhenResultResetToPending(t *testing.T) {
	state := RunState{
		QAExecutionByMode: map[string]QAExecutionResult{
			"blackbox": {Status: "PENDING"},
		},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{
			"blackbox": {Status: "PASS", Snapshot: "s1"},
		},
	}
	key, result := qaModeCarryResultKey(state, "blackbox")
	if key != "blackbox" || result.Status != "PASS" || result.Snapshot != "s1" {
		t.Fatalf("carry did not fall back to the preserved prior: key=%s result=%#v", key, result)
	}
	none := RunState{}
	if key, result := qaModeCarryResultKey(none, "whitebox"); key != "" || result.Status != "" {
		t.Fatalf("carry with no record did not fall back to the merged key: key=%s result=%#v", key, result)
	}
	prefer := RunState{
		QAExecutionByMode: map[string]QAExecutionResult{
			"whitebox": {Status: "FAIL", Snapshot: "s2"},
		},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{
			"whitebox": {Status: "PASS", Snapshot: "s1"},
		},
	}
	if key, result := qaModeCarryResultKey(prefer, "whitebox"); key != "whitebox" || result.Status != "FAIL" || result.Snapshot != "s2" {
		t.Fatalf("carry did not prefer the authoritative result: key=%s result=%#v", key, result)
	}
}

// TestSealedStateRetainsIntegrity 覆盖 RQ-009 的"随 Seal 保留"：SEALED run 状态文件仍携带
// StateIntegrity，保存后原样加载成功、JSON 内字段在场（完整性校验对封存状态同样生效）。
func TestSealedStateRetainsIntegrity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "sealed-integrity")
	state.Status = "SEALED"
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatalf("sealed state round-trip load failed: %v", err)
	}
	if loaded.Status != "SEALED" || loaded.StateIntegrity == "" {
		t.Fatalf("sealed state lost its integrity field: %#v", loaded)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	if integrity, _ := decoded["stateIntegrity"].(string); integrity == "" {
		t.Fatal("sealed state file does not persist the stateIntegrity field")
	}
}

// TestBacklogAndKnowledgeDocsIgnored 覆盖 RQ-005/RQ-006 的验收：P2-BACKLOG.md 与
// PROMPT-ENGINEERING-KNOWLEDGE.md 存在于仓库根目录且被 .gitignore 忽略（git check-ignore
// 通过），记录范围外、不跟踪。
func TestBacklogAndKnowledgeDocsIgnored(t *testing.T) {
	root := repoRootForCanaryTest(t)
	for _, name := range []string{"P2-BACKLOG.md", "PROMPT-ENGINEERING-KNOWLEDGE.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("%s missing at the repo root: %v", name, err)
		}
		out, err := (execNativeCommandRunner{}).Run(root, "git", "check-ignore", name)
		if err != nil || !strings.Contains(out, name) {
			t.Fatalf("git check-ignore %s failed: out=%q err=%v", name, out, err)
		}
	}
}

// TestReviewRuleSingleHomeInFormalFlow 覆盖 RQ-004 的去重结构：复审机制全文唯一持有在
// references/formal-flow.md；SKILL.md 第 4 步保留可执行摘要（决策级铁律：P0/P1/P2 分级、
// 仅含 P2 可 PASS、确认→重审/驳回→作废、主代理无破例权）并一行指针指向 formal-flow，
// 不再重复机制全文。
func TestReviewRuleSingleHomeInFormalFlow(t *testing.T) {
	root := repoRootForCanaryTest(t)
	formalFlow, err := os.ReadFile(filepath.Join(root, "references", "formal-flow.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"仅含 P2 → 该轮即记录 PASS", "含 P0/P1 → 记录 FAIL", "主代理无破例权", "需重审"} {
		if !strings.Contains(string(formalFlow), marker) {
			t.Fatalf("references/formal-flow.md missing the full review-rule marker %q (single home)", marker)
		}
	}
	skill, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"P0/P1/P2 分级", "仅含 P2 可记录 PASS、含 P0/P1 记录 FAIL", "驳回→作废", "主代理无破例权", "references/formal-flow.md"} {
		if !strings.Contains(string(skill), marker) {
			t.Fatalf("SKILL.md missing the step-4 review-rule summary marker %q", marker)
		}
	}
}
