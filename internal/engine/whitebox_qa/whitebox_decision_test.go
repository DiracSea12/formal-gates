package whitebox_qa

import (
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// mkState 是 NewState 的夹具薄包装；fxState 绑定夹具定义版本。
func mkState(t *testing.T, version authoring.DefinitionVersion, phase runtime.RunPhase) *decision.State {
	t.Helper()
	s, err := decision.NewState(version, phase)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	return s
}

func fxState(t *testing.T, phase runtime.RunPhase) *decision.State {
	t.Helper()
	return mkState(t, fxDefVersion, phase)
}

func fxDecide(t *testing.T, state *decision.State, obs decision.Observation, cd *compiler.CompiledDefinition) *decision.Plan {
	t.Helper()
	plan, err := decision.Decide(state, obs, cd)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	return plan
}

// 用例：State 构造校验版本与 phase 枚举；phase/task 迁移经静态表校验；
// 未登记任务键缺省按 QUEUED 解释。
func TestStateValidatesAndTransitionsPhaseAndTasks(t *testing.T) {
	if _, err := decision.NewState("", runtime.PhaseDevelopmentParallel); err == nil {
		t.Error("empty definition version must be rejected")
	}
	if _, err := decision.NewState(fxDefVersion, runtime.RunPhase("DAYDREAM")); err == nil {
		t.Error("invalid phase must be rejected")
	} else {
		wantErrContaining(t, err, "invalid")
	}

	s := fxState(t, runtime.PhaseDevelopmentParallel)
	if err := s.TransitionPhase(runtime.PhaseSnapshotReady); err != nil {
		t.Fatalf("legal phase transition rejected: %v", err)
	}
	if s.Phase != runtime.PhaseSnapshotReady {
		t.Fatalf("phase = %s, want SNAPSHOT_READY", s.Phase)
	}
	if err := s.TransitionPhase(runtime.PhaseIntakeRegistered); err == nil {
		t.Error("backward phase transition must be rejected")
	} else {
		wantErrContaining(t, err, "illegal phase transition")
	}

	key := runtime.TaskKey{Node: "n1", Step: "s.review"}
	if got := s.TaskStatusOf(key); got != runtime.TaskQueued {
		t.Fatalf("unregistered task status = %s, want QUEUED", got)
	}
	if err := s.TransitionTask(runtime.TaskKey{Node: "", Step: "s.review"}, runtime.TaskIssued); err == nil {
		t.Error("invalid task key must be rejected")
	} else {
		wantErrContaining(t, err, "task key")
	}
	if err := s.TransitionTask(key, runtime.TaskIssued); err != nil {
		t.Fatalf("QUEUED -> ISSUED must be legal: %v", err)
	}
	if got := s.TaskStatusOf(key); got != runtime.TaskIssued {
		t.Fatalf("task status = %s, want ISSUED", got)
	}
	if err := s.TransitionTask(key, runtime.TaskQueued); err == nil {
		t.Error("task rollback must be rejected")
	}

	// canonical 字节的空集编码为 []（不是 null）。
	fresh := fxState(t, runtime.PhaseDevelopmentParallel)
	b, err := fresh.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"completed": []`) || !strings.Contains(string(b), `"tasks": []`) {
		t.Fatalf("state canonical bytes must encode empty sets as []: %s", b)
	}
	d, err := fresh.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d, "sha256:") {
		t.Fatalf("state digest %q must carry sha256: prefix", d)
	}
}

// 用例：CompleteStep 是运行时边界的接口校验——版本绑定不符、未知步骤、
// 重复完成、乱序/遗漏依赖四类拒绝；Completed 维持有序，使 canonical 字节
// 与完成登记顺序无关。
func TestCompleteStepEnforcesFrontierDisciplineAndSortedCanonicalBytes(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))

	fresh := fxState(t, runtime.PhaseDevelopmentParallel)
	if err := fresh.CompleteStep("s.parse", nil); err == nil {
		t.Error("nil definition must be rejected")
	}

	mismatch, _ := decision.NewState("9", runtime.PhaseDevelopmentParallel)
	if err := mismatch.CompleteStep("s.parse", cd); err == nil {
		t.Error("version mismatch must be rejected")
	} else {
		wantErrContaining(t, err, "definition version")
	}

	s := fxState(t, runtime.PhaseDevelopmentParallel)
	if err := s.CompleteStep("s.ghost", cd); err == nil {
		t.Error("unknown step must be rejected")
	} else {
		wantErrContaining(t, err, "not in definition")
	}
	if err := s.CompleteStep("s.persist", cd); err == nil {
		t.Error("out-of-order completion (dependency missing) must be rejected")
	} else {
		wantErrContaining(t, err, "out-of-order or skipped")
	}
	if err := s.CompleteStep("s.parse", cd); err != nil {
		t.Fatalf("valid completion rejected: %v", err)
	}
	if err := s.CompleteStep("s.parse", cd); err == nil {
		t.Error("duplicate completion must be rejected")
	} else {
		wantErrContaining(t, err, "duplicate")
	}

	// 有序插入：完成顺序不影响 Completed 排序与 canonical 字节。
	a := fxState(t, runtime.PhaseDevelopmentParallel)
	for _, id := range []string{"s.parse", "s.review", "s.ask", "s.dispatch"} {
		if err := a.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatalf("a: complete %q: %v", id, err)
		}
	}
	b := fxState(t, runtime.PhaseDevelopmentParallel)
	for _, id := range []string{"s.parse", "s.dispatch", "s.ask", "s.review"} {
		if err := b.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatalf("b: complete %q: %v", id, err)
		}
	}
	if got, want := a.Completed, []authoring.StepID{"s.ask", "s.dispatch", "s.parse", "s.review"}; len(got) != len(want) {
		t.Fatalf("Completed = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Completed = %v, want %v", got, want)
			}
		}
	}
	ba, err := a.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	bb, err := b.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(ba) != string(bb) {
		t.Fatalf("state canonical bytes depend on completion order:\n%s\n%s", ba, bb)
	}
}

// 用例：Observe 汇成规范 Observation——事实按 (Source, Key) 排序；来源枚举
// 非法、事实来源与收集器不符、空 Key、重复事实、nil 收集器、收集器错误
// 一律拒绝；空观察编码为 "facts": []。
func TestObserveNormalizesAndRejectsConflictingFacts(t *testing.T) {
	s := fxState(t, runtime.PhaseDevelopmentParallel)

	if _, err := decision.Observe(nil, nil); err == nil {
		t.Error("nil state must be rejected")
	}
	if _, err := decision.Observe(s, []decision.Collector{nil}); err == nil {
		t.Error("nil collector must be rejected")
	}
	bad := &fakeCollector{src: "TELEPATHY", facts: nil}
	if _, err := decision.Observe(s, []decision.Collector{bad}); err == nil {
		t.Error("invalid source enum must be rejected")
	} else {
		wantErrContaining(t, err, "invalid")
	}
	mismatch := &fakeCollector{src: decision.SourceVCS,
		facts: []decision.Fact{{Source: decision.SourceFile, Key: "k", Value: "v"}}}
	if _, err := decision.Observe(s, []decision.Collector{mismatch}); err == nil {
		t.Error("fact source must match collector source")
	} else {
		wantErrContaining(t, err, "produced fact with source")
	}
	emptyKey := &fakeCollector{src: decision.SourceHost, facts: []decision.Fact{{Source: decision.SourceHost, Key: "", Value: "v"}}}
	if _, err := decision.Observe(s, []decision.Collector{emptyKey}); err == nil {
		t.Error("empty fact key must be rejected")
	} else {
		wantErrContaining(t, err, "empty fact key")
	}
	dup := &fakeCollector{src: decision.SourceFile, facts: []decision.Fact{
		{Source: decision.SourceFile, Key: "k", Value: "1"},
		{Source: decision.SourceFile, Key: "k", Value: "2"},
	}}
	if _, err := decision.Observe(s, []decision.Collector{dup}); err == nil {
		t.Error("duplicate (source, key) must be rejected regardless of value")
	} else {
		wantErrContaining(t, err, "duplicate fact")
	}
	acrossA := &fakeCollector{src: decision.SourceReceipt, facts: []decision.Fact{{Source: decision.SourceReceipt, Key: "r", Value: "x"}}}
	acrossB := &fakeCollector{src: decision.SourceReceipt, facts: []decision.Fact{{Source: decision.SourceReceipt, Key: "r", Value: "y"}}}
	if _, err := decision.Observe(s, []decision.Collector{acrossA, acrossB}); err == nil {
		t.Error("duplicate across collectors must be rejected")
	}
	failing := &fakeCollector{src: decision.SourceCapacity, err: errBoom}
	if _, err := decision.Observe(s, []decision.Collector{failing}); err == nil {
		t.Error("collector error must propagate")
	} else {
		wantErrContaining(t, err, "CAPACITY", "boom")
	}

	// 规范排序与收集器顺序、事实提交顺序无关。
	vcs := &fakeCollector{src: decision.SourceVCS, facts: []decision.Fact{
		{Source: decision.SourceVCS, Key: "zz", Value: "1"},
		{Source: decision.SourceVCS, Key: "aa", Value: "2"},
	}}
	file := &fakeCollector{src: decision.SourceFile, facts: []decision.Fact{
		{Source: decision.SourceFile, Key: "m", Value: "3"},
	}}
	obs, err := decision.Observe(s, []decision.Collector{vcs, file})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"FILE/m", "VCS/aa", "VCS/zz"}
	if len(obs.Facts) != len(wantOrder) {
		t.Fatalf("facts = %v, want order %v", obs.Facts, wantOrder)
	}
	for i, w := range wantOrder {
		f := obs.Facts[i]
		if got := string(f.Source) + "/" + f.Key; got != w {
			t.Fatalf("facts[%d] = %s, want %s", i, got, w)
		}
	}
	obs2, err := decision.Observe(s, []decision.Collector{file, vcs})
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := obs.CanonicalBytes()
	b2, _ := obs2.CanonicalBytes()
	if string(b1) != string(b2) {
		t.Fatal("observation canonical bytes must not depend on collector order")
	}

	empty, err := decision.Observe(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := empty.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(eb), `"facts": []`) {
		t.Fatalf("empty observation must encode as []: %s", eb)
	}
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }

// 用例：eligible frontier 完整且按 compiled ordinal 固定排序——新鲜 state 的
// frontier 是入口根步骤；完成根步骤后 frontier 精确等于依赖已满足且未完成
// 的全部步骤（跨全部六种 Kind）。
func TestDecideFrontierCompleteAndOrdered(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))
	s := fxState(t, runtime.PhaseDevelopmentParallel)

	plan := fxDecide(t, s, decision.Observation{}, cd)
	if len(plan.Frontier) != 1 || string(plan.Frontier[0].Step) != "s.parse" ||
		plan.Frontier[0].Ordinal != 0 || plan.Frontier[0].Kind != compiler.KindLocal {
		t.Fatalf("fresh frontier = %+v, want [s.parse(0, LOCAL)]", plan.Frontier)
	}

	if err := s.CompleteStep("s.parse", cd); err != nil {
		t.Fatal(err)
	}
	plan = fxDecide(t, s, decision.Observation{}, cd)
	want := []struct {
		id      string
		ordinal int
		kind    compiler.StepKind
	}{
		{"s.persist", 1, compiler.KindDurable},
		{"s.ask", 2, compiler.KindHumanAsk},
		{"s.dispatch", 3, compiler.KindHostAction},
		{"s.review", 4, compiler.KindAgent},
		{"s.fan", 5, compiler.KindParallel},
	}
	if len(plan.Frontier) != len(want) {
		t.Fatalf("frontier after root = %+v, want %+v", plan.Frontier, want)
	}
	for i, w := range want {
		e := plan.Frontier[i]
		if string(e.Step) != w.id || e.Ordinal != w.ordinal || e.Kind != w.kind {
			t.Fatalf("frontier[%d] = {step %s ordinal %d kind %s}, want {%s %d %s}",
				i, e.Step, e.Ordinal, e.Kind, w.id, w.ordinal, w.kind)
		}
	}
}

// 用例：NextResult Kind 判定优先级固定 Complete > Ready > HostAction > Ask
// > Wait：agent/host/ask 同时 eligible 时只产出 READY；已签发任务不进入
// 可签发集；全部签发后降级；终态 phase 或全部完成压倒一切。
func TestDecideKindPriorityCompleteOverReadyHostAskWait(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))

	newState := func(t *testing.T) *decision.State {
		s := fxState(t, runtime.PhaseDevelopmentParallel)
		if err := s.CompleteStep("s.parse", cd); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// agent/host/ask 同时 eligible：只产出 READY（agent 任务集）。
	s := newState(t)
	plan := fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindReady {
		t.Fatalf("kind = %s, want READY (agent dispatched first)", plan.Next.Kind)
	}
	if len(plan.Next.Ready.Tasks) != 1 {
		t.Fatalf("ready tasks = %+v, want exactly the agent task", plan.Next.Ready.Tasks)
	}
	tk := plan.Next.Ready.Tasks[0].Task
	if tk.String() != "n1/s.review" || string(plan.Next.Ready.Tasks[0].Step) != "s.review" {
		t.Fatalf("ready task = {%s %s}, want {n1/s.review s.review}", tk.String(), plan.Next.Ready.Tasks[0].Step)
	}
	if plan.Next.HostAction != nil || plan.Next.Ask != nil || plan.Next.Wait != nil || plan.Next.Complete != nil {
		t.Fatal("payloads outside READY must be nil")
	}
	if err := plan.Next.Validate(); err != nil {
		t.Fatal(err)
	}

	// agent 在途：降级为 HOST_ACTION。
	s = newState(t)
	mustTransitionTask(t, s, "n1", "s.review", runtime.TaskIssued)
	plan = fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindHostAction || len(plan.Next.HostAction.Steps) != 1 ||
		string(plan.Next.HostAction.Steps[0]) != "s.dispatch" {
		t.Fatalf("next = %+v, want HOST_ACTION [s.dispatch]", plan.Next)
	}

	// agent+host 在途：降级为 ASK。
	s = newState(t)
	mustTransitionTask(t, s, "n1", "s.review", runtime.TaskIssued)
	mustTransitionTask(t, s, "n1", "s.dispatch", runtime.TaskIssued)
	plan = fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindAsk || len(plan.Next.Ask.Steps) != 1 ||
		string(plan.Next.Ask.Steps[0]) != "s.ask" {
		t.Fatalf("next = %+v, want ASK [s.ask]", plan.Next)
	}

	// 全部外部步骤在途：WAIT/TASKS_IN_FLIGHT。
	s = newState(t)
	mustTransitionTask(t, s, "n1", "s.review", runtime.TaskIssued)
	mustTransitionTask(t, s, "n1", "s.dispatch", runtime.TaskIssued)
	mustTransitionTask(t, s, "n1", "s.ask", runtime.TaskIssued)
	plan = fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindWait || plan.Next.Wait.Reason != decision.WaitTasksInFlight {
		t.Fatalf("next = %+v, want WAIT/TASKS_IN_FLIGHT", plan.Next)
	}

	// 终态 phase 压倒一切（frontier 仍有可签发项）。
	terminal := mkState(t, fxDefVersion, runtime.PhaseTerminal)
	if err := terminal.CompleteStep("s.parse", cd); err != nil {
		t.Fatal(err)
	}
	plan = fxDecide(t, terminal, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindComplete {
		t.Fatalf("terminal phase next = %s, want COMPLETE", plan.Next.Kind)
	}

	// 全部步骤完成：COMPLETE 且 frontier 为空（空集编码为 []）。
	s = fxState(t, runtime.PhaseDevelopmentParallel)
	for _, id := range []string{"s.parse", "s.persist", "s.review", "s.dispatch", "s.ask",
		"s.fan", "s.left", "s.right", "s.join", "s.cost"} {
		if err := s.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatalf("complete %q: %v", id, err)
		}
	}
	plan = fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindComplete {
		t.Fatalf("all-completed next = %s, want COMPLETE", plan.Next.Kind)
	}
	if len(plan.Frontier) != 0 {
		t.Fatalf("frontier after completion = %+v, want empty", plan.Frontier)
	}
	b, err := plan.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"frontier": []`) {
		t.Fatalf("plan canonical bytes must encode empty frontier as []: %s", b)
	}
}

