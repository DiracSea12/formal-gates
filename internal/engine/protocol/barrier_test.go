// barrier_test.go 覆盖修订屏障（Phase A）作为提交边界属性的统一闸与挂
// 起收尾：恢复/对账 API 在屏障期可区分拒绝（解除后照常），EXECUTED 回
// 扣与 RESET/ABORT 决定的机械后果（intent 清算、frontier settle、汇合
// 步）挂起到解除事务统一执行，FAILED 回执只落观察而失败路由挂起——
// 且"屏障期回执先到"与"解除后回执到"两种顺序到达同一终态；UNKNOWN
// 回执屏障期挂接的暂存配对同样在解除事务完成。
package protocol

import (
	"sort"
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

// TestBarrierRejectsRecoveryAndReconciliationUntilTheAskResolves：恢复与
// host 对账是权威写入口——屏障期一律可区分拒绝且零状态变化；Ask 解决
// （解除）后同一调用照常受理并落账。
func TestBarrierRejectsRecoveryAndReconciliationUntilTheAskResolves(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if _, err := engine.Submit(unknownHostReceipt(t, "evt-hold-unknown", intent), fp); err != nil {
		t.Fatalf("unknown receipt: %v", err)
	}
	request, err := NewRequirementRequestEvent("req-hold-recovery", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}

	before := stateBytesOf(t, dir)
	if _, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine}, fp); err == nil {
		t.Fatal("attempt recovery accepted while the barrier holds")
	} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
		t.Fatalf("recovery code = %q", code)
	}
	if _, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed-hold", true, false, fp); err == nil {
		t.Fatal("host reconciliation accepted while the barrier holds")
	} else if code := rejectionCode(t, err); code != CodeChangeBarrierActive {
		t.Fatalf("reconciliation code = %q", code)
	}
	if after := stateBytesOf(t, dir); string(before) != string(after) {
		t.Fatal("barrier rejections changed state bytes")
	}

	// 对照：解除后同一调用照常。
	abortExistingAsk(t, engine, "req-hold-recovery")
	recovered, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine}, fp)
	if err != nil || recovered.Action != RecoveryResumeAttempt {
		t.Fatalf("recovery after the lift = %+v err=%v", recovered, err)
	}
	reconciled, _, err := engine.ReconcileHostAction(intent.ActionID, "sha256:observed-hold", true, false, fp)
	if err != nil || reconciled.Action != RecoveryReconcile {
		t.Fatalf("reconciliation after the lift = %+v err=%v", reconciled, err)
	}
	snap := engine.LoadMustSucceed(t)
	if effect, ok := snap.State.ReconciledEffects[intent.ActionID]; !ok || effect.Status != "FULFILLED" {
		t.Fatalf("reconciled effect = %+v ok=%v", effect, ok)
	}
}

// hostAskStepsFixture 构造带 HOST_ACTION 与 HUMAN_ASK frontier 步的串行
// 定义：settle 的机械后果（intent 清算完成 host 步、决定完成 ask 步、
// 之后 LOCAL 汇合）都有可观察落点。
func hostAskStepsFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	reg := definition.Registry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
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
	prep, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.entry.parse"})
	must(err)
	ask, err := authoring.NewHumanAskStep(header("gate.confirm", "prep.parse"), authoring.HumanAskSpec{
		AskKind: "decision", RequestSchema: "schema.ask.decision.request",
		ResponseSchema: "schema.ask.decision.response", FreshnessTTL: time.Minute,
	})
	must(err)
	host, err := authoring.NewHostActionStep(header("fan.transport", "prep.parse"), io("prep.parse"), authoring.HostActionSpec{
		Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI,
		Operation: opTransport, Schema: "schema.host.fan.transport", Timeout: 10 * time.Second,
	})
	must(err)
	join, err := authoring.NewLocalStep(header("after.join", "gate.confirm", "fan.transport"),
		io("gate.confirm", "fan.transport"), authoring.LocalSpec{Handler: "engine.fan.join"})
	must(err)
	cd, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "review",
		Steps: []authoring.Step{prep, ask, host, join}}, reg)
	must(err)
	return cd, reg
}

