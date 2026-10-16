// Review admission separates native worker completion, independent semantic
// verification, user disposition, and the confirmed correction revision.
package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func originalReviewResult(t *testing.T, engine *Engine, action string, findings []review.Finding) Event {
	t.Helper()
	event, err := NewReviewWorkerResultEvent("original-result", action, testProvider, "original-review-content", reviewEvidence(review.ProductReview, action, "", "PASS", "Complete original review", findings))
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func independentReviewObservation(t *testing.T, engine *Engine, action string, findings []ReviewFindingVerification) Event {
	t.Helper()
	source := engine.LoadMustSucceed(t).State.Results[action]
	if source.ActionID != action || source.Review == nil || source.PayloadDigest != "original-review-content" || source.Review.RequirementRevision != "rev-1" {
		t.Fatalf("independent fixture has a different original source: %+v", source)
	}
	return Event{ID: "independent-verification", Kind: KindOperatorObservation, Observation: &OperatorObservationPayload{Subject: action, Review: &ReviewVerificationObservation{ResultDigest: source.Digest, Verification: review.OperatorVerification{Checks: review.RequiredVerificationChecks(), Evidence: "Independent synthetic actor checked the exact worker payload, confirmed rev-1 and complete serial review responsibility. Source locations, documented entry, requirement match, scope and severity follow this fixture's explicitly declared scenario."}, Findings: findings}}}
}

func submitIndependentObservation(t *testing.T, engine *Engine, event Event) Acceptance {
	t.Helper()
	accepted, err := engine.Submit(event, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}

func TestReviewCompletionWaitsForIndependentAdmissionAcrossArrivalOrders(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"receipt-first", "result-first", "terminal-first"} {
		t.Run(order, func(t *testing.T) {
			cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
			engine, _ := reviewEngine(t, cd, reg)
			action := issueReady(t, engine)[0]
			pending := engine.LoadMustSucceed(t).State.PendingActions[action]
			result := originalReviewResult(t, engine, action, nil)
			if order == "result-first" {
				accepted := submitIndependentObservation(t, engine, result)
				if accepted.Status != "STAGED" || len(engine.LoadMustSucceed(t).State.PendingReviewVerifications()) != 0 {
					t.Fatal("unpaired original result created semantic authority", accepted)
				}
			}
			submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
			if order == "terminal-first" {
				for index, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					progress, err := NewTaskEvent(EventID(fmt.Sprintf("terminal-progress-%d", index)), pending.Task, pending.AttemptID, status)
					if err != nil {
						t.Fatal(err)
					}
					submitIndependentObservation(t, engine, progress)
				}
			}
			if order != "result-first" {
				submitIndependentObservation(t, engine, result)
			}
			state := engine.LoadMustSucceed(t).State
			original := state.Results[action]
			plan, _, err := engine.Plan(Observation{})
			if err != nil || plan.Next.Kind != decision.KindOperator || state.ReviewSeries.ProductReview.Completed != 0 || len(state.PendingReviewVerifications()) != 1 || len(state.PendingActions) != 0 {
				t.Fatalf("completed source skipped independent verification: next=%+v state=%+v err=%v", plan.Next, state.ReviewSeries.ProductReview, err)
			}
			observation := independentReviewObservation(t, engine, action, nil)
			submitIndependentObservation(t, engine, observation)
			state = engine.LoadMustSucceed(t).State
			if state.ReviewSeries.ProductReview.Completed != 1 || state.ReviewSeries.ProductReview.Effective.Status != "PASS" || !reflect.DeepEqual(state.Results[action], original) || len(state.PendingReviewVerifications()) != 0 {
				t.Fatal("independent admission changed the raw result or counted incorrectly")
			}
			for _, repeated := range []Event{observation, func() Event { copy := observation; copy.ID = "same-verification-new-event"; return copy }()} {
				submitIndependentObservation(t, engine, repeated)
			}
			if engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Completed != 1 {
				t.Fatal("replay counted the review twice")
			}
		})
	}
}

