//go:build phase0whitebox

package validate

// 白盒结构测试（QA-INCREMENTAL-ISOLATION 白盒用例实现）。
//
// 本文件是白盒设计者独立交付的结构测试代码，绑定本 run 的每一条白盒用例
// （--test "<文件路径>::<函数名>"，一测试实现一用例）。按三处改动的职责归属分层：
//   - 改动 1（黑盒用例隔离 + seal 合回）：mirror 派生视图、mirror 写入门、物化过滤、
//     seal 合回路径。
//   - 改动 2（QA 用例真增量）：默认增量保留、--case-id 修改、未知 id 拒绝、语义重复拒绝、
//     --remove-case / --replace-all、按 mode 隔离、跨 mode 全局唯一 id、返工约束、
//     --test 绑定、审查提示词增量上下文。
//   - 改动 3（write-block 收窄）：只按真实写目标判定，只读命令（即使提到 .gates）放行、
//     真写仍拦；重定向解析；代码/run 状态路径判定；hook 级放行/拦截。
//
// 测试在开发后主工作区（绑定当前快照）运行，与 development-worker 交付的既有测试相互独立。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertBlackboxMirrorContent asserts the file at path holds exactly the given
// derived-view bytes (single source: mirror / seal materialization are both
// blackboxCasesMarkdown of the run-state cases).
func assertBlackboxMirrorContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("file is not the run-state derived view:\n--- got ---\n%s\n--- want ---\n%s", data, want)
	}
}

// TestWhiteboxMirrorDerivedFromRunStateBlackboxCases verifies 改动 1 的 mirror 是
// run-state 的派生视图（单一来源，无双重 source drift）：黑盒 qa-design 记录（已登记隔离
// 工作区）后，隔离工作区 .gates/cases/blackbox.md 的字节等于
// blackboxCasesMarkdown(run-state 黑盒用例)，包含每条用例的 id/mode/description/
// procedure/oracle/review status；主干不可见；补全轮每轮重写 mirror。
func TestWhiteboxMirrorDerivedFromRunStateBlackboxCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-mirror-derived"), "custom", []string{blackboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	mirrorPath := filepath.Join(worktree, ".gates", "cases", "blackbox.md")

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertBlackboxMirrorContent(t, mirrorPath, blackboxCasesMarkdown(state.RunID, state.qaModeCases("blackbox")))
	data, _ := os.ReadFile(mirrorPath)
	for _, want := range []string{"CASE-001", "CASE-002", "mode: blackbox", "review status: PENDING"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("mirror missing %q:\n%s", want, data)
		}
	}
	// 黑盒阶段主干上看不到用例文件。
	if _, err := os.Stat(filepath.Join(root, ".gates", "cases", "blackbox.md")); !os.IsNotExist(err) {
		t.Fatalf("mirror leaked to the main worktree: %v", err)
	}

	// 补全轮每轮重写 mirror，包含新增用例。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "edge behavior", Procedure: "run edge CLI", Oracle: "edge success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertBlackboxMirrorContent(t, mirrorPath, blackboxCasesMarkdown(state.RunID, state.qaModeCases("blackbox")))
	data, _ = os.ReadFile(mirrorPath)
	for _, want := range []string{"CASE-003", "edge behavior"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("mirror not rewritten after supplement (missing %q):\n%s", want, data)
		}
	}
}

