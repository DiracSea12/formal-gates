package compiler

import (
	"fmt"
	"sort"

	"formal-gates/internal/engine/authoring"
)

// Error 是 compiler 层的全部拒绝错误。Class 是机械失败分类
// （master-requirements §5.8）：结构性拒绝（图不变量、compiled IR 二次
// 防线、registry kind/runner 不匹配）为 INVARIANT_VIOLATION；仅"registry
// ID 未注册"在正常 compile 下路由为 BLOCKED_BUG——MISSING_ENGINE_ADAPTER
// 场景，正常 compile/drive 必须以 BLOCKED_BUG 拒绝并 diagnose（§5.4）。
type Error struct {
	Class authoring.FailureClass
	Msg   string
}

func (e *Error) Error() string { return e.Msg }

// invariantError 构造 INVARIANT_VIOLATION 分类的拒绝。
func invariantError(format string, args ...any) *Error {
	return &Error{Class: authoring.FailureInvariantViolation, Msg: "compile: " + fmt.Sprintf(format, args...)}
}

// blockedBugError 构造 BLOCKED_BUG 分类的拒绝（MISSING_ENGINE_ADAPTER 路由）。
func blockedBugError(format string, args ...any) *Error {
	return &Error{Class: authoring.FailureBlockedBug, Msg: "compile: " + fmt.Sprintf(format, args...)}
}

// Definition 是编译输入：definition 版本信封 + 入口节点 + 显式步骤表
// （ADR-001：显式节点/步骤表是唯一定义编写形态的表段，由 authoring 变体
// 构成）。Steps 的顺序只是 assembly 顺序，不进入编译产物——ordinal 由
// compiler 按确定性拓扑序派生，assembly 顺序不得泄漏进制品（spike 结论）。
type Definition struct {
	Version   authoring.DefinitionVersion
	EntryNode authoring.NodeID
	Steps     []authoring.Step
}

// Diagnostic 是一条 MISSING_ENGINE_ADAPTER 诊断：步骤 Step 引用的 registry
// ID Ref 在期望槽位 Want 上未注册（closed world not found）。
type Diagnostic struct {
	Step authoring.StepID
	Ref  string
	Want EntryKind
}

// DiagnosticResult 是 diagnostic-only 编译的产物：带 marker 的编译结果 +
// 全部诊断。Diagnostics 非空时 Definition.MissingEngineAdapter 恒为 true。
type DiagnosticResult struct {
	Definition  *CompiledDefinition
	Diagnostics []Diagnostic
}

// Compile 以正常模式编译：全部图不变量 + compiled IR 二次防线 + registry
// 完备解析。任一 registry ID 未注册时以 BLOCKED_BUG 拒绝签发——
// MISSING_ENGINE_ADAPTER 不是可执行定义的合法状态，正常 compile 不产出
// marker，只拒绝。
func Compile(def *Definition, reg *Registry) (*CompiledDefinition, error) {
	cd, diags, err := compile(def, reg, false)
	if err != nil {
		return nil, err
	}
	if len(diags) > 0 {
		d := diags[0]
		return nil, blockedBugError(`step %q: MISSING_ENGINE_ADAPTER: %s id %q not registered (closed world); use diagnostic compile for the technical-debt marker`, d.Step, d.Want, d.Ref)
	}
	return cd, nil
}

// CompileDiagnostic 以开发期 diagnostic-only 模式编译：结构性校验（图不变量、
// IR 二次防线、kind/runner 匹配）照常硬拒绝；仅"registry ID 未注册"不再
// 失败，而是记为 MISSING_ENGINE_ADAPTER 诊断并在产物上物化 marker
// （§5.4 技术债标记）。带 marker 的定义可加载诊断、不可执行。
func CompileDiagnostic(def *Definition, reg *Registry) (*DiagnosticResult, error) {
	cd, diags, err := compile(def, reg, true)
	if err != nil {
		return nil, err
	}
	return &DiagnosticResult{Definition: cd, Diagnostics: diags}, nil
}

