package decision

import (
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
)

// testDefinition 构造覆盖全部六变体的测试定义（批 1 定义形态的最小复用）：
//
//	boot.local ──→ work.agent ──→ gate.host ──┐
//	    │────────→ work.ask  ──→ gate.durable ┴→ fin.report
//
// ordinal（确定性 Kahn，逐轮取 (nodeID, stepID) 字典序最小就绪步）：
// boot.local=0, work.agent=1, gate.host=2, work.ask=3, gate.durable=4,
// fin.report=5。
const testDefVersion authoring.DefinitionVersion = "test-1"

func testDefinition(t *testing.T) *compiler.CompiledDefinition {
	t.Helper()
	reg := compiler.NewRegistry()
	for _, h := range []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.test.boot", authoring.RunnerEngineLocal},
		{"engine.test.work", authoring.RunnerAgentWorker},
		{"engine.test.gate", authoring.RunnerHostAdapter},
		{"engine.test.durable", authoring.RunnerDurableActivity},
		{"engine.test.fin", authoring.RunnerEngineLocal},
	} {
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler: %v", err)
		}
	}
	for _, id := range []authoring.CodecID{"codec.test.in", "codec.test.out"} {
		if err := reg.RegisterCodec(id); err != nil {
			t.Fatalf("register codec: %v", err)
		}
	}
	if err := reg.RegisterPredicate("pred.test.work"); err != nil {
		t.Fatalf("register predicate: %v", err)
	}
	if err := reg.RegisterReconciler("reconcile.test.durable"); err != nil {
		t.Fatalf("register reconciler: %v", err)
	}
	for _, id := range []authoring.SchemaID{"schema.test.ask.request", "schema.test.ask.response"} {
		if err := reg.RegisterSchema(id); err != nil {
			t.Fatalf("register schema: %v", err)
		}
	}
	if err := reg.RegisterOperation("op.test.gate"); err != nil {
		t.Fatalf("register operation: %v", err)
	}

	ioWith := func(bindings ...string) authoring.IO {
		inputs := make([]authoring.InputBinding, 0, len(bindings))
		for _, b := range bindings {
			inputs = append(inputs, authoring.InputBinding{From: authoring.StepID(b), OutputField: "out", ToField: "in"})
		}
		return authoring.IO{InputCodec: "codec.test.in", OutputCodec: "codec.test.out", Inputs: inputs}
	}
	mk := func(s authoring.Step, err error) authoring.Step {
		if err != nil {
			t.Fatalf("construct step: %v", err)
		}
		return s
	}
	hdr := func(id, node string, deps ...string) authoring.Header {
		ds := make([]authoring.StepID, 0, len(deps))
		for _, d := range deps {
			ds = append(ds, authoring.StepID(d))
		}
		return authoring.Header{ID: authoring.StepID(id), NodeID: authoring.NodeID(node), Dependencies: ds, DefinitionVersion: testDefVersion}
	}
	agentIO := ioWith("boot.local")
	agentIO.Postconditions = []authoring.PredicateRef{{ID: "pred.test.work"}}

	def := &compiler.Definition{Version: testDefVersion, EntryNode: "boot", Steps: []authoring.Step{
		mk(authoring.NewLocalStep(hdr("boot.local", "boot"), ioWith(), authoring.LocalSpec{Handler: "engine.test.boot"})),
		mk(authoring.NewAgentStep(hdr("work.agent", "work", "boot.local"), agentIO, authoring.AgentSpec{
			Handler: "engine.test.work", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute,
		})),
		mk(authoring.NewHumanAskStep(hdr("work.ask", "work", "boot.local"), authoring.HumanAskSpec{
			AskKind: "route", RequestSchema: "schema.test.ask.request",
			ResponseSchema: "schema.test.ask.response", FreshnessTTL: time.Minute,
		})),
		mk(authoring.NewHostActionStep(hdr("gate.host", "gate", "work.agent"), ioWith("work.agent"), authoring.HostActionSpec{
			Handler: "engine.test.gate", Boundary: authoring.BoundaryExternalCapability,
			Operation: "op.test.gate", Timeout: time.Minute,
		})),
		mk(authoring.NewDurableStep(hdr("gate.durable", "gate", "work.ask"), ioWith("work.ask"), authoring.DurableSpec{
			Handler: "engine.test.durable", Idempotency: authoring.IdempotencyDeterministicInput,
			Reconcile: "reconcile.test.durable", Timeout: time.Minute,
			Retry: authoring.RetryPolicy{MaxAttempts: 1},
		})),
		mk(authoring.NewLocalStep(hdr("fin.report", "fin", "gate.host", "gate.durable"), ioWith("gate.host", "gate.durable"),
			authoring.LocalSpec{Handler: "engine.test.fin"})),
	}}
	cd, err := compiler.Compile(def, reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cd
}

