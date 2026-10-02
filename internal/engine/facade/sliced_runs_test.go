// sliced_runs_test.go 覆盖拆分 run 身份层的 façade 行为：split 拓扑绑定的
// AC 责任映射校验与合并路线锁定、child run 创建的认领互斥/身份匹配/崩溃
// 窗恢复，以及 child 全程继承（无自身审查轮、QA-free 路线）。
package facade

import (
	"os"
	"path/filepath"
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
	d := startValidationDriver(t, nil)
	passPreDevelopmentReviews(t, d, commandPlan())
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{
		Subject:    d.revision,
		Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: d.revision, Document: validationDocument},
	}})
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

// driverState 读取 run 的权威状态投影。
func driverState(t *testing.T, d *validationDriver) *protocol.State {
	t.Helper()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.State
}

// sliceChildStart 构造 child run 的启动请求（与 master 同根、共享同一 Git
// 夹具 runner、受理 revision 与 master 当前确认对齐）。
func sliceChildStart(d *validationDriver, childRunID, sliceID string, receipt IntakeConfirmationReceipt) StartOptions {
	return StartOptions{Capacity: 16, Root: d.f.root, VCSRunner: d.f.vcsRunner, Request: StartRequest{
		RunID: childRunID, Route: "regular", BaseSnapshot: frozenSnapshot,
		DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: receipt, MasterRunID: d.f.runID, SliceID: sliceID,
	}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}}
}

// sliceChildReceipt 构造 child 受理回执（revision 与 master 当前确认对齐时
// 合法；传其他值用于身份不匹配用例）。
func sliceChildReceipt(revision string) IntakeConfirmationReceipt {
	receipt := testReceipt()
	receipt.RequirementRevision = revision
	receipt.Artifacts = []IntakeArtifact{{Path: "requirements.md", Revision: revision}}
	return receipt
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
// 绑定成功且同事务锁定 RouteMode=merge、SelectedGates=[merge-qa, merge-gate]；
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
					if state := driverState(t, d); state.Topology != nil || state.RouteMode != "" {
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
	if state.RouteMode != "merge" || len(state.SelectedGates) != 2 || state.SelectedGates[0] != "merge-qa" || state.SelectedGates[1] != "merge-gate" {
		t.Fatalf("merge route lock = %q %v", state.RouteMode, state.SelectedGates)
	}
	if state.Phase != runtime.PhaseTopologyAndRoute {
		t.Fatalf("master phase after split binding = %s, want TOPOLOGY_AND_ROUTE", state.Phase)
	}
	for _, ask := range state.PendingAsks {
		if ask.Control == protocol.ControlSelectRoute {
			t.Fatalf("merge-locked master carries a pending SELECT_ROUTE ask: %+v", ask)
		}
	}
	if err := controlError(t, d, protocol.ControlSelectRoute, "custom", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Mode: "custom", Gates: []string{"gate.quality"}, Selected: []string{"gate.quality"},
	}}); err == nil {
		t.Fatal("route selection accepted on the merge-locked master")
	}
}

// TestSplitBindingRequiresNoPendingRouteAsk：未决 SELECT_ROUTE Ask 存在时不
// 绑定 split——绑定后的 run 不携带不可能再决定的路线 Ask。
func TestSplitBindingRequiresNoPendingRouteAsk(t *testing.T) {
	d := splitMasterDriver(t)
	ask, err := protocol.NewRouteRequestEvent(d.eventID(), []string{"blackbox", "whitebox", "gate.quality"}, protocol.AskOption{ID: "full", Label: "全部门"})
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
	if state := driverState(t, d); state.Topology != nil || state.RouteMode != "" {
		t.Fatalf("rejected split binding changed state: %+v", state.Topology)
	}
}

