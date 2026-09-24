// requirement_test.go 覆盖用户需求变化在提交协议层的嫁接行为：确认
// revision 账本的受理回填与登记、P2/P1/用户发起三类来源的分类强制、
// CHANGED 重走范围的机械执行（series stale、拓扑/路线解绑）、已确认阻塞
// 对新鲜轮的门槛、修订屏障（Phase A）对业务签发的冻结、结论 carry 有效
// 性（续用结论与重走级联的开门/关门），以及账本完整性对篡改的可区分
// 拒绝。全部拒绝都断言零状态变化。
package protocol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// requirementOptions 是 REQUEST_REQUIREMENT_CHANGE 的标准选项集（choice id
// 封闭为 proceed/abort，Ask 上绑定用户确认的变更包）。
func requirementOptions() []AskOption {
	return []AskOption{{ID: "proceed", Label: "确认变更包"}, {ID: "abort", Label: "放弃变更"}}
}

// requirementPackageOf 投影决定载荷的包字段；载荷为 nil（放弃路径）时给
// 出合法默认包——Ask 必须携包，放弃决定不与包比对。
func requirementPackageOf(payload *RequirementDecisionPayload) RequirementDecisionPackage {
	if payload == nil {
		return RequirementDecisionPackage{
			CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-default",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		}
	}
	return payload.packageView()
}

// reviewEvidenceAt 构造绑定任意需求 revision 的审查证据。
func reviewEvidenceAt(action review.Action, dispatchID, revision, status, message string, findings []review.Finding) ReviewResultEvidence {
	return ReviewResultEvidence{
		Action: action, DispatchID: dispatchID, RequirementRevision: revision,
		Status: status, Message: message, Findings: findings, Verification: reviewVerification(),
	}
}

// askAndDecideRequirement 走需求变化控制的两段：创建 Ask，取当前
// freshness token 后提交决定（payload 为 nil 时是放弃决定）。
func askAndDecideRequirement(t *testing.T, engine *Engine, requestID string, choice AskOptionID, payload *RequirementDecisionPayload) Acceptance {
	t.Helper()
	fp := fingerprintOf(t, engine)
	request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(payload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	acceptance, err := engine.Submit(requirementDecideEvent(t, requestID, token, choice, payload), fp)
	if err != nil {
		t.Fatalf("submit decide %s: %v", requestID, err)
	}
	return acceptance
}

// askAndDecideRequirementRejecting 创建 Ask 后提交需求变化决定并返回其
// 错误；零状态变化断言只围绕决定提交本身（Ask 落账是合法提交）。
func askAndDecideRequirementRejecting(t *testing.T, engine *Engine, dir, requestID string, choice AskOptionID, payload *RequirementDecisionPayload) error {
	t.Helper()
	fp := fingerprintOf(t, engine)
	request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(payload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	before := stateBytesOf(t, dir)
	_, err = engine.Submit(requirementDecideEvent(t, requestID, token, choice, payload), fp)
	if err == nil {
		return nil
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatalf("rejected decide %s changed state bytes", requestID)
	}
	return err
}

// abortExistingAsk 以 decide-only 放弃一个已存在的需求变化 Ask（Ask 已由
// 原确认包创建，重发请求会撞事件台账）。
func abortExistingAsk(t *testing.T, engine *Engine, requestID string) {
	t.Helper()
	fp := fingerprintOf(t, engine)
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness %s: %v", requestID, err)
	}
	if _, err := engine.Submit(requirementDecideEvent(t, requestID, token, "abort", nil), fp); err != nil {
		t.Fatalf("abort %s: %v", requestID, err)
	}
}

func requirementDecideEvent(t *testing.T, requestID, token string, choice AskOptionID, payload *RequirementDecisionPayload) Event {
	t.Helper()
	var (
		ev  Event
		err error
	)
	if payload == nil {
		ev, err = NewDecideEvent(EventID("evt-"+requestID), RequestID(requestID), token, choice)
	} else {
		ev, err = NewRequirementDecideEvent(EventID("evt-"+requestID), RequestID(requestID), token, choice, *payload)
	}
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	return ev
}

// freshRoundAfterRedo 在重走确认后提交新鲜轮证据：pre-floor 的旧派发被
// 按声明终止并立即重签发（OBSOLETE_DISPATCH，结果留迟到证据账），同一
// 证据在替换派发上被正常接纳（回执先到）。返回替换派发的 actionID。
// freshRoundOnTheReplacement 完成重走新鲜轮的标记派发路径：重走决定已
// 按签发标记退位旧派发并在同一事务装上替换（签发代数＝决定）；替换携
// 新鲜证据接纳。
func freshRoundOnTheReplacement(t *testing.T, engine *Engine, retiredActionID, eventID string, evidence ReviewResultEvidence) string {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	obsolete, known := snap.State.ObsoleteActions[retiredActionID]
	if !known || obsolete.ReplacedBy == "" {
		t.Fatalf("the marked dispatch was not retired and replaced by the redo: %+v", obsolete)
	}
	replacement := obsolete.ReplacedBy
	acceptance := submitReviewResult(t, engine, eventID, replacement, evidence)
	if acceptance.Status != "ACCEPTED" {
		t.Fatalf("the fresh round on the replacement = %+v", acceptance)
	}
	return replacement
}

// freshRoundAfterDeclaredTermination 完成重走新鲜轮的证据声明路径：步不
// 带系列标记（语义判断步），系列身份由到达的证据声明——pre-floor 旧派
// 发的证据按声明终止（OBSOLETE_DISPATCH），替换派发携同证据完成新鲜轮。
func freshRoundAfterDeclaredTermination(t *testing.T, engine *Engine, oldActionID, eventID string, evidence ReviewResultEvidence) string {
	t.Helper()
	fp := fingerprintOf(t, engine)
	superseded, err := NewReviewWorkerResultEvent(EventID(eventID+"-old"), oldActionID, testProvider, "sha256:payload-"+eventID+"-old", evidence)
	if err != nil {
		t.Fatalf("superseded event: %v", err)
	}
	acceptance, err := engine.Submit(superseded, fp)
	if err != nil || acceptance.Status != "OBSOLETE_DISPATCH" {
		t.Fatalf("the pre-floor dispatch must be terminated by declaration: %v %+v", err, acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	replacement := snap.State.ObsoleteActions[oldActionID].ReplacedBy
	if replacement == "" || replacement == oldActionID {
		t.Fatalf("the terminated dispatch was not replaced: %+v", snap.State.ObsoleteActions[oldActionID])
	}
	if _, err := engine.Submit(spawnReceipt(t, EventID(eventID+"-spawn"), replacement, "agent-"+eventID), fp); err != nil {
		t.Fatalf("replacement spawn: %v", err)
	}
	fresh, err := NewReviewWorkerResultEvent(EventID(eventID), replacement, testProvider, "sha256:payload-"+eventID, evidence)
	if err != nil {
		t.Fatalf("fresh event: %v", err)
	}
	if acceptance, err := engine.Submit(fresh, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh round on the replacement: %v %+v", err, acceptance)
	}
	return replacement
}

// walkToDevelopment 走完双审（rev-1 双 PASS）＋拓扑（no-split）＋路线
// （full），返回尚未提交结果的第三个 agent 步的 pending action（重走轮
// 的派发槽）。
func walkToDevelopment(t *testing.T, engine *Engine) string {
	t.Helper()
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
	return pendingActionID(t, engine)
}

// TestRequirementLedgerSeedsFromIntakeReceipt：账本为空而受理回执已落账
// 时，加载即回填单条 INTAKE 初始账目；权威 revision 读取与受理值一致。
func TestRequirementLedgerSeedsFromIntakeReceipt(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.RequirementRevisions) != 1 {
		t.Fatalf("ledger entries = %d, want the seeded intake entry", len(snap.State.RequirementRevisions))
	}
	entry := snap.State.RequirementRevisions[0]
	if entry.Source != RequirementSourceIntake || entry.Classification != RequirementClassificationIntake ||
		entry.Revision != "rev-1" || entry.BaseRevision != "" || entry.StateRevision != snap.State.IntakeReceipt.Revision {
		t.Fatalf("intake entry = %+v", entry)
	}
	if got := snap.State.RequirementRevision(); got != "rev-1" {
		t.Fatalf("authoritative revision = %q, want rev-1", got)
	}
}

// TestP2DrivenPreservedRevisionKeepsReviews：P2 处置驱动的修订按用户声明
// 落 PRESERVED——series 原样（completed/effective 不动、不清审、不耗轮
// 次）、拓扑路线不动；未勾重走即续用，绑已确认旧 revision 的证据按
// carry 接纳，新 revision 的新鲜轮也被纳且无强制重审要求。
func TestP2DrivenPreservedRevisionKeepsReviews(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, _ := reviewEngine(t, cd, reg)

	// 产品审：一条 P2 finding（advisory→effective PASS），用户确认处置。
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the documented path"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-p2", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-product-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the advisory", ExpectedEffectiveRevision: 1,
	}})
	// 产品审轮已完成（证据语义 FAIL 仍完成派发），confirm 的 refill 已签发
	// 技术审：直接续走 readiness→拓扑→路线。
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
	nextAction := pendingActionID(t, engine)
	productBefore := snap.State.ReviewSeries.ProductReview
	readinessBefore := snap.State.ReviewSeries.StartReadiness

	// REVIEW_FINDING_P2 修订，用户声明 PRESERVED（P2 默认不清审）。
	acceptance := askAndDecideRequirement(t, engine, "req-change-p2", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p2",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "the fix touches wording only", FindingDigest: candidate.Findings[0].Digest,
	})
	snap = engine.LoadMustSucceed(t)
	ledger := snap.State.RequirementRevisions
	if len(ledger) != 2 || ledger[1].Revision != "rev-2" || ledger[1].BaseRevision != "rev-1" ||
		ledger[1].Classification != RequirementClassificationPreserved || len(ledger[1].DecidedScope) != 0 ||
		ledger[1].RequestID != "req-change-p2" || ledger[1].StateRevision != acceptance.Revision {
		t.Fatalf("change entry = %+v", ledger[1])
	}
	if got := snap.State.RequirementRevision(); got != "rev-2" {
		t.Fatalf("authoritative revision = %q, want rev-2", got)
	}
	// series 原样：账本、计轮与 effective 结论都不动。
	sameSeries := func(before, after review.Series) bool {
		return before.Completed == after.Completed &&
			before.Effective.Status == after.Effective.Status &&
			before.Effective.Source == after.Effective.Source &&
			before.Effective.Revision == after.Effective.Revision &&
			before.Effective.RequirementRevision == after.Effective.RequirementRevision &&
			len(before.Effective.BlockingFindingIDs) == len(after.Effective.BlockingFindingIDs) &&
			len(before.Effective.AdvisoryFindingIDs) == len(after.Effective.AdvisoryFindingIDs)
	}
	if !sameSeries(productBefore, snap.State.ReviewSeries.ProductReview) {
		t.Fatalf("PRESERVED must not touch the product series: %+v", snap.State.ReviewSeries.ProductReview)
	}
	if !sameSeries(readinessBefore, snap.State.ReviewSeries.StartReadiness) {
		t.Fatalf("PRESERVED must not touch the readiness series: %+v", snap.State.ReviewSeries.StartReadiness)
	}
	if snap.State.Topology == nil || snap.State.RouteMode != "full" {
		t.Fatalf("PRESERVED must keep topology and route: %+v %q", snap.State.Topology, snap.State.RouteMode)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}

	// 未勾重走即续用（carry 授权，§7.2.2:4）：绑已确认 rev-1 的证据照常
	// 接纳并携原 revision 记账——不因新权威 rev-2 而失格，也无强制重审。
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "spawn-carry-rev", nextAction, "agent-carry-rev"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	carried, err := NewReviewWorkerResultEvent("result-carried-rev", nextAction, testProvider, "sha256:carried-rev", reviewEvidenceAt(review.ProductReview, "dispatch-carried", "rev-1", "PASS", "old binding carried", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(carried, fp); err != nil {
		t.Fatalf("carried rev-1 evidence rejected under a preserved change: %v", err)
	}
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("carried round = completed %d %+v", series.Completed, series.Effective)
	}
	if _, admitted := snap.State.Results[nextAction]; !admitted {
		t.Fatal("the carried round did not reach the results ledger")
	}
}