// TestWhiteboxMirrorOnlyForBlackboxDesignWithRegisteredWorktree verifies 改动 1 的
// mirror 写入门：仅 blackbox mode、仅隔离工作区已登记时写入。白盒设计轮不写
// .gates/cases/blackbox.md；黑盒设计轮才写，且 mirror 只含黑盒用例。
func TestWhiteboxMirrorOnlyForBlackboxDesignWithRegisteredWorktree(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-mirror-gate"), "custom", []string{blackboxQAID, whiteboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worktree, ".gates", "cases", "blackbox.md")); !os.IsNotExist(err) {
		t.Fatalf("whitebox design round wrote a blackbox mirror: %v", err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	mirrorPath := filepath.Join(worktree, ".gates", "cases", "blackbox.md")
	assertBlackboxMirrorContent(t, mirrorPath, blackboxCasesMarkdown(state.RunID, state.qaModeCases("blackbox")))
	data, _ := os.ReadFile(mirrorPath)
	if strings.Contains(string(data), "mode: whitebox") {
		t.Fatalf("blackbox mirror leaked whitebox cases:\n%s", data)
	}
}

// TestWhiteboxMaterializeOnlyApprovedBlackboxCasesFromRunState verifies 改动 1 的
// seal 物化函数职责：materializeBlackboxCases 只物化已批准（ReviewStatus PASS）黑盒用例，
// 内容等于 blackboxCasesMarkdown(已批准集合)；来源是 run-state（即使隔离工作区已清空也
// 可物化）；无已批准用例时不产出物化文件。
func TestWhiteboxMaterializeOnlyApprovedBlackboxCasesFromRunState(t *testing.T) {
	root := t.TempDir()
	state := RunState{
		RunID: "wb-materialize",
		VCS:   "git",
		QACasesByMode: map[string][]QACase{
			"blackbox": {
				{ID: "CASE-001", Mode: "blackbox", Description: "approved behavior", Procedure: "p1", Oracle: "o1", ReviewStatus: "PASS"},
				{ID: "CASE-002", Mode: "blackbox", Description: "pending behavior", Procedure: "p2", Oracle: "o2", ReviewStatus: "PENDING"},
				{ID: "CASE-003", Mode: "blackbox", Description: "failed behavior", Procedure: "p3", Oracle: "o3", ReviewStatus: "FAIL"},
			},
		},
		// 黑盒 review PASS 后隔离工作区已清空：物化读 run-state，不依赖工作区残留。
		QAWorktree: "/nonexistent-cleared-worktree",
	}
	if err := materializeBlackboxCases(root, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	approved := []QACase{{ID: "CASE-001", Mode: "blackbox", Description: "approved behavior", Procedure: "p1", Oracle: "o1", ReviewStatus: "PASS"}}
	assertBlackboxMirrorContent(t, path, blackboxCasesMarkdown(state.RunID, approved))
	for _, absent := range []string{"CASE-002", "CASE-003", "pending behavior", "failed behavior"} {
		if strings.Contains(string(data), absent) {
			t.Fatalf("materialized file leaked unapproved case %q:\n%s", absent, data)
		}
	}

	// 无已批准黑盒用例：不产出物化文件（与 ledger 同目录、同交付行为）。
	root2 := t.TempDir()
	empty := RunState{RunID: "wb-materialize-empty", QACasesByMode: map[string][]QACase{"blackbox": {{ID: "CASE-001", Mode: "blackbox", ReviewStatus: "PENDING"}}}}
	if err := materializeBlackboxCases(root2, empty); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root2, ".gates", "results", empty.RunID+".blackbox-cases.md")); !os.IsNotExist(err) {
		t.Fatalf("no-approved run produced a materialized file: %v", err)
	}
}

// TestWhiteboxSealMaterializesApprovedBlackboxCasesToLedgerDir verifies 改动 1 的合回
// 时点与执行者：seal 时 CLI 把已批准黑盒用例物化到主工作区 .gates/results/
// <run-id>.blackbox-cases.md（与 seal ledger 同目录），内容等于 blackboxCasesMarkdown；
// 黑盒 review PASS 已清空隔离工作区登记，物化仍成功（读 run-state 单一来源）。
func TestWhiteboxSealMaterializesApprovedBlackboxCasesToLedgerDir(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-seal-materialize"), "custom", []string{blackboxQAID, "quality"})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-seal-bb", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.QAWorktree != "" {
		t.Fatalf("blackbox review PASS did not clear the isolation worktree registration: %q", state.QAWorktree)
	}
	want := blackboxCasesMarkdown(state.RunID, state.qaModeCases("blackbox"))

	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-wb-seal.txt"), "delivery\n")
	commitAll(t, root, "delivery wb seal")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	exec := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, exec, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "wb-seal-quality", "PASS", "", nil)
	summary, err := Seal(root, pkg, state.RunID, nil, false, "sealed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("summary=%#v", summary)
	}

	path := filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")
	assertBlackboxMirrorContent(t, path, want)
	data, _ := os.ReadFile(path)
	for _, wantStr := range []string{"CASE-001", "CASE-002", "review status: PASS"} {
		if !strings.Contains(string(data), wantStr) {
			t.Fatalf("materialized file missing %q:\n%s", wantStr, data)
		}
	}
}

