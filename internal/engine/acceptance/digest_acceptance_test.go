// digest_acceptance_test.go 覆盖批次 3 验收的两条 digest 判据（对
// checked-in definitions/workflow.json 的定义源 definition.Workflow()）：
//
//  1. 分离：仅"实现侧"（registry 实例/槽位描述）变化，DefinitionDigest 不变；
//  2. 敏感性：定义语义逐项变异（依赖/policy/reason/schema/handler/join 语义），
//     每类变异 DefinitionDigest 必变。
//
// PackageDigest（安装事务侧的实现包摘要）属后续批次；本文件固化分离的结构
// 基础——DefinitionDigest 的唯一输入是 canonical 制品字节，而制品字节是
// 定义 IR 的纯函数；registry 只是激活门控，不进入字节。
package acceptance_test

import (
	"bytes"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
)

// TestAcceptanceDefinitionDigestSeparatedFromImplementationSide：handler ID
// 与定义语义完全相同、仅实现侧不同的 registry 变体，编译产物的制品字节与
// DefinitionDigest 逐字节不变。
func TestAcceptanceDefinitionDigestSeparatedFromImplementationSide(t *testing.T) {
	_, canonicalBytes, canonicalDigest := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))
	if canonicalDigest != definition.WorkflowDefinitionDigest {
		t.Fatalf("canonical digest = %s, want checked-in identity %s", canonicalDigest, definition.WorkflowDefinitionDigest)
	}

	// 变体一：同一批 HandlerID/封闭 ID 在另一个独立 registry 实例中再注册一次
	// （实例相互独立，"同 ID 二次注册"只发生在跨实例维度——实现绑定侧换了个
	// 实例，定义语义未动）。
	_, instanceBytes, instanceDigest := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))

	// 变体二：registry 槽位描述差异——追加六类"已注册但定义未引用"的条目
	// （含一个 runner 语义不同于任何 canonical handler 的多余 handler），
	// 模拟实现集扩充/codec 侧内容差异：这些实现侧描述不进定义。
	aug := workflowRegistry(t)
	for _, reg := range []struct {
		name string
		fn   func() error
	}{
		{"handler", func() error { return aug.RegisterHandler("engine.impl.extra", authoring.RunnerDurableActivity) }},
		{"codec", func() error { return aug.RegisterCodec("codec.impl.extra") }},
		{"predicate", func() error { return aug.RegisterPredicate("pred.impl.extra") }},
		{"reconciler", func() error { return aug.RegisterReconciler("reconcile.impl.extra") }},
		{"schema", func() error { return aug.RegisterSchema("schema.impl.extra") }},
		{"operation", func() error { return aug.RegisterOperation("op.impl.extra") }},
	} {
		if err := reg.fn(); err != nil {
			t.Fatalf("register extra %s: %v", reg.name, err)
		}
	}
	_, augBytes, augDigest := compileWorkflow(t, definition.Workflow(), aug)

	for _, row := range []struct {
		name   string
		data   []byte
		digest string
	}{
		{"independent registry instance", instanceBytes, instanceDigest},
		{"registry slot description differences", augBytes, augDigest},
	} {
		if !bytes.Equal(row.data, canonicalBytes) {
			t.Errorf("%s: artifact bytes differ from canonical", row.name)
		}
		if row.digest != canonicalDigest {
			t.Errorf("%s: DefinitionDigest = %s, want unchanged %s", row.name, row.digest, canonicalDigest)
		}
	}
}

