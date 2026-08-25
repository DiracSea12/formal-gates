package compiler

import (
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
)

// golden 定义：九步覆盖全部六变体 + 并行组（split → slice/transport → join），
// 跨四个业务节点（entry/fan/ask/review/report），使 ordinal 的节点字典序
// tie-break 有实际作用。图表（A → B 表示 B 依赖 A）：
//
//	entry.parse ──→ entry.persist ──→ ask.decide ──┐
//	    │              └──────────→ review.worker  │
//	    └──→ fan.split ──→ fan.slice ──→ fan.join ─┴→ report.cost
//	                └───→ fan.transport ──↗
//
// 期望 ordinal（Kahn + (nodeID, stepID) 字典序；末轮 "report" < "review"）：
// parse=0, persist=1, ask=2, split=3, slice=4, transport=5, join=6,
// cost=7, review=8。

const (
	goldenVersion = "wf-v1"
	goldenEntry   = "entry"
)

// golden 注册表条目。goldenRegistry 顺序注册；
// TestCompileIrrelevantToAssemblyAndRegistrationOrder 反序注册同一批条目，
// 证明注册顺序不影响编译产物。
var (
	goldenHandlers = []struct {
		id     authoring.HandlerID
		runner authoring.RunnerKind
	}{
		{"engine.entry.parse", authoring.RunnerEngineLocal},
		{"engine.entry.persist", authoring.RunnerDurableActivity},
		{"engine.review.worker", authoring.RunnerAgentWorker},
		{"engine.fan.slice", authoring.RunnerEngineLocal},
		{"engine.fan.transport", authoring.RunnerHostAdapter},
		{"engine.fan.join", authoring.RunnerEngineLocal},
		{"engine.report.cost", authoring.RunnerEngineLocal},
	}
	goldenCodecs     = []authoring.CodecID{"codec.any.in", "codec.any.out"}
	goldenPredicates = []authoring.PredicateID{"pred.review.post"}
	goldenReconciles = []authoring.ReconcileID{"reconcile.entry.persist"}
	goldenSchemas    = []authoring.SchemaID{"schema.ask.decision.request", "schema.ask.decision.response", "schema.host.fan.transport"}
	goldenOperations = []authoring.OperationID{"op.fan.transport"}
	goldenAskKinds   = []authoring.AskKindID{"decision"}
)

// goldenRegistry 注册 golden 定义引用的全部 ID。skip 列出故意不注册的 ID
// （MISSING_ENGINE_ADAPTER 用例）。
func goldenRegistry(t *testing.T, skip ...string) *Registry {
	t.Helper()
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	reg := NewRegistry()
	for _, h := range goldenHandlers {
		if skipSet[string(h.id)] {
			continue
		}
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler %q: %v", h.id, err)
		}
	}
	for _, id := range goldenCodecs {
		if !skipSet[string(id)] {
			if err := reg.RegisterCodec(id); err != nil {
				t.Fatalf("register codec %q: %v", id, err)
			}
		}
	}
	for _, id := range goldenPredicates {
		if !skipSet[string(id)] {
			if err := reg.RegisterPredicate(id); err != nil {
				t.Fatalf("register predicate %q: %v", id, err)
			}
		}
	}
	for _, id := range goldenReconciles {
		if !skipSet[string(id)] {
			if err := reg.RegisterReconciler(id); err != nil {
				t.Fatalf("register reconciler %q: %v", id, err)
			}
		}
	}
	for _, id := range goldenSchemas {
		if !skipSet[string(id)] {
			if err := reg.RegisterSchema(id); err != nil {
				t.Fatalf("register schema %q: %v", id, err)
			}
		}
	}
	for _, id := range goldenOperations {
		if !skipSet[string(id)] {
			if err := reg.RegisterOperation(id); err != nil {
				t.Fatalf("register operation %q: %v", id, err)
			}
		}
	}
	for _, id := range goldenAskKinds {
		if !skipSet[string(id)] {
			if err := reg.RegisterAskKind(id); err != nil {
				t.Fatalf("register ask kind %q: %v", id, err)
			}
		}
	}
	return reg
}

// header 构造合法公共头（版本恒为 goldenVersion）。
func header(id, node string, deps ...string) authoring.Header {
	ds := make([]authoring.StepID, 0, len(deps))
	for _, d := range deps {
		ds = append(ds, authoring.StepID(d))
	}
	return authoring.Header{
		ID:                authoring.StepID(id),
		NodeID:            authoring.NodeID(node),
		Dependencies:      ds,
		DefinitionVersion: goldenVersion,
	}
}

