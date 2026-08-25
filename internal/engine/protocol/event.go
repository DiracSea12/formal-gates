package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
)

// 独立命名 ID（ids.go 哲学：不同用途的 ID 之间不可直接赋值）。
type (
	// EventID 是提交事件的唯一标识：幂等台账的主键，同 ID 的重放按
	// payload digest 判定等价或硬拒绝。
	EventID string
	// RequestID 标识一个 pending Ask。受限 REQUEST_* 事件创建 Ask 时，
	// 其 EventID 即转化为 RequestID（同一标识符的两个角色），用户决定
	// 以该 ID 提交。
	RequestID string
	// AskOptionID 是 Ask 选项集内的选项标识（决定事件引用）。
	AskOptionID string
)

// ControlKind 是受限 REQUEST_* 事件的封闭控制类型（draft §2.3）。RESET
// 与 ABORT 的授权路径在 draft §10 已冻结（活动 run 的 reset/abort 必须
// 由 Ask → submit 授权、不保留公开 cleanup 后门）；需求变化等业务控制
// 随阶段 4 迁移扩充本枚举（封闭集合的既定扩展点）。
type ControlKind string

const (
	ControlReset    ControlKind = "RESET"
	ControlAbort    ControlKind = "ABORT"
	ControlRecovery ControlKind = "RECOVER_ATTEMPT"
)

// Valid 报告 k 是否属于封闭控制集合。
func (k ControlKind) Valid() bool {
	return k == ControlReset || k == ControlAbort || k == ControlRecovery
}

// AskOption 是 pending Ask 落账的单个选项。
type AskOption struct {
	ID    AskOptionID `json:"id"`
	Label string      `json:"label"`
}

// EventKind 是提交事件的封闭 kind 集合（draft §2.2/§2.3/§3.4/§9.1）。刻意
// 不存在任何自由 USER_* 直写 kind：用户决定只能走 REQUEST_* 创建
// pending Ask → DECIDE 两阶段路径。
type EventKind string

const (
	// KindRequestControl：受限 REQUEST_* 事件，创建 pending Ask（不直接
	// 执行任何控制）。
	KindRequestControl EventKind = "REQUEST_CONTROL"
	// KindDecide：用户对 pending Ask 的决定提交（request ID + freshness
	// token + 选项）。
	KindDecide EventKind = "DECIDE"
	// KindTaskProgress：外部边界对 expected 任务当前 Attempt 的进度
	// 观察（RUNNING/VALIDATING/TERMINAL）。
	KindTaskProgress EventKind = "TASK_PROGRESS"
	// KindSpawnReceipt：fake host 对一次签发的 SpawnReceipt（actionID/
	// correlation/provider/status 公共字段，draft §9.1）。接纳后任务进入
	// 声明签发态并落账 receipt。
	KindSpawnReceipt EventKind = "SPAWN_RECEIPT"
	// KindWorkerResult：worker 的 typed result（actionID/provider/outcome/
	// payload digest）。receipt 未到时暂存（result-before-receipt），配对
	// 后接纳并按容量补位签发。
	KindWorkerResult EventKind = "WORKER_RESULT"
	// KindOperatorObservation：主代理的 typed observation（draft §2.2
	// Operator：核实事实、不替用户授权），入账并绑定来源对账项。
	KindOperatorObservation EventKind = "OPERATOR_OBSERVATION"
	// KindHostActionReceipt：宿主对 HostAction 的回执；接纳后 pending
	// intent 清账（draft §9.1：先持久化 intent 再执行）。
	KindHostActionReceipt EventKind = "HOST_ACTION_RECEIPT"
	// KindLifecycleEvent：宿主 lifecycle 事件（成对 start/stop）；只写
	// observation buffer，不直接改 workflow state。
	KindLifecycleEvent EventKind = "LIFECYCLE_EVENT"
)

