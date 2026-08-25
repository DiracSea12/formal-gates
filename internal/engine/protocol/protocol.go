// Package protocol 是 engine 的提交协议内核（阶段 2 批 1b：
// orchestration-pipeline-engine-phase-2/master-requirements.md §3
// 「提交协议」，final-implementation-draft §2.2/§2.3/§3.3/§3.4）。
//
// 在批 1a 持久化基座（internal/engine/persistence）之上交付五件能力：
//
//   - 状态模型：engine 权威状态投影落账 expected 任务清单
//     （runtime.TaskKey）、每 TaskKey 的当前 Attempt（标识 + 签发绑定）、
//     pendingActions[actionID]（已签发 intent 参数）、pending Ask、已提交
//     决定与事件台账。决策视图直接内嵌 decision.State 复用阶段 1 的
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
// 本批边界：SpawnReceipt/worker result/Ask/Operator/HostAction receipt/
// lifecycle event 的统一接纳与失败分类路由是阶段 2 后续批次（master-
// requirements §4/§5）；reset/abort 等控制决定的执行语义属阶段 4——本批
// 只落账决定，不执行控制。全部写入经 persistence 的四段协议落盘（信封/
// 摘要/CAS/指纹），不改公开 CLI 面。
package protocol

import (
	"encoding/json"
	"fmt"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
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
	// （含空 provider）——硬拒绝，不降级 default（draft §9.1）。
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
)

// AttemptBindings 是判断中断能否安全续接的权威绑定投影。task、外部
// snapshot 与执行责任任一变化，都不能复用原 Attempt。
type AttemptBindings struct {
	Task           runtime.TaskKey `json:"task"`
	Snapshot       string          `json:"snapshot"`
	Responsibility string          `json:"responsibility"`
}

func (b AttemptBindings) Equal(other AttemptBindings) bool {
	return b.Task == other.Task && b.Snapshot == other.Snapshot && b.Responsibility == other.Responsibility
}

// PlanIdentity 记录授权一次签发的 canonical 决策身份。分量摘要用于定位
// 过期或伪造输入，PlanDigest 绑定完整 frontier 与 NextResult。
type PlanIdentity struct {
	PlanDigest        string `json:"planDigest"`
	DefinitionDigest  string `json:"definitionDigest"`
	StateDigest       string `json:"stateDigest"`
	ObservationDigest string `json:"observationDigest"`
}

// Attempt 是一个 TaskKey 的当前执行实例（draft §3.3「task/dispatch/
// Attempt 与 source bindings」的本批形态）。签发参数整体复用
// decision.IssuedAction（actionID + 任务薄指针），ID 由签发时的落盘
// revision 确定性派生（"att:<task>:<revision>"）：revision 单调，同一
// 任务的重签发天然得到新实例标识，不含随机数与时间。任务调度位置
// （QUEUED/ISSUED/...）不在本结构重复维护——权威投影就是内嵌
// decision.State 的 Tasks 视图。
type Attempt struct {
	decision.IssuedAction
	ID       string          `json:"id"`
	Bindings AttemptBindings `json:"bindings"`
	Plan     PlanIdentity    `json:"plan"`
	// Attempts counts transient executions observed for this logical task. It
	// starts at zero and advances once per transient result. MaxAttempts is
	// copied from the compiled step's declaration when issued; zero means that
	// the step declares no retry bound (the review.worker fixture intentionally
	// has this shape).
	Attempts       int  `json:"attempts,omitempty"`
	MaxAttempts    int  `json:"maxAttempts,omitempty"`
	RetryExhausted bool `json:"retryExhausted,omitempty"`
}

// PendingAction 是 pendingActions[actionID] 的落账形态（draft §3.3）：
// 已签发 intent 的参数。与 Attempt 同源同生（一次 Issue 同时落两处索引），
// 任务到 TERMINAL 时一并移除。
type PendingAction struct {
	ActionID  string          `json:"actionId"`
	Task      runtime.TaskKey `json:"task"`
	Step      string          `json:"step"`
	AttemptID string          `json:"attemptId"`
}

// PendingAsk 是受限 REQUEST_* 事件创建的待决 Ask（draft §2.3）：request
// ID、控制类型与选项集落账。freshness token 不静态存储——它确定性绑定
// (当前 revision, requestID)，随 Freshness/Submit 即时求值，因此任何
// 后续提交都自然使旧 token 失效。Resolved 后记录决定并保留（审计），
// 不允许二次决定。
type PendingAsk struct {
	RequestID string      `json:"requestId"`
	Control   ControlKind `json:"control"`
	Options   []AskOption `json:"options"`
	Resolved  bool        `json:"resolved"`
}

// RecordedDecision 是用户经两阶段 Ask 提交的决定（本批只落账不执行）。
type RecordedDecision struct {
	RequestID string      `json:"requestId"`
	Control   ControlKind `json:"control"`
	Choice    AskOptionID `json:"choice"`
	EventID   string      `json:"eventId"`
	Revision  uint64      `json:"revision"`
}

// EventRecord 是事件台账的单条记录：accepted 事件的 payload digest 与
// acceptance 快照。幂等重放按 eventID 命中后原样返回 acceptance。
type EventRecord struct {
	Digest     string     `json:"digest"`
	Acceptance Acceptance `json:"acceptance"`
}

// SpawnReceipt 是落账的 SpawnReceipt（draft §9.1 公共字段）。Digest 是
// 回执 canonical 字节的摘要：同 actionID 的逐字节重发按它判幂等，字节
// 不同即 RECEIPT_CONFLICT 硬拒绝。
type SpawnReceipt struct {
	ActionID          string                 `json:"actionId"`
	Provider          string                 `json:"provider"`
	Correlation       string                 `json:"correlation"`
	Status            string                 `json:"status"`
	FailureClass      authoring.FailureClass `json:"failureClass,omitempty"`
	LifecycleIdentity string                 `json:"lifecycleIdentity,omitempty"`
	Digest            string                 `json:"digest"`
}

