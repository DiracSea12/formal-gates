package validate

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
)

// TestReproQACarryRegression reproduces the complexity-gate P1 finding: with QA
// selected, main-agent carry must rebind QAExecution.Snapshot to the current
// snapshot, not write a spurious Gates["qa"] entry.
func TestReproQACarryRegression(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "repro-qa-carry", "custom", []string{blackboxQAID, "architecture"})
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.QAExecution.Snapshot != state.CurrentSnapshot {
		t.Fatalf("QA execution snapshot=%s want=%s", state.QAExecution.Snapshot, state.CurrentSnapshot)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "repro-arch", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advanceRepair(t, root, pkg, state, "repro-repair")
	if got := eligibleMainCarryResults(state, false); !reflect.DeepEqual(got, []string{blackboxQAID}) {
		t.Fatalf("eligible carry results=%v want=[blackbox]", got)
	}
	state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "repair does not touch QA behavior")
	if err != nil {
		t.Fatal(err)
	}
	if state.QAExecution.Snapshot != state.CurrentSnapshot {
		t.Fatalf("QAExecution.Snapshot=%s want=%s (carry failed to rebind QA snapshot)", state.QAExecution.Snapshot, state.CurrentSnapshot)
	}
	if _, ok := state.Gates["qa"]; ok {
		t.Fatalf("spurious Gates[qa] entry written: %#v", state.Gates["qa"])
	}
}

// TestReproFAILResultPromptChangeReDispatches locks in R 修复清单 P1-1: a recorded
// FAIL gate/action result whose prompt changed must be directly re-dispatchable
// (re-dispatch is the disposal), not deadlocked behind carry --main-agent, which
// only inherits PASS results.
func TestReproFAILResultPromptChangeReDispatches(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "repro-fail-redispatch", "custom", []string{"architecture"})
	state = recordGateResult(t, root, pkg, state, "architecture", "repro-fail-arch", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	writeTestFile(t, filepath.Join(pkg, "gates", "architecture.md"), "new architecture checks\n")
	// FAIL 结果提示词变化 → 重派即处置，不允许卡死在待决继承。
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", false, ""); err != nil {
		t.Fatalf("FAIL gate with changed prompt was not re-dispatchable (P1-1 deadlock): %v", err)
	}
	// 同 snapshot 下继续 / 记录入口也不能被待决继承卡住。
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "architecture", "repro-fail-arch-2")
	if _, err := RecordGate(root, pkg, state.RunID, "architecture", dispatchID, "PASS", "", comparedRange(state), nil); err != nil {
		t.Fatalf("re-dispatch of the FAIL gate could not record (P1-1 deadlock): %v", err)
	}
}

// TestReproResumeThreeBranch covers RQ-007/013 at the gate prepare path: an
// objective transient API interruption reason forces a resume with the explicit
// cause in the message, a recorded non-objective reason opens a fresh dispatch,
// and a missing/unknown reason forces the user decision branch.
func TestReproResumeThreeBranch(t *testing.T) {
	t.Run("objective reason forces resume", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		state := readyDeliveryForRoute(t, root, pkg, "resume-objective", "custom", []string{"quality"})
		prior := workflowLifecycle
		workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, interruptionReason: "HTTP 429"}
		t.Cleanup(func() { workflowLifecycle = prior })
		prepareAndClaim(t, root, pkg, state.RunID, "quality", "interrupted-quality")
		if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "objective transient API cause") || !strings.Contains(err.Error(), "resume the original agent") {
			t.Fatalf("objective interruption reason did not force resume with the cause: %v", err)
		}
	})
	t.Run("non-objective reason opens fresh dispatch", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		state := readyDeliveryForRoute(t, root, pkg, "resume-user-abort", "custom", []string{"quality"})
		prior := workflowLifecycle
		workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, interruptionReason: "user abort"}
		t.Cleanup(func() { workflowLifecycle = prior })
		prepareAndClaim(t, root, pkg, state.RunID, "quality", "interrupted-quality")
		if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
			t.Fatalf("non-objective interruption reason was still forced to resume: %v", err)
		}
	})
	t.Run("unknown reason forces user decision", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		state := readyDeliveryForRoute(t, root, pkg, "resume-unknown", "custom", []string{"quality"})
		prior := workflowLifecycle
		workflowLifecycle = &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Verified}, interruptionReason: "未知"}
		t.Cleanup(func() { workflowLifecycle = prior })
		prepareAndClaim(t, root, pkg, state.RunID, "quality", "interrupted-quality")
		if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "the user must decide") {
			t.Fatalf("unknown interruption reason did not force the user decision branch: %v", err)
		}
	})
}

