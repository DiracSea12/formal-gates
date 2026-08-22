// mutation_acceptance_test.go 覆盖批次 3 验收的两类拒绝性判据（对象均为
// checked-in 定义源 definition.Workflow()）：
//
//  1. registry 完备性：缺失 ID、重复 ID、kind 不匹配三类全拒；只有完整解析
//     才产出 CompiledDefinition；
//  2. mutation tests：对步骤表的系统性破坏（删依赖/删 join policy/删 failure
//     policy/删 reconcile 引用/解绑 definition version/删 join 边）compiler
//     必拒——表驱动全枚举 + 随机组合 fuzz。
package acceptance_test

import (
	"errors"
	"math/rand"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
)

// TestAcceptanceRegistryCompleteness：registry 三类失败全拒；完整解析才产出
// CompiledDefinition。无 skip 的复刻注册表编译产物必须等于 checked-in 身份，
// 把夹具钉死在真实注册表上。
func TestAcceptanceRegistryCompleteness(t *testing.T) {
	t.Run("complete resolution produces CompiledDefinition", func(t *testing.T) {
		cd, _, digest := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))
		if cd.MissingEngineAdapter {
			t.Fatal("fully registered definition must not carry MISSING_ENGINE_ADAPTER")
		}
		if len(cd.Steps) != 9 {
			t.Fatalf("step count = %d, want 9", len(cd.Steps))
		}
		if digest != definition.WorkflowDefinitionDigest {
			t.Fatalf("fixture registry digest = %s, want checked-in identity %s (fixture drifted from definition.Registry)", digest, definition.WorkflowDefinitionDigest)
		}
	})

	// 缺失 ID：从完整注册表移除一个注册（每类槽位各一行），正常 compile 必须
	// 以 BLOCKED_BUG 拒绝并点名 MISSING_ENGINE_ADAPTER（closed world）。
	t.Run("missing id rejected per kind", func(t *testing.T) {
		for _, row := range []struct {
			kind string
			skip string
		}{
			{"handler", "engine.fan.slice"},
			{"codec", "codec.any.in"},
			{"predicate", "pred.review.post"},
			{"reconciler", "reconcile.entry.persist"},
			{"schema", "schema.ask.decision.response"},
			{"operation", "op.fan.transport"},
			{"askKind", "decision"},
		} {
			t.Run(row.kind, func(t *testing.T) {
				_, err := compiler.Compile(definition.Workflow(), workflowRegistry(t, row.skip))
				wantErr(t, err, "MISSING_ENGINE_ADAPTER")
				wantErr(t, err, "not registered")
				var ce *compiler.Error
				if !errors.As(err, &ce) || ce.Class != authoring.FailureBlockedBug {
					t.Fatalf("missing registration must classify BLOCKED_BUG: %v", err)
				}
			})
		}
	})

	// 重复 ID：同 ID 二次注册（同 kind 与跨 kind）一律注册期拒绝。
	t.Run("duplicate id rejected at registration", func(t *testing.T) {
		reg := workflowRegistry(t)
		wantErr(t, reg.RegisterCodec("codec.any.in"), `duplicate id "codec.any.in"`)
		wantErr(t, reg.RegisterHandler("engine.entry.parse", authoring.RunnerEngineLocal), `duplicate id "engine.entry.parse"`)
		wantErr(t, reg.RegisterPredicate("codec.any.in"), `duplicate id "codec.any.in"`)
	})

	// kind 不匹配：handler ID 放进 codec 槽（解析期报 kind 错用而非含糊的
	// not found）；对称地，codec ID 放进 predicate 槽同拒。
	t.Run("kind mismatch rejected (handler id in codec slot)", func(t *testing.T) {
		reg := workflowRegistry(t, "engine.entry.parse")
		if err := reg.RegisterCodec("engine.entry.parse"); err != nil {
			t.Fatalf("register codec: %v", err)
		}
		_, err := compiler.Compile(definition.Workflow(), reg)
		wantErr(t, err, `id "engine.entry.parse" registered as codec, want handler`)
		var ce *compiler.Error
		if !errors.As(err, &ce) || ce.Class != authoring.FailureInvariantViolation {
			t.Fatalf("kind mismatch must classify INVARIANT_VIOLATION: %v", err)
		}

		reg2 := workflowRegistry(t, "codec.any.in")
		if err := reg2.RegisterPredicate("codec.any.in"); err != nil {
			t.Fatalf("register predicate: %v", err)
		}
		_, err = compiler.Compile(definition.Workflow(), reg2)
		wantErr(t, err, `id "codec.any.in" registered as predicate, want codec`)
	})
}

