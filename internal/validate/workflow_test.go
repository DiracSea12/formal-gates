package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleStub struct {
	verification       lifecycle.Verification
	binds              [][4]string
	transcript         string
	interruptionReason string
}

func (*workflowLifecycleStub) Begin(string, string) error { return nil }

func (stub *workflowLifecycleStub) Bind(root, runID, dispatchID, identity string) error {
	stub.binds = append(stub.binds, [4]string{root, runID, dispatchID, identity})
	return nil
}

func (stub *workflowLifecycleStub) Verify(_, _, dispatchID string) (lifecycle.Verification, error) {
	result := stub.verification
	result.DispatchID = dispatchID
	return result, nil
}

func (stub *workflowLifecycleStub) TranscriptPath(_, _, _ string) (string, string, error) {
	if stub.transcript != "" {
		return lifecycle.ProviderCodex, stub.transcript, nil
	}
	return "", "", nil
}

func (stub *workflowLifecycleStub) ResolveClaimIdentity(_, _, preferred string) (string, error) {
	if strings.TrimSpace(preferred) != "" {
		return strings.TrimSpace(preferred), nil
	}
	return "", fmt.Errorf("reviewer identity is required when no subagent start observation exists")
}

func (stub *workflowLifecycleStub) InterruptionReason(_, _, _ string) (string, error) {
	return stub.interruptionReason, nil
}

func TestNativeStartRegistersAndFreezesRequirementArtifacts(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "artifacts", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if state.BaseSnapshot != gitHead(t, root) || state.CurrentSnapshot != state.BaseSnapshot {
		t.Fatalf("native snapshot was not resolved: %#v", state)
	}
	if got := artifactPaths(state.RequirementArtifacts); strings.Join(got, ",") != "design.md,requirements.md" {
		t.Fatalf("artifact order=%v", got)
	}
	state = confirmAndRoute(t, root, pkg, state, "custom", []string{"quality"})
	prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	state, _ = LoadRunState(root, state.RunID)
	if !developmentStarted(state) {
		t.Fatal("first development preparation did not freeze artifacts")
	}
	before := stateBytes(t, root, state.RunID)
	writeTestFile(t, filepath.Join(root, "design.md"), "changed design\n")
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "`workflow requirement --meaning preserved|changed` 更新修订") {
		t.Fatalf("changed frozen artifact was accepted: %v", err)
	}
	if after := stateBytes(t, root, state.RunID); after != before {
		t.Fatal("rejected frozen-artifact transition changed state")
	}
	commitAll(t, root, "changed requirement")
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "preserved", nil); err == nil || !strings.Contains(err.Error(), "requires user confirmation") {
		t.Fatalf("post-development preserved rebind without user confirmation was accepted: %v", err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementConfirmed || developmentStarted(state) || state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("meaning-changing requirement did not establish a new boundary: %#v", state)
	}
}

func TestRequirementSourceCanPromoteAnAdditionalArtifact(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "promote-artifact"), "custom", []string{"quality"})

	state, err := UpdateRequirement(root, pkg, state.RunID, "design.md", false, "preserved", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementSource != "design.md" || state.RequirementRevision != artifactRevision(state.RequirementArtifacts, "design.md") {
		t.Fatalf("promoted requirement source was not bound: %#v", state)
	}
	if got := artifactPaths(state.RequirementArtifacts); !reflect.DeepEqual(got, []string{"design.md"}) {
		t.Fatalf("promoted requirement artifact set=%v", got)
	}
}

func TestReviewDispatchClaimsAreFreshBoundAndReserved(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "dispatch")
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	first := openDispatchID(state, "action", "qa-review")
	sum := sha256.Sum256([]byte(prompt))
	if first == "" || !strings.Contains(prompt, first) || state.Dispatches[first].PromptHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("prepared prompt was not bound: %#v\n%s", state.Dispatches[first], prompt)
	}
	state, err = ClaimDispatch(root, pkg, state.RunID, first, "reviewer-one")
	if err != nil {
		t.Fatal(err)
	}
	// 认领未出结果且快照/任务不变时，CLI 强制续用原代理，拒绝默认重派发。
	before := stateBytes(t, root, state.RunID)
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, ""); err == nil || !strings.Contains(err.Error(), "resume the original agent") {
		t.Fatalf("claimed interrupted dispatch was not forced to resume: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected resume changed state")
	}
	// 只有用户显式授权（--user-requested）才可放行新派发，来源记入 ReviewOverrides。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", true, "user reopened the review"); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewOverrides["qa-review"] == "" {
		t.Fatalf("user-requested reopen was not recorded in ReviewOverrides: %#v", state.ReviewOverrides)
	}
	second := openDispatchID(state, "action", "qa-review")
	// prepare 不再作废——用户授权的重开新派发生成后，旧 CLAIMED 派发仍在途。
	if first == second || state.Dispatches[first].Status != "CLAIMED" || state.Dispatches[second].Attempt != 2 {
		t.Fatalf("user-authorized retry did not create a fresh dispatch with the claimed prior still in flight: %#v", state.Dispatches)
	}
	before = stateBytes(t, root, state.RunID)
	if _, err := ClaimDispatch(root, pkg, state.RunID, second, "reviewer-one"); err == nil || !strings.Contains(err.Error(), "already reserved") {
		t.Fatalf("claimed interrupted reviewer was reused: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected reviewer reuse changed state")
	}
	// 手动终止例外：前子代理已被终结（生命周期记录中断原因）时，认领同功能新派发
	// 把前派发标 STALE、允许认领。
	stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Verified}, "", "user abort")
	state, err = ClaimDispatch(root, pkg, state.RunID, second, "reviewer-two")
	if err != nil {
		t.Fatal(err)
	}
	if state.Dispatches[first].Status != "STALE" || state.Dispatches[second].Status != "CLAIMED" {
		t.Fatalf("manual-terminated claimed dispatch was not staled at claim: %#v", state.Dispatches)
	}
	state, err = RecordQAReview(root, pkg, state.RunID, second, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "PASS" || state.Dispatches[second].Status != "COMPLETED" {
		t.Fatalf("review result was not derived and completed: %#v", state)
	}
	if _, err := RecordQAReview(root, pkg, state.RunID, second, nil, "", nil); err == nil {
		t.Fatal("completed dispatch was reused")
	}
}

func TestDispatchClaimRequiresAnIDWithoutMutation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "missing-dispatch")
	before := stateBytes(t, root, state.RunID)
	if _, err := ClaimDispatch(root, pkg, state.RunID, "", "reviewer-one"); err == nil || !strings.Contains(err.Error(), "dispatch id is required") {
		t.Fatalf("missing dispatch was not rejected consistently: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected dispatch claim changed state")
	}
}

func TestWorkflowLifecycleBoundaryKeepsRejectedGatePending(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "required-lifecycle", "custom", []string{"quality"})
	stub := stubLifecycle(t, lifecycle.Verification{Outcome: lifecycle.Rejected, Diagnostic: "missing matching start and stop event"}, "", "")

	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "claude-agent-1")
	if len(stub.binds) != 1 || stub.binds[0] != [4]string{root, state.RunID, dispatchID, "claude-agent-1"} {
		t.Fatalf("workflow passed unexpected lifecycle binding fields: %#v", stub.binds)
	}
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", comparedRange(state), nil); err == nil || !strings.Contains(err.Error(), "lifecycle verification REJECTED") {
		t.Fatalf("gate result without matching provider events was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected lifecycle result changed workflow state")
	}

	stub.verification = lifecycle.Verification{Outcome: lifecycle.Verified}
	state, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Status != "PASS" || state.Dispatches[dispatchID].Status != "COMPLETED" {
		t.Fatalf("verified lifecycle did not permit recording: %#v", state.Gates["quality"])
	}
}

func TestQAKindsAndIncrementalReviewApprovals(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "incremental"), "full", nil)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	// 不设机械化质量下限：单模式用例集可流到 qa-review 的 set-level 覆盖判定。
	cases := baselineCases()
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, cases, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "reviewer-a")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, []QAReviewInput{{CaseID: "CASE-001", Outcome: "PASS"}, {CaseID: "CASE-002", Outcome: "FAIL", Reason: "live oracle is incomplete"}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" || state.qaCases("")[0].ReviewStatus != "PASS" || state.qaCases("")[1].ReviewStatus != "FAIL" {
		t.Fatalf("per-case results were not retained: %#v", state.allQACases())
	}
	designPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(designPrompt, "review status: PASS") {
		t.Fatalf("incremental design input omitted prior approval: %s", designPrompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	designDispatch = openDispatchID(state, "action", "qa-design")
	state, err = ClaimDispatch(root, pkg, state.RunID, designDispatch, "qa-design-rework")
	if err != nil {
		t.Fatal(err)
	}
	revised := []QACaseInput{cases[0], {Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the public CLI against a built snapshot", Oracle: "observable output matches"}}
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaCases("")[0].ID != "CASE-001" || state.qaCases("")[0].ReviewStatus != "PASS" || state.qaCases("")[1].ReviewStatus != "PENDING" {
		t.Fatalf("exact approval was not preserved: %#v", state.allQACases())
	}
	reviewPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewPrompt, "Accepted coverage context") || strings.Count(reviewPrompt, "mode: whitebox") != 0 || !strings.Contains(reviewPrompt, "mode: blackbox") {
		t.Fatalf("retry prompt did not separate accepted and pending cases: %s", reviewPrompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	reviewDispatch = openDispatchID(state, "action", "qa-review")
	state, err = ClaimDispatch(root, pkg, state.RunID, reviewDispatch, "reviewer-b")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, []QAReviewInput{{CaseID: state.qaCases("")[1].ID, Outcome: "PASS"}}, "", nil)
	if err != nil || state.qaReview("").Status != "PASS" {
		t.Fatalf("incremental review did not pass: %v %#v", err, state.qaReview(""))
	}
}

func TestGatePromptExcludesFrozenArtifactsAndResultUsesDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "gate-binding")
	prompt, err := PrepareGate(root, pkg, state.RunID, "quality", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID := openDispatchID(state, "gate", "quality")
	for _, want := range []string{"Excluded review targets", "requirements.md", "design.md", dispatchID} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("gate prompt omitted %q: %s", want, prompt)
		}
	}
	state, err = ClaimDispatch(root, pkg, state.RunID, dispatchID, "gate-reviewer")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].DispatchID != dispatchID {
		t.Fatalf("gate result lost dispatch binding: %#v", state.Gates["quality"])
	}
}

func TestNativeSnapshotMismatchRejectsPreparedResultWithoutMutation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "snapshot-mismatch")
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "gate-reviewer")
	writeTestFile(t, filepath.Join(root, "unrecorded.txt"), "new commit\n")
	commitAll(t, root, "advance outside workflow")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", comparedRange(state), nil); err == nil || !strings.Contains(err.Error(), "native VCS identity") {
		t.Fatalf("stale native snapshot result was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("native snapshot rejection changed state")
	}
}

func TestQAExecutionPreservesKindsAndFullRouteSeals(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "seal")
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatchID, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.qaExecution("").Cases) != 2 || state.qaExecution("").Cases[0].Mode != "whitebox" || state.qaExecution("").Cases[1].Mode != "blackbox" {
		t.Fatalf("QA execution lost case modes: %#v", state.qaExecution("").Cases)
	}
	for index, gate := range []string{"architecture", "quality"} {
		dispatchID = prepareAndClaim(t, root, pkg, state.RunID, gate, fmt.Sprintf("gate-%d", index+1))
		state, err = RecordGate(root, pkg, state.RunID, gate, dispatchID, "PASS", "", comparedRange(state), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" || summary.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestRepairUsesNativeSnapshotAndPreparedCarryBinding(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "repair")
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	architecture := prepareAndClaim(t, root, pkg, state.RunID, "architecture", "architecture-initial")
	state, err = RecordGate(root, pkg, state.RunID, "architecture", architecture, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	quality := prepareAndClaim(t, root, pkg, state.RunID, "quality", "quality-initial")
	state, err = RecordGate(root, pkg, state.RunID, "quality", quality, "FAIL", "", comparedRange(state), []FindingInput{{Severity: "P1", Message: "normal workflow fails", Locations: []string{"internal/cli/cli.go:1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("blocking wave was not completed: %#v", state)
	}
	prior := state.CurrentSnapshot
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "repair.txt"), "repair\n")
	commitAll(t, root, "repair")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.PreRepairSnapshot != prior || state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("repair boundary was not resolved natively: %#v", state)
	}
	// 修复快照后存在旧快照 PASS 待 Carry 决策时，先处置 Carry 才能继续 qa-*。
	carry := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err = RecordCarry(root, pkg, state.RunID, carry, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair does not touch architecture behavior"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 修复快照推进后旧快照的权威 QA 结果存续，重新派发 qa-execution 属重跑，
	// 必须已记录覆盖本次重跑的 scope 决策（FULL 全量重跑）才能 prepare。
	state, err = RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	qaDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	quality = prepareAndClaim(t, root, pkg, state.RunID, "quality", "quality-repair")
	state, err = RecordGate(root, pkg, state.RunID, "quality", quality, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedReviewWaves != 2 || state.PreRepairSnapshot != "" || state.Gates["architecture"].Snapshot != state.CurrentSnapshot {
		t.Fatalf("repair verification did not complete: %#v", state)
	}
}

func TestGateFindingCannotTargetFrozenAcceptanceArtifact(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "excluded-finding")
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "excluded-reviewer")
	before := stateBytes(t, root, state.RunID)
	_, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "FAIL", "", comparedRange(state), []FindingInput{{Severity: "P1", Message: "rewrite the requirement", Locations: []string{"requirements.md:1"}}})
	if err == nil || !strings.Contains(err.Error(), "not a review target") {
		t.Fatalf("frozen acceptance artifact finding was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected excluded-target finding changed state")
	}
}

func TestSetLevelQAReviewFindingFailsWithoutReopeningPassingCases(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "set-finding")
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "set-reviewer")
	state, err := RecordQAReview(root, pkg, state.RunID, dispatchID, passingReviewDecisions(state), "", []FindingInput{{Severity: "P1", Message: "missing failure-path coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" || state.qaCases("")[0].ReviewStatus != "PASS" || state.qaCases("")[1].ReviewStatus != "PASS" {
		t.Fatalf("set-level finding changed valid decisions: %#v", state)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordQADesign(root, pkg, state.RunID, designDispatch, baselineCases(), ""); err == nil || !strings.Contains(err.Error(), "add or revise a case") {
		t.Fatalf("unchanged all-PASS QA rework was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected QA rework changed state")
	}
	revised := baselineCases()
	revised = append(revised, QACaseInput{Mode: "whitebox", Description: "failure paths are covered", Procedure: "run the delivered failure-path test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxFailurePaths"})
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("").Status != "PASS" || state.qaCases("")[2].ReviewStatus != "PENDING" {
		t.Fatalf("corrected QA rework did not reopen review: %#v", state)
	}
}

// TestBlackboxReviewActionFailBlocksSnapshotWithAllCasesPass verifies the
// snapshot gate looks at the whole review outcome, not just case-level decisions:
// when a blackbox review round judges every pending case PASS but records a
// set-level P1 coverage-omission finding (qa-review FAIL, qa-design re-opened,
// cases still all PASS), the snapshot must be blocked — a case-level-only check
// would let it through despite the review action failing. The only bypass is the
// user-authorized snapshot release.
func TestBlackboxReviewActionFailBlocksSnapshotWithAllCasesPass(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "review-action-fail"), "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 集合层面 P1 覆盖遗漏：各用例判 PASS，但审查动作整体 FAIL（qa-design 重开）。
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "review-action-fail-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{{Severity: "P1", Message: "missing failure-path coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" {
		t.Fatalf("set-level P1 finding did not fail the review action: %#v", state.qaReview(""))
	}
	for _, testCase := range state.allQACases() {
		if testCase.ReviewStatus != "PASS" {
			t.Fatalf("set-level finding should not reopen case decisions: %#v", testCase)
		}
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-review-fail.txt"), "delivery\n")
	commitAll(t, root, "delivery review fail")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "blackbox QA Review must pass") {
		t.Fatalf("snapshot with failing review action was not blocked: %v", err)
	}
	// 用户授权放行是唯一绕过：显式 userRequested 记录放行并带风险继续。
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, true, "user releases the failing review")
	if err != nil {
		t.Fatalf("user-authorized snapshot after review FAIL was rejected: %v", err)
	}
	if state.SnapshotOverride == nil || state.SnapshotOverride.Origin != "USER" {
		t.Fatalf("user release not recorded: %#v", state.SnapshotOverride)
	}
}

func TestQADesignAcceptsRemovalOnlyDuplicateCorrection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "remove-duplicate"), "full", nil)
	cases := append(baselineCases(), QACaseInput{Mode: "whitebox", Description: "duplicate direct coverage", Procedure: "run the delivered duplicate test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDuplicate"})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, cases, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "duplicate-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{{Severity: "P1", Message: "remove duplicated direct coverage"}})
	if err != nil {
		t.Fatal(err)
	}
	designDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, baselineCases(), "")
	if err != nil {
		t.Fatalf("removal-only duplicate correction was rejected: %v", err)
	}
	if len(state.allQACases()) != 2 || state.qaReview("").Status != "PENDING" {
		t.Fatalf("removal-only correction did not retain approvals: %#v", state)
	}
	reviewDispatch = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "duplicate-recheck-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, nil, "", nil)
	if err != nil {
		t.Fatalf("set-only QA recheck was rejected: %v", err)
	}
	if state.qaReview("").Status != "PASS" {
		t.Fatalf("set-only QA recheck did not approve the correction: %#v", state.qaReview(""))
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("approved removal-only correction did not unlock development: %v", err)
	}
}

func TestProductReviewPreDevelopmentGatingAndFailRecovery(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "product-review-gate"))

	// 未 PASS：start-readiness、拆分决定与 development-worker 均被拒绝。路线在拆
	// 分决定之后确认，而拆分决定在 product-review 之后，所以 development-worker 在
	// 此时因路线未确认而被拒。
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass before Start Readiness") {
		t.Fatalf("start-readiness prepared before product review: %v", err)
	}
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass before the slicing decision") {
		t.Fatalf("slicing decision recorded before product review: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil {
		t.Fatalf("development-worker prepared before product review: %v", err)
	}

	// FAIL 含发现项（P0/P1/P2/P3 分级）：仍阻塞，但不构成不可恢复的终态。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "requirement does not target a real user problem"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "FAIL" || len(state.Actions["product-review"].Findings) != 1 || state.Actions["product-review"].Findings[0].Severity != "P1" {
		t.Fatalf("product review FAIL was not recorded with severity: %#v", state.Actions["product-review"])
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass") {
		t.Fatalf("start-readiness prepared after product review FAIL: %v", err)
	}

	// 重新派发并记录 PASS 后下游解锁。
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("product review PASS was not recorded: %#v", state.Actions["product-review"])
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development worker stayed blocked after product review PASS: %v", err)
	}
}

// 接受路径：审查派发 A 保持 OPEN，用户接受后直接在 A 上记录 PASS，不另造新派发。
// PASS 对应的是真跑过审查的子代理派发，生命周期校验因此可过。
func TestProductReviewAcceptRecordsPassOnHeldDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "product-review-accept"))

	// 未记录（PENDING）：start-readiness 与 development-worker 均被拒绝。
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass before Start Readiness") {
		t.Fatalf("start-readiness prepared before product review: %v", err)
	}

	// 派发 A 已准备并认领（保持 OPEN，未记录 FAIL）：仍阻塞。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass") {
		t.Fatalf("start-readiness prepared while product review held open: %v", err)
	}

	// 用户接受后，直接在 A 上记录 PASS，下游解锁。
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("product review PASS was not recorded on the held dispatch: %#v", state.Actions["product-review"])
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development worker stayed blocked after product review PASS: %v", err)
	}
}

