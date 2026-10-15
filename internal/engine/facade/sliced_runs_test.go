// sliced_runs_test.go 覆盖拆分 run 身份层的 façade 行为：split 拓扑绑定的
// AC 责任映射校验与合并路线锁定、child 的继承身份，以及 child 全程继承（无自身审查轮、QA-free 路线）。
package facade

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// splitMasterDriver 建立双审已 PASS 且确认需求文档已冻结的 master（分片
// 绑定起点：AC 责任映射以冻结文档的验收条件为宇宙）。
func splitMasterDriver(t *testing.T) *validationDriver {
	t.Helper()
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, splitCommandPlan())
	d.drive()
	return d
}

// splitTopologyDecision 构造双分片拓扑决定（validationDocument 的唯一验收
// 条件 AC-001 由 owner 承担）。
func splitTopologyDecision(owners ...protocol.ACOwner) *protocol.TopologyDecision {
	return &protocol.TopologyDecision{
		Decision: "split", SplitCount: 2,
		Slices:   []string{"lane-core", "lane-tooling"},
		Parallel: "two lanes",
		ACOwners: owners,
	}
}

// splitMaster 绑定合法 split 拓扑（AC-001 主责 lane-core）并返回驱动。
func splitMaster(t *testing.T) *validationDriver {
	t.Helper()
	d := splitMasterDriver(t)
	d.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{
		Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"}),
	})
	return d
}

