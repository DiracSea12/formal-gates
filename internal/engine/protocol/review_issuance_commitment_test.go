// review_issuance_commitment_test.go 覆盖审查系列签发闸的序无关放行承诺：
// Completed 在签发与入账之间漂移（整轮作废压回限内）时任意兄弟入账序下
// "放行 ⇒ 可背书 ⇒ 可入账"保持成立；容量自洽的签出不绑槽不消费授权；换
// 新派发继承被换新旧派发的原槽；同一派发绑两条授权在加载边界可区分拒绝。
package protocol

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/review"
)

// driftPairFixture 构造漂移狩猎的最小 DAG：三轮串行后一对并行（fan.pair.a/
// fan.pair.b 挂 round.three）——持槽越限派发与补位兄弟可同时在途竞争容量。
func driftPairFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	return productReviewDAGFixture(t,
		dagStep{name: "round.one", deps: []string{"prep.parse"}},
		dagStep{name: "round.two", deps: []string{"round.one"}},
		dagStep{name: "round.three", deps: []string{"round.two"}},
		dagStep{name: "fan.pair.a", deps: []string{"round.three"}},
		dagStep{name: "fan.pair.b", deps: []string{"round.three"}},
	)
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

// driftToCapacityDip 走满三轮自动容量、授权一轮放行并行对的一支（越限形，
// 独占绑 auth[0]），再整轮作废第三轮把 Completed 压回 2：补位兄弟被闸、
// 槽上恰有唯一在途承载者。返回持槽在途派发与其授权 ID。
func driftToCapacityDip(t *testing.T, engine *Engine, tag string) (string, string) {
	t.Helper()
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, tag+"-dip-r"+string(rune('0'+round)), action, reviewEvidence(review.ProductReview, action, "", "PASS", "round "+string(rune('0'+round)), nil))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("the parallel pair must stay gated at exhausted capacity: %+v", pending)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-"+tag+"-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "one more lane after the automatic rounds",
	}})
	overLimit := pendingActionID(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	authID := series.Authorizations[0].ID
	if !slices.Contains(series.Authorizations[0].DispatchIDs, overLimit) || liveCarriersOn(t, engine, 0) != 1 {
		t.Fatalf("the admitted over-limit dispatch must hold the granted slot exclusively: %+v", series.Authorizations[0])
	}
	invalidateRoundOf(t, engine, "req-"+tag+"-invalidate", action)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 2 || series.RemainingCapacity != 2 {
		t.Fatalf("after the invalidation completed=%d capacity=%d, want 2/2", series.Completed, series.RemainingCapacity)
	}
	pending := engine.LoadMustSucceed(t).State.PendingActions
	if _, live := pending[overLimit]; len(pending) != 1 || !live {
		t.Fatalf("the sibling lane must stay gated while the exclusive slot carries the live dispatch: %+v", pending)
	}
	if liveCarriersOn(t, engine, 0) != 1 {
		t.Fatalf("the granted slot must carry exactly the one live dispatch: %+v", series.Authorizations[0])
	}
	assertSeriesCommitmentHeld(t, engine)
	return overLimit, authID
}

