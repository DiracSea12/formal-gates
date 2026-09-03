package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// perModeReadyDelivery builds a run with blackbox and whitebox QA fully decoupled
// each mode is designed and reviewed PASS through its own per-mode
// dispatch, then a development snapshot is taken. The route selects blackbox,
// whitebox, and the quality gate. Case IDs: blackbox CASE-001, whitebox CASE-002.
func perModeReadyDelivery(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, id), "custom", []string{blackboxQAID, whiteboxQAID, "quality"})
	// 黑盒设计/审查需注册 QA 隔离工作区。
	worktree := createQAWorktree(t, root, state)
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	// 黑盒设计 + 审查 PASS。
	bbDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, bbDesign, []QACaseInput{{Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	bbReview := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-bb-reviewer", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, bbReview, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 白盒设计 + 审查 PASS。
	wbDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, wbDesign, []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	wbReview := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-wb-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, wbReview, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 开发 + 快照。
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-"+id+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+id)
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// TestLegacyQAModeAndStorageAreRejected verifies that old merged QA state is
// not migrated or resumed: both the legacy qa mode and the single-list result
// fields are rejected as unsupported run state.
func TestLegacyQAModeAndStorageAreRejected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "legacy-qa-rejected")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["selectedGates"] = []string{"qa"}
	decoded["qaCases"] = []any{}
	decoded["qaExecution"] = map[string]any{"status": "PASS"}
	decoded["priorQAExecution"] = map[string]any{"status": "FAIL"}
	delete(decoded, "stateIntegrity")
	data, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state.RunID); err == nil || !strings.Contains(err.Error(), "unsupported legacy run state") {
		t.Fatalf("legacy merged QA state was not rejected: %v", err)
	}
}

// TestLegacyQAModeWithCurrentStorageIsRejected verifies that even a state using
// the current per-mode storage schema cannot resume the removed merged "qa"
// selection.
func TestLegacyQAModeWithCurrentStorageIsRejected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "legacy-qa-mode-rejected")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	decoded["selectedGates"] = []string{"qa"}
	delete(decoded, "stateIntegrity")
	data, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state.RunID); err == nil || !strings.Contains(err.Error(), "selected QA mode") {
		t.Fatalf("legacy qa mode with current storage was not rejected: %v", err)
	}
}

// TestWhiteboxDesignDoesNotClearBlackboxExecution covers a whitebox
// qa-design round must reset only its own mode's execution result, never the
// blackbox result already recorded (workflow.go RecordQADesign defect).
func TestWhiteboxDesignDoesNotClearBlackboxExecution(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "wb-design-no-clear")
	blackboxCases := state.qaModeCases("blackbox")
	state = recordModeQA(t, root, pkg, state, "blackbox", passingExecution(blackboxCases))
	if state.qaExecution("blackbox").Status != "PASS" || len(state.qaExecution("blackbox").Cases) != len(blackboxCases) {
		t.Fatalf("blackbox execution not recorded: %#v", state.qaExecution("blackbox"))
	}
	// 白盒设计轮（改写白盒用例）：不得清除黑盒执行结果。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	var err error
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure revised", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructureRevised"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("blackbox").Status != "PASS" || len(state.qaExecution("blackbox").Cases) != len(blackboxCases) {
		t.Fatalf("whitebox design cleared the blackbox execution result: %#v", state.qaExecution("blackbox"))
	}
	if status := state.qaExecution("whitebox").Status; status != "PENDING" && status != "" {
		t.Fatalf("whitebox design did not reset its own execution result: %#v", state.qaExecution("whitebox"))
	}
}

