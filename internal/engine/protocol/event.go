package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/review"
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

// ControlKind 是受限 REQUEST_* 事件的封闭控制类型。RESET
// 与 ABORT 的授权路径已冻结（活动 run 的 reset/abort 必须由 Ask →
// submit 授权、不保留公开 cleanup 后门）；需求变化等业务控制经既有
// 扩展点扩充本封闭集合。
type ControlKind string

const (
	ControlValidation   ControlKind = "VALIDATION"
	ControlReset        ControlKind = "RESET"
	ControlAbort        ControlKind = "ABORT"
	ControlRecovery     ControlKind = "RECOVER_ATTEMPT"
	ControlContinueNode ControlKind = "CONTINUE_NODE"
	// 四个开发前审查/拓扑/路线业务控制（总需求 §6.2、§7.3.5、§8.1.1）：
	// Ask 创建方给出选项集，DECIDE 携带与控制一一对应的 typed Review
	// 载荷分支（REVIEW_ROUND_GRANT 的 decline 选项除外）。
	ControlReviewRoundGrant      ControlKind = "REVIEW_ROUND_GRANT"
	ControlReviewFindingDecision ControlKind = "REVIEW_FINDING_DECISION"
	ControlConfirmTopology       ControlKind = "CONFIRM_TOPOLOGY"
	ControlSelectRoute           ControlKind = "SELECT_ROUTE"
	// ControlRequestRequirementChange 是用户需求变化控制（总需求 §6.3.2/
	// §7.2）：Ask 期间进入修订屏障，DECIDE 以封闭 choice id（proceed/
	// abort）锚定——proceed 必携 typed Requirement 载荷登记新确认 revision
	// 并机械执行用户勾选的重走范围，abort 不携载荷、不登记。
	ControlRequestRequirementChange ControlKind = "REQUEST_REQUIREMENT_CHANGE"
)

// Valid 报告 k 是否属于封闭控制集合。
func (k ControlKind) Valid() bool {
	switch k {
	case ControlValidation, ControlReset, ControlAbort, ControlRecovery, ControlContinueNode,
		ControlReviewRoundGrant, ControlReviewFindingDecision, ControlConfirmTopology, ControlSelectRoute,
		ControlRequestRequirementChange:
		return true
	}
	return false
}

// reviewControl 报告控制是否属于四个审查类业务控制——它们的 DECIDE
// 必须携 Review 载荷（REVIEW_ROUND_GRANT 的 decline 例外），其余控制的
// DECIDE 携 Review 载荷一律 schema 拒绝。
func reviewControl(k ControlKind) bool {
	switch k {
	case ControlReviewRoundGrant, ControlReviewFindingDecision, ControlConfirmTopology, ControlSelectRoute:
		return true
	}
	return false
}

// AskOption 是 pending Ask 落账的单个选项。
type AskOption struct {
	ID    AskOptionID `json:"id"`
	Label string      `json:"label"`
}

// EventKind 是提交事件的封闭 kind 集合。刻意
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
	// KindSpawnReceipt：宿主对一次签发的 SpawnReceipt（actionID/
	// correlation/provider/status 公共字段）。接纳后任务进入
	// 声明签发态并落账 receipt。
	KindSpawnReceipt EventKind = "SPAWN_RECEIPT"
	// KindWorkerResult：worker 的 typed result（actionID/provider/outcome/
	// payload digest）。receipt 未到时暂存（result-before-receipt），配对
	// 后接纳并按容量补位签发。
	KindWorkerResult EventKind = "WORKER_RESULT"
	// KindOperatorObservation：主代理的 typed observation（
	// Operator：核实事实、不替用户授权），入账并绑定来源对账项。
	KindOperatorObservation EventKind = "OPERATOR_OBSERVATION"
	// KindHostActionReceipt：宿主对 HostAction 的回执；接纳后 pending
	// intent 清账（先持久化 intent 再执行）。
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

// RequestPayload starts a typed control Ask. A requirement request may start
// without a package to freeze work during clarification. A later update binds
// the complete package to the same Ask before the user confirms it.
type RequestPayload struct {
	Continuation *NodeContinuationRequest
	Handoff      *DevelopmentHandoffRequest
	Validation   *ValidationRequest
	// Request and Token bind a package update to an existing requirement Ask.
	// Empty Request starts a new control request, including a package-free barrier.
	Request RequestID
	Token   string
	Control ControlKind
	Options []AskOption
	Package *RequirementDecisionPackage
	// RouteGates 是 SELECT_ROUTE 请求创建时声明的候选门全集，接纳落账时
	// 钉进 pending Ask：用户确认边界以此为唯一权威，DECIDE 载荷的候选表
	// 必须与之集合相等。
	RouteGates []string
}

