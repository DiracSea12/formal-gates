// review_test.go 覆盖开发前审查内核在提交协议层的嫁接行为：审查证据随
// WORKER_RESULT 同事务入账、逐 finding 两阶段决定、轮次耗尽与授权、拓
// 扑/路线绑定的顺序门控、run phase 的门控推进，以及加载/提交边界对篡改
// 账本与越门 phase 的可区分拒绝。全部拒绝都断言零状态变化。
package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// reviewStepsFixture 构造审查序列测试用的自定义定义：prep 本地步后接
// 串行 agent 步。fresh-review 派发不在本刀，连续语义轮经多个串行 agent
// 步驱动，每条审查证据各占一个 pending action。
func reviewStepsFixture(t *testing.T, agentSteps ...string) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	// INDEPENDENT_REVIEW 步必填系列归属标记：含 "readiness" 的步按技术审
	// 系列标记，其余步（含 canonical 产品审步与连续审查轮步）按产品审
	// 系列标记。
	return agentStepsDefinition(t, func(name string) authoring.AgentSpec {
		mark := authoring.ReviewSeriesProductReview
		if strings.Contains(name, "readiness") {
			mark = authoring.ReviewSeriesStartReadiness
		}
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: mark, Timeout: time.Minute,
		}
	}, agentSteps...)
}

// declaredSeriesStepsFixture 构造语义判断（非独立审查）agent 步的定义：
// 步不带系列标记，系列身份只能由到达的证据声明——签发标记缺失时的
// floor 比对与按声明终止兜底以此为可达形态。
func declaredSeriesStepsFixture(t *testing.T, agentSteps ...string) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	return agentStepsDefinition(t, func(string) authoring.AgentSpec {
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute,
		}
	}, agentSteps...)
}

// agentStepsDefinition 是串行 agent 步定义的共用构造：prep 本地步后接
// 每步一线性依赖的 agent 步，步载荷由 specOf 按步名给出。
func agentStepsDefinition(t *testing.T, specOf func(name string) authoring.AgentSpec, agentSteps ...string) (*compiler.CompiledDefinition, *compiler.Registry) {
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
	io := func(bindings ...string) authoring.IO {
		inputs := make([]authoring.InputBinding, 0, len(bindings))
		for _, b := range bindings {
			inputs = append(inputs, authoring.InputBinding{From: authoring.StepID(b), OutputField: "out", ToField: "in"})
		}
		return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
	}
	steps := []authoring.Step{}
	prep, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.test.prep"})
	must(err)
	steps = append(steps, prep)
	previous := "prep.parse"
	for _, name := range agentSteps {
		stepIO := io(previous)
		stepIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
		step, err := authoring.NewAgentStep(header(name, previous), stepIO, specOf(name))
		must(err)
		steps = append(steps, step)
		previous = name
	}
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: steps}, reg)
	must(err)
	return cd, reg
}

// reviewConfirmation 构造与 run 绑定的受理确认（需求 revision rev-1）。
func reviewConfirmation() *IntakeConfirmationReceipt {
	return &IntakeConfirmationReceipt{
		Source: "stable-driver", Authority: IntakeAuthorityStableDriver, Transport: IntakeTransportStableLauncher,
		RequirementSource: "requirement.md", RequirementRevision: "rev-1",
		Artifacts:        []IntakeArtifact{{Path: "requirement.md", Revision: "r1"}},
		SolutionRevision: "s1", SolutionDigest: "sha256:solution",
	}
}

// reviewEngine 构造已受理的引擎（容量 4）：prep 步在视图内完成、受理回
// 执落账后 run 处于 PRODUCT_REVIEW。runID 参与审查身份派生，固定为
// run-review。
func reviewEngine(t *testing.T, cd *compiler.CompiledDefinition, reg *compiler.Registry) (*Engine, string) {
	t.Helper()
	return reviewEngineCapacity(t, cd, reg, 4)
}

// reviewEngineCapacity 是 reviewEngine 的容量参数化形态（容量边界测试
// 用）。
func reviewEngineCapacity(t *testing.T, cd *compiler.CompiledDefinition, reg *compiler.Registry, capacity int) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{Definition: cd, Registry: reg, Capacity: capacity}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	if err := view.CompleteStep("prep.parse", cd); err != nil {
		t.Fatalf("complete prep: %v", err)
	}
	fp := fingerprintOf(t, engine)
	confirmation := reviewConfirmation()
	if err := engine.InitWithMetadata(view, testProvider, fp, "run-review", "", confirmation); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, fp); err != nil {
		t.Fatalf("intake receipt: %v", err)
	}
	return engine, dir
}

// reviewVerification 是 Operator 八项核实的完整记录。
func reviewVerification() review.OperatorVerification {
	return review.OperatorVerification{
		Checks:   []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"},
		Evidence: "operator re-ran the bound dispatch and compared the recorded evidence",
	}
}

// issueReady 签发当前 Ready frontier 并返回 actionID。
func issueReady(t *testing.T, engine *Engine) []string {
	t.Helper()
	fp := fingerprintOf(t, engine)
	plan, _, err := engine.Plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	ids := make([]string, 0, len(issued))
	for _, action := range issued {
		ids = append(ids, action.ActionID)
	}
	return ids
}

// pendingActionID 返回引擎当前唯一 pending action 的 ID。
func pendingActionID(t *testing.T, engine *Engine) string {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.PendingActions) != 1 {
		t.Fatalf("pending actions = %d, want exactly one", len(snap.State.PendingActions))
	}
	for id := range snap.State.PendingActions {
		return id
	}
	return ""
}

// reviewEvidence 构造绑定 rev-1 的审查证据。
func reviewEvidence(action review.Action, dispatchID, authorizationID, status, message string, findings []review.Finding) ReviewResultEvidence {
	return ReviewResultEvidence{
		Action: action, DispatchID: dispatchID, RequirementRevision: "rev-1", AuthorizationID: authorizationID,
		Status: status, Message: message, Findings: findings, Verification: reviewVerification(),
	}
}

// submitReviewResult 派发回执后提交携带审查证据的 PASS worker result。
func submitReviewResult(t *testing.T, engine *Engine, event, actionID string, evidence ReviewResultEvidence) Acceptance {
	t.Helper()
	evidence.DispatchID = actionID
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, EventID("spawn-"+event), actionID, "agent-"+event), fp); err != nil {
		t.Fatalf("spawn %s: %v", event, err)
	}
	evidence.DispatchID = actionID
	ev, err := NewReviewWorkerResultEvent(EventID(event), actionID, testProvider, "sha256:payload-"+event, evidence)
	if err != nil {
		t.Fatalf("review result event: %v", err)
	}
	acceptance, err := engine.Submit(ev, fp)
	if err != nil {
		t.Fatalf("submit review result %s: %v", event, err)
	}
	return acceptance
}

// askAndDecide 走完整两段：创建受限 Ask，取当前 freshness token 后提交
// 决定（payload 为 nil 时是普通决定事件）。
func askAndDecide(t *testing.T, engine *Engine, control ControlKind, requestID string, options []AskOption, choice AskOptionID, payload *ReviewDecisionPayload) Acceptance {
	t.Helper()
	fp := fingerprintOf(t, engine)
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
	var ev Event
	if payload == nil {
		ev, err = NewDecideEvent(EventID("evt-"+requestID), RequestID(requestID), token, choice)
	} else {
		ev, err = NewReviewDecideEvent(EventID("evt-"+requestID), RequestID(requestID), token, choice, *payload)
	}
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	acceptance, err := engine.Submit(ev, fp)
	if err != nil {
		t.Fatalf("submit decide %s: %v", requestID, err)
	}
	return acceptance
}

// grantOptions 是 REVIEW_ROUND_GRANT 的固定选项集。
func grantOptions() []AskOption {
	return []AskOption{{ID: "grant", Label: "授权恰好一轮"}, {ID: "decline", Label: "拒绝"}}
}

// topologyOptions 是 CONFIRM_TOPOLOGY 的标准选项集。
func topologyOptions() []AskOption {
	return []AskOption{{ID: "no-split", Label: "不拆分"}, {ID: "split", Label: "拆分"}}
}

// routeOptions 是 SELECT_ROUTE 的标准选项集。
func routeOptions() []AskOption {
	return []AskOption{{ID: "full", Label: "全部门"}, {ID: "custom", Label: "自定义子集"}}
}

func findingOption(t *testing.T, engine *Engine, action review.Action, index int, decision string) AskOption {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	series, ok := snap.State.ReviewSeries.Series(action)
	if !ok || len(series.RawCandidates) == 0 {
		t.Fatalf("no recorded candidates for %s", action)
	}
	candidate := series.RawCandidates[len(series.RawCandidates)-1]
	if index >= len(candidate.Findings) {
		t.Fatalf("candidate has %d findings, want index %d", len(candidate.Findings), index)
	}
	return AskOption{ID: AskOptionID(decision + ":" + candidate.Findings[index].ID), Label: decision}
}

