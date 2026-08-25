package protocol

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
)

// testProvider 是 fake host 的 provider 身份（run 绑定）。
const testProvider = "fake-host"

// newTestEngine 在隔离临时目录构造引擎（绑定 checked-in 定义与注册表、
// 容量 4；无外部事实依赖——指纹为空观察的确定 digest），返回引擎与其
// 状态目录。
func newTestEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	engine, err := New(store, Config{
		Definition: compiledWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	}, nil)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return engine, dir
}

// fingerprintOf 取引擎的当前观察指纹（提交事务的期望值）。
func fingerprintOf(t *testing.T, engine *Engine) string {
	t.Helper()
	fp, err := engine.ObserveFingerprint()
	if err != nil {
		t.Fatalf("observe fingerprint: %v", err)
	}
	return fp
}

// stateBytesOf 读取落盘的 state.json 原始字节（零状态变化断言用）。
func stateBytesOf(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	return data
}

// mustStore 在既有目录上重开持久 Store（重启语义）。
func mustStore(t *testing.T, dir string) *persistence.Store {
	t.Helper()
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store
}

// compiledWorkflow 编译 checked-in 定义（签发链路复用阶段 1 全套派生）。
func compiledWorkflow(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile workflow: %v", err)
	}
	return cd
}

// preparedEngine 构造已初始化、entry 两步已完成的引擎，并返回 review
// worker 的 Ready 计划（issue 测试的标准起点）。
func preparedEngine(t *testing.T) (*Engine, string, *decision.Plan, *compiler.CompiledDefinition) {
	t.Helper()
	engine, dir := newTestEngine(t)
	cd := compiledWorkflow(t)
	view, err := decision.NewState(definition.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	for _, step := range []authoring.StepID{"entry.parse", "entry.persist"} {
		if err := view.CompleteStep(step, cd); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}
	plan, err := decision.Decide(view, decision.Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if plan.Next.Kind != decision.KindReady {
		t.Fatalf("plan kind = %s, want READY", plan.Next.Kind)
	}
	if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("init: %v", err)
	}
	return engine, dir, plan, cd
}

// reviewTaskKey 是 checked-in 定义的 agent 任务键（review/review.worker）。
func reviewTaskKey() runtime.TaskKey {
	return runtime.TaskKey{Node: "review", Step: "review.worker"}
}

// TestInitLoadLifecycle：初始化落盘 revision 1；重复初始化与未初始化
// 读取可区分拒绝；重复初始化零状态变化。
func TestInitLoadLifecycle(t *testing.T) {
	engine, dir := newTestEngine(t)
	if _, err := engine.Load(); err == nil {
		t.Fatal("load before init accepted")
	} else if code := rejectionCode(t, err); code != CodeNotInitialized {
		t.Fatalf("load before init code = %q", code)
	}
	view, err := decision.NewState("2", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
		t.Fatalf("init: %v", err)
	}
	snap, err := engine.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snap.Revision != 1 {
		t.Fatalf("initial revision = %d, want 1", snap.Revision)
	}
	if snap.State.Phase != runtime.PhaseIntakeRegistered || string(snap.State.DefinitionVersion) != "2" {
		t.Fatalf("view not persisted: %+v", snap.State.State)
	}
	before := stateBytesOf(t, dir)
	if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err == nil {
		t.Fatal("re-init accepted")
	} else if code := rejectionCode(t, err); code != CodeAlreadyInitialized {
		t.Fatalf("re-init code = %q", code)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected re-init changed state bytes")
	}
	if err := engine.Init(nil, testProvider, "fp"); err == nil {
		t.Fatal("nil view accepted")
	}
}

// TestIssueFromPlanRecordsModel：签发落账 expected 清单、当前 Attempt
// （确定性 ID）与 pendingActions，任务视图推进 ISSUED，且重启后仍在档。
func TestIssueFromPlanRecordsModel(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	issued, revision, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(issued) != 1 || issued[0].ActionID != "act:review/review.worker" {
		t.Fatalf("issued set = %+v", issued)
	}
	if revision != 2 {
		t.Fatalf("issue revision = %d, want 2", revision)
	}
	key := reviewTaskKey()
	snap, err := engine.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.State.Expected) != 1 || snap.State.Expected[0] != key {
		t.Fatalf("expected = %+v", snap.State.Expected)
	}
	attempt, ok := snap.State.Attempts[key]
	if !ok || attempt.ID != "att:review/review.worker:2" || attempt.ActionID != "act:review/review.worker" {
		t.Fatalf("attempt = %+v ok=%v", attempt, ok)
	}
	wantIdentity, err := identityOfPlan(plan)
	if err != nil {
		t.Fatalf("plan identity: %v", err)
	}
	if attempt.Bindings != (AttemptBindings{Task: key, Snapshot: fp, Responsibility: testProvider}) || attempt.Plan != wantIdentity {
		t.Fatalf("attempt bindings/plan = %+v / %+v", attempt.Bindings, attempt.Plan)
	}
	pending, ok := snap.State.PendingActions["act:review/review.worker"]
	if !ok || pending.AttemptID != attempt.ID || pending.Task != key || pending.Step != "review.worker" {
		t.Fatalf("pending action = %+v ok=%v", pending, ok)
	}
	if snap.State.TaskStatusOf(key) != runtime.TaskIssued {
		t.Fatalf("task status = %s, want ISSUED", snap.State.TaskStatusOf(key))
	}

	// 独立引擎实例（同一状态目录）重读：签发落账持久。
	store2, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package"})
	if err != nil {
		t.Fatalf("store2: %v", err)
	}
	engine2, err := New(store2, Config{
		Definition: compiledWorkflow(t), Registry: definition.Registry(), Capacity: 4,
	}, nil)
	if err != nil {
		t.Fatalf("engine2: %v", err)
	}
	snap2, err := engine2.Load()
	if err != nil {
		t.Fatalf("engine2 load: %v", err)
	}
	if !reflect.DeepEqual(snap.State, snap2.State) {
		t.Fatal("state differs across engine instances")
	}
}

