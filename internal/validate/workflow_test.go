package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	state = recordReadiness(t, root, pkg, state)
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatal(err)
	}
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
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "preserved", nil); err == nil || !strings.Contains(err.Error(), "unavailable after development") {
		t.Fatalf("post-development preserved rebind was accepted: %v", err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementConfirmed || developmentStarted(state) || state.CurrentSnapshot != gitHead(t, root) {
		t.Fatalf("meaning-changing requirement did not establish a new boundary: %#v", state)
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

func TestQAKindsAndIncrementalReviewApprovals(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "incremental"), "full", nil)
	designDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	before := stateBytes(t, root, state.RunID)
	if _, err := RecordQADesign(root, pkg, state.RunID, designDispatch, []QACaseInput{{Kind: "STATIC", Description: "only static", Procedure: "go test", Oracle: "passes"}}, ""); err == nil || !strings.Contains(err.Error(), "STATIC and one LIVE") {
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
	state, err = RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", nil)
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
	if _, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "PASS", "", nil); err == nil || !strings.Contains(err.Error(), "native VCS identity") {
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
		state, err = RecordGate(root, pkg, state.RunID, gate, dispatchID, "PASS", "", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	summary, err := Seal(root, pkg, state.RunID, nil)
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
	state, err = RecordGate(root, pkg, state.RunID, "architecture", architecture, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	quality := prepareAndClaim(t, root, pkg, state.RunID, "quality", "quality-initial")
	state, err = RecordGate(root, pkg, state.RunID, "quality", quality, "FAIL", "", []FindingInput{{Severity: "P1", Message: "normal workflow fails", Locations: []string{"internal/cli/cli.go:1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("blocking wave was not completed: %#v", state)
	}
	prior := state.CurrentSnapshot
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "repair.txt"), "repair\n")
	commitAll(t, root, "repair")
	state, err = AdvanceSnapshot(root, pkg, state.RunID)
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
	state, err = RecordGate(root, pkg, state.RunID, "quality", quality, "PASS", "", nil)
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
	_, err := RecordGate(root, pkg, state.RunID, "quality", dispatchID, "FAIL", "", []FindingInput{{Severity: "P1", Message: "rewrite the requirement", Locations: []string{"requirements.md:1"}}})
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

func TestRetainedOverallSnapshotFreezesRequirementArtifacts(t *testing.T) {
	root, pkg := workflowFixture(t)
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "retained-freeze", Flow: "formal", RequirementSource: "requirements.md", RequirementArtifacts: []string{"design.md"}, VCS: "git", RetainedOverall: true})
	if err != nil {
		t.Fatal(err)
	}
	state = confirmAndRoute(t, root, pkg, state, "custom", []string{"quality"})
	state = recordReadiness(t, root, pkg, state)
	writeTestFile(t, filepath.Join(root, "merged-delivery.txt"), "merged delivery\n")
	commitAll(t, root, "merged delivery")
	state, err = AdvanceSnapshot(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !developmentStarted(state) {
		t.Fatalf("retained snapshot did not establish development start: %#v", state.Actions["development-worker"])
	}
	writeTestFile(t, filepath.Join(root, "design.md"), "changed design\n")
	commitAll(t, root, "changed retained requirement")
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "preserved", nil); err == nil || !strings.Contains(err.Error(), "unavailable after development") {
		t.Fatalf("retained snapshot allowed meaning-preserved artifact rebinding: %v", err)
	}
}

func TestDevelopmentSnapshotRejectsUncommittedTrackedGitChanges(t *testing.T) {
	root, pkg := workflowFixture(t)
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "base delivery\n")
	commitAll(t, root, "tracked delivery")
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "uncommitted-snapshot"), "custom", []string{"quality"})
	state = recordReadiness(t, root, pkg, state)
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "delivery.txt"), "edited delivery\n")
	before := stateBytes(t, root, state.RunID)
	if _, err := AdvanceSnapshot(root, pkg, state.RunID); err == nil || !strings.Contains(err.Error(), "tracked Git changes must be committed") {
		t.Fatalf("uncommitted tracked delivery was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected uncommitted snapshot changed state")
	}
	commitAll(t, root, "commit delivery")
	if _, err := AdvanceSnapshot(root, pkg, state.RunID); err != nil {
		t.Fatalf("committed delivery snapshot was rejected: %v", err)
	}
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
	dispatchID := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
	state, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatchID, "PASS", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err = SetRoute(root, pkg, state.RunID, mode, selected)
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
	state = recordReadiness(t, root, pkg, state)
	dispatchID := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-qa-reviewer")
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, dispatchID, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "delivery-"+id+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+id)
	state, err = AdvanceSnapshot(root, pkg, state.RunID)
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
	if target == "quality" || target == "architecture" {
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
	if target == "quality" || target == "architecture" {
		kind = "gate"
	}
	return openDispatchID(state, kind, target)
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
	return root, promptPackage(t, map[string]string{"quality": "quality checks", "architecture": "architecture checks"})
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