// TestReviewSequenceThroughTopologyAndRoute：受理→产品审 FAIL→逐 finding
// DECIDE（dismiss 解阻塞）→effective PASS 推进 phase→start-readiness
// PASS→拓扑（no-split+note）→路线（full）→DEVELOPMENT_PARALLEL。
func TestReviewSequenceThroughTopologyAndRoute(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)

	// 受理落账即推进 INTAKE_REGISTERED→PRODUCT_REVIEW。
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseProductReview {
		t.Fatalf("phase after intake = %s, want PRODUCT_REVIEW", snap.State.Phase)
	}

	// 产品审：FAIL，一条 P1 阻塞 finding。
	actions := issueReady(t, engine)
	if len(actions) != 1 {
		t.Fatalf("issued = %v, want the product review action only", actions)
	}
	productAction := actions[0]
	submitReviewResult(t, engine, "result-product-1", productAction, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "findings block the product", []review.Finding{
		{Severity: "P1", Message: "normal path produces a wrong result"},
	}))
	snap = engine.LoadMustSucceed(t)
	product := snap.State.ReviewSeries.ProductReview
	if product.Completed != 1 || product.RemainingAutomatic != 2 || product.NextRoundRequirement != "AUTOMATIC_CAPACITY" {
		t.Fatalf("product capacity after round 1 = %+v", product)
	}
	if product.Effective.Status != "FAIL" || len(product.Effective.BlockingFindingIDs) != 1 || len(product.RawCandidates) != 1 || len(product.Adjudications) != 0 {
		t.Fatalf("product ledger after round 1 = %+v", product)
	}
	if snap.State.Phase != runtime.PhaseProductReview {
		t.Fatalf("phase after blocking product review = %s, want PRODUCT_REVIEW (静默留置)", snap.State.Phase)
	}

	// 逐 finding 决定：dismiss 解除阻塞，effective 直接 PASS、不为驳回项
	// 另派一轮；同一提交事务内 phase 推进到 TECHNICAL_REVIEW。
	dismiss := findingOption(t, engine, review.ProductReview, 0, "dismiss")
	confirm := findingOption(t, engine, review.ProductReview, 0, "confirm")
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-finding-1", []AskOption{confirm, dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: product.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: product.RawCandidates[0].ResultDigest, FindingID: product.RawCandidates[0].Findings[0].ID,
		Decision: "DISMISS", Reason: "user rejects the finding after checking the entry", ExpectedEffectiveRevision: 1,
	}})
	snap = engine.LoadMustSucceed(t)
	product = snap.State.ReviewSeries.ProductReview
	if product.Completed != 1 || product.Effective.Status != "PASS" || len(product.Adjudications) != 1 || product.RemainingAutomatic != 2 {
		t.Fatalf("product ledger after dismissal = %+v", product)
	}
	if snap.State.Phase != runtime.PhaseTechnicalReview {
		t.Fatalf("phase after product PASS = %s, want TECHNICAL_REVIEW", snap.State.Phase)
	}

	// start-readiness：PASS 后同事务连推两边到 TOPOLOGY_AND_ROUTE。
	readinessAction := pendingActionID(t, engine)
	if readinessAction == productAction {
		t.Fatalf("refill did not issue the readiness action (%s)", readinessAction)
	}
	submitReviewResult(t, engine, "result-readiness-1", readinessAction, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready to start", nil))
	snap = engine.LoadMustSucceed(t)
	readiness := snap.State.ReviewSeries.StartReadiness
	if readiness.Completed != 1 || readiness.Effective.Status != "PASS" {
		t.Fatalf("readiness ledger = %+v", readiness)
	}
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase after readiness PASS = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}

	// 拓扑：no-split 必带理由 note；绑定点落账后 phase 仍差路线。
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰，单人可并行推进",
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Topology == nil || snap.State.Topology.Decision != "no-split" || snap.State.Topology.Note == "" {
		t.Fatalf("topology binding = %+v", snap.State.Topology)
	}
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase after topology = %s, want TOPOLOGY_AND_ROUTE (route missing)", snap.State.Phase)
	}

	// 路线：full 显式全集；绑定后推进 DEVELOPMENT_PARALLEL。
	askAndDecide(t, engine, ControlSelectRoute, "req-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" || len(snap.State.SelectedGates) != 2 || snap.State.SelectedGates[0] != "gate.quality" || snap.State.SelectedGates[1] != "gate.security" {
		t.Fatalf("route binding = %q %v", snap.State.RouteMode, snap.State.SelectedGates)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// TestSplitTopologyAndCustomRouteBinding：choice 与载荷一致的 split 拓扑
// 与 custom 路线各走通——split 落账完整拆分参数，custom 按候选全集顺序
// 归一落账真子集，两者绑定后推进 DEVELOPMENT_PARALLEL。
func TestSplitTopologyAndCustomRouteBinding(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))

	askAndDecide(t, engine, ControlConfirmTopology, "req-topology-split", topologyOptions(), "split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "split", SplitCount: 2, Slices: []string{"lane-core", "lane-tooling"}, Parallel: "two lanes",
	}})
	snap := engine.LoadMustSucceed(t)
	topology := snap.State.Topology
	if topology == nil || topology.Decision != "split" || topology.SplitCount != 2 ||
		len(topology.Slices) != 2 || topology.Slices[0] != "lane-core" || topology.Slices[1] != "lane-tooling" || topology.Parallel != "two lanes" {
		t.Fatalf("split topology binding = %+v", topology)
	}

	askAndDecide(t, engine, ControlSelectRoute, "req-route-custom", routeOptions(), "custom", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "custom", Gates: []string{"gate.quality", "gate.security", "gate.release"}, Selected: []string{"gate.release", "gate.quality"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "custom" || len(snap.State.SelectedGates) != 2 ||
		snap.State.SelectedGates[0] != "gate.quality" || snap.State.SelectedGates[1] != "gate.release" {
		t.Fatalf("custom route binding = %q %v", snap.State.RouteMode, snap.State.SelectedGates)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after custom route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// TestReviewRepairLoopRoundExhaustionAndGrant：返修环 CONFIRM→仍 FAIL→
// RESEVERITIZE 降 P3→PASS 推进；三轮耗尽后未授权的下一轮被签发闸扣发
// （保持 Expected、decline 仍闸、显式签发同闸）、typed Ask 授权恰好一轮
// （不绑派发）后同事务补位签发＋自动绑定、伪造授权的证据仍拒、第四轮记
// 录占用授权。
func TestReviewRepairLoopRoundExhaustionAndGrant(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "round.one", "round.two", "round.three", "round.four")
	engine, _ := reviewEngine(t, cd, reg)

	// 轮 1：FAIL，一条 P1 finding；CONFIRM 后仍 FAIL。
	action := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-1", action, reviewEvidence(review.ProductReview, "dispatch-1", "", "FAIL", "round 1", []review.Finding{
		{Severity: "P1", Message: "core acceptance blocked"},
	}))
	snap := engine.LoadMustSucceed(t)
	round1 := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
	confirm := AskOption{ID: AskOptionID("confirm:" + round1.Findings[0].ID), Label: "confirm"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-confirm-1", []AskOption{confirm, findingOption(t, engine, review.ProductReview, 0, "dismiss")}, confirm.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: round1.DispatchID, RequirementRevision: "rev-1",
		ResultDigest: round1.ResultDigest, FindingID: round1.Findings[0].ID,
		Decision: "CONFIRM", Reason: "user confirms the blocker", ExpectedEffectiveRevision: 1,
	}})
	snap = engine.LoadMustSucceed(t)
	product := snap.State.ReviewSeries.ProductReview
	if product.Effective.Status != "FAIL" || product.Completed != 1 {
		t.Fatalf("confirmed P1 must stay blocking: %+v", product.Effective)
	}

	// RESEVERITIZE 降 P3：有效严重度降级后立即 PASS 并推进 phase。
	option := AskOption{ID: AskOptionID("RESEVERITIZE:" + round1.Findings[0].ID), Label: "reseverity"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-resev-1", []AskOption{option}, option.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: round1.DispatchID, RequirementRevision: "rev-1",
		ResultDigest: round1.ResultDigest, FindingID: round1.Findings[0].ID,
		Decision: "RESEVERITIZE", Severity: "P3", SeverityRationale: "impact limited to an optional path",
		Supersedes: product.Adjudications[0].ID, Reason: "user downgrades after tracing the impact", ExpectedEffectiveRevision: 2,
	}})
	snap = engine.LoadMustSucceed(t)
	product = snap.State.ReviewSeries.ProductReview
	if product.Effective.Status != "PASS" || len(product.Effective.AdvisoryFindingIDs) != 1 || len(product.Corrections) != 1 {
		t.Fatalf("downgraded finding must be advisory: %+v", product.Effective)
	}
	if snap.State.Phase != runtime.PhaseTechnicalReview {
		t.Fatalf("phase after downgrade = %s, want TECHNICAL_REVIEW", snap.State.Phase)
	}

	// 三轮耗尽：轮 2、轮 3 各记一轮，容量归零后转入用户决定形态。
	for round := 2; round <= 3; round++ {
		next := pendingActionID(t, engine)
		submitReviewResult(t, engine, string(rune('0'+round))+"-round", next, reviewEvidence(review.ProductReview, string(rune('0'+round))+"-dispatch", "", "FAIL", "round still failing", []review.Finding{
			{Severity: "P1", Message: "blocker persists"},
		}))
	}
	snap = engine.LoadMustSucceed(t)
	product = snap.State.ReviewSeries.ProductReview
	if product.Completed != 3 || product.RemainingCapacity != 0 || product.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("capacity boundary = %+v", product)
	}

	// 额度耗尽且无授权：第四轮审查步不签发——无 Attempt/pending action，
	// 步骤保持 Expected/Ready；decline 后仍闸；显式签发入口同闸。
	if len(snap.State.PendingActions) != 0 || len(snap.State.Attempts) != 0 {
		t.Fatalf("the exhausted series must not carry an issued dispatch: %+v", snap.State.PendingActions)
	}
	plan, _, err := engine.Plan()
	if err != nil || plan.Next.Kind != decision.KindReady || plan.Next.Ready == nil || len(plan.Next.Ready.Tasks) != 1 {
		t.Fatalf("the fourth review step must stay ready behind the gate: %v %+v", err, plan.Next)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-grant-decline", grantOptions(), "decline", nil)
	snap = engine.LoadMustSucceed(t)
	if got := len(snap.State.ReviewSeries.ProductReview.Authorizations); got != 0 {
		t.Fatalf("decline added authorizations: %d", got)
	}
	if len(snap.State.PendingActions) != 0 {
		t.Fatalf("a declined grant must keep the fourth round gated: %+v", snap.State.PendingActions)
	}
	if issued := issueReady(t, engine); len(issued) != 0 {
		t.Fatalf("explicit issuance signed past the exhausted series: %v", issued)
	}

	// typed Ask 授权恰好一轮（授权先于派发、不绑派发）：授权落账的同一事务
	// 收尾补位签发第四轮并自动绑上授权。
	acceptance := askAndDecide(t, engine, ControlReviewRoundGrant, "req-grant-1", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "one more semantic round is required",
	}})
	snap = engine.LoadMustSucceed(t)
	product = snap.State.ReviewSeries.ProductReview
	if product.ExtraAuthorized != 1 || product.CurrentLimit != 4 || product.RemainingCapacity != 1 || product.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" {
		t.Fatalf("authorization did not extend capacity: %+v", product)
	}
	fourth := pendingActionID(t, engine)
	if len(acceptance.Refill) != 1 || acceptance.Refill[0] != fourth {
		t.Fatalf("the grant transaction must refill-issue the authorized round: %+v", acceptance.Refill)
	}
	authorizationID := product.Authorizations[0].ID
	if !slices.Contains(product.Authorizations[0].DispatchIDs, fourth) {
		t.Fatalf("the issued dispatch must auto-bind the granted slot: %+v", product.Authorizations[0])
	}

	// 入账负控：授权必须点名背书其派发——伪造的授权身份可区分拒绝。
	if _, err := engine.Submit(spawnReceipt(t, "spawn-4", fourth, "agent-4"), fingerprintOf(t, engine)); err != nil {
		t.Fatalf("spawn 4: %v", err)
	}
	forged, err := NewReviewWorkerResultEvent("result-4-forged", fourth, testProvider, "sha256:payload-4f", reviewEvidence(review.ProductReview, fourth, "review-authorization-forged", "FAIL", "round 4 with a forged grant", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(forged, fingerprintOf(t, engine)); err == nil {
		t.Fatal("evidence with a forged authorization was accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("forged authorization code = %q", code)
	}

	// 第四轮携授权记录：占用授权槽，完成数到 4。
	finalRound, err := NewReviewWorkerResultEvent("result-4", fourth, testProvider, "sha256:payload-4", reviewEvidence(review.ProductReview, fourth, authorizationID, "FAIL", "round 4 endorsed by the grant", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(finalRound, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("submit authorized round 4: %v", err)
	}
	snap = engine.LoadMustSucceed(t)
	product = snap.State.ReviewSeries.ProductReview
	if product.Completed != 4 || product.ExtraConsumed != 1 || product.RemainingCapacity != 0 || product.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("authorized round did not consume its slot: %+v", product)
	}
}

// TestGrantIsPrematureWhileAutomaticCapacityRemains：授权政策由引擎强制
// ——自动容量未耗尽（completed < automatic limit）时 REVIEW_ROUND_GRANT
// 可区分拒绝且零状态变化；容量耗尽后的授权正控见上文耗尽/授权测试。
func TestGrantIsPrematureWhileAutomaticCapacityRemains(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "round.one", "round.two")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-grant-premature", product, reviewEvidence(review.ProductReview, product, "", "FAIL", "one automatic round is done", []review.Finding{
		{Severity: "P1", Message: "blocker persists"},
	}))
	if completed := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Completed; completed != 1 {
		t.Fatalf("automatic rounds completed = %d, want 1", completed)
	}
	// 容量内的下一轮照常补位签发（一个在途派发）。
	pendingActionID(t, engine)
	err := askAndDecideRejecting(t, engine, dir, ControlReviewRoundGrant, "req-grant-premature", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "the user wants one more round early",
	}})
	if err == nil {
		t.Fatal("grant accepted while automatic capacity remains")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("premature grant code = %q", code)
	} else if !strings.Contains(err.Error(), "automatic round capacity is not exhausted") {
		t.Fatalf("premature grant rejection does not name the policy: %v", err)
	}
}