// Valid 报告 k 是否属于八值封闭集合。
func (k EventKind) Valid() bool {
	switch k {
	case KindRequestControl, KindDecide, KindTaskProgress, KindSpawnReceipt,
		KindWorkerResult, KindOperatorObservation, KindHostActionReceipt, KindLifecycleEvent:
		return true
	}
	return false
}

// RequestPayload 是 REQUEST_CONTROL 的 payload：控制类型 + Ask 选项集。
type RequestPayload struct {
	Control ControlKind
	Options []AskOption
}

// DecidePayload 是 DECIDE 的 payload：目标 request、freshness token 与
// 选中选项（选项合法性对 pending Ask 的落账选项集校验，属接纳层）。
type DecidePayload struct {
	Request RequestID
	Token   string
	Choice  AskOptionID
}

// TaskPayload 是 TASK_PROGRESS 的 payload：目标任务、期望的当前
// Attempt 与观察到的进度状态。
type TaskPayload struct {
	Task    runtime.TaskKey
	Attempt string
	Status  runtime.TaskStatus
}

// SpawnReceiptPayload 是 SPAWN_RECEIPT 的 payload：draft §9.1 的公共
// receipt 字段子集（actionID/correlation/provider/status）。Correlation
// 是宿主侧 agent 身份关联，lifecycle 配对按它认领。
type SpawnReceiptPayload struct {
	ActionID     string
	Provider     string
	Correlation  string
	Status       string
	FailureClass authoring.FailureClass
}

// SpawnReceipt status 的封闭集合：SPAWNED 声明派发成功；FAILED 声明派发
// 失败（失败分类路由属后续批次，本批只落账）。
const (
	SpawnStatusSpawned = "SPAWNED"
	SpawnStatusFailed  = "FAILED"
	// SpawnStatusUnknown records that the host-side outcome is not known. It
	// must be reconciled against lifecycle evidence before any respawn.
	SpawnStatusUnknown  = "UNKNOWN"
	SpawnStatusAttached = "ATTACHED"
)

// WorkerResultPayload 是 WORKER_RESULT 的 payload：typed result 的最小
// 形态——目标 action、worker 的 provider 身份、封闭 outcome 与结果字节
// 的 payload digest。
type WorkerResultPayload struct {
	ActionID      string
	Provider      string
	Outcome       string
	PayloadDigest string
	FailureClass  authoring.FailureClass
}

// worker result outcome 的封闭集合（runtime/task.go：结果分类不进入任务
// 状态机）。PASS 推进 frontier；FAIL/RUNTIME_ERROR 按显式 failure class
// 路由，终态失败不会推进 frontier。
const (
	OutcomePass         = "PASS"
	OutcomeFail         = "FAIL"
	OutcomeRuntimeError = "RUNTIME_ERROR"
)

// OperatorObservationPayload 是 OPERATOR_OBSERVATION 的 payload：主代理
// 核实的事实集合（复用 decision.Fact 的 typed 形态与来源枚举）与它绑定
// 的来源对账项标识（如 actionID/receipt）。
type OperatorObservationPayload struct {
	Subject string
	Facts   []decision.Fact
}

// HostActionReceiptPayload 是 HOST_ACTION_RECEIPT 的 payload：draft §9.1
// 公共 receipt 字段。Operation 必须与 pending intent 的声明精确一致，
// PayloadDigest 必须等于 intent 参数的 canonical digest。
type HostActionReceiptPayload struct {
	ActionID          string
	Operation         HostActionOperation
	AdapterOperation  authoring.OperationID
	Provider          string
	Correlation       string
	PayloadDigest     string
	Status            string
	FailureClass      authoring.FailureClass
	LifecycleEvidence *LifecycleEvidence
	AdapterEvidence   *AdapterEvidence
}

// HostAction receipt status 的封闭集合。
const (
	HostActionStatusExecuted = "EXECUTED"
	HostActionStatusFailed   = "FAILED"
	HostActionStatusUnknown  = "UNKNOWN"
)

