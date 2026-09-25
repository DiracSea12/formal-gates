// change_disposition_test.go 覆盖需求变化处置与派发生命周期、系列标记
// 的交集：在途（pending）、暂存（证据到回执未到）、隔离（屏障期回执与
// 证据都在）、任务终态（terminal）四种生命周期 × 重走本系列/重走他系列/
// 续用/放弃 × 产品审/技术审/非开发前标记的可达组合逐个驱动到终局。每个
// 组合断言同一组事实：Results 与 Obsolete 的归属、series 计轮、屏障期内
// 不出现新派发身份、账本照常加载。
package protocol

import (
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// intersectionFixture 返回引擎与在途的目标步派发：目标步（第三步）按给定
// 系列归属标记构造。非开发前标记走完双审与拓扑后在第三步上取目标；开发
// 前系列在结论后不再签发任何轮（签发闸规则 a），带标记目标改为该系列的
// 在途第二轮——首轮记录语义 FAIL（未裁决 P1 阻塞，系列未结论），账本欠轮
// 提案签出第二轮。
func intersectionFixture(t *testing.T, mark authoring.ReviewSeriesAction) (*Engine, string, string) {
	t.Helper()
	targetStep := "tail.gate"
	switch mark {
	case authoring.ReviewSeriesProductReview:
		targetStep = "tail.product"
	case authoring.ReviewSeriesStartReadiness:
		targetStep = "tail.readiness"
	}
	cd, reg := agentStepsDefinition(t, func(step string) authoring.AgentSpec {
		series := authoring.ReviewSeriesUnaffiliated
		if strings.Contains(step, "readiness") {
			series = authoring.ReviewSeriesStartReadiness
		} else if strings.Contains(step, "product") {
			series = authoring.ReviewSeriesProductReview
		}
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: series, Timeout: time.Minute,
		}
	}, "product.review", "start.readiness", targetStep)
	engine, dir := reviewEngine(t, cd, reg)
	if mark == authoring.ReviewSeriesUnaffiliated {
		return engine, dir, walkToTopologyAndRoute(t, engine)
	}
	return engine, dir, walkToInFlightRound(t, engine, seriesEvidenceOf(mark))
}

// walkToInFlightRound 把带标记系列驱动到在途的第二轮：首轮记录语义 FAIL
// （未裁决 P1 阻塞——系列保持未结论），账本欠轮提案在容量内签出第二轮。
// readiness 系列先过产品审（顺序门前提）。返回在途第二轮的派发身份。
func walkToInFlightRound(t *testing.T, engine *Engine, action review.Action) string {
	t.Helper()
	if action == review.StartReadiness {
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-ix-gate", product, reviewEvidence(review.ProductReview, product, "", "PASS", "gate round", nil))
	} else {
		issueReady(t, engine)
	}
	round1 := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-ix-round-1", round1, reviewEvidence(action, round1, "", "FAIL", "round one blocks", []review.Finding{
		{Severity: "P1", Message: "unconfirmed blocker keeps the series open"},
	}))
	return pendingActionID(t, engine)
}

// intersectionChange 描述一组需求变化决定：redoProduct/redoReadiness 勾选
// 重走范围；preserved 为确认续用；abort 为放弃；digest 参与包身份。
type intersectionChange struct {
	name           string
	redoProduct    bool
	redoReadiness  bool
	preserved      bool
	abort          bool
	deltaDigest    string
	reason         string
	decisionReason string
}

func (c intersectionChange) payload() *RequirementDecisionPayload {
	classification := RequirementClassificationChanged
	if c.preserved {
		classification = RequirementClassificationPreserved
	}
	return &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: c.deltaDigest,
		ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
		Classification: classification, RedoProductReview: c.redoProduct,
		RedoStartReadiness: c.redoReadiness, Reason: c.decisionReason,
	}
}

// drive 在引擎上提交一个 Ask 并按变更描述决定。
func (c intersectionChange) drive(t *testing.T, engine *Engine, requestID string) {
	t.Helper()
	if c.abort {
		request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request %s: %v", requestID, err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
		abortExistingAsk(t, engine, requestID)
		return
	}
	askAndDecideRequirement(t, engine, requestID, "proceed", c.payload())
}