// TestWhiteboxIncrementalRetainsUnmentionedPassingCases verifies 改动 2 的默认增量：
// qa-design 只返回变更，未提及用例及其 PASS 状态自动保留（不再清除），新增用例由 CLI
// 分配 id、ReviewStatus PENDING，本轮新增留痕记入 QADesignChangesByMode。
func TestWhiteboxIncrementalRetainsUnmentionedPassingCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-inc-retain"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-inc-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 第二轮只新增一个用例，两个既有用例未提及。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	cases := state.allQACases()
	if len(cases) != 3 {
		t.Fatalf("unmentioned cases were cleared by omission: %#v", cases)
	}
	byID := map[string]QACase{}
	for _, testCase := range cases {
		byID[testCase.ID] = testCase
	}
	for _, id := range []string{"CASE-001", "CASE-002"} {
		if c := byID[id]; c.ReviewStatus != "PASS" {
			t.Fatalf("unmentioned PASS case %s was reopened: %#v", id, c)
		}
	}
	if c := byID["CASE-003"]; c.ReviewStatus != "PENDING" {
		t.Fatalf("new case not PENDING: %#v", c)
	}
	change := state.QADesignChangesByMode[""]
	if len(change.Added) != 1 || change.Added[0] != "CASE-003" || len(change.Modified) != 0 || len(change.Removed) != 0 {
		t.Fatalf("incremental change list not recorded: %#v", change)
	}
}

// TestWhiteboxCaseIDModifyUpdatesAndReopensExisting verifies 改动 2 的 --case-id 修改：
// 按 id 引用既有用例、原地更新规格、ReviewStatus 置回 PENDING（须重新审查）、id 不变、
// 其余用例不动，本轮修改留痕记入 Modified。
func TestWhiteboxCaseIDModifyUpdatesAndReopensExisting(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-caseid-modify"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-caseid-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{CaseID: "CASE-001", Mode: "blackbox", Description: "public workflow with retries", Procedure: "run the public CLI twice", Oracle: "observable success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	cases := state.qaCases("")
	if len(cases) != 2 {
		t.Fatalf("modify round changed the set size: %#v", cases)
	}
	c := cases[0]
	if c.ID != "CASE-001" || c.Description != "public workflow with retries" || c.Procedure != "run the public CLI twice" || c.ReviewStatus != "PENDING" {
		t.Fatalf("--case-id modify not applied in place: %#v", c)
	}
	if cases[1].ID != "CASE-002" || cases[1].ReviewStatus != "PASS" {
		t.Fatalf("unmodified case was disturbed: %#v", cases[1])
	}
	change := state.QADesignChangesByMode[""]
	if len(change.Modified) != 1 || change.Modified[0] != "CASE-001" || len(change.Added) != 0 || len(change.Removed) != 0 {
		t.Fatalf("modified list not recorded: %#v", change)
	}
}

// TestWhiteboxIncrementalUnknownIDsRejectedWithoutStateChange verifies 改动 2 的
// id 失配行为定死：--case-id / --remove-case 引用不存在的 id 均被拒绝、状态不变，不静默
// 当新用例造成重复。
func TestWhiteboxIncrementalUnknownIDsRejectedWithoutStateChange(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-unknown-id"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{CaseID: "CASE-999", Mode: "blackbox", Description: "x", Procedure: "y", Oracle: "z"}}, ""); err == nil || !strings.Contains(err.Error(), "references unknown id") {
		t.Fatalf("unknown --case-id accepted: %v", err)
	}
	if _, err := RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{RemoveCases: []string{"CASE-999"}}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown --remove-case id accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected incremental records changed state")
	}
}