// DevelopmentHandoffRequest binds a same-host transfer to its current developer.
type DevelopmentHandoffRequest struct {
	ActionID string `json:"actionId"`
	Reason   string `json:"reason"`
}

// DecidePayload 是 DECIDE 的 payload：目标 request、freshness token 与
// 选中选项（选项合法性对 pending Ask 的落账选项集校验，属接纳层）。审查
// 类业务控制额外携带 typed Review 载荷（分支与控制一一对应）；需求变化
// 控制携带 typed Requirement 载荷（或作为 decline 两者皆空）。两个变体
// 指针同时只允许一个非空。
type DecidePayload struct {
	Continuation *NodeContinuationDecision
	Validation   *ValidationDecision
	Request      RequestID
	Token        string
	Choice       AskOptionID
	Review       *ReviewDecisionPayload
	Requirement  *RequirementDecisionPayload
	Reset        *ResetDecision
}

// TaskPayload 是 TASK_PROGRESS 的 payload：目标任务、期望的当前
// Attempt 与观察到的进度状态。
type TaskPayload struct {
	Task    runtime.TaskKey
	Attempt string
	Status  runtime.TaskStatus
}

// SpawnReceiptPayload 是 SPAWN_RECEIPT 的 payload：的公共
// receipt 字段子集（actionID/correlation/provider/status）。Correlation
// 是宿主侧 agent 身份关联，lifecycle 配对按它认领。
type SpawnReceiptPayload struct {
	ActionID     string
	Provider     string
	Correlation  string
	Status       string
	FailureClass authoring.FailureClass
}

// SpawnReceipt status 的封闭集合：SPAWNED 声明派发成功，FAILED 声明失败。
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
// 的 payload digest。Review 变体是审查派发的语义证据（参照
// HostActionReceiptPayload 的变体指针先例）：只在 Outcome 为 PASS 的
// 结果上携带，RUNTIME_ERROR/FAIL 的审查结果是普通失败结果、不建
// candidate。
type WorkerResultPayload struct {
	SourceResult       string `json:"sourceResult,omitempty"`
	InterruptionReason string
	InvalidReason      string
	Validation         *ValidationResult
	ActionID           string
	Provider           string
	Outcome            string
	PayloadDigest      string
	FailureClass       authoring.FailureClass
	Review             *ReviewResultEvidence
}

// ReviewResultEvidence 是审查结果的完整语义证据：绑定动作、派发身份、
// 需求 revision 与（越过自动上限时的）复审授权，以及完整 raw 结论。
// 独立 Operator 核实不属于 worker result。只有 PASS/FAIL 是语义状态；身份与
// 摘要由 review 内核在接纳时派生。
type ReviewResultEvidence struct {
	GranularityReview   *GranularityReview `json:"granularityReview,omitempty"`
	Action              review.Action      `json:"action"`
	DispatchID          string             `json:"dispatchId"`
	RequirementRevision string             `json:"requirementRevision"`
	AuthorizationID     string             `json:"authorizationId,omitempty"`
	Status              string             `json:"status"`
	Message             string             `json:"message,omitempty"`
	Findings            []review.Finding   `json:"findings"`
}

// ReviewDecisionPayload 是审查类业务控制的 DECIDE typed 载荷：小 tagged
// union，四个分支各一指针、与 ControlKind 一一对应，同时只允许一个分支
// 非空（Validate 强制，控制↔分支配对由接纳层对 pending Ask 校验）。
type ReviewDecisionPayload struct {
	RoundGrant *RoundGrantDecision     `json:"roundGrant,omitempty"`
	Correction *review.CorrectionInput `json:"correction,omitempty"`
	Topology   *TopologyDecision       `json:"topology,omitempty"`
	Route      *RouteDecision          `json:"route,omitempty"`
}

