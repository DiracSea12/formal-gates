// helpers_test.go 是验收套件（阶段 1 批 3）的共用夹具：仓库根定位、真实
// 定义的编译/编码通道、封闭 registry 的可裁剪复刻，以及对 authoring 值类型
// 变体的公共头读写工具。变异类用例需要按条目控制注册表与绕过 constructor
// 改写步骤，definition/authoring 包不为测试提供这些入口，故在此复刻。
package acceptance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/encoder"
)

// repoRoot 返回仓库根（本包测试 CWD 是 internal/engine/acceptance）；go.mod
// 存在性校验防止在错误目录下静默通过。
func repoRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	return root
}

// wantErr 断言错误非 nil 且消息含 distinguishing 子串（命中目标分支而非
// 偶然的其他错误）。
func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("want error containing %q, got %q", substr, err.Error())
	}
}

// compileWorkflow 编译给定定义并编码为 canonical 制品字节，返回 (IR, 字节,
// digest)。任一步失败即用例夹具自身缺陷，直接 Fatal；"必须拒绝"类断言都在
// 调用方对变异副本单独 Compile。
func compileWorkflow(t *testing.T, def *compiler.Definition, reg *compiler.Registry) (*compiler.CompiledDefinition, []byte, string) {
	t.Helper()
	cd, err := compiler.Compile(def, reg)
	if err != nil {
		t.Fatalf("compile workflow: %v", err)
	}
	data, err := encoder.Encode(cd)
	if err != nil {
		t.Fatalf("encode workflow: %v", err)
	}
	return cd, data, encoder.Digest(data)
}

// workflowFixtureHandlers 复刻 definition/workflow.go 的 handler 注册表
// （同 ID、同 runner）。registry 完备性用例需要按条目移除/错位注册，
// definition.Registry 不提供裁剪入口；复刻与真实注册表的等价性由
// TestAcceptanceRegistryCompleteness 的完整行钉死（无 skip 时产物 digest
// 必须等于 checked-in 身份常量），两处不会静默漂移。
var workflowFixtureHandlers = []struct {
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

// workflowRegistry 按复刻清单注册全部封闭 ID；skip 列出故意不注册的 ID
// （缺失/kind 错用用例）。
func workflowRegistry(t *testing.T, skip ...string) *compiler.Registry {
	t.Helper()
	skipSet := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipSet[s] = true
	}
	reg := compiler.NewRegistry()
	for _, h := range workflowFixtureHandlers {
		if skipSet[string(h.id)] {
			continue
		}
		if err := reg.RegisterHandler(h.id, h.runner); err != nil {
			t.Fatalf("register handler %q: %v", h.id, err)
		}
	}
	for _, id := range []authoring.CodecID{"codec.any.in", "codec.any.out"} {
		if !skipSet[string(id)] {
			if err := reg.RegisterCodec(id); err != nil {
				t.Fatalf("register codec %q: %v", id, err)
			}
		}
	}
	for _, id := range []authoring.PredicateID{"pred.review.post"} {
		if !skipSet[string(id)] {
			if err := reg.RegisterPredicate(id); err != nil {
				t.Fatalf("register predicate %q: %v", id, err)
			}
		}
	}
	for _, id := range []authoring.ReconcileID{"reconcile.entry.persist"} {
		if !skipSet[string(id)] {
			if err := reg.RegisterReconciler(id); err != nil {
				t.Fatalf("register reconciler %q: %v", id, err)
			}
		}
	}
	for _, id := range []authoring.SchemaID{"schema.ask.decision.request", "schema.ask.decision.response"} {
		if !skipSet[string(id)] {
			if err := reg.RegisterSchema(id); err != nil {
				t.Fatalf("register schema %q: %v", id, err)
			}
		}
	}
	for _, id := range []authoring.OperationID{"op.fan.transport"} {
		if !skipSet[string(id)] {
			if err := reg.RegisterOperation(id); err != nil {
				t.Fatalf("register operation %q: %v", id, err)
			}
		}
	}
	return reg
}