// intersectionCheck 断言交集组的共同事实：屏障期无新派发身份、账本可加载。
func intersectionCheck(t *testing.T, engine *Engine) {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	for actionID := range snap.State.PendingActions {
		if strings.HasPrefix(actionID, "retry:") {
			t.Fatalf("a replacement identity appeared while the change was pending: %s", actionID)
		}
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("the ledger must load after the disposition: %v", err)
	}
}

// targetSeriesOf 把目标标记映射到期望的系列账本（非开发前标记无系列账）。
func targetSeriesOf(t *testing.T, engine *Engine, mark authoring.ReviewSeriesAction) *review.Series {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	switch mark {
	case authoring.ReviewSeriesProductReview:
		return &snap.State.ReviewSeries.ProductReview
	case authoring.ReviewSeriesStartReadiness:
		return &snap.State.ReviewSeries.StartReadiness
	}
	return nil
}

// TestPendingDispatchDispositionAcrossChangesAndMarks：在途派发 × 变化 ×
// 标记。重走本系列→决定事务内退位＋替换（替换完成新鲜轮）；重走他系列、
// 续用、放弃→派发原样在途，其结果按原绑定接纳。
func TestPendingDispatchDispositionAcrossChangesAndMarks(t *testing.T) {
	redone := intersectionChange{name: "redo own series", redoProduct: true, deltaDigest: "sha256:ix-redo-product", decisionReason: "the product walk is redone"}
	rows := []struct {
		mark    authoring.ReviewSeriesAction
		change  intersectionChange
		freshOK bool // 重走本系列：替换派发完成新鲜轮
		stays   bool // 派发原样在途：原派发直接完成
	}{
		{authoring.ReviewSeriesProductReview, redone, true, false},
		{authoring.ReviewSeriesProductReview, intersectionChange{name: "redo other series", redoReadiness: true, deltaDigest: "sha256:ix-redo-other", decisionReason: "only the readiness walk is redone"}, false, true},
		{authoring.ReviewSeriesStartReadiness, intersectionChange{name: "redo own series", redoReadiness: true, deltaDigest: "sha256:ix-redo-readiness", decisionReason: "the readiness walk is redone"}, true, false},
		{authoring.ReviewSeriesUnaffiliated, intersectionChange{name: "redo both predevelopment series", redoProduct: true, redoReadiness: true, deltaDigest: "sha256:ix-redo-both", decisionReason: "both walks are redone"}, false, true},
		{authoring.ReviewSeriesProductReview, intersectionChange{name: "preserved carry", preserved: true, deltaDigest: "sha256:ix-preserved", decisionReason: "meaning preserved"}, false, true},
		{authoring.ReviewSeriesProductReview, intersectionChange{name: "abort", abort: true, deltaDigest: "sha256:ix-abort", decisionReason: "the change is dropped"}, false, true},
	}
	for _, row := range rows {
		t.Run(row.change.name+" on "+string(row.mark), func(t *testing.T) {
			engine, _, target := intersectionFixture(t, row.mark)
			row.change.drive(t, engine, "req-ix-pending")
			snap := engine.LoadMustSucceed(t)
			if row.freshOK {
				if _, still := snap.State.PendingActions[target]; still {
					t.Fatal("the dispatch of the redone series must be retired by the decision")
				}
				if obsolete := snap.State.ObsoleteActions[target]; obsolete.ReplacedBy == "" {
					t.Fatalf("the retired dispatch must carry its replacement: %+v", obsolete)
				}
				freshRoundOnTheReplacement(t, engine, target, "result-ix-fresh", reviewEvidenceAt(seriesEvidenceOf(row.mark), "dispatch-ix-fresh", "rev-2", "PASS", "fresh round after the redo", nil))
			} else if row.stays {
				if _, still := snap.State.PendingActions[target]; !still {
					t.Fatal("a dispatch outside the redo scope must stay in flight")
				}
				if _, obsolete := snap.State.ObsoleteActions[target]; obsolete {
					t.Fatal("a dispatch outside the redo scope must not be retired")
				}
				if row.mark == authoring.ReviewSeriesUnaffiliated {
					submitPlainResult(t, engine, "result-ix-plain", target)
				} else {
					submitReviewResult(t, engine, "result-ix-carry", target, reviewEvidenceAt(seriesEvidenceOf(row.mark), "dispatch-ix-carry", "rev-1", "PASS", "carried binding", nil))
				}
			}
			snap = engine.LoadMustSucceed(t)
			if _, inResults := snap.State.Results[target]; !inResults && !row.freshOK {
				t.Fatalf("the completing result must be owned by Results: %+v", snap.State.Results)
			}
			if _, voided := snap.State.ObsoleteResults[target]; voided && !row.freshOK {
				t.Fatal("an admitted result must not be voided")
			}
			if series := targetSeriesOf(t, engine, row.mark); series != nil {
				if series.Completed != 2 || series.Effective.Status != "PASS" {
					t.Fatalf("the target series = completed %d %+v", series.Completed, series.Effective)
				}
			}
			intersectionCheck(t, engine)
		})
	}
}