// TestAcceptanceMutationEnumerationRejected：对步骤表的每类系统性破坏，compiler
// 必须拒绝（INVARIANT_VIOLATION），并命中对应的 distinguishing 分支文案。
func TestAcceptanceMutationEnumerationRejected(t *testing.T) {
	rows := []struct {
		name   string
		mutate func(d *compiler.Definition)
		want   string
	}{
		{"delete dependency keep binding", func(d *compiler.Definition) {
			// report.cost 删依赖但保留 input binding：inputs 集合 != deps 集合。
			i := stepIndex(t, d, "report.cost")
			if !dropDep(d, i, "ask.decide") {
				t.Fatal("dependency not found")
			}
		}, `input binding source "ask.decide" is not a dependency`},
		{"delete dependency and binding", func(d *compiler.Definition) {
			// report.cost 删掉全部依赖与 binding：自身失去可满足的依赖路径，
			// 又不在入口节点，成为不可达步骤。
			i := stepIndex(t, d, "report.cost")
			for _, dep := range []string{"fan.join", "ask.decide"} {
				if !dropDep(d, i, dep) || !dropBinding(d, i, dep) {
					t.Fatalf("dependency/binding %q not found", dep)
				}
			}
		}, "unreachable steps [report.cost]"},
		{"delete fan child dependency", func(d *compiler.Definition) {
			// fan.slice 删对 fan-out 锚点的依赖：不在入口节点，成为不可达步骤。
			i := stepIndex(t, d, "fan.slice")
			if !dropDep(d, i, "fan.split") || !dropBinding(d, i, "fan.split") {
				t.Fatal("dependency/binding not found")
			}
		}, "unreachable steps [fan.slice]"},
		{"delete join edge", func(d *compiler.Definition) {
			// fan.join 删对一名 child 的依赖 + binding：join 覆盖不完整
			// （children 集合 ⊃ join 依赖集合）。
			i := stepIndex(t, d, "fan.join")
			if !dropDep(d, i, "fan.transport") || !dropBinding(d, i, "fan.transport") {
				t.Fatal("dependency/binding not found")
			}
		}, `(fan-out coverage)`},
		{"delete join policy", func(d *compiler.Definition) {
			// join policy 整体置零：join 锚点引用先在存在性检查被拒。
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Join = authoring.JoinPolicy{}
			d.Steps[i] = s
		}, `join step "" not found`},
		{"delete join mode", func(d *compiler.Definition) {
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Join.Mode = ""
			d.Steps[i] = s
		}, "join policy (joinStep + mode ALL|ANY) required"},
		{"delete failure policy", func(d *compiler.Definition) {
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Failure = authoring.FailurePolicy{}
			d.Steps[i] = s
		}, "failure policy (mode FAIL_FAST|WAIT_ALL + escalate class) required"},
		{"delete failure escalate", func(d *compiler.Definition) {
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Failure.Escalate = ""
			d.Steps[i] = s
		}, "failure policy (mode FAIL_FAST|WAIT_ALL + escalate class) required"},
		{"delete reconcile reference", func(d *compiler.Definition) {
			i := stepIndex(t, d, "entry.persist")
			s := d.Steps[i].(authoring.DurableStep)
			s.Reconcile = ""
			d.Steps[i] = s
		}, `durable step "entry.persist": reconcile id required`},
		{"unbind definition version", func(d *compiler.Definition) {
			i := stepIndex(t, d, "entry.persist")
			if !editStepHeader(d, i, func(h *authoring.Header) { h.DefinitionVersion = "1" }) {
				t.Fatal("step not editable")
			}
		}, `(unbound definition version)`},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			def := definition.Workflow()
			row.mutate(def)
			_, err := compiler.Compile(def, workflowRegistry(t))
			wantErr(t, err, row.want)
			var ce *compiler.Error
			if !errors.As(err, &ce) || ce.Class != authoring.FailureInvariantViolation {
				t.Fatalf("mutation must classify INVARIANT_VIOLATION: %v", err)
			}
		})
	}
}

