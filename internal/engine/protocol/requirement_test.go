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
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	definition "formal-gates/internal/engine/testkit/workflow"
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

func TestUnchangedRequirementConfirmationKeepsPlanScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		payload  RequirementDecisionPayload
		accepted bool
	}{
		{name: "route", payload: RequirementDecisionPayload{RedoRoute: true}, accepted: true},
		{name: "technical-review-and-route", payload: RequirementDecisionPayload{RedoStartReadiness: true, RedoRoute: true}, accepted: true},
		{name: "no-plan-scope"},
		{name: "technical-review-only", payload: RequirementDecisionPayload{RedoStartReadiness: true}},
		{name: "product-review-and-route", payload: RequirementDecisionPayload{RedoProductReview: true, RedoRoute: true}},
		{name: "topology-and-route", payload: RequirementDecisionPayload{RedoTopology: true, RedoRoute: true}},
		{name: "changed-meaning", payload: RequirementDecisionPayload{RedoRoute: true, Classification: RequirementClassificationChanged}},
		{name: "finding-driven", payload: RequirementDecisionPayload{RedoRoute: true, Source: RequirementSourceReviewFindingP2, FindingDigest: "confirmed-finding"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := tc.payload
			payload.BaseRevision, payload.CandidateRevision = "same-requirement", "same-requirement"
			payload.DeltaDigest, payload.ImpactDigest, payload.Reason = "confirmed-delta", "confirmed-impact", "confirm the unchanged requirement and displayed plan scope"
			if payload.Source == "" {
				payload.Source = RequirementSourceUserInitiated
			}
			if payload.Classification == "" {
				payload.Classification = RequirementClassificationPreserved
			}
			_, err := NewRequirementDecideEvent("confirm-plan", "plan-ask", "fresh-token", "proceed", payload)
			if (err == nil) != tc.accepted {
				t.Fatalf("unchanged requirement confirmation accepted=%t, error=%v", tc.accepted, err)
			}
		})
	}
}

// reviewEvidenceAt 构造绑定任意需求 revision 的审查证据。
func reviewEvidenceAt(action review.Action, dispatchID, revision, status, message string, findings []review.Finding) ReviewResultEvidence {
	return ReviewResultEvidence{
		Action: action, DispatchID: dispatchID, RequirementRevision: revision,
		Status: status, Message: message, Findings: findings,
	}
}

// verifyOriginalFixtureReview checks the paired source against the complete
// declared worker event before the fixture's independent actor accepts it.
func verifyOriginalFixtureReview(t *testing.T, engine *Engine, event Event) {
	t.Helper()
	state := engine.LoadMustSucceed(t).State
	source := state.Results[event.Result.ActionID]
	if source.ActionID != event.Result.ActionID || source.Provider != event.Result.Provider || source.Outcome != event.Result.Outcome || source.PayloadDigest != event.Result.PayloadDigest || !reflect.DeepEqual(source.Review, event.Result.Review) || source.Digest == "" {
		t.Fatalf("paired source differs from its complete original worker event: %+v", source)
	}
	if !state.reviewResultAwaitingVerification(source.ActionID) {
		t.Fatal("paired source must retain its independent acceptance duty")
	}
	before, _ := state.ReviewSeries.Series(source.Review.Action)
	verifyCompleteFixtureReview(t, engine, source.ActionID)
	after := engine.LoadMustSucceed(t).State
	series, _ := after.ReviewSeries.Series(source.Review.Action)
	if series.Completed != before.Completed+1 || !reflect.DeepEqual(after.Results[source.ActionID], source) || after.reviewResultAwaitingVerification(source.ActionID) {
		t.Fatal("independent acceptance must preserve the original result and count exactly one round")
	}
	candidate := series.RawCandidates[len(series.RawCandidates)-1]
	if candidate.DispatchID != source.ActionID || candidate.RequirementRevision != source.Review.RequirementRevision || !reflect.DeepEqual(candidate.OperatorVerification.Checks, review.RequiredVerificationChecks()) || !strings.Contains(candidate.OperatorVerification.Evidence, source.Digest) || !strings.Contains(candidate.OperatorVerification.Evidence, source.PayloadDigest) {
		t.Fatal("independent acceptance lost the source binding or its complete eight-check evidence")
	}
}

// askAndDecideRequirement 走需求变化控制的两段：创建 Ask，取当前
// freshness token 后提交决定（payload 为 nil 时是放弃决定）。
func askAndDecideRequirement(t *testing.T, engine *Engine, requestID string, choice AskOptionID, payload *RequirementDecisionPayload) Acceptance {
	t.Helper()
	observedInput := Observation{}
	request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(payload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	acceptance, err := engine.Submit(requirementDecideEvent(t, requestID, token, choice, payload), observedInput)
	if err != nil {
		t.Fatalf("submit decide %s: %v", requestID, err)
	}
	return acceptance
}

// askAndDecideRequirementRejecting 创建 Ask 后提交需求变化决定并返回其
// 错误；零状态变化断言只围绕决定提交本身（Ask 落账是合法提交）。
func askAndDecideRequirementRejecting(t *testing.T, engine *Engine, dir, requestID string, choice AskOptionID, payload *RequirementDecisionPayload) error {
	t.Helper()
	observedInput := Observation{}
	request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(payload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	before := stateBytesOf(t, dir)
	_, err = engine.Submit(requirementDecideEvent(t, requestID, token, choice, payload), observedInput)
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
	observedInput := Observation{}
	token, err := engine.Freshness(RequestID(requestID))
	if err != nil {
		t.Fatalf("freshness %s: %v", requestID, err)
	}
	if _, err := engine.Submit(requirementDecideEvent(t, requestID, token, "abort", nil), observedInput); err != nil {
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

// freshReviewAfterRedo observes the original dispatch's stop, then locates its
// newly issued review obligation without reusing the original task.
func freshReviewAfterRedo(t *testing.T, engine *Engine, retiredActionID string) string {
	t.Helper()
	stopSupersededDispatch(t, engine, retiredActionID)
	snap := engine.LoadMustSucceed(t)
	obsolete, known := snap.State.ObsoleteActions[retiredActionID]
	if !known || snap.State.TaskStatusOf(obsolete.Task) != runtime.TaskTerminal {
		t.Fatalf("the old review task was not retired and closed: %+v", obsolete)
	}
	mark := engine.reviewSeriesOf(obsolete.Task.Step)
	for actionID, pending := range snap.State.PendingActions {
		if pending.ReviewSeries == mark {
			if pending.Task == obsolete.Task {
				t.Fatalf("requirement redo reused task %s", pending.Task)
			}
			return actionID
		}
	}
	t.Fatalf("no fresh %s review after the redo", mark)
	return ""
}

func completeReviewAfterRedo(t *testing.T, engine *Engine, retiredActionID, eventID string, evidence ReviewResultEvidence) string {
	t.Helper()
	fresh := freshReviewAfterRedo(t, engine, retiredActionID)
	if acceptance := submitVerifiedReviewResult(t, engine, eventID, fresh, evidence); acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh review = %+v", acceptance)
	}
	return fresh
}

// freshRoundFromTheLedgerProposal 完成账本欠轮提案签出的新鲜轮：决定事务
// 内提案已在容量内签出新鲜轮（系列首载体、签发代数＝决定），直接提交证
// 据完成。返回新鲜轮的 actionID。
func freshRoundFromTheLedgerProposal(t *testing.T, engine *Engine, step string, eventID string, evidence ReviewResultEvidence) string {
	t.Helper()
	fresh := pendingActionByStep(t, engine, step)
	acceptance := submitVerifiedReviewResult(t, engine, eventID, fresh, evidence)
	if acceptance.Status != "ACCEPTED" {
		t.Fatalf("the ledger-proposed fresh round = %+v", acceptance)
	}
	return fresh
}

// walkToDevelopment 走完双审（rev-1 双 PASS）＋拓扑（no-split）＋路线
// （显式全选）。两条系列双双结论后签发闸不再出轮：重走的新鲜轮由决定事务的
// 账本欠轮提案签出（见 freshRoundFromTheLedgerProposal）。
func walkToDevelopment(t *testing.T, engine *Engine) {
	t.Helper()
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitVerifiedReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// TestRequirementLedgerSeedsFromIntakeReceipt：账本为空而受理回执已落账
// 时，加载即回填单条 INTAKE 初始账目；权威 revision 读取与受理值一致。
func TestRequirementLedgerSeedsFromIntakeReceipt(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)

	// 产品审：一条 P2 finding（advisory→effective PASS），用户确认处置。
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the documented path"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-p2", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the advisory", ExpectedEffectiveRevision: 1,
	}})
	// The independently established P2 requires its confirmed complete remedy
	// before readiness. The preserved correction consumes no additional review.
	askAndDecideRequirement(t, engine, "req-clarify-p2", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-clarified", DeltaDigest: "sha256:clarified-advisory",
		ImpactDigest: "sha256:impact", Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "user confirms the complete bounded wording correction", FindingDigest: candidate.Findings[0].Digest,
	})
	readiness := pendingActionByStep(t, engine, "start.readiness")
	submitVerifiedReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidenceAt(review.StartReadiness, readiness, engine.LoadMustSucceed(t).State.RequirementRevision(), "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
	productBefore := snap.State.ReviewSeries.ProductReview
	readinessBefore := snap.State.ReviewSeries.StartReadiness
	baseRevision := snap.State.RequirementRevision()
	priorEntries := len(snap.State.RequirementRevisions)

	// REVIEW_FINDING_P2 修订，用户声明 PRESERVED（P2 默认不清审）。
	acceptance := askAndDecideRequirement(t, engine, "req-change-p2", "proceed", &RequirementDecisionPayload{
		BaseRevision: baseRevision, CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p2",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "the fix touches wording only", FindingDigest: candidate.Findings[0].Digest,
	})
	snap = engine.LoadMustSucceed(t)
	ledger := snap.State.RequirementRevisions
	if len(ledger) != priorEntries+1 || ledger[priorEntries].Revision != "rev-2" || ledger[priorEntries].BaseRevision != baseRevision ||
		ledger[priorEntries].Classification != RequirementClassificationPreserved || len(ledger[priorEntries].DecidedScope) != 0 ||
		ledger[priorEntries].RequestID != "req-change-p2" || ledger[priorEntries].StateRevision != acceptance.Revision {
		t.Fatalf("change ledger = %+v", ledger)
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
	if snap.State.Topology == nil || !snap.State.RouteConfirmed {
		t.Fatalf("PRESERVED must keep topology and route: %+v %t", snap.State.Topology, snap.State.RouteConfirmed)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}

	// 未勾重走即续用（carry 授权，§7.2 第 4 项）：作废产品审旧轮把系列置回
	// 未结论（欠轮提案随即签出新一轮），绑已确认 rev-1 的证据照常接纳并
	// 携原 revision 记账——不因新权威 rev-2 而失格，也无强制重审。
	invalidateRoundOf(t, engine, "req-p2-invalidate", product)
	nextAction := pendingActionByStep(t, engine, "product.review")
	observedInput := Observation{}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-carry-rev", nextAction, "agent-carry-rev"), observedInput); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	carried, err := NewReviewWorkerResultEvent("result-carried-rev", nextAction, testProvider, "sha256:carried-rev", reviewEvidenceAt(review.ProductReview, nextAction, "rev-1", "PASS", "old binding carried", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(carried, observedInput); err != nil {
		t.Fatalf("carried rev-1 evidence rejected under a preserved change: %v", err)
	}
	verifyCompleteFixtureReview(t, engine, nextAction)
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
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
	t.Parallel()
	newRun := func(t *testing.T) (*Engine, string) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again", "readiness.again")
		engine, dir := reviewEngine(t, cd, reg)
		walkToDevelopment(t, engine)
		return engine, dir
	}

	t.Run("product-review only keeps bindings and carries readiness", func(t *testing.T) {
		engine, _ := newRun(t)
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
		if snap.State.Topology == nil || !snap.State.RouteConfirmed || len(snap.State.SelectedGates) != 4 {
			t.Fatalf("unchecked topology/route must stay bound: %+v %t", snap.State.Topology, snap.State.RouteConfirmed)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (phase 不变)", snap.State.Phase)
		}
		// 新 revision 的产品审新鲜轮可走（决定事务的欠轮提案签出）；其重
		// 过不消耗技术审的任何轮次——携用的 readiness PASS 原样（门重开的
		// 完整走线见 TestProductOnlyRedoCarriesTheReadinessConclusion）。
		freshRoundFromTheLedgerProposal(t, engine, "product.review", "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		snap = engine.LoadMustSucceed(t)
		if downstream := snap.State.ReviewSeries.StartReadiness; downstream.Effective.Status != "PASS" || downstream.Completed != 1 || downstream.Effective.RequirementRevision != "rev-1" {
			t.Fatalf("the carried readiness must survive the product re-review without a fresh round: %+v completed=%d", downstream.Effective, downstream.Completed)
		}
	})

	t.Run("full scope reopens the complete re-walk path", func(t *testing.T) {
		engine, _ := newRun(t)
		askAndDecideRequirement(t, engine, "req-change-full", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-full",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, RedoStartReadiness: true, RedoTopology: true, RedoRoute: true,
			Reason: "the whole walk is affected",
		})
		snap := engine.LoadMustSucceed(t)
		// 拓扑/路线解绑，已生效义务在新的配置决定前保持在册。
		if snap.State.Topology != nil || snap.State.RouteConfirmed || len(snap.State.SelectedGates) != 4 {
			t.Fatalf("checked topology/route must be unbound with obligations retained: %+v %t %v", snap.State.Topology, snap.State.RouteConfirmed, snap.State.SelectedGates)
		}
		if entry := snap.State.RequirementRevisions[1]; len(entry.DecidedScope) != 4 || entry.DecidedScope[0] != "product-review" ||
			entry.DecidedScope[1] != "start-readiness" || entry.DecidedScope[2] != "topology" || entry.DecidedScope[3] != "route" {
			t.Fatalf("full scope entry = %+v", entry.DecidedScope)
		}
		if snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL (账本 residue 仍证明其合法)", snap.State.Phase)
		}
		// 重新双审（rev-2）→ 重新拓扑 → 重新路线。产品审新鲜轮由决定事务
		// 的欠轮提案签出；其后签发的技术审派发天然 ≥floor（顺序门在签发
		// 侧等产品审重过）。
		if snap.State.ReviewSeries.ProductReview.Effective.Status != "PENDING" || snap.State.ReviewSeries.StartReadiness.Effective.Status != "PENDING" {
			t.Fatalf("both series must be staled: %+v %+v", snap.State.ReviewSeries.ProductReview.Effective, snap.State.ReviewSeries.StartReadiness.Effective)
		}
		freshRoundFromTheLedgerProposal(t, engine, "product.review", "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
		readinessRound := pendingActionByStep(t, engine, "start.readiness")
		submitVerifiedReviewResult(t, engine, "result-readiness-2", readinessRound, reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		snap = engine.LoadMustSucceed(t)
		if downstream := snap.State.ReviewSeries.StartReadiness; downstream.Effective.Status != "PASS" || downstream.UpstreamVoid {
			t.Fatalf("fresh readiness round must rebuild its own conclusion: %+v", downstream.Effective)
		}
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology-2", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆：重走后仍整体推进",
		}})
		askAndDecide(t, engine, ControlSelectRoute, "req-route-2", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
		snap = engine.LoadMustSucceed(t)
		if snap.State.Topology == nil || !snap.State.RouteConfirmed {
			t.Fatalf("re-walk must rebind topology and route: %+v %t", snap.State.Topology, snap.State.RouteConfirmed)
		}
	})
}