// seriesEvidenceOf 给目标标记对应的开发前系列返回证据动作。
func seriesEvidenceOf(mark authoring.ReviewSeriesAction) review.Action {
	switch mark {
	case authoring.ReviewSeriesStartReadiness:
		return review.StartReadiness
	default:
		return review.ProductReview
	}
}

// submitPlainResult 提交无审查证据的 PASS worker result（非开发前派发的
// 正常完成形态）。
func submitPlainResult(t *testing.T, engine *Engine, event, actionID string) {
	t.Helper()
	if _, err := engine.Submit(spawnReceipt(t, EventID("spawn-"+event), actionID, "agent-"+event), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("plain spawn: %v", err)
	}
	ev, err := NewWorkerResultEvent(EventID(event), actionID, testProvider, OutcomePass, "sha256:payload-"+event, "")
	if err != nil {
		t.Fatalf("plain result event: %v", err)
	}
	if acceptance, err := engine.Submit(ev, fingerprintOf(t, engine)); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("plain result: %v %+v", err, acceptance)
	}
}

// TestStagedDispatchDispositionAcrossChanges：证据先到（暂存、回执未到）
// × 变化。重走本系列→暂存被声明处置作废＋替换（新鲜轮在替换上完成）；
// 他系列重走、续用、放弃→暂存保留，迟到回执配对完成。
func TestStagedDispatchDispositionAcrossChanges(t *testing.T) {
	rows := []struct {
		name   string
		mark   authoring.ReviewSeriesAction
		change intersectionChange
		redone bool
	}{
		{"redo own series voids the staged evidence", authoring.ReviewSeriesProductReview,
			intersectionChange{redoProduct: true, deltaDigest: "sha256:ix-staged-redo", decisionReason: "the product walk is redone"}, true},
		{"redo other series keeps the staging", authoring.ReviewSeriesStartReadiness,
			intersectionChange{redoProduct: true, deltaDigest: "sha256:ix-staged-other", decisionReason: "only the product walk is redone"}, false},
		{"preserved carry pairs the late receipt", authoring.ReviewSeriesProductReview,
			intersectionChange{preserved: true, deltaDigest: "sha256:ix-staged-preserved", decisionReason: "meaning preserved"}, false},
		{"abort pairs the late receipt", authoring.ReviewSeriesProductReview,
			intersectionChange{abort: true, deltaDigest: "sha256:ix-staged-abort", decisionReason: "the change is dropped"}, false},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			engine, _, target := intersectionFixture(t, row.mark)
			fp := fingerprintOf(t, engine)
			staged, err := NewReviewWorkerResultEvent("result-ix-staged", target, testProvider, "sha256:payload-ix-staged",
				reviewEvidence(seriesEvidenceOf(row.mark), target, "", "PASS", "evidence before the receipt", nil))
			if err != nil {
				t.Fatalf("staged event: %v", err)
			}
			if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
				t.Fatalf("stage: %v %+v", err, acceptance)
			}
			row.change.drive(t, engine, "req-ix-staged")
			snap := engine.LoadMustSucceed(t)
			if row.redone {
				if _, kept := snap.State.StagedResults[target]; kept {
					t.Fatal("the staged evidence of the redone series must leave staging")
				}
				if _, voided := snap.State.ObsoleteResults[target]; !voided {
					t.Fatal("the staged evidence of the redone series must be voided")
				}
				if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
					t.Fatalf("the voided round must not count: completed=%d", series.Completed)
				}
				freshRoundOnTheReplacement(t, engine, target, "result-ix-staged-fresh", reviewEvidenceAt(review.ProductReview, "dispatch-ix-staged-fresh", "rev-2", "PASS", "fresh round on the replacement", nil))
			} else if row.change.redoProduct {
				// 顺序门在产品审重走期关闭：迟到回执照常落账，配对挂起、
				// 暂存保留，计轮为零。
				if _, kept := snap.State.StagedResults[target]; !kept {
					t.Fatal("staging outside the redo scope must be kept")
				}
				receipt := spawnReceipt(t, "spawn-ix-staged", target, "agent-ix-staged")
				if acceptance, err := engine.Submit(receipt, fp); err != nil || acceptance.Status != "ACCEPTED" {
					t.Fatalf("late receipt during the redo: %v %+v", err, acceptance)
				}
				snap = engine.LoadMustSucceed(t)
				if _, kept := snap.State.StagedResults[target]; !kept {
					t.Fatal("the suspended pairing must keep the staging")
				}
				if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 {
					t.Fatalf("a suspended round must not count: completed=%d", series.Completed)
				}
				suspended := false
				for _, record := range snap.State.RecoveryRecords {
					if record.ActionID == target && strings.Contains(record.Detail, "stays pending until the product-review conclusion re-passes") {
						suspended = true
					}
				}
				if !suspended {
					t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
				}
			} else {
				if _, kept := snap.State.StagedResults[target]; !kept {
					t.Fatal("staging outside the redo scope must be kept")
				}
				receipt := spawnReceipt(t, "spawn-ix-staged", target, "agent-ix-staged")
				if acceptance, err := engine.Submit(receipt, fp); err != nil || acceptance.Status != "ACCEPTED" {
					t.Fatalf("late receipt pairing: %v %+v", err, acceptance)
				}
				if series := targetSeriesOf(t, engine, row.mark); series.Completed != 2 || series.Effective.Status != "PASS" {
					t.Fatalf("the paired round = completed %d %+v", series.Completed, series.Effective)
				}
			}
			intersectionCheck(t, engine)
		})
	}
}

