package authoring

// 封闭枚举。除注明外，零值一律非法：必填枚举缺失由 constructor 拒绝
// （Go 无编译器穷举的 sum type，constructor error 是可达的最强拦截层）。

// DecisionAuthority 是执行责任的第一维（master-requirements §5.3）：谁拥有
// 该步输出的判断权。由步骤变体派生物化，作者没有填写入口。
type DecisionAuthority string

const (
	AuthorityEngine DecisionAuthority = "ENGINE"
	AuthorityAgent  DecisionAuthority = "AGENT"
	AuthorityHuman  DecisionAuthority = "HUMAN"
)

// RunnerKind 是执行责任的第二维（§5.3）：该步在哪里、以何种可靠性边界执行。
// HOST 只表示外部能力/执行位置，不拥有流程决定权；不存在 CONTROL 值，
// 并行控制步物化为 ENGINE/ENGINE_LOCAL（spike 结论已拍板）。
type RunnerKind string

const (
	RunnerEngineLocal     RunnerKind = "ENGINE_LOCAL"
	RunnerDurableActivity RunnerKind = "DURABLE_ACTIVITY"
	RunnerHostAdapter     RunnerKind = "HOST_ADAPTER"
	RunnerAgentWorker     RunnerKind = "AGENT_WORKER"
)

// NonProgrammableReason 是 AGENT 步骤的封闭理由枚举（§5.4）。
// 零值与任何其他字符串（包括"实现麻烦"）一律非法。
type NonProgrammableReason string

const (
	// ReasonSemanticJudgment：证据含义、影响范围、冲突意图等无法由确定性
	// 规则完备判断。
	ReasonSemanticJudgment NonProgrammableReason = "SEMANTIC_JUDGMENT"
	// ReasonCreativeImplementation：在已确认范围内设计或编辑代码、测试、文档。
	ReasonCreativeImplementation NonProgrammableReason = "CREATIVE_IMPLEMENTATION"
	// ReasonIndependentReview：需要新鲜、隔离的产品/技术/QA/门审判断。
	ReasonIndependentReview NonProgrammableReason = "INDEPENDENT_REVIEW"
)

// Valid 报告 r 是否属于三值封闭集合。
func (r NonProgrammableReason) Valid() bool {
	return r == ReasonSemanticJudgment || r == ReasonCreativeImplementation || r == ReasonIndependentReview
}

// HostBoundaryReason 是可执行 HOST_ADAPTER 步骤的封闭边界枚举（§5.6）。
// MISSING_ENGINE_ADAPTER 刻意不在其中：它是 diagnostic-only definition
// marker，不是 runner reason。
type HostBoundaryReason string

const (
	BoundaryExternalCapability HostBoundaryReason = "EXTERNAL_CAPABILITY_BOUNDARY"
	BoundaryUserIOTransport    HostBoundaryReason = "USER_IO_TRANSPORT"
	BoundaryAgentDispatchAPI   HostBoundaryReason = "AGENT_DISPATCH_API"
)

// Valid 报告 r 是否属于三值封闭集合。
func (r HostBoundaryReason) Valid() bool {
	return r == BoundaryExternalCapability || r == BoundaryUserIOTransport || r == BoundaryAgentDispatchAPI
}

// IdempotencyKeyStrategy 是 DurableStep 必填的幂等键策略（§5.7 副作用类拒绝
// 的 constructor 拦截层）。
type IdempotencyKeyStrategy string

const (
	IdempotencyDeterministicInput IdempotencyKeyStrategy = "DETERMINISTIC_INPUT"
	IdempotencyTaskKeyScoped      IdempotencyKeyStrategy = "TASK_KEY_SCOPED"
)

// Valid 报告 s 是否属于封闭集合。
func (s IdempotencyKeyStrategy) Valid() bool {
	return s == IdempotencyDeterministicInput || s == IdempotencyTaskKeyScoped
}

// JoinMode 是并行组 join 策略模式。
type JoinMode string

const (
	JoinAll JoinMode = "ALL"
	JoinAny JoinMode = "ANY"
)

// Valid 报告 m 是否属于封闭集合。
func (m JoinMode) Valid() bool { return m == JoinAll || m == JoinAny }

// ParallelFailureMode 是并行组失败策略模式。
type ParallelFailureMode string

const (
	FailFast ParallelFailureMode = "FAIL_FAST"
	WaitAll  ParallelFailureMode = "WAIT_ALL"
)

// Valid 报告 m 是否属于封闭集合。
func (m ParallelFailureMode) Valid() bool { return m == FailFast || m == WaitAll }

// FailureClass 是固定失败分类（§5.8）：引擎失败不得静默或动态降级给代理/LLM。
type FailureClass string

const (
	FailureTransientEngine    FailureClass = "TRANSIENT_ENGINE_ERROR"
	FailureBusinessReject     FailureClass = "BUSINESS_REJECT"
	FailureUserActionRequired FailureClass = "USER_ACTION_REQUIRED"
	FailureSideEffectUnknown  FailureClass = "SIDE_EFFECT_UNKNOWN"
	FailureInvariantViolation FailureClass = "INVARIANT_VIOLATION"
	FailureBlockedBug         FailureClass = "BLOCKED_BUG"
	FailureAgentRecoverable   FailureClass = "AGENT_RECOVERABLE_SEMANTIC_ERROR"
)

// Valid 报告 f 是否属于七值封闭集合。
func (f FailureClass) Valid() bool {
	switch f {
	case FailureTransientEngine, FailureBusinessReject, FailureUserActionRequired,
		FailureSideEffectUnknown, FailureInvariantViolation, FailureBlockedBug,
		FailureAgentRecoverable:
		return true
	}
	return false
}