// TestUserInitiatedChangedRedoScopeCombinations：CHANGED 只机械执行勾选
// 项——只勾 product-review 时仅该 series stale（readiness 经 UpstreamVoid
// 级联 PENDING 但账本不动、拓扑路线保持绑定）；全勾时完整重走路径可用
// （重新双审→重新拓扑→重新路线）。
func TestUserInitiatedChangedRedoScopeCombinations(t *testing.T) {
	newRun := func(t *testing.T) (*Engine, string) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again", "readiness.again")
		engine, dir := reviewEngine(t, cd, reg)
		walkToDevelopment(t, engine)
		return engine, dir
	}

	t.Run("product-review only keeps bindings and carries readiness", func(t *testing.T) {
		engine, _ := newRun(t)
		nextAction := pendingActionID(t, engine)
		askAndDecideRequirement(t, engine, "req-change-partial", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-partial",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "product meaning changed",
		})
		snap := engine.LoadMustSucceed(t)
		product := snap.State.ReviewSeries.ProductReview
		if product.Effective.Status != "PENDING" || !strings.HasPrefix(product.Effective.Source, "requirement-change:") ||
			len(product.Effective.DownstreamImpact) == 0 || product.Completed != 1 {
			t.Fatalf("redo product series = %+v", product.Effective)
		}
		// 未勾选的 readiness 即续用：结论、账本、UpstreamVoid 全部原样
		//（不勾不失效，§7.2.3/§7.2.4）。
		readiness := snap.State.ReviewSeries.StartReadiness
		if readiness.Effective.Status != "PASS" || readiness.UpstreamVoid || readiness.Completed != 1 || readiness.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("an unchecked readiness must be carried untouched: %+v void=%v completed=%d", readiness.Effective, readiness.UpstreamVoid, readiness.Completed)
		}
		if snap.State.Topology == nil || snap.State.RouteMode != "full" || len(snap.State.SelectedGates) != 2 {
			t.Fatalf("unchecked topology/route must stay bound: %+v %q", snap.State.Topology, snap.State.RouteMode)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (phase 不变)", snap.State.Phase)
		}
		// 新 revision 的产品审新鲜轮可走（pre-floor 旧派发按声明终止后由
		// 替换派发完成）；其重过不消耗技术审的任何轮次——携用的 readiness
		// PASS 原样（门重开的完整走线见
		// TestProductOnlyRedoCarriesTheReadinessConclusion）。
		freshRoundOnTheReplacement(t, engine, nextAction, "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		snap = engine.LoadMustSucceed(t)
		if downstream := snap.State.ReviewSeries.StartReadiness; downstream.Effective.Status != "PASS" || downstream.Completed != 1 || downstream.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("the carried readiness must survive the product re-review without a fresh round: %+v completed=%d", downstream.Effective, downstream.Completed)
		}
	})

	t.Run("full scope reopens the complete re-walk path", func(t *testing.T) {
		engine, _ := newRun(t)
		productRound := pendingActionID(t, engine)
		askAndDecideRequirement(t, engine, "req-change-full", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-full",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, RedoStartReadiness: true, RedoTopology: true, RedoRoute: true,
			Reason: "the whole walk is affected",
		})
		snap := engine.LoadMustSucceed(t)
		if snap.State.Topology != nil || snap.State.RouteMode != "" || snap.State.SelectedGates != nil {
			t.Fatalf("checked topology/route must be unbound: %+v %q %v", snap.State.Topology, snap.State.RouteMode, snap.State.SelectedGates)
		}
		if entry := snap.State.RequirementRevisions[1]; len(entry.DecidedScope) != 4 || entry.DecidedScope[0] != "product-review" ||
			entry.DecidedScope[1] != "start-readiness" || entry.DecidedScope[2] != "topology" || entry.DecidedScope[3] != "route" {
			t.Fatalf("full scope entry = %+v", entry.DecidedScope)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (账本 residue 仍证明其合法)", snap.State.Phase)
		}
		// 重新双审（rev-2）→ 重新拓扑 → 重新路线。pre-floor 的产品审在途
		// 派发按声明终止，替换派发完成新鲜轮；其后签发的技术审派发天然
		// ≥floor。
		if snap.State.ReviewSeries.ProductReview.Effective.Status != "PENDING" || snap.State.ReviewSeries.StartReadiness.Effective.Status != "PENDING" {
			t.Fatalf("both series must be staled: %+v %+v", snap.State.ReviewSeries.ProductReview.Effective, snap.State.ReviewSeries.StartReadiness.Effective)
		}
		freshRoundOnTheReplacement(t, engine, productRound, "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		readinessRound := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-2", readinessRound, reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		snap = engine.LoadMustSucceed(t)
		if downstream := snap.State.ReviewSeries.StartReadiness; downstream.Effective.Status != "PASS" || downstream.UpstreamVoid {
			t.Fatalf("fresh readiness round must rebuild its own conclusion: %+v", downstream.Effective)
		}
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology-2", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆：重走后仍整体推进",
		}})
		askAndDecide(t, engine, ControlSelectRoute, "req-route-2", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "full", Gates: []string{"gate.quality", "gate.security"},
		}})
		snap = engine.LoadMustSucceed(t)
		if snap.State.Topology == nil || snap.State.RouteMode != "full" {
			t.Fatalf("re-walk must rebind topology and route: %+v %q", snap.State.Topology, snap.State.RouteMode)
		}
	})
}

// TestP1DrivenChangeForcesChangedAndOwningSeriesRedo：确认 P1 驱动的修订
// 不可声明 PRESERVED、重走范围必须包含 finding 所在 series；正确载荷登记
// 后，已确认阻塞门槛在新 revision 下放行新一轮。
func TestP1DrivenChangeForcesChangedAndOwningSeriesRedo(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "review.p1.one", "review.p1.two", "review.p1.three")
	engine, dir := reviewEngine(t, cd, reg)

	// 产品审确认一条 P1 阻塞 finding。
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-p1", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-product-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the blocker", ExpectedEffectiveRevision: 1,
	}})

	digest := candidate.Findings[0].Digest
	preserved := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP1, Classification: RequirementClassificationPreserved,
		Reason: "attempt to preserve meaning", FindingDigest: digest,
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-p1-preserved", "proceed", preserved); err == nil {
		t.Fatal("P1-driven PRESERVED accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("P1 PRESERVED code = %q", code)
	}

	missingScope := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
		RedoTopology: true, Reason: "scope omits the owning series", FindingDigest: digest,
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-p1-noscope", "proceed", missingScope); err == nil {
		t.Fatal("P1-driven change without the owning series redo accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("P1 no-scope code = %q", code)
	}

	unknown := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "binds a finding that does not exist", FindingDigest: "sha256:no-such-finding",
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-unknown-finding", "proceed", unknown); err == nil {
		t.Fatal("change bound to an unknown finding accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("unknown finding code = %q", code)
	}

	// 被拒决定只解决不了 Ask——三笔未决 Ask 各以 abort 收束，解除其屏障
	//（拒绝本身零状态，Ask 仍在等待有效决定）。
	for _, pending := range []string{"req-p1-preserved", "req-p1-noscope", "req-unknown-finding"} {
		abortExistingAsk(t, engine, pending)
	}

	// 正确载荷：CHANGED＋重走 finding 所在 series。此后新 revision 的新
	// 鲜轮放行（既有阻塞不再拦）——决定按签发标记退位在途派发并装上替
	// 换，替换派发完成新鲜轮。
	preDecide := pendingActionID(t, engine)
	askAndDecideRequirement(t, engine, "req-p1-changed", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the confirmed blocker is repaired in the changed requirement", FindingDigest: digest,
	})
	freshRoundOnTheReplacement(t, engine, preDecide, "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "fresh review of the changed requirement", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("fresh round under the new revision = completed %d %+v", series.Completed, series.Effective)
	}
}

// TestConfirmedBlockingGateHoldsFreshRounds：同需求 revision 下已确认的
// P0/P1 阻塞拒绝新候选；dismiss 清除阻塞后同 revision 新候选放行（纠正
// 路径不受门槛约束）。
func TestConfirmedBlockingGateHoldsFreshRounds(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "round.one", "round.two")
	engine, dir := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)

	first := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-1", first, reviewEvidence(review.ProductReview, "dispatch-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the blocker", ExpectedEffectiveRevision: 1,
	}})

	// 同 revision 新候选被门槛拒绝。
	second := pendingActionID(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "spawn-2", second, "agent-2"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	blocked, err := NewReviewWorkerResultEvent("result-2", second, testProvider, "sha256:payload-2", reviewEvidence(review.ProductReview, "dispatch-2", "", "PASS", "resample the same inputs", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(blocked, fp); err == nil {
		t.Fatal("fresh round accepted while a confirmed P1 blocks at the same revision")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), "changed and reconfirmed") {
		t.Fatalf("blocking gate code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("rejected blocked round changed state bytes")
	}

	// dismiss 清除阻塞：同 revision 新候选放行。Supersedes 与 CAS 代数都从
	// 账本读取当前最新决定。
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	dismiss := AskOption{ID: AskOptionID("dismiss:" + candidate.Findings[0].ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-dismiss", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Supersedes: series.Adjudications[0].ID,
		Reason: "user rejects the finding after re-verification", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Effective.Status != "PASS" {
		t.Fatalf("dismiss must clear the block: %+v", series.Effective)
	}
	retry, err := NewReviewWorkerResultEvent("result-2-retry", second, testProvider, "sha256:payload-2", reviewEvidence(review.ProductReview, "dispatch-2", "", "PASS", "round after the dismissal", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(retry, fp); err != nil {
		t.Fatalf("round after dismissal rejected: %v", err)
	}
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 {
		t.Fatalf("completed = %d, want 2", series.Completed)
	}
}

// TestRequirementChangeBarrierPhaseA：未决需求变化 Ask 冻结新的业务签发
// （可区分错误），回执/结果/既有 Ask 不受阻（refill 静默扣发），Ask 决定
// 后解冻；放弃决定不登记任何 revision；其它控制不触发屏障。
func TestRequirementChangeBarrierPhaseA(t *testing.T) {
	t.Run("issuance frozen while the ask is pending", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequirementRequestEvent("req-barrier", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		plan, _, err := engine.Plan()
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err == nil {
			t.Fatal("issuance accepted while the change ask is pending")
		} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
			t.Fatalf("barrier code = %q", code)
		}
		// 放弃决定（nil 载荷）：Ask 解决、屏障解除（同一次提交的 refill
		// 即补进首个业务任务）、账本不登记。
		token, err := engine.Freshness("req-barrier")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		decline, err := NewDecideEvent("evt-req-barrier", "req-barrier", token, "abort")
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		if _, err := engine.Submit(decline, fp); err != nil {
			t.Fatalf("decline decide: %v", err)
		}
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.PendingActions) != 1 {
			t.Fatalf("issuance must resume on the decline: %v", snap.State.PendingActions)
		}
		if len(snap.State.RequirementRevisions) != 1 {
			t.Fatalf("decline must not register a revision: %d entries", len(snap.State.RequirementRevisions))
		}
	})

	t.Run("results are durably quarantined and reclassified by a preserved decide", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		preserved := &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-barrier",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
			Reason: "wording only",
		}
		request, err := NewRequirementRequestEvent("req-barrier-result", preserved.packageView(), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		// 回执不受阻；结果持久隔离：回执 QUARANTINED、事件进台账防重放、
		// 不推进不入 Results（进程重启不丢失，重分类在决定事务内）。
		if _, err := engine.Submit(spawnReceipt(t, "spawn-under-barrier", product, "agent-under-barrier"), fp); err != nil {
			t.Fatalf("spawn receipt under barrier: %v", err)
		}
		// worker 已按 Ask 绑定的候选 revision（rev-2）复审：隔离账存原样。
		ev, err := NewReviewWorkerResultEvent("result-under-barrier", product, testProvider, "sha256:payload-barrier", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed the candidate", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		acceptance, err := engine.Submit(ev, fp)
		if err != nil {
			t.Fatalf("quarantined submit: %v", err)
		}
		if acceptance.Status != "QUARANTINED" {
			t.Fatalf("result under barrier status = %q, want QUARANTINED", acceptance.Status)
		}
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 1 || len(snap.State.Results) != 0 || snap.State.ReviewSeries.ProductReview.Completed != 0 {
			t.Fatalf("quarantine must park the result without admitting it: quarantined=%d results=%d completed=%d",
				len(snap.State.QuarantinedResults), len(snap.State.Results), snap.State.ReviewSeries.ProductReview.Completed)
		}
		// PRESERVED 决定的同一事务：隔离结果按正常接纳路径重放，真实完成
		// 入账（completed+1），隔离账清空。
		askAndDecideRequirement(t, engine, "req-barrier-result", "proceed", preserved)
		snap = engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 0 {
			t.Fatalf("the decide must reclassify the quarantine ledger: %+v", snap.State.QuarantinedResults)
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("replayed result did not complete admission: completed=%d %+v", series.Completed, series.Effective)
		}
		if _, admitted := snap.State.Results[product]; !admitted {
			t.Fatal("replayed result did not reach the results ledger")
		}
		if len(snap.State.PendingActions) != 1 {
			t.Fatalf("issuance must resume after the decide: %v", snap.State.PendingActions)
		}
	})

	t.Run("a changed decide voids in-scope quarantined evidence", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		request, err := NewRequirementRequestEvent("req-barrier-void", RequirementDecisionPackage{
			CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-void", Source: RequirementSourceUserInitiated,
			ImpactDigest:   "sha256:impact",
			Classification: RequirementClassificationChanged, RedoProductReview: true,
		}, requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-void", product, "agent-void"), fp); err != nil {
			t.Fatalf("spawn receipt under barrier: %v", err)
		}
		ev, err := NewReviewWorkerResultEvent("result-void", product, testProvider, "sha256:payload-void", reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "bound to the superseded revision", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(ev, fp); err != nil || acceptance.Status != "QUARANTINED" {
			t.Fatalf("quarantine: %v %+v", err, acceptance)
		}
		askAndDecideRequirement(t, engine, "req-barrier-void", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-void",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "the product review is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 0 {
			t.Fatalf("the decide must reclassify the quarantine ledger: %+v", snap.State.QuarantinedResults)
		}
		if _, voided := snap.State.ObsoleteResults[product]; !voided {
			t.Fatalf("in-scope quarantined evidence must be voided: %+v", snap.State.ObsoleteResults)
		}
		if replacement := snap.State.ObsoleteActions[product].ReplacedBy; replacement == "" {
			t.Fatalf("the in-scope dispatch must be replaced for the fresh round: %+v", snap.State.ObsoleteActions[product])
		}
		voided := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "terminated by declaration") {
				voided = true
			}
		}
		if !voided {
			t.Fatalf("no void trail for %s: %+v", product, snap.State.RecoveryRecords)
		}
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 0 || series.Effective.Status != "PENDING" {
			t.Fatalf("voided evidence must not reach the ledger: completed=%d %+v", series.Completed, series.Effective)
		}
	})

	t.Run("other decides are frozen by the table", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		requirementAsk, err := NewRequirementRequestEvent("req-barrier-topology", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(requirementAsk, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		topologyAsk, err := NewRequestEvent("req-topology-frozen", ControlConfirmTopology, topologyOptions()...)
		if err != nil {
			t.Fatalf("topology ask: %v", err)
		}
		if _, err := engine.Submit(topologyAsk, fp); err != nil {
			t.Fatalf("submit topology ask: %v", err)
		}
		token, err := engine.Freshness("req-topology-frozen")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		ev, err := NewReviewDecideEvent("evt-req-topology-frozen", "req-topology-frozen", token, "no-split", ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆",
		}})
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("topology decide accepted while the change ask is pending")
		} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
			t.Fatalf("frozen topology decide code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("frozen topology decide changed state bytes")
		}
		// 屏障解除后同一 Ask 的决定照常走接纳（此处被业务条件拒绝，证明
		// 之前的拦截确系屏障而非其它约束）。
		askAndDecideRequirement(t, engine, "req-barrier-topology", "abort", nil)
		token, err = engine.Freshness("req-topology-frozen")
		if err != nil {
			t.Fatalf("freshness after the barrier: %v", err)
		}
		retry, err := NewReviewDecideEvent("evt-req-topology-retry", "req-topology-frozen", token, "no-split", ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆",
		}})
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		if _, err := engine.Submit(retry, fp); err == nil {
			t.Fatal("post-barrier topology decide accepted without readiness PASS")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("post-barrier topology code = %q", code)
		}
	})

	t.Run("host action intents are frozen", func(t *testing.T) {
		engine, _, _, _ := preparedEngine(t)
		fp := fingerprintOf(t, engine)
		request, err := NewRequirementRequestEvent("req-barrier-host", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if _, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp); err == nil {
			t.Fatal("host action intent persisted while the change ask is pending")
		} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
			t.Fatalf("frozen host action code = %q", code)
		}
		// 决定（放弃）后同一调用照常落账。
		token, err := engine.Freshness("req-barrier-host")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		decline, err := NewDecideEvent("evt-req-barrier-host", "req-barrier-host", token, "abort")
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		if _, err := engine.Submit(decline, fp); err != nil {
			t.Fatalf("abort decide: %v", err)
		}
		if _, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp); err != nil {
			t.Fatalf("host action after the decide: %v", err)
		}
	})

	t.Run("other pending asks do not freeze issuance", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequestEvent("req-reset-idle", ControlReset, AskOption{ID: "proceed", Label: "重置"})
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		// 非 REQ_CHANGE 的未决 Ask 不构成屏障：业务签发照常。
		issueReady(t, engine)
	})
}