// TestPerModePriorSurvivesOtherModeRecord covers recording one mode's new
// authoritative result retains both that mode's bounded prior audit and the other
// mode's prior. The other mode's rerun recognition stays intact.
func TestPerModePriorSurvivesOtherModeRecord(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "per-mode-prior")
	var err error
	bbCases := state.qaModeCases("blackbox")
	wbCases := state.qaModeCases("whitebox")
	// Wave 1：黑盒 FAIL、白盒 FAIL、质量 FAIL → 全部记录，波次完成。
	state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: bbCases[0].ID, Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: wbCases[0].ID, Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordGateResult(t, root, pkg, state, "quality", "pm-prior-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("wave 1 count=%d", state.CompletedReviewWaves)
	}
	// 修复快照推进：两个 mode 的上一轮权威结果各自保留到 PriorQAExecutionByMode。
	state = advanceRepair(t, root, pkg, state, "pm-prior-repair")
	if state.priorQAExecution("blackbox") == nil || state.priorQAExecution("whitebox") == nil {
		t.Fatalf("per-mode priors were not preserved: %#v", state.PriorQAExecutionByMode)
	}
	blackboxPriorSnapshot := state.priorQAExecution("blackbox").Snapshot
	// 黑盒 FULL scope 重跑 PASS：当前结果被替换，黑盒与白盒 prior 审计都保留。
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordModeQA(t, root, pkg, state, "blackbox", passingExecution(bbCases))
	if prior := state.priorQAExecution("blackbox"); prior == nil || prior.Status != "FAIL" || prior.Snapshot != blackboxPriorSnapshot {
		t.Fatalf("blackbox rerun lost its own prior audit: %#v", prior)
	}
	if state.priorQAExecution("whitebox") == nil {
		t.Fatalf("blackbox rerun cleared the whitebox prior: %#v", state.PriorQAExecutionByMode)
	}
	// 白盒重跑仍是重跑（白盒 prior 未丢）：prepare 必须强制 scope 决策。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, ""); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("whitebox rerun without a scope decision after the blackbox rerun: %v", err)
	}
}

// TestBlackboxReviewInFlightDoesNotLockWhiteboxDesign covers the QA design
// lock is per mode — a blackbox qa-review dispatch in flight locks the blackbox
// design but must not lock the whitebox design (workflow.go qaReviewDispatchPrepared
// defect).
func TestBlackboxReviewInFlightDoesNotLockWhiteboxDesign(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "review-lock"), "custom", []string{blackboxQAID, whiteboxQAID, "quality"})
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
	// 黑盒 review 在飞（已认领、未记录）。
	bbReview := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "review-lock-bb", "blackbox")
	// 正面控制：黑盒 review 在飞锁黑盒 qa-design。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "blackbox", false, ""); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("blackbox design was not locked by the in-flight blackbox review: %v", err)
	}
	// 黑盒 review 在飞不锁白盒 qa-design。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "whitebox", false, ""); err != nil {
		t.Fatalf("in-flight blackbox review locked the whitebox design: %v", err)
	}
	// 完成黑盒 review 以保持状态一致。
	state, err = RecordQAReview(root, pkg, state.RunID, bbReview, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
}

// TestPerModeIndependentBlockerAndSealParsing covers blocker detection and
// Seal resolution treat each QA mode's result independently — a blackbox FAIL blocks
// (and blocks Seal) while the whitebox PASS result is independently preserved.
func TestPerModeIndependentBlockerAndSealParsing(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "per-mode-seal")
	bbCases := state.qaModeCases("blackbox")
	wbCases := state.qaModeCases("whitebox")
	// 黑盒 FAIL、白盒 PASS、质量 PASS：波次完成但存在黑盒阻塞项。
	state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: bbCases[0].ID, Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordModeQA(t, root, pkg, state, "whitebox", passingExecution(wbCases))
	state = recordGateResult(t, root, pkg, state, "quality", "pm-seal-quality", "PASS", "", nil)
	if state.qaExecution("blackbox").Status != "FAIL" || state.qaExecution("whitebox").Status != "PASS" {
		t.Fatalf("per-mode results not independent: blackbox=%#v whitebox=%#v", state.qaExecution("blackbox"), state.qaExecution("whitebox"))
	}
	if !hasRepairableBlocker(state) {
		t.Fatalf("blackbox FAIL did not block while whitebox PASS: %#v", state.qaExecution("blackbox"))
	}
	if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err == nil {
		t.Fatalf("Seal allowed with a blackbox FAIL result")
	}
	// 白盒 PASS 独立存续：黑盒 FAIL 记录不清白盒结果，返修输入仍含黑盒失败原因。
	if !strings.Contains(repairInput(state), "QA FAIL") {
		t.Fatalf("repair input lost the blackbox FAIL finding: %s", repairInput(state))
	}
}

