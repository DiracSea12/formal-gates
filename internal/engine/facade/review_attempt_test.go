package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

// Invalid worker content ends its execution without completing a semantic review.
func TestReadinessContentUsesFiniteOriginalAttempt(t *testing.T) {
	t.Parallel()
	for _, delivery := range []string{"receipt-first", "result-first", "terminal-first", "carried-result-first"} {
		for _, shape := range []string{"valid", "missing", "bad-dependency"} {
			t.Run(delivery+"/"+shape, func(t *testing.T) {
				d := startValidationDriver(t, nil)
				submitReview := func(id string, kind review.Action, plan *protocol.GranularityReview) protocol.Event {
					input, issued := driverState(t, d).WorkerInput(id, d.f.definition)
					if !issued || input.Review == nil {
						t.Fatal("review fixture has no original issued responsibility")
					}
					if plan != nil {
						fixturePlanRequirements(plan, input.RequirementPoints)
					}
					event, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, DefaultProvider, string(d.eventID()), protocol.ReviewResultEvidence{Action: kind, DispatchID: id, RequirementRevision: input.Review.RequirementRevision, Status: "PASS", GranularityReview: plan, Findings: []review.Finding{}})
					if err != nil {
						t.Fatal(err)
					}
					return event
				}
				var product string
				for id, p := range d.run.PendingActions {
					if p.ReviewSeries == review.ProductReview {
						product = id
					}
				}
				d.receipt(product)
				productResult := submitReview(product, review.ProductReview, nil)
				d.submit(productResult)
				verifyReviewFixture(t, d, productResult)
				d.drive()
				var readiness string
				for id, p := range d.run.PendingActions {
					if p.ReviewSeries == review.StartReadiness {
						readiness = id
					}
				}
				task := d.run.PendingActions[readiness].Task
				plan := commandPlan()
				if shape == "missing" {
					plan = nil
				}
				if shape == "bad-dependency" {
					plan.Batches[0].DependsOn = []string{"missing-batch"}
				}
				event := submitReview(readiness, review.StartReadiness, plan)
				if delivery == "receipt-first" || delivery == "terminal-first" {
					d.receipt(readiness)
				}
				if delivery == "terminal-first" {
					attempt := d.run.PendingActions[readiness].AttemptID
					for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
						progress, err := protocol.NewTaskEvent(d.eventID(), task, attempt, status)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(progress)
					}
				}
				if delivery == "carried-result-first" {
					confirmValidationWork(d, "")
					d.reopen()
				}
				acceptance, _, submitErr := d.f.Submit(event)
				t.Logf("worker submit status=%s err=%v", acceptance.Status, submitErr)
				if delivery == "result-first" || delivery == "carried-result-first" {
					d.receipt(readiness)
				}
				d.reopen()
				d.drive()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("readiness=%s task=%s pending=%t rejected=%+v ended=%+v staged=%d completed=%d", readiness, task, snap.State.PendingActions[readiness].ActionID != "", snap.State.RejectedResults[readiness], snap.State.EndedAttempts[task], len(snap.State.StagedResults), snap.State.ReviewSeries.StartReadiness.Completed)
				if shape == "valid" {
					if snap.State.ReviewSeries.StartReadiness.Completed != 0 || len(d.run.ReviewVerifications) != 1 {
						t.Fatal("completed source bypassed independent plan verification")
					}
					verifyReviewFixture(t, d, event)
					snap, err = d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if snap.State.ReviewSeries.StartReadiness.Completed != 1 {
						t.Fatal("valid plan negative control not accepted")
					}
					return
				}
				if snap.State.RejectedResults[readiness].Status != "REJECTED_INVALID_RESULT" {
					t.Fatalf("invalid completed readiness plan did not close original attempt or enter finite retry")
				}
				original, err := json.Marshal(snap.State.RejectedResults[readiness].Payload.Review.GranularityReview)
				if err != nil {
					t.Fatal(err)
				}
				expected, _ := json.Marshal(plan)
				if string(original) != string(expected) {
					t.Fatal("rejected evidence changed the original plan")
				}
				before, _ := json.Marshal(snap)
				duplicate, _, err := d.f.Submit(event)
				if err != nil {
					t.Fatal(err)
				}
				// Receipt-first already returned the final rejection; result-first keeps its stable original STAGED receipt.
				if duplicate.Status != acceptance.Status {
					t.Fatalf("same-event acceptance changed: %s -> %s", acceptance.Status, duplicate.Status)
				}
				repeated, _ := d.f.engine.Load()
				after, _ := json.Marshal(repeated)
				if string(before) != string(after) {
					t.Fatal("same invalid event rewrote durable state")
				}
				var replacement string
				for id, p := range d.run.PendingActions {
					if p.Task == task {
						replacement = id
					}
				}
				if replacement == "" || replacement == readiness {
					t.Fatal("first invalid result did not create finite replacement for same task")
				}
				if snap.State.Attempts[task].Attempts != 1 {
					t.Fatal("first invalid result changed retry count incorrectly")
				}
				second := submitReview(replacement, review.StartReadiness, plan)
				if delivery == "receipt-first" || delivery == "terminal-first" {
					d.receipt(replacement)
				}
				_, _, err = d.f.Submit(second)
				if err != nil {
					t.Fatal(err)
				}
				if delivery == "result-first" || delivery == "carried-result-first" {
					d.receipt(replacement)
				}
				d.reopen()
				d.drive()
				exhausted, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				ended := exhausted.State.EndedAttempts[task]
				if !ended.RetryExhausted || ended.Attempts != 2 || ended.MaxAttempts != 2 {
					t.Fatalf("invalid plan escaped finite original task budget: %+v", ended)
				}
				if exhausted.State.ReviewSeries.StartReadiness.Completed != 0 || len(exhausted.State.StagedResults) != 0 {
					t.Fatal("invalid plan counted semantic review or remained staged")
				}
				for _, p := range exhausted.State.PendingActions {
					if p.Task == task {
						t.Fatal("exhausted task issued without authorization")
					}
				}
				decideValidation(d, d.validationAsk("RETRY"), "approve")
				var granted string
				for id, pending := range d.run.PendingActions {
					if pending.Task == task {
						granted = id
					}
				}
				if granted == "" || d.run.Attempts[task].Attempts != 2 || d.run.Attempts[task].MaxAttempts != 3 {
					t.Fatal("extra attempt lost original allowance")
				}
				d.receipt(granted)
				replacementResult := submitReview(granted, review.StartReadiness, commandPlan())
				d.submit(replacementResult)
				verifyReviewFixture(t, d, replacementResult)
				completed, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if completed.State.ReviewSeries.StartReadiness.Completed != 1 || completed.State.ReviewSeries.StartReadiness.FirstPass != true {
					t.Fatal("valid authorized replacement did not establish one effective review")
				}
				if !reflect.DeepEqual(completed.State.RejectedResults[readiness], snap.State.RejectedResults[readiness]) {
					t.Fatal("later success rewrote original invalid plan")
				}

			})
		}
	}
}