// LifecycleEventPayload 是 LIFECYCLE_EVENT 的 payload：宿主 lifecycle
// 事件（参照 internal/lifecycle 的 subagent_start/subagent_stop 语义）。
// Identity 是宿主 agent 身份，配对与认领都按它进行。
type LifecycleEventPayload struct {
	Provider    string
	Correlation string
	Identity    string
	Event       string
}

// lifecycle 事件的封闭集合（与 internal/lifecycle 的 eventStart/eventStop
// 同值）。
const (
	LifecycleStart = "subagent_start"
	LifecycleStop  = "subagent_stop"
)

// Event 是提交事件的 tagged union：除本 kind 对应的一个 payload 外其余
// 必须为 nil（Validate 强制，形态与 decision.NextResult 一致）。
type Event struct {
	ID          EventID
	Kind        EventKind
	Request     *RequestPayload
	Decide      *DecidePayload
	Task        *TaskPayload
	Spawn       *SpawnReceiptPayload
	Result      *WorkerResultPayload
	Observation *OperatorObservationPayload
	HostAction  *HostActionReceiptPayload
	Lifecycle   *LifecycleEventPayload
}

// NewRequestEvent 构造受限 REQUEST_* 事件：ID 非空、控制类型合法、
// 选项集非空且 ID 唯一非空、标签非空。
func NewRequestEvent(id EventID, control ControlKind, options ...AskOption) (Event, error) {
	ev := Event{ID: id, Kind: KindRequestControl, Request: &RequestPayload{Control: control, Options: append([]AskOption(nil), options...)}}
	return ev, ev.Validate()
}

// NewDecideEvent 构造用户决定事件：request/token/选项均非空。
func NewDecideEvent(id EventID, request RequestID, token string, choice AskOptionID) (Event, error) {
	ev := Event{ID: id, Kind: KindDecide, Decide: &DecidePayload{Request: request, Token: token, Choice: choice}}
	return ev, ev.Validate()
}

// NewTaskEvent 构造任务进度事件：任务键可解析、attempt 非空、进度状态
// 属外部观察可报告的封闭子集（RUNNING/VALIDATING/TERMINAL；ISSUED 由
// 签发落账产生，不接受外部报告）。
func NewTaskEvent(id EventID, task runtime.TaskKey, attempt string, status runtime.TaskStatus) (Event, error) {
	ev := Event{ID: id, Kind: KindTaskProgress, Task: &TaskPayload{Task: task, Attempt: attempt, Status: status}}
	return ev, ev.Validate()
}

// NewSpawnReceiptEvent 构造 SpawnReceipt 事件：actionID/provider/
// correlation/status 非空且 status 属封闭集合。Provider 由接纳层对照
// run 绑定硬校验（不降级 default）。
func NewSpawnReceiptEvent(id EventID, actionID, provider, correlation, status string) (Event, error) {
	return NewSpawnReceiptWithFailureClass(id, actionID, provider, correlation, status, "")
}

// NewSpawnReceiptWithFailureClass constructs a spawn receipt with an explicit
// failure route. FAILED requires a class; successful/unknown receipts reject
// one so callers cannot smuggle an unrelated recovery decision.
func NewSpawnReceiptWithFailureClass(id EventID, actionID, provider, correlation, status string, class authoring.FailureClass) (Event, error) {
	ev := Event{ID: id, Kind: KindSpawnReceipt, Spawn: &SpawnReceiptPayload{
		ActionID: actionID, Provider: provider, Correlation: correlation, Status: status, FailureClass: class,
	}}
	return ev, ev.Validate()
}

// NewWorkerResultEvent 是唯一的 typed worker-result 构造入口。失败结果
// 必须显式携带封闭 failure class；PASS 结果必须传空 class。
func NewWorkerResultEvent(id EventID, actionID, provider, outcome, payloadDigest string, class authoring.FailureClass) (Event, error) {
	ev := Event{ID: id, Kind: KindWorkerResult, Result: &WorkerResultPayload{
		ActionID: actionID, Provider: provider, Outcome: outcome, PayloadDigest: payloadDigest, FailureClass: class,
	}}
	return ev, ev.Validate()
}