// TestReviewGatingNegatives：逐类负控——每例断言可区分错误码与零状态
// 变化。
func TestReviewGatingNegatives(t *testing.T) {
	t.Run("start-readiness evidence before product-review PASS", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		// 语义 FAIL 的结果仍是完成的派发：step 完成、start-readiness 被
		// 补位签发——但账本阻塞，双审顺序守卫必须拒绝其证据。
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
			{Severity: "P1", Message: "core path broken"},
		}))
		readiness := pendingActionID(t, engine)
		ev, err := NewReviewWorkerResultEvent("result-readiness-early", readiness, testProvider, "sha256:early", reviewEvidence(review.StartReadiness, readiness, "", "PASS", "early", nil))
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		if _, err := engine.Submit(spawnReceipt(t, "spawn-early", readiness, "agent-early"), fp); err != nil {
			t.Fatalf("spawn: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("start-readiness evidence accepted before product-review PASS")
		} else if code := rejectionCode(t, err); code != CodeReviewSequenceGate {
			t.Fatalf("early readiness code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected early readiness changed state bytes")
		}
	})

	t.Run("topology before readiness PASS", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		// phase TECHNICAL_REVIEW：start-readiness 尚未 PASS，拓扑须拒绝。
		if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-topo-early", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆：尚无 readiness",
		}}); err == nil {
			t.Fatal("topology accepted before readiness PASS")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("early topology code = %q", code)
		}
	})

	t.Run("route before topology", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-early", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "full", Gates: []string{"gate.quality"},
		}}); err == nil {
			t.Fatal("route accepted before topology binding")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("early route code = %q", code)
		}
	})

	t.Run("topology re-confirmation is not re-cut", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology-1", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆：模块小",
		}})
		if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-topology-2", topologyOptions(), "split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "split", SplitCount: 2, Slices: []string{"slice-a", "slice-b"}, Parallel: "two lanes",
		}}); err == nil {
			t.Fatal("topology re-confirmation accepted")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("re-cut code = %q", code)
		}
	})

	t.Run("topology choice must anchor the payload decision", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
		split := &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "split", SplitCount: 2, Slices: []string{"slice-a", "slice-b"}, Parallel: "two lanes",
		}}
		if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-topo-cross", topologyOptions(), "no-split", split); err == nil {
			t.Fatal("no-split choice with a split payload accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("crossed topology code = %q", code)
		}
		// 选项集由 Ask 创建方给出：集合外的 choice id 即使与某个载荷字段
		// 巧合，也不是合法拓扑决定。
		deferDecision := &ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "建议不拆"}}
		if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-topo-foreign", []AskOption{{ID: "defer", Label: "推迟"}}, "defer", deferDecision); err == nil {
			t.Fatal("foreign topology choice accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("foreign topology code = %q", code)
		}
	})

	t.Run("route choice must anchor the payload mode", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆",
		}})
		custom := &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "custom", Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.quality"},
		}}
		if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-cross", routeOptions(), "full", custom); err == nil {
			t.Fatal("full choice with a custom payload accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("crossed route code = %q", code)
		}
	})

	t.Run("no-split without note is schema invalid", func(t *testing.T) {
		if _, err := NewReviewDecideEvent("evt-no-note", "req-topology", "sha256:token", "no-split", ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split",
		}}); err == nil {
			t.Fatal("no-split without note constructed")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("no-note code = %q", code)
		}
	})

	t.Run("custom route empty or full subset is schema invalid", func(t *testing.T) {
		if _, err := NewReviewDecideEvent("evt-empty", "req-route", "sha256:token", "custom", ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "custom", Gates: []string{"gate.a", "gate.b"}, Selected: []string{},
		}}); err == nil {
			t.Fatal("empty custom subset constructed")
		}
		if _, err := NewReviewDecideEvent("evt-full", "req-route", "sha256:token", "custom", ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "custom", Gates: []string{"gate.a", "gate.b"}, Selected: []string{"gate.a", "gate.b"},
		}}); err == nil {
			t.Fatal("full custom subset constructed")
		}
	})

	t.Run("review control decide without payload", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequestEvent("req-topology-nopayload", ControlConfirmTopology, topologyOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		token, err := engine.Freshness("req-topology-nopayload")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		ev, err := NewDecideEvent("evt-decide-nopayload", "req-topology-nopayload", token, "no-split")
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("payload-less topology decide accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("no-payload code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected no-payload decide changed state bytes")
		}
	})

	t.Run("grant choice without the roundGrant branch is rejected, not a panic", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequestEvent("req-grant-nopayload", ControlReviewRoundGrant, grantOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		token, err := engine.Freshness("req-grant-nopayload")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		ev, err := NewDecideEvent("evt-grant-nopayload", "req-grant-nopayload", token, "grant")
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("payload-less grant decide accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("no-payload grant code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected no-payload grant changed state bytes")
		}
	})

	t.Run("correction payload must match the chosen option", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
			{Severity: "P1", Message: "core path broken"},
		}))
		snap := engine.LoadMustSucceed(t)
		candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
		confirm := AskOption{ID: AskOptionID("confirm:" + candidate.Findings[0].ID), Label: "confirm"}
		payload := ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
			ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
			Decision: "DISMISS", Reason: "payload says dismiss", ExpectedEffectiveRevision: 1,
		}}
		if err := askAndDecideRejecting(t, engine, dir, ControlReviewFindingDecision, "req-mismatch", []AskOption{confirm}, confirm.ID, &payload); err == nil {
			t.Fatal("mismatched correction accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("mismatch code = %q", code)
		}
	})

	t.Run("correction CAS on stale effective revision", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "FAIL", "blocked", []review.Finding{
			{Severity: "P1", Message: "core path broken"},
		}))
		dismiss := findingOption(t, engine, review.ProductReview, 0, "dismiss")
		snap := engine.LoadMustSucceed(t)
		candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
		payload := ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
			ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
			Decision: "DISMISS", Reason: "stale CAS", ExpectedEffectiveRevision: 99,
		}}
		if err := askAndDecideRejecting(t, engine, dir, ControlReviewFindingDecision, "req-cas", []AskOption{dismiss}, dismiss.ID, &payload); err == nil {
			t.Fatal("stale CAS correction accepted")
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("stale CAS code = %q", code)
		}
	})

	t.Run("non-review control cannot carry review payload", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequestEvent("req-reset-payload", ControlReset, AskOption{ID: "proceed", Label: "重置"})
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		token, err := engine.Freshness("req-reset-payload")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		payload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: " smuggled"}}
		ev, err := NewReviewDecideEvent("evt-reset-payload", "req-reset-payload", token, "proceed", payload)
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("RESET decide with review payload accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("reset payload code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected reset payload changed state bytes")
		}
	})

	t.Run("stale freshness on a review decide", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		request, err := NewRequestEvent("req-stale", ControlConfirmTopology, topologyOptions()...)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		stale, err := engine.Freshness("req-stale")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		// 任何后续提交取代旧 token。
		other, err := NewRequestEvent("req-stale-other", ControlAbort, AskOption{ID: "abort", Label: "终止"})
		if err != nil {
			t.Fatalf("other request: %v", err)
		}
		if _, err := engine.Submit(other, fp); err != nil {
			t.Fatalf("submit other request: %v", err)
		}
		payload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "建议不拆"}}
		ev, err := NewReviewDecideEvent("evt-stale", "req-stale", stale, "no-split", payload)
		if err != nil {
			t.Fatalf("decide: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("stale token accepted")
		} else if code := rejectionCode(t, err); code != CodeStaleFreshness {
			t.Fatalf("stale code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("stale rejection changed state bytes")
		}
	})

	t.Run("duplicate result digest rejected", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "round.one", "round.two")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		first := issueReady(t, engine)[0]
		evidence := reviewEvidence(review.ProductReview, first, "", "FAIL", "same bytes", []review.Finding{
			{Severity: "P1", Message: "same finding"},
		})
		submitReviewResult(t, engine, "result-1", first, evidence)
		// 派发身份绑定后，同摘要只能来自同 action 的逐字节重发：结果级
		// 摘要幂等判 DUPLICATE（跨 action 的摘要碰撞已被 DispatchID 绑定
		// 结构性排除；内核级重复摘要拒绝由 review 包测试覆盖）。
		repeat, err := NewReviewWorkerResultEvent("result-duplicate", first, testProvider, "sha256:payload-result-1", evidence)
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		before := stateBytesOf(t, dir)
		if acceptance, err := engine.Submit(repeat, fp); err != nil || acceptance.Status != "DUPLICATE" {
			t.Fatalf("same-bytes resubmission = %v %+v (want DUPLICATE)", err, acceptance)
		}
		_ = before
	})

	t.Run("review evidence with a stale requirement revision", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		if _, err := engine.Submit(spawnReceipt(t, "spawn-stale-revision", product, "agent-stale-revision"), fp); err != nil {
			t.Fatalf("spawn: %v", err)
		}
		stale := reviewEvidence(review.ProductReview, "dispatch-stale", "", "FAIL", "bound to an older requirement", []review.Finding{
			{Severity: "P1", Message: "old revision finding"},
		})
		stale.RequirementRevision = "rev-0"
		stale.DispatchID = product
		ev, err := NewReviewWorkerResultEvent("result-stale-revision", product, testProvider, "sha256:stale-revision", stale)
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(ev, fp); err == nil {
			t.Fatal("stale requirement revision accepted")
		} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
			t.Fatalf("stale revision code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected stale revision changed state bytes")
		}
	})

	t.Run("review content participates in result identity", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		product := issueReady(t, engine)[0]
		evidence := reviewEvidence(review.ProductReview, "dispatch-identity", "", "PASS", "first verdict", nil)
		submitReviewResult(t, engine, "result-identity", product, evidence)
		// 同字节重发（新 event ID）仍是 DUPLICATE——判重口径收窄不误伤
		// 幂等重放。
		evidence.DispatchID = product
		identical, err := NewReviewWorkerResultEvent("result-identity-replay", product, testProvider, "sha256:payload-result-identity", evidence)
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		acceptance, err := engine.Submit(identical, fp)
		if err != nil {
			t.Fatalf("identical replay rejected: %v", err)
		}
		if acceptance.Status != "DUPLICATE" {
			t.Fatalf("identical replay status = %q, want DUPLICATE", acceptance.Status)
		}
		// 同 action、同外层字段（provider/outcome/payload digest/failure
		// class）、新 event ID、仅审查内容不同：这是另一个结果，必须
		// 冲突拒绝而非误判 DUPLICATE。
		altered := evidence
		altered.Message = "second verdict"
		altered.DispatchID = product
		replayed, err := NewReviewWorkerResultEvent("result-identity-alt", product, testProvider, "sha256:payload-result-identity", altered)
		if err != nil {
			t.Fatalf("event: %v", err)
		}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(replayed, fp); err == nil {
			t.Fatal("altered review content resubmission accepted")
		} else if code := rejectionCode(t, err); code != CodeReceiptConflict {
			t.Fatalf("altered review code = %q", code)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected altered review replay changed state bytes")
		}
	})

	t.Run("review evidence cannot ride a non-PASS outcome", func(t *testing.T) {
		// 失败结果携合法 failure class：拒绝必须来自"审查证据只能随
		// PASS 结果"这条约束本身，而不是别的字段先失败。
		evidence := ReviewResultEvidence{
			Action: review.ProductReview, DispatchID: "dispatch-1", RequirementRevision: "rev-1", Status: "PASS",
		}
		for _, outcome := range []string{OutcomeRuntimeError, OutcomeFail} {
			ev := Event{ID: EventID("evt-" + outcome + "-review"), Kind: KindWorkerResult, Result: &WorkerResultPayload{
				ActionID: "act:x", Provider: testProvider, Outcome: outcome, PayloadDigest: "sha256:p",
				FailureClass: authoring.FailureBlockedBug, Review: &evidence,
			}}
			err := ev.Validate()
			if err == nil {
				t.Fatalf("%s review evidence accepted", outcome)
			}
			if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
				t.Fatalf("%s review code = %q", outcome, code)
			}
			var rejected *RejectedError
			if !errors.As(err, &rejected) || !strings.Contains(rejected.Detail, "PASS outcome") {
				t.Fatalf("%s rejection does not point at the review/outcome constraint: %v", outcome, err)
			}
		}
	})

	t.Run("decide payload union must carry exactly one branch", func(t *testing.T) {
		payload := ReviewDecisionPayload{
			RoundGrant: &RoundGrantDecision{Action: review.ProductReview, Reason: "r"},
			Topology:   &TopologyDecision{Decision: "no-split", Note: "n"},
		}
		if _, err := NewReviewDecideEvent("evt-two-branches", "req-x", "sha256:token", "grant", payload); err == nil {
			t.Fatal("two-branch payload constructed")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("two-branch code = %q", code)
		}
	})
}

