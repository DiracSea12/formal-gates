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
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	definition "formal-gates/internal/engine/testkit/workflow"
)

// reviewStepsFixture constructs serial review carriers after a local prep step.
// Each carrier declares its review series; review obligations determine issuance.
func reviewStepsFixture(t *testing.T, agentSteps ...string) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	// INDEPENDENT_REVIEW 步必填系列归属标记：含 "readiness" 的步按技术审
	// 系列标记，其余载体按产品审系列标记。
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

// declaredSeriesStepsFixture constructs plain semantic-judgment workers. They
// have no review-series authority and cannot submit predevelopment reviews.
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
	if err := engine.InitWithMetadata(view, testProvider, fp, "run-review", "", confirmation, "", nil); err != nil {
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
	if plan.Next.Kind == decision.KindWait || plan.Next.Kind == decision.KindComplete {
		return nil
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

// controlRequestEvent 构造受限控制请求：SELECT_ROUTE 以决定载荷的候选表
// 声明 Ask 的门全集（用户所见与所批一致的正路径形态），其余控制走通用
// 构造器。
func controlRequestEvent(t *testing.T, control ControlKind, requestID string, payload *ReviewDecisionPayload, options []AskOption) Event {
	t.Helper()
	if control == ControlSelectRoute {
		if payload == nil || payload.Route == nil {
			t.Fatalf("route ask %s needs the gate list its decide will carry", requestID)
		}
		ev, err := NewRouteRequestEvent(EventID(requestID), payload.Route.Gates, options...)
		if err != nil {
			t.Fatalf("route request event: %v", err)
		}
		return ev
	}
	ev, err := NewRequestEvent(EventID(requestID), control, options...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	return ev
}

// askAndDecide 走完整两段：创建受限 Ask，取当前 freshness token 后提交
// 决定（payload 为 nil 时是普通决定事件）。
func askAndDecide(t *testing.T, engine *Engine, control ControlKind, requestID string, options []AskOption, choice AskOptionID, payload *ReviewDecisionPayload) Acceptance {
	t.Helper()
	fp := fingerprintOf(t, engine)
	request := controlRequestEvent(t, control, requestID, payload, options)
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

	// start-readiness：PASS 后同事务连推两边到 TOPOLOGY_AND_ROUTE。判定
	// 轮后的自动补发轮在解阻塞后仍在途，按步骤定位技术审派发。
	readinessAction := pendingActionByStep(t, engine, "start.readiness")
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
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" || len(snap.State.SelectedGates) != 4 ||
		snap.State.SelectedGates[0] != "blackbox" || snap.State.SelectedGates[1] != "whitebox" ||
		snap.State.SelectedGates[2] != "gate.quality" || snap.State.SelectedGates[3] != "gate.security" {
		t.Fatalf("route binding = %q %v", snap.State.RouteMode, snap.State.SelectedGates)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// TestSplitTopologyRequiresTheConfirmedACUniverse：split 绑定的 AC 责任映
// 射以确认需求冻结投影的验收条件为宇宙——文档未冻结时绑定可区分拒绝、
// 零状态变化；分片名非标识形状在 schema 层拒绝（owner 引用与 child 认领
// 键都以它进行）。
func TestSplitTopologyRequiresTheConfirmedACUniverse(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))

	if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-topology-split", topologyOptions(), "split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "split", SplitCount: 2, Slices: []string{"lane-core", "lane-tooling"}, Parallel: "two lanes",
		ACOwners: []ACOwner{{AC: "AC-001", Owner: "lane-core"}, {AC: "AC-002", Owner: "lane-tooling"}},
	}}); err == nil {
		t.Fatal("split topology accepted without the confirmed requirement document")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("universe-less split code = %q (%v)", code, err)
	}
	for name, slices := range map[string][]string{
		"uppercase-slice":   {"lane-core", "Lane.Tooling"},
		"whitespace-slice":  {"lane-core", "lane tooling"},
		"underscore-slice":  {"lane-core", "lane_tooling"},
		"empty-slice-entry": {"lane-core", " "},
	} {
		if _, err := NewReviewDecideEvent(EventID("evt-"+name), RequestID("req-topology-"+name), "sha256:token", "split", ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "split", SplitCount: 2, Slices: slices, Parallel: "two lanes",
		}}); err == nil {
			t.Fatalf("%s accepted", name)
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("%s code = %q (%v)", name, code, err)
		}
	}
	// no-split 不得携带 AC 责任参数（与拆分参数同族约束）。
	if _, err := NewReviewDecideEvent("evt-no-split-owners", "req-topology-no-split-owners", "sha256:token", "no-split", ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "建议不拆", ACOwners: []ACOwner{{AC: "AC-001", Owner: masterMergeOwner}},
	}}); err == nil {
		t.Fatal("no-split with AC owners accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("no-split owners code = %q (%v)", code, err)
	}
}

// TestCustomRouteBindsWholeCandidateList：custom 的集合约束是至少选一项
// 且逐项属于候选列表（sliced-runs-confirmation-and-qa-refactor 需求9），
// 无真子集要求——候选表仅一项门且无 QA 模式（full 因必含双内建 QA 不
// 可用）时 custom 选该项仍可绑定；全选多元素候选表时接纳且 SelectedGates
// 与候选全集一致，绑定后推进 DEVELOPMENT_PARALLEL。
func TestCustomRouteBindsWholeCandidateList(t *testing.T) {
	for name, gates := range map[string][]string{
		"single candidate gate": {"quality"},
		"whole candidate list":  {"blackbox", "whitebox", "gate.quality", "gate.security"},
	} {
		t.Run(name, func(t *testing.T) {
			cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
			engine, _ := reviewEngine(t, cd, reg)
			product := issueReady(t, engine)[0]
			submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
			readiness := pendingActionID(t, engine)
			submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
			askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
				Decision: "no-split", Note: "建议不拆",
			}})
			askAndDecide(t, engine, ControlSelectRoute, "req-route-custom-all", routeOptions(), "custom", &ReviewDecisionPayload{Route: &RouteDecision{
				Mode: "custom", Gates: gates, Selected: gates,
			}})
			snap := engine.LoadMustSucceed(t)
			if snap.State.RouteMode != "custom" || !slices.Equal(snap.State.SelectedGates, gates) {
				t.Fatalf("custom route binding = %q %v, want gates %v", snap.State.RouteMode, snap.State.SelectedGates, gates)
			}
			if snap.State.Phase != runtime.PhaseDevelopmentParallel {
				t.Fatalf("phase after custom whole-list route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
			}
		})
	}
}