// TestWhiteboxIncrementalSemanticDuplicateRejected verifies 改动 2 的语义重复拒绝：
// 无 id 提交的规格与既有用例 description+procedure+oracle 一致时判重复、拒绝并提示改用
// --case-id 修改，不分配新 id。
func TestWhiteboxIncrementalSemanticDuplicateRejected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-dup"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	dup := []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}
	if _, err := RecordQADesign(root, pkg, state.RunID, design, dup, ""); err == nil || !strings.Contains(err.Error(), "duplicates existing case CASE-001") || !strings.Contains(err.Error(), "--case-id") {
		t.Fatalf("semantic duplicate no-id submission accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected semantic duplicate changed state")
	}
}

// TestWhiteboxRemoveAndReplaceAllSemantics verifies 改动 2 的显式操作：--remove-case 按
// id 删除并留痕 Removed；--replace-all 与 --remove-case 互斥；--replace-all 整体替换本
// mode 用例集、空集即清空该 mode。
func TestWhiteboxRemoveAndReplaceAllSemantics(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-remove-replace"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	if _, err := RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{ReplaceAll: true, RemoveCases: []string{"CASE-001"}}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("--replace-all + --remove-case accepted: %v", err)
	}

	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{RemoveCases: []string{"CASE-002"}})
	if err != nil {
		t.Fatal(err)
	}
	if all := state.allQACases(); len(all) != 1 || all[0].ID != "CASE-001" {
		t.Fatalf("--remove-case did not delete the target: %#v", all)
	}
	change := state.QADesignChangesByMode[""]
	if len(change.Removed) != 1 || change.Removed[0] != "CASE-002" {
		t.Fatalf("removed list not recorded: %#v", change)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "replacement structure", Procedure: "run the replacement test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructureRevised"},
	}, "", QADesignRecordOptions{ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if all := state.allQACases(); len(all) != 1 || all[0].Description != "replacement structure" || all[0].ReviewStatus != "PENDING" {
		t.Fatalf("--replace-all did not replace the whole set: %#v", all)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if all := state.allQACases(); len(all) != 0 {
		t.Fatalf("--replace-all empty did not clear the mode: %#v", all)
	}
}

// TestWhiteboxDesignPerModeIsolation verifies 改动 2 的按 mode 隔离：白盒设计轮只动本
// mode 的用例列表，不触碰黑盒列表；各 mode 设计结果独立记录。
func TestWhiteboxDesignPerModeIsolation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-permode"), "custom", []string{blackboxQAID, whiteboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if bb := state.qaCases("blackbox"); len(bb) != 2 {
		t.Fatalf("whitebox round touched the blackbox list: %#v", bb)
	}
	if wb := state.qaCases("whitebox"); len(wb) != 1 || wb[0].Mode != "whitebox" {
		t.Fatalf("whitebox list not recorded under its own key: %#v", wb)
	}
	if all := state.allQACases(); len(all) != 3 {
		t.Fatalf("allQACases did not merge per-mode lists: %#v", all)
	}
	if state.qaDesign("blackbox").Status != "PASS" || state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("per-mode design results not recorded independently: bb=%#v wb=%#v", state.qaDesign("blackbox"), state.qaDesign("whitebox"))
	}
}

// TestWhiteboxNewCaseIDsGloballyUniqueAcrossModes verifies 改动 2 的跨 mode 全局唯一 id：
// 新 id 生成保留全部 mode 已占用 id，白盒新增用例不与黑盒既有用例撞号。
func TestWhiteboxNewCaseIDsGloballyUniqueAcrossModes(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-globalid"), "custom", []string{blackboxQAID, whiteboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	wb := state.qaCases("whitebox")
	if len(wb) != 1 || wb[0].ID != "CASE-003" {
		t.Fatalf("new whitebox id collides with blackbox ids or is unexpected: %#v", wb)
	}
	ids := map[string]bool{}
	for _, testCase := range state.allQACases() {
		if ids[testCase.ID] {
			t.Fatalf("duplicate case id across modes: %s", testCase.ID)
		}
		ids[testCase.ID] = true
	}
}

