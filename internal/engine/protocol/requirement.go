// requirement.go 是用户需求变化（总需求 §6.3.2、§7.1、§7.2）在提交协议层
// 的嫁接点：REQUEST_REQUIREMENT_CHANGE 的两段 Ask、确认 revision 账本的
// 登记与完整性校验、修订屏障（Phase A）对业务签发的冻结。含义分类与重走
// 范围由用户在 typed 载荷中逐项声明，引擎只做机械接纳与机械执行——不自动
// 扩大失效范围，也不把 P2/P3 修订自行升级。所有写入经 Engine 的事务紧随
// 其后原子完成，本文件只做校验与 state 变换、不做 I/O。
package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
)

// 需求修订来源与含义分类的封闭词汇（持久化契约的一部分）。INTAKE 是账本
// 初始账目专用值，不出现在决定载荷中。
const (
	// RequirementSourceIntake 标记受理回执回填的初始账目。
	RequirementSourceIntake = "INTAKE"
	// RequirementSourceUserInitiated 是用户主动发起的需求变化。
	RequirementSourceUserInitiated = "USER_INITIATED"
	// RequirementSourceReviewFindingP2 是产品审/技术审确认的 P2/P3 处置
	// 驱动的修订（默认 meaning-preserved，用户保留升级权）。
	RequirementSourceReviewFindingP2 = "REVIEW_FINDING_P2"
	// RequirementSourceReviewFindingP1 是产品审/技术审确认的 P0/P1 导致的
	// 修订（必须 meaning-changed 并重走对应审查 series）。
	RequirementSourceReviewFindingP1 = "REVIEW_FINDING_P1"

	// RequirementClassificationIntake 标记初始账目。
	RequirementClassificationIntake = "INTAKE"
	// RequirementClassificationPreserved：含义不变——不清审、不重审、不耗
	// 审查轮次（机主修订 §7.1.3）。
	RequirementClassificationPreserved = "PRESERVED"
	// RequirementClassificationChanged：含义变化——机械执行用户勾选的
	// 重走项。
	RequirementClassificationChanged = "CHANGED"
)

// requirementScopeItems 是 DecidedScope 的封闭重走项集合（固定顺序，前两
// 项即 review.Action 的持久化值）。
var requirementScopeItems = []string{
	string(review.ProductReview),
	string(review.StartReadiness),
	"topology",
	"route",
}

// validRequirementSource 报告来源是否属于决定载荷可用的封闭三值。
func validRequirementSource(source string) bool {
	switch source {
	case RequirementSourceUserInitiated, RequirementSourceReviewFindingP2, RequirementSourceReviewFindingP1:
		return true
	}
	return false
}

// validRequirementClassification 报告分类是否属于决定载荷可用的封闭两值。
func validRequirementClassification(classification string) bool {
	return classification == RequirementClassificationPreserved || classification == RequirementClassificationChanged
}

// ---- 事件 schema（无状态形状校验，由 Event.Validate 调用） ----

// validateRequirementDecisionPayload 校验需求变化决定载荷的形状：CAS 基
// 线、候选 revision、delta 指纹与理由非空；来源与分类封闭；发现项驱动的
// 来源必携 finding digest，用户主动发起必空。来源↔分类的强制规则（P1 源
// 必须 CHANGED）与账目绑定在接纳层对 pending Ask 执行。
func validateRequirementDecisionPayload(payload *RequirementDecisionPayload) error {
	if strings.TrimSpace(payload.BaseRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the base revision it is decided against"}
	}
	if strings.TrimSpace(payload.CandidateRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the candidate revision binding"}
	}
	if payload.CandidateRevision == payload.BaseRevision {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision candidate revision must differ from its base"}
	}
	if strings.TrimSpace(payload.DeltaDigest) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the review-delta digest binding"}
	}
	if !validRequirementSource(payload.Source) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("requirement decision source %q invalid (closed set: USER_INITIATED, REVIEW_FINDING_P2, REVIEW_FINDING_P1)", payload.Source)}
	}
	if !validRequirementClassification(payload.Classification) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("requirement decision classification %q invalid (closed set: PRESERVED, CHANGED)", payload.Classification)}
	}
	if strings.TrimSpace(payload.Reason) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires a non-empty reason"}
	}
	findingBound := strings.TrimSpace(payload.FindingDigest) != ""
	if payload.Source == RequirementSourceUserInitiated && findingBound {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a user-initiated requirement decision cannot bind a review finding digest"}
	}
	if payload.Source != RequirementSourceUserInitiated && !findingBound {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a finding-driven requirement decision must bind its finding digest"}
	}
	return nil
}