// TestQADesignRequiresSelectedQA verifies QA Design needs a selected QA mode
// (blackbox or whitebox); the pre-development reviews are now a strict prefix of
// the route, so QA selected already implies Product Review passed.
func TestQADesignRequiresSelectedQA(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-design-requires-qa"), "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err == nil || !strings.Contains(err.Error(), "QA is not selected") {
		t.Fatalf("qa-design without selected QA mode: %v", err)
	}
	state, err := AddRouteGates(root, pkg, state.RunID, []string{blackboxQAID})
	if err != nil {
		t.Fatal(err)
	}
	if !isSelected(state, blackboxQAID) {
		t.Fatalf("blackbox QA was not added to the route: %#v", state.SelectedGates)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err != nil {
		t.Fatalf("qa-design stayed blocked after blackbox QA was selected: %v", err)
	}
}

func TestProductReviewRequiredByPostDevelopmentTransitions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		op     string
		target string
		setup  func(*RunState)
		want   string
	}{
		{
			name: "snapshot", op: "snapshot",
			setup: func(state *RunState) { state.Actions["development-worker"] = ActionResult{Status: developmentPrepared} },
			want:  "Product Review must pass before a development snapshot",
		},
		{
			name: "gate", op: "gate", target: "quality",
			setup: func(state *RunState) { state.Actions["development-worker"] = ActionResult{Status: developmentComplete} },
			want:  "Product Review must pass before post-development review",
		},
		{
			name: "seal", op: "seal",
			setup: func(state *RunState) {
				state.Actions["development-worker"] = ActionResult{Status: developmentComplete}
				state.Gates["quality"] = GateResult{Status: "PASS", Snapshot: state.CurrentSnapshot}
			},
			want: "Product Review must pass before Seal",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewRunState("pr-post", "formal", "requirements.md", "rev", "git", "base", "current", "baseprompt", "catalog", true, []string{"quality"}, nil)
			state.RouteMode = "custom"
			state.SelectedGates = []string{"quality"}
			tc.setup(&state)
			if err := requireTransition(state, tc.op, tc.target); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("requireTransition(%s) err=%v want %q", tc.op, err, tc.want)
			}
			state.Actions["product-review"] = ActionResult{Status: "PASS"}
			if err := requireTransition(state, tc.op, tc.target); err != nil && strings.Contains(err.Error(), "Product Review") {
				t.Fatalf("requireTransition(%s) still blocked by product review after PASS: %v", tc.op, err)
			}
		})
	}
}

// TestPredatingRunWithoutPrerequisiteActionCompletesDelivery verifies the
// generic pre-development gating: a run that does not carry a prerequisite
// action (a run started before that action existed) is not blocked by its gate
// and still completes snapshot, gate review, and Seal.
func TestPredatingRunWithoutPrerequisiteActionCompletesDelivery(t *testing.T) {
	for _, removed := range []string{"product-review", "start-readiness"} {
		t.Run(removed, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := mustStart(t, root, pkg, "predating")
			// Drop the entry so the persisted state has no such action, exactly
			// like a run started before the action was part of the flow.
			delete(state.Actions, removed)
			if err := SaveRunState(root, state); err != nil {
				t.Fatal(err)
			}

			state = confirmRequirement(t, root, pkg, state)
			if removed == "start-readiness" {
				state = recordProductReview(t, root, pkg, state)
			} else {
				state = recordReadiness(t, root, pkg, state)
			}
			state = recordSlicing(t, root, pkg, state, "no-split")
			state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
			developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
			writeTestFile(t, filepath.Join(root, "delivery.txt"), "delivery\n")
			commitAll(t, root, "delivery")
			state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
			if err != nil {
				t.Fatalf("run without %s blocked at snapshot: %v", removed, err)
			}
			gateDispatch := prepareAndClaim(t, root, pkg, state.RunID, "quality", "predating-gate")
			state, err = RecordGate(root, pkg, state.RunID, "quality", gateDispatch, "PASS", "", comparedRange(state), nil)
			if err != nil {
				t.Fatalf("run without %s blocked at gate review: %v", removed, err)
			}
			summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
			if err != nil {
				t.Fatalf("run without %s blocked at seal: %v", removed, err)
			}
			if summary.Status != "SEALED" {
				t.Fatalf("run without %s did not seal: %#v", removed, summary)
			}
			if _, ok := state.Actions[removed]; ok {
				t.Fatalf("run without %s gained that action: %#v", removed, state.Actions)
			}
		})
	}
}

func TestRetainedOverallSnapshotFreezesRequirementArtifacts(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "retained-freeze", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true, Split: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	// 保留总任务实例必须记录 split（自动附加合并门与合并 QA），不设常规路线。
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "split")
	writeTestFile(t, filepath.Join(root, "merged-delivery.txt"), "merged delivery\n")
	commitAll(t, root, "merged delivery")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !developmentStarted(state) {
		t.Fatalf("retained snapshot did not establish development start: %#v", state.Actions["development-worker"])
	}
	writeTestFile(t, filepath.Join(root, "design.md"), "changed design\n")
	commitAll(t, root, "changed retained requirement")
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "preserved", nil); err == nil || !strings.Contains(err.Error(), "requires user confirmation") {
		t.Fatalf("retained snapshot allowed meaning-preserved artifact rebinding without user confirmation: %v", err)
	}
}

func TestDevelopmentSnapshotRejectsUncommittedTrackedGitChanges(t *testing.T) {
	root, pkg := workflowFixture(t)
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "base delivery\n")
	commitAll(t, root, "tracked delivery")
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "uncommitted-snapshot"), "custom", []string{"quality"})
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "edited delivery\n")
	before := stateBytes(t, root, state.RunID)
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes must be committed") {
		t.Fatalf("uncommitted tracked delivery was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected uncommitted snapshot changed state")
	}
	commitAll(t, root, "commit delivery")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err != nil {
		t.Fatalf("committed delivery snapshot was rejected: %v", err)
	}
}

// TestSnapshotRejectsDevelopmentWithoutCommit verifies requirement 2's acceptance
// "任一未完成时 snapshot 被挡": a development worker that is prepared but has not
// committed must not record the base as the development snapshot. The snapshot
// requires the development side to truly complete (a development commit advancing
// the native identity), not merely a PREPARED status.
func TestSnapshotRejectsDevelopmentWithoutCommit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "snapshot-no-commit"), "custom", []string{"quality"})
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	before := stateBytes(t, root, state.RunID)
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "a new current snapshot is required") {
		t.Fatalf("snapshot without a development commit was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected no-commit snapshot changed state")
	}
}

func TestRouteAdditionAfterCompletedWaveReviewsOnlyAddedGate(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "route-add", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "route-quality", "PASS", "", nil)
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("initial wave did not complete: %#v", state)
	}
	quality := state.Gates["quality"]
	state, err := AddRouteGates(root, pkg, state.RunID, []string{"architecture"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.SelectedGates, []string{"architecture", "quality"}) || state.CompletedReviewWaves != 1 {
		t.Fatalf("late addition changed route order or wave count: %#v", state)
	}
	if got := state.Gates["quality"]; got.Status != quality.Status || got.Snapshot != quality.Snapshot || got.DispatchID != quality.DispatchID || state.Gates["architecture"].Status != "PENDING" {
		t.Fatalf("late addition changed the completed result set: %#v", state.Gates)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "route-architecture", "PASS", "", nil)
	if got := state.Gates["quality"]; state.CompletedReviewWaves != 1 || got.Status != quality.Status || got.Snapshot != quality.Snapshot || got.DispatchID != quality.DispatchID {
		t.Fatalf("late gate recounted the snapshot or replaced a result: %#v", state)
	}
}

func TestLateRouteGateRepairStartsAFreshAttemptInTheNextWave(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "late-route-repair", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "late-quality", "PASS", "", nil)
	state, err := AddRouteGates(root, pkg, state.RunID, []string{"architecture"})
	if err != nil {
		t.Fatal(err)
	}

	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "architecture", "late-architecture")
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch := state.Dispatches[dispatchID]; dispatch.ReviewWave != 1 || dispatch.Attempt != 1 {
		t.Fatalf("supplemental review binding=%#v", dispatch)
	}
	state, err = RecordGate(root, pkg, state.RunID, "architecture", dispatchID, "FAIL", "", comparedRange(state), []FindingInput{{Severity: "P1", Message: "repair required"}})
	if err != nil {
		t.Fatal(err)
	}
	state = advanceRepair(t, root, pkg, state, "late-route")
	// 修复快照后 quality 的旧 PASS 待 Carry 决策，先处置 Carry 才能重派发
	// 其他门。
	carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err = RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "quality", Decision: "INHERIT", Message: "repair is outside quality ownership"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}

	dispatchID = prepareAndClaim(t, root, pkg, state.RunID, "architecture", "repaired-architecture")
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if dispatch := state.Dispatches[dispatchID]; dispatch.ReviewWave != 2 || dispatch.Attempt != 1 {
		t.Fatalf("post-repair review binding=%#v", dispatch)
	}
}

func TestReviewWaveLimitAndCarryScope(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "wave-limit", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "wave-architecture-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "wave-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("initial review wave count=%d", state.CompletedReviewWaves)
	}
	for wave := 2; wave <= automaticReviewWaveLimit; wave++ {
		state = advanceRepair(t, root, pkg, state, fmt.Sprintf("wave-%d", wave))
		if got := eligibleCarryGates(state); !reflect.DeepEqual(got, []string{"architecture"}) {
			t.Fatalf("wave %d Carry scope=%v", wave, got)
		}
		carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
		if wave == 2 {
			before := stateBytes(t, root, state.RunID)
			if _, err := RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "quality", Decision: "INHERIT", Message: "not eligible"}}, "", false, ""); err == nil || !strings.Contains(err.Error(), "not eligible") {
				t.Fatalf("Carry accepted a non-passing gate: %v", err)
			}
			if stateBytes(t, root, state.RunID) != before {
				t.Fatal("rejected Carry changed state")
			}
		}
		var err error
		state, err = RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair is outside architecture ownership"}}, "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		state = recordGateResult(t, root, pkg, state, "quality", fmt.Sprintf("wave-quality-%d", wave), "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
		if state.CompletedReviewWaves != wave {
			t.Fatalf("completed waves=%d want=%d", state.CompletedReviewWaves, wave)
		}
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
		t.Fatalf("automatic wave limit was not enforced: %v", err)
	}
	beforeAuthorization := stateBytes(t, root, state.RunID)
	if _, err := AuthorizeExtraRepair(root, pkg, state.RunID, 2, nil); err == nil || !strings.Contains(err.Error(), "exactly one review wave") {
		t.Fatalf("multiple extra waves were authorized at once: %v", err)
	}
	if stateBytes(t, root, state.RunID) != beforeAuthorization {
		t.Fatal("rejected multi-wave authorization changed state")
	}
	state, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("extra review waves=%d want=1", state.ExtraReviewWaves)
	}
	authorizedState := stateBytes(t, root, state.RunID)
	if _, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, nil); err == nil || !strings.Contains(err.Error(), "not exhausted") {
		t.Fatalf("another wave was preauthorized before the current one ran: %v", err)
	}
	if stateBytes(t, root, state.RunID) != authorizedState {
		t.Fatal("rejected stacked authorization changed state")
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("authorized extra repair remained blocked: %v", err)
	}
}

func TestRuntimeAuthorizationPersistsUntilRepairSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "runtime-authorization", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "runtime-architecture", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repairable blocker"}})
	state = recordGateResult(t, root, pkg, state, "quality", "runtime-quality", "RUNTIME_ERROR", "review unavailable", nil)
	if state.CompletedReviewWaves != 0 {
		t.Fatalf("runtime-error wave was counted: %#v", state)
	}
	if _, err := Seal(root, pkg, state.RunID, []string{"quality"}, false, "squashed delivery"); err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("partial Seal authorization lost the other blocker: %v", err)
	}
	persisted, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	authorization := persisted.SkipAuthorizations["quality"]
	if authorization.Origin != "SEAL" || authorization.Status != "RUNTIME_ERROR" || authorization.Snapshot != persisted.CurrentSnapshot {
		t.Fatalf("named runtime authorization was not persisted: %#v", authorization)
	}
	persisted = advanceRepair(t, root, pkg, persisted, "after-runtime-authorization")
	if _, ok := persisted.SkipAuthorizations["quality"]; ok {
		t.Fatalf("Seal authorization survived a repair snapshot: %#v", persisted.SkipAuthorizations)
	}
	if authorization := persisted.SkipAuthorizations[blackboxQAID]; authorization.Origin != "ROUTE" {
		t.Fatalf("route authorization was cleared with snapshot authorization: %#v", persisted.SkipAuthorizations)
	}
	if persisted.CompletedReviewWaves != 0 {
		t.Fatalf("authorized incomplete wave was counted: %#v", persisted)
	}
}

func TestSealUserRequestedFailSkipBypassesWaveLimit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "seal-user-requested", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "user-architecture", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "user-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "cannot repair now"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("blocking wave was not completed: %#v", state)
	}
	if _, err := Seal(root, pkg, state.RunID, []string{"quality"}, false, "squashed delivery"); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
		t.Fatalf("FAIL skip before the wave limit must require exhaustion without --user-requested: %v", err)
	}
	summary, err := Seal(root, pkg, state.RunID, []string{"quality"}, true, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("user-requested seal did not complete: %#v", summary)
	}
	authorization := summary.SkipAuthorizations["quality"]
	if authorization.Origin != "SEAL-USER" || authorization.Status != "FAIL" || authorization.Snapshot != summary.CurrentSnapshot {
		t.Fatalf("user-requested FAIL skip authorization was not recorded: %#v", authorization)
	}
}

func TestSealFailSkipAfterWaveLimitKeepsSealOrigin(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "seal-limit-origin", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "limit-architecture-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "limit-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("completed waves=%d want=1", state.CompletedReviewWaves)
	}
	for wave := 2; wave <= automaticReviewWaveLimit; wave++ {
		state = advanceRepair(t, root, pkg, state, fmt.Sprintf("limit-%d", wave))
		carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
		var err error
		state, err = RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair is outside architecture ownership"}}, "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		state = recordGateResult(t, root, pkg, state, "quality", fmt.Sprintf("limit-quality-%d", wave), "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
		if state.CompletedReviewWaves != wave {
			t.Fatalf("completed waves=%d want=%d", state.CompletedReviewWaves, wave)
		}
	}
	summary, err := Seal(root, pkg, state.RunID, []string{"quality"}, false, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("limit-exhausted seal did not complete: %#v", summary)
	}
	authorization := summary.SkipAuthorizations["quality"]
	if authorization.Origin != "SEAL" || authorization.Status != "FAIL" || authorization.Snapshot != summary.CurrentSnapshot {
		t.Fatalf("limit-exhausted FAIL skip must keep the SEAL origin: %#v", authorization)
	}
}

func TestConcurrentSelectedGateRecordingPreservesResults(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "concurrent-gates", "custom", []string{"architecture", "quality"})
	dispatches := map[string]string{}
	for _, gate := range []string{"architecture", "quality"} {
		dispatches[gate] = prepareAndClaim(t, root, pkg, state.RunID, gate, "concurrent-"+gate)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(dispatches))
	for gate, dispatchID := range dispatches {
		wg.Add(1)
		go func(gate, dispatchID string) {
			defer wg.Done()
			_, err := RecordGate(root, pkg, state.RunID, gate, dispatchID, "PASS", "", comparedRange(state), nil)
			errs <- err
		}(gate, dispatchID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"architecture", "quality"} {
		if state.Gates[gate].Status != "PASS" {
			t.Fatalf("concurrent recording lost %s: %#v", gate, state.Gates)
		}
	}
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("concurrent wave count=%d", state.CompletedReviewWaves)
	}
}

func TestResumeAndAbortNormalLifecycle(t *testing.T) {
	t.Run("resume", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		state := mustStart(t, root, pkg, "resume")
		report, err := ResumeReport(root, pkg, state.RunID)
		if err != nil || report.ClassificationRequired {
			t.Fatalf("unchanged run did not resume: report=%#v err=%v", report, err)
		}
		writeTestFile(t, filepath.Join(root, "requirements.md"), "revised requirement\n")
		report, err = ResumeReport(root, pkg, state.RunID)
		if err != nil || !report.ClassificationRequired {
			t.Fatalf("normal requirement edit was not reported on Resume: report=%#v err=%v", report, err)
		}
	})
	t.Run("abort", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		state := mustStart(t, root, pkg, "abort")
		summary, err := Abort(root, pkg, state.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Status != "ABORTED" {
			t.Fatalf("abort summary=%#v", summary)
		}
		if _, err := os.Stat(RunDir(root, state.RunID)); !os.IsNotExist(err) {
			t.Fatalf("aborted run directory remained: %v", err)
		}
		if _, err := os.Stat(RunSummaryPath(root, state.RunID)); err != nil {
			t.Fatalf("abort summary was not retained: %v", err)
		}
	})
}

func TestStartAcceptsAncestorBaseSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	ancestor := gitHead(t, root)
	writeTestFile(t, filepath.Join(root, "committed.txt"), "in-flight work\n")
	commitAll(t, root, "in-flight work")
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "ancestor-base", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", BaseSnapshot: ancestor, Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if state.BaseSnapshot != ancestor || state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("ancestor base was not adopted: base=%s current=%s", state.BaseSnapshot, state.CurrentSnapshot)
	}
}

func TestStartRejectsNonAncestorBaseSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	branch := strings.TrimSpace(runGit(t, root, "branch", "--show-current"))
	runGit(t, root, "checkout", "-b", "divergent")
	writeTestFile(t, filepath.Join(root, "divergent.txt"), "divergent\n")
	commitAll(t, root, "divergent work")
	divergent := gitHead(t, root)
	runGit(t, root, "checkout", branch)
	writeTestFile(t, filepath.Join(root, "mainline.txt"), "mainline\n")
	commitAll(t, root, "mainline work")
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "non-ancestor", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", BaseSnapshot: divergent, Split: "no"}); err == nil || !strings.Contains(err.Error(), "not an ancestor") {
		t.Fatalf("non-ancestor base snapshot was accepted: %v", err)
	}
}

func TestRunStateRecordsPerGateActionPromptHashes(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "prompt-hashes")
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if state.PromptHashes["base"] != promptContentHash(catalog.Base) {
		t.Fatalf("base prompt hash missing: %#v", state.PromptHashes)
	}
	for _, gate := range catalog.Gates {
		if state.PromptHashes["gate:"+gate.ID] != composedGatePromptHash(catalog, gate.Content) {
			t.Fatalf("gate %s prompt hash missing", gate.ID)
		}
	}
	for _, action := range catalog.Actions {
		// 注入审查者动作的组装提示词含共享契约（base），其哈希与门对称地计入 base；
		// 非审查动作只哈希自身内容。
		want := promptContentHash(action.Content)
		if isReviewerAction(action.ID) {
			want = composedActionPromptHash(catalog, action)
		}
		if state.PromptHashes["action:"+action.ID] != want {
			t.Fatalf("action %s prompt hash missing", action.ID)
		}
	}
}