func newTestState(t *testing.T) *State {
	t.Helper()
	s, err := NewState(testDefVersion, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	return s
}

func complete(t *testing.T, s *State, cd *compiler.CompiledDefinition, ids ...authoring.StepID) {
	t.Helper()
	for _, id := range ids {
		if err := s.CompleteStep(id, cd); err != nil {
			t.Fatalf("complete %s: %v", id, err)
		}
	}
}

func frontierSteps(p *Plan) []string {
	out := make([]string, 0, len(p.Frontier))
	for _, e := range p.Frontier {
		out = append(out, string(e.Step))
	}
	return out
}

// TestDecideKindScenarios 核对 Kind 判定的固定优先级与各形态的产出：
// 内部步骤→Wait(ENGINE_INTERNAL)、agent→Ready、在途排除→Ask、
// 全在途→Wait(TASKS_IN_FLIGHT)、host→HostAction、完成→Complete。
func TestDecideKindScenarios(t *testing.T) {
	cd := testDefinition(t)

	// 初始：frontier 只有 engine-internal 步骤。
	s := newTestState(t)
	p, err := Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindWait || p.Next.Wait.Reason != WaitEngineInternal {
		t.Fatalf("initial kind = %s/%v, want WAIT/ENGINE_INTERNAL", p.Next.Kind, p.Next.Wait)
	}

	// boot.local 完成后：frontier = [work.agent, work.ask]（ordinal 序），
	// agent 可签发 → Ready。
	complete(t, s, cd, "boot.local")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if got, want := strings.Join(frontierSteps(p), ","), "work.agent,work.ask"; got != want {
		t.Fatalf("frontier = %q, want %q", got, want)
	}
	if p.Next.Kind != KindReady {
		t.Fatalf("kind = %s, want READY", p.Next.Kind)
	}
	if len(p.Next.Ready.Tasks) != 1 || p.Next.Ready.Tasks[0].Step != "work.agent" {
		t.Fatalf("ready tasks = %v, want [work.agent]", p.Next.Ready.Tasks)
	}
	// frontier 完整性：Ready 只携可签发子集，Plan.Frontier 仍含全部 eligible。
	if len(p.Frontier) != 2 {
		t.Fatalf("plan frontier = %v, want 2 entries", frontierSteps(p))
	}

	// work.agent 已签发在途：可签发外部步骤只剩 ask → Ask。
	agentKey := runtime.TaskKey{Node: "work", Step: "work.agent"}
	if err := s.TransitionTask(agentKey, runtime.TaskIssued); err != nil {
		t.Fatalf("transition task: %v", err)
	}
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindAsk || len(p.Next.Ask.Steps) != 1 || p.Next.Ask.Steps[0] != "work.ask" {
		t.Fatalf("kind = %s (%v), want ASK([work.ask])", p.Next.Kind, p.Next.Ask)
	}

	// ask 也在途：无任何可签发外部步骤 → Wait(TASKS_IN_FLIGHT)。
	askKey := runtime.TaskKey{Node: "work", Step: "work.ask"}
	if err := s.TransitionTask(askKey, runtime.TaskIssued); err != nil {
		t.Fatalf("transition task: %v", err)
	}
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindWait || p.Next.Wait.Reason != WaitTasksInFlight {
		t.Fatalf("kind = %s/%v, want WAIT/TASKS_IN_FLIGHT", p.Next.Kind, p.Next.Wait)
	}

	// work.agent 完成：frontier = [gate.host] → HostAction。
	complete(t, s, cd, "work.agent")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindHostAction || len(p.Next.HostAction.Steps) != 1 || p.Next.HostAction.Steps[0] != "gate.host" {
		t.Fatalf("kind = %s (%v), want HOST_ACTION([gate.host])", p.Next.Kind, p.Next.HostAction)
	}

	// work.ask 与 gate.host 完成：frontier = [gate.durable]（internal）→ Wait。
	complete(t, s, cd, "work.ask", "gate.host")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindWait || p.Next.Wait.Reason != WaitEngineInternal {
		t.Fatalf("kind = %s/%v, want WAIT/ENGINE_INTERNAL", p.Next.Kind, p.Next.Wait)
	}

	// 全部完成 → Complete（非终态 phase 也无空结果：Complete 是非空结果）。
	complete(t, s, cd, "gate.durable", "fin.report")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindComplete || p.Next.Complete == nil {
		t.Fatalf("kind = %s, want COMPLETE", p.Next.Kind)
	}
	if len(p.Frontier) != 0 {
		t.Fatalf("complete plan frontier = %v, want empty", frontierSteps(p))
	}

	// 终态 phase：即使尚有未完成步骤（abort 场景）也投影 Complete。
	s2 := newTestState(t)
	if err := s2.TransitionPhase(runtime.PhaseTerminal); err != nil {
		t.Fatalf("transition phase: %v", err)
	}
	p, err = Decide(s2, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindComplete {
		t.Fatalf("terminal phase kind = %s, want COMPLETE", p.Next.Kind)
	}
}

// TestDecideFrontierOrder 核对 frontier 按 compiled definition 的稳定
// ordinal 排序（gate.host ordinal 2 先于 gate.durable 4——ordinal 是逐轮
// 就绪分配，不是节点分组序）。
func TestDecideFrontierOrder(t *testing.T) {
	cd := testDefinition(t)
	s := newTestState(t)
	complete(t, s, cd, "boot.local", "work.agent", "work.ask")
	p, err := Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	got := frontierSteps(p)
	want := []string{"gate.host", "gate.durable"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("frontier = %v, want %v (ordinal order)", got, want)
	}
	if p.Frontier[0].Ordinal != 2 || p.Frontier[1].Ordinal != 4 {
		t.Fatalf("ordinals = %d,%d, want 2,4", p.Frontier[0].Ordinal, p.Frontier[1].Ordinal)
	}
}

// TestDecideByteStability 核对 canonical Plan 字节稳定：同输入两次编码
// 相同；observation 或 state 变化必须改变字节与 PlanDigest。
func TestDecideByteStability(t *testing.T) {
	cd := testDefinition(t)
	s := newTestState(t)
	complete(t, s, cd, "boot.local")
	obs := Observation{Facts: []Fact{{Source: SourceVCS, Key: "head", Value: "abc"}}}

	p1, err := Decide(s, obs, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	p2, err := Decide(s, obs, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	b1, err1 := p1.CanonicalBytes()
	d1, _ := p1.Digest()
	b2, err2 := p2.CanonicalBytes()
	d2, _ := p2.Digest()
	if err1 != nil || err2 != nil {
		t.Fatalf("canonical bytes: %v / %v", err1, err2)
	}
	if string(b1) != string(b2) || d1 != d2 {
		t.Fatal("same inputs must produce byte-identical plans")
	}

	// 不同 observation → 不同 ObservationDigest → 不同 PlanDigest。
	obs2 := Observation{Facts: []Fact{{Source: SourceVCS, Key: "head", Value: "def"}}}
	p3, err := Decide(s, obs2, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	d3, _ := p3.Digest()
	if d3 == d1 {
		t.Fatal("different observation must change the plan digest")
	}
	if p3.ObservationDigest == p1.ObservationDigest {
		t.Fatal("observation digest must track observation content")
	}

	// 不同 state → 不同 StateDigest → 不同 PlanDigest。
	complete(t, s, cd, "work.agent")
	p4, err := Decide(s, obs, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	d4, _ := p4.Digest()
	if d4 == d1 || p4.StateDigest == p1.StateDigest {
		t.Fatal("state change must change the plan digest")
	}
}

// TestDecideRejectsInconsistentState 核对纯函数入口对不一致 state 的拒绝：
// 版本不符、未知/重复/乱序（依赖未完成）完成。
func TestDecideRejectsInconsistentState(t *testing.T) {
	cd := testDefinition(t)
	mk := func(completed ...authoring.StepID) *State {
		s := newTestState(t)
		s.Completed = completed
		return s
	}
	for name, tc := range map[string]struct {
		s    *State
		want string
	}{
		"version mismatch":  {&State{DefinitionVersion: "other", Phase: runtime.PhasePostReview}, "version"},
		"unknown step":      {mk("no.such.step"), "not in definition"},
		"duplicated step":   {mk("boot.local", "boot.local"), "duplicated"},
		"out-of-order step": {mk("work.agent"), "out-of-order"},
	} {
		_, err := Decide(tc.s, Observation{}, cd)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, tc.want)
		}
	}
	if _, err := Decide(nil, Observation{}, cd); err == nil {
		t.Error("nil state must be rejected")
	}
	if _, err := Decide(newTestState(t), Observation{}, nil); err == nil {
		t.Error("nil definition must be rejected")
	}
}

// TestDecideRejectsDiagnosticDefinition 核对携带 MISSING_ENGINE_ADAPTER
// marker 的 diagnostic-only 定义不得进入 executable plan。
func TestDecideRejectsDiagnosticDefinition(t *testing.T) {
	reg := compiler.NewRegistry()
	def := &compiler.Definition{Version: testDefVersion, EntryNode: "boot"}
	// 空 registry + 任意合法定义 → 全部 ID 记为诊断，marker 置位。
	full := testDefinition(t)
	def.Steps = stepCopies(t, full)
	res, err := compiler.CompileDiagnostic(def, reg)
	if err != nil {
		t.Fatalf("diagnostic compile: %v", err)
	}
	if !res.Definition.MissingEngineAdapter {
		t.Fatal("diagnostic compile must carry the marker")
	}
	s := newTestState(t)
	if _, err := Decide(s, Observation{}, res.Definition); err == nil {
		t.Fatal("marker definition must be rejected by Decide")
	}
}

// stepCopies 从已编译定义还原 authoring 步骤表（复用其构造合法性），
// 用于以空 registry 重编译出 diagnostic marker。
func stepCopies(t *testing.T, cd *compiler.CompiledDefinition) []authoring.Step {
	t.Helper()
	out := make([]authoring.Step, 0, len(cd.Steps))
	for i := range cd.Steps {
		h := cd.Steps[i].Header
		hdr := authoring.Header{ID: h.ID, NodeID: h.NodeID, Dependencies: h.Dependencies, DefinitionVersion: h.DefinitionVersion}
		io := cd.Steps[i].IO
		var (
			s   authoring.Step
			err error
		)
		switch p := cd.Steps[i].Payload.(type) {
		case compiler.CompiledLocalStep:
			s, err = authoring.NewLocalStep(hdr, io, authoring.LocalSpec{Handler: p.Handler, Timeout: p.Timeout, Retry: p.Retry})
		case compiler.CompiledDurableStep:
			s, err = authoring.NewDurableStep(hdr, io, authoring.DurableSpec{
				Handler: p.Handler, Idempotency: p.Idempotency, Reconcile: p.Reconcile,
				Timeout: p.Timeout, Retry: p.Retry,
			})
		case compiler.CompiledHostActionStep:
			s, err = authoring.NewHostActionStep(hdr, io, authoring.HostActionSpec{
				Handler: p.Handler, Boundary: p.Boundary, Operation: p.Operation, Timeout: p.Timeout,
			})
		case compiler.CompiledAgentStep:
			s, err = authoring.NewAgentStep(hdr, io, authoring.AgentSpec{
				Handler: p.Handler, Reason: p.Reason, Timeout: p.Timeout, Retry: p.Retry,
			})
		case compiler.CompiledHumanAskStep:
			s, err = authoring.NewHumanAskStep(hdr, authoring.HumanAskSpec{
				AskKind: p.AskKind, RequestSchema: p.RequestSchema,
				ResponseSchema: p.ResponseSchema, FreshnessTTL: p.FreshnessTTL,
			})
		case compiler.CompiledParallelStep:
			s, err = authoring.NewParallelStep(hdr, authoring.ParallelSpec{
				Children: p.Children, Join: p.Join, Failure: p.Failure,
			})
		default:
			t.Fatalf("unknown payload %T", p)
		}
		if err != nil {
			t.Fatalf("reconstruct step %s: %v", h.ID, err)
		}
		out = append(out, s)
	}
	return out
}

// TestDecideRealWorkflowDefinition 在批 1c 的真实定义上冒烟：初始只剩
// engine-internal frontier；entry 两步完成后 Ready 携 review.worker。
func TestDecideRealWorkflowDefinition(t *testing.T) {
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatalf("compile real definition: %v", err)
	}
	s, err := NewState(cd.Version, runtime.PhaseDevelopmentParallel)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	p, err := Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindWait || p.Next.Wait.Reason != WaitEngineInternal {
		t.Fatalf("initial kind = %s/%v, want WAIT/ENGINE_INTERNAL", p.Next.Kind, p.Next.Wait)
	}
	complete(t, s, cd, "entry.parse", "entry.persist")
	p, err = Decide(s, Observation{}, cd)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if p.Next.Kind != KindReady || len(p.Next.Ready.Tasks) != 1 || p.Next.Ready.Tasks[0].Step != "review.worker" {
		t.Fatalf("kind = %s (%v), want READY([review.worker])", p.Next.Kind, p.Next.Ready)
	}
	// frontier 完整且按 ordinal：ask.decide(2) < fan.split(3) < review.worker(7)。
	if got, want := strings.Join(frontierSteps(p), ","), "ask.decide,fan.split,review.worker"; got != want {
		t.Fatalf("frontier = %q, want %q", got, want)
	}
}
