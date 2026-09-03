package validate

import (
	"strings"
	"testing"
)

func TestPreDevelopmentReviewSeriesRejectsPartialAndTamperedProjection(t *testing.T) {
	state := NewRunState("series-unit", "formal", "requirements.md", "req-rev", "git", "base", "current", "prompt", "catalog", false, nil, nil)
	if err := validatePreDevelopmentReviewState(state); err != nil {
		t.Fatal(err)
	}

	partial := state
	partial.PreDevelopmentReviewSeries = newPreDevelopmentReviewSeries()
	delete(partial.PreDevelopmentReviewSeries, "start-readiness")
	if err := validatePreDevelopmentReviewState(partial); err == nil || !strings.Contains(err.Error(), "expected exactly") {
		t.Fatalf("partial series was accepted: %v", err)
	}

	result, err := recordSemanticReviewCandidate(&state, "product-review", PreparedDispatch{ID: "dispatch-unit", Target: "product-review", RequirementRevision: "req-rev"}, ActionResult{Status: "PASS", Findings: []Finding{{Severity: "P2", Message: "advisory"}}})
	if err != nil {
		t.Fatal(err)
	}
	state.Actions["product-review"] = result
	if err := validatePreDevelopmentReviewState(state); err != nil {
		t.Fatal(err)
	}

	series := state.PreDevelopmentReviewSeries["product-review"]
	series.RawCandidates[0].Findings[0].Message = "tampered raw evidence"
	state.PreDevelopmentReviewSeries["product-review"] = series
	if err := validatePreDevelopmentReviewState(state); err == nil || !strings.Contains(err.Error(), "raw finding identity") {
		t.Fatalf("tampered raw evidence was accepted: %v", err)
	}
}

func TestPreDevelopmentReviewCorrectionProjectionKeepsOneRound(t *testing.T) {
	state := NewRunState("correction-unit", "formal", "requirements.md", "req-rev", "git", "base", "current", "prompt", "catalog", true, nil, nil)
	dispatch := PreparedDispatch{ID: "dispatch-correction", Target: "product-review", RequirementRevision: "req-rev"}
	result, err := recordSemanticReviewCandidate(&state, "product-review", dispatch, ActionResult{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "candidate blocker"}}})
	if err != nil {
		t.Fatal(err)
	}
	state.Actions["product-review"] = result
	series := state.PreDevelopmentReviewSeries["product-review"]
	candidate := series.RawCandidates[0]
	finding := candidate.Findings[0]

	if err := applyReviewCorrection(&state, ReviewCorrectionInput{Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: finding.ID, Decision: "DISMISS", Reason: "user rejects false blocker", Source: "user receipt", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}); err != nil {
		t.Fatal(err)
	}
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 1 || series.EffectiveReviewStatus.Status != "PASS" || len(series.RawCandidates) != 1 || len(series.Adjudications) != 1 {
		t.Fatalf("finding disposition changed raw evidence or round count: %#v", series)
	}

	if err := applyReviewCorrection(&state, ReviewCorrectionInput{Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "MISBOUND", Reason: "operator verified whole-result misbinding", Source: "user receipt", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}); err != nil {
		t.Fatal(err)
	}
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 0 || series.EffectiveReviewStatus.Status != "PENDING" || len(series.RawCandidates) != 1 {
		t.Fatalf("whole-result invalidation did not exclude only the count/effective projection: %#v", series)
	}
}

func TestConfirmedBlockingFindingRequiresChangedRequirementBeforeFreshReview(t *testing.T) {
	state := NewRunState("confirmed-blocker-unit", "formal", "requirements.md", "req-rev", "git", "base", "current", "prompt", "catalog", true, nil, nil)
	dispatch := PreparedDispatch{ID: "dispatch-confirmed-blocker", Target: "product-review", RequirementRevision: state.RequirementRevision}
	result, err := recordSemanticReviewCandidate(&state, "product-review", dispatch, ActionResult{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "confirmed requirement defect"}}})
	if err != nil {
		t.Fatal(err)
	}
	state.Actions["product-review"] = result
	series := state.PreDevelopmentReviewSeries["product-review"]
	candidate := series.RawCandidates[0]
	finding := candidate.Findings[0]
	if err := applyReviewCorrection(&state, ReviewCorrectionInput{
		Action:                    "product-review",
		DispatchID:                candidate.DispatchID,
		ResultDigest:              candidate.ResultDigest,
		RequirementRevision:       candidate.RequirementRevision,
		FindingID:                 finding.ID,
		Decision:                  "CONFIRM",
		Reason:                    "user confirms the requirement defect",
		Source:                    "user receipt",
		UserAuthorized:            true,
		ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision,
	}); err != nil {
		t.Fatal(err)
	}

	for _, requested := range []bool{false, true} {
		err := requirePreDevelopmentReviewPreparation(state, "product-review", requested, "user asks for another review")
		if err == nil || !strings.Contains(err.Error(), "changed and reconfirmed requirement revision") {
			t.Fatalf("fresh review against the unchanged requirement was accepted (user-requested=%t): %v", requested, err)
		}
	}

	state.RequirementRevision = "req-rev-2"
	markPreDevelopmentReviewsStale(&state, "REQUIREMENT_REVISION_INVALIDATION")
	if err := requirePreDevelopmentReviewPreparation(state, "product-review", false, ""); err != nil {
		t.Fatalf("changed requirement revision did not reopen fresh review: %v", err)
	}
}