// TestBarrierHoldsSettlementsUntilTheAskResolves：EXECUTED 回执与
// RESET/ABORT 决定在屏障期只落观察与决定事实——intent 清算、frontier
// settle 与汇合步全部挂起，Ask 解除的同一事务统一收尾完成。
func TestBarrierHoldsSettlementsUntilTheAskResolves(t *testing.T) {
	cd, reg := hostAskStepsFixture(t)
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)

	// ABORT Ask 先建（绑定 gate.confirm），host intent 随后，最后立屏障。
	abortAsk, err := NewRequestEvent("req-hold-abort", ControlAbort, AskOption{ID: "abort", Label: "终止"})
	if err != nil {
		t.Fatalf("abort ask event: %v", err)
	}
	if _, err := engine.Submit(abortAsk, fp); err != nil {
		t.Fatalf("submit abort ask: %v", err)
	}
	intent, _, err := engine.ExecuteHostAction(opTransport, map[string]any{"target": "fan.slice"}, fp)
	if err != nil {
		t.Fatalf("intent: %v", err)
	}
	if intent.Step != "fan.transport" {
		t.Fatalf("intent step = %q, want the frontier host step", intent.Step)
	}
	change, err := NewRequirementRequestEvent("req-hold-settle", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("change ask event: %v", err)
	}
	if _, err := engine.Submit(change, fp); err != nil {
		t.Fatalf("submit change ask: %v", err)
	}

	// EXECUTED 回执：观察照常落账，清算挂起。
	if _, err := engine.Submit(hostReceipt(t, "evt-hold-executed", intent), fp); err != nil {
		t.Fatalf("executed receipt: %v", err)
	}
	snap := engine.LoadMustSucceed(t)
	if recorded, ok := snap.State.HostActionReceipts[intent.ActionID]; !ok || recorded.Status != HostActionStatusExecuted {
		t.Fatalf("executed receipt observation = %+v ok=%v", recorded, ok)
	}
	if _, pending := snap.State.PendingHostActions[intent.ActionID]; !pending {
		t.Fatal("the barrier must keep the intent unsettled")
	}
	if len(snap.State.HeldHostSettles) != 1 || snap.State.HeldHostSettles[0] != intent.ActionID {
		t.Fatalf("held settles = %v", snap.State.HeldHostSettles)
	}
	if snap.State.stepCompleted("fan.transport") {
		t.Fatal("frontier settle ran during the barrier")
	}
	heldTrail := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == intent.ActionID && strings.Contains(record.Detail, "held while the requirement change barrier holds") {
			heldTrail = true
		}
	}
	if !heldTrail {
		t.Fatalf("no held-settlement trail: %+v", snap.State.RecoveryRecords)
	}

	// ABORT 决定（处置表放行）：决定落账，settle 挂起。
	token, err := engine.Freshness("req-hold-abort")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	abort, err := NewDecideEvent("evt-hold-abort", "req-hold-abort", token, "abort")
	if err != nil {
		t.Fatalf("abort decide event: %v", err)
	}
	if _, err := engine.Submit(abort, fp); err != nil {
		t.Fatalf("abort decide during the barrier: %v", err)
	}
	snap = engine.LoadMustSucceed(t)
	if _, decided := snap.State.Decisions["req-hold-abort"]; !decided {
		t.Fatal("the abort decision was not recorded")
	}
	if !snap.State.FrontierSettleHeld {
		t.Fatal("the decide settle must be held while the barrier holds")
	}
	if snap.State.stepCompleted("gate.confirm") {
		t.Fatal("the ask step settled during the barrier")
	}

	// 解除：同一事务统一收尾——intent 清算、双 settle、汇合步完成。
	abortExistingAsk(t, engine, "req-hold-settle")
	snap = engine.LoadMustSucceed(t)
	if _, pending := snap.State.PendingHostActions[intent.ActionID]; pending {
		t.Fatal("the lifting transaction did not settle the intent")
	}
	if len(snap.State.HeldHostSettles) != 0 || snap.State.FrontierSettleHeld {
		t.Fatalf("held settle ledgers must clear: %v %v", snap.State.HeldHostSettles, snap.State.FrontierSettleHeld)
	}
	for _, step := range []authoring.StepID{"gate.confirm", "fan.transport", "after.join"} {
		if !snap.State.stepCompleted(step) {
			t.Fatalf("step %s not settled at the lift", step)
		}
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}