// RoundGrantDecision 为某审查动作授权恰好一轮额外语义轮（总需求
// §6.3.6）：授权先于派发存在，只绑定动作与理由；授权不能预先累积或转给
// 另一动作，空槽由签发点自动绑上首个派发。
type RoundGrantDecision struct {
	Action review.Action `json:"action"`
	Reason string        `json:"reason"`
}

// TopologyDecision 是 CONFIRM_TOPOLOGY 的精确拓扑决定：no-split 必带
// 理由 note；split 必带 SplitCount≥2、与之一致的 Slices 与并行度
// start 不声明拆分意向，绑定唯一发生在此处。ACOwners 是 split 的验收条件
// 主责声明列表——载荷以列表
// 形态携带使重复声明可区分拒绝，绑定点归一为 map 后落账。
type TopologyDecision struct {
	DevelopmentPlan *DevelopmentPlanConfirmation `json:"developmentPlan,omitempty"`
	Decision        string                       `json:"decision"`
	Note            string                       `json:"note,omitempty"`
	SplitCount      int                          `json:"splitCount,omitempty"`
	Slices          []string                     `json:"slices,omitempty"`
	Parallel        string                       `json:"parallel,omitempty"`
	ACOwners        []ACOwner                    `json:"acOwners,omitempty"`
}

// ACOwner 是 split 拓扑决定里的单条验收条件主责声明（AC-ID → 分片名或
// master-merge）。owner 引用的合法性（∈ Slices ∪ {master-merge}）与 AC
// 宇宙由绑定点对确认需求校验。
type ACOwner struct {
	AC    string `json:"ac"`
	Owner string `json:"owner"`
}

// RouteDecision records the candidate gates, configuration-derived selection
// and the user's development plan confirmation.
type RouteDecision struct {
	DevelopmentPlan *DevelopmentPlanConfirmation `json:"developmentPlan,omitempty"`
	Gates           []string                     `json:"gates"`
	Selected        []string                     `json:"selected"`
}

// RequirementDecisionPackage binds the revision, impact preview and each
// downstream work disposition shown on the requirement-change Ask.
type RequirementDecisionPackage struct {
	Intake             *IntakeConfirmationReceipt `json:"intake,omitempty"`
	Coverage           *CoverageInventory         `json:"coverage,omitempty"`
	CandidateRevision  string                     `json:"candidateRevision"`
	DeltaDigest        string                     `json:"deltaDigest"`
	ImpactDigest       string                     `json:"impactDigest"`
	Source             string                     `json:"source"`
	Classification     string                     `json:"classification"`
	RedoProductReview  bool                       `json:"redoProductReview"`
	RedoStartReadiness bool                       `json:"redoStartReadiness"`
	RedoTopology       bool                       `json:"redoTopology"`
	RedoRoute          bool                       `json:"redoRoute"`
	WorkDecisions      []RequirementWorkDecision  `json:"workDecisions,omitempty"`
	Children           []ChildRequirementDecision `json:"children,omitempty"`
	Impact             *RequirementImpact         `json:"impact,omitempty"`
	ResetRequestID     string                     `json:"resetRequestId,omitempty"`
	FindingDigest      string                     `json:"findingDigest,omitempty"`
}

// packageView 投影出 DECIDE 载荷中的包字段，供与 Ask 落账包的逐字段
// 相等比较。
func (p *RequirementDecisionPayload) packageView() RequirementDecisionPackage {
	return RequirementDecisionPackage{Intake: p.Intake, Coverage: p.Coverage,
		CandidateRevision:  p.CandidateRevision,
		DeltaDigest:        p.DeltaDigest,
		ImpactDigest:       p.ImpactDigest,
		Source:             p.Source,
		Classification:     p.Classification,
		RedoProductReview:  p.RedoProductReview,
		RedoStartReadiness: p.RedoStartReadiness,
		RedoTopology:       p.RedoTopology,
		RedoRoute:          p.RedoRoute,
		WorkDecisions:      p.WorkDecisions,
		Children:           p.Children, Impact: p.Impact, ResetRequestID: p.ResetRequestID,
		FindingDigest: p.FindingDigest,
	}
}