// TestRouteBindingIsFinalForAskAndDecision：路线绑定即最终（需求 7「路线
// 只问一次」）。绑定后第二次 SELECT_ROUTE 的 Ask 抬起在接纳处可区分拒绝；
// 绑定前已并存的第二个 Ask，其决定在同一绑定后同样可区分拒绝——两处都
// 零状态写入，并存 Ask 保持未决，原绑定不变。
func TestRouteBindingIsFinalForAskAndDecision(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, dir := reviewEngine(t, cd, reg)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
	readiness := pendingActionID(t, engine)
	submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "one cohesive command",
	}})
	gates := []string{"blackbox", "whitebox", "gate.quality"}
	// 绑定前两个 Ask 并存（PendingAsks 按 requestID 各自落账）。
	fp := fingerprintOf(t, engine)
	for _, requestID := range []string{"req-route-binding", "req-route-coexisting"} {
		if _, err := engine.Submit(controlRequestEvent(t, ControlSelectRoute, requestID, &ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}}, routeOptions()), fp); err != nil {
			t.Fatalf("submit ask %s: %v", requestID, err)
		}
	}
	token, err := engine.Freshness("req-route-binding")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewReviewDecideEvent("evt-route-binding", "req-route-binding", token, "full", ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(binding, fp); err != nil {
		t.Fatalf("submit binding decide: %v", err)
	}
	before := stateBytesOf(t, dir)
	// 绑定后的第二次 Ask 抬起即拒。
	if _, err := engine.Submit(controlRequestEvent(t, ControlSelectRoute, "req-route-second", &ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}}, routeOptions()), fingerprintOf(t, engine)); err == nil {
		t.Fatal("second route ask after binding accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("second ask code = %q (%v)", code, err)
	}
	// 并存 Ask 的决定同样拒绝（token 按当前 revision 重取）。
	coexist, err := engine.Freshness("req-route-coexisting")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewReviewDecideEvent("evt-route-coexisting", "req-route-coexisting", coexist, "full", ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(decision, fingerprintOf(t, engine)); err == nil {
		t.Fatal("coexisting route decide after binding accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("coexisting decide code = %q (%v)", code, err)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("rejected re-selection attempts changed state bytes")
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.RouteMode != routeModeFull || len(snap.State.SelectedGates) != len(gates) {
		t.Fatalf("binding after rejections = %q %v", snap.State.RouteMode, snap.State.SelectedGates)
	}
	ask, pending := snap.State.PendingAsks["req-route-coexisting"]
	if !pending || ask.Resolved {
		t.Fatalf("coexisting ask did not survive the rejection: %+v", ask)
	}
}

// TestRouteGateSetPinnedOnAskBindsRequestAndDecision：SELECT_ROUTE 请求声
// 明的候选门全集钉进 Ask 后是决定的确认边界。请求缺全集或全集含
// merge/重复在接纳处可区分拒绝、零写；决定的候选表少列或多列一门（vs
// Ask 钉住的全集）可区分拒绝、零写且 Ask 存活，携带完整全集（顺序无关）
// 的修正决定照常绑定。
func TestRouteGateSetPinnedOnAskBindsRequestAndDecision(t *testing.T) {
	t.Run("request without the gate list is rejected without state writes", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		// 字面构造事件绕开构造器前置校验，让引擎在接纳处拒绝。
		request := Event{ID: EventID("req-route-nogates"), Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectRoute, Options: routeOptions()}}
		before := stateBytesOf(t, dir)
		if _, err := engine.Submit(request, fp); err == nil {
			t.Fatal("route request without the gate list accepted")
		} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
			t.Fatalf("gate-less request code = %q (%v)", code, err)
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected route request changed state bytes")
		}
	})

	t.Run("request gate list carrying merge or a duplicate is rejected without state writes", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		fp := fingerprintOf(t, engine)
		// 保留字全集（merge/merge-gate/merge-qa，命名权威 internal/validate/
		// catalog.go:28-31）一律拒绝零写。
		for name, gates := range map[string][]string{
			"request-with-merge":      {"blackbox", "whitebox", "merge"},
			"request-with-merge-gate": {"blackbox", "whitebox", "merge-gate"},
			"request-with-merge-qa":   {"blackbox", "whitebox", "merge-qa"},
			"request-with-duplicate":  {"blackbox", "whitebox", "gate.quality", "gate.quality"},
		} {
			request := Event{ID: EventID("req-" + name), Kind: KindRequestControl, Request: &RequestPayload{Control: ControlSelectRoute, Options: routeOptions(), RouteGates: gates}}
			before := stateBytesOf(t, dir)
			if _, err := engine.Submit(request, fp); err == nil {
				t.Fatalf("%s accepted", name)
			} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
				t.Fatalf("%s code = %q (%v)", name, code, err)
			}
			if after := stateBytesOf(t, dir); string(before) != string(after) {
				t.Fatalf("rejected %s changed state bytes", name)
			}
		}
	})

	t.Run("decide diverging from the pinned set is rejected, the ask survives and a corrected decide binds", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆",
		}})
		pinned := []string{"blackbox", "whitebox", "gate.security"}
		request, err := NewRouteRequestEvent("req-route-diverging", pinned, routeOptions()...)
		if err != nil {
			t.Fatalf("route request: %v", err)
		}
		fp := fingerprintOf(t, engine)
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit route request: %v", err)
		}
		for name, gates := range map[string][]string{
			"decide-missing-one-gate": {"blackbox", "whitebox"},
			"decide-with-extra-gate":  {"blackbox", "whitebox", "gate.security", "gate.extra"},
		} {
			token, err := engine.Freshness("req-route-diverging")
			if err != nil {
				t.Fatalf("freshness for %s: %v", name, err)
			}
			ev, err := NewReviewDecideEvent(EventID("evt-"+name), "req-route-diverging", token, "full", ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}})
			if err != nil {
				t.Fatalf("decide %s: %v", name, err)
			}
			before := stateBytesOf(t, dir)
			if _, err := engine.Submit(ev, fp); err == nil {
				t.Fatalf("%s accepted", name)
			} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
				t.Fatalf("%s code = %q (%v)", name, code, err)
			}
			if after := stateBytesOf(t, dir); string(before) != string(after) {
				t.Fatalf("rejected %s changed state bytes", name)
			}
			ask := engine.LoadMustSucceed(t).State.PendingAsks["req-route-diverging"]
			if ask.Resolved || !slices.Equal(ask.RouteGates, pinned) {
				t.Fatalf("after rejecting %s the ask = %+v, want unresolved with pinned gates %v", name, ask, pinned)
			}
		}
		// 修正决定：完整全集且顺序与 Ask 钉住的不同——集合相等即接纳。
		token, err := engine.Freshness("req-route-diverging")
		if err != nil {
			t.Fatalf("freshness for corrected decide: %v", err)
		}
		corrected, err := NewReviewDecideEvent(EventID("evt-route-corrected"), "req-route-diverging", token, "full", ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "full", Gates: []string{"gate.security", "blackbox", "whitebox"},
		}})
		if err != nil {
			t.Fatalf("corrected decide: %v", err)
		}
		if _, err := engine.Submit(corrected, fp); err != nil {
			t.Fatalf("corrected decide rejected: %v", err)
		}
		snap := engine.LoadMustSucceed(t)
		if snap.State.RouteMode != "full" || !sameGateSet(snap.State.SelectedGates, pinned) {
			t.Fatalf("route binding = %q %v, want the pinned set %v", snap.State.RouteMode, snap.State.SelectedGates, pinned)
		}
		if !snap.State.PendingAsks["req-route-diverging"].Resolved {
			t.Fatal("corrected decide left the ask unresolved")
		}
	})
}

