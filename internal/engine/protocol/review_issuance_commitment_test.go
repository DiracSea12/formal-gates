// review_issuance_commitment_test.go covers authorization ownership when prior
// evidence is invalidated, an attempt is retried, or a new requirement revision
// explicitly owes another review. Capacity never creates concurrent reviews.
package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
)

// singleCarrierFixture 构造单载体产品审系列：prep 后一个带标记 agent 步，
// 连续语义轮都作为该载体步的轮任务（scope＝轮号）经账本欠轮提案签出。
func singleCarrierFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	return productReviewDAGFixture(t, dagStep{name: "review.round", deps: []string{"prep.parse"}})
}

// liveCarriersOn 统计产品审授权槽上承载的仍在途派发数。
func liveCarriersOn(t *testing.T, engine *Engine, authorizationIndex int) int {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	count := 0
	for _, dispatchID := range series.Authorizations[authorizationIndex].DispatchIDs {
		if _, live := snap.State.PendingActions[dispatchID]; live {
			count++
		}
	}
	return count
}

// driftToCapacityDip fills automatic capacity through explicit requirement
// revisions, authorizes the next owed review, then invalidates prior evidence.
// The sole in-flight dispatch retains its original authorization as capacity dips.
func driftToCapacityDip(t *testing.T, engine *Engine, tag string) (string, string) {
	t.Helper()
	exhaustProductReviewCapacity(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	previous := series.RawCandidates[len(series.RawCandidates)-1].DispatchID
	redoProductReview(t, engine, tag+"-redo")
	askAndDecide(t, engine, ControlReviewRoundGrant, tag+"-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "review the explicitly changed requirement",
	}})
	action := pendingActionID(t, engine)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	authorization := review.AuthorizationOfDispatch(series, action)
	if authorization == "" || liveCarriersOn(t, engine, 0) != 1 {
		t.Fatalf("the owed review must exclusively hold its authorization: %+v", series.Authorizations)
	}
	invalidateRoundOf(t, engine, tag+"-invalidate", previous)
	snap := engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Completed != 2 || series.RemainingCapacity != 2 {
		t.Fatalf("invalidation must release one unit of automatic capacity: %+v", series)
	}
	if _, live := snap.State.PendingActions[action]; len(snap.State.PendingActions) != 1 || !live {
		t.Fatalf("capacity drift must not create another obligation: %+v", snap.State.PendingActions)
	}
	assertSeriesCommitmentHeld(t, engine)
	return action, authorization
}

func TestCapacityDipPreservesDispatchAuthorization(t *testing.T) {
	for _, useAuthorization := range []bool{false, true} {
		name := "automatic"
		if useAuthorization {
			name = "authorized"
		}
		t.Run(name, func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			engine, _ := reviewEngine(t, cd, reg)
			action, authorization := driftToCapacityDip(t, engine, name)
			revision := engine.LoadMustSucceed(t).State.RequirementRevision()
			evidence := reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "review after capacity changed", nil)
			consumed := 0
			if useAuthorization {
				evidence.AuthorizationID = authorization
				consumed = 1
			}
			submitVerifiedReviewResult(t, engine, "capacity-dip-result", action, evidence)
			series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			if series.Completed != 3 || series.ExtraConsumed != consumed {
				t.Fatalf("the accepted evidence must determine authorization consumption: %+v", series)
			}
			assertReviewIssuanceHeld(t, engine)
			redoProductReview(t, engine, "capacity-dip-next")
			if useAuthorization {
				assertReviewIssuanceHeld(t, engine)
				askAndDecide(t, engine, ControlReviewRoundGrant, "capacity-dip-next-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
					Action: review.ProductReview, Reason: "review the next changed requirement",
				}})
			}
			next := pendingActionID(t, engine)
			series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			nextAuthorization := review.AuthorizationOfDispatch(series, next)
			wantAuthorization := authorization
			if useAuthorization {
				wantAuthorization = series.Authorizations[1].ID
			}
			if nextAuthorization != wantAuthorization {
				t.Fatalf("the next obligation must use authorization %q, got %q", wantAuthorization, nextAuthorization)
			}
			revision = engine.LoadMustSucceed(t).State.RequirementRevision()
			evidence = reviewEvidenceAt(review.ProductReview, next, revision, "PASS", "next requested review", nil)
			evidence.AuthorizationID = nextAuthorization
			submitVerifiedReviewResult(t, engine, "capacity-next-result", next, evidence)
			series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			if series.Completed != 4 || series.ExtraConsumed != consumed+1 {
				t.Fatalf("both obligations must finish on their own evidence: %+v", series)
			}
			assertSeriesCommitmentHeld(t, engine)
		})
	}
}