// RequirementDecisionPayload 是 REQUEST_REQUIREMENT_CHANGE 的 DECIDE
// typed 载荷（总需求 §7.2.1）：在 Ask 落账的确认包之外补充 BaseRevision
// （CAS 基线，必须等于 Ask 落账基线与当前权威 revision）与 Reason（决定
// 理由，随决定落账）。其余字段必须与包逐字段相等。
type RequirementDecisionPayload struct {
	Intake             *IntakeConfirmationReceipt `json:"intake,omitempty"`
	Coverage           *CoverageInventory         `json:"coverage,omitempty"`
	BaseRevision       string                     `json:"baseRevision"`
	CandidateRevision  string                     `json:"candidateRevision"`
	DeltaDigest        string                     `json:"deltaDigest"`
	ImpactDigest       string                     `json:"impactDigest"`
	Source             string                     `json:"source"`
	Classification     string                     `json:"classification"`
	RedoProductReview  bool                       `json:"redoProductReview"`
	RedoStartReadiness bool                       `json:"redoStartReadiness"`
	RedoTopology       bool                       `json:"redoTopology"`
	RedoRoute          bool                       `json:"redoRoute"`
	WorkDecisions      []RequirementWorkDecision  `json:"workDecisions,omitempty"`
	Children           []ChildRequirementDecision `json:"children,omitempty"`
	Impact             *RequirementImpact         `json:"impact,omitempty"`
	ResetRequestID     string                     `json:"resetRequestId,omitempty"`
	Reason             string                     `json:"reason"`
	FindingDigest      string                     `json:"findingDigest,omitempty"`
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
	Review     *ReviewVerificationObservation `json:"review,omitempty"`
	Validation *ValidationObservation         `json:"validation,omitempty"`
	Subject    string                         `json:"subject"`
	Facts      []decision.Fact                `json:"facts,omitempty"`
}

// HostActionReceiptPayload 是 HOST_ACTION_RECEIPT 的 payload：
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
	// HostActionStatusReconciled is an internal durable status written after
	// external observation confirms an UNKNOWN side effect. It is not accepted
	// as a host-submitted receipt status.
	HostActionStatusReconciled = "RECONCILED"
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
	nodeParent          *Snapshot
	resetPreview        *ResetPreview
	requirementDocument string
	ID                  EventID
	Kind                EventKind
	Request             *RequestPayload
	Decide              *DecidePayload
	Task                *TaskPayload
	Spawn               *SpawnReceiptPayload
	Result              *WorkerResultPayload
	Observation         *OperatorObservationPayload
	HostAction          *HostActionReceiptPayload
	Lifecycle           *LifecycleEventPayload
}

// NewRequestEvent 构造受限 REQUEST_* 事件：ID 非空、控制类型合法、
// 选项集非空且 ID 唯一非空、标签非空。
func NewRequestEvent(id EventID, control ControlKind, options ...AskOption) (Event, error) {
	ev := Event{ID: id, Kind: KindRequestControl, Request: &RequestPayload{Control: control, Options: append([]AskOption(nil), options...)}}
	return ev, ev.Validate()
}

// NewRequirementRequestEvent 构造携变更包的需求变化请求：包形状在此校
// 验；控制固定为 REQUEST_REQUIREMENT_CHANGE，选项集由请求方枚举
// （proceed/abort）。
func NewRequirementRequestEvent(id EventID, pkg RequirementDecisionPackage, options ...AskOption) (Event, error) {
	ev := Event{ID: id, Kind: KindRequestControl, Request: &RequestPayload{Control: ControlRequestRequirementChange, Options: append([]AskOption(nil), options...), Package: &pkg}}
	return ev, ev.Validate()
}

// NewRequirementPackageEvent binds the prepared package to the existing barrier
// without resolving it. Its new token must be fetched before user confirmation.
func NewRequirementPackageEvent(id EventID, request RequestID, token string, pkg RequirementDecisionPackage) (Event, error) {
	ev := Event{ID: id, Kind: KindRequestControl, Request: &RequestPayload{Control: ControlRequestRequirementChange, Request: request, Token: token, Package: &pkg}}
	return ev, ev.Validate()
}

// NewRouteRequestEvent 构造 SELECT_ROUTE 请求：候选门全集随请求声明并在
// 此校验形状（非空、无重复、不含合并验证）；全集在接纳落账时钉进
// pending Ask，决定的候选表必须与之集合相等。
func NewRouteRequestEvent(id EventID, gates []string, options ...AskOption) (Event, error) {
	if err := validateRouteGateCandidates(gates); err != nil {
		return Event{}, err
	}
	ev := Event{ID: id, Kind: KindRequestControl, Request: &RequestPayload{
		Control: ControlSelectRoute, Options: append([]AskOption(nil), options...), RouteGates: append([]string(nil), gates...),
	}}
	return ev, ev.Validate()
}