// Confirming and downgrading a finding changes its conclusion without adding a review.
func TestReviewFindingDowngradeConcludesTheSameRound(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "review.round")
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

	if product.Issued != 1 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("finding disposition must not consume another review: %+v", product)
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
	// A failed review waits for disposition even while automatic capacity remains.
	if len(engine.LoadMustSucceed(t).State.PendingActions) != 0 {
		t.Fatal("unresolved finding issued another review")
	}
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
		// 产品审先 effective PASS：技术审载体步才可达（拓扑上依赖产品审
		// 载体）并被补位签发；随后整轮作废把产品审刷回 PENDING——在途技
		// 术审证据在产品审重 PASS 之前必须被双审顺序守卫扣住。
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		snap := engine.LoadMustSucceed(t)
		candidate := snap.State.ReviewSeries.ProductReview.RawCandidates[0]
		invalidate := AskOption{ID: AskOptionID("INVALIDATE_RESULT"), Label: "invalidate"}
		askAndDecide(t, engine, ControlReviewFindingDecision, "req-invalidate-early", []AskOption{invalidate}, invalidate.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: review.ProductReview, DispatchID: candidate.DispatchID, RequirementRevision: "rev-1",
			ResultDigest: candidate.ResultDigest,
			Decision:     "INVALIDATE_RESULT", Invalidity: "INVALID",
			Reason: "the PASS round reviewed the wrong revision", ExpectedEffectiveRevision: snap.State.ReviewSeries.ProductReview.Effective.Revision,
		}})
		if series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview; series.Effective.Status != "PENDING" {
			t.Fatalf("the invalidated product round must leave the series pending: %+v", series.Effective)
		}
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
			Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality"},
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

	t.Run("route candidates missing a built-in QA mode for full or carrying merge are rejected without state writes", func(t *testing.T) {
		cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
		engine, dir := reviewEngine(t, cd, reg)
		product := issueReady(t, engine)[0]
		submitReviewResult(t, engine, "result-product-1", product, reviewEvidence(review.ProductReview, "dispatch-product-1", "", "PASS", "clean", nil))
		readiness := pendingActionID(t, engine)
		submitReviewResult(t, engine, "result-readiness-1", readiness, reviewEvidence(review.StartReadiness, "dispatch-readiness-1", "", "PASS", "ready", nil))
		askAndDecide(t, engine, ControlConfirmTopology, "req-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
			Decision: "no-split", Note: "建议不拆",
		}})
		fp := fingerprintOf(t, engine)
		for name, route := range map[string]*RouteDecision{
			"full-with-merge":      {Mode: "full", Gates: []string{"blackbox", "whitebox", "merge", "gate.quality"}},
			"custom-with-merge":    {Mode: "custom", Gates: []string{"blackbox", "whitebox", "merge", "gate.quality"}, Selected: []string{"gate.quality"}},
			"full-with-merge-gate": {Mode: "full", Gates: []string{"blackbox", "whitebox", "merge-gate", "gate.quality"}},
			"custom-with-merge-qa": {Mode: "custom", Gates: []string{"blackbox", "whitebox", "merge-qa", "gate.quality"}, Selected: []string{"gate.quality"}},
		} {
			// 字面构造事件绕开构造器前置校验，让引擎在 Submit 边界拒绝；
			// 拒绝必须零状态写入。
			ev := Event{ID: EventID("evt-" + name), Kind: KindDecide, Decide: &DecidePayload{
				Request: RequestID("req-route-" + name), Token: "sha256:token", Choice: AskOptionID(route.Mode),
				Review: &ReviewDecisionPayload{Route: route},
			}}
			before := stateBytesOf(t, dir)
			if _, err := engine.Submit(ev, fp); err == nil {
				t.Fatalf("%s accepted", name)
			} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
				t.Fatalf("%s code = %q (%v)", name, code, err)
			}
			if after := stateBytesOf(t, dir); string(before) != string(after) {
				t.Fatalf("rejected %s changed state bytes", name)
			}
		}
		// full 缺内建 QA 是 run 形态相关约束（child 的 full 不含 QA），在接
		// 纳层判定：真实 Ask 上提交缺 QA 的 full 决定被可区分拒绝、零写且
		// Ask 存活。
		for name, gates := range map[string][]string{
			"full-without-whitebox": {"blackbox", "gate.quality"},
			"full-without-blackbox": {"whitebox", "gate.quality"},
		} {
			request := controlRequestEvent(t, ControlSelectRoute, "req-route-"+name, &ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}}, routeOptions())
			if _, err := engine.Submit(request, fp); err != nil {
				t.Fatalf("route ask %s: %v", name, err)
			}
			token, err := engine.Freshness(RequestID("req-route-" + name))
			if err != nil {
				t.Fatalf("freshness %s: %v", name, err)
			}
			event, err := NewReviewDecideEvent(EventID("evt-"+name), RequestID("req-route-"+name), token, "full", ReviewDecisionPayload{Route: &RouteDecision{Mode: "full", Gates: gates}})
			if err != nil {
				t.Fatalf("decide event %s: %v", name, err)
			}
			before := stateBytesOf(t, dir)
			if _, err := engine.Submit(event, fp); err == nil {
				t.Fatalf("%s accepted", name)
			} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
				t.Fatalf("%s code = %q (%v)", name, code, err)
			}
			if after := stateBytesOf(t, dir); string(before) != string(after) {
				t.Fatalf("rejected %s changed state bytes", name)
			}
		}
		// 正向对照：合法 custom 选择照常接纳。
		askAndDecide(t, engine, ControlSelectRoute, "req-route-custom-subset", routeOptions(), "custom", &ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "custom", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"}, Selected: []string{"gate.quality"},
		}})
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

	t.Run("custom route with an empty selection is schema invalid", func(t *testing.T) {
		if _, err := NewReviewDecideEvent("evt-empty", "req-route", "sha256:token", "custom", ReviewDecisionPayload{Route: &RouteDecision{
			Mode: "custom", Gates: []string{"gate.a", "gate.b"}, Selected: []string{},
		}}); err == nil {
			t.Fatal("empty custom subset constructed")
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
		request, err := NewRequestEvent("req-cancel-payload", ControlAbort, AskOption{ID: "cancel", Label: "取消终止"})
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		token, err := engine.Freshness("req-cancel-payload")
		if err != nil {
			t.Fatalf("freshness: %v", err)
		}
		payload := ReviewDecisionPayload{Topology: &TopologyDecision{Decision: "no-split", Note: " smuggled"}}
		ev, err := NewReviewDecideEvent("evt-cancel-payload", "req-cancel-payload", token, "cancel", payload)
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
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality"},
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
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality"},
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
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"},
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

// Invalid signed review evidence consumes its attempt only after receipt pairing.
func TestInvalidReviewEvidenceClosesAttemptAfterReceiptPairing(t *testing.T) {
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
			engine, _ := reviewEngine(t, cd, reg)
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
			if a, err := engine.Submit(ev, fp); err != nil || a.Status != "STAGED" {
				t.Fatalf("result before receipt: %+v %v", a, err)
			}
			submitSpawnReceipt(t, engine, "spawn-pair", product)
			snap := engine.LoadMustSucceed(t)
			if snap.State.RejectedResults[product].Status != "REJECTED_INVALID_RESULT" || len(snap.State.StagedResults) != 0 || snap.State.ReviewSeries.ProductReview.Completed != 0 {
				t.Fatalf("invalid evidence effects: %+v", snap.State.RejectedResults)
			}
			replacement := pendingActionID(t, engine)
			if snap.State.PendingActions[replacement].Task != snap.State.ObsoleteActions[product].Task {
				t.Fatal("invalid result changed logical duty")
			}
			product = replacement
			submitSpawnReceipt(t, engine, "replacement-spawn", product)

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
			snap = engine.LoadMustSucceed(t)
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
	request := controlRequestEvent(t, control, requestID, payload, options)
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
	err = engine.InitWithMetadata(view, testProvider, fingerprintOf(t, engine), "run-dial", "", reviewConfirmation(), "", nil)
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

// exhaustProductReviewCapacity completes three explicitly requested reviews of
// successive requirement revisions. The final revision has a PASS conclusion.
func exhaustProductReviewCapacity(t *testing.T, engine *Engine) {
	t.Helper()
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		name := "capacity-" + string(rune('0'+round))
		revision := engine.LoadMustSucceed(t).State.RequirementRevision()
		submitReviewResult(t, engine, name, action,
			reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "reviewed revision "+revision, nil))
		if round < 3 {
			redoProductReview(t, engine, name)
			action = pendingActionID(t, engine)
		}
	}
}

func TestReleasedAuthorizationSlotIsConsumedByTheNextIssuance(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, dir := reviewEngine(t, cd, reg)
	exhaustProductReviewCapacity(t, engine)
	redoProductReview(t, engine, "slot-extra")
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("exhausted capacity issued work: %+v", pending)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "slot-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "review the changed requirement",
	}})
	fourth := pendingActionID(t, engine)
	snap := engine.LoadMustSucceed(t)
	auth := snap.State.ReviewSeries.ProductReview.Authorizations[0].ID
	revision := snap.State.RequirementRevision()
	evidence := reviewEvidenceAt(review.ProductReview, fourth, revision, "PASS", "extra review", nil)
	evidence.AuthorizationID = auth
	submitReviewResult(t, engine, "slot-fourth", fourth, evidence)
	invalidateRoundOf(t, engine, "slot-invalid", fourth)
	fifth := pendingActionID(t, engine)
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 3 || !slices.Contains(series.Authorizations[0].DispatchIDs, fifth) {
		t.Fatalf("invalid result must release and rebind its slot: %+v", series)
	}
	if err := askAndDecideRejecting(t, engine, dir, ControlReviewRoundGrant, "slot-duplicate-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "another slot",
	}}); err == nil {
		t.Fatal("granted another slot while the released slot is unused")
	}
	observedStoppedWorker(t, engine, fifth)
	plan, _, err := engine.RecoverAttempt(fifth, Interruption{CauseKnown: true}, fingerprintOf(t, engine))
	if err != nil || plan.Action != RecoveryNewAttempt {
		t.Fatalf("retry = %+v, %v", plan, err)
	}
	replacement := engine.LoadMustSucceed(t).State.ObsoleteActions[fifth].ReplacedBy
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if !slices.Contains(series.Authorizations[0].DispatchIDs, replacement) {
		t.Fatalf("retry lost the same authorization: %+v", series.Authorizations)
	}
	evidence.DispatchID = replacement
	evidence.Message = "retried review"
	submitReviewResult(t, engine, "slot-retry", replacement, evidence)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 4 || series.ExtraConsumed != 1 || series.RemainingCapacity != 0 {
		t.Fatalf("retried round did not consume its slot: %+v", series)
	}
}

