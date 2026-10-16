package protocol

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// A requirement redo creates the same obligation regardless of whether task
// termination was observed first or the superseded task used transient retries.
func TestReviewRedoHasIndependentTaskAndRetryBudget(t *testing.T) {
	t.Parallel()
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
					accepted, err := engine.Submit(event, Observation{})
					if err != nil || accepted.Status != OutcomeRuntimeError {
						t.Fatalf("transient failure = %+v, %v", accepted, err)
					}
					if attempt := engine.LoadMustSucceed(t).State.Attempts[original.Task]; attempt.Attempts != 1 {
						t.Fatalf("fixture did not consume a retry: %+v", attempt)
					}
				}
				if usedRetry {
					old = pendingActionID(t, engine)
					submitSpawnReceipt(t, engine, "redo-retry-spawn", old)
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
				submitVerifiedReviewResult(t, engine, "redo-fresh-pass", fresh,
					reviewEvidenceAt(review.ProductReview, fresh, snap.State.RequirementRevision(), "PASS", "new revision reviewed", nil))
				series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
				if series.Completed != 1 || series.FreshOwed || series.Effective.Status != "PASS" {
					t.Fatalf("fresh review failed to discharge the new obligation: %+v", series)
				}
			})
		}
	}
}

func TestRequirementRedoRetiresExhaustedReviewDuty(t *testing.T) {
	t.Parallel()
	cd, reg := agentStepsDefinition(t, func(string) authoring.AgentSpec {
		return authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview, ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 2}}
	}, "product.review")
	e, _ := reviewEngine(t, cd, reg)
	id := issueReady(t, e)[0]
	original := e.LoadMustSucceed(t).State.PendingActions[id].Task
	for i := 0; i < 2; i++ {
		id = pendingActionID(t, e)
		submitSpawnReceipt(t, e, fmt.Sprintf("spawn-%d", i), id)
		event, err := NewWorkerResultEvent(EventID(fmt.Sprintf("runtime-%d", i)), id, testProvider, OutcomeRuntimeError, fmt.Sprintf("sha256:failure-%d", i), authoring.FailureTransientEngine)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Submit(event, Observation{}); err != nil {
			t.Fatal(err)
		}
	}
	before := e.LoadMustSucceed(t)
	if !before.State.EndedAttempts[original].RetryExhausted {
		t.Fatal("review did not exhaust original policy")
	}
	raw := before.State.RejectedResults
	redoProductReview(t, e, "new-review-responsibility")
	fresh := freshReviewAfterRedo(t, e, id)
	after := e.LoadMustSucceed(t)
	if _, old := after.State.EndedAttempts[original]; old {
		t.Fatal("series REDO retained exhausted attempt control")
	}
	for _, ask := range after.State.PendingAsks {
		if !ask.Resolved && ask.Validation != nil && ask.Validation.Target == original.String() {
			t.Fatal("series REDO left old retry actionable")
		}
	}
	if !reflect.DeepEqual(raw, after.State.RejectedResults) {
		t.Fatal("series REDO changed original rejection evidence")
	}
	pending := after.State.PendingActions[fresh]
	if pending.Task == original || after.State.Attempts[pending.Task].Attempts != 0 {
		t.Fatal("new series duty reused old retry allowance")
	}
	submitVerifiedReviewResult(t, e, "fresh-review-pass", fresh, reviewEvidenceAt(review.ProductReview, fresh, after.State.RequirementRevision(), "PASS", "new revision reviewed", nil))
	if got := e.LoadMustSucceed(t).State.ReviewSeries.ProductReview; got.Completed != 1 || got.FreshOwed {
		t.Fatalf("new duty failed to settle once: %+v", got)
	}
}
