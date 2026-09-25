// canonical_review_test.go 对 canonical 定义本身做双审走查：不注入任何
// 自定义审查步夹具，受理→产品审→技术审→拓扑→路线全链由
// definition.Workflow() 的编译产物承载——两条审查系列各有定义内 agent 步
// 作为派发载体，双审门（dualReviewPass）在两条系列都携已确认 revision 的
// effective PASS 后打开，拓扑确认与路线选择依次可达。
package protocol

import (
	"testing"

	"formal-gates/internal/engine/authoring"
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
	engine := canonicalReviewEngine(t)

	// 产品审：entry 两步完成后 Ready 前沿只含 review.worker（ask.decide 属
	// Ask 边界、fan.* 属引擎内部，都不进 Ready 派发集）。
	actions := issueReady(t, engine)
	if len(actions) != 1 || actions[0] != "act:review/review.worker" {
		t.Fatalf("issued = %v, want the canonical product review action only", actions)
	}
	product := engine.LoadMustSucceed(t).State.PendingActions[actions[0]]
	if product.ReviewSeries != review.ProductReview {
		t.Fatalf("product review dispatch series = %q, want %s", product.ReviewSeries, review.ProductReview)
	}
	submitReviewResult(t, engine, "canonical-product-1", actions[0],
		reviewEvidence(review.ProductReview, "canonical-product-1", "", "PASS", "clean", nil))
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
		reviewEvidence(review.StartReadiness, "canonical-readiness-1", "", "PASS", "ready to start", nil))
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
	askAndDecide(t, engine, ControlSelectRoute, "canonical-route", routeOptions(), "full", &ReviewDecisionPayload{Route: &RouteDecision{
		Mode: "full", Gates: []string{"gate.quality", "gate.security"},
	}})
	snap = engine.LoadMustSucceed(t)
	if snap.State.RouteMode != "full" {
		t.Fatalf("canonical route binding = %q", snap.State.RouteMode)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after canonical route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// canonicalReviewEngine 构造绑定 canonical 编译定义的已受理引擎：entry
// 两步在视图内完成、受理回执落账，run 处于 PRODUCT_REVIEW。
func canonicalReviewEngine(t *testing.T) *Engine {
	t.Helper()
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{
		Definition: compiledWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	cd := compiledWorkflow(t)
	view, err := decision.NewState(definition.Version, runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
		if err := view.CompleteStep(step, cd); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}
	fp := fingerprintOf(t, engine)
	confirmation := reviewConfirmation()
	if err := engine.InitWithMetadata(view, testProvider, fp, "run-canonical", "", confirmation); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, fp); err != nil {
		t.Fatalf("intake receipt: %v", err)
	}
	return engine
}
