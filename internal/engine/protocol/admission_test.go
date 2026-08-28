package protocol

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
)

// spawnReceipt 构造标准 SPAWNED 回执事件。
func spawnReceipt(t *testing.T, id EventID, actionID, correlation string) Event {
	t.Helper()
	ev, err := NewSpawnReceiptEvent(id, actionID, testProvider, correlation, SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("spawn receipt event: %v", err)
	}
	return ev
}

func unknownSpawnReceipt(t *testing.T, id EventID, actionID, correlation string) Event {
	t.Helper()
	ev, err := NewSpawnReceiptEvent(id, actionID, testProvider, correlation, SpawnStatusUnknown)
	if err != nil {
		t.Fatalf("unknown spawn receipt event: %v", err)
	}
	return ev
}

func TestBoundedTransientRetryExhaustionUsesCompiledStepPolicy(t *testing.T) {
	root := t.TempDir()
	store, err := persistence.NewStore(root, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatal(err)
	}
	registry := definition.Registry()
	step, err := authoring.NewAgentStep(
		authoring.Header{ID: "retry.worker", NodeID: "retry", DefinitionVersion: definition.Version},
		authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Postconditions: []authoring.PredicateRef{{ID: "pred.review.post"}}},
		authoring.AgentSpec{Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute, Retry: &authoring.RetryPolicy{MaxAttempts: 3}},
	)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(&compiler.Definition{Version: definition.Version, EntryNode: "retry", Steps: []authoring.Step{step}}, registry)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(store, Config{Definition: compiled, Registry: registry, Capacity: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatal(err)
	}
	fp := fingerprintOf(t, engine)
	if err := engine.Init(view, testProvider, fp); err != nil {
		t.Fatal(err)
	}
	plan, err := decision.Decide(view, decision.Observation{}, compiled)
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fp)
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue = %+v err=%v", issued, err)
	}
	action := issued[0].ActionID
	if _, err := engine.Submit(spawnReceipt(t, "retry-spawn", action, "retry-agent"), fp); err != nil {
		t.Fatal(err)
	}
	for i, want := range []RecoveryAction{RecoveryResumeAttempt, RecoveryResumeAttempt, RecoveryWait} {
		result, err := NewWorkerResultEvent(EventID("retry-result-"+string(rune('1'+i))), action, testProvider, OutcomeRuntimeError, "sha256:retry-"+string(rune('1'+i)), authoring.FailureTransientEngine)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := engine.Submit(result, fp)
		if err != nil || accepted.RecoveryAction != string(want) {
			t.Fatalf("retry %d acceptance=%+v err=%v want=%s", i+1, accepted, err, want)
		}
	}
	snapshot := engine.LoadMustSucceed(t)
	attempt := snapshot.State.Attempts[runtime.TaskKey{Node: "retry", Step: "retry.worker"}]
	if attempt.Attempts != 3 || attempt.MaxAttempts != 3 || !attempt.RetryExhausted {
		t.Fatalf("retry ledger = %+v", attempt)
	}
	recovery, _, err := engine.RecoverAttempt(action, Interruption{Class: authoring.FailureTransientEngine}, fp)
	if err != nil || recovery.Action != RecoveryWait {
		t.Fatalf("exhausted recovery = %+v err=%v", recovery, err)
	}
}

// workerResult 构造标准 PASS 结果事件。
func workerResult(t *testing.T, id EventID, actionID, digest string) Event {
	t.Helper()
	ev, err := NewWorkerResultEvent(id, actionID, testProvider, OutcomePass, digest, "")
	if err != nil {
		t.Fatalf("worker result event: %v", err)
	}
	return ev
}