// TestRequirementDecisionValidationAndCAS：CAS 基线、载荷配对与 freshness
// 的负控——每例可区分错误码、零状态变化。
func TestRequirementDecisionValidationAndCAS(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)

	// CAS：载荷基线≠Ask 落账基线（＝当前权威）→ Ask 过期拒绝（零状态，
	// 须在新基线上重建）。
	stale := &RequirementDecisionPayload{
		BaseRevision: "rev-0", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "stale base",
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-cas", "proceed", stale); err == nil {
		t.Fatal("stale base accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementAskExpired || !strings.Contains(err.Error(), "rebuild the ask") {
		t.Fatalf("stale base code = %q (%v)", code, err)
	}

	// 载荷形状：构造层拒绝（不触引擎）。
	schema := []struct {
		name    string
		payload RequirementDecisionPayload
	}{
		{"candidate equals base", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-1", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"empty delta digest", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"empty impact digest", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"empty reason", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved}},
		{"unknown source", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: "AI_INITIATED", Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"unknown classification", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: "UNCHANGED", Reason: "r"}},
		{"user-initiated binds a finding", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r", FindingDigest: "sha256:f"}},
		{"finding-driven without a digest", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved, Reason: "r"}},
	}
	for _, tc := range schema {
		if _, err := NewRequirementDecideEvent("evt-schema", "req-schema", "sha256:token", "proceed", tc.payload); err == nil {
			t.Fatalf("%s constructed", tc.name)
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q", tc.name, code)
		}
	}
	// 双 typed 变体并存拒绝。
	both := Event{ID: "evt-both", Kind: KindDecide, Decide: &DecidePayload{Request: "req-both", Token: "sha256:token", Choice: "proceed",
		Review:      &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "n"}},
		Requirement: &RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}}}
	if err := both.Validate(); err == nil {
		t.Fatal("dual typed variants accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("dual variant code = %q", code)
	}

	// 被拒决定留下未决 Ask（屏障在位）：先以 decide-only 收束（Ask 已由
	// 原包创建，不得重发请求），避免污染后续配对用例。
	abortExistingAsk(t, engine, "req-cas")

	// 控制↔载荷配对：需求控制携 Review 载荷、审查控制携 Requirement 载荷
	// 都拒绝。
	reviewPayload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "n"}}
	if err := askAndDecideRequirementCrossPayload(t, engine, dir, "req-cross-review", &reviewPayload, nil); err == nil {
		t.Fatal("requirement control with a review payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("cross review code = %q", code)
	}
	abortExistingAsk(t, engine, "req-cross-review")
	requirementPayload := RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}
	if err := askAndDecideRequirementCrossPayload(t, engine, dir, "req-cross-requirement", nil, &requirementPayload); err == nil {
		t.Fatal("topology control with a requirement payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("cross requirement code = %q", code)
	}

	// freshness 过期回归：后续提交取代旧 token。
	fp := fingerprintOf(t, engine)
	request, err := NewRequirementRequestEvent("req-fresh", requirementPackageOf(&requirementPayload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	oldToken, err := engine.Freshness("req-fresh")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	other, err := NewRequestEvent("req-fresh-other", ControlAbort, AskOption{ID: "abort", Label: "终止"})
	if err != nil {
		t.Fatalf("other request: %v", err)
	}
	if _, err := engine.Submit(other, fp); err != nil {
		t.Fatalf("submit other request: %v", err)
	}
	expired := requirementDecideEvent(t, "req-fresh", oldToken, "proceed", &requirementPayload)
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(expired, fp); err == nil {
		t.Fatal("stale token accepted")
	} else if code := rejectionCode(t, err); code != CodeStaleFreshness {
		t.Fatalf("stale token code = %q", code)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("stale token rejection changed state bytes")
	}
}

// askAndDecideRequirementCrossPayload 在需求控制 Ask 上提交 Review 载荷
// （reviewPayload 非 nil），或在拓扑控制 Ask 上提交 Requirement 载荷
// （requirementPayload 非 nil），返回决定提交的错误并断言零状态变化。
func askAndDecideRequirementCrossPayload(t *testing.T, engine *Engine, dir, requestID string, reviewPayload *ReviewDecisionPayload, requirementPayload *RequirementDecisionPayload) error {
	t.Helper()
	fp := fingerprintOf(t, engine)
	control := ControlRequestRequirementChange
	choice := AskOptionID("proceed")
	options := requirementOptions()
	pkg := requirementPackageOf(requirementPayload)
	if requirementPayload != nil {
		control = ControlConfirmTopology
		choice = "no-split"
		options = topologyOptions()
	}
	var request Event
	var err error
	if control == ControlRequestRequirementChange {
		request, err = NewRequirementRequestEvent(EventID(requestID), pkg, options...)
	} else {
		request, err = NewRequestEvent(EventID(requestID), control, options...)
	}
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	decide := &DecidePayload{Request: RequestID(requestID), Token: token, Choice: choice, Review: reviewPayload, Requirement: requirementPayload}
	ev := Event{ID: EventID("evt-" + requestID), Kind: KindDecide, Decide: decide}
	if err := ev.Validate(); err != nil {
		t.Fatalf("decide event: %v", err)
	}
	before := stateBytesOf(t, dir)
	_, err = engine.Submit(ev, fp)
	if err == nil {
		return nil
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatalf("cross payload decide %s changed state bytes", requestID)
	}
	return err
}

// TestRequirementLedgerTamperingRejectedAtLoad：篡改确认账本（断 base 链、
// 伪造 P1 源的分类、越集 scope、抹掉 INTAKE 首条、伪造 state 代数）在
// 加载边界可区分拒绝；还原后可正常加载。
func TestRequirementLedgerTamperingRejectedAtLoad(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	askAndDecideRequirement(t, engine, "req-change-tamper", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-tamper",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "seed a committed change entry",
	})
	original := stateBytesOf(t, dir)
	restore := func() {
		if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
			t.Fatalf("restore state: %v", err)
		}
	}
	entry := func(content map[string]any) map[string]any {
		ledger, _ := content["requirementRevisions"].([]any)
		change, _ := ledger[1].(map[string]any)
		return change
	}

	tamperStateContent(t, dir, func(content map[string]any) {
		entry(content)["baseRevision"] = "rev-x"
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("broken base chain loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("broken chain code = %q", code)
	}
	restore()

	tamperStateContent(t, dir, func(content map[string]any) {
		entry(content)["source"] = RequirementSourceReviewFindingP1
		entry(content)["classification"] = RequirementClassificationPreserved
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("forged classification loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("forged classification code = %q", code)
	}
	restore()

	tamperStateContent(t, dir, func(content map[string]any) {
		entry(content)["decidedScope"] = []any{"qa"}
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("out-of-set scope loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("out-of-set scope code = %q", code)
	}
	restore()

	tamperStateContent(t, dir, func(content map[string]any) {
		entry(content)["decidedScope"] = []any{"topology", "route", "topology"}
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("repeated scope item loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("repeated scope code = %q", code)
	}
	restore()

	tamperStateContent(t, dir, func(content map[string]any) {
		ledger, _ := content["requirementRevisions"].([]any)
		content["requirementRevisions"] = ledger[1:]
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("ledger without the intake head loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("missing head code = %q", code)
	}
	restore()

	tamperStateContent(t, dir, func(content map[string]any) {
		entry(content)["stateRevision"] = float64(0)
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("non-monotonic state revision loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("non-monotonic code = %q", code)
	}
	restore()

	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}
}

// TestRequirementVariantsParticipateInCanonicalDigest：需求决定载荷进入
// canonical 编码——内容变化必变 digest，事件身份覆盖变体字节。
func TestRequirementVariantsParticipateInCanonicalDigest(t *testing.T) {
	payload := RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "first",
	}
	first, err := NewRequirementDecideEvent("evt-requirement-digest", "req-1", "sha256:token", "proceed", payload)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	digest, err := first.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	repeated, err := NewRequirementDecideEvent("evt-requirement-digest", "req-1", "sha256:token", "proceed", payload)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if again, _ := repeated.Digest(); again != digest {
		t.Fatal("requirement decide digest is not stable")
	}
	payload.Reason = "different reason"
	changed, err := NewRequirementDecideEvent("evt-requirement-digest", "req-1", "sha256:token", "proceed", payload)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if again, _ := changed.Digest(); again == digest {
		t.Fatal("requirement decision payload does not participate in the event digest")
	}
}

// walkToTopologyAndRoute 走完双审（rev-1 双 PASS）＋拓扑（no-split）但不选
// 路线，返回尚未提交结果的第三个 agent 步的 pending action——用于在
// TOPOLOGY_AND_ROUTE 上公开观察双审门的开合。
func walkToTopologyAndRoute(t *testing.T, engine *Engine) string {
	t.Helper()
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}
	return pendingActionID(t, engine)
}

// TestCarriedProductConclusionReopensAfterReadinessOnlyRedo：用户只勾选重走
// start-readiness（产品审携原 revision 续用，§7.2.4 carry）——续用结论绑
// rev-1 时，rev-2 的新一轮 readiness 证据必须被顺序守卫放行，readiness
// 重新 PASS 后开发门重开（路线选择重新可接纳并推进 DEVELOPMENT_PARALLEL）。
func TestCarriedProductConclusionReopensAfterReadinessOnlyRedo(t *testing.T) {
	t.Run("gate stays closed until the redone readiness series repasses", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
		engine, dir := reviewEngine(t, cd, reg)
		nextAction := walkToTopologyAndRoute(t, engine)

		askAndDecideRequirement(t, engine, "req-change-readiness-only", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-readiness",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoStartReadiness: true, Reason: "only the readiness walk is affected",
		})
		snap := engine.LoadMustSucceed(t)
		// 产品审续用：PASS 与计轮原样（结论绑 rev-1，属确认账本内 revision）。
		if product := snap.State.ReviewSeries.ProductReview; product.Completed != 1 || product.Effective.Status != "PASS" || product.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("carried product series = completed %d %+v", product.Completed, product.Effective)
		}
		if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PENDING" || readiness.Completed != 1 {
			t.Fatalf("redone readiness series = %+v completed=%d", readiness.Effective, readiness.Completed)
		}
		// 门关：readiness 重走期间路线选择拒绝。
		if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-closed-1", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "full", Gates: []string{"gate.quality", "gate.security"},
		}}); err == nil {
			t.Fatal("route accepted while the redone readiness series is PENDING")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("closed gate code = %q", code)
		}

		// 死锁修复点：rev-2 的新一轮 readiness 证据被顺序守卫放行（续用的
		// 产品审 PASS 绑 rev-1 同样有效）；pre-floor 的旧派发按声明终止，
		// 替换派发完成新鲜轮。
		freshRoundOnTheReplacement(t, engine, nextAction, "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		snap = engine.LoadMustSucceed(t)
		if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Completed != 2 || readiness.Effective.Status != "PASS" || readiness.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("readiness fresh round at rev-2 = completed %d %+v", readiness.Completed, readiness.Effective)
		}
		// 门重开：路线选择接纳并推进 DEVELOPMENT_PARALLEL。
		askAndDecide(t, engine, ControlSelectRoute, "req-route-reopen", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "full", Gates: []string{"gate.quality", "gate.security"},
		}})
		snap = engine.LoadMustSucceed(t)
		if snap.State.RouteMode != "full" || snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("route = %q phase = %s, want full/DEVELOPMENT_PARALLEL", snap.State.RouteMode, snap.State.Phase)
		}
	})

	t.Run("literal repro at DEVELOPMENT_PARALLEL", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
		engine, _ := reviewEngine(t, cd, reg)
		nextAction := walkToDevelopment(t, engine)
		askAndDecideRequirement(t, engine, "req-change-dev-repro", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-dev",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoStartReadiness: true, Reason: "readiness-only redo after development started",
		})
		// 主代理复现的原句拒绝在这里必须不再发生（pre-floor 旧派发按声明
		// 终止，替换派发完成 rev-2 新鲜轮）。
		freshRoundOnTheReplacement(t, engine, nextAction, "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		snap := engine.LoadMustSucceed(t)
		if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PASS" || readiness.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("readiness after the fresh round = %+v", readiness.Effective)
		}
		if product := snap.State.ReviewSeries.ProductReview; product.Effective.Status != "PASS" || product.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("carried product conclusion must survive = %+v", product.Effective)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (the development gate must not have closed)", snap.State.Phase)
		}
	})
}

