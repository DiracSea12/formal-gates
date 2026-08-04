package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleStub struct {
	verification lifecycle.Verification
	binds        [][4]string
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

func (*workflowLifecycleStub) TranscriptPath(_, _, _ string) (string, string, error) {
	return "", "", nil
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err == nil || !strings.Contains(err.Error(), "frozen requirement artifact") {
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
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-review")
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
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-review"); err != nil {
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
	before := stateBytes(t, root, state.RunID)
	// full 路线同时选中黑盒与白盒：黑盒要求至少一个 LIVE 行为执行用例。
	if _, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "STATIC", Description: "only static", Procedure: "go test", Oracle: "passes"}}, ""); err == nil || !strings.Contains(err.Error(), "at least one LIVE behavior execution case") {
		t.Fatalf("single-kind QA set was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected QA set changed state")
	}
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
	designPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-design")
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
	revised := []QACaseInput{cases[0], {Kind: "LIVE", Description: "public workflow succeeds", Procedure: "run the public CLI against a built snapshot", Oracle: "observable output matches"}}
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.QACases[0].ID != "CASE-001" || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "PENDING" {
		t.Fatalf("exact approval was not preserved: %#v", state.QACases)
	}
	reviewPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-review")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewPrompt, "Accepted coverage context") || strings.Count(reviewPrompt, "kind: STATIC") != 0 || !strings.Contains(reviewPrompt, "kind: LIVE") {
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
	if len(state.QAExecution.Cases) != 2 || state.QAExecution.Cases[0].Kind != "STATIC" || state.QAExecution.Cases[1].Kind != "LIVE" {
		t.Fatalf("QA execution lost case kinds: %#v", state.QAExecution.Cases)
	}
	for index, gate := range []string{"architecture", "quality"} {
		dispatchID = prepareAndClaim(t, root, pkg, state.RunID, gate, fmt.Sprintf("gate-%d", index+1))
		state, err = RecordGate(root, pkg, state.RunID, gate, dispatchID, "PASS", "", comparedRange(state), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false)
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
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
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
	state, err := RecordQAReview(root, pkg, state.RunID, dispatchID, passingReviewDecisions(state), "", []FindingInput{{Message: "missing failure-path coverage"}})
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
	revised = append(revised, QACaseInput{Kind: "STATIC", Description: "failure paths are covered", Procedure: "run direct failure-path tests", Oracle: "all failure-path tests pass"})
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, revised, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["qa-design"].Status != "PASS" || state.QACases[2].ReviewStatus != "PENDING" {
		t.Fatalf("corrected QA rework did not reopen review: %#v", state)
	}
}

func TestQADesignAcceptsRemovalOnlyDuplicateCorrection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "remove-duplicate"), "full", nil)
	cases := append(baselineCases(), QACaseInput{Kind: "STATIC", Description: "duplicate direct coverage", Procedure: "run overlapping direct checks", Oracle: "the same rules pass"})
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, cases, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "duplicate-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", []FindingInput{{Message: "remove duplicated direct coverage"}})
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatalf("approved removal-only correction did not unlock development: %v", err)
	}
}

func TestProductReviewPreDevelopmentGatingAndFailRecovery(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "product-review-gate"))

	// 未 PASS：start-readiness、拆分决定与 development-worker 均被拒绝。路线在拆
	// 分决定之后确认，而拆分决定在 product-review 之后，所以 development-worker 在
	// 此时因路线未确认而被拒。
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness"); err == nil || !strings.Contains(err.Error(), "Product Review must pass before Start Readiness") {
		t.Fatalf("start-readiness prepared before product review: %v", err)
	}
	if _, err := RecordSlicing(root, pkg, state.RunID, "no-split", 0, nil, "", "reason", ""); err == nil || !strings.Contains(err.Error(), "Product Review must pass before the slicing decision") {
		t.Fatalf("slicing decision recorded before product review: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err == nil {
		t.Fatalf("development-worker prepared before product review: %v", err)
	}

	// FAIL 含发现项（P0/P1/P2 分级）：仍阻塞，但不构成不可恢复的终态。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "requirement does not target a real user problem"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "FAIL" || len(state.Actions["product-review"].Findings) != 1 || state.Actions["product-review"].Findings[0].Severity != "P1" {
		t.Fatalf("product review FAIL was not recorded with severity: %#v", state.Actions["product-review"])
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness"); err == nil || !strings.Contains(err.Error(), "Product Review must pass") {
		t.Fatalf("start-readiness prepared after product review FAIL: %v", err)
	}

	// 重新派发并记录 PASS 后下游解锁。
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "product-review")
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("product review PASS was not recorded: %#v", state.Actions["product-review"])
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatalf("development worker stayed blocked after product review PASS: %v", err)
	}
}