// ---- 权威 revision 读取与修订屏障 ----

// RequirementRevision 返回当前权威需求 revision：确认账本的末条；账本为空
// （受理回执尚未落账的窗口或账本字段之前的旧 run）时回退受理确认回执。
// 一切 revision 比对（审查证据绑定、决定账目、双审门）都读它，需求变化
// 登记后自动跟随新权威。
func (s *State) RequirementRevision() string {
	if n := len(s.RequirementRevisions); n > 0 {
		return s.RequirementRevisions[n-1].Revision
	}
	if s.IntakeConfirmationReceipt == nil {
		return ""
	}
	return s.IntakeConfirmationReceipt.RequirementRevision
}

// requirementChangePending 报告修订屏障是否在位：存在未决的
// REQUEST_REQUIREMENT_CHANGE Ask。屏障只冻结新的业务任务签发（Phase A），
// 既有 Ask、回执与结果类事件不受阻。
func (s *State) requirementChangePending() bool {
	for _, ask := range s.PendingAsks {
		if ask.Control == ControlRequestRequirementChange && !ask.Resolved {
			return true
		}
	}
	return false
}

// ---- 决定的账目变换（DECIDE 接纳路径） ----

// admitRequirementDecision 在选项合法后执行需求变化控制的账目变换。nil
// 载荷是 decline：只解决 Ask（屏障随之解除），不登记任何 revision——受理
// Ask 的选项集由请求方枚举（如 proceed/abort），引擎无从锚定具体选项 id，
// typed 载荷才是登记修订的唯一权威。非 nil 载荷先过全部前置校验（任一拒
// 绝零状态变化），再在同一事务内登记 ConfirmedRevision 并按分类机械执行
// 用户勾选的重走项；未勾选的环节一律不动（§7.2.3：ImpactSet 是建议，用户
// 逐项确认后引擎机械执行）。
func (e *Engine) admitRequirementDecision(state *State, ask PendingAsk, requestID string, decide *DecidePayload, nextRevision uint64) error {
	if ask.Control != ControlRequestRequirementChange {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s does not take a requirement decision payload", ask.Control)}
	}
	payload := decide.Requirement
	if payload == nil {
		return nil
	}
	reject := func(detail string) error {
		return &RejectedError{Code: CodeRequirementDecisionRejected, Detail: detail}
	}
	if state.IntakeReceipt == nil {
		return reject("requirement change requires the recorded intake receipt (the run has not started)")
	}
	current := state.RequirementRevision()
	if payload.BaseRevision != current {
		return reject(fmt.Sprintf("requirement decision base revision %q is not the current authoritative revision %q (conflict)", payload.BaseRevision, current))
	}
	// 分类规则（§7.1 的引擎强制）：
	//   - REVIEW_FINDING_P1 必须 CHANGED 且重走范围必须包含 finding 所在
	//     审查 series（确认 P0/P1 的新鲜复审义务，用户不可选 PRESERVED）。
	//   - REVIEW_FINDING_P2 允许 PRESERVED（默认）或 CHANGED（用户升级权）。
	//   - USER_INITIATED 两种分类皆可。
	// 发现项驱动的修订必须绑回真实账目中存在的 finding。
	if payload.Source != RequirementSourceUserInitiated {
		owner, found := review.FindingOwner(state.ReviewSeries, payload.FindingDigest)
		if !found {
			return reject(fmt.Sprintf("requirement decision binds finding digest %q that no recorded review evidence contains", payload.FindingDigest))
		}
		if payload.Source == RequirementSourceReviewFindingP1 {
			if payload.Classification != RequirementClassificationChanged {
				return reject("a REVIEW_FINDING_P1 requirement decision must be classified CHANGED (a confirmed P0/P1 requires the fresh re-review)")
			}
			if (owner == review.ProductReview && !payload.RedoProductReview) || (owner == review.StartReadiness && !payload.RedoStartReadiness) {
				return reject(fmt.Sprintf("a REVIEW_FINDING_P1 requirement decision must redo the owning review series %s", owner))
			}
		}
	}
	scope := make([]string, 0, len(requirementScopeItems))
	if payload.Classification == RequirementClassificationChanged {
		for _, item := range requirementScopeItems {
			switch item {
			case string(review.ProductReview):
				if payload.RedoProductReview {
					scope = append(scope, item)
				}
			case string(review.StartReadiness):
				if payload.RedoStartReadiness {
					scope = append(scope, item)
				}
			case "topology":
				if payload.RedoTopology {
					scope = append(scope, item)
				}
			case "route":
				if payload.RedoRoute {
					scope = append(scope, item)
				}
			}
		}
	}
	// 登记确认事实（新权威 revision）。
	state.RequirementRevisions = append(state.RequirementRevisions, ConfirmedRevision{
		Revision:       payload.CandidateRevision,
		DeltaDigest:    payload.DeltaDigest,
		BaseRevision:   payload.BaseRevision,
		Source:         payload.Source,
		Classification: payload.Classification,
		DecidedScope:   scope,
		RequestID:      requestID,
		StateRevision:  nextRevision,
	})
	if payload.Classification != RequirementClassificationChanged {
		// PRESERVED：审查 series、拓扑、路线全部不动——不清审、不重审、
		// 不耗审查轮次（§7.1.3）。
		return nil
	}
	// CHANGED：只机械执行勾选项。勾选的 series 调单 series stale 原语
	//（product-review 的重走按依赖语义级联置 start-readiness 的
	// UpstreamVoid——是结论失效，不是重走范围自动扩大）；勾选拓扑/路线
	// 即解绑，重新确认走既有 CONFIRM_TOPOLOGY/SELECT_ROUTE 路径。phase
	// 与推进门不变，由 advanceReviewPhases 的既有条件自然生效。
	source := "requirement-change:" + requestID
	if payload.RedoProductReview {
		state.ReviewSeries = review.StaleSeries(state.ReviewSeries, review.ProductReview, source)
	}
	if payload.RedoStartReadiness {
		state.ReviewSeries = review.StaleSeries(state.ReviewSeries, review.StartReadiness, source)
	}
	if payload.RedoTopology {
		state.Topology = nil
	}
	if payload.RedoRoute {
		state.RouteMode = ""
		state.SelectedGates = nil
	}
	return nil
}