// TestProductOnlyRedoCarriesTheReadinessConclusion（P1-2 完整正向）：只勾选
// 重走 product-review——产品审 PENDING 期间路线拒绝（门关）；产品审在
// rev-2 重过 PASS 后，携用的 readiness PASS（绑 rev-1、无 void、零新轮次）
// 直接满足双审门，路线接纳并推进 DEVELOPMENT_PARALLEL。若新产品审阻塞，
// 门自然关门（由 FAIL 态承担，无需级联失效）。
func TestProductOnlyRedoCarriesTheReadinessConclusion(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, dir := reviewEngine(t, cd, reg)
	productRound := walkToTopologyAndRoute(t, engine)

	askAndDecideRequirement(t, engine, "req-change-product-only", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-product",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "only the product walk is affected",
	})
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-pending-1", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}}); err == nil {
		t.Fatal("route accepted while the redone product series is PENDING")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("pending-product gate code = %q", code)
	}

	// 产品审在 rev-2 重新 PASS（pre-floor 旧派发按声明终止，替换派发完成）：
	// 携用的 readiness 不被级联失效、不耗新轮次，双审门直接重开。
	freshRoundOnTheReplacement(t, engine, productRound, "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
	snap := engine.LoadMustSucceed(t)
	if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PASS" || readiness.UpstreamVoid || readiness.Completed != 1 || readiness.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the carried readiness must open the gate without a fresh round: %+v void=%v completed=%d", readiness.Effective, readiness.UpstreamVoid, readiness.Completed)
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-route-reopened", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" || snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("route = %q phase = %s, want full/DEVELOPMENT_PARALLEL", snap.State.RouteMode, snap.State.Phase)
	}

}

// TestCarryValidityBindsToTheConfirmedLedger：结论 carry 有效性按确认账本
// 成员判定——账本外 revision 字符串上的 effective PASS 不开门（正控制：
// 账本内 revision 的 PASS 有效且顺序守卫放行）。
func TestCarryValidityBindsToTheConfirmedLedger(t *testing.T) {
	build := func(revision string) *State {
		view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
		if err != nil {
			t.Fatalf("new view: %v", err)
		}
		state := NewState(*view)
		state.RunID = "run-carry"
		state.IntakeConfirmationReceipt = &IntakeConfirmationReceipt{RequirementRevision: "rev-1"}
		set := review.NewSet()
		set, _, err = review.RecordCandidate(set, "run-carry", review.ProductReview,
			review.Dispatch{ID: "dispatch-" + revision, RequirementRevision: revision},
			review.Result{Status: "PASS", Message: "bound to " + revision}, reviewVerification())
		if err != nil {
			t.Fatalf("record candidate at %s: %v", revision, err)
		}
		state.ReviewSeries = set
		return state
	}
	phantom := build("rev-phantom")
	if phantom.effectivePass(phantom.ReviewSeries.ProductReview) {
		t.Fatal("a PASS bound outside the confirmed ledger must not be a valid conclusion")
	}
	if _, err := phantom.admitReviewEvidence(&ReviewResultEvidence{
		Action: review.StartReadiness, DispatchID: "dispatch-readiness-1", RequirementRevision: "rev-1",
		Status: "PASS", Verification: reviewVerification(),
	}, 0); err == nil {
		t.Fatal("the sequence guard opened behind an out-of-ledger PASS")
	} else if code := rejectionCode(t, err); code != CodeReviewSequenceGate {
		t.Fatalf("out-of-ledger guard code = %q", code)
	}

	carried := build("rev-1")
	if !carried.effectivePass(carried.ReviewSeries.ProductReview) {
		t.Fatal("a PASS bound to a confirmed revision must stay valid")
	}
	if _, err := carried.admitReviewEvidence(&ReviewResultEvidence{
		Action: review.StartReadiness, DispatchID: "dispatch-readiness-1", RequirementRevision: "rev-1",
		Status: "PASS", Verification: reviewVerification(),
	}, 0); err != nil {
		t.Fatalf("the sequence guard must admit behind a confirmed-revision PASS: %v", err)
	}
}

// TestForgedCarriedPassRejectedAtLoad：在账本内 revision 上伪造 effective
// PASS（实际投影为 FAIL）——证据链断裂由加载边界的既有重投影校验拦截。
func TestForgedCarriedPassRejectedAtLoad(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	}))
	original := stateBytesOf(t, dir)
	tamperStateContent(t, dir, func(content map[string]any) {
		series := content["reviewSeries"].(map[string]any)
		product := series["productReview"].(map[string]any)
		product["effectiveReviewStatus"].(map[string]any)["status"] = "PASS"
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("a forged effective PASS loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("forged PASS code = %q", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}
}

// TestRequirementChangeChoiceAnchoring：choice id 封闭为 proceed/abort——
// abort 不得携载荷、proceed 必携载荷、选项集内的外来 id 仍拒（与
// REVIEW_ROUND_GRANT 的 grant/decline 同理的固定项锚定）；被拒决定不解
// 决 Ask。
func TestRequirementChangeChoiceAnchoring(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	valid := RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-anchor",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "anchoring probe",
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-anchor-abort", "abort", &valid); err == nil {
		t.Fatal("abort with a payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("abort-with-payload code = %q", code)
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-anchor-proceed", "proceed", nil); err == nil {
		t.Fatal("payload-less proceed accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("payload-less proceed code = %q", code)
	}
	// 选项集由请求方枚举：集合内的外来 choice id（defer）仍被封闭集拒绝。
	fp := fingerprintOf(t, engine)
	request, err := NewRequirementRequestEvent("req-anchor-defer", valid.packageView(), AskOption{ID: "defer", Label: "推迟"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness("req-anchor-defer")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(requirementDecideEvent(t, "req-anchor-defer", token, "defer", &valid), fp); err == nil {
		t.Fatal("foreign choice id accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid || !strings.Contains(err.Error(), "closed set") {
		t.Fatalf("foreign choice code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("rejected anchoring decide changed state bytes")
	}
	snap := engine.LoadMustSucceed(t)
	for _, id := range []string{"req-anchor-abort", "req-anchor-proceed", "req-anchor-defer"} {
		if ask, ok := snap.State.PendingAsks[id]; !ok || ask.Resolved {
			t.Fatalf("a rejected decide must leave ask %s pending: %+v", id, ask)
		}
	}
}

// TestCarriedConclusionFindingsRemainCorrectable：需求修订后，续用结论
// （绑旧 revision）上的 finding 处置纠正仍可行——纠正的 stale 门按 series
// 当前结论绑定的 revision 判定，不按当前权威；CAS 与精确绑定照旧。
func TestCarriedConclusionFindingsRemainCorrectable(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "start.readiness.two")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "FAIL", "advisory", []review.Finding{
		{Severity: "P2", Message: "advisory concern on the carried conclusion"},
	}))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality"},
	}})

	// 只重走产品审：readiness 携用（绑 rev-1、带一条建议项）。
	askAndDecideRequirement(t, engine, "req-change-carry", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-carry",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "product meaning changed again",
	})
	snap := engine.LoadMustSucceed(t)
	carried := snap.State.ReviewSeries.StartReadiness
	if carried.Effective.Status != "PASS" || len(carried.Effective.AdvisoryFindingIDs) != 1 || carried.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("carried readiness = %+v", carried.Effective)
	}
	candidate := carried.RawCandidates[0]

	// CAS 负控照旧：过期 ExpectedEffectiveRevision 拒绝且零状态。
	staleCAS := ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.StartReadiness, DispatchID: "dispatch-readiness-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Reason: "stale CAS probe", ExpectedEffectiveRevision: carried.Effective.Revision + 5,
	}}
	dismissOption := AskOption{ID: AskOptionID("dismiss:" + candidate.Findings[0].ID), Label: "dismiss"}
	if err := askAndDecideRejecting(t, engine, dir, ControlReviewFindingDecision, "req-dismiss-stale", []AskOption{dismissOption}, dismissOption.ID, &staleCAS); err == nil {
		t.Fatal("stale CAS on a carried conclusion accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("stale CAS code = %q", code)
	}

	// 正控：携用结论上的 finding 处置纠正被纳——stale 门按绑定 revision
	//（rev-1）判定而非当前权威（rev-2）。
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-dismiss-carried", []AskOption{dismissOption}, dismissOption.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.StartReadiness, DispatchID: "dispatch-readiness-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Reason: "user rejects the advisory on the carried conclusion", ExpectedEffectiveRevision: carried.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	after := snap.State.ReviewSeries.StartReadiness
	if after.Effective.Status != "PASS" || len(after.Effective.AdvisoryFindingIDs) != 0 || len(after.Adjudications) != 1 || after.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("corrected carried readiness = %+v adjudications=%d", after.Effective, len(after.Adjudications))
	}
}

// TestFindingDrivenSourceMustMatchTheEffectiveDecision：来源标签必须对照
// finding 的有效裁决——已确认阻塞（CONFIRM/RESEVERITIZE 且有效严重度
// P0/P1）只能走 REVIEW_FINDING_P1，非阻塞 finding 只能走 REVIEW_FINDING_P2；
// 不符零状态拒绝（升级权经真实标签保留）。
func TestFindingDrivenSourceMustMatchTheEffectiveDecision(t *testing.T) {
	newEngineWithFinding := func(t *testing.T, severity string) (*Engine, string, string) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
			{Severity: severity, Message: "finding under adjudication"},
		}))
		confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
		snap := engine.LoadMustSucceed(t)
		candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
		askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-label", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: review.ProductReview, DispatchID: "dispatch-product-1", RequirementRevision: "rev-1",
			ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
			Decision: "CONFIRM", Reason: "user confirms the disposition", ExpectedEffectiveRevision: 1,
		}})
		return engine, dir, candidate.Findings[0].Digest
	}

	t.Run("confirmed P1 cannot be labeled REVIEW_FINDING_P2", func(t *testing.T) {
		engine, dir, digest := newEngineWithFinding(t, "P1")
		for _, classification := range []string{RequirementClassificationPreserved, RequirementClassificationChanged} {
			payload := &RequirementDecisionPayload{
				BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-label",
				ImpactDigest: "sha256:impact",
				Source:       RequirementSourceReviewFindingP2, Classification: classification,
				RedoProductReview: classification == RequirementClassificationChanged,
				Reason:            "mislabeled source", FindingDigest: digest,
			}
			if err := askAndDecideRequirementRejecting(t, engine, dir, "req-label-p2-"+classification, "proceed", payload); err == nil {
				t.Fatalf("P2 label on a confirmed blocker accepted (%s)", classification)
			} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
				t.Fatalf("P2 label code = %q (%s)", code, classification)
			}
		}
	})

	t.Run("confirmed advisory cannot be labeled REVIEW_FINDING_P1", func(t *testing.T) {
		engine, dir, digest := newEngineWithFinding(t, "P2")
		payload := &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-label",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "inflated source", FindingDigest: digest,
		}
		if err := askAndDecideRequirementRejecting(t, engine, dir, "req-label-p1", "proceed", payload); err == nil {
			t.Fatal("P1 label on a confirmed advisory accepted")
		} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
			t.Fatalf("P1 label code = %q", code)
		}
	})
}

