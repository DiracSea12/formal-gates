package contracttests

import (
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

// 用例：compile 管线拒绝信封与身份层缺陷——nil 定义/registry、空版本、空
// 入口节点、零步骤与空节点（绕过 constructor 的零值头部）。
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
		{"human ask without ask kind", authoring.HumanAskStep{
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

// 用例：registry ID 未注册的正常 compile 路由 BLOCKED_BUG 并拒绝签发
// （MISSING_ENGINE_ADAPTER 不是可执行定义的合法状态）；diagnostic 模式产出
// marker 定义与逐条诊断（step/ref/want），不再失败。
func TestCompileRoutesUnregisteredIDsPerMode(t *testing.T) {
	def := fxDefinition(t)

	// 正常模式：七类槽位各抽一个未注册 ID，均 BLOCKED_BUG + MISSING_ENGINE_ADAPTER。
	for _, skip := range []string{"h.s.review", "c.in", "pred.done", "r.persist", "s.req", "op.x", "confirm"} {
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

	// 多个未注册 ID：诊断包含 handler 与 codec 的两个引用。
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

// 用例：编译通过实际步骤引用按唯一注册项解析各槽位 ID，
// 并拒绝未注册 predicate 与槽位错用。
func TestCompileResolvesRegistrySlotReferences(t *testing.T) {
	reg := fxRegistryWithout(t, "schema.x")
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
	if err := reg.RegisterAskKind("ask.x"); err != nil {
		t.Fatal(err)
	}

	// 未注册 predicate 在真实编译路径中按 closed world 拒绝。
	parse := fxParse(t)
	parse.IO.Preconditions = []authoring.PredicateRef{{ID: "pred.nope"}}
	_, err := compiler.Compile(fxDefinition(t, parse), reg)
	wantCompileErr(t, err, authoring.FailureBlockedBug, `predicate id "pred.nope" not registered (closed world)`)

	// 槽位错用：立即暴露为 kind 不匹配而非含糊的 not found。
	parse.IO.Preconditions = []authoring.PredicateRef{{ID: "h.s.parse"}}
	_, err = compiler.Compile(fxDefinition(t, parse), reg)
	wantCompileErr(t, err, authoring.FailureInvariantViolation, "registered as handler, want predicate")

	// 已注册的各槽位 ID 通过实际步骤引用解析。
	parse.IO.Preconditions = []authoring.PredicateRef{{ID: "pred.x"}}
	parse.IO.InputCodec = "codec.x"
	parse.IO.OutputCodec = "codec.x"
	persist := fxPersist(t)
	persist.Reconcile = "rec.x"
	dispatch := fxDispatch(t)
	dispatch.Schema = "schema.x"
	dispatch.Operation = "op.x2"
	ask := fxAsk(t)
	ask.AskKind = "ask.x"
	ask.RequestSchema = "schema.x"
	ask.ResponseSchema = "schema.x"
	if _, err := compiler.Compile(fxDefinition(t, parse, persist, dispatch, ask), reg); err != nil {
		t.Fatalf("registered predicate/codec/reconciler/schema/operation/askKind resolution failed: %v", err)
	}
}