// TestReproDevelopmentWorkerUserRequested covers R 修复清单 P1-2: prepare
// development-worker must pass the CLI's --user-requested through to the shared
// prepareBoundPrompt so a user-authorized fresh development/repair dispatch can
// reopen an interrupted claimed dispatch.
func TestReproDevelopmentWorkerUserRequested(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "dev-user-requested"), "custom", []string{"quality"})
	// prepareDispatch 已为 development-worker 完成认领（reviewer-required）。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	// 无 --user-requested：中断的已认领开发派发必须被拦下强制续用。
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "resume the original agent") {
		t.Fatalf("interrupted development dispatch was not forced to resume: %v", err)
	}
	// --user-requested：用户显式授权重开，来源记入 ReviewOverrides，新派发生效。
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", true, "user reopened the development worker"); err != nil {
		t.Fatalf("user-authorized development re-dispatch was rejected (P1-2): %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewOverrides["development-worker"] == "" {
		t.Fatalf("development reopen was not recorded in ReviewOverrides: %#v", state.ReviewOverrides)
	}
	if state.Dispatches[dispatchID].Status != "STALE" {
		t.Fatalf("superseded development dispatch was not staled: %#v", state.Dispatches[dispatchID])
	}
}

// TestReproQAExecutionPerModeDispatch covers R 修复清单 item 3: qa-execution filters
// the required set by the dispatch mode so blackbox and whitebox each dispatch
// independently and can record in parallel into the shared QAExecution result.
func TestReproQAExecutionPerModeDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "qa-per-mode")
	// 黑盒与白盒各准备一个独立派发：提示词各自只含对应 mode 的需执行集。
	blackboxPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blackboxPrompt, "mode: whitebox") || !strings.Contains(blackboxPrompt, "mode: blackbox") {
		t.Fatalf("blackbox dispatch mixed modes: %s", blackboxPrompt)
	}
	whiteboxPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(whiteboxPrompt, "mode: blackbox") || !strings.Contains(whiteboxPrompt, "mode: whitebox") {
		t.Fatalf("whitebox dispatch mixed modes: %s", whiteboxPrompt)
	}
	// 各自独立记录：先黑盒、后白盒，两个 mode 的用例都累计进共享结果。
	state, _ = LoadRunState(root, state.RunID)
	blackboxDispatch := qaExecutionDispatchByMode(t, root, state.RunID, "blackbox")
	if _, err := ClaimDispatch(root, pkg, state.RunID, blackboxDispatch, "blackbox-executor"); err != nil {
		t.Fatal(err)
	}
	blackboxCases := filterQACasesByMode(state.QACases, "blackbox")
	if _, err := RecordQAExecution(root, pkg, state.RunID, blackboxDispatch, passingExecution(blackboxCases), ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	whiteboxDispatch := qaExecutionDispatchByMode(t, root, state.RunID, "whitebox")
	if _, err := ClaimDispatch(root, pkg, state.RunID, whiteboxDispatch, "whitebox-executor"); err != nil {
		t.Fatal(err)
	}
	whiteboxCases := filterQACasesByMode(state.QACases, "whitebox")
	if _, err := RecordQAExecution(root, pkg, state.RunID, whiteboxDispatch, passingExecution(whiteboxCases), ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.QAExecution.Status != "PASS" || len(state.QAExecution.Cases) != len(state.QACases) {
		t.Fatalf("parallel per-mode execution did not accumulate: %#v", state.QAExecution)
	}
}

// qaExecutionDispatchByMode returns the OPEN qa-execution dispatch id with the
// given mode (R 修复清单 item 3 per-mode dispatch).
func qaExecutionDispatchByMode(t *testing.T, root, runID, mode string) string {
	t.Helper()
	state, err := LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == "action" && dispatch.Target == "qa-execution" && dispatch.Mode == mode && dispatch.Status == "OPEN" {
			return id
		}
	}
	t.Fatalf("no OPEN qa-execution dispatch for mode %q", mode)
	return ""
}