// controlError 走完整两段并返回首个错误（Ask 创建或决定），供拒绝断言。
func controlError(t *testing.T, d *validationDriver, control protocol.ControlKind, choice string, payload protocol.ReviewDecisionPayload) error {
	t.Helper()
	id := d.eventID()
	var ask protocol.Event
	var err error
	if control == protocol.ControlSelectRoute && payload.Route != nil {
		ask, err = protocol.NewRouteRequestEvent(id, payload.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		if err == nil {
			ask.Request.Configuration = payload.Route.Configuration
		}
	} else {
		ask, err = protocol.NewRequestEvent(id, control, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
	}
	if err != nil {
		t.Fatal(err)
	}
	_, run, askErr := d.f.Submit(ask)
	if askErr != nil {
		return askErr
	}
	d.run = run
	token := ""
	for _, pending := range run.PendingAsks {
		if pending.RequestID == string(id) {
			token = pending.Token
		}
	}
	if token == "" {
		t.Fatalf("ask %s is not pending after creation", id)
	}
	decide, err := protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(id), token, protocol.AskOptionID(choice), payload)
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = d.f.Submit(decide)
	if err != nil {
		return err
	}
	d.run = run
	return nil
}

// decideRaw 走完整两段，但决定以字面事件提交（不经构造器前置校验），使
// schema 层拒绝落在引擎接纳处——供载荷形状拒绝用例断言。
func decideRaw(t *testing.T, d *validationDriver, control protocol.ControlKind, choice string, payload protocol.ReviewDecisionPayload) error {
	t.Helper()
	id := d.eventID()
	var ask protocol.Event
	var err error
	if control == protocol.ControlSelectRoute && payload.Route != nil {
		ask, err = protocol.NewRouteRequestEvent(id, payload.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
		if err == nil {
			ask.Request.Configuration = payload.Route.Configuration
		}
	} else {
		ask, err = protocol.NewRequestEvent(id, control, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, run, askErr := d.f.Submit(ask); askErr != nil {
		return askErr
	} else {
		d.run = run
	}
	token := ""
	for _, pending := range d.run.PendingAsks {
		if pending.RequestID == string(id) {
			token = pending.Token
		}
	}
	decide := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
		Request: protocol.RequestID(id), Token: token, Choice: protocol.AskOptionID(choice), Review: &payload,
	}}
	_, run, err := d.f.Submit(decide)
	if err != nil {
		return err
	}
	d.run = run
	return nil
}

// driverState 读取 run 的权威状态投影。
func driverState(t *testing.T, d *validationDriver) *protocol.State {
	t.Helper()
	if d.run.Termination != nil && d.run.Termination.Phase == runtime.PhaseTerminal {
		return d.run.Termination
	}
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.State
}

// sliceChildStart 构造 child run 的启动请求（与 master 同根、共享同一 Git
// 夹具 runner；child 身份与受理均由 master 账本派生，不由本载荷证明）。
func sliceChildStart(d *validationDriver, childRunID, sliceID string, receipt IntakeConfirmationReceipt) StartOptions {
	return StartOptions{Capacity: 16, Root: d.f.root, VCSRunner: d.f.vcsRunner, Request: StartRequest{
		RunID: childRunID, Route: "regular", Provider: d.host(), BaseSnapshot: d.run.BaseSnapshot,
		DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt, MasterRunID: d.f.runID, SliceID: sliceID,
	}, Admission: &d.f.admission}
}

// sliceChildReceipt 构造绑定指定 revision 的 child 受理回执载荷。
func sliceChildReceipt(revision string) IntakeConfirmationReceipt {
	receipt := testReceipt()
	receipt.RequirementRevision = revision
	receipt.Artifacts = []IntakeArtifact{{Path: "requirements.md", Revision: revision}}
	return receipt
}

// sliceLanePlan 是宿主为 lane-core 分片派生的片内开发计划（从 master 继承
// 的整体粒度审派生的单批退化形态——child 无自身 readiness 候选，计划随路
// 线决定内联确认）。
func sliceLanePlan() *protocol.GranularityReview {
	plan := commandPlan()
	plan.Reason = "The lane-core slice inherits the master granularity review as one cohesive batch."
	return plan
}

// confirmSliceRoute 是 child 的路线确认辅助：内联提案先随载荷提交预览，
// 取回精确确认后再走两段决定。
func (d *validationDriver) confirmSliceRoute(route protocol.RouteDecision, plan *protocol.GranularityReview) {
	d.t.Helper()
	d.configureRoute(&route)
	d.drive()
	state := driverState(d.t, d)
	master, err := Open(d.f.root, state.SliceChild.MasterRunID)
	if err != nil {
		d.t.Fatal(err)
	}
	source, err := master.engine.Load()
	if err != nil {
		d.t.Fatal(err)
	}
	inherited, err := source.State.SliceDevelopmentPlan(state.SliceChild.SliceID)
	if err != nil {
		d.t.Fatal(err)
	}
	for index := range plan.Batches {
		if plan.Batches[index].RequirementPoints == nil {
			for _, batch := range inherited.Batches {
				if batch.ID == plan.Batches[index].ID {
					plan.Batches[index].RequirementPoints = append([]string{}, batch.RequirementPoints...)
				}
			}
		}
	}
	route.DevelopmentPlan = &protocol.DevelopmentPlanConfirmation{Proposal: plan}
	preview, err := d.f.DevelopmentPlan(route)
	if err != nil {
		d.t.Fatal(err)
	}
	route.DevelopmentPlan = &preview.Confirmation
	d.control(protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route})
}

// abortRun 提交并落账 abort 决定，使 run 离开 ACTIVE（保持 ABORTING：账
// 本与状态目录仍在）。
func abortRun(t *testing.T, d *validationDriver) {
	t.Helper()
	id := d.eventID()
	ask, err := protocol.NewRequestEvent(id, protocol.ControlAbort, protocol.AskOption{ID: "abort", Label: "中止"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(ask)
	token := ""
	for _, pending := range d.run.PendingAsks {
		if pending.RequestID == string(id) {
			token = pending.Token
		}
	}
	decide, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(id), token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	d.submit(decide)
}

// TestSplitTopologyBindsACOwnersAndLocksMergeRoute：AC 责任映射逐项拒绝
// （缺 owner／重复／未知 owner／未知 AC），合法映射（含 master-merge 主责）
// 绑定成功且同事务锁定 RouteConfirmed=true、SelectedGates=[merge-qa, merge-gate]；
// 绑定后 master 不进开发段、不再抬 SELECT_ROUTE。
func TestSplitTopologyBindsACOwnersAndLocksMergeRoute(t *testing.T) {
	for name, owners := range map[string][]protocol.ACOwner{
		"missing-owner": nil,
		"duplicate":     {{AC: "AC-001", Owner: "lane-core"}, {AC: "AC-001", Owner: "lane-tooling"}},
		"unknown-owner": {{AC: "AC-001", Owner: "no-such-slice"}},
		"unknown-ac":    {{AC: "AC-999", Owner: "lane-core"}},
	} {
		t.Run(name, func(t *testing.T) {
			d := splitMasterDriver(t)
			err := controlError(t, d, protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(owners...)})
			if err == nil {
				t.Fatalf("split binding with %s owners accepted", name)
			}
			for _, want := range []string{"primary responsibility", "unknown acceptance condition", "unknown owner"} {
				if strings.Contains(err.Error(), want) {
					if state := driverState(t, d); state.Topology != nil || state.RouteConfirmed {
						t.Fatalf("rejected split binding changed state: %+v", state.Topology)
					}
					return
				}
			}
			t.Fatalf("split binding %s rejection = %v", name, err)
		})
	}

	t.Run("master-merge primary responsibility binds", func(t *testing.T) {
		d := splitMasterDriver(t)
		d.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{
			Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "master-merge"}),
		})
		if state := driverState(t, d); state.Topology == nil || state.Topology.ACOwners["AC-001"] != "master-merge" {
			t.Fatalf("master-merge owner binding = %+v", state.Topology)
		}
	})

	d := splitMaster(t)
	state := driverState(t, d)
	if state.Topology == nil || state.Topology.Decision != "split" {
		t.Fatalf("split topology = %+v", state.Topology)
	}
	if len(state.Topology.ACOwners) != 1 || state.Topology.ACOwners["AC-001"] != "lane-core" {
		t.Fatalf("AC owner map = %v", state.Topology.ACOwners)
	}
	if !state.RouteConfirmed || len(state.SelectedGates) != 2 || state.SelectedGates[0] != "merge-qa" || state.SelectedGates[1] != "merge-gate" {
		t.Fatalf("merge route lock = %t %v", state.RouteConfirmed, state.SelectedGates)
	}
	if state.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("master phase after split binding = %s, want TOPOLOGY_AND_ROUTE", state.Phase)
	}
	for _, ask := range state.PendingAsks {
		if ask.Control == protocol.ControlSelectRoute {
			t.Fatalf("merge-locked master carries a pending SELECT_ROUTE ask: %+v", ask)
		}
	}
	if err := controlError(t, d, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"},
	}}); err == nil {
		t.Fatal("route selection accepted on the merge-locked master")
	}
}

