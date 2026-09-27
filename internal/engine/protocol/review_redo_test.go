package protocol

import (
	"fmt"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// A requirement redo creates the same obligation regardless of whether task
// termination was observed first or the superseded task used transient retries.
func TestReviewRedoHasIndependentTaskAndRetryBudget(t *testing.T) {
	type facts struct {
		task        runtime.TaskKey
		issued      int
		completed   int
		attempts    int
		maxAttempts int
	}
	var baseline *facts
	for _, terminal := range []bool{false, true} {
		for _, usedRetry := range []bool{false, true} {
			t.Run(fmt.Sprintf("terminal=%t/retry=%t", terminal, usedRetry), func(t *testing.T) {
				cd, reg := agentStepsDefinition(t, func(string) authoring.AgentSpec {
					return authoring.AgentSpec{
						Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
						ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
						Retry: &authoring.RetryPolicy{MaxAttempts: 2},
					}
				}, "product.review")
				engine, _ := reviewEngine(t, cd, reg)
				old := issueReady(t, engine)[0]
				submitSpawnReceipt(t, engine, "redo-old-spawn", old)
				original := engine.LoadMustSucceed(t).State.PendingActions[old]
				originalAttempt := engine.LoadMustSucceed(t).State.Attempts[original.Task]
				if usedRetry {
					event, err := NewWorkerResultEvent("redo-transient", old, testProvider, OutcomeRuntimeError, "sha256:redo-transient", authoring.FailureTransientEngine)
					if err != nil {
						t.Fatal(err)
					}
					accepted, err := engine.Submit(event, fingerprintOf(t, engine))
					if err != nil || accepted.RecoveryAction != string(RecoveryResumeAttempt) {
						t.Fatalf("transient failure = %+v, %v", accepted, err)
					}
					if attempt := engine.LoadMustSucceed(t).State.Attempts[original.Task]; attempt.Attempts != 1 {
						t.Fatalf("fixture did not consume a retry: %+v", attempt)
					}
				}
				if terminal {
					terminalizeDispatchTask(t, engine, old)
				}
				redoProductReview(t, engine, "identity-change")
				fresh := freshReviewAfterRedo(t, engine, old)
				snap := engine.LoadMustSucceed(t)
				pending := snap.State.PendingActions[fresh]
				attempt := snap.State.Attempts[pending.Task]
				series := snap.State.ReviewSeries.ProductReview
				if len(snap.State.PendingActions) != 1 || pending.Task == original.Task || attempt.Attempts != 0 || attempt.RetryExhausted || attempt.MaxAttempts != originalAttempt.MaxAttempts {
					t.Fatalf("redo must own a new task and full retry budget: %+v", attempt)
				}
				if attempt.Plan == originalAttempt.Plan || attempt.Bindings.Task != pending.Task {
					t.Fatalf("fresh task retained the old plan or binding: %+v", attempt)
				}
				if series.Completed != 0 || series.Issued != 2 || !series.FreshOwed {
					t.Fatalf("redo must not consume a semantic completion: %+v", series)
				}
				got := facts{pending.Task, series.Issued, series.Completed, attempt.Attempts, attempt.MaxAttempts}
				if baseline == nil {
					baseline = &got
				} else if got != *baseline {
					t.Fatalf("obligation depends on termination/retry timing: %+v, want %+v", got, *baseline)
				}
				if _, err := engine.Load(); err != nil {
					t.Fatal(err)
				}
				submitReviewResult(t, engine, "redo-fresh-pass", fresh,
					reviewEvidenceAt(review.ProductReview, fresh, snap.State.RequirementRevision(), "PASS", "new revision reviewed", nil))
				series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
				if series.Completed != 1 || series.FreshOwed || series.Effective.Status != "PASS" {
					t.Fatalf("fresh review failed to discharge the new obligation: %+v", series)
				}
			})
		}
	}
}
