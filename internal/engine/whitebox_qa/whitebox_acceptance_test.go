package whitebox_qa

import (
	"bytes"
	"testing"
	"time"

	"formal-gates/internal/engine/acceptance"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

// 用例：收口段 marker 扫描——nil 定义、携带 MISSING_ENGINE_ADAPTER 的
// diagnostic-only 定义拒绝；正常编译产物（含 shipped definition）通过，
// 通过即"可编码为 canonical 制品、无 marker、可执行"的机械证明。
func TestScanNoMissingEngineAdapterBindsMarkerToExecutability(t *testing.T) {
	if err := acceptance.ScanNoMissingEngineAdapter(nil); err == nil {
		t.Error("nil definition must be rejected")
	}

	res, err := compiler.CompileDiagnostic(fxDefinition(t), fxRegistryWithout(t, "h.s.review"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Definition.MissingEngineAdapter {
		t.Fatal("fixture must carry the marker for this case")
	}
	err = acceptance.ScanNoMissingEngineAdapter(res.Definition)
	if err == nil {
		t.Error("marker definition must fail the closure scan")
	} else {
		wantErrContaining(t, err, "MISSING_ENGINE_ADAPTER")
	}

	if err := acceptance.ScanNoMissingEngineAdapter(fxCompile(t, fxDefinition(t), fxRegistry(t))); err != nil {
		t.Fatalf("clean compile must pass the scan: %v", err)
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptance.ScanNoMissingEngineAdapter(cd); err != nil {
		t.Fatalf("shipped definition must pass the scan: %v", err)
	}
}

// 用例：DefinitionDigest 只绑定定义语义——registry 是解析环境而非制品内容：
// 同一定义在含额外未引用注册的 registry 下编译，制品字节与 digest 完全不变
// （definition/package digest 分离的制品级基础：包内容变化不进入定义身份）。
func TestDefinitionDigestIgnoresRegistryContents(t *testing.T) {
	def := fxDefinition(t)
	lean := fxCompile(t, def, fxRegistry(t))
	rich := fxCompile(t, def, fxRegistryExtra(t))
	leanBytes, err := encoder.Encode(lean)
	if err != nil {
		t.Fatal(err)
	}
	richBytes, err := encoder.Encode(rich)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leanBytes, richBytes) {
		t.Fatal("artifact bytes must not depend on registry contents beyond resolution")
	}
	if encoder.Digest(leanBytes) != encoder.Digest(richBytes) {
		t.Fatal("definition digest must not depend on registry contents")
	}
}

// 用例：digest 语义敏感性——改变依赖边、join/failure 语义、timeout、retry、
// handler/codec/predicate/schema/operation ID、reason、幂等策略任一项，
// definition digest 必变（ADR-001 验收 6）。
func TestDefinitionDigestSensitiveToSemanticMutations(t *testing.T) {
	reg := fxRegistryExtra(t)
	base := fxCompile(t, fxDefinition(t), reg)
	baseBytes, err := encoder.Encode(base)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest := encoder.Digest(baseBytes)

	rows := []struct {
		name string
		step func(t *testing.T) authoring.Step
	}{
		{"dependency edge", func(t *testing.T) authoring.Step {
			return mkLocalStep(t, fxHeader("s.cost", "n3", "s.join"),
				fxIO("s.join"), authoring.LocalSpec{Handler: "h.s.cost"})
		}},
		{"join mode", func(t *testing.T) authoring.Step {
			return mkParallelStep(t, fxHeader("s.fan", "n2", "s.parse"),
				authoring.ParallelSpec{
					Children: []authoring.StepID{"s.left", "s.right"},
					Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAny},
					Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
				})
		}},
		{"failure mode", func(t *testing.T) authoring.Step {
			return mkParallelStep(t, fxHeader("s.fan", "n2", "s.parse"),
				authoring.ParallelSpec{
					Children: []authoring.StepID{"s.left", "s.right"},
					Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
					Failure:  authoring.FailurePolicy{Mode: authoring.WaitAll, Escalate: authoring.FailureInvariantViolation},
				})
		}},
		{"failure escalate class", func(t *testing.T) authoring.Step {
			return mkParallelStep(t, fxHeader("s.fan", "n2", "s.parse"),
				authoring.ParallelSpec{
					Children: []authoring.StepID{"s.left", "s.right"},
					Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
					Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureBlockedBug},
				})
		}},
		{"timeout", func(t *testing.T) authoring.Step {
			io := fxIO("s.parse")
			io.Postconditions = []authoring.PredicateRef{{ID: "pred.done"}}
			return mkAgentStep(t, fxHeader("s.review", "n1", "s.parse"), io,
				authoring.AgentSpec{Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment, Timeout: 90 * time.Second})
		}},
		{"retry policy", func(t *testing.T) authoring.Step {
			return mkDurableStep(t, fxHeader("s.persist", "n0", "s.parse"), fxIO("s.parse"),
				authoring.DurableSpec{
					Handler: "h.s.persist", Idempotency: authoring.IdempotencyDeterministicInput,
					Reconcile: "r.persist", Timeout: 30 * time.Second,
					Retry: authoring.RetryPolicy{MaxAttempts: 4, Backoff: time.Second},
				})
		}},
		{"handler id", func(t *testing.T) authoring.Step {
			return mkLocalStep(t, fxHeader("s.parse", "n0"), fxIO(),
				authoring.LocalSpec{Handler: "h.alt"})
		}},
		{"codec id", func(t *testing.T) authoring.Step {
			return mkLocalStep(t, fxHeader("s.parse", "n0"),
				authoring.IO{InputCodec: "c.in", OutputCodec: "c.alt"},
				authoring.LocalSpec{Handler: "h.s.parse"})
		}},
		{"predicate id", func(t *testing.T) authoring.Step {
			io := fxIO("s.parse")
			io.Postconditions = []authoring.PredicateRef{{ID: "pred.alt"}}
			return mkAgentStep(t, fxHeader("s.review", "n1", "s.parse"), io,
				authoring.AgentSpec{Handler: "h.s.review", Reason: authoring.ReasonSemanticJudgment, Timeout: time.Minute})
		}},
		{"schema id", func(t *testing.T) authoring.Step {
			return mkHumanAskStep(t, fxHeader("s.ask", "n1", "s.parse"),
				authoring.HumanAskSpec{AskKind: "confirm", RequestSchema: "s.alt",
					ResponseSchema: "s.resp", FreshnessTTL: 10 * time.Minute})
		}},
		{"ask kind id", func(t *testing.T) authoring.Step {
			// ask 类型是 registry 解析的定义语义：换一个已注册的合法类型，
			// 制品字节与 digest 必变（封闭 askKind 槽位的 digest 敏感性）。
			return mkHumanAskStep(t, fxHeader("s.ask", "n1", "s.parse"),
				authoring.HumanAskSpec{AskKind: "ask.alt", RequestSchema: "s.req",
					ResponseSchema: "s.resp", FreshnessTTL: 10 * time.Minute})
		}},
		{"non-programmable reason", func(t *testing.T) authoring.Step {
			io := fxIO("s.parse")
			io.Postconditions = []authoring.PredicateRef{{ID: "pred.done"}}
			return mkAgentStep(t, fxHeader("s.review", "n1", "s.parse"), io,
				authoring.AgentSpec{Handler: "h.s.review", Reason: authoring.ReasonCreativeImplementation, Timeout: time.Minute})
		}},
		{"idempotency strategy", func(t *testing.T) authoring.Step {
			return mkDurableStep(t, fxHeader("s.persist", "n0", "s.parse"), fxIO("s.parse"),
				authoring.DurableSpec{
					Handler: "h.s.persist", Idempotency: authoring.IdempotencyTaskKeyScoped,
					Reconcile: "r.persist", Timeout: 30 * time.Second,
					Retry: authoring.RetryPolicy{MaxAttempts: 3, Backoff: time.Second},
				})
		}},
		{"operation id", func(t *testing.T) authoring.Step {
			return mkHostActionStep(t, fxHeader("s.dispatch", "n1", "s.parse"), fxIO("s.parse"),
				authoring.HostActionSpec{
					Handler: "h.s.dispatch", Boundary: authoring.BoundaryExternalCapability,
					Operation: "op.alt", Schema: "schema.alt", Timeout: 5 * time.Second,
				})
		}},
	}
	for _, row := range rows {
		steps := withStep(fxAllSteps(t), stepID(row.step(t)), row.step(t))
		cd := fxCompile(t, fxDefinition(t, steps...), reg)
		data, err := encoder.Encode(cd)
		if err != nil {
			t.Fatalf("%s: encode: %v", row.name, err)
		}
		if bytes.Equal(data, baseBytes) {
			t.Errorf("%s: mutation left artifact bytes unchanged", row.name)
		}
		if encoder.Digest(data) == baseDigest {
			t.Errorf("%s: definition digest must change", row.name)
		}
	}
}

// 用例：复杂度止损——新增一个普通业务节点（新 handler/codec，仅注册表追加）
// 走完 compile → encode → decode 全链路，不触碰 compiler core；ordinal 按
// 拓扑性质自动落位。
func TestCompilerAcceptsNewBusinessNodeWithoutCoreChange(t *testing.T) {
	extra := mkLocalStep(t, fxHeader("s.extra", "n3", "s.join"), fxIO("s.join"),
		authoring.LocalSpec{Handler: "h.newbiz"})
	steps := append(fxAllSteps(t), extra)

	cd := fxCompile(t, fxDefinition(t, steps...), fxRegistryExtra(t))
	cs := findStep(t, cd, "s.extra")
	if cs.Header.Kind != compiler.KindLocal || cs.Header.Ordinal != 10 {
		t.Fatalf("new node = {kind %s ordinal %d}, want {LOCAL 10 (topology-derived)}", cs.Header.Kind, cs.Header.Ordinal)
	}
	if cs.Header.Authority != authoring.AuthorityEngine || cs.Header.Runner != authoring.RunnerEngineLocal {
		t.Fatalf("new node derived dims = %s/%s", cs.Header.Authority, cs.Header.Runner)
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Decode(data); err != nil {
		t.Fatalf("extended definition must round-trip: %v", err)
	}
}

// 用例：mutation 删除类拒绝——删除依赖边、join 成员覆盖、步骤表成员、
// reconcile、retry、版本绑定、input binding、failure policy，compiler 必须
// 拒绝（ADR-001 验收 9）。
func TestCompilerRejectsStructuralMutationDeletions(t *testing.T) {
	reg := fxRegistry(t)
	rows := []struct {
		name   string
		steps  func(t *testing.T) []authoring.Step
		substr string
	}{
		{"join loses child coverage", func(t *testing.T) []authoring.Step {
			join := mkLocalStep(t, fxHeader("s.join", "n2", "s.left"),
				fxIO("s.left"), authoring.LocalSpec{Handler: "h.s.join"})
			return withStep(fxAllSteps(t), "s.join", join)
		}, "does not depend on child"},
		{"step table drops fan child", func(t *testing.T) []authoring.Step {
			return withoutStep(fxAllSteps(t), "s.left")
		}, "not found"},
		{"fan loses anchor dependency", func(t *testing.T) []authoring.Step {
			fan := mkParallelStep(t, fxHeader("s.fan", "n0"),
				authoring.ParallelSpec{
					Children: []authoring.StepID{"s.left", "s.right"},
					Join:     authoring.JoinPolicy{JoinStep: "s.join", Mode: authoring.JoinAll},
					Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
				})
			return withStep(fxAllSteps(t), "s.fan", fan)
		}, "fan-out anchor dependency required"},
		{"durable loses reconcile", func(t *testing.T) []authoring.Step {
			persist := fxPersist(t)
			persist.Reconcile = ""
			return withStep(fxAllSteps(t), "s.persist", persist)
		}, "reconcile id required"},
		{"durable loses retry", func(t *testing.T) []authoring.Step {
			persist := fxPersist(t)
			persist.Retry = authoring.RetryPolicy{}
			return withStep(fxAllSteps(t), "s.persist", persist)
		}, "maxAttempts must be >= 1"},
		{"step loses version binding", func(t *testing.T) []authoring.Step {
			h := fxHeader("s.parse", "n0")
			h.DefinitionVersion = "9"
			parse := mkLocalStep(t, h, fxIO(), authoring.LocalSpec{Handler: "h.s.parse"})
			return withStep(fxAllSteps(t), "s.parse", parse)
		}, "unbound definition version"},
		{"step loses input binding", func(t *testing.T) []authoring.Step {
			cost := mkLocalStep(t, fxHeader("s.cost", "n3", "s.join", "s.ask"),
				fxIO("s.join"), authoring.LocalSpec{Handler: "h.s.cost"})
			return withStep(fxAllSteps(t), "s.cost", cost)
		}, "has no typed input binding"},
		{"fan loses failure policy", func(t *testing.T) []authoring.Step {
			fan := fxFan(t)
			fan.Failure.Escalate = ""
			return withStep(fxAllSteps(t), "s.fan", fan)
		}, "failure policy"},
	}
	for _, row := range rows {
		_, err := compiler.Compile(fxDefinition(t, row.steps(t)...), reg)
		if err == nil {
			t.Errorf("%s: compiler must reject this mutation, got nil", row.name)
			continue
		}
		wantCompileErr(t, err, authoring.FailureInvariantViolation, row.substr)
	}
}
