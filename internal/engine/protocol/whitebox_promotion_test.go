package protocol

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func TestWhiteboxPendingSnapshotNeedsCurrentIndependentReview(t *testing.T) {
	e := preparationEngine(t, "blackbox", "whitebox")
	gitReceipt(t, e, "base")
	completeBatch(t, e, "B1")
	gitReceipt(t, e, "D1")
	completeBatch(t, e, "B2")
	gitReceipt(t, e, "D2")
	completeQA(t, e, "blackbox", "design")
	completeQA(t, e, "blackbox", "review")
	gitReceipt(t, e, "whitebox-workspace")
	completeQA(t, e, "whitebox", "design")
	gitReceipt(t, e, "Q1", "REVIEW_PENDING")
	loaded, _ := e.Load()
	p := loaded.State.Regular.Preparation
	if p.Blocked != "" || p.QA["whitebox"].Snapshot != "Q1" || WhiteboxPromotionReady(p) || p.Candidate != nil {
		t.Fatal("middle snapshot treated as promotion")
	}
	x := currentExecution(t, e, "whitebox-review")
	if x.Claim.Candidate != "Q1" {
		t.Fatal("review not bound to middle snapshot")
	}
	// A case-set waiver cannot stand in for the test-only review.
	ask := &RegularAsk{QA: &QARecoveryBinding{Mode: "whitebox", Task: x.Claim.Task, RequirementRevision: loaded.State.Regular.Requirement.Revision, Candidate: "Q1", CaseSetDigest: digestOfCanonical(p.QA["whitebox"].Design)}}
	for _, choice := range []string{"skip", "waiver"} {
		if err := decideQARecovery(loaded.State, ask, RegularDecision{Choice: choice, Reason: "fixture request"}, loaded.Revision); err == nil || !strings.Contains(err.Error(), "test-only review") {
			t.Fatalf("%s bypassed protection: %v", choice, err)
		}
	}
	claimAndAttach(t, e, x)
	q := p.QA["whitebox"]
	v := preparationResult(x)
	v.Status = "FAIL"
	v.Message = "Full D2..Q1 diff changes production entry; restore it before candidate promotion."
	v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SetStatus: coverage.StatusFail, SourceDecisions: []coverage.ReviewDecision{{ID: loaded.State.Regular.Requirement.Path, Status: coverage.StatusFail}}, PointDecisions: []coverage.ReviewDecision{{ID: "WHITEBOX:AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "WHITEBOX:C1", Status: coverage.StatusPass}}}
	if got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v}); got.Status != "ACCEPTED" {
		t.Fatal(got)
	}
	sourceUnavailable(t, e, x)
	loaded, _ = e.Load()
	if WhiteboxPromotionReady(loaded.State.Regular.Preparation) || loaded.State.Regular.Preparation.Candidate != nil {
		t.Fatal("review FAIL promoted")
	}
	completeQA(t, e, "whitebox", "design")
	gitReceipt(t, e, "Q2", "REVIEW_PENDING")
	x = currentExecution(t, e, "whitebox-review")
	claimAndAttach(t, e, x)
	loaded, _ = e.Load()
	q = loaded.State.Regular.Preparation.QA["whitebox"]
	v = preparationResult(x)
	v.Message = "Full D2..Q2 diff restores production entry and changes only the assertion test."
	v.Review = &coverage.ReviewResult{Binding: q.Design.Manifest.Binding, Scope: q.ReviewScope, SourceDecisions: []coverage.ReviewDecision{{ID: loaded.State.Regular.Requirement.Path, Status: coverage.StatusPass}}, PointDecisions: []coverage.ReviewDecision{{ID: "WHITEBOX:AC-001", Status: coverage.StatusPass}}, CaseDecisions: []coverage.ReviewDecision{{ID: "WHITEBOX:C1", Status: coverage.StatusPass}}}
	v.Candidate = "Q1"
	if got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v}); got.Status != "REJECTED_RESULT" {
		t.Fatal("stale review accepted", got)
	}
	sourceUnavailable(t, e, x)
	x = currentExecution(t, e, "whitebox-review")
	claimAndAttach(t, e, x)
	binding := preparationResult(x)
	v.ActionID, v.AttemptID, v.Provider, v.Identity, v.RequirementRevision, v.Candidate = binding.ActionID, binding.AttemptID, binding.Provider, binding.Identity, binding.RequirementRevision, binding.Candidate
	if got := submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &v}); got.Status != "ACCEPTED" {
		t.Fatal(got)
	}
	sourceUnavailable(t, e, x)
	loaded, _ = e.Load()
	if !WhiteboxPromotionReady(loaded.State.Regular.Preparation) {
		t.Fatal("current independent review did not authorize candidate")
	}
	gitReceipt(t, e, "final")
	loaded, _ = e.Load()
	if loaded.State.Regular.Preparation.Candidate == nil {
		t.Fatal("reviewed candidate missing")
	}
	t.Log("REVIEW_PENDING only creates an isolated review snapshot; FAIL, stale PASS and waiver cannot promote; corrected snapshot requires its own accepted review")
}

func TestWhiteboxIncrementRetainsOnlyReviewedArtifacts(t *testing.T) {
	for _, reviewed := range []bool{false, true} {
		name := "pending"
		if reviewed {
			name = "reviewed"
		}
		t.Run(name, func(t *testing.T) {
			e := preparationEngine(t, "blackbox", "whitebox")
			gitReceipt(t, e, "base")
			completeBatch(t, e, "B1")
			gitReceipt(t, e, "D1")
			completeBatch(t, e, "B2")
			gitReceipt(t, e, "D2")
			completeQA(t, e, "blackbox", "design")
			completeQA(t, e, "blackbox", "review")
			gitReceipt(t, e, "workspace")
			completeQA(t, e, "whitebox", "design")
			gitReceipt(t, e, "Q1", "REVIEW_PENDING")
			if reviewed {
				completeQA(t, e, "whitebox", "review")
				gitReceipt(t, e, "final")
			}
			before, _ := e.Load()
			batch := before.State.Regular.Plan.Batches[1]
			regularChoice(t, e, RegularRequest{Action: "development-increment", Target: "B2", Meaning: "preserved", Reason: "fixture incremental correction", Impact: map[string]string{"batch:B1": "unaffected", "blackbox-review": "affected", "whitebox-review": "affected"}, Increment: &DevelopmentIncrement{Kind: "optimize", BatchID: batch.ID, WorkspaceView: "fixture-current-workspace"}}, "confirm")
			after, _ := e.Load()
			q := after.State.Regular.Preparation.QA["whitebox"]
			if reviewed {
				if q.TestArtifactsFrom != "Q1" || q.TestArtifactsBase != "D2" {
					t.Fatal("reviewed test binding lost")
				}
			} else if q.TestArtifactsFrom != "" || q.TestArtifactsBase != "" {
				t.Fatal("unreviewed project changes carried as tests")
			}
		})
	}
}