func TestIndependentReviewFindingsRequireIndividualDisposition(t *testing.T) {
	t.Parallel()
	for _, severity := range []string{"P1", "P2"} {
		t.Run(severity, func(t *testing.T) {
			cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
			engine, _ := reviewEngine(t, cd, reg)
			action := issueReady(t, engine)[0]
			findings := []review.Finding{{Severity: severity, Message: "Document omits the declared error outcome"}, {Severity: "P3", Message: "Documented option needs a bounded clarification"}}
			submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
			submitIndependentObservation(t, engine, originalReviewResult(t, engine, action, findings))
			submitIndependentObservation(t, engine, independentReviewObservation(t, engine, action, []ReviewFindingVerification{{Valid: true, Reason: "Independently checked the omitted outcome against the documented normal error entry and requirement"}, {Valid: true, Reason: "Independently checked the option wording against the declared scope"}}))
			state := engine.LoadMustSucceed(t).State
			plan, _, err := engine.Plan(Observation{})
			if err != nil || plan.Next.Kind != decision.KindAsk || len(plan.Next.Ask.Requests) != 2 || state.ReviewSeries.ProductReview.Completed != 1 || len(state.PendingActions) != 0 {
				t.Fatalf("finding disposition was concealed: %+v err=%v", plan.Next, err)
			}
			ids := append([]string{}, plan.Next.Ask.Requests...)
			for index, id := range ids {
				current := engine.LoadMustSucceed(t).State
				binding, ok := current.ReviewFindingBinding(id)
				if !ok {
					t.Fatal("another finding decision changed this stable Ask")
				}
				binding.Decision, binding.Reason = "DISMISS", "User dismisses this exact independently verified fixture finding"
				token, err := engine.Freshness(RequestID(id))
				if err != nil {
					t.Fatal(err)
				}
				event, err := NewReviewDecideEvent(EventID(fmt.Sprintf("finding-decision-%d", index)), RequestID(id), token, AskOptionID("dismiss:"+binding.FindingID), ReviewDecisionPayload{Correction: &binding})
				if err != nil {
					t.Fatal(err)
				}
				submitIndependentObservation(t, engine, event)
				if index == 0 && engine.LoadMustSucceed(t).State.PendingAsks[ids[1]].Resolved {
					t.Fatal("disposing one finding retired another finding's stable Ask")
				}
			}
			state = engine.LoadMustSucceed(t).State
			if state.ReviewSeries.ProductReview.Completed != 1 || !state.effectivePass(state.ReviewSeries.ProductReview) || len(state.ReviewSeries.ProductReview.RawCandidates) != 1 {
				t.Fatal("dismissal required a new review or left a false obligation")
			}
		})
	}
}

func TestIndependentDiscardPreservesOriginalAndCreatesNoUserObligation(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
	submitIndependentObservation(t, engine, originalReviewResult(t, engine, action, []review.Finding{{Severity: "P1", Message: "Malicious manual state edits can corrupt the projection"}}))
	original := engine.LoadMustSucceed(t).State.Results[action]
	submitIndependentObservation(t, engine, independentReviewObservation(t, engine, action, []ReviewFindingVerification{{Valid: false, Reason: "This candidate only describes malicious manual state rewriting, which reviewer-base excludes from the documented-use scope"}}))
	state := engine.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.Completed != 1 || !state.effectivePass(state.ReviewSeries.ProductReview) || len(state.PendingAsks) != 0 || !reflect.DeepEqual(state.Results[action], original) || !strings.Contains(state.ReviewSeries.ProductReview.RawCandidates[0].OperatorVerification.Evidence, "valid=false") {
		t.Fatal("discard created a user obligation or lost original/independent evidence")
	}
}

