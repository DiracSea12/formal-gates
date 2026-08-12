package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestQADesignIncrementalMergeSemantics covers 改动 2 的增量合并核心语义：默认只返回
// 变更、未提及用例及其 PASS 状态自动保留；--case-id 修改既有用例；--remove-case 删除
// （id 必须存在）；--replace-all 整体替换（空集清空）；以及各 id 失配行为定死
// （未知 id / 语义重复 / 互斥操作均拒绝且不改状态）。
func TestQADesignIncrementalMergeSemantics(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "incremental-merge"), "full", nil)

	// 首次设计 3 个用例（1 黑盒 + 2 白盒）。
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
		{Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"},
		{Mode: "whitebox", Description: "failure paths covered", Procedure: "run the delivered failure-path test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxFailurePaths"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 3 || state.qaCases("")[0].ID != "CASE-001" || state.qaCases("")[2].ID != "CASE-003" {
		t.Fatalf("first design not recorded: %#v", state.allQACases())
	}
	// review 全部 PASS。
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "inc-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 默认增量：只新增一个用例；未提及的 3 个既有用例（含 PASS）自动保留。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "recovery workflow succeeds", Procedure: "trigger recovery and rerun", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 4 {
		t.Fatalf("unmentioned cases were cleared by omission: %#v", state.allQACases())
	}
	for _, testCase := range state.qaCases("") {
		switch testCase.ID {
		case "CASE-001", "CASE-002", "CASE-003":
			if testCase.ReviewStatus != "PASS" {
				t.Fatalf("unmentioned PASS case %s was reopened: %#v", testCase.ID, testCase)
			}
		case "CASE-004":
			if testCase.ReviewStatus != "PENDING" {
				t.Fatalf("new case not PENDING: %#v", testCase)
			}
		default:
			t.Fatalf("unexpected id %q: %#v", testCase.ID, testCase)
		}
	}
	change := state.QADesignChangesByMode[""]
	if len(change.Added) != 1 || change.Added[0] != "CASE-004" || len(change.Modified) != 0 || len(change.Removed) != 0 {
		t.Fatalf("incremental change list not recorded: %#v", change)
	}

	// --case-id 修改既有用例：CASE-002 规格更新、置回 PENDING、记录 modified。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{CaseID: "CASE-002", Mode: "blackbox", Description: "public workflow succeeds with retries", Procedure: "run the documented public CLI twice", Oracle: "observable output succeeds"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaCases("")[1].ID != "CASE-002" || state.qaCases("")[1].ReviewStatus != "PENDING" || state.qaCases("")[1].Description != "public workflow succeeds with retries" {
		t.Fatalf("--case-id modify not applied: %#v", state.qaCases("")[1])
	}
	change = state.QADesignChangesByMode[""]
	if len(change.Modified) != 1 || change.Modified[0] != "CASE-002" {
		t.Fatalf("modified list not recorded: %#v", change)
	}

	// 全部拒绝性校验共用一个已 CLAIMED 派发（每次被拒的 RecordQADesign 不完成派发，
	// 可复用；复用一个派发可避免触发"被中断派发"的续用/询问守卫）。

	// --case-id 引用不存在的 id：拒绝且不改状态。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{CaseID: "CASE-999", Mode: "blackbox", Description: "x", Procedure: "y", Oracle: "z"}}, ""); err == nil || !strings.Contains(err.Error(), "references unknown id") {
		t.Fatalf("unknown --case-id accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected unknown --case-id changed state")
	}

	// --remove-case 引用不存在的 id：拒绝。
	if _, err := RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{RemoveCases: []string{"CASE-999"}}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown --remove-case id accepted: %v", err)
	}

	// --replace-all 与 --remove-case 互斥：拒绝。
	if _, err := RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{ReplaceAll: true, RemoveCases: []string{"CASE-002"}}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("--replace-all + --remove-case accepted: %v", err)
	}

	// 语义重复：无 id 重提既有 CASE-001 规格 → 拒绝并提示改用 --case-id 修改。
	if _, err := RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}, ""); err == nil || !strings.Contains(err.Error(), "duplicates existing case CASE-001") {
		t.Fatalf("semantic duplicate no-id submission accepted: %v", err)
	}

	// --remove-case 删除 CASE-003：其余保留（复用同一派发完成记录）。
	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{RemoveCases: []string{"CASE-003"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 3 {
		t.Fatalf("--remove-case did not delete: %#v", state.allQACases())
	}
	for _, testCase := range state.qaCases("") {
		if testCase.ID == "CASE-003" {
			t.Fatalf("removed case still present: %#v", state.allQACases())
		}
	}
	change = state.QADesignChangesByMode[""]
	if len(change.Removed) != 1 || change.Removed[0] != "CASE-003" {
		t.Fatalf("removed list not recorded: %#v", change)
	}

	// --replace-all 整体替换：换整套、id 从 CASE-001 重新分配。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "replacement structure", Procedure: "run the replacement test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxReplacement"},
		{Mode: "blackbox", Description: "replacement behavior", Procedure: "run the replacement CLI", Oracle: "observable success"},
	}, "", QADesignRecordOptions{ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 2 || state.qaCases("")[0].Description != "replacement structure" || state.qaCases("")[1].Description != "replacement behavior" || state.qaCases("")[0].ReviewStatus != "PENDING" {
		t.Fatalf("--replace-all did not replace the whole set: %#v", state.allQACases())
	}
	// 新 id 全局唯一（CASE-001 格式）：不与既有 id 撞号。
	if state.qaCases("")[0].ID == state.qaCases("")[1].ID {
		t.Fatalf("--replace-all produced duplicate ids: %#v", state.allQACases())
	}

	// --replace-all 空集清空该 mode（显式逃生门）。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 0 {
		t.Fatalf("--replace-all empty did not clear the mode: %#v", state.allQACases())
	}
}