// ioWith 构造合法共享 IO 段：每个来源步骤恰好一个 typed input binding
// （inputs 集合 == dependencies 集合是 compiler 强制的不变量）。
func ioWith(bindings ...string) authoring.IO {
	inputs := make([]authoring.InputBinding, 0, len(bindings))
	for _, b := range bindings {
		inputs = append(inputs, authoring.InputBinding{
			From: authoring.StepID(b), OutputField: "out", ToField: "in",
		})
	}
	return authoring.IO{InputCodec: "codec.any.in", OutputCodec: "codec.any.out", Inputs: inputs}
}

// goldenSteps 经 authoring constructor 构造的九步表。每次调用产生全新实例，
// 测试可安全增删改。
func goldenSteps() []authoring.Step {
	parse, err := authoring.NewLocalStep(header("entry.parse", "entry"), ioWith(),
		authoring.LocalSpec{Handler: "engine.entry.parse"})
	if err != nil {
		panic(err)
	}
	persist, err := authoring.NewDurableStep(header("entry.persist", "entry", "entry.parse"),
		ioWith("entry.parse"), authoring.DurableSpec{
			Handler: "engine.entry.persist", Idempotency: authoring.IdempotencyDeterministicInput,
			Reconcile: "reconcile.entry.persist", Timeout: 30 * time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 3, Backoff: 2 * time.Second},
		})
	if err != nil {
		panic(err)
	}
	reviewIO := ioWith("entry.persist")
	reviewIO.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
	review, err := authoring.NewAgentStep(header("review.worker", "review", "entry.persist"),
		reviewIO, authoring.AgentSpec{
			Handler: "engine.review.worker", Reason: authoring.ReasonIndependentReview, Timeout: time.Minute,
		})
	if err != nil {
		panic(err)
	}
	ask, err := authoring.NewHumanAskStep(header("ask.decide", "ask", "entry.persist"),
		authoring.HumanAskSpec{
			AskKind: "decision", RequestSchema: "schema.ask.decision.request",
			ResponseSchema: "schema.ask.decision.response", FreshnessTTL: 15 * time.Minute,
		})
	if err != nil {
		panic(err)
	}
	split, err := authoring.NewParallelStep(header("fan.split", "fan", "entry.parse"),
		authoring.ParallelSpec{
			Children: []authoring.StepID{"fan.transport", "fan.slice"},
			Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	if err != nil {
		panic(err)
	}
	slice, err := authoring.NewLocalStep(header("fan.slice", "fan", "fan.split"),
		ioWith("fan.split"), authoring.LocalSpec{Handler: "engine.fan.slice"})
	if err != nil {
		panic(err)
	}
	transport, err := authoring.NewHostActionStep(header("fan.transport", "fan", "fan.split"),
		ioWith("fan.split"), authoring.HostActionSpec{
			Handler: "engine.fan.transport", Boundary: authoring.BoundaryAgentDispatchAPI,
			Operation: "op.fan.transport", Schema: "schema.host.fan.transport", Timeout: 10 * time.Second,
		})
	if err != nil {
		panic(err)
	}
	join, err := authoring.NewLocalStep(header("fan.join", "fan", "fan.slice", "fan.transport"),
		ioWith("fan.slice", "fan.transport"), authoring.LocalSpec{Handler: "engine.fan.join"})
	if err != nil {
		panic(err)
	}
	cost, err := authoring.NewLocalStep(header("report.cost", "report", "fan.join", "ask.decide"),
		ioWith("fan.join", "ask.decide"), authoring.LocalSpec{
			Handler: "engine.report.cost", Retry: &authoring.RetryPolicy{MaxAttempts: 2},
		})
	if err != nil {
		panic(err)
	}
	return []authoring.Step{parse, persist, review, ask, split, slice, transport, join, cost}
}

func goldenDefinition() *Definition {
	return &Definition{Version: goldenVersion, EntryNode: goldenEntry, Steps: goldenSteps()}
}

// TestCompileGolden 固化 golden 定义的完整编译结果：ordinal、稳定排序、
// authority/runner/kind 物化、版本绑定、payload 归一化、human/parallel
// 零 IO 段。
func TestCompileGolden(t *testing.T) {
	cd, err := Compile(goldenDefinition(), goldenRegistry(t))
	if err != nil {
		t.Fatalf("golden compile: %v", err)
	}
	if cd.MissingEngineAdapter {
		t.Fatal("fully registered golden must not carry MISSING_ENGINE_ADAPTER marker")
	}
	if cd.Version != goldenVersion || cd.EntryNode != goldenEntry {
		t.Fatalf("envelope not materialized: %+v", cd)
	}

	wantOrd := map[string]int{
		"entry.parse": 0, "entry.persist": 1, "ask.decide": 2, "fan.split": 3,
		"fan.slice": 4, "fan.transport": 5, "fan.join": 6, "report.cost": 7, "review.worker": 8,
	}
	wantKind := map[string]struct {
		kind StepKind
		auth authoring.DecisionAuthority
		run  authoring.RunnerKind
	}{
		"entry.parse":   {KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"entry.persist": {KindDurable, authoring.AuthorityEngine, authoring.RunnerDurableActivity},
		"review.worker": {KindAgent, authoring.AuthorityAgent, authoring.RunnerAgentWorker},
		"ask.decide":    {KindHumanAsk, authoring.AuthorityHuman, authoring.RunnerHostAdapter},
		"fan.split":     {KindParallel, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"fan.slice":     {KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"fan.transport": {KindHostAction, authoring.AuthorityEngine, authoring.RunnerHostAdapter},
		"fan.join":      {KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"report.cost":   {KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
	}
	// 步骤位置期望：按 (nodeID, ordinal, id) 排序——ask < entry < fan < report < review。
	wantOrder := []string{
		"ask.decide", "entry.parse", "entry.persist", "fan.split",
		"fan.slice", "fan.transport", "fan.join", "report.cost", "review.worker",
	}
	if len(cd.Steps) != len(wantOrd) {
		t.Fatalf("step count = %d, want %d", len(cd.Steps), len(wantOrd))
	}
	for i, cs := range cd.Steps {
		id := string(cs.Header.ID)
		w := wantKind[id]
		if cs.Header.Ordinal != wantOrd[id] {
			t.Errorf("step %q ordinal = %d, want %d", id, cs.Header.Ordinal, wantOrd[id])
		}
		if cs.Header.Kind != w.kind || cs.Header.Authority != w.auth || cs.Header.Runner != w.run {
			t.Errorf("step %q kind/auth/runner = %s/%s/%s, want %s/%s/%s",
				id, cs.Header.Kind, cs.Header.Authority, cs.Header.Runner, w.kind, w.auth, w.run)
		}
		if cs.Header.DefinitionVersion != goldenVersion {
			t.Errorf("step %q definitionVersion = %q, want %q", id, cs.Header.DefinitionVersion, goldenVersion)
		}
		if id != wantOrder[i] {
			t.Errorf("steps[%d] = %q, want %q (stable (nodeID, ordinal, id) sort)", i, id, wantOrder[i])
		}
	}

	// payload 物化 spot check。
	byID := map[authoring.StepID]CompiledStep{}
	for _, cs := range cd.Steps {
		byID[cs.Header.ID] = cs
	}
	if p, ok := byID["entry.persist"].Payload.(CompiledDurableStep); !ok ||
		p.Idempotency != authoring.IdempotencyDeterministicInput || p.Reconcile != "reconcile.entry.persist" ||
		p.Retry.MaxAttempts != 3 || p.Timeout != 30*time.Second {
		t.Fatalf("durable payload not materialized: %+v", byID["entry.persist"].Payload)
	}
	if p, ok := byID["fan.transport"].Payload.(CompiledHostActionStep); !ok ||
		p.Boundary != authoring.BoundaryAgentDispatchAPI || p.Operation != "op.fan.transport" || p.Schema != "schema.host.fan.transport" {
		t.Fatalf("host action payload not materialized: %+v", byID["fan.transport"].Payload)
	}
	if p, ok := byID["fan.split"].Payload.(CompiledParallelStep); !ok ||
		len(p.Children) != 2 || p.Children[0] != "fan.slice" || p.Children[1] != "fan.transport" ||
		p.Join.JoinStep != "fan.join" || p.Failure.Escalate != authoring.FailureInvariantViolation {
		t.Fatalf("parallel payload not materialized (children sorted): %+v", byID["fan.split"].Payload)
	}
	if p, ok := byID["ask.decide"].Payload.(CompiledHumanAskStep); !ok ||
		p.RequestSchema != "schema.ask.decision.request" || p.ResponseSchema != "schema.ask.decision.response" ||
		p.FreshnessTTL != 15*time.Minute {
		t.Fatalf("human ask payload not materialized: %+v", byID["ask.decide"].Payload)
	}
	// human/parallel 无共享 IO 段；report.cost 的可选 retry 指针物化。
	if io := byID["ask.decide"].IO; io.InputCodec != "" || io.OutputCodec != "" || io.Inputs != nil {
		t.Fatalf("human ask must not carry IO block: %+v", io)
	}
	if io := byID["fan.split"].IO; io.InputCodec != "" || io.OutputCodec != "" || io.Inputs != nil {
		t.Fatalf("parallel must not carry IO block: %+v", io)
	}
	if p, ok := byID["report.cost"].Payload.(CompiledLocalStep); !ok || p.Retry == nil || p.Retry.MaxAttempts != 2 {
		t.Fatalf("local optional retry not materialized: %+v", byID["report.cost"].Payload)
	}
}

// TestCompileIrrelevantToAssemblyAndRegistrationOrder：同一定义不同 assembly
// 顺序、同一 registry 条目不同注册顺序，编译产物完全相同（ordinal 是图性质
// 的函数——assembly 顺序不得泄漏进制品，ADR-001 验收 2 在 IR 层的前置）。
func TestCompileIrrelevantToAssemblyAndRegistrationOrder(t *testing.T) {
	fwd, err := Compile(goldenDefinition(), goldenRegistry(t))
	if err != nil {
		t.Fatalf("forward compile: %v", err)
	}
	revSteps := goldenSteps()
	for i, j := 0, len(revSteps)-1; i < j; i, j = i+1, j-1 {
		revSteps[i], revSteps[j] = revSteps[j], revSteps[i]
	}
	rev, err := Compile(&Definition{Version: goldenVersion, EntryNode: goldenEntry, Steps: revSteps}, goldenRegistry(t))
	if err != nil {
		t.Fatalf("reversed assembly compile: %v", err)
	}
	if !reflect.DeepEqual(fwd, rev) {
		t.Fatal("compiled IR differs between assembly orders")
	}

	// 注册顺序反排：同一批条目逆序注册。
	reg := NewRegistry()
	for _, id := range goldenAskKinds {
		if err := reg.RegisterAskKind(id); err != nil {
			t.Fatalf("register ask kind: %v", err)
		}
	}
	for _, id := range goldenOperations {
		if err := reg.RegisterOperation(id); err != nil {
			t.Fatalf("register operation: %v", err)
		}
	}
	for _, id := range goldenSchemas {
		if err := reg.RegisterSchema(id); err != nil {
			t.Fatalf("register schema: %v", err)
		}
	}
	for _, id := range goldenReconciles {
		if err := reg.RegisterReconciler(id); err != nil {
			t.Fatalf("register reconciler: %v", err)
		}
	}
	for _, id := range goldenPredicates {
		if err := reg.RegisterPredicate(id); err != nil {
			t.Fatalf("register predicate: %v", err)
		}
	}
	for _, id := range goldenCodecs {
		if err := reg.RegisterCodec(id); err != nil {
			t.Fatalf("register codec: %v", err)
		}
	}
	for i := len(goldenHandlers) - 1; i >= 0; i-- {
		h := goldenHandlers[i]
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler %q: %v", h.id, err)
		}
	}
	revReg, err := Compile(goldenDefinition(), reg)
	if err != nil {
		t.Fatalf("reversed registry compile: %v", err)
	}
	if !reflect.DeepEqual(fwd, revReg) {
		t.Fatal("compiled IR differs between registration orders")
	}

	// 同一 ready 轮内的字典序 tie-break 与 assembly 顺序无关：
	// 两个零依赖步骤 s1/s2 无论以何种顺序输入，s1 的 ordinal 恒更小。
	mk := func(order ...string) *Definition {
		var steps []authoring.Step
		for _, id := range order {
			s, err := authoring.NewLocalStep(header(id, goldenEntry), ioWith(),
				authoring.LocalSpec{Handler: "engine.entry.parse"})
			if err != nil {
				panic(err)
			}
			steps = append(steps, s)
		}
		return &Definition{Version: goldenVersion, EntryNode: goldenEntry, Steps: steps}
	}
	a, err := Compile(mk("tie.s1", "tie.s2"), goldenRegistry(t))
	if err != nil {
		t.Fatalf("tie compile: %v", err)
	}
	b, err := Compile(mk("tie.s2", "tie.s1"), goldenRegistry(t))
	if err != nil {
		t.Fatalf("tie compile reversed: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("tie-break ordinals differ between assembly orders")
	}
	if a.Steps[0].Header.ID != "tie.s1" || a.Steps[0].Header.Ordinal != 0 ||
		a.Steps[1].Header.ID != "tie.s2" || a.Steps[1].Header.Ordinal != 1 {
		t.Fatalf("tie-break ordinal wrong: [%d %s, %d %s]",
			a.Steps[0].Header.Ordinal, a.Steps[0].Header.ID, a.Steps[1].Header.Ordinal, a.Steps[1].Header.ID)
	}
}