// ---- 账本完整性（加载/提交边界） ----

// verifyRequirementLedger 重算确认 revision 账本的结构链：首条必须是与受
// 理回执一致的 INTAKE 初始账目；其后每条 base 链精确衔接、来源/分类/重走
// 项封闭、state 代数严格单调，且 P1 源不得声明 PRESERVED（伪造分类可检）。
// 账本为空的旧 run 跳过（由解码边界的受理回执回填向前兼容）。
func verifyRequirementLedger(state *State) error {
	if len(state.RequirementRevisions) == 0 {
		return nil
	}
	fail := func(format string, args ...any) error {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: fmt.Sprintf("requirement revision ledger: %s", fmt.Sprintf(format, args...))}
	}
	intake := state.RequirementRevisions[0]
	if intake.Source != RequirementSourceIntake || intake.Classification != RequirementClassificationIntake ||
		intake.BaseRevision != "" || intake.DeltaDigest != "" || len(intake.DecidedScope) != 0 || intake.RequestID != "" {
		return fail("the first entry is not the intake-seeded initial fact")
	}
	if state.IntakeReceipt == nil ||
		intake.Revision != state.IntakeReceipt.Confirmation.RequirementRevision ||
		intake.StateRevision != state.IntakeReceipt.Revision {
		return fail("the initial entry does not match the recorded intake receipt")
	}
	previousRevision := intake.Revision
	previousState := intake.StateRevision
	for index, entry := range state.RequirementRevisions[1:] {
		if entry.BaseRevision != previousRevision {
			return fail("entry %d base revision %q breaks the confirmation chain (previous %q)", index+2, entry.BaseRevision, previousRevision)
		}
		if entry.Revision == "" || entry.Revision == entry.BaseRevision || entry.DeltaDigest == "" || entry.RequestID == "" {
			return fail("entry %d is missing its revision, delta, or ask binding", index+2)
		}
		if !validRequirementSource(entry.Source) || !validRequirementClassification(entry.Classification) {
			return fail("entry %d carries a forged source or classification", index+2)
		}
		if entry.Source == RequirementSourceReviewFindingP1 && entry.Classification != RequirementClassificationChanged {
			return fail("entry %d declares a confirmed P0/P1 source with a preserved classification", index+2)
		}
		if entry.Classification == RequirementClassificationPreserved && len(entry.DecidedScope) != 0 {
			return fail("entry %d declares a preserved classification with a redo scope", index+2)
		}
		seenScope := make(map[string]bool, len(entry.DecidedScope))
		for _, item := range entry.DecidedScope {
			if !slices.Contains(requirementScopeItems, item) || seenScope[item] {
				return fail("entry %d scope item %q is outside the closed redo set or repeated", index+2, item)
			}
			seenScope[item] = true
		}
		if entry.StateRevision <= previousState {
			return fail("entry %d state revision is not strictly increasing", index+2)
		}
		previousRevision = entry.Revision
		previousState = entry.StateRevision
	}
	return nil
}