func mustTransitionTask(t *testing.T, s *decision.State, node, step string, to runtime.TaskStatus) {
	t.Helper()
	if err := s.TransitionTask(runtime.TaskKey{Node: authoring.NodeID(node), Step: authoring.StepID(step)}, to); err != nil {
		t.Fatalf("transition task %s/%s -> %s: %v", node, step, to, err)
	}
}

// 用例：Wait 的封闭原因——frontier 只剩 engine-internal 步骤时投影为
// WAIT/ENGINE_INTERNAL（而非空结果）；外部步骤全部在途时 WAIT/TASKS_IN_FLIGHT。
func TestDecideWaitReasonsEngineInternalVsTasksInFlight(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))

	// 新鲜 state：唯一 frontier 步骤是 LOCAL。
	s := fxState(t, runtime.PhaseDevelopmentParallel)
	plan := fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindWait || plan.Next.Wait.Reason != decision.WaitEngineInternal {
		t.Fatalf("fresh next = %+v, want WAIT/ENGINE_INTERNAL", plan.Next)
	}

	// 外部步骤全部在途：WAIT/TASKS_IN_FLIGHT。
	s = fxState(t, runtime.PhaseDevelopmentParallel)
	if err := s.CompleteStep("s.parse", cd); err != nil {
		t.Fatal(err)
	}
	mustTransitionTask(t, s, "n1", "s.review", runtime.TaskIssued)
	mustTransitionTask(t, s, "n1", "s.dispatch", runtime.TaskIssued)
	mustTransitionTask(t, s, "n1", "s.ask", runtime.TaskIssued)
	plan = fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindWait || plan.Next.Wait.Reason != decision.WaitTasksInFlight {
		t.Fatalf("all-external-issued next = %+v, want WAIT/TASKS_IN_FLIGHT", plan.Next)
	}

	// 外部步骤全部完成后只剩 engine-internal：回到 WAIT/ENGINE_INTERNAL。
	s2 := fxState(t, runtime.PhaseDevelopmentParallel)
	for _, id := range []string{"s.parse", "s.fan", "s.ask", "s.dispatch", "s.review"} {
		if err := s2.CompleteStep(authoring.StepID(id), cd); err != nil {
			t.Fatal(err)
		}
	}
	plan = fxDecide(t, s2, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindWait || plan.Next.Wait.Reason != decision.WaitEngineInternal {
		t.Fatalf("engine-only next = %+v, want WAIT/ENGINE_INTERNAL", plan.Next)
	}
}