// TestDualModeReviewDesignFullyDecoupled covers blackbox qa-review FAIL
// resets only the blackbox qa-design to PENDING and never touches the whitebox
// design/review; the whitebox review can still be prepared and recorded
// independently (each mode's review/design authoritative results are stored and
// judged per mode).
func TestDualModeReviewDesignFullyDecoupled(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "dual-mode-decoupled"), "custom", []string{blackboxQAID, whiteboxQAID, "quality"})
	worktree := createQAWorktree(t, root, state)
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	// 黑盒设计 PASS、白盒设计 PASS。
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
		t.Fatalf("per-mode designs not recorded: blackbox=%#v whitebox=%#v", state.qaDesign("blackbox"), state.qaDesign("whitebox"))
	}
	// 黑盒 review FAIL：只重置黑盒设计为 PENDING，白盒设计/审查判定不受影响。
	bbReview := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "bb-decoupled-reviewer", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, bbReview, []QAReviewInput{{CaseID: state.qaModeCases("blackbox")[0].ID, Outcome: "FAIL", Reason: "coverage gap"}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("blackbox").Status != "FAIL" || state.qaDesign("blackbox").Status != "PENDING" {
		t.Fatalf("blackbox FAIL did not reset only the blackbox design: review=%#v design=%#v", state.qaReview("blackbox"), state.qaDesign("blackbox"))
	}
	if state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("blackbox FAIL reset the whitebox design: %#v", state.qaDesign("whitebox"))
	}
	if state.qaReview("whitebox").Status != "" && state.qaReview("whitebox").Status != "PENDING" {
		t.Fatalf("blackbox FAIL touched the whitebox review: %#v", state.qaReview("whitebox"))
	}
	// 白盒 review 仍可独立准备并记录 PASS（任一 mode 的 prepare-action
	// qa-review 只受本 mode 的 review/design 状态约束）。
	wbReview := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "wb-decoupled-reviewer", "whitebox")
	wbDecisions := make([]QAReviewInput, 0, 1)
	for _, testCase := range state.qaModeCases("whitebox") {
		wbDecisions = append(wbDecisions, QAReviewInput{CaseID: testCase.ID, Outcome: "PASS"})
	}
	state, err = RecordQAReview(root, pkg, state.RunID, wbReview, wbDecisions, "", nil)
	if err != nil {
		t.Fatalf("whitebox review blocked after blackbox FAIL (RQ-001): %v", err)
	}
	if state.qaReview("whitebox").Status != "PASS" || state.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("whitebox review did not pass independently: review=%#v design=%#v", state.qaReview("whitebox"), state.qaDesign("whitebox"))
	}
}

// TestPerModeCarryInheritsPreRepairPASS covers after a repair snapshot,
// main-agent carry inherits every selected QA mode whose pre-repair execution
// PASS is preserved (direct per-mode read, not current-snapshot-gated), rebinding
// each mode's result snapshot to the current snapshot.
func TestPerModeCarryInheritsPreRepairPASS(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "per-mode-carry")
	bbCases := state.qaModeCases("blackbox")
	wbCases := state.qaModeCases("whitebox")
	// 两个 mode 均 PASS 执行 + 一个门 FAIL → 波次完成 → 修复快照。
	state = recordModeQA(t, root, pkg, state, "blackbox", passingExecution(bbCases))
	state = recordModeQA(t, root, pkg, state, "whitebox", passingExecution(wbCases))
	state = recordGateResult(t, root, pkg, state, "quality", "pm-carry-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("wave 1 count=%d", state.CompletedReviewWaves)
	}
	state = advanceRepair(t, root, pkg, state, "pm-carry-repair")
	if state.PreRepairSnapshot == "" {
		t.Fatalf("repair did not set the pre-repair snapshot")
	}
	// 修复快照推进后，两个 mode 的 PASS 都保留在各自 mode 键（Snapshot == PreRepairSnapshot），
	// 均可被 main-agent Carry 继承（直取该 mode，不要求 current snapshot）。
	if got := eligibleMainCarryResults(state, false); !reflect.DeepEqual(got, []string{blackboxQAID, whiteboxQAID}) {
		t.Fatalf("eligible carry results=%v want=[blackbox whitebox]", got)
	}
	state, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "repair does not touch QA behavior")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("blackbox").Status != "PASS" || state.qaExecution("whitebox").Status != "PASS" {
		t.Fatalf("carry lost per-mode PASS status: blackbox=%#v whitebox=%#v", state.qaExecution("blackbox"), state.qaExecution("whitebox"))
	}
	if state.qaExecution("blackbox").Snapshot != state.CurrentSnapshot || state.qaExecution("whitebox").Snapshot != state.CurrentSnapshot {
		t.Fatalf("carry did not rebind per-mode QA snapshots: blackbox=%#v whitebox=%#v", state.qaExecution("blackbox"), state.qaExecution("whitebox"))
	}
}