// TestWhiteboxIncrementalReworkRequiresChange verifies 改动 2 的返工约束：集合层面 P1
// 发现项使 review FAIL 而各用例仍 PASS 时，无实质变更的 qa-design 记录被拒；显式
// --remove-case 即实质变更、返工通过。
func TestWhiteboxIncrementalReworkRequiresChange(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-rework"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}

	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-rework-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{{Severity: "P1", Message: "coverage gap: recovery path untested"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" {
		t.Fatalf("set-level P1 finding did not fail the review: %#v", state.qaReview(""))
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	if _, err := RecordQADesign(root, pkg, state.RunID, design, nil, ""); err == nil || !strings.Contains(err.Error(), "add or revise a case") {
		t.Fatalf("no-op rework accepted: %v", err)
	}
	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{RemoveCases: []string{"CASE-002"}})
	if err != nil {
		t.Fatal(err)
	}
	if all := state.allQACases(); len(all) != 1 || all[0].ID != "CASE-001" {
		t.Fatalf("removal-only rework not applied: %#v", all)
	}
}

// TestWhiteboxIncrementalCaseLevelFailReworkRejected verifies 改动 2 的返工约束在用例级
// review FAIL 下同样生效：至少一条用例被判 FAIL（而非集合级覆盖遗漏）时，无实质变更的
// qa-design 记录（相同规格经 --case-id 重交）被拒；实质修订（改规格）才放行、review 重开。
func TestWhiteboxIncrementalCaseLevelFailReworkRejected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-case-fail-rework"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}

	// 用例级 FAIL：CASE-001 被判 FAIL（其余 PASS），review 权威结果 FAIL。
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-case-fail-rework-reviewer")
	var decisions []QAReviewInput
	for _, testCase := range state.allQACases() {
		if testCase.ID == "CASE-001" {
			decisions = append(decisions, QAReviewInput{CaseID: testCase.ID, Outcome: "FAIL", Reason: "oracle does not pin the observable outcome"})
		} else {
			decisions = append(decisions, QAReviewInput{CaseID: testCase.ID, Outcome: "PASS"})
		}
	}
	state, err = RecordQAReview(root, pkg, state.RunID, review, decisions, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" {
		t.Fatalf("case-level FAIL did not fail the review: %#v", state.qaReview(""))
	}

	// 相同规格经 --case-id 重交：无实质变更，返工约束拒绝（用例级 FAIL 不再因"还有未
	// PASS 用例"而放行）。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	identical := []QACaseInput{{CaseID: "CASE-001", Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}
	if _, err := RecordQADesign(root, pkg, state.RunID, design, identical, ""); err == nil || !strings.Contains(err.Error(), "add or revise a case") {
		t.Fatalf("identical-spec case-level rework accepted: %v", err)
	}

	// 实质修订（改 oracle）放行：设计 PASS、被改用例 ReviewStatus 置回 PENDING（review 重开）。
	revised := []QACaseInput{{CaseID: "CASE-001", Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the delivered structure test passes deterministically", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}
	state, err = RecordQADesign(root, pkg, state.RunID, design, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("").Status != "PASS" {
		t.Fatalf("substantive rework not recorded: %#v", state.qaDesign(""))
	}
	for _, testCase := range state.allQACases() {
		if testCase.ID == "CASE-001" && testCase.ReviewStatus != "PENDING" {
			t.Fatalf("substantive rework did not reopen case review: %#v", testCase)
		}
	}
}

// TestWhiteboxTestBindingEnforced verifies 改动 2 的白盒测试绑定：白盒用例必须带
// --test "<文件>::<函数>" 引用（缺失即拒绝）；同一引用不能被两条白盒用例共用（一测试
// 实现一用例）；各自独占引用时正常记录。
func TestWhiteboxTestBindingEnforced(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-binding"), "full", nil)

	// 拒绝性记录共用一个已 CLAIMED 派发（被拒记录不完成派发，可复用；复用避免触发
	// "被中断派发"的续用/询问守卫）。
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	if _, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "whitebox", Description: "missing test", Procedure: "run test", Oracle: "passes"}}, ""); err == nil || !strings.Contains(err.Error(), "requires a --test") {
		t.Fatalf("whitebox case without a test reference accepted: %v", err)
	}
	if _, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "one", Procedure: "run t", Oracle: "passes", Test: "delivered.go::TestOne"},
		{Mode: "whitebox", Description: "two", Procedure: "run t", Oracle: "passes", Test: "delivered.go::TestOne"},
	}, ""); err == nil || !strings.Contains(err.Error(), "one test implements one case") {
		t.Fatalf("shared whitebox test reference accepted: %v", err)
	}
	state, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "one", Procedure: "run t", Oracle: "passes", Test: "delivered.go::TestOne"},
		{Mode: "whitebox", Description: "two", Procedure: "run t", Oracle: "passes", Test: "delivered.go::TestTwo"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if all := state.allQACases(); len(all) != 2 {
		t.Fatalf("unique-bound whitebox cases not recorded: %#v", all)
	}
}