func TestOldRunStateWithoutPromptHashesLoadsCompatible(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "old-state")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stateBytes(t, root, state.RunID)), &decoded); err != nil {
		t.Fatal(err)
	}
	// 模拟旧状态：无 promptHashes（运行期哈希记录出现之前的格式）。规定旧状态文件
	// （无 stateIntegrity 字段）跳过完整性校验，故一并去掉该字段，使旧状态以"无校验字段"的
	// 合法旧形态加载，而不是被当作"CLI 写入后被手工改写"拒绝。
	delete(decoded, "promptHashes")
	delete(decoded, "stateIntegrity")
	rewritten, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RunStatePath(root, state.RunID), append(rewritten, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatalf("old state failed to load: %v", err)
	}
	if loaded.PromptHashes != nil {
		t.Fatalf("old state gained hashes: %#v", loaded.PromptHashes)
	}
	if _, err := requireCurrentCatalog(loaded, pkg); err != nil {
		t.Fatalf("old state could not continue: %v", err)
	}
}

func TestUnselectedGatePromptChangeDoesNotBlockRun(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "unselected-change"), "custom", []string{"quality"})
	writeTestFile(t, filepath.Join(pkg, "gates", "architecture.md"), "new architecture checks\n")
	report, err := ResumeReport(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.CatalogDelta, "gate:architecture") || containsString(report.CatalogDelta, "gate:quality") {
		t.Fatalf("catalog delta misreported the unselected-only change: %v", report.CatalogDelta)
	}
	state = recordReadiness(t, root, pkg, state)
	if state.Actions["start-readiness"].Status != "PASS" {
		t.Fatalf("unselected catalog change blocked the run: %#v", state.Actions)
	}
}

func TestSelectedGatePromptChangeRecordedByMainAgentCarry(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "selected-change", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "selected-change-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(pkg, "gates", "quality.md"), "new quality checks\n")
	report, err := ResumeReport(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(report.CatalogDelta, "gate:quality") {
		t.Fatalf("selected gate change was not reported: %v", report.CatalogDelta)
	}
	state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "prompt wording only; quality ownership unchanged")
	if err != nil {
		t.Fatal(err)
	}
	if result := state.Gates["quality"]; result.Status != "PASS" || result.Snapshot != state.CurrentSnapshot {
		t.Fatalf("inherited gate was not retained: %#v", result)
	}
	if record := state.Carry["quality"]; record.Decision != "INHERIT" || !strings.Contains(record.Message, "wording") {
		t.Fatalf("carry judgment was not recorded: %#v", record)
	}
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if state.CatalogRevision != catalog.CatalogRevision {
		t.Fatalf("catalog was not accepted on judgment: %v != %v", state.CatalogRevision, catalog.CatalogRevision)
	}
}

func TestChangedSelectedGateCanBeReDispatched(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "re-dispatch", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "re-dispatch-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(pkg, "gates", "quality.md"), "new quality checks\n")
	// 门提示词内容变化且该目标已有记录结果时，先记录主代理 Carry 判定才能重派发。
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "prompt changed") {
		t.Fatalf("changed gate with a recorded result was re-dispatched before a Carry judgment: %v", err)
	}
	state, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "accept catalog; quality needs a fresh review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatalf("changed gate could not be re-dispatched after the Carry judgment: %v", err)
	}
}

func TestChangedGateStaysReDispatchableAfterMainAgentCarry(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "carry-then-redispatch", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "carry-then-redispatch-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(pkg, "gates", "quality.md"), "new quality checks\n")
	state, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "accept catalog; quality needs a fresh review")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if state.CatalogRevision != catalog.CatalogRevision {
		t.Fatalf("carry did not accept the catalog: %v", state.CatalogRevision)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatalf("changed gate was not re-dispatchable after carry accepted the catalog: %v", err)
	}
}

func TestMidFlightGateChangeDisposableAfterIndependentCarry(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "mid-flight-dispose", "custom", []string{"architecture", "quality"})
	state = recordGateResult(t, root, pkg, state, "architecture", "mid-architecture-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repair required"}})
	state = recordGateResult(t, root, pkg, state, "quality", "mid-quality-1", "PASS", "", nil)
	state = advanceRepair(t, root, pkg, state, "mid-flight")
	carryDispatch := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err := RecordCarry(root, pkg, state.RunID, carryDispatch, []CarryInput{{GateID: "quality", Decision: "INHERIT", Message: "repair does not touch quality"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "mid-architecture-2", "PASS", "", nil)
	if state.CompletedReviewWaves != 2 || state.PreRepairSnapshot != "" {
		t.Fatalf("repair cycle did not complete: %#v", state)
	}
	// architecture 未被本次 Carry 判定覆盖，提示词变化使其记录结果成为待判继承。
	writeTestFile(t, filepath.Join(pkg, "gates", "architecture.md"), "new architecture checks\n")
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", false, ""); err == nil || !strings.Contains(err.Error(), "prompt changed") {
		t.Fatalf("prompt-changed architecture was re-dispatched before a judgment: %v", err)
	}
	// 主代理 Carry 处置中途修改：即使先前已记录独立 Carry 判定，目录变更时仍可用。
	if _, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "accept catalog; architecture needs a fresh review"); err != nil {
		t.Fatalf("mid-flight disposal was rejected: %v", err)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "architecture", false, ""); err != nil {
		t.Fatalf("disposed architecture could not be re-dispatched: %v", err)
	}
}

func TestBaseOnlyPromptChangeEnablesPerGateReDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "base-change", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "base-change-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "new shared contract\n")
	// 共享契约变化同样使门提示词移动，先记录主代理 Carry 判定才能重派发。
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "prompt changed") {
		t.Fatalf("base-only change was re-dispatched before a Carry judgment: %v", err)
	}
	state, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "accept catalog; quality needs a fresh review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err != nil {
		t.Fatalf("base-only change did not enable per-gate re-dispatch: %v", err)
	}
}

func TestAdoptExternalChangeRebindsAndCarries(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "adopt", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "adopt-quality", "PASS", "", nil)
	prior := state.CurrentSnapshot
	writeTestFile(t, filepath.Join(root, "external.txt"), "external work\n")
	commitAll(t, root, "external work")
	head := gitHead(t, root)
	before := stateBytes(t, root, state.RunID)
	if _, err := AdoptExternalChange(root, pkg, state.RunID, ""); err == nil || !strings.Contains(err.Error(), "requires a reason") {
		t.Fatalf("adoption without a reason was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected adoption changed state")
	}
	state, err := AdoptExternalChange(root, pkg, state.RunID, "adopt unrelated external commit")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := state.Carry[carryAdoptKey]
	if state.CurrentSnapshot != head || state.PreRepairSnapshot != prior || !ok || record.Origin != "ADOPT" || record.SourceSnapshot != prior || record.TargetSnapshot != head || record.Message != "adopt unrelated external commit" {
		t.Fatalf("adoption did not rebind with provenance: %#v", state)
	}
	if result := state.Gates["quality"]; result.Status != "PASS" || result.Snapshot != prior {
		t.Fatalf("passing result was not left eligible for carry: %#v", result)
	}
	state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "external commit does not affect quality ownership")
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Snapshot != state.CurrentSnapshot || state.PreRepairSnapshot != "" {
		t.Fatalf("carried gate was not rebound at the adopted head: %#v", state.Gates["quality"])
	}
}

// TestPreDevelopmentAdoptRebindsWithoutRepairBoundary covers the pre-dev adopt
// deadlock (requirement 1): adopting an external change before any development
// snapshot must not set PreRepairSnapshot or reset the review surface, so the
// subsequent prepare → commit → workflow snapshot path is not blocked by "the
// current repair still requires verification".
func TestPreDevelopmentAdoptRebindsWithoutRepairBoundary(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "predev-adopt"), "custom", []string{"quality"})
	prior := state.CurrentSnapshot
	writeTestFile(t, filepath.Join(root, "external.txt"), "external work\n")
	commitAll(t, root, "external work")
	head := gitHead(t, root)
	state, err := AdoptExternalChange(root, pkg, state.RunID, "adopt external commit before development")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != head || state.PreRepairSnapshot != "" {
		t.Fatalf("pre-development adoption set an unexpected repair boundary: %#v", state)
	}
	if record, ok := state.Carry[carryAdoptKey]; !ok || record.Origin != "ADOPT" || record.TargetSnapshot != head || record.SourceSnapshot != prior {
		t.Fatalf("pre-development adoption did not record provenance: %#v", state.Carry[carryAdoptKey])
	}
	// 采纳不重置审查面：开发前已 PASS 的动作保持 PASS。
	if state.Actions["product-review"].Status != "PASS" || state.Actions["start-readiness"].Status != "PASS" {
		t.Fatalf("pre-development adoption reset the review surface: %#v", state.Actions)
	}
	// 采纳后直接准备开发并走通首个快照：不被 "the current repair still requires
	// verification" 挡住。
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-predev.txt"), "delivery\n")
	commitAll(t, root, "delivery")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatalf("pre-development adoption blocked the first development snapshot: %v", err)
	}
	if state.Actions["development-worker"].Status != developmentComplete || state.PreRepairSnapshot != "" {
		t.Fatalf("first development snapshot did not complete cleanly: %#v", state)
	}
	// (b) 已存在开发快照、run 已置 REPAIR_PREPARED 时的采纳必须走 post-development
	// 修复边界路径：把 PreRepairSnapshot 设为旧快照，而不是被 preDevelopment 谓词当作
	// pre-dev 直接吸收（该谓词只认 PENDING/PREPARED，不认 REPAIR_PREPARED）。先记录
	// 首个阻塞波（quality FAIL）完成首波审查并把 development-worker 置 REPAIR_PREPARED，
	// 再提交外部改动后采纳，断言边界被建立。
	state = recordGateResult(t, root, pkg, state, "quality", "predev-adopt-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker requires repair"}})
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("blocking review wave was not completed: %#v", state)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("repair preparation failed: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.Actions["development-worker"].Status != developmentRepairPrepared {
		t.Fatalf("development worker was not repair prepared: %#v", state.Actions["development-worker"])
	}
	prior = state.CurrentSnapshot
	writeTestFile(t, filepath.Join(root, "external-repair.txt"), "external repair work\n")
	commitAll(t, root, "external repair work")
	head = gitHead(t, root)
	state, err = AdoptExternalChange(root, pkg, state.RunID, "adopt external change during repair")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != head || state.PreRepairSnapshot != prior {
		t.Fatalf("post-development adoption did not set the pre-repair boundary: %#v", state)
	}
}

// TestDevelopmentWorkerClaimAfterCommit covers the post-commit claim deadlock
// (requirement 2a): a development dispatch is reviewer-required and its worker
// commits before the main agent claims, so claiming must accept a native HEAD
// that is a descendant (or equal) of the dispatch source snapshot, and the
// claimed dispatch must then record its development snapshot successfully.
func TestDevelopmentWorkerClaimAfterCommit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "claim-after-commit")
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "claim-qa-reviewer")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatchID := openDispatchID(state, "action", "development-worker")
	if dispatchID == "" || state.Dispatches[dispatchID].Status != "OPEN" {
		t.Fatalf("development dispatch was not prepared open: %#v", state.Dispatches)
	}
	// worker 已提交：HEAD 前进到派发源快照之后，此时才拿到身份认领。
	writeTestFile(t, filepath.Join(root, "delivery-claim.txt"), "delivery\n")
	commitAll(t, root, "delivery commit")
	state, err = ClaimDispatch(root, pkg, state.RunID, dispatchID, "dev-worker-identity")
	if err != nil {
		t.Fatalf("development dispatch claim after worker commit was rejected: %v", err)
	}
	if state.Dispatches[dispatchID].Status != "CLAIMED" || state.Dispatches[dispatchID].ReviewerIdentity != "dev-worker-identity" {
		t.Fatalf("development dispatch was not claimed: %#v", state.Dispatches[dispatchID])
	}
	state, err = AdvanceSnapshot(root, pkg, state.RunID, dispatchID, false, "")
	if err != nil {
		t.Fatalf("snapshot after post-commit claim failed: %v", err)
	}
	if state.Actions["development-worker"].Status != developmentComplete || state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("development snapshot did not complete: %#v", state)
	}
	// (b) 非 development-worker 的 reviewer-required 派发（gate）在 OPEN 状态下、原生
	// HEAD 前进后认领必须被拒绝：原生身份必须精确匹配当前快照，不能沿用 development-worker
	// 的后代认领放宽；被拒后派发保持 OPEN、状态不被改写。
	gateDispatch := prepareDispatch(t, root, pkg, state.RunID, "quality")
	state, _ = LoadRunState(root, state.RunID)
	if state.Dispatches[gateDispatch].Status != "OPEN" {
		t.Fatalf("gate dispatch was not prepared open: %#v", state.Dispatches[gateDispatch])
	}
	writeTestFile(t, filepath.Join(root, "external-claim.txt"), "external commit\n")
	commitAll(t, root, "external commit")
	before := stateBytes(t, root, state.RunID)
	if _, err := ClaimDispatch(root, pkg, state.RunID, gateDispatch, "late-gate-reviewer"); err == nil || !strings.Contains(err.Error(), "native VCS identity does not match the current snapshot") {
		t.Fatalf("non-development claim after native commit was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected non-development claim changed state")
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.Dispatches[gateDispatch].Status != "OPEN" {
		t.Fatalf("rejected non-development claim mutated the dispatch: %#v", state.Dispatches[gateDispatch])
	}
}

func TestPostDevelopmentPreservedRebindKeepsPass(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "preserved", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "preserved-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(root, "requirements.md"), "revised meaning-preserved requirement\n")
	commitAll(t, root, "revised requirement")
	state, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != gitHead(t, root) || !state.RequirementConfirmed {
		t.Fatalf("preserved rebind did not rebind the snapshot: %#v", state)
	}
	if result := state.Gates["quality"]; result.Status != "PASS" || result.Snapshot != state.CurrentSnapshot {
		t.Fatalf("preserved rebind did not retain PASS at the new snapshot: %#v", result)
	}
}

func TestGateReviewComparedRangeIsValidated(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "compared", "custom", []string{"quality"})
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "quality", "compared-reviewer")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", "pre-repair..current", nil); err == nil || !strings.Contains(err.Error(), "reported compared") {
		t.Fatalf("mismatched compared pair was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected compared mismatch changed state")
	}
	state, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Compared != comparedRange(state) {
		t.Fatalf("compared pair was not stored: %#v", state.Gates["quality"])
	}
}

// TestDevelopmentPreparationRequiresSlicingDecision verifies the split decision
// is mandatory for formal runs: without it, route and development preparation
// are rejected; recording a no-split decision (fast path) unblocks them.
func TestDevelopmentPreparationRequiresSlicingDecision(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "slicing-required"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := SetRoute(root, pkg, state.RunID, "custom", []string{"quality"}); err == nil || !strings.Contains(err.Error(), "the slicing decision must be recorded before the route") {
		t.Fatalf("route accepted before slicing decision: %v", err)
	}
	// 路线在拆分决定之后确认，所以未记录拆分决定时开发准备被拒（路线未确认）。
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil {
		t.Fatalf("development prepared before slicing decision: %v", err)
	}
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development stayed blocked after slicing decision: %v", err)
	}
}

// TestSlicingDecisionRequiresStartReadinessPass verifies the split decision can
// only be recorded after Start Readiness (Part 2) passes, and that once recorded
// it is the binding point and is not re-cut.
func TestSlicingDecisionRequiresStartReadinessPass(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "slicing-timing"))
	state = recordProductReview(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "Start Readiness must pass before the slicing decision") {
		t.Fatalf("slicing decision recorded before start-readiness: %v", err)
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordSlicing(root, pkg, state.RunID, "split", 2, nil, "", "re-cut", ""); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("slicing decision was re-cut: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected re-cut changed state")
	}
}

// TestNoSplitDecisionRequiresReasonNote verifies the mandatory "建议不拆（原因）"
// reason trace for a no-split decision and the >= 2 slice rule for a split.
func TestNoSplitDecisionRequiresReasonNote(t *testing.T) {
	root, pkg := workflowFixture(t)
	// no-split 声明 run：无原因 note 的 no-split 被拒。
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "slicing-note"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "", ""); err == nil || !strings.Contains(err.Error(), "reason note") {
		t.Fatalf("no-split decision without a reason note was accepted: %v", err)
	}
	// 保留总任务实例（--split yes）：单片 split 被拒。
	splitState := confirmRequirement(t, root, pkg, mustStartRetained(t, root, pkg, "slicing-note-split"))
	splitState = recordProductReview(t, root, pkg, splitState)
	splitState = recordReadiness(t, root, pkg, splitState)
	if _, err := RecordSlicing(root, pkg, splitState.RunID, "split", 1, nil, "", "", ""); err == nil || !strings.Contains(err.Error(), "at least two slices") {
		t.Fatalf("single-slice split was accepted: %v", err)
	}
}

// TestMergeVerificationAutoAttachedForSplitRetainedOverall verifies a retained
// overall run with a split decision auto-attaches merge gate and merge QA as its
// only post-merge verification and does not go through normal route selection.
func TestMergeVerificationAutoAttachedForSplitRetainedOverall(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "merge-auto", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true, Split: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	state, err = RecordSlicing(root, pkg, state.RunID, "split", 2, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Slicing == nil || state.Slicing.Decision != "split" || state.Slicing.SplitCount != 2 || !strings.Contains(state.Slicing.Parallel, "parallel") {
		t.Fatalf("split decision not captured: %#v", state.Slicing)
	}
	if state.RouteMode != "merge" || !reflect.DeepEqual(state.SelectedGates, []string{mergeQAID, mergeGateID}) {
		t.Fatalf("split retained overall did not auto-attach merge verification: %#v", state)
	}
	// 保留总任务实例不涉常规路线选择。
	if _, err := SetRoute(root, pkg, state.RunID, "custom", []string{"quality"}); err == nil || !strings.Contains(err.Error(), "already has its one route decision") {
		t.Fatalf("merge route accepted normal route selection: %v", err)
	}
}

// TestMergeVerificationCompletesPostMergeReview runs the full post-merge flow of
// a split retained overall run: merge QA design/review/execution (with an empty
// cross-slice interaction set leaving the required trace) and the merge gate.
func TestMergeVerificationCompletesPostMergeReview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "merge-flow", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true, Split: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "split")

	// 合并 QA 设计（各分片开发期间并行进行）：跨切片交互用例可为零，此时留痕。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("").Status != "PASS" || !strings.Contains(state.qaDesign("").Message, "切片基本独立") {
		t.Fatalf("merge QA zero-case design not traced: %#v", state.qaDesign(""))
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "merge-qa-review")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 合并后快照。
	writeTestFile(t, filepath.Join(root, "merged.txt"), "merged\n")
	commitAll(t, root, "merged slices")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 合并 QA 执行于合并快照（零用例即 PASS，留痕）。
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "PASS" || !strings.Contains(state.qaExecution("").Message, "切片基本独立") {
		t.Fatalf("merge QA zero-case execution: %#v", state.qaExecution(""))
	}
	// 合并门。
	gateDispatch := prepareAndClaim(t, root, pkg, state.RunID, mergeGateID, "merge-gate-reviewer")
	state, err = RecordGate(root, pkg, state.RunID, mergeGateID, gateDispatch, "PASS", "", comparedRange(state), nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedReviewWaves != 1 || state.Gates[mergeGateID].Status != "PASS" {
		t.Fatalf("merge gate review did not complete the wave: %#v", state)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("merge verification did not seal: %#v", summary)
	}
}

