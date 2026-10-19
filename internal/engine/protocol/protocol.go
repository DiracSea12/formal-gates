// Package protocol 是 engine 的提交协议内核。
//
// 在持久化基座（internal/engine/persistence）之上提供五件能力：
//
//   - 状态模型：engine 权威状态投影落账 expected 任务清单
//     （runtime.TaskKey）、每 TaskKey 的当前 Attempt（标识 + 签发绑定）、
//     pendingActions[actionID]（已签发 intent 参数）、pending Ask、已提交
//     决定与事件台账。决策视图直接内嵌 decision.State 复用其
//     CompleteStep/TransitionTask 派生与校验，不造平行概念。
//   - typed request/event/action：封闭 kind 集合 + constructor（参照
//     authoring 形态，非法形态不可构造）；提交路径逐事件校验未知 kind、
//     payload schema、当前节点归属（事件绑定的任务不在 expected 集即
//     非当前节点，可区分拒绝）。
//   - 幂等 submit：事件台账按 eventID 记录 payload digest 与 acceptance；
//     同 ID 同 digest 重放返回稳定 acceptance（不重复签发、revision 不再
//     +1）；同 ID 不同 digest 硬拒绝且零状态变化。
//   - freshness 校验：freshness token 确定性绑定 (当前 revision, requestID)；
//     任何新提交使旧 token 失效（STALE_FRESHNESS 拒绝、零状态变化），
//     当前 token 放行。
//   - 两阶段主动控制：受限 REQUEST_* 事件先创建 pending Ask（request ID
//     与选项集落账），用户以 request ID + 当前 freshness token submit 决定；
//     封闭 kind 集合中不存在自由 USER_* 直写。
//
// SpawnReceipt、worker result、用户决定、lifecycle 与 HostAction receipt
// 共用本包的接纳、失败路由和事务收尾。恢复与终止消费原 attempt 和外部
// effect 事实；不支持的主动控制在写入前拒绝。全部状态写入经 persistence
// 的信封、摘要、CAS 与指纹校验落盘。

package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/settings"
)