// 用例：Decide 对输入一致性硬拒绝——nil state/definition、版本不符、重复
// 完成、完成不在定义内的步骤、依赖未完成的先序完成（手工构造 state 的
// 二次复核），绝不静默产出空结果。
func TestDecideRejectsInconsistentInputs(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))
	obs := decision.Observation{}

	if _, err := decision.Decide(nil, obs, cd); err == nil {
		t.Error("nil state must be rejected")
	}
	if _, err := decision.Decide(fxState(t, runtime.PhaseDevelopmentParallel), obs, nil); err == nil {
		t.Error("nil definition must be rejected")
	}
	wrong, _ := decision.NewState("9", runtime.PhaseDevelopmentParallel)
	if _, err := decision.Decide(wrong, obs, cd); err == nil {
		t.Error("version mismatch must be rejected")
	} else {
		wantErrContaining(t, err, "definition version")
	}

	dup := &decision.State{DefinitionVersion: fxDefVersion, Phase: runtime.PhaseDevelopmentParallel,
		Completed: []authoring.StepID{"s.parse", "s.parse"}}
	if _, err := decision.Decide(dup, obs, cd); err == nil {
		t.Error("duplicated completed step must be rejected")
	} else {
		wantErrContaining(t, err, "duplicated")
	}

	unknown := &decision.State{DefinitionVersion: fxDefVersion, Phase: runtime.PhaseDevelopmentParallel,
		Completed: []authoring.StepID{"s.ghost"}}
	if _, err := decision.Decide(unknown, obs, cd); err == nil {
		t.Error("completed step outside definition must be rejected")
	} else {
		wantErrContaining(t, err, "not in definition")
	}

	outOfOrder := &decision.State{DefinitionVersion: fxDefVersion, Phase: runtime.PhaseDevelopmentParallel,
		Completed: []authoring.StepID{"s.persist"}}
	if _, err := decision.Decide(outOfOrder, obs, cd); err == nil {
		t.Error("completion before dependency must be rejected")
	} else {
		wantErrContaining(t, err, "before dependency")
	}
}