// 接受路径：审查派发 A 保持 OPEN，用户接受后直接在 A 上记录 PASS，不另造新派发。
// PASS 对应的是真跑过审查的子代理派发，生命周期校验因此可过。
func TestProductReviewAcceptRecordsPassOnHeldDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "product-review-accept"))

	// 未记录（PENDING）：start-readiness 与 development-worker 均被拒绝。
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness"); err == nil || !strings.Contains(err.Error(), "Product Review must pass before Start Readiness") {
		t.Fatalf("start-readiness prepared before product review: %v", err)
	}

	// 派发 A 已准备并认领（保持 OPEN，未记录 FAIL）：仍阻塞。
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness"); err == nil || !strings.Contains(err.Error(), "Product Review must pass") {
		t.Fatalf("start-readiness prepared while product review held open: %v", err)
	}

	// 用户接受后，直接在 A 上记录 PASS，下游解锁。
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("product review PASS was not recorded on the held dispatch: %#v", state.Actions["product-review"])
	}
	state = recordReadiness(t, root, pkg, state)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatalf("development worker stayed blocked after product review PASS: %v", err)
	}
}

// TestQADesignRequiresSelectedQA verifies QA Design needs a selected QA mode
// (blackbox or whitebox); the pre-development reviews are now a strict prefix of
// the route, so QA selected already implies Product Review passed.
func TestQADesignRequiresSelectedQA(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-design-requires-qa"), "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design"); err == nil || !strings.Contains(err.Error(), "QA is not selected") {
		t.Fatalf("qa-design without selected QA mode: %v", err)
	}
	state, err := AddRouteGates(root, pkg, state.RunID, []string{blackboxQAID})
	if err != nil {
		t.Fatal(err)
	}
	if !isSelected(state, blackboxQAID) {
		t.Fatalf("blackbox QA was not added to the route: %#v", state.SelectedGates)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design"); err != nil {
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
			state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
			if err != nil {
				t.Fatalf("run without %s blocked at snapshot: %v", removed, err)
			}
			gateDispatch := prepareAndClaim(t, root, pkg, state.RunID, "quality", "predating-gate")
			state, err = RecordGate(root, pkg, state.RunID, "quality", gateDispatch, "PASS", "", comparedRange(state), nil)
			if err != nil {
				t.Fatalf("run without %s blocked at gate review: %v", removed, err)
			}
			summary, err := Seal(root, pkg, state.RunID, nil, false)
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
	state = confirmAndRoute(t, root, pkg, state, "custom", []string{"quality"})
	writeTestFile(t, filepath.Join(root, "merged-delivery.txt"), "merged delivery\n")
	commitAll(t, root, "merged delivery")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "")
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
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch); err == nil || !strings.Contains(err.Error(), "unsubmitted git changes must be committed") {
		t.Fatalf("uncommitted tracked delivery was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected uncommitted snapshot changed state")
	}
	commitAll(t, root, "commit delivery")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch); err != nil {
		t.Fatalf("committed delivery snapshot was rejected: %v", err)
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
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
	if _, err := Seal(root, pkg, state.RunID, []string{"quality"}, false); err == nil || !strings.Contains(err.Error(), "architecture") {
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
	if _, err := Seal(root, pkg, state.RunID, []string{"quality"}, false); err == nil || !strings.Contains(err.Error(), "review-wave limit is exhausted") {
		t.Fatalf("FAIL skip before the wave limit must require exhaustion without --user-requested: %v", err)
	}
	summary, err := Seal(root, pkg, state.RunID, []string{"quality"}, true)
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
	summary, err := Seal(root, pkg, state.RunID, []string{"quality"}, false)
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err == nil {
		t.Fatalf("development prepared before slicing decision: %v", err)
	}
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
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
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "")
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
	summary, err := Seal(root, pkg, state.RunID, nil, false)
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
// custom can select each QA mode independently, and the per-mode quality floors
// are enforced (blackbox requires a LIVE case, whitebox requires a STATIC case).
func TestBlackboxWhiteboxOptionalityInRoute(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-blackbox-only"), "custom", []string{blackboxQAID})
	if !isSelected(state, blackboxQAID) || isSelected(state, whiteboxQAID) {
		t.Fatalf("blackbox-only route: %#v", state.SelectedGates)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	if _, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "STATIC", Description: "structure", Procedure: "run unit tests", Oracle: "pass"}}, ""); err == nil || !strings.Contains(err.Error(), "at least one LIVE") {
		t.Fatalf("blackbox quality floor not enforced: %v", err)
	}
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
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
	// 白盒设计在开发后读实现进行：先完成开发与快照，再设计，结构测试下限才生效。
	developmentDispatch := prepareDispatch(t, root, pkg, whitebox.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-whitebox-floor.txt"), "delivery\n")
	commitAll(t, root, "delivery whitebox floor")
	whitebox, err = AdvanceSnapshot(root, pkg, whitebox.RunID, developmentDispatch)
	if err != nil {
		t.Fatal(err)
	}
	designDispatch = prepareDispatch(t, root, pkg, whitebox.RunID, "qa-design")
	if _, err := RecordQADesign(root, pkg, whitebox.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "behavior", Procedure: "run", Oracle: "observe"}}, ""); err == nil || !strings.Contains(err.Error(), "at least one STATIC") {
		t.Fatalf("whitebox quality floor not enforced: %v", err)
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
	if _, err := PrepareAction(root, pkg, fast.RunID, "development-worker"); err == nil || !strings.Contains(err.Error(), "QA Review must pass") {
		t.Fatalf("fast path development before QA review: %v", err)
	}

	split := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "split-path"))
	// 拆分路径是一个切片实例，记录拆分决定时必须引用已通过整体审查的保留总任务
	// master，经继承满足整体级审查与 development-worker 门。
	splitMaster := sliceMaster(t, root, pkg, "split-path-master")
	split = recordSlicing(t, root, pkg, split, "split", splitMaster)
	split = setRoute(t, root, pkg, split, "custom", []string{blackboxQAID})
	designDispatch := prepareDispatch(t, root, pkg, split.RunID, "qa-design")
	split, err := RecordQADesign(root, pkg, split.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "behavior", Procedure: "run the public command", Oracle: "observable success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, split.RunID, "qa-review", "split-qa-reviewer")
	split, err = RecordQAReview(root, pkg, split.RunID, reviewDispatch, passingReviewDecisions(split), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, split.RunID, "development-worker"); err != nil {
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
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
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Severity: "P0", Message: "blocking"}, {Severity: "P2", Message: "minor"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Findings[0].Severity != "P0" || state.Actions["product-review"].Findings[1].Severity != "P2" {
		t.Fatalf("severity not retained: %#v", state.Actions["product-review"].Findings)
	}
	next := prepareDispatch(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", next, "FAIL", "", []FindingInput{{Severity: "P3", Message: "bad"}}); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
		t.Fatalf("invalid severity accepted: %v", err)
	}
	state = recordProductReview(t, root, pkg, state)
	readiness := recordReadiness(t, root, pkg, state)
	dispatchID = prepareDispatch(t, root, pkg, readiness.RunID, "start-readiness")
	if _, err := RecordAction(root, pkg, readiness.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "technical gap"}}); err != nil {
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
	state, err := RecordSettledFindings(root, pkg, state.RunID, "product-review", []string{"settled finding one", "settled finding two"})
	if err != nil {
		t.Fatal(err)
	}
	if got := state.SettledFindings["product-review"]; len(got) != 2 {
		t.Fatalf("settled findings not recorded: %#v", state.SettledFindings)
	}
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "settled finding one") || !strings.Contains(prompt, "Do not re-raise") {
		t.Fatalf("settled findings not injected into product-review prompt: %s", prompt)
	}
	state, err = RecordSettledFindings(root, pkg, state.RunID, "start-readiness", []string{"settled technical decision"})
	if err != nil {
		t.Fatal(err)
	}
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	prompt, err = PrepareAction(root, pkg, state.RunID, "start-readiness")
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
// designs and reviews its STATIC structure cases after development: development
// start and the snapshot are not gated on whitebox QA Review, the post-development
// design reads the implementation, and the run seals.
func TestWhiteboxQADesignsAndReviewsAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "whitebox-post-dev"), "custom", []string{whiteboxQAID})
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatalf("whitebox development blocked before QA design: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	developmentDispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-whitebox.txt"), "delivery\n")
	commitAll(t, root, "delivery whitebox")
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
	if err != nil {
		t.Fatal(err)
	}
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "STATIC", Description: "structure tests", Procedure: "run unit tests", Oracle: "pass"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewDispatch := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer")
	state, err = RecordQAReview(root, pkg, state.RunID, reviewDispatch, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	executionDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, executionDispatch, passingExecution(state.QACases), "")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("whitebox-only run did not seal: %#v", summary)
	}
}