// TestIssueRejectsForgedPlanComponents：definition/state/observation 中任一
// canonical binding 被改写，都在签发落账前拒绝。
func TestIssueRejectsForgedPlanComponents(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	before := stateBytesOf(t, dir)
	cases := []struct {
		name   string
		mutate func(*decision.Plan)
	}{
		{"definition", func(p *decision.Plan) { p.DefinitionDigest = "sha256:forged-definition" }},
		{"state", func(p *decision.Plan) { p.StateDigest = "sha256:forged-state" }},
		{"observation", func(p *decision.Plan) { p.ObservationDigest = "sha256:forged-observation" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := *plan
			tc.mutate(&candidate)
			if _, _, err := engine.IssueFromPlan(&candidate, decision.Admission{Capacity: 4}, fp); err == nil {
				t.Fatal("forged plan was issued")
			} else if code := rejectionCode(t, err); code != CodePlanBindingMismatch {
				t.Fatalf("forged plan code = %q", code)
			}
			if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatal("forged plan changed state bytes")
			}
		})
	}
}

// TestIssueStalePlanRejected：首次签发改变 state identity 后，原计划不能
// 再次签发，且拒绝保持零状态变化。
func TestIssueStalePlanRejected(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("first issue: %v", err)
	}
	before := stateBytesOf(t, dir)
	snapBefore, err := engine.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, _, err = engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp)
	if err == nil {
		t.Fatal("duplicate issue accepted")
	} else if code := rejectionCode(t, err); code != CodePlanBindingMismatch {
		t.Fatalf("stale plan issue code = %q", code)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected duplicate issue changed state bytes")
	}
	snapAfter, err := engine.Load()
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if snapBefore.Revision != snapAfter.Revision || !reflect.DeepEqual(snapBefore.State, snapAfter.State) {
		t.Fatal("rejected duplicate issue changed state")
	}
}

// TestIssueNegativePaths：未初始化签发拒绝；非 Ready 计划签发拒绝。
func TestIssueNegativePaths(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	_, _, err := engine.IssueFromPlan(nil, decision.Admission{Capacity: 1}, fingerprintOf(t, engine))
	if err == nil {
		t.Fatal("nil plan accepted")
	}

	fresh, _ := newTestEngine(t)
	view, err := decision.NewState("2", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatalf("new view: %v", err)
	}
	cd := compiledWorkflow(t)
	waitPlan, err := decision.Decide(view, decision.Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, _, err := fresh.IssueFromPlan(waitPlan, decision.Admission{Capacity: 1}, "fp"); err == nil {
		t.Fatal("issue before init accepted")
	} else if code := rejectionCode(t, err); code != CodeNotInitialized {
		t.Fatalf("issue before init code = %q", code)
	}
	// 初始化后同一非 Ready 计划：SelectIssued 的派生拒绝原样上抛。
	if err := fresh.Init(view, testProvider, fingerprintOf(t, fresh)); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, _, err := fresh.IssueFromPlan(waitPlan, decision.Admission{Capacity: 1}, "fp"); err == nil {
		t.Fatal("non-ready plan accepted")
	} else if !strings.Contains(err.Error(), "not READY") {
		t.Fatalf("non-ready plan error = %v", err)
	}
}

// TestTaskProgressLifecycle：RUNNING → VALIDATING → TERMINAL 全链推进；
// TERMINAL 后 expected/pendingAction/当前 Attempt 收回、任务视图留在
// TERMINAL，全部持久。
func TestTaskProgressLifecycle(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	key := reviewTaskKey()
	attemptID := "att:review/review.worker:2"
	for i, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID([]string{"evt-run-1", "evt-run-2", "evt-run-3"}[i]), key, attemptID, status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fp); err != nil {
			t.Fatalf("submit %s: %v", status, err)
		}
	}
	snap, err := engine.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.State.Expected) != 0 || len(snap.State.Attempts) != 0 || len(snap.State.PendingActions) != 0 {
		t.Fatalf("terminal bookkeeping not reclaimed: expected=%+v attempts=%+v pending=%+v",
			snap.State.Expected, snap.State.Attempts, snap.State.PendingActions)
	}
	if snap.State.TaskStatusOf(key) != runtime.TaskTerminal {
		t.Fatalf("task status = %s, want TERMINAL", snap.State.TaskStatusOf(key))
	}
	if len(snap.State.Events) != 3 {
		t.Fatalf("event ledger = %d entries, want 3", len(snap.State.Events))
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("state file: %v", err)
	}
}