// NewOperatorObservationEvent 构造 Operator observation 事件：对账项
// 非空、事实集非空且逐条 typed 合法（来源封闭枚举 + 非空键，复用
// decision 的 Fact 规则）。
func NewOperatorObservationEvent(id EventID, subject string, facts ...decision.Fact) (Event, error) {
	ev := Event{ID: id, Kind: KindOperatorObservation, Observation: &OperatorObservationPayload{
		Subject: subject, Facts: append([]decision.Fact(nil), facts...),
	}}
	return ev, ev.Validate()
}

// NewHostActionReceiptEvent 构造 HostAction 回执事件：公共字段非空、
// operation 引用非空、status 属封闭集合。
func NewHostActionReceiptEvent(id EventID, actionID string, operation authoring.OperationID, provider, correlation, payloadDigest, status string) (Event, error) {
	return NewAdapterHostActionReceiptEvent(id, actionID, operation, provider, correlation, payloadDigest, status, "", map[string]any{})
}

// NewAdapterHostActionReceiptEvent constructs the adapter branch of the
// HostAction receipt union. Evidence is validated against the operation schema
// by Engine.Submit before it is persisted.
func NewAdapterHostActionReceiptEvent(id EventID, actionID string, operation authoring.OperationID, provider, correlation, payloadDigest, status string, class authoring.FailureClass, evidence map[string]any) (Event, error) {
	if evidence == nil {
		evidence = map[string]any{}
	}
	ev := Event{ID: id, Kind: KindHostActionReceipt, HostAction: &HostActionReceiptPayload{
		ActionID: actionID, Operation: HostActionExecuteAdapterOperation, AdapterOperation: operation, Provider: provider,
		Correlation: correlation, PayloadDigest: payloadDigest, Status: status, FailureClass: class,
		AdapterEvidence: &AdapterEvidence{Values: evidence},
	}}
	return ev, ev.Validate()
}

// NewAgentHostActionReceiptEvent constructs RESUME_AGENT or TERMINATE_AGENT
// receipt evidence. The lifecycle event must already exist in the durable
// observation buffer when the engine admits the receipt.
func NewAgentHostActionReceiptEvent(id EventID, actionID string, operation HostActionOperation, provider, correlation, payloadDigest, status string, class authoring.FailureClass, evidence LifecycleEvidence) (Event, error) {
	ev := Event{ID: id, Kind: KindHostActionReceipt, HostAction: &HostActionReceiptPayload{
		ActionID: actionID, Operation: operation, Provider: provider,
		Correlation: correlation, PayloadDigest: payloadDigest, Status: status, FailureClass: class,
		LifecycleEvidence: &evidence,
	}}
	return ev, ev.Validate()
}

// NewLifecycleEventEvent 构造 lifecycle 事件：provider/identity 非空、
// 事件名属封闭集合（subagent_start/subagent_stop）。
func NewLifecycleEventEvent(id EventID, provider, identity, eventName string) (Event, error) {
	return NewCorrelatedLifecycleEvent(id, provider, identity, identity, eventName)
}

// NewCorrelatedLifecycleEvent separates transport correlation from host
// identity. Multiple paired identities may share one correlation, which is
// required to represent the UNKNOWN receipt multi-match Operator branch.
func NewCorrelatedLifecycleEvent(id EventID, provider, correlation, identity, eventName string) (Event, error) {
	ev := Event{ID: id, Kind: KindLifecycleEvent, Lifecycle: &LifecycleEventPayload{
		Provider: provider, Correlation: correlation, Identity: identity, Event: eventName,
	}}
	return ev, ev.Validate()
}

