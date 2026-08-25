// Package authoring 是编排定义的唯一编写形态：六种封闭步骤变体 + constructor
// + 公共头/共享 IO 段/变体 payload 三段式结构（ADR-001，spike 结论已拍板）。
//
// 封闭性与拦截分层：
//   - Step 接口含非导出方法 seal()，外部包无法新增变体；
//   - 每个变体只声明自己适用的字段：LocalStep 没有 join/reason/receipt 字段，
//     HumanAskStep 没有 handler/retry/timeout/codec 字段，ParallelStep 没有
//     handler 字段——字段级非法组合在编译期不可表示；
//   - 枚举值缺失（如 AGENT 缺 nonProgrammableReason、DurableStep 缺
//     reconcile）无法用类型消除，由 constructor 返回 error 拒绝——这是 Go
//     下可达的最强拦截层；
//   - DecisionAuthority/RunnerKind 由变体派生物化，authoring 类型上不存在
//     作者填写入口，写不出 HumanAskStep + ENGINE 之类的非法组合；
//   - 变体零值与绕过 constructor 的直接构造不在正常 authoring API 约定内，
//     后续 compiler 批次对零值步骤一律拒绝。
//
// 本包只做类型层：registry 解析、全局图不变量、ordinal 派生与 canonical
// 编码属后续批次。
package authoring

import "time"

// Step 是六种封闭步骤变体的接口。非导出方法 seal() 使外部包不能实现新变体；
// Authority/RunnerKind 是变体的派生物，经接口统一暴露但不可由作者提供。
type Step interface {
	seal()
	// Authority 返回该变体派生的判断权维度。
	Authority() DecisionAuthority
	// RunnerKind 返回该变体派生的执行边界维度。
	RunnerKind() RunnerKind
}

// Header 是所有变体共享的公共头（authoring 段）。compiled IR 中的公共头另含
// compiler 派生的 ordinal/kind 与物化的 authority/runner，均不在 authoring
// 入参中——assembly 顺序不得泄漏进制品。
type Header struct {
	ID                StepID
	NodeID            NodeID
	Dependencies      []StepID
	DefinitionVersion DefinitionVersion
}

// PredicateRef 只能引用封闭 registry 的 predicate ID；定义结构上不存在自然
// 语言 pre/postcondition 字段（八类拒绝中"自然语言-only pre/postcondition"
// 的结构性拦截）。
type PredicateRef struct {
	ID      PredicateID
	Negated bool
}

// InputBinding 是 typed source binding：把依赖步骤的输出字段绑定到本步输入
// 字段。后续 compiler 批次强制非 parallel/human 步骤的 {Inputs[i].From} 集合
// 与 Dependencies 集合精确相等。
type InputBinding struct {
	From        StepID
	OutputField string
	ToField     string
}

// IO 是共享 IO 段，仅 local/durable/host_action/agent 四个可执行变体嵌入。
// human 的 typed I/O 是 payload 内的 request/response schema，parallel 是纯
// 调度语义，二者不嵌入 IO。
type IO struct {
	InputCodec     CodecID
	OutputCodec    CodecID
	Preconditions  []PredicateRef
	Postconditions []PredicateRef
	Inputs         []InputBinding
}

// RetryPolicy 是声明式机械重试策略（仅 TRANSIENT_ENGINE_ERROR 类失败适用，
// §5.8）。MaxAttempts >= 1；MaxAttempts == 1 表示不重试。
type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

// JoinPolicy 是并行组的 fan-in 策略：join 步骤 + 聚合模式。JoinStep 必须是
// parallel 组之外的分立步骤（不得同时是 children 成员）。
type JoinPolicy struct {
	JoinStep StepID
	Mode     JoinMode
}

// FailurePolicy 是并行组的失败策略：支路失败时的调度模式与升级失败类。
type FailurePolicy struct {
	Mode     ParallelFailureMode
	Escalate FailureClass
}

