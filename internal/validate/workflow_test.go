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
	verification lifecycle.Verification
	binds        [][4]string
	transcript   string
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

func TestNativeStartRegistersAndFreezesRequirementArtifacts(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "artifacts", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git"})
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err == nil || !strings.Contains(err.Error(), "frozen requirement artifact") {
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
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	second := openDispatchID(state, "action", "qa-review")
	if first == second || state.Dispatches[first].Status != "STALE" || state.Dispatches[second].Attempt != 2 {
		t.Fatalf("retry did not create a fresh stale-bound dispatch: %#v", state.Dispatches)
	}
	before := stateBytes(t, root, state.RunID)
	if _, err := ClaimDispatch(root, pkg, state.RunID, second, "reviewer-one"); err == nil || !strings.Contains(err.Error(), "already reserved") {
		t.Fatalf("claimed interrupted reviewer was reused: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected reviewer reuse changed state")
	}
	state, err = ClaimDispatch(root, pkg, state.RunID, second, "reviewer-two")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAReview(root, pkg, state.RunID, second, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["qa-review"].Status != "PASS" || state.Dispatches[second].Status != "COMPLETED" {
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
	priorLifecycle := workflowLifecycle
	stub := &workflowLifecycleStub{verification: lifecycle.Verification{Outcome: lifecycle.Rejected, Diagnostic: "missing matching start and stop event"}}
	workflowLifecycle = stub
	t.Cleanup(func() { workflowLifecycle = priorLifecycle })

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
	if state.Actions["qa-review"].Status != "FAIL" || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "FAIL" {
		t.Fatalf("per-case results were not retained: %#v", state.QACases)
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
	if state.QACases[0].ID != "CASE-001" || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "PENDING" {
		t.Fatalf("exact approval was not preserved: %#v", state.QACases)
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
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, []QAReviewInput{{CaseID: state.QACases[1].ID, Outcome: "PASS"}}, "", nil)
	if err != nil || state.Actions["qa-review"].Status != "PASS" {
		t.Fatalf("incremental review did not pass: %v %#v", err, state.Actions["qa-review"])
	}
}

func TestGatePromptExcludesFrozenArtifactsAndResultUsesDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "gate-binding")
	prompt, err := PrepareGate(root, pkg, state.RunID, "quality")
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
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatchID, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QAExecution.Cases) != 2 || state.QAExecution.Cases[0].Mode != "whitebox" || state.QAExecution.Cases[1].Mode != "blackbox" {
		t.Fatalf("QA execution lost case modes: %#v", state.QAExecution.Cases)
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
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.QACases), "")
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
	qaDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	carry := prepareDispatch(t, root, pkg, state.RunID, "carry")
	state, err = RecordCarry(root, pkg, state.RunID, carry, []CarryInput{{GateID: "architecture", Decision: "INHERIT", Message: "repair does not touch architecture behavior"}}, "", false, "")
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
	if state.Actions["qa-review"].Status != "FAIL" || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "PASS" {
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
	revised = append(revised, QACaseInput{Mode: "whitebox", Description: "failure paths are covered", Procedure: "run direct failure-path tests", Oracle: "all failure-path tests pass"})
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["qa-design"].Status != "PASS" || state.QACases[2].ReviewStatus != "PENDING" {
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
	if state.Actions["qa-review"].Status != "FAIL" {
		t.Fatalf("set-level P1 finding did not fail the review action: %#v", state.Actions["qa-review"])
	}
	for _, testCase := range state.QACases {
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
	cases := append(baselineCases(), QACaseInput{Mode: "whitebox", Description: "duplicate direct coverage", Procedure: "run overlapping direct checks", Oracle: "the same rules pass"})
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
	if len(state.QACases) != 2 || state.Actions["qa-review"].Status != "PENDING" {
		t.Fatalf("removal-only correction did not retain approvals: %#v", state)
	}
	reviewDispatch = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "duplicate-recheck-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, nil, "", nil)
	if err != nil {
		t.Fatalf("set-only QA recheck was rejected: %v", err)
	}
	if state.Actions["qa-review"].Status != "PASS" {
		t.Fatalf("set-only QA recheck did not approve the correction: %#v", state.Actions["qa-review"])
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

	// FAIL 含发现项（P0/P1/P2 分级）：仍阻塞，但不构成不可恢复的终态。
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
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "retained-freeze", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true})
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
	if _, err := AuthorizeExtraRepair(root, pkg, state.RunID, 2); err == nil || !strings.Contains(err.Error(), "exactly one review wave") {
		t.Fatalf("multiple extra waves were authorized at once: %v", err)
	}
	if stateBytes(t, root, state.RunID) != beforeAuthorization {
		t.Fatal("rejected multi-wave authorization changed state")
	}
	state, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("extra review waves=%d want=1", state.ExtraReviewWaves)
	}
	authorizedState := stateBytes(t, root, state.RunID)
	if _, err := AuthorizeExtraRepair(root, pkg, state.RunID, 1); err == nil || !strings.Contains(err.Error(), "not exhausted") {
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
		summary, err := Abort(root, state.RunID)
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
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "ancestor-base", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", BaseSnapshot: ancestor})
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
	if _, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "non-ancestor", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", BaseSnapshot: divergent}); err == nil || !strings.Contains(err.Error(), "not an ancestor") {
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
		if state.PromptHashes["action:"+action.ID] != promptContentHash(action.Content) {
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
	delete(decoded, "promptHashes")
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
	if _, err := PrepareGate(root, pkg, state.RunID, "quality"); err != nil {
		t.Fatalf("changed gate could not be re-dispatched: %v", err)
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
	if _, err := PrepareGate(root, pkg, state.RunID, "quality"); err != nil {
		t.Fatalf("changed gate was not re-dispatchable after carry accepted the catalog: %v", err)
	}
}

func TestBaseOnlyPromptChangeEnablesPerGateReDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "base-change", "custom", []string{"quality"})
	state = recordGateResult(t, root, pkg, state, "quality", "base-change-quality", "PASS", "", nil)
	writeTestFile(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "new shared contract\n")
	if _, err := PrepareGate(root, pkg, state.RunID, "quality"); err != nil {
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
// reason trace for a no-split decision.
func TestNoSplitDecisionRequiresReasonNote(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "slicing-note"))
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "", ""); err == nil || !strings.Contains(err.Error(), "reason note") {
		t.Fatalf("no-split decision without a reason note was accepted: %v", err)
	}
	if _, err := RecordSlicing(root, pkg, state.RunID, "split", 1, nil, "", "", ""); err == nil || !strings.Contains(err.Error(), "at least two slices") {
		t.Fatalf("single-slice split was accepted: %v", err)
	}
}

// TestMergeVerificationAutoAttachedForSplitRetainedOverall verifies a retained
// overall run with a split decision auto-attaches merge gate and merge QA as its
// only post-merge verification and does not go through normal route selection.
func TestMergeVerificationAutoAttachedForSplitRetainedOverall(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "merge-auto", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true})
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
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "merge-flow", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true})
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
	if state.Actions["qa-design"].Status != "PASS" || !strings.Contains(state.Actions["qa-design"].Message, "切片基本独立") {
		t.Fatalf("merge QA zero-case design not traced: %#v", state.Actions["qa-design"])
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
	if state.QAExecution.Status != "PASS" || !strings.Contains(state.QAExecution.Message, "切片基本独立") {
		t.Fatalf("merge QA zero-case execution: %#v", state.QAExecution)
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
	if state.Actions["qa-design"].Status != "PASS" {
		t.Fatalf("blackbox design did not pass: %#v", state.Actions["qa-design"])
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
	whitebox, err = RecordQADesign(root, pkg, whitebox.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure", Procedure: "run unit tests", Oracle: "pass"}}, "")
	if err != nil {
		t.Fatalf("whitebox design after development failed: %v", err)
	}
	if whitebox.Actions["qa-design"].Status != "PASS" {
		t.Fatalf("whitebox design did not pass: %#v", whitebox.Actions["qa-design"])
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

	split := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "split-path"))
	// 拆分路径是一个切片实例，记录拆分决定时必须引用已通过整体审查的保留总任务
	// master，经继承满足整体级审查与 development-worker 门。
	splitMaster := sliceMaster(t, root, pkg, "split-path-master")
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
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "slice-route"))
	// 切片实例不重跑整体级产品审/技术审：引用已通过整体审查的保留总任务 master，
	// 记录继承来源，不再要求切片内重跑。
	master := sliceMaster(t, root, pkg, "slice-route-master")
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
// start-readiness findings are graded P0/P1/P2 so the main agent can apply the
// re-review boundary (only P2 -> revise without re-review).
func TestPreDevelopmentReviewFindingsCarrySeverity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "review-severity"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P0", Message: "blocking"}, {Severity: "P2", Message: "minor"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Findings[0].Severity != "P0" || state.Actions["product-review"].Findings[1].Severity != "P2" {
		t.Fatalf("severity not retained: %#v", state.Actions["product-review"].Findings)
	}
	next := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", next, "FAIL", "", []FindingInput{{Severity: "P3", Message: "bad"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
		t.Fatalf("invalid severity accepted: %v", err)
	}
	state = recordProductReview(t, root, pkg, state)
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
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.QACases), "")
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
	if state.QAExecution.Snapshot != state.CurrentSnapshot {
		t.Fatalf("legacy QAExecution.Snapshot=%s want=%s", state.QAExecution.Snapshot, state.CurrentSnapshot)
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
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "whitebox", Description: "structure tests", Procedure: "run unit tests", Oracle: "pass"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, passingExecution(state.QACases), "")
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
	// 开发后白盒设计：在既有黑盒用例上增补白盒结构用例（已覆盖用例保留）。
	designDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}, {Mode: "whitebox", Description: "structure", Procedure: "run unit tests", Oracle: "pass"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QACases) != 2 || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "PENDING" {
		t.Fatalf("blackbox approval was not preserved in the whitebox redesign: %#v", state.QACases)
	}
	reviewDispatch = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, passingExecution(state.QACases), "")
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
	if state.Actions["qa-design"].Status != "PASS" || len(state.QACases) != 1 {
		t.Fatalf("fast-path design not recorded: %#v", state.Actions["qa-design"])
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
	if state.Actions["qa-design"].Status != "PASS" {
		t.Fatalf("fast-path design lost after route confirmation: %#v", state.Actions["qa-design"])
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
	if state.Actions["qa-design"].Status != "PENDING" || len(state.QACases) != 0 {
		t.Fatalf("fast-path design not discarded on a route without blackbox QA: %#v", state.Actions["qa-design"])
	}
}

// TestPreDevelopmentReviewFindingsRequireSeverity verifies product-review and
// start-readiness findings must be graded non-empty P0/P1/P2 (requirement 14), so
// an ungraded finding is rejected instead of slipping through.
func TestPreDevelopmentReviewFindingsRequireSeverity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "severity-required"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded finding"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
		t.Fatalf("ungraded product-review finding accepted: %v", err)
	}
	state = recordProductReview(t, root, pkg, state)
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded technical finding"}}, false, ""); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
		t.Fatalf("ungraded start-readiness finding accepted: %v", err)
	}
}

