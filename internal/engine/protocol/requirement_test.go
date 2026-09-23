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

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// requirementOptions 是 REQUEST_REQUIREMENT_CHANGE 的示例选项集（引擎不
// 锚定具体选项 id：typed 载荷才是登记修订的唯一权威，nil 载荷＝放弃）。
func requirementOptions() []AskOption {
	return []AskOption{{ID: "proceed", Label: "确认变更包"}, {ID: "abort", Label: "放弃变更"}}
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
	request, err := NewRequestEvent(EventID(requestID), ControlRequestRequirementChange, requirementOptions()...)
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
	request, err := NewRequestEvent(EventID(requestID), ControlRequestRequirementChange, requirementOptions()...)
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
// 次）、拓扑路线不动；旧 revision 的证据此后被拒，新 revision 的新鲜轮
// 被纳且无强制重审要求。
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
		Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
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

	// 旧 revision 证据被拒（新权威不再认 rev-1 绑定）。
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "spawn-stale-rev", nextAction, "agent-stale-rev"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	stale, err := NewReviewWorkerResultEvent("result-stale-rev", nextAction, testProvider, "sha256:stale-rev", reviewEvidenceAt(review.ProductReview, "dispatch-stale", "rev-1", "PASS", "old binding", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(stale, fp); err == nil {
		t.Fatal("rev-1 evidence accepted after the revision change")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("stale evidence code = %q", code)
	}

	// 新 revision 的新鲜轮被纳：轮次可继续、无重审要求、无阻塞门槛（派发
	// 回执已随被拒的旧绑定尝试落账，重用同一 action）。
	fresh, err := NewReviewWorkerResultEvent("result-product-2", nextAction, testProvider, "sha256:payload-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "fresh round on the changed requirement", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(fresh, fp); err != nil {
		t.Fatalf("fresh round at rev-2: %v", err)
	}
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("fresh round at rev-2 = completed %d %+v", series.Completed, series.Effective)
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

	t.Run("product-review only keeps bindings and cascades readiness void", func(t *testing.T) {
		engine, _ := newRun(t)
		nextAction := pendingActionID(t, engine)
		askAndDecideRequirement(t, engine, "req-change-partial", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-partial",
			Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "product meaning changed",
		})
		snap := engine.LoadMustSucceed(t)
		product := snap.State.ReviewSeries.ProductReview
		if product.Effective.Status != "PENDING" || !strings.HasPrefix(product.Effective.Source, "requirement-change:") ||
			len(product.Effective.DownstreamImpact) == 0 || product.Completed != 1 {
			t.Fatalf("redo product series = %+v", product.Effective)
		}
		readiness := snap.State.ReviewSeries.StartReadiness
		if readiness.Effective.Status != "PENDING" || !readiness.UpstreamVoid || readiness.Completed != 1 {
			t.Fatalf("readiness must cascade to upstream void with its ledger untouched: %+v completed=%d", readiness.Effective, readiness.Completed)
		}
		if snap.State.Topology == nil || snap.State.RouteMode != "full" || len(snap.State.SelectedGates) != 2 {
			t.Fatalf("unchecked topology/route must stay bound: %+v %q", snap.State.Topology, snap.State.RouteMode)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (phase 不变)", snap.State.Phase)
		}
		// 新 revision 的产品审新鲜轮可走；其 PASS 不复活被级联失效的技术审
		// ——双审门保持关闭直到 readiness 自己重走。
		submitReviewResult(t, engine, "result-product-2", nextAction, reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		snap = engine.LoadMustSucceed(t)
		if downstream := snap.State.ReviewSeries.StartReadiness; downstream.Effective.Status != "PENDING" || !downstream.UpstreamVoid {
			t.Fatalf("readiness must stay void until its own fresh round: %+v", downstream.Effective)
		}
	})

	t.Run("full scope reopens the complete re-walk path", func(t *testing.T) {
		engine, _ := newRun(t)
		productRound := pendingActionID(t, engine)
		askAndDecideRequirement(t, engine, "req-change-full", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-full",
			Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
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
		// 重新双审（rev-2）→ 重新拓扑 → 重新路线。
		if snap.State.ReviewSeries.ProductReview.Effective.Status != "PENDING" || snap.State.ReviewSeries.StartReadiness.Effective.Status != "PENDING" {
			t.Fatalf("both series must be staled: %+v %+v", snap.State.ReviewSeries.ProductReview.Effective, snap.State.ReviewSeries.StartReadiness.Effective)
		}
		submitReviewResult(t, engine, "result-product-2", productRound, reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
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
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
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
		Source: RequirementSourceReviewFindingP1, Classification: RequirementClassificationPreserved,
		Reason: "attempt to preserve meaning", FindingDigest: digest,
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-p1-preserved", "proceed", preserved); err == nil {
		t.Fatal("P1-driven PRESERVED accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("P1 PRESERVED code = %q", code)
	}

	missingScope := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		Source: RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
		RedoTopology: true, Reason: "scope omits the owning series", FindingDigest: digest,
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-p1-noscope", "proceed", missingScope); err == nil {
		t.Fatal("P1-driven change without the owning series redo accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("P1 no-scope code = %q", code)
	}

	unknown := &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "binds a finding that does not exist", FindingDigest: "sha256:no-such-finding",
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-unknown-finding", "proceed", unknown); err == nil {
		t.Fatal("change bound to an unknown finding accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected {
		t.Fatalf("unknown finding code = %q", code)
	}

	// 正确载荷：CHANGED＋重走 finding 所在 series。此后新 revision 的新
	// 鲜轮放行（既有阻塞不再拦）。
	askAndDecideRequirement(t, engine, "req-p1-changed", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		Source: RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the confirmed blocker is repaired in the changed requirement", FindingDigest: digest,
	})
	freshRound := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-product-2", freshRound, reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "fresh review of the changed requirement", nil))
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
		request, err := NewRequestEvent("req-barrier", ControlRequestRequirementChange, requirementOptions()...)
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

	t.Run("receipts and results pass through with refill withheld", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		request, err := NewRequestEvent("req-barrier-result", ControlRequestRequirementChange, requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		// 审查回执与结果照常接纳；refill 被扣发（不签发 start-readiness）。
		acceptance := submitReviewResult(t, engine, "result-under-barrier", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		if acceptance.Status != "ACCEPTED" {
			t.Fatalf("result under barrier status = %q", acceptance.Status)
		}
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.PendingActions) != 0 {
			t.Fatalf("refill must be withheld under the barrier: %v", snap.State.PendingActions)
		}
		if snap.State.ReviewSeries.ProductReview.Completed != 1 {
			t.Fatalf("review evidence must be admitted under the barrier: %+v", snap.State.ReviewSeries.ProductReview)
		}
		// 决定后解冻：同一次提交的 refill 补进下一轮派发。
		askAndDecideRequirement(t, engine, "req-barrier-result", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-barrier",
			Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
			Reason: "wording only",
		})
		snap = engine.LoadMustSucceed(t)
		if len(snap.State.PendingActions) != 1 {
			t.Fatalf("issuance must resume after the decide: %v", snap.State.PendingActions)
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

	// CAS：基线不是当前权威 revision → 冲突拒绝。
	stale := &RequirementDecisionPayload{
		BaseRevision: "rev-0", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta",
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
		Reason: "stale base",
	}
	if err := askAndDecideRequirementRejecting(t, engine, dir, "req-cas", "proceed", stale); err == nil {
		t.Fatal("stale base accepted")
	} else if code := rejectionCode(t, err); code != CodeRequirementDecisionRejected || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("stale base code = %q (%v)", code, err)
	}

	// 载荷形状：构造层拒绝（不触引擎）。
	schema := []struct {
		name    string
		payload RequirementDecisionPayload
	}{
		{"candidate equals base", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-1", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"empty delta digest", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"empty reason", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved}},
		{"unknown source", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: "AI_INITIATED", Classification: RequirementClassificationPreserved, Reason: "r"}},
		{"unknown classification", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: "UNCHANGED", Reason: "r"}},
		{"user-initiated binds a finding", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r", FindingDigest: "sha256:f"}},
		{"finding-driven without a digest", RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved, Reason: "r"}},
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
		Requirement: &RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}}}
	if err := both.Validate(); err == nil {
		t.Fatal("dual typed variants accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("dual variant code = %q", code)
	}

	// 控制↔载荷配对：需求控制携 Review 载荷、审查控制携 Requirement 载荷
	// 都拒绝。
	reviewPayload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "n"}}
	if err := askAndDecideRequirementCrossPayload(t, engine, dir, "req-cross-review", &reviewPayload, nil); err == nil {
		t.Fatal("requirement control with a review payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("cross review code = %q", code)
	}
	requirementPayload := RequirementDecisionPayload{BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:d", Source: RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved, Reason: "r"}
	if err := askAndDecideRequirementCrossPayload(t, engine, dir, "req-cross-requirement", nil, &requirementPayload); err == nil {
		t.Fatal("topology control with a requirement payload accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("cross requirement code = %q", code)
	}

	// freshness 过期回归：后续提交取代旧 token。
	fp := fingerprintOf(t, engine)
	request, err := NewRequestEvent("req-fresh", ControlRequestRequirementChange, requirementOptions()...)
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
	if requirementPayload != nil {
		control = ControlConfirmTopology
		choice = "no-split"
		options = topologyOptions()
	}
	request, err := NewRequestEvent(EventID(requestID), control, options...)
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
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
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
		entry(content)["classification"] = RequirementClassificationPreserved
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("preserved classification with a redo scope loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("preserved-with-scope code = %q", code)
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
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
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
			Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
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
		// 产品审 PASS 绑 rev-1 同样有效）。
		submitReviewResult(t, engine, "result-readiness-2", nextAction, reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
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
			Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoStartReadiness: true, Reason: "readiness-only redo after development started",
		})
		// 主代理复现的原句拒绝在这里必须不再发生。
		acceptance := submitReviewResult(t, engine, "result-readiness-2", nextAction, reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		if acceptance.Status != "ACCEPTED" {
			t.Fatalf("rev-2 readiness evidence status = %q, want ACCEPTED", acceptance.Status)
		}
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

// TestProductOnlyRedoKeepsUpstreamVoidUntilReadinessRewalk：只勾选重走
// product-review——产品审重过（rev-2）后 readiness 仍因 UpstreamVoid 保持
// PENDING，门保持关闭；级联失效语义不被 carry 判定破坏，readiness 自行
// 重走后门才重开。
func TestProductOnlyRedoKeepsUpstreamVoidUntilReadinessRewalk(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again", "readiness.again")
	engine, dir := reviewEngine(t, cd, reg)
	productRound := walkToTopologyAndRoute(t, engine)

	askAndDecideRequirement(t, engine, "req-change-product-only", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-product",
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "only the product walk is affected",
	})
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-void-1", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}}); err == nil {
		t.Fatal("route accepted while the redone product series is PENDING")
	}

	// 产品审重走并在 rev-2 重新 PASS：级联 void 仍持有 readiness 的 PENDING。
	submitReviewResult(t, engine, "result-product-2", productRound, reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
	snap := engine.LoadMustSucceed(t)
	if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PENDING" || !readiness.UpstreamVoid {
		t.Fatalf("a carried-over upstream void must hold readiness at PENDING: %+v void=%v", readiness.Effective, readiness.UpstreamVoid)
	}
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-void-2", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}}); err == nil {
		t.Fatal("route accepted while readiness stays void")
	}

	// readiness 自行重走（rev-2）后门重开。
	readinessRound := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-2", readinessRound, reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
	snap = engine.LoadMustSucceed(t)
	if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PASS" || readiness.UpstreamVoid {
		t.Fatalf("fresh readiness round must rebuild its conclusion: %+v", readiness.Effective)
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-route-open-2", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
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
	}); err == nil {
		t.Fatal("the sequence guard opened behind an out-of-ledger PASS")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("out-of-ledger guard code = %q", code)
	}

	carried := build("rev-1")
	if !carried.effectivePass(carried.ReviewSeries.ProductReview) {
		t.Fatal("a PASS bound to a confirmed revision must stay valid")
	}
	if _, err := carried.admitReviewEvidence(&ReviewResultEvidence{
		Action: review.StartReadiness, DispatchID: "dispatch-readiness-1", RequirementRevision: "rev-1",
		Status: "PASS", Verification: reviewVerification(),
	}); err != nil {
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