// failedSpawnReceipt 构造携瞬态失败类的 FAILED 派发回执。
func failedSpawnReceipt(t *testing.T, id EventID, actionID, correlation string) Event {
	t.Helper()
	ev, err := NewSpawnReceiptWithFailureClass(id, actionID, testProvider, correlation, SpawnStatusFailed, authoring.FailureTransientEngine)
	if err != nil {
		t.Fatalf("failed receipt event: %v", err)
	}
	return ev
}

// failedRoutingFacts 是 FAILED 回执处置后的业务终态投影：观察账、暂存
// 删除、结果账、计轮、重试记账、路由留痕与挂起账清零。
type failedRoutingFacts struct {
	failures   int
	staged     int
	results    int
	completed  int
	attempts   int
	retryDone  bool
	routed     bool
	heldRoutes int
}

// driveFailedReceipt 走一条顺序：先暂存结果，再按 receiptDuringBarrier
// 决定 FAILED 回执落在屏障内（解除事务统一处置）或屏障解除之后（立即
// 路由），返回终态投影。
func driveFailedReceipt(t *testing.T, receiptDuringBarrier bool) failedRoutingFacts {
	t.Helper()
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]
	task := engine.LoadMustSucceed(t).State.PendingActions[product].Task

	staged, err := NewReviewWorkerResultEvent("result-failed-staged", product, testProvider, "sha256:payload-failed-staged",
		reviewEvidence(review.ProductReview, product, "", "PASS", "staged before the failed receipt", nil))
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("stage: %v %+v", err, acceptance)
	}

	if receiptDuringBarrier {
		request, err := NewRequirementRequestEvent("req-failed-hold", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request event: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		if acceptance, err := engine.Submit(failedSpawnReceipt(t, "spawn-failed-hold", product, "agent-failed"), fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("failed receipt during the barrier: %v %+v", err, acceptance)
		}
		snap := engine.LoadMustSucceed(t)
		if len(snap.State.SpawnFailures[product]) != 1 {
			t.Fatalf("the failure observation must land during the barrier: %+v", snap.State.SpawnFailures)
		}
		if _, kept := snap.State.StagedResults[product]; !kept {
			t.Fatal("the held routing must keep the staging until the lift")
		}
		if len(snap.State.HeldFailureRoutes) != 1 || snap.State.HeldFailureRoutes[0] != product {
			t.Fatalf("held routes = %v", snap.State.HeldFailureRoutes)
		}
		abortExistingAsk(t, engine, "req-failed-hold")
	} else {
		request, err := NewRequirementRequestEvent("req-failed-hold", requirementPackageOf(nil), requirementOptions()...)
		if err != nil {
			t.Fatalf("request event: %v", err)
		}
		if _, err := engine.Submit(request, fp); err != nil {
			t.Fatalf("submit request: %v", err)
		}
		abortExistingAsk(t, engine, "req-failed-hold")
		if acceptance, err := engine.Submit(failedSpawnReceipt(t, "spawn-failed-hold", product, "agent-failed"), fp); err != nil || acceptance.Status != "ACCEPTED" {
			t.Fatalf("failed receipt after the lift: %v %+v", err, acceptance)
		}
	}

	snap := engine.LoadMustSucceed(t)
	attempt := snap.State.Attempts[task]
	routed := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == product && record.Class == authoring.FailureTransientEngine && record.Action == RecoveryResumeAttempt {
			routed = true
		}
	}
	return failedRoutingFacts{
		failures:   len(snap.State.SpawnFailures[product]),
		staged:     len(snap.State.StagedResults),
		results:    len(snap.State.Results),
		completed:  snap.State.ReviewSeries.ProductReview.Completed,
		attempts:   attempt.Attempts,
		retryDone:  attempt.RetryExhausted,
		routed:     routed,
		heldRoutes: len(snap.State.HeldFailureRoutes),
	}
}