// TestWordingCoversSplitSuggestionAndOverallReviewInheritance checks the
// user-visible wording required by the acceptance evidence: the split suggestion
// (including the 改拆后果说明) is mandated in SKILL.md and the product-review
// prompt, and the overall-level review inheritance is documented in SKILL.md and
// formal-flow.md.
func TestWordingCoversSplitSuggestionAndOverallReviewInheritance(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"SKILL.md", []string{"改拆后果说明", "整体级产品审/技术审足够", "切片继承整体审查结果"}},
		{"references/formal-flow.md", []string{"改拆后果说明", "整体级产品审/技术审足够", "切片继承整体审查结果"}},
		{"references/sliced-runs.md", []string{"改拆后果说明"}},
		{"prompts/actions/product-review.md", []string{"改拆后果说明"}},
		{"prompts/actions/qa-design.md", []string{"开发前", "开发后"}},
	}
	for _, check := range checks {
		data, err := os.ReadFile(filepath.Join("..", "..", check.path))
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		content := string(data)
		for _, want := range check.want {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q", check.path, want)
			}
		}
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
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git"})
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
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true})
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
		_, err = PrepareGate(root, pkg, runID, target)
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
	return []QACaseInput{{Mode: "whitebox", Description: "direct rules pass", Procedure: "run direct-owner automated checks", Oracle: "all checks pass"}, {Mode: "blackbox", Description: "public workflow succeeds", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"}}
}

func passingReviewDecisions(state RunState) []QAReviewInput {
	var decisions []QAReviewInput
	for _, testCase := range state.QACases {
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
	state, err := RegisterQAWorktree(root, state.RunID, worktree)
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
	if _, err := RegisterQAWorktree(root, state.RunID, worktree); err == nil || !strings.Contains(err.Error(), "does not match the run revision") {
		t.Fatalf("qa-worktree registration with stale injected revision was accepted: %v", err)
	}
}

// TestSnapshotRequiresBlackboxReviewPassed verifies requirement 2: the snapshot
// gate requires development complete 且 黑盒 qa-review PASS.
func TestSnapshotRequiresBlackboxReviewPassed(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "snapshot-blackbox-gate"), "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
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
	worktree := createQAWorktree(t, root, state)
	state, err = RegisterQAWorktree(root, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review", "blackbox", false, ""); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	reviewDispatch := openDispatchID(state, "action", "qa-review")
	state, err = ClaimDispatch(root, pkg, state.RunID, reviewDispatch, "snapshot-gate-reviewer")
	if err != nil {
		t.Fatal(err)
	}
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
	if state.Actions["qa-review"].Status != "PASS" {
		t.Fatalf("empty-set blackbox review did not pass: %#v", state.Actions["qa-review"])
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
	if state.Actions["qa-review"].Status != "PASS" {
		t.Fatalf("empty-set blackbox review did not pass: %#v", state.Actions["qa-review"])
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
	if state.QAExecution.Status != "PASS" {
		t.Fatalf("empty-set QA execution did not record PASS: %#v", state.QAExecution)
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
	if state.Actions["qa-review"].Status != "PENDING" {
		t.Fatalf("qa-review should be PENDING after an empty-set design: %#v", state.Actions["qa-review"])
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
	if state.QAExecution.Status != "PASS" {
		t.Fatalf("released blackbox cases did not count as PASS: %#v", state.QAExecution)
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

// TestReviewRuleOnlyP2RecordsPassWithVisibleFindings verifies requirement 5:
// a review carrying only P2 findings records PASS with the P2 suggestions visible
// and does not produce FAIL or require a re-review.
func TestReviewRuleOnlyP2RecordsPassWithVisibleFindings(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "only-p2"))
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", []FindingInput{{Severity: "P2", Message: "minor wording suggestion"}}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" || len(state.Actions["product-review"].Findings) != 1 || state.Actions["product-review"].Findings[0].Severity != "P2" {
		t.Fatalf("P2-only PASS was not recorded with visible findings: %#v", state.Actions["product-review"])
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
