package compiler

import (
	"fmt"

	"formal-gates/internal/engine/authoring"
)

// 封闭 registry（ADR-001 决策 3/9）：七类 registry ID（HandlerID/PredicateID/
// CodecID/ReconcileID/SchemaID/OperationID/AskKindID）共用单一 ID 命名空间。单一
// 命名空间的收益（spike 实证）：把 handler ID 填进 predicate 槽不是含糊的
// not found，而是立即暴露为 kind 不匹配，错误更早更准。
//
// 条目不携带函数/闭包：编译只做 ID 解析（存在、唯一、kind 匹配），实现绑定
// 属运行时批次；compiled IR 里 likewise 只有稳定 ID 引用。
//
// 命名建议沿用 spike 结论 domain.family.name（如 engine.persist.intent、
// reconcile.intent.persist、schema.ask.decision.request）。

// EntryKind 是 registry 条目的槽位种类。同一原始 ID 只能注册为其中一种；
// 跨槽位复用同一 ID 在注册期被拒（duplicate），槽位错用在解析期被拒
// （kind mismatch）。
type EntryKind string

const (
	KindHandler    EntryKind = "handler"
	KindPredicate  EntryKind = "predicate"
	KindCodec      EntryKind = "codec"
	KindReconciler EntryKind = "reconciler"
	KindSchema     EntryKind = "schema"
	KindOperation  EntryKind = "operation"
	KindAskKind    EntryKind = "askKind"
)

// entry 是单条注册。runner 仅对 handler 条目有意义：编译期用于 handler
// runner 与变体派生 runner 的匹配（跨 runner 换绑拒绝）。
type entry struct {
	kind   EntryKind
	runner authoring.RunnerKind
}

// Registry 是封闭 registry：NewRegistry 后逐条注册，注册完成即不可变。
// "executable definition 只有在同一候选包 registry 能完整、唯一解析其全部
// ID 时才能激活"（锁步激活）由 Compile 对每条引用逐一解析实现。
type Registry struct {
	entries map[string]entry
}

// NewRegistry 返回空 registry。
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]entry)}
}

func (r *Registry) register(id string, e entry) error {
	if id == "" {
		return fmt.Errorf("registry: empty id")
	}
	if _, dup := r.entries[id]; dup {
		// 同 ID 二次注册（无论同 kind 还是跨 kind 复用同一 ID）一律注册期
		// 拒绝：重复检测在注册期而非编译期。
		return fmt.Errorf("registry: duplicate id %q", id)
	}
	r.entries[id] = e
	return nil
}

// lookup 是唯一直读入口，供本包解析路径复用。
func (r *Registry) lookup(id string) (entry, bool) {
	e, ok := r.entries[id]
	return e, ok
}

// resolve 是解析核心：要求存在、唯一（注册期已保证不重复）、kind 匹配。
// 返回值 runner 仅对 handler 条目非零。
func (r *Registry) resolve(id string, want EntryKind) (authoring.RunnerKind, error) {
	e, ok := r.lookup(id)
	if !ok {
		return "", fmt.Errorf("registry: %s %q not found (closed world)", want, id)
	}
	if e.kind != want {
		return "", fmt.Errorf("registry: id %q registered as %s, want %s", id, e.kind, want)
	}
	return e.runner, nil
}

// runnerValid 报告 runner 是否属于四值封闭 RunnerKind 集合（authoring 包未为
// RunnerKind 提供 Valid——那里的枚举校验只覆盖 authoring 入参用到的枚举）。
func runnerValid(r authoring.RunnerKind) bool {
	switch r {
	case authoring.RunnerEngineLocal, authoring.RunnerDurableActivity,
		authoring.RunnerHostAdapter, authoring.RunnerAgentWorker:
		return true
	}
	return false
}

// RegisterHandler 注册可恢复执行合同。runner 必填且必须是合法 RunnerKind：
// handler 条目携带 runner 供编译期与变体派生 runner 匹配。
func (r *Registry) RegisterHandler(id authoring.HandlerID, runner authoring.RunnerKind) error {
	if !runnerValid(runner) {
		return fmt.Errorf("registry: handler %q: invalid runner kind %q", id, runner)
	}
	return r.register(string(id), entry{kind: KindHandler, runner: runner})
}

// RegisterPredicate 注册可执行 pre/postcondition predicate。
func (r *Registry) RegisterPredicate(id authoring.PredicateID) error {
	return r.register(string(id), entry{kind: KindPredicate})
}

// RegisterCodec 注册 typed input/output codec。
func (r *Registry) RegisterCodec(id authoring.CodecID) error {
	return r.register(string(id), entry{kind: KindCodec})
}

// RegisterReconciler 注册副作用 reconciler。
func (r *Registry) RegisterReconciler(id authoring.ReconcileID) error {
	return r.register(string(id), entry{kind: KindReconciler})
}

// RegisterSchema 注册 typed schema（如 human ask 的 request/response schema）。
func (r *Registry) RegisterSchema(id authoring.SchemaID) error {
	return r.register(string(id), entry{kind: KindSchema})
}

// RegisterOperation 注册定义可引用的 host adapter operation。它不是自由 shell
// 通道（master-requirements §5.12）：只有注册过的 operation 才能被
// HostActionStep 引用。
func (r *Registry) RegisterOperation(id authoring.OperationID) error {
	return r.register(string(id), entry{kind: KindOperation})
}

// RegisterAskKind 注册 human ask 的合法类型。Ask 类型不是自由字符串通道：
// HumanAskStep 引用的 askKind 必须在此注册，缺失按 MISSING_ENGINE_ADAPTER
// 路由（正常 compile 以 BLOCKED_BUG 拒绝）。
func (r *Registry) RegisterAskKind(id authoring.AskKindID) error {
	return r.register(string(id), entry{kind: KindAskKind})
}

// ResolveHandler 解析 handler ID，返回注册时声明的 runner。
func (r *Registry) ResolveHandler(id authoring.HandlerID) (authoring.RunnerKind, error) {
	return r.resolve(string(id), KindHandler)
}

// ResolvePredicate 解析 predicate ID。
func (r *Registry) ResolvePredicate(id authoring.PredicateID) error {
	_, err := r.resolve(string(id), KindPredicate)
	return err
}

// ResolveCodec 解析 codec ID。
func (r *Registry) ResolveCodec(id authoring.CodecID) error {
	_, err := r.resolve(string(id), KindCodec)
	return err
}

// ResolveReconciler 解析 reconciler ID。
func (r *Registry) ResolveReconciler(id authoring.ReconcileID) error {
	_, err := r.resolve(string(id), KindReconciler)
	return err
}

// ResolveSchema 解析 schema ID。
func (r *Registry) ResolveSchema(id authoring.SchemaID) error {
	_, err := r.resolve(string(id), KindSchema)
	return err
}

// ResolveOperation 解析 operation ID。
func (r *Registry) ResolveOperation(id authoring.OperationID) error {
	_, err := r.resolve(string(id), KindOperation)
	return err
}

// ResolveAskKind 解析 ask 类型 ID。
func (r *Registry) ResolveAskKind(id authoring.AskKindID) error {
	_, err := r.resolve(string(id), KindAskKind)
	return err
}