// NewDecideEvent 构造用户决定事件：request/token/选项均非空。
func NewDecideEvent(id EventID, request RequestID, token string, choice AskOptionID) (Event, error) {
	ev := Event{ID: id, Kind: KindDecide, Decide: &DecidePayload{Request: request, Token: token, Choice: choice}}
	return ev, ev.Validate()
}

// NewReviewDecideEvent 构造携带 typed Review 载荷的决定事件：审查类业务
// 控制的 DECIDE 入口。载荷的 union 与分支形状在此校验；控制↔分支配对
// 与账目变换由接纳层对 pending Ask 执行。
func NewReviewDecideEvent(id EventID, request RequestID, token string, choice AskOptionID, payload ReviewDecisionPayload) (Event, error) {
	ev := Event{ID: id, Kind: KindDecide, Decide: &DecidePayload{Request: request, Token: token, Choice: choice, Review: &payload}}
	return ev, ev.Validate()
}

// NewRequirementDecideEvent 构造携带 typed Requirement 载荷的决定事件：
// REQUEST_REQUIREMENT_CHANGE 的 DECIDE 入口。载荷形状在此校验；CAS、
// 分类规则与重走范围的账目变换由接纳层对 pending Ask 执行。
func NewRequirementDecideEvent(id EventID, request RequestID, token string, choice AskOptionID, payload RequirementDecisionPayload) (Event, error) {
	ev := Event{ID: id, Kind: KindDecide, Decide: &DecidePayload{Request: request, Token: token, Choice: choice, Requirement: &payload}}
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
// 原 action 的执行绑定硬校验（不降级 default）。
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

// NewWorkerResultEvent 构造不携语义证据的普通 worker result。失败结果
// 必须显式携带封闭 failure class；PASS 结果必须传空 class。
func NewWorkerResultEvent(id EventID, actionID, provider, outcome, payloadDigest string, class authoring.FailureClass) (Event, error) {
	ev := Event{ID: id, Kind: KindWorkerResult, Result: &WorkerResultPayload{
		ActionID: actionID, Provider: provider, Outcome: outcome, PayloadDigest: payloadDigest, FailureClass: class,
	}}
	return ev, ev.Validate()
}

// NewInvalidWorkerResultEvent records program rejection of source-verified
// semantic output. It preserves successful transport without a business PASS.
func NewInvalidWorkerResultEvent(id EventID, actionID, provider, digest, reason string) (Event, error) {
	if strings.TrimSpace(reason) == "" {
		return Event{}, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "invalid semantic result requires a reason"}
	}
	e := Event{ID: id, Kind: KindWorkerResult, Result: &WorkerResultPayload{ActionID: actionID, Provider: provider, Outcome: OutcomePass, PayloadDigest: digest, InvalidReason: reason}}
	return e, e.Validate()
}