// TestRequirementChangeDisposesInFlightEvidenceByScope：需求变化登记的同
// 一事务内，处置只认用户勾选的重走范围——重走 series 上已声明身份的暂
// 存证据按声明作废（派发退位、结果留迟到证据账、同任务重签发，替换派
// 发完成新鲜轮）；未勾选 series 的暂存一律保留（迟到回执照常配对或由
// 顺序守卫按前提状态判定），PRESERVED 的旧 revision 暂存携原 revision
// carry 完成。
func TestRequirementChangeDisposesInFlightEvidenceByScope(t *testing.T) {
	t.Run("redone series staging is terminated and the replacement completes", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		staged, err := NewReviewWorkerResultEvent("result-staged-old", product, testProvider, "sha256:payload-staged-old", reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
			t.Fatalf("stage before the change: %v %+v", err, acceptance)
		}

		askAndDecideRequirement(t, engine, "req-change-staging", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-staging",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "the product review is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.StagedResults) != 0 {
			t.Fatalf("in-scope staging must be disposed: %+v", snap.State.StagedResults)
		}
		replacement := snap.State.ObsoleteActions[product].ReplacedBy
		if replacement == "" {
			t.Fatalf("the terminated dispatch must be replaced: %+v", snap.State.ObsoleteActions[product])
		}
		if _, voided := snap.State.ObsoleteResults[product]; !voided {
			t.Fatalf("the terminated staging must be kept as late evidence: %+v", snap.State.ObsoleteResults)
		}
		terminated := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "terminated by declaration") {
				terminated = true
			}
		}
		if !terminated {
			t.Fatalf("no termination trail for %s: %+v", product, snap.State.RecoveryRecords)
		}

		// 替换派发（≥floor）完成新鲜轮：completed=1、effective 绑 rev-2。
		if _, err := engine.Submit(spawnReceipt(t, "spawn-fresh-staging", replacement, "agent-fresh-staging"), fp); err != nil {
			t.Fatalf("replacement spawn: %v", err)
		}
		fresh, err := NewReviewWorkerResultEvent("result-fresh", replacement, testProvider, "sha256:payload-fresh", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(fresh, fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("fresh result on the replacement: %v %+v", err, acceptance)
		}
		snap = engine.LoadMustSucceed(t)
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("fresh result did not reach the ledger: completed=%d %+v", series.Completed, series.Effective)
		}
	})

	t.Run("out-of-scope staging is retained", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "start.readiness.two")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		staged, err := NewReviewWorkerResultEvent("result-staged-kept", readiness, testProvider, "sha256:payload-staged-kept", reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if _, err := engine.Submit(staged, fp); err != nil {
			t.Fatalf("stage: %v", err)
		}

		// 只重走产品审：readiness 不在 scope 内即续用——暂存保留，不因
		// revision 绑定被清除（处置只认用户勾选的重走范围）。
		askAndDecideRequirement(t, engine, "req-change-kept", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-kept",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "only the product review is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if _, kept := snap.State.StagedResults[readiness]; !kept {
			t.Fatalf("out-of-scope staging must be retained for its late receipt: %+v", snap.State.StagedResults)
		}
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == readiness {
				t.Fatalf("retained staging must not carry a disposal trail: %+v", record)
			}
		}
		if _, voided := snap.State.ObsoleteResults[readiness]; voided {
			t.Fatal("retained staging must not be voided")
		}
		// 迟到回执是无条件观察事实：产品审重走期间顺序门未开——回执
		// ACCEPTED 落账、配对挂起、暂存与派发槽原样保留（产品审重新
		// PASS 的事务自动补配对，见
		// TestLateReceiptPairsWhenTheProductGateReopens）。
		if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-kept-late", readiness, "agent-kept-late"), fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("late receipt during the redo = %v %+v", err, acceptance)
		}
		snap = engine.LoadMustSucceed(t)
		if _, kept := snap.State.StagedResults[readiness]; !kept {
			t.Fatalf("the suspended pairing must leave the staging intact: %+v", snap.State.StagedResults)
		}
		if _, waiting := snap.State.PendingActions[readiness]; !waiting {
			t.Fatal("the readiness dispatch slot must stay intact for its fresh round")
		}
		if _, admitted := snap.State.Results[readiness]; admitted {
			t.Fatal("the suspended round must not reach the results ledger yet")
		}
		suspended := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == readiness && strings.Contains(record.Detail, "stays pending until the product-review conclusion re-passes") {
				suspended = true
			}
		}
		if !suspended {
			t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
		}
	})

	t.Run("a preserved change carries the old-revision staging to completion", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		staged, err := NewReviewWorkerResultEvent("result-staged-preserved", product, testProvider, "sha256:payload-staged-preserved", reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if _, err := engine.Submit(staged, fp); err != nil {
			t.Fatalf("stage: %v", err)
		}

		// PRESERVED 修订：未勾重走即续用——旧 revision 暂存保留，迟到回
		// 执按 carry 语义携原 revision 配对完成（completed+1）。
		askAndDecideRequirement(t, engine, "req-change-preserved-staging", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-preserved-staging",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
			Reason: "wording only",
		})
		snap := engine.LoadMustSucceed(t)
		if _, kept := snap.State.StagedResults[product]; !kept {
			t.Fatalf("a preserved change must retain the old-revision staging: %+v", snap.State.StagedResults)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-preserved-late", product, "agent-preserved-late"), fp); err != nil {
			t.Fatalf("late spawn receipt: %v", err)
		}
		snap = engine.LoadMustSucceed(t)
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("the carried staging did not complete on its late receipt: completed=%d %+v", series.Completed, series.Effective)
		}
		if _, admitted := snap.State.Results[product]; !admitted {
			t.Fatal("the carried staging did not reach the results ledger")
		}
		// 新 revision 的新鲜轮仍可走（重用同一 action 的新派发槽不可得——
		// 任务已完成；由后续派发承载，语义已由上一用例覆盖）。
	})
}

// TestRequirementPackageBindsTheAsk：Ask 持久化用户确认的变更包，DECIDE 载
// 荷必须与之逐字段相等——同一 Ask 不可登记单字段之差的另一个包（冲突拒
// 绝零状态）；请求缺包在构造层即拒。
func TestRequirementPackageBindsTheAsk(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	confirmed := RequirementDecisionPackage{
		CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-package",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
	}
	request, err := NewRequirementRequestEvent("req-package", confirmed, requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness("req-package")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	base := RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-package",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "matches the confirmed package",
	}
	for name, mutate := range map[string]func(*RequirementDecisionPayload){
		"candidate revision": func(p *RequirementDecisionPayload) { p.CandidateRevision = "rev-3" },
		"delta digest":       func(p *RequirementDecisionPayload) { p.DeltaDigest = "sha256:other-delta" },
		"classification":     func(p *RequirementDecisionPayload) { p.Classification = RequirementClassificationChanged },
		"redo scope":         func(p *RequirementDecisionPayload) { p.RedoTopology = true },
	} {
		diverging := base
		mutate(&diverging)
		ev, err := NewRequirementDecideEvent(EventID("evt-package-"+name), "req-package", token, "proceed", diverging)
		if err != nil {
			t.Fatalf("%s event: %v", name, err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatalf("%s divergence from the confirmed package accepted", name)
		} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected || !strings.Contains(err.Error(), "package confirmed on ask") {
			t.Fatalf("%s divergence code = %q (%v)", name, code, err)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatalf("%s divergence changed state bytes", name)
		}
	}
	// 载荷与包逐字段相等时通过，登记照常。
	askAndDecideRequirement(t, engine, "req-package", "proceed", &base)
	snap := engine.LoadMustSucceed(t)
	if entry := snap.State.RequirementRevisions[len(snap.State.RequirementRevisions)-1]; entry.Revision != "rev-2" || entry.Classification != RequirementClassificationPreserved || len(entry.DecidedScope) != 0 {
		t.Fatalf("package-bound registration = %+v", entry)
	}

	// 请求缺包在构造层拒绝。
	if _, err := NewRequestEvent("req-no-package", ControlRequestRequirementChange, requirementOptions()...); err == nil {
		t.Fatal("package-less requirement request constructed")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("package-less request code = %q", code)
	}
	// 非需求控制不得携包。
	misplaced := Event{ID: "req-misplaced", Kind: KindRequestControl, Request: &RequestPayload{Control: ControlReset, Options: requirementOptions(), Package: &confirmed}}
	if err := misplaced.Validate(); err == nil {
		t.Fatal("a non-requirement request carrying a package validated")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("misplaced package code = %q", code)
	}
}

// TestPreservedScopeIsRecordedAndExecuted：PRESERVED＋scope 是合法组合
// （§7.2.1 保留产品含义＋重做受影响技术审）——账本如实落 DecidedScope，
// 勾选的 series 真实重走（stale→PENDING），未勾选的续用；加载边界对
// PRESERVED 带 scope 的账目不拒。
func TestPreservedScopeIsRecordedAndExecuted(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	nextAction := walkToTopologyAndRoute(t, engine)

	askAndDecideRequirement(t, engine, "req-preserved-scope", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-preserved-scope",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		RedoStartReadiness: true, Reason: "product meaning preserved; the affected technical review is redone",
	})
	snap := engine.LoadMustSucceed(t)
	entry := snap.State.RequirementRevisions[len(snap.State.RequirementRevisions)-1]
	if entry.Classification != RequirementClassificationPreserved || len(entry.DecidedScope) != 1 || entry.DecidedScope[0] != "start-readiness" {
		t.Fatalf("preserved scope entry = %+v", entry)
	}
	if product := snap.State.ReviewSeries.ProductReview; product.Completed != 1 || product.Effective.Status != "PASS" || product.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the unredone product series must be carried: %+v completed=%d", product.Effective, product.Completed)
	}
	readiness := snap.State.ReviewSeries.StartReadiness
	if readiness.Effective.Status != "PENDING" || !strings.HasPrefix(readiness.Effective.Source, "requirement-change:") || readiness.Completed != 1 {
		t.Fatalf("the redone readiness series = %+v completed=%d", readiness.Effective, readiness.Completed)
	}
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase = %s, want TOPOLOGY_AND_ROUTE (门因 readiness 重走关闭)", snap.State.Phase)
	}
	// 重走完成：readiness 在 rev-2 重新 PASS（pre-floor 旧派发按声明终止，
	// 替换派发完成新鲜轮），门重新打开（路线接纳）。
	freshRoundOnTheReplacement(t, engine, nextAction, "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
	askAndDecide(t, engine, ControlSelectRoute, "req-route-after-preserved-scope", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
	// PRESERVED 带 scope 的账目经加载边界完整接纳（完整性校验不拒）。
	if _, err := engine.Load(); err != nil {
		t.Fatalf("ledger with a preserved scope failed to load: %v", err)
	}
}

// TestUnadjudicatedFindingCannotDriveAnySource：未裁决（raw）的 finding 不
// 能驱动任何修订来源——P1/P2 源都拒绝，先处置再发起修订。
func TestUnadjudicatedFindingCannotDriveAnySource(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "raw finding without a disposition"},
	}))
	snap := engine.LoadMustSucceed(t)
	digest := snap.State.ReviewSeries.ProductReview.RawCandidates[0].Findings[0].Digest

	for _, source := range []string{RequirementSourceReviewFindingP1, RequirementSourceReviewFindingP2} {
		payload := &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-raw",
			ImpactDigest: "sha256:impact",
			Source:       source, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "raw finding probe", FindingDigest: digest,
		}
		if err := askAndDecideRequirementRejecting(t, engine, dir, "req-raw-"+source, "proceed", payload); err == nil {
			t.Fatalf("an unadjudicated finding accepted as %s source", source)
		} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected || !strings.Contains(err.Error(), "adjudicate the disposition first") {
			t.Fatalf("unadjudicated %s code = %q (%v)", source, code, err)
		}
	}
}

// TestRedoneSeriesFreshRoundRequiresAPostDecisionDispatch：确认重走某审查
// series 后，重走前签发的旧派发不能完成"新鲜复审"——其证据按声明终止
// （OBSOLETE_DISPATCH＋迟到证据账＋同任务重签发），替换派发（签发代数
// ≥floor）上的同证据才完成新鲜轮。步为语义判断步（不带签发标记——系
// 列身份由证据声明，标记缺失派发的 floor＋按声明终止兜底路径；已标记
// 路径见 TestMarkedReviewDispatchIsTerminatedByTheRedoDecision）。
func TestRedoneSeriesFreshRoundRequiresAPostDecisionDispatch(t *testing.T) {
	cd, reg := declaredSeriesStepsFixture(t, "review.floor.one", "review.floor.two", "review.floor.three")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]

	askAndDecideRequirement(t, engine, "req-redo-floor", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-floor",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "redo the product review",
	})
	snap := engine.LoadMustSucceed(t)
	if _, pending := snap.State.PendingActions[product]; !pending {
		t.Fatal("setup: the in-flight action disappeared before any result")
	}
	floor := snap.State.RequirementRevisions[len(snap.State.RequirementRevisions)-1].StateRevision
	if snap.State.ReviewSeries.ProductReview.FreshFloor != floor {
		t.Fatalf("fresh floor = %d, want the decide revision %d", snap.State.ReviewSeries.ProductReview.FreshFloor, floor)
	}
	// 旧派发（pre-floor）即使绑新 revision 也不纳：按声明终止＋重签发。
	freshRoundAfterDeclaredTermination(t, engine, product, "result-floor-fresh", reviewEvidenceAt(review.ProductReview, "dispatch-floor-2", "rev-2", "PASS", "old dispatch completing the fresh round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("the fresh round must complete exactly once on the replacement: completed=%d %+v", series.Completed, series.Effective)
	}
	if _, voided := snap.State.ObsoleteResults[product]; !voided {
		t.Fatal("the terminated dispatch's evidence must be kept as late evidence")
	}
}

// TestUnredoneSeriesQuarantinedResultIsCarried：未勾重走的 series，其屏障
// 期隔离结果按 carry 恢复——携原 revision、原证据字节接纳完成（completed
// +1），留 carry 留痕，不作废。
func TestUnredoneSeriesQuarantinedResultIsCarried(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	readiness := func() string {
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-carry-product", product, reviewEvidence(review.ProductReview, "dispatch-carry-product", "", "PASS", "clean", nil))
		return pendingActionID(t, engine)
	}()
	fp := fingerprintOf(t, engine)

	preserved := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-carry",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "meaning preserved, nothing redone",
	}
	request, err := NewRequirementRequestEvent("req-carry", preserved.packageView(), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-carry", readiness, "agent-carry"), fp); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-carry", readiness, testProvider, "sha256:payload-carry",
		reviewEvidence(review.StartReadiness, "dispatch-carry-readiness", "", "PASS", "in-flight against rev-1", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, fp); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine = %+v %v", acceptance, err)
	}
	askAndDecideRequirement(t, engine, "req-carry", "proceed", preserved)
	snap := engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the quarantined result was not carried to admission: completed=%d %+v", series.Completed, series.Effective)
	}
	if _, admitted := snap.State.Results[readiness]; !admitted {
		t.Fatal("the carried result did not reach the results ledger")
	}
	if _, voided := snap.State.ObsoleteResults[readiness]; voided {
		t.Fatal("a carried result must not be voided")
	}
	carried := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == readiness && strings.Contains(record.Detail, "carried by requirement decision req-carry") {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("no carry trail: %+v", snap.State.RecoveryRecords)
	}
}