// TestUpstreamVoidReleaseReprojectsTheDownstreamLedger：双审 PASS、拓扑
// 确认后，产品审纠正升 P1 重新阻塞＋级联失效技术审（路线门关闭）。失
// 效窗口内技术审对自己的 finding 纠正照常落账（投影仍被失效压住）；上
// 游驳回升级项回 PASS 的同一事务内解除失效——技术审结论按其当前账本重
// 投影：窗口内升级的 P1 如实阻塞（不是回放失效前快照），驳回后路线门
// 重新打开并推进 DEVELOVELOPMENT_PARALLEL（§7.3.3）。
func TestUpstreamVoidReleaseReprojectsTheDownstreamLedger(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness.one", "start.readiness.two")
	engine, dir := reviewEngine(t, cd, reg)

	// 双审到位：各携一条建议项（effective PASS）。
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, product, "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the documented path"},
	}))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, readiness, "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the readiness path"},
	}))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆：模块边界清晰，单人可并行推进",
	}})

	// 产品审纠正升 P1：重新阻塞＋级联失效技术审。
	snap := engine.LoadMustSucceed(t)
	productSeries := snap.State.ReviewSeries.ProductReview
	productFinding := productSeries.RawCandidates[0].Findings[0]
	upgrade := AskOption{ID: AskOptionID("RESEVERITIZE:" + productFinding.ID), Label: "reseverity"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-upgrade", []AskOption{upgrade}, upgrade.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: productSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: productSeries.RawCandidates[0].ResultDigest, FindingID: productFinding.ID,
		Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "the defect blocks the documented checkout path",
		Reason: "user upgrades the advisory after re-tracing the impact", ExpectedEffectiveRevision: productSeries.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Effective.Status != "FAIL" {
		t.Fatalf("upgrade did not re-block product-review: %+v", snap.State.ReviewSeries.ProductReview.Effective)
	}
	voided := snap.State.ReviewSeries.StartReadiness
	if voided.Effective.Status != "PENDING" || !voided.UpstreamVoid {
		t.Fatalf("upgrade did not void start-readiness: %+v", voided.Effective)
	}
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase = %s, want TOPOLOGY_AND_ROUTE (gate must stay closed)", snap.State.Phase)
	}

	// 重新阻塞期间选路线：可区分拒绝、零状态变化。
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-blocked", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality"},
	}}); err == nil {
		t.Fatal("route accepted while product-review is re-blocked")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("route-under-block code = %q", code)
	} else if !strings.Contains(err.Error(), "both pre-development reviews") {
		t.Fatalf("route-under-block rejection does not name the dual gate: %v", err)
	}

	// 失效窗口内：技术审对自己的建议项升级 P1——合法落账，投影仍被失
	// 效压住（下游自身纠正不解除上游失效）。
	readinessSeries := snap.State.ReviewSeries.StartReadiness
	readinessFinding := readinessSeries.RawCandidates[0].Findings[0]
	windowRaise := AskOption{ID: AskOptionID("RESEVERITIZE:" + readinessFinding.ID), Label: "reseverity"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-window-raise", []AskOption{windowRaise}, windowRaise.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.StartReadiness, DispatchID: readinessSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: readinessSeries.RawCandidates[0].ResultDigest, FindingID: readinessFinding.ID,
		Decision: "RESEVERITIZE", Severity: "P1", SeverityRationale: "re-traced during the void: the readiness defect blocks the documented entry",
		Reason: "raised inside the void window", ExpectedEffectiveRevision: readinessSeries.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Effective.Status != "PENDING" || !series.UpstreamVoid {
		t.Fatalf("the void-period correction must commit cleanly while staying held: %+v void=%v", series.Effective, series.UpstreamVoid)
	}

	// 上游驳回升级项回 PASS 的同一事务内解除失效：技术审按当前账本重
	// 投影——窗口内升级的 P1 如实阻塞（不回放失效前快照）。
	snap = engine.LoadMustSucceed(t)
	productSeries = snap.State.ReviewSeries.ProductReview
	dismiss := AskOption{ID: AskOptionID("dismiss:" + productFinding.ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-dismiss", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: productSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: productSeries.RawCandidates[0].ResultDigest, FindingID: productFinding.ID,
		Decision: "DISMISS", Supersedes: productSeries.Adjudications[0].ID,
		Reason: "user rejects the upgrade after re-verification", ExpectedEffectiveRevision: productSeries.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Effective.Status != "PASS" {
		t.Fatalf("dismiss did not restore product-review PASS: %+v", snap.State.ReviewSeries.ProductReview.Effective)
	}
	released := snap.State.ReviewSeries.StartReadiness
	if released.UpstreamVoid {
		t.Fatal("the upstream re-pass must release the void in the same transaction")
	}
	if released.Effective.Status != "FAIL" || len(released.Effective.BlockingFindingIDs) != 1 {
		t.Fatalf("the release must re-project the current ledger (the window raise blocks): %+v", released.Effective)
	}
	if released.Effective.Revision <= voided.Effective.Revision {
		t.Fatalf("the released projection must keep the revision monotonic: %d <= %d", released.Effective.Revision, voided.Effective.Revision)
	}
	if err := askAndDecideRejecting(t, engine, dir, ControlSelectRoute, "req-route-held", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality"},
	}}); err == nil {
		t.Fatal("route accepted while the re-projected readiness conclusion blocks")
	}

	// 驳回窗口内的升级项：技术审回 PASS，路线门打开并推进。
	snap = engine.LoadMustSucceed(t)
	readinessSeries = snap.State.ReviewSeries.StartReadiness
	windowDismiss := AskOption{ID: AskOptionID("dismiss:" + readinessFinding.ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-window-dismiss", []AskOption{windowDismiss}, windowDismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.StartReadiness, DispatchID: readinessSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: readinessSeries.RawCandidates[0].ResultDigest, FindingID: readinessFinding.ID,
		Decision: "DISMISS", Supersedes: readinessSeries.Adjudications[0].ID,
		Reason: "the window raise is rejected after re-verification", ExpectedEffectiveRevision: readinessSeries.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Effective.Status != "PASS" || series.Completed != 1 {
		t.Fatalf("dismissing the window raise must restore readiness PASS: %+v", series.Effective)
	}
	askAndDecide(t, engine, ControlSelectRoute, "req-route-open", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" || snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("route = %q phase = %s, want full/DEVELOPMENT_PARALLEL after the release round trip", snap.State.RouteMode, snap.State.Phase)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load after the release round trip: %v", err)
	}
}

// TestDirectDismissLeavesTheDownstreamConclusionUntouched：未经升级往返的
// 直接 DISMISS 对照：产品审建议项被驳回时技术审从未被失效，其结论与计轮
// 原样（与往返恢复的路径区分）。
func TestDirectDismissLeavesTheDownstreamConclusionUntouched(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness.one", "start.readiness.two")
	engine, _ := reviewEngine(t, cd, reg)

	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, product, "", "FAIL", "advisory only", []review.Finding{
		{Severity: "P2", Message: "advisory defect on the documented path"},
	}))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, readiness, "", "PASS", "ready", nil))
	before := engine.LoadMustSucceed(t).State.ReviewSeries.StartReadiness

	productSeries := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	finding := productSeries.RawCandidates[0].Findings[0]
	dismiss := AskOption{ID: AskOptionID("dismiss:" + finding.ID), Label: "dismiss"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-dismiss-direct", []AskOption{dismiss}, dismiss.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: productSeries.RawCandidates[0].DispatchID, RequirementRevision: "rev-1",
		ResultDigest: productSeries.RawCandidates[0].ResultDigest, FindingID: finding.ID,
		Decision: "DISMISS",
		Reason:   "user rejects the advisory directly", ExpectedEffectiveRevision: productSeries.Effective.Revision,
	}})
	after := engine.LoadMustSucceed(t).State.ReviewSeries.StartReadiness
	if after.Effective.ResultDigest != before.Effective.ResultDigest || after.Effective.Revision != before.Effective.Revision ||
		after.Completed != before.Completed || after.UpstreamVoid {
		t.Fatalf("a direct dismissal must leave the downstream conclusion untouched: before %+v after %+v", before.Effective, after.Effective)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// TestStaleRevisionReviewResultIsRejectedBeforeStaging：回执未到时携审查
// 证据的旧 revision 结果不得进入暂存——直接 CodeReviewEvidenceRejected 拒
// 绝且零状态变化，正确 revision 的结果随后照常暂存并在回执到达后完成
// 入账（暂存槽不被毒化）。
func TestStaleRevisionReviewResultIsRejectedBeforeStaging(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]

	stale := reviewEvidence(review.ProductReview, "dispatch-stale", "", "PASS", "bound to an older requirement", nil)
	stale.RequirementRevision = "rev-0"
	stale.DispatchID = product
	ev, err := NewReviewWorkerResultEvent("result-stale-first", product, testProvider, "sha256:stale-first", stale)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("stale-revision review result staged before its receipt")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("stale staging code = %q", code)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("rejected stale staging changed state bytes")
	}
	if snap := engine.LoadMustSucceed(t); len(snap.State.StagedResults) != 0 {
		t.Fatalf("stale review result poisoned staging: %+v", snap.State.StagedResults)
	}

	current := reviewEvidence(review.ProductReview, "dispatch-current", "", "PASS", "current verdict", nil)
	current.DispatchID = product
	evCurrent, err := NewReviewWorkerResultEvent("result-current", product, testProvider, "sha256:current", current)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	acceptance, err := engine.Submit(evCurrent, fp)
	if err != nil {
		t.Fatalf("current result: %v", err)
	}
	if acceptance.Status != "STAGED" {
		t.Fatalf("current result status = %q, want STAGED", acceptance.Status)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-current", product, "agent-current"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.ReviewSeries.ProductReview.Completed != 1 || len(snap.State.StagedResults) != 0 {
		t.Fatalf("paired current result did not reach the ledger: completed=%d staged=%d", snap.State.ReviewSeries.ProductReview.Completed, len(snap.State.StagedResults))
	}
}