// TestSplitBindingRequiresNoPendingRouteAsk：未决 SELECT_ROUTE Ask 存在时不
// 绑定 split——绑定后的 run 不携带不可能再决定的路线 Ask。
func TestSplitBindingRequiresNoPendingRouteAsk(t *testing.T) {
	d := splitMasterDriver(t)
	ask, err := protocol.NewRouteRequestEvent(d.eventID(), []string{"blackbox", "whitebox", "gate.quality"}, protocol.AskOption{ID: "approve", Label: "全部门"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(ask)
	if err := controlError(t, d, protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{
		Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"}),
	}); err == nil {
		t.Fatal("split binding accepted with a pending route ask")
	} else if !strings.Contains(err.Error(), "route selection ask") {
		t.Fatalf("pending-ask rejection = %v", err)
	}
	if state := driverState(t, d); state.Topology != nil || state.RouteConfirmed {
		t.Fatalf("rejected split binding changed state: %+v", state.Topology)
	}
}

// TestSliceChildInheritsAndSelectsRoute：child 创建成功后 master 记录认领映
// 射、child 状态含继承事实，全程无自身审查轮，携片内计划的 QA-free 路线
// 确认后推进开发段；继承拓扑不可重切。
func TestSliceChildInheritsAndSelectsRoute(t *testing.T) {
	master := splitMaster(t)
	f, run, err := semanticValidationFacade(t, sliceChildStart(master, "child-lane-core", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	if state := driverState(t, master); state.SliceChildren["lane-core"] != f.runID {
		t.Fatalf("master claim map = %v", state.SliceChildren)
	}
	child := &validationDriver{t: t, f: f, run: run, provider: master.host(), git: f.vcs}
	child.drive()
	state := driverState(t, child)
	if state.SliceChild == nil || state.SliceChild.MasterRunID != master.f.runID || state.SliceChild.SliceID != "lane-core" {
		t.Fatalf("child inheritance binding = %+v", state.SliceChild)
	}
	if state.Topology == nil || state.Topology.Decision != "split" || state.Topology.ACOwners["AC-001"] != "lane-core" {
		t.Fatalf("inherited topology = %+v", state.Topology)
	}
	if state.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("child phase = %s, want TOPOLOGY_AND_ROUTE", state.Phase)
	}
	if state.ReviewSeries.ProductReview.Completed != 0 || state.ReviewSeries.StartReadiness.Completed != 0 {
		t.Fatalf("child ran its own review rounds: %+v", state.ReviewSeries)
	}
	if len(state.PendingActions) != 0 {
		t.Fatalf("child dispatched work before its route was selected: %+v", state.PendingActions)
	}
	if err := controlError(t, child, protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "re-cut"}}); err == nil {
		t.Fatal("child topology confirmation accepted")
	}
	// 候选含 QA 的 Ask 拒绝；缺片内计划的决定拒绝（计划随路线内联确认）。
	if err := controlError(t, child, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Gates: []string{"blackbox", "whitebox", "gate.quality"},
	}}); err == nil {
		t.Fatal("QA-carrying child route ask accepted")
	}
	if err := controlError(t, child, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.security"},
	}}); err == nil {
		t.Fatal("child route decision without its slice plan accepted")
	} else if !strings.Contains(err.Error(), "slice development plan") {
		t.Fatalf("plan-less child route error = %v", err)
	}
	if state := driverState(t, child); state.RouteConfirmed {
		t.Fatalf("rejected plan-less decision bound a route: %t", state.RouteConfirmed)
	}
	// 携片内计划的 QA-free 路线照常绑定并推进。
	child.confirmSliceRoute(protocol.RouteDecision{
		Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.security"},
	}, sliceLanePlan())
	state = driverState(t, child)
	if !state.RouteConfirmed || len(state.SelectedGates) != 1 || state.SelectedGates[0] != "gate.security" {
		t.Fatalf("child route binding = %t %v", state.RouteConfirmed, state.SelectedGates)
	}
	if state.Phase != runtime.PhaseDevelopmentParallel {
		t.Fatalf("child phase after route = %s, want DEVELOPMENT_PARALLEL", state.Phase)
	}
}