// dagStep 声明产品审 DAG 中的一个 agent 步：步名与显式依赖。
type dagStep struct {
	name string
	deps []string
}

// productReviewDAGFixture 构造 prep 本地步后接显式依赖 DAG 的产品审
// agent 步定义（步骤按声明序给定，签发序以引擎实际派发为准）。
func productReviewDAGFixture(t *testing.T, steps ...dagStep) (*compiler.CompiledDefinition, *compiler.Registry) {
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
	definitionSteps := []authoring.Step{prep}
	for _, step := range steps {
		definitionSteps = append(definitionSteps, reviewStep(step.name, step.deps...))
	}
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: definitionSteps}, reg)
	must(err)
	return cd, reg
}

// parallelReviewLanesFixture declares sibling and sequential carriers of one
// review series. Their shared obligation must never create parallel rounds.
func parallelReviewLanesFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	return productReviewDAGFixture(t,
		dagStep{name: "review.lane.a", deps: []string{"prep.parse"}},
		dagStep{name: "review.lane.b", deps: []string{"prep.parse"}},
		dagStep{name: "review.followup.one", deps: []string{"review.lane.a"}},
		dagStep{name: "review.followup.two", deps: []string{"review.followup.one"}},
	)
}

// Multiple declared carriers share one series obligation. Repeated issuance,
// sibling readiness, and spare automatic capacity cannot duplicate that work.
func TestReviewCarriersShareOneOutstandingObligation(t *testing.T) {
	cd, reg := parallelReviewLanesFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	lanes := issueReady(t, engine)
	if len(lanes) != 1 {
		t.Fatalf("one initial review obligation issued %v", lanes)
	}
	if more := issueReady(t, engine); len(more) != 0 {
		t.Fatalf("an in-flight review was duplicated: %v", more)
	}
	submitReviewResult(t, engine, "carrier-fail", lanes[0], blockingReviewEvidence(review.ProductReview, lanes[0], "unresolved finding"))
	if more := issueReady(t, engine); len(more) != 0 {
		t.Fatalf("a sibling carrier bypassed disposition: %v", more)
	}
	redoProductReview(t, engine, "carrier-redo")
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.PendingActions) != 1 || snap.State.ReviewSeries.ProductReview.Issued != 2 {
		t.Fatalf("one redo must issue one review: %+v", snap.State.PendingActions)
	}
	assertSeriesCommitmentHeld(t, engine)
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
		if name == "plain.gate" || name == "plain.worker" {
			return authoring.AgentSpec{Handler: "engine.test.reviewer", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute}
		}
		return authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: authoring.ReviewSeriesProductReview, Timeout: time.Minute,
		}
	}, "round.one", "plain.gate", "plain.worker")
	engine, _ := reviewEngine(t, cd, reg)

	action := issueReady(t, engine)[0]
	for round := 1; round <= 2; round++ {
		name := "unmarked-" + string(rune('0'+round))
		revision := engine.LoadMustSucceed(t).State.RequirementRevision()
		submitReviewResult(t, engine, name, action, reviewEvidenceAt(review.ProductReview, action, revision, "FAIL", "acceptance needs revision", []review.Finding{
			{Severity: "P1", Message: "requirement omits acceptance"},
		}))
		redoProductReview(t, engine, name)
		action = pendingActionID(t, engine)
	}
	revision := engine.LoadMustSucceed(t).State.RequirementRevision()
	submitReviewResult(t, engine, "unmarked-pass", action, reviewEvidenceAt(review.ProductReview, action, revision, "PASS", "acceptance complete", nil))
	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 3 || series.RemainingCapacity != 0 {
		t.Fatalf("exhausted review capacity must be facade-visible: %+v", series)
	}
	pending := snap.State.PendingActions[pendingActionID(t, engine)]
	if pending.Step != "plain.gate" || pending.ReviewSeries != "" {
		t.Fatalf("the issued successor must be the unmarked gate step: %+v", pending)
	}
	// 容量保持耗尽：无标记后继照常补位签发。
	submitPlainResult(t, engine, "result-unmarked-gate", pending.ActionID)
	if worker := engine.LoadMustSucceed(t).State.PendingActions[pendingActionID(t, engine)]; worker.Step != "plain.worker" || worker.ReviewSeries != "" {
		t.Fatalf("the unmarked worker must issue while the series stays exhausted: %+v", worker)
	}
}