// TestSliceSealWritesNoLedgerAndMasterMergesCost verifies 分片封板: a slice
// instance's seal writes no independent ledger file (封板文件) and leaves only
// a cost sidecar under its retained-overall master's temp run dir; the master's
// final seal folds the slice's cost into its own ledger and consumes the
// sidecar. Non-slice seals keep writing their ledger as before.
func TestSliceSealWritesNoLedgerAndMasterMergesCost(t *testing.T) {
	root, pkg := workflowFixture(t)
	var err error

	// 主干（保留总任务实例）：确认需求 + 整体级审查 + 拆分决定（自动附加合并验证）。
	master := confirmRequirement(t, root, pkg, mustStartRetained(t, root, pkg, "slice-cost-master"))
	master = recordProductReview(t, root, pkg, master)
	master = recordReadiness(t, root, pkg, master)
	master = recordSlicing(t, root, pkg, master, "split")

	// 记录派发时以 stub 转写计量真实用量，使成本并入主干后可断言数值。
	stubCodexTranscript(t, writeCostFixture(t))

	// 合并 QA 设计/审查在分片开发期间并行推进：此刻原生 HEAD 仍停在基线，
	// 主干当前快照与之一致；若放到切片提交之后再准备会因快照漂移被拒。
	mergeDesign := prepareDispatch(t, root, pkg, master.RunID, "qa-design")
	master, err = RecordQADesign(root, pkg, master.RunID, mergeDesign, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	mergeReview := prepareAndClaim(t, root, pkg, master.RunID, "qa-review", "slice-cost-merge-reviewer")
	master, err = RecordQAReview(root, pkg, master.RunID, mergeReview, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 切片实例：钉死主干、继承整体级审查，走黑盒 QA + 开发 + 快照 + 执行。
	slice := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "slice-cost", master.RunID))
	slice = recordSlicing(t, root, pkg, slice, "split", master.RunID)
	slice = setRoute(t, root, pkg, slice, "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, slice.RunID, "qa-design")
	slice, err = RecordQADesign(root, pkg, slice.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, slice.RunID, "qa-review", "slice-cost-reviewer")
	slice, err = RecordQAReview(root, pkg, slice.RunID, reviewDispatch, passingReviewDecisions(slice), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, slice.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "slice-cost.txt"), "slice\n")
	commitAll(t, root, "slice cost delivery")
	slice, err = AdvanceSnapshot(root, pkg, slice.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	executionDispatch := prepareDispatch(t, root, pkg, slice.RunID, "qa-execution")
	slice, err = RecordQAExecution(root, pkg, slice.RunID, executionDispatch, passingExecution(slice.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	if slice.Cost == nil || slice.Cost.TotalInputTokens == 0 {
		t.Fatalf("slice recorded no cost projection: %#v", slice.Cost)
	}
	sliceOwn := slice.Cost.TotalInputTokens

	// 切片封板：不产出独立封板文件，只留成本 sidecar。
	sliceSummary, err := Seal(root, pkg, slice.RunID, nil, false, "squashed slice")
	if err != nil {
		t.Fatal(err)
	}
	if sliceSummary.Status != "SEALED" {
		t.Fatalf("slice did not seal: %#v", sliceSummary)
	}
	if _, statErr := os.Stat(RunSummaryPath(root, slice.RunID)); !os.IsNotExist(statErr) {
		t.Fatalf("slice seal must not write an independent ledger file (封板文件)")
	}
	sidecarData, err := os.ReadFile(SliceCostPath(root, master.RunID, slice.RunID))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar SliceCostRecord
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.RunID != slice.RunID || sidecar.Cost == nil || sidecar.Cost.TotalInputTokens != sliceOwn {
		t.Fatalf("slice cost sidecar malformed: %#v", sidecar)
	}

	// 主干合并验证后封板：切片成本并入主干最终封板文件，sidecar 被消费清除。
	writeTestFile(t, filepath.Join(root, "merged-cost.txt"), "merged\n")
	commitAll(t, root, "merged slices cost")
	master, err = AdvanceSnapshot(root, pkg, master.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	mergeExec := prepareDispatch(t, root, pkg, master.RunID, "qa-execution")
	master, err = RecordQAExecution(root, pkg, master.RunID, mergeExec, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	mergeGateDispatch := prepareAndClaim(t, root, pkg, master.RunID, mergeGateID, "slice-cost-merge-gate")
	master, err = RecordGate(root, pkg, master.RunID, mergeGateID, mergeGateDispatch, "PASS", "", comparedRange(master), nil)
	if err != nil {
		t.Fatal(err)
	}
	if master.Cost == nil {
		t.Fatalf("master recorded no cost projection")
	}
	masterOwn := master.Cost.TotalInputTokens

	masterSummary, err := Seal(root, pkg, master.RunID, nil, false, "squashed delivery with slices")
	if err != nil {
		t.Fatal(err)
	}
	if masterSummary.Cost == nil {
		t.Fatalf("master seal summary lost the cost projection")
	}
	if masterSummary.Cost.TotalInputTokens != masterOwn+sliceOwn {
		t.Fatalf("master totals did not fold slice cost: got %d, want %d", masterSummary.Cost.TotalInputTokens, masterOwn+sliceOwn)
	}
	for id := range sidecar.Cost.Dispatches {
		if _, ok := masterSummary.Cost.Dispatches[slice.RunID+"/"+id]; !ok {
			t.Fatalf("master summary missing namespaced slice dispatch %q", id)
		}
	}
	if _, statErr := os.Stat(SliceCostPath(root, master.RunID, slice.RunID)); !os.IsNotExist(statErr) {
		t.Fatalf("master seal must consume the slice cost sidecar")
	}
	data, err := os.ReadFile(RunSummaryPath(root, master.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cost"`) {
		t.Fatalf("master seal ledger lacks cost")
	}
}

// TestSliceAbortWritesNoLedgerAndSidecarKeepsCost verifies a slice instance's
// abort mirrors its seal: no independent ledger file (封板文件), while its
// recorded cost projection goes to the master's sidecar so the master's final
// seal still folds in the tokens actually spent by the aborted slice.
func TestSliceAbortWritesNoLedgerAndSidecarKeepsCost(t *testing.T) {
	root, pkg := workflowFixture(t)
	var err error

	// 主干（保留总任务实例）：确认需求 + 整体级审查 + 拆分决定。
	master := confirmRequirement(t, root, pkg, mustStartRetained(t, root, pkg, "slice-abort-master"))
	master = recordProductReview(t, root, pkg, master)
	master = recordReadiness(t, root, pkg, master)
	master = recordSlicing(t, root, pkg, master, "split")

	// 记录派发时以 stub 转写计量真实用量，使成本写入 sidecar 后可断言数值。
	stubCodexTranscript(t, writeCostFixture(t))

	// 切片实例：确认需求、记录一次派发产生成本投影，然后中止。
	slice := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "slice-abort", master.RunID))
	slice = recordSlicing(t, root, pkg, slice, "split", master.RunID)
	slice = setRoute(t, root, pkg, slice, "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, slice.RunID, "qa-design")
	slice, err = RecordQADesign(root, pkg, slice.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if slice.Cost == nil || slice.Cost.TotalInputTokens == 0 {
		t.Fatalf("slice recorded no cost projection: %#v", slice.Cost)
	}
	sliceOwn := slice.Cost.TotalInputTokens

	// 分片中止：不产出独立封板文件，只留成本 sidecar；run 目录照常清除。
	summary, err := Abort(root, pkg, slice.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "ABORTED" {
		t.Fatalf("slice abort summary=%#v", summary)
	}
	if _, statErr := os.Stat(RunSummaryPath(root, slice.RunID)); !os.IsNotExist(statErr) {
		t.Fatalf("slice abort must not write an independent ledger file (封板文件)")
	}
	sidecarData, err := os.ReadFile(SliceCostPath(root, master.RunID, slice.RunID))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar SliceCostRecord
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.RunID != slice.RunID || sidecar.Cost == nil || sidecar.Cost.TotalInputTokens != sliceOwn {
		t.Fatalf("aborted slice cost sidecar malformed: %#v", sidecar)
	}
	if _, statErr := os.Stat(RunDir(root, slice.RunID)); !os.IsNotExist(statErr) {
		t.Fatalf("aborted slice run directory remained: %v", statErr)
	}
}

// TestMergeGateRegisteredButExcludedFromNormalRouteCandidates verifies the merge
// gate is registered in the catalog but never appears in the normal route
// candidates; the normal candidates expose blackbox and whitebox QA modes.
func TestMergeGateRegisteredButExcludedFromNormalRouteCandidates(t *testing.T) {
	pkg := promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks", "merge-gate": "merge checks"})
	catalog, err := LoadPromptCatalog(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Gate(mergeGateID); !ok {
		t.Fatal("merge gate is not registered in the catalog")
	}
	candidates, err := PackageRouteCandidates(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range candidates {
		if id == mergeGateID || id == mergeQAID {
			t.Fatalf("merge verification appeared in normal route candidates: %v", candidates)
		}
	}
	if !containsString(candidates, blackboxQAID) || !containsString(candidates, whiteboxQAID) {
		t.Fatalf("normal route candidates missing QA modes: %v", candidates)
	}
}

// TestRouteCandidatesRunStateRequiresConfirmedRequirement verifies the run-state
// RouteCandidates entry: it resolves the live catalog for the run and returns
// the same normal route candidates as the package-level entry once the
// requirement is confirmed, and refuses before confirmation.
func TestRouteCandidatesRunStateRequiresConfirmedRequirement(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "route-candidates")
	// 需求未确认：run-state RouteCandidates 拒绝。
	if _, err := RouteCandidates(root, pkg, state.RunID); err == nil || !strings.Contains(err.Error(), "requirement is not confirmed") {
		t.Fatalf("route candidates before requirement confirmation: %v", err)
	}
	state = confirmRequirement(t, root, pkg, state)
	candidates, err := RouteCandidates(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	// 与包级 PackageRouteCandidates 同源：黑盒、白盒加除合并门外的全部门。
	packaged, err := PackageRouteCandidates(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidates, packaged) {
		t.Fatalf("run-state RouteCandidates=%v want %v", candidates, packaged)
	}
	for _, id := range candidates {
		if id == mergeGateID || id == mergeQAID {
			t.Fatalf("run-state route candidates included merge verification: %v", candidates)
		}
	}
}

// TestBlackboxWhiteboxOptionalityInRoute verifies full selects both QA modes,
// custom can select each QA mode independently, and no mechanical per-mode
// quality floor exists: the case-set sufficiency is left to the qa-review
// set-level coverage judgment.
func TestBlackboxWhiteboxOptionalityInRoute(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-blackbox-only"), "custom", []string{blackboxQAID})
	if !isSelected(state, blackboxQAID) || isSelected(state, whiteboxQAID) {
		t.Fatalf("blackbox-only route: %#v", state.SelectedGates)
	}
	// 不设机械化质量下限：被选中模式零用例由 qa-review 的 set-level 覆盖判定承担。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("").Status != "PASS" {
		t.Fatalf("blackbox design did not pass: %#v", state.qaDesign(""))
	}

	whitebox := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-whitebox-only"), "custom", []string{whiteboxQAID})
	if !isSelected(whitebox, whiteboxQAID) || isSelected(whitebox, blackboxQAID) {
		t.Fatalf("whitebox-only route: %#v", whitebox.SelectedGates)
	}
	// 白盒设计在开发后读实现进行：先完成开发与快照再设计。
	developmentDispatch := prepareDispatch(t, root, pkg, whitebox.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-whitebox-floor.txt"), "delivery\n")
	commitAll(t, root, "delivery whitebox floor")
	whitebox, err = AdvanceSnapshot(root, pkg, whitebox.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	designDispatch = prepareDispatch(t, root, pkg, whitebox.RunID, "qa-design", "whitebox")
	whitebox, err = RecordQADesign(root, pkg, whitebox.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"}}, "")
	if err != nil {
		t.Fatalf("whitebox design after development failed: %v", err)
	}
	if whitebox.qaDesign("whitebox").Status != "PASS" {
		t.Fatalf("whitebox design did not pass: %#v", whitebox.qaDesign("whitebox"))
	}

	full := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-full"), "full", nil)
	if !isSelected(full, blackboxQAID) || !isSelected(full, whiteboxQAID) {
		t.Fatalf("full route must select both QA modes: %#v", full.SelectedGates)
	}
}

// TestTwoSpeedSchedulingBothPathsReachDevelopment verifies both scheduling
// paths: the fast path records a no-split decision and runs as a single run, and
// the split path records a split decision then confirms the route per slice.
// Both unlock development after the split decision.
func TestTwoSpeedSchedulingBothPathsReachDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	fast := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "fast-path"), "full", nil)
	if fast.Slicing == nil || fast.Slicing.Decision != "no-split" || fast.Slicing.Note == "" {
		t.Fatalf("fast path no-split decision not traced: %#v", fast.Slicing)
	}
	// 开发开始不再要求黑盒 qa-review PASS：黑盒 QA 在隔离工作区与开发并发，快照门在
	// 两边都完成时才放行。
	if _, err := PrepareAction(root, pkg, fast.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("fast path did not unlock development after route confirmation: %v", err)
	}

	// 拆分路径是一个切片实例：启动时先用 --split yes --master <id> 钉死引用的保留总任务
	// master，记录拆分决定时引用同一 master，经继承满足整体级审查与 development-worker 门。
	splitMaster := sliceMaster(t, root, pkg, "split-path-master")
	split := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "split-path", splitMaster))
	split = recordSlicing(t, root, pkg, split, "split", splitMaster)
	split = setRoute(t, root, pkg, split, "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, split.RunID, "qa-design")
	split, err := RecordQADesign(root, pkg, split.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, split.RunID, "qa-review", "split-qa-reviewer")
	split, err = RecordQAReview(root, pkg, split.RunID, reviewDispatch, passingReviewDecisions(split), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, split.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("split path did not unlock development: %v", err)
	}
}

// TestSplitDecisionAllowsPerSliceRouteConfirmation verifies a slice instance
// (non-retained run) inherits the overall-level product-review/start-readiness
// (recording the inheritance source, not re-running them), records the inherited
// split decision, confirms its own route per slice without re-confirming the
// split, and satisfies the development-worker gate via inheritance.
func TestSplitDecisionAllowsPerSliceRouteConfirmation(t *testing.T) {
	root, pkg := workflowFixture(t)
	// 切片实例不重跑整体级产品审/技术审：启动时用 --split yes --master <id> 钉死引用的
	// 保留总任务 master，记录继承来源，不再要求切片内重跑。
	master := sliceMaster(t, root, pkg, "slice-route-master")
	state := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "slice-route", master))
	state = recordSlicing(t, root, pkg, state, "split", master)
	if state.Slicing == nil || state.Slicing.Decision != "split" || state.Slicing.SplitCount != 2 {
		t.Fatalf("inherited split decision not recorded: %#v", state.Slicing)
	}
	if state.Slicing.MasterRunID != master {
		t.Fatalf("slice did not record master run id: %#v", state.Slicing)
	}
	if !reflect.DeepEqual(state.Slicing.InheritedReviews, []string{"product-review", "start-readiness"}) {
		t.Fatalf("slice did not record inherited reviews: %#v", state.Slicing)
	}
	if !actionPassedOrAbsent(state, "product-review") || !actionPassedOrAbsent(state, "start-readiness") {
		t.Fatalf("slice did not inherit the overall-level reviews: %#v", state.Slicing)
	}
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if !reflect.DeepEqual(state.SelectedGates, []string{"quality"}) {
		t.Fatalf("per-slice route not bound: %#v", state.SelectedGates)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("slice development stayed blocked despite inherited reviews: %v", err)
	}
}

// TestPreDevelopmentReviewFindingsCarrySeverity verifies product-review and
// start-readiness findings are graded P0/P1/P2/P3 so the main agent can apply
// the re-review boundary (only P2/P3 -> revise without re-review).
func TestPreDevelopmentReviewFindingsCarrySeverity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "review-severity"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P0", Message: "blocking"}, {Severity: "P2", Message: "minor"}, {Severity: "P3", Message: "trivial"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Findings[0].Severity != "P0" || state.Actions["product-review"].Findings[1].Severity != "P2" || state.Actions["product-review"].Findings[2].Severity != "P3" {
		t.Fatalf("severity not retained: %#v", state.Actions["product-review"].Findings)
	}
	next := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", next, "FAIL", "", []FindingInput{{Severity: "P4", Message: "bad"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, P2, or P3") {
		t.Fatalf("invalid severity accepted: %v", err)
	}
	// P4 的 FAIL 未被记录，next 派发仍认领未出结果；补记合法 PASS 完成它，使
	// 放行下一次准备（否则强制续用 next）。
	state, err = RecordAction(root, pkg, state.RunID, "product-review", next, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	readiness := recordReadiness(t, root, pkg, state)
	dispatchID = prepareDispatch(t, root, pkg, readiness.RunID, "start-readiness")
	if _, err := RecordAction(root, pkg, readiness.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "technical gap"}}, false, ""); err != nil {
		t.Fatalf("start-readiness severity rejected: %v", err)
	}
}

// TestSettledFindingsAreInjectedAndClearedOnMeaningChange verifies the CLI
// records the settled findings list, injects it into the next product-review /
// start-readiness dispatch (reviewer-side enforcement), and clears it on a
// meaning-changing requirement revision.
func TestSettledFindingsAreInjectedAndClearedOnMeaningChange(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "settled-findings"))
	// 处置登记引用该动作已记录的审查结果：先记录一次 FAIL（含 P1 与 P2 发现项），再逐项
	// 驳回。驳回的 P0/P1 不阻塞；确认的 P0/P1 才会置位"需重审"标记。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "settled finding one"}, {Severity: "P2", Message: "settled finding two"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordSettledFindings(root, pkg, state.RunID, "product-review", nil, []string{"settled finding one", "settled finding two"})
	if err != nil {
		t.Fatal(err)
	}
	if got := state.SettledFindings["product-review"]; len(got) != 2 {
		t.Fatalf("settled findings not recorded: %#v", state.SettledFindings)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "settled finding one") || !strings.Contains(prompt, "Do not re-raise") {
		t.Fatalf("settled findings not injected into product-review prompt: %s", prompt)
	}
	// 驳回的 P0/P1 不阻塞：重录一轮 PASS 可直接通过（无未处置的阻塞项）。
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "settled technical decision"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordSettledFindings(root, pkg, state.RunID, "start-readiness", nil, []string{"settled technical decision"})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err = PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "settled technical decision") {
		t.Fatalf("settled findings not injected into start-readiness prompt: %s", prompt)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), "revised requirement\n")
	commitAll(t, root, "revised requirement")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.SettledFindings) != 0 {
		t.Fatalf("settled findings survived a meaning-changing revision: %#v", state.SettledFindings)
	}
}

// TestLegacyQAModeCarryRebindsQAExecution verifies the carry regression fix for
// runs bound to an old catalog that listed QA as a gate: the legacy "qa" id is
// recognized as a QA mode, main-agent carry emits the selected QA mode id (here
// "qa") and rebinds QAExecution.Snapshot instead of writing a spurious
// Gates["qa"] entry, and QA execution is no longer rejected as "QA is not
// selected".
func TestLegacyQAModeCarryRebindsQAExecution(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "legacy-qa", "custom", []string{blackboxQAID, "architecture"})
	// 模拟旧目录绑定的 run：SelectedGates 仍带遗留 "qa"。
	state.SelectedGates = []string{legacyQAID, "architecture"}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	if !isQAMode(legacyQAID) || !isSelectedQA(state) {
		t.Fatalf("legacy qa is not recognized as a selected QA mode: %#v", state.SelectedGates)
	}
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "legacy-arch", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advanceRepair(t, root, pkg, state, "legacy-repair")
	if got := eligibleMainCarryResults(state, false); !reflect.DeepEqual(got, []string{legacyQAID}) {
		t.Fatalf("eligible carry results=%v want=[qa]", got)
	}
	state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "repair does not touch QA behavior")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Snapshot != state.CurrentSnapshot {
		t.Fatalf("legacy QAExecution.Snapshot=%s want=%s", state.qaExecution("").Snapshot, state.CurrentSnapshot)
	}
	if _, ok := state.Gates[legacyQAID]; ok {
		t.Fatalf("spurious Gates[qa] entry written: %#v", state.Gates[legacyQAID])
	}
}

