// slice_test.go 覆盖 child run 的继承身份行为：启动即处于拓扑/路线段、
// 继承拓扑不可重切、SELECT_ROUTE 候选不含内建 QA、选路线后推进
// DEVELOPMENT_PARALLEL，且全程不产生自身审查轮（审查结论经继承等价满
// 足，账本校验在加载边界重算通过）。
package protocol

import (
	"slices"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	definition "formal-gates/internal/engine/testkit/workflow"
	"formal-gates/internal/settings"
)

func TestSliceChildInheritsTopologyAndSelectsQAFreeRoute(t *testing.T) {
	// 候选含内建 QA 的路线 Ask 在接纳处拒绝（普通 QA 不进 child 候选）。
	engine, _ := sliceChildEngine(t)
	observedInput := Observation{}
	for name, gates := range map[string][]string{
		"with-blackbox": {"blackbox", "gate.quality"},
		"with-whitebox": {"whitebox", "gate.quality"},
	} {
		if _, err := engine.Submit(controlRequestEvent(t, ControlSelectRoute, "req-child-route-"+name, &ReviewDecisionPayload{Route: &RouteDecision{Selected: gates, Gates: gates}}, routeOptions()), observedInput); err == nil {
			t.Fatalf("QA-carrying child route ask %s accepted", name)
		} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
			t.Fatalf("QA-carrying ask %s code = %q (%v)", name, code, err)
		}
	}

	// QA-free explicit 子集按候选全集顺序归一落账并推进开发段。
	engine, _ = sliceChildEngine(t)
	askAndDecide(t, engine, ControlSelectRoute, "req-child-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Gates: []string{"gate.quality", "gate.security", "gate.release"}, Selected: []string{"gate.release", "gate.quality"}}})
	snap := engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || len(snap.State.SelectedGates) != 2 ||
		snap.State.SelectedGates[0] != "gate.quality" || snap.State.SelectedGates[1] != "gate.release" {
		t.Fatalf("child gate selection binding = %t %v", snap.State.RouteConfirmed, snap.State.SelectedGates)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after child route = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
	// §6.3.1 开发前可重选：绑定后的第二次路线选择 Ask 照常抬起，缩集重
	// 选落账新绑定。
	askAndDecide(t, engine, ControlSelectRoute, "req-child-route-again", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Gates: []string{"gate.quality", "gate.security", "gate.release"}, Selected: []string{"gate.security"}}})
	snap = engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || len(snap.State.SelectedGates) != 1 || snap.State.SelectedGates[0] != "gate.security" {
		t.Fatalf("child re-selected route = %t %v", snap.State.RouteConfirmed, snap.State.SelectedGates)
	}
}

