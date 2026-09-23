// Package review 承载开发前审查（product-review 与 start-readiness）系列的
// 纯数据模型与纯函数内核：不可变 raw candidate 只作证据，逐 finding 的
// adjudication 与 correction receipt 记录用户裁决，effective status 由账本
// 确定性投影。所有变换值进值出、不做 I/O；身份与摘要由 canonical JSON 编码
// 派生，防篡改校验重算全部投影与身份链。
package review

import "fmt"

// Action 是开发前审查动作的封闭枚举，恰两值；Controller 按 run 与动作各自
// 持有一条独立 series。
type Action string

const (
	// ProductReview 是整体产品语义审查动作。
	ProductReview Action = "product-review"
	// StartReadiness 是开工就绪审查动作。
	StartReadiness Action = "start-readiness"
)

// 状态、决定与账本的封闭词汇：这些字符串是持久化契约的一部分。
const (
	statusPending = "PENDING"
	statusPass    = "PASS"
	statusFail    = "FAIL"

	decisionConfirm           = "CONFIRM"
	decisionDismiss           = "DISMISS"
	decisionReseveritize      = "RESEVERITIZE"
	decisionInvalidateFinding = "INVALIDATE_FINDING"
	decisionInvalidateResult  = "INVALIDATE_RESULT"

	invalidityInvalid    = "INVALID"
	invalidityIncomplete = "INCOMPLETE"
	invalidityStale      = "STALE"
	invalidityMisbound   = "MISBOUND"

	completenessComplete    = "COMPLETE"
	activationWorkflowStart = "WORKFLOW_START"
	authorizationUserSource = "USER_REQUESTED"

	nextRequirementAutomatic  = "AUTOMATIC_CAPACITY"
	nextRequirementAuthorized = "AUTHORIZED_EXTRA_ROUND"
	nextRequirementUser       = "USER_DECISION_REQUIRED"

	// automaticRoundLimit 是 workflow policy 预授权的自动语义轮上限；越过
	// 它的每一轮都必须由用户逐次授权。
	automaticRoundLimit = 3
)

// Set 是一个 run 的两条审查 series 的封闭容器：恰 product-review 与
// start-readiness 两条，用固定字段的 struct 而非 map，保证 JSON 序列化确定
// 且不存在缺条目的部分投影。
type Set struct {
	ProductReview  Series `json:"productReview"`
	StartReadiness Series `json:"startReadiness"`
}

// NewSet 建立两条 series 的初始态：PENDING、自动上限 3、四个账本非 nil、
// 容量投影齐平。
func NewSet() Set {
	return Set{
		ProductReview:  newSeries(ProductReview),
		StartReadiness: newSeries(StartReadiness),
	}
}

// Series 返回指定动作的账本；未知动作返回 false。
func (s Set) Series(action Action) (Series, bool) {
	switch action {
	case ProductReview:
		return s.ProductReview, true
	case StartReadiness:
		return s.StartReadiness, true
	}
	return Series{}, false
}

// seriesFor 返回指向调用方 Set 内对应 series 的可变指针——接纳路径经它就地
// 改账并返回同一 Set 值；未知动作是调用方契约错误。
func (s *Set) seriesFor(action Action) (*Series, error) {
	switch action {
	case ProductReview:
		return &s.ProductReview, nil
	case StartReadiness:
		return &s.StartReadiness, nil
	}
	return nil, fmt.Errorf("review: unknown pre-development review action %q", action)
}

func newSeries(action Action) Series {
	return Series{
		Action:               action,
		ActivationSource:     activationWorkflowStart,
		AutomaticLimit:       automaticRoundLimit,
		CurrentLimit:         automaticRoundLimit,
		RemainingAutomatic:   automaticRoundLimit,
		RemainingCapacity:    automaticRoundLimit,
		NextRoundRequirement: nextRequirementAutomatic,
		Authorizations:       []RoundAuthorization{},
		RawCandidates:        []RawCandidate{},
		Adjudications:        []Adjudication{},
		Corrections:          []CorrectionReceipt{},
		Effective: EffectiveStatus{
			Status:             statusPending,
			Source:             activationWorkflowStart,
			BlockingFindingIDs: []string{},
			AdvisoryFindingIDs: []string{},
			DownstreamImpact:   []string{},
		},
	}
}

