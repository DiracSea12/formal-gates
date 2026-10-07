package validate

// End-to-end convergence tests live here so the review-wave, frozen-case-set,
// adjudication, repair and carry sequence is exercised as one state machine.

import (
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func blackboxFrozenDelivery(t *testing.T, id string, selected []string) (string, string, RunState) {
	t.Helper()
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, id), "custom", selected)
	worktree := createQAWorktree(t, root, state)
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "blackbox", Description: "public command succeeds", Procedure: "run the documented command", Oracle: "the command returns the documented result"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", id+"-blackbox-review", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.QACaseSets["blackbox"]; got.Status != caseSetFrozen || got.Version == "" {
		t.Fatalf("blackbox case-set did not freeze after a clean PASS review: %#v", got)
	}
	development := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-"+id+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+id)
	state, err = AdvanceSnapshot(root, pkg, state.RunID, development, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := state.QACaseSets["blackbox"]; got.BoundSnapshot != state.CurrentSnapshot {
		t.Fatalf("frozen case-set was not rebound to the development snapshot: %#v", got)
	}
	return root, pkg, state
}

func TestQAReviewSuggestionsMustBeDisposedBeforeFreeze(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "suggestion-freeze"), "custom", []string{prompts.BlackboxQAID})
	worktree := createQAWorktree(t, root, state)
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "blackbox", Description: "public command", Procedure: "run it", Oracle: "success"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "suggestion-freeze-review", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{{Severity: "P2", Message: "make the success oracle explicit"}, {Severity: "P3", Message: "simplify the setup wording"}})
	if err != nil {
		t.Fatal(err)
	}
	caseSet := state.QACaseSets["blackbox"]
	if caseSet.Status != caseSetReviewed || len(caseSet.Suggestions) != 2 || caseSet.Version != "" {
		t.Fatalf("suggested PASS review froze too early: %#v", caseSet)
	}
	state, err = RecordQASuggestion(root, pkg, state.RunID, "blackbox", caseSet.Suggestions[0].ID, "DEFER", "existing approved oracle covers the requirement", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.QACaseSets["blackbox"].Status == caseSetFrozen {
		t.Fatal("unhandled second suggestion froze")
	}
	state, err = RecordQASuggestion(root, pkg, state.RunID, "blackbox", caseSet.Suggestions[1].ID, "DECLINE", "existing setup remains executable", nil)
	if err != nil {
		t.Fatal(err)
	}
	caseSet = state.QACaseSets["blackbox"]
	if caseSet.Status != caseSetFrozen || caseSet.Version == "" {
		t.Fatalf("fully disposed suggestion set did not freeze: %#v", caseSet)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "blackbox", false, ""); err == nil {
		t.Fatal("ordinary QA design reopened a FROZEN case-set")
	}
}

func TestWhiteboxS1CannotInitializeWaveBeforeTestSnapshotAndFreeze(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "whitebox-s2-wave"), "custom", []string{prompts.WhiteboxQAID, "quality"})
	development := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "delivery-whitebox-s2-wave.txt"), "delivery\n")
	commitAll(t, root, "whitebox S1 delivery")
	var err error
	state, err = AdvanceSnapshot(root, pkg, state.RunID, development, false, "")
	if err != nil {
		t.Fatal(err)
	}
	s1 := state.CurrentSnapshot
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err == nil {
		t.Fatal("whitebox S1 initialized a review-wave before test design/review/freeze")
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "whitebox")
	writeTestFile(t, filepath.Join(root, "whitebox_s2_test.go"), "package fixture\n\nfunc TestWhiteboxS2() {}\n")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "whitebox", Description: "structure remains valid", Procedure: "run the delivered test", Oracle: "the test passes", Test: "whitebox_s2_test.go::TestWhiteboxS2"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	commitAll(t, root, "whitebox S2 test")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot == s1 {
		t.Fatal("whitebox test commit did not advance S1 to S2")
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "whitebox-s2-reviewer", "whitebox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	caseSet := state.QACaseSets["whitebox"]
	if caseSet.Status != caseSetFrozen || caseSet.BoundSnapshot != state.CurrentSnapshot {
		t.Fatalf("whitebox final case-set is not frozen at S2: %#v", caseSet)
	}
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if wave.Snapshot != state.CurrentSnapshot || wave.Snapshot == s1 || len(wave.Expected) != 2 {
		t.Fatalf("review-wave did not bind final S2 and all targets: %#v", wave)
	}
}