// TestP1DrivenChangeForcesChangedAndOwningSeriesRedo：确认 P1 驱动的修订
// 不可声明 PRESERVED、重走范围必须包含 finding 所在 series；正确载荷登记
// 后，已确认阻塞门槛在新 revision 下放行新一轮。
func TestP1DrivenChangeForcesChangedAndOwningSeriesRedo(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "review.p1.one", "review.p1.two", "review.p1.three")
	engine, dir := reviewEngine(t, cd, reg)

	// 产品审确认一条 P1 阻塞 finding。
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	}))
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-p1", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
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

	// A confirmed blocker waits for the corrected requirement before a new round.
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("finding disposition issued premature review work: %+v", pending)
	}
	askAndDecideRequirement(t, engine, "req-p1-changed", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-p1",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceReviewFindingP1, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the confirmed blocker is repaired in the changed requirement", FindingDigest: digest,
	})
	freshRoundFromTheLedgerProposal(t, engine, "review.p1.one", "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "fresh review of the changed requirement", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("fresh round under the new revision = completed %d %+v", series.Completed, series.Effective)
	}
}

// A confirmed blocker forbids a fresh round on unchanged input. A correction
// dismissing it concludes that same review without consuming another round.
func TestConfirmedBlockingGateHoldsFreshRounds(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "round.one", "round.two")
	engine, _ := reviewEngine(t, cd, reg)
	first := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "confirmed-result", first, reviewEvidence(review.ProductReview, first, "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	}))
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	candidate := series.RawCandidates[0]
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	askAndDecide(t, engine, ControlReviewFindingDecision, "confirmed-finding", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: first, RequirementRevision: candidate.RequirementRevision,
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "acceptance fails", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	assertReviewIssuanceHeld(t, engine)
	snap := engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if len(snap.State.PendingActions) != 0 || series.Issued != 1 || !review.HasConfirmedBlocking(series) {
		t.Fatalf("confirmed blocker must hold issuance: %+v", series)
	}
	dismiss := AskOption{ID: AskOptionID("dismiss:" + candidate.Findings[0].ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "correct-confirmed-finding", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: first, RequirementRevision: candidate.RequirementRevision,
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Supersedes: series.Adjudications[0].ID,
		Reason: "reproduction disproves the finding", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Effective.Status != "PASS" || series.Completed != 1 || series.Issued != 1 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("dismissal must conclude the original review: %+v", series)
	}
}

// TestRequirementChangeBarrierPhaseA：未决需求变化 Ask 冻结新的业务签发
// （可区分错误），回执/结果/既有 Ask 不受阻（refill 静默扣发），Ask 决定
// 后解冻；放弃决定不登记任何 revision；其它控制不触发屏障。
func TestRequirementChangeBarrierPhaseA(t *testing.T) {
	t.Parallel()
	t.Run("issuance frozen while the ask is pending", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		observedInput := Observation{}
		request, err := NewRequirementRequestEvent("req-barrier", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		plan, _, err := engine.Plan(Observation{})
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err == nil {
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
		if _, err := engine.Submit(decline, observedInput); err != nil {
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
		observedInput := Observation{}
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
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		// 回执不受阻；结果持久隔离：回执 QUARANTINED、事件进台账防重放、
		// 不推进不入 Results（进程重启不丢失，重分类在决定事务内）。
		if _, err := engine.Submit(spawnReceipt(t, "spawn-under-barrier", product, "agent-under-barrier"), observedInput); err != nil {
			t.Fatalf("spawn receipt under barrier: %v", err)
		}
		// worker 已按 Ask 绑定的候选 revision（rev-2）复审：隔离账存原样。
		ev, err := NewReviewWorkerResultEvent("result-under-barrier", product, testProvider, "sha256:payload-barrier", reviewEvidenceAt(review.ProductReview, product, "rev-2", "PASS", "re-reviewed the candidate", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		acceptance, err := engine.Submit(ev, observedInput)
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
		// PRESERVED 决定重放完整原结果并清空隔离账，独立接纳才计语义轮。
		askAndDecideRequirement(t, engine, "req-barrier-result", "proceed", preserved)
		snap = engine.LoadMustSucceed(t)
		if len(snap.State.QuarantinedResults) != 0 {
			t.Fatalf("the decide must reclassify the quarantine ledger: %+v", snap.State.QuarantinedResults)
		}
		if snap.State.ReviewSeries.ProductReview.Completed != 0 {
			t.Fatal("quarantine replay must wait for independent semantic acceptance")
		}
		verifyOriginalFixtureReview(t, engine, ev)
		snap = engine.LoadMustSucceed(t)
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
		observedInput := Observation{}
		product := issueReady(t, engine)[0]
		request, err := NewRequirementRequestEvent("req-barrier-void", RequirementDecisionPackage{
			CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-void", Source: RequirementSourceUserInitiated,
			ImpactDigest:   "sha256:impact",
			Classification: RequirementClassificationChanged, RedoProductReview: true,
		}, requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-void", product, "agent-void"), observedInput); err != nil {
			t.Fatalf("spawn receipt under barrier: %v", err)
		}
		ev, err := NewReviewWorkerResultEvent("result-void", product, testProvider, "sha256:payload-void", reviewEvidence(review.ProductReview, product, "", "PASS", "bound to the superseded revision", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(ev, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
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
		freshReviewAfterRedo(t, engine, product)
		voided := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "superseded by the redo declared") {
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
		observedInput := Observation{}
		requirementAsk, err := NewRequirementRequestEvent("req-barrier-topology", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(requirementAsk, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		topologyAsk, err := NewRequestEvent("req-topology-frozen", ControlConfirmTopology, topologyOptions()...)
		if err != nil {
			t.Fatalf("topology ask: %v", err)
		}
		if _, err := engine.Submit(topologyAsk, observedInput); err != nil {
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
		if _, err := engine.Submit(ev, observedInput); err == nil {
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
		if _, err := engine.Submit(retry, observedInput); err == nil {
			t.Fatal("post-barrier topology decide accepted without readiness PASS")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("post-barrier topology code = %q", code)
		}
	})

	t.Run("host action intents are frozen", func(t *testing.T) {
		engine, dir, plan, _ := preparedEngine(t)
		observedInput := Observation{}
		if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, observedInput); err != nil {
			t.Fatalf("issue: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "evt-barrier-host-spawn", reviewAction, "agent-barrier-host"), observedInput); err != nil {
			t.Fatalf("spawn: %v", err)
		}
		request, err := NewRequirementRequestEvent("req-barrier-host", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine}, observedInput); err == nil {
			t.Fatal("host action intent persisted while the change ask is pending")
		} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
			t.Fatalf("frozen host action code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("frozen host recovery changed state bytes")
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
		if _, err := engine.Submit(decline, observedInput); err != nil {
			t.Fatalf("abort decide: %v", err)
		}
		if _, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine}, observedInput); err != nil {
			t.Fatalf("host action after the decide: %v", err)
		}
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.PendingHostActions) != 1 {
			t.Fatalf("host recovery intent after the decide: %+v", snap.State.PendingHostActions)
		}
		for _, intent := range snap.State.PendingHostActions {
			if intent.Operation != HostActionResumeAgent || intent.Resume == nil || intent.Resume.WorkerActionID != reviewAction || intent.Resume.Identity != "agent-barrier-host" {
				t.Fatalf("host recovery intent = %+v", intent)
			}
		}
	})

	t.Run("other pending asks do not freeze issuance", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, _ := reviewEngine(t, cd, reg)
		observedInput := Observation{}
		request, err := NewRequestEvent("req-abort-idle", ControlAbort, AskOption{ID: "cancel", Label: "取消终止"})
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		// 非 REQ_CHANGE 的未决 Ask 不构成屏障：业务签发照常。
		issueReady(t, engine)
	})
}

// TestRequirementDecisionValidationAndCAS：CAS 基线、载荷配对与 freshness
// 的负控——每例可区分错误码、零状态变化。
func TestRequirementDecisionValidationAndCAS(t *testing.T) {
	t.Parallel()
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
	observedInput := Observation{}
	request, err := NewRequirementRequestEvent("req-fresh", requirementPackageOf(&requirementPayload), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
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
	if _, err := engine.Submit(other, observedInput); err != nil {
		t.Fatalf("submit other request: %v", err)
	}
	expired := requirementDecideEvent(t, "req-fresh", oldToken, "proceed", &requirementPayload)
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(expired, observedInput); err == nil {
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
	observedInput := Observation{}
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
	if _, err := engine.Submit(request, observedInput); err != nil {
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
	_, err = engine.Submit(ev, observedInput)
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
	t.Parallel()
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
	t.Parallel()
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
// 路线。两条系列双双结论后签发闸不再出轮：无标记的第三步照常签出（返回
// 其在途派发），带标记的第三步被拒（返回空）——重走的新鲜轮由决定事务
// 的账本欠轮提案签出。
func walkToTopologyAndRoute(t *testing.T, engine *Engine) string {
	t.Helper()
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitVerifiedReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰",
	}})
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}
	if len(snap.State.PendingActions) == 0 {
		return ""
	}
	return pendingActionID(t, engine)
}

// TestCarriedProductConclusionReopensAfterReadinessOnlyRedo：用户只勾选重走
// start-readiness（产品审携原 revision 续用，§7.2.4 carry）——续用结论绑
// rev-1 时，rev-2 的新一轮 readiness 证据必须被顺序守卫放行，readiness
// 重新 PASS 后开发门重开（路线选择重新可接纳并推进 DEVELOPMENT_PARALLEL）。
func TestCarriedProductConclusionReopensAfterReadinessOnlyRedo(t *testing.T) {
	t.Parallel()
	t.Run("gate stays closed until the redone readiness series repasses", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
		engine, dir := reviewEngine(t, cd, reg)
		walkToTopologyAndRoute(t, engine)

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
		rejectRouteRequest(t, engine, dir, "req-route-closed-1", []string{"blackbox", "whitebox", "gate.quality", "gate.security"})

		// rev-2 的新一轮 readiness 证据被顺序守卫放行（续用的
		// 产品审 PASS 绑 rev-1 同样有效）；决定事务的欠轮提案已签出新鲜
		// 轮，直接提交证据完成。
		freshRoundFromTheLedgerProposal(t, engine, "start.readiness", "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
		snap = engine.LoadMustSucceed(t)
		if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Completed != 2 || readiness.Effective.Status != "PASS" || readiness.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("readiness fresh round at rev-2 = completed %d %+v", readiness.Completed, readiness.Effective)
		}
		// 门重开：路线选择接纳并推进 DEVELOPMENT_PARALLEL。
		askAndDecide(t, engine, ControlSelectRoute, "req-route-reopen", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
		snap = engine.LoadMustSucceed(t)
		if !snap.State.RouteConfirmed || snap.State.Phase != runtime.PhaseDevelopmentParallel {
			t.Fatalf("route = %t phase = %s, want confirmed/DEVELOPMENT_PARALLEL", snap.State.RouteConfirmed, snap.State.Phase)
		}
	})

	t.Run("literal repro at DEVELOPMENT_PARALLEL", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
		engine, _ := reviewEngine(t, cd, reg)
		walkToDevelopment(t, engine)
		askAndDecideRequirement(t, engine, "req-change-dev-repro", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-dev",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoStartReadiness: true, Reason: "readiness-only redo after development started",
		})
		// 决定事务的欠轮提案签出 rev-2 新鲜轮，直接提交证据完成。
		freshRoundFromTheLedgerProposal(t, engine, "start.readiness", "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, dir := reviewEngine(t, cd, reg)
	walkToTopologyAndRoute(t, engine)

	askAndDecideRequirement(t, engine, "req-change-product-only", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-product",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "only the product walk is affected",
	})
	rejectRouteRequest(t, engine, dir, "req-route-pending-1", []string{"blackbox", "whitebox", "gate.quality", "gate.security"})

	// 产品审在 rev-2 重新 PASS（决定事务的欠轮提案签出新鲜轮，直接提交
	// 证据完成）：携用的 readiness 不被级联失效、不耗新轮次，双审门直接
	// 重开。
	freshRoundFromTheLedgerProposal(t, engine, "product.review", "result-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-product-2", "rev-2", "PASS", "re-reviewed product", nil))
	snap := engine.LoadMustSucceed(t)
	if readiness := snap.State.ReviewSeries.StartReadiness; readiness.Effective.Status != "PASS" || readiness.UpstreamVoid || readiness.Completed != 1 || readiness.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the carried readiness must open the gate without a fresh round: %+v void=%v completed=%d", readiness.Effective, readiness.UpstreamVoid, readiness.Completed)
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-route-reopened", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
	snap = engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("route = %t phase = %s, want confirmed/DEVELOPMENT_PARALLEL", snap.State.RouteConfirmed, snap.State.Phase)
	}

}

// TestCarryValidityBindsToTheConfirmedLedger：结论 carry 有效性按确认账本
// 成员判定——账本外 revision 字符串上的 effective PASS 不开门（正控制：
// 账本内 revision 的 PASS 有效且顺序守卫放行）。
func TestCarryValidityBindsToTheConfirmedLedger(t *testing.T) {
	t.Parallel()
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
		state.ReviewSeries = review.AdmitRound(set, review.StartReadiness, "act:review/start.readiness", 0)
		return state
	}
	phantom := build("rev-phantom")
	if phantom.effectivePass(phantom.ReviewSeries.ProductReview) {
		t.Fatal("a PASS bound outside the confirmed ledger must not be a valid conclusion")
	}
	if _, err := phantom.admitReviewEvidence(&ReviewResultEvidence{
		Action: review.StartReadiness, DispatchID: "act:review/start.readiness", RequirementRevision: "rev-1",
		Status: "PASS",
	}, reviewVerification(), "act:review/start.readiness", 0); err == nil {
		t.Fatal("the sequence guard opened behind an out-of-ledger PASS")
	} else if code := rejectionCode(t, err); code != CodeReviewSequenceGate {
		t.Fatalf("out-of-ledger guard code = %q", code)
	}

	carried := build("rev-1")
	if !carried.effectivePass(carried.ReviewSeries.ProductReview) {
		t.Fatal("a PASS bound to a confirmed revision must stay valid")
	}
	if _, err := carried.admitReviewEvidence(&ReviewResultEvidence{
		Action: review.StartReadiness, DispatchID: "act:review/start.readiness", RequirementRevision: "rev-1",
		Status: "PASS",
	}, reviewVerification(), "act:review/start.readiness", 0); err != nil {
		t.Fatalf("the sequence guard must admit behind a confirmed-revision PASS: %v", err)
	}
}

// TestForgedCarriedPassRejectedAtLoad：在账本内 revision 上伪造 effective
// PASS（实际投影为 FAIL）——证据链断裂由加载边界的既有重投影校验拦截。
func TestForgedCarriedPassRejectedAtLoad(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
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
	t.Parallel()
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
	observedInput := Observation{}
	request, err := NewRequirementRequestEvent("req-anchor-defer", valid.packageView(), AskOption{ID: "defer", Label: "推迟"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	token, err := engine.Freshness("req-anchor-defer")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(requirementDecideEvent(t, "req-anchor-defer", token, "defer", &valid), observedInput); err == nil {
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "start.readiness.two")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitVerifiedReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "FAIL", "advisory", []review.Finding{
		{Severity: "P2", Category: "acceptance-entry", Message: "advisory concern on the carried conclusion"},
	}))
	settleFixtureFinding(t, engine, review.StartReadiness, "req-confirm-readiness-advisory", "CONFIRM")
	readinessCandidate := engine.LoadMustSucceed(t).State.ReviewSeries.StartReadiness.RawCandidates[0]
	askAndDecideRequirement(t, engine, "req-clarify-readiness", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-clarified", DeltaDigest: "sha256:readiness-clarification",
		ImpactDigest: "sha256:impact", Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "user confirms the complete acceptance-entry wording correction", FindingDigest: readinessCandidate.Findings[0].Digest,
	})
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆",
	}})
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality"}, Gates: []string{"blackbox", "whitebox", "gate.quality"}}})

	// 只重走产品审：readiness 携用（绑 rev-1、带一条建议项）。
	askAndDecideRequirement(t, engine, "req-change-carry", "proceed", &RequirementDecisionPayload{
		BaseRevision: engine.LoadMustSucceed(t).State.RequirementRevision(), CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-carry",
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
		Action: review.StartReadiness, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
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
		Action: review.StartReadiness, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Supersedes: review.LatestDecisionID(carried, candidate.ResultDigest, candidate.Findings[0].ID), Reason: "user rejects the advisory on the carried conclusion", ExpectedEffectiveRevision: carried.Effective.Revision,
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
	t.Parallel()
	newEngineWithFinding := func(t *testing.T, severity string) (*Engine, string, string) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
			{Severity: severity, Message: "finding under adjudication"},
		}))
		confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
		snap := engine.LoadMustSucceed(t)
		candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
		askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-label", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
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
	t.Parallel()
	t.Run("redone series staging is terminated and the replacement completes", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		observedInput := Observation{}
		product := issueReady(t, engine)[0]
		staged, err := NewReviewWorkerResultEvent("result-staged-old", product, testProvider, "sha256:payload-staged-old", reviewEvidence(review.ProductReview, product, "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(staged, observedInput); err != nil || acceptance.Status != "STAGED" {
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
		replacement := freshReviewAfterRedo(t, engine, product)
		if _, voided := snap.State.ObsoleteResults[product]; !voided {
			t.Fatalf("the terminated staging must be kept as late evidence: %+v", snap.State.ObsoleteResults)
		}
		terminated := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "superseded by the redo declared") {
				terminated = true
			}
		}
		if !terminated {
			t.Fatalf("no termination trail for %s: %+v", product, snap.State.RecoveryRecords)
		}

		// 替换派发（≥floor）完成新鲜轮：completed=1、effective 绑 rev-2。
		if _, err := engine.Submit(spawnReceipt(t, "spawn-fresh-staging", replacement, "agent-fresh-staging"), observedInput); err != nil {
			t.Fatalf("replacement spawn: %v", err)
		}
		fresh, err := NewReviewWorkerResultEvent("result-fresh", replacement, testProvider, "sha256:payload-fresh", reviewEvidenceAt(review.ProductReview, replacement, "rev-2", "PASS", "re-reviewed product", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if acceptance, err := engine.Submit(fresh, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("fresh result on the replacement: %v %+v", err, acceptance)
		}
		verifyOriginalFixtureReview(t, engine, fresh)
		snap = engine.LoadMustSucceed(t)
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("fresh result did not reach the ledger: completed=%d %+v", series.Completed, series.Effective)
		}
	})

	t.Run("out-of-scope staging is retained", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "start.readiness.two")
		engine, _ := reviewEngine(t, cd, reg)
		observedInput := Observation{}
		product := issueReady(t, engine)[0]
		submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		staged, err := NewReviewWorkerResultEvent("result-staged-kept", readiness, testProvider, "sha256:payload-staged-kept", reviewEvidence(review.StartReadiness, readiness, "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if _, err := engine.Submit(staged, observedInput); err != nil {
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
		if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-kept-late", readiness, "agent-kept-late"), observedInput); err != nil || acceptance.Status != "ACCEPTED" {
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
		observedInput := Observation{}
		product := issueReady(t, engine)[0]
		staged, err := NewReviewWorkerResultEvent("result-staged-preserved", product, testProvider, "sha256:payload-staged-preserved", reviewEvidence(review.ProductReview, product, "", "PASS", "bound before the change", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if _, err := engine.Submit(staged, observedInput); err != nil {
			t.Fatalf("stage: %v", err)
		}

		// PRESERVED 修订：未勾重走即续用——旧 revision 暂存保留，迟到回
		// 执按 carry 语义携原 revision 配对，再独立接纳计轮。
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
		if _, err := engine.Submit(spawnReceipt(t, "spawn-preserved-late", product, "agent-preserved-late"), observedInput); err != nil {
			t.Fatalf("late spawn receipt: %v", err)
		}
		verifyOriginalFixtureReview(t, engine, staged)
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
// 绝零状态）；未准备包的请求只建立屏障。
func TestRequirementPackageBindsTheAsk(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	observedInput := Observation{}
	confirmed := RequirementDecisionPackage{
		CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-package",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationPreserved,
	}
	request, err := NewRequirementRequestEvent("req-package", confirmed, requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
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
		if _, err := engine.Submit(ev, observedInput); err == nil {
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

	// A package-free request starts the barrier; proceeding still requires
	// a complete package bound to this same Ask before confirmation.
	if _, err := NewRequestEvent("req-no-package", ControlRequestRequirementChange, requirementOptions()...); err != nil {
		t.Fatalf("package-free barrier request: %v", err)
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	walkToTopologyAndRoute(t, engine)

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
	// 重走完成：readiness 在 rev-2 重新 PASS（决定事务的欠轮提案签出新鲜
	// 轮，直接提交证据完成），门重新打开（路线接纳）。
	freshRoundFromTheLedgerProposal(t, engine, "start.readiness", "result-readiness-2", reviewEvidenceAt(review.StartReadiness, "dispatch-readiness-2", "rev-2", "PASS", "re-reviewed readiness", nil))
	askAndDecide(t, engine, ControlSelectRoute, "req-route-after-preserved-scope", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}}})
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
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
// series 后，重走前签发的旧派发不能完成"新鲜复审"——语义判断步不携带
// 任何审查系列身份，其上的审查证据与签发标记冲突，可区分拒绝、零状态
// （重走范围派发在决定事务内按标记终止并由替换派发完成新鲜轮，见
// TestMarkedReviewDispatchWaitsForExactStopAfterRedo）。
func TestRedoneSeriesFreshRoundRequiresAPostDecisionDispatch(t *testing.T) {
	t.Parallel()
	cd, reg := declaredSeriesStepsFixture(t, "review.floor.one", "review.floor.two", "review.floor.three")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]

	work, err := engine.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	askAndDecideRequirement(t, engine, "req-redo-floor", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-floor",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "redo the product review",
		WorkDecisions: []RequirementWorkDecision{{Work: work[0], Disposition: RequirementWorkCarry, Reason: "the semantic worker remains applicable"}},
	})
	snap := engine.LoadMustSucceed(t)
	if _, pending := snap.State.PendingActions[product]; !pending {
		t.Fatal("setup: the in-flight action disappeared before any result")
	}
	floor := snap.State.RequirementRevisions[len(snap.State.RequirementRevisions)-1].StateRevision
	if snap.State.ReviewSeries.ProductReview.FreshFloor != floor {
		t.Fatalf("fresh floor = %d, want the decide revision %d", snap.State.ReviewSeries.ProductReview.FreshFloor, floor)
	}
	// 旧派发（pre-floor）的声明证据不再被接纳：未标记派发没有可绑定的
	// 系列身份，证据与签发标记冲突即可区分拒绝、零状态。
	evidence := reviewEvidenceAt(review.ProductReview, product, "rev-2", "PASS", "old dispatch completing the fresh round", nil)
	ev, err := NewReviewWorkerResultEvent("result-floor-fresh", product, testProvider, "sha256:payload-result-floor-fresh", evidence)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(ev, Observation{}); err == nil {
		t.Fatal("the pre-floor dispatch's declared evidence was accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), `issued for the "" review series`) {
		t.Fatalf("pre-floor declared evidence code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("the rejected declared evidence changed state bytes")
	}
}

// TestUnredoneSeriesQuarantinedResultIsCarried：未勾重走的 series，其屏障
// 期隔离结果按 carry 恢复——携原 revision、原证据字节接纳完成（completed
// +1），留 carry 留痕，不作废。
func TestUnredoneSeriesQuarantinedResultIsCarried(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	readiness := func() string {
		product := issueReady(t, engine)[0]
		submitVerifiedReviewResult(t, engine, "result-carry-product", product, reviewEvidence(review.ProductReview, "dispatch-carry-product", "", "PASS", "clean", nil))
		return pendingActionID(t, engine)
	}()
	observedInput := Observation{}

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
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-carry", readiness, "agent-carry"), observedInput); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-carry", readiness, testProvider, "sha256:payload-carry",
		reviewEvidence(review.StartReadiness, readiness, "", "PASS", "in-flight against rev-1", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine = %+v %v", acceptance, err)
	}
	askAndDecideRequirement(t, engine, "req-carry", "proceed", preserved)
	verifyCompleteFixtureReview(t, engine, readiness)
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	observedInput := Observation{}

	for _, requestID := range []string{"req-held-first", "req-held-second"} {
		request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request %s: %v", requestID, err)
		}
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-held", product, "agent-held"), observedInput); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-held", product, testProvider, "sha256:payload-held",
		reviewEvidence(review.ProductReview, product, "", "PASS", "quarantined while two asks pend", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
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
	// 最后一个 Ask 解决：重放原结果，独立接纳仍须单独完成。
	abortExistingAsk(t, engine, "req-held-second")
	snap = engine.LoadMustSucceed(t)
	if len(snap.State.QuarantinedResults) != 0 || snap.State.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatalf("the last decide must replay the quarantine: held=%d completed=%d",
			len(snap.State.QuarantinedResults), snap.State.ReviewSeries.ProductReview.Completed)
	}
	verifyOriginalFixtureReview(t, engine, quarantined)
	if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("the independently accepted quarantined review = %+v", series)
	}
}

// FreshOwed keeps a redo pending through overlapping requirement-change asks.
func TestDualAskRedoIssuesFreshWorkOnlyAfterTheBarrierLifts(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	original := engine.LoadMustSucceed(t).State.PendingActions[product]
	observedInput := Observation{}

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
		if _, err := engine.Submit(request, observedInput); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
	}
	token, err := engine.Freshness("req-defer-first")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	if _, err := engine.Submit(requirementDecideEvent(t, "req-defer-first", token, "proceed", redoPayload), observedInput); err != nil {
		t.Fatalf("decide within the barrier: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if _, stillPending := snap.State.PendingActions[product]; !stillPending || len(snap.State.PendingActions) != 1 {
		t.Fatal("the superseded original execution must remain pending until its exact stop")
	}
	if _, known := snap.State.ObsoleteActions[product]; !known {
		t.Fatal("the old dispatch must be marked superseded while the barrier holds")
	}
	if !snap.State.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("the barrier must retain the fresh-review obligation")
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-deferred-original", product, "worker:"+product), observedInput); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []string{LifecycleStart, LifecycleStop} {
		event, err := NewCorrelatedLifecycleEvent(EventID("deferred-original:"+edge), testProvider, "worker:"+product, "worker:"+product, edge)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(event, observedInput); err != nil {
			t.Fatal(err)
		}
	}
	snap = engine.LoadMustSucceed(t)
	if receipt := snap.State.SpawnReceipts[product]; receipt.ActionID != product || receipt.Task != original.Task || receipt.Provider != testProvider || receipt.Correlation != "worker:"+product || !reflect.DeepEqual(snap.State.lifecycleMatches(testProvider, receipt.Correlation), []string{"worker:" + product}) {
		t.Fatal("barrier lost the original dispatch's exact receipt and lifecycle pair")
	}
	if !reflect.DeepEqual(snap.State.PendingActions[product], original) || len(snap.State.PendingActions) != 1 || snap.State.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatal("barrier must hold the original action and attempt without replacement or counting")
	}
	abortExistingAsk(t, engine, "req-defer-second")
	fresh := freshReviewAfterRedo(t, engine, product)
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Issued != 2 || len(snap.State.PendingActions) != 1 || snap.State.DispatchIssueRevisions[fresh] < series.FreshFloor {
		t.Fatalf("barrier lift must issue one fresh task at the floor: %+v", series)
	}
}

// TestRequirementAskExpiresWhenItsBaseRevisionIsSuperseded：Ask 落账创建时
// 绑定权威基线；后续确认使基线前移后，旧 Ask 的决定（即使把载荷基线改
// 成新权威）以可区分错误拒绝且零状态——必须在新基线上重建 Ask。
func TestRequirementAskExpiresWhenItsBaseRevisionIsSuperseded(t *testing.T) {
	t.Parallel()
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
	observedInput := Observation{}
	secondRequest, err := NewRequirementRequestEvent("req-expire-second", secondPackage, requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(secondRequest, observedInput); err != nil {
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
	if _, err := engine.Submit(rebasedEvent, observedInput); err == nil {
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	snap := engine.LoadMustSucceed(t)
	task := snap.State.PendingActions[product].Task
	attempt := snap.State.PendingActions[product].AttemptID
	observedInput := Observation{}

	request, err := NewRequirementRequestEvent("req-replay-terminal", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-replay-terminal", product, "agent-replay-terminal"), observedInput); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-replay-terminal", product, testProvider, "sha256:payload-replay-terminal",
		reviewEvidence(review.ProductReview, product, "", "PASS", "quarantined then the task reports terminal", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine: %+v %v", acceptance, err)
	}
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID("task-replay-terminal-"+string(rune('a'+i))), task, attempt, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, observedInput); err != nil {
			t.Fatalf("task progress %s during barrier: %v", status, err)
		}
	}
	abortExistingAsk(t, engine, "req-replay-terminal")
	verifyCompleteFixtureReview(t, engine, product)
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "readiness.again")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	observedInput := Observation{}

	request, err := NewRequirementRequestEvent("req-digest-identity", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-digest", product, "agent-digest"), observedInput); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	first, err := NewReviewWorkerResultEvent("evt-digest-first", product, testProvider, "sha256:payload-digest",
		reviewEvidence(review.ProductReview, product, "", "PASS", "digest identity", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(first, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
		t.Fatalf("quarantine: %+v %v", acceptance, err)
	}
	abortExistingAsk(t, engine, "req-digest-identity")
	verifyCompleteFixtureReview(t, engine, product)
	snap := engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Completed != 1 {
		t.Fatalf("the replayed result did not complete: completed=%d", snap.State.ReviewSeries.ProductReview.Completed)
	}
	replay, err := NewReviewWorkerResultEvent("evt-digest-second", product, testProvider, "sha256:payload-digest",
		reviewEvidence(review.ProductReview, product, "", "PASS", "digest identity", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	acceptance, err := engine.Submit(replay, Observation{})
	if err != nil || acceptance.Status != "DUPLICATE" {
		t.Fatalf("same payload under a new event id = %v %+v (want DUPLICATE)", err, acceptance)
	}
}

// TestForgedFreshFloorRejectedAtLoad：series 的 fresh floor 只认确认账本
// （覆盖该 series 的最后一次决定的 state 代数）——手改 floor 在加载边界
// 可区分拒绝，恢复原状即照常加载。
func TestForgedFreshFloorRejectedAtLoad(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	if authoring.ReviewSeriesProductReview != authoring.ReviewSeriesAction(review.ProductReview) ||
		authoring.ReviewSeriesStartReadiness != authoring.ReviewSeriesAction(review.StartReadiness) {
		t.Fatalf("authoring marks %q/%q must equal the review kernel actions %q/%q",
			authoring.ReviewSeriesProductReview, authoring.ReviewSeriesStartReadiness, review.ProductReview, review.StartReadiness)
	}
}

// TestMarkedReviewDispatchWaitsForExactStopAfterRedo：定义在签发时断言
// 审查系列身份的派发，重走决定登记终止义务，原执行精确停定后替换派发
// （携标记、签发代数＝停定事务）完成新鲜轮；未勾选系列的派发保持在途。
func TestMarkedReviewDispatchWaitsForExactStopAfterRedo(t *testing.T) {
	t.Parallel()
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
		if _, stillPending := snap.State.PendingActions[product]; !stillPending {
			t.Fatal("the original review was retired before observing its stop")
		}
		stopRevision := stopSupersededDispatch(t, engine, product)
		freshID := freshReviewAfterRedo(t, engine, product)
		snap = engine.LoadMustSucceed(t)
		if len(snap.State.ObsoleteResults) != 0 {
			t.Fatalf("a dispatch without any result must not void results: %+v", snap.State.ObsoleteResults)
		}
		replacementPending, ok := snap.State.PendingActions[freshID]
		if !ok || replacementPending.ReviewSeries != review.ProductReview {
			t.Fatalf("the replacement must inherit the series mark: %+v", replacementPending)
		}
		if issued := snap.State.DispatchIssueRevisions[freshID]; issued != stopRevision {
			t.Fatalf("the replacement must be issued in the original stop transaction %d: issued=%d", stopRevision, issued)
		}
		terminated := false
		for _, record := range snap.State.RecoveryRecords {
			if record.ActionID == product && strings.Contains(record.Detail, "superseded by the redo declared in requirement decision req-marked-redo") {
				terminated = true
			}
		}
		if !terminated {
			t.Fatalf("no termination trail: %+v", snap.State.RecoveryRecords)
		}
		// 替换派发（≥floor、携标记）完成新鲜轮。
		fresh, err := NewReviewWorkerResultEvent("result-marked-fresh", freshID, testProvider, "sha256:payload-marked-fresh",
			reviewEvidenceAt(review.ProductReview, freshID, "rev-2", "PASS", "fresh round on the marked replacement", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		observedInput := Observation{}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-marked-fresh", freshID, "agent-marked-fresh"), observedInput); err != nil {
			t.Fatalf("spawn: %v", err)
		}
		if acceptance, err := engine.Submit(fresh, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("fresh round on the replacement: %v %+v", err, acceptance)
		}
		verifyOriginalFixtureReview(t, engine, fresh)
		snap = engine.LoadMustSucceed(t)
		if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
			t.Fatalf("fresh round on the marked replacement = completed %d %+v", series.Completed, series.Effective)
		}
	})

	t.Run("marked dispatch of an unchecked series stays in flight", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitVerifiedReviewResult(t, engine, "result-unchecked-product", product, reviewEvidence(review.ProductReview, "dispatch-unchecked-product", "", "PASS", "clean", nil))
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	observedInput := Observation{}

	if _, err := engine.Submit(spawnReceipt(t, "spawn-mismatch", product, "agent-mismatch"), observedInput); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	mismatched, err := NewReviewWorkerResultEvent("result-mismatch", product, testProvider, "sha256:payload-mismatch",
		reviewEvidence(review.StartReadiness, product, "", "PASS", "declares the wrong series", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(mismatched, observedInput); err == nil {
		t.Fatal("mismatched evidence accepted on a marked dispatch")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), `issued for the "product-review"`) {
		t.Fatalf("mismatch code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("the mismatched evidence changed state bytes")
	}
	// 回执已配对：同 action 的相符证据保存后由独立 actor 接纳。
	matched, err := NewReviewWorkerResultEvent("result-match", product, testProvider, "sha256:payload-match",
		reviewEvidence(review.ProductReview, product, "", "PASS", "declares the marked series", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(matched, observedInput); err != nil {
		t.Fatalf("matching evidence rejected: %v", err)
	}
	verifyOriginalFixtureReview(t, engine, matched)
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
	t.Parallel()
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
	observedInput := Observation{}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-unaffiliated", gate, "agent-unaffiliated"), observedInput); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	for name, action := range map[string]review.Action{
		"product-review":  review.ProductReview,
		"start-readiness": review.StartReadiness,
	} {
		mismatched, err := NewReviewWorkerResultEvent(EventID("result-unaffiliated-"+name), gate, testProvider, "sha256:payload-unaffiliated-"+name,
			reviewEvidence(action, gate, "", "PASS", "predevelopment evidence on an unaffiliated dispatch", nil))
		if err != nil {
			t.Fatalf("event %s: %v", name, err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(mismatched, observedInput); err == nil {
			t.Fatalf("%s evidence accepted on an unaffiliated dispatch", name)
		} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected || !strings.Contains(err.Error(), `issued for the "unaffiliated"`) {
			t.Fatalf("%s mismatch code = %q (%v)", name, code, err)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatalf("%s mismatched evidence changed state bytes", name)
		}
	}

	// 非开发前系列不属于重走范围：产品审重走决定不终止该派发。
	work, err := engine.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	askAndDecideRequirement(t, engine, "req-unaffiliated-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-unaffiliated",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the product review is redone",
		WorkDecisions: []RequirementWorkDecision{{Work: work[0], Disposition: RequirementWorkCarry, Reason: "the gate is unaffected by the changed product review"}},
	})
	snap = engine.LoadMustSucceed(t)
	if pending, ok := snap.State.PendingActions[gate]; !ok || pending.ReviewSeries != review.Action(authoring.ReviewSeriesUnaffiliated) {
		t.Fatalf("the unaffiliated dispatch must stay in flight across a predevelopment redo: %+v", pending)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestLateReceiptPairsWhenTheProductGateReopens：回执是无条件观察事实、
// 配对是惰性业务动作——只勾重走产品审时，就绪系列的暂存结果其迟到回执
// 在产品审重走期照常 ACCEPTED 落账（配对挂起、暂存保留、无重试请求）；
// 另一派发完成产品审新鲜轮（effective PASS@rev-2）的同一事务内，挂起配
// 对自动保存原结果，独立接纳后计轮；同回执重发判 DUPLICATE、不双计。
func TestLateReceiptPairsWhenTheProductGateReopens(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	observedInput := Observation{}
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, product, "", "PASS", "clean", nil))
	stagedSlot := pendingActionByStep(t, engine, "start.readiness")
	var snap Snapshot

	// 就绪系列的下一轮证据先到（暂存，回执未到）。
	staged, err := NewReviewWorkerResultEvent("result-pair-staged", stagedSlot, testProvider, "sha256:payload-pair-staged",
		reviewEvidence(review.StartReadiness, stagedSlot, "", "PASS", "readiness round before the change", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, observedInput); err != nil || acceptance.Status != "STAGED" {
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
	if acceptance, err := engine.Submit(receipt, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("late receipt during the redo = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	if _, kept := snap.State.StagedResults[stagedSlot]; !kept {
		t.Fatalf("the suspended pairing must keep the staging: %+v", snap.State.StagedResults)
	}
	if _, admitted := snap.State.Results[stagedSlot]; admitted {
		t.Fatal("the suspended round must not be admitted yet")
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 0 {
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

	// 产品审新鲜轮由重走决定的欠轮提案签出（同一决定事务、签发代数＝决
	// 定）：其 PASS@rev-2 的同一事务内挂起配对自动完成。
	freshSlot := pendingActionByStep(t, engine, "product.review")
	submitVerifiedReviewResult(t, engine, "result-pair-product", freshSlot, reviewEvidenceAt(review.ProductReview, freshSlot, "rev-2", "PASS", "fresh product round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("product fresh round = completed %d %+v", series.Completed, series.Effective)
	}
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 0 {
		t.Fatalf("the suspended pairing counted before independent acceptance: completed=%d", series.Completed)
	}
	if _, admitted := snap.State.Results[stagedSlot]; !admitted {
		t.Fatal("the paired round did not reach the results ledger")
	}
	if _, kept := snap.State.StagedResults[stagedSlot]; kept {
		t.Fatalf("the paired round must leave staging: %+v", snap.State.StagedResults)
	}
	verifyOriginalFixtureReview(t, engine, staged)
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("the independently accepted paired round = completed %d %+v", series.Completed, series.Effective)
	}

	// 同回执字节换事件 ID 重发判 DUPLICATE；已完成的配对不双计。
	duplicateReceipt := spawnReceipt(t, "spawn-pair-replay", stagedSlot, "agent-pair-late")
	if acceptance, err := engine.Submit(duplicateReceipt, Observation{}); err != nil || acceptance.Status != "DUPLICATE" {
		t.Fatalf("same-bytes receipt under a new event id = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 {
		t.Fatalf("the replayed receipt double-counted: completed=%d", series.Completed)
	}
}

// TestQuarantinedReadinessUnderProductRedoStaysSuspended：重放路径与回执
// 配对同享惰性语义——产品审重走期隔离的就绪结果，屏障解除的重放在顺序
// 门未开时回暂存挂起（不作废、不计轮），留挂起留痕。
func TestQuarantinedReadinessUnderProductRedoStaysSuspended(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	observedInput := Observation{}
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-qap-product", product, reviewEvidence(review.ProductReview, "dispatch-qap-product", "", "PASS", "clean", nil))
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
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-qap", readiness, "agent-qap"), observedInput); err != nil {
		t.Fatalf("receipt during barrier: %v", err)
	}
	quarantined, err := NewReviewWorkerResultEvent("evt-qap", readiness, testProvider, "sha256:payload-qap",
		reviewEvidence(review.StartReadiness, readiness, "", "PASS", "in-flight against rev-1", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(quarantined, observedInput); err != nil || acceptance.Status != "QUARANTINED" {
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
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, dir := reviewEngine(t, cd, reg)

	// 产品审第一轮：PASS 携一条 P2 建议项；技术审 PASS 后走到拓扑。
	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-owed-product-1", product, reviewEvidence(review.ProductReview, "dispatch-owed-1", "", "PASS", "clean round", []review.Finding{
		{Severity: "P2", Message: "advisory noise in the old round"},
	}))
	settleFixtureFinding(t, engine, review.ProductReview, "req-confirm-old-advisory", "CONFIRM")
	productCandidate := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.RawCandidates[0]
	askAndDecideRequirement(t, engine, "req-clarify-old-advisory", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-clarified", DeltaDigest: "sha256:old-advisory-clarification",
		ImpactDigest: "sha256:impact", Source: RequirementSourceReviewFindingP2, Classification: RequirementClassificationPreserved,
		Reason: "user confirms the complete bounded correction of the advisory wording", FindingDigest: productCandidate.Findings[0].Digest,
	})
	readiness := pendingActionID(t, engine)
	submitVerifiedReviewResult(t, engine, "result-owed-readiness-1", readiness, reviewEvidenceAt(review.StartReadiness, readiness, engine.LoadMustSucceed(t).State.RequirementRevision(), "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-owed-topo", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single unit"}})
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]

	// 确认重走产品审：义务置位，在途产品派发退位＋替换。
	askAndDecideRequirement(t, engine, "req-owed-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: snap.State.RequirementRevision(), CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-owed",
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
		Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "DISMISS", Supersedes: review.LatestDecisionID(snap.State.ReviewSeries.ProductReview, candidate.ResultDigest, candidate.Findings[0].ID), Reason: "the advisory noise is accepted", ExpectedEffectiveRevision: snap.State.ReviewSeries.ProductReview.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the correction must keep refreshing the projection: %+v", series.Effective)
	}
	if !series.FreshOwed {
		t.Fatal("a correction of the old round must not discharge the redo obligation")
	}
	rejectRouteRequest(t, engine, dir, "req-owed-route-closed", []string{"blackbox", "whitebox", "g1", "g2"})
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

	// 满足 floor 的新鲜轮完成（重走决定与纠正的事务各提案一轮，按步定位
	// 任一在途新鲜轮）：义务清偿，双审门重开。
	freshRoundFromTheLedgerProposal(t, engine, "product.review", "result-owed-product-2", reviewEvidenceAt(review.ProductReview, "dispatch-owed-2", "rev-2", "PASS", "fresh round of the changed requirement", nil))
	snap = engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("the satisfied fresh round must discharge the obligation")
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-owed-route-open", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "g1", "g2"}, Gates: []string{"blackbox", "whitebox", "g1", "g2"}}})
	if snap = engine.LoadMustSucceed(t); snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase = %s, want DEVELOPMENT_PARALLEL after the obligation cleared", snap.State.Phase)
	}
}

// Redo supersedes a terminal task and sets a persistent evidence floor. Its
// native execution stops before replacement; obsolete evidence remains evidence only.
func TestCarriedEvidenceBelowTheRedoFloorIsRejected(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	old := issueReady(t, engine)[0]
	submitSpawnReceipt(t, engine, "floor-old-spawn", old)
	terminalizeDispatchTask(t, engine, old)
	redoProductReview(t, engine, "floor-change")
	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if !series.FreshOwed || series.FreshFloor == 0 || snap.State.DispatchIssueRevisions[old] >= series.FreshFloor {
		t.Fatalf("redo must establish a fresh evidence floor: %+v", series)
	}
	if _, obsolete := snap.State.ObsoleteActions[old]; !obsolete {
		t.Fatal("redo must retire a terminal dispatch before issuing replacement review")
	}
	fresh := freshReviewAfterRedo(t, engine, old)
	snap = engine.LoadMustSucceed(t)
	if fresh == old || snap.State.DispatchIssueRevisions[fresh] < series.FreshFloor {
		t.Fatalf("replacement must be issued at or after the floor: %q", fresh)
	}
	rejectOldRevision := func(eventID, action string) {
		t.Helper()
		before := stateBytesOf(t, dir)
		event, err := NewReviewWorkerResultEvent(EventID(eventID), action, testProvider, "sha256:"+eventID,
			reviewEvidence(review.ProductReview, action, "", "PASS", "obsolete requirement revision", nil))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(event, Observation{}); err == nil || rejectionCode(t, err) != CodeReviewEvidenceRejected {
			t.Fatalf("pre-floor requirement accepted: %v", err)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected evidence changed state")
		}
	}
	rejectOldRevision("floor-stale-during-redo", fresh)
	revision := snap.State.RequirementRevision()
	submitVerifiedReviewResult(t, engine, "floor-fresh-result", fresh,
		reviewEvidenceAt(review.ProductReview, fresh, revision, "FAIL", "fresh review blocks", []review.Finding{
			{Severity: "P1", Message: "changed requirement omits core acceptance"},
		}))
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	candidate := series.RawCandidates[0]
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	askAndDecide(t, engine, ControlReviewFindingDecision, "floor-confirm", []AskOption{confirm}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: fresh, RequirementRevision: revision,
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "acceptance cannot succeed", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	late, err := NewReviewWorkerResultEvent("floor-late-obsolete", old, testProvider, "sha256:floor-late-obsolete",
		reviewEvidence(review.ProductReview, old, "", "PASS", "late old conclusion", nil))
	if err != nil {
		t.Fatal(err)
	}
	acceptance, err := engine.Submit(late, Observation{})
	if err != nil || acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late obsolete result = %+v, %v", acceptance, err)
	}
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.FreshOwed || !review.HasConfirmedBlocking(series) || series.Completed != 1 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("late evidence changed the confirmed conclusion: %+v", series)
	}
	if _, authoritative := snap.State.Results[old]; authoritative || len(snap.State.ObsoleteResults[old]) != 1 || snap.State.ObsoleteResults[old][0].Review == nil {
		t.Fatal("late evidence must be retained only in the obsolete ledger")
	}
	// Invalidating the current result owes a new review after the redo obligation
	// was discharged. Its persistent floor still rejects the old revision.
	invalidateRoundOf(t, engine, "floor-invalid-current", fresh)
	if engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("result invalidation must not recreate a requirement-redo obligation")
	}
	rejectOldRevision("floor-stale-after-discharge", pendingActionID(t, engine))
	assertSeriesCommitmentHeld(t, engine)
}

// A review at the floor clears the redo obligation. A subsequent replacement
// for an invalid result may carry that confirmed revision after discharge.
func TestCarriedEvidenceAtTheRedoFloorIsAdmitted(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	old := issueReady(t, engine)[0]
	redoProductReview(t, engine, "atfloor-change")
	first := freshReviewAfterRedo(t, engine, old)
	snap := engine.LoadMustSucceed(t)
	floor := snap.State.ReviewSeries.ProductReview.FreshFloor
	if snap.State.DispatchIssueRevisions[first] < floor {
		t.Fatal("fresh review predates redo floor")
	}
	revision := snap.State.RequirementRevision()
	submitVerifiedReviewResult(t, engine, "atfloor-first", first, reviewEvidenceAt(review.ProductReview, first, revision, "PASS", "reviewed changed revision", nil))
	invalidateRoundOf(t, engine, "atfloor-invalid", first)
	second := pendingActionID(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.FreshOwed || series.FreshFloor != floor {
		t.Fatalf("result replacement must retain the discharged floor: %+v", series)
	}
	submitVerifiedReviewResult(t, engine, "atfloor-second", second, reviewEvidenceAt(review.ProductReview, second, revision, "PASS", "replacement review at confirmed revision", nil))
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if len(series.RawCandidates) != 2 || series.Completed != 1 || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != revision {
		t.Fatalf("replacement review was not accepted after discharge: %+v", series)
	}
	if snap.State.Phase != runtime.PhaseTechnicalReview {
		t.Fatalf("phase = %s, want TECHNICAL_REVIEW", snap.State.Phase)
	}
	assertSeriesCommitmentHeld(t, engine)
}

// TestBarrierHoldsTheReceiptPairingUntilTheDecision：屏障前暂存而屏障内收
// 回执——回执是观察事实照常 ACCEPTED 落账，暂存保留、配对挂起（留痕），
// 计轮为零；决定解除屏障补配对，独立接纳才计语义轮。
func TestBarrierHoldsTheReceiptPairingUntilTheDecision(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	observedInput := Observation{}

	staged, err := NewReviewWorkerResultEvent("result-barrier-staged", product, testProvider, "sha256:payload-barrier-staged",
		reviewEvidence(review.ProductReview, product, "", "PASS", "staged before the barrier", nil))
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, observedInput); err != nil || acceptance.Status != "STAGED" {
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
	if _, err := engine.Submit(request, observedInput); err != nil {
		t.Fatalf("submit request: %v", err)
	}

	// 屏障内迟到回执：观察事实照常落账；配对挂起、暂存保留、计轮为零。
	receipt := spawnReceipt(t, "spawn-barrier-pair", product, "agent-barrier-pair")
	if acceptance, err := engine.Submit(receipt, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
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

	// 决定解除屏障：同一事务内补配对，独立接纳才计语义轮。
	askAndDecideRequirement(t, engine, "req-barrier-pair", "proceed", preserved)
	snap = engine.LoadMustSucceed(t)
	if _, kept := snap.State.StagedResults[product]; kept {
		t.Fatalf("the lift must pair the suspended staging: %+v", snap.State.StagedResults)
	}
	if _, admitted := snap.State.Results[product]; !admitted {
		t.Fatal("the paired result must reach Results in the lifting transaction")
	}
	if snap.State.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatal("physical receipt pairing must not count a semantic round")
	}
	verifyOriginalFixtureReview(t, engine, staged)
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("the paired round = completed %d %+v", series.Completed, series.Effective)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestTerminalFreshDispatchResultAdmitsAtItsRealIssueRevision：替换派发
// （签发代数＝原执行停定事务）先 RUNNING→VALIDATING→TERMINAL 再交合法 fresh 结
// 果——terminal 对账以持久签发事实过 floor 校验（真实代数 ≥floor），新鲜
// 轮照常完成。
func TestTerminalFreshDispatchResultAdmitsAtItsRealIssueRevision(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]

	askAndDecideRequirement(t, engine, "req-terminal-fresh", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-terminal-fresh",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the product walk is redone",
	})
	replacement := freshReviewAfterRedo(t, engine, product)
	snap := engine.LoadMustSucceed(t)
	series, _ := snap.State.ReviewSeries.Series(review.ProductReview)
	if issued := snap.State.DispatchIssueRevisions[replacement]; issued < series.FreshFloor {
		t.Fatalf("the replacement must be issued at or after the floor: issued=%d floor=%d", issued, series.FreshFloor)
	}

	// 回执先落账（派发观察事实），替换派发随后到 terminal。
	observedInput := Observation{}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-terminal-fresh", replacement, "agent-terminal-fresh"), observedInput); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pending := snap.State.PendingActions[replacement]
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID("task-terminal-fresh-"+string(rune('a'+i))), pending.Task, pending.AttemptID, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, observedInput); err != nil {
			t.Fatalf("task progress %s: %v", status, err)
		}
	}

	// 合法 fresh 结果随后到达：真实签发代数过 floor，terminal 对账完成
	// 新鲜轮。
	fresh, err := NewReviewWorkerResultEvent("result-terminal-fresh", replacement, testProvider, "sha256:payload-terminal-fresh",
		reviewEvidenceAt(review.ProductReview, replacement, "rev-2", "PASS", "fresh round on a terminal task", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(fresh, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("fresh result on the terminal replacement = %v %+v", err, acceptance)
	}
	verifyOriginalFixtureReview(t, engine, fresh)
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
// 独立接纳后的终态完全相同——两种路径都保存完整原结果、补齐挂起技术审
// 配对，并由各自的独立接纳完成计轮、结论和暂存/隔离清空。
func TestProductFreshRoundCompletionOrderReachesTheSameTerminalState(t *testing.T) {
	t.Parallel()
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
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, _ := reviewEngine(t, cd, reg)
		observedInput := Observation{}

		product := issueReady(t, engine)[0]
		submitVerifiedReviewResult(t, engine, "result-order-product-1", product, reviewEvidence(review.ProductReview, "dispatch-order-product-1", "", "PASS", "clean", nil))
		stagedSlot := pendingActionByStep(t, engine, "start.readiness")
		var snap Snapshot

		// 技术审下一轮证据先到（暂存、回执未到）。
		staged, err := NewReviewWorkerResultEvent("result-order-readiness-2", stagedSlot, testProvider, "sha256:payload-order-readiness-2",
			reviewEvidence(review.StartReadiness, stagedSlot, "", "PASS", "readiness round before the change", nil))
		if err != nil {
			t.Fatalf("staged event: %v", err)
		}
		if acceptance, err := engine.Submit(staged, observedInput); err != nil || acceptance.Status != "STAGED" {
			t.Fatalf("stage: %v %+v", err, acceptance)
		}

		// 只勾重走产品审。
		askAndDecideRequirement(t, engine, "req-order-redo", "proceed", &RequirementDecisionPayload{
			BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-order",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "only the product review is redone",
		})
		// 产品审新鲜轮由重走决定的欠轮提案签出（同一决定事务）。
		freshSlot := pendingActionByStep(t, engine, "product.review")

		// 技术审槽的回执到达：配对在顺序门挂起（产品审义务未清偿）。
		if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-readiness", stagedSlot, "agent-order-readiness"), observedInput); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("readiness receipt: %v %+v", err, acceptance)
		}
		snap = engine.LoadMustSucceed(t)
		if _, kept := snap.State.StagedResults[stagedSlot]; !kept {
			t.Fatalf("the readiness pairing must stay suspended at the gate: %+v", snap.State.StagedResults)
		}

		// 产品审新鲜轮经两种顺序完成。
		fresh := reviewEvidenceAt(review.ProductReview, freshSlot, "rev-2", "PASS", "fresh product round", nil)
		freshResult, err := NewReviewWorkerResultEvent("result-order-product-2", freshSlot, testProvider, "sha256:payload-order-product-2", fresh)
		if err != nil {
			t.Fatalf("fresh event: %v", err)
		}
		if resultFirst {
			if acceptance, err := engine.Submit(freshResult, observedInput); err != nil || acceptance.Status != "STAGED" {
				t.Fatalf("fresh stage: %v %+v", err, acceptance)
			}
			if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-product", freshSlot, "agent-order-product"), observedInput); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh receipt: %v %+v", err, acceptance)
			}
		} else {
			if acceptance, err := engine.Submit(spawnReceipt(t, "spawn-order-product", freshSlot, "agent-order-product"), observedInput); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh receipt: %v %+v", err, acceptance)
			}
			if acceptance, err := engine.Submit(freshResult, observedInput); err != nil || acceptance.Status != "ACCEPTED" {
				t.Fatalf("fresh result: %v %+v", err, acceptance)
			}
		}

		snap = engine.LoadMustSucceed(t)
		if snap.State.ReviewSeries.ProductReview.Completed != 1 || snap.State.ReviewSeries.StartReadiness.Completed != 0 {
			t.Fatal("physical pairing must retain each review's independent acceptance duty")
		}
		verifyOriginalFixtureReview(t, engine, freshResult)
		verifyOriginalFixtureReview(t, engine, staged)
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
		stagedFirst.readinessCompleted != 1 || stagedFirst.readinessEffective != "PASS" || stagedFirst.readinessBound != "rev-1" ||
		stagedFirst.staging != 0 || stagedFirst.quarantine != 0 {
		t.Fatalf("terminal state = %+v (want product 2/PASS@rev-2, readiness 1/PASS@rev-1, no staging or quarantine)", stagedFirst)
	}
	if stagedFirst.resultsEntries != 3 {
		t.Fatalf("the ledger must hold two product reviews and one readiness review: %d", stagedFirst.resultsEntries)
	}
}

// TestUpstreamVoidReleaseCoversTheRequirementRedoPath：解除的触发是"上游
// 重新 PASS"的任何事务——不只纠正路径：产品审升级项把技术审置上游失效
// 后，需求变化（CHANGED）只重走产品审，其新鲜轮 PASS 的
// 同一事务内解除失效：技术审按自身账本重投影（rev-1 carry 续用），无技
// 术审槽也不卡死，路线门开放推进 DEVELOPMENT_PARALLEL。
func TestUpstreamVoidReleaseCoversTheRequirementRedoPath(t *testing.T) {
	t.Parallel()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness", "product.again")
	engine, dir := reviewEngine(t, cd, reg)

	product := issueReady(t, engine)[0]
	submitVerifiedReviewResult(t, engine, "result-redo-release-product", product, reviewEvidence(review.ProductReview, product, "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the documented path"},
	}))
	settleFixtureFinding(t, engine, review.ProductReview, "req-redo-release-dismiss-advisory", "DISMISS")
	readiness := pendingActionID(t, engine)
	submitVerifiedReviewResult(t, engine, "result-redo-release-readiness", readiness, reviewEvidence(review.StartReadiness, readiness, "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-redo-release-topo", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "single"}})

	// 产品审升级使技术审结论失效；finding 处置前没有新轮。
	snap := engine.LoadMustSucceed(t)
	productSeries := snap.State.ReviewSeries.ProductReview
	finding := productSeries.RawCandidates[0].Findings[0]
	upgrade := AskOption{ID: AskOptionID("RESEVERITIZE:" + finding.ID), Label: "reseverity"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-redo-release-upgrade", []AskOption{upgrade}, upgrade.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: productSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: productSeries.RawCandidates[0].ResultDigest, FindingID: finding.ID,
		Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the defect blocks the documented checkout path",
		Supersedes: review.LatestDecisionID(productSeries, productSeries.RawCandidates[0].ResultDigest, finding.ID),
		Reason:     "user corrects the dismissed advisory's severity", ExpectedEffectiveRevision: productSeries.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	voided := snap.State.ReviewSeries.StartReadiness
	if voided.Effective.Status != "PENDING" || !voided.UpstreamVoid {
		t.Fatalf("upgrade did not void start-readiness: %+v", voided.Effective)
	}
	rejectRouteRequest(t, engine, dir, "req-redo-release-blocked", []string{"blackbox", "whitebox", "g1"})

	// Changed requirements create a fresh product-review obligation.
	askAndDecideRequirement(t, engine, "req-redo-release-change", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-redo-release",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the revised requirement addresses the confirmed defect",
	})
	freshRoundFromTheLedgerProposal(t, engine, "product.review", "result-redo-release-fresh", reviewEvidenceAt(review.ProductReview, "dispatch-redo-release", "rev-2", "PASS", "fresh product round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" || series.FreshOwed {
		t.Fatalf("the fresh product round = %+v owed=%v", series.Effective, series.FreshOwed)
	}
	released := snap.State.ReviewSeries.StartReadiness
	if released.UpstreamVoid || released.Effective.Status != "PASS" || released.Effective.RequirementRevision != "rev-1" {
		t.Fatalf("the redo re-pass must release the void and re-project the carried conclusion: %+v void=%v", released.Effective, released.UpstreamVoid)
	}
	if released.Effective.Revision <= voided.Effective.Revision {
		t.Fatalf("the released projection must keep the revision monotonic: %d <= %d", released.Effective.Revision, voided.Effective.Revision)
	}

	// 路线门开放：无需任何技术审新轮。
	askAndDecide(t, engine, ControlSelectRoute, "req-redo-release-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Selected: []string{"blackbox", "whitebox", "g1", "g2"}, Gates: []string{"blackbox", "whitebox", "g1", "g2"}}})
	snap = engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("route = %t phase = %s, want confirmed/DEVELOPMENT_PARALLEL", snap.State.RouteConfirmed, snap.State.Phase)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// A granted slot stays available while a fresh obligation waits for the barrier.
// Normal issuance binds it exactly once when the final ask resolves.
func TestBarrierLiftBindsTheFreshReviewAuthorizationOnce(t *testing.T) {
	t.Parallel()
	cd, reg := singleCarrierFixture(t)
	engine, _ := reviewEngine(t, cd, reg)

	exhaustProductReviewCapacity(t, engine)
	redoProductReview(t, engine, "deferred-extra")
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-deferred-pair-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the endorsed round",
	}})
	fourth := pendingActionID(t, engine)
	authID := review.AuthorizationOfDispatch(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview, fourth)
	if authID == "" {
		t.Fatal("the endorsed dispatch must hold the granted slot")
	}

	baseRevision := engine.LoadMustSucceed(t).State.RequirementRevision()

	// 两个需求 Ask 同时未决：重走决定只退位、替换延迟（另一 Ask 持屏障）。
	for _, requestID := range []string{"req-deferred-pair-ask1", "req-deferred-pair-ask2"} {
		request, err := NewRequirementRequestEvent(EventID(requestID), requirementPackageOf(&RequirementDecisionPayload{
			BaseRevision: baseRevision, CandidateRevision: "rev-2", DeltaDigest: "sha256:deferred-pair-delta",
			ImpactDigest: "sha256:impact",
			Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
			RedoProductReview: true, Reason: "redo the product series",
		}), requirementOptions()...)
		if err != nil {
			t.Fatalf("request %s: %v", requestID, err)
		}
		if _, err := engine.Submit(request, Observation{}); err != nil {
			t.Fatalf("submit request %s: %v", requestID, err)
		}
	}
	token, err := engine.Freshness(RequestID("req-deferred-pair-ask1"))
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	redoPayload := &RequirementDecisionPayload{
		BaseRevision: baseRevision, CandidateRevision: "rev-2", DeltaDigest: "sha256:deferred-pair-delta",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "redo the product series",
	}
	if _, err := engine.Submit(requirementDecideEvent(t, "req-deferred-pair-ask1", token, "proceed", redoPayload), Observation{}); err != nil {
		t.Fatalf("decide the redo: %v", err)
	}
	state := engine.LoadMustSucceed(t).State
	original := state.PendingActions[fourth]
	if _, obsolete := state.ObsoleteActions[fourth]; !obsolete || !state.ReviewSeries.ProductReview.FreshOwed {
		t.Fatal("the old dispatch must be superseded while its new obligation remains owed")
	}
	if _, pending := state.PendingActions[fourth]; !pending {
		t.Fatal("the original dispatch must remain pending until its exact stop")
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-granted-original", fourth, "worker:"+fourth), Observation{}); err != nil {
		t.Fatal(err)
	}
	for _, edge := range []string{LifecycleStart, LifecycleStop} {
		event, err := NewCorrelatedLifecycleEvent(EventID("granted-original:"+edge), testProvider, "worker:"+fourth, "worker:"+fourth, edge)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(event, Observation{}); err != nil {
			t.Fatal(err)
		}
	}
	state = engine.LoadMustSucceed(t).State
	if receipt := state.SpawnReceipts[fourth]; receipt.ActionID != fourth || receipt.Task != original.Task || receipt.Provider != testProvider || receipt.Correlation != "worker:"+fourth || !reflect.DeepEqual(state.lifecycleMatches(testProvider, receipt.Correlation), []string{"worker:" + fourth}) {
		t.Fatal("barrier lost the granted dispatch's exact receipt and lifecycle pair")
	}
	if !reflect.DeepEqual(state.PendingActions[fourth], original) || state.ReviewSeries.ProductReview.Completed != 3 || state.ReviewSeries.ProductReview.ExtraConsumed != 0 {
		t.Fatal("barrier must retain the original action and attempt without counting or consuming its grant")
	}
	for id, pending := range state.PendingActions {
		if pending.ReviewSeries == review.ProductReview && id != fourth {
			t.Fatalf("the barrier must freeze replacement review dispatches: %s = %+v", id, pending)
		}
	}

	// 解除屏障：替换先补发（继承原槽），投影与补位随后——补位没有把该槽
	// 当干净槽绑走，槽上只有替换一条在途派发。
	abortExistingAsk(t, engine, "req-deferred-pair-ask2")
	replacement := freshReviewAfterRedo(t, engine, fourth)
	state = engine.LoadMustSucceed(t).State
	if got := review.AuthorizationOfDispatch(state.ReviewSeries.ProductReview, replacement); got != authID {
		t.Fatalf("the replacement must inherit the original slot: %q", got)
	}
	live := 0
	for id, pending := range state.PendingActions {
		if pending.ReviewSeries != review.ProductReview {
			continue
		}
		live++
		if review.AuthorizationOfDispatch(state.ReviewSeries.ProductReview, id) != authID || id != replacement {
			t.Fatalf("a refill-issued dispatch shares the inherited slot: %s = %+v", id, pending)
		}
	}
	if live != 1 {
		t.Fatalf("in-flight product dispatches after the lift = %d, want exactly the replacement: %+v", live, state.PendingActions)
	}
	if state.ReviewSeries.ProductReview.Completed != 3 || state.ReviewSeries.ProductReview.ExtraConsumed != 0 {
		t.Fatal("physical retirement and replacement must not consume a semantic round or its grant")
	}
	assertSeriesCommitmentHeld(t, engine)

	// 替换携槽新鲜轮入账清偿义务：系列结论，不再签发任何轮。
	fresh := reviewEvidenceAt(review.ProductReview, replacement, "rev-2", "PASS", "fresh round on the inherited slot", nil)
	fresh.AuthorizationID = authID
	submitVerifiedReviewResult(t, engine, "result-deferred-pair-fresh", replacement, fresh)
	snap := engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.FreshOwed || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" || series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("the fresh round must discharge and conclude: %+v", series)
	}
	if pending := snap.State.PendingActions; len(pending) != 0 {
		t.Fatalf("the concluded series must issue no further round: %+v", pending)
	}
	assertSeriesCommitmentHeld(t, engine)
}