// TestFailedReceiptRoutingConvergesAcrossOrders：FAILED 回执在屏障期只落
// 观察（暂存保留、路由挂起留痕），解除事务按既定规则处置（删暂存＋路
// 由留痕）；与解除后到达立即路由的顺序终态等价。
func TestFailedReceiptRoutingConvergesAcrossOrders(t *testing.T) {
	during := driveFailedReceipt(t, true)
	after := driveFailedReceipt(t, false)
	if during != after {
		t.Fatalf("terminal facts diverge across orders: %+v vs %+v", during, after)
	}
	if during.failures != 1 || during.staged != 0 || !during.routed || during.heldRoutes != 0 || during.completed != 0 || during.results != 0 {
		t.Fatalf("failed-receipt facts = %+v", during)
	}
}

// completionFacts 是审查轮完成后的终态投影：系列完成数与结论、结果账、
// 完成步集合、pending 派发面、任务态与在途 Attempt 数。
type completionFacts struct {
	completed  int
	status     string
	boundTo    string
	results    int
	staging    int
	steps      string
	dispatches string
	taskStates string
	attempts   int
}

// driveCompletionOrder 走一条完成顺序：回执先落账后，结果先到（直接完
// 成路径）或任务先报 TERMINAL（terminal 对账路径）。
func driveCompletionOrder(t *testing.T, terminalFirst bool) completionFacts {
	t.Helper()
	cd, reg := reviewStepsFixture(t, "product.review", "start.readiness")
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]
	pending := engine.LoadMustSucceed(t).State.PendingActions[product]

	if _, err := engine.Submit(spawnReceipt(t, "spawn-completion", product, "agent-completion"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if terminalFirst {
		for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
			ev, err := NewTaskEvent(EventID("task-completion-"+string(rune('a'+i))), pending.Task, pending.AttemptID, status)
			if err != nil {
				t.Fatalf("task event: %v", err)
			}
			if _, err := engine.Submit(ev, fp); err != nil {
				t.Fatalf("progress %s: %v", status, err)
			}
		}
	}
	result, err := NewReviewWorkerResultEvent("result-completion", product, testProvider, "sha256:payload-completion",
		reviewEvidence(review.ProductReview, product, "", "PASS", "the round completes", nil))
	if err != nil {
		t.Fatalf("result event: %v", err)
	}
	if acceptance, err := engine.Submit(result, fp); err != nil || acceptance.Status != "ACCEPTED" {
		t.Fatalf("result: %v %+v", err, acceptance)
	}

	snap := engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	stepIDs := make([]string, 0, len(snap.State.Completed))
	for _, id := range snap.State.Completed {
		stepIDs = append(stepIDs, string(id))
	}
	dispatches := make([]string, 0, len(snap.State.PendingActions))
	for _, action := range snap.State.PendingActions {
		dispatches = append(dispatches, action.Step)
	}
	taskStates := make([]string, 0, len(snap.State.Tasks))
	for key, status := range snap.State.Tasks {
		taskStates = append(taskStates, key.String()+":"+string(status))
	}
	sort.Strings(stepIDs)
	sort.Strings(dispatches)
	sort.Strings(taskStates)
	return completionFacts{
		completed:  series.Completed,
		status:     series.Effective.Status,
		boundTo:    series.Effective.RequirementRevision,
		results:    len(snap.State.Results),
		staging:    len(snap.State.StagedResults),
		steps:      strings.Join(stepIDs, ","),
		dispatches: strings.Join(dispatches, ","),
		taskStates: strings.Join(taskStates, ","),
		attempts:   len(snap.State.Attempts),
	}
}

// TestTerminalAndResultFirstOrdersReachTheSameState：同一审查轮经"结果
// 先到"（完成路径：步骤完成＋补位签发）或"任务先报 TERMINAL"（terminal
// 对账路径）完成，终态完全相同——含 pending 派发面与任务态。
func TestTerminalAndResultFirstOrdersReachTheSameState(t *testing.T) {
	resultFirst := driveCompletionOrder(t, false)
	terminalFirst := driveCompletionOrder(t, true)
	if resultFirst != terminalFirst {
		t.Fatalf("terminal states diverge across orders:\nresult-first:  %+v\nterminal-first: %+v", resultFirst, terminalFirst)
	}
	if resultFirst.completed != 1 || resultFirst.status != "PASS" || resultFirst.boundTo != "rev-1" || resultFirst.results != 1 || resultFirst.staging != 0 {
		t.Fatalf("completion facts = %+v", resultFirst)
	}
	if !strings.Contains(resultFirst.steps, "product.review") {
		t.Fatalf("the review step must complete: %q", resultFirst.steps)
	}
	if resultFirst.dispatches != "start.readiness" {
		t.Fatalf("the pending dispatch face = %q, want the follow-up readiness dispatch", resultFirst.dispatches)
	}
	if resultFirst.attempts != 1 {
		t.Fatalf("the follow-up dispatch must hold one current attempt: %d", resultFirst.attempts)
	}
}