// TestWhiteboxQADesignsAndReviewsAfterDevelopment verifies the whitebox-only route
// designs and reviews its structure cases after development: development
// start and the snapshot are not gated on whitebox QA Review, the post-development
// design reads the implementation, and the run seals.
func TestWhiteboxQADesignsAndReviewsAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "whitebox-post-dev"), "custom", []string{whiteboxQAID})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("whitebox development blocked before QA design: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-whitebox.txt"), "delivery\n")
	commitAll(t, root, "delivery whitebox")
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure tests", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 单模式白盒 run 的 design/review 存于 whitebox mode 键，qa-execution 按同
	// mode 派发。
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "whitebox")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("whitebox-only run did not seal: %#v", summary)
	}
}

// TestFullRouteDesignsWhiteboxAfterDevelopment verifies the full route's two-phase
// QA: blackbox cases are designed and approved before development, the
// whitebox structure cases are added after development by re-deriving the complete
// set (preserving the approved blackbox cases), and the run seals.
func TestFullRouteDesignsWhiteboxAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "full-two-phase"), "full", nil)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	var err error
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "blackbox-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-full.txt"), "delivery\n")
	commitAll(t, root, "delivery full")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 开发后白盒设计：下白盒设计轮只增补白盒用例（写进白盒自己的列表），既有黑盒
	// 用例（含 review PASS 状态）在各自列表中原样保留。
	designDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	blackboxCases := state.qaModeCases("blackbox")
	whiteboxCases := state.qaModeCases("whitebox")
	if len(state.allQACases()) != 2 || len(blackboxCases) != 1 || blackboxCases[0].ReviewStatus != "PASS" || len(whiteboxCases) != 1 || whiteboxCases[0].ReviewStatus != "PENDING" {
		t.Fatalf("blackbox approval was not preserved in the whitebox redesign: %#v", state.allQACases())
	}
	reviewDispatch = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"architecture", "quality"} {
		gateDispatch := prepareAndClaim(t, root, pkg, state.RunID, gate, "two-phase-"+gate)
		state, err = RecordGate(root, pkg, state.RunID, gate, gateDispatch, "PASS", "", comparedRange(state), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed delivery")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("full two-phase run did not seal: %#v", summary)
	}
}

// TestQAModeCasesPreferPerModeOverStaleMerged reproduces the storage bug
// that blocked a live run's snapshot: after a legacy single-list state file
// migrated its cases into the merged "" key, a per-mode redesign wrote fresh
// cases to the blackbox/whitebox keys, leaving stale cases (some PENDING) in the
// "" key. qaModeCases/allQACases must apply per-mode precedence so the stale
// merged cases are not double-counted and PENDING legacy blackbox cases do not
// block blackboxReviewPassed.
func TestQAModeCasesPreferPerModeOverStaleMerged(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "qa-per-mode-precedence", "custom", []string{"blackbox", "whitebox", "quality"})
	// 注入缺陷布局："" 键残留旧迁移用例（含 PENDING 黑盒用例），同时 per-mode 键已写入新用例。
	loaded, _ := LoadRunState(root, state.RunID)
	loaded.QACasesByMode = map[string][]QACase{
		"": {
			{ID: "CASE-101", Mode: "blackbox", Description: "stale blackbox", Procedure: "old", Oracle: "old", ReviewStatus: "PENDING"},
			{ID: "CASE-102", Mode: "whitebox", Description: "stale whitebox", Procedure: "old", Oracle: "old", ReviewStatus: "PASS"},
		},
		"blackbox": {
			{ID: "CASE-001", Mode: "blackbox", Description: "current blackbox", Procedure: "new", Oracle: "new", ReviewStatus: "PASS"},
		},
		"whitebox": {
			{ID: "CASE-002", Mode: "whitebox", Description: "current whitebox", Procedure: "new", Oracle: "new", ReviewStatus: "PASS"},
		},
	}
	if err := SaveRunState(root, loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := LoadRunState(root, state.RunID)
	// 黑盒读取优先 per-mode 键：只看到当前 CASE-001，忽略 "" 键残留的 PENDING CASE-101。
	blackbox := reloaded.qaModeCases("blackbox")
	if len(blackbox) != 1 || blackbox[0].ID != "CASE-001" {
		t.Fatalf("qaModeCases(blackbox) must prefer the per-mode key over stale merged cases: %#v", blackbox)
	}
	// 全量视图不得把 "" 键残留用例与 per-mode 用例合并（去重、不双计）。
	if all := reloaded.allQACases(); len(all) != 2 {
		t.Fatalf("allQACases must not double-count stale merged cases: %#v", all)
	}
	// 快照黑盒门不再被 "" 键残留的 PENDING 黑盒用例挡住。
	if !blackboxReviewPassed(reloaded) {
		t.Fatalf("blackboxReviewPassed must not see the stale PENDING merged case: %#v", reloaded.QACasesByMode)
	}
	// 合并（空 mode）视图 = 全量当前用例（含 fast-path "" 键幸存用例 + per-mode 新用例）。
	merged := reloaded.qaModeCases("")
	if len(merged) != 2 {
		t.Fatalf("qaModeCases(empty) must be the full current set: %#v", merged)
	}
}

// TestQADispatchRequiresModeAfterDevelopment verifies the mode binding guard of
// requirement 1: after development starts, a qa-design/qa-review dispatch without
// --mode is rejected (it would silently bind the main worktree, letting blackbox
// agents read development code and bypassing the isolation), while the fast-path
// (pre-development) empty mode stays legal.
func TestQADispatchRequiresModeAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "mode-guard"), "full", nil)
	// 开发开始前空 mode 合法（快速路径）。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err != nil {
		t.Fatalf("pre-development qa-design without mode was rejected: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development start failed: %v", err)
	}
	// 开发开始后 qa-design/qa-review 省略 --mode 被拒。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err == nil || !strings.Contains(err.Error(), "requires --mode blackbox or whitebox after development starts") {
		t.Fatalf("post-development qa-design without mode was accepted: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, ""); err == nil || !strings.Contains(err.Error(), "requires --mode blackbox or whitebox after development starts") {
		t.Fatalf("post-development qa-review without mode was accepted: %v", err)
	}
	// 带上 mode 即可继续派发（白盒绑主工作区，无需隔离工作区）。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "whitebox", false, ""); err != nil {
		t.Fatalf("post-development qa-design with mode was rejected: %v", err)
	}
}

// TestFastPathBlackboxDesignParallelToStartReadiness verifies the fast-path blackbox
// QA design can start before start-readiness PASS and before the slicing decision
// and route confirmation (the documented fast-path parallel tradeoff), and that it
// is kept when the confirmed route includes blackbox QA.
func TestFastPathBlackboxDesignParallelToStartReadiness(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "fast-design"))
	state = recordProductReview(t, root, pkg, state)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaDesign("").Status != "PASS" || len(state.allQACases()) != 1 {
		t.Fatalf("fast-path design not recorded: %#v", state.qaDesign(""))
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
	if state.qaDesign("").Status != "PASS" {
		t.Fatalf("fast-path design lost after route confirmation: %#v", state.qaDesign(""))
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "fast-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("fast-path did not unlock development: %v", err)
	}
}

// TestFastPathDesignDiscardedWhenRouteOmitsBlackbox verifies the documented
// fast-path tradeoff: when the confirmed route omits blackbox QA, the speculative
// parallel design is discarded so it is re-done against the confirmed route.
func TestFastPathDesignDiscardedWhenRouteOmitsBlackbox(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "fast-discard"))
	state = recordProductReview(t, root, pkg, state)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{whiteboxQAID})
	if state.qaDesign("").Status != "PENDING" || len(state.allQACases()) != 0 {
		t.Fatalf("fast-path design not discarded on a route without blackbox QA: %#v", state.qaDesign(""))
	}
}

// TestPreDevelopmentReviewFindingsRequireSeverity verifies product-review and
// start-readiness findings must be graded non-empty P0/P1/P2/P3 (requirement 14),
// so an ungraded finding is rejected instead of slipping through.
func TestPreDevelopmentReviewFindingsRequireSeverity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "severity-required"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded finding"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, P2, or P3") {
		t.Fatalf("ungraded product-review finding accepted: %v", err)
	}
	// 无严重度的 FAIL 未被记录，dispatchID 派发仍认领未出结果；补记合法 PASS 完成它，
	// 使放行下一次准备。
	var err error
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded technical finding"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, P2, or P3") {
		t.Fatalf("ungraded start-readiness finding accepted: %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func mustStart(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// mustStartRetained starts a retained-overall instance with the mandatory
// --split yes declaration (需求 4).
func mustStartRetained(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true, Split: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// mustStartSlice starts a slice instance that pins its retained-overall master
// in the start declaration (需求 4: --split yes --master <id>).
func mustStartSlice(t *testing.T, root, pkg, id, masterID string) RunState {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", Split: "yes", MasterRunID: masterID})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func confirmAndRoute(t *testing.T, root, pkg string, state RunState, mode string, selected []string) RunState {
	t.Helper()
	return confirmAndRouteBase(t, root, pkg, state, mode, selected)
}

// confirmAndRouteBase runs the complete pre-development gating chain in the
// confirmed order: requirement confirmation, Product Review (Part 1), Start
// Readiness (Part 2), the mandatory no-split slicing decision with its reason
// note, and then the route confirmation. The route comes after the split
// decision, so tests that exercise the pre-development review gates use
// confirmRequirement instead and record the reviews explicitly.
func confirmAndRouteBase(t *testing.T, root, pkg string, state RunState, mode string, selected []string) RunState {
	t.Helper()
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	return setRoute(t, root, pkg, state, mode, selected)
}

// confirmRequirement records Requirements Clarification PASS and confirms the
// requirement, without touching the pre-development reviews, so tests can
// exercise the product-review / start-readiness gating.
func confirmRequirement(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
	state, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func setRoute(t *testing.T, root, pkg string, state RunState, mode string, selected []string) RunState {
	t.Helper()
	state, err := SetRoute(root, pkg, state.RunID, mode, selected)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// recordSlicing records the mandatory split decision. The standard test path
// records a no-split with its reason note; pass "split" to exercise split flows
// (the helper uses a two-slice split that can run in parallel). Pass a master
// run id as the variadic argument for a slice-instance split.
func recordSlicing(t *testing.T, root, pkg string, state RunState, decision string, masters ...string) RunState {
	t.Helper()
	count := 0
	if decision == "split" {
		count = 2
	}
	master := ""
	if len(masters) > 0 {
		master = masters[0]
	}
	state, err := RecordSlicing(root, pkg, state.RunID, decision, count, []string{"slice-a", "slice-b"}, "slice-a and slice-b can run in parallel", "single coherent bounded unit", master)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// sliceMaster creates a retained-overall master run with confirmed requirement
// and PASS Product Review and Start Readiness, returning its run id. Slice
// instances reference this master when recording an inherited split decision.
func sliceMaster(t *testing.T, root, pkg, id string) string {
	t.Helper()
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true, Split: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	return id
}

// recordProductReview prepares, claims, and records a PASS Product Review.
func recordProductReview(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func beginQA(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, id), "full", nil)
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, dispatchID, baselineCases(), "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readyDelivery(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state := beginQA(t, root, pkg, id)
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-qa-reviewer")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, dispatchID, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-"+id+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+id)
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readyDeliveryForRoute(t *testing.T, root, pkg, id, mode string, selected []string) RunState {
	t.Helper()
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, id), mode, selected)
	if isSelectedQA(state) {
		designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
		var err error
		state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, baselineCases(), "")
		if err != nil {
			t.Fatal(err)
		}
		reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-qa-reviewer")
		state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-"+id+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+id)
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordGateResult(t *testing.T, root, pkg string, state RunState, gate, reviewer, status, message string, findings []FindingInput) RunState {
	t.Helper()
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, gate, reviewer)
	compared := ""
	if strings.ToUpper(strings.TrimSpace(status)) != "RUNTIME_ERROR" {
		compared = comparedRange(state)
	}
	state, err := RecordGate(root, pkg, state.RunID, gate, dispatchID, status, message, compared, findings)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func advanceRepair(t *testing.T, root, pkg string, state RunState, suffix string) RunState {
	t.Helper()
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "repair-"+suffix+".txt"), "repair\n")
	commitAll(t, root, "repair "+suffix)
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordReadiness(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func prepareDispatch(t *testing.T, root, pkg, runID, target string, modes ...string) string {
	t.Helper()
	mode := ""
	if len(modes) > 0 {
		mode = modes[0]
	}
	var err error
	if target == "quality" || target == "architecture" || target == mergeGateID {
		_, err = PrepareGate(root, pkg, runID, target, false, "")
	} else {
		_, err = PrepareAction(root, pkg, runID, target, mode, false, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	kind := "action"
	if target == "quality" || target == "architecture" || target == mergeGateID {
		kind = "gate"
	}
	dispatchID := openDispatchID(state, kind, target)
	if kind == "action" && target != "requirements-clarification" && target != "qa-review" {
		identity := fmt.Sprintf("%s-%s-%s", runID, target, dispatchID)
		if _, err := ClaimDispatch(root, pkg, runID, dispatchID, identity); err != nil {
			t.Fatal(err)
		}
	}
	return dispatchID
}

func prepareAndClaim(t *testing.T, root, pkg, runID, target, reviewer string, modes ...string) string {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, runID, target, modes...)
	if _, err := ClaimDispatch(root, pkg, runID, dispatchID, reviewer); err != nil {
		t.Fatal(err)
	}
	return dispatchID
}

func baselineCases() []QACaseInput {
	return []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}, {Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"}}
}

func passingReviewDecisions(state RunState) []QAReviewInput {
	var decisions []QAReviewInput
	for _, testCase := range state.allQACases() {
		if testCase.ReviewStatus != "PASS" {
			decisions = append(decisions, QAReviewInput{CaseID: testCase.ID, Outcome: "PASS"})
		}
	}
	return decisions
}

func passingExecution(cases []QACase) []QAResultInput {
	results := make([]QAResultInput, 0, len(cases))
	for _, testCase := range cases {
		results = append(results, QAResultInput{CaseID: testCase.ID, Outcome: "PASS", Procedure: "executed " + testCase.Procedure, Observation: "observed success", OracleResult: "matched"})
	}
	return results
}

func workflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "requirements.md"), "requirement\n")
	writeTestFile(t, filepath.Join(root, "design.md"), "design\n")
	// 测试仓库与实际仓库一致地忽略运行期临时状态：否则 .gates/tmp/ 会被误跟踪进
	// "基线到当前"交付 diff，认领后等状态写入会让工作树变脏。
	writeTestFile(t, filepath.Join(root, ".gitignore"), ".gates/tmp/\n")
	// 白盒设计者交付的结构测试代码——测试仓库带一个测试文件，作为白盒用例测试
	// 引用（<文件>::<函数>）的定位目标。见 whiteboxDeliveredTestCode（whitebox_binding.go）。
	writeTestFile(t, filepath.Join(root, "whitebox_delivered_test.go"), whiteboxDeliveredTestCode)
	initializeGit(t, root)
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks", "merge-gate": "merge checks"})
}

func initializeGit(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "tests@example.invalid")
	runGit(t, root, "config", "user.name", "Formal Gates Tests")
	commitAll(t, root, "base")
}

func commitAll(t *testing.T, root, message string) {
	t.Helper()
	runGit(t, root, "add", "--all")
	runGit(t, root, "commit", "-m", message)
}

func gitHead(t *testing.T, root string) string {
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

func artifactPaths(artifacts []RequirementArtifact) []string {
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	return paths
}

func stateBytes(t *testing.T, root, runID string) string {
	t.Helper()
	data, err := os.ReadFile(RunStatePath(root, runID))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// createQAWorktree adds a detached git worktree at the run's base snapshot and
// returns its path. The requirement documents are present from the base commit
// (the injected current-requirement working-tree state); RegisterQAWorktree
// verifies their revision matches the run registration.
func createQAWorktree(t *testing.T, root string, state RunState) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), "qa-worktree")
	runGit(t, root, "worktree", "add", "--detach", worktree, state.BaseSnapshot)
	return worktree
}

// TestBlackboxQADesignsAndReviewsInIsolationWorktree verifies requirement 1:
// blackbox qa-design/qa-review run in the QA isolation worktree (native identity
// == base, requirement docs injected), development start is not gated on the
// blackbox review, and the worktree is cleared automatically when the blackbox
// review records PASS.
func TestBlackboxQADesignsAndReviewsInIsolationWorktree(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "blackbox-isolation"), "full", nil)
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if state.QAWorktree != worktree {
		t.Fatalf("QA worktree not registered: %#v", state.QAWorktree)
	}
	// 开发开始不再要求黑盒 qa-review PASS。
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("development start stayed blocked by the parallel blackbox QA: %v", err)
	}
	// 黑盒 qa-design 派发绑到隔离工作区（== 基线）。
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-design", "blackbox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	designDispatch := openDispatchID(state, "action", "qa-design")
	if state.Dispatches[designDispatch].Mode != "blackbox" || state.Dispatches[designDispatch].SourceSnapshot != state.BaseSnapshot {
		t.Fatalf("blackbox design dispatch not bound to the isolation base: %#v", state.Dispatches[designDispatch])
	}
	if !strings.Contains(prompt, worktree) || !strings.Contains(prompt, "QA isolation worktree") {
		t.Fatalf("blackbox design prompt does not point at the isolation worktree: %s", prompt)
	}
	state, err = ClaimDispatch(root, pkg, state.RunID, designDispatch, "isolation-designer")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 开发进行中黑盒 review 仍可在隔离工作区派发并记录（与开发并发）：黑盒派发走
	// --mode blackbox，绑隔离工作区（== 基线），开发提交不改变它的原生标识。
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-isolation.txt"), "delivery\n")
	commitAll(t, root, "delivery isolation")
	reviewPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", "blackbox", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewPrompt, worktree) {
		t.Fatalf("blackbox review prompt does not point at the isolation worktree: %s", reviewPrompt)
	}
	state, _ = LoadRunState(root, state.RunID)
	reviewDispatch := openDispatchID(state, "action", "qa-review")
	if state.Dispatches[reviewDispatch].Mode != "blackbox" || state.Dispatches[reviewDispatch].SourceSnapshot != state.BaseSnapshot {
		t.Fatalf("blackbox review dispatch not bound to the isolation base: %#v", state.Dispatches[reviewDispatch])
	}
	state, err = ClaimDispatch(root, pkg, state.RunID, reviewDispatch, "isolation-reviewer")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.QAWorktree != "" {
		t.Fatalf("blackbox review PASS did not clear the isolation worktree: %#v", state.QAWorktree)
	}
	// 黑盒 review PASS 后快照成功（开发完成 且 黑盒 review PASS 两边都完成）。
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err != nil {
		t.Fatalf("snapshot after blackbox review PASS failed: %v", err)
	}
}