// TestWhiteboxReviewPromptInjectsIncrementalChangeContext verifies 改动 2 的审查全上下文：
// qa-review 提示词注入本设计轮的变更清单（按 mode 独立）；黑盒（隔离工作区已登记）提示词
// 同时指向隔离工作区的用例 mirror 文件；白盒提示词不指黑盒 mirror。
func TestWhiteboxReviewPromptInjectsIncrementalChangeContext(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "wb-review-context"), "custom", []string{blackboxQAID, whiteboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", "blackbox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	for _, want := range []string{"changed: added CASE-001", filepath.Join(worktree, ".gates", "cases", "blackbox.md")} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("blackbox review prompt omitted %q:\n%s", want, prompt)
		}
	}

	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err = PrepareAction(root, pkg, state.RunID, "qa-review", "whitebox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "changed: added CASE-002") {
		t.Fatalf("whitebox review prompt omitted its mode change list:\n%s", prompt)
	}
	if strings.Contains(prompt, "blackbox.md") {
		t.Fatalf("whitebox review prompt should not point at the blackbox mirror:\n%s", prompt)
	}
}

// TestWhiteboxCommandWritesFilesJudgesRealTargetsNotGatesMention verifies 改动 3 的最低层
// commandWritesFiles：命令文本提到 .gates 但只是只读查询不再判写入；真实写目标
// （git/tee/sed -i/重定向到 .gates/代码）仍判写入。
func TestWhiteboxCommandWritesFilesJudgesRealTargetsNotGatesMention(t *testing.T) {
	readOnly := []string{
		"rg --files .gates/",
		"grep -rn reviewStatus .gates/cases/",
		"cat .gates/cases/blackbox.md",
		"git log --oneline -- .gates/results/",
		"git diff --stat HEAD -- .gates/results/",
		"git show HEAD:.gates/tmp/x/state.json",
		"python3 read.py .gates/cases/blackbox.md",
	}
	for _, cmd := range readOnly {
		if commandWritesFiles(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a write: %q", cmd)
		}
	}
	writing := []string{
		"git add .gates/results/x.json",
		"git commit -m x",
		"echo x > .gates/cases/blackbox.md",
		"echo x >> .gates/tmp/state.json",
		"tee .gates/results/x.md",
		"sed -i 's/a/b/' internal/code.go",
	}
	for _, cmd := range writing {
		if !commandWritesFiles(cmd) {
			t.Fatalf("real write to .gates/code not detected: %q", cmd)
		}
	}
}

// TestWhiteboxBashWriteTargetsJudgesRealTargetsNotGatesMention verifies 改动 3 的主线程
// Bash 判定 bashWriteTargetsCodeOrState：只读命令（含 .gates 字样）放行；真写 .gates/代码
// 仍拦；重定向到非代码、非 run 状态文档不命中。
func TestWhiteboxBashWriteTargetsJudgesRealTargetsNotGatesMention(t *testing.T) {
	readOnly := []string{
		"rg --files .gates/",
		"grep -rn reviewStatus .gates/cases/",
		"cat .gates/cases/blackbox.md",
		"git log --oneline -- .gates/results/",
		"git status --short .gates/",
		"git show HEAD:.gates/tmp/x/state.json",
		"python3 read.py .gates/cases/blackbox.md",
	}
	for _, cmd := range readOnly {
		if bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a code/state write: %q", cmd)
		}
	}
	writing := []string{
		"git add .gates/results/x.json",
		"git commit -m x",
		"echo x > .gates/cases/blackbox.md",
		"tee .gates/results/x.md",
		"echo x > internal/code.go",
	}
	for _, cmd := range writing {
		if !bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("real write to .gates/code not detected by the main-thread judge: %q", cmd)
		}
	}
	for _, cmd := range []string{"echo note >> P2-BACKLOG.md", "echo x > notes.md"} {
		if bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("redirect to a non-code doc misjudged as a code/state write: %q", cmd)
		}
	}
}