// TestReviewEvidenceMustBindItsDispatchingAction：证据的 DispatchID 就是
// 提交结果的 actionID（派发身份唯一事实）——指向其它派发的证据可区分
// 拒绝且零状态变化，正确绑定的结果随后照常暂存（暂存槽不被毒化）。
func TestReviewEvidenceMustBindItsDispatchingAction(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]

	misbound := reviewEvidence(review.ProductReview, "act:review/another.dispatch", "", "PASS", "evidence naming a foreign dispatch", nil)
	misboundEvent, err := NewReviewWorkerResultEvent("result-misbound", product, testProvider, "sha256:payload-misbound", misbound)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	before := stateBytesOf(t, dir)
	if _, err := engine.Submit(misboundEvent, fp); err == nil {
		t.Fatal("foreign-dispatch evidence accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("misbound evidence code = %q", code)
	} else if !strings.Contains(err.Error(), "does not match the dispatching action") {
		t.Fatalf("misbound rejection does not name the dispatch identity: %v", err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("rejected misbound evidence changed state bytes")
	}

	correct, err := NewReviewWorkerResultEvent("result-bound", product, testProvider, "sha256:payload-bound",
		reviewEvidence(review.ProductReview, product, "", "PASS", "evidence bound to its own dispatch", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(correct, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("correctly bound result after the rejection: %v %+v", err, acceptance)
	}
}

// TestInadmissibleReviewEvidenceIsRejectedBeforeStaging：回执未到时携不完
// 整审查证据（Operator 核实缺项、finding 严重度 P9）的结果不得进入暂存
// ——直接 CodeReviewEvidenceRejected 拒绝且零状态变化；回执照常接纳，正
// 确结果随后照常完成入账（一次填写错误不污染暂存槽、不必换 attempt 即可
// 恢复）。
func TestInadmissibleReviewEvidenceIsRejectedBeforeStaging(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(evidence *ReviewResultEvidence)
	}{
		{"operator verification missing checks", func(evidence *ReviewResultEvidence) {
			evidence.Verification = review.OperatorVerification{Checks: []string{"binding"}, Evidence: "partial"}
		}},
		{"finding severity outside P0-P3", func(evidence *ReviewResultEvidence) {
			evidence.Findings = []review.Finding{{Severity: "P9", Message: "unclassified impact"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
			engine, dir := reviewEngine(t, cd, reg)
			fp := fingerprintOf(t, engine)
			product := issueReady(t, engine)[0]

			broken := reviewEvidence(review.ProductReview, "dispatch-1", "", "FAIL", "inadmissible evidence", []review.Finding{
				{Severity: "P1", Message: "core path broken"},
			})
			tc.corrupt(&broken)
			broken.DispatchID = product
			ev, err := NewReviewWorkerResultEvent("result-inadmissible", product, testProvider, "sha256:inadmissible", broken)
			if err != nil {
				t.Fatalf("event: %v", err)
			}
			before := stateBytesOf(t, dir)
			if _, err := engine.Submit(ev, fp); err == nil {
				t.Fatal("inadmissible review result staged before its receipt")
			} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
				t.Fatalf("inadmissible staging code = %q", code)
			}
			if after := stateBytesOf(t, dir); string(before) != string(after) {
				t.Fatal("rejected inadmissible staging changed state bytes")
			}
			if snap := engine.LoadMustSucceed(t); len(snap.State.StagedResults) != 0 {
				t.Fatalf("inadmissible review result poisoned staging: %+v", snap.State.StagedResults)
			}

			if _, err := engine.Submit(spawnReceipt(t, "spawn-pair", product, "agent-pair"), fp); err != nil {
				t.Fatalf("receipt after a rejected staging: %v", err)
			}
			correct := reviewEvidence(review.ProductReview, "dispatch-1", "", "FAIL", "admissible evidence", []review.Finding{
				{Severity: "P1", Message: "core path broken"},
			})
			correct.DispatchID = product
			evCorrect, err := NewReviewWorkerResultEvent("result-correct", product, testProvider, "sha256:correct", correct)
			if err != nil {
				t.Fatalf("event: %v", err)
			}
			acceptance, err := engine.Submit(evCorrect, fp)
			if err != nil {
				t.Fatalf("correct result after a rejected staging: %v", err)
			}
			if acceptance.Status != "ACCEPTED" {
				t.Fatalf("correct result status = %q, want ACCEPTED", acceptance.Status)
			}
			snap := engine.LoadMustSucceed(t)
			if snap.State.ReviewSeries.ProductReview.Completed != 1 || len(snap.State.StagedResults) != 0 {
				t.Fatalf("correct result did not reach the ledger: completed=%d staged=%d", snap.State.ReviewSeries.ProductReview.Completed, len(snap.State.StagedResults))
			}
		})
	}
}

// askAndDecideRejecting 创建 Ask 后提交决定并返回其错误；零状态变化断言
// 只围绕决定提交本身（Ask 落账是合法提交，不属于被拒部分）。
func askAndDecideRejecting(t *testing.T, engine *Engine, dir string, control ControlKind, requestID string, options []AskOption, choice AskOptionID, payload *ReviewDecisionPayload) error {
	t.Helper()
	fp := fingerprintOf(t, engine)
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
	ev, err := NewReviewDecideEvent(EventID("evt-"+requestID), RequestID(requestID), token, choice, *payload)
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	before := stateBytesOf(t, dir)
	_, err = engine.Submit(ev, fp)
	if err == nil {
		return nil
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatalf("rejected decide %s changed state bytes", requestID)
	}
	return err
}

// TestReviewTamperedStateRejectedAtLoad：篡改 state.json 的账本投影或
// phase 后，加载边界可区分拒绝。
func TestReviewTamperedStateRejectedAtLoad(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	original := stateBytesOf(t, dir)
	restore := func() {
		if err := os.WriteFile(filepath.Join(dir, "state.json"), original, 0o600); err != nil {
			t.Fatalf("restore state: %v", err)
		}
	}

	// 账本篡改：把 completed 拨到 3（无候选支撑）——完整性摘要同步重算
	// 以穿过持久层，协议层的投影重算必须拒绝。
	tamperStateContent(t, dir, func(content map[string]any) {
		series := content["reviewSeries"].(map[string]any)
		series["productReview"].(map[string]any)["completed"] = float64(3)
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("tampered ledger loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("tampered ledger code = %q", code)
	}
	restore()

	// phase 篡改：手工拨到 DEVELOPMENT_PARALLEL（账本只证明 PRODUCT_REVIEW）。
	tamperStateContent(t, dir, func(content map[string]any) {
		content["phase"] = runtime.PhaseDevelopmentParallel
	})
	if _, err := engine.Load(); err == nil {
		t.Fatal("hand-dialed phase loaded")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("hand-dial phase code = %q", code)
	}
	restore()

	// 还原后可正常加载。
	if _, err := engine.Load(); err != nil {
		t.Fatalf("restored state failed to load: %v", err)
	}
}

// tamperStateContent 修改 state.json 的 content 并同步重算完整性摘要：
// 只模拟能穿过持久层的篡改，协议层的账本校验才是被测边界。
func tamperStateContent(t *testing.T, dir string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		t.Fatalf("decode state document: %v", err)
	}
	var content map[string]any
	if err := json.Unmarshal(outer["content"], &content); err != nil {
		t.Fatalf("decode content: %v", err)
	}
	mutate(content)
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("encode content: %v", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		t.Fatalf("compact content: %v", err)
	}
	outer["content"] = json.RawMessage(encoded)
	outer["contentDigest"] = json.RawMessage(`"` + encoder.Digest(compact.Bytes()) + `"`)
	out, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("encode document: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

// TestReviewVariantsParticipateInCanonicalDigest：审查证据与决定载荷进入
// canonical 编码——内容变化必变 digest，事件身份覆盖变体字节。
func TestReviewVariantsParticipateInCanonicalDigest(t *testing.T) {
	evidence := reviewEvidence(review.ProductReview, "dispatch-1", "", "FAIL", "blocked", []review.Finding{
		{Severity: "P1", Message: "core path broken"},
	})
	evidence.DispatchID = "act:a"
	first, err := NewReviewWorkerResultEvent("evt-digest", "act:a", testProvider, "sha256:payload", evidence)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	digest, err := first.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	evidence.DispatchID = "act:a"
	repeated, err := NewReviewWorkerResultEvent("evt-digest", "act:a", testProvider, "sha256:payload", evidence)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if again, _ := repeated.Digest(); again != digest {
		t.Fatal("review result digest is not stable")
	}
	evidence.Status = "PASS"
	evidence.DispatchID = "act:a"
	changed, err := NewReviewWorkerResultEvent("evt-digest", "act:a", testProvider, "sha256:payload", evidence)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if again, _ := changed.Digest(); again == digest {
		t.Fatal("review evidence bytes do not participate in the event digest")
	}

	payload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: "n"}}
	decide, err := NewReviewDecideEvent("evt-decide-digest", "req-1", "sha256:token", "no-split", payload)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	decideDigest, err := decide.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	payload.Topology.Note = "different note"
	decideChanged, err := NewReviewDecideEvent("evt-decide-digest", "req-1", "sha256:token", "no-split", payload)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if again, _ := decideChanged.Digest(); again == decideDigest {
		t.Fatal("review decision payload does not participate in the event digest")
	}
}

// TestReviewPhaseDialPastRejectedAtCommit：经公开 API 把 phase 直接初始化
// 到账本无法证明的位置时，提交边界拒绝且零落盘。
func TestReviewPhaseDialPastRejectedAtCommit(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{Definition: cd, Registry: reg, Capacity: 4}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	if err := view.CompleteStep("prep.parse", cd); err != nil {
		t.Fatalf("complete prep: %v", err)
	}
	err = engine.InitWithMetadata(view, testProvider, fingerprintOf(t, engine), "run-dial", "", reviewConfirmation())
	if err == nil {
		t.Fatal("dial-past init accepted")
	} else if code := rejectionCode(t, err); code != CodeStateIntegrityInvalid {
		t.Fatalf("dial-past code = %q", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "state.json")); statErr == nil {
		t.Fatal("rejected init wrote state")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat: %v", statErr)
	}
}

// TestReleasedAuthorizationSlotIsConsumedByTheNextIssuance：授权轮整轮
// INVALIDATE_RESULT 后授权槽释放；释放后的下一 ready 审查步经同一决定事
// 务的收尾补位签出并自动绑上释放槽；占用期不存在未绑定后继——补位签发
// 在授权被占用时被容量闸扣发（该形状不可达）。释放槽在位期间新 grant
// 仍被"存在未占用授权"拒；在途派发经恢复换新（NEW_ATTEMPT）后替换派发
// 同样自动绑定，重试轮在新派发上入账消费它。
func TestReleasedAuthorizationSlotIsConsumedByTheNextIssuance(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "round.two", "round.three", "round.four", "round.five")
	engine, dir := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)

	// 三轮自动容量走满：第四轮被闸，无在途派发。
	first := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-slot-1", first, reviewEvidence(review.ProductReview, first, "", "PASS", "round one", nil))
	round := func(step, event string) string {
		action := pendingActionID(t, engine)
		if got := engine.LoadMustSucceed(t).State.PendingActions[action].Step; got != step {
			t.Fatalf("pending step = %s, want %s", got, step)
		}
		submitReviewResult(t, engine, event, action, reviewEvidence(review.ProductReview, action, "", "PASS", "round "+step, nil))
		return action
	}
	round("round.two", "result-slot-2")
	round("round.three", "result-slot-3")
	if snap := engine.LoadMustSucceed(t); len(snap.State.PendingActions) != 0 {
		t.Fatalf("the fourth round must stay gated at exhausted capacity: %+v", snap.State.PendingActions)
	}

	// 授权一轮（不绑派发）：同事务补位签发第四轮并自动绑定。
	askAndDecide(t, engine, ControlReviewRoundGrant, "req-slot-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "one more round is required",
	}})
	grant := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0]
	fourth := pendingActionID(t, engine)
	if !slices.Contains(grant.DispatchIDs, fourth) {
		t.Fatalf("the granted slot must auto-bind the issued dispatch: %+v", grant)
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-slot-4", fourth, "agent-slot-4"), fp); err != nil {
		t.Fatalf("spawn four: %v", err)
	}
	authorized, err := NewReviewWorkerResultEvent("result-slot-4", fourth, testProvider, "sha256:payload-slot-4", ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: fourth, RequirementRevision: "rev-1", AuthorizationID: grant.ID,
		Status: "PASS", Message: "authorized round", Verification: reviewVerification(),
	})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(authorized, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("authorized round = %v %+v", err, acceptance)
	}

	// 授权占用期：未绑定后继的形状不可达——第五轮被容量闸扣发。
	if snap := engine.LoadMustSucceed(t); len(snap.State.PendingActions) != 0 {
		t.Fatalf("an in-flight successor must not exist while the granted slot is occupied: %+v", snap.State.PendingActions)
	}

	// 整轮失效：授权释放（投影回到 AUTHORIZED_EXTRA_ROUND、容量+1）；同一
	// 决定事务的收尾补位签出第五轮并自动绑上释放槽。
	snap := engine.LoadMustSucceed(t)
	candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[3]
	choice := AskOption{ID: AskOptionID("INVALIDATE_RESULT"), Label: "invalidate"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "req-slot-invalidate", []AskOption{choice}, choice.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: fourth, RequirementRevision: "rev-1",
		ResultDigest: candidate.ResultDigest,
		Decision:     "INVALIDATE_RESULT", Invalidity: "INVALID",
		Reason: "wrong document binding", ExpectedEffectiveRevision: snap.State.ReviewSeries.ProductReview.Effective.Revision,
	}})
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 3 || series.NextRoundRequirement != "AUTHORIZED_EXTRA_ROUND" || series.RemainingCapacity != 1 {
		t.Fatalf("the invalidated round must release its slot: %+v", series)
	}
	fifth := pendingActionID(t, engine)
	if got := snap.State.PendingActions[fifth].Step; got != "round.five" {
		t.Fatalf("the released slot must refill-issue the next ready review step, got %s", got)
	}
	if !slices.Contains(snap.State.ReviewSeries.ProductReview.Authorizations[0].DispatchIDs, fifth) {
		t.Fatalf("the refill dispatch must auto-bind the released slot: %+v", snap.State.ReviewSeries.ProductReview.Authorizations[0])
	}

	// 释放槽在位：新 grant 仍被拒（存在未占用授权）。
	if err := askAndDecideRejecting(t, engine, dir, ControlReviewRoundGrant, "req-slot-grant-2", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "another slot",
	}}); err == nil {
		t.Fatal("a second grant was accepted beside the released slot")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("second-grant code = %q", code)
	}

	// 在途的第五轮派发先经恢复换新（NEW_ATTEMPT 重试）：替换派发同样自动
	// 绑上授权槽，重试轮在新派发上入账消费它。
	plan, _, err := engine.RecoverAttempt(fifth, Interruption{CauseKnown: true}, fp)
	if err != nil || plan.Action != RecoveryNewAttempt {
		t.Fatalf("the in-flight successor must retry on a fresh attempt: %+v err=%v", plan, err)
	}
	replacement := engine.LoadMustSucceed(t).State.ObsoleteActions[fifth].ReplacedBy
	if !slices.Contains(engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0].DispatchIDs, replacement) {
		t.Fatalf("the retry dispatch must auto-bind the same authorization: %+v", engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview.Authorizations[0])
	}
	if _, err := engine.Submit(spawnReceipt(t, "spawn-slot-5", replacement, "agent-slot-5"), fp); err != nil {
		t.Fatalf("spawn retry: %v", err)
	}
	retry, err := NewReviewWorkerResultEvent("result-slot-5", replacement, testProvider, "sha256:payload-slot-5", ReviewResultEvidence{
		Action: review.ProductReview, DispatchID: replacement, RequirementRevision: "rev-1", AuthorizationID: grant.ID,
		Status: "PASS", Message: "retry round on the rebound dispatch", Verification: reviewVerification(),
	})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if acceptance, err := engine.Submit(retry, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("the retry round on the rebound dispatch = %v %+v", err, acceptance)
	}
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Completed != 4 || series.ExtraConsumed != 1 || series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("the retry round must consume the released slot: %+v", series)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// parallelReviewLanesFixture 构造同系列并行审查步定义：prep 后两条并行产
// 品审步（review.lane.a、review.lane.b），lane.a 之后串行 review.followup.one
// 与 review.followup.two——在途计数需要"同系列多条派发同时在途"的形态。
func parallelReviewLanesFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
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
	io := func(bindings ...string) authoring.IO {
		inputs := make([]authoring.InputBinding, 0, len(bindings))
		for _, b := range bindings {
			inputs = append(inputs, authoring.InputBinding{From: authoring.StepID(b), OutputField: "out", ToField: "in"})
		}
		return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
	}
	reviewStep := func(name string, deps ...string) authoring.Step {
		stepIO := io(deps...)
		stepIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
		step, err := authoring.NewAgentStep(header(name, deps...), stepIO, authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
		})
		must(err)
		return step
	}
	prep, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.test.prep"})
	must(err)
	steps := []authoring.Step{
		prep,
		reviewStep("review.lane.a", "prep.parse"),
		reviewStep("review.lane.b", "prep.parse"),
		reviewStep("review.followup.one", "review.lane.a"),
		reviewStep("review.followup.two", "review.followup.one"),
	}
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: steps}, reg)
	must(err)
	return cd, reg
}