// TestExplicitPerModeRerunScopePreventsCarryInheritance covers the ownership
// boundary between an explicit QA rerun decision and Carry. Once FULL is bound
// to the old PASS snapshot, Carry cannot turn that PASS into a current result;
// qa-execution remains dispatchable, replaces the mode's current result, and
// retains the prior result as audit.
func TestExplicitPerModeRerunScopePreventsCarryInheritance(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "scoped-rerun-carry")
	bbCases := state.qaModeCases("blackbox")
	state = recordModeQA(t, root, pkg, state, "blackbox", passingExecution(bbCases))
	state = recordGateResult(t, root, pkg, state, "quality", "scoped-rerun-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advanceRepair(t, root, pkg, state, "scoped-rerun")
	priorSnapshot := state.PreRepairSnapshot

	var err error
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "rerun the complete blackbox set")
	if err != nil {
		t.Fatal(err)
	}
	if got := eligibleMainCarryResults(state, false); len(got) != 0 {
		t.Fatalf("explicit FULL rerun remained eligible for Carry: %v", got)
	}
	if _, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "repair does not touch QA behavior"); err == nil || !strings.Contains(err.Error(), "no prior passing selected results") {
		t.Fatalf("Carry accepted a mode with an explicit rerun scope: %v", err)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result := state.qaExecution("blackbox"); result.Snapshot != priorSnapshot {
		t.Fatalf("rejected Carry rebound the old PASS: %#v", result)
	}

	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, "")
	if err != nil {
		t.Fatalf("explicit FULL rerun could not prepare qa-execution: %v", err)
	}
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("qa-execution rerun prompt is empty")
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := openDispatchID(state, "action", "qa-execution")
	if _, err := ClaimDispatch(root, pkg, state.RunID, dispatch, "scoped-rerun-executor"); err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(bbCases), "")
	if err != nil {
		t.Fatal(err)
	}
	if result := state.qaExecution("blackbox"); result.Status != "PASS" || result.Snapshot != state.CurrentSnapshot {
		t.Fatalf("new execution did not replace the current mode result: %#v", result)
	}
	if prior := state.priorQAExecution("blackbox"); prior == nil || prior.Status != "PASS" || prior.Snapshot != priorSnapshot {
		t.Fatalf("new execution lost the prior audit: %#v", prior)
	}
}

func TestQAExecutionRuntimeErrorCanRetrySameMode(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := perModeReadyDelivery(t, root, pkg, "runtime-retry")
	bbCases := state.qaModeCases("blackbox")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, nil, "temporary execution host outage")
	if err != nil {
		t.Fatal(err)
	}
	if qaExecutionModeResulted(state, "blackbox") {
		t.Fatalf("RUNTIME_ERROR became an authoritative result: %#v", state.qaExecution("blackbox"))
	}

	retry := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, retry, passingExecution(bbCases), "")
	if err != nil {
		t.Fatalf("same-mode retry after RUNTIME_ERROR failed: %v", err)
	}
	if result := state.qaExecution("blackbox"); result.Status != "PASS" || result.Snapshot != state.CurrentSnapshot {
		t.Fatalf("retry did not replace RUNTIME_ERROR with PASS: %#v", result)
	}
}

// TestLegacyStateWithoutPerModeReviewDesignFieldsErrors covers a run
// state file that lacks the per-mode qaReviewByMode / qaDesignByMode fields (the
// old format, or any schema mismatch) fails to load with a clear format error
// instead of silently degrading to empty/default state.
func TestLegacyStateWithoutPerModeReviewDesignFieldsErrors(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "old-format-error")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	delete(decoded, "qaReviewByMode")
	delete(decoded, "qaDesignByMode")
	rewritten, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state.RunID), append(rewritten, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state.RunID); err == nil || !strings.Contains(err.Error(), "does not match the current schema") {
		t.Fatalf("legacy state without per-mode review/design fields did not error clearly: %v", err)
	}
	// 顺带验证严格解码拒绝未知字段（任何 schema 不符均报错）。
	state2 := mustStart(t, root, pkg, "unknown-field-error")
	var decoded2 map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state2.RunID)), &decoded2); err != nil {
		t.Fatal(err)
	}
	decoded2["someUnknownField"] = "x"
	rewritten2, err := json.Marshal(decoded2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state2.RunID), append(rewritten2, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRunState(root, state2.RunID); err == nil || !strings.Contains(err.Error(), "does not match the current schema") {
		t.Fatalf("state with an unknown field did not error clearly: %v", err)
	}
}