// TestQuarantinedDispatchDispositionAcrossChanges：屏障期隔离（回执与证据
// 都在账）× 变化。重走本系列→解除时按声明作废、不重放；续用→解除时携原
// revision 与用户理由重放完成；放弃→解除时原样接纳；双 Ask 期隔离持有到
// 最后一个决定，持有期无重放也无新派发身份。
func TestQuarantinedDispatchDispositionAcrossChanges(t *testing.T) {
	quarantine := func(t *testing.T, engine *Engine, target string) {
		t.Helper()
		fp := fingerprintOf(t, engine)
		if _, err := engine.Submit(spawnReceipt(t, "spawn-ix-q", target, "agent-ix-q"), fp); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		ev, err := NewReviewWorkerResultEvent("result-ix-q", target, testProvider, "sha256:payload-ix-q",
			reviewEvidence(review.ProductReview, target, "", "PASS", "quarantined while the ask pends", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(ev, fp); err != nil || acceptance.Status != "QUARANTINED" {
			t.Fatalf("quarantine: %v %+v", err, acceptance)
		}
	}
	t.Run("redo own series voids the quarantined evidence at the lift", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		request, err := NewRequirementRequestEvent("req-ix-q-redo", requirementPackageOf(&RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-q-redo",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "the product walk is redone",
		}), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		quarantine(t, engine, target)
		token, err := engine.Freshness("req-ix-q-redo")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		if _, err := engine.Submit(requirementDecideEvent(t, "req-ix-q-redo", token, "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-q-redo",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "the product walk is redone",
		}), fingerprintOf(t, engine)); err != nil {
			t.Fatalf("decide: %v", err)
		}
		snap := engine.LoadMustSucceed(t)
		if _, admitted := snap.State.Results[target]; admitted {
			t.Fatal("the quarantined evidence of the redone series must not be admitted")
		}
		if _, voided := snap.State.ObsoleteResults[target]; !voided {
			t.Fatal("the quarantined evidence of the redone series must be voided")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
			t.Fatalf("the voided round must not count: completed=%d", series.Completed)
		}
		freshRoundOnTheReplacement(t, engine, target, "result-ix-q-fresh", reviewEvidenceAt(review.ProductReview, "dispatch-ix-q-fresh", "rev-2", "PASS", "fresh round on the replacement", nil))
		intersectionCheck(t, engine)
	})
	t.Run("preserved carry replays with the revision and the user reason", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		request, err := NewRequirementRequestEvent("req-ix-q-carry", requirementPackageOf(&RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-q-carry",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationPreserved, Reason: "meaning preserved, nothing redone",
		}), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		quarantine(t, engine, target)
		askAndDecideRequirement(t, engine, "req-ix-q-carry", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-q-carry",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationPreserved, Reason: "meaning preserved, nothing redone",
		})
		snap := engine.LoadMustSucceed(t)
		if _, admitted := snap.State.Results[target]; !admitted {
			t.Fatal("the carried result must reach Results")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("the carried round = completed %d %+v", series.Completed, series.Effective)
		}
		carried := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == target && strings.Contains(record.Detail, "carried by requirement decision req-ix-q-carry (meaning preserved, nothing redone)") {
				carried = true
			}
		}
		if !carried {
			t.Fatalf("the carry trail must carry the deciding request and the user reason: %+v", snap.State.RecoveryRecords)
		}
		intersectionCheck(t, engine)
	})
	t.Run("abort replays the quarantine unchanged", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		request, err := NewRequirementRequestEvent("req-ix-q-abort", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		quarantine(t, engine, target)
		abortExistingAsk(t, engine, "req-ix-q-abort")
		snap := engine.LoadMustSucceed(t)
		if _, admitted := snap.State.Results[target]; !admitted {
			t.Fatal("the replayed result must reach Results after the abort")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("the replayed round = completed %d %+v", series.Completed, series.Effective)
		}
		intersectionCheck(t, engine)
	})
	t.Run("dual asks hold the quarantine until the last decision", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		fp := fingerprintOf(t, engine)
		for _, requestID := range []string{"req-ix-q-hold", "req-ix-q-last"} {
			request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(nil), requirementOptions()...)
			if err != nil {
				t.Fatalf("request %s: %v", requestID, err)
			}
			if _, err := engine.Submit(request, fp); err != nil {
				t.Fatalf("submit request %s: %v", requestID, err)
			}
		}
		quarantine(t, engine, target)
		abortExistingAsk(t, engine, "req-ix-q-hold")
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 1 {
			t.Fatalf("the quarantine ledger must stay held while another ask pends: %+v", snap.State.QuarantinedResults)
		}
		if _, admitted := snap.State.Results[target]; admitted {
			t.Fatal("no replay may run while the barrier holds")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
			t.Fatalf("no round may complete while the barrier holds: completed=%d", series.Completed)
		}
		intersectionCheck(t, engine)
		abortExistingAsk(t, engine, "req-ix-q-last")
		snap = engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 0 || snap.State.ReviewSeries.ProductReview.Completed != 2 {
			t.Fatalf("the last decision must replay the quarantine: held=%d completed=%d",
				len(snap.State.QuarantinedResults), snap.State.ReviewSeries.ProductReview.Completed)
		}
	})
}