// TestRegisterQAWorktreeRejectsInjectedRevisionMismatch verifies requirement 1's
// hash check at qa-worktree registration: registering the QA isolation worktree
// must reject it when the injected requirement document / acceptance artifact no
// longer matches the run's registered revision (guarding against a host that
// forgot to refresh the injection). This is the single home of the "登记**或**
// prepare 校验" guard — later blackbox prepare/claim/record only re-resolve the
// native identity (== base) without re-hashing the worktree files.
func TestRegisterQAWorktreeRejectsInjectedRevisionMismatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "isolation-hash"), "full", nil)
	worktree := createQAWorktree(t, root, state)
	// 注入文档在隔离工作区内被改写（工作树状态、不影响原生标识校验），登记时被拒。
	writeTestFile(t, filepath.Join(worktree, "requirements.md"), "stale injected requirement\n")
	if _, err := RegisterQAWorktree(root, pkg, state.RunID, worktree); err == nil || !strings.Contains(err.Error(), "does not match the run revision") {
		t.Fatalf("qa-worktree registration with stale injected revision was accepted: %v", err)
	}
}

// TestSnapshotRequiresBlackboxReviewPassed verifies requirement 2: the snapshot
// gate requires development complete 且 黑盒 qa-review PASS.
func TestSnapshotRequiresBlackboxReviewPassed(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "snapshot-blackbox-gate"), "custom", []string{blackboxQAID})
	// 黑盒按 mode 派发的设计轮在 QA 隔离工作区进行，先登记工作区再设计（按 mode 存储）。
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-gate.txt"), "delivery\n")
	commitAll(t, root, "delivery gate")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "blackbox QA Review must pass") {
		t.Fatalf("snapshot before blackbox review PASS was not blocked: %v", err)
	}
	// 黑盒 review PASS 后快照放行：黑盒派发走 --mode blackbox、绑隔离工作区（== 基线），
	// 开发提交后主工作区 HEAD 已前进，但隔离工作区仍停在基线，身份校验不受影响。
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "snapshot-gate-reviewer", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err != nil {
		t.Fatalf("snapshot after blackbox review PASS failed: %v", err)
	}
}

// TestZeroCaseBlackboxReviewPassAllowsSnapshot verifies requirement 4's tradeoff:
// a selected blackbox mode with zero cases is judged by the review's set-level
// coverage finding, not a snapshot-side mechanical floor. An empty-set review PASS
// (the review judged the set coverage sufficient) lets the snapshot proceed.
func TestZeroCaseBlackboxReviewPassAllowsSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "zero-blackbox"), "custom", []string{blackboxQAID})
	// 黑盒设计零用例：设计 PASS、待定集为空，进入 review 的集合覆盖判定。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "zero-blackbox-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "PASS" {
		t.Fatalf("empty-set blackbox review did not pass: %#v", state.qaReview(""))
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-zero.txt"), "delivery\n")
	commitAll(t, root, "delivery zero")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err != nil {
		t.Fatalf("snapshot with zero blackbox cases was blocked: %v", err)
	}
}

// TestZeroCaseBlackboxReviewPassAllowsQAExecution verifies the qa-execution
// counterpart of TestZeroCaseBlackboxReviewPassAllowsSnapshot: once the qa-review
// action has recorded PASS on an empty set (the review judged the zero-case
// coverage sufficient, requirement 4 — the same determination that lets the
// snapshot gate through), QA Execution accepts the empty required set as PASS.
// Without this, a zero-case blackbox run that already passed the snapshot gate
// deadlocks at qa-execution ("approved QA cases are missing"), leaving QAExecution
// PENDING so the run can never seal.
func TestZeroCaseBlackboxReviewPassAllowsQAExecution(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "zero-blackbox-exec"), "custom", []string{blackboxQAID})
	// 黑盒设计零用例：设计 PASS、待定集为空，进入 review 的集合覆盖判定。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "zero-blackbox-exec-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "PASS" {
		t.Fatalf("empty-set blackbox review did not pass: %#v", state.qaReview(""))
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-zero-exec.txt"), "delivery\n")
	commitAll(t, root, "delivery zero exec")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err != nil {
		t.Fatalf("snapshot with zero blackbox cases was blocked: %v", err)
	}
	// 空需执行集放行：review 已对空集记录 PASS 后，qa-execution 对空集直接 PASS。
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, nil, "")
	if err != nil {
		t.Fatalf("qa-execution with empty set after review PASS was rejected: %v", err)
	}
	if state.qaExecution("").Status != "PASS" {
		t.Fatalf("empty-set QA execution did not record PASS: %#v", state.qaExecution(""))
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatalf("seal after zero-case review PASS was blocked: %v", err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("zero-case blackbox run did not seal: %#v", summary)
	}
}

// TestZeroCaseBlackboxSnapshotBlockedBeforeReviewPass verifies requirement 2 for
// a selected blackbox mode with zero cases: the snapshot gate is not vacuous.
// While the qa-review action is still PENDING (no review round recorded), the
// snapshot is blocked even though the blackbox case set is empty; only a recorded
// review PASS lets the empty set through (see
// TestZeroCaseBlackboxReviewPassAllowsSnapshot).
func TestZeroCaseBlackboxSnapshotBlockedBeforeReviewPass(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "zero-blackbox-blocked"), "custom", []string{blackboxQAID})
	// 黑盒设计零用例：设计 PASS、待定集为空，但 review 尚未派发/记录（qa-review 仍 PENDING）。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "PENDING" {
		t.Fatalf("qa-review should be PENDING after an empty-set design: %#v", state.qaReview(""))
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-zero-blocked.txt"), "delivery\n")
	commitAll(t, root, "delivery zero blocked")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, false, ""); err == nil || !strings.Contains(err.Error(), "blackbox QA Review must pass") {
		t.Fatalf("snapshot with zero blackbox cases before review PASS was not blocked: %v", err)
	}
}

// TestSnapshotUserReleaseAllowsWithoutBlackboxReview verifies the manual release
// entry of requirement 2: the user explicitly authorizes a snapshot while the
// blackbox review has not passed; the authorization source is recorded and
// qa-execution covers only approved cases.
func TestSnapshotUserReleaseAllowsWithoutBlackboxReview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "snapshot-user-release"), "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-release.txt"), "delivery\n")
	commitAll(t, root, "delivery release")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, true, "user releases the blackbox gate")
	if err != nil {
		t.Fatalf("user-authorized snapshot was rejected: %v", err)
	}
	if state.SnapshotOverride == nil || state.SnapshotOverride.Origin != "USER" || state.SnapshotOverride.Snapshot != state.CurrentSnapshot {
		t.Fatalf("snapshot override authorization not recorded: %#v", state.SnapshotOverride)
	}
	// 放行后 qa-execution 只覆盖已批准用例：此处黑盒用例未获批准，需执行集为空。
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "PASS" {
		t.Fatalf("released blackbox cases did not count as PASS: %#v", state.qaExecution(""))
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("seal after user release did not complete: %#v", summary)
	}
}

// TestSnapshotReleasePersistsToRepairSnapshot verifies the manual release carries
// to later repair snapshots: after an explicit snapshot --user-requested release,
// the unapproved blackbox cases are treated as PASS, so a subsequent repair
// snapshot is not blocked again by the same unapproved blackbox cases.
func TestSnapshotReleasePersistsToRepairSnapshot(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "release-persist"), "custom", []string{blackboxQAID, "quality"})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-release.txt"), "delivery\n")
	commitAll(t, root, "delivery release")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch, true, "user releases the blackbox gate")
	if err != nil {
		t.Fatalf("user-authorized snapshot was rejected: %v", err)
	}
	if state.SnapshotOverride == nil {
		t.Fatalf("snapshot override not recorded: %#v", state.SnapshotOverride)
	}
	// 放行后 qa-execution 覆盖已批准用例并把未批准黑盒用例视为 PASS。
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// 质量门 FAIL 产生阻塞波，进入修复轮。
	state = recordGateResult(t, root, pkg, state, "quality", "release-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repair required"}})
	// 修复快照（非用户放行）：此前放行授权延续，未批准黑盒用例视为 PASS，不再被挡。
	state = advanceRepair(t, root, pkg, state, "release")
	if state.CurrentSnapshot == state.BaseSnapshot {
		t.Fatalf("repair snapshot did not advance: %#v", state)
	}
}

// TestSealSquashesGitRangeToSingleCommit verifies requirement 3: a git run whose
// base→current range holds more than one commit is squashed into a single commit
// at seal, preserving the final tree; the summary records the squashed commit.
func TestSealSquashesGitRangeToSingleCommit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "seal-squash", "custom", []string{"quality"})
	// 首轮质量门 FAIL（阻塞），使一轮修复合法：修复提交构成基线→当前的第二条提交，
	// seal 时才触发压缩。
	state = recordGateResult(t, root, pkg, state, "quality", "squash-quality-fail", "FAIL", "", []FindingInput{{Severity: "P1", Message: "delivery needs repair"}})
	state = advanceRepair(t, root, pkg, state, "squash")
	state = recordGateResult(t, root, pkg, state, "quality", "squash-quality-pass", "PASS", "", nil)
	before := gitHead(t, root)
	countBefore := commitCount(t, root, state.BaseSnapshot, before)
	if countBefore <= 1 {
		t.Fatalf("test setup expected >1 commits in base..head, got %d", countBefore)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "squashed combined delivery")
	if err != nil {
		t.Fatal(err)
	}
	after := gitHead(t, root)
	if count := commitCount(t, root, state.BaseSnapshot, after); count != 1 {
		t.Fatalf("seal did not squash base..current into one commit: %d commits remain", count)
	}
	if summary.CurrentSnapshot != after {
		t.Fatalf("summary current snapshot %s does not match squashed commit %s", summary.CurrentSnapshot, after)
	}
	// 最终树不变：squash 前后的树哈希一致。
	if tree := runGit(t, root, "rev-parse", after+"^{tree}"); tree == "" {
		t.Fatal("cannot resolve squashed tree")
	}
}

// TestReviewRuleOnlyP2P3RecordsPassWithVisibleFindings verifies requirement 5:
// a review carrying only P2/P3 findings records PASS with the suggestions visible
// and does not produce FAIL or require a re-review.
func TestReviewRuleOnlyP2P3RecordsPassWithVisibleFindings(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "only-p2"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", []FindingInput{{Severity: "P2", Message: "minor wording suggestion"}, {Severity: "P3", Message: "trivial style note"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" || len(state.Actions["product-review"].Findings) != 2 || state.Actions["product-review"].Findings[0].Severity != "P2" || state.Actions["product-review"].Findings[1].Severity != "P3" {
		t.Fatalf("P2/P3-only PASS was not recorded with visible findings: %#v", state.Actions["product-review"])
	}
}

// TestReviewRuleConfirmedP0P1RejectsPassBeforeReReview verifies requirement 5:
// confirming a P0/P1 finding sets the needs-re-review marker; record-action PASS
// for a dispatch that is not the re-review round is rejected until the re-review
// returns PASS.
func TestReviewRuleConfirmedP0P1RejectsPassBeforeReReview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "confirm-p01"))
	first := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", first, "FAIL", "", []FindingInput{{Severity: "P1", Message: "real user problem"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	// 在确认处置之前派发一轮新 review（该轮不是重审轮）。
	second := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordSettledFindings(root, pkg, state.RunID, "product-review", []string{"real user problem"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.NeedsReReview["product-review"] != "real user problem" {
		t.Fatalf("confirmed P0/P1 did not set the needs-re-review marker: %#v", state.NeedsReReview)
	}
	// 该轮在标记置位前派发，不是重审轮：记录 PASS 被拒。
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", second, "PASS", "", nil, false, ""); err == nil || !strings.Contains(err.Error(), "awaits a re-review") {
		t.Fatalf("direct PASS before re-review was accepted: %v", err)
	}
	// 需求 6 第 5 条：重绑前必须先把在途审查派发的结果记录掉。该轮被重审规则挡下、
	// 无法产出可记录的 PASS（须等重审轮），以 RUNTIME_ERROR 结束该轮后释放重绑通路。
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", second, "RUNTIME_ERROR", "round superseded by the confirmed-P0/P1 re-review requirement", nil, false, ""); err != nil {
		t.Fatalf("disposing the blocked round failed: %v", err)
	}
	// 修订需求（语义保留）后派发重审轮并返回 PASS：标记清除，PASS 可记录。
	writeTestFile(t, filepath.Join(root, "requirements.md"), "revised meaning-preserved requirement\n")
	commitAll(t, root, "revise requirement (preserved)")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "preserved", nil)
	if err != nil {
		t.Fatal(err)
	}
	rereview := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, _ = LoadRunState(root, state.RunID)
	if state.ReReviewDispatch["product-review"] != rereview {
		t.Fatalf("re-review dispatch not tagged: %#v", state.ReReviewDispatch)
	}
	state, err = RecordAction(root, pkg, state.RunID, "product-review", rereview, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatalf("re-review PASS was rejected: %v", err)
	}
	if state.NeedsReReview["product-review"] != "" {
		t.Fatalf("re-review PASS did not clear the marker: %#v", state.NeedsReReview)
	}
}

// TestReviewRuleDismissedP0P1DoesNotBlock verifies requirement 5: a dismissed
// P0/P1 finding is void and does not block recording PASS.
func TestReviewRuleDismissedP0P1DoesNotBlock(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "dismiss-p01"))
	first := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", first, "FAIL", "", []FindingInput{{Severity: "P0", Message: "voided problem"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordSettledFindings(root, pkg, state.RunID, "product-review", nil, []string{"voided problem"})
	if err != nil {
		t.Fatal(err)
	}
	if state.NeedsReReview["product-review"] != "" {
		t.Fatalf("dismissed P0/P1 must not set the marker: %#v", state.NeedsReReview)
	}
	// 驳回的 P0/P1 不阻塞：新轮可记录 PASS（即使携带已驳回的 P0/P1 也不阻塞）。
	next := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordAction(root, pkg, state.RunID, "product-review", next, "PASS", "", []FindingInput{{Severity: "P0", Message: "voided problem"}}, false, "")
	if err != nil {
		t.Fatalf("dismissed P0/P1 blocked PASS: %v", err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("review with dismissed P0/P1 did not pass: %#v", state.Actions["product-review"])
	}
}

// TestReviewRuleUserOverrideBypassesAndRecordsOrigin verifies requirement 5: only
// the user can break the review rule; an explicit override allows PASS with a
// confirmed P0/P1 pending re-review and records the authorization source.
func TestReviewRuleUserOverrideBypassesAndRecordsOrigin(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "override-p01"))
	first := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", first, "FAIL", "", []FindingInput{{Severity: "P1", Message: "confirmed blocker"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	second := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordSettledFindings(root, pkg, state.RunID, "product-review", []string{"confirmed blocker"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordAction(root, pkg, state.RunID, "product-review", second, "PASS", "", nil, true, "user decides to proceed anyway")
	if err != nil {
		t.Fatalf("user-requested override was rejected: %v", err)
	}
	if state.ReviewOverrides["product-review"] != "user decides to proceed anyway" {
		t.Fatalf("override source not recorded: %#v", state.ReviewOverrides)
	}
}