// TestSliceChildStartValidations：child 创建的全部拒绝路径逐个可区分（逐
// 用例独立 child run id——引擎 start 视任何同 id 残留为已存在）。
func TestSliceChildStartValidations(t *testing.T) {
	master := splitMaster(t)
	validReceipt := sliceChildReceipt(master.revision)

	t.Run("master missing", func(t *testing.T) {
		options := sliceChildStart(master, "child-missing", "lane-core", validReceipt)
		options.Request.MasterRunID = "no-such-master"
		if _, _, err := Start(options); err == nil {
			t.Fatal("orphan child start accepted")
		} else if !strings.Contains(err.Error(), "no engine state") {
			t.Fatalf("missing master error = %v", err)
		}
	})
	t.Run("master not split-bound", func(t *testing.T) {
		noSplit := splitMasterDriver(t)
		noSplit.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
		if _, _, err := Start(sliceChildStart(noSplit, "child-no-split", "lane-core", sliceChildReceipt(noSplit.revision))); err == nil {
			t.Fatal("no-split master accepted a slice child")
		} else if !strings.Contains(err.Error(), "no confirmed split topology") {
			t.Fatalf("no-split master error = %v", err)
		}
	})
	t.Run("master not ACTIVE", func(t *testing.T) {
		aborted := splitMaster(t)
		abortRun(t, aborted)
		if _, _, err := Start(sliceChildStart(aborted, "child-aborted", "lane-core", sliceChildReceipt(aborted.revision))); err == nil {
			t.Fatal("aborted master accepted a slice child")
		} else if !strings.Contains(err.Error(), "not ACTIVE") {
			t.Fatalf("aborted master error = %v", err)
		}
	})
	t.Run("slice outside the topology", func(t *testing.T) {
		if _, _, err := Start(sliceChildStart(master, "child-foreign-slice", "lane-unknown", validReceipt)); err == nil {
			t.Fatal("unknown slice accepted")
		} else if !strings.Contains(err.Error(), "not part of the split topology") {
			t.Fatalf("unknown slice error = %v", err)
		}
	})
	t.Run("claim held by another child run", func(t *testing.T) {
		if _, _, err := Start(sliceChildStart(master, "child-first", "lane-core", validReceipt)); err != nil {
			t.Fatalf("first claim failed: %v", err)
		}
		if _, _, err := Start(sliceChildStart(master, "child-second", "lane-core", validReceipt)); err == nil {
			t.Fatal("second child run claimed the same slice")
		} else if !strings.Contains(err.Error(), "already claimed") {
			t.Fatalf("second claim error = %v", err)
		}
	})
	t.Run("intake revision mismatch", func(t *testing.T) {
		if _, _, err := Start(sliceChildStart(master, "child-revision", "lane-core", sliceChildReceipt("rev-mismatched"))); err == nil {
			t.Fatal("revision mismatch accepted")
		} else if !strings.Contains(err.Error(), "does not match the master's current confirmed revision") {
			t.Fatalf("revision mismatch error = %v", err)
		}
	})
	t.Run("base snapshot mismatch", func(t *testing.T) {
		options := sliceChildStart(master, "child-snapshot", "lane-core", validReceipt)
		options.Request.Route = "lightweight"
		options.Request.BaseSnapshot = ""
		options.VCSRunner = nil
		if _, _, err := Start(options); err == nil {
			t.Fatal("base snapshot mismatch accepted")
		} else if !strings.Contains(err.Error(), "does not match the master base snapshot") {
			t.Fatalf("base snapshot mismatch error = %v", err)
		}
	})
	t.Run("both identity fields are required", func(t *testing.T) {
		options := sliceChildStart(master, "child-half-identity", "lane-core", validReceipt)
		options.Request.SliceID = ""
		if _, _, err := Start(options); err == nil {
			t.Fatal("master run id without slice id accepted")
		}
	})
}