// LocalStep 是纯内存、廉价、确定性的 engine 本地变换（§5.5）。
// 派生 ENGINE/ENGINE_LOCAL；无幂等/receipt/join/reason 字段。
type LocalStep struct {
	Header
	IO
	Handler HandlerID
	Timeout time.Duration // 可选；0 表示不设超时
	Retry   *RetryPolicy  // 可选
}

// DurableStep 是需要独立恢复/重试/幂等的持久化副作用步骤。
// 派生 ENGINE/DURABLE_ACTIVITY；幂等策略、reconcile、retry/timeout 全部必填
// （§5.7 副作用类拒绝的 constructor 拦截层，比 spike 更严：reconcile 无条件必填）。
type DurableStep struct {
	Header
	IO
	Handler     HandlerID
	Idempotency IdempotencyKeyStrategy
	Reconcile   ReconcileID
	Timeout     time.Duration // 必填 > 0
	Retry       RetryPolicy   // 必填；MaxAttempts >= 1
}

// HostActionStep 是经宿主 adapter 执行的程序化动作（§5.12：宿主只执行已签发
// 参数并回 receipt）。派生 ENGINE/HOST_ADAPTER；边界枚举与 operation 引用必填。
type HostActionStep struct {
	Header
	IO
	Handler   HandlerID
	Boundary  HostBoundaryReason
	Operation OperationID
	Schema    SchemaID
	Timeout   time.Duration // 必填 > 0
}

// AgentStep 是不可程序化语义工作步骤（§5.4）。派生 AGENT/AGENT_WORKER；
// 三值封闭理由枚举与 postcondition predicate 引用（worker result 合同）必填。
type AgentStep struct {
	Header
	IO
	Handler HandlerID
	Reason  NonProgrammableReason
	Timeout time.Duration // 必填 > 0
	Retry   *RetryPolicy  // 可选
}

// HumanAskStep 是用户 Ask 等待步骤。派生 HUMAN/HOST_ADAPTER；typed
// request/response schema 在 payload 内必填。无 handler/retry/timeout/IO 字段。
type HumanAskStep struct {
	Header
	AskKind        AskKindID // Ask 类型标识，非空；合法集合由 compiler 对 registry 的 askKind 槽位解析
	RequestSchema  SchemaID
	ResponseSchema SchemaID
	FreshnessTTL   time.Duration // 必填 > 0
}

// ParallelStep 是 fan-out/fan-in 控制步（无 handler，spike 结论已拍板）。
// 派生 ENGINE/ENGINE_LOCAL；join/failure 策略与 >= 2 个 children 必填。
type ParallelStep struct {
	Header
	Children []StepID
	Join     JoinPolicy
	Failure  FailurePolicy
}

// seal 实现集在每个变体上一行：外部包不能新增变体。
func (LocalStep) seal()      {}
func (DurableStep) seal()    {}
func (HostActionStep) seal() {}
func (AgentStep) seal()      {}
func (HumanAskStep) seal()   {}
func (ParallelStep) seal()   {}

// 派生物化：authority/runner 是每个变体类型的常量函数，作者无法影响。

func (LocalStep) Authority() DecisionAuthority { return AuthorityEngine }
func (LocalStep) RunnerKind() RunnerKind       { return RunnerEngineLocal }

func (DurableStep) Authority() DecisionAuthority { return AuthorityEngine }
func (DurableStep) RunnerKind() RunnerKind       { return RunnerDurableActivity }

func (HostActionStep) Authority() DecisionAuthority { return AuthorityEngine }
func (HostActionStep) RunnerKind() RunnerKind       { return RunnerHostAdapter }

func (AgentStep) Authority() DecisionAuthority { return AuthorityAgent }
func (AgentStep) RunnerKind() RunnerKind       { return RunnerAgentWorker }

func (HumanAskStep) Authority() DecisionAuthority { return AuthorityHuman }
func (HumanAskStep) RunnerKind() RunnerKind       { return RunnerHostAdapter }

func (ParallelStep) Authority() DecisionAuthority { return AuthorityEngine }
func (ParallelStep) RunnerKind() RunnerKind       { return RunnerEngineLocal }