// Validate 校验 tagged union 不变量与逐 payload schema。手拼事件（绕过
// constructor）在此决定性失败：未知 kind、union 破坏、payload 缺项一律
// 可区分拒绝。kind 先于其余校验——未知 kind 是最粗的分类，不让空 ID
// 之类的细节掩盖它。
func (e Event) Validate() error {
	if !e.Kind.Valid() {
		return &RejectedError{Code: CodeUnknownEventKind, Detail: fmt.Sprintf("event kind %q is not in the closed set (no free USER_* writes exist)", e.Kind)}
	}
	if strings.TrimSpace(string(e.ID)) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "event id is empty"}
	}
	slots := []struct {
		kind     EventKind
		set      bool
		mismatch string
	}{
		{KindRequestControl, e.Request != nil, "request"},
		{KindDecide, e.Decide != nil, "decide"},
		{KindTaskProgress, e.Task != nil, "task"},
		{KindSpawnReceipt, e.Spawn != nil, "spawn"},
		{KindWorkerResult, e.Result != nil, "result"},
		{KindOperatorObservation, e.Observation != nil, "observation"},
		{KindHostActionReceipt, e.HostAction != nil, "hostAction"},
		{KindLifecycleEvent, e.Lifecycle != nil, "lifecycle"},
	}
	for _, s := range slots {
		if s.set != (s.kind == e.Kind) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("event kind %s with payload %s presence = %v (payloads outside the kind must be empty)", e.Kind, s.mismatch, s.set)}
		}
	}
	switch e.Kind {
	case KindRequestControl:
		if !e.Request.Control.Valid() {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("request control %q invalid (closed set: RESET, ABORT, RECOVER_ATTEMPT)", e.Request.Control)}
		}
		if len(e.Request.Options) == 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "request options are empty"}
		}
		seen := make(map[AskOptionID]bool, len(e.Request.Options))
		for _, option := range e.Request.Options {
			if strings.TrimSpace(string(option.ID)) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "request option id is empty"}
			}
			if strings.TrimSpace(option.Label) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("request option %q label is empty", option.ID)}
			}
			if seen[option.ID] {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("request option %q duplicated", option.ID)}
			}
			seen[option.ID] = true
		}
	case KindDecide:
		if strings.TrimSpace(string(e.Decide.Request)) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "decide request id is empty"}
		}
		if strings.TrimSpace(e.Decide.Token) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "decide freshness token is empty"}
		}
		if strings.TrimSpace(string(e.Decide.Choice)) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "decide choice is empty"}
		}
	case KindTaskProgress:
		if !e.Task.Task.Valid() {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("task progress key %q invalid", e.Task.Task.String())}
		}
		if strings.TrimSpace(e.Task.Attempt) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "task progress attempt is empty"}
		}
		switch e.Task.Status {
		case runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal:
		default:
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("task progress status %q not reportable (closed set: RUNNING, VALIDATING, TERMINAL)", e.Task.Status)}
		}
	case KindSpawnReceipt:
		if strings.TrimSpace(e.Spawn.ActionID) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "spawn receipt action id is empty"}
		}
		if strings.TrimSpace(e.Spawn.Provider) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "spawn receipt provider is empty (no default downgrade)"}
		}
		if strings.TrimSpace(e.Spawn.Correlation) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "spawn receipt correlation is empty"}
		}
		switch e.Spawn.Status {
		case SpawnStatusSpawned, SpawnStatusFailed, SpawnStatusUnknown:
		default:
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("spawn receipt status %q invalid (closed set: SPAWNED, FAILED)", e.Spawn.Status)}
		}
		if e.Spawn.Status == SpawnStatusFailed {
			if !e.Spawn.FailureClass.Valid() {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "FAILED spawn receipt requires a valid failure class"}
			}
		} else if e.Spawn.FailureClass != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "non-FAILED spawn receipt cannot carry a failure class"}
		}
	case KindWorkerResult:
		if strings.TrimSpace(e.Result.ActionID) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "worker result action id is empty"}
		}
		if strings.TrimSpace(e.Result.Provider) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "worker result provider is empty (no default downgrade)"}
		}
		if strings.TrimSpace(e.Result.PayloadDigest) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "worker result payload digest is empty"}
		}
		if e.Result.FailureClass != "" && !e.Result.FailureClass.Valid() {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("worker result failure class %q invalid", e.Result.FailureClass)}
		}
		if e.Result.Outcome == OutcomePass && e.Result.FailureClass != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "PASS worker result cannot carry a failure class"}
		}
		switch e.Result.Outcome {
		case OutcomePass, OutcomeFail, OutcomeRuntimeError:
		default:
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("worker result outcome %q invalid (closed set: PASS, FAIL, RUNTIME_ERROR)", e.Result.Outcome)}
		}
		if e.Result.Outcome != OutcomePass && !e.Result.FailureClass.Valid() {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "failed worker result requires an explicit failure class"}
		}
	case KindOperatorObservation:
		if strings.TrimSpace(e.Observation.Subject) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "operator observation subject is empty"}
		}
		if len(e.Observation.Facts) == 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "operator observation facts are empty"}
		}
		for _, fact := range e.Observation.Facts {
			if !fact.Source.Valid() {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("operator observation fact source %q invalid", fact.Source)}
			}
			if strings.TrimSpace(fact.Key) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("operator observation fact %s key is empty", fact.Source)}
			}
		}
	case KindHostActionReceipt:
		if strings.TrimSpace(e.HostAction.ActionID) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "host action receipt action id is empty"}
		}
		if !e.HostAction.Operation.Valid() {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("host action receipt operation %q is not in the closed set", e.HostAction.Operation)}
		}
		if strings.TrimSpace(e.HostAction.Provider) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "host action receipt provider is empty (no default downgrade)"}
		}
		if strings.TrimSpace(e.HostAction.Correlation) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "host action receipt correlation is empty"}
		}
		if strings.TrimSpace(e.HostAction.PayloadDigest) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "host action receipt payload digest is empty"}
		}
		switch e.HostAction.Status {
		case HostActionStatusExecuted, HostActionStatusFailed, HostActionStatusUnknown:
		default:
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("host action receipt status %q invalid (closed set: EXECUTED, FAILED)", e.HostAction.Status)}
		}
		if e.HostAction.Status == HostActionStatusFailed {
			if !e.HostAction.FailureClass.Valid() {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "FAILED host action receipt requires a valid failure class"}
			}
		} else if e.HostAction.FailureClass != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "non-FAILED host action receipt cannot carry a failure class"}
		}
		switch e.HostAction.Operation {
		case HostActionResumeAgent:
			if e.HostAction.LifecycleEvidence == nil || e.HostAction.AdapterEvidence != nil || e.HostAction.LifecycleEvidence.Event != LifecycleStart {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "RESUME_AGENT receipt requires start lifecycle evidence and no adapter evidence"}
			}
		case HostActionTerminateAgent:
			if e.HostAction.LifecycleEvidence == nil || e.HostAction.AdapterEvidence != nil || e.HostAction.LifecycleEvidence.Event != LifecycleStop {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "TERMINATE_AGENT receipt requires stop lifecycle evidence and no adapter evidence"}
			}
		case HostActionExecuteAdapterOperation:
			if e.HostAction.AdapterEvidence == nil || e.HostAction.LifecycleEvidence != nil {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "EXECUTE_ADAPTER_OPERATION receipt requires adapter evidence and no lifecycle evidence"}
			}
			if strings.TrimSpace(string(e.HostAction.AdapterOperation)) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "EXECUTE_ADAPTER_OPERATION receipt requires a registered adapter operation"}
			}
		}
		if e.HostAction.Operation != HostActionExecuteAdapterOperation && e.HostAction.AdapterOperation != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "agent HostAction receipt cannot carry an adapter operation"}
		}
		if e.HostAction.LifecycleEvidence != nil && strings.TrimSpace(e.HostAction.LifecycleEvidence.Identity) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "host action lifecycle evidence identity is empty"}
		}
	case KindLifecycleEvent:
		if strings.TrimSpace(e.Lifecycle.Provider) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "lifecycle event provider is empty (no default downgrade)"}
		}
		if strings.TrimSpace(e.Lifecycle.Identity) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "lifecycle event identity is empty"}
		}
		if strings.TrimSpace(e.Lifecycle.Correlation) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "lifecycle event correlation is empty"}
		}
		switch e.Lifecycle.Event {
		case LifecycleStart, LifecycleStop:
		default:
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("lifecycle event %q invalid (closed set: subagent_start, subagent_stop)", e.Lifecycle.Event)}
		}
	}
	return nil
}