// TestTaskProgressNotCurrent：从未签发的任务、已终态的任务都判非当前
// 节点（EVENT_NOT_CURRENT），零状态变化。
func TestTaskProgressNotCurrent(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	before := stateBytesOf(t, dir)

	// 从未签发的任务（别的节点）：非当前节点。
	stranger := runtime.TaskKey{Node: "fan", Step: "fan.slice"}
	ev, err := NewTaskEvent("evt-ghost", stranger, "att:x:1", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("stranger task accepted")
	} else if code := rejectionCode(t, err); code != CodeEventNotCurrent {
		t.Fatalf("stranger code = %q", code)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("non-current rejection changed state bytes")
	}

	// 推到 TERMINAL 后同一任务再报进度：expected 已收回，仍非当前。
	key := reviewTaskKey()
	for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskTerminal} {
		ev, err := NewTaskEvent(EventID("evt-t-"+string(status)), key, "att:review/review.worker:2", status)
		if err != nil {
			t.Fatalf("task event: %v", err)
		}
		if _, err := engine.Submit(ev, fp); err != nil {
			t.Fatalf("submit %s: %v", status, err)
		}
	}
	ev, err = NewTaskEvent("evt-t-late", key, "att:review/review.worker:2", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("progress after terminal accepted")
	} else if code := rejectionCode(t, err); code != CodeEventNotCurrent {
		t.Fatalf("post-terminal code = %q", code)
	}
}

// TestTaskProgressStaleAttempt：事件携带的 attempt 不是当前 Attempt 时
// 可区分拒绝（STALE_ATTEMPT），零状态变化。
func TestTaskProgressStaleAttempt(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	before := stateBytesOf(t, dir)
	ev, err := NewTaskEvent("evt-stale", reviewTaskKey(), "att:review/review.worker:99", runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("stale attempt accepted")
	} else if code := rejectionCode(t, err); code != CodeStaleAttempt {
		t.Fatalf("stale attempt code = %q", code)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("stale attempt rejection changed state bytes")
	}
}

// TestTaskProgressIllegalTransition：跳过 RUNNING 直报 VALIDATING、自环
// 重复 RUNNING 都违反 runtime 任务状态机（复用阶段 1 迁移表）。
func TestTaskProgressIllegalTransition(t *testing.T) {
	engine, _, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	key := reviewTaskKey()
	attempt := "att:review/review.worker:2"
	ev, err := NewTaskEvent("evt-skip", key, attempt, runtime.TaskValidating)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("skip-transition accepted")
	} else if code := rejectionCode(t, err); code != CodeIllegalTransition {
		t.Fatalf("skip code = %q", code)
	}
	ev, err = NewTaskEvent("evt-run", key, attempt, runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err != nil {
		t.Fatalf("submit running: %v", err)
	}
	ev, err = NewTaskEvent("evt-repeat", key, attempt, runtime.TaskRunning)
	if err != nil {
		t.Fatalf("task event: %v", err)
	}
	if _, err := engine.Submit(ev, fp); err == nil {
		t.Fatal("self-loop accepted")
	} else if code := rejectionCode(t, err); code != CodeIllegalTransition {
		t.Fatalf("self-loop code = %q", code)
	}
}

// TestSubmitFingerprintIntegration：期望指纹与实际不符时由批 1a 持久层
// 拒绝（写前拒绝、零状态变化）——协议层不绕过指纹纪律。
func TestSubmitFingerprintIntegration(t *testing.T) {
	engine, dir, plan, _ := preparedEngine(t)
	fp := fingerprintOf(t, engine)
	if _, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 4}, fp); err != nil {
		t.Fatalf("issue: %v", err)
	}
	before := stateBytesOf(t, dir)
	request, err := NewRequestEvent("evt-req-fp", ControlAbort, AskOption{ID: "abort", Label: "终止"})
	if err != nil {
		t.Fatalf("request event: %v", err)
	}
	_, err = engine.Submit(request, "sha256:different-observation")
	var changed *persistence.FingerprintChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("fingerprint mismatch error is %v, want *persistence.FingerprintChangedError", err)
	}
	if after := stateBytesOf(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatal("fingerprint rejection changed state bytes")
	}
	if _, ok := engine.LoadMustSucceed(t).State.PendingAsks["evt-req-fp"]; ok {
		t.Fatal("ask created despite fingerprint rejection")
	}
}

// LoadMustSucceed 是测试辅助：Load 或 Fatal。
func (e *Engine) LoadMustSucceed(t *testing.T) Snapshot {
	t.Helper()
	snap, err := e.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return snap
}