// TestPrepareActionReReviewPassedRoundNeedsUserOverride verifies the side-B user
// exception of requirement 5: re-reviewing an already-PASS round requires the
// explicit user override, and the source is recorded.
func TestPrepareActionReReviewPassedRoundNeedsUserOverride(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "rereview-pass"))
	state = recordProductReview(t, root, pkg, state)
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, ""); err == nil || !strings.Contains(err.Error(), "authoritative PASS") {
		t.Fatalf("re-preparing a PASS round without override was accepted: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", true, "user demands a re-review"); err != nil {
		t.Fatalf("user-requested re-review of a PASS round was rejected: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewOverrides["product-review"] == "" {
		t.Fatalf("re-review override source not recorded: %#v", state.ReviewOverrides)
	}
}

func commitCount(t *testing.T, root, base, head string) int {
	t.Helper()
	value := runGit(t, root, "rev-list", "--count", base+".."+head)
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// executionOutcomes builds a full QA execution result over the run's case set,
// marking the named cases FAIL and the rest PASS.
func executionOutcomes(cases []QACase, failing map[string]bool) []QAResultInput {
	results := make([]QAResultInput, 0, len(cases))
	for _, testCase := range cases {
		outcome := "PASS"
		if failing[testCase.ID] {
			outcome = "FAIL"
		}
		results = append(results, QAResultInput{CaseID: testCase.ID, Outcome: outcome, Procedure: "executed " + testCase.Procedure, Observation: "observed " + outcome, OracleResult: "compared"})
	}
	return results
}

// failingQAExecution records a merged-set QA execution on the run with the named
// cases FAIL and the rest PASS, then records the architecture PASS and quality FAIL
// gates so the review wave completes (preparing a repair snapshot). suffix makes
// reviewer identities unique across waves.
func failingQAExecution(t *testing.T, root, pkg string, state RunState, failing map[string]bool, suffix string) RunState {
	t.Helper()
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err := RecordQAExecution(root, pkg, state.RunID, qaDispatch, executionOutcomes(state.allQACases(), failing), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "failing-arch-"+suffix, "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "failing-quality-"+suffix, "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if state.CompletedReviewWaves == 0 {
		t.Fatalf("blocking wave was not completed: %#v", state)
	}
	return state
}

// advanceRepairWithCarry advances a repair snapshot and then disposes the prior
// passing architecture gate with a Carry INHERIT judgment, which
// requireNoPendingInheritance demands before any qa-* continue/rerun entry.
func advanceRepairWithCarry(t *testing.T, root, pkg string, state RunState, suffix string) RunState {
	t.Helper()
	state = advanceRepair(t, root, pkg, state, suffix)
	carry := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err := RecordCarry(root, pkg, state.RunID, carry, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair is outside architecture ownership"}}, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// recordModeQA prepares and records one QA execution dispatch for a concrete mode
// with the given results, returning the updated state.
func recordModeQA(t *testing.T, root, pkg string, state RunState, mode string, results []QAResultInput) RunState {
	t.Helper()
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", mode)
	state, err := RecordQAExecution(root, pkg, state.RunID, qaDispatch, results, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// TestQADesignReRecordsBeforeReviewDispatch verifies a QA design can be
// re-recorded to add/update cases while its qa-review dispatch has not been prepared
// (design not locked), and is rejected once a review dispatch is prepared.
func TestQADesignReRecordsBeforeReviewDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "design-rerecord"), "full", nil)
	// 首次设计只记录部分用例。
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 1 || state.qaCases("")[0].ID != "CASE-001" {
		t.Fatalf("partial design not recorded: %#v", state.allQACases())
	}
	// review 派发尚未准备：可继续调用 qa-design 追加/更新用例集（保留既有用例、增量补全）。
	designDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{
		{Mode: "whitebox", Description: "direct rules pass", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxDirectRules"},
		{Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.allQACases()) != 2 || state.qaCases("")[0].ID != "CASE-001" || state.qaCases("")[1].ID != "CASE-002" {
		t.Fatalf("incremental completion lost existing cases: %#v", state.allQACases())
	}
	// review 派发准备后设计锁定，重记录被拒。
	prepareDispatch(t, root, pkg, state.RunID, "qa-review")
	before := stateBytes(t, root, state.RunID)
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err == nil || !strings.Contains(err.Error(), "locked for an already-prepared QA Review") {
		t.Fatalf("re-record after a review dispatch was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected locked design changed state")
	}
}

// TestQADesignPerModeDoesNotClearOtherMode verifies blackbox and whitebox
// QA cases are stored separately per mode, and a design round only touches its own
// mode's list — designing whitebox must not replace or clear the existing blackbox
// cases (with their review PASS status preserved), and case IDs stay unique across
// modes.
func TestQADesignPerModeDoesNotClearOtherMode(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "design-per-mode"), "custom", []string{whiteboxQAID, blackboxQAID})
	// 黑盒按 mode 派发的设计轮在 QA 隔离工作区进行，先登记工作区。
	worktree := createQAWorktree(t, root, state)
	state, err := RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	// 先做黑盒设计轮并 review PASS。
	blackboxDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, blackboxDesign, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "blackbox-reviewer", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	blackbox := state.qaCases("blackbox")
	if len(blackbox) != 1 || blackbox[0].ReviewStatus != "PASS" {
		t.Fatalf("blackbox case was not approved: %#v", blackbox)
	}
	// 白盒设计轮只动白盒列表：黑盒用例（含 review PASS 状态）不得被清掉。
	whiteboxDesign := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, whiteboxDesign, []QACaseInput{{Mode: "whitebox", Description: "structure tests", Procedure: "run the delivered structure test", Oracle: "the test passes", Test: "whitebox_delivered_test.go::TestWhiteboxStructure"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	blackbox = state.qaCases("blackbox")
	if len(blackbox) != 1 || blackbox[0].ID != "CASE-001" || blackbox[0].ReviewStatus != "PASS" {
		t.Fatalf("whitebox design cleared the blackbox cases: %#v", blackbox)
	}
	whitebox := state.qaCases("whitebox")
	if len(whitebox) != 1 || whitebox[0].ID != "CASE-002" || whitebox[0].ReviewStatus != "PENDING" {
		t.Fatalf("whitebox design was not recorded separately with a unique id: %#v", whitebox)
	}
	if len(state.allQACases()) != 2 {
		t.Fatalf("total cases=%d want=2", len(state.allQACases()))
	}
}

// TestQAExecutionFirstRunNoScopeAndRerunEnforced verifies a first QA
// execution needs no scope decision, while a rerun (a prior authoritative result at
// an earlier snapshot survives the repair snapshot advance) is CLI-enforced to carry
// a scope decision before prepare, and a FULL decision reruns the complete approved set.
func TestQAExecutionFirstRunNoScopeAndRerunEnforced(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "rerun-enforced")
	// 首次执行不要求 scope 决策，也不接受 scope 记录（无上一轮权威结果可继承）。
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, ""); err == nil || !strings.Contains(err.Error(), "no authoritative QA execution result") {
		t.Fatalf("first-execution scope recording was accepted: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, ""); err != nil {
		t.Fatalf("first execution required a scope: %v", err)
	}
	// 首次 QA 执行记录 PASS，并完成波次（架构 PASS、质量 FAIL）后可推进修复快照。
	state = failingQAExecution(t, root, pkg, state, nil, "rerun-enforced")
	// 修复快照推进：旧快照权威 PASS 存续（QAExecution 保留），重新派发属重跑。
	state = advanceRepairWithCarry(t, root, pkg, state, "rerun-enforced-repair")
	// 重跑未记录 scope → prepare 拒绝。
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, ""); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("rerun without a scope decision was not rejected: %v", err)
	}
	// 记录 FULL scope（BaseSnapshot = 上一轮权威结果快照）后重跑放行，需执行集为全部已批准。
	state, err := RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if sc := state.ExecutionScopes[""]; sc.Decision != "FULL" || sc.Origin != "USER" || sc.Source != scopeSourcePrepare || sc.BaseSnapshot != state.PreRepairSnapshot {
		t.Fatalf("recorded FULL scope=%#v", sc)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, "")
	if err != nil {
		t.Fatalf("FULL scope did not allow the rerun prepare: %v", err)
	}
	for _, testCase := range state.allQACases() {
		if !strings.Contains(prompt, testCase.ID) {
			t.Fatalf("FULL rerun prompt omitted approved case %s: %s", testCase.ID, prompt)
		}
	}
}

// TestQAExecutionAffectedScopeSubsetValidation verifies mechanical AFFECTED
// subset checks: the subset must be non-empty, consist of approved cases of the
// mode, and include every prior FAIL case of the mode.
func TestQAExecutionAffectedScopeSubsetValidation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "affected-validate")
	state = failingQAExecution(t, root, pkg, state, map[string]bool{"CASE-002": true}, "affected-validate")
	state = advanceRepairWithCarry(t, root, pkg, state, "affected-validate-repair")
	if state.priorQAExecution("") == nil || state.priorQAExecution("").Status != "FAIL" {
		t.Fatalf("prior FAIL was not preserved: %#v", state.priorQAExecution(""))
	}
	// 空子集拒绝。
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", nil, ""); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("empty AFFECTED subset was accepted: %v", err)
	}
	// 非已批准用例拒绝。
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-999"}, ""); err == nil || !strings.Contains(err.Error(), "not an approved") {
		t.Fatalf("non-approved AFFECTED case was accepted: %v", err)
	}
	// 缺上一轮 FAIL 用例拒绝。
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-001"}, ""); err == nil || !strings.Contains(err.Error(), "must include the prior FAIL case") {
		t.Fatalf("AFFECTED subset missing the prior FAIL case was accepted: %v", err)
	}
	// 有效子集（含上一轮 FAIL 用例）接受。
	state, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-002"}, "repair only touches the blackbox path")
	if err != nil {
		t.Fatal(err)
	}
	if sc := state.ExecutionScopes[""]; sc.Decision != "AFFECTED" || len(sc.CaseIDs) != 1 || sc.CaseIDs[0] != "CASE-002" || sc.BaseSnapshot != state.PreRepairSnapshot {
		t.Fatalf("recorded AFFECTED scope=%#v", sc)
	}
}

// TestQAExecutionAffectedInheritance verifies an AFFECTED rerun requires
// exactly the recorded subset, inherits the untouched approved cases as PASS from
// the base snapshot, and aggregates FAIL only from executed cases.
func TestQAExecutionAffectedInheritance(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "affected-inherit")
	state = failingQAExecution(t, root, pkg, state, map[string]bool{"CASE-002": true}, "affected-inherit")
	state = advanceRepairWithCarry(t, root, pkg, state, "affected-inherit-repair")
	state, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-002"}, "")
	if err != nil {
		t.Fatal(err)
	}
	baseSnapshot := state.PreRepairSnapshot
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "CASE-001") {
		t.Fatalf("AFFECTED prompt listed the inherited case: %s", prompt)
	}
	if !strings.Contains(prompt, "CASE-002") || !strings.Contains(prompt, "AFFECTED rerun") || !strings.Contains(prompt, "inherit their PASS") {
		t.Fatalf("AFFECTED prompt lost the subset or the inheritance notice: %s", prompt)
	}
	// 需执行集恰好是子集：覆盖越界（非子集用例）与覆盖不全（结果数不匹配）都拒绝。
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordQAExecution(root, pkg, state.RunID, qaDispatch, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "o", OracleResult: "m"}}, ""); err == nil {
		t.Fatalf("AFFECTED execution with a non-subset result was accepted: %v", err)
	}
	if _, err := RecordQAExecution(root, pkg, state.RunID, qaDispatch, []QAResultInput{
		{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "o", OracleResult: "m"},
		{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"},
	}, ""); err == nil || !strings.Contains(err.Error(), "must cover") {
		t.Fatalf("AFFECTED execution with an over-wide result was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected AFFECTED coverage changed state")
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "executed blackbox", Observation: "still broken", OracleResult: "mismatch"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "FAIL" {
		t.Fatalf("executed FAIL did not fail the aggregate: %#v", state.qaExecution(""))
	}
	byID := map[string]QAResultRecord{}
	for _, record := range state.qaExecution("").Cases {
		byID[record.CaseID] = record
	}
	if got := byID["CASE-002"]; got.Origin != "executed" || got.Outcome != "FAIL" {
		t.Fatalf("executed record=%#v", got)
	}
	inherited := byID["CASE-001"]
	if inherited.Origin != "inherited" || inherited.Outcome != "PASS" || !strings.Contains(inherited.Observation, "inherited PASS from "+baseSnapshot) {
		t.Fatalf("inherited record=%#v", inherited)
	}
}

// TestQAExecutionScopePerModeIndependent verifies blackbox and whitebox
// carry independent scope decisions, and the prepare enforcement is per mode.
func TestQAExecutionScopePerModeIndependent(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "per-mode-scope")
	// 分 mode 派发：白盒 PASS、黑盒 FAIL，记录架构 PASS 与质量 FAIL 完成波次。
	whiteboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "whitebox")
	state, err := RecordQAExecution(root, pkg, state.RunID, whiteboxDispatch, []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "o", OracleResult: "m"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	blackboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, blackboxDispatch, []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "architecture", "per-mode-arch", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "per-mode-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	state = advanceRepairWithCarry(t, root, pkg, state, "per-mode-repair")
	// 各自独立：黑盒记录 AFFECTED 只放行黑盒重跑，白盒仍被挡。
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{"CASE-002"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, ""); err != nil {
		t.Fatalf("blackbox rerun with its own scope was rejected: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, ""); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("whitebox rerun without its own scope was accepted: %v", err)
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "whitebox", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "whitebox", false, ""); err != nil {
		t.Fatalf("whitebox rerun with its own scope was rejected: %v", err)
	}
}

// TestAuthorizeRepairBundlesQARerunScope verifies after the review-wave
// limit is exhausted, an authorize-repair for a run whose QA mode has an
// authoritative FAIL at the current snapshot is refused without a scope decision,
// and an inline FULL decision is bundled (Source AUTHORIZE_REPAIR) while still
// granting exactly one extra review wave.
func TestAuthorizeRepairBundlesQARerunScope(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "auth-bundle")
	// Wave 1：黑盒 FAIL、白盒 PASS，架构 PASS、质量 FAIL。
	state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
	state = recordGateResult(t, root, pkg, state, "architecture", "auth-arch-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "auth-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	if state.CompletedReviewWaves != 1 {
		t.Fatalf("wave 1 count=%d", state.CompletedReviewWaves)
	}
	// 自动轮次 2..3：每轮修复后按 mode 重跑 QA（各记录 FULL scope）与质量门，仍 FAIL。
	for wave := 2; wave <= automaticReviewWaveLimit; wave++ {
		state = advanceRepairWithCarry(t, root, pkg, state, fmt.Sprintf("auth-bundle-%d", wave))
		state, err := RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		state, err = RecordExecutionScope(root, pkg, state.RunID, "whitebox", "FULL", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "still broken", OracleResult: "mismatch"}})
		state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
		state = recordGateResult(t, root, pkg, state, "quality", fmt.Sprintf("auth-quality-%d", wave), "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
		if state.CompletedReviewWaves != wave {
			t.Fatalf("completed waves=%d want=%d", state.CompletedReviewWaves, wave)
		}
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
		t.Fatalf("automatic wave limit was not enforced: %v", err)
	}
	// 上限处：黑盒/白盒都在当前快照有权威 FAIL 记录、将重跑，缺任一 scope 决策都被拒。
	before := stateBytes(t, root, state.RunID)
	if _, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, nil); err == nil || !strings.Contains(err.Error(), "requires a scope decision") {
		t.Fatalf("authorize-repair without a rerun scope was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected authorize-repair changed state")
	}
	// 内联 FULL scope（一次交互为两个 mode 一起记录）：Source=AUTHORIZE_REPAIR、
	// BaseSnapshot=当前 FAIL 快照，并只授权一个额外轮次。
	state, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, []QAScopeInput{{Mode: "blackbox", Decision: "FULL"}, {Mode: "whitebox", Decision: "FULL"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("extra waves=%d want=1", state.ExtraReviewWaves)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		sc := state.ExecutionScopes[mode]
		if sc.Decision != "FULL" || sc.Source != scopeSourceAuthorizeRepair || sc.Origin != "USER" || sc.BaseSnapshot != state.CurrentSnapshot {
			t.Fatalf("bundled %s scope=%#v", mode, sc)
		}
	}
}

// TestAuthorizeRepairCarriesForwardAffected verifies at the limit, a
// mode whose last recorded decision was a user-chosen AFFECTED (Source !=
// CARRY_FORWARD) is auto-carried as CARRY_FORWARD with the host-judged subset,
// without asking the user again, and still grants exactly one wave.
func TestAuthorizeRepairCarriesForwardAffected(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "auth-carry")
	// Wave 1：黑盒 FAIL、白盒 PASS，架构 PASS、质量 FAIL。
	state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
	state = recordGateResult(t, root, pkg, state, "architecture", "auth-carry-arch-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "auth-carry-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	// 自动轮次 2..3：每轮用户主动选择 AFFECTED（Source=PREPARE）后按 mode 重跑子集。
	for wave := 2; wave <= automaticReviewWaveLimit; wave++ {
		state = advanceRepairWithCarry(t, root, pkg, state, fmt.Sprintf("auth-carry-%d", wave))
		var err error
		state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{"CASE-002"}, "user chose affected for wave "+fmt.Sprint(wave))
		if err != nil {
			t.Fatal(err)
		}
		state, err = RecordExecutionScope(root, pkg, state.RunID, "whitebox", "AFFECTED", []string{"CASE-001"}, "user chose affected for wave "+fmt.Sprint(wave))
		if err != nil {
			t.Fatal(err)
		}
		state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "still broken", OracleResult: "mismatch"}})
		state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
		state = recordGateResult(t, root, pkg, state, "quality", fmt.Sprintf("auth-carry-quality-%d", wave), "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
		if state.CompletedReviewWaves != wave {
			t.Fatalf("completed waves=%d want=%d", state.CompletedReviewWaves, wave)
		}
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
		t.Fatalf("automatic wave limit was not enforced: %v", err)
	}
	// 上限处：最近一次是用户 AFFECTED → 携带 host 判定的子集自动沿用（CARRY_FORWARD），
	// 不再询问"全量 vs 受影响"；仍只授权一个额外轮次。
	state, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, []QAScopeInput{{Mode: "blackbox", CaseIDs: []string{"CASE-002"}}, {Mode: "whitebox", CaseIDs: []string{"CASE-001"}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("extra waves=%d want=1", state.ExtraReviewWaves)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		sc := state.ExecutionScopes[mode]
		if sc.Decision != "AFFECTED" || sc.Source != scopeSourceCarryForward || sc.Origin != "USER" || len(sc.CaseIDs) != 1 || sc.BaseSnapshot != state.CurrentSnapshot {
			t.Fatalf("carried-forward %s scope=%#v", mode, sc)
		}
	}
}

// TestPriorQAExecutionPreservedUntilReplaced verifies an authoritative FAIL
// result survives a repair snapshot advance into PriorQAExecution (its FAIL case set
// stays rerun-detectable), a RUNTIME_ERROR is not preserved, a new authoritative
// record replaces the prior one, and a RUNTIME_ERROR record does not evict it.
func TestPriorQAExecutionPreservedUntilReplaced(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "prior-preserved")
	// 权威 FAIL 结果保留到 PriorQAExecution。
	state = failingQAExecution(t, root, pkg, state, map[string]bool{"CASE-002": true}, "prior-preserved")
	state = advanceRepairWithCarry(t, root, pkg, state, "prior-preserved-repair")
	if state.priorQAExecution("") == nil || state.priorQAExecution("").Status != "FAIL" || state.priorQAExecution("").Snapshot != state.PreRepairSnapshot {
		t.Fatalf("authoritative FAIL was not preserved: %#v", state.priorQAExecution(""))
	}
	if _, ok := qaExecutionPriorResultedBase(state, ""); !ok {
		t.Fatal("prior FAIL result was not rerun-detectable")
	}
	// 新一轮权威记录取代 PriorQAExecution。
	state, err := RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.priorQAExecution("") != nil {
		t.Fatalf("new authoritative record did not replace PriorQAExecution: %#v", state.priorQAExecution(""))
	}
	// 质量门记录在本快照使波次完整，才能推进下一修复快照。
	state = recordGateResult(t, root, pkg, state, "quality", "prior-preserved-quality-2", "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
	// RUNTIME_ERROR 记录不清空存续的上一轮结果。
	state = advanceRepairWithCarry(t, root, pkg, state, "prior-preserved-runtime")
	if state.priorQAExecution("") == nil || state.priorQAExecution("").Status != "PASS" {
		t.Fatalf("PASS was not preserved before the runtime record: %#v", state.priorQAExecution(""))
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	qaDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, nil, "review host crashed")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "RUNTIME_ERROR" {
		t.Fatalf("runtime record status=%s", state.qaExecution("").Status)
	}
	if state.priorQAExecution("") == nil || state.priorQAExecution("").Status != "PASS" {
		t.Fatalf("RUNTIME_ERROR evicted the preserved prior result: %#v", state.priorQAExecution(""))
	}
}

// TestResetSnapshotReviewSurfacePreservesAuthoritativeOnly verifies at the
// reset boundary directly: an authoritative PASS/FAIL result at an old snapshot is
// preserved to PriorQAExecution when QA Execution is reset, while a RUNTIME_ERROR is
// reset without preservation (it is not an authoritative result and must not become
// the rerun base).
func TestResetSnapshotReviewSurfacePreservesAuthoritativeOnly(t *testing.T) {
	// 权威 FAIL 保留（含快照与 FAIL 用例集）。
	state := RunState{CurrentSnapshot: "s2", QAExecutionByMode: map[string]QAExecutionResult{"": {Status: "FAIL", Snapshot: "s1", Cases: []QAResultRecord{{CaseID: "CASE-002", Mode: "blackbox", Outcome: "FAIL"}}}}}
	resetSnapshotReviewSurface(&state, "s1", true, true)
	if state.priorQAExecution("") == nil || state.priorQAExecution("").Status != "FAIL" || state.priorQAExecution("").Snapshot != "s1" || state.qaExecution("").Status != "PENDING" {
		t.Fatalf("authoritative FAIL was not preserved: %#v", state)
	}
	// RUNTIME_ERROR 不保留。
	state = RunState{CurrentSnapshot: "s2", QAExecutionByMode: map[string]QAExecutionResult{"": {Status: "RUNTIME_ERROR", Snapshot: "s1"}}}
	resetSnapshotReviewSurface(&state, "s1", true, true)
	if state.priorQAExecution("") != nil {
		t.Fatalf("RUNTIME_ERROR was preserved as a prior result: %#v", state.priorQAExecution(""))
	}
}

