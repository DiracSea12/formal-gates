package whitebox_qa

import (
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

// 用例：compile 管线拒绝信封与身份层缺陷——nil 定义/registry、空版本、空
// 入口节点、零步骤、nil 步骤元素、重复步骤 ID、步骤版本未与信封绑定、
// 空 ID/节点（绕过 constructor 的零值头部）。
func TestCompileRejectsEnvelopeAndIdentityDefects(t *testing.T) {
	reg := fxRegistry(t)
	rows := []struct {
		name   string
		build  func() *compiler.Definition
		substr string
	}{
		{"nil definition", func() *compiler.Definition { return nil }, "nil definition"},
		{"empty version", func() *compiler.Definition {
			def := fxDefinition(t)
			def.Version = ""
			return def
		}, "definition version required"},
		{"empty entry node", func() *compiler.Definition {
			def := fxDefinition(t)
			def.EntryNode = ""
			return def
		}, "entry node required"},
		{"no steps", func() *compiler.Definition {
			return &compiler.Definition{Version: fxDefVersion, EntryNode: "n0"}
		}, "definition has no steps"},
		{"nil step element", func() *compiler.Definition {
			return &compiler.Definition{Version: fxDefVersion, EntryNode: "n0", Steps: []authoring.Step{nil}}
		}, "nil step"},
		{"duplicate step id", func() *compiler.Definition {
			return fxDefinition(t, fxParse(t), fxParse(t))
		}, "duplicate step id"},
		{"step version unbound", func() *compiler.Definition {
			h := fxHeader("s.parse", "n0")
			h.DefinitionVersion = "9"
			s := mkLocalStep(t, h, fxIO(), authoring.LocalSpec{Handler: "h.s.parse"})
			return fxDefinition(t, s)
		}, "unbound definition version"},
		{"zero-value step id", func() *compiler.Definition {
			s := authoring.LocalStep{Header: fxHeader("", "n0"), IO: fxIO(), Handler: "h.s.parse"}
			return fxDefinition(t, s)
		}, "id required"},
		{"zero-value step node", func() *compiler.Definition {
			s := authoring.LocalStep{Header: fxHeader("s.parse", ""), IO: fxIO(), Handler: "h.s.parse"}
			return fxDefinition(t, s)
		}, "node id required"},
	}
	for _, row := range rows {
		_, err := compiler.Compile(row.build(), reg)
		if err == nil {
			t.Errorf("%s: expected rejection, got nil", row.name)
			continue
		}
		wantCompileErr(t, err, authoring.FailureInvariantViolation, row.substr)
	}
	_, err := compiler.Compile(fxDefinition(t), nil)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "nil registry")
}