// TestReproWaveAndSealRequireEverySelectedMode covers R 修复清单 item 8: after
// qa-execution is split by mode (item 3), wave completion and Seal must require
// every selected mode to have recorded execution. A single mode recording PASS
// with all gates recorded must not complete the wave nor allow Seal while the
// other selected mode is silently skipped.
func TestReproWaveAndSealRequireEverySelectedMode(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "wave-per-mode")
	// 只记录黑盒 mode 的需执行集：合并结果呈 PASS，但白盒 mode 从未派发记录。
	blackboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, _ = LoadRunState(root, state.RunID)
	blackboxCases := filterQACasesByMode(state.QACases, "blackbox")
	state, err := RecordQAExecution(root, pkg, state.RunID, blackboxDispatch, passingExecution(blackboxCases), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.QAExecution.Status != "PASS" || len(state.QAExecution.Cases) != len(blackboxCases) {
		t.Fatalf("blackbox-only execution not recorded: %#v", state.QAExecution)
	}
	// 全部门记录 PASS 后波次仍不得完成：另一 mode 未记录执行。
	for _, gate := range []string{"architecture", "quality"} {
		state = recordGateResult(t, root, pkg, state, gate, "wave-per-mode-"+gate, "PASS", "", nil)
	}
	if state.CompletedReviewWaves != 0 {
		t.Fatalf("wave completed with only one QA mode recorded: %#v", state)
	}
	if _, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery"); err == nil || !strings.Contains(err.Error(), "QA mode") {
		t.Fatalf("Seal allowed with a silently skipped QA mode: %v", err)
	}
	// 白盒 mode 独立派发并记录后，两个 mode 均已记录执行，波次完成、seal 放行。
	whiteboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "whitebox")
	state, _ = LoadRunState(root, state.RunID)
	whiteboxCases := filterQACasesByMode(state.QACases, "whitebox")
	state, err = RecordQAExecution(root, pkg, state.RunID, whiteboxDispatch, passingExecution(whiteboxCases), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("wave did not complete after both modes recorded: %#v", state)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
	if err != nil {
		t.Fatalf("Seal blocked after both QA modes recorded: %v", err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("summary=%#v", summary)
	}
}

// TestReproINHERITKeepsAuthoritativeGuard locks in R 修复清单 item 9's corrected
// oracle for the carry cases: INHERIT (keep the old conclusion) makes the result
// authoritative at the current snapshot, so a fresh prepare of the same target
// (unchanged prompt) is still blocked by the authoritative-result guard — the
// old oracle that expected prepare to pass was wrong.
func TestReproINHERITKeepsAuthoritativeGuard(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "inherit-guard", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "inherit-arch-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "inherit-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repair required"}})
	state = advanceRepair(t, root, pkg, state, "inherit")
	// 独立 Carry INHERIT：保留旧结论，结果在当前快照仍权威。
	carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err := RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair does not touch architecture"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if result := state.Gates["architecture"]; result.Status != "PASS" || result.Snapshot != state.CurrentSnapshot {
		t.Fatalf("INHERIT did not keep the result authoritative: %#v", result)
	}
	// 提示词未变：既有权威结果守卫正确地阻止重跑（INHERIT → 不重跑、权威守卫拦下）。
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", false, ""); err == nil || !strings.Contains(err.Error(), "authoritative PASS") {
		t.Fatalf("INHERIT should keep the authoritative result guard blocking re-run: %v", err)
	}
}

// TestReproRERUNResetsForReDispatch locks in R 修复清单 item 9's corrected oracle
// for RERUN: the decision resets the recorded result to PENDING, so the same
// target becomes re-dispatchable (RERUN → 重置、可重派).
func TestReproRERUNResetsForReDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "rerun-reset", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "rerun-arch-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "rerun-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repair required"}})
	state = advanceRepair(t, root, pkg, state, "rerun")
	carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err := RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "architecture", Decision: "RERUN", Message: "architecture needs a fresh review"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["architecture"].Status != "PENDING" {
		t.Fatalf("RERUN did not reset the result to PENDING: %#v", state.Gates["architecture"])
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", false, ""); err != nil {
		t.Fatalf("RERUN should make the target re-dispatchable: %v", err)
	}
}