func TestAutomaticReviewAfterCapacityDipSparesAuthorization(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	action, _ := driftToCapacityDip(t, engine, "spared")
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	for index, candidate := range series.RawCandidates {
		if !review.ResultExcluded(series, candidate.ResultDigest) {
			invalidateRoundOf(t, engine, "spared-invalidate-"+string(rune('a'+index)), candidate.DispatchID)
		}
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Completed != 0 || len(snap.State.PendingActions) != 1 {
		t.Fatalf("invalidation must free capacity without preissuing work: %+v", snap.State.ReviewSeries.ProductReview)
	}
	revision := snap.State.RequirementRevision()
	submitVerifiedReviewResult(t, engine, "spared-result", action, reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "record under available automatic capacity", nil))
	redoProductReview(t, engine, "spared-next")
	next := pendingActionID(t, engine)
	snap = engine.LoadMustSucceed(t)
	if authorization := review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, next); authorization != "" {
		t.Fatalf("automatic issuance must not bind the unused extra authorization: %q", authorization)
	}
	submitVerifiedReviewResult(t, engine, "spared-next-result", next, reviewEvidenceAt(review.ProductReview, next, snap.State.RequirementRevision(), "PASS", "next automatic review", nil))
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 2 || series.ExtraConsumed != 0 || len(series.Authorizations[0].RecordedResultDigests) != 0 {
		t.Fatalf("automatic reviews must leave the authorization unconsumed: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// 故障重试继承原派发的授权，不抢占其它空闲授权；自动容量内的重试保持
// 未绑定。需求变更产生新任务，不走此故障重试路径。
func TestReplacementInheritsItsBoundSlot(t *testing.T) {
	t.Run("rebinds the original slot beside another free slot", func(t *testing.T) {
		cd, reg := singleCarrierFixture(t)
		engine, _ := reviewEngine(t, cd, reg)

		exhaustProductReviewCapacity(t, engine)
		redoProductReview(t, engine, "inherit-fourth")
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-inherit-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "the fourth round needs endorsement",
		}})
		fourth := pendingActionID(t, engine)
		auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
		evidence := reviewEvidenceAt(review.ProductReview, fourth, engine.LoadMustSucceed(t).State.RequirementRevision(), "PASS", "authorized review", nil)
		evidence.AuthorizationID = auth1
		submitVerifiedReviewResult(t, engine, "result-inherit-r4", fourth, evidence)
		redoProductReview(t, engine, "inherit-fifth")
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-inherit-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "the fifth round needs endorsement",
		}})
		fifth := pendingActionID(t, engine)
		auth2 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[1].ID
		// 作废第四轮：auth[0] 释放为首条干净槽——换新 round.five 不得改绑
		// 它，必须继承承载旧派发的 auth[1]。
		invalidateRoundOf(t, engine, "req-inherit-inv-4", fourth)
		observedStoppedWorker(t, engine, fifth)
		plan, _, err := engine.RecoverAttempt(fifth, Interruption{CauseKnown: true}, Observation{})
		if err != nil || plan.Action != RecoveryNewAttempt {
			t.Fatalf("recovery = %+v err=%v", plan, err)
		}
		snap := engine.LoadMustSucceed(t)
		replacement := snap.State.ObsoleteActions[fifth].ReplacedBy
		series := snap.State.ReviewSeries.ProductReview
		if !slices.Contains(series.Authorizations[1].DispatchIDs, replacement) {
			t.Fatalf("the replacement must inherit the slot of the replaced dispatch: %+v", series.Authorizations)
		}
		if slices.Contains(series.Authorizations[0].DispatchIDs, replacement) {
			t.Fatalf("the replacement must not claim the other free slot: %+v", series.Authorizations[0])
		}
		if got := liveCarriersOn(t, engine, 1); got != 1 {
			t.Fatalf("the inherited slot must carry exactly the replacement: %d", got)
		}
		// 换新携原槽入账：completed 4、槽消费。
		evidence = reviewEvidenceAt(review.ProductReview, replacement, snap.State.RequirementRevision(), "PASS", "replacement on its inherited slot", nil)
		evidence.AuthorizationID = auth2
		submitVerifiedReviewResult(t, engine, "result-inherit-r5", replacement, evidence)
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 4 || series.ExtraConsumed != 1 {
			t.Fatalf("final series = %+v", series)
		}
		assertSeriesCommitmentHeld(t, engine)
	})

	t.Run("unbound dispatch stays unbound", func(t *testing.T) {
		cd, reg := singleCarrierFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		first := issueReady(t, engine)[0]
		observedStoppedWorker(t, engine, first)
		plan, _, err := engine.RecoverAttempt(first, Interruption{CauseKnown: true}, Observation{})
		if err != nil || plan.Action != RecoveryNewAttempt {
			t.Fatalf("recovery = %+v err=%v", plan, err)
		}
		snap := engine.LoadMustSucceed(t)
		replacement := snap.State.ObsoleteActions[first].ReplacedBy
		if review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, replacement) != "" {
			t.Fatalf("the replacement of an unbound dispatch must stay unbound: %+v", snap.State.ReviewSeries.ProductReview.Authorizations)
		}
		submitVerifiedReviewResult(t, engine, "result-inherit-unbound", replacement, blockingReviewEvidence(review.ProductReview, replacement, "replacement records under automatic capacity"))
		if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || series.ExtraConsumed != 0 {
			t.Fatalf("the unbound replacement must record as an automatic round: %+v", series)
		}
	})
}