// pendingActionOfStep 返回某步当前在途派发的身份（无在途派发时为空），
// 供"未签出"负控断言读取。
func pendingActionOfStep(t *testing.T, engine *Engine, step string) string {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	for id, pending := range snap.State.PendingActions {
		if pending.Step == step {
			return id
		}
	}
	return ""
}

// dismissSeriesFinding 以用户纠正驳回绑定到 dispatchID 的候选的首个
// finding（REVIEW_FINDING_DECISION 决定）：聚合按 draft §4.4 重算，被驳回
// 的 P0/P1 不再拦后续轮的 PASS。
func dismissSeriesFinding(t *testing.T, engine *Engine, requestID string, action review.Action, dispatchID string) {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	series, _ := snap.State.ReviewSeries.Series(action)
	for _, candidate := range series.RawCandidates {
		if candidate.DispatchID != dispatchID || len(candidate.Findings) == 0 {
			continue
		}
		choice := AskOption{ID: AskOptionID("dismiss:" + candidate.Findings[0].ID), Label: "dismiss"}
		askAndDecide(t, engine, ControlReviewFindingDecision, requestID, []AskOption{choice}, choice.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: action, DispatchID: candidate.DispatchID, RequirementRevision: candidate.RequirementRevision,
			ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
			Decision: "DISMISS", Reason: "the round-one blocker is rejected after re-verification",
			ExpectedEffectiveRevision: series.Effective.Revision,
		}})
		return
	}
	t.Fatalf("no candidate with findings bound to dispatch %s on %s: %+v", dispatchID, action, series.RawCandidates)
}

