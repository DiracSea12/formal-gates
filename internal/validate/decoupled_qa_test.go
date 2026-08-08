package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// perModeReadyDelivery builds a run with blackbox and whitebox QA fully decoupled
// (RQ-012): each mode is designed and reviewed PASS through its own per-mode
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
	state, err = RecordQADesign(root, pkg, state.RunID, wbDesign, []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "go test ./...", Oracle: "pass"}}, "")
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

// TestLegacyQAExecutionAndPriorMigrateToMergedKey verifies the RQ-012 migration: an
// old state.json with the single qaExecution / priorQAExecution fields loads into
// the per-mode storage under the merged "" key, so legacy runs keep their recorded
// execution results and rerun-recognition base.
func TestLegacyQAExecutionAndPriorMigrateToMergedKey(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "legacy-migrate")
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	prior := QAExecutionResult{Status: "FAIL", Snapshot: "s-old", Cases: []QAResultRecord{{CaseID: "CASE-002", Mode: "blackbox", Outcome: "FAIL"}}}
	legacy := struct {
		RunState
		LegacyQAExecution      QAExecutionResult  `json:"qaExecution"`
		LegacyPriorQAExecution *QAExecutionResult `json:"priorQAExecution"`
	}{
		RunState:               state,
		LegacyQAExecution:      state.qaExecution(""),
		LegacyPriorQAExecution: &prior,
	}
	legacy.QAExecutionByMode = nil
	legacy.PriorQAExecutionByMode = nil
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.qaExecution("").Snapshot != state.CurrentSnapshot || len(loaded.qaExecution("").Cases) == 0 {
		t.Fatalf("legacy QAExecution did not migrate to the merged key: %#v", loaded.qaExecution(""))
	}
	if loaded.priorQAExecution("") == nil || loaded.priorQAExecution("").Status != "FAIL" || loaded.priorQAExecution("").Snapshot != "s-old" {
		t.Fatalf("legacy PriorQAExecution did not migrate to the merged key: %#v", loaded.PriorQAExecutionByMode)
	}
}

// TestWhiteboxDesignDoesNotClearBlackboxExecution covers RQ-012: a whitebox
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
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure revised", Procedure: "go test ./...", Oracle: "pass"}}, "")
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

// TestPerModePriorSurvivesOtherModeRecord covers RQ-012: recording one mode's new
// authoritative result must clear only that mode's prior authoritative result, never
// the other mode's (workflow.go RecordQAExecution defect) — the other mode's rerun
// recognition stays intact.
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
	// 黑盒 FULL scope 重跑 PASS：只清黑盒自己的 prior，白盒 prior 保留。
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordModeQA(t, root, pkg, state, "blackbox", passingExecution(bbCases))
	if state.priorQAExecution("blackbox") != nil {
		t.Fatalf("blackbox rerun did not clear its own prior: %#v", state.PriorQAExecutionByMode["blackbox"])
	}
	if state.priorQAExecution("whitebox") == nil {
		t.Fatalf("blackbox rerun cleared the whitebox prior: %#v", state.PriorQAExecutionByMode)
	}
	// 白盒重跑仍是重跑（白盒 prior 未丢）：prepare 必须强制 scope 决策。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, ""); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("whitebox rerun without a scope decision after the blackbox rerun: %v", err)
	}
}

// TestBlackboxReviewInFlightDoesNotLockWhiteboxDesign covers RQ-012: the QA design
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
	// RQ-012：黑盒 review 在飞不锁白盒 qa-design。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "whitebox", false, ""); err != nil {
		t.Fatalf("in-flight blackbox review locked the whitebox design: %v", err)
	}
	// 完成黑盒 review 以保持状态一致。
	state, err = RecordQAReview(root, pkg, state.RunID, bbReview, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
}

// TestPerModeIndependentBlockerAndSealParsing covers RQ-012: blocker detection and
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
