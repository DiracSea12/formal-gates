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