// TestBlackboxDesignMirrorWritesIsolationWorktreeCaseFile verifies 改动 1 的隔离写入：
// 黑盒 qa-design 记录后 CLI 把 run-state 的黑盒用例 mirror 到隔离工作区的
// .gates/cases/blackbox.md（主干看不到）；后续补全轮重写 mirror。白盒/无工作区不写。
func TestBlackboxDesignMirrorWritesIsolationWorktreeCaseFile(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "mirror"), "custom", []string{blackboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	mirrorPath := filepath.Join(worktree, ".gates", "cases", "blackbox.md")

	// 黑盒设计轮：写 mirror。
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mirrorPath)
	if err != nil {
		t.Fatalf("mirror not written to isolation worktree: %v", err)
	}
	for _, want := range []string{"# Blackbox QA cases (run: " + state.RunID, "CASE-001", "CASE-002", "public workflow", "recovery workflow"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("mirror missing %q:\n%s", want, data)
		}
	}
	// 主干上不出现该文件（黑盒阶段隔离）。
	if _, err := os.Stat(filepath.Join(root, ".gates", "cases", "blackbox.md")); !os.IsNotExist(err) {
		t.Fatalf("mirror leaked to the main worktree: %v", err)
	}

	// 补全轮：只新增一个黑盒用例，mirror 重写并包含新增用例。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "edge behavior", Procedure: "run edge CLI", Oracle: "edge success"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(mirrorPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CASE-001", "CASE-002", "CASE-003", "edge behavior"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("mirror not rewritten after incremental design (missing %q):\n%s", want, data)
		}
	}
	// 主干仍无文件。
	if _, err := os.Stat(filepath.Join(root, ".gates", "cases", "blackbox.md")); !os.IsNotExist(err) {
		t.Fatalf("mirror leaked to the main worktree after supplement: %v", err)
	}
}

// TestSealMaterializesApprovedBlackboxCases verifies 改动 1 的合回时点：seal 时 CLI 把
// 已批准 blackbox 用例物化到主工作区 .gates/results/<run-id>.blackbox-cases.md（与 seal
// ledger 同目录），只含已批准黑盒用例；中止（abort）不物化。非分片 run 直接物化。
func TestSealMaterializesApprovedBlackboxCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "seal-materialize"), "custom", []string{blackboxQAID, whiteboxQAID, "quality"})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}

	// 黑盒设计 + review PASS（隔离工作区）。
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "seal-bb-reviewer", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 白盒设计 + review PASS（主 worktree）。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	review = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "seal-wb-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 开发 → 快照。
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-seal.txt"), "delivery\n")
	commitAll(t, root, "delivery seal")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}

	// QA 执行 PASS（按 mode 分流）+ 门 PASS → seal。
	for _, mode := range []string{"blackbox", "whitebox"} {
		exec := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", mode)
		state, err = RecordQAExecution(root, pkg, state.RunID, exec, passingExecution(state.qaModeCases(mode)), "")
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, gate := range []string{"quality"} {
		state = recordGateResult(t, root, pkg, state, gate, gate+"-seal-reviewer", "PASS", "", nil)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "sealed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("summary=%#v", summary)
	}

	// 物化文件：只含已批准黑盒用例，不含白盒用例。
	path := filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("materialized blackbox cases file missing: %v", err)
	}
	for _, want := range []string{state.RunID, "CASE-001", "CASE-002", "public workflow", "recovery workflow"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("materialized file missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "mode: whitebox") {
		t.Fatalf("materialized file leaked whitebox cases:\n%s", data)
	}
}