// invalidateRoundOf 整轮作废绑定到 dispatchID 的候选。
func invalidateRoundOf(t *testing.T, engine *Engine, requestID string, dispatchID string) {
	t.Helper()
	invalidateSeriesRoundOf(t, engine, requestID, review.ProductReview, dispatchID)
}

// invalidateSeriesRoundOf 整轮作废指定系列上绑定给定派发的已入账轮。
func invalidateSeriesRoundOf(t *testing.T, engine *Engine, requestID string, action review.Action, dispatchID string) {
	t.Helper()
	snap := engine.LoadMustSucceed(t)
	series, _ := snap.State.ReviewSeries.Series(action)
	for _, candidate := range series.RawCandidates {
		if candidate.DispatchID != dispatchID {
			continue
		}
		choice := AskOption{ID: AskOptionID("INVALIDATE_RESULT"), Label: "invalidate"}
		askAndDecide(t, engine, ControlReviewFindingDecision, requestID, []AskOption{choice}, choice.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
			Action: action, DispatchID: candidate.DispatchID, RequirementRevision: candidate.RequirementRevision,
			ResultDigest: candidate.ResultDigest,
			Decision:     "INVALIDATE_RESULT", Invalidity: "INVALID",
			Reason: "the round was misrecorded", ExpectedEffectiveRevision: series.Effective.Revision,
		}})
		return
	}
	t.Fatalf("no candidate bound to dispatch %s", dispatchID)
}

// Extra authorizations remain distinct across completed, invalidated, and
// retried reviews. A new obligation may use only an unoccupied slot.
func TestIssuedOverLimitDispatchesBindExclusiveAuthorizationSlots(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	engine, dir := reviewEngine(t, cd, reg)
	exhaustProductReviewCapacity(t, engine)
	rounds := []string{}
	for _, name := range []string{"first", "second"} {
		redoProductReview(t, engine, "exclusive-"+name)
		askAndDecide(t, engine, ControlReviewRoundGrant, "exclusive-grant-"+name, grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
			Action: review.ProductReview, Reason: "review the revised requirement",
		}})
		action := pendingActionID(t, engine)
		rounds = append(rounds, action)
		snap := engine.LoadMustSucceed(t)
		auth := snap.State.ReviewSeries.ProductReview.Authorizations[len(rounds)-1]
		if !slices.Contains(auth.DispatchIDs, action) {
			t.Fatalf("dispatch did not bind its available slot: %+v", auth)
		}
		evidence := reviewEvidenceAt(review.ProductReview, action, snap.State.RequirementRevision(), "PASS", name+" extra review", nil)
		evidence.AuthorizationID = auth.ID
		submitReviewResult(t, engine, "exclusive-result-"+name, action, evidence)
	}
	for index, previous := range rounds {
		name := "exclusive-reuse-" + string(rune('1'+index))
		invalidateRoundOf(t, engine, name+"-invalidate", previous)
		if len(engine.LoadMustSucceed(t).State.PendingActions) != 0 {
			t.Fatal("invalidating an older round must not duplicate the current PASS")
		}
		redoProductReview(t, engine, name)
		action := pendingActionID(t, engine)
		snap := engine.LoadMustSucceed(t)
		series := snap.State.ReviewSeries.ProductReview
		auth := series.Authorizations[index]
		if !slices.Contains(auth.DispatchIDs, action) || review.AuthorizationOfDispatch(series, action) != auth.ID {
			t.Fatalf("dispatch did not exclusively bind the released slot: %+v", series.Authorizations)
		}
		submitSpawnReceipt(t, engine, name+"-spawn", action)
		before := stateBytesOf(t, dir)
		for _, invalidAuth := range []string{"", series.Authorizations[1-index].ID} {
			evidence := reviewEvidenceAt(review.ProductReview, action, snap.State.RequirementRevision(), "PASS", "wrong authorization", nil)
			evidence.AuthorizationID = invalidAuth
			event, err := NewReviewWorkerResultEvent(EventID(name+invalidAuth), action, testProvider, "sha256:wrong-"+invalidAuth, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = engine.Submit(event, fingerprintOf(t, engine)); err == nil || rejectionCode(t, err) != CodeReviewEvidenceRejected {
				t.Fatalf("wrong authorization admitted: %v", err)
			}
		}
		if after := stateBytesOf(t, dir); string(before) != string(after) {
			t.Fatal("rejected evidence mutated state")
		}
		evidence := reviewEvidenceAt(review.ProductReview, action, snap.State.RequirementRevision(), "PASS", "correct authorization", nil)
		evidence.AuthorizationID = auth.ID
		event, err := NewReviewWorkerResultEvent(EventID(name+"-pass"), action, testProvider, "sha256:"+name+"-pass", evidence)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Submit(event, fingerprintOf(t, engine)); err != nil {
			t.Fatal(err)
		}
		assertSeriesCommitmentHeld(t, engine)
	}
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 5 || series.ExtraConsumed != 2 {
		t.Fatalf("each slot must endorse exactly one effective round: %+v", series)
	}
}