// TestSpawnReceiptAdmitsAndRecords：回执接纳后落账（声明签发态），
// 任务台账不动（进度观察是独立事件），持久可重读。
func TestSpawnReceiptAdmitsAndRecords(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	acceptance, err := engine.Submit(spawnReceipt(t, "evt-receipt-1", reviewAction, "agent-review-1"), fp)
	if err != nil {
		t.Fatalf("submit receipt: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || acceptance.ActionID != reviewAction {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	receipt, ok := snap.State.SpawnReceipts[reviewAction]
	if !ok || receipt.Provider != testProvider || receipt.Correlation != "agent-review-1" || receipt.Status != SpawnStatusSpawned {
		t.Fatalf("receipt = %+v ok=%v", receipt, ok)
	}
	// 任务台账不动：仍在 expected、仍是 ISSUED（进度观察是独立边界）。
	if !snap.State.expectedContains(reviewTaskKey()) || snap.State.TaskStatusOf(reviewTaskKey()) != runtime.TaskIssued {
		t.Fatalf("receipt admission must not move task bookkeeping")
	}
	if len(stateBytesOf(t, dir)) == 0 {
		t.Fatal("state not durable")
	}
}

// TestSpawnReceiptByteIdenticalReplayIdempotent：同回执逐字节重发不重复
// receipt 效果；换新事件 ID 时只占用新的事件台账键。
func TestSpawnReceiptByteIdenticalReplayIdempotent(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-receipt-1", reviewAction, "agent-review-1"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	// 同事件 ID 重放：1b 台账路径。
	same, err := engine.Submit(spawnReceipt(t, "evt-receipt-1", reviewAction, "agent-review-1"), fp)
	if err != nil {
		t.Fatalf("same-id replay: %v", err)
	}
	if same.Status != "ACCEPTED" {
		t.Fatalf("same-id replay status = %q", same.Status)
	}
	// 换事件 ID、同回执字节：payload 级 duplicate 路径。
	dup, err := engine.Submit(spawnReceipt(t, "evt-receipt-1bis", reviewAction, "agent-review-1"), fp)
	if err != nil {
		t.Fatalf("cross-id replay: %v", err)
	}
	if dup.Status != "DUPLICATE" || dup.Revision != before.Revision+1 {
		t.Fatalf("cross-id replay acceptance = %+v", dup)
	}
	after := engine.LoadMustSucceed(t)
	if after.Revision != before.Revision+1 {
		t.Fatalf("cross-id replay revision: %d -> %d", before.Revision, after.Revision)
	}
	if len(after.State.SpawnReceipts) != 1 || len(after.State.Events) != 2 {
		t.Fatalf("record growth: receipts=%d events=%d", len(after.State.SpawnReceipts), len(after.State.Events))
	}

	// 同 actionID 不同回执字节：硬拒绝且零状态变化。
	conflicting, err := NewSpawnReceiptEvent("evt-receipt-2", reviewAction, testProvider, "agent-other", SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("conflicting event: %v", err)
	}
	if _, err := engine.Submit(conflicting, fp); err == nil {
		t.Fatal("conflicting receipt accepted")
	} else if code := rejectionCode(t, err); code != CodeReceiptConflict {
		t.Fatalf("conflicting code = %q", code)
	}
	final := engine.LoadMustSucceed(t)
	if final.Revision != after.Revision || !reflect.DeepEqual(after.State, final.State) {
		t.Fatal("conflict changed state")
	}
}

// TestSpawnReceiptNegatives：未知 action、provider mismatch（不同与空）。
func TestSpawnReceiptNegatives(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	unknown, err := NewSpawnReceiptEvent("evt-u", "act:ghost/task", testProvider, "agent-x", SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(unknown, fp); err == nil {
		t.Fatal("unknown action accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("unknown action code = %q", code)
	}
	mismatch, err := NewSpawnReceiptEvent("evt-m", reviewAction, "another-host", "agent-x", SpawnStatusSpawned)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatch, fp); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("provider mismatch code = %q", code)
	}
	// 空 provider 在 schema 层即拒（不降级 default）。
	if _, err := NewSpawnReceiptEvent("evt-e", reviewAction, "", "agent-x", SpawnStatusSpawned); err == nil {
		t.Fatal("empty provider accepted at schema")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("empty provider code = %q", code)
	}
}

// TestUnknownSpawnReceiptRecoveryIsDurable：UNKNOWN 回执的恢复记录没有任务
// 键也必须可编码；对账完成后重复调用只返回已提交的 ATTACHED 事实。
func TestUnknownSpawnReceiptRecoveryIsDurable(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	acceptance, err := engine.Submit(unknownSpawnReceipt(t, "evt-unknown-spawn", reviewAction, "agent-review-1"), fp)
	if err != nil {
		t.Fatalf("unknown receipt: %v", err)
	}
	if acceptance.RecoveryAction != string(RecoveryOperator) {
		t.Fatalf("unknown receipt route = %q", acceptance.RecoveryAction)
	}
	if snap := engine.LoadMustSucceed(t); len(snap.State.RecoveryRecords) != 1 {
		t.Fatalf("recovery record not durable: %+v", snap.State.RecoveryRecords)
	}
	for _, eventName := range []string{LifecycleStart, LifecycleStop} {
		ev, eventErr := NewCorrelatedLifecycleEvent(EventID("evt-unknown-"+eventName), testProvider, "agent-review-1", "agent-review-1", eventName)
		if eventErr != nil {
			t.Fatalf("lifecycle event: %v", eventErr)
		}
		if _, eventErr = engine.Submit(ev, fp); eventErr != nil {
			t.Fatalf("submit lifecycle event: %v", eventErr)
		}
	}
	first, revision, err := engine.ReconcileUnknownReceipt(reviewAction, fp)
	if err != nil {
		t.Fatalf("reconcile unknown receipt: %v", err)
	}
	if first.Action != RecoveryAttachReceipt || revision != 6 {
		t.Fatalf("reconcile plan=%+v revision=%d", first, revision)
	}
	second, replayRevision, err := engine.ReconcileUnknownReceipt(reviewAction, fp)
	if err != nil {
		t.Fatalf("reconcile replay: %v", err)
	}
	if second.Action != first.Action || replayRevision != revision {
		t.Fatalf("reconcile replay plan=%+v revision=%d, first=%+v revision=%d", second, replayRevision, first, revision)
	}
}

// twoAgentFixture 是补位签发测试用的自定义定义：entry 前置步 + 两个
// 依赖它的 agent 步（容量 1 时一次只签发一个，PASS 后按容量补位）。
func twoAgentFixture(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	reg := compiler.NewRegistry()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	must(reg.RegisterHandler("engine.test.parse", authoring.RunnerEngineLocal))
	must(reg.RegisterHandler("engine.test.alpha", authoring.RunnerAgentWorker))
	must(reg.RegisterHandler("engine.test.beta", authoring.RunnerAgentWorker))
	must(reg.RegisterCodec("codec.any.in"))
	must(reg.RegisterCodec("codec.any.out"))
	must(reg.RegisterPredicate("pred.test.post"))
	header := func(id string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: "n", Dependencies: ds, DefinitionVersion: definition.Version}
	}
	io := func(bindings ...string) authoring.IO {
		inputs := make([]authoring.InputBinding, 0, len(bindings))
		for _, b := range bindings {
			inputs = append(inputs, authoring.InputBinding{From: authoring.StepID(b), OutputField: "out", ToField: "in"})
		}
		return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
	}
	parse, err := authoring.NewLocalStep(header("prep.parse"), io(), authoring.LocalSpec{Handler: "engine.test.parse"})
	must(err)
	alphaIO := io("prep.parse")
	alphaIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
	alpha, err := authoring.NewAgentStep(header("alpha.worker", "prep.parse"), alphaIO, authoring.AgentSpec{
		Handler: "engine.test.alpha", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute,
	})
	must(err)
	betaIO := io("prep.parse")
	betaIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.post"}}
	beta, err := authoring.NewAgentStep(header("beta.worker", "prep.parse"), betaIO, authoring.AgentSpec{
		Handler: "engine.test.beta", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute,
	})
	must(err)
	cd, err := compiler.Compile(&compiler.Definition{
		Version: definition.Version, EntryNode: "n",
		Steps: []authoring.Step{parse, alpha, beta},
	}, reg)
	must(err)
	return cd, reg
}

// refillEngine 构造绑定双 agent 定义、容量 1 的引擎，并完成前置步、
// 签发 alpha（容量 1 只签第一个）。
func refillEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	dir := t.TempDir()
	cd, reg := twoAgentFixture(t)
	store := mustStore(t, dir)
	engine, err := New(store, Config{Definition: cd, Registry: reg, Capacity: 1}, nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if err := view.CompleteStep("prep.parse", cd); err != nil {
		t.Fatalf("complete prep: %v", err)
	}
	plan, err := decision.Decide(view, decision.Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	fp := fingerprintOf(t, engine)
	if err := engine.Init(view, testProvider, fp); err != nil {
		t.Fatalf("init: %v", err)
	}
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, fp)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(issued) != 1 || issued[0].ActionID != "act:n/alpha.worker" {
		t.Fatalf("issued = %+v", issued)
	}
	return engine, dir, "act:n/alpha.worker"
}

// TestWorkerResultCompletesAndRefills：typed result 接纳后 expected 项
// 完成（任务 TERMINAL、step 完成、frontier 推进）并按容量补位签发
// beta（复用 SelectIssued 语义）；回执在先是正常路径。
func TestWorkerResultCompletesAndRefills(t *testing.T) {
	engine, dir, alphaAction := refillEngine(t)
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "evt-r-alpha", alphaAction, "agent-alpha"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	acceptance, err := engine.Submit(workerResult(t, "evt-res-alpha", alphaAction, "sha256:alpha-out"), fp)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || acceptance.ActionID != alphaAction {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	if len(acceptance.Refill) != 1 || acceptance.Refill[0] != "act:n/beta.worker" {
		t.Fatalf("refill = %+v", acceptance.Refill)
	}
	snap := engine.LoadMustSucceed(t)
	alphaKey := runtime.TaskKey{Node: "n", Step: "alpha.worker"}
	betaKey := runtime.TaskKey{Node: "n", Step: "beta.worker"}
	if snap.State.TaskStatusOf(alphaKey) != runtime.TaskTerminal {
		t.Fatalf("alpha = %s, want TERMINAL", snap.State.TaskStatusOf(alphaKey))
	}
	if !containsStep(snap.State.Completed, "alpha.worker") {
		t.Fatalf("alpha step not completed: %+v", snap.State.Completed)
	}
	if !snap.State.expectedContains(betaKey) || snap.State.TaskStatusOf(betaKey) != runtime.TaskIssued {
		t.Fatalf("beta not refilled: expected=%+v status=%s", snap.State.Expected, snap.State.TaskStatusOf(betaKey))
	}
	if _, ok := snap.State.PendingActions["act:n/beta.worker"]; !ok {
		t.Fatal("beta pending action missing")
	}
	// 重启后补位签发仍在档（同一事务原子落盘）。
	reopened := reopenEngineWith(t, dir)
	snap2 := reopened.LoadMustSucceed(t)
	if !reflect.DeepEqual(snap.State, snap2.State) {
		t.Fatal("refill not durable")
	}
}

func containsStep(steps []authoring.StepID, id string) bool {
	for _, step := range steps {
		if string(step) == id {
			return true
		}
	}
	return false
}

// TestWorkerResultStagesBeforeReceipt：result 先于 SpawnReceipt 到达时
// 暂存（不丢弃、不推进、STAGED 状态），回执到达后配对接纳并补位签发。
func TestWorkerResultStagesBeforeReceipt(t *testing.T) {
	engine, _, alphaAction := refillEngine(t)
	fp := fingerprintOf(t, engine)

	staged, err := engine.Submit(workerResult(t, "evt-res-early", alphaAction, "sha256:alpha-out"), fp)
	if err != nil {
		t.Fatalf("early result: %v", err)
	}
	if staged.Status != "STAGED" || staged.ActionID != alphaAction {
		t.Fatalf("staged acceptance = %+v", staged)
	}
	snap := engine.LoadMustSucceed(t)
	if _, ok := snap.State.StagedResults[alphaAction]; !ok {
		t.Fatal("result not staged")
	}
	// 不推进：任务仍 ISSUED、step 未完成、无补位。
	alphaKey := runtime.TaskKey{Node: "n", Step: "alpha.worker"}
	if snap.State.TaskStatusOf(alphaKey) != runtime.TaskIssued || containsStep(snap.State.Completed, "alpha.worker") {
		t.Fatalf("staged result advanced workflow: %s completed=%+v", snap.State.TaskStatusOf(alphaKey), snap.State.Completed)
	}

	// 回执到达：配对接纳（同事务完成 + 补位）。
	acceptance, err := engine.Submit(spawnReceipt(t, "evt-r-late", alphaAction, "agent-alpha"), fp)
	if err != nil {
		t.Fatalf("late receipt: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || len(acceptance.Refill) != 1 || acceptance.Refill[0] != "act:n/beta.worker" {
		t.Fatalf("paired acceptance = %+v", acceptance)
	}
	snap2 := engine.LoadMustSucceed(t)
	if _, still := snap2.State.StagedResults[alphaAction]; still {
		t.Fatal("staged result not consumed by pairing")
	}
	if snap2.State.TaskStatusOf(alphaKey) != runtime.TaskTerminal || !containsStep(snap2.State.Completed, "alpha.worker") {
		t.Fatalf("paired result did not complete task/step")
	}
	if _, ok := snap2.State.SpawnReceipts[alphaAction]; !ok {
		t.Fatal("pairing receipt not recorded")
	}
}

// TestWorkerResultNegatives：未知 action、provider mismatch、同 action
// 异字节结果冲突、FAIL 结果只终结当前任务而不推进完成 frontier。
func TestWorkerResultNegatives(t *testing.T) {
	engine, _, alphaAction := refillEngine(t)
	fp := fingerprintOf(t, engine)
	if _, err := engine.Submit(spawnReceipt(t, "evt-r-alpha", alphaAction, "agent-alpha"), fp); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	before := engine.LoadMustSucceed(t)

	unknown, err := NewWorkerResultEvent("evt-res-u", "act:ghost", testProvider, OutcomePass, "sha256:x", "")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(unknown, fp); err == nil {
		t.Fatal("unknown action accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("unknown code = %q", code)
	}
	mismatch, err := NewWorkerResultEvent("evt-res-m", alphaAction, "another-host", OutcomePass, "sha256:x", "")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(mismatch, fp); err == nil {
		t.Fatal("provider mismatch accepted")
	} else if code := rejectionCode(t, err); code != CodeProviderMismatch {
		t.Fatalf("mismatch code = %q", code)
	}
	if after := engine.LoadMustSucceed(t); after.Revision != before.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("rejected results changed state")
	}

	// FAIL：任务终结、step 不完成；未签发的兄弟 Ready 仍保留在完整
	// Expected 前沿中，失败分类路由属后续批次。
	fail, err := NewWorkerResultEvent("evt-res-fail", alphaAction, testProvider, OutcomeFail, "sha256:fail", authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	acceptance, err := engine.Submit(fail, fp)
	if err != nil {
		t.Fatalf("fail result: %v", err)
	}
	if acceptance.Status != "ACCEPTED" || len(acceptance.Refill) != 0 {
		t.Fatalf("fail acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	alphaKey := runtime.TaskKey{Node: "n", Step: "alpha.worker"}
	if snap.State.TaskStatusOf(alphaKey) != runtime.TaskTerminal {
		t.Fatalf("failed task = %s, want TERMINAL", snap.State.TaskStatusOf(alphaKey))
	}
	if containsStep(snap.State.Completed, "alpha.worker") || len(snap.State.Expected) != 1 || snap.State.Expected[0].Step != "beta.worker" {
		t.Fatalf("FAIL result advanced frontier: completed=%+v expected=%+v", snap.State.Completed, snap.State.Expected)
	}

	// 同字节结果换事件 ID 重发：DUPLICATE 幂等（任务已终结、结果已落账）。
	sameBytes, err := NewWorkerResultEvent("evt-res-same", alphaAction, testProvider, OutcomeFail, "sha256:fail", authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	dup, err := engine.Submit(sameBytes, fp)
	if err != nil || dup.Status != "DUPLICATE" {
		t.Fatalf("result duplicate = %+v err = %v", dup, err)
	}

	// 同 actionID 异字节结果：硬拒绝。
	different, err := NewWorkerResultEvent("evt-res-diff", alphaAction, testProvider, OutcomePass, "sha256:other", "")
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if _, err := engine.Submit(different, fp); err == nil {
		t.Fatal("conflicting result accepted")
	} else if code := rejectionCode(t, err); code != CodeReceiptConflict {
		t.Fatalf("conflict code = %q", code)
	}
}

// TestDefinitionFailureResultsAreTerminalAndDiagnosable：定义/引擎缺陷结果
// 必须显式失败并回收当前 Attempt，不得进入 Operator 或 agent 恢复路径。
func TestDefinitionFailureResultsAreTerminalAndDiagnosable(t *testing.T) {
	for _, class := range []authoring.FailureClass{
		authoring.FailureInvariantViolation,
		authoring.FailureBlockedBug,
	} {
		t.Run(string(class), func(t *testing.T) {
			engine, _, plan, _ := preparedEngine(t)
			fp := fingerprintOf(t, engine)
			if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
				t.Fatalf("issue: %v", err)
			}
			if _, err := engine.Submit(spawnReceipt(t, EventID("evt-def-receipt-"+strings.ToLower(string(class))), reviewAction, "agent-review-1"), fp); err != nil {
				t.Fatalf("receipt: %v", err)
			}
			result, err := NewWorkerResultEvent(
				EventID("evt-def-result-"+strings.ToLower(string(class))), reviewAction,
				testProvider, OutcomeRuntimeError, "sha256:defect", class,
			)
			if err != nil {
				t.Fatalf("result event: %v", err)
			}
			acceptance, err := engine.Submit(result, fp)
			if err != nil {
				t.Fatalf("result: %v", err)
			}
			if acceptance.FailureClass != string(class) || acceptance.RecoveryAction != string(RecoveryFail) {
				t.Fatalf("failure acceptance = %+v", acceptance)
			}
			if len(acceptance.Refill) != 0 {
				t.Fatalf("definition failure refilled actions: %+v", acceptance.Refill)
			}

			snap := engine.LoadMustSucceed(t)
			key := reviewTaskKey()
			if snap.State.TaskStatusOf(key) != runtime.TaskTerminal {
				t.Fatalf("task status = %s, want TERMINAL", snap.State.TaskStatusOf(key))
			}
			if len(snap.State.Expected) != 0 {
				t.Fatalf("expected tasks remain: %+v", snap.State.Expected)
			}
			if _, ok := snap.State.Attempts[key]; ok {
				t.Fatalf("current Attempt remains: %+v", snap.State.Attempts[key])
			}
			if _, ok := snap.State.PendingActions[reviewAction]; ok {
				t.Fatalf("pending action remains: %+v", snap.State.PendingActions[reviewAction])
			}
			stored, ok := snap.State.Results[reviewAction]
			if !ok || stored.FailureClass != class {
				t.Fatalf("stored failure result = %+v ok=%v", stored, ok)
			}
			if len(snap.State.RecoveryRecords) != 1 {
				t.Fatalf("recovery records = %+v", snap.State.RecoveryRecords)
			}
			record := snap.State.RecoveryRecords[0]
			if record.Class != class || record.Action != RecoveryFail {
				t.Fatalf("recovery record = %+v", record)
			}
			detail := strings.ToLower(record.Detail)
			if !strings.Contains(detail, "diagnose") {
				t.Fatalf("failure detail lacks diagnose guidance: %q", record.Detail)
			}
			if strings.Contains(detail, "operator") || strings.Contains(detail, "agent") {
				t.Fatalf("definition failure detail exposes recovery action: %q", record.Detail)
			}
		})
	}
}

// TestTransientResultCanResumeOriginalAttempt verifies that a recoverable
// RUNTIME_ERROR remains audit-visible without occupying the terminal result
// slot. The same action/Attempt can subsequently finish with PASS.
func TestTransientResultCanResumeOriginalAttempt(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-transient-spawn", reviewAction, "agent-review-1"), fp); err != nil {
		t.Fatalf("spawn receipt: %v", err)
	}
	transient, err := NewWorkerResultEvent(
		"evt-transient-failure", reviewAction, testProvider, OutcomeRuntimeError,
		"sha256:temporary", authoring.FailureTransientEngine,
	)
	if err != nil {
		t.Fatalf("transient event: %v", err)
	}
	accepted, err := engine.Submit(transient, fp)
	if err != nil {
		t.Fatalf("transient result: %v", err)
	}
	if accepted.RecoveryAction != string(RecoveryResumeAttempt) {
		t.Fatalf("transient acceptance = %+v", accepted)
	}
	afterFailure := engine.LoadMustSucceed(t)
	if _, terminal := afterFailure.State.Results[reviewAction]; terminal {
		t.Fatal("recoverable result occupied terminal result slot")
	}
	if len(afterFailure.State.RecoverableResults[reviewAction]) != 1 || len(afterFailure.State.PendingActions) != 1 {
		t.Fatalf("recoverable ledger/pending = %+v / %+v", afterFailure.State.RecoverableResults, afterFailure.State.PendingActions)
	}
	beforeAttempt := afterFailure.State.Attempts[reviewTaskKey()]
	recovery, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine}, fp)
	if err != nil || recovery.Action != RecoveryResumeAttempt {
		t.Fatalf("recover original Attempt = %+v err=%v", recovery, err)
	}
	if current := engine.LoadMustSucceed(t).State.Attempts[reviewTaskKey()]; current.ID != beforeAttempt.ID {
		t.Fatalf("resume replaced Attempt %q with %q", beforeAttempt.ID, current.ID)
	}
	pass, err := NewWorkerResultEvent("evt-transient-pass", reviewAction, testProvider, OutcomePass, "sha256:complete", "")
	if err != nil {
		t.Fatalf("pass event: %v", err)
	}
	if finalAcceptance, err := engine.Submit(pass, fp); err != nil || finalAcceptance.Status != "ACCEPTED" {
		t.Fatalf("PASS after resume = %+v err=%v", finalAcceptance, err)
	}
	final := engine.LoadMustSucceed(t)
	if result, ok := final.State.Results[reviewAction]; !ok || result.Outcome != OutcomePass {
		t.Fatalf("terminal result = %+v ok=%v", result, ok)
	}
	if _, pending := final.State.PendingActions[reviewAction]; pending {
		t.Fatal("completed resumed Attempt remained pending")
	}
}

// TestFailedSpawnReceiptRoutesAndAllowsRetry verifies FAILED is classified and
// retained as failure history without blocking a later SPAWNED receipt for the
// same durable Attempt.
func TestFailedSpawnReceiptRoutesAndAllowsRetry(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	failed, err := NewSpawnReceiptWithFailureClass(
		"evt-spawn-failed", reviewAction, testProvider, "agent-review-1",
		SpawnStatusFailed, authoring.FailureTransientEngine,
	)
	if err != nil {
		t.Fatalf("failed spawn event: %v", err)
	}
	acceptance, err := engine.Submit(failed, fp)
	if err != nil || acceptance.RecoveryAction != string(RecoveryResumeAttempt) {
		t.Fatalf("failed spawn acceptance = %+v err=%v", acceptance, err)
	}
	afterFailure := engine.LoadMustSucceed(t)
	if len(afterFailure.State.SpawnFailures[reviewAction]) != 1 || len(afterFailure.State.RecoveryRecords) != 1 {
		t.Fatalf("spawn failure/recovery ledger = %+v / %+v", afterFailure.State.SpawnFailures, afterFailure.State.RecoveryRecords)
	}
	if _, final := afterFailure.State.SpawnReceipts[reviewAction]; final {
		t.Fatal("FAILED spawn occupied final receipt slot")
	}
	if _, pending := afterFailure.State.PendingActions[reviewAction]; !pending {
		t.Fatal("retryable spawn failure retired current Attempt")
	}
	if retried, err := engine.Submit(spawnReceipt(t, "evt-spawn-retried", reviewAction, "agent-review-1"), fp); err != nil || retried.Status != "ACCEPTED" {
		t.Fatalf("SPAWNED retry = %+v err=%v", retried, err)
	}
}

func TestFailedSpawnReceiptClearsResultStagedBeforeReceipt(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	staged, err := NewWorkerResultEvent("evt-staged-before-failed-spawn", reviewAction, testProvider, OutcomeFail, "sha256:failed", authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("staged result event: %v", err)
	}
	if acceptance, err := engine.Submit(staged, fp); err != nil || acceptance.Status != "STAGED" {
		t.Fatalf("staged result = %+v err=%v", acceptance, err)
	}
	failed, err := NewSpawnReceiptWithFailureClass("evt-failed-spawn-after-result", reviewAction, testProvider, "agent-review-1", SpawnStatusFailed, authoring.FailureBusinessReject)
	if err != nil {
		t.Fatalf("failed spawn event: %v", err)
	}
	if _, err := engine.Submit(failed, fp); err != nil {
		t.Fatalf("failed spawn: %v", err)
	}
	snapshot := engine.LoadMustSucceed(t)
	if _, ok := snapshot.State.StagedResults[reviewAction]; ok {
		t.Fatal("FAILED spawn left a result staged for the failed dispatch")
	}
	if snapshot.State.TaskStatusOf(reviewTaskKey()) != runtime.TaskTerminal {
		t.Fatalf("task status = %s, want TERMINAL", snapshot.State.TaskStatusOf(reviewTaskKey()))
	}
}

func TestRecoverableResultBecomesObsoleteAfterAttemptReplacement(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-transient-spawn-for-replacement", reviewAction, "agent-review-1"), fp); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	transient, err := NewWorkerResultEvent("evt-transient-result-for-replacement", reviewAction, testProvider, OutcomeRuntimeError, "sha256:temporary", authoring.FailureTransientEngine)
	if err != nil {
		t.Fatalf("transient result event: %v", err)
	}
	if _, err := engine.Submit(transient, fp); err != nil {
		t.Fatalf("transient result: %v", err)
	}
	if _, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine, CauseKnown: true}, fp); err != nil {
		t.Fatalf("replace attempt: %v", err)
	}
	late, err := NewWorkerResultEvent("evt-late-recoverable-result", reviewAction, testProvider, OutcomeRuntimeError, "sha256:temporary", authoring.FailureTransientEngine)
	if err != nil {
		t.Fatalf("late result event: %v", err)
	}
	acceptance, err := engine.Submit(late, fp)
	if err != nil || acceptance.Status != "OBSOLETE_RESULT" {
		t.Fatalf("late result = %+v err=%v", acceptance, err)
	}
	snapshot := engine.LoadMustSucceed(t)
	if _, ok := snapshot.State.ObsoleteResults[reviewAction]; !ok {
		t.Fatal("obsolete result was not made visible after attempt replacement")
	}
}

func TestFailedUnknownSpawnReceiptIsReconcileable(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	failed, err := NewSpawnReceiptWithFailureClass("evt-failed-unknown-spawn", reviewAction, testProvider, "agent-review-unknown", SpawnStatusFailed, authoring.FailureSideEffectUnknown)
	if err != nil {
		t.Fatalf("failed unknown spawn event: %v", err)
	}
	acceptance, err := engine.Submit(failed, fp)
	if err != nil || acceptance.RecoveryAction != string(RecoveryReconcile) {
		t.Fatalf("failed unknown spawn = %+v err=%v", acceptance, err)
	}
	snapshot := engine.LoadMustSucceed(t)
	if receipt := snapshot.State.SpawnReceipts[reviewAction]; receipt.Status != SpawnStatusUnknown {
		t.Fatalf("reconcileable receipt = %+v", receipt)
	}
	planResult, _, err := engine.ReconcileUnknownReceipt(reviewAction, fp)
	if err != nil || planResult.Action != RecoveryOperator {
		t.Fatalf("reconcile failed unknown spawn = %+v err=%v", planResult, err)
	}
}

func TestUnknownReceiptRetrySequenceAttachesOnlyUniqueLifecycle(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatal(err)
	}
	failed, err := NewSpawnReceiptWithFailureClass("unknown-sequence-failed", reviewAction, testProvider, "old-agent", SpawnStatusFailed, authoring.FailureSideEffectUnknown)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := engine.Submit(failed, fp)
	if err != nil || accepted.RecoveryAction != string(RecoveryReconcile) {
		t.Fatalf("failed UNKNOWN route = %+v err=%v", accepted, err)
	}
	if receipt := engine.LoadMustSucceed(t).State.SpawnReceipts[reviewAction]; receipt.Status != SpawnStatusUnknown {
		t.Fatalf("failed side-effect receipt = %+v", receipt)
	}
	recovery, _, err := engine.RecoverAttempt(reviewAction, Interruption{Class: authoring.FailureTransientEngine, CauseKnown: true}, fp)
	if err != nil || recovery.Action != RecoveryNewAttempt {
		t.Fatalf("retry replacement = %+v err=%v", recovery, err)
	}
	snapshot := engine.LoadMustSucceed(t)
	var replacement string
	for actionID := range snapshot.State.PendingActions {
		if actionID != reviewAction {
			replacement = actionID
		}
	}
	if replacement == "" {
		t.Fatal("replacement action not installed")
	}
	correlation := "replacement-agent"
	for _, event := range []Event{
		mustLifecycleEvent(t, "unknown-sequence-start", correlation, correlation, LifecycleStart),
		mustLifecycleEvent(t, "unknown-sequence-stop", correlation, correlation, LifecycleStop),
	} {
		if _, err := engine.Submit(event, fp); err != nil {
			t.Fatal(err)
		}
	}
	unknown, err := NewSpawnReceiptEvent("unknown-sequence-receipt", replacement, testProvider, correlation, SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(unknown, fp); err != nil {
		t.Fatal(err)
	}
	planResult, _, err := engine.ReconcileUnknownReceipt(replacement, fp)
	if err != nil || planResult.Action != RecoveryAttachReceipt || planResult.LifecycleMatches != 1 {
		t.Fatalf("unique lifecycle reconciliation = %+v err=%v", planResult, err)
	}
	final := engine.LoadMustSucceed(t)
	if final.State.SpawnReceipts[replacement].Status != SpawnStatusAttached || len(final.State.SpawnFailures[reviewAction]) != 1 {
		t.Fatalf("unknown sequence final ledger = %+v failures=%+v", final.State.SpawnReceipts, final.State.SpawnFailures)
	}
}

func mustLifecycleEvent(t *testing.T, id, correlation, identity, event string) Event {
	t.Helper()
	result, err := NewCorrelatedLifecycleEvent(EventID(id), testProvider, correlation, identity, event)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// TestOperatorObservationAdmits：typed observation 绑定真实对账项入账；
// 伪造 subject（无 UNKNOWN receipt / 未清账 intent）拒绝且零写入；Ask
// 决定（1b 两阶段）在统一接纳面上照常关账生效。
func TestOperatorObservationAdmits(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	forged, err := NewOperatorObservationEvent("evt-obs-forged", "forged-not-pending",
		decision.Fact{Source: decision.SourceReceipt, Key: "spawn.status", Value: "forged"},
	)
	if err != nil {
		t.Fatalf("forged observation event: %v", err)
	}
	before := engine.LoadMustSucceed(t)
	if _, err := engine.Submit(forged, fp); err == nil {
		t.Fatal("forged observation subject was accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownAction {
		t.Fatalf("forged observation code = %q, want %q", code, CodeUnknownAction)
	}
	after := engine.LoadMustSucceed(t)
	if len(after.State.OperatorObservations) != 0 || after.Revision != before.Revision {
		t.Fatalf("forged observation mutated state: revision %d -> %d, observations=%d",
			before.Revision, after.Revision, len(after.State.OperatorObservations))
	}

	unknownSpawn, err := NewSpawnReceiptEvent("evt-obs-unknown-spawn", reviewAction, testProvider, "obs-agent", SpawnStatusUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(unknownSpawn, fp); err != nil {
		t.Fatalf("unknown spawn: %v", err)
	}
	observation, err := NewOperatorObservationEvent("evt-obs-1", reviewAction,
		decision.Fact{Source: decision.SourceReceipt, Key: "spawn.status", Value: "observed-running"},
		decision.Fact{Source: decision.SourceHost, Key: "bridge.available", Value: "true"},
	)
	if err != nil {
		t.Fatalf("observation event: %v", err)
	}
	acceptance, err := engine.Submit(observation, fp)
	if err != nil {
		t.Fatalf("submit observation: %v", err)
	}
	if acceptance.Status != "ACCEPTED" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	snap := engine.LoadMustSucceed(t)
	if len(snap.State.OperatorObservations) != 1 {
		t.Fatalf("observations = %d", len(snap.State.OperatorObservations))
	}
	recorded := snap.State.OperatorObservations[0]
	if recorded.Subject != reviewAction || recorded.EventID != "evt-obs-1" || len(recorded.Facts) != 2 {
		t.Fatalf("recorded = %+v", recorded)
	}
	if recorded.Facts[0].Source != decision.SourceReceipt || recorded.Facts[0].Key != "spawn.status" {
		t.Fatalf("facts not typed: %+v", recorded.Facts)
	}

	// Ask 决定关账生效（衔接 1b）。
	if _, err := engine.Submit(resetRequest(t, "req-reset-9"), fp); err != nil {
		t.Fatalf("request: %v", err)
	}
	token, err := engine.Freshness("req-reset-9")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}
	decide, err := NewDecideEvent("evt-decide-9", "req-reset-9", token, "proceed")
	if err != nil {
		t.Fatalf("decide event: %v", err)
	}
	if _, err := engine.Submit(decide, fp); err != nil {
		t.Fatalf("decide: %v", err)
	}
	final := engine.LoadMustSucceed(t)
	if ask, pending := final.State.PendingAsks["req-reset-9"]; !pending || !ask.Resolved || final.State.Decisions["req-reset-9"].Choice != "proceed" {
		t.Fatalf("ask not closed: ask=%+v pending=%v decisions=%+v", ask, pending, final.State.Decisions)
	}

	// Operator 负向：非法来源枚举、空 subject、空事实集。
	if _, err := NewOperatorObservationEvent("evt-obs-bad", reviewAction,
		decision.Fact{Source: decision.FactSource("NOT_A_SOURCE"), Key: "k", Value: "v"},
	); err == nil {
		t.Fatal("invalid fact source accepted")
	} else if code := rejectionCode(t, err); code != CodeEventSchemaInvalid {
		t.Fatalf("bad source code = %q", code)
	}
	if _, err := NewOperatorObservationEvent("evt-obs-empty", " ", decision.Fact{Source: decision.SourceHost, Key: "k", Value: "v"}); err == nil {
		t.Fatal("empty subject accepted")
	}
	if _, err := NewOperatorObservationEvent("evt-obs-nofacts", reviewAction); err == nil {
		t.Fatal("empty facts accepted")
	}
}

// TestFreeUserEventRejectedThroughSubmit：越权自由 USER_* 事件在统一
// 接纳面上拒绝且零状态变化（衔接 1b 封闭 kind）。
func TestFreeUserEventRejectedThroughSubmit(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	before := stateBytesOf(t, dir)
	rogue := Event{ID: "evt-rogue", Kind: EventKind("USER_FORCE_SEAL")}
	if _, err := engine.Submit(rogue, fp); err == nil {
		t.Fatal("free USER_* event accepted")
	} else if code := rejectionCode(t, err); code != CodeUnknownEventKind {
		t.Fatalf("rogue code = %q", code)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("rogue event changed state bytes")
	}
	if _, err := engine.Submit(spawnReceipt(t, "evt-recv-after-rogue", reviewAction, "agent-1"), fp); err != nil {
		t.Fatalf("engine unusable after rogue event: %v", err)
	}
}

// reopenEngineWith 用双 agent fixture 配置在同一状态目录重开引擎（补位
// 签发的重启语义验证）。
func reopenEngineWith(t *testing.T, dir string) *Engine {
	t.Helper()
	cd, reg := twoAgentFixture(t)
	engine, err := New(mustStore(t, dir), Config{Definition: cd, Registry: reg, Capacity: 1}, nil)
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	return engine
}