// TestQuarantineHeldUntilTheLastPendingAskResolves：多个需求 Ask 并存时，
// 屏障只随最后一个未决 Ask 的解决解除——此前隔离账继续持有（留痕），任
// 何单个决定都不提前重放。
func TestQuarantineHeldUntilTheLastPendingAskResolves(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	fp := fingerprintOf(t, engine)

	for _, requestID := range []string{"req-held-first", "req-held-second"} {
		request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request %s: %v", requestID, err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-held", product, "agent-held"), fp); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-held", product, testProvider, "sha256:payload-held",
		reviewEvidence(review.ProductReview, "dispatch-held", "", "PASS", "quarantined while two asks pend", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, fp); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine = %+v %v", acceptance, err)
	}
	abortExistingAsk(t, engine, "req-held-first")
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.QuarantinedResults) != 1 {
		t.Fatalf("the quarantine ledger must stay held while another ask is pending: %+v", snap.State.QuarantinedResults)
	}
	if snap.State.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatalf("replay must not run before the last ask resolves: completed=%d", snap.State.ReviewSeries.ProductReview.Completed)
	}
	held := false
	for _, record := range snap.State.RecoveryRecords {
		if strings.Contains(record.Detail, "quarantine ledger stays held") {
			held = true
		}
	}
	if !held {
		t.Fatalf("no hold trail: %+v", snap.State.RecoveryRecords)
	}
	// 最后一个 Ask 解决：同一事务内重放完成。
	abortExistingAsk(t, engine, "req-held-second")
	snap = engine.LoadMustSucceed(t)
	if len(snap.State.QuarantinedResults) != 0 || snap.State.ReviewSeries.ProductReview.Completed != 1 {
		t.Fatalf("the last decide must replay the quarantine: held=%d completed=%d",
			len(snap.State.QuarantinedResults), snap.State.ReviewSeries.ProductReview.Completed)
	}
}

// TestDualAskRedoDefersTheReplacementUntilTheBarrierLifts：重走决定落在
// 双 Ask 屏障期时，被终止派发的替换不即签——屏障期内账面不出现任何新
// 派发身份（无 retry: 条目），退位条目携延迟标记与留痕；最后一个 Ask
// 解决的同一事务里补发替换，补发派发的签发代数不早于重走新鲜底线。
func TestDualAskRedoDefersTheReplacementUntilTheBarrierLifts(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	fp := fingerprintOf(t, engine)

	redoPayload := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-defer",
		ImpactDigest: "sha256:impact", Source: RequirementSourceUserInitiated,
		Classification: RequirementClassificationChanged, RedoProductReview: true, Reason: "redo the product review",
	}
	for _, requestID := range []string{"req-defer-first", "req-defer-second"} {
		request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(redoPayload), requirementOptions()...)
		if err != nil {
			t.Fatalf("request %s: %v", requestID, err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
	}
	token, err := engine.Freshness("req-defer-first")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if _, err := engine.Submit(requirementDecideEvent(t, "req-defer-first", token, "proceed", redoPayload), fp); err != nil {
		t.Fatalf("decide within the barrier: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if _, stillPending := snap.State.PendingActions[product]; stillPending {
		t.Fatal("the redone dispatch must be retired by the decision")
	}
	for actionID := range snap.State.PendingActions {
		if strings.HasPrefix(actionID, "retry:") {
			t.Fatalf("a replacement identity appeared while the barrier held: %s", actionID)
		}
	}
	obsolete, known := snap.State.ObsoleteActions[product]
	if !known || obsolete.Deferred == nil {
		t.Fatalf("the retired dispatch must carry a deferred replacement: %+v", obsolete)
	}
	if obsolete.ReplacedBy != "" {
		t.Fatalf("no replacement may be named while the barrier holds: %s", obsolete.ReplacedBy)
	}
	deferred := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == product && strings.Contains(record.Detail, "deferred until the barrier lifts") {
			deferred = true
		}
	}
	if !deferred {
		t.Fatalf("no deferral trail: %+v", snap.State.RecoveryRecords)
	}

	// 最后一个 Ask 解决：同一事务补发替换。
	abortExistingAsk(t, engine, "req-defer-second")
	snap = engine.LoadMustSucceed(t)
	obsolete = snap.State.ObsoleteActions[product]
	if obsolete.Deferred != nil || !strings.HasPrefix(obsolete.ReplacedBy, "retry:") {
		t.Fatalf("the lift must install the deferred replacement: %+v", obsolete)
	}
	if _, replaced := snap.State.PendingActions[obsolete.ReplacedBy]; !replaced {
		t.Fatal("the deferred replacement must be a pending dispatch after the lift")
	}
	series, _ := snap.State.ReviewSeries.Series(review.ProductReview)
	if issued := snap.State.DispatchIssueRevisions[obsolete.ReplacedBy]; issued < series.FreshFloor {
		t.Fatalf("the deferred replacement predates the fresh floor: issued=%d floor=%d", issued, series.FreshFloor)
	}
	issued := false
	for _, record := range snap.State.RecoveryRecords {
		if strings.Contains(record.Detail, "after the barrier lifted") {
			issued = true
		}
	}
	if !issued {
		t.Fatalf("no post-lift issuance trail: %+v", snap.State.RecoveryRecords)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load after the lift: %v", err)
	}
}

// TestRequirementAskExpiresWhenItsBaseRevisionIsSuperseded：Ask 落账创建时
// 绑定权威基线；后续确认使基线前移后，旧 Ask 的决定（即使把载荷基线改
// 成新权威）以可区分错误拒绝且零状态——必须在新基线上重建 Ask。
func TestRequirementAskExpiresWhenItsBaseRevisionIsSuperseded(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, dir := reviewEngine(t, cd, reg)
	issueReady(t, engine)

	// 两个 Ask 都在 rev-1 创建；先确认第一个（→rev-2），第二个的基线随
	// 之过期。
	first := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-expire-first",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "first",
	}
	secondPackage := requirementPackageOf(&RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-3", DeltaDigest: "sha256:delta-expire-second",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "authored on the superseded base",
	})
	fp := fingerprintOf(t, engine)
	secondRequest, err := NewRequirementRequestEvent("req-expire-second", secondPackage, requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(secondRequest, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	askAndDecideRequirement(t, engine, "req-expire-first", "proceed", first)
	// 把载荷基线改成新权威也过不了三方相等：Ask 本身绑的还是 rev-1。
	token, err := engine.Freshness("req-expire-second")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	rebasedEvent := requirementDecideEvent(t, "req-expire-second", token, "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-2", CandidateRevision: "rev-3", DeltaDigest: "sha256:delta-expire-second",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "rebased onto the newer baseline",
	})
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(rebasedEvent, fp); err == nil {
		t.Fatal("a stale-based ask was decided")
	} else if code := rejectionCode(t, err); code != CodeRequirementAskExpired {
		t.Fatalf("expired ask code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("the expired decide changed state bytes")
	}
	if tail := engine.LoadMustSucceed(t).State.RequirementRevisions; len(tail) != 2 || tail[1].Revision != "rev-2" {
		t.Fatalf("the expired decide must not register: %+v", tail)
	}
	// 新基线上重建的 Ask 照常确认。
	askAndDecideRequirement(t, engine, "req-expire-rebuilt", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-2", CandidateRevision: "rev-3", DeltaDigest: "sha256:delta-expire-rebuilt",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "rebuilt on the current base",
	})
	if tail := engine.LoadMustSucceed(t).State.RequirementRevisions; len(tail) != 3 || tail[2].Revision != "rev-3" {
		t.Fatalf("the rebuilt ask did not register: %+v", tail)
	}
}

// TestTerminalTaskQuarantinedResultReconcilesOnReplay：任务在屏障期内到
// terminal 后，隔离记录的接纳走 terminal 对账路径——回执（SPAWNED）证明
// 派发真实在途过，结果入 Results＋审查证据记账＋携决定 requestID 的对账
// 留痕；不产生 obsolete 条目，账本一致，state 照常加载。
func TestTerminalTaskQuarantinedResultReconcilesOnReplay(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	snap := engine.LoadMustSucceed(t)
	task := snap.State.PendingActions[product].Task
	attempt := snap.State.PendingActions[product].AttemptID
	fp := fingerprintOf(t, engine)

	request, err := NewRequirementRequestEvent("req-replay-terminal", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-replay-terminal", product, "agent-replay-terminal"), fp); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-replay-terminal", product, testProvider, "sha256:payload-replay-terminal",
		reviewEvidence(review.ProductReview, "dispatch-replay-terminal", "", "PASS", "quarantined then the task reports terminal", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, fp); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine: %+v %v", acceptance, err)
	}
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID("task-replay-terminal-"+string(rune('a'+i))), task, attempt, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fp); err != nil {
			t.Fatalf("task progress %s during barrier: %v", status, err)
		}
	}
	abortExistingAsk(t, engine, "req-replay-terminal")
	snap = engine.LoadMustSucceed(t)
	if completed := snap.State.ReviewSeries.ProductReview.Completed; completed != 1 {
		t.Fatalf("the replayed result must record its review evidence: completed=%d", completed)
	}
	if _, inResults := snap.State.Results[product]; !inResults {
		t.Fatal("the replayed result must be admitted to Results")
	}
	if _, obsolete := snap.State.ObsoleteResults[product]; obsolete {
		t.Fatal("the reconciled result must not land in ObsoleteResults")
	}
	reconciled := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == product && strings.Contains(record.Detail, "reconciled against the terminal task by requirement decision req-replay-terminal") {
			reconciled = true
		}
	}
	if !reconciled {
		t.Fatalf("no reconciliation trail: %+v", snap.State.RecoveryRecords)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load after the reconciled replay: %v", err)
	}
}

// TestQuarantinedResultIdentityIgnoresTheEventID：隔离账与重放后的结果账
// 都按结果级摘要（不含事件 ID）记身份——同一载荷换事件 ID 重发判
// DUPLICATE，不误报 RECEIPT_CONFLICT。
func TestQuarantinedResultIdentityIgnoresTheEventID(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	fp := fingerprintOf(t, engine)

	request, err := NewRequirementRequestEvent("req-digest-identity", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-digest", product, "agent-digest"), fp); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	first, err := NewReviewWorkerResultEvent("evt-digest-first", product, testProvider, "sha256:payload-digest",
		reviewEvidence(review.ProductReview, "dispatch-digest", "", "PASS", "digest identity", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(first, fp); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine: %+v %v", acceptance, err)
	}
	abortExistingAsk(t, engine, "req-digest-identity")
	snap := engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Completed != 1 {
		t.Fatalf("the replayed result did not complete: completed=%d", snap.State.ReviewSeries.ProductReview.Completed)
	}
	replay, err := NewReviewWorkerResultEvent("evt-digest-second", product, testProvider, "sha256:payload-digest",
		reviewEvidence(review.ProductReview, "dispatch-digest", "", "PASS", "digest identity", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	acceptance, err := engine.Submit(replay, fingerprintOf(t, engine))
	if err != nil || acceptance.Status != "DUPLICATE" {
		t.Fatalf("same payload under a new event id = %v %+v (want DUPLICATE)", err, acceptance)
	}
}

// TestForgedFreshFloorRejectedAtLoad：series 的 fresh floor 只认确认账本
// （覆盖该 series 的最后一次决定的 state 代数）——手改 floor 在加载边界
// 可区分拒绝，恢复原状即照常加载。
func TestForgedFreshFloorRejectedAtLoad(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	issueReady(t, engine)
	askAndDecideRequirement(t, engine, "req-floor-tamper", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-floor-tamper",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "set the fresh floor",
	})
	original := stateBytesOf(t, dir)
	tamperStateContent(t, dir, func(content map[string]any) {
		series := content["reviewSeries"].(map[string]any)
		product := series["productReview"].(map[string]any)
		product["freshFloor"] = float64(1)
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("a forged fresh floor loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("forged floor code = %q", code)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}
}

// TestReviewSeriesVocabularyMatchesTheReviewKernel：authoring 的两条开发
// 前系列标记词汇是 review 内核 Action 持久化值的镜像（authoring 不得
// import review——review 经 encoder 依赖 authoring），两包词汇相等由本
// 测试锁死；非开发前系列标记没有内核对应物（它永不进入审查账本）。
func TestReviewSeriesVocabularyMatchesTheReviewKernel(t *testing.T) {
	if authoring.ReviewSeriesProductReview != authoring.ReviewSeriesAction(review.ProductReview) ||
		authoring.ReviewSeriesStartReadiness != authoring.ReviewSeriesAction(review.StartReadiness) {
		t.Fatalf("authoring marks %q/%q must equal the review kernel actions %q/%q",
			authoring.ReviewSeriesProductReview, authoring.ReviewSeriesStartReadiness, review.ProductReview, review.StartReadiness)
	}
}

// TestMarkedReviewDispatchIsTerminatedByTheRedoDecision：定义在签发时断言
// 审查系列身份的派发，重走决定事务内被主动终止——无需等任何证据到达：
// 旧派发退位（无结果可作废）、同任务替换派发（携标记、签发代数＝决定）
// 完成新鲜轮；未勾选系列的已标记派发不被终止。
func TestMarkedReviewDispatchIsTerminatedByTheRedoDecision(t *testing.T) {
	t.Run("marked dispatch of the redone series", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, _ := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]

		askAndDecideRequirement(t, engine, "req-marked-redo", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-marked",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "the product review is redone",
		})
		snap := engine.LoadMustSucceed(t)
		if _, stillPending := snap.State.PendingActions[product]; stillPending {
			t.Fatal("the marked dispatch was not terminated by the decide")
		}
		obsolete := snap.State.ObsoleteActions[product]
		if obsolete.ReplacedBy == "" {
			t.Fatalf("the terminated dispatch was not replaced: %+v", obsolete)
		}
		if len(snap.State.ObsoleteResults) != 0 {
			t.Fatalf("a dispatch without any result must not void results: %+v", snap.State.ObsoleteResults)
		}
		replacementPending, ok := snap.State.PendingActions[obsolete.ReplacedBy]
		if !ok || replacementPending.ReviewSeries != review.ProductReview {
			t.Fatalf("the replacement must inherit the series mark: %+v", replacementPending)
		}
		decide := snap.State.RequirementRevisions[len(snap.State.RequirementRevisions)-1]
		if issued := snap.State.DispatchIssueRevisions[obsolete.ReplacedBy]; issued != decide.StateRevision {
			t.Fatalf("the replacement must be issued at the decide (state revision %d): issued=%d", decide.StateRevision, issued)
		}
		terminated := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "terminated by the redo declared in requirement decision req-marked-redo") {
				terminated = true
			}
		}
		if !terminated {
			t.Fatalf("no termination trail: %+v", snap.State.RecoveryRecords)
		}
		// 替换派发（≥floor、携标记）完成新鲜轮。
		fresh, err := NewReviewWorkerResultEvent("result-marked-fresh", obsolete.ReplacedBy, testProvider, "sha256:payload-marked-fresh",
			reviewEvidenceAt(review.ProductReview, "dispatch-marked-2", "rev-2", "PASS", "fresh round on the marked replacement", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		fp := fingerprintOf(t, engine)
		if _, err := engine.Submit(spawnReceipt(t, "spawn-marked-fresh", obsolete.ReplacedBy, "agent-marked-fresh"), fp); err != nil {
			t.Fatalf("spawn: %v", err)
		}
		if acceptance, err := engine.Submit(fresh, fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("fresh round on the replacement: %v %+v", err, acceptance)
		}
		snap = engine.LoadMustSucceed(t)
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("fresh round on the marked replacement = completed %d %+v", series.Completed, series.Effective)
		}
	})

	t.Run("marked dispatch of an unchecked series stays in flight", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-unchecked-product", product, reviewEvidence(review.ProductReview, "dispatch-unchecked-product", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)

		askAndDecideRequirement(t, engine, "req-marked-unchecked", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-unchecked",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "only the product review is redone",
		})
		snap := engine.LoadMustSucceed(t)
		pending, ok := snap.State.PendingActions[readiness]
		if !ok || pending.ReviewSeries != review.StartReadiness {
			t.Fatalf("the unchecked marked dispatch must stay in flight with its mark: %+v", pending)
		}
		if _, obsolete := snap.State.ObsoleteActions[readiness]; obsolete {
			t.Fatal("the unchecked marked dispatch was terminated")
		}
	})
}

