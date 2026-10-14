// canonical_review_test.go 对 canonical 定义本身做双审走查：不注入任何
// 自定义审查步夹具，受理→产品审→技术审→拓扑→路线全链由
// definition.Workflow() 的编译产物承载——两条审查系列各有定义内 agent 步
// 作为派发载体，双审门（dualReviewPass）在两条系列都携已确认 revision 的
// effective PASS 后打开，拓扑确认与路线选择依次可达。
package protocol

import (
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// TestCanonicalDualReviewThroughTopologyAndRoute：entry 内部步完成后唯一
// Ready 派发是产品审步；产品审 PASS 入账的同事务补位签发技术审步（start
// -readiness 系列、独立 action）；技术审 PASS 入账后双审门成立、phase 推进
// TOPOLOGY_AND_ROUTE；拓扑确认与路线选择 Ask 相继可达，路线绑定后进入
// DEVELOPMENT_PARALLEL。
func TestCanonicalDualReviewThroughTopologyAndRoute(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)

	// 产品审：entry 注册事实完成后 Ready 前沿只含 review.worker。
	actions := issueReady(t, engine)
	if len(actions) != 1 || actions[0] != "act:review/review.worker" {
		t.Fatalf("issued = %v, want the canonical product review action only", actions)
	}
	product := engine.LoadMustSucceed(t).State.PendingActions[actions[0]]
	if product.ReviewSeries != review.ProductReview {
		t.Fatalf("product review dispatch series = %q, want %s", product.ReviewSeries, review.ProductReview)
	}
	submitReviewResult(t, engine, "canonical-product-1", actions[0],
		plannedReviewEvidence(review.ProductReview, "canonical-product-1", "", "PASS", "clean", nil))
	snap := engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("canonical product ledger = %+v", series)
	}
	if snap.State.Phase != runtime.PhaseTechnicalReview {
		t.Fatalf("phase after canonical product PASS = %s, want TECHNICAL_REVIEW", snap.State.Phase)
	}

	// 技术审：产品审完成后补位签发 review.readiness——canonical 自己的
	// start-readiness 载体，有自己的 action 与系列标记。
	readinessAction := pendingActionID(t, engine)
	if readinessAction == actions[0] {
		t.Fatalf("refill reused the product action %q", readinessAction)
	}
	readiness := engine.LoadMustSucceed(t).State.PendingActions[readinessAction]
	if readiness.Step != "review.readiness" || readiness.ReviewSeries != review.StartReadiness {
		t.Fatalf("canonical readiness dispatch = %+v", readiness)
	}
	submitReviewResult(t, engine, "canonical-readiness-1", readinessAction,
		plannedReviewEvidence(review.StartReadiness, "canonical-readiness-1", "", "PASS", "ready to start", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("canonical readiness ledger = %+v", series)
	}
	// 双审门成立：两条系列都携已确认 revision 的 effective PASS，phase 连推
	// 到 TOPOLOGY_AND_ROUTE。
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase after canonical readiness PASS = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}

	// 拓扑确认 Ask 可达且可决：no-split 绑定落账。
	askAndDecide(t, engine, ControlConfirmTopology, "canonical-topology", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "canonical walk keeps a single lane",
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.Topology == nil || snap.State.Topology.Decision != "no-split" {
		t.Fatalf("canonical topology binding = %+v", snap.State.Topology)
	}

	// 路线选择读取双审门：full 绑定后进入 DEVELOPMENT_PARALLEL。
	confirmPlannedRoute(t, engine, "canonical-route", RouteDecision{
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"},
	})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" {
		t.Fatalf("canonical route binding = %q", snap.State.RouteMode)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after canonical route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// canonicalReviewEngine 构造绑定 canonical 编译定义的已受理引擎：entry
// 注册事实在视图内完成、受理回执落账，run 处于 PRODUCT_REVIEW。
func canonicalReviewEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{
		Definition: compiledCanonicalWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	cd := compiledCanonicalWorkflow(t)
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	if err := view.CompleteStep("entry.registered", cd); err != nil {
		t.Fatalf("complete entry.registered: %v", err)
	}
	observedInput := Observation{}
	confirmation := reviewConfirmation()
	if err := engine.InitWithMetadata(view, testProvider, observedInput, "run-canonical", "", confirmation, "", "", nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, observedInput); err != nil {
		t.Fatalf("intake receipt: %v", err)
	}
	return engine, dir
}

// A failed round awaits disposition. Dismissing its only blocker concludes
// the same round and opens readiness without consuming another review round.
func TestCanonicalProductFailAwaitsDisposition(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "canonical-fail", product,
		plannedReviewEvidence(review.ProductReview, product, "", "FAIL", "blocked", []review.Finding{
			{Severity: "P1", Message: "core acceptance blocked"},
		}))
	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.Effective.Status != "FAIL" || series.Issued != 1 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("failed review must await disposition: %+v, pending=%+v", series, snap.State.PendingActions)
	}
	assertReviewIssuanceHeld(t, engine)
	dismissSeriesFinding(t, engine, "canonical-dismiss", review.ProductReview, product)
	snap = engine.LoadMustSucceed(t)
	series = snap.State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.Issued != 1 || series.Effective.Status != "PASS" || !snap.State.stepCompleted("review.worker") {
		t.Fatalf("dismissal must conclude the original round: %+v", series)
	}
	pendingActionByStep(t, engine, "review.readiness")
}