// ---- canonical 编码与 digest（幂等判定的输入） ----

type eventWire struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	Request     *requestWire           `json:"request,omitempty"`
	Decide      *decideEventWire       `json:"decide,omitempty"`
	Task        *taskEventWire         `json:"task,omitempty"`
	Spawn       *spawnReceiptEventWire `json:"spawn,omitempty"`
	Result      *workerResultEventWire `json:"result,omitempty"`
	Observation *operatorObsWire       `json:"observation,omitempty"`
	HostAction  *hostActionReceiptWire `json:"hostAction,omitempty"`
	Lifecycle   *lifecycleEventWire    `json:"lifecycle,omitempty"`
}

type requestWire struct {
	Control string      `json:"control"`
	Options []AskOption `json:"options"`
}

type decideEventWire struct {
	Request string      `json:"request"`
	Token   string      `json:"token"`
	Choice  AskOptionID `json:"choice"`
}

type taskEventWire struct {
	Task    string `json:"task"`
	Attempt string `json:"attempt"`
	Status  string `json:"status"`
}

type spawnReceiptEventWire struct {
	ActionID     string `json:"actionId"`
	Provider     string `json:"provider"`
	Correlation  string `json:"correlation"`
	Status       string `json:"status"`
	FailureClass string `json:"failureClass,omitempty"`
}