func TestQuarantinedReviewReplaysAfterBarrierLift(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		name := "automatic"
		if authorized {
			name = "authorized"
		}
		t.Run(name, func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			engine, _ := reviewEngine(t, cd, reg)
			if authorized {
				exhaustProductReviewCapacity(t, engine)
				redoProductReview(t, engine, "quarantine-extra")
				askAndDecide(t, engine, ControlReviewRoundGrant, "quarantine-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
					Action: review.ProductReview, Reason: "review the changed requirement",
				}})
			} else {
				issueReady(t, engine)
			}
			action := pendingActionID(t, engine)
			submitSpawnReceipt(t, engine, "quarantine-spawn", action)
			snap := engine.LoadMustSucceed(t)
			evidence := reviewEvidenceAt(review.ProductReview, action, snap.State.RequirementRevision(), "PASS", "review completed during barrier", nil)
			evidence.AuthorizationID = review.AuthorizationOfDispatch(snap.State.ReviewSeries.ProductReview, action)
			request, err := NewRequirementRequestEvent("quarantine-ask", requirementPackageOf(nil), requirementOptions()...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Submit(request, fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
			event, err := NewReviewWorkerResultEvent("quarantine-result", action, testProvider, "sha256:quarantine-result", evidence)
			if err != nil {
				t.Fatal(err)
			}
			acceptance, err := engine.Submit(event, fingerprintOf(t, engine))
			if err != nil || acceptance.Status != "QUARANTINED" {
				t.Fatalf("quarantine = %+v, %v", acceptance, err)
			}
			askAndDecideRequirement(t, engine, "quarantine-ask", "abort", nil)
			snap = engine.LoadMustSucceed(t)
			result, ok := snap.State.Results[action]
			if !ok || result.Review == nil || result.Review.Message != evidence.Message || len(snap.State.QuarantinedResults) != 0 || len(snap.State.ObsoleteResults) != 0 {
				t.Fatalf("replay failed to preserve raw review: %+v", snap.State.Results)
			}
			series := snap.State.ReviewSeries.ProductReview
			wantCompleted, wantExtra := 1, 0
			if authorized {
				wantCompleted, wantExtra = 4, 1
			}
			if series.Completed != wantCompleted || series.ExtraConsumed != wantExtra || series.Effective.Status != "PASS" {
				t.Fatalf("replay miscounted its review: %+v", series)
			}
			assertSeriesCommitmentHeld(t, engine)
		})
	}
}

// relayKind 描述载体链中继步的载荷形态：载体完成可解锁中继步、中继步
// 完成又满足下游载体——下游载体的完成由系列首过闩承载，不经投影迭代。
type relayKind string

const (
	relayLocal relayKind = "local"
	relayAsk   relayKind = "ask"
)

// relayChainFixture 构造同系列双载体步经中继步相连的审查链：产品审系
// 列为 prep→head→mid→tail；start-readiness 系列前插产品审步（顺序门前
// 提），链为 prep→product→head→mid→tail。中继步 local 走机械完成，
// ask 走两阶段决定。
func relayChainFixture(t *testing.T, series authoring.ReviewSeriesAction, mid relayKind) (*compiler.CompiledDefinition, *compiler.Registry) {
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
	must(reg.RegisterAskKind("ask.mid.gate"))
	must(reg.RegisterSchema("schema.ask.request"))
	must(reg.RegisterSchema("schema.ask.response"))
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
	carrier := func(name string, mark authoring.ReviewSeriesAction, deps ...string) authoring.Step {
		stepIO := io(deps...)
		stepIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
		step, err := authoring.NewAgentStep(header(name, deps...), stepIO, authoring.AgentSpec{
			Handler: "engine.test.reviewer", Reason: authoring.ReasonIndependentReview,
			ReviewSeries: mark, Timeout: time.Minute,
		})
		must(err)
		return step
	}
	steps := []authoring.Step{}
	prep, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.test.prep"})
	must(err)
	steps = append(steps, prep)
	headDeps := []string{"prep.parse"}
	if series == authoring.ReviewSeriesStartReadiness {
		steps = append(steps, carrier("product.review", authoring.ReviewSeriesProductReview, "prep.parse"))
		headDeps = []string{"product.review"}
	}
	steps = append(steps, carrier("head.carrier", series, headDeps...))
	switch mid {
	case relayLocal:
		midStep, err := authoring.NewLocalStep(header("mid.local", "head.carrier"), io("head.carrier"), authoring.LocalSpec{Handler: "engine.test.prep"})
		must(err)
		steps = append(steps, midStep)
	case relayAsk:
		midStep, err := authoring.NewHumanAskStep(header("mid.ask", "head.carrier"), authoring.HumanAskSpec{
			AskKind: "ask.mid.gate", RequestSchema: "schema.ask.request", ResponseSchema: "schema.ask.response",
			FreshnessTTL: time.Hour,
		})
		must(err)
		steps = append(steps, midStep)
	default:
		t.Fatalf("fixture: unknown relay kind %q", mid)
	}
	var midName string
	if mid == relayLocal {
		midName = "mid.local"
	} else {
		midName = "mid.ask"
	}
	steps = append(steps, carrier("tail.carrier", series, midName))
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review", Steps: steps}, reg)
	must(err)
	return cd, reg
}

// TestReadinessHeadPassLatchesTheLocalRelayChain：同系列双载体步经 LOCAL
// 中继相连——首载体轮 effective PASS 的一次性闩在同一事务完成下游载体、
// 中继步机械补完，下游载体从不派发；整轮作废后链保持完成（闩不回退），
// 欠轮提案在首载体上签出新鲜轮，重 PASS 恢复 effective PASS 且链保持完
// 成。
func TestReadinessHeadPassLatchesTheLocalRelayChain(t *testing.T) {
	cd, reg := relayChainFixture(t, authoring.ReviewSeriesStartReadiness, relayLocal)
	engine, _ := reviewEngine(t, cd, reg)

	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-relay-local-product", product,
		reviewEvidence(review.ProductReview, product, "", "PASS", "product clean", nil))
	head := pendingActionByStep(t, engine, "head.carrier")
	submitReviewResult(t, engine, "result-relay-local-h1", head,
		reviewEvidence(review.StartReadiness, head, "", "PASS", "head round", nil))
	snap := engine.LoadMustSucceed(t)
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the head pass must complete the relay chain in one transaction: %+v", snap.State.Completed)
		}
	}
	if pending := snap.State.PendingActions; len(pending) != 0 {
		t.Fatalf("the latched tail must never dispatch: %+v", pending)
	}

	// 整轮作废 effective 轮：链保持完成（一次性闩），系列回未结论——欠
	// 轮提案在首载体上签出新鲜轮。
	invalidateSeriesRoundOf(t, engine, "req-relay-local-invalidate", review.StartReadiness, head)
	snap = engine.LoadMustSucceed(t)
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the one-way latch must keep the relay chain completed: %+v", snap.State.Completed)
		}
	}
	fresh := pendingActionByStep(t, engine, "head.carrier")

	// 重 PASS：effective PASS 恢复，链保持完成。
	submitReviewResult(t, engine, "result-relay-local-fresh", fresh,
		reviewEvidence(review.StartReadiness, fresh, "", "PASS", "fresh round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Effective.Status != "PASS" {
		t.Fatalf("the readiness series must repass: %+v", series.Effective)
	}
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the relay chain must stay completed: %+v", snap.State.Completed)
		}
	}
}