func TestIndependentInvalidResultUsesItsOriginalFiniteRetry(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	pending := engine.LoadMustSucceed(t).State.PendingActions[action]
	submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
	submitIndependentObservation(t, engine, originalReviewResult(t, engine, action, nil))
	original := engine.LoadMustSucceed(t).State.Results[action]
	event := independentReviewObservation(t, engine, action, nil)
	event.Observation.Review.InvalidReason = "The original review explicitly did not read its complete confirmed requirement; this result is incomplete"
	submitIndependentObservation(t, engine, event)
	state := engine.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.Completed != 0 || state.RejectedResults[action].Status != "REJECTED_INVALID_RESULT" || !reflect.DeepEqual(state.Results[action], original) {
		t.Fatal("invalid independent review was counted or changed its original source")
	}
	if len(state.PendingActions) != 1 {
		t.Fatalf("invalid result did not resume its one original finite duty: %+v", state.PendingActions)
	}
	for _, retry := range state.PendingActions {
		if retry.Task != pending.Task || retry.ActionID == action || state.Attempts[retry.Task].Attempts != 1 {
			t.Fatal("invalid result created a new semantic round instead of the original retry", retry)
		}
	}
}

func TestIncompleteIndependentVerificationLeavesTheOriginalDutyUnchanged(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
	submitIndependentObservation(t, engine, originalReviewResult(t, engine, action, nil))
	before, err := json.Marshal(engine.LoadMustSucceed(t))
	if err != nil {
		t.Fatal(err)
	}
	observation := independentReviewObservation(t, engine, action, nil)
	observation.Observation.Review.Verification.Checks = []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope"}
	if _, err := engine.Submit(observation, Observation{}); err == nil || rejectionCode(t, err) != CodeReviewEvidenceRejected {
		t.Fatal("incomplete independent verification created semantic authority", err)
	}
	after, err := json.Marshal(engine.LoadMustSucceed(t))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected independent verification rewrote the original duty")
	}
}

func TestReviewCostHeldResultReopensWithTheSameIndependentDuty(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	action := issueReady(t, engine)[0]
	held := true
	engine.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
		if ref.RunID != "run-review" || ref.ActionID != action || ref.Provider != testProvider || ref.Correlation != "original-reviewer" {
			t.Fatalf("cost fixture received another native source: %+v", ref)
		}
		if held {
			return ResolvedCostSource{State: CostSourcePending}
		}
		return ResolvedCostSource{State: CostSourceReady, Usage: cost.Usage{InputCacheMissTokens: 120, OutputTokens: 30}}
	}
	submitIndependentObservation(t, engine, spawnReceipt(t, "source-receipt", action, "original-reviewer"))
	accepted := submitIndependentObservation(t, engine, originalReviewResult(t, engine, action, nil))
	state := engine.LoadMustSucceed(t).State
	if accepted.Status != StatusCostPending || len(state.PendingReviewVerifications()) != 0 || state.ReviewSeries.ProductReview.Completed != 0 || state.StagedResults[action].Review == nil {
		t.Fatal("unsettled native cost created semantic authority or lost the held source")
	}
	held = false
	submitIndependentObservation(t, engine, lifecycleEventOf(t, "native-source-settled", "original-reviewer", LifecycleStart))
	reopened, err := New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	state = reopened.LoadMustSucceed(t).State
	if len(state.StagedResults) != 0 || len(state.PendingReviewVerifications()) != 1 || state.ReviewSeries.ProductReview.Completed != 0 || state.Cost.TotalInputTokens != 120 || state.Cost.OutputTokens != 30 {
		t.Fatal("cost settlement or reopen lost the original independent duty")
	}
	observation := independentReviewObservation(t, reopened, action, nil)
	submitIndependentObservation(t, reopened, observation)
	submitIndependentObservation(t, reopened, observation)
	state = reopened.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.Completed != 1 || state.Cost.TotalInputTokens != 120 || state.Cost.OutputTokens != 30 {
		t.Fatal("independent admission/replay recounted the review or native cost")
	}
}
