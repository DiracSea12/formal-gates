// review_dispatch_commitment_test.go 覆盖 terminal 化派发的承诺保持与落
// 定：任务经 TASK_PROGRESS 到 terminal 只收回簿记，签发闸的在途承诺不随
// 之解除——迟到结果（直接对账与隔离重放）照常入账，且承诺账在任意兄弟
// 入账序下全程自洽；永不回 PASS 的派发由宿主的失败结果经同一对账路径落
// 定释放。状态级承诺 oracle（assertSeriesCommitmentHeld）同时在既有签发
// 闸测试的关键事务后断言。
package protocol

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// ---- 状态级承诺 oracle（自建断言，不复用实现的判定谓词） ----

// unsettledDispatchIDs 枚举当前未落定的派发：PendingActions 成员，或回执
// SPAWNED/ATTACHED 且 Results 与 ObsoleteActions 均无该派发的 terminal 派
// 发——承诺账必须覆盖的完整在途集合。
func unsettledDispatchIDs(st *State) []string {
	ids := make([]string, 0, len(st.PendingActions)+len(st.SpawnReceipts))
	for id := range st.PendingActions {
		ids = append(ids, id)
	}
	for id, receipt := range st.SpawnReceipts {
		if _, pending := st.PendingActions[id]; pending {
			continue
		}
		if receipt.Status != SpawnStatusSpawned && receipt.Status != SpawnStatusAttached {
			continue
		}
		if _, recorded := st.Results[id]; recorded {
			continue
		}
		if _, retired := st.ObsoleteActions[id]; retired {
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// seriesOfDispatch 归属一条未落定派发的审查系列：签发标记为权威；簿记已
// 收回的 terminal 派发先按授权槽绑定归属，再按回执固化任务步骤的系列标
// 记推导。
func seriesOfDispatch(st *State, engine *Engine, dispatchID string) review.Action {
	if pending, ok := st.PendingActions[dispatchID]; ok && pending.ReviewSeries != "" {
		return pending.ReviewSeries
	}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, _ := st.ReviewSeries.Series(action)
		if review.AuthorizationOfDispatch(series, dispatchID) != "" {
			return action
		}
	}
	return engine.reviewSeriesOf(st.SpawnReceipts[dispatchID].Task.Step)
}

// seriesCommitmentViolations 扫描双 series 对"放行 ⇒ 可背书 ⇒ 可入账（任
// 意兄弟序）"承诺的违背：
//  1. 存在未持槽在途派发但 Completed+在途(未持槽+持槽) > AutomaticLimit
//     ——某个排序下未持槽派发入账时 Completed 已达上限且无背书；
//  2. 持槽在途派发的槽已被有效结果占用——其携槽入账必被
//     "already endorses a valid result"拒绝；
//  3. 同一授权承载多于一条在途派发，或同一派发绑两条授权——独占性破坏。
func seriesCommitmentViolations(t *testing.T, engine *Engine) []string {
	t.Helper()
	st := engine.LoadMustSucceed(t).State
	live := unsettledDispatchIDs(st)
	var violations []string
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, _ := st.ReviewSeries.Series(action)
		slotOf := map[string]string{}
		for _, authorization := range series.Authorizations {
			for _, dispatchID := range authorization.DispatchIDs {
				if other, dup := slotOf[dispatchID]; dup {
					violations = append(violations, fmt.Sprintf(
						"dispatch %s is bound to both authorizations %s and %s", dispatchID, other, authorization.ID))
				}
				slotOf[dispatchID] = authorization.ID
			}
		}
		unslotted, slotted := 0, 0
		liveOnSlot := map[string][]string{}
		for _, dispatchID := range live {
			if seriesOfDispatch(st, engine, dispatchID) != action {
				continue
			}
			authID := review.AuthorizationOfDispatch(series, dispatchID)
			if authID == "" {
				unslotted++
				continue
			}
			slotted++
			liveOnSlot[authID] = append(liveOnSlot[authID], dispatchID)
		}
		if unslotted > 0 && series.Completed+unslotted+slotted > series.AutomaticLimit {
			violations = append(violations, fmt.Sprintf(
				"unslotted in-flight dispatches exist but completed=%d + unslotted=%d + slotted=%d exceeds the automatic limit %d: some sibling order leaves an unslotted dispatch unrecordable",
				series.Completed, unslotted, slotted, series.AutomaticLimit))
		}
		occupied := review.OccupiedAuthorizationIDs(series)
		for authID, carriers := range liveOnSlot {
			if len(carriers) > 1 {
				violations = append(violations, fmt.Sprintf(
					"authorization %s carries %d in-flight dispatches %v", authID, len(carriers), carriers))
			}
			if occupied[authID] {
				violations = append(violations, fmt.Sprintf(
					"in-flight dispatches %v hold authorization %s that already endorses a valid result", carriers, authID))
			}
		}
	}
	return violations
}

// assertSeriesCommitmentHeld 断言当前状态满足签发闸的承诺不变量。
func assertSeriesCommitmentHeld(t *testing.T, engine *Engine) {
	t.Helper()
	if violations := seriesCommitmentViolations(t, engine); len(violations) != 0 {
		t.Fatalf("the series commitment invariant is torn:\n%s", strings.Join(violations, "\n"))
	}
}

// ---- terminal 化与迟到结果的驱动助手 ----

// terminalizeDispatchTask 经公开 TASK_PROGRESS 把派发任务驱动到 TERMINAL
// （ISSUED→RUNNING→VALIDATING→TERMINAL）：模拟宿主观察到的派发终止——
// 派发簿记随终态收回、回执与签发代数保留，迟到结果的 terminal 对账以此
// 为前提。
func terminalizeDispatchTask(t *testing.T, engine *Engine, actionID string) {
	t.Helper()
	pending, known := engine.LoadMustSucceed(t).State.PendingActions[actionID]
	if !known {
		t.Fatalf("cannot terminalize %s: not in flight", actionID)
	}
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID(fmt.Sprintf("progress-%s-%d", actionID, i)), pending.Task, pending.AttemptID, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("task progress %s of %s: %v", status, actionID, err)
		}
	}
	if _, still := engine.LoadMustSucceed(t).State.PendingActions[actionID]; still {
		t.Fatalf("terminal task still has dispatch bookkeeping: %s", actionID)
	}
}