// TestUnknownReceiptAttachPairsWhenTheBarrierLifts：UNKNOWN 回执与对账挂
// 接是观察事实（屏障期照常落账），暂存配对接纳挂起；Ask 解除的同一事
// 务完成配对（Results 入账、计轮、暂存清空）。
func TestUnknownReceiptAttachPairsWhenTheBarrierLifts(t *testing.T) {
	cd, reg := reviewStepsFixture(t, "product.review")
	engine, _ := reviewEngine(t, cd, reg)
	fp := fingerprintOf(t, engine)
	product := issueReady(t, engine)[0]

	staged, err := NewReviewWorkerResultEvent("result-attach-staged", product, testProvider, "sha256:payload-attach-staged",
		reviewEvidence(review.ProductReview, product, "", "PASS", "staged before the unknown receipt", nil))
	if err != nil {
		t.Fatalf("staged event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("stage: %v %+v", err, acceptance)
	}

	request, err := NewRequirementRequestEvent("req-attach-hold", requirementPackageOf(nil), requirementOptions()...)
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	if _, err := engine.Submit(request, fp); err != nil {
		t.Fatalf("submit request: %v", err)
	}
	if _, err := engine.Submit(unknownSpawnReceipt(t, "spawn-attach-unknown", product, "agent-attach"), fp); err != nil {
		t.Fatalf("unknown receipt during the barrier: %v", err)
	}
	for _, eventName := range []string{LifecycleStart, LifecycleStop} {
		ev, err := NewCorrelatedLifecycleEvent(EventID("evt-attach-"+eventName), testProvider, "agent-attach", "agent-attach", eventName)
		if err != nil {
			t.Fatalf("lifecycle event: %v", err)
		}
		if _, err := engine.Submit(ev, fp); err != nil {
			t.Fatalf("lifecycle %s: %v", eventName, err)
		}
	}

	plan, _, err := engine.ReconcileUnknownReceipt(product, fp)
	if err != nil || plan.Action != RecoveryAttachReceipt {
		t.Fatalf("attach during the barrier = %+v err=%v", plan, err)
	}
	snap := engine.LoadMustSucceed(t)
	if receipt := snap.State.SpawnReceipts[product]; receipt.Status != SpawnStatusAttached {
		t.Fatalf("attached receipt = %+v", receipt)
	}
	if _, kept := snap.State.StagedResults[product]; !kept {
		t.Fatal("the pairing must stay suspended while the barrier holds")
	}
	if series := snap.State.ReviewSeries.ProductReview; series.Completed != 0 {
		t.Fatalf("the suspended pairing must not count: completed=%d", series.Completed)
	}
	suspended := false
	for _, record := range snap.State.RecoveryRecords {
		if record.ActionID == product && strings.Contains(record.Detail, "stays suspended until the barrier lifts") {
			suspended = true
		}
	}
	if !suspended {
		t.Fatalf("no suspension trail: %+v", snap.State.RecoveryRecords)
	}

	abortExistingAsk(t, engine, "req-attach-hold")
	snap = engine.LoadMustSucceed(t)
	series := snap.State.ReviewSeries.ProductReview
	if series.Completed != 1 || series.Effective.Status != "PASS" {
		t.Fatalf("the paired round did not complete at the lift: completed=%d %+v", series.Completed, series.Effective)
	}
	if _, admitted := snap.State.Results[product]; !admitted {
		t.Fatal("the paired result must reach Results in the lifting transaction")
	}
	if _, kept := snap.State.StagedResults[product]; kept {
		t.Fatalf("the paired round must leave staging: %+v", snap.State.StagedResults)
	}
	if _, err := engine.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
}