func assertReviewIssuanceHeld(t *testing.T, engine *Engine) {
	t.Helper()
	plan, _, err := engine.Plan(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Next.Kind == decision.KindReady {
		if issued := issueReady(t, engine); len(issued) != 0 {
			t.Fatalf("review obligation issued extra work: %v", issued)
		}
	}
}

// redoProductReview records an explicit requirement revision and the user's
// decision to review that revision. It does not submit a review result.
func redoProductReview(t *testing.T, engine *Engine, requestID string) {
	t.Helper()
	base := engine.LoadMustSucceed(t).State.RequirementRevision()
	askAndDecideRequirement(t, engine, requestID+"-change", "proceed", &RequirementDecisionPayload{
		BaseRevision: base, CandidateRevision: "rev-" + requestID,
		DeltaDigest: "sha256:delta-" + requestID, ImpactDigest: "sha256:impact-" + requestID,
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, Reason: "the revised requirement needs product review",
	})
}

func TestCanonicalConfirmedBlockerRequiresRevisedRequirement(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)
	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "canonical-confirm-fail", product,
		plannedReviewEvidence(review.ProductReview, product, "", "FAIL", "blocked", []review.Finding{
			{Severity: "P1", Message: "core acceptance blocked"},
		}))
	series := engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	candidate := series.RawCandidates[0]
	choice := AskOption{ID: AskOptionID("CONFIRM:" + candidate.Findings[0].ID), Label: "confirm"}
	askAndDecide(t, engine, ControlReviewFindingDecision, "canonical-confirm", []AskOption{choice}, choice.ID, &ReviewDecisionPayload{Correction: &review.CorrectionInput{
		Action: review.ProductReview, DispatchID: product, RequirementRevision: candidate.RequirementRevision,
		ResultDigest: candidate.ResultDigest, FindingID: candidate.Findings[0].ID,
		Decision: "CONFIRM", Reason: "acceptance cannot succeed", ExpectedEffectiveRevision: series.Effective.Revision,
	}})
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("confirmation alone must not create fresh work: %+v", pending)
	}
	redoProductReview(t, engine, "canonical-revised")
	fresh := pendingActionByStep(t, engine, "review.worker")
	if fresh == product || !strings.HasPrefix(fresh, product+"/") {
		t.Fatalf("fresh round must have a distinct task: %q", fresh)
	}
	assertReviewIssuanceHeld(t, engine)
	revision := engine.LoadMustSucceed(t).State.RequirementRevision()
	submitReviewResult(t, engine, "canonical-revised-pass", fresh,
		reviewEvidenceAt(review.ProductReview, fresh, revision, "PASS", "revised requirement is complete", nil))
	pendingActionByStep(t, engine, "review.readiness")
}