// TestAcceptanceDefinitionDigestSensitiveToDefinitionMutations：对
// definition.Workflow() 逐类施加"仍合法"的语义变异（变异后必须编译成功，
// 否则用例夹具自身有缺陷），每类变异后 DefinitionDigest 必变，且各类变异
// 之间互不碰撞（不同语义维度进入不同制品字段）。
func TestAcceptanceDefinitionDigestSensitiveToDefinitionMutations(t *testing.T) {
	reg := augmentedRegistry(t)
	_, canonicalBytes, canonicalDigest := compileWorkflow(t, definition.Workflow(), reg)
	if canonicalDigest != definition.WorkflowDefinitionDigest {
		t.Fatalf("canonical digest = %s, want checked-in identity %s (extra unused registrations must not shift bytes)", canonicalDigest, definition.WorkflowDefinitionDigest)
	}

	// renameJoinAnchor 是 join 语义变异：fan-out 的 join 锚点身份改变
	// （fan.join → fan.join.alt），join policy 引用与下游依赖/binding 同步
	// 迁移——图仍合法，join 锚点语义已不同。
	renameJoinAnchor := func(d *compiler.Definition) {
		ji := stepIndex(t, d, "fan.join")
		editStepHeader(d, ji, func(h *authoring.Header) { h.ID = "fan.join.alt" })
		si := stepIndex(t, d, "fan.split")
		split := d.Steps[si].(authoring.ParallelStep)
		split.Join.JoinStep = "fan.join.alt"
		d.Steps[si] = split
		ci := stepIndex(t, d, "report.cost")
		cost := d.Steps[ci].(authoring.LocalStep)
		for i := range cost.Header.Dependencies {
			if string(cost.Header.Dependencies[i]) == "fan.join" {
				cost.Header.Dependencies[i] = "fan.join.alt"
			}
		}
		for i := range cost.IO.Inputs {
			if string(cost.IO.Inputs[i].From) == "fan.join" {
				cost.IO.Inputs[i].From = "fan.join.alt"
			}
		}
		d.Steps[ci] = cost
	}

	rows := []struct {
		name   string
		mutate func(d *compiler.Definition)
	}{
		{"dependency", func(d *compiler.Definition) {
			// report.cost 增加对 review.worker 的依赖与对应 typed binding。
			i := stepIndex(t, d, "report.cost")
			s := d.Steps[i].(authoring.LocalStep)
			s.Header.Dependencies = append(s.Header.Dependencies, "review.worker")
			s.IO.Inputs = append(s.IO.Inputs, authoring.InputBinding{From: "review.worker", OutputField: "out", ToField: "in"})
			d.Steps[i] = s
		}},
		{"retry policy", func(d *compiler.Definition) {
			i := stepIndex(t, d, "report.cost")
			s := d.Steps[i].(authoring.LocalStep)
			s.Retry = &authoring.RetryPolicy{MaxAttempts: 3}
			d.Steps[i] = s
		}},
		{"join policy mode", func(d *compiler.Definition) {
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Join.Mode = authoring.JoinAny
			d.Steps[i] = s
		}},
		{"failure policy mode", func(d *compiler.Definition) {
			i := stepIndex(t, d, "fan.split")
			s := d.Steps[i].(authoring.ParallelStep)
			s.Failure.Mode = authoring.WaitAll
			d.Steps[i] = s
		}},
		{"agent reason", func(d *compiler.Definition) {
			i := stepIndex(t, d, "review.worker")
			s := d.Steps[i].(authoring.AgentStep)
			s.Reason = authoring.ReasonSemanticJudgment
			d.Steps[i] = s
		}},
		{"schema id", func(d *compiler.Definition) {
			i := stepIndex(t, d, "ask.decide")
			s := d.Steps[i].(authoring.HumanAskStep)
			s.RequestSchema = "schema.ask.decision.request.alt"
			d.Steps[i] = s
		}},
		{"handler id", func(d *compiler.Definition) {
			i := stepIndex(t, d, "entry.parse")
			s := d.Steps[i].(authoring.LocalStep)
			s.Handler = "engine.entry.parse.alt"
			d.Steps[i] = s
		}},
		{"join anchor semantics", renameJoinAnchor},
	}

	seen := map[string]string{canonicalDigest: "canonical"}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			def := definition.Workflow()
			row.mutate(def)
			_, data, digest := compileWorkflow(t, def, reg)
			if bytes.Equal(data, canonicalBytes) {
				t.Fatalf("mutation %q left artifact bytes unchanged", row.name)
			}
			if digest == canonicalDigest {
				t.Fatalf("mutation %q left DefinitionDigest unchanged (%s)", row.name, digest)
			}
			if prev, dup := seen[digest]; dup {
				t.Fatalf("mutations %q and %q collide on digest %s", prev, row.name, digest)
			}
			seen[digest] = row.name
		})
	}
}