// RejectedError 是提交协议的全部可区分拒绝。Code 是封闭拒绝码，调用方
// 按码机械分类（与 persistence 的 typed error 并存：本包的业务拒绝用
// 本类型，持久化层的拒绝原样上抛）。
type RejectedError struct {
	Code   string
	Detail string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

// 封闭拒绝码。
const (
	// CodeUnknownEventKind：事件 kind 不在封闭集合（含一切自由 USER_* 直写）。
	CodeUnknownEventKind = "UNKNOWN_EVENT_KIND"
	// CodeEventSchemaInvalid：payload 不合 schema 或 tagged union 不变量破坏。
	CodeEventSchemaInvalid = "EVENT_SCHEMA_INVALID"
	// CodeDuplicateEventMismatch：同 eventID 不同 payload digest——硬拒绝。
	CodeDuplicateEventMismatch = "DUPLICATE_EVENT_DIGEST_MISMATCH"
	// CodeStaleFreshness：freshness token 已被后续提交取代。
	CodeStaleFreshness = "STALE_FRESHNESS"
	// CodeUnknownRequest：决定的 requestID 不存在。
	CodeUnknownRequest = "UNKNOWN_REQUEST"
	// CodeRequestResolved：request 已有决定，不得二次决定。
	CodeRequestResolved = "REQUEST_ALREADY_RESOLVED"
	// CodeInvalidChoice：决定选项不在 pending Ask 落账的选项集内。
	CodeInvalidChoice = "INVALID_CHOICE"
	// CodeEventNotCurrent：事件绑定的任务不在 expected 集（非当前节点）。
	CodeEventNotCurrent = "EVENT_NOT_CURRENT"
	// CodeStaleAttempt：事件携带的 attempt 不是该任务当前 Attempt。
	CodeStaleAttempt = "STALE_ATTEMPT"
	// CodeIllegalTransition：任务进度违反 runtime 任务状态机。
	CodeIllegalTransition = "ILLEGAL_TASK_TRANSITION"
	// CodeDuplicateAction：签发落账撞上已存在的 actionID。
	CodeDuplicateAction = "DUPLICATE_ACTION"
	// CodeAlreadyInitialized：run 状态已存在，不得重复初始化。
	CodeAlreadyInitialized = "ALREADY_INITIALIZED"
	// CodeNotInitialized：run 状态尚不存在。
	CodeNotInitialized = "NOT_INITIALIZED"
	// CodeProviderMismatch：事件/回执的 provider 身份与 run 绑定不同
	// （含空 provider）——硬拒绝，不降级 default。
	CodeProviderMismatch = "PROVIDER_MISMATCH"
	// CodeUnknownAction：回执/结果引用的 actionID 不存在（从未签发或
	// 已完结收回）。
	CodeUnknownAction = "UNKNOWN_ACTION"
	// CodeReceiptConflict：同一 actionID 出现字节不同的回执/结果——硬拒绝。
	CodeReceiptConflict = "RECEIPT_CONFLICT"
	// CodeOperationNotRegistered：EXECUTE_ADAPTER_OPERATION 引用了
	// definition registry 未注册的 operation——宿主零执行。
	CodeOperationNotRegistered = "OPERATION_NOT_REGISTERED"
	// CodeOperationSchemaInvalid：adapter operation 缺少运行时 schema，或
	// 参数/receipt evidence 含未声明字段、缺必填字段或类型不匹配。
	CodeOperationSchemaInvalid = "OPERATION_SCHEMA_INVALID"
	// CodeFreeCommandForm：HostAction 以自由命令形态表达（空 operation
	// 或非结构化参数）——封闭 typed union 之外不存在 shell 通道。
	CodeFreeCommandForm = "FREE_COMMAND_FORM"
	// CodeIntentMismatch：HostAction 回执与 pending intent 的 operation
	// 或参数 digest 不一致。
	CodeIntentMismatch = "INTENT_MISMATCH"
	// CodePlanBindingMismatch：待签发 plan 不是当前 state、observation 与
	// definition 确定性导出的 canonical plan。
	CodePlanBindingMismatch = "PLAN_BINDING_MISMATCH"
	// CodeReviewEvidenceRejected：WORKER_RESULT 携带的审查证据无法接纳
	//（双审顺序越序、重复结果摘要、越过自动上限无授权或 Operator 核实
	// 不全）——拒绝整个事件、零状态变化。
	CodeReviewEvidenceRejected = "REVIEW_EVIDENCE_REJECTED"
	// CodeReviewDecisionRejected：审查类业务控制的 DECIDE 无法执行（CAS
	// 冲突、越序、拓扑重复绑定等账目约束）——零状态变化。
	CodeReviewDecisionRejected = "REVIEW_DECISION_REJECTED"
	// CodeStateIntegrityInvalid：加载/提交边界发现 state 与账本不一致
	//（篡改的审查账本投影、伪造的需求 revision 账本或越门的 run phase）。
	CodeStateIntegrityInvalid = "STATE_INTEGRITY_INVALID"
	// CodeRequirementDecisionRejected：REQUEST_REQUIREMENT_CHANGE 的 DECIDE
	// 无法执行（CAS 基线过期、发现项绑定缺失、P1 源缺重走范围等账目
	// 约束）——零状态变化。
	CodeRequirementDecisionRejected = "REQUIREMENT_DECISION_REJECTED"
	// CodeRequirementAskExpired：需求变化 Ask 落账的权威基线 revision 已被
	// 后续确认取代——Ask 过期，零状态变化，须在新基线上重建 Ask。
	CodeRequirementAskExpired = "REQUIREMENT_ASK_EXPIRED"
	// CodeReviewSequenceGate：start-readiness 证据到达时产品审结论尚未
	// 就绪（双审顺序门未开）。对结果提交是可区分拒绝；对回执配对是
	// "暂不可配"信号——回执照常落账，配对挂起至产品审结论重新 PASS 的
	// 事务自动补齐。
	CodeReviewSequenceGate = "REVIEW_SEQUENCE_GATE"
	// CodeChangeBarrierActive：存在未决的需求变化 Ask（修订屏障 Phase A），
	// 新的业务任务签发被拒绝；既有 Ask、回执与结果类事件不受阻。
	CodeChangeBarrierActive = "CHANGE_BARRIER_ACTIVE"
	// CodeGuardAllowanceBlocked：候选派发在 guard 额度包络之外（无授权 basis
	// source 或该 TaskKey 的有限 retry 已耗尽）——零签发；额度扩张只经既有
	// typed Ask 通道（新有效义务/经确认新计划/一次性 attempt 追加）。
	CodeGuardAllowanceBlocked = "GUARD_ALLOWANCE_BLOCKED"
	// CodeGuardBlockedBug：签发额度账目与权威事实无法对齐（重算 digest 与持
	// 久投影不一致、已签发事实无来源、或将要超发而账本未记）——以
	// BLOCKED_BUG 终止该次签发，零派发。
	CodeGuardBlockedBug = "BLOCKED_BUG"
)

// AttemptBindings 是判断中断能否安全续接的权威绑定投影。task、外部
// snapshot 与执行责任任一变化，都不能复用原 Attempt。
// model 或 reasoningEffort 缺字段时读取 run 冻结值；显式空值使用原生默认。
type AttemptBindings struct {
	Task            runtime.TaskKey `json:"task"`
	Snapshot        string          `json:"snapshot"`
	Responsibility  string          `json:"responsibility"`
	Model           *string         `json:"model,omitempty"`
	ReasoningEffort *string         `json:"reasoningEffort,omitempty"`
}

func (b AttemptBindings) Equal(other AttemptBindings) bool {
	return b.Task == other.Task && b.Snapshot == other.Snapshot && b.Responsibility == other.Responsibility && equalExecutionValue(b.Model, other.Model) && equalExecutionValue(b.ReasoningEffort, other.ReasoningEffort)
}

func equalExecutionValue(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// PlanIdentity 记录授权一次签发的 canonical 决策身份。分量摘要用于定位
// 过期或伪造输入，PlanDigest 绑定完整 frontier 与 NextResult。
type PlanIdentity struct {
	PlanDigest        string `json:"planDigest"`
	DefinitionDigest  string `json:"definitionDigest"`
	StateDigest       string `json:"stateDigest"`
	ObservationDigest string `json:"observationDigest"`
}

// Attempt 是一个 TaskKey 的当前执行实例。签发参数整体复用
// decision.IssuedAction（actionID + 任务薄指针），ID 由签发时的落盘
// revision 确定性派生（"att:<task>:<revision>"）：revision 单调，同一
// 任务的重签发天然得到新实例标识，不含随机数与时间。任务调度位置
// （QUEUED/ISSUED/...）不在本结构重复维护——权威投影就是内嵌
// decision.State 的 Tasks 视图。
type Attempt struct {
	Business       bool         `json:"business,omitempty"`
	AwaitingResult bool         `json:"awaitingResult,omitempty"`
	PreviousAction string       `json:"previousAction,omitempty"`
	Work           *WorkBinding `json:"work,omitempty"`
	decision.IssuedAction
	ID       string          `json:"id"`
	Bindings AttemptBindings `json:"bindings"`
	Plan     PlanIdentity    `json:"plan"`
	// Attempts counts transient executions observed for this logical task. It
	// starts at zero and advances once per transient result. MaxAttempts is
	// copied from the compiled step's declaration when issued; zero means the
	// step declares no retry bound, so a transient result never exhausts and
	// stays resumable through the recovery contract.
	Attempts       int  `json:"attempts,omitempty"`
	MaxAttempts    int  `json:"maxAttempts,omitempty"`
	RetryExhausted bool `json:"retryExhausted,omitempty"`
}

// PendingAction 是 pendingActions[actionID] 的落账形态：
// 已签发 intent 的参数。与 Attempt 同源同生（一次 Issue 同时落两处索引），
// 任务到 TERMINAL 时一并移除。ReviewSeries 是定义在签发时断言的审查系列
// 身份（未标记为空）：签发点唯一写，重签发随步继承——证据声明的系列必须
// 与之相等，需求重走决定按它主动终止在途派发。
type PendingAction struct {
	ActionID     string          `json:"actionId"`
	Task         runtime.TaskKey `json:"task"`
	Step         string          `json:"step"`
	AttemptID    string          `json:"attemptId"`
	ReviewSeries review.Action   `json:"reviewSeries,omitempty"`
}

// PendingAsk 是外部受限 REQUEST_* 事件或引擎义务创建的请求：request
// ID、控制类型与完整 offer 落账。freshness token 不静态存储——它确定性绑定
// (当前 revision, requestID)，随 Freshness/Submit 即时求值，因此任何
// 后续提交都自然使旧 token 失效。决定后仍保留 PendingAsks 条目并置
// Resolved=true；已失去绑定事实的自动授权 Ask 也退休为 Resolved。
// 用户决定另保留在 Decisions 中，不允许二次决定。
type PendingAsk struct {
	Continuation   *NodeContinuationPreview `json:"continuation,omitempty"`
	Reset          *ResetPreview            `json:"reset,omitempty"`
	ResetRequestID string                   `json:"resetRequestId,omitempty"`
	Validation     *ValidationRequest       `json:"validation,omitempty"`
	RequestID      string                   `json:"requestId"`
	Control        ControlKind              `json:"control"`
	Step           authoring.StepID         `json:"step,omitempty"`
	Options        []AskOption              `json:"options"`
	// Package 是携包控制（当前仅需求变化）在 Ask 上持久化的 typed 决定
	// 包：用户确认边界落账于此，DECIDE 载荷必须与之逐字段相等。
	Package *RequirementDecisionPackage `json:"package,omitempty"`
	// RouteGates 是 SELECT_ROUTE Ask 创建时声明的候选门全集（请求方钉
	// 住、引擎落账，门目录发现仍在协议层外）：用户所见与所批一致的确认
	// 边界落账于此，DECIDE 载荷的候选表必须与之集合相等（顺序无关）。
	RouteGates []string `json:"routeGates,omitempty"`
	// RoundGrant is the immutable offer for an engine-generated review Ask.
	// The pending request owns it until decided or its bound facts are superseded.
	RoundGrant *ReviewGrantRequest `json:"roundGrant,omitempty"`
	// BaseRevision 是需求变化 Ask 创建时的权威基线 revision（引擎落账，
	// 不随请求载荷）：DECIDE 必须三方相等——载荷基线＝Ask 基线＝当前权
	// 威。任何后续确认都使 Ask 过期（可区分拒绝、零状态、须重建）。
	BaseRevision string `json:"baseRevision,omitempty"`
	// BaseGeneration binds the Ask to a confirmation occurrence, even when the
	// same requirement content is confirmed again. It is immutable for this Ask.
	BaseGeneration uint64 `json:"baseGeneration,omitempty"`
	Resolved       bool   `json:"resolved"`
}

// RecordedDecision preserves the user decision and its typed action.
type RecordedDecision struct {
	Continuation *NodeContinuationDecision `json:"continuation,omitempty"`
	Reset        *ResetDecision            `json:"reset,omitempty"`
	Review       *ReviewDecisionPayload    `json:"review,omitempty"`
	Validation   *ValidationDecision       `json:"validation,omitempty"`
	RequestID    string                    `json:"requestId"`
	Control      ControlKind               `json:"control"`
	Step         authoring.StepID          `json:"step,omitempty"`
	Choice       AskOptionID               `json:"choice"`
	EventID      string                    `json:"eventId"`
	Revision     uint64                    `json:"revision"`
}

// EventRecord 是事件台账的单条记录：accepted 事件的 payload digest 与
// acceptance 快照。幂等重放按 eventID 命中后原样返回 acceptance。
type EventRecord struct {
	Digest     string     `json:"digest"`
	Acceptance Acceptance `json:"acceptance"`
}

// SpawnReceipt 是落账的 SpawnReceipt。Digest 是
// 回执 canonical 字节的摘要：同 actionID 的逐字节重发按它判幂等，字节
// 不同即 RECEIPT_CONFLICT 硬拒绝。
type SpawnReceipt struct {
	// Execution preserves the signed choice when the receipt is accepted,
	// including native omissions, independently of the Attempt's lifetime.
	Execution         *ExecutionChoice       `json:"execution,omitempty"`
	ActionID          string                 `json:"actionId"`
	Provider          string                 `json:"provider"`
	Correlation       string                 `json:"correlation"`
	Status            string                 `json:"status"`
	FailureClass      authoring.FailureClass `json:"failureClass,omitempty"`
	LifecycleIdentity string                 `json:"lifecycleIdentity,omitempty"`
	// Task 是回执落账时固化的派发任务身份（观察事实）：任务簿记随后可
	// 被收回（如 TASK_PROGRESS 到 terminal），terminal 对账以它定位机
	// 械后果的归属步骤。
	Task   runtime.TaskKey `json:"task,omitempty"`
	Digest string          `json:"digest"`
}

// WorkerResult 是落账的 typed worker result（含 result-before-receipt
// 暂存复用同一形态）。Review 保留原始审查内容；独立 Operator observation
// 接纳后才进入 review 内核，原内容参与结果的幂等/冲突判定。
type WorkerResult struct {
	SourceResult       string                 `json:"sourceResult,omitempty"`
	InterruptionReason string                 `json:"interruptionReason,omitempty"`
	InvalidReason      string                 `json:"invalidReason,omitempty"`
	Validation         *ValidationResult      `json:"validation,omitempty"`
	ActionID           string                 `json:"actionId"`
	Provider           string                 `json:"provider"`
	Outcome            string                 `json:"outcome"`
	PayloadDigest      string                 `json:"payloadDigest"`
	FailureClass       authoring.FailureClass `json:"failureClass,omitempty"`
	Review             *ReviewResultEvidence  `json:"review,omitempty"`
	Digest             string                 `json:"digest"`
}

// WorkerResultHistory retains distinct dispatch evidence in array wire form.
type WorkerResultHistory []WorkerResult

// archiveObsoleteResult is the single obsolete-evidence writer. Ordering by
// digest makes archival independent of arrival order and map iteration.
func (s *State) archiveObsoleteResult(result WorkerResult) {
	history := s.ObsoleteResults[result.ActionID]
	for _, existing := range history {
		if existing.Digest == result.Digest {
			return
		}
	}
	history = append(history, result)
	slices.SortFunc(history, func(a, b WorkerResult) int { return strings.Compare(a.Digest, b.Digest) })
	s.ObsoleteResults[result.ActionID] = history
}

// ObsoleteAction 保留被替换 Attempt 的 action/attempt 关联，使迟到结果
// 可被明确报告为 OBSOLETE_RESULT，而不是被误报成未知 action。
type ObsoleteAction struct {
	ActionID   string          `json:"actionId"`
	Task       runtime.TaskKey `json:"task"`
	AttemptID  string          `json:"attemptId"`
	ReplacedBy string          `json:"replacedBy"`
	Reason     string          `json:"reason"`
	Bindings   AttemptBindings `json:"bindings"`
	Plan       PlanIdentity    `json:"plan"`
}

// RecoveryRecord retains the recovery route and its original interruption evidence.
// It does not establish completion of a host effect or user authorization.
// RequestID binds the original attempt to its actionable recovery request.
type RecoveryRecord struct {
	Handoff bool `json:"handoff,omitempty"`
	// InterruptionReason is the host/worker's exact cause evidence; Detail is
	// the engine's routing explanation. An empty cause remains unknown.
	InterruptionReason string                 `json:"interruptionReason,omitempty"`
	ActionID           string                 `json:"actionId"`
	Task               runtime.TaskKey        `json:"task,omitempty"`
	AttemptID          string                 `json:"attemptId"`
	Class              authoring.FailureClass `json:"class,omitempty"`
	Action             RecoveryAction         `json:"action"`
	RequestID          string                 `json:"requestId,omitempty"`
	Detail             string                 `json:"detail"`
	LifecycleMatches   int                    `json:"lifecycleMatches,omitempty"`
	Revision           uint64                 `json:"revision"`
}

// ReconciledEffect retains the disposition of a settled host responsibility.
// Its original intent admits late evidence without reopening that responsibility.
type ReconciledEffect struct {
	Intent            *HostActionIntent     `json:"intent,omitempty"`
	Promotion         *PromotionObservation `json:"promotion,omitempty"`
	Resource          *ResourceObservation  `json:"resource,omitempty"`
	UnknownReceipt    *HostActionReceipt    `json:"unknownReceipt,omitempty"`
	ActionID          string                `json:"actionId"`
	Operation         HostActionOperation   `json:"operation"`
	AdapterOperation  authoring.OperationID `json:"adapterOperation,omitempty"`
	ObservationDigest string                `json:"observationDigest"`
	Status            string                `json:"status"`
	Revision          uint64                `json:"revision"`
}

// OperatorObservation 是入账的 Operator typed observation：绑定来源
// 对账项（Subject，如 actionID/receipt 标识）+ 复用 decision.Fact 的
// typed 事实集合。
type OperatorObservation struct {
	Review   *ReviewVerificationObservation `json:"review,omitempty"`
	Subject  string                         `json:"subject"`
	Facts    []decision.Fact                `json:"facts"`
	EventID  string                         `json:"eventId"`
	Revision uint64                         `json:"revision"`
}

// HostActionOperation 是 HostAction 的封闭操作集合。Adapter operation 是
// EXECUTE_ADAPTER_OPERATION 变体内的 definition registry 引用，不与这三个
// 协议操作混为一个自由字符串。
type HostActionOperation string

const (
	HostActionResumeAgent             HostActionOperation = "RESUME_AGENT"
	HostActionTerminateAgent          HostActionOperation = "TERMINATE_AGENT"
	HostActionExecuteAdapterOperation HostActionOperation = "EXECUTE_ADAPTER_OPERATION"
)

func (o HostActionOperation) Valid() bool {
	switch o {
	case HostActionResumeAgent, HostActionTerminateAgent, HostActionExecuteAdapterOperation:
		return true
	}
	return false
}

// AgentHostAction 是 resume/terminate 共用的 typed 目标。WorkerActionID 与
// AttemptID 把宿主动作绑定到当前 durable Attempt；Identity 是宿主 lifecycle
// identity，不允许宿主通过任意参数对象改写目标。
type AgentHostAction struct {
	Input          *ValidationInput `json:"input,omitempty"`
	WorkerActionID string           `json:"workerActionId"`
	AttemptID      string           `json:"attemptId"`
	Identity       string           `json:"identity"`
	Reason         string           `json:"reason,omitempty"`
}

// AdapterHostAction 是 EXECUTE_ADAPTER_OPERATION 的唯一 payload。Schema
// 来自 compiled canonical definition；Params 仍以 JSON 对象持久化，但只
// 能通过该 schema 对应的固定 typed adapter contract。
type AdapterHostAction struct {
	Operation authoring.OperationID `json:"operation"`
	Schema    authoring.SchemaID    `json:"schema"`
	Params    map[string]any        `json:"params"`
}

// HostActionIntent 是先持久化再执行的 tagged union pending intent（draft
// §9.1）。除 Operation 对应的一个 payload 外其余必须为空；PayloadDigest
// 绑定整个变体 payload，receipt 必须精确回传。
type HostActionIntent struct {
	ActionID      string              `json:"actionId"`
	Operation     HostActionOperation `json:"operation"`
	Step          authoring.StepID    `json:"step,omitempty"`
	Resume        *AgentHostAction    `json:"resume,omitempty"`
	Terminate     *AgentHostAction    `json:"terminate,omitempty"`
	Adapter       *AdapterHostAction  `json:"adapter,omitempty"`
	PayloadDigest string              `json:"payloadDigest"`
	Correlation   string              `json:"correlation,omitempty"`
	Revision      uint64              `json:"revision"`
}

// LifecycleEvidence 是 resume/terminate receipt 的 typed lifecycle 证据。
// Event 必须是 durable lifecycle buffer 中该 identity 的实际事件。
type LifecycleEvidence struct {
	Identity string `json:"identity"`
	Event    string `json:"event"`
}

// AdapterEvidence 是 adapter operation receipt 的结构化证据对象。与参数
// 相同，它必须通过 canonical schema 对应的 typed contract 后才能持久化。
type AdapterEvidence struct {
	Values map[string]any `json:"values"`
}

// HostActionReceipt 是清账后留档的 tagged union HostAction 回执。
type HostActionReceipt struct {
	ActionID          string                 `json:"actionId"`
	Operation         HostActionOperation    `json:"operation"`
	Step              authoring.StepID       `json:"step,omitempty"`
	AdapterOperation  authoring.OperationID  `json:"adapterOperation,omitempty"`
	Provider          string                 `json:"provider"`
	Correlation       string                 `json:"correlation"`
	PayloadDigest     string                 `json:"payloadDigest"`
	Status            string                 `json:"status"`
	FailureClass      authoring.FailureClass `json:"failureClass,omitempty"`
	LifecycleEvidence *LifecycleEvidence     `json:"lifecycleEvidence,omitempty"`
	AdapterEvidence   *AdapterEvidence       `json:"adapterEvidence,omitempty"`
	Digest            string                 `json:"digest"`
}

// LifecycleEventRecord 是 observation buffer 的单条 lifecycle 事件
// （参照 internal/lifecycle 的 subagent_start/subagent_stop 语义）。
// Digest 用于逐字节重发的 duplicate 幂等判定。
type LifecycleEventRecord struct {
	Provider    string `json:"provider"`
	Correlation string `json:"correlation"`
	Identity    string `json:"identity"`
	Event       string `json:"event"`
	Digest      string `json:"digest"`
}

// State 是 engine 权威状态投影。内嵌 decision.State 复用决策视图的全部
// 派生与校验；协议台账是其上的增量。TaskKey owns
// its text codec, and encoding/json deterministically orders map keys, so the
// persistent representation stays on the domain model without a parallel
// wire projection.
type State struct {
	FlowSelection *FlowSelection         `json:"flowSelection,omitempty"`
	TaskMessages  map[string]TaskMessage `json:"taskMessages,omitempty"`
	Settings      *settings.Effective    `json:"settings,omitempty"`
	// AppliedConfigurations is the ordered applied-value history of the
	// configuration bound by Settings.ConfigurationName.
	AppliedConfigurations []AppliedConfiguration    `json:"appliedConfigurations,omitempty"`
	StartedAt             string                    `json:"startedAt,omitempty"`
	RequirementGuarantee  *GuaranteeActivation      `json:"requirementGuarantee,omitempty"`
	VCSName               string                    `json:"vcsName,omitempty"`
	NativeHandle          vcs.WorkspaceHandle       `json:"nativeHandle,omitempty"`
	NativeRoot            string                    `json:"nativeRoot,omitempty"`
	NativeResources       map[string]NativeResource `json:"nativeResources,omitempty"`
	// BaseSnapshot 是 regular 路线经原生 VCS provider 校验过的不可变基线身份
	// （启动时 verify＋ancestor-or-equal；lightweight 无 VCS 语义，恒空）。
	BaseSnapshot string `json:"baseSnapshot,omitempty"`
	// InitialSnapshot is the immutable native HEAD accepted at regular Start.
	// It owns the first development source and target identity before a promotion.
	InitialSnapshot string `json:"initialSnapshot,omitempty"`
	// PromotedSnapshot 是已绑定晋升的候选快照身份（与 PromotedDigest 同
	// 事务写入）；终态边界在两者绑定前保持等待。
	PromotedSnapshot string `json:"promotedSnapshot,omitempty"`
	// PromotedDigest 是晋升绑定的内容身份（原生树摘要）：与当前 wave 已
	// 验证最终候选的 digest 相等才构成收口事实——晋升证明的是「这份已验
	// 证内容」，不是任意一个原生身份。
	PromotedDigest     string                     `json:"promotedDigest,omitempty"`
	CandidatePromotion *CandidatePromotionReceipt `json:"candidatePromotion,omitempty"`
	// QAWorktree projects the active blackbox design role. NativeResources owns
	// the native registration and cleanup obligation after the projection clears.
	QAWorktree       string                      `json:"qaWorktree,omitempty"`
	Validation       *Validation                 `json:"validation,omitempty"`
	DispatchBindings map[string]WorkBinding      `json:"dispatchBindings,omitempty"`
	EndedAttempts    map[runtime.TaskKey]Attempt `json:"endedAttempts,omitempty"`
	RejectedResults  map[string]RejectedResult   `json:"rejectedResults,omitempty"`
	decision.State
	// Run metadata is owned by the engine façade but persisted with the same
	// authoritative state document so a read never needs a second workflow
	// writer.  Confirmation is captured at start; IntakeReceipt is populated by
	// the first drive and is intentionally not a second confirmation event.
	RunID                     string                      `json:"runId,omitempty"`
	Route                     string                      `json:"route,omitempty"`
	IntakeConfirmationReceipt *IntakeConfirmationReceipt  `json:"intakeConfirmationReceipt,omitempty"`
	IntakeReceipt             *IntakeReceipt              `json:"intakeReceipt,omitempty"`
	Expected                  []runtime.TaskKey           `json:"expected"`
	Attempts                  map[runtime.TaskKey]Attempt `json:"attempts"`
	PendingActions            map[string]PendingAction    `json:"pendingActions"`
	PendingAsks               map[string]PendingAsk       `json:"pendingAsks"`
	Decisions                 map[string]RecordedDecision `json:"decisions"`
	Events                    map[string]EventRecord      `json:"events"`

	// RunProvider identifies the admitted controller. Worker execution belongs
	// to its original Attempt bindings, independently of the controller host.
	RunProvider string `json:"runProvider"`
	// DispatchIssueRevisions 按 actionID 落账每个派发的签发代数（签发
	// 路径唯一写两处：issueInto/installReplacement；终态清理不删——
	// 签发事实是持久事实，任务 terminal 后的重走 floor 校验仍读它）。
	DispatchIssueRevisions map[string]uint64 `json:"dispatchIssueRevisions"`
	// HeldFailureRoutes 记录屏障期内到达的 FAILED 回执（spawn 与 host
	// action）的 actionID：回执本身是观察事实已照常落账，其失败路由
	//（暂存删除、重试记账、terminalize、intent 清算）挂起至此，屏障解
	// 除事务的收尾单点按既定规则统一执行。
	HeldFailureRoutes []string `json:"heldFailureRoutes,omitempty"`
	// HeldHostSettles holds completed host action IDs until transaction settlement.
	// Their receipts are durable facts; a requirement barrier defers settlement.
	HeldHostSettles []string `json:"heldHostSettles,omitempty"`
	// FrontierSettleHeld 标记屏障期内有决定/回执的 frontier settle 被挂
	// 起：解除事务统一补 settle。
	FrontierSettleHeld bool `json:"frontierSettleHeld,omitempty"`
	// routingReleasedCapacity 是当前提交事务的瞬态补位标记（不入账）：
	// 失败路由/终态化释放容量，或 host 固定执行范围使等待任务可派。
	// 事务收尾单点读取即复位。
	routingReleasedCapacity bool
	// SpawnReceipts 按 actionID 落账宿主回执（声明签发态）。
	SpawnReceipts map[string]SpawnReceipt `json:"spawnReceipts"`
	// SpawnFailures 保留可恢复的 FAILED receipt 历史；它们不占用最终
	// SpawnReceipts 槽，因此同一 Attempt 可以按恢复决定安全重试。
	SpawnFailures map[string][]SpawnReceipt `json:"spawnFailures"`
	// StagedResults 是 result-before-receipt 的暂存（actionID → result）：
	// 不丢弃、不推进，receipt 配对后在同一事务内接纳。
	StagedResults map[string]WorkerResult `json:"stagedResults"`
	// QuarantinedResults 是修订屏障（Phase A）期间的持久隔离账
	//（eventID → result）：接纳回执为 QUARANTINED、不推进不入 Results；
	// 需求决定落账的同一事务内原子重分类（正常重放或按 revision 作废），
	// 进程重启不丢失。
	QuarantinedResults map[string]WorkerResult `json:"quarantinedResults"`
	// Results 按 actionID 落账已接纳的 worker result。
	Results map[string]WorkerResult `json:"results"`
	// RecoverableResults 按 actionID 保留可恢复失败结果及同一开发派发中
	// 被替代的编辑结果。历史 payload 作为原始证据保留，并用于幂等识别。
	RecoverableResults map[string][]WorkerResult `json:"recoverableResults"`
	// OperatorObservations 有序入账主代理核实事实（绑定来源对账项）。
	OperatorObservations []OperatorObservation `json:"operatorObservations"`
	// PendingHostActions 是先持久化再执行的 HostAction intent。
	PendingHostActions map[string]HostActionIntent `json:"pendingHostActions"`
	// HostActionReceipts 按 actionID 留档已清账 intent 的回执。
	HostActionReceipts map[string]HostActionReceipt `json:"hostActionReceipts"`
	// HostActionFailures 保留未清账 intent 的 FAILED receipt 历史。
	HostActionFailures map[string][]HostActionReceipt `json:"hostActionFailures"`
	// LifecycleEvents 是 lifecycle observation buffer（只记录，不改
	// workflow 投影）。
	LifecycleEvents   []LifecycleEventRecord         `json:"lifecycleEvents"`
	ObsoleteActions   map[string]ObsoleteAction      `json:"obsoleteActions"`
	ObsoleteResults   map[string]WorkerResultHistory `json:"obsoleteResults"`
	RecoveryRecords   []RecoveryRecord               `json:"recoveryRecords"`
	ReconciledEffects map[string]ReconciledEffect    `json:"reconciledEffects"`
	// ReviewSeries 是本 run 的开发前审查账本（product-review 与
	// start-readiness 两条 series）：raw candidate 只作证据，effective
	// status 由账本确定性投影。run phase 的推进条件从它重算。
	ReviewSeries review.Set `json:"reviewSeries"`
	// RequirementRevisions stores local confirmations and their DeltaDigest
	// bindings. Its first INTAKE entry preserves the original receipt; current
	// requirement authority is read through RequirementRevision.
	RequirementRevisions []ConfirmedRevision `json:"requirementRevisions"`
	// Topology 是 start-readiness PASS 后唯一绑定的拆分拓扑决定（总需求
	// §8.1.1 绑定点：确认后不得重切）。child run 落账的是从 master 继承的
	// 拓扑副本（同样不可重切）。nil 表示尚未确认。
	Topology *TopologyBinding `json:"topology,omitempty"`
	// RouteConfirmed records the accepted gate selection independently from its
	// retained obligations during requirement reconfirmation.
	RouteConfirmed bool     `json:"routeConfirmed,omitempty"`
	SelectedGates  []string `json:"selectedGates,omitempty"`
	// SliceChild 是 child run 的继承事实（master run、分片名与继承结论绑定
	// 的需求 revision）；nil＝普通 run。产品审/技术审经它等价满足，child 不
	// 自行增加审查轮次。
	SliceChild    *SliceChildBinding `json:"sliceChild,omitempty"`
	ChildCreation *ChildCreation     `json:"childCreation,omitempty"`
	// SliceReceipt is the immutable successful child checkpoint. Its sidecar
	// is evidence only; phase, cost and the owner endpoint commit with this value.
	SliceReceipt *SliceReceipt `json:"sliceReceipt,omitempty"`
	// SliceChildren is the complete released membership; preparation is owned
	// by ChildCreation and cannot authorize a child.
	SliceChildren map[string]string `json:"sliceChildren,omitempty"`
	// SliceReceipts is the master's immutable accepted child evidence, keyed by
	// child run. Receipt insertion, its cases payload and cost contribution share
	// one transaction; historical evidence survives requirement/topology changes.
	SliceResetAuthorities map[string]ResetAuthority `json:"sliceResetAuthorities,omitempty"`
	SliceReceiptHistory   []SliceReceipt            `json:"sliceReceiptHistory,omitempty"`
	SliceReceipts         map[string]SliceReceipt   `json:"sliceReceipts,omitempty"`
	// Guard 是从签发/结果账本派生的额度投影（requestAllowance/retryAllowance/
	// totalAllowance/budgetBasisDigest＋used 事实与 token cap 面）。事实源是账本
	// 本身，本投影只在提交点重derive写入，加载边界重算比对；受理前的 run（包络
	// 未就位）为 nil。
	Guard *GuardBudget `json:"guard,omitempty"`
	// Cost 是本 run 的 cost 权威投影（dispatch 逐条＋run 合计；owner 项缺源时
	// 缺席）。cost 只由引擎事务写入。
	Cost *cost.RunCost `json:"cost,omitempty"`
	// TokenCapMaxTokens 是项目级 token cap 配置的引擎持久化（nil＝未配置/不可
	// 用；只统计 engine 派发用量，不含 owner 终态报告）。受理 bootstrap 一次写
	// 入，run 期间不可变。
	TokenCapMaxTokens *int64 `json:"tokenCapMaxTokens,omitempty"`
	// Owner 投影是主代理（owner）计量的持久绑定、启动时点与启动基线（REQ-012
	// AC-050）：来源稳定时由 facade 在 bootstrap 一次写入（OwnerStartedAt 是
	// 区间事实的本方端点，终态重扫与终态摘要消费它）；缺源不承诺 owner 计量
	//（AC-051 零数字）。收口差值只作报告（Cost.Owner），不参与 in-flight guard；
	// child 的闭合端点和计量原值随 SliceReceipt 同事务冻结。
	OwnerTranscript string                   `json:"ownerTranscript,omitempty"`
	OwnerSession    string                   `json:"ownerSession,omitempty"`
	OwnerProvider   string                   `json:"ownerProvider,omitempty"`
	OwnerStartedAt  string                   `json:"ownerStartedAt,omitempty"`
	OwnerBaseline   *cost.TranscriptSnapshot `json:"ownerBaseline,omitempty"`
	OwnerOverlapped bool                     `json:"ownerOverlapped,omitempty"`
}

// TopologyBinding 是拓扑确认的持久形态：no-split 必有理由 note（建议不拆
// 原因留痕）；split 必有 SplitCount≥2、与之一致的 Slices 与并行度，以及每
// 条验收条件恰一个主责 owner 的责任映射（owner ∈ Slices ∪ {master-merge}，
// 绑定时已归一校验）。类型名避开 Slicing 一词，防止与审查动作
// start-readiness 的下游影响表混淆。
type TopologyBinding struct {
	Decision   string            `json:"decision"`
	Note       string            `json:"note,omitempty"`
	SplitCount int               `json:"splitCount,omitempty"`
	Slices     []string          `json:"slices,omitempty"`
	Parallel   string            `json:"parallel,omitempty"`
	ACOwners   map[string]string `json:"acOwners,omitempty"`
}

// ConfirmedRevision 是需求 revision 权威账本的单条确认事实。Revision 是
// 确认后的需求 revision；BaseRevision 是它所基于的上一确认 revision
// （INTAKE 初始账目为空）；DeltaDigest 绑定程序按原确认材料生成的穷尽 review-delta 指纹；
// Source/Classification/DecidedScope 记录用户声明的来源、含义分类与勾选的
// 重走范围；RequestID 绑定授权它的 Ask；StateRevision 是登记落账时的 state
// 代数（单调，防篡改链的一环）。
type ConfirmedRevision struct {
	Intake         *IntakeConfirmationReceipt `json:"intake,omitempty"`
	Coverage       *CoverageInventory         `json:"coverage,omitempty"`
	Revision       string                     `json:"revision"`
	DeltaDigest    string                     `json:"deltaDigest,omitempty"`
	BaseRevision   string                     `json:"baseRevision,omitempty"`
	Source         string                     `json:"source"`
	Classification string                     `json:"classification"`
	DecidedScope   []string                   `json:"decidedScope,omitempty"`
	WorkDecisions  []RequirementWorkDecision  `json:"workDecisions,omitempty"`
	Children       []ChildRequirementDecision `json:"children,omitempty"`
	Impact         *RequirementImpact         `json:"impact,omitempty"`
	ResetRequestID string                     `json:"resetRequestId,omitempty"`
	RequestID      string                     `json:"requestId,omitempty"`
	// Reason 是用户决定的理由原文（随确认落账持久化）：carry 留痕据此
	// 携理由（§7.2 第 4 项 携原 revision、证据和理由）。
	Reason        string `json:"reason,omitempty"`
	StateRevision uint64 `json:"stateRevision"`
}

// IntakeMaterial is the normalized confirmed text carried by a worker payload.
type IntakeMaterial struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

// IntakeArtifact is one canonical requirement/solution artifact binding.
type IntakeArtifact struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
}

// IntakeAuthority identifies the stable driver that owns the confirmation.
// It is a closed value so a candidate cannot accept a receipt made by an
// unrelated writer.
type IntakeAuthority string

const (
	IntakeAuthorityStableDriver IntakeAuthority = "stable-driver"
)

// IntakeTransport identifies the fixed launcher transport used to deliver the
// confirmation receipt.  A candidate accepts only this documented transport.
type IntakeTransport string

const (
	IntakeTransportStableLauncher IntakeTransport = "stable-launcher"
)

// IntakeConfirmationReceipt is the typed material confirmation supplied by the
// host. File verification and canonical digesting belong to the facade before
// admission. The initial receipt remains separate from later confirmed revisions.
type IntakeConfirmationReceipt struct {
	Coverage            *CoverageInventory `json:"coverage,omitempty"`
	Source              string             `json:"source"`
	Authority           IntakeAuthority    `json:"authority"`
	Transport           IntakeTransport    `json:"transport"`
	RequirementSource   string             `json:"requirementSource"`
	RequirementRevision string             `json:"requirementRevision"`
	Artifacts           []IntakeArtifact   `json:"artifacts"`
	// SolutionSource is the selected solution path; empty selects RequirementSource.
	SolutionSource   string `json:"solutionSource,omitempty"`
	SolutionRevision string `json:"solutionRevision"`
	SolutionDigest   string `json:"solutionDigest"`
	Digest           string `json:"digest,omitempty"`
}

// IntakeReceipt is the first-drive durable projection of the exact
// IntakeConfirmationReceipt and its canonical digest.
type IntakeReceipt struct {
	Confirmation IntakeConfirmationReceipt `json:"confirmation"`
	IntakeDigest string                    `json:"intakeDigest"`
	Revision     uint64                    `json:"revision"`
}

// NewState 构造空台账的初始状态；决策视图经 decision.NewState 校验，
// 任务视图 map 补齐非 nil（决策内核对缺省键按 QUEUED 解释，这里补齐
// 容器供台账与测试直接落笔）。
func NewState(view decision.State) *State {
	state := &State{State: view}
	state.normalizeCollections()
	return state
}

// UnmarshalJSON keeps decoding on the domain structure and performs only the
// invariant normalization needed by mutation paths. It deliberately does not
// project maps through duplicate wire structs.
func (s *State) UnmarshalJSON(data []byte) error {
	type stateJSON State
	var decoded stateJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode state: %w", err)
	}
	*s = State(decoded)
	s.normalizeCollections()
	return nil
}

