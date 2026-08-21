package compiler

import (
	"errors"
	"testing"

	"formal-gates/internal/engine/authoring"
)

// 本文件覆盖图不变量全分支（compiler 主责的四类拒绝中的图部分：循环/不可达/
// 缺依赖/join 缺失与覆盖）+ 归一化 + inputs==deps 不变量。
// compiler 主责的版本绑定与 registry 完备性分支分别在下面的版本行与
// matrix_test.go 的路由测试中覆盖。

func mustLocal(h authoring.Header, io authoring.IO, handler authoring.HandlerID) authoring.Step {
	s, err := authoring.NewLocalStep(h, io, authoring.LocalSpec{Handler: handler})
	if err != nil {
		panic(err)
	}
	return s
}

// parallelSteps 构建并行组基座：root → split(children) → join(joinDeps)。
// children/joinStep/joinDeps 是各覆盖分支的观测点。
func parallelSteps(children []string, joinStep string, joinDeps ...string) []authoring.Step {
	root := mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse")
	kids := make([]authoring.StepID, 0, len(children))
	for _, c := range children {
		kids = append(kids, authoring.StepID(c))
	}
	split, err := authoring.NewParallelStep(header("fan.split", "fan", "entry.root"),
		authoring.ParallelSpec{
			Children: kids,
			Join:     authoring.JoinPolicy{JoinStep: authoring.StepID(joinStep), Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		})
	if err != nil {
		panic(err)
	}
	var steps []authoring.Step
	steps = append(steps, root, split)
	for _, c := range children {
		steps = append(steps, mustLocal(header(c, "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice"))
	}
	steps = append(steps, mustLocal(header("fan.join", "fan", joinDeps...), ioWith(joinDeps...), "engine.fan.join"))
	return steps
}

func defWith(steps ...authoring.Step) *Definition {
	return &Definition{Version: goldenVersion, EntryNode: goldenEntry, Steps: steps}
}

// TestGraphRejects 逐分支验证图不变量拒绝，错误信息含具体位置（步骤 ID）。
func TestGraphRejects(t *testing.T) {
	orphan := mustLocal(header("island.solo", "island"), ioWith(), "engine.entry.parse")
	ghostDep, err := authoring.NewLocalStep(header("entry.ghost", "entry", "ghost"),
		ioWith("ghost"), authoring.LocalSpec{Handler: "engine.entry.parse"})
	if err != nil {
		t.Fatalf("ghost dep step: %v", err)
	}
	dupParse := mustLocal(header("entry.parse", "entry"), ioWith(), "engine.entry.parse")
	rootA := mustLocal(header("fan.a", "fan"), ioWith(), "engine.entry.parse")
	depB := mustLocal(header("entry.b", "entry", "fan.a"), ioWith("fan.a"), "engine.entry.parse")
	// 绕过 constructor 的原始结构体：版本不绑定、自引用、零值三行
	//（constructor 已拦同类问题，这里验证 compiler 二次防线同样拒绝）。
	unbound := authoring.LocalStep{
		Header:  authoring.Header{ID: "entry.u", NodeID: "entry", DefinitionVersion: "wf-v2"},
		IO:      ioWith(),
		Handler: "engine.entry.parse",
	}
	selfDep := authoring.LocalStep{
		Header:  authoring.Header{ID: "entry.self", NodeID: "entry", Dependencies: []authoring.StepID{"entry.self"}, DefinitionVersion: goldenVersion},
		IO:      ioWith(),
		Handler: "engine.entry.parse",
	}
	anchorSteps := func() []authoring.Step {
		// split 在入口节点且无依赖（可达性通过），随后被锚点检查拒绝。
		split, err := authoring.NewParallelStep(header("fan.split", "entry"),
			authoring.ParallelSpec{
				Children: []authoring.StepID{"fan.s1", "fan.s2"},
				Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
		if err != nil {
			t.Fatalf("anchor split: %v", err)
		}
		return []authoring.Step{
			split,
			mustLocal(header("fan.s1", "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice"),
			mustLocal(header("fan.s2", "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice"),
			mustLocal(header("fan.join", "fan", "fan.s1", "fan.s2"), ioWith("fan.s1", "fan.s2"), "engine.fan.join"),
		}
	}
	joinIsChild := func() []authoring.Step {
		steps := parallelSteps([]string{"fan.s1", "fan.s2"}, "fan.join", "fan.s1", "fan.s2")
		// 替换为 join 同时是 child 的原始结构体（constructor 拒绝该组合，
		// compiler 二次防线验证）。
		steps[1] = authoring.ParallelStep{
			Header:   header("fan.split", "fan", "entry.root"),
			Children: []authoring.StepID{"fan.s1", "fan.join"},
			Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
			Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
		}
		return steps
	}
	cycA := mustLocal(header("cyc.a", "entry", "cyc.b"), ioWith("cyc.b"), "engine.entry.parse")
	cycB := mustLocal(header("cyc.b", "entry", "cyc.a"), ioWith("cyc.a"), "engine.entry.parse")
	// 缺 child：ghost.c2 在 children 里但表内不存在（parallelSteps 会为每个
	// child 建步骤，因此单独构建）。
	missingChild := func() []authoring.Step {
		split, err := authoring.NewParallelStep(header("fan.split", "fan", "entry.root"),
			authoring.ParallelSpec{
				Children: []authoring.StepID{"fan.s1", "ghost.c2"},
				Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAll},
				Failure:  authoring.FailurePolicy{Mode: authoring.FailFast, Escalate: authoring.FailureInvariantViolation},
			})
		if err != nil {
			t.Fatalf("missing child split: %v", err)
		}
		return []authoring.Step{
			mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse"),
			split,
			mustLocal(header("fan.s1", "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice"),
			mustLocal(header("fan.join", "fan", "fan.s1"), ioWith("fan.s1"), "engine.fan.join"),
		}
	}

	rows := []struct {
		name  string
		steps []authoring.Step
		entry string
		want  string
	}{
		{"cycle", []authoring.Step{cycA, cycB}, goldenEntry, "dependency cycle among steps [cyc.a cyc.b]"},
		{"unreachable orphan", append(goldenSteps(), orphan), goldenEntry, "unreachable steps [island.solo]"},
		{"missing dependency", append(goldenSteps(), ghostDep), goldenEntry, `dependency "ghost" not found`},
		{"missing join step", parallelSteps([]string{"fan.s1", "fan.s2"}, "ghost.join", "fan.s1", "fan.s2"), goldenEntry, `join step "ghost.join" not found`},
		{"missing child", missingChild(), goldenEntry, `child "ghost.c2" not found`},
		{"join misses child (fan-out coverage)", parallelSteps([]string{"fan.s1", "fan.s2"}, "fan.join", "fan.s1"), goldenEntry, `join step "fan.join" does not depend on child "fan.s2" (fan-out coverage)`},
		{"join depends outside children", parallelSteps([]string{"fan.s1", "fan.s2"}, "fan.join", "fan.s1", "fan.s2", "entry.root"), goldenEntry, `depends on "entry.root" outside children (fan-out coverage)`},
		{"join is a child", joinIsChild(), goldenEntry, `join step "fan.join" must not be a child`},
		{"branch closure", append(parallelSteps([]string{"fan.s1", "fan.s2"}, "fan.join", "fan.s1", "fan.s2"),
			mustLocal(header("fan.x", "fan", "fan.s1"), ioWith("fan.s1"), "engine.fan.slice")), goldenEntry,
			`child "fan.s1" has dependent "fan.x" other than join "fan.join"`},
		{"parallel without anchor", anchorSteps(), goldenEntry, "fan-out anchor dependency required"},
		{"duplicate step id", append(goldenSteps(), dupParse), goldenEntry, `duplicate step id "entry.parse"`},
		{"entry node without steps", goldenSteps(), "nowhere", `entry node "nowhere" has no steps`},
		{"entry node without roots", []authoring.Step{rootA, depB}, goldenEntry, "no dependency-free step"},
		{"unbound definition version", []authoring.Step{unbound}, goldenEntry, `definitionVersion "wf-v2" != definition "wf-v1" (unbound definition version)`},
		{"self dependency", []authoring.Step{selfDep}, goldenEntry, "references the step itself"},
		{"zero-value step", []authoring.Step{authoring.LocalStep{}}, goldenEntry, "step: id required"},
		{"nil step", []authoring.Step{nil}, goldenEntry, "nil step"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			def := &Definition{Version: goldenVersion, EntryNode: authoring.NodeID(row.entry), Steps: row.steps}
			_, err := Compile(def, goldenRegistry(t))
			wantErr(t, err, row.want)
			// 结构性拒绝一律分类为 INVARIANT_VIOLATION。
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("compile error is not *compiler.Error: %T", err)
			}
			if ce.Class != authoring.FailureInvariantViolation {
				t.Fatalf("class = %s, want INVARIANT_VIOLATION", ce.Class)
			}
		})
	}

	t.Run("nil definition and registry", func(t *testing.T) {
		if _, err := Compile(nil, goldenRegistry(t)); err == nil {
			t.Fatal("nil definition accepted")
		}
		if _, err := Compile(goldenDefinition(), nil); err == nil {
			t.Fatal("nil registry accepted")
		}
		if _, err := Compile(&Definition{Version: goldenVersion, EntryNode: goldenEntry}, goldenRegistry(t)); err == nil {
			t.Fatal("empty steps accepted")
		}
	})
}

// TestCompileNormalization：依赖/children 去重排序、predicate/input 排序、
// 空集合归一化为 nil——即使输入是绕过 constructor 的原始结构体。
func TestCompileNormalization(t *testing.T) {
	root := mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse")
	a := mustLocal(header("entry.a", "entry", "entry.root"), ioWith("entry.root"), "engine.entry.parse")
	z := mustLocal(header("entry.z", "entry", "entry.root"), ioWith("entry.root"), "engine.entry.parse")
	merge := authoring.LocalStep{
		Header: authoring.Header{
			ID: "entry.merge", NodeID: "entry", DefinitionVersion: goldenVersion,
			Dependencies: []authoring.StepID{"entry.z", "entry.a", "entry.z"}, // 乱序 + 重复
		},
		IO: authoring.IO{
			InputCodec: "codec.any.in", OutputCodec: "codec.any.out",
			Postconditions: []authoring.PredicateRef{ // 乱序 + 同 ID 否定在前
				{ID: "pred.b"}, {ID: "pred.a", Negated: true}, {ID: "pred.a"},
			},
			Inputs: []authoring.InputBinding{ // 乱序
				{From: "entry.z", OutputField: "o", ToField: "i2"},
				{From: "entry.a", OutputField: "o", ToField: "i1"},
			},
		},
		Handler: "engine.entry.parse",
	}
	split := authoring.ParallelStep{
		Header:   header("fan.split", "fan", "entry.root"),
		Children: []authoring.StepID{"fan.c2", "fan.c1", "fan.c2"}, // 乱序 + 重复
		Join:     authoring.JoinPolicy{JoinStep: "fan.join", Mode: authoring.JoinAny},
		Failure:  authoring.FailurePolicy{Mode: authoring.WaitAll, Escalate: authoring.FailureBusinessReject},
	}
	c1 := mustLocal(header("fan.c1", "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice")
	c2 := mustLocal(header("fan.c2", "fan", "fan.split"), ioWith("fan.split"), "engine.fan.slice")
	join := mustLocal(header("fan.join", "fan", "fan.c1", "fan.c2"), ioWith("fan.c1", "fan.c2"), "engine.fan.join")

	reg := goldenRegistry(t)
	for _, p := range []authoring.PredicateID{"pred.a", "pred.b"} {
		if err := reg.RegisterPredicate(p); err != nil {
			t.Fatalf("register predicate %q: %v", p, err)
		}
	}
	cd, err := Compile(defWith(root, a, z, merge, split, c1, c2, join), reg)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	byID := map[authoring.StepID]CompiledStep{}
	for _, cs := range cd.Steps {
		byID[cs.Header.ID] = cs
	}
	m := byID["entry.merge"]
	if want := []authoring.StepID{"entry.a", "entry.z"}; !depsEqual(m, want) {
		t.Fatalf("dependencies not deduped+sorted: %v", m.Header.Dependencies)
	}
	post := m.IO.Postconditions
	if len(post) != 3 || post[0].ID != "pred.a" || post[0].Negated ||
		post[1].ID != "pred.a" || !post[1].Negated || post[2].ID != "pred.b" {
		t.Fatalf("postconditions not sorted (id, non-negated first): %+v", post)
	}
	if in := m.IO.Inputs; len(in) != 2 || in[0].From != "entry.a" || in[1].From != "entry.z" {
		t.Fatalf("inputs not sorted by source: %+v", in)
	}
	if root := byID["entry.root"]; root.Header.Dependencies != nil || root.IO.Preconditions != nil {
		t.Fatalf("empty collections must normalize to nil: %+v", root)
	}
	p, ok := byID["fan.split"].Payload.(CompiledParallelStep)
	if !ok || len(p.Children) != 2 || p.Children[0] != "fan.c1" || p.Children[1] != "fan.c2" {
		t.Fatalf("children not deduped+sorted: %+v", byID["fan.split"].Payload)
	}
}

// depsEqual 报告 compiled 步骤的依赖切片是否与 want 逐项相等。
func depsEqual(cs CompiledStep, want []authoring.StepID) bool {
	if len(cs.Header.Dependencies) != len(want) {
		return false
	}
	for i, d := range cs.Header.Dependencies {
		if d != want[i] {
			return false
		}
	}
	return true
}

// TestInputsEqualDeps：typed source bindings 集合与依赖集合精确相等
// （spike 拍板不变量：删依赖必被拒，与 submit 的 source bindings 校验同源）。
func TestInputsEqualDeps(t *testing.T) {
	t.Run("binding from non-dependency", func(t *testing.T) {
		root := mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse")
		other := mustLocal(header("entry.other", "entry"), ioWith(), "engine.entry.parse")
		io := ioWith("entry.root", "entry.other") // other 不是依赖
		m, err := authoring.NewLocalStep(header("entry.m", "entry", "entry.root"), io,
			authoring.LocalSpec{Handler: "engine.entry.parse"})
		if err != nil {
			t.Fatalf("construct: %v", err)
		}
		_, err = Compile(defWith(root, other, m), goldenRegistry(t))
		wantErr(t, err, `input binding source "entry.other" is not a dependency`)
	})
	t.Run("dependency without binding", func(t *testing.T) {
		root := mustLocal(header("entry.root", "entry"), ioWith(), "engine.entry.parse")
		other := mustLocal(header("entry.other", "entry"), ioWith(), "engine.entry.parse")
		io := ioWith("entry.root") // entry.other 是依赖但无 binding
		m, err := authoring.NewLocalStep(header("entry.m", "entry", "entry.root", "entry.other"), io,
			authoring.LocalSpec{Handler: "engine.entry.parse"})
		if err != nil {
			t.Fatalf("construct: %v", err)
		}
		_, err = Compile(defWith(root, other, m), goldenRegistry(t))
		wantErr(t, err, `dependency "entry.other" has no typed input binding`)
	})
}