// 用例：携带 MISSING_ENGINE_ADAPTER marker 的 diagnostic-only 定义不得进入
// executable plan——Decide 经 canonical 编码拒绝 marker 定义。
func TestDecideRejectsDiagnosticOnlyDefinition(t *testing.T) {
	res, err := compiler.CompileDiagnostic(fxDefinition(t), fxRegistryWithout(t, "h.s.review"))
	if err != nil {
		t.Fatalf("diagnostic compile: %v", err)
	}
	if !res.Definition.MissingEngineAdapter {
		t.Fatal("fixture must carry the marker for this case")
	}
	s := fxState(t, runtime.PhaseDevelopmentParallel)
	_, err = decision.Decide(s, decision.Observation{}, res.Definition)
	if err == nil {
		t.Fatal("marker definition must not produce an executable plan")
	}
	wantErrContaining(t, err, "MISSING_ENGINE_ADAPTER")
}

// 用例：Plan canonical 字节是 (definition, state, observation) 的纯函数——
// 相同输入逐字节稳定；改变 observation 只改 ObservationDigest 与 PlanDigest；
// 改变 state 只改 StateDigest 与 PlanDigest；三类输入 digest 绑定进 Plan。
// NextResult.Validate 拒绝 Kind 与 payload 不匹配。
func TestPlanCanonicalBytesStableAndDigestBound(t *testing.T) {
	cd := fxCompile(t, fxDefinition(t), fxRegistry(t))
	s := fxState(t, runtime.PhaseDevelopmentParallel)
	obs := decision.Observation{}

	p1 := fxDecide(t, s, obs, cd)
	p2 := fxDecide(t, s, obs, cd)
	b1, err := p1.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	b2, err := p2.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Fatal("same state+observation must yield byte-identical plan")
	}
	d1, d2 := mustDigest(t, p1), mustDigest(t, p2)
	if d1 != d2 {
		t.Fatal("plan digest must be stable across identical decisions")
	}

	// 改变 observation：ObservationDigest/PlanDigest 变，其余不变。
	obs2, err := decision.Observe(s, []decision.Collector{&fakeCollector{src: decision.SourceVCS,
		facts: []decision.Fact{{Source: decision.SourceVCS, Key: "head", Value: "abc"}}}})
	if err != nil {
		t.Fatal(err)
	}
	p3 := fxDecide(t, s, obs2, cd)
	if p3.ObservationDigest == p1.ObservationDigest {
		t.Error("observation digest must react to observation change")
	}
	if p3.StateDigest != p1.StateDigest || p3.DefinitionDigest != p1.DefinitionDigest {
		t.Error("state/definition digests must not react to observation change")
	}
	if mustDigest(t, p3) == d1 {
		t.Error("plan digest must react to observation change")
	}

	// 改变 state：StateDigest/PlanDigest 变，ObservationDigest 不变。
	s2 := fxState(t, runtime.PhaseDevelopmentParallel)
	if err := s2.CompleteStep("s.parse", cd); err != nil {
		t.Fatal(err)
	}
	p4 := fxDecide(t, s2, obs2, cd)
	if p4.StateDigest == p3.StateDigest {
		t.Error("state digest must react to state change")
	}
	if p4.ObservationDigest != p3.ObservationDigest {
		t.Error("observation digest must not react to state change")
	}
	if mustDigest(t, p4) == mustDigest(t, p3) {
		t.Error("plan digest must react to state change")
	}

	// tagged union 校验：Kind 与 payload 必须恰好匹配。
	bad := []decision.NextResult{
		{Kind: decision.KindReady},
		{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitEngineInternal}, Complete: &decision.CompletePayload{}},
		{Kind: decision.Kind("FREE_FORM")},
	}
	for i, n := range bad {
		if err := n.Validate(); err == nil {
			t.Errorf("bad next result %d must fail Validate", i)
		}
	}
	if err := (decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitEngineInternal}}).Validate(); err != nil {
		t.Errorf("well-formed next result must validate: %v", err)
	}
}