// TestSliceChildRouteDecisionRebindsCoexistingAsk：child 绑定前并存的第
// 二个路线 Ask，其决定在绑定后仍被各自接纳（候选表锚定各自 Ask），缩集
// 重选落账新绑定。
func TestSliceChildRouteDecisionRebindsCoexistingAsk(t *testing.T) {
	engine, _ := sliceChildEngine(t)
	gates := []string{"gate.quality", "gate.security"}
	observedInput := Observation{}
	selected := ConfigurationSelection{Configuration: settings.DefaultConfiguration()}
	selected.SetSelectedGates([]string{"gate.security"})
	for _, requestID := range []string{"req-child-bind", "req-child-coexist"} {
		route := &RouteDecision{Gates: gates, Selected: []string{"gate.quality"}}
		if requestID == "req-child-coexist" {
			route.Configuration = &selected
			route.Selected = []string{"gate.security"}
		}
		if _, err := engine.Submit(controlRequestEvent(t, ControlSelectRoute, requestID, &ReviewDecisionPayload{Route: route}, routeOptions()), observedInput); err != nil {
			t.Fatalf("submit ask %s: %v", requestID, err)
		}
	}
	token, err := engine.Freshness("req-child-bind")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewReviewDecideEvent("evt-child-bind", "req-child-bind", token, "approve", ReviewDecisionPayload{Route: &RouteDecision{Gates: gates, Selected: []string{"gate.quality"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(binding, observedInput); err != nil {
		t.Fatalf("submit binding decide: %v", err)
	}
	coexist, err := engine.Freshness("req-child-coexist")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewReviewDecideEvent("evt-child-coexist", "req-child-coexist", coexist, "approve", ReviewDecisionPayload{Route: &RouteDecision{Gates: gates, Selected: []string{"gate.security"}, Configuration: &selected}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(decision, Observation{}); err != nil {
		t.Fatalf("coexisting child route decide after binding: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || len(snap.State.SelectedGates) != 1 || snap.State.SelectedGates[0] != "gate.security" {
		t.Fatalf("child binding after the coexisting decide = %t %v", snap.State.RouteConfirmed, snap.State.SelectedGates)
	}
	if ask, pending := snap.State.PendingAsks["req-child-coexist"]; !pending || !ask.Resolved {
		t.Fatalf("coexisting child ask did not settle: %+v", ask)
	}
}

func TestSliceChildCanSelectEveryDiscoveredGate(t *testing.T) {
	engine, _ := sliceChildEngine(t)
	gates := []string{"gate.quality", "gate.security", "gate.delivery"}
	askAndDecide(t, engine, ControlSelectRoute, "req-child-route", routeOptions(), "approve", &ReviewDecisionPayload{Route: &RouteDecision{Gates: gates, Selected: gates}})
	snap := engine.LoadMustSucceed(t)
	if !snap.State.RouteConfirmed || !slices.Equal(snap.State.SelectedGates, gates) || !snap.State.routeHasNoQAModes() {
		t.Fatalf("child gate selection = %t %v", snap.State.RouteConfirmed, snap.State.SelectedGates)
	}
	if snap.State.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("phase after child selection = %s, want DEVELOPMENT_PARALLEL", snap.State.Phase)
	}
}

// sliceChildEngine 构造已继承 master 拓扑、受理即处于 TOPOLOGY_AND_ROUTE 的
// child run（继承 revision 与受理确认同为 rev-1；无任何自身审查轮）。
func sliceChildEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, err := New(mustStore(t, dir), Config{InternalSteps: fixtureInternalSteps(), Definition: cd, Registry: reg, Capacity: 4})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseTopologyAndRoute)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	observedInput := Observation{}
	confirmation := reviewConfirmation()
	sliceStart := &SliceRunStart{
		Binding: SliceChildBinding{MasterRunID: "run-master", SliceID: "lane-core", RequirementRevision: "rev-1"},
		Topology: TopologyBinding{
			Decision: topologySplit, SplitCount: 2,
			Slices:   []string{"lane-core", "lane-tooling"},
			Parallel: "two lanes",
			ACOwners: map[string]string{"AC-001": "lane-core", "AC-002": "lane-tooling"},
		},
	}
	if err := engine.InitWithMetadata(view, testProvider, observedInput, "run-child", "", confirmation, "", "", sliceStart); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := engine.RecordIntakeReceipt(IntakeReceipt{Confirmation: *confirmation, IntakeDigest: "sha256:intake"}, observedInput); err != nil {
		t.Fatalf("intake receipt: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if snap.State.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("child phase = %s, want TOPOLOGY_AND_ROUTE", snap.State.Phase)
	}
	if snap.State.SliceChild == nil || snap.State.SliceChild.MasterRunID != "run-master" || snap.State.SliceChild.SliceID != "lane-core" {
		t.Fatalf("child binding = %+v", snap.State.SliceChild)
	}
	if snap.State.Topology == nil || snap.State.Topology.Decision != topologySplit || snap.State.Topology.Slices[1] != "lane-tooling" || snap.State.Topology.ACOwners["AC-002"] != "lane-tooling" {
		t.Fatalf("inherited topology = %+v", snap.State.Topology)
	}
	// 继承即满足整体审查：两条 series 无任何轮次，也不派发审查工作。
	if snap.State.ReviewSeries.ProductReview.Completed != 0 || snap.State.ReviewSeries.StartReadiness.Completed != 0 ||
		len(snap.State.ReviewSeries.ProductReview.RawCandidates) != 0 || len(snap.State.ReviewSeries.StartReadiness.RawCandidates) != 0 {
		t.Fatalf("child carries its own review rounds: %+v", snap.State.ReviewSeries)
	}
	if !snap.State.dualReviewPass() {
		t.Fatal("inherited reviews do not satisfy the dual-review gate")
	}
	if issued := issueReady(t, engine); len(issued) != 0 {
		t.Fatalf("child dispatched review work: %v", issued)
	}
	// 继承绑定不可重切。
	if err := askAndDecideRejecting(t, engine, dir, ControlConfirmTopology, "req-child-topology", topologyOptions(), "split", &ReviewDecisionPayload{Topology: &TopologyDecision{
		Decision: "split", SplitCount: 2, Slices: []string{"re-cut-a", "re-cut-b"}, Parallel: "re-cut",
	}}); err == nil {
		t.Fatal("child topology re-cut accepted")
	} else if code := rejectionCode(t, err); code != CodeReviewDecisionRejected {
		t.Fatalf("child re-cut code = %q (%v)", code, err)
	}
	return engine, dir
}