// compile 是两条公开路径的共用管线。阶段顺序刻意安排，使每类非法定义先
// 命中最具体的错误（信息含具体位置）：输入信封 → 单步物化（头部/版本绑定/
// 引用形态归一化）→ 引用存在 → 循环（定序）→ 可达性 → 并行组 → IR 二次
// 防线 → registry 解析（唯一按模式路由的阶段）→ 定序落位 + 稳定排序。
func compile(def *Definition, reg *Registry, diagnostic bool) (*CompiledDefinition, []Diagnostic, error) {
	if def == nil {
		return nil, nil, invariantError("nil definition")
	}
	if reg == nil {
		return nil, nil, invariantError("nil registry")
	}
	if def.Version == "" {
		return nil, nil, invariantError("definition version required")
	}
	if def.EntryNode == "" {
		return nil, nil, invariantError("entry node required")
	}
	if len(def.Steps) == 0 {
		return nil, nil, invariantError("definition has no steps")
	}

	steps := make([]CompiledStep, 0, len(def.Steps))
	index := make(map[authoring.StepID]int, len(def.Steps))
	for i, s := range def.Steps {
		if s == nil {
			return nil, nil, invariantError("steps[%d]: nil step", i)
		}
		cs, err := materializeStep(def, s)
		if err != nil {
			return nil, nil, err
		}
		if _, dup := index[cs.Header.ID]; dup {
			return nil, nil, invariantError("duplicate step id %q", cs.Header.ID)
		}
		index[cs.Header.ID] = len(steps)
		steps = append(steps, cs)
	}
	if err := checkRefs(steps, index); err != nil {
		return nil, nil, err
	}
	ordinals, err := computeOrdinals(steps)
	if err != nil {
		return nil, nil, err
	}
	if err := checkReachable(def.EntryNode, steps); err != nil {
		return nil, nil, err
	}
	if err := checkParallelGroups(steps, index); err != nil {
		return nil, nil, err
	}
	for i := range steps {
		if err := validateStepIR(steps[i]); err != nil {
			return nil, nil, err
		}
		if err := checkInputsEqualDeps(steps[i]); err != nil {
			return nil, nil, err
		}
	}
	rc := &resolveCtx{reg: reg, diagnostic: diagnostic}
	for i := range steps {
		if err := rc.resolveStepRefs(&steps[i]); err != nil {
			return nil, nil, err
		}
	}

	cd := &CompiledDefinition{Version: def.Version, EntryNode: def.EntryNode, MissingEngineAdapter: len(rc.diags) > 0}
	for i := range steps {
		steps[i].Header.Ordinal = ordinals[steps[i].Header.ID]
	}
	sort.Slice(steps, func(i, j int) bool {
		a, b := steps[i].Header, steps[j].Header
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.Ordinal != b.Ordinal {
			return a.Ordinal < b.Ordinal
		}
		return a.ID < b.ID
	})
	cd.Steps = steps
	return cd, rc.diags, nil
}

// materializeStep 把一个 authoring 变体物化为 compiled IR：公共头携带变体
// 派生的 kind 与 authority/runner（接口常量函数，作者无填写入口）；依赖与
// children 去重排序；共享 IO 段拷贝排序。零值/绕过 constructor 的步骤在此
// 只做头部与引用形态检查——payload 必填项由 validateStepIR 二次防线拒绝
// （enforcement matrix：constructor 主拦、compiler 二次防线）。
func materializeStep(def *Definition, s authoring.Step) (CompiledStep, error) {
	switch v := s.(type) {
	case authoring.LocalStep:
		h, err := compiledHeader(def, v.Header, KindLocal, v)
		if err != nil {
			return CompiledStep{}, err
		}
		return CompiledStep{Header: h, IO: normalizeIO(v.IO),
			Payload: CompiledLocalStep{Handler: v.Handler, Timeout: v.Timeout, Retry: v.Retry}}, nil
	case authoring.DurableStep:
		h, err := compiledHeader(def, v.Header, KindDurable, v)
		if err != nil {
			return CompiledStep{}, err
		}
		return CompiledStep{Header: h, IO: normalizeIO(v.IO),
			Payload: CompiledDurableStep{Handler: v.Handler, Idempotency: v.Idempotency,
				Reconcile: v.Reconcile, Timeout: v.Timeout, Retry: v.Retry}}, nil
	case authoring.HostActionStep:
		h, err := compiledHeader(def, v.Header, KindHostAction, v)
		if err != nil {
			return CompiledStep{}, err
		}
		return CompiledStep{Header: h, IO: normalizeIO(v.IO),
			Payload: CompiledHostActionStep{Handler: v.Handler, Boundary: v.Boundary,
				Operation: v.Operation, Schema: v.Schema, Timeout: v.Timeout}}, nil
	case authoring.AgentStep:
		h, err := compiledHeader(def, v.Header, KindAgent, v)
		if err != nil {
			return CompiledStep{}, err
		}
		return CompiledStep{Header: h, IO: normalizeIO(v.IO),
			Payload: CompiledAgentStep{Handler: v.Handler, Reason: v.Reason,
				Timeout: v.Timeout, Retry: v.Retry}}, nil
	case authoring.HumanAskStep:
		h, err := compiledHeader(def, v.Header, KindHumanAsk, v)
		if err != nil {
			return CompiledStep{}, err
		}
		// human 变体结构上不存在共享 IO 段：typed I/O 就是 payload 内的
		// request/response schema，编译不写入 IO。
		return CompiledStep{Header: h,
			Payload: CompiledHumanAskStep{AskKind: v.AskKind, RequestSchema: v.RequestSchema,
				ResponseSchema: v.ResponseSchema, FreshnessTTL: v.FreshnessTTL}}, nil
	case authoring.ParallelStep:
		h, err := compiledHeader(def, v.Header, KindParallel, v)
		if err != nil {
			return CompiledStep{}, err
		}
		children, err := normalizeIDs(v.Header.ID, "child", v.Children)
		if err != nil {
			return CompiledStep{}, err
		}
		// parallel 是纯调度语义：无 handler、无 codec，不物化 IO。
		return CompiledStep{Header: h,
			Payload: CompiledParallelStep{Children: children, Join: v.Join, Failure: v.Failure}}, nil
	default:
		// seal() 使外部包不能实现新变体；走到这里只可能是 nil 接口、
		// 未知实现或 typed-nil 指针（值类型变体的指针不匹配任何 case）。
		return CompiledStep{}, invariantError("unknown step variant %T", s)
	}
}