// A review waiting on its upstream conclusion is not a Ready task.
// IssueFromPlan accepts only Ready plans and rejects this input without writes.
func TestBlockedReviewPlanRejectsIssuanceWithoutWriting(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	base := engine.LoadMustSucceed(t)
	baseBytes := stateBytesOf(t, dir)
	for attempt := 0; attempt < 2; attempt++ {
		plan, _, err := engine.Plan(Observation{})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Next.Kind != decision.KindWait {
			t.Fatalf("blocked review plan = %s, want WAIT", plan.Next.Kind)
		}
		issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, Observation{})
		if err == nil || len(issued) != 0 {
			t.Fatalf("non-Ready issuance = %+v, %v", issued, err)
		}
		now := engine.LoadMustSucceed(t)
		if now.Revision != base.Revision || len(now.State.PendingActions) != 0 || len(now.State.Expected) != len(base.State.Expected) || !bytes.Equal(stateBytesOf(t, dir), baseBytes) {
			t.Fatal("blocked issuance changed durable state")
		}
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestLoadRejectsDispatchBoundToTwoAuthorizationSlots：同一派发同时绑两条
// 授权即可在入账时择槽消费，属结构破坏——加载边界的完整性校验必须可区分
// 拒绝（手改 state 后 ContentDigest 同步重算以穿过持久层）。
func TestLoadRejectsDispatchBoundToTwoAuthorizationSlots(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, dir := reviewEngine(t, cd, reg)

	// 逐次确认需求 revision，完成三轮自动审查和一轮授权审查；下一条
	// 明确要求的新鲜审查在途，独占绑定第二条授权。
	exhaustProductReviewCapacity(t, engine)
	redoProductReview(t, engine, "dual-fourth")
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-dual-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fourth round needs endorsement",
	}})
	fourth := pendingActionID(t, engine)
	auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
	evidence := reviewEvidenceAt(review.ProductReview, fourth, engine.LoadMustSucceed(t).State.RequirementRevision(), "PASS", "authorized review", nil)
	evidence.AuthorizationID = auth1
	submitVerifiedReviewResult(t, engine, "result-dual-r4", fourth, evidence)
	redoProductReview(t, engine, "dual-fifth")
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-dual-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fifth round needs endorsement",
	}})
	fifth := pendingActionID(t, engine)
	if _, live := engine.LoadMustSucceed(t).State.PendingActions[fifth]; !live {
		t.Fatal("the fifth round dispatch must be in flight")
	}

	// 篡改：把在途的第五轮派发追加上 auth[0]（同派发绑两槽）。占用投影不
	// 受 DispatchIDs 影响，完整性面只剩"一派发多槽"可检。
	original := stateBytesOf(t, dir)
	tamperStateContent(t, dir, func(content map[string]any) {
		series := content["reviewSeries"].(map[string]any)["productReview"].(map[string]any)
		authorizations := series["authorizations"].([]any)
		first := authorizations[0].(map[string]any)
		first["dispatchIds"] = append(first["dispatchIds"].([]any), fifth)
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("a dispatch bound to two slots loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("dual-slot binding code = %q", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}
}

// Freed authorizations can be reused by later obligations; invalidating another
// result while one review is live cannot preissue a second review.
func TestInvalidationRecoveryReusesAuthorizationWithoutConcurrentReviews(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	exhaustProductReviewCapacity(t, engine)
	rounds := []string{}
	for _, name := range []string{"first", "second"} {
		redoProductReview(t, engine, "exclusive-"+name)
		askAndDecide(t, engine, ControlReviewRoundGrant, "exclusive-grant-"+name, grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "review the explicitly changed requirement",
		}})
		action := pendingActionID(t, engine)
		snap := engine.LoadMustSucceed(t)
		evidence := reviewEvidenceAt(review.ProductReview, action, snap.State.RequirementRevision(), "PASS", "authorized review", nil)
		evidence.AuthorizationID = review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, action)
		submitVerifiedReviewResult(t, engine, "exclusive-result-"+name, action, evidence)
		rounds = append(rounds, action)
	}
	invalidateRoundOf(t, engine, "exclusive-invalidate-earlier", rounds[0])
	assertReviewIssuanceHeld(t, engine)
	redoProductReview(t, engine, "exclusive-reuse")
	inFlight := pendingActionID(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	firstAuthorization, secondAuthorization := series.Authorizations[0].ID, series.Authorizations[1].ID
	if slot := review.AuthorizationOfDispatch(series, inFlight); slot != firstAuthorization {
		t.Fatalf("new obligation must reuse the released authorization: %q", slot)
	}
	invalidateRoundOf(t, engine, "exclusive-invalidate-later", rounds[1])
	snap := engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if _, live := snap.State.PendingActions[inFlight]; len(snap.State.PendingActions) != 1 || !live || series.Completed != 3 || series.RemainingCapacity != 2 {
		t.Fatalf("released slots must not duplicate the live obligation: pending=%+v series=%+v", snap.State.PendingActions, series)
	}
	assertSeriesCommitmentHeld(t, engine)
	evidence := reviewEvidenceAt(review.ProductReview, inFlight, snap.State.RequirementRevision(), "PASS", "review on the released slot", nil)
	evidence.AuthorizationID = firstAuthorization
	submitVerifiedReviewResult(t, engine, "exclusive-reused-result", inFlight, evidence)
	redoProductReview(t, engine, "exclusive-reuse-next")
	next := pendingActionID(t, engine)
	snap = engine.LoadMustSucceed(t)
	if slot := review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, next); slot != secondAuthorization {
		t.Fatalf("the following obligation must use the other unconsumed authorization: %q", slot)
	}
	evidence = reviewEvidenceAt(review.ProductReview, next, snap.State.RequirementRevision(), "PASS", "review on the other released slot", nil)
	evidence.AuthorizationID = secondAuthorization
	submitVerifiedReviewResult(t, engine, "exclusive-next-result", next, evidence)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 5 || series.ExtraConsumed != 2 {
		t.Fatalf("both reused authorizations must remain backed by distinct results: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}