// operatorBoundary 读取 run 当前的 operator 边界提示（无 operator 边界即失
// 败——本测试的每个观察点都应有明确提示）。
func operatorBoundary(t *testing.T, d *validationDriver) string {
	t.Helper()
	if d.run.Next.Operator == nil || len(d.run.Next.Operator.Facts) == 0 {
		t.Fatalf("no operator boundary: next = %+v", d.run.Next)
	}
	return d.run.Next.Operator.Facts[0].Value
}

// TestSliceChildBoundaryFollowsRouteSelection：child 的 operator 边界随进度
// 前进——未选路线时提示选择分片路线，选完路线后经 adapter 前置进入开发；
// 文档继承 master 的确认绑定，计划随路线一次确认。
func TestSliceChildBoundaryFollowsRouteSelection(t *testing.T) {
	master, child := controlChildren(t)
	child.drive()
	if boundary := operatorBoundary(t, child); !strings.Contains(boundary, "select the slice route") {
		t.Fatalf("boundary before route selection = %q", boundary)
	} else if strings.Contains(boundary, "development plan") {
		t.Fatalf("child boundary carries a development-plan prompt: %q", boundary)
	}
	child.confirmSliceRoute(protocol.RouteDecision{
		Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.quality"},
	}, sliceLanePlan())
	child.drive()
	if child.run.Validation == nil || child.run.Validation.Document.Content != validationDocument || child.run.Validation.RequirementRevision != master.revision {
		t.Fatal("route selection lost the inherited confirmed requirement binding")
	}
	if !child.run.Review.RouteConfirmed {
		t.Fatal("child route confirmation was not retained")
	}
	_, input := planBatchAction(child, "command")
	if input.Development == nil || input.Development.Batch.ID != "command" {
		t.Fatal("confirmed inline plan did not expose its development duty")
	}
}