func TestReviewWaveAdjudicationCreatesOneP2RepairAndCarry(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "wave-p2", []string{prompts.BlackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(wave.Expected) != 2 || wave.ExpectedDigest == "" {
		t.Fatalf("review-wave expected-set = %#v", wave)
	}
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "wave-p2-quality", "PASS", "", []FindingInput{{Severity: "P2", Message: "remove the reachable duplicate branch", Locations: []string{"delivery-wave-p2.txt:1"}}})
	wave = state.ReviewWaves[state.ActiveReviewWaveID]
	if wave.Status != reviewWaveAdjudicating || state.CompletedReviewWaves != 1 {
		t.Fatalf("collected wave did not enter adjudication exactly once: %#v", wave)
	}
	var findingID string
	for id, finding := range state.PostReviewFindings {
		if finding.WaveID == wave.ID {
			findingID = id
		}
	}
	state, err = AdjudicateReviewWave(root, pkg, state.RunID, ReviewWaveAdjudicationInput{WaveID: wave.ID, StateRevision: state.StateIntegrity, Findings: []FindingAdjudicationInput{{FindingID: findingID, Decision: "NEW", Dimensions: ProblemDimensions{Requirement: "REQ-005", NormalFailurePath: "normal gate review of the delivery", AffectedModule: "delivery command", RootCause: "duplicate reachable branch", ActualConsequence: "unnecessary implementation path remains"}, Verification: FindingVerification{Scope: "confirmed the finding is in scope", Severity: "confirmed P2 is sufficient", Causality: "traced the branch to the result", Reproduction: "reproduced on the review snapshot"}, References: []string{"delivery-wave-p2.txt:1"}, Reason: "the five dimensions show one new canonical problem"}}})
	if err != nil {
		t.Fatal(err)
	}
	wave = state.ReviewWaves[wave.ID]
	if wave.Status != reviewWaveReadyForRepair || len(state.Problems) != 1 {
		t.Fatalf("adjudication did not open one problem: wave=%#v problems=%#v", wave, state.Problems)
	}
	development := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	state, _ = LoadRunState(root, state.RunID)
	wave = state.ReviewWaves[wave.ID]
	if wave.RepairPlan == nil || len(wave.RepairPlan.ProblemIDs) != 1 || wave.RepairPlan.DispatchID != development {
		t.Fatalf("unified repair plan was not bound to one dispatch: %#v", wave.RepairPlan)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
		t.Fatalf("same repair input did not reuse its existing dispatch: %v", err)
	}
	writeTestFile(t, filepath.Join(root, "repair-wave-p2.txt"), "repair\n")
	commitAll(t, root, "repair wave p2")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, development, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = CompleteGateP2Repair(root, pkg, state.RunID, wave.ID, []string{"go build ./..."}, []string{"go test ./..."}, []string{state.qaModeCases("blackbox")[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Snapshot != state.CurrentSnapshot || state.Carry["quality"].Origin != "GATE_P2_COMPLETION" || len(state.P2CompletionCredentials) != 1 || state.CompletedReviewWaves != 1 {
		t.Fatalf("gate P2 completion did not carry PASS without a new review wave: %#v %#v", state.Carry, state.P2CompletionCredentials)
	}
	if state.ReviewWaves[wave.ID].Status != reviewWaveComplete || hasSuggestionRecommendation(state) {
		t.Fatalf("completed gate P2 remained repairable: %#v", state.ReviewWaves[wave.ID])
	}
	caseID := state.qaModeCases("blackbox")[0].ID
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{caseID}, "the P2 repair touches this public path")
	if err != nil {
		t.Fatal(err)
	}
	qaDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(qaExecutionRequiredCases(state, "blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "final candidate full regression")
	if err != nil {
		t.Fatal(err)
	}
	qaDispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("blackbox").Status != "PASS" || state.CompletedReviewWaves != 1 {
		t.Fatalf("final FULL QA did not close without a gate re-review: %#v", state.qaExecution("blackbox"))
	}
}

func TestFrozenCaseSetRequiresLegalThawAndBuildsANewWave(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "case-set-thaw", []string{prompts.BlackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytes(t, root, state.RunID)
	caseID := state.qaModeCases("blackbox")[0].ID
	if _, err := ThawQACaseSet(root, pkg, state.RunID, "blackbox", "TEMPORARY_ENVIRONMENT", "runner unavailable once", state.RequirementRevision, []string{caseID}); err == nil {
		t.Fatal("temporary environment failure was accepted as a thaw category")
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected thaw changed run state")
	}
	state, err = ThawQACaseSet(root, pkg, state.RunID, "blackbox", "CASE_EXECUTABILITY", "the documented command moved", state.RequirementRevision, []string{caseID})
	if err != nil {
		t.Fatal(err)
	}
	if state.QACaseSets["blackbox"].Status != caseSetThawed || state.qaModeCases("blackbox")[0].ReviewStatus != "PENDING" || state.ActiveReviewWaveID != "" || state.ReviewWaves[wave.ID].Status != reviewWaveInvalidated {
		t.Fatalf("legal thaw did not invalidate the old case/wave authority: %#v %#v", state.QACaseSets["blackbox"], state.ReviewWaves[wave.ID])
	}
	worktree := createQAWorktree(t, root, state)
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{CaseID: caseID, Mode: "blackbox", Description: "public command succeeds", Procedure: "run the command at its documented new path", Oracle: "the command returns the documented result"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "case-set-thaw-review", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	newWave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if newWave.ID == wave.ID || newWave.ExpectedDigest == wave.ExpectedDigest {
		t.Fatalf("legal thaw reused the invalidated wave identity: old=%#v new=%#v", wave, newWave)
	}
}

func TestReviewWaveRetryRejectsLateAttemptAndRequiresErrorDisposition(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "wave-retry", []string{prompts.BlackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	key := reviewWaveTargetKey("action", "qa-execution", "blackbox")
	firstID := wave.Expected[key].DispatchID
	if _, err := ClaimDispatch(root, pkg, state.RunID, firstID, "wave-retry-first"); err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, firstID, nil, "runner unavailable")
	if err != nil {
		t.Fatal(err)
	}
	wave, err = PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	secondID := wave.Expected[key].DispatchID
	if secondID == firstID || wave.Expected[key].Status != reviewTargetPrepared {
		t.Fatalf("runtime retry did not rebind the expected target: %#v", wave.Expected[key])
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, firstID, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	wave = state.ReviewWaves[state.ActiveReviewWaveID]
	if wave.Expected[key].DispatchID != secondID || wave.Expected[key].Status != reviewTargetPrepared || len(wave.LateResults) != 1 {
		t.Fatalf("late first attempt changed the retry target: %#v", wave)
	}
	if _, err := ClaimDispatch(root, pkg, state.RunID, secondID, "wave-retry-second"); err != nil {
		t.Fatal(err)
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, secondID, nil, "runner still unavailable")
	if err != nil {
		t.Fatal(err)
	}
	wave = state.ReviewWaves[state.ActiveReviewWaveID]
	if wave.Expected[key].Status != reviewTargetRetryExhaust {
		t.Fatalf("second runtime error did not exhaust the target: %#v", wave.Expected[key])
	}
	state, err = DisposeReviewWaveTarget(root, pkg, state.RunID, key, state.StateIntegrity, "user ends this unavailable target without PASS")
	if err != nil {
		t.Fatal(err)
	}
	if state.ReviewWaves[state.ActiveReviewWaveID].Expected[key].Status != reviewTargetDisposed {
		t.Fatalf("explicit error disposition was not retained: %#v", state.ReviewWaves[state.ActiveReviewWaveID].Expected[key])
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, secondID, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil || state.qaExecution("blackbox").Status != "RUNTIME_ERROR" || len(state.ReviewWaves[state.ActiveReviewWaveID].LateResults) != 2 || state.DispatchObligations[state.Dispatches[secondID].TaskKey].Status != "RETRY_EXHAUSTED" {
		t.Fatalf("disposed late result changed the effective result or attempt: %v %+v", err, state.ReviewWaves[state.ActiveReviewWaveID])
	}
}

func TestGatePromptOmitsQACasesAndP3RemainsSealAdvisory(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "wave-p3", []string{prompts.BlackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	gateTarget := wave.Expected[reviewWaveTargetKey("gate", "quality", "")]
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(state.Dispatches[gateTarget.DispatchID].PromptFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prompt), "CASE-001") || strings.Contains(string(prompt), "public command succeeds") {
		t.Fatalf("gate prompt received QA case content:\n%s", prompt)
	}
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "wave-p3-quality", "PASS", "", []FindingInput{{Severity: "P3", Message: "optional wording cleanup", Locations: []string{"delivery-wave-p3.txt:1"}}})
	wave = state.ReviewWaves[state.ActiveReviewWaveID]
	var findingID string
	for id, finding := range state.PostReviewFindings {
		if finding.WaveID == wave.ID {
			findingID = id
		}
	}
	state, err = AdjudicateReviewWave(root, pkg, state.RunID, ReviewWaveAdjudicationInput{WaveID: wave.ID, StateRevision: state.StateIntegrity, Findings: []FindingAdjudicationInput{{FindingID: findingID, Decision: "NEW", Dimensions: ProblemDimensions{Requirement: "REQ-005", NormalFailurePath: "normal quality review", AffectedModule: "delivery wording", RootCause: "wording could be shorter", ActualConsequence: "no correctness impact"}, Verification: FindingVerification{Scope: "in scope advisory", Severity: "P3 confirmed", Causality: "wording only", Reproduction: "read on bound snapshot"}, References: []string{"delivery-wave-p3.txt:1"}, Reason: "retain as a non-blocking seal advisory"}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.ReviewWaves[wave.ID].Status != reviewWaveComplete {
		t.Fatalf("P3-only wave created a repair: %#v", state.ReviewWaves[wave.ID])
	}
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.GateP3Advisories) != 1 || summary.GateP3Advisories[0].ID != findingID {
		t.Fatalf("Seal omitted the unresolved P3 advisory: %#v", summary.GateP3Advisories)
	}
}

func TestRepairPreviewIsReadOnlyAndStateBound(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "repair-preview", []string{prompts.BlackboxQAID, "quality"})
	before := stateBytes(t, root, state.RunID)
	estimate := RepairRerunEstimate{BuildTargets: []string{"go build ./..."}, TestTargets: []string{"go test ./..."}, QACases: map[string][]string{"blackbox": {state.qaModeCases("blackbox")[0].ID}}}
	preview, err := PreviewRepairAuthorization(root, pkg, state.RunID, estimate)
	if err != nil {
		t.Fatal(err)
	}
	after := stateBytes(t, root, state.RunID)
	if before != after {
		t.Fatal("repair preview mutated run state")
	}
	if preview.Digest == "" || preview.StateRevision != state.StateIntegrity || preview.EngineAllowance != "UNAVAILABLE" {
		t.Fatalf("repair preview binding = %#v", preview)
	}
}

func TestRepairAuthorizationUsesPreviewBoundAffectedSubset(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "repair-preview-scope", []string{prompts.BlackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	caseID := state.qaModeCases("blackbox")[0].ID
	qaDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qaDispatch, []QAResultInput{{CaseID: caseID, Outcome: "FAIL", Procedure: "ran the documented command", Observation: "captured the incorrect result", OracleResult: "the result differed from the approved oracle"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "repair-preview-scope-quality", "PASS", "", nil)
	if state.ReviewWaves[wave.ID].Status != reviewWaveAdjudicating {
		t.Fatalf("failed review-wave was not collected: %#v", state.ReviewWaves[wave.ID])
	}
	state, err = mutateRun(root, state.RunID, func(current *RunState) error {
		current.CompletedReviewWaves = automaticReviewWaveLimit
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	estimate := RepairRerunEstimate{BuildTargets: []string{"go build ./..."}, TestTargets: []string{"go test ./..."}, QACases: map[string][]string{"blackbox": {caseID}}}
	preview, err := PreviewRepairAuthorization(root, pkg, state.RunID, estimate)
	if err != nil {
		t.Fatal(err)
	}
	state, err = AuthorizeExtraRepair(root, pkg, state.RunID, 1, nil, RepairAuthorizationInput{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision, Estimate: estimate})
	if err != nil {
		t.Fatal(err)
	}
	scope := state.ExecutionScopes["blackbox"]
	if scope.Decision != "AFFECTED" || scope.Origin != "MAIN_AGENT" || scope.BaseSnapshot != state.CurrentSnapshot || len(scope.CaseIDs) != 1 || scope.CaseIDs[0] != caseID {
		t.Fatalf("preview-bound authorization did not record fixed AFFECTED scope: %#v", scope)
	}
}