// TestAbortDoesNotMaterializeBlackboxCases verifies 改动 1 的合回时点边界：中止（abort）
// 不是 seal，不产出 .gates/results/<run-id>.blackbox-cases.md 物化文件。
func TestAbortDoesNotMaterializeBlackboxCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "abort-no-materialize"), "custom", []string{blackboxQAID})
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
	if _, err := Abort(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")); !os.IsNotExist(err) {
		t.Fatalf("abort produced a materialized blackbox cases file: %v", err)
	}
}

// TestBlackboxCasePathsAreVCSUnified verifies 改动 1 的三 VCS 统一：blackbox 用例 mirror
// 路径与 seal 物化路径是 worktree/root 的纯函数，与 run 的 VCS（git/svn/p4）无关——三种
// VCS 下推导出同一份路径，保证隔离 + seal 合回语义一致。
func TestBlackboxCasePathsAreVCSUnified(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "qa-worktree")
	root := t.TempDir()
	for _, vcs := range []string{"git", "svn", "p4"} {
		state := RunState{RunID: "unified-path", VCS: vcs, QAWorktree: worktree}
		wantMirror := filepath.Join(cleanWorktree(worktree), ".gates", "cases", "blackbox.md")
		if got := blackboxCaseMirrorPath(state.QAWorktree); got != wantMirror {
			t.Fatalf("%s mirror path=%q want=%q", vcs, got, wantMirror)
		}
		// 物化路径与 seal ledger（.gates/results/<run-id>.json）同目录；materializeBlackboxCases
		// 内部用 lifecycle.CleanRoot 解析 root，temp 绝对路径清洗后为恒等。
		wantMaterialized := filepath.Join(root, ".gates", "results", state.RunID+".blackbox-cases.md")
		if err := materializeBlackboxCases(root, RunState{RunID: state.RunID, VCS: vcs, QAWorktree: worktree, QACasesByMode: map[string][]QACase{"blackbox": {{ID: "CASE-001", Mode: "blackbox", ReviewStatus: "PASS"}}}}); err != nil {
			t.Fatalf("%s materialize: %v", vcs, err)
		}
		if _, err := os.Stat(wantMaterialized); err != nil {
			t.Fatalf("%s materialized file missing at %q: %v", vcs, wantMaterialized, err)
		}
	}
}

// TestReviewPromptInjectsChangeListAndMirrorPath verifies 改动 2 的审查全上下文：qa-review
// 提示词注入本轮新增/修改/删除的用例 id 列表；黑盒（隔离工作区已登记）提示词同时指向
// 隔离工作区的用例 mirror 文件路径。
func TestReviewPromptInjectsChangeListAndMirrorPath(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "review-context"), "custom", []string{blackboxQAID, whiteboxQAID})
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	// 黑盒设计轮记录 2 个用例（含本轮增量变更留痕）。
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"},
		{Mode: "blackbox", Description: "recovery workflow", Procedure: "trigger recovery", Oracle: "state restored"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", "blackbox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	for _, want := range []string{
		"changed: added CASE-001, CASE-002",
		filepath.Join(worktree, ".gates", "cases", "blackbox.md"),
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("blackbox review prompt omitted %q:\n%s", want, prompt)
		}
	}
	// 白盒设计轮后再准备白盒 review。
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 白盒 review 提示词注入白盒 mode 本轮变更列表（每 mode 独立）、但不指黑盒 mirror 文件。
	prompt, err = PrepareAction(root, pkg, state.RunID, "qa-review", "whitebox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "changed: added CASE-003") {
		t.Fatalf("whitebox review prompt omitted its mode's change list:\n%s", prompt)
	}
	if strings.Contains(prompt, "blackbox.md") {
		t.Fatalf("whitebox review prompt should not point at the blackbox mirror:\n%s", prompt)
	}
}
