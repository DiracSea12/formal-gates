package compiler

import (
	"time"

	"formal-gates/internal/engine/authoring"
)

// CompiledDefinition IR（ADR-001 决策 4/5、spike §1 拍板的三段式）：编译输出
// 是具体结构——公共头（compiler 物化段）+ 共享 IO 段 + 封闭变体 payload。
// IR 不含函数、闭包、内存地址、绝对路径或无序 map：执行引用一律是封闭
// registry 中的稳定 ID。
//
// canonical 编码与制品生成属批次 1c，本文件只定义 IR 结构本身。

// StepKind 是编译产物的六值封闭步骤种类，由 authoring 变体类型派生。
// 与 authority/runner 一样不存在作者填写入口。
type StepKind string

const (
	KindLocal      StepKind = "LOCAL"
	KindDurable    StepKind = "DURABLE"
	KindHostAction StepKind = "HOST_ACTION"
	KindAgent      StepKind = "AGENT"
	KindHumanAsk   StepKind = "HUMAN_ASK"
	KindParallel   StepKind = "PARALLEL"
)

// CompiledHeader 是所有变体相同的公共头（制品中物化段）。ordinal 由 compiler
// 按确定性拓扑序派生，authoring API 不暴露该字段；authority/runner 由变体
// 派生物化；definitionVersion 恒等于 definition 信封版本（一致绑定）。
type CompiledHeader struct {
	ID                authoring.StepID
	NodeID            authoring.NodeID
	Ordinal           int
	Kind              StepKind
	DefinitionVersion authoring.DefinitionVersion
	Dependencies      []authoring.StepID          // 编译期去重排序
	Authority         authoring.DecisionAuthority // 变体派生物化
	Runner            authoring.RunnerKind        // 变体派生物化
}

// Payload 是封闭的 compiled 变体 payload 接口：外部包不能伪造变体 IR。
// 变体判别只存在于 compiler 的 payload switch（spike 结论）。
type Payload interface{ payloadSeal() }

// CompiledLocalStep 是 local 变体 payload：handler 必填；timeout 可选
// （0 表示不设超时）；retry 可选（nil 表示不重试）。
type CompiledLocalStep struct {
	Handler authoring.HandlerID
	Timeout time.Duration
	Retry   *authoring.RetryPolicy
}

// CompiledDurableStep 是 durable 变体 payload：幂等策略、reconcile、
// timeout、retry 全部必填（副作用步骤没有无幂等/无 reconcile 形态）。
type CompiledDurableStep struct {
	Handler     authoring.HandlerID
	Idempotency authoring.IdempotencyKeyStrategy
	Reconcile   authoring.ReconcileID
	Timeout     time.Duration
	Retry       authoring.RetryPolicy
}

// CompiledHostActionStep 是 host_action 变体 payload：边界枚举与注册的
// operation 引用必填；宿主只执行已签发参数并回 receipt。
type CompiledHostActionStep struct {
	Handler   authoring.HandlerID
	Boundary  authoring.HostBoundaryReason
	Operation authoring.OperationID
	Schema    authoring.SchemaID
	Timeout   time.Duration
}

// CompiledAgentStep 是 agent 变体 payload：三值封闭理由枚举必填；
// worker result 合同由共享 IO 段的 postcondition predicate 承载。
type CompiledAgentStep struct {
	Handler authoring.HandlerID
	Reason  authoring.NonProgrammableReason
	Timeout time.Duration
	Retry   *authoring.RetryPolicy
}

// CompiledHumanAskStep 是 human_ask 变体 payload：typed request/response
// schema 与 registry 中的 ask 类型必填。无 handler/retry/timeout/共享 IO
// 段——human 的 typed I/O 就是 payload 内的 schema。
type CompiledHumanAskStep struct {
	AskKind        authoring.AskKindID
	RequestSchema  authoring.SchemaID
	ResponseSchema authoring.SchemaID
	FreshnessTTL   time.Duration
}

// CompiledParallelStep 是 parallel 变体 payload：纯调度语义，无 handler、
// 无 codec。join 步骤必须是组外分立步骤；children 覆盖完整 fan-out 由
// 图校验强制。
type CompiledParallelStep struct {
	Children []authoring.StepID
	Join     authoring.JoinPolicy
	Failure  authoring.FailurePolicy
}

// payloadSeal 实现集在每个变体上一行：外部包不能新增 compiled 变体。
func (CompiledLocalStep) payloadSeal()      {}
func (CompiledDurableStep) payloadSeal()    {}
func (CompiledHostActionStep) payloadSeal() {}
func (CompiledAgentStep) payloadSeal()      {}
func (CompiledHumanAskStep) payloadSeal()   {}
func (CompiledParallelStep) payloadSeal()   {}

// CompiledStep 是单个编译后步骤：公共头 + 共享 IO 段 + 变体 payload。
// IO 仅 local/durable/host_action/agent 四个可执行变体使用；human_ask 与
// parallel 的 IO 恒为零值（物化时就不写入）。
type CompiledStep struct {
	Header  CompiledHeader
	IO      authoring.IO
	Payload Payload
}

// CompiledDefinition 是编译输出。Steps 按 (nodeID, ordinal, id) 稳定排序：
// 输出顺序是编译产物性质的函数，与输入 assembly 顺序无关。
//
// MissingEngineAdapter 是 diagnostic-only marker（master-requirements §5.4）：
// 仅 CompileDiagnostic 可产出 true（registry 存在未注册 ID）。携带该 marker
// 的定义不得进入 executable plan、不得签发 Ready/HostAction；正常 Compile
// 从不签发它——未完整实现的定义以 BLOCKED_BUG 拒绝。
type CompiledDefinition struct {
	Version              authoring.DefinitionVersion
	EntryNode            authoring.NodeID
	MissingEngineAdapter bool
	Steps                []CompiledStep
}