// TestReadinessHeadPassLatchesTheAskRelayChain：HUMAN_ASK 中继变体——
// 首载体轮 PASS 置闩后，中继 Ask 步凭两阶段决定机械补完的同一事务里下
// 游载体随闩完成、从不派发；整轮作废后链保持完成，新鲜轮重 PASS 后链保
// 持完成。
func TestReadinessHeadPassLatchesTheAskRelayChain(t *testing.T) {
	cd, reg := relayChainFixture(t, authoring.ReviewSeriesStartReadiness, relayAsk)
	engine, _ := reviewEngine(t, cd, reg)

	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-relay-ask-product", product,
		reviewEvidence(review.ProductReview, product, "", "PASS", "product clean", nil))
	head := pendingActionByStep(t, engine, "head.carrier")
	submitReviewResult(t, engine, "result-relay-ask-h1", head,
		reviewEvidence(review.StartReadiness, head, "", "PASS", "head round", nil))
	askAndDecide(t, engine, ControlAbort, "req-relay-ask-mid", []AskOption{{ID: "cancel", Label: "cancel abort"}}, "cancel", nil)
	snap := engine.LoadMustSucceed(t)
	for _, step := range []string{"head.carrier", "mid.ask", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the decision transaction must complete the relay chain: %+v", snap.State.Completed)
		}
	}
	if pending := snap.State.PendingActions; len(pending) != 0 {
		t.Fatalf("the latched tail must never dispatch: %+v", pending)
	}

	// 整轮作废 effective 轮：链保持完成（一次性闩），欠轮提案在首载体上
	// 签出新鲜轮。
	invalidateSeriesRoundOf(t, engine, "req-relay-ask-invalidate", review.StartReadiness, head)
	snap = engine.LoadMustSucceed(t)
	for _, step := range []string{"head.carrier", "mid.ask", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the one-way latch must keep the relay chain completed: %+v", snap.State.Completed)
		}
	}
	fresh := pendingActionByStep(t, engine, "head.carrier")

	submitReviewResult(t, engine, "result-relay-ask-fresh", fresh,
		reviewEvidence(review.StartReadiness, fresh, "", "PASS", "fresh round", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Effective.Status != "PASS" {
		t.Fatalf("the readiness series must repass: %+v", series.Effective)
	}
	for _, step := range []string{"head.carrier", "mid.ask", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the relay chain must stay completed: %+v", snap.State.Completed)
		}
	}
}

// TestProductRedoFreshRoundKeepsTheRelayChainLatched：产品审双载体步经
// 中继步相连——首载体轮 PASS 的一次性闩在同一事务完成全链、下游载体从
// 不派发；重走决定（不置上游失效）后链保持完成，新鲜轮只欠在首载体上，
// 携当前权威 revision 清偿后链保持完成。
func TestProductRedoFreshRoundKeepsTheRelayChainLatched(t *testing.T) {
	cd, reg := relayChainFixture(t, authoring.ReviewSeriesProductReview, relayLocal)
	engine, dir := reviewEngine(t, cd, reg)

	head := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "result-relay-redo-h1", head,
		reviewEvidence(review.ProductReview, head, "", "PASS", "head round", nil))
	snap := engine.LoadMustSucceed(t)
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the head pass must complete the relay chain in one transaction: %+v", snap.State.Completed)
		}
	}
	if pending := snap.State.PendingActions; len(pending) != 0 {
		t.Fatalf("the latched tail must never dispatch: %+v", pending)
	}

	askAndDecideRequirement(t, engine, "req-relay-redo-change", "proceed", &RequirementDecisionPayload{
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-relay-redo",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "redo the product walk",
	})
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; !series.FreshOwed {
		t.Fatalf("the redo must owe a fresh round: %+v", series)
	}
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the one-way latch must keep the relay chain completed: %+v", snap.State.Completed)
		}
	}
	fresh := pendingActionByStep(t, engine, "head.carrier")
	if fresh == "" {
		t.Fatalf("no fresh round issued on the head: %+v", snap.State.PendingActions)
	}
	// 负控：重走窗口内绑旧 revision 的新鲜轮证据被拒，零状态。
	before := stateBytesOf(t, dir)
	fp := fingerprintOf(t, engine)
	ev, err := NewReviewWorkerResultEvent("result-relay-redo-stale", fresh, testProvider, "sha256:relay-redo-stale",
		reviewEvidence(review.ProductReview, fresh, "", "PASS", "bound to the superseded revision", nil))
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("evidence bound to the superseded revision was accepted during the redo window")
	} else if code := rejectionCode(t, err); code != CodeReviewEvidenceRejected {
		t.Fatalf("stale-bound fresh round code = %q", code)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("the rejected event changed state bytes")
	}

	// 新鲜轮携当前权威 revision 清偿：链保持完成（一次性闩）。
	submitReviewResult(t, engine, "result-relay-redo-fresh", fresh,
		reviewEvidenceAt(review.ProductReview, fresh, "rev-2", "PASS", "fresh round clean", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.FreshOwed || series.Effective.Status != "PASS" || series.Effective.RequirementRevision != "rev-2" {
		t.Fatalf("the fresh round must discharge and repass: %+v", series)
	}
	for _, step := range []string{"head.carrier", "mid.local", "tail.carrier"} {
		if !snap.State.stepCompleted(authoring.StepID(step)) {
			t.Fatalf("the relay chain must stay completed: %+v", snap.State.Completed)
		}
	}
}