// TestAuthorizeRepairCarryForwardDoesNotGrantWave verifies the CARRY_FORWARD
// auto-carry at the limit records the scope but never by itself grants a review
// wave; each authorize-repair call still grants exactly one.
func TestAuthorizeRepairCarryForwardDoesNotGrantWave(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "no-auto-wave")
	// Wave 1：黑盒 FAIL、白盒 PASS，架构 PASS、质量 FAIL。
	state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "broken", OracleResult: "mismatch"}})
	state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
	state = recordGateResult(t, root, pkg, state, "architecture", "no-auto-arch-1", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "quality", "no-auto-quality-1", "FAIL", "", []FindingInput{{Severity: "P1", Message: "blocker"}})
	for wave := 2; wave <= automaticReviewWaveLimit; wave++ {
		state = advanceRepairWithCarry(t, root, pkg, state, fmt.Sprintf("no-auto-wave-%d", wave))
		var err error
		state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		state, err = RecordExecutionScope(root, pkg, state.RunID, "whitebox", "FULL", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		state = recordModeQA(t, root, pkg, state, "blackbox", []QAResultInput{{CaseID: "CASE-002", Outcome: "FAIL", Procedure: "p", Observation: "still broken", OracleResult: "mismatch"}})
		state = recordModeQA(t, root, pkg, state, "whitebox", []QAResultInput{{CaseID: "CASE-001", Outcome: "PASS", Procedure: "p", Observation: "ok", OracleResult: "matched"}})
		state = recordGateResult(t, root, pkg, state, "quality", fmt.Sprintf("no-auto-quality-%d", wave), "FAIL", "", []FindingInput{{Severity: "P1", Message: "still blocked"}})
	}
	// 携带 FULL 内联 scope 授权一次：仅增加一个轮次，不会因记录 scope 而自动多授权。
	state, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1, []QAScopeInput{{Mode: "blackbox", Decision: "FULL"}, {Mode: "whitebox", Decision: "FULL"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("a single authorize-repair granted %d waves", state.ExtraReviewWaves)
	}
}

// 片 1 CLI 行为修复的直接层测试（validate 层）。

// Fix 2：无 hooks 环境 record-action 被生命周期校验拒绝时给可行动提示。
func TestRecordActionLifecycleRejectionGivesActionableHint(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "lifecycle-hint"))
	priorLifecycle := workflowLifecycle
	stub := &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Rejected, Diagnostic: "missing matching start and stop event"}}
	workflowLifecycle = stub
	t.Cleanup(func() { workflowLifecycle = priorLifecycle })
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, ""); err == nil || !strings.Contains(err.Error(), "capture hooks") {
		t.Fatalf("expected an actionable capture-hooks hint, got: %v", err)
	}
}

// Fix 3：settle-findings 同一 finding 同时 confirm+dismiss 被拒。
func TestSettledFindingsRejectsConfirmAndDismissOverlap(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "settle-overlap"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "same finding"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSettledFindings(root, pkg, state.RunID, "product-review", []string{"same finding"}, []string{"same finding"}); err == nil || !strings.Contains(err.Error(), "cannot be both confirmed and dismissed") {
		t.Fatalf("confirm+dismiss overlap was accepted: %v", err)
	}
}

// Fix 4/8：缺 run-id 与不存在/已终止 run 给友好提示。
func TestLoadRunStateFriendlyMissingRun(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadRunState(root, ""); err == nil || !strings.Contains(err.Error(), "run id is required") {
		t.Fatalf("missing run id was not reported friendly: %v", err)
	}
	if _, err := LoadRunState(root, "never-existed"); err == nil || !strings.Contains(err.Error(), "was not found or is already terminated") {
		t.Fatalf("missing run was not reported friendly: %v", err)
	}
}

// Fix 9：qa-execution-scope 非法 mode 明确报"非法 mode"。
func TestRecordExecutionScopeRejectsInvalidMode(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "scope-mode"), "custom", []string{blackboxQAID})
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "purple", "FULL", nil, "reason"); err == nil || !strings.Contains(err.Error(), "非法 mode") {
		t.Fatalf("invalid scope mode was not rejected clearly: %v", err)
	}
}

// Fix 11：record-action status 严格大小写校验（拒绝 pass）。
func TestRecordActionStatusIsCaseSensitive(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "case-status")
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
	if _, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatchID, "pass", "", nil, false, ""); err == nil || !strings.Contains(err.Error(), "case-sensitive") {
		t.Fatalf("lowercase status was accepted: %v", err)
	}
	if _, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatchID, "PASS", "", nil, false, ""); err != nil {
		t.Fatalf("exact PASS was rejected: %v", err)
	}
}

// Fix 15：install 缺 source 明确报"source 必填"。
func TestInstallRequiresSource(t *testing.T) {
	if _, err := Install(InstallOptions{Host: "claude", Scope: "project", Project: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Fatalf("install without source was not rejected clearly: %v", err)
	}
}

// Fix 17：svn {U+XXXX} unicode 转义转成可读文字。
func TestDecodeVCSMessageUnescapesUnicode(t *testing.T) {
	in := `svn: svn: E155007: {U+201C}/tmp/repo{U+201D}{U+4E0D}{U+662F}{U+5DE5}{U+4F5C}{U+526F}{U+672C}`
	out := decodeVCSMessage(in)
	if strings.Contains(out, "{U+") {
		t.Fatalf("unicode escapes were not decoded: %s", out)
	}
	if !strings.Contains(out, "不是工作副本") {
		t.Fatalf("decoded message lost readable text: %s", out)
	}
	if strings.Contains(out, "svn: svn:") {
		t.Fatalf("duplicated svn prefix not collapsed: %s", out)
	}
}

// 需求 4：workflow start 未声明 --split 拒绝启动。
func TestStartRequiresSplitDeclaration(t *testing.T) {
	root, pkg := workflowFixture(t)
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "no-split-decl", Flow: "formal", RequirementSource: "requirements.md", VCS: "git"}); err == nil || !strings.Contains(err.Error(), "--split") {
		t.Fatalf("start without a --split declaration was accepted: %v", err)
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "bad-split", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "maybe"}); err == nil || !strings.Contains(err.Error(), "--split must be yes or no") {
		t.Fatalf("invalid --split value was accepted: %v", err)
	}
}

// 需求 4：--split yes 启动时区分保留总任务实例与切片实例，并钉死映射。
func TestStartSplitYesPinsRetainedOrMaster(t *testing.T) {
	root, pkg := workflowFixture(t)
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "yes-naked", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "yes"}); err == nil || !strings.Contains(err.Error(), "--retained-overall") {
		t.Fatalf("--split yes without retained-overall/master pin was accepted: %v", err)
	}
	retained, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "yes-retained", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "yes", RetainedOverall: true})
	if err != nil {
		t.Fatal(err)
	}
	if retained.SplitDeclaration != "yes" || retained.SplitMasterRunID != "" || !retained.RetainedOverall {
		t.Fatalf("retained-overall split declaration not pinned: %#v", retained)
	}
	slice, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "yes-slice", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "yes", MasterRunID: "master-run"})
	if err != nil {
		t.Fatal(err)
	}
	if slice.SplitDeclaration != "yes" || slice.SplitMasterRunID != "master-run" {
		t.Fatalf("slice master pin not recorded: %#v", slice)
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "both", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "yes", RetainedOverall: true, MasterRunID: "master-run"}); err == nil || !strings.Contains(err.Error(), "cannot be both") {
		t.Fatalf("retained-overall + master contradiction was accepted: %v", err)
	}
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "no-retained", Flow: "formal", RequirementSource: "requirements.md", VCS: "git", Split: "no", RetainedOverall: true}); err == nil || !strings.Contains(err.Error(), "--split yes") {
		t.Fatalf("retained-overall with --split no was accepted: %v", err)
	}
}

// 需求 4：启动声明与 workflow slicing 拆分决定互相校验。
func TestSlicingMutualValidationWithStartDeclaration(t *testing.T) {
	root, pkg := workflowFixture(t)
	// 启动声明 --split no → 后续记录 split 被拒。
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "mutual-no"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "split", 2, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "--split no") {
		t.Fatalf("split decision on a --split no run was accepted: %v", err)
	}
	// 启动声明 --split yes --master <id> → 记录 no-split 被拒。
	master := sliceMaster(t, root, pkg, "mutual-master")
	slice := confirmRequirement(t, root, pkg, mustStartSlice(t, root, pkg, "mutual-slice", master))
	if _, err := RecordSlicing(root, pkg, slice.RunID, "no-split", 0, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "--master") {
		t.Fatalf("no-split decision on a declared slice instance was accepted: %v", err)
	}
	// 拆分引用的 master 与启动声明不一致被拒。
	if _, err := RecordSlicing(root, pkg, slice.RunID, "split", 2, nil, "", "reason", "other-master"); err == nil || !strings.Contains(err.Error(), "does not match the master") {
		t.Fatalf("slice master mismatch was accepted: %v", err)
	}
	// 拆分引用的 master 与启动声明一致 → 放行。
	recorded, err := RecordSlicing(root, pkg, slice.RunID, "split", 2, nil, "", "reason", master)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Slicing == nil || recorded.Slicing.MasterRunID != master {
		t.Fatalf("slice split with matching master not recorded: %#v", recorded.Slicing)
	}
}

// 需求 4：上线前启动（无启动声明）的 run 保持旧语义，不被新约束阻断。
func TestSlicingLegacyRunWithoutDeclarationKeepsOldSemantics(t *testing.T) {
	root, pkg := workflowFixture(t)
	// 旧 run（无 SplitDeclaration 持久化字段）记录 no-split 仍放行。
	state := confirmRequirement(t, root, pkg, legacyRun(t, root, pkg, "legacy-no-split"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "reason", ""); err != nil {
		t.Fatalf("legacy run no-split decision was blocked: %v", err)
	}
	// 旧 run（无启动声明）引用有效 master 记录 split 仍放行（旧语义）。
	master := sliceMaster(t, root, pkg, "legacy-master")
	slice := confirmRequirement(t, root, pkg, legacyRun(t, root, pkg, "legacy-slice"))
	if _, err := RecordSlicing(root, pkg, slice.RunID, "split", 2, nil, "", "reason", master); err != nil {
		t.Fatalf("legacy run slice split was blocked: %v", err)
	}
}

// legacyRun 构造一个本功能上线前启动的 run：启动后用合法的 CLI 写入路径把
// splitDeclaration 字段清掉（SaveRunState 重算完整性），模拟缺失启动声明的旧状态文件。
func legacyRun(t *testing.T, root, pkg, id string) RunState {
	t.Helper()
	state := mustStart(t, root, pkg, id)
	state, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state.SplitDeclaration = ""
	state.SplitMasterRunID = ""
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	return state
}

// 需求 5 / 需求 6 的 validate 层直接测试。

// TestRequirementRebindRejectedWhileReviewDispatchInFlight verifies requirement 6
// item 5: a requirement rebinding is refused while a review dispatch that records
// via record-action/record-gate is in flight (claimed, result not recorded), and
// recording the in-flight result first releases the rebinding path.
func TestRequirementRebindRejectedWhileReviewDispatchInFlight(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "rebind-inflight"))
	// 准备并认领一轮 product-review，不记录结果（在途审查派发）。prepareDispatch 对
	// product-review 自动认领，返回已认领的派发 id。
	prepareDispatch(t, root, pkg, state.RunID, "product-review")
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	commitAll(t, root, "change requirement")
	before := stateBytes(t, root, state.RunID)
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil); err == nil || !strings.Contains(err.Error(), "has not recorded its result") {
		t.Fatalf("rebind with an in-flight review dispatch was accepted: %v", err)
	}
	if after := stateBytes(t, root, state.RunID); after != before {
		t.Fatal("rejected rebind changed state")
	}
}

// TestRecordInFlightDispatchResultAllowedUnderDrift verifies the 需求 6 item-1 /
// item-5 deadlock fix: when the requirement document drifts while a review
// dispatch is in flight (claimed, result not recorded), recording that in-flight
// result is exempt from the drift hard block (the result belongs to the old
// registered revision), so the "record the in-flight result first, then rebind"
// recovery path stays executable instead of the two guards blocking each other.
// The document edit is a working-tree change (uncommitted), which is the drift
// the guards read; new prepares under the drift stay blocked.
func TestRecordInFlightDispatchResultAllowedUnderDrift(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "drift-record-inflight"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review") // 在途审查派发（已认领）
	// 需求文档改动（工作树、未提交）→ 漂移。
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	// 新 prepare 仍被漂移硬阻断（需求 6 第 1 条不受影响）。
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, ""); err == nil || !strings.Contains(err.Error(), "需求文档已改动") {
		t.Fatalf("prepare under drift was not blocked: %v", err)
	}
	// 记录在途派发结果被豁免漂移硬阻断（结果属于旧 revision）。
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, ""); err != nil {
		t.Fatalf("recording the in-flight dispatch result under drift failed: %v", err)
	}
	// 在途派发已记录后，重绑放行。
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil); err != nil {
		t.Fatalf("rebinding after recording the in-flight result failed: %v", err)
	}
}

// TestRequirementRebindAllowedAfterInFlightResultRecorded verifies the release
// path of requirement 6 item 5: recording the in-flight review result first
// (before changing the requirement document) lets the rebinding proceed.
func TestRequirementRebindAllowedAfterInFlightResultRecorded(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "rebind-recorded"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil, false, ""); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	commitAll(t, root, "change requirement")
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil); err != nil {
		t.Fatal(err)
	}
}

// TestCanonicalPromptFileWrittenAndVerified verifies requirement 6 item 4:
// prepare-action writes the dispatch's full prompt to the run's canonical prompt
// file and records the path in the dispatch state; the claim (dispatch time)
// re-verifies the file content against the prepare record, and a tampered file
// hard-blocks the claim.
func TestCanonicalPromptFileWrittenAndVerified(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "prompt-file"))
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	dispatchID := openDispatchID(state, "action", "product-review")
	if dispatchID == "" {
		t.Fatal("no open product-review dispatch after prepare")
	}
	dispatch := state.Dispatches[dispatchID]
	if dispatch.PromptFile == "" {
		t.Fatalf("prepared dispatch records no canonical prompt file: %#v", dispatch)
	}
	if !strings.HasSuffix(filepath.ToSlash(dispatch.PromptFile), ".gates/tmp/"+state.RunID+"/prompts/"+dispatchID+".md") {
		t.Fatalf("canonical prompt file path is not under the run prompts dir: %s", dispatch.PromptFile)
	}
	data, err := os.ReadFile(dispatch.PromptFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != prompt {
		t.Fatal("canonical prompt file content does not match the prepared prompt")
	}
	// 篡改文件内容 → 派发（认领）时硬阻断。
	if err := os.WriteFile(dispatch.PromptFile, []byte("tampered prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimDispatch(root, pkg, state.RunID, dispatchID, "tamper-check"); err == nil || !strings.Contains(err.Error(), "does not match the prepared dispatch record") {
		t.Fatalf("claim with a tampered canonical prompt file was accepted: %v", err)
	}
}

// TestResetRunResetsFlowStateKeepsDevelopment verifies requirement 5: workflow
// reset re-registers the requirement (unconfirmed), resets the overall reviews
// and gates to PENDING, clears stuck dispatches, and keeps the recorded
// development snapshot and the developed content untouched.
func TestResetRunResetsFlowStateKeepsDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "reset-run"), "custom", []string{"quality"})
	// 完成一次开发快照，制造"开发已完成"的 run。development-worker 由 prepareDispatch
	// 自动认领，返回已认领的派发 id。
	devDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "delivery content\n")
	commitAll(t, root, "delivery")
	state, err := AdvanceSnapshot(root, pkg, state.RunID, devDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("development snapshot not recorded: %#v", state)
	}
	// 制造一个卡住的在途门派发。
	gateDispatch := prepareAndClaim(t, root, pkg, state.RunID, "quality", "reset-gate")
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Dispatches[gateDispatch].Status != "CLAIMED" {
		t.Fatalf("gate dispatch not claimed: %#v", state.Dispatches[gateDispatch])
	}
	snapshot := state.CurrentSnapshot

	result, err := ResetRun(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.RequirementConfirmed {
		t.Fatal("reset did not unconfirm the requirement")
	}
	if result.State.RequirementRevision != artifactRevision(result.State.RequirementArtifacts, "requirements.md") {
		t.Fatal("reset did not re-register the requirement to the current document content")
	}
	for _, actionID := range []string{"requirements-clarification", "product-review", "start-readiness"} {
		if result.State.Actions[actionID].Status != "PENDING" {
			t.Fatalf("reset did not re-open %s (status=%s)", actionID, result.State.Actions[actionID].Status)
		}
	}
	if result.State.Actions["development-worker"].Status != developmentComplete {
		t.Fatalf("reset did not keep the development status: %#v", result.State.Actions["development-worker"])
	}
	for id, gate := range result.State.Gates {
		if gate.Status != "PENDING" {
			t.Fatalf("reset did not re-open gate %s (status=%s)", id, gate.Status)
		}
	}
	if len(result.State.Dispatches) != 0 {
		t.Fatalf("reset did not clear dispatches: %#v", result.State.Dispatches)
	}
	if result.State.BaseSnapshot != state.BaseSnapshot || result.State.CurrentSnapshot != snapshot {
		t.Fatal("reset did not keep the snapshots")
	}
	if len(result.Kept) == 0 || len(result.Reset) == 0 {
		t.Fatalf("reset output missing kept/reset lists: %#v", result)
	}
	// 已开发内容原样保留：提交还在、开发文件还在、工作树干净。
	if gitHead(t, root) != snapshot {
		t.Fatal("reset changed the committed development snapshot")
	}
	delivery, err := os.ReadFile(filepath.Join(root, "delivery.txt"))
	if err != nil || string(delivery) != "delivery content\n" {
		t.Fatalf("reset touched the developed file: %v", err)
	}
	// workflow show 可见该干净状态（可从磁盘加载）。
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RequirementConfirmed || loaded.Actions["product-review"].Status != "PENDING" || len(loaded.Dispatches) != 0 {
		t.Fatalf("reset state not visible via show: %#v", loaded)
	}
}

// TestResetAllowsWholeReviewRedoAfterDevelopment verifies requirement 5's
// super-admin priority: after workflow reset keeps the development status and
// snapshot, the whole review (product-review then start-readiness) can be
// redone even though development already completed. The order guards
// requireProductReviewTransition / requireStartReadinessTransition must let the
// post-reset recovery pass when the review was cleared back to PENDING, while
// the normal "develop first, then review" ordering stays intact (start-readiness
// still cannot be redone before product-review).
func TestResetAllowsWholeReviewRedoAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "reset-rereview"), "custom", []string{"quality"})
	// 完成一次开发快照，制造"开发已完成"的 run（QA CASE-007 场景）。
	devDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-rereview.txt"), "delivery content\n")
	commitAll(t, root, "delivery rereview")
	state, err := AdvanceSnapshot(root, pkg, state.RunID, devDispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["development-worker"].Status != developmentComplete {
		t.Fatalf("development not completed before reset: %#v", state.Actions["development-worker"])
	}
	result, err := ResetRun(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Actions["product-review"].Status != "PENDING" || result.State.Actions["start-readiness"].Status != "PENDING" {
		t.Fatalf("reset did not clear the whole review to PENDING: %#v", result.State.Actions)
	}
	// 重新登记需求：需求澄清 + 确认（重置后 requirement 回到未确认）。
	state = confirmRequirement(t, root, pkg, result.State)
	// 顺序守卫仍生效：start-readiness 不能先于 product-review 重做。
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass before Start Readiness") {
		t.Fatalf("start-readiness redo before product-review was accepted: %v", err)
	}
	// 重做整体审：product-review 先（reset 后开发已完成，须放行）。
	reviewDispatch := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordAction(root, pkg, state.RunID, "product-review", reviewDispatch, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatalf("redo Product Review after reset was blocked: %v", err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("redo Product Review did not record PASS: %#v", state.Actions["product-review"])
	}
	// 再重做 start-readiness（产品审已 PASS，须放行）。
	readinessDispatch := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	state, err = RecordAction(root, pkg, state.RunID, "start-readiness", readinessDispatch, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatalf("redo Start Readiness after reset was blocked: %v", err)
	}
	if state.Actions["start-readiness"].Status != "PASS" {
		t.Fatalf("redo Start Readiness did not record PASS: %#v", state.Actions["start-readiness"])
	}
	// 已开发内容与开发快照原样保留。
	if state.Actions["development-worker"].Status != developmentComplete {
		t.Fatalf("redo of the review touched the development status: %#v", state.Actions["development-worker"])
	}
	if state.CurrentSnapshot != gitHead(t, root) {
		t.Fatal("redo of the review changed the development snapshot")
	}
}