// TestDriftKeepsEveryAdmittedDispatchRecordableInEitherOrder：Completed 漂移
// 回限内后，两种入账序都不得悬空——持槽越限派发 D 全程可背书入账，补位兄
// 弟 E 在漂移窗口被闸（不污染槽）；D 先消费槽时 E 经再授权放行，D 按自动
// 容量入账（槽保持干净）时 E 直接继承槽放行。终态两轮都入账、无悬空派发。
func TestDriftKeepsEveryAdmittedDispatchRecordableInEitherOrder(t *testing.T) {
	t.Run("slot consumed first", func(t *testing.T) {
		cd, reg := driftPairFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		overLimit, auth1 := driftToCapacityDip(t, engine, "consume")

		// D 携自己的槽入账（入账时 Completed=2 在限内，槽背书合法）：
		// completed 3、auth[0] 被消费。
		submitReviewResult(t, engine, "result-drift-consume-over", overLimit, reviewEvidence(review.ProductReview, overLimit, auth1, "PASS", "over-limit dispatch records on its exclusive slot", nil))
		series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 3 || series.ExtraConsumed != 1 {
			t.Fatalf("after the dip round records: %+v", series)
		}
		// 槽已消费：兄弟继续被闸；再授权后放行并独占绑定新槽。
		if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
			t.Fatalf("the sibling must stay gated while no clean slot exists: %+v", pending)
		}
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-consume-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "a fresh slot for the sibling lane",
		}})
		sibling := pendingActionID(t, engine)
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		auth2 := series.Authorizations[1].ID
		if !slices.Contains(series.Authorizations[1].DispatchIDs, sibling) || liveCarriersOn(t, engine, 1) != 1 {
			t.Fatalf("the sibling must hold the fresh slot exclusively: %+v", series.Authorizations)
		}
		submitReviewResult(t, engine, "result-drift-consume-sibling", sibling, reviewEvidence(review.ProductReview, sibling, auth2, "PASS", "sibling records on its own fresh slot", nil))
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 4 || series.ExtraConsumed != 2 {
			t.Fatalf("final series = %+v", series)
		}
		if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
			t.Fatalf("no dispatch may stay stranded: %+v", pending)
		}
		assertSeriesCommitmentHeld(t, engine)
	})

	t.Run("slot freed by an automatic record", func(t *testing.T) {
		cd, reg := driftPairFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		overLimit, auth1 := driftToCapacityDip(t, engine, "free")

		// D 按自动容量携空授权入账（入账时 Completed=2<3）：completed 3、
		// auth[0] 不被消费、保持干净。
		submitReviewResult(t, engine, "result-drift-free-over", overLimit, reviewEvidence(review.ProductReview, overLimit, "", "PASS", "over-limit dispatch records under the dipped automatic capacity", nil))
		series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 3 || series.ExtraConsumed != 0 || len(series.Authorizations[0].RecordedResultDigests) != 0 {
			t.Fatalf("the dipped round must record without consuming the slot: %+v", series)
		}
		// D 落定且槽净：兄弟经背书形放行并独占绑定 auth[0]，携槽入账。
		sibling := pendingActionID(t, engine)
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if !slices.Contains(series.Authorizations[0].DispatchIDs, sibling) || liveCarriersOn(t, engine, 0) != 1 {
			t.Fatalf("the sibling must hold the freed slot exclusively: %+v", series.Authorizations[0])
		}
		submitReviewResult(t, engine, "result-drift-free-sibling", sibling, reviewEvidence(review.ProductReview, sibling, auth1, "PASS", "sibling records on the slot freed by the automatic round", nil))
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 4 || series.ExtraConsumed != 1 {
			t.Fatalf("final series = %+v", series)
		}
		if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
			t.Fatalf("no dispatch may stay stranded: %+v", pending)
		}
		assertSeriesCommitmentHeld(t, engine)
	})
}