func mustDigest(t *testing.T, p *decision.Plan) string {
	t.Helper()
	d, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// twoAgentFixture：两个 agent 步骤同时 eligible，供 SelectIssued 用例使用。
func twoAgentFixture(t *testing.T) (*compiler.CompiledDefinition, *decision.State) {
	t.Helper()
	root := mkLocalStep(t, fxHeader("a.root", "n0"), fxIO(),
		authoring.LocalSpec{Handler: "h.r"})
	io := fxIO("a.root")
	io.Postconditions = []authoring.PredicateRef{{ID: "pred.ok"}}
	a1 := mkAgentStep(t, fxHeader("a.one", "n1", "a.root"), io,
		authoring.AgentSpec{Handler: "h.a1", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute})
	a2 := mkAgentStep(t, fxHeader("a.two", "n1", "a.root"), io,
		authoring.AgentSpec{Handler: "h.a2", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute})
	reg := compiler.NewRegistry()
	for _, h := range []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"h.r", authoring.RunnerEngineLocal},
		{"h.a1", authoring.RunnerAgentWorker},
		{"h.a2", authoring.RunnerAgentWorker},
	} {
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []authoring.CodecID{"c.in", "c.out"} {
		if err := reg.RegisterCodec(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.RegisterPredicate("pred.ok"); err != nil {
		t.Fatal(err)
	}
	cd := fxCompile(t, fxDefinition(t, root, a1, a2), reg)
	s := fxState(t, runtime.PhaseDevelopmentParallel)
	if err := s.CompleteStep("a.root", cd); err != nil {
		t.Fatal(err)
	}
	return cd, s
}

// 用例：SelectIssued 按容量机械裁剪——可签发 N、容量 C 时恰好 min(C,N) 个，
// 顺序固定为 Plan 顺序；actionID 由 TaskKey 确定性派生；非 READY 计划、
// 负容量、nil plan/store 拒绝；落账失败传播；落账内容与签发集一致。
func TestSelectIssuedCapacityAndDeterministicActionIDs(t *testing.T) {
	cd, s := twoAgentFixture(t)
	plan := fxDecide(t, s, decision.Observation{}, cd)
	if plan.Next.Kind != decision.KindReady || len(plan.Next.Ready.Tasks) != 2 {
		t.Fatalf("fixture plan = %+v, want READY with 2 tasks", plan.Next)
	}

	store := &recordingStore{}
	issued, err := decision.SelectIssued(plan, decision.Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 1 || issued[0].ActionID != "act:n1/a.one" || issued[0].Task.String() != "n1/a.one" ||
		string(issued[0].Step) != "a.one" {
		t.Fatalf("capacity 1 issued = %+v, want [a.one with act:n1/a.one]", issued)
	}

	store = &recordingStore{}
	issued, err = decision.SelectIssued(plan, decision.Admission{Capacity: 5}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 2 {
		t.Fatalf("capacity 5 issued = %d, want 2 (min(C,N))", len(issued))
	}
	if issued[0].ActionID != "act:n1/a.one" || issued[1].ActionID != "act:n1/a.two" {
		t.Fatalf("issued order = [%s %s], want plan order [a.one a.two]", issued[0].ActionID, issued[1].ActionID)
	}
	// actionID 确定性：两次签发同一任务得到同一 actionID。
	again, err := decision.SelectIssued(plan, decision.Admission{Capacity: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].ActionID != "act:n1/a.one" {
		t.Fatalf("actionID not deterministic: %s", again[0].ActionID)
	}
	if len(store.sets) != 2 {
		t.Fatalf("store persisted %d sets, want 2 (persist before return)", len(store.sets))
	}

	// 容量 0：空签发集仍落账。
	store = &recordingStore{}
	issued, err = decision.SelectIssued(plan, decision.Admission{Capacity: 0}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued) != 0 || len(store.sets) != 1 || len(store.sets[0]) != 0 {
		t.Fatalf("capacity 0 must issue and persist an empty set, got %+v / %+v", issued, store.sets)
	}

	// 拒绝路径。
	if _, err := decision.SelectIssued(plan, decision.Admission{Capacity: -1}, &recordingStore{}); err == nil {
		t.Error("negative capacity must be rejected")
	}
	if _, err := decision.SelectIssued(nil, decision.Admission{}, &recordingStore{}); err == nil {
		t.Error("nil plan must be rejected")
	}
	if _, err := decision.SelectIssued(plan, decision.Admission{}, nil); err == nil {
		t.Error("nil store must be rejected")
	}
	waitPlan := fxDecide(t, fxState(t, runtime.PhaseDevelopmentParallel), decision.Observation{}, cd)
	if _, err := decision.SelectIssued(waitPlan, decision.Admission{Capacity: 2}, &recordingStore{}); err == nil {
		t.Error("non-READY plan must not be issuable")
	} else {
		wantErrContaining(t, err, "not READY")
	}
	failing := &recordingStore{err: errBoom}
	if _, err := decision.SelectIssued(plan, decision.Admission{Capacity: 1}, failing); err == nil {
		t.Error("persist failure must propagate")
	} else {
		wantErrContaining(t, err, "persist")
	}
}