// TestWhiteboxRedirectWriteTargetParsing verifies 改动 3 的重定向目标解析：引号目标、追加重
// 定向、stderr 重定向、noclobber 都能解析；只读惯用法（2>&1、> /dev/null、2>/dev/null）不
// 命中。
func TestWhiteboxRedirectWriteTargetParsing(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{command: `echo x > "quoted.log"`, want: []string{"quoted.log"}},
		{command: "echo x >> log.txt", want: []string{"log.txt"}},
		{command: "cmd 2> err.log", want: []string{"err.log"}},
		{command: "echo a >| out.txt", want: []string{"out.txt"}},
		{command: "echo x > main.go", want: []string{"main.go"}},
		{command: "go test ./... 2>&1 | head", want: nil},
		{command: "echo hi > /dev/null", want: nil},
		{command: "go test ./... 2>/dev/null", want: nil},
		{command: "echo hi > /dev/null 2>&1", want: nil},
	} {
		got := redirectWriteTargets(tc.command)
		if len(got) != len(tc.want) {
			t.Fatalf("redirectWriteTargets(%q) = %#v, want %#v", tc.command, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("redirectWriteTargets(%q) = %#v, want %#v", tc.command, got, tc.want)
			}
		}
	}
}

// TestWhiteboxIsCodeOrRunStatePathClassification verifies 改动 3 的目标路径判定：run 状态
// （任何 .gates 路径）与代码扩展名命中，非代码、非状态文档不命中。
func TestWhiteboxIsCodeOrRunStatePathClassification(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: ".gates/cases/blackbox.md", want: true},
		{path: "internal/main.go", want: true},
		{path: "scripts/deploy.sh", want: true},
		{path: "P2-BACKLOG.md", want: false},
		{path: "notes.md", want: false},
		{path: "README.md", want: false},
		{path: "", want: false},
		{path: ".gates", want: true},
	} {
		if got := isCodeOrRunStatePath(tc.path); got != tc.want {
			t.Fatalf("isCodeOrRunStatePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestWhiteboxHookAllowsGatesMentionReadsAndDeniesRealWrites verifies 改动 3 的 hook 级
// 判定：活动 run 下主线程/审查类代理的只读 Bash（含 .gates 字样）放行；真写 .gates/代码
// 仍被拦。
func TestWhiteboxHookAllowsGatesMentionReadsAndDeniesRealWrites(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	for _, tc := range []struct {
		name     string
		command  string
		agent    string
		wantDeny bool
	}{
		{name: "main thread rg reads gates", command: "rg --files .gates/"},
		{name: "reviewer grep reads gates", command: "grep -rn reviewStatus .gates/cases/", agent: "qa-review"},
		{name: "main thread python3 reads gates", command: "python3 read.py .gates/cases/blackbox.md"},
		{name: "main thread git diff over gates path", command: "git diff --stat HEAD -- .gates/results/"},
		{name: "main thread redirect writes gates", command: "echo x > .gates/cases/blackbox.md", wantDeny: true},
		{name: "reviewer tee writes code", command: "tee internal/code.go", agent: "qa-review", wantDeny: true},
		{name: "main thread git add gates", command: "git add .gates/results/x.json", wantDeny: true},
	} {
		agentPart := ""
		if tc.agent != "" {
			agentPart = fmt.Sprintf(`,"agent_type":%q`, tc.agent)
		}
		payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}%s}`, root, tc.command, agentPart)
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.wantDeny && decision.PermissionDecision != "deny" {
			t.Fatalf("%s: expected deny, got %#v", tc.name, decision)
		}
		if !tc.wantDeny && decision.PermissionDecision != "allow" {
			t.Fatalf("%s: expected allow, got %#v", tc.name, decision)
		}
	}
}