// NewReviewWorkerResultEvent 构造携带审查证据的 worker result：outcome
// 固定为 PASS（审查派发完成并产出语义证据；runtime 错误走普通失败结果，
// 不建 candidate），payload digest 与 evidence 一并纳入 canonical 编码。
func NewReviewWorkerResultEvent(id EventID, actionID, provider, payloadDigest string, evidence ReviewResultEvidence) (Event, error) {
	ev := Event{ID: id, Kind: KindWorkerResult, Result: &WorkerResultPayload{
		ActionID: actionID, Provider: provider, Outcome: OutcomePass, PayloadDigest: payloadDigest, Review: &evidence,
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
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf(
				"request control %q invalid (closed set: %s, %s, %s, %s, %s, %s, %s, %s)",
				e.Request.Control, ControlReset, ControlAbort, ControlRecovery,
				ControlReviewRoundGrant, ControlReviewFindingDecision, ControlConfirmTopology, ControlSelectRoute,
				ControlRequestRequirementChange)}
		}
		if e.Request.Handoff != nil && (e.Request.Control != ControlRecovery || len(e.Request.Options) != 0) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "development handoff uses a recovery request with engine-owned options"}
		}
		if len(e.Request.Options) == 0 && e.Request.Request == "" && e.Request.Handoff == nil {
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
		if e.Request.Control == ControlRequestRequirementChange {
			if e.Request.Request != "" {
				if e.Request.Token == "" || e.Request.Package == nil || len(e.Request.Options) != 0 {
					return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a requirement package update needs its request, token and full package; options belong to the existing Ask"}
				}
			} else if e.Request.Token != "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a new request does not take an update token"}
			}
			if e.Request.Package != nil {
				if err := validateRequirementPackage(e.Request.Package); err != nil {
					return err
				}
			}
		} else if e.Request.Package != nil || e.Request.Request != "" || e.Request.Token != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s does not carry a requirement package update", e.Request.Control)}
		}
		if e.Request.Control == ControlContinueNode {
			if e.Request.Continuation == nil || e.Request.Continuation.Target == "" || e.Request.Continuation.PreviewDigest == "" {
				return validationError("节点继续请求缺少原目标预览")
			}
		} else if e.Request.Continuation != nil {
			return validationError("该请求不接受节点继续载荷")
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
		if e.Decide.Review != nil && e.Decide.Requirement != nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "decide payload must carry at most one typed variant (review or requirement)"}
		}
		if e.Decide.Continuation != nil {
			if e.Decide.Review != nil || e.Decide.Requirement != nil || e.Decide.Reset != nil || e.Decide.Validation != nil {
				return validationError("节点继续决定只接受其 typed 载荷")
			}
			if err := validateWorkDecisionShape(e.Decide.Continuation.Work); err != nil {
				return err
			}
		}
		if e.Decide.Review != nil {
			if err := validateReviewDecisionPayload(e.Decide.Review); err != nil {
				return err
			}
		}
		if e.Decide.Requirement != nil {
			if err := validateRequirementDecisionPayload(e.Decide.Requirement); err != nil {
				return err
			}
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
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf(
				"spawn receipt status %q invalid (closed set: %s, %s, %s)",
				e.Spawn.Status, SpawnStatusSpawned, SpawnStatusFailed, SpawnStatusUnknown)}
		}
		if e.Spawn.Status == SpawnStatusFailed {
			if !e.Spawn.FailureClass.Valid() {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "FAILED spawn receipt requires a valid failure class"}
			}
		} else if e.Spawn.FailureClass != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "non-FAILED spawn receipt cannot carry a failure class"}
		}
	case KindWorkerResult:
		if e.Result.InvalidReason != "" && (strings.TrimSpace(e.Result.InvalidReason) == "" || e.Result.Outcome != OutcomePass || e.Result.FailureClass != "" || e.Result.Review != nil || e.Result.Validation != nil || e.Result.InterruptionReason != "") {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "invalid semantic result requires successful transport, a real reason and no completed evidence"}
		}
		if e.Result.InterruptionReason != "" && e.Result.Outcome != OutcomeRuntimeError {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "interruption reason requires a runtime error"}
		}
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
		if e.Result.Review != nil {
			// 审查证据是完整语义裁决，只能由成功完成的派发携带
			// （NewReviewWorkerResultEvent 固定 OutcomePass）；崩溃或失败的
			// 结果是普通失败结果，不产生语义候选。
			if e.Result.Outcome != OutcomePass {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("review evidence requires a PASS outcome (got %q): a failed or crashed dispatch is not a semantic review verdict", e.Result.Outcome)}
			}
		}
	case KindOperatorObservation:
		if strings.TrimSpace(e.Observation.Subject) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "operator observation subject is empty"}
		}
		if len(e.Observation.Facts) == 0 && e.Observation.Validation == nil && e.Observation.Review == nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "operator observation facts are empty"}
		}
		if e.Observation.Review != nil && (e.Observation.Validation != nil || len(e.Observation.Facts) != 0) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "review verification cannot contain another observation role"}
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
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf(
				"host action receipt status %q invalid (closed set: %s, %s, %s)",
				e.HostAction.Status, HostActionStatusExecuted, HostActionStatusFailed, HostActionStatusUnknown)}
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
	Continuation *NodeContinuationRequest    `json:"continuation,omitempty"`
	Handoff      *DevelopmentHandoffRequest  `json:"handoff,omitempty"`
	Validation   *ValidationRequest          `json:"validation,omitempty"`
	Request      string                      `json:"request,omitempty"`
	Token        string                      `json:"token,omitempty"`
	Control      string                      `json:"control"`
	Options      []AskOption                 `json:"options"`
	Package      *RequirementDecisionPackage `json:"package,omitempty"`
	RouteGates   []string                    `json:"routeGates,omitempty"`
}