// TestFullRouteDesignsWhiteboxAfterDevelopment verifies the full route's two-phase
// QA: blackbox LIVE cases are designed and approved before development, the
// whitebox STATIC cases are added after development by re-deriving the complete
// set (preserving the approved blackbox cases), and the run seals with both
// floors satisfied.
func TestFullRouteDesignsWhiteboxAfterDevelopment(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "full-two-phase"), "full", nil)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	var err error
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
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
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
	if err != nil {
		t.Fatal(err)
	}
	// 开发后白盒设计：在既有黑盒用例上增补 STATIC 结构用例（已覆盖用例保留）。
	designDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}, {Kind: "STATIC", Description: "structure", Procedure: "run unit tests", Oracle: "pass"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.QACases) != 2 || state.QACases[0].ReviewStatus != "PASS" || state.QACases[1].ReviewStatus != "PENDING" {
		t.Fatalf("blackbox approval was not preserved in the whitebox redesign: %#v", state.QACases)
	}
	reviewDispatch = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-reviewer")
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
	summary, err := Seal(root, pkg, state.RunID, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "SEALED" {
		t.Fatalf("full two-phase run did not seal: %#v", summary)
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
	if _, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "STATIC", Description: "structure", Procedure: "run unit tests", Oracle: "pass"}}, ""); err == nil || !strings.Contains(err.Error(), "at least one LIVE") {
		t.Fatalf("fast-path non-blackbox design accepted: %v", err)
	}
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
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
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
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
	state, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "LIVE", Description: "public workflow", Procedure: "run the public CLI", Oracle: "observable success"}}, "")
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
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded finding"}}); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
		t.Fatalf("ungraded product-review finding accepted: %v", err)
	}
	state = recordProductReview(t, root, pkg, state)
	dispatchID = prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	if _, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "FAIL", "", []FindingInput{{Message: "ungraded technical finding"}}); err == nil || !strings.Contains(err.Error(), "severity must be P0, P1, or P2") {
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
	state, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatchID, "PASS", "", nil)
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
	state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatchID, "PASS", "", nil)
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
	state, err = AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
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
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
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
	state, err := AdvanceSnapshot(root, pkg, state.RunID, developmentDispatch)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func recordReadiness(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func prepareDispatch(t *testing.T, root, pkg, runID, target string) string {
	t.Helper()
	var err error
	if target == "quality" || target == "architecture" || target == mergeGateID {
		_, err = PrepareGate(root, pkg, runID, target)
	} else {
		_, err = PrepareAction(root, pkg, runID, target)
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

func prepareAndClaim(t *testing.T, root, pkg, runID, target, reviewer string) string {
	t.Helper()
	dispatchID := prepareDispatch(t, root, pkg, runID, target)
	if _, err := ClaimDispatch(root, pkg, runID, dispatchID, reviewer); err != nil {
		t.Fatal(err)
	}
	return dispatchID
}

func baselineCases() []QACaseInput {
	return []QACaseInput{{Kind: "STATIC", Description: "direct rules pass", Procedure: "run direct-owner automated checks", Oracle: "all checks pass"}, {Kind: "LIVE", Description: "public workflow succeeds", Procedure: "run the documented public CLI against a built snapshot", Oracle: "observable output succeeds"}}
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