// TestSliceChildConfirmsInheritedPlanIntoDevelopmentDispatch：child 全程——
// 继承→选 QA-free 路线（携片内计划）→提交 REQUIREMENT 观察→Drive 产生开
// 发派发任务；断言具体 batch/task 与 DevelopmentInput 内容（片内计划即执
// 行契约）。
func TestSliceChildConfirmsInheritedPlanIntoDevelopmentDispatch(t *testing.T) {
	master := splitMaster(t)
	f, run, err := semanticValidationFacade(t, sliceChildStart(master, "child-dispatch", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: master.revision, provider: master.host(), git: f.vcs}
	child.drive()
	route := protocol.RouteDecision{Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"}}
	plan := sliceLanePlan()
	child.confirmSliceRoute(route, plan)

	child.drive()
	child.reopen()
	child.drive()
	id, in := planBatchAction(child, "command")
	if in.Development == nil {
		t.Fatalf("child route produced no development dispatch: inputs=%+v next=%+v", child.run.ValidationInputs, child.run.Next)
	}
	if !reflect.DeepEqual(in.Development.Batch, plan.Batches[0]) {
		t.Fatalf("dispatched batch contract = %+v, want the confirmed slice plan batch", in.Development.Batch)
	}
	if len(in.Development.Dependencies) != 0 {
		t.Fatalf("single-batch slice plan carried dependencies: %+v", in.Development.Dependencies)
	}
	if state := driverState(t, child); len(state.PendingActions) == 0 || state.PendingActions[id].Task != in.Binding.Task {
		t.Fatalf("development dispatch %s is not pending: %+v", id, state.PendingActions)
	}
}

// TestSliceChildRouteWithoutPlanIsRejected：child 路线决定缺片内计划（提案
// 为空）可区分拒绝、零写；摘要与提案不一致同样拒绝。
func TestSliceChildRouteWithoutPlanIsRejected(t *testing.T) {
	master := splitMaster(t)
	f, run, err := semanticValidationFacade(t, sliceChildStart(master, "child-unplanned", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	child := &validationDriver{t: t, f: f, run: run, provider: master.host(), git: f.vcs}
	child.drive()
	before := driverState(t, child)
	route := protocol.RouteDecision{Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"}}
	if err := controlError(t, child, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route}); err == nil {
		t.Fatal("child route decision without a slice plan accepted")
	} else if !strings.Contains(err.Error(), "slice development plan") {
		t.Fatalf("plan-less child route error = %v", err)
	}
	// 摘要与提案不一致：schema 层拒绝。
	mismatched := route
	mismatched.DevelopmentPlan = &protocol.DevelopmentPlanConfirmation{Digest: "sha256:wrong", Proposal: sliceLanePlan()}
	if err := decideRaw(t, child, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &mismatched}); err == nil {
		t.Fatal("child route decision with a mismatched plan digest accepted")
	} else if !strings.Contains(err.Error(), "digest must match its proposal") {
		t.Fatalf("mismatched digest error = %v", err)
	}
	after := driverState(t, child)
	if after.RouteConfirmed || after.SelectedGates != nil || len(after.Decisions) != len(before.Decisions) {
		t.Fatalf("rejected plan-less child route changed state: %t %v", after.RouteConfirmed, after.SelectedGates)
	}
}

// TestChildCreationIsFrozenByRequirementBarrier：需求变化写屏障（Phase A）在
// 位时 child 认领可区分拒绝、零写；Ask 了结后照常认领。
func TestChildCreationIsFrozenByRequirementBarrier(t *testing.T) {
	master := splitMaster(t)
	ask, err := protocol.NewRequestEvent(master.eventID(), protocol.ControlRequestRequirementChange,
		protocol.AskOption{ID: "proceed", Label: "Revise"}, protocol.AskOption{ID: "abort", Label: "Keep"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(ask)
	if master.run.PendingRequirementAsk == nil {
		t.Fatal("requirement barrier not established")
	}
	master.drive()
	if driverState(t, master).ChildCreation != nil {
		t.Fatal("barrier created a child intent")
	}
	if state := driverState(t, master); state.SliceChildren["lane-core"] != "" {
		t.Fatalf("barrier-period start wrote a claim: %v", state.SliceChildren)
	}
	// 了结屏障（放弃变更）后认领照常。
	decide, err := protocol.NewDecideEvent(master.eventID(), protocol.RequestID(master.run.PendingRequirementAsk.RequestID), master.run.PendingRequirementAsk.Token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	master.submit(decide)
	master.drive()
	if state := driverState(t, master); state.SliceChildren["lane-core"] == "" {
		t.Fatalf("post-barrier claim = %v", state.SliceChildren)
	}
}

// TestSplitTopologyRequiresDistinctSliceNames：split 的 Slices 列表逐项唯一
// （分片名是 child 认领键），重复声明在绑定校验（slice.go splitACOwners）
// 可区分拒绝、零写。
func TestSplitTopologyRequiresDistinctSliceNames(t *testing.T) {
	master := splitMasterDriver(t)
	topology := splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})
	topology.Slices = []string{"lane-core", "lane-core"}
	before := driverState(t, master)
	if err := controlError(t, master, protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: topology}); err == nil {
		t.Fatal("split count 2 admitted with one distinct slice identity")
	} else if !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("duplicate slice rejection = %v", err)
	}
	after := driverState(t, master)
	if after.Topology != nil || after.RouteConfirmed || len(after.Decisions) != len(before.Decisions) {
		t.Fatalf("rejected duplicate-name binding changed state: %+v", after.Topology)
	}
}

// TestSliceChildPlanSwapRequiresPerBatchUserDecisions：片内计划的换约纪律
// 与普通 run 同一条规则——开发已启动后，路线重选携带内容不同的片内计划
// 时，逐批变更须有用户工作决定授权：无决定可区分拒绝、零写；带齐决定的
// 换约接纳并切换执行契约（换约后派发批次来自新计划）。开发前的同集同计
// 划重选照常（用户决定本身即确认）。
func TestSliceChildPlanSwapRequiresPerBatchUserDecisions(t *testing.T) {
	master, child := controlChildren(t)
	child.drive()
	first := sliceLanePlan()
	route := protocol.RouteDecision{Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"}}
	child.confirmSliceRoute(route, first)
	// 开发前同集同计划重选：决定本身即再确认，无额外要求。
	child.confirmSliceRoute(route, first)
	if state := driverState(t, child); !state.RouteConfirmed || len(state.SelectedGates) != 1 {
		t.Fatalf("pre-development same-plan reselection = %t %v", state.RouteConfirmed, state.SelectedGates)
	}

	child.drive()
	old, oldInput := planBatchAction(child, "command")
	if oldInput.Development == nil {
		t.Fatal("development did not start under the first plan")
	}
	// 开发后换计划、无用户工作决定：可区分拒绝、零写。
	swapped := sliceLanePlan()
	swapped.Batches[0].RequirementPoints = append([]string{}, first.Batches[0].RequirementPoints...)
	swapped.Batches[0].Deliverable = swapped.Batches[0].Deliverable + " (swapped execution contract)"
	swapRoute := protocol.RouteDecision{Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.quality", "gate.security"}, DevelopmentPlan: &protocol.DevelopmentPlanConfirmation{Proposal: swapped}}
	child.configureRoute(&swapRoute)
	preview, err := child.f.DevelopmentPlan(swapRoute)
	if err != nil {
		t.Fatal(err)
	}
	swapRoute.DevelopmentPlan = &preview.Confirmation
	before := driverState(t, child)
	if err := controlError(t, child, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &swapRoute}); err == nil {
		t.Fatal("plan swap after development start accepted without per-batch user decisions")
	} else if !strings.Contains(err.Error(), "changed plan requires a matching explicit work disposition for batch command") {
		t.Fatalf("plan swap rejection = %v", err)
	}
	after := driverState(t, child)
	if after.RouteConfirmed != before.RouteConfirmed || len(after.SelectedGates) != len(before.SelectedGates) ||
		len(after.Decisions) != len(before.Decisions) || len(after.PendingActions) != len(before.PendingActions) {
		t.Fatalf("rejected plan swap changed state: %t %v decisions=%d pending=%d", after.RouteConfirmed, after.SelectedGates, len(after.Decisions), len(after.PendingActions))
	}
	if _, pending := after.PendingActions[old]; !pending {
		t.Fatal("rejected plan swap disturbed the dispatched batch")
	}
	// 带齐逐批用户决定（需求变化对换约批次 REDO）：换约接纳并切换执行
	// 契约。
	children := requirementChildChoices(t, master)
	matched := false
	for i := range children {
		if children[i].ChildRunID != child.f.runID {
			continue
		}
		for j := range children[i].WorkDecisions {
			work := &children[i].WorkDecisions[j]
			if work.Work.ActionID == old {
				matched = true
				work.Disposition = protocol.RequirementWorkRedo
				work.Reason = "the swapped slice contract replaces the command dispatch"
			}
		}
	}
	if !matched {
		t.Fatal("issued batch missing from master child work decisions")
	}
	document := validationDocument + "\n"
	if err := os.WriteFile(filepath.Join(master.f.root, "requirements.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	intake, err := prepareConfirmedIntake(master.f.root, master.run.IntakeConfirmationReceipt, true)
	if err != nil {
		t.Fatal(err)
	}
	pkg := protocol.RequirementDecisionPackage{Intake: &intake.IntakeCandidate, CandidateRevision: revision, DeltaDigest: "sha256:slice-plan-swap", ImpactDigest: "sha256:command-contract", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, Children: children, Impact: &protocol.RequirementImpact{ChangedItems: []string{"slice command contract"}, TransitiveImpacts: []string{"slice batch replacement"}, CarryReasons: []string{"unchanged sibling obligations"}}}
	for _, work := range master.run.RequirementWork {
		pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged master responsibility"})
	}
	request, err := protocol.NewRequirementRequestEvent(master.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm slice plan change"})
	if err != nil {
		t.Fatal(err)
	}
	master.submit(request)
	ask := master.run.PendingRequirementAsk
	master.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{Intake: pkg.Intake, BaseRevision: ask.BaseRevision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, Children: children, Impact: pkg.Impact, Reason: "authorize the bound slice plan and work changes"}}}.WithRequirementDocument(document))
	child.drive()
	child.confirmSliceRoute(swapRoute, swapped)
	if got := child.run.Review.SelectedGates; len(got) != 2 || !strings.Contains(strings.Join(got, ","), "security") {
		t.Fatalf("swap route binding = %v", got)
	}
	child.drive()
	id, in := planBatchAction(child, "command")
	if id == old || in.Binding.Task == oldInput.Binding.Task {
		t.Fatal("authorized swap kept the old dispatch identity")
	}
	if !strings.Contains(in.Development.Batch.Deliverable, "(swapped execution contract)") {
		t.Fatalf("re-dispatched batch contract = %q, want the swapped plan", in.Development.Batch.Deliverable)
	}
}

// 所选普通门真实执行至 wave COMPLETE，全程不产生 QA 派发。
func TestSliceChildSelectedGatesExecuteToCompletion(t *testing.T) {
	master := splitMaster(t)
	f, run, err := semanticValidationFacade(t, sliceChildStart(master, "child-gates", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: master.revision, provider: master.host(), git: f.vcs}
	route := protocol.RouteDecision{Gates: []string{"quality"}, Selected: []string{"quality"}}
	child.confirmSliceRoute(route, sliceLanePlan())

	child.drive()
	id, in := planBatchAction(child, "command")
	if in.Binding.RequirementRevision == "" {
		t.Fatal("development dispatch has no bound revision")
	}
	child.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	// 候选逐段冻结直至 REVIEW_READY（开发交付物进候选，片内计划收口）。
	child.drive()
	child.drive()
	if child.run.Validation == nil || child.run.Validation.Candidate == nil {
		t.Fatalf("REVIEW_READY candidate missing after development: candidate=%v", child.run.Validation.Candidate)
	}
	// 所选门真实派发并接纳。
	gate, gateInput := child.gateAction("quality")
	if gateInput.Gate == nil || gateInput.Gate.Gate != "quality" {
		t.Fatalf("selected gate dispatch = %+v", gateInput.Gate)
	}
	child.business(gate, protocol.ValidationResult{Binding: gateInput.Binding, Status: "PASS"})
	child.drive()
	child.reopen()
	child.drive()
	waves := child.run.Validation.Waves
	if len(waves) == 0 || waves[len(waves)-1].Status != "COMPLETE" {
		t.Fatalf("selected gates did not complete: %+v", waves)
	}
	for _, input := range child.run.ValidationInputs {
		if input.Handler == "engine.qa.execution.v4" || input.Handler == "engine.qa.design.v6" {
			t.Fatalf("QA-free route dispatched QA work: %+v", input)
		}
	}
}
