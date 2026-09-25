// review_issuance_commitment_test.go 覆盖审查系列签发闸的序无关放行承诺：
// Completed 在签发与入账之间漂移（整轮作废压回限内）时任意兄弟入账序下
// "放行 ⇒ 可背书 ⇒ 可入账"保持成立；容量自洽的签出不绑槽不消费授权；换
// 新派发继承被换新旧派发的原槽；同一派发绑两条授权在加载边界可区分拒绝；
// 全批被闸的签发是零状态变化、不经提交（前沿首次登记除外）。
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

// driftToCapacityDip 走满三轮自动容量（连续语义 FAIL 轮，系列不结论）、授
// 权一轮放行第四轮（越限形，独占绑 auth[0]），再整轮作废第三轮把 Completed
// 压回 2：补位的下一轮被闸、槽上恰有唯一在途承载者。返回持槽在途派发与
// 其授权 ID。
func driftToCapacityDip(t *testing.T, engine *Engine, tag string) (string, string) {
	t.Helper()
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, tag+"-dip-r"+string(rune('0'+round)), action,
			blockingReviewEvidence(review.ProductReview, action, "round "+string(rune('0'+round))))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("the fourth round must stay gated at exhausted capacity: %+v", pending)
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
		cd, reg := singleCarrierFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		overLimit, auth1 := driftToCapacityDip(t, engine, "consume")

		// D 携自己的槽入账（入账时 Completed=2 在限内，槽背书合法；记 FAIL
		// 轮保持系列未结论，兄弟轮仍可走）：completed 3、auth[0] 被消费。
		submitReviewResult(t, engine, "result-drift-consume-over", overLimit,
			reviewEvidence(review.ProductReview, overLimit, auth1, "FAIL", "over-limit dispatch records on its exclusive slot", []review.Finding{
				{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
			}))
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
		submitReviewResult(t, engine, "result-drift-consume-sibling", sibling,
			reviewEvidence(review.ProductReview, sibling, auth2, "FAIL", "sibling records on its own fresh slot", []review.Finding{
				{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
			}))
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
		cd, reg := singleCarrierFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		overLimit, auth1 := driftToCapacityDip(t, engine, "free")

		// D 按自动容量携空授权入账（入账时 Completed=2<3；记 FAIL 轮保持系
		// 列未结论）：completed 3、auth[0] 不被消费、保持干净。
		submitReviewResult(t, engine, "result-drift-free-over", overLimit,
			blockingReviewEvidence(review.ProductReview, overLimit, "over-limit dispatch records under the dipped automatic capacity"))
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
		submitReviewResult(t, engine, "result-drift-free-sibling", sibling,
			reviewEvidence(review.ProductReview, sibling, auth1, "FAIL", "sibling records on the slot freed by the automatic round", []review.Finding{
				{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
			}))
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
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)

	// 三轮语义 FAIL（completed 3、系列不结论）后授权一轮：第四轮经背书形
	// 放行并独占绑 auth[0]（在途）。
	action := issueReady(t, engine)[0]
	rounds := []string{}
	for round := 1; round <= 3; round++ {
		rounds = append(rounds, action)
		submitReviewResult(t, engine, "result-spare-r"+string(rune('0'+round)), action,
			blockingReviewEvidence(review.ProductReview, action, "round "+string(rune('0'+round))))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-spare-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fourth round needs endorsement",
	}})
	endorsed := pendingActionID(t, engine)
	if got := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, endorsed); got == "" {
		t.Fatal("the fourth round must bind the granted slot")
	}
	assertSeriesCommitmentHeld(t, engine)

	// 整轮作废前三轮（completed 0）：逐次作废的事务在容量内各自提案一轮
	//（容量自洽形，与在途背书轮并发）——不绑被在途背书轮占用的槽，也不
	// 消费它。
	for i, dispatchID := range rounds {
		invalidateRoundOf(t, engine, "req-spare-inv-"+string(rune('0'+i+1)), dispatchID)
	}
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 0 || series.ExtraConsumed != 0 {
		t.Fatalf("after the invalidations completed=%d extraConsumed=%d, want 0/0", series.Completed, series.ExtraConsumed)
	}
	fifth := ""
	for id := range engine.LoadMustSucceed(t).State.PendingActions {
		if id != endorsed {
			fifth = id
		}
	}
	if fifth == "" {
		t.Fatal("the dip must refill a self-consistent round beside the endorsed one")
	}
	if got := review.AuthorizationOfDispatch(series, fifth); got != "" {
		t.Fatalf("self-consistent issuance must not bind the occupied slot: %q", got)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 第五轮按自动容量落地（completed 1）：槽不被消费、保持干净。
	submitReviewResult(t, engine, "result-spare-r5", fifth, blockingReviewEvidence(review.ProductReview, fifth, "round five under automatic capacity"))
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.ExtraConsumed != 0 || len(series.Authorizations[0].RecordedResultDigests) != 0 {
		t.Fatalf("the automatic round must not consume the authorization slot: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestReplacementInheritsItsBoundSlot：换新派发继承承载被换新旧派发的原槽
// ——多槽且存在其他干净槽时也不改绑；旧派发未绑定（容量自洽签出）时换新
// 保持未绑定。
func TestReplacementInheritsItsBoundSlot(t *testing.T) {
	t.Run("rebinds the original slot beside another free slot", func(t *testing.T) {
		cd, reg := singleCarrierFixture(t)
		engine, _ := reviewEngine(t, cd, reg)

		action := issueReady(t, engine)[0]
		for round := 1; round <= 3; round++ {
			submitReviewResult(t, engine, "result-inherit-r"+string(rune('0'+round)), action,
				blockingReviewEvidence(review.ProductReview, action, "round "+string(rune('0'+round))))
			if round < 3 {
				action = pendingActionID(t, engine)
			}
		}
		askAndDecide(t, engine, ControlReviewRoundGrant, "req-inherit-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "the fourth round needs endorsement",
		}})
		fourth := pendingActionID(t, engine)
		auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
		submitReviewResult(t, engine, "result-inherit-r4", fourth,
			reviewEvidence(review.ProductReview, fourth, auth1, "FAIL", "round four", []review.Finding{
				{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
			}))
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
		submitReviewResult(t, engine, "result-inherit-r5", replacement,
			reviewEvidence(review.ProductReview, replacement, auth2, "FAIL", "replacement records on the inherited slot", []review.Finding{
				{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
			}))
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
		plan, _, err := engine.RecoverAttempt(first, Interruption{CauseKnown: true}, fingerprintOf(t, engine))
		if err != nil || plan.Action != RecoveryNewAttempt {
			t.Fatalf("recovery = %+v err=%v", plan, err)
		}
		snap := engine.LoadMustSucceed(t)
		replacement := snap.State.ObsoleteActions[first].ReplacedBy
		if review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, replacement) != "" {
			t.Fatalf("the replacement of an unbound dispatch must stay unbound: %+v", snap.State.ReviewSeries.ProductReview.Authorizations)
		}
		submitReviewResult(t, engine, "result-inherit-unbound", replacement, blockingReviewEvidence(review.ProductReview, replacement, "replacement records under automatic capacity"))
		if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || series.ExtraConsumed != 0 {
			t.Fatalf("the unbound replacement must record as an automatic round: %+v", series)
		}
	})
}

// TestFullyGatedIssueSkipsTheEmptyCommit：全批被闸的显式签发是零状态变
// 化——签出集为空且 Expected 无新增条目时不经提交（revision 与落盘字节
// 均不动）；对照：同形签发在 retainExpected 首次登记前沿（Expected 有增
// 量）时属状态变化，照常提交。闸源取 start-readiness 的顺序门：产品审
// 未结论前其轮签发被拒。
func TestFullyGatedIssueSkipsTheEmptyCommit(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)

	base := engine.LoadMustSucceed(t)
	expectedBefore := len(base.State.Expected)
	baseBytes := stateBytesOf(t, dir)

	// 首次签发：全批被顺序门闸住、签出集为空，但 retainExpected 首次登记
	// 前沿任务——状态变化，照常提交。
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issued, revision, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fingerprintOf(t, engine))
	if err != nil {
		t.Fatalf("frontier-registering issue: %v", err)
	}
	if len(issued) != 0 {
		t.Fatalf("the sequenced-off series must gate the whole batch: %v", issued)
	}
	if revision != base.Revision+1 {
		t.Fatalf("the frontier registration is a state change and must commit: revision %d, want %d", revision, base.Revision+1)
	}
	if now := engine.LoadMustSucceed(t); len(now.State.Expected) != expectedBefore+1 {
		t.Fatalf("the gated frontier task must be retained as Expected: %d entries, want %d", len(now.State.Expected), expectedBefore+1)
	}
	if bytes.Equal(stateBytesOf(t, dir), baseBytes) {
		t.Fatal("the frontier-registering commit must change the persisted state bytes")
	}

	// 再次同形签发：全批被闸且 Expected 已登记——零状态变化，不经提交。
	plan, _, err = engine.Plan()
	if err != nil {
		t.Fatalf("plan again: %v", err)
	}
	gatedBytes := stateBytesOf(t, dir)
	issued, revision, err = engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fingerprintOf(t, engine))
	if err != nil {
		t.Fatalf("fully gated issue: %v", err)
	}
	if len(issued) != 0 {
		t.Fatalf("the sequenced-off series must keep the whole batch gated: %v", issued)
	}
	if revision != base.Revision+1 {
		t.Fatalf("a fully gated issue with no new Expected entries must not commit: revision %d, want %d", revision, base.Revision+1)
	}
	if !bytes.Equal(stateBytesOf(t, dir), gatedBytes) {
		t.Fatal("a fully gated issue with no new Expected entries must leave state bytes untouched")
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestLoadRejectsDispatchBoundToTwoAuthorizationSlots：同一派发同时绑两条
// 授权即可在入账时择槽消费，属结构破坏——加载边界的完整性校验必须可区分
// 拒绝（手改 state 后 ContentDigest 同步重算以穿过持久层）。
func TestLoadRejectsDispatchBoundToTwoAuthorizationSlots(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, dir := reviewEngine(t, cd, reg)

	// 三轮语义 FAIL（completed 3）＋背书第四轮入账（completed 4、auth[0] 被
	// 消费），第五轮背书派发在途且独占绑 auth[1]。
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, "result-dual-r"+string(rune('0'+round)), action,
			blockingReviewEvidence(review.ProductReview, action, "round "+string(rune('0'+round))))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-dual-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the fourth round needs endorsement",
	}})
	fourth := pendingActionID(t, engine)
	auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
	submitReviewResult(t, engine, "result-dual-r4", fourth,
		reviewEvidence(review.ProductReview, fourth, auth1, "FAIL", "round four", []review.Finding{
			{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
		}))
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

// TestInvalidationRecoveryRunsEndorsedDispatchesOnExclusiveSlots：两次整轮
// 失效把两条授权槽同时腾空、其一已承载在途派发时——重开载体经背书形再
// 签出的派发必须绑到不承载在途派发的独占空槽：失效回收后一在途一新增
// 两条 endorsed 派发并发在途、各持一槽，闭包不变量（一条受闸派发与在途
// 受闸兄弟共享槽的形状不存在）保持。新鲜轮落在最早重开的载体上，由签发
// 闸决定放行与绑槽。
func TestInvalidationRecoveryRunsEndorsedDispatchesOnExclusiveSlots(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)

	// 三轮自动容量（语义 FAIL）＋两轮背书：第四轮绑 auth[0]、第五轮绑
	// auth[1]，各自携槽入账（completed 5、系列不结论）。
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, "result-exclusive-pair-"+string(rune('0'+round)), action,
			blockingReviewEvidence(review.ProductReview, action, "round "+string(rune('0'+round))))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-exclusive-pair-g1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "slot one",
	}})
	first := pendingActionID(t, engine)
	auth0 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].ID
	submitReviewResult(t, engine, "result-exclusive-pair-r4", first,
		reviewEvidence(review.ProductReview, first, auth0, "FAIL", "round four", []review.Finding{
			{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
		}))
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-exclusive-pair-g2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "slot two",
	}})
	second := pendingActionID(t, engine)
	auth1 := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[1].ID
	submitReviewResult(t, engine, "result-exclusive-pair-r5", second,
		reviewEvidence(review.ProductReview, second, auth1, "FAIL", "round five", []review.Finding{
			{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
		}))

	// 失效第四轮（auth[0] 释放）：欠轮提案经背书形再签出并绑回 auth[0]
	// （在途、未入账）。
	invalidateRoundOf(t, engine, "req-exclusive-pair-inv-4", first)
	inFlight := pendingActionID(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if got := review.AuthorizationOfDispatch(series, inFlight); got != auth0 {
		t.Fatalf("the recovery dispatch must hold the released slot exclusively: %q", got)
	}

	// 失效第五轮（effective 轮）：系列回 PENDING、两槽皆空（auth[0] 只承
	// 载在途派发）——新的欠轮提案必须绑 auth[1]，与在途者各持一槽并发。
	invalidateRoundOf(t, engine, "req-exclusive-pair-inv-5", second)
	snap := engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Completed != 3 || series.RemainingCapacity != 2 {
		t.Fatalf("after both invalidations = %+v", series)
	}
	var fresh string
	for id := range snap.State.PendingActions {
		if id == inFlight {
			continue
		}
		fresh = id
	}
	if fresh == "" {
		t.Fatalf("an endorsed dispatch must re-issue beside the in-flight one: %+v", snap.State.PendingActions)
	}
	if got := review.AuthorizationOfDispatch(series, fresh); got != auth1 {
		t.Fatalf("the new endorsed dispatch must hold the slot free of the in-flight carrier: %q (auth0=%v auth1=%v)",
			got, series.Authorizations[0].DispatchIDs, series.Authorizations[1].DispatchIDs)
	}
	livePerSlot := map[string]int{}
	for id := range snap.State.PendingActions {
		if slot := review.AuthorizationOfDispatch(series, id); slot != "" {
			livePerSlot[slot]++
		}
	}
	for slot, count := range livePerSlot {
		if count != 1 {
			t.Fatalf("slot %s carries %d in-flight dispatches: %+v", slot, count, snap.State.PendingActions)
		}
	}
	assertSeriesCommitmentHeld(t, engine)
}