// Series 是单动作的审查账本。Completed 与容量字段是从 RawCandidates、
// Corrections 与 Authorizations 投影出的缓存，每次接纳 candidate 或
// correction 后由 refresh 重算；防篡改校验会强制它们与账本一致。
type Series struct {
	Action               Action               `json:"action"`
	ActivationSource     string               `json:"activationSource"`
	AutomaticLimit       int                  `json:"automaticLimit"`
	Completed            int                  `json:"completed"`
	CurrentLimit         int                  `json:"currentLimit"`
	RemainingAutomatic   int                  `json:"remainingAutomatic"`
	RemainingCapacity    int                  `json:"remainingCapacity"`
	NextRoundRequirement string               `json:"nextRoundRequirement"`
	ExtraAuthorized      int                  `json:"extraAuthorized"`
	ExtraConsumed        int                  `json:"extraConsumed"`
	Authorizations       []RoundAuthorization `json:"authorizations"`
	RawCandidates        []RawCandidate       `json:"rawCandidates"`
	Adjudications        []Adjudication       `json:"adjudications"`
	Corrections          []CorrectionReceipt  `json:"corrections"`
	Effective            EffectiveStatus      `json:"effectiveReviewStatus"`
}

// RoundAuthorization 记录一次动作内、用户请求的额外语义轮授权。运行期重试
// 可以把多个 dispatch 绑到同一授权上，但至多一个未被整轮失效的结果占用它。
type RoundAuthorization struct {
	ID                    string   `json:"id"`
	Action                Action   `json:"action"`
	Source                string   `json:"source"`
	Reason                string   `json:"reason"`
	DispatchIDs           []string `json:"dispatchIds"`
	RecordedResultDigests []string `json:"recordedResultDigests"`
}

// RawCandidate 是经生命周期、绑定、语义形状与 Operator 核实后接纳的不可变
// 审查证据。它只作证据：任何人都不能把它直接写成权威 PASS/FAIL，权威结论
// 由账本投影为 EffectiveStatus。
type RawCandidate struct {
	ID                   string               `json:"id"`
	Action               Action               `json:"action"`
	DispatchID           string               `json:"dispatchId"`
	ResultDigest         string               `json:"resultDigest"`
	RequirementRevision  string               `json:"requirementRevision"`
	RawStatus            string               `json:"rawStatus"`
	Message              string               `json:"message,omitempty"`
	Findings             []Finding            `json:"findings"`
	Completeness         string               `json:"completeness"`
	AuthorizationID      string               `json:"authorizationId,omitempty"`
	OperatorVerification OperatorVerification `json:"operatorVerification"`
}

// Finding 是候选内的一条原始发现。ID 与 Digest 只由十个内容字段派生，与
// 后续用户处置或严重度纠正无关，因此同一发现跨轮次可稳定引用。
type Finding struct {
	ID                string              `json:"id"`
	Digest            string              `json:"digest"`
	Severity          string              `json:"severity"`
	SeverityRationale string              `json:"severityRationale,omitempty"`
	Category          string              `json:"category,omitempty"`
	Requirement       string              `json:"requirement,omitempty"`
	NormalPath        string              `json:"normalPath,omitempty"`
	ActualError       string              `json:"actualError,omitempty"`
	DeferralImpact    string              `json:"deferralImpact,omitempty"`
	Verification      FindingVerification `json:"verification,omitempty"`
	Message           string              `json:"message"`
	Locations         []string            `json:"locations,omitempty"`
}

// FindingVerification 是发现内容的一部分：五个核实维度各自的自然语言记录。
type FindingVerification struct {
	Scope          string `json:"scope,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Causality      string `json:"causality,omitempty"`
	Reproduction   string `json:"reproduction,omitempty"`
	Classification string `json:"classification,omitempty"`
}

// OperatorVerification 记录接纳候选前 Operator 明确完成的核实。八项检查是
// 封闭集：缺、重、未知或证据为空都拒绝。
type OperatorVerification struct {
	Checks   []string `json:"checks,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
}