// TestMarkedDispatchRejectsMismatchedEvidence：已标记派发的审查证据必须声
// 明同一系列——签发标记是权威事实，声明他系列即可区分拒绝、零状态；同
// action 的相符证据随后照常接纳。
func TestMarkedDispatchRejectsMismatchedEvidence(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	fp := fingerprintOf(t, engine)

	if _, err := engine.Submit(spawnReceipt(t, "spawn-mismatch", product, "agent-mismatch"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mismatched, err := NewReviewWorkerResultEvent("result-mismatch", product, testProvider, "sha256:payload-mismatch",
		reviewEvidence(review.StartReadiness, "dispatch-mismatch", "", "PASS", "declares the wrong series", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(mismatched, fp); err == nil {
		t.Fatal("mismatched evidence accepted on a marked dispatch")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), "issued for the product-review") {
		t.Fatalf("mismatch code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("the mismatched evidence changed state bytes")
	}
	// 回执已配对：同 action 的相符证据直接完成。
	matched, err := NewReviewWorkerResultEvent("result-match", product, testProvider, "sha256:payload-match",
		reviewEvidence(review.ProductReview, "dispatch-match", "", "PASS", "declares the marked series", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(matched, fp); err != nil {
		t.Fatalf("matching evidence rejected: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("matching evidence on the marked dispatch = completed %d %+v", series.Completed, series.Effective)
	}
}

// TestUnaffiliatedDispatchRejectsPredevelopmentEvidence：声明非开发前系列
// 的派发不携带任何开发前审查系列身份——其上的产品审/技术审证据一律按
// 标记不一致可区分拒绝、零状态；开发前系列的重走决定也不终止它（不属
// 于重走范围）。
func TestUnaffiliatedDispatchRejectsPredevelopmentEvidence(t *testing.T) {
	cd, reg := agentStepsDefinition(t, func(string) authoring.AgentSpec {
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesUnaffiliated, Timeout: time.Minute,
		}
	}, "gate.check")
	engine, dir := reviewEngine(t, cd, reg)
	gate := issueReady(t, engine)[0]
	snap := engine.LoadMustSucceed(t)
	if mark := snap.State.PendingActions[gate].ReviewSeries; mark != review.Action(authoring.ReviewSeriesUnaffiliated) {
		t.Fatalf("the dispatch must carry the unaffiliated mark at issuance: %q", mark)
	}
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "spawn-unaffiliated", gate, "agent-unaffiliated"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	for name, action := range map[string]review.Action{
		"product-review":  review.ProductReview,
		"start-readiness": review.StartReadiness,
	} {
		mismatched, err := NewReviewWorkerResultEvent(EventID("result-unaffiliated-"+name), gate, testProvider, "sha256:payload-unaffiliated-"+name,
			reviewEvidence(action, "dispatch-unaffiliated-"+name, "", "PASS", "predevelopment evidence on an unaffiliated dispatch", nil))
		if err != nil {
			t.Fatalf("event %s: %v", name, err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(mismatched, fp); err == nil {
			t.Fatalf("%s evidence accepted on an unaffiliated dispatch", name)
		} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), "issued for the unaffiliated") {
			t.Fatalf("%s mismatch code = %q (%v)", name, code, err)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatalf("%s mismatched evidence changed state bytes", name)
		}
	}

	// 非开发前系列不属于重走范围：产品审重走决定不终止该派发。
	askAndDecideRequirement(t, engine, "req-unaffiliated-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-unaffiliated",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the product review is redone",
	})
	snap = engine.LoadMustSucceed(t)
	if pending, ok := snap.State.PendingActions[gate]; !ok || pending.ReviewSeries != review.Action(authoring.ReviewSeriesUnaffiliated) {
		t.Fatalf("the unaffiliated dispatch must stay in flight across a predevelopment redo: %+v", pending)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// parallelTailFixture 构造双审（canonical 两步带标记）后并列两个 agent
// 派发槽（就绪轮槽携技术审标记、产品新鲜轮槽携产品审标记）的定义：
// 挂起配对（回执＋暂存占一槽）与开门补配对（另一槽完成产品审新鲜轮）
// 需要两个同时在途的 action。
func parallelTailFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	reg := compiler.NewRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	must(reg.RegisterHandler("engine.test.prep", authoring.RunnerEngineLocal))
	must(reg.RegisterHandler("engine.test.reviewer", authoring.RunnerAgentWorker))
	must(reg.RegisterCodec("codec.any.in"))
	must(reg.RegisterCodec("codec.any.out"))
	must(reg.RegisterPredicate("pred.test.post"))
	header := func(id string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: "review", Dependencies: ds, DefinitionVersion: definition.Version}
	}
	io := func(from string) authoring.IO {
		return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
			Inputs:         []authoring.InputBinding{{From: authoring.StepID(from), OutputField: "out", ToField: "in"}},
			Postconditions: []authoring.PredicateRef{{ID: "pred.test.post"}}}
	}
	agent := func(id, dep string, mark authoring.ReviewSeriesAction) authoring.Step {
		step, err := authoring.NewAgentStep(header(id, dep), io(dep), authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: mark, Timeout: time.Minute,
		})
		must(err)
		return step
	}
	prep, err := authoring.NewLocalStep(header("prep.parse"), authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out"}, authoring.LocalSpec{Handler: "engine.test.prep"})
	must(err)
	steps := []authoring.Step{
		prep,
		agent("product.review", "prep.parse", authoring.ReviewSeriesProductReview),
		agent("start.readiness", "product.review", authoring.ReviewSeriesStartReadiness),
		agent("tail.readiness", "start.readiness", authoring.ReviewSeriesStartReadiness),
		agent("tail.product", "start.readiness", authoring.ReviewSeriesProductReview),
	}
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: steps}, reg)
	must(err)
	return cd, reg
}

// TestLateReceiptPairsWhenTheProductGateReopens：回执是无条件观察事实、
// 配对是惰性业务动作——只勾重走产品审时，就绪系列的暂存结果其迟到回执
// 在产品审重走期照常 ACCEPTED 落账（配对挂起、暂存保留、无重试请求）；
// 另一派发完成产品审新鲜轮（effective PASS@rev-2）的同一事务内，挂起配
// 对自动完成（completed+1、Results 在账）；同回执重发判 DUPLICATE、不
// 双计。
func TestLateReceiptPairsWhenTheProductGateReopens(t *testing.T) {
	cd, reg := parallelTailFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)

	// 双审走完＋拓扑＋路线：并列两派发槽同时在途。
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-pair-topo", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single unit"}})
	askAndDecide(t, engine, ControlSelectRoute, "req-pair-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: []string{"g1", "g2"}}})
	snap := engine.LoadMustSucceed(t)
	stagedSlot, freshSlot := "", ""
	for actionID, pending := range snap.State.PendingActions {
		switch pending.Step {
		case "tail.readiness":
			stagedSlot = actionID
		case "tail.product":
			freshSlot = actionID
		}
	}
	if stagedSlot == "" || freshSlot == "" {
		t.Fatalf("the parallel tail must hold both in-flight dispatches: %+v", snap.State.PendingActions)
	}

	// 就绪系列的下一轮证据先到（暂存，回执未到）。
	staged, err := NewReviewWorkerResultEvent("result-pair-staged", stagedSlot, testProvider, "sha256:payload-pair-staged",
		reviewEvidence(review.StartReadiness, "dispatch-readiness-2", "", "PASS", "readiness round before the change", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("stage: %v %+v", err, acceptance)
	}

	// 只勾重走产品审：就绪系列未勾即续用。
	askAndDecideRequirement(t, engine, "req-pair-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-pair",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "only the product review is redone",
	})

	// 回执在产品审重走期到达：无条件落账，配对挂起，暂存保留。
	receipt := spawnReceipt(t, "spawn-pair-late", stagedSlot, "agent-pair-late")
	if acceptance, err := engine.Submit(receipt, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("late receipt during the redo = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	if _, kept := snap.State.StagedResults[stagedSlot]; !kept {
		t.Fatalf("the suspended pairing must keep the staging: %+v", snap.State.StagedResults)
	}
	if _, admitted := snap.State.Results[stagedSlot]; admitted {
		t.Fatal("the suspended round must not be admitted yet")
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 {
		t.Fatalf("the suspended round must not count yet: completed=%d", series.Completed)
	}
	suspended := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == stagedSlot && strings.Contains(record.Detail, "stays pending until the product-review conclusion re-passes") {
			suspended = true
		}
	}
	if !suspended {
		t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
	}

	// 另一派发完成产品审新鲜轮（pre-floor 旧派发按声明终止，替换派发
	// PASS@rev-2）：同一事务内挂起配对自动完成。
	freshRoundOnTheReplacement(t, engine, freshSlot, "result-pair-product", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "fresh product round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("product fresh round = completed %d %+v", series.Completed, series.Effective)
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 2 || series.Effective.Status != "PASS" {
		t.Fatalf("the suspended pairing did not complete in the same transaction: completed=%d %+v", series.Completed, series.Effective)
	}
	if _, admitted := snap.State.Results[stagedSlot]; !admitted {
		t.Fatal("the paired round did not reach the results ledger")
	}
	if _, kept := snap.State.StagedResults[stagedSlot]; kept {
		t.Fatalf("the paired round must leave staging: %+v", snap.State.StagedResults)
	}

	// 同回执字节换事件 ID 重发判 DUPLICATE；已完成的配对不双计。
	duplicateReceipt := spawnReceipt(t, "spawn-pair-replay", stagedSlot, "agent-pair-late")
	if acceptance, err := engine.Submit(duplicateReceipt, fingerprintOf(t, engine)); err != nil || acceptance.Status != "DUPLICATE" {
		t.Fatalf("same-bytes receipt under a new event id = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 2 {
		t.Fatalf("the replayed receipt double-counted: completed=%d", series.Completed)
	}
}

// TestQuarantinedReadinessUnderProductRedoStaysSuspended：重放路径与回执
// 配对同享惰性语义——产品审重走期隔离的就绪结果，屏障解除的重放在顺序
// 门未开时回暂存挂起（不作废、不计轮），留挂起留痕。
func TestQuarantinedReadinessUnderProductRedoStaysSuspended(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-qap-product", product, reviewEvidence(review.ProductReview, "dispatch-qap-product", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)

	redo := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-qap",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "only the product review is redone",
	}
	request, err := NewRequirementRequestEvent("req-qap", redo.packageView(), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-qap", readiness, "agent-qap"), fp); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-qap", readiness, testProvider, "sha256:payload-qap",
		reviewEvidence(review.StartReadiness, "dispatch-qap-readiness", "", "PASS", "in-flight against rev-1", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, fp); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine: %+v %v", acceptance, err)
	}
	askAndDecideRequirement(t, engine, "req-qap", "proceed", redo)
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.QuarantinedResults) != 0 {
		t.Fatalf("the decide must reclassify the quarantine ledger: %+v", snap.State.QuarantinedResults)
	}
	if _, staged := snap.State.StagedResults[readiness]; !staged {
		t.Fatalf("the gate-blocked replay must return to staging: %+v", snap.State.StagedResults)
	}
	if _, voided := snap.State.ObsoleteResults[readiness]; voided {
		t.Fatal("the gate-blocked replay must not be voided")
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 0 {
		t.Fatalf("the suspended replay must not count a round: completed=%d", series.Completed)
	}
	suspended := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == readiness && strings.Contains(record.Detail, "returned to staging") {
			suspended = true
		}
	}
	if !suspended {
		t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
	}
}

// TestRedoObligationSurvivesACorrectionOfTheOldRound：重走义务是不可变事
// 实，不活在投影里——确认重走后对旧轮 P2 建议项的 DISMISS 纠正照常记账
// （账目成员前提不变、投影刷新为 PASS），义务不解除：进入开发的双审门
// 保持关闭；满足 floor 的新鲜轮完成后义务清偿、门重开。手改 FreshOwed
// 在加载边界可区分拒绝，恢复原状即照常加载。
func TestRedoObligationSurvivesACorrectionOfTheOldRound(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, dir := reviewEngine(t, cd, reg)

	// 产品审第一轮：PASS 携一条 P2 建议项；技术审 PASS 后走到拓扑。
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-owed-product-1", product, reviewEvidence(review.ProductReview, "dispatch-owed-1", "", "PASS", "clean round", []review.Finding{
		{Severity: "P2", Message: "advisory noise in the old round"},
	}))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-owed-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-owed-r1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-owed-topo", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single unit"}})
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	preRedoDispatch := pendingActionID(t, engine)

	// 确认重走产品审：义务置位，在途产品派发退位＋替换。
	askAndDecideRequirement(t, engine, "req-owed-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-owed",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the product walk is redone",
	})
	snap = engine.LoadMustSucceed(t)
	if !snap.State.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("the redo decision must set the fresh-round obligation")
	}

	// 对旧轮 finding 的 DISMISS 纠正：记账成功、投影刷成 PASS，但义务
	// 不解除——双审门保持关闭。
	dismiss := findingOption(t, engine, review.ProductReview, 0, "dismiss")
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-owed-dismiss", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: "dispatch-owed-1", RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Reason: "the advisory noise is accepted", ExpectedEffectiveRevision: snap.State.ReviewSeries.ProductReview.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the correction must keep refreshing the projection: %+v", series.Effective)
	}
	if !series.FreshOwed {
		t.Fatal("a correction of the old round must not discharge the redo obligation")
	}
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-owed-route-closed", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"g1", "g2"},
	}}); err == nil {
		t.Fatal("the development gate opened on a corrected projection while the redo obligation stands")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("closed gate code = %q (%v)", code, err)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("the obligated ledger must load: %v", err)
	}

	// 篡改负控：手改 FreshOwed 在加载边界可区分拒绝。
	original := stateBytesOf(t, dir)
	tamperStateContent(t, dir, func(content map[string]any) {
		productSeries := content["reviewSeries"].(map[string]any)["productReview"].(map[string]any)
		delete(productSeries, "freshOwed")
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("a forged discharged obligation loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("forged obligation code = %q (%v)", code, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
		t.Fatalf("restore state: %v", err)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}

	// 满足 floor 的新鲜轮完成：义务清偿，双审门重开。
	freshRoundOnTheReplacement(t, engine, preRedoDispatch, "result-owed-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-owed-2", "rev-2", "PASS", "fresh round of the changed requirement", nil))
	snap = engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("the satisfied fresh round must discharge the obligation")
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-owed-route-open", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"g1", "g2"},
	}})
	if snap = engine.LoadMustSucceed(t); snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL after the obligation cleared", snap.State.Phase)
	}
}