// augmentedRegistry 在完整注册表之上追加 digest 敏感性用例需要的变异 ID
// （engine.entry.parse.alt、schema.ask.decision.request.alt——变异后的定义
// 仍须编译成功才谈得上 digest 对比）。多出的未引用条目不进制品字节，
// 该分离由 TestAcceptanceDefinitionDigestSeparatedFromImplementationSide
// 单独证明。
func augmentedRegistry(t *testing.T) *compiler.Registry {
	t.Helper()
	reg := workflowRegistry(t)
	if err := reg.RegisterHandler("engine.entry.parse.alt", authoring.RunnerEngineLocal); err != nil {
		t.Fatalf("register alt handler: %v", err)
	}
	if err := reg.RegisterSchema("schema.ask.decision.request.alt"); err != nil {
		t.Fatalf("register alt schema: %v", err)
	}
	return reg
}

// stepHeader 经变体 type switch 读公共头（authoring.Step 不暴露 Header
// 访问器；六变体都嵌入 authoring.Header）。
func stepHeader(s authoring.Step) (authoring.Header, bool) {
	switch v := s.(type) {
	case authoring.LocalStep:
		return v.Header, true
	case authoring.DurableStep:
		return v.Header, true
	case authoring.HostActionStep:
		return v.Header, true
	case authoring.AgentStep:
		return v.Header, true
	case authoring.HumanAskStep:
		return v.Header, true
	case authoring.ParallelStep:
		return v.Header, true
	}
	return authoring.Header{}, false
}

// editStepHeader 对指定步骤的公共头施加 fn 后写回（Steps 以值类型变体持有，
// 读出的接口值是副本，必须整体写回才生效）；未知变体返回 false。
func editStepHeader(d *compiler.Definition, i int, fn func(*authoring.Header)) bool {
	switch s := d.Steps[i].(type) {
	case authoring.LocalStep:
		fn(&s.Header)
		d.Steps[i] = s
	case authoring.DurableStep:
		fn(&s.Header)
		d.Steps[i] = s
	case authoring.HostActionStep:
		fn(&s.Header)
		d.Steps[i] = s
	case authoring.AgentStep:
		fn(&s.Header)
		d.Steps[i] = s
	case authoring.HumanAskStep:
		fn(&s.Header)
		d.Steps[i] = s
	case authoring.ParallelStep:
		fn(&s.Header)
		d.Steps[i] = s
	default:
		return false
	}
	return true
}

// stepIndex 返回定义表中指定步骤的下标。
func stepIndex(t *testing.T, d *compiler.Definition, id string) int {
	t.Helper()
	for i, s := range d.Steps {
		if h, ok := stepHeader(s); ok && string(h.ID) == id {
			return i
		}
	}
	t.Fatalf("step %q not found in definition", id)
	return -1
}

// dropDep 从指定步骤的公共头依赖中移除一个 ID 并写回；返回是否移除成功。
func dropDep(d *compiler.Definition, i int, dep string) bool {
	removed := false
	ok := editStepHeader(d, i, func(h *authoring.Header) {
		out := h.Dependencies[:0]
		for _, x := range h.Dependencies {
			if string(x) == dep {
				removed = true
				continue
			}
			out = append(out, x)
		}
		h.Dependencies = out
	})
	return ok && removed
}

// dropBinding 从指定 local 步骤的 input bindings 中移除一个来源并写回；
// 返回是否移除成功（仅 local 变体需要：dependency 类变异只作用于 local 步）。
func dropBinding(d *compiler.Definition, i int, from string) bool {
	s, ok := d.Steps[i].(authoring.LocalStep)
	if !ok {
		return false
	}
	out := s.IO.Inputs[:0]
	removed := false
	for _, b := range s.IO.Inputs {
		if string(b.From) == from {
			removed = true
			continue
		}
		out = append(out, b)
	}
	s.IO.Inputs = out
	d.Steps[i] = s
	return removed
}