// TestTerminalTaskDispatchDispositionAcrossChanges：任务在屏障期内到
// terminal × 变化。重走他系列→解除时隔离记录走 terminal 对账（回执证明
// 派发在途过：入 Results＋记账＋对账留痕）；重走本系列→隔离记录按声明
// 作废，不进对账。
func TestTerminalTaskDispatchDispositionAcrossChanges(t *testing.T) {
	driveTerminal := func(t *testing.T, engine *Engine, target string) {
		t.Helper()
		fp := fingerprintOf(t, engine)
		snap := engine.LoadMustSucceed(t)
		pending := snap.State.PendingActions[target]
		for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
			ev, err := NewTaskEvent(EventID("task-ix-"+string(rune('a'+i))), pending.Task, pending.AttemptID, status)
			if err != nil {
				t.Fatalf("task event: %v", err)
			}
			if _, err := engine.Submit(ev, fp); err != nil {
				t.Fatalf("task progress %s during the barrier: %v", status, err)
			}
		}
	}
	t.Run("redo of another series reconciles the terminal task", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		fp := fingerprintOf(t, engine)
		request, err := NewRequirementRequestEvent("req-ix-term-other", requirementPackageOf(&RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-term-other",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoStartReadiness: true, Reason: "only the readiness walk is redone",
		}), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-ix-term", target, "agent-ix-term"), fp); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		ev, err := NewReviewWorkerResultEvent("result-ix-term", target, testProvider, "sha256:payload-ix-term",
			reviewEvidence(review.ProductReview, target, "", "PASS", "quarantined then the task reports terminal", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(ev, fp); err != nil || acceptance.Status != "QUARANTINED" {
			t.Fatalf("quarantine: %v %+v", err, acceptance)
		}
		driveTerminal(t, engine, target)
		askAndDecideRequirement(t, engine, "req-ix-term-other", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-term-other",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoStartReadiness: true, Reason: "only the readiness walk is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if _, admitted := snap.State.Results[target]; !admitted {
			t.Fatal("the terminal task's record must reconcile into Results")
		}
		if _, voided := snap.State.ObsoleteResults[target]; voided {
			t.Fatal("a reconciled record must not be voided")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" {
			t.Fatalf("the reconciled round = completed %d %+v", series.Completed, series.Effective)
		}
		reconciled := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == target && strings.Contains(record.Detail, "reconciled against the terminal task by requirement decision req-ix-term-other") {
				reconciled = true
			}
		}
		if !reconciled {
			t.Fatalf("no reconciliation trail: %+v", snap.State.RecoveryRecords)
		}
		intersectionCheck(t, engine)
	})
	t.Run("redo of the own series voids the quarantined evidence by declaration", func(t *testing.T) {
		engine, _, target := intersectionFixture(t, authoring.ReviewSeriesProductReview)
		fp := fingerprintOf(t, engine)
		request, err := NewRequirementRequestEvent("req-ix-term-own", requirementPackageOf(&RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-term-own",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "the product walk is redone",
		}), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-ix-term-own", target, "agent-ix-term-own"), fp); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		ev, err := NewReviewWorkerResultEvent("result-ix-term-own", target, testProvider, "sha256:payload-ix-term-own",
			reviewEvidence(review.ProductReview, target, "", "PASS", "quarantined then the task reports terminal", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(ev, fp); err != nil || acceptance.Status != "QUARANTINED" {
			t.Fatalf("quarantine: %v %+v", err, acceptance)
		}
		driveTerminal(t, engine, target)
		askAndDecideRequirement(t, engine, "req-ix-term-own", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:ix-term-own",
			ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
			Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "the product walk is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if _, admitted := snap.State.Results[target]; admitted {
			t.Fatal("the quarantined evidence of the redone series must not reconcile")
		}
		if _, voided := snap.State.ObsoleteResults[target]; !voided {
			t.Fatal("the quarantined evidence of the redone series must be voided by declaration")
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 {
			t.Fatalf("the voided round must not count: completed=%d", series.Completed)
		}
		intersectionCheck(t, engine)
	})
}