// 用例：全局图不变量拒绝——依赖不存在（先于定序报 not found）、依赖循环
// （报卡住的步骤集合）、不可达步骤、入口节点无步骤、入口节点无依赖自由步。
func TestCompileRejectsReferenceCycleAndReachabilityDefects(t *testing.T) {
	reg := fxRegistry(t)
	rows := []struct {
		name   string
		build  func(t *testing.T) *compiler.Definition
		substr string
	}{
		{"dependency not found", func(t *testing.T) *compiler.Definition {
			join := mkLocalStep(t,
				fxHeader("s.join", "n2", "s.left", "s.right", "s.ghost"),
				fxIO("s.left", "s.right", "s.ghost"), authoring.LocalSpec{Handler: "h.s.join"})
			steps := fxAllSteps(t)
			return fxDefinition(t, append(withoutStep(steps, "s.join"), join)...)
		}, "not found"},
		{"dependency cycle", func(t *testing.T) *compiler.Definition {
			root := mkLocalStep(t, fxHeader("s.parse", "n0"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse"})
			a := mkLocalStep(t, fxHeader("s.a", "n1", "s.b"), fxIO("s.b"),
				authoring.LocalSpec{Handler: "h.s.left"})
			b := mkLocalStep(t, fxHeader("s.b", "n1", "s.a"), fxIO("s.a"),
				authoring.LocalSpec{Handler: "h.s.right"})
			return fxDefinition(t, root, a, b)
		}, "dependency cycle"},
		{"unreachable orphan step", func(t *testing.T) *compiler.Definition {
			orphan := mkLocalStep(t, fxHeader("s.orphan", "n9"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.cost"})
			return fxDefinition(t, append(fxAllSteps(t), orphan)...)
		}, "unreachable steps"},
		{"entry node has no steps", func(t *testing.T) *compiler.Definition {
			alone := mkLocalStep(t, fxHeader("s.parse", "n1"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse"})
			return fxDefinition(t, alone)
		}, "has no steps"},
		{"entry node has no dependency-free step", func(t *testing.T) *compiler.Definition {
			root := mkLocalStep(t, fxHeader("s.r", "n9"), fxIO(),
				authoring.LocalSpec{Handler: "h.s.parse"})
			a := mkLocalStep(t, fxHeader("s.a", "n0", "s.r"), fxIO("s.r"),
				authoring.LocalSpec{Handler: "h.s.left"})
			return fxDefinition(t, root, a)
		}, "no dependency-free step"},
	}
	for _, row := range rows {
		_, err := compiler.Compile(row.build(t), reg)
		if err == nil {
			t.Errorf("%s: expected rejection, got nil", row.name)
			continue
		}
		wantCompileErr(t, err, authoring.FailureInvariantViolation, row.substr)
	}
}

// 用例：并行组图不变量——fan-out 锚点依赖必填、join 依赖集合与 children
// 精确相等（缺成员/多拉外部都拒）、成员只允许被 join 消费、join 不得是
// children 成员（graph 层对绕过 constructor 的结构复核）。
func TestCompileEnforcesParallelGroupInvariants(t *testing.T) {
	reg := fxRegistry(t)

	// fan-out 锚点依赖必填：fan 位于入口节点且无依赖（绕开不可达错误先行命中）。
	fanNoAnchor := mkParallelStep(t, fxHeader("s.fan", "n0"),
		authoring.ParallelSpec{
			Children: []authoring.StepID{"s.left", "s.right"},
			Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	_, err := compiler.Compile(fxDefinition(t, fxParse(t), fanNoAnchor, fxLeft(t), fxRight(t), fxJoin(t)), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "fan-out anchor dependency required")

	// join 只依赖部分 children（缺 s.right 覆盖）。
	joinPartial := mkLocalStep(t, fxHeader("s.join", "n2", "s.left"),
		fxIO("s.left"), authoring.LocalSpec{Handler: "h.s.join"})
	_, err = compiler.Compile(fxDefinition(t, fxParse(t), fxFan(t), fxLeft(t), fxRight(t), joinPartial), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "does not depend on child")

	// join 多拉外部步骤 s.parse（不在 children 中）。
	joinExtra := mkLocalStep(t,
		fxHeader("s.join", "n2", "s.left", "s.right", "s.parse"),
		fxIO("s.left", "s.right", "s.parse"), authoring.LocalSpec{Handler: "h.s.join"})
	_, err = compiler.Compile(fxDefinition(t, fxParse(t), fxFan(t), fxLeft(t), fxRight(t), joinExtra), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "outside children")

	// 组内成员被 join 之外的步骤消费（s.cost 同时依赖 s.join 与 s.left）。
	costOutsider := mkLocalStep(t,
		fxHeader("s.cost", "n3", "s.join", "s.left"), fxIO("s.join", "s.left"),
		authoring.LocalSpec{Handler: "h.s.cost"})
	steps := fxAllSteps(t)
	steps = withoutStep(steps, "s.cost")
	_, err = compiler.Compile(fxDefinition(t, append(steps, costOutsider)...), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "other than join")

	// join 同时是 children 成员：绕过 constructor 的直接结构体构造（graph 层复核）。
	joinAsChild := authoring.ParallelStep{
		Header:   fxHeader("s.fan", "n0", "s.parse"),
		Children: []authoring.StepID{"s.left", "s.right", "s.join"},
		Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
		Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
	}
	_, err = compiler.Compile(fxDefinition(t, fxParse(t), joinAsChild, fxLeft(t), fxRight(t), fxJoin(t)), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "must not be a child")
}

// 用例：ordinal 由确定性 Kahn 拓扑序派生、产物按 (nodeID, ordinal, id)
// 稳定排序；任意 assembly 顺序编译产生逐字节相同的 canonical 制品与 digest
// （ADR-001 验收 2：assembly/注册顺序不变）。
func TestCompileDerivesDeterministicOrdinalsIndependentOfAssemblyOrder(t *testing.T) {
	reg := fxRegistry(t)
	forward := fxAllSteps(t)
	reversed := make([]authoring.Step, 0, len(forward))
	for i := len(forward) - 1; i >= 0; i-- {
		reversed = append(reversed, forward[i])
	}
	shuffled := []authoring.Step{forward[9], forward[0], forward[5], forward[3], forward[6],
		forward[4], forward[8], forward[2], forward[7], forward[1]}
	orders := [][]authoring.Step{forward, reversed, shuffled}

	var wantBytes []byte
	for i, steps := range orders {
		cd := fxCompile(t, fxDefinition(t, steps...), reg)
		if got := len(cd.Steps); got != len(fxOrderedIDs) {
			t.Fatalf("order %d: compiled steps = %d, want %d", i, got, len(fxOrderedIDs))
		}
		for j, cs := range cd.Steps {
			if want := fxOrderedIDs[j]; string(cs.Header.ID) != want {
				t.Fatalf("order %d: steps[%d] = %q, want %q (output must be (nodeID, ordinal, id) sorted)", i, j, cs.Header.ID, want)
			}
			if want := fxOrdinals[fxOrderedIDs[j]]; cs.Header.Ordinal != want {
				t.Errorf("order %d: step %q ordinal = %d, want %d", i, cs.Header.ID, cs.Header.Ordinal, want)
			}
			if cs.Header.DefinitionVersion != fxDefVersion {
				t.Errorf("step %q definitionVersion not bound to envelope", cs.Header.ID)
			}
		}
		data, err := encoder.Encode(cd)
		if err != nil {
			t.Fatalf("order %d: encode: %v", i, err)
		}
		if wantBytes == nil {
			wantBytes = data
		} else if string(data) != string(wantBytes) {
			t.Fatalf("order %d: artifact bytes differ from first assembly order", i)
		}
	}
}

// 用例：compiled IR 二次防线——绕过 constructor 的零值 payload 步骤与
// 指针形态变体（不匹配任何值类型 case）在 compiler 层仍被拒绝。
func TestCompileSecondLineRejectsConstructorBypassSteps(t *testing.T) {
	reg := fxRegistry(t)
	rows := []struct {
		name   string
		step   authoring.Step
		substr string
	}{
		{"local without handler", authoring.LocalStep{
			Header: fxHeader("s.z", "n0"), IO: fxIO(),
		}, "handler id required"},
		{"durable without idempotency", authoring.DurableStep{
			Header: fxHeader("s.z", "n0"), IO: fxIO(), Handler: "h.s.persist",
			Reconcile: "r.persist", Timeout: time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 1},
		}, "idempotency key strategy required"},
		{"durable without reconcile", authoring.DurableStep{
			Header: fxHeader("s.z", "n0"), IO: fxIO(), Handler: "h.s.persist",
			Idempotency: authoring.IdempotencyDeterministicInput, Timeout: time.Second,
			Retry: authoring.RetryPolicy{MaxAttempts: 1},
		}, "reconcile id required"},
		{"host action without boundary", authoring.HostActionStep{
			Header: fxHeader("s.z", "n0"), IO: fxIO(), Handler: "h.s.dispatch",
			Operation: "op.x", Timeout: time.Second,
		}, "hostBoundaryReason required"},
		{"agent without reason", authoring.AgentStep{
			Header: fxHeader("s.z", "n0"), IO: fxIO(), Handler: "h.s.review",
			Timeout: time.Second,
		}, "nonProgrammableReason required"},
		{"human ask without schemas", authoring.HumanAskStep{
			Header: fxHeader("s.z", "n0"),
		}, "ask kind required"},
		{"parallel without children", authoring.ParallelStep{
			// join 合法、锚点合法（deps 指向入口根 join），children 空：
			// graph 校验放行后由 IR 二次防线拒绝。
			Header: fxHeader("s.fan", "n1", "s.join"),
			Join:   authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
		}, "at least 2 children required"},
	}
	joinRoot := authoring.LocalStep{Header: fxHeader("s.join", "n0"), IO: fxIO(), Handler: "h.s.join"}
	for _, row := range rows {
		_, err := compiler.Compile(fxDefinition(t, row.step, joinRoot), reg)
		if err == nil {
			t.Errorf("%s: expected second-line rejection, got nil", row.name)
			continue
		}
		wantCompileErr(t, err, authoring.FailureInvariantViolation, row.substr)
	}

	// 指针形态变体不匹配值类型 case：未知变体拒绝。
	_, err := compiler.Compile(fxDefinition(t, &[]authoring.LocalStep{fxParse(t)}[0]), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "unknown step variant")
}

// 用例：可执行变体的 typed source bindings 集合与依赖集合精确相等——
// binding 指向非依赖、依赖缺 binding 均拒绝（删除依赖必被拒的机械不变量）。
func TestCompileRequiresInputsEqualToDependencies(t *testing.T) {
	reg := fxRegistry(t)
	root := fxParse(t)

	// 依赖存在但缺 binding。
	noBinding := mkLocalStep(t, fxHeader("s.x", "n0", "s.parse"), fxIO(),
		authoring.LocalSpec{Handler: "h.s.left"})
	_, err := compiler.Compile(fxDefinition(t, root, noBinding), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation,
		"dependency \"s.parse\" has no typed input binding")

	// binding 指向非依赖步骤。
	orphanBinding := fxIO("s.parse")
	orphan := mkLocalStep(t, fxHeader("s.x", "n0"), orphanBinding,
		authoring.LocalSpec{Handler: "h.s.left"})
	_, err = compiler.Compile(fxDefinition(t, root, orphan), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation,
		"input binding source \"s.parse\" is not a dependency")

	// 集合精确相等的合法夹具（human 依赖不要求 IO binding）应编译通过。
	_, err = compiler.Compile(fxDefinition(t), reg)
	if err != nil {
		t.Fatalf("fixture with human deps but no IO bindings should compile: %v", err)
	}
}

// 用例：registry ID 未注册的正常 compile 路由 BLOCKED_BUG 并拒绝签发
// （MISSING_ENGINE_ADAPTER 不是可执行定义的合法状态）；diagnostic 模式产出
// marker 定义与逐条诊断（step/ref/want），不再失败。
func TestCompileRoutesUnregisteredIDsPerMode(t *testing.T) {
	def := fxDefinition(t)

	// 正常模式：六类槽位各抽一个未注册 ID，均 BLOCKED_BUG + MISSING_ENGINE_ADAPTER。
	for _, skip := range []string{"h.s.review", "c.in", "pred.done", "r.persist", "s.req", "op.x"} {
		_, err := compiler.Compile(def, fxRegistryWithout(t, skip))
		if err == nil {
			t.Fatalf("unregistered %q: expected BLOCKED_BUG rejection, got nil", skip)
		}
		wantCompileErr(t, err, authoring.FailureBlockedBug, "MISSING_ENGINE_ADAPTER", "not registered")
	}

	// 完整 registry：正常签发且无 marker。
	cd := fxCompile(t, def, fxRegistry(t))
	if cd.MissingEngineAdapter {
		t.Fatal("normal compile must not emit MISSING_ENGINE_ADAPTER marker")
	}

	// diagnostic 模式：marker + 诊断（step/ref/want）。
	res, err := compiler.CompileDiagnostic(def, fxRegistryWithout(t, "h.s.review"))
	if err != nil {
		t.Fatalf("diagnostic compile: %v", err)
	}
	if !res.Definition.MissingEngineAdapter {
		t.Fatal("diagnostic result with unregistered ids must carry the marker")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %d, want 1", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if string(d.Step) != "s.review" || d.Ref != "h.s.review" || d.Want != compiler.KindHandler {
		t.Fatalf("diagnostic = {step %q ref %q want %s}, want {s.review h.s.review handler}", d.Step, d.Ref, d.Want)
	}

	// 多个未注册 ID：全部记入诊断。
	res, err = compiler.CompileDiagnostic(def, fxRegistryWithout(t, "h.s.review", "c.in"))
	if err != nil {
		t.Fatalf("diagnostic compile (multi): %v", err)
	}
	if len(res.Diagnostics) < 2 {
		t.Fatalf("diagnostics = %d, want >= 2 (handler + codec)", len(res.Diagnostics))
	}
	wantRefs := map[string]bool{"h.s.review": false, "c.in": false}
	for _, d := range res.Diagnostics {
		if _, ok := wantRefs[d.Ref]; ok {
			wantRefs[d.Ref] = true
		}
	}
	for ref, seen := range wantRefs {
		if !seen {
			t.Errorf("diagnostics missing ref %q", ref)
		}
	}
}

// 用例：ID 存在但错用槽位（kind 不匹配）与 handler runner 错绑是定义错误，
// 正常与 diagnostic 两种模式都硬拒绝（INVARIANT_VIOLATION）——锁步激活的
// 编译期落实。
func TestCompileRejectsKindAndRunnerMismatchesInBothModes(t *testing.T) {
	// kind 不匹配：把 handler ID 填进 postcondition predicate 槽。
	io := fxIO("s.parse")
	io.Postconditions = []authoring.PredicateRef{{ID: "h.s.left"}}
	misuse := mkAgentStep(t, fxHeader("s.review", "n1", "s.parse"), io,
		authoring.AgentSpec{Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute})
	def := fxDefinition(t, withStep(fxAllSteps(t), "s.review", misuse)...)

	_, err := compiler.Compile(def, fxRegistry(t))
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "registered as handler, want predicate")
	_, err = compiler.CompileDiagnostic(def, fxRegistry(t))
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "registered as handler, want predicate")

	// runner 错绑：HOST_ACTION 变体绑到 ENGINE_LOCAL runner 的 handler。
	reg := fxRegistry(t)
	if err := reg.RegisterHandler("h.wrong.runner", authoring.RunnerEngineLocal); err != nil {
		t.Fatal(err)
	}
	bound := mkHostActionStep(t, fxHeader("s.dispatch", "n1", "s.parse"), fxIO("s.parse"),
		authoring.HostActionSpec{
			Handler: "h.wrong.runner", Boundary: authoring.BoundaryExternalCapability,
			Operation: "op.x", Timeout: time.Second,
		})
	def2 := fxDefinition(t, withStep(fxAllSteps(t), "s.dispatch", bound)...)
	_, err = compiler.Compile(def2, reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "runner ENGINE_LOCAL != variant runner HOST_ADAPTER")
	_, err = compiler.CompileDiagnostic(def2, reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "runner ENGINE_LOCAL != variant runner HOST_ADAPTER")
}

// 用例：registry 注册期拒绝——空 ID、同 ID 二次注册（同 kind 或跨 kind）、
// handler 非法 runner；解析期对每个 ID 精确解析一次并返回注册的 runner。
func TestRegistryRegistrationAndResolutionRules(t *testing.T) {
	reg := compiler.NewRegistry()
	if err := reg.RegisterHandler("h.x", authoring.RunnerAgentWorker); err != nil {
		t.Fatalf("RegisterHandler: %v", err)
	}
	if r, err := reg.ResolveHandler("h.x"); err != nil || r != authoring.RunnerAgentWorker {
		t.Fatalf("ResolveHandler = (%s, %v), want (AGENT_WORKER, nil)", r, err)
	}
	if err := reg.RegisterHandler("h.x", authoring.RunnerAgentWorker); err == nil {
		t.Error("same-kind duplicate registration must be rejected")
	}
	if err := reg.RegisterPredicate("h.x"); err == nil {
		t.Error("cross-kind duplicate registration must be rejected")
	}
	if err := reg.RegisterCodec(""); err == nil {
		t.Error("empty id registration must be rejected")
	}
	if err := reg.RegisterHandler("h.bad", "CONTROL"); err == nil {
		t.Error("invalid runner kind must be rejected")
	}

	if err := reg.RegisterPredicate("pred.x"); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterCodec("codec.x"); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterReconciler("rec.x"); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterSchema("schema.x"); err != nil {
		t.Fatal(err)
	}
	if err := reg.RegisterOperation("op.x2"); err != nil {
		t.Fatal(err)
	}

	// 未注册：closed world not found。
	if err := reg.ResolvePredicate("pred.nope"); err == nil {
		t.Error("unregistered predicate must not resolve")
	} else {
		wantErrContaining(t, err, "not found")
	}
	// 槽位错用：立即暴露为 kind 不匹配而非含糊的 not found。
	if err := reg.ResolvePredicate("h.x"); err == nil {
		t.Error("handler id in predicate slot must be kind mismatch")
	} else {
		wantErrContaining(t, err, "registered as handler, want predicate")
	}
	for _, probe := range []struct {
		name string
		call func() error
	}{
		{"codec", func() error { return reg.ResolveCodec("codec.x") }},
		{"reconciler", func() error { return reg.ResolveReconciler("rec.x") }},
		{"schema", func() error { return reg.ResolveSchema("schema.x") }},
		{"operation", func() error { return reg.ResolveOperation("op.x2") }},
	} {
		if err := probe.call(); err != nil {
			t.Errorf("%s resolution of registered id failed: %v", probe.name, err)
		}
	}
}