// TestBarrierHoldsTheReceiptPairingUntilTheDecision：屏障前暂存而屏障内收
// 回执——回执是观察事实照常 ACCEPTED 落账，暂存保留、配对挂起（留痕），
// 计轮为零；决定解除屏障的同一事务内补配对完成接纳。
func TestBarrierHoldsTheReceiptPairingUntilTheDecision(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	fp := fingerprintOf(t, engine)

	staged, err := NewReviewWorkerResultEvent("result-barrier-staged", product, testProvider, "sha256:payload-barrier-staged",
		reviewEvidence(review.ProductReview, "dispatch-barrier-staged", "", "PASS", "staged before the barrier", nil))
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("stage: %v %+v", err, acceptance)
	}
	preserved := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-barrier-pair",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "meaning preserved",
	}
	request, err := NewRequirementRequestEvent("req-barrier-pair", preserved.packageView(), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}

	// 屏障内迟到回执：观察事实照常落账；配对挂起、暂存保留、计轮为零。
	receipt := spawnReceipt(t, "spawn-barrier-pair", product, "agent-barrier-pair")
	if acceptance, err := engine.Submit(receipt, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("receipt during the barrier = %v %+v", err, acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if _, kept := snap.State.StagedResults[product]; !kept {
		t.Fatalf("the staged result must stay suspended while the barrier holds: %+v", snap.State.StagedResults)
	}
	if _, admitted := snap.State.Results[product]; admitted {
		t.Fatal("no result admission may run while the barrier holds")
	}
	if snap.State.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatalf("a suspended pairing must not count: completed=%d", snap.State.ReviewSeries.ProductReview.Completed)
	}
	held := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == product && strings.Contains(record.Detail, "stays suspended until the barrier lifts") {
			held = true
		}
	}
	if !held {
		t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
	}

	// 决定解除屏障：同一事务内补配对完成接纳。
	askAndDecideRequirement(t, engine, "req-barrier-pair", "proceed", preserved)
	snap = engine.LoadMustSucceed(t)
	if _, kept := snap.State.StagedResults[product]; kept {
		t.Fatalf("the lift must pair the suspended staging: %+v", snap.State.StagedResults)
	}
	if _, admitted := snap.State.Results[product]; !admitted {
		t.Fatal("the paired result must reach Results in the lifting transaction")
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("the paired round = completed %d %+v", series.Completed, series.Effective)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestTerminalFreshDispatchResultAdmitsAtItsRealIssueRevision：替换派发
// （签发代数＝重走决定）先 RUNNING→VALIDATING→TERMINAL 再交合法 fresh 结
// 果——terminal 对账以持久签发事实过 floor 校验（真实代数 ≥floor），新鲜
// 轮照常完成。
func TestTerminalFreshDispatchResultAdmitsAtItsRealIssueRevision(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]

	askAndDecideRequirement(t, engine, "req-terminal-fresh", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-terminal-fresh",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the product walk is redone",
	})
	snap := engine.LoadMustSucceed(t)
	replacement := snap.State.ObsoleteActions[product].ReplacedBy
	series, _ := snap.State.ReviewSeries.Series(review.ProductReview)
	if issued := snap.State.DispatchIssueRevisions[replacement]; issued < series.FreshFloor {
		t.Fatalf("the replacement must be issued at or after the floor: issued=%d floor=%d", issued, series.FreshFloor)
	}

	// 回执先落账（派发观察事实），替换派发随后到 terminal。
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "spawn-terminal-fresh", replacement, "agent-terminal-fresh"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pending := snap.State.PendingActions[replacement]
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID("task-terminal-fresh-"+string(rune('a'+i))), pending.Task, pending.AttemptID, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fp); err != nil {
			t.Fatalf("task progress %s: %v", status, err)
		}
	}

	// 合法 fresh 结果随后到达：真实签发代数过 floor，terminal 对账完成
	// 新鲜轮。
	fresh, err := NewReviewWorkerResultEvent("result-terminal-fresh", replacement, testProvider, "sha256:payload-terminal-fresh",
		reviewEvidenceAt(review.ProductReview, "dispatch-terminal-fresh", "rev-2", "PASS", "fresh round on a terminal task", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(fresh, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh result on the terminal replacement = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("the terminal fresh round = completed %d %+v", series.Completed, series.Effective)
	}
	if series.FreshOwed {
		t.Fatal("the completed fresh round must discharge the obligation")
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestProductFreshRoundCompletionOrderReachesTheSameTerminalState：产品审
// 新鲜轮经"结果先到（暂存→回执）"或"回执先到→结果后到"两种顺序完成，
// 终态完全相同——挂起的技术审配对在两种路径的同一事务收尾单点都补齐
// （completed、effective、Results 归属、暂存/隔离清空、账本可加载）。
func TestProductFreshRoundCompletionOrderReachesTheSameTerminalState(t *testing.T) {
	type completionFacts struct {
		productCompleted   int
		productEffective   string
		productBound       string
		readinessCompleted int
		readinessEffective string
		readinessBound     string
		resultsEntries     int
		staging            int
		quarantine         int
	}
	drive := func(t *testing.T, resultFirst bool) completionFacts {
		cd, reg := parallelTailFixture(t)
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)

		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-order-product-1", product, reviewEvidence(review.ProductReview, "dispatch-order-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-order-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-order-readiness-1", "", "PASS", "ready", nil))
		askAndDecide(t, engine, ControlConfirmTopology, "req-order-topo", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single unit"}})
		askAndDecide(t, engine, ControlSelectRoute, "req-order-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: []string{"g1", "g2"}}})
		snap := engine.LoadMustSucceed(t)
		stagedSlot, freshSlot := "", ""
		for actionID, pending := range snap.State.PendingActions {
			switch pending.Step {
			case "tail.readiness":
				stagedSlot = actionID
			case "tail.product":
				freshSlot = actionID
			}
		}
		if stagedSlot == "" || freshSlot == "" {
			t.Fatalf("the parallel tail must hold both dispatches: %+v", snap.State.PendingActions)
		}

		// 技术审下一轮证据先到（暂存、回执未到）。
		staged, err := NewReviewWorkerResultEvent("result-order-readiness-2", stagedSlot, testProvider, "sha256:payload-order-readiness-2",
			reviewEvidence(review.StartReadiness, "dispatch-order-readiness-2", "", "PASS", "readiness round before the change", nil))
		if err != nil {
			t.Fatalf("staged event: %v", err)
		}
		if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
			t.Fatalf("stage: %v %+v", err, acceptance)
		}

		// 只勾重走产品审。
		askAndDecideRequirement(t, engine, "req-order-redo", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-order",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "only the product review is redone",
		})
		snap = engine.LoadMustSucceed(t)
		replacement := snap.State.ObsoleteActions[freshSlot].ReplacedBy

		// 技术审槽的回执到达：配对在顺序门挂起（产品审义务未清偿）。
		if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-readiness", stagedSlot, "agent-order-readiness"), fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("readiness receipt: %v %+v", err, acceptance)
		}
		snap = engine.LoadMustSucceed(t)
		if _, kept := snap.State.StagedResults[stagedSlot]; !kept {
			t.Fatalf("the readiness pairing must stay suspended at the gate: %+v", snap.State.StagedResults)
		}

		// 产品审新鲜轮经两种顺序完成。
		fresh := reviewEvidenceAt(review.ProductReview, "dispatch-order-product-2", "rev-2", "PASS", "fresh product round", nil)
		if resultFirst {
			stagedFresh, err := NewReviewWorkerResultEvent("result-order-product-2", replacement, testProvider, "sha256:payload-order-product-2", fresh)
			if err != nil {
				t.Fatalf("fresh event: %v", err)
			}
			if acceptance, err := engine.Submit(stagedFresh, fp); err != nil || acceptance.Status != "STAGED" {
				t.Fatalf("fresh stage: %v %+v", err, acceptance)
			}
			if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-product", replacement, "agent-order-product"), fp); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh receipt: %v %+v", err, acceptance)
			}
		} else {
			if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-product", replacement, "agent-order-product"), fp); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh receipt: %v %+v", err, acceptance)
			}
			directFresh, err := NewReviewWorkerResultEvent("result-order-product-2", replacement, testProvider, "sha256:payload-order-product-2", fresh)
			if err != nil {
				t.Fatalf("fresh event: %v", err)
			}
			if acceptance, err := engine.Submit(directFresh, fp); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh result: %v %+v", err, acceptance)
			}
		}

		snap = engine.LoadMustSucceed(t)
		facts := completionFacts{
			productCompleted:   snap.State.ReviewSeries.ProductReview.Completed,
			productEffective:   snap.State.ReviewSeries.ProductReview.Effective.Status,
			productBound:       snap.State.ReviewSeries.ProductReview.Effective.RequirementRevision,
			readinessCompleted: snap.State.ReviewSeries.StartReadiness.Completed,
			readinessEffective: snap.State.ReviewSeries.StartReadiness.Effective.Status,
			readinessBound:     snap.State.ReviewSeries.StartReadiness.Effective.RequirementRevision,
			staging:            len(snap.State.StagedResults),
			quarantine:         len(snap.State.QuarantinedResults),
		}
		facts.resultsEntries = len(snap.State.Results)
		if _, err := engine.Load(); err != nil {
			t.Fatalf("load: %v", err)
		}
		return facts
	}
	stagedFirst := drive(t, true)
	receiptFirst := drive(t, false)
	if stagedFirst != receiptFirst {
		t.Fatalf("the two completion orders reached different terminal states: %+v vs %+v", stagedFirst, receiptFirst)
	}
	if stagedFirst.productCompleted != 2 || stagedFirst.productEffective != "PASS" || stagedFirst.productBound != "rev-2" ||
		stagedFirst.readinessCompleted != 2 || stagedFirst.readinessEffective != "PASS" || stagedFirst.readinessBound != "rev-1" ||
		stagedFirst.staging != 0 || stagedFirst.quarantine != 0 {
		t.Fatalf("terminal state = %+v (want product 2/PASS@rev-2, readiness 2/PASS@rev-1, no staging or quarantine)", stagedFirst)
	}
	if stagedFirst.resultsEntries != 4 {
		t.Fatalf("the ledger must hold all four admitted results (two review rounds and two tail completions): %d", stagedFirst.resultsEntries)
	}
}