// Three completed reviews consume the automatic allowance. Each subsequent
// review requires a real redo decision; an extra grant only supplies capacity.
func TestCanonicalExhaustionThenGrantEndorsesTheFourthRound(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)
	action := issueReady(t, engine)[0]
	for round := 1; round <= 3; round++ {
		name := "canonical-exhaust-" + string(rune('0'+round))
		revision := engine.LoadMustSucceed(t).State.RequirementRevision()
		submitReviewResult(t, engine, name, action,
			reviewEvidenceAt(review.ProductReview, action, revision, "FAIL", "blocked", []review.Finding{
				{Severity: "P1", Message: "acceptance needs a requirement correction"},
			}))
		if round < 3 {
			redoProductReview(t, engine, name)
			action = pendingActionByStep(t, engine, "review.worker")
		}
	}
	snap := engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 3 || series.RemainingCapacity != 0 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("series at exhaustion = %+v, pending=%+v", series, snap.State.PendingActions)
	}
	askAndDecide(t, engine, ControlReviewRoundGrant, "canonical-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{
		Action: review.ProductReview, Reason: "reserve capacity for the corrected requirement",
	}})
	if pending := engine.LoadMustSucceed(t).State.PendingActions; len(pending) != 0 {
		t.Fatalf("capacity grant must not bypass disposition: %+v", pending)
	}
	redoProductReview(t, engine, "canonical-fourth")
	fourth := pendingActionByStep(t, engine, "review.worker")
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if !slices.Contains(series.Authorizations[0].DispatchIDs, fourth) {
		t.Fatalf("fourth round must bind its grant: %+v", series.Authorizations[0])
	}
	evidence := reviewEvidenceAt(review.ProductReview, fourth, snap.State.RequirementRevision(), "PASS", "corrected requirement", nil)
	evidence.AuthorizationID = series.Authorizations[0].ID
	submitReviewResult(t, engine, "canonical-pass-fourth", fourth, evidence)
	series = engine.LoadMustSucceed(t).State.ReviewSeries.ProductReview
	if series.Completed != 4 || series.ExtraConsumed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("fourth round result = %+v", series)
	}
	pendingActionByStep(t, engine, "review.readiness")
}