// submitSpawnReceipt 落一条 SPAWNED 回执（不提交结果）。
func submitSpawnReceipt(t *testing.T, engine *Engine, event, actionID string) {
	t.Helper()
	if _, err := engine.Submit(spawnReceipt(t, EventID(event), actionID, "agent-"+event), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("spawn receipt %s: %v", event, err)
	}
}

// submitLateDispatchResult 为已 terminal 化的派发提交迟到 worker result
// （回执必须已在账），返回提交错误供拒绝断言。
func submitLateDispatchResult(t *testing.T, engine *Engine, event, actionID string, evidence ReviewResultEvidence) error {
	t.Helper()
	evidence.DispatchID = actionID
	ev, err := NewReviewWorkerResultEvent(EventID(event), actionID, testProvider, "sha256:payload-"+event, evidence)
	if err != nil {
		t.Fatalf("late result event: %v", err)
	}
	_, err = engine.Submit(ev, fingerprintOf(t, engine))
	return err
}

// ---- 三个违诺形状的修复期态与对照 ----

// TestTerminalizedDispatchKeepsCapacityPromiseUntilItsResultRecords：自洽
// 形承诺在 terminal 化后保持——四并行道前三条容量自洽放行、第四条被闸；
// 一条落回执后被宿主置 terminal，腾空的在途计数不得放行第四条（迟到结果
// 仍可对账入账）；迟到 PASS 入账后承诺集合如实收缩，兄弟任意序入账全部
// 闭环，第四条经授权背书放行——不存在悬空派发。
func TestTerminalizedDispatchKeepsCapacityPromiseUntilItsResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	// Completed=0：前三条容量自洽放行（0+2+1=3 达界）、不绑槽，第四条被闸。
	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	for _, lane := range []string{laneA, laneB, laneC} {
		if review.AuthorizationOfDispatch(series, lane) != "" {
			t.Fatalf("capacity-consistent issuance must not bind a slot: %s", lane)
		}
	}
	assertSeriesCommitmentHeld(t, engine)

	// lane.a 落 SPAWNED 回执后被宿主置 terminal：簿记收回、承诺保持——
	// 在途计数不因 terminal 化腾空，lane.d 不得被放行。
	submitSpawnReceipt(t, engine, "spawn-terminal-lane-a", laneA)
	terminalizeDispatchTask(t, engine, laneA)
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the terminalized lane holds its promise: %v", reissued)
	}

	// 迟到 PASS 对账入账（Completed=1）：此刻在途 {b,c}，1+2+1=4>3，
	// lane.d 仍被闸。
	if err := submitLateDispatchResult(t, engine, "late-terminal-lane-a", laneA,
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "late result of the terminalized lane", nil)); err != nil {
		t.Fatalf("the late result of the terminalized dispatch must reconcile: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
		t.Fatalf("the late reconciliation must record a round: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated until the siblings settle: %v", reissued)
	}

	// 兄弟序走完：b、c 相继入账（任意序都在容量内），Completed=3。
	submitReviewResult(t, engine, "result-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 3 {
		t.Fatalf("completed = %d, want 3", series.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("the exhausted series must keep lane.d gated: %v", reissued)
	}

	// 授权后 lane.d 经背书形放行并独占绑槽，携槽入账闭环——无悬空派发。
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-terminal-lane-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the gated fourth lane",
	}})
	laneD := pendingActionByStep(t, engine, "lane.d")
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	authID := review.AuthorizationOfDispatch(series, laneD)
	if authID == "" {
		t.Fatal("the gated lane must hold the granted slot")
	}
	submitReviewResult(t, engine, "result-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, authID, "PASS", "the gated lane records on its slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestTerminalizedDispatchKeepsItsAuthorizationSlotUntilItsResultRecords：
// 背书形承诺在 terminal 化后保持——三轮耗尽后授权放行的持槽派发被宿主
// 置 terminal，其槽在迟到结果落账前不回"干净"，先前被闸的兄弟不得绑
// 上；迟到携原授权入账消费该槽后，兄弟经再授权的新槽放行——不存在
// "already endorses"悬空。
func TestTerminalizedDispatchKeepsItsAuthorizationSlotUntilItsResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "round.one", deps: []string{"prep.parse"}},
		dagStep{name: "round.two", deps: []string{"round.one"}},
		dagStep{name: "round.three", deps: []string{"round.two"}},
		dagStep{name: "pair.a", deps: []string{"round.three"}},
		dagStep{name: "pair.b", deps: []string{"round.three"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	// 三轮自动容量走满（Completed=3），并行对被闸；授权一轮：pair.a 背书
	// 形独占绑 auth[0]，pair.b 被闸。
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, fmt.Sprintf("result-slot-r%d", round), action, reviewEvidence(review.ProductReview, action, "", "PASS", fmt.Sprintf("round %d", round), nil))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-terminal-slot-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "one endorsed lane",
	}})
	pairA := pendingActionByStep(t, engine, "pair.a")
	authA := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, pairA)
	if authA == "" {
		t.Fatal("the endorsed dispatch must hold the granted slot")
	}
	assertSeriesCommitmentHeld(t, engine)

	// pair.a 落回执后置 terminal：承诺保持——auth[0] 仍承载未落定派发，
	// pair.b 不得绑上"看似干净"的原槽。
	submitSpawnReceipt(t, engine, "spawn-terminal-pair-a", pairA)
	terminalizeDispatchTask(t, engine, pairA)
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("pair.b must stay gated while the terminalized pair.a holds the slot: %v", reissued)
	}

	// 迟到携原授权 PASS 对账入账：越限轮由槽背书，槽被有效结果消费。
	if err := submitLateDispatchResult(t, engine, "late-terminal-pair-a", pairA,
		reviewEvidence(review.ProductReview, pairA, authA, "PASS", "late endorsed result of the terminalized lane", nil)); err != nil {
		t.Fatalf("the late endorsed result must reconcile: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("the late endorsed record must consume the slot: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)

	// pair.b 仍闸（槽已背书有效结果）；再授权放行并独占绑新槽，携槽入账。
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("pair.b must stay gated while no clean slot exists: %v", reissued)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-terminal-slot-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "a fresh slot for the gated sibling",
	}})
	pairB := pendingActionByStep(t, engine, "pair.b")
	authB := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, pairB)
	if authB == "" || authB == authA {
		t.Fatalf("the gated sibling must hold a distinct fresh slot: %q vs %q", authB, authA)
	}
	submitReviewResult(t, engine, "result-pair-b", pairB, reviewEvidence(review.ProductReview, pairB, authB, "PASS", "the gated sibling records on the fresh slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 5 || series.ExtraConsumed != 2 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestQuarantinedLateResultReplayKeepsCapacityPromise：同一承诺在隔离重
// 放路径全程保持——terminal 化发生在修订屏障之前，迟到结果在屏障期被持
// 久隔离，解除事务的重放经同一对账路径入账；屏障期与重放后腾空的计数都
// 不放行被闸的第四条，入账后兄弟任意序闭环。
func TestQuarantinedLateResultReplayKeepsCapacityPromise(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")
	submitSpawnReceipt(t, engine, "spawn-quarantine-lane-a", laneA)
	terminalizeDispatchTask(t, engine, laneA)
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the terminalized lane holds its promise: %v", reissued)
	}

	// 屏障期：迟到结果被隔离（QUARANTINED），签发面冻结。
	request, err := NewRequirementRequestEvent(EventID("req-quarantine-promise"), requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	late, err := NewReviewWorkerResultEvent(EventID("quarantine-late-lane-a"), laneA, testProvider,
		"sha256:payload-quarantine-late-lane-a",
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "quarantined late result of the terminalized lane", nil))
	if err != nil {
		t.Fatalf("late event: %v", err)
	}
	if acceptance, err := engine.Submit(late, fingerprintOf(t, engine)); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("barrier quarantine = %+v err=%v", acceptance, err)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 解除（放弃变更）：隔离账重放，terminal 对账入账（Completed=1）；
	// 此刻在途 {b,c}，1+2+1=4>3，lane.d 仍被闸。
	abortExistingAsk(t, engine, "req-quarantine-promise")
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
		t.Fatalf("the quarantined late result must reconcile on lift: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated after the replay records the late round: %v", reissued)
	}

	// 兄弟序走完（Completed=3），lane.d 经授权背书放行闭环。
	submitReviewResult(t, engine, "result-quarantine-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-quarantine-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-quarantine-lane-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the gated fourth lane after the replay",
	}})
	laneD := pendingActionByStep(t, engine, "lane.d")
	authID := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, laneD)
	if authID == "" {
		t.Fatal("the gated lane must hold the granted slot after the replay")
	}
	submitReviewResult(t, engine, "result-quarantine-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, authID, "PASS", "the gated lane records on its slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestUncontestedLateTerminalResultRecords：对照——系列无在途承诺承载
// 时，terminal 迟到对账入账只是多记一轮：兄弟的容量承诺在签发时点计数了
// 该派发，迟到入账是那一计数的兑现，全程无害。
func TestUncontestedLateTerminalResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 2 {
		t.Fatalf("batch admitted %d lanes, want both: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	submitSpawnReceipt(t, engine, "spawn-uncontested-lane-a", laneA)
	terminalizeDispatchTask(t, engine, laneA)
	assertSeriesCommitmentHeld(t, engine)
	if err := submitLateDispatchResult(t, engine, "late-uncontested-lane-a", laneA,
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "late but uncontended", nil)); err != nil {
		t.Fatalf("uncontended late result: %v", err)
	}
	submitReviewResult(t, engine, "result-uncontested-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 2 {
		t.Fatalf("completed = %d, want 2", series.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// ---- 挂接回执（ATTACHED）形状的承诺与落定 ----

// submitUnknownReceipt 落一条 UNKNOWN 回执（宿主不知派发结果是否产生的
// 观察事实），留待对账。
func submitUnknownReceipt(t *testing.T, engine *Engine, event, actionID, correlation string) {
	t.Helper()
	if _, err := engine.Submit(unknownSpawnReceipt(t, EventID(event), actionID, correlation), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("unknown receipt %s: %v", event, err)
	}
}

// attachUnknownReceipt 补齐唯一 lifecycle 配对并把 UNKNOWN 回执升为
// ATTACHED（对账把宿主的中断观察挂接到 lifecycle 身份；挂接不要求派发
// 仍在途）。
func attachUnknownReceipt(t *testing.T, engine *Engine, actionID, correlation, identity string) {
	t.Helper()
	for i, name := range []string{LifecycleStart, LifecycleStop} {
		ev, err := NewCorrelatedLifecycleEvent(EventID(fmt.Sprintf("cycle-%s-%d", actionID, i)), testProvider, correlation, identity, name)
		if err != nil {
			t.Fatalf("lifecycle event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("lifecycle %s: %v", name, err)
		}
	}
	plan, _, err := engine.ReconcileUnknownReceipt(actionID, fingerprintOf(t, engine))
	if err != nil || plan.Action != RecoveryAttachReceipt {
		t.Fatalf("reconcile %s = %+v err=%v", actionID, plan, err)
	}
	if receipt := engine.LoadMustSucceed(t).State.SpawnReceipts[actionID]; receipt.Status != SpawnStatusAttached {
		t.Fatalf("receipt %s = %q, want ATTACHED", actionID, receipt.Status)
	}
}

// submitFailureResult 为派发提交一条非 PASS 的宿主失败结果（不携审查证
// 据），返回接纳回执与提交错误——永不回 PASS 派发的落定出口。
func submitFailureResult(t *testing.T, engine *Engine, event, actionID string) (Acceptance, error) {
	t.Helper()
	ev, err := NewWorkerResultEvent(EventID(event), actionID, testProvider, OutcomeFail, "sha256:payload-"+event, authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("failure event: %v", err)
	}
	acceptance, err := engine.Submit(ev, fingerprintOf(t, engine))
	return acceptance, err
}

// TestAttachedTerminalDispatchKeepsPromiseUntilItsResultRecords：自洽形
// 承诺在"回执 UNKNOWN→任务 terminal 化→对账挂接 ATTACHED"后保持——挂接
// 是观察事实，terminal 化只收回簿记；持 ATTACHED 回执的 terminal 派发与
// SPAWNED 同判计入在途，被闸的第四条不放行；迟到 PASS 经同一 terminal 对
// 账入账后承诺落定，兄弟任意序走完，第四条经授权背书放行闭环。
func TestAttachedTerminalDispatchKeepsPromiseUntilItsResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")

	// 回执 UNKNOWN → 任务被宿主置 terminal → 对账把回执挂接为 ATTACHED。
	submitUnknownReceipt(t, engine, "spawn-unknown-lane-a", laneA, "corr-unknown-lane-a")
	terminalizeDispatchTask(t, engine, laneA)
	attachUnknownReceipt(t, engine, laneA, "corr-unknown-lane-a", "worker-lane-a")
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the attached terminal dispatch holds its promise: %v", reissued)
	}

	// 迟到 PASS 经 terminal 对账入账（Completed=1）：承诺随 Results 落定。
	if err := submitLateDispatchResult(t, engine, "late-attached-lane-a", laneA,
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "late result of the attached terminal dispatch", nil)); err != nil {
		t.Fatalf("the late result of the attached terminal dispatch must reconcile: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || len(series.RawCandidates) != 1 {
		t.Fatalf("the late reconciliation must record a round: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated until the siblings settle: %v", reissued)
	}

	// 兄弟序走完（Completed=3），lane.d 仍闸；授权背书放行并携槽入账闭环。
	submitReviewResult(t, engine, "result-attached-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-attached-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("the exhausted series must keep lane.d gated: %v", reissued)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-attached-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the gated fourth lane",
	}})
	laneD := pendingActionByStep(t, engine, "lane.d")
	authID := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, laneD)
	if authID == "" {
		t.Fatal("the gated lane must hold the granted slot")
	}
	submitReviewResult(t, engine, "result-attached-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, authID, "PASS", "the gated lane records on its slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestAttachedTerminalDispatchFailureResultSettlesTheHeldPromise：挂接为
// ATTACHED 的 terminal 派发永不回 PASS 时（宿主丢失结果），由宿主提交失
// 败结果经同一 terminal 对账落账——Results 有记录、不建候选、不占轮、步
// 骤不完成——承诺随之落定释放，同事务的容量补位放行第四条，兄弟任意序
// 入账闭环。
func TestAttachedTerminalDispatchFailureResultSettlesTheHeldPromise(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")

	submitUnknownReceipt(t, engine, "spawn-unknown-settle-lane-a", laneA, "corr-unknown-settle")
	terminalizeDispatchTask(t, engine, laneA)
	attachUnknownReceipt(t, engine, laneA, "corr-unknown-settle", "worker-settle")
	assertSeriesCommitmentHeld(t, engine)

	// 失败结果经 terminal 对账落账：只进 Results 与对账留痕，不建候选。
	if _, err := submitFailureResult(t, engine, "fail-attached-lane-a", laneA); err != nil {
		t.Fatalf("the failure result of the attached terminal dispatch must settle it: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if _, recorded := snap.State.Results[laneA]; !recorded {
		t.Fatal("the failure result must be recorded in Results")
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 0 || len(series.RawCandidates) != 0 {
		t.Fatalf("a non-PASS result must not create a review round: %+v", series)
	}
	if containsStep(snap.State.Completed, "lane.a") {
		t.Fatalf("a non-PASS result must not complete the step: %+v", snap.State.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 承诺已落定：同一事务的容量补位放行 lane.d（0+2+1=3 达界），兄弟任
	// 意序入账全部闭环。
	laneD := pendingActionByStep(t, engine, "lane.d")
	if got := review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, laneD); got != "" {
		t.Fatalf("the refilled lane must stay unslotted: %q", got)
	}
	submitReviewResult(t, engine, "result-settle-attached-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-settle-attached-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	submitReviewResult(t, engine, "result-settle-attached-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, "", "PASS", "the refilled lane records under the released promise", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 3 {
		t.Fatalf("completed = %d, want 3", series.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestAttachedEndorsedCarrierHoldsItsSlotUntilItsResultRecords：背书形独
// 占绑槽的派发走到 ATTACHED+terminal 形状时，槽在迟到结果落账前保持承载
// ——不回干净、不被被闸兄弟抢占，再授权因存在未消费授权被拒；迟到携原
// 授权结果经 terminal 对账入账消费该槽后，兄弟经再授权的新槽放行闭环
// ——授权容量不泄漏。
func TestAttachedEndorsedCarrierHoldsItsSlotUntilItsResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "round.one", deps: []string{"prep.parse"}},
		dagStep{name: "round.two", deps: []string{"round.one"}},
		dagStep{name: "round.three", deps: []string{"round.two"}},
		dagStep{name: "pair.a", deps: []string{"round.three"}},
		dagStep{name: "pair.b", deps: []string{"round.three"}},
	)
	engine, dir := reviewEngine(t, cd, reg)

	// 三轮自动容量走满（Completed=3），授权一轮：pair.a 背书形独占绑
	// auth[0]，pair.b 被闸。
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, fmt.Sprintf("result-attached-slot-r%d", round), action,
			reviewEvidence(review.ProductReview, action, "", "PASS", fmt.Sprintf("round %d", round), nil))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-attached-slot-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the endorsed carrier",
	}})
	pairA := pendingActionByStep(t, engine, "pair.a")
	authA := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, pairA)
	if authA == "" {
		t.Fatal("the endorsed dispatch must hold the granted slot")
	}
	assertSeriesCommitmentHeld(t, engine)

	// pair.a 走到 ATTACHED+terminal：槽仍承载未落定派发——pair.b 被闸，
	// 再授权被"存在未消费授权"拒绝（每次只授权恰好一轮）。
	submitUnknownReceipt(t, engine, "spawn-unknown-pair-a", pairA, "corr-unknown-pair-a")
	terminalizeDispatchTask(t, engine, pairA)
	attachUnknownReceipt(t, engine, pairA, "corr-unknown-pair-a", "worker-pair-a")
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("pair.b must stay gated while the attached carrier holds the slot: %v", reissued)
	}
	if err := askAndDecideRejecting(t, engine, dir, ControlReviewRoundGrant, "req-attached-slot-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "a fresh slot for the gated sibling",
	}}); err == nil {
		t.Fatal("a second grant must not be creatable while the attached carrier holds the unconsumed slot")
	}

	// 迟到携原授权 PASS 经 terminal 对账入账：越限轮由槽背书，槽被有效
	// 结果消费。
	if err := submitLateDispatchResult(t, engine, "late-attached-pair-a", pairA,
		reviewEvidence(review.ProductReview, pairA, authA, "PASS", "late endorsed result of the attached terminal carrier", nil)); err != nil {
		t.Fatalf("the late endorsed result must reconcile and consume the slot: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("the late endorsed record must consume the slot: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)

	// pair.b 仍闸（原槽已背书有效结果）；再授权放行并独占绑新槽，携槽
	// 入账闭环。
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("pair.b must stay gated while no clean slot exists: %v", reissued)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-attached-slot-grant-3", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "a fresh slot for the gated sibling",
	}})
	pairB := pendingActionByStep(t, engine, "pair.b")
	authB := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, pairB)
	if authB == "" || authB == authA {
		t.Fatalf("the gated sibling must hold a distinct fresh slot: %q vs %q", authB, authA)
	}
	submitReviewResult(t, engine, "result-attached-pair-b", pairB, reviewEvidence(review.ProductReview, pairB, authB, "PASS", "the gated sibling records on the fresh slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 5 || series.ExtraConsumed != 2 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestStagedResultOfAttachedTerminalDispatchPairsIntoResults：结果先到暂
// 存、回执 UNKNOWN、任务 terminal 化、挂接 ATTACHED——挂接的同一事务收
// 尾即完成配对（terminal 对账与配对候选集对 ATTACHED 同判）：暂存结果入
// Results、承诺落定，不悬挂；lane.d 在兄弟走完前保持被闸。
func TestStagedResultOfAttachedTerminalDispatchPairsIntoResults(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")

	// 结果先到（无回执）：暂存并携证据过完整预检。
	evidence := reviewEvidence(review.ProductReview, laneA, "", "PASS", "staged before the unknown receipt", nil)
	staged, err := NewReviewWorkerResultEvent(EventID("staged-attached-lane-a"), laneA, testProvider, "sha256:payload-staged-attached", evidence)
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fingerprintOf(t, engine)); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("staging = %+v err=%v", acceptance, err)
	}
	assertSeriesCommitmentHeld(t, engine)

	submitUnknownReceipt(t, engine, "spawn-unknown-staged-lane-a", laneA, "corr-unknown-staged")
	terminalizeDispatchTask(t, engine, laneA)
	attachUnknownReceipt(t, engine, laneA, "corr-unknown-staged", "worker-staged")

	// 挂接事务的收尾即配对：暂存离开暂存账、入 Results、承诺落定。
	snap := engine.LoadMustSucceed(t)
	if _, recorded := snap.State.Results[laneA]; !recorded {
		t.Fatalf("the staged result must pair in the attaching transaction: staged=%+v", snap.State.StagedResults[laneA])
	}
	if _, staged := snap.State.StagedResults[laneA]; staged {
		t.Fatal("the paired result must leave staging")
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
		t.Fatalf("the paired result must record its round: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 兄弟序走完（Completed=3）：lane.d 在承诺与兄弟全部落定前保持被闸。
	for _, lane := range []string{laneB, laneC} {
		submitReviewResult(t, engine, "result-staged-"+lane, lane, reviewEvidence(review.ProductReview, lane, "", "PASS", "sibling lane", nil))
	}
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("the exhausted series must keep lane.d gated: %v", reissued)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestFailRouteAfterAttachKeepsTheDispatchRecordable：回执 UNKNOWN 在
// pending 期挂接为 ATTACHED 后，宿主中断经 RecoverAttempt 的 FAIL 路由由
// 引擎终态化并收回簿记——派发变为非 pending＋ATTACHED＋无结果，仍计入承
// 诺（被闸兄弟不放行），迟到结果照常经 terminal 对账入账。
func TestFailRouteAfterAttachKeepsTheDispatchRecordable(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")

	// 挂接发生在派发仍在途时（回执对账的正常链路）。
	submitUnknownReceipt(t, engine, "spawn-unknown-recovery-lane-a", laneA, "corr-unknown-recovery")
	attachUnknownReceipt(t, engine, laneA, "corr-unknown-recovery", "worker-recovery")
	assertSeriesCommitmentHeld(t, engine)

	// 宿主中断、FAIL 路由：引擎终态化任务并收回簿记。
	plan, _, err := engine.RecoverAttempt(laneA, Interruption{Class: authoring.FailureBusinessReject, CauseKnown: true}, fingerprintOf(t, engine))
	if err != nil || plan.Action != RecoveryFail {
		t.Fatalf("recover %s = %+v err=%v", laneA, plan, err)
	}
	if _, still := engine.LoadMustSucceed(t).State.PendingActions[laneA]; still {
		t.Fatal("the FAIL route must reclaim the dispatch bookkeeping")
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the recovered dispatch holds its promise: %v", reissued)
	}

	// 迟到 PASS 经 terminal 对账入账：承诺随 Results 落定。
	if err := submitLateDispatchResult(t, engine, "late-recovered-lane-a", laneA,
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "late result after the FAIL route", nil)); err != nil {
		t.Fatalf("the dispatch kept recordable after the FAIL route: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 {
		t.Fatalf("the late result must record its round: %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestSuspendedPairingFailureLeavesItsTrail：暂存结果的收尾配对在接纳失
// 败（非顺序门——此处为已确认阻塞 finding 的同 revision 新候选门槛）时不
// 作废也不静默：每次尝试留痕、暂存保留、承诺保持，等待账本变化后的下一
// 次收尾再试。
func TestSuspendedPairingFailureLeavesItsTrail(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 2 {
		t.Fatalf("batch admitted %d lanes, want both: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")

	// lane.a 的 PASS 证据先到暂存（无回执）。
	evidence := reviewEvidence(review.ProductReview, laneA, "", "PASS", "staged before the blocking round", nil)
	staged, err := NewReviewWorkerResultEvent(EventID("staged-trail-lane-a"), laneA, testProvider, "sha256:payload-staged-trail", evidence)
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fingerprintOf(t, engine)); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("staging = %+v err=%v", acceptance, err)
	}

	// 兄弟 lane.b 的 FAIL 轮经用户确认形成已确认阻塞：同 revision 的新候
	// 选被门槛拦下，lane.a 暂存结果的配对尝试将失败。
	submitReviewResult(t, engine, "result-blocking-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "FAIL", "findings block the product", []review.Finding{
		{Severity: "P1", Message: "normal path produces a wrong result"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-blocking", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: laneB, RequirementRevision: "rev-1",
		ResultDigest: engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.RawCandidates[0].ResultDigest,
		FindingID:    engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.RawCandidates[0].Findings[0].ID,
		Decision:     "CONFIRM", Reason: "the user confirms the blocking finding", ExpectedEffectiveRevision: 1,
	}})

	// 挂接触发收尾配对：失败留痕、暂存保留、承诺保持。
	submitUnknownReceipt(t, engine, "spawn-unknown-trail-lane-a", laneA, "corr-unknown-trail")
	terminalizeDispatchTask(t, engine, laneA)
	attachUnknownReceipt(t, engine, laneA, "corr-unknown-trail", "worker-trail")
	snap := engine.LoadMustSucceed(t)
	if _, recorded := snap.State.Results[laneA]; recorded {
		t.Fatal("the pairing failure must not record the staged result")
	}
	if _, staged := snap.State.StagedResults[laneA]; !staged {
		t.Fatal("the suspended result must stay staged")
	}
	suspended := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == laneA && strings.Contains(record.Detail, "stays suspended") {
			suspended = true
		}
	}
	if !suspended {
		t.Fatalf("the failed pairing attempt must leave its trail: %+v", snap.State.RecoveryRecords)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// 放被拒的记录消化入迟到证据账（ObsoleteResults＋"could not be readmitted"
// 留痕），但派发本身不因此落定——terminal 对账路径对新结果保持开放，承
// 诺继续成立；随后到达的可纳结果经同一对账路径落账，承诺才落定。
// TestQuarantineReplayRejectionKeepsThePromiseUntilAResultRecords：隔离重
// 放被拒的记录消化入迟到证据账（ObsoleteResults＋"could not be readmitted"
// 留痕），但派发本身不因此落定——terminal 对账路径对新结果保持开放，承
// 诺继续成立；随后到达的可纳结果经同一对账路径落账，承诺才落定。
func TestQuarantineReplayRejectionKeepsThePromiseUntilAResultRecords(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA, laneB, laneC := pendingActionByStep(t, engine, "lane.a"), pendingActionByStep(t, engine, "lane.b"), pendingActionByStep(t, engine, "lane.c")
	// 兄弟先入账（Completed=2），terminal 化的 lane.a 仍持承诺：2+1+1=4>3，
	// lane.d 被闸。
	submitSpawnReceipt(t, engine, "spawn-reject-lane-a", laneA)
	terminalizeDispatchTask(t, engine, laneA)
	submitReviewResult(t, engine, "result-reject-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-reject-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the terminalized lane holds its promise: %v", reissued)
	}

	// 屏障期隔离一条不可纳的迟到结果（Operator 核实缺项只在重放时被
	// review 内核拒绝）。
	request, err := NewRequirementRequestEvent(EventID("req-reject-quarantine"), requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	inadmissible := reviewEvidence(review.ProductReview, laneA, "", "PASS", "late result that cannot be readmitted", nil)
	inadmissible.Verification = review.OperatorVerification{}
	late, err := NewReviewWorkerResultEvent(EventID("quarantine-inadmissible-lane-a"), laneA, testProvider, "sha256:payload-inadmissible-lane-a", inadmissible)
	if err != nil {
		t.Fatalf("late event: %v", err)
	}
	if acceptance, err := engine.Submit(late, fingerprintOf(t, engine)); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("barrier quarantine = %+v err=%v", acceptance, err)
	}

	// 解除：重放拒绝——记录消化入 ObsoleteResults 并留痕；派发不落定，
	// 承诺保持（terminal 对账路径对 lane.a 的新结果仍开放）。
	abortExistingAsk(t, engine, "req-reject-quarantine")
	snap := engine.LoadMustSucceed(t)
	if _, digested := snap.State.ObsoleteResults[laneA]; !digested {
		t.Fatal("the rejected replay must digest its record into the obsolete ledger")
	}
	readmittedTrail := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == laneA && strings.Contains(record.Detail, "could not be readmitted") {
			readmittedTrail = true
		}
	}
	if !readmittedTrail {
		t.Fatalf("the rejected replay must leave its trail: %+v", snap.State.RecoveryRecords)
	}
	if _, recorded := snap.State.Results[laneA]; recorded {
		t.Fatal("the rejected record must not enter Results")
	}
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated after the rejected replay: %v", reissued)
	}

	// 随后到达的可纳迟到结果经同一对账路径落账（Completed=3），承诺随
	// Results 落定；lane.d 仍闸，经授权背书放行闭环。
	if err := submitLateDispatchResult(t, engine, "late-admissible-lane-a", laneA,
		reviewEvidence(review.ProductReview, laneA, "", "PASS", "the admissible late result", nil)); err != nil {
		t.Fatalf("the admissible late result must reconcile: %v", err)
	}
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 3 {
		t.Fatalf("completed = %d, want 3", series.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-reject-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the gated fourth lane after the rejected replay",
	}})
	laneD := pendingActionByStep(t, engine, "lane.d")
	authID := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, laneD)
	if authID == "" {
		t.Fatal("the gated lane must hold the granted slot")
	}
	submitReviewResult(t, engine, "result-reject-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, authID, "PASS", "the gated lane records on its slot", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("final series = %+v", series)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestTerminalDispatchFailureResultSettlesTheHeldPromise：terminal 化后
// 永不回 PASS 结果的派发（宿主丢失结果）由宿主提交失败结果经同一对账路
// 径落账——Results 有记录、不建候选、不占轮、步骤不完成——承诺随之落
// 定释放，腾出的容量在同一事务补位放行，兄弟任意序入账闭环。
func TestTerminalDispatchFailureResultSettlesTheHeldPromise(t *testing.T) {
	cd, reg := productReviewDAGFixture(t,
		dagStep{name: "lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "lane.c", deps: []string{"prep.parse"}},
		dagStep{name: "lane.d", deps: []string{"prep.parse"}},
	)
	engine, _ := reviewEngine(t, cd, reg)

	lanes := issueReady(t, engine)
	if len(lanes) != 3 {
		t.Fatalf("batch admitted %d lanes, want the capacity-consistent three: %v", len(lanes), lanes)
	}
	laneA := pendingActionByStep(t, engine, "lane.a")
	laneB := pendingActionByStep(t, engine, "lane.b")
	laneC := pendingActionByStep(t, engine, "lane.c")
	submitSpawnReceipt(t, engine, "spawn-settle-lane-a", laneA)
	terminalizeDispatchTask(t, engine, laneA)
	assertSeriesCommitmentHeld(t, engine)
	if reissued := issueReady(t, engine); len(reissued) != 0 {
		t.Fatalf("lane.d must stay gated while the terminalized lane holds its promise: %v", reissued)
	}

	// 宿主丢失结果后提交失败结果（非 PASS、无审查证据）：经 terminal 对
	// 账路径落账——只进 Results 与对账留痕，不建候选、不占轮、步骤不完成。
	fail, err := NewWorkerResultEvent(EventID("settle-fail-lane-a"), laneA, testProvider, OutcomeFail, "sha256:payload-settle-fail", authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("failure event: %v", err)
	}
	if _, err := engine.Submit(fail, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("the failure result of the terminalized dispatch must reconcile: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if _, recorded := snap.State.Results[laneA]; !recorded {
		t.Fatal("the failure result must be recorded in Results")
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 0 || len(series.RawCandidates) != 0 {
		t.Fatalf("a non-PASS result must not create a review round: %+v", series)
	}
	if containsStep(snap.State.Completed, "lane.a") {
		t.Fatalf("a non-PASS result must not complete the step: %+v", snap.State.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)

	// 承诺已落定：同一事务的容量补位放行 lane.d（0+2+1=3 达界），兄弟任
	// 意序入账全部闭环。
	laneD := pendingActionByStep(t, engine, "lane.d")
	if got := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, laneD); got != "" {
		t.Fatalf("the refilled lane must stay unslotted: %q", got)
	}
	submitReviewResult(t, engine, "result-settle-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	submitReviewResult(t, engine, "result-settle-lane-c", laneC, reviewEvidence(review.ProductReview, laneC, "", "PASS", "lane c", nil))
	submitReviewResult(t, engine, "result-settle-lane-d", laneD, reviewEvidence(review.ProductReview, laneD, "", "PASS", "the refilled lane records under the released promise", nil))
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 3 {
		t.Fatalf("completed = %d, want 3", series.Completed)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestDispatchPromiseMatchesResultRecordabilityAcrossReceiptFaces：回执状态
// {无回执, SPAWNED, ATTACHED, UNKNOWN, FAILED} × 任务期 {pending, terminal}
// 的状态空间表——每格断言承诺双条件（计入 ⇔ 结果可入账或落定出口存
// 在）：计入判定以独立枚举（unsettledDispatchIDs，不复用实现谓词）为
// 准；落定出口以宿主失败结果（僵尸出口）为探针。terminal 行：计入 ⇔ 落
// 账成功；pending 行：结果必被接纳——回执构成在途观察事实的直接落账，其
// 余暂存挂起、承诺保持。UNKNOWN 回执在 terminal 行不计入且不可入账，对账
// 挂接为 ATTACHED 后两侧同步翻转（同一事实同一身份）。新增回执状态必须
// 在此表补行并保持同判。
func TestDispatchPromiseMatchesResultRecordabilityAcrossReceiptFaces(t *testing.T) {
	submitTransientFailureReceipt := func(t *testing.T, engine *Engine, lane string) {
		t.Helper()
		ev, err := NewSpawnReceiptWithFailureClass(EventID("spawn-face-failed"), lane, testProvider, "agent-face", SpawnStatusFailed, authoring.FailureTransientEngine)
		if err != nil {
			t.Fatalf("failed receipt event: %v", err)
		}
		if _, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("failed receipt submit: %v", err)
		}
	}
	// pending 列的构造在 terminal 列复用：先落回执面，再按需 terminal 化。
	rows := []struct {
		name       string
		terminal   bool
		counted    bool
		outcome    string // 失败结果探针的期态：recorded / staged / rejected
		attachFlip bool   // UNKNOWN 探针被拒后经挂接翻转两侧再落账
		prepare    func(t *testing.T, engine *Engine, lane string)
	}{
		{"pending without a receipt", false, true, "staged", false, func(t *testing.T, engine *Engine, lane string) {}},
		{"pending with a SPAWNED receipt", false, true, "recorded", false, func(t *testing.T, engine *Engine, lane string) {
			submitSpawnReceipt(t, engine, "spawn-face", lane)
		}},
		{"pending with an ATTACHED receipt", false, true, "recorded", false, func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
			attachUnknownReceipt(t, engine, lane, "corr-face", "worker-face")
		}},
		{"pending with an UNKNOWN receipt", false, true, "staged", false, func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
		}},
		{"pending with a FAILED receipt", false, true, "staged", false, func(t *testing.T, engine *Engine, lane string) {
			submitTransientFailureReceipt(t, engine, lane)
		}},
		{"terminal without a receipt", true, false, "rejected", false, func(t *testing.T, engine *Engine, lane string) {}},
		{"terminal with a SPAWNED receipt", true, true, "recorded", false, func(t *testing.T, engine *Engine, lane string) {
			submitSpawnReceipt(t, engine, "spawn-face", lane)
		}},
		{"terminal with an ATTACHED receipt", true, true, "recorded", false, func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
			attachUnknownReceipt(t, engine, lane, "corr-face", "worker-face")
		}},
		{"terminal with an UNKNOWN receipt", true, false, "rejected", true, func(t *testing.T, engine *Engine, lane string) {
			submitUnknownReceipt(t, engine, "spawn-face", lane, "corr-face")
		}},
		{"terminal with a FAILED receipt", true, false, "rejected", false, func(t *testing.T, engine *Engine, lane string) {
			submitTransientFailureReceipt(t, engine, lane)
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			cd, reg := productReviewDAGFixture(t, dagStep{name: "lane.a", deps: []string{"prep.parse"}})
			engine, _ := reviewEngine(t, cd, reg)
			lanes := issueReady(t, engine)
			if len(lanes) != 1 {
				t.Fatalf("single-lane fixture issued %d dispatches: %v", len(lanes), lanes)
			}
			lane := lanes[0]
			row.prepare(t, engine, lane)
			if row.terminal {
				terminalizeDispatchTask(t, engine, lane)
			}
			assertCounted := func(want bool) {
				t.Helper()
				st := engine.LoadMustSucceed(t).State
				if got := slices.Contains(unsettledDispatchIDs(st), lane); got != want {
					t.Fatalf("unsettled-promise membership = %v, want %v", got, want)
				}
			}
			assertCounted(row.counted)

			acceptance, err := submitFailureResult(t, engine, "fail-face", lane)
			switch row.outcome {
			case "recorded":
				if err != nil {
					t.Fatalf("the failure-result exit must settle the counted dispatch: %v", err)
				}
				if _, recorded := engine.LoadMustSucceed(t).State.Results[lane]; !recorded {
					t.Fatal("the failure result must be recorded in Results")
				}
				assertCounted(false)
			case "staged":
				if err != nil || acceptance.Status != "STAGED" {
					t.Fatalf("the pending dispatch must hold its result in staging: %+v err=%v", acceptance, err)
				}
				assertCounted(true)
			case "rejected":
				if err == nil || rejectionCode(t, err) != CodeUnknownAction {
					t.Fatalf("an uncounted dispatch must reject the late result: %v", err)
				}
				assertCounted(false)
			}
			if row.attachFlip {
				// 对账挂接使观察事实取得在途身份：计入与可入账同步翻转。
				attachUnknownReceipt(t, engine, lane, "corr-face", "worker-face")
				assertCounted(true)
				if _, err := submitFailureResult(t, engine, "fail-face-after-attach", lane); err != nil {
					t.Fatalf("the attached dispatch must settle through the failure-result exit: %v", err)
				}
				if _, recorded := engine.LoadMustSucceed(t).State.Results[lane]; !recorded {
					t.Fatal("the failure result after the attach must be recorded in Results")
				}
				assertCounted(false)
			}
			assertSeriesCommitmentHeld(t, engine)
		})
	}
}

// TestUndefinedReceiptStatusStaysOutOfPromiseAndExits：未定义的回执状态
// 不构成在途观察事实——承诺不计入、terminal 对账不接纳，两侧保守地保持
// 双条件；未来新增回执状态在补入判定的同时必须在上表补行。
func TestUndefinedReceiptStatusStaysOutOfPromiseAndExits(t *testing.T) {
	if inflightWitnessReceipt("SOME_UNDEFINED_STATUS") {
		t.Fatal("an undefined receipt status must not count as an in-flight witness")
	}
}