type decideEventWire struct {
	Continuation *NodeContinuationDecision   `json:"continuation,omitempty"`
	Reset        *ResetDecision              `json:"reset,omitempty"`
	Validation   *ValidationDecision         `json:"validation,omitempty"`
	Request      string                      `json:"request"`
	Token        string                      `json:"token"`
	Choice       AskOptionID                 `json:"choice"`
	Review       *ReviewDecisionPayload      `json:"review,omitempty"`
	Requirement  *RequirementDecisionPayload `json:"requirement,omitempty"`
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
	SourceResult       string                `json:"sourceResult,omitempty"`
	InterruptionReason string                `json:"interruptionReason,omitempty"`
	InvalidReason      string                `json:"invalidReason,omitempty"`
	Validation         *ValidationResult     `json:"validation,omitempty"`
	ActionID           string                `json:"actionId"`
	Provider           string                `json:"provider"`
	Outcome            string                `json:"outcome"`
	PayloadDigest      string                `json:"payloadDigest,omitempty"`
	FailureClass       string                `json:"failureClass,omitempty"`
	Review             *ReviewResultEvidence `json:"review,omitempty"`
}

type operatorObsWire struct {
	Review     *ReviewVerificationObservation `json:"review,omitempty"`
	Validation *ValidationObservation         `json:"validation,omitempty"`
	Subject    string                         `json:"subject"`
	Facts      []operatorFactWire             `json:"facts"`
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
		w.Request = &requestWire{Handoff: e.Request.Handoff, Validation: e.Request.Validation, Control: string(e.Request.Control), Options: options, Package: e.Request.Package, Request: string(e.Request.Request), Token: e.Request.Token, RouteGates: e.Request.RouteGates}
		w.Request.Continuation = e.Request.Continuation
	}
	if e.Decide != nil {
		w.Decide = &decideEventWire{Reset: e.Decide.Reset, Validation: e.Decide.Validation, Request: string(e.Decide.Request), Token: e.Decide.Token, Choice: e.Decide.Choice, Review: e.Decide.Review, Requirement: e.Decide.Requirement}
		w.Decide.Continuation = e.Decide.Continuation
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
			SourceResult: e.Result.SourceResult, InterruptionReason: e.Result.InterruptionReason, InvalidReason: e.Result.InvalidReason, FailureClass: string(e.Result.FailureClass), Review: e.Result.Review, Validation: e.Result.Validation,
		}
	}
	if e.Observation != nil {
		facts := make([]operatorFactWire, 0, len(e.Observation.Facts))
		for _, fact := range e.Observation.Facts {
			facts = append(facts, operatorFactWire{Source: string(fact.Source), Key: fact.Key, Value: fact.Value})
		}
		w.Observation = &operatorObsWire{Review: e.Observation.Review, Validation: e.Observation.Validation, Subject: e.Observation.Subject, Facts: facts}
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
// QUARANTINED（修订屏障期间的持久隔离，待需求决定同事务重分类）、
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

// freshnessToken 确定性求值 freshness token：token = sha256(canonical{revision, requestID})。
// 绑定当前 revision 意味着任何后续提交（新签发、新事件）都自然取代旧
// token——旧的提交被 STALE_FRESHNESS 拒绝且零状态变化，新的须以
// Freshness() 重新获取。
func freshnessToken(revision uint64, request RequestID) string {
	data, _ := canonicalJSON(struct {
		Revision uint64 `json:"revision"`
		Request  string `json:"request"`
	}{revision, string(request)})
	return encoder.Digest(data)
}

// canonicalJSON 是本包统一的 canonical 编码。
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

// WithResetPreview carries facade-observed document registration into the existing
// request handler. It is engine input, not part of the external event identity.
func (e Event) WithResetPreview(preview ResetPreview) Event { e.resetPreview = &preview; return e }

// WithRequirementDocument carries the original confirmed material into the
// decision transaction. Its issued candidate revision binds the content digest.
func (e Event) WithRequirementDocument(document string) Event {
	e.requirementDocument = document
	return e
}

// WithNodeParent carries the original observed master inside the existing
// parent-to-child native window; it is not an external user payload.
func (e Event) WithNodeParent(parent Snapshot) Event { e.nodeParent = &parent; return e }