// Adjudication 是一条 finding 的首个用户裁决。后续变更不覆盖它，只追加
// CorrectionReceipt。
type Adjudication struct {
	ID                  string `json:"id"`
	RunID               string `json:"runId"`
	Action              Action `json:"action"`
	DispatchID          string `json:"dispatchId"`
	ResultDigest        string `json:"resultDigest"`
	RequirementRevision string `json:"requirementRevision"`
	FindingID           string `json:"findingId"`
	Decision            string `json:"decision"`
	Severity            string `json:"severity,omitempty"`
	SeverityRationale   string `json:"severityRationale,omitempty"`
	OldValue            string `json:"oldValue"`
	NewValue            string `json:"newValue"`
	Reason              string `json:"reason"`
	Source              string `json:"source"`
	UserAuthorized      bool   `json:"userAuthorized"`
	EffectiveRevision   int    `json:"effectiveRevision"`
}

// CorrectionReceipt 取代此前的 adjudication/correction，或把一条完整结果整
// 轮判为失效。原始 candidate 与被取代的旧决定都不删除，只被本回执标记。
type CorrectionReceipt struct {
	ID                  string `json:"id"`
	RunID               string `json:"runId"`
	Action              Action `json:"action"`
	DispatchID          string `json:"dispatchId"`
	ResultDigest        string `json:"resultDigest"`
	RequirementRevision string `json:"requirementRevision"`
	FindingID           string `json:"findingId,omitempty"`
	Decision            string `json:"decision"`
	Severity            string `json:"severity,omitempty"`
	SeverityRationale   string `json:"severityRationale,omitempty"`
	Invalidity          string `json:"invalidity,omitempty"`
	Supersedes          string `json:"supersedes,omitempty"`
	OldValue            string `json:"oldValue"`
	NewValue            string `json:"newValue"`
	Reason              string `json:"reason"`
	Source              string `json:"source"`
	UserAuthorized      bool   `json:"userAuthorized"`
	EffectiveRevision   int    `json:"effectiveRevision"`
}

// EffectiveStatus 是从账本确定性投影出的工作流结论，同时携带受 PASS 变
// 阻塞影响的下游证据面。Revision 是投影的单调代数，供纠正的 CAS 使用。
type EffectiveStatus struct {
	Status              string   `json:"status"`
	RawStatus           string   `json:"rawStatus,omitempty"`
	ResultDigest        string   `json:"resultDigest,omitempty"`
	RequirementRevision string   `json:"requirementRevision,omitempty"`
	Source              string   `json:"source"`
	Revision            int      `json:"revision"`
	BlockingFindingIDs  []string `json:"blockingFindingIds"`
	AdvisoryFindingIDs  []string `json:"advisoryFindingIds"`
	DownstreamImpact    []string `json:"downstreamImpact"`
}

// CorrectionInput 是纠正的精确绑定/CAS 契约：必须命中 (resultDigest,
// findingID) 的当前最新决定，且 ExpectedEffectiveRevision 必须等于当前投影
// 代数，否则整笔拒绝、零状态变化。
type CorrectionInput struct {
	Action                    Action
	DispatchID                string
	ResultDigest              string
	RequirementRevision       string
	FindingID                 string
	Decision                  string
	Severity                  string
	SeverityRationale         string
	Invalidity                string
	Supersedes                string
	Reason                    string
	Source                    string
	UserAuthorized            bool
	ExpectedEffectiveRevision int
}

// Dispatch 是记录候选时所需的派发绑定事实：派发身份、它审查的需求
// revision，以及该派发被签发时携带的复审授权（自动容量内为空）。
type Dispatch struct {
	ID                  string
	RequirementRevision string
	AuthorizationID     string
}

// Result 是一条待接纳的完整审查结果：raw 状态、消息与内容级 findings（身份
// 由记录派生）。只有 PASS/FAIL 是可接纳的语义状态。
type Result struct {
	Status   string
	Message  string
	Findings []Finding
}