func TestHistoricalWholeResultInvalidationPreservesCurrentEffectiveCandidate(t *testing.T) {
	t.Run("superseded candidate", func(t *testing.T) {
		state := NewRunState("historical-correction", "formal", "requirements.md", "req-rev", "git", "base", "current", "prompt", "catalog", true, nil, nil)
		firstResult, err := recordSemanticReviewCandidate(&state, "product-review", PreparedDispatch{ID: "dispatch-first", Target: "product-review", RequirementRevision: "req-rev"}, ActionResult{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "first blocker"}}})
		if err != nil {
			t.Fatal(err)
		}
		state.Actions["product-review"] = firstResult
		first := state.PreDevelopmentReviewSeries["product-review"].RawCandidates[0]
		secondResult, err := recordSemanticReviewCandidate(&state, "product-review", PreparedDispatch{ID: "dispatch-second", Target: "product-review", RequirementRevision: "req-rev"}, ActionResult{Status: "PASS", Findings: []Finding{}})
		if err != nil {
			t.Fatal(err)
		}
		state.Actions["product-review"] = secondResult
		series := state.PreDevelopmentReviewSeries["product-review"]
		second := series.RawCandidates[1]
		beforeRevision := series.EffectiveReviewStatus.Revision

		if err := applyReviewCorrection(&state, ReviewCorrectionInput{Action: "product-review", DispatchID: first.DispatchID, ResultDigest: first.ResultDigest, RequirementRevision: first.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "INVALID", Reason: "user invalidates the historical result", Source: "user receipt", UserAuthorized: true, ExpectedEffectiveRevision: beforeRevision}); err != nil {
			t.Fatal(err)
		}
		series = state.PreDevelopmentReviewSeries["product-review"]
		if series.Completed != 1 || len(series.RawCandidates) != 2 || len(series.Corrections) != 1 || !candidateExcluded(series, first.ResultDigest) {
			t.Fatalf("historical invalidation did not preserve raw evidence and remove one round: %#v", series)
		}
		if series.EffectiveReviewStatus.Status != "PASS" || series.EffectiveReviewStatus.ResultDigest != second.ResultDigest || series.EffectiveReviewStatus.Revision != beforeRevision+1 || state.Actions["product-review"].Status != "PASS" {
			t.Fatalf("historical invalidation overwrote the current effective candidate: series=%#v action=%#v", series, state.Actions["product-review"])
		}
		if err := applyReviewCorrection(&state, ReviewCorrectionInput{Action: "product-review", DispatchID: first.DispatchID, ResultDigest: first.ResultDigest, RequirementRevision: first.RequirementRevision, FindingID: first.Findings[0].ID, Decision: "DISMISS", Reason: "stale finding correction", Source: "user receipt", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}); err == nil || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("finding-level correction accepted a historical candidate: %v", err)
		}
	})

	t.Run("candidate from prior requirement revision", func(t *testing.T) {
		state := NewRunState("historical-revision-correction", "formal", "requirements.md", "old-rev", "git", "base", "current", "prompt", "catalog", true, nil, nil)
		result, err := recordSemanticReviewCandidate(&state, "start-readiness", PreparedDispatch{ID: "dispatch-old-revision", Target: "start-readiness", RequirementRevision: "old-rev"}, ActionResult{Status: "PASS", Findings: []Finding{}})
		if err != nil {
			t.Fatal(err)
		}
		state.Actions["start-readiness"] = result
		candidate := state.PreDevelopmentReviewSeries["start-readiness"].RawCandidates[0]
		state.RequirementRevision = "new-rev"
		markPreDevelopmentReviewsStale(&state, "REQUIREMENT_REVISION_INVALIDATION")
		series := state.PreDevelopmentReviewSeries["start-readiness"]

		if err := applyReviewCorrection(&state, ReviewCorrectionInput{Action: "start-readiness", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "STALE", Reason: "user invalidates the prior-revision result", Source: "user receipt", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}); err != nil {
			t.Fatal(err)
		}
		series = state.PreDevelopmentReviewSeries["start-readiness"]
		if series.Completed != 0 || len(series.RawCandidates) != 1 || !candidateExcluded(series, candidate.ResultDigest) || series.EffectiveReviewStatus.Status != "PENDING" || state.Actions["start-readiness"].Status != "PENDING" {
			t.Fatalf("prior-revision invalidation did not preserve evidence and return to PENDING: %#v", series)
		}
	})
}