// TestSelfConsistentIssuanceSparesAuthorizationSlots：容量自洽形（Completed+
// 在途(含本条) ≤ 自动上限）放行不绑任何授权槽——自动轮不消费用户的授权；
// 干净槽在场时两条并行自洽派发全部按自动容量落地，未被消费的槽留给真正
// 需要背书的轮。
func TestSelfConsistentIssuanceSparesAuthorizationSlots(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "round.one", deps: []string{"prep.parse"}},
		dagStep{name: "round.two", deps: []string{"round.one"}},
		dagStep{name: "round.three", deps: []string{"round.two"}},
		dagStep{name: "round.four", deps: []string{"round.three"}},
		dagStep{name: "fan.a", deps: []string{"round.four"}},
		dagStep{name: "fan.b", deps: []string{"round.four"}},
		dagStep{name: "round.after", deps: []string{"fan.a", "fan.b"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	// 三轮自动＋授权一轮入账（completed 4、auth[0] 被第四轮占用）。
	dispatches := map[string]string{} // 步名 → 该轮派发
	action := issueReady(t, engine)[0]
	for _, step := range []string{"round.one", "round.two", "round.three"} {
		dispatches[step] = action
		submitReviewResult(t, engine, "result-spare-"+step, action, reviewEvidence(review.ProductReview, action, "", "PASS", "round "+step, nil))
		if step != "round.three" {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-spare-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fourth round needs endorsement",
	}})
	fourth := pendingActionID(t, engine)
	dispatches["round.four"] = fourth
	authID := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
	submitReviewResult(t, engine, "result-spare-round.four", fourth, reviewEvidence(review.ProductReview, fourth, authID, "PASS", "round four", nil))

	// 作废 round.three 与 round.two（completed 2：fan.a 容量自洽放行、不绑
	// 槽），再作废 round.four 的轮（completed 1、auth[0] 释放为干净槽）：
	// fan.b 仍容量自洽放行——干净槽在场也不被自动轮消费。
	invalidateRoundOf(t, engine, "req-spare-inv-3", dispatches["round.three"])
	invalidateRoundOf(t, engine, "req-spare-inv-2", dispatches["round.two"])
	invalidateRoundOf(t, engine, "req-spare-inv-4", dispatches["round.four"])

	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.ExtraConsumed != 0 {
		t.Fatalf("after the invalidations completed=%d extraConsumed=%d, want 1/0", series.Completed, series.ExtraConsumed)
	}
	fanA, fanB := pendingActionByStep(t, engine, "fan.a"), pendingActionByStep(t, engine, "fan.b")
	if got := series.Authorizations[0].DispatchIDs; slices.Contains(got, fanA) || slices.Contains(got, fanB) {
		t.Fatalf("self-consistent issuance must not bind the free slot: %+v", got)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 两条自洽派发都按自动容量落地：授权槽保持未消费（第四轮的记录已被
	// 整轮作废排除，槽空出）。
	submitReviewResult(t, engine, "result-spare-fan-a", fanA, reviewEvidence(review.ProductReview, fanA, "", "PASS", "fan a automatic round", nil))
	submitReviewResult(t, engine, "result-spare-fan-b", fanB, reviewEvidence(review.ProductReview, fanB, "", "PASS", "fan b automatic round", nil))
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 3 || series.ExtraConsumed != 0 {
		t.Fatalf("automatic rounds must not consume the authorization slot: %+v", series)
	}
	// 未被消费的槽由需要背书的后续轮消费：round.after 经背书形放行并绑定。
	after := pendingActionID(t, engine)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if !slices.Contains(series.Authorizations[0].DispatchIDs, after) || liveCarriersOn(t, engine, 0) != 1 {
		t.Fatalf("the spared slot must back the endorsement round: %+v", series.Authorizations[0])
	}
	submitReviewResult(t, engine, "result-spare-after", after, reviewEvidence(review.ProductReview, after, authID, "PASS", "round on the spared slot", nil))
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestReplacementInheritsItsBoundSlot：换新派发继承承载被换新旧派发的原槽
// ——多槽且存在其他干净槽时也不改绑；旧派发未绑定（容量自洽签出）时换新
// 保持未绑定。
func TestReplacementInheritsItsBoundSlot(t *testing.T) {
	t.Run("rebinds the original slot beside another free slot", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "round.one", "round.two", "round.three", "round.four", "round.five")
		engine, _ := reviewEngine(t, cd, reg)

		action := issueReady(t, engine)[0]
		for round := 1; round <= 3; round++ {
			submitReviewResult(t, engine, "result-inherit-r"+string(rune('0'+round)), action, reviewEvidence(review.ProductReview, action, "", "PASS", "round "+string(rune('0'+round)), nil))
			if round < 3 {
				action = pendingActionID(t, engine)
			}
		}
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-inherit-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "the fourth round needs endorsement",
		}})
		fourth := pendingActionID(t, engine)
		auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
		submitReviewResult(t, engine, "result-inherit-r4", fourth, reviewEvidence(review.ProductReview, fourth, auth1, "PASS", "round four", nil))
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-inherit-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "the fifth round needs endorsement",
		}})
		fifth := pendingActionID(t, engine)
		auth2 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[1].ID
		// 作废第四轮：auth[0] 释放为首条干净槽——换新 round.five 不得改绑
		// 它，必须继承承载旧派发的 auth[1]。
		invalidateRoundOf(t, engine, "req-inherit-inv-4", fourth)
		plan, _, err := engine.RecoverAttempt(fifth, Interruption{CauseKnown: true}, fingerprintOf(t, engine))
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
		submitReviewResult(t, engine, "result-inherit-r5", replacement, reviewEvidence(review.ProductReview, replacement, auth2, "PASS", "replacement records on the inherited slot", nil))
		series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
		if series.Completed != 4 || series.ExtraConsumed != 1 {
			t.Fatalf("final series = %+v", series)
		}
		assertSeriesCommitmentHeld(t, engine)
	})

	t.Run("unbound dispatch stays unbound", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "round.one", "round.two")
		engine, _ := reviewEngine(t, cd, reg)
		first := issueReady(t, engine)[0]
		plan, _, err := engine.RecoverAttempt(first, Interruption{CauseKnown: true}, fingerprintOf(t, engine))
		if err != nil || plan.Action != RecoveryNewAttempt {
			t.Fatalf("recovery = %+v err=%v", plan, err)
		}
		snap := engine.LoadMustSucceed(t)
		replacement := snap.State.ObsoleteActions[first].ReplacedBy
		if review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, replacement) != "" {
			t.Fatalf("the replacement of an unbound dispatch must stay unbound: %+v", snap.State.ReviewSeries.ProductReview.Authorizations)
		}
		submitReviewResult(t, engine, "result-inherit-unbound", replacement, reviewEvidence(review.ProductReview, replacement, "", "PASS", "replacement records under automatic capacity", nil))
		if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || series.ExtraConsumed != 0 {
			t.Fatalf("the unbound replacement must record as an automatic round: %+v", series)
		}
	})
}

// TestLoadRejectsDispatchBoundToTwoAuthorizationSlots：同一派发同时绑两条
// 授权即可在入账时择槽消费，属结构破坏——加载边界的完整性校验必须可区分
// 拒绝（手改 state 后 ContentDigest 同步重算以穿过持久层）。
func TestLoadRejectsDispatchBoundToTwoAuthorizationSlots(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "round.one", "round.two", "round.three", "round.four", "round.five")
	engine, dir := reviewEngine(t, cd, reg)

	// 三轮自动＋两次授权入账（completed 5），第五轮派发在途且独占绑 auth[1]。
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, "result-dual-r"+string(rune('0'+round)), action, reviewEvidence(review.ProductReview, action, "", "PASS", "round "+string(rune('0'+round)), nil))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-dual-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fourth round needs endorsement",
	}})
	fourth := pendingActionID(t, engine)
	auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
	submitReviewResult(t, engine, "result-dual-r4", fourth, reviewEvidence(review.ProductReview, fourth, auth1, "PASS", "round four", nil))
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