// TestInFlightReviewDispatchesCountAgainstSeriesCapacity：签发闸按"已入账
// 轮＋在途轮"合计计——同系列在途派发占用容量额度，后继签发在合计到达
// 自动上限时被扣发；同批并行签出的派发逐个计入；授权落账后的下一签发照
// 常放行并自动绑定。
func TestInFlightReviewDispatchesCountAgainstSeriesCapacity(t *testing.T) {
	cd, reg := parallelReviewLanesFixture(t)
	engine, _ := reviewEngine(t, cd, reg)

	// 两条并行审查步同批签出：批内逐个计入在途（容量 3 覆盖两条）。
	lanes := issueReady(t, engine)
	if len(lanes) != 2 {
		t.Fatalf("parallel review lanes = %v, want both issued", lanes)
	}
	laneB := lanes[1]

	// lane.a 入账（completed 1）：followup.one 补位签出（容量 2 ＞ 在途 1）。
	submitReviewResult(t, engine, "result-lane-a", lanes[0], reviewEvidence(review.ProductReview, lanes[0], "", "PASS", "lane a", nil))
	followup := pendingActionByStep(t, engine, "review.followup.one")

	// followup.one 入账（completed 2）：followup.two 被闸——在途的 lane.b
	// 已占掉最后一个自动名额（容量 1 ≤ 在途 1）。
	submitReviewResult(t, engine, "result-followup-one", followup, reviewEvidence(review.ProductReview, followup, "", "PASS", "followup one", nil))
	snap := engine.LoadMustSucceed(t)
	if pending := snap.State.PendingActions; len(pending) != 1 {
		t.Fatalf("the aggregate gate must leave exactly the in-flight lane: %+v", pending)
	} else if entry := pending[laneB]; entry.Step != "review.lane.b" {
		t.Fatalf("the only in-flight dispatch must be review.lane.b, got %+v", entry)
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 2 || series.RemainingCapacity != 1 {
		t.Fatalf("capacity at the aggregate boundary = %+v", series)
	}

	// lane.b 入账（completed 3）：容量归零，followup.two 仍闸。
	submitReviewResult(t, engine, "result-lane-b", laneB, reviewEvidence(review.ProductReview, laneB, "", "PASS", "lane b", nil))
	snap = engine.LoadMustSucceed(t)
	if len(snap.State.PendingActions) != 0 {
		t.Fatalf("the exhausted series must gate the successor: %+v", snap.State.PendingActions)
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 3 || series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("exhausted capacity = %+v", series)
	}

	// 授权一轮：同事务补位签出 followup.two 并自动绑定。
	acceptance := askAndDecide(t, engine, ControlReviewRoundGrant, "req-lanes-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "one more round after the lanes settle",
	}})
	snap = engine.LoadMustSucceed(t)
	final := pendingActionByStep(t, engine, "review.followup.two")
	if len(acceptance.Refill) != 1 || acceptance.Refill[0] != final {
		t.Fatalf("the grant transaction must refill-issue the successor: %+v", acceptance.Refill)
	}
	if !slices.Contains(snap.State.ReviewSeries.ProductReview.Authorizations[0].DispatchIDs, final) {
		t.Fatalf("the granted slot must auto-bind the issued dispatch: %+v", snap.State.ReviewSeries.ProductReview.Authorizations[0])
	}
}

