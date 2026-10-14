package protocol

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
)

func TestRequirementBarrierBeforePackagePreparation(t *testing.T) {
	for _, choice := range []string{"abort", "proceed"} {
		t.Run(choice, func(t *testing.T) {
			engine, dir := canonicalReviewEngine(t)
			action := issueReady(t, engine)[0]
			request, err := NewRequestEvent("begin-change", ControlRequestRequirementChange, requirementOptions()...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = engine.Submit(request, Observation{}); err != nil {
				t.Fatal(err)
			}
			plan, _, err := engine.Plan(Observation{})
			if err != nil {
				t.Fatal(err)
			}
			state := engine.LoadMustSucceed(t).State
			if plan.Next.Kind != decision.KindAsk || !slices.Contains(plan.Next.Ask.Requests, "begin-change") || !state.requirementChangePending() || state.PendingAsks["begin-change"].Package != nil || len(state.RequirementRevisions) != 1 {
				t.Fatalf("initial barrier = %+v, ask=%+v", plan.Next, state.PendingAsks)
			}
			acceptance := submitReviewResult(t, engine, "during-clarification", action, reviewEvidenceAt(review.ProductReview, action, "rev-1", "PASS", "review completed during clarification", nil))
			if acceptance.Status != "QUARANTINED" {
				t.Fatalf("result escaped barrier: %+v", acceptance)
			}
			state = engine.LoadMustSucceed(t).State
			if len(state.QuarantinedResults) != 1 || len(state.Results) != 0 || state.ReviewSeries.ProductReview.Effective.Status != "PENDING" {
				t.Fatal("barrier result advanced the review")
			}
			engine, err = New(mustStore(t, dir), engine.cfg)
			if err != nil {
				t.Fatal(err)
			}
			payload := RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "delta:clarified", ImpactDigest: "impact:clarified", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "confirm the prepared content and carry the reviewed work"}
			token, err := engine.Freshness("begin-change")
			if err != nil {
				t.Fatal(err)
			}
			if choice == "proceed" {
				unbound, err := NewRequirementDecideEvent("unbound-proceed", "begin-change", token, "proceed", payload)
				if err != nil {
					t.Fatal(err)
				}
				before := stateBytesOf(t, dir)
				if _, err = engine.Submit(unbound, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
					t.Fatal("unbound package was executed or changed state")
				}
				update, err := NewRequirementPackageEvent("prepare-package", "begin-change", token, payload.packageView())
				if err != nil {
					t.Fatal(err)
				}
				accepted, err := engine.Submit(update, Observation{})
				if err != nil {
					t.Fatal(err)
				}
				bound := engine.LoadMustSucceed(t)
				if len(bound.State.PendingAsks) != 1 || bound.State.PendingAsks["begin-change"].Resolved || !sameRequirementPackage(*bound.State.PendingAsks["begin-change"].Package, payload.packageView()) || bound.State.RequirementRevision() != "rev-1" {
					t.Fatal("package update replaced the barrier or advanced the revision")
				}
				oldToken, err := NewRequirementDecideEvent("stale-confirm", "begin-change", token, "proceed", payload)
				if err != nil {
					t.Fatal(err)
				}
				before = stateBytesOf(t, dir)
				if _, err = engine.Submit(oldToken, Observation{}); err == nil || rejectionCode(t, err) != CodeStaleFreshness || !bytes.Equal(before, stateBytesOf(t, dir)) {
					t.Fatalf("old confirmation token = %v", err)
				}
				engine, err = New(mustStore(t, dir), engine.cfg)
				if err != nil {
					t.Fatal(err)
				}
				replay, err := engine.Submit(update, Observation{})
				if err != nil || !reflect.DeepEqual(replay, accepted) || !bytes.Equal(before, stateBytesOf(t, dir)) {
					t.Fatalf("package replay = %+v, %v", replay, err)
				}
				token, err = engine.Freshness("begin-change")
				if err != nil {
					t.Fatal(err)
				}
			}
			event, err := NewDecideEvent("confirm-change", "begin-change", token, AskOptionID(choice))
			if err != nil {
				t.Fatal(err)
			}
			if choice == "proceed" {
				event, err = NewRequirementDecideEvent("confirm-change", "begin-change", token, "proceed", payload)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = engine.Submit(event, Observation{}); err != nil {
				t.Fatal(err)
			}
			state = engine.LoadMustSucceed(t).State
			wantRevision := "rev-1"
			wantEntries := 1
			if choice == "proceed" {
				wantRevision = "rev-2"
				wantEntries = 2
			}
			if state.requirementChangePending() || !state.PendingAsks["begin-change"].Resolved || len(state.QuarantinedResults) != 0 || state.ReviewSeries.ProductReview.Completed != 1 || state.Results[action].Review.RequirementRevision != "rev-1" || state.RequirementRevision() != wantRevision || len(state.RequirementRevisions) != wantEntries {
				t.Fatal("resolved barrier did not readmit original evidence through the normal path")
			}
		})
	}
}

func TestReviewGrantAskLifecycle(t *testing.T) {
	for _, choice := range []string{grantOption, declineOption} {
		t.Run(choice, func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			engine, dir := reviewEngine(t, cd, reg)
			exhaustProductReviewCapacity(t, engine)
			redoProductReview(t, engine, "additional-review")
			plan, _, err := engine.Drive(Observation{})
			if err != nil {
				t.Fatal(err)
			}
			state := engine.LoadMustSucceed(t).State
			if plan.Next.Kind != decision.KindAsk || len(plan.Next.Ask.Requests) != 1 || len(state.PendingActions) != 0 {
				t.Fatalf("exhausted boundary = %+v", plan.Next)
			}
			requestID := plan.Next.Ask.Requests[0]
			ask := state.PendingAsks[requestID]
			if ask.Control != ControlReviewRoundGrant || ask.RoundGrant == nil || *ask.RoundGrant != state.reviewGrantBinding(review.ProductReview) || ask.RoundGrant.Completed != 3 {
				t.Fatalf("unbound grant offer: %+v", ask)
			}
			wantRequestDigest := digestOfCanonical(ask)
			if _, external := state.Events[requestID]; external || state.requestDigest(requestID) != wantRequestDigest {
				t.Fatal("generated grant request lost its complete offer evidence")
			}
			before := stateBytesOf(t, dir)
			engine, err = New(mustStore(t, dir), engine.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = engine.Drive(Observation{}); err != nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
				t.Fatal("re-entry changed the same grant Ask")
			}
			token, err := engine.Freshness(RequestID(requestID))
			if err != nil {
				t.Fatal(err)
			}
			wrong, err := NewReviewDecideEvent("wrong-series", RequestID(requestID), token, grantOption, ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{Action: review.StartReadiness, Reason: "other series"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = engine.Submit(wrong, Observation{}); err == nil || !bytes.Equal(before, stateBytesOf(t, dir)) {
				t.Fatal("grant changed the action bound to its Ask")
			}
			event, err := NewDecideEvent("decide-grant", RequestID(requestID), token, AskOptionID(choice))
			if err != nil {
				t.Fatal(err)
			}
			if choice == grantOption {
				event, err = NewReviewDecideEvent("decide-grant", RequestID(requestID), token, grantOption, ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{Action: review.ProductReview, Reason: "review the newly confirmed requirement"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			accepted, err := engine.Submit(event, Observation{})
			if err != nil {
				t.Fatal(err)
			}
			before = stateBytesOf(t, dir)
			replay, err := engine.Submit(event, Observation{})
			if err != nil || !reflect.DeepEqual(replay, accepted) || !bytes.Equal(before, stateBytesOf(t, dir)) {
				t.Fatal("grant decision replay changed durable state")
			}
			state = engine.LoadMustSucceed(t).State
			if !state.PendingAsks[requestID].Resolved || state.Decisions[requestID].Choice != AskOptionID(choice) || state.requestDigest(requestID) != wantRequestDigest {
				t.Fatal("decision did not settle its exact request")
			}
			if choice == grantOption {
				if len(state.PendingActions) != 1 || len(state.ReviewSeries.ProductReview.Authorizations) != 1 {
					t.Fatalf("grant did not issue exactly one owed review: %+v", state.PendingActions)
				}
			} else {
				plan, _, err = engine.Drive(Observation{})
				if err != nil {
					t.Fatal(err)
				}
				if plan.Next.Kind != decision.KindWait || plan.Next.Wait.Reason != decision.WaitCapacity || len(state.PendingActions) != 0 || len(state.ReviewSeries.ProductReview.Authorizations) != 0 || !bytes.Equal(before, stateBytesOf(t, dir)) {
					t.Fatalf("decline did not preserve capacity wait: %+v", plan.Next)
				}
			}
		})
	}
}

func TestReconfirmedContentOwnsFreshReviewGeneration(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, dir := reviewEngine(t, cd, reg)
	first := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "initial-review", first, reviewEvidenceAt(review.ProductReview, first, "rev-1", "PASS", "reviewed original content", nil))
	redoProductReview(t, engine, "different-content")
	second := pendingActionID(t, engine)
	revision := engine.LoadMustSucceed(t).State.RequirementRevision()
	submitReviewResult(t, engine, "second-review", second, reviewEvidenceAt(review.ProductReview, second, revision, "PASS", "reviewed changed content", nil))
	payload := &RequirementDecisionPayload{BaseRevision: revision, CandidateRevision: "rev-1", DeltaDigest: "delta:restore", ImpactDigest: "impact:restore", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "restore content and request an independent fresh review"}
	askAndDecideRequirement(t, engine, "restore-original", "proceed", payload)
	var err error
	engine, err = New(mustStore(t, dir), engine.cfg)
	if err != nil {
		t.Fatal(err)
	}
	state := engine.LoadMustSucceed(t).State
	if !state.ReviewSeries.ProductReview.FreshOwed || state.ReviewSeries.ProductReview.FreshFloor != state.requirementGeneration() || len(state.ReviewSeries.ProductReview.RawCandidates) != 2 {
		t.Fatal("historical content discharged new execution obligation")
	}
	fresh := pendingActionID(t, engine)
	if fresh == first || fresh == second || state.issuedRevisionOf(fresh) < state.ReviewSeries.ProductReview.FreshFloor {
		t.Fatal("restored content reused historical dispatch identity")
	}
	submitReviewResult(t, engine, "restored-review", fresh, reviewEvidenceAt(review.ProductReview, fresh, "rev-1", "PASS", "fresh review of restored content", nil))
	state = engine.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.FreshOwed || state.ReviewSeries.ProductReview.Completed != 3 || len(state.RequirementRevisions) != 3 {
		t.Fatalf("fresh completion did not retain all confirmations: %+v", state.ReviewSeries.ProductReview)
	}
}

func TestRequirementAskExpiresAcrossReconfirmedContent(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, dir := reviewEngine(t, cd, reg)
	pending := &RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "other-content", DeltaDigest: "delta:pending", ImpactDigest: "impact:pending", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "an earlier proposal"}
	request, err := NewRequirementRequestEvent("earlier-ask", pending.packageView(), requirementOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	for i, revision := range []string{"rev-2", "rev-1"} {
		payload := &RequirementDecisionPayload{BaseRevision: engine.LoadMustSucceed(t).State.RequirementRevision(), CandidateRevision: revision, DeltaDigest: "delta:" + revision, ImpactDigest: "impact:" + revision, Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "confirm requested content"}
		askAndDecideRequirement(t, engine, fmt.Sprintf("confirm-%d", i), "proceed", payload)
	}
	token, err := engine.Freshness("earlier-ask")
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	if _, err = engine.Submit(requirementDecideEvent(t, "earlier-ask", token, "proceed", pending), Observation{}); err == nil || rejectionCode(t, err) != CodeRequirementAskExpired || !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatalf("reused content refreshed a stale Ask: %v", err)
	}
}