// compiledHeader 构造公共头并做头部级校验：id/nodeId 非空、definition
// version 与信封一致绑定（八类拒绝之一的主拦截层之一）、依赖去重排序。
// authority/runner 经变体接口派生物化。
func compiledHeader(def *Definition, h authoring.Header, kind StepKind, s authoring.Step) (CompiledHeader, error) {
	if h.ID == "" {
		return CompiledHeader{}, invariantError("step: id required")
	}
	if h.NodeID == "" {
		return CompiledHeader{}, invariantError("step %q: node id required", h.ID)
	}
	if h.DefinitionVersion != def.Version {
		return CompiledHeader{}, invariantError(`step %q: definitionVersion %q != definition %q (unbound definition version)`, h.ID, h.DefinitionVersion, def.Version)
	}
	deps, err := normalizeIDs(h.ID, "dependency", h.Dependencies)
	if err != nil {
		return CompiledHeader{}, err
	}
	return CompiledHeader{
		ID: h.ID, NodeID: h.NodeID, Kind: kind, DefinitionVersion: def.Version,
		Dependencies: deps, Authority: s.Authority(), Runner: s.RunnerKind(),
	}, nil
}

// normalizeIDs 去重、排序一组步骤 ID 引用，拒绝空 ID 与自引用；返回新
// 切片（不改写调用方入参），空集归一化为 nil。
func normalizeIDs(self authoring.StepID, what string, in []authoring.StepID) ([]authoring.StepID, error) {
	seen := make(map[authoring.StepID]bool, len(in))
	var out []authoring.StepID
	for _, id := range in {
		if id == "" {
			return nil, invariantError(`step %q: empty %s id`, self, what)
		}
		if id == self {
			return nil, invariantError(`step %q: %s references the step itself`, self, what)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// normalizeIO 拷贝并排序共享 IO 段：pre/postcondition 按（ID，非否定在前），
// inputs 按（来源步骤，目标字段）。空集归一化为 nil；不 dedup（去重只约定
// 作用于依赖与 children）。
func normalizeIO(io authoring.IO) authoring.IO {
	out := authoring.IO{InputCodec: io.InputCodec, OutputCodec: io.OutputCodec}
	out.Preconditions = sortPredicateRefs(io.Preconditions)
	out.Postconditions = sortPredicateRefs(io.Postconditions)
	if len(io.Inputs) > 0 {
		in := make([]authoring.InputBinding, len(io.Inputs))
		copy(in, io.Inputs)
		sort.Slice(in, func(i, j int) bool {
			if in[i].From != in[j].From {
				return in[i].From < in[j].From
			}
			return in[i].ToField < in[j].ToField
		})
		out.Inputs = in
	}
	return out
}

func sortPredicateRefs(refs []authoring.PredicateRef) []authoring.PredicateRef {
	if len(refs) == 0 {
		return nil
	}
	out := make([]authoring.PredicateRef, len(refs))
	copy(out, refs)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return !out[i].Negated && out[j].Negated
	})
	return out
}