// TestSliceChildInheritsAndSelectsRoute：child 创建成功后 master 记录认领映
// 射、child 状态含继承事实，全程无自身审查轮，QA-free 路线确认后推进开发
// 段；继承拓扑不可重切。
func TestSliceChildInheritsAndSelectsRoute(t *testing.T) {
	master := splitMaster(t)
	f, run, err := Start(sliceChildStart(master, "child-lane-core", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	if state := driverState(t, master); state.SliceChildren["lane-core"] != "child-lane-core" {
		t.Fatalf("master claim map = %v", state.SliceChildren)
	}
	child := &validationDriver{t: t, f: f, run: run, provider: DefaultProvider, git: master.git, frozen: master.frozen}
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
	// 候选含 QA 的 Ask 拒绝；QA-free 候选的 custom 路线照常绑定并推进。
	if err := controlError(t, child, protocol.ControlSelectRoute, "full", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Mode: "full", Gates: []string{"blackbox", "whitebox", "gate.quality"},
	}}); err == nil {
		t.Fatal("QA-carrying child route ask accepted")
	}
	child.control(protocol.ControlSelectRoute, "custom", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Mode: "custom", Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.security"},
	}})
	state = driverState(t, child)
	if state.RouteMode != "custom" || len(state.SelectedGates) != 1 || state.SelectedGates[0] != "gate.security" {
		t.Fatalf("child route binding = %q %v", state.RouteMode, state.SelectedGates)
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
// 前进——未选路线时提示选择分片路线，选完路线后指向确认需求文档的提交
// （验证波次依赖 Validation 冻结）；全程不出现开发计划提示（child 的路线
// 确认不绑定开发计划）。
func TestSliceChildBoundaryFollowsRouteSelection(t *testing.T) {
	master := splitMaster(t)
	f, run, err := Start(sliceChildStart(master, "child-boundary", "lane-core", sliceChildReceipt(master.revision)))
	if err != nil {
		t.Fatalf("slice child start: %v", err)
	}
	child := &validationDriver{t: t, f: f, run: run, provider: DefaultProvider, git: master.git, frozen: master.frozen}
	child.drive()
	if boundary := operatorBoundary(t, child); !strings.Contains(boundary, "select the slice route") {
		t.Fatalf("boundary before route selection = %q", boundary)
	} else if strings.Contains(boundary, "development plan") {
		t.Fatalf("child boundary carries a development-plan prompt: %q", boundary)
	}
	child.control(protocol.ControlSelectRoute, "custom", protocol.ReviewDecisionPayload{Route: &protocol.RouteDecision{
		Mode: "custom", Gates: []string{"gate.quality", "gate.security"}, Selected: []string{"gate.quality"},
	}})
	if boundary := operatorBoundary(t, child); !strings.Contains(boundary, "REQUIREMENT document") {
		t.Fatalf("boundary after route selection = %q, want the confirmed requirement document", boundary)
	} else if strings.Contains(boundary, "development plan") {
		t.Fatalf("child boundary carries a development-plan prompt: %q", boundary)
	}
}

// TestSliceChildParallelClaimsAreMutuallyExclusive：两个并发创建同一分片恰成
// 一个。
func TestSliceChildParallelClaimsAreMutuallyExclusive(t *testing.T) {
	master := splitMaster(t)
	receipt := sliceChildReceipt(master.revision)
	racers := []string{"child-racer-a", "child-racer-b"}
	type claimResult struct {
		childRunID string
		err        error
	}
	results := make(chan claimResult, len(racers))
	for _, childID := range racers {
		go func(childID string) {
			_, _, err := Start(sliceChildStart(master, childID, "lane-tooling", receipt))
			results <- claimResult{childRunID: childID, err: err}
		}(childID)
	}
	firstResult, secondResult := <-results, <-results
	succeeded, winner := 0, ""
	for _, result := range []claimResult{firstResult, secondResult} {
		if result.err == nil {
			succeeded++
			winner = result.childRunID
		}
	}
	if succeeded != 1 {
		t.Fatalf("parallel claims: %d succeeded, want exactly one (%v / %v)", succeeded, firstResult, secondResult)
	}
	if state := driverState(t, master); state.SliceChildren["lane-tooling"] != winner {
		t.Fatalf("master claim map = %v, want the winner %q", state.SliceChildren, winner)
	}
}

// TestSliceChildClaimSurvivesInterruptedStart：认领已写、child 状态未写时，
// 同 childRunID 重入成功补齐；异 childRunID 被拒。
func TestSliceChildClaimSurvivesInterruptedStart(t *testing.T) {
	master := splitMaster(t)
	if _, _, err := Start(sliceChildStart(master, "child-window", "lane-tooling", sliceChildReceipt(master.revision))); err != nil {
		t.Fatalf("initial child start: %v", err)
	}
	// 两写窗中断：认领在 master 账上，child 的状态与生命周期标记均未写
	//（生命周期标记写在 child 状态之后，窗内不可能存在）。
	if err := os.RemoveAll(filepath.Join(master.f.root, filepath.FromSlash(EngineNamespace), "child-window")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(master.f.root, ".gates", "tmp", "child-window")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Start(sliceChildStart(master, "child-window", "lane-tooling", sliceChildReceipt(master.revision))); err != nil {
		t.Fatalf("same-id re-entry after the interrupted start window: %v", err)
	}
	state := driverState(t, master)
	if state.SliceChildren["lane-tooling"] != "child-window" {
		t.Fatalf("claim map after re-entry = %v", state.SliceChildren)
	}
	if _, _, err := Start(sliceChildStart(master, "child-other", "lane-tooling", sliceChildReceipt(master.revision))); err == nil {
		t.Fatal("different child run id re-entered the claimed slice")
	} else if !strings.Contains(err.Error(), "already claimed") {
		t.Fatalf("foreign re-entry error = %v", err)
	}
}