type workerResultEventWire struct {
	ActionID      string `json:"actionId"`
	Provider      string `json:"provider"`
	Outcome       string `json:"outcome"`
	PayloadDigest string `json:"payloadDigest"`
	FailureClass  string `json:"failureClass,omitempty"`
}

type operatorObsWire struct {
	Subject string             `json:"subject"`
	Facts   []operatorFactWire `json:"facts"`
}

type operatorFactWire struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

type hostActionReceiptWire struct {
	ActionID          string             `json:"actionId"`
	Operation         string             `json:"operation"`
	AdapterOperation  string             `json:"adapterOperation,omitempty"`
	Provider          string             `json:"provider"`
	Correlation       string             `json:"correlation"`
	PayloadDigest     string             `json:"payloadDigest"`
	Status            string             `json:"status"`
	FailureClass      string             `json:"failureClass,omitempty"`
	LifecycleEvidence *LifecycleEvidence `json:"lifecycleEvidence,omitempty"`
	AdapterEvidence   *AdapterEvidence   `json:"adapterEvidence,omitempty"`
}

type lifecycleEventWire struct {
	Provider    string `json:"provider"`
	Correlation string `json:"correlation"`
	Identity    string `json:"identity"`
	Event       string `json:"event"`
}

// CanonicalBytes 返回事件的 canonical 字节（形态与 engine 各包一致：
// JSON、2 空格缩进、不转义 HTML、恰一个尾随换行；选项集与事实集保持
// 构造顺序，这是 Ask/Observation 语义的一部分）。
func (e Event) CanonicalBytes() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	w := eventWire{ID: string(e.ID), Kind: string(e.Kind)}
	if e.Request != nil {
		options := make([]AskOption, 0, len(e.Request.Options))
		options = append(options, e.Request.Options...)
		w.Request = &requestWire{Control: string(e.Request.Control), Options: options}
	}
	if e.Decide != nil {
		w.Decide = &decideEventWire{Request: string(e.Decide.Request), Token: e.Decide.Token, Choice: e.Decide.Choice}
	}
	if e.Task != nil {
		w.Task = &taskEventWire{Task: e.Task.Task.String(), Attempt: e.Task.Attempt, Status: string(e.Task.Status)}
	}
	if e.Spawn != nil {
		w.Spawn = &spawnReceiptEventWire{
			ActionID: e.Spawn.ActionID, Provider: e.Spawn.Provider,
			Correlation: e.Spawn.Correlation, Status: e.Spawn.Status, FailureClass: string(e.Spawn.FailureClass),
		}
	}
	if e.Result != nil {
		w.Result = &workerResultEventWire{
			ActionID: e.Result.ActionID, Provider: e.Result.Provider,
			Outcome: e.Result.Outcome, PayloadDigest: e.Result.PayloadDigest,
			FailureClass: string(e.Result.FailureClass),
		}
	}
	if e.Observation != nil {
		facts := make([]operatorFactWire, 0, len(e.Observation.Facts))
		for _, fact := range e.Observation.Facts {
			facts = append(facts, operatorFactWire{Source: string(fact.Source), Key: fact.Key, Value: fact.Value})
		}
		w.Observation = &operatorObsWire{Subject: e.Observation.Subject, Facts: facts}
	}
	if e.HostAction != nil {
		w.HostAction = &hostActionReceiptWire{
			ActionID: e.HostAction.ActionID, Operation: string(e.HostAction.Operation),
			AdapterOperation: string(e.HostAction.AdapterOperation),
			Provider:         e.HostAction.Provider, Correlation: e.HostAction.Correlation,
			PayloadDigest: e.HostAction.PayloadDigest, Status: e.HostAction.Status,
			FailureClass: string(e.HostAction.FailureClass), LifecycleEvidence: e.HostAction.LifecycleEvidence,
			AdapterEvidence: e.HostAction.AdapterEvidence,
		}
	}
	if e.Lifecycle != nil {
		w.Lifecycle = &lifecycleEventWire{Provider: e.Lifecycle.Provider, Correlation: e.Lifecycle.Correlation, Identity: e.Lifecycle.Identity, Event: e.Lifecycle.Event}
	}
	return canonicalJSON(w)
}