// TestAcceptanceMutationFuzzRejected：对步骤表施加随机组合的破坏性变异，
// 每轮 compiler 必拒（INVARIANT_VIOLATION）。
//
// 可靠性论证：菜单每一项单独必拒，且菜单只含删除/置零类操作——版本绑定、
// join/failure 策略必填、reconcile 必填、inputs==deps、join 覆盖与可达性
// 都是单调不变量（一旦违反，删除/置零类操作无法修复），因此任意非空组合
// 必拒。种子固定，失败可复现。
func TestAcceptanceMutationFuzzRejected(t *testing.T) {
	fanJoinIndex := func(d *compiler.Definition) int { return stepIndex(t, d, "fan.join") }
	fanSplitIndex := func(d *compiler.Definition) int { return stepIndex(t, d, "fan.split") }
	children := map[string]bool{"fan.slice": true, "fan.transport": true}

	menu := []struct {
		name  string
		apply func(d *compiler.Definition, rng *rand.Rand)
	}{
		{"zero join policy", func(d *compiler.Definition, rng *rand.Rand) {
			i := fanSplitIndex(d)
			s := d.Steps[i].(authoring.ParallelStep)
			s.Join = authoring.JoinPolicy{}
			d.Steps[i] = s
		}},
		{"zero failure policy", func(d *compiler.Definition, rng *rand.Rand) {
			i := fanSplitIndex(d)
			s := d.Steps[i].(authoring.ParallelStep)
			s.Failure = authoring.FailurePolicy{}
			d.Steps[i] = s
		}},
		{"clear reconcile", func(d *compiler.Definition, rng *rand.Rand) {
			i := stepIndex(t, d, "entry.persist")
			s := d.Steps[i].(authoring.DurableStep)
			s.Reconcile = ""
			d.Steps[i] = s
		}},
		{"unbind version", func(d *compiler.Definition, rng *rand.Rand) {
			// 随机步骤的版本信封解绑（改写为任意其他值）。
			i := rng.Intn(len(d.Steps))
			if !editStepHeader(d, i, func(h *authoring.Header) { h.DefinitionVersion = "fuzz-unbound" }) {
				t.Fatalf("step %d not editable", i)
			}
		}},
		{"drop dependency keep binding", func(d *compiler.Definition, rng *rand.Rand) {
			// 随机依赖边：只删头部的依赖、保留 input binding（inputs != deps）。
			// 每轮至多删 2 条边（本项与 join 边各一），全图 10 条边必有剩余。
			type edge struct {
				step int
				dep  string
			}
			var edges []edge
			for i, s := range d.Steps {
				h, ok := stepHeader(s)
				if !ok {
					t.Fatalf("step %d has no header", i)
				}
				for _, dep := range h.Dependencies {
					edges = append(edges, edge{i, string(dep)})
				}
			}
			if len(edges) == 0 {
				t.Fatal("no dependency edges left to drop (menu math broken)")
			}
			e := edges[rng.Intn(len(edges))]
			if !dropDep(d, e.step, e.dep) {
				t.Fatalf("drop dependency %q failed", e.dep)
			}
		}},
		{"drop join child edge", func(d *compiler.Definition, rng *rand.Rand) {
			// fan.join 删对一名 child 的依赖 + 对应 binding（join 覆盖被破坏）。
			i := fanJoinIndex(d)
			var childDeps []string
			if h, ok := stepHeader(d.Steps[i]); ok {
				for _, dep := range h.Dependencies {
					if children[string(dep)] {
						childDeps = append(childDeps, string(dep))
					}
				}
			}
			if len(childDeps) == 0 {
				t.Fatal("fan.join has no child dependency left (menu math broken)")
			}
			dep := childDeps[rng.Intn(len(childDeps))]
			if !dropDep(d, i, dep) || !dropBinding(d, i, dep) {
				t.Fatalf("drop join edge %q failed", dep)
			}
		}},
	}

	const rounds = 200
	rng := rand.New(rand.NewSource(20260822)) // 固定种子：轮次可复现
	for r := 0; r < rounds; r++ {
		def := definition.Workflow()
		n := 1 + rng.Intn(len(menu))
		pick := rng.Perm(len(menu))[:n]
		var applied []string
		for _, i := range pick {
			menu[i].apply(def, rng)
			applied = append(applied, menu[i].name)
		}
		_, err := compiler.Compile(def, workflowRegistry(t))
		if err == nil {
			t.Fatalf("round %d: mutations %v were accepted by compiler", r, applied)
		}
		var ce *compiler.Error
		if !errors.As(err, &ce) || ce.Class != authoring.FailureInvariantViolation {
			t.Fatalf("round %d (%v): must classify INVARIANT_VIOLATION, got %v", r, applied, err)
		}
	}
}