// TestCanonicalFullRedoFreshRoundsKeepTheCarriersLatched：双审通过、拓
// 扑与路线绑定后全勾重走——两条系列 FreshOwed、载体步保持完成（一次性
// 闩）、
// 新鲜轮由账本欠轮提案在容量内派发；两条新鲜轮 PASS 各自清偿义务后双审
// 门重开，拓扑与路线重新确认可达开发阶段。
func TestCanonicalFullRedoFreshRoundsKeepTheCarriersLatched(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)

	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "canonical-redo-product-1", product,
		plannedReviewEvidence(review.ProductReview, product, "", "PASS", "clean", nil))
	submitReviewResult(t, engine, "canonical-redo-ready-1", pendingActionByStep(t, engine, "review.readiness"),
		plannedReviewEvidence(review.StartReadiness, pendingActionByStep(t, engine, "review.readiness"), "", "PASS", "ready", nil))
	askAndDecide(t, engine, ControlConfirmTopology, "canonical-redo-topo-1", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "single lane",
	}})
	confirmPlannedRoute(t, engine, "canonical-redo-route-1", RouteDecision{
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"},
	})

	work, err := engine.RequirementWork()
	if err != nil {
		t.Fatal(err)
	}
	choices := []RequirementWorkDecision{}
	for _, item := range work {
		choices = append(choices, RequirementWorkDecision{Work: item, Disposition: RequirementWorkRedo, Reason: "The changed requirement replaces the confirmed implementation obligation"})
	}
	askAndDecideRequirement(t, engine, "canonical-redo-change", "proceed", &RequirementDecisionPayload{WorkDecisions: choices,
		BaseRevision: "rev-1", CandidateRevision: "rev-2", DeltaDigest: "sha256:delta-redo",
		ImpactDigest: "sha256:impact",
		Source:       RequirementSourceUserInitiated, Classification: RequirementClassificationChanged,
		RedoProductReview: true, RedoStartReadiness: true, RedoTopology: true, RedoRoute: true,
		Reason: "the whole walk is affected",
	})
	snap := engine.LoadMustSucceed(t)
	for _, mark := range []review.Action{review.ProductReview, review.StartReadiness} {
		series, _ := snap.State.ReviewSeries.Series(mark)
		if !series.FreshOwed || series.Effective.Status != "PENDING" {
			t.Fatalf("%s after the full redo = %+v", mark, series)
		}
	}
	// 步完成是一次性闩：重走只刷账本结论（PENDING＋欠新鲜轮），载体步保持
	// 完成、不再 reopen——新鲜轮由账本欠轮提案签发，不依赖前沿。
	if !snap.State.stepCompleted("review.worker") || !snap.State.stepCompleted("review.readiness") {
		t.Fatal("the redo must keep both canonical carrier steps completed (one-way latch)")
	}

	// 新鲜轮由账本提案在自动容量内签发：产品审先行（start-readiness 的顺
	// 序门在签发侧把技术审新鲜轮扣住）。
	freshProduct := pendingActionByStep(t, engine, "review.worker")
	submitReviewResult(t, engine, "canonical-redo-product-2", freshProduct,
		reviewEvidenceAt(review.ProductReview, freshProduct, "rev-2", "PASS", "fresh round clean", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.FreshOwed {
		t.Fatalf("the fresh product round must discharge the obligation: %+v", series)
	}
	freshReadiness := pendingActionByStep(t, engine, "review.readiness")
	submitReviewResult(t, engine, "canonical-redo-ready-2", freshReadiness,
		plannedReviewEvidenceAt(review.StartReadiness, freshReadiness, "rev-2", "PASS", "fresh round ready", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.FreshOwed {
		t.Fatalf("the fresh readiness round must discharge the obligation: %+v", series)
	}
	if !snap.State.stepCompleted("review.worker") || !snap.State.stepCompleted("review.readiness") {
		t.Fatal("both carriers must stay completed through the fresh rounds")
	}

	// 双审门重开：拓扑与路线重新确认后回到开发阶段。
	askAndDecide(t, engine, ControlConfirmTopology, "canonical-redo-topo-2", topologyOptions(), "no-split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "no-split", Note: "reconfirmed after the redo",
	}})
	confirmPlannedRoute(t, engine, "canonical-redo-route-2", RouteDecision{
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality", "gate.security"},
	})
	if snap := engine.LoadMustSucceed(t); snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after the redone walk = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// TestCanonicalInvalidateThenRepassReleasesUpstreamVoid：双审通过后整轮
// 作废产品审——产品审系列 PENDING、技术审携结论被置上游失效，载体步保持
// 完成（一次性闩）、产品审新鲜轮由账本提案签出；重新 PASS 后上游失效解
// 除、携结论重投影，技术审不被重新派发。
func TestCanonicalInvalidateThenRepassReleasesUpstreamVoid(t *testing.T) {
	engine, _ := canonicalReviewEngine(t)

	product := issueReady(t, engine)[0]
	submitReviewResult(t, engine, "canonical-void-product-1", product,
		plannedReviewEvidence(review.ProductReview, product, "", "PASS", "clean", nil))
	readiness := pendingActionByStep(t, engine, "review.readiness")
	submitReviewResult(t, engine, "canonical-void-ready-1", readiness,
		plannedReviewEvidence(review.StartReadiness, readiness, "", "PASS", "ready", nil))
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("phase after dual PASS = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}

	invalidateRoundOf(t, engine, "canonical-void-invalidate", product)
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.ProductReview; series.Effective.Status != "PENDING" {
		t.Fatalf("the invalidated product series = %+v", series)
	}
	if series := snap.State.ReviewSeries.StartReadiness; !series.UpstreamVoid || series.Effective.Status != "PENDING" {
		t.Fatalf("the downstream series must be voided and pending: %+v", series)
	}
	// 一次性闩：作废只刷账本结论（PENDING＋上游失效），载体步保持完成。
	if !snap.State.stepCompleted("review.worker") || !snap.State.stepCompleted("review.readiness") {
		t.Fatal("the invalidation must keep both carrier steps completed (one-way latch)")
	}
	if fresh := pendingActionByStep(t, engine, "review.worker"); fresh == "" {
		t.Fatalf("pending actions after the invalidation = %+v, want the fresh product round", snap.State.PendingActions)
	}

	// 重新 PASS：上游失效解除、携结论重投影——载体步本就保持完成（闩单
	// 调），技术审载体不被重新派发（携结论续用，不产生第二条技术审派发）。
	second := pendingActionByStep(t, engine, "review.worker")
	submitReviewResult(t, engine, "canonical-void-product-2", second,
		plannedReviewEvidence(review.ProductReview, second, "", "PASS", "clean again", nil))
	snap = engine.LoadMustSucceed(t)
	if series := snap.State.ReviewSeries.StartReadiness; series.UpstreamVoid || series.Effective.Status != "PASS" || series.Completed != 1 {
		t.Fatalf("the re-pass must release the void and re-project the carried conclusion: %+v", series)
	}
	if !snap.State.stepCompleted("review.worker") || !snap.State.stepCompleted("review.readiness") {
		t.Fatal("both carriers must stay completed after the re-pass")
	}
	if len(snap.State.PendingActions) != 0 {
		t.Fatalf("pending actions after the re-pass = %+v, want none (no readiness re-dispatch)", snap.State.PendingActions)
	}
}

func compiledCanonicalWorkflow(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	return cd
}

func plannedReviewEvidence(action review.Action, dispatch, authorization, status, message string, findings []review.Finding) ReviewResultEvidence {
	e := reviewEvidence(action, dispatch, authorization, status, message, findings)
	if action == review.StartReadiness {
		e.GranularityReview = &GranularityReview{Reason: "One cohesive implementation", VerificationTier: "FINAL", Batches: []DevelopmentBatch{{ID: "implementation", Subtasks: []DevelopmentSubtask{{ID: "body", Description: "Implement the confirmed requirement"}}, Deliverable: "Working public command", PublicEntry: "app run", Artifacts: []string{"app.go"}, Interfaces: "Existing command", StateBoundary: "No persistent state", Risk: "Input validation", DoD: []DevelopmentCriterion{{ID: "outcome", Description: "Expected command outcome"}}, Checks: []DevelopmentCheck{{ID: "build", Purpose: CheckFullBuild, Command: "go build ./..."}, {ID: "test", Purpose: CheckFullTests, Command: "go test ./..."}}, Rollback: "Revert implementation", Recovery: "Repeat from clean state", Handoff: "Build and test outcomes", ContextCost: "One command fits context"}}}
	}
	return e
}
func confirmPlannedRoute(t *testing.T, e *Engine, id string, route RouteDecision) {
	t.Helper()
	p, err := e.DevelopmentPlan(route)
	if err != nil {
		t.Fatal(err)
	}
	route.DevelopmentPlan = &p.Confirmation
	askAndDecide(t, e, ControlSelectRoute, id, routeOptions(), "full", &ReviewDecisionPayload{Route: &route})
}

func plannedReviewEvidenceAt(action review.Action, dispatch, revision, status, message string, findings []review.Finding) ReviewResultEvidence {
	e := plannedReviewEvidence(action, dispatch, "", status, message, findings)
	e.RequirementRevision = revision
	return e
}