// Digest 返回事件 canonical 字节的 SHA-256 摘要：幂等台账按
// (eventID, digest) 判定重放等价。
func (e Event) Digest() (string, error) {
	data, err := e.CanonicalBytes()
	if err != nil {
		return "", err
	}
	return encoder.Digest(data), nil
}

// Acceptance 是事件接纳回执（含重放时原样返回的稳定结果）。Status 取值：
// ACCEPTED（已接纳并提交）、STAGED（result-before-receipt 暂存）、
// DUPLICATE（逐字节 payload 重发；新 event ID 仍会被 durable 占用）。
type Acceptance struct {
	EventID        string   `json:"eventId"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Revision       uint64   `json:"revision"`
	RequestID      string   `json:"requestId,omitempty"`
	FreshnessToken string   `json:"freshnessToken,omitempty"`
	ActionID       string   `json:"actionId,omitempty"`
	Refill         []string `json:"refill,omitempty"`
	FailureClass   string   `json:"failureClass,omitempty"`
	RecoveryAction string   `json:"recoveryAction,omitempty"`
}

// freshnessToken 确定性求值 freshness token（draft §2.3 availableActions
// freshness 的协议层形态）：token = sha256(canonical{revision, requestID})。
// 绑定当前 revision 意味着任何后续提交（新签发、新事件）都自然取代旧
// token——旧的提交被 STALE_FRESHNESS 拒绝且零状态变化，新的须以
// Freshness() 重新获取。
func freshnessToken(revision uint64, request RequestID) string {
	data, err := canonicalJSON(struct {
		Revision uint64 `json:"revision"`
		Request  string `json:"request"`
	}{revision, string(request)})
	if err != nil {
		// wire 只含 int 与 string，编码不会失败；保守起见落到确定值。
		return encoder.Digest([]byte(fmt.Sprintf("freshness:%d:%s", revision, request)))
	}
	return encoder.Digest(data)
}

// canonicalJSON 是本包统一的 canonical 编码（与 decision/shadow 同形态）。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("protocol: canonical encode: %w", err)
	}
	return buf.Bytes(), nil
}
