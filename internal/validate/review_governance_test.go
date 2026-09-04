package validate

import (
	"strings"
	"testing"
)

func completeGovernanceFinding(message string, gate bool) FindingInput {
	finding := FindingInput{
		Severity:          "P1",
		SeverityRationale: "The normal documented workflow cannot complete, so the defect blocks the confirmed requirement.",
		Category:          FindingCategoryRequirementConflict,
		Requirement:       "REQ-001",
		NormalPath:        "Run the documented workflow from a clean checkout with the confirmed requirement.",
		DeferralImpact:    "Users remain unable to complete the documented workflow until the defect is corrected.",
		Message:           message,
		Locations:         []string{"requirements.md:1"},
		Verification: FindingVerification{
			Scope:          "Compared the finding with the complete requested batch and excluded unrelated surfaces.",
			Severity:       "Verified that the observed failure blocks the normal path and is therefore P1.",
			Causality:      "Reproduced the failure from the bound requirement and traced it to the cited evidence.",
			Reproduction:   "Repeated the normal-path steps on the current candidate and observed the same failure.",
			Classification: "Verified that this is a requirement conflict, not an implementation-detail preference.",
		},
	}
	if gate {
		finding.Category = ""
		finding.ActualError = "The command exits nonzero before producing the required result."
		finding.Verification.Classification = ""
	}
	return finding
}

func confirmedStartFindingRun(t *testing.T, runID string) (string, string, RunState, string) {
	t.Helper()
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, runID))
	state = recordProductReview(t, root, pkg, state)
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	finding := completeGovernanceFinding("the confirmed requirement conflicts with the normal start path", false)
	state, err := RecordActionWithOperatorVerification(root, pkg, state.RunID, "start-readiness", dispatch, "FAIL", "", []FindingInput{finding}, false, "", legacyOperatorVerification())
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordSettledFindings(root, pkg, state.RunID, "start-readiness", []string{finding.Message}, nil)
	if err != nil {
		t.Fatal(err)
	}
	series := state.PreDevelopmentReviewSeries["start-readiness"]
	candidate, ok := semanticCandidateByDigest(series, series.EffectiveReviewStatus.ResultDigest)
	if !ok || len(candidate.Findings) != 1 {
		t.Fatalf("confirmed finding candidate missing: %#v", series)
	}
	return root, pkg, state, candidate.Findings[0].ID
}