// WorkerResult 是落账的 typed worker result（含 result-before-receipt
// 暂存复用同一形态）。
type WorkerResult struct {
	ActionID      string                 `json:"actionId"`
	Provider      string                 `json:"provider"`
	Outcome       string                 `json:"outcome"`
	PayloadDigest string                 `json:"payloadDigest"`
	FailureClass  authoring.FailureClass `json:"failureClass,omitempty"`
	Digest        string                 `json:"digest"`
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

// RecoveryRecord 是一次恢复路由的 durable 记录。它只记录 engine 已经
// 机械决定的事实，不冒充宿主动作或用户授权已经完成。
type RecoveryRecord struct {
	ActionID         string                 `json:"actionId"`
	Task             runtime.TaskKey        `json:"task,omitempty"`
	AttemptID        string                 `json:"attemptId"`
	Class            authoring.FailureClass `json:"class,omitempty"`
	Action           RecoveryAction         `json:"action"`
	RequestID        string                 `json:"requestId,omitempty"`
	Detail           string                 `json:"detail"`
	LifecycleMatches int                    `json:"lifecycleMatches,omitempty"`
	Revision         uint64                 `json:"revision"`
}

// ReconciledEffect 记录 UNKNOWN 本地副作用已由外部事实满足后的结算。
// 结算只提交结果，不再次执行原动作。
type ReconciledEffect struct {
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
	Subject  string          `json:"subject"`
	Facts    []decision.Fact `json:"facts"`
	EventID  string          `json:"eventId"`
	Revision uint64          `json:"revision"`
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
	WorkerActionID string `json:"workerActionId"`
	AttemptID      string `json:"attemptId"`
	Identity       string `json:"identity"`
	Reason         string `json:"reason,omitempty"`
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

// LifecycleVerification 是一个 identity 的已验证配对：start 与 stop 都
// 在 buffer 中且 identity 被某个 SpawnReceipt 的 correlation 认领。
type LifecycleVerification struct {
	Correlation string `json:"correlation"`
	Identity    string `json:"identity"`
	Provider    string `json:"provider"`
	Revision    uint64 `json:"revision"`
}

// State 是 engine 权威状态投影（批 1b/1c 形态）。内嵌 decision.State 复用
// 阶段 1 决策视图的全部派生与校验；协议台账是其上的增量。TaskKey owns
// its text codec, and encoding/json deterministically orders map keys, so the
// persistent representation stays on the domain model without a parallel
// wire projection.
type State struct {
	decision.State
	Expected       []runtime.TaskKey           `json:"expected"`
	Attempts       map[runtime.TaskKey]Attempt `json:"attempts"`
	PendingActions map[string]PendingAction    `json:"pendingActions"`
	PendingAsks    map[string]PendingAsk       `json:"pendingAsks"`
	Decisions      map[string]RecordedDecision `json:"decisions"`
	Events         map[string]EventRecord      `json:"events"`

	// RunProvider 是 run 绑定的宿主 provider 身份（Init 落账）：一切携带
	// provider 的事件/回执与之精确比对，不同或为空即硬拒绝（draft §9.1
	// 不降级 default）。
	RunProvider string `json:"runProvider"`
	// SpawnReceipts 按 actionID 落账宿主回执（声明签发态）。
	SpawnReceipts map[string]SpawnReceipt `json:"spawnReceipts"`
	// SpawnFailures 保留可恢复的 FAILED receipt 历史；它们不占用最终
	// SpawnReceipts 槽，因此同一 Attempt 可以按恢复决定安全重试。
	SpawnFailures map[string][]SpawnReceipt `json:"spawnFailures"`
	// StagedResults 是 result-before-receipt 的暂存（actionID → result）：
	// 不丢弃、不推进，receipt 配对后在同一事务内接纳。
	StagedResults map[string]WorkerResult `json:"stagedResults"`
	// Results 按 actionID 落账已接纳的 worker result。
	Results map[string]WorkerResult `json:"results"`
	// RecoverableResults 保留未终结当前 Attempt 的失败结果历史。后续同一
	// actionID 的 PASS 可完成原 Attempt，而相同失败 payload 仍可幂等识别。
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
	LifecycleEvents []LifecycleEventRecord `json:"lifecycleEvents"`
	// LifecycleVerified 按 identity 落账已验证的 start/stop 配对。
	LifecycleVerified map[string]LifecycleVerification `json:"lifecycleVerified"`
	ObsoleteActions   map[string]ObsoleteAction        `json:"obsoleteActions"`
	ObsoleteResults   map[string]WorkerResult          `json:"obsoleteResults"`
	RecoveryRecords   []RecoveryRecord                 `json:"recoveryRecords"`
	ReconciledEffects map[string]ReconciledEffect      `json:"reconciledEffects"`
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
	if s.LifecycleVerified == nil {
		s.LifecycleVerified = map[string]LifecycleVerification{}
	}
	if s.ObsoleteActions == nil {
		s.ObsoleteActions = map[string]ObsoleteAction{}
	}
	if s.ObsoleteResults == nil {
		s.ObsoleteResults = map[string]WorkerResult{}
	}
	if s.RecoveryRecords == nil {
		s.RecoveryRecords = []RecoveryRecord{}
	}
	if s.ReconciledEffects == nil {
		s.ReconciledEffects = map[string]ReconciledEffect{}
	}
}