// pendingActionByStep 返回指定步骤当前 pending action 的 ID（在途派发多
// 于一条时按步骤定位，不要求唯一 pending）。
func pendingActionByStep(t *testing.T, engine *Engine, step string) string {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	for id, pending := range snap.State.PendingActions {
		if pending.Step == step {
			return id
		}
	}
	t.Fatalf("no pending action for step %s: %+v", step, snap.State.PendingActions)
	return ""
}

// TestUnmarkedDispatchesAreIssuedWhileReviewCapacityIsExhausted：容量闸只
// 作用于带审查系列标记的派发——系列额度耗尽（façade 投影字段可见
// Remaining=0 与"需要用户"）时，无系列标记的后续步骤照常补位签发。
func TestUnmarkedDispatchesAreIssuedWhileReviewCapacityIsExhausted(t *testing.T) {
	cd, reg := agentStepsDefinition(t, func(name string) authoring.AgentSpec {
		if name == "plain.worker" {
			return authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute}
		}
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
		}
	}, "round.one", "round.two", "round.three", "plain.worker")
	engine, _ := reviewEngine(t, cd, reg)

	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		submitReviewResult(t, engine, "result-unmarked-"+string(rune('0'+round)), action, reviewEvidence(review.ProductReview, action, "", "PASS", "round complete", nil))
		if round < 3 {
			action = pendingActionID(t, engine)
		}
	}
	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 3 || series.RemainingCapacity != 0 || series.NextRoundRequirement != "USER_DECISION_REQUIRED" {
		t.Fatalf("exhausted review capacity must be facade-visible: %+v", series)
	}
	pending := snap.State.PendingActions[pendingActionID(t, engine)]
	if pending.Step != "plain.worker" || pending.ReviewSeries != "" {
		t.Fatalf("the issued successor must be the unmarked worker step: %+v", pending)
	}
}