func previewDeltaForFinding(t *testing.T, root, pkg string, state RunState, findingID, scope string) ReviewDeltaPackage {
	t.Helper()
	writeTestFile(t, resolveFromRoot(root, state.RequirementSource), testRequirementDocument("Revised requirement produced from the confirmed review finding."))
	commitAll(t, root, "revise requirement from confirmed review finding")
	delta, err := PreviewReviewDelta(root, pkg, state.RunID, "", nil, ReviewDeltaPackage{
		Sources:           []ReviewDeltaSource{{Action: "start-readiness", FindingID: findingID, Decision: "CONFIRM"}},
		Hunks:             []ReviewDeltaHunk{{ID: "HUNK-001", Affected: []string{"REQ-001"}, SourceFindingIDs: []string{findingID}, Disposition: "CONFIRM", Impact: "Corrects the confirmed requirement conflict.", Verification: "Compared the complete base text captured before editing with the complete candidate and found no unrelated hunk."}},
		AffectedActions:   []string{"start-readiness"},
		ConfirmationScope: scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	return delta
}

func TestA1InvalidGateResultReceiptAndFiniteTaskRetry(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDeliveryForRoute(t, root, pkg, "a1-invalid-result", "custom", []string{"quality"})
	invalid := completeGovernanceFinding("quality gate blocker", true)
	invalid.SeverityRationale = ""
	invalid.Locations = []string{"delivery-a1-invalid-result.txt:1"}

	first := prepareAndClaim(t, root, pkg, state.RunID, "quality", "a1-quality-1")
	if _, err := RecordGateWithEvidence(root, pkg, state.RunID, "quality", first, "FAIL", "", comparedRange(state), []FindingInput{invalid}); err == nil || !strings.Contains(err.Error(), "REJECTED_INVALID_RESULT") {
		t.Fatalf("invalid finding was not rejected with a receipt: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if len(state.RejectedResults) != 1 || state.Gates["quality"].Status != "PENDING" || state.Dispatches[first].ResultStatus != "REJECTED_INVALID_RESULT" {
		t.Fatalf("invalid result changed semantic state or lost its receipt: %#v", state)
	}
	taskKey := state.Dispatches[first].TaskKey
	if state.DispatchObligations[taskKey].Status != "RETRYABLE" {
		t.Fatalf("first invalid attempt was not retryable: %#v", state.DispatchObligations[taskKey])
	}

	second := prepareAndClaim(t, root, pkg, state.RunID, "quality", "a1-quality-2")
	if state, _ = LoadRunState(root, state.RunID); state.Dispatches[second].TaskKey != taskKey || state.Dispatches[second].Attempt != 2 {
		t.Fatalf("retry did not reuse TaskKey/attempt sequence: %#v", state.Dispatches[second])
	}
	if _, err := RecordGateWithEvidence(root, pkg, state.RunID, "quality", second, "FAIL", "", comparedRange(state), []FindingInput{invalid}); err == nil || !strings.Contains(err.Error(), "REJECTED_INVALID_RESULT") {
		t.Fatalf("second invalid result was not rejected: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.DispatchObligations[taskKey].Status != "RETRY_EXHAUSTED" || len(state.RejectedResults) != 2 {
		t.Fatalf("finite retry budget was not exhausted: %#v", state.DispatchObligations[taskKey])
	}
	before := stateBytes(t, root, state.RunID)
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "RETRY_EXHAUSTED") {
		t.Fatalf("exhausted TaskKey prepared another automatic attempt: %v", err)
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected exhausted prepare changed state")
	}
	if _, err := AuthorizeDispatchRetry(root, pkg, state.RunID, taskKey, "stale-state-revision", "user authorizes a final attempt"); err == nil {
		t.Fatal("stale retry authorization was accepted")
	}
	state, err := AuthorizeDispatchRetry(root, pkg, state.RunID, taskKey, state.StateIntegrity, "user authorizes exactly one final attempt")
	if err != nil {
		t.Fatal(err)
	}
	third := prepareAndClaim(t, root, pkg, state.RunID, "quality", "a1-quality-3")
	valid := completeGovernanceFinding("quality gate blocker", true)
	valid.Locations = []string{"delivery-a1-invalid-result.txt:1"}
	state, err = RecordGateWithEvidence(root, pkg, state.RunID, "quality", third, "FAIL", "", comparedRange(state), []FindingInput{valid})
	if err != nil {
		t.Fatal(err)
	}
	if state.Gates["quality"].Status != "FAIL" || state.Dispatches[third].Attempt != 3 || state.Dispatches[third].TaskKey != taskKey {
		t.Fatalf("authorized final attempt was not authoritative: %#v", state.Dispatches[third])
	}
	state, _ = LoadRunState(root, state.RunID)
	if _, err := AuthorizeDispatchRetry(root, pkg, state.RunID, taskKey, state.StateIntegrity, "try to add another attempt"); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("second extra-attempt authorization was accepted: %v", err)
	}
}

func TestA1StartReadinessCategoryAndRationaleProjection(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "a1-start-category"))
	state = recordProductReview(t, root, pkg, state)
	invalid := completeGovernanceFinding("implementation preference incorrectly classified as blocking", false)
	invalid.Category = FindingCategoryImplementation
	first := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	if _, err := RecordActionWithOperatorVerification(root, pkg, state.RunID, "start-readiness", first, "FAIL", "", []FindingInput{invalid}, false, "", legacyOperatorVerification()); err == nil || !strings.Contains(err.Error(), "at most P2") {
		t.Fatalf("implementation-detail P1 was not rejected: %v", err)
	}
	second := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	valid := completeGovernanceFinding("confirmed requirement conflict blocks startup", false)
	state, err := RecordActionWithOperatorVerification(root, pkg, state.RunID, "start-readiness", second, "FAIL", "", []FindingInput{valid}, false, "", legacyOperatorVerification())
	if err != nil {
		t.Fatal(err)
	}
	got := state.Actions["start-readiness"].Findings
	if len(got) != 1 || got[0].ID == "" || got[0].SeverityRationale != valid.SeverityRationale || got[0].Category != FindingCategoryRequirementConflict {
		t.Fatalf("effective projection omitted stable id/rationale/category: %#v", got)
	}
}