func (s *State) normalizeCollections() {
	if s.DispatchBindings == nil {
		s.DispatchBindings = map[string]WorkBinding{}
	}
	if s.EndedAttempts == nil {
		s.EndedAttempts = map[runtime.TaskKey]Attempt{}
	}
	if s.RejectedResults == nil {
		s.RejectedResults = map[string]RejectedResult{}
	}
	if s.Completed == nil {
		s.Completed = []authoring.StepID{}
	}
	if s.Tasks == nil {
		s.Tasks = map[runtime.TaskKey]runtime.TaskStatus{}
	}
	if s.Expected == nil {
		s.Expected = []runtime.TaskKey{}
	}
	if s.Attempts == nil {
		s.Attempts = map[runtime.TaskKey]Attempt{}
	}
	if s.DispatchIssueRevisions == nil {
		s.DispatchIssueRevisions = map[string]uint64{}
	}
	if s.PendingActions == nil {
		s.PendingActions = map[string]PendingAction{}
	}
	if s.PendingAsks == nil {
		s.PendingAsks = map[string]PendingAsk{}
	}
	if s.Decisions == nil {
		s.Decisions = map[string]RecordedDecision{}
	}
	if s.Events == nil {
		s.Events = map[string]EventRecord{}
	}
	if s.SpawnReceipts == nil {
		s.SpawnReceipts = map[string]SpawnReceipt{}
	}
	if s.SpawnFailures == nil {
		s.SpawnFailures = map[string][]SpawnReceipt{}
	}
	if s.StagedResults == nil {
		s.StagedResults = map[string]WorkerResult{}
	}
	if s.QuarantinedResults == nil {
		s.QuarantinedResults = map[string]WorkerResult{}
	}
	if s.Results == nil {
		s.Results = map[string]WorkerResult{}
	}
	if s.RecoverableResults == nil {
		s.RecoverableResults = map[string][]WorkerResult{}
	}
	if s.OperatorObservations == nil {
		s.OperatorObservations = []OperatorObservation{}
	}
	if s.PendingHostActions == nil {
		s.PendingHostActions = map[string]HostActionIntent{}
	}
	if s.HostActionReceipts == nil {
		s.HostActionReceipts = map[string]HostActionReceipt{}
	}
	if s.HostActionFailures == nil {
		s.HostActionFailures = map[string][]HostActionReceipt{}
	}
	if s.LifecycleEvents == nil {
		s.LifecycleEvents = []LifecycleEventRecord{}
	}
	if s.ObsoleteActions == nil {
		s.ObsoleteActions = map[string]ObsoleteAction{}
	}
	if s.ObsoleteResults == nil {
		s.ObsoleteResults = map[string]WorkerResultHistory{}
	}
	if s.RecoveryRecords == nil {
		s.RecoveryRecords = []RecoveryRecord{}
	}
	if s.ReconciledEffects == nil {
		s.ReconciledEffects = map[string]ReconciledEffect{}
	}
	// 两条 series 均无动作绑定的格式使用初始账本；只有一条绑定的部分
	// 投影由加载/提交边界的 review.Validate 拒绝。
	if s.ReviewSeries.ProductReview.Action == "" && s.ReviewSeries.StartReadiness.Action == "" {
		configuration := s.AppliedConfiguration()
		s.ReviewSeries = review.NewSetWithLimits(configuration.RoundLimit("product-review"), configuration.RoundLimit("tech-review"), 0)
	}
	// An empty local ledger is seeded from this run's original intake receipt.
	if len(s.RequirementRevisions) == 0 && s.IntakeReceipt != nil {
		s.RequirementRevisions = []ConfirmedRevision{{
			Revision:       s.IntakeReceipt.Confirmation.RequirementRevision,
			Source:         RequirementSourceIntake,
			Classification: RequirementClassificationIntake,
			StateRevision:  s.IntakeReceipt.Revision,
		}}
	}
	// Guard 投影支持缺省 TokenCapStatus，归一为 UNAVAILABLE；
	// 无配置 run 的 cap 面恒为 UNAVAILABLE。
	if s.Guard != nil && s.Guard.TokenCapStatus == "" {
		s.Guard.TokenCapStatus = TokenCapUnavailable
	}
}

func (s *State) NativeVCS() string {
	if s.VCSName == "" {
		return "git"
	}
	return s.VCSName
}