func TestA1ReviewDeltaMeaningPreservedProjection(t *testing.T) {
	root, pkg, state, findingID := confirmedStartFindingRun(t, "a1-delta-preserved")
	delta := previewDeltaForFinding(t, root, pkg, state, findingID, "REVIEW_DELTA")
	delta.UserConfirmation = "user confirms the displayed complete review delta"
	updated, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil, RequirementUpdateOptions{ReviewDelta: &delta})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Actions["product-review"].Status != "PASS" || updated.Actions["start-readiness"].Status != "PENDING" || len(updated.ReviewDeltaReceipts) != 1 {
		t.Fatalf("meaning-preserved transition invalidated unrelated PASS or lost receipt: %#v", updated)
	}
	if updated.PreDevelopmentReviewSeries["start-readiness"].EffectiveReviewStatus.Status != "PENDING" {
		t.Fatalf("affected review projection was not marked pending: %#v", updated.PreDevelopmentReviewSeries["start-readiness"])
	}
}

func TestA1MeaningChangedPreviewIsReadOnlyAndApplyIsAtomic(t *testing.T) {
	root, pkg, state, findingID := confirmedStartFindingRun(t, "a1-impact-changed")
	delta := previewDeltaForFinding(t, root, pkg, state, findingID, "REVIEW_DELTA_AND_REQUIREMENT_IMPACT")
	beforePreview := stateBytes(t, root, state.RunID)
	impact := RequirementImpactInput{Scope: "The confirmed startup requirement changes and requires Product Review to be repeated."}
	preview, err := PreviewRequirementImpact(root, pkg, state.RunID, "", nil, delta.Digest, impact)
	if err != nil {
		t.Fatal(err)
	}
	if stateBytes(t, root, state.RunID) != beforePreview {
		t.Fatal("read-only impact preview changed run state")
	}
	delta.UserConfirmation = "user confirms the complete delta and displayed rollback impact"
	delta.ImpactPreviewDigest = preview.Digest
	bad := RequirementUpdateOptions{ReviewDelta: &delta, ImpactAuthorization: &RequirementImpactAuthorization{PreviewDigest: "stale-preview", StateRevision: preview.StateRevision, Impact: impact}}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, "changed", nil, bad); err == nil || !strings.Contains(err.Error(), "preview digest") {
		t.Fatalf("stale impact authorization was accepted: %v", err)
	}
	if stateBytes(t, root, state.RunID) != beforePreview {
		t.Fatal("failed meaning-changed apply partially changed state")
	}
	good := RequirementUpdateOptions{ReviewDelta: &delta, ImpactAuthorization: &RequirementImpactAuthorization{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision, Impact: impact}}
	updated, err := UpdateRequirement(root, pkg, state.RunID, "", true, "changed", nil, good)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.RequirementConfirmed || updated.Actions["requirements-clarification"].Status != "PASS" || updated.Actions["product-review"].Status != "PENDING" {
		t.Fatalf("meaning-changed transition did not atomically confirm candidate and roll back Product Review: %#v", updated.Actions)
	}
	receipt := updated.ReviewDeltaReceipts[len(updated.ReviewDeltaReceipts)-1]
	if receipt.ImpactPreviewDigest != preview.Digest || receipt.ImpactStateRevision != preview.StateRevision || receipt.ImpactConfirmationSource == "" || receipt.Impact == nil {
		t.Fatalf("impact confirmation was not durably bound to the atomic transition: %#v", receipt)
	}
}
