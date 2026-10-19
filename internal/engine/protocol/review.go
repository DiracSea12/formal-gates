// review.go 是开发前审查纯内核（internal/engine/review）在提交协议层的
// 嫁接点：原始审查结果保存、独立 Operator 核实接纳、审查类业务控制的
// DECIDE 账目变换、run phase 的门控推进与加载/提交边界的账本完整性
// 校验。所有持久化都经 Engine 的事务紧随其后原子完成，本文件只做
// 校验与 state 变换、不做 I/O。
package protocol

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

// 审查状态的持久化词汇（与 review 内核的封闭字符串同值）。
const (
	reviewStatusPass = "PASS"

	topologySplit   = "split"
	topologyNoSplit = "no-split"

	grantOption   = "grant"
	declineOption = "decline"
)

// validReviewAction 报告动作是否属于开发前审查的封闭两值集合。
func validReviewAction(action review.Action) bool {
	return action == review.ProductReview || action == review.StartReadiness
}

// ---- 事件 schema（无状态形状校验，由 Event.Validate 调用） ----

// validateReviewDecisionPayload 强制决定载荷的小 tagged union：恰一个分
// 支非空，且该分支自身形状合法。控制↔分支配对与账目变换在接纳层对
// pending Ask 执行。
func validateReviewDecisionPayload(payload *ReviewDecisionPayload) error {
	set := 0
	for _, branch := range []bool{payload.RoundGrant != nil, payload.Correction != nil, payload.Topology != nil, payload.Route != nil} {
		if branch {
			set++
		}
	}
	if set != 1 {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("review decision payload must carry exactly one branch, got %d", set)}
	}
	switch {
	case payload.RoundGrant != nil:
		grant := payload.RoundGrant
		if !validReviewAction(grant.Action) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("round grant action %q is not a pre-development review action", grant.Action)}
		}
		if strings.TrimSpace(grant.Reason) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "round grant requires a non-empty reason"}
		}
	case payload.Topology != nil:
		if err := validateTopologyDecision(payload.Topology); err != nil {
			return err
		}
	case payload.Route != nil:
		if err := validateRouteDecision(payload.Route); err != nil {
			return err
		}
	}
	return nil
}

func (s *State) topologyBinding(decision *TopologyDecision) (*TopologyBinding, error) {
	if err := validateTopologyDecision(decision); err != nil {
		return nil, err
	}
	binding := &TopologyBinding{Decision: decision.Decision, Note: strings.TrimSpace(decision.Note), SplitCount: decision.SplitCount, Slices: slices.Clone(decision.Slices), Parallel: strings.TrimSpace(decision.Parallel)}
	if decision.Decision == topologySplit {
		owners, err := s.splitACOwners(decision)
		if err != nil {
			return nil, err
		}
		binding.ACOwners = owners
	}
	return binding, nil
}

// validateTopologyDecision 校验拓扑决定的形状：no-split 必带理由 note 且
// 不携带拆分参数；split 必有 SplitCount≥2、等长的 Slices 与并行度，分片名
// 须是标识形状（小写字母数字连字符，同 run id 规则——owner 引用与 child
// 认领键都以它进行）。Slices 逐项唯一与 AC 责任映射对确认需求的校验在绑
// 定点（slice.go splitACOwners）。
func validateTopologyDecision(topology *TopologyDecision) error {
	switch topology.Decision {
	case topologyNoSplit:
		if strings.TrimSpace(topology.Note) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a no-split topology requires the mandatory reason note"}
		}
		if topology.SplitCount != 0 || len(topology.Slices) != 0 || strings.TrimSpace(topology.Parallel) != "" || len(topology.ACOwners) != 0 || topology.DevelopmentPlan != nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a no-split topology cannot carry split parameters"}
		}
	case topologySplit:
		if topology.SplitCount < 2 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a split topology requires at least two slices"}
		}
		if len(topology.Slices) != topology.SplitCount {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("a split topology's slice list (%d) must match the declared count (%d)", len(topology.Slices), topology.SplitCount)}
		}
		for _, slice := range topology.Slices {
			if !validSliceID(slice) {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("split slice name %q must be a lowercase run-id shaped identifier (letters, digits, hyphens)", slice)}
			}
		}
		for _, entry := range topology.ACOwners {
			if strings.TrimSpace(entry.AC) == "" || strings.TrimSpace(entry.Owner) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "split AC responsibility entries need both the acceptance condition id and the owner"}
			}
		}
		if strings.TrimSpace(topology.Parallel) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a split topology requires an explicit parallelism"}
		}
	default:
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("topology decision %q invalid (closed set: split, no-split)", topology.Decision)}
	}
	return nil
}

// reservedRouteGates 是路线候选的保留字全集：合并验证（merge）与其保留总任务
// 实例（merge-gate/merge-qa）只在拆分运行中自动附加，不进正常选择列表。
var reservedRouteGates = []string{"merge", "merge-gate", "merge-qa"}

// validateRouteGateCandidates 校验候选门全集的形状：非空、逐项非空、无重
// 复、不含保留合并验证。SELECT_ROUTE 请求声明的全集与路线决定的候选表共用本
// 定义点。
func validateRouteGateCandidates(gates []string) error {
	if len(gates) == 0 {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "SELECT_ROUTE requires the explicit candidate gate list"}
	}
	seen := make(map[string]bool, len(gates))
	for _, gate := range gates {
		if strings.TrimSpace(gate) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "route candidate gates cannot be empty"}
		}
		if seen[gate] {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("route candidate gate %q listed twice", gate)}
		}
		seen[gate] = true
	}
	for _, reserved := range reservedRouteGates {
		if seen[reserved] {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("%s verification is attached automatically for split runs and is not a route candidate", reserved)}
		}
	}
	return nil
}

// sameGateSet 报告两个候选门列表是否为同一集合（顺序无关）。两侧均已经
// 过 validateRouteGateCandidates（无重复），长度相等加逐项包含即集合相等。
func sameGateSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, gate := range a {
		if !slices.Contains(b, gate) {
			return false
		}
	}
	return true
}

// validateRouteDecision checks an explicit subset of the candidate gates.
func validateRouteDecision(route *RouteDecision) error {
	seen := make(map[string]bool, len(route.Selected))
	for _, gate := range route.Selected {
		if seen[gate] {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("route selects gate %q twice", gate)}
		}
		seen[gate] = true
		if !slices.Contains(route.Gates, gate) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("route selects gate %q outside the explicit candidate list", gate)}
		}
	}

	// 计划确认的两种形态不得混携：内联片内提案（child，摘要＝提案内容摘
	// 要、结构复用 ValidateGranularity）与 readiness 引用（普通 run）。
	if plan := route.DevelopmentPlan; plan != nil && plan.Proposal != nil {
		if plan.ReviewActionID != "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "an inline development plan proposal cannot also reference its readiness review action"}
		}
		if err := ValidateGranularity(plan.Proposal); err != nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: err.Error()}
		}
		if plan.Digest != planContractDigest(plan.Proposal) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "the inline development plan digest must match its proposal"}
		}
	}
	return validateRouteGateCandidates(route.Gates)
}

// ---- 审查证据接纳（WORKER_RESULT 同事务） ----

// confirmedRevisionGeneration finds the latest work authorization bound to
// this confirmed content.
// Content may recur; execution freshness is checked separately against the
// dispatch's issuance generation, so reconfirmation cannot refresh old work.
func (s *State) confirmedRevisionGeneration(revision string) (uint64, bool) {
	entries := s.workAuthorizations()
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if (s.SliceChild == nil || i > 0) && entry.Revision == revision {
			return entry.StateRevision, true
		}
	}
	return 0, (s.SliceChild != nil || len(s.RequirementRevisions) == 0) && revision != "" && revision == s.initialRequirementRevision()
}

func (s *State) confirmedRequirementRevision(revision string) bool {
	_, confirmed := s.confirmedRevisionGeneration(revision)
	return confirmed
}

// effectivePass accepts a PASS at confirmed content only after any fresh-round
// obligation is discharged. Requirement confirmations either request redo or
// carry the existing conclusion; the content hash need not be unique in history.
// Slice children satisfy the same gate through their inherited binding
// (inheritedReviewPass)——child 不自跑审查轮。
func (s *State) effectivePass(series review.Series) bool {
	if s.SliceChild != nil {
		return s.inheritedReviewPass(series)
	}
	// FreshOwed 在则该系列不算 PASS：重走义务未清偿时，即便投影被纠正
	// 刷成携已确认 revision 的 PASS，门也不开。
	return series.Effective.Status == reviewStatusPass && !series.FreshOwed && !s.reviewDispositionPending(series) && !s.reviewVerificationPending(series.Action) && s.confirmedRequirementRevision(series.Effective.RequirementRevision)
}

// dualReviewPass reports actual effective PASS for both review series, including
// confirmed revision and freshness checks. Workflow choices are checked separately.
func (s *State) dualReviewPass() bool {
	return s.effectivePass(s.ReviewSeries.ProductReview) && s.effectivePass(s.ReviewSeries.StartReadiness)
}

// reviewAdmission checks the signed dispatch, authorization, confirmed revision
// and redo floor before any worker content can consume an attempt. A carried
// dispatch keeps its original confirmed revision; a redo requires fresh input.
// The product-review sequence gate is temporary and can suspend result pairing.
func (s *State) reviewAdmission(evidence *ReviewResultEvidence, actionID string, issuedRevision uint64) error {
	// 派发身份唯一事实：证据的 DispatchID 必须就是提交结果的 actionID
	//（授权占用按真实派发判）。
	if evidence.DispatchID != actionID {
		return &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("review evidence dispatch id %q does not match the dispatching action %q",
				evidence.DispatchID, actionID),
		}
	}
	series, _ := s.ReviewSeries.Series(evidence.Action)
	execution, issued := series.DispatchRounds[actionID]
	limit, known := review.LimitAt(series.LimitHistory, execution.Revision)
	if evidence.AuthorizationID != review.AuthorizationOfDispatch(series, actionID) && (evidence.AuthorizationID != "" || !known || series.Completed >= limit) {
		return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "review authorization differs from its signed dispatch"}
	}
	if !issued {
		return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "review result has no signed round admission"}
	}
	if seriesInRedoStale(series) {
		if evidence.RequirementRevision != s.RequirementRevision() {
			return &RejectedError{
				Code: CodeReviewEvidenceRejected,
				Detail: fmt.Sprintf("%s is being redone; its fresh round must bind the current authoritative revision %q (evidence binds %q)",
					evidence.Action, s.RequirementRevision(), evidence.RequirementRevision),
			}
		}
		if issuedRevision < series.FreshFloor {
			return &RejectedError{
				Code: CodeReviewEvidenceRejected,
				Detail: fmt.Sprintf("%s fresh round requires a dispatch issued at or after the redo decision (state revision >= %d; this dispatch was issued at %d)",
					evidence.Action, series.FreshFloor, issuedRevision),
			}
		}
	} else if generation, confirmed := s.confirmedRevisionGeneration(evidence.RequirementRevision); !confirmed {
		return &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("review evidence requirement revision %q is not a confirmed revision (current authoritative %q)",
				evidence.RequirementRevision, s.RequirementRevision()),
		}
	} else if generation < series.FreshFloor || issuedRevision < series.FreshFloor {
		// 重走系列的下界为最后一次重走确认代次；内容确认和派发签发
		// 都不得早于它。内容 hash 的重复确认不改变历史派发的签发代次。
		// floor 为零（从未重走）的系列两个比较恒假，carry 语义原样。
		return &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("%s was redone by the requirement decision at state revision %d; carried evidence must bind a revision confirmed at or after it and come from a dispatch issued at or after it (revision confirmed at %d, dispatch issued at %d)",
				evidence.Action, series.FreshFloor, generation, issuedRevision),
		}
	}
	// §7.1.2 门槛：series 上存在当前 revision 下确认的 P0/P1 阻塞 finding
	// 时，同一 revision 不得再记新候选——对相同输入再采样一轮不能消除已
	// 确认的需求缺陷，必须先换到经确认变更的新 revision（或经纠正路径
	// dismiss/降级清除阻塞，该路径不受本门槛约束）。series 的 effective
	// 尚绑在旧 revision 时门槛自然关闭，新 revision 的新鲜轮自动放行。
	if series.Effective.RequirementRevision == evidence.RequirementRevision && review.HasConfirmedBlocking(series) {
		return &RejectedError{
			Code:   CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("confirmed blocking finding on %s requires a changed and reconfirmed requirement revision before a fresh round", evidence.Action),
		}
	}
	if evidence.Action == review.StartReadiness && !s.preDevelopmentReviewSatisfied(review.ProductReview) {
		return &RejectedError{
			Code: CodeReviewSequenceGate,
			Detail: fmt.Sprintf("start-readiness evidence requires a carried product-review PASS at a confirmed requirement revision (current: %s bound to %q)",
				s.ReviewSeries.ProductReview.Effective.Status, s.ReviewSeries.ProductReview.Effective.RequirementRevision),
		}
	}
	return nil
}

func (s *State) admitReviewEvidence(evidence *ReviewResultEvidence, verification review.OperatorVerification, actionID string, issuedRevision uint64) (review.Set, error) {
	if err := s.reviewAdmission(evidence, actionID, issuedRevision); err != nil {
		return review.Set{}, err
	}
	series, _ := s.ReviewSeries.Series(evidence.Action)
	updated, _, err := review.RecordCandidate(s.ReviewSeries, s.RunID, evidence.Action,
		review.Dispatch{
			ID:                  evidence.DispatchID,
			RequirementRevision: evidence.RequirementRevision,
			AuthorizationID:     evidence.AuthorizationID,
		},
		review.Result{Status: evidence.Status, Message: evidence.Message, Findings: evidence.Findings},
		verification)
	if err != nil {
		return review.Set{}, &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
	}
	// 清点单一：重走义务只在满足 floor 的新鲜轮完成接纳时清偿（本分支
	// 已验绑当前权威 revision 且签发代数 ≥floor）。
	if seriesInRedoStale(series) {
		updated = review.DischargeFreshOwed(updated, evidence.Action)
	}
	return updated, nil
}

// isSequenceGate 报告错误是否为双审顺序门未开（区别于结构性不可纳）：
// 门未开是暂时态——回执配对挂起、开门迁移点补配对；其余拒绝照旧。
func isSequenceGate(err error) bool {
	var rejected *RejectedError
	return errors.As(err, &rejected) && rejected.Code == CodeReviewSequenceGate
}

// prepareReviewResult validates the complete worker content without mutating its
// review series. Dispatch admission remains separate from invalid result handling.
func (e *Engine) prepareReviewResult(state *State, evidence *ReviewResultEvidence, actionID string) error {
	if err := state.reviewAdmission(evidence, actionID, state.issuedRevisionOf(actionID)); err != nil {
		return err
	}
	if err := review.ValidateResult(evidence.Action, review.Result{Status: evidence.Status, Message: evidence.Message, Findings: evidence.Findings}); err != nil {
		return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
	}
	if pipelineEnabled(state, e.cfg.Definition) && evidence.Action == review.StartReadiness && evidence.Status == "PASS" {
		if !state.FlowNodeEnabled("development") {
			if evidence.GranularityReview != nil {
				return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "a disabled development node does not prepare a development plan"}
			}
			return nil
		}
		if err := ValidateGranularity(evidence.GranularityReview); err != nil {
			return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
		}
		if err := state.validatePlanRequirementCoverage(evidence.GranularityReview, true); err != nil {
			return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
		}
		if err := ValidateProjectChecks(evidence.GranularityReview); err != nil {
			return &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
		}
	}
	return nil
}

// ---- 审查类决定的账目变换（DECIDE 接纳路径） ----

// admitReviewDecision 在选项合法后执行审查类业务控制的账目变换：校验
// 控制↔分支配对与选项一致性，再做对应变换。任一拒绝都发生在 state 变
// 更之前（零状态变化）。授权者身份固定：DECIDE 本身就是用户决定，
// UserAuthorized 恒真，Source 形如 engine-decide:<requestID>。
func (e *Engine) admitReviewDecision(state *State, ask PendingAsk, requestID string, decide *DecidePayload, parent *Snapshot, nextRevision uint64) error {
	payload := decide.Review
	if !reviewControl(ask.Control) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s does not take a review decision payload", ask.Control)}
	}
	branch := func() string {
		switch {
		case payload == nil:
			return ""
		case payload.RoundGrant != nil:
			return "roundGrant"
		case payload.Correction != nil:
			return "correction"
		case payload.Topology != nil:
			return "topology"
		case payload.Route != nil:
			return "route"
		}
		return ""
	}()
	// REVIEW_ROUND_GRANT 的 Ask 选项固定为 grant/decline：decline 不携
	// payload（拒绝授权也是有效决定），grant 必携 roundGrant 分支，其余
	// 审查控制的决定必携对应分支。
	if ask.Control == ControlReviewRoundGrant {
		if offer := ask.RoundGrant; offer != nil {
			if *offer != state.reviewGrantBinding(offer.Action) || !state.reviewRoundOwed(offer.Action) {
				return &RejectedError{Code: CodeReviewDecisionRejected, Detail: "the review authorization offer is no longer current"}
			}
			if payload != nil && payload.RoundGrant != nil && payload.RoundGrant.Action != offer.Action {
				return &RejectedError{Code: CodeReviewDecisionRejected, Detail: "the review grant action does not match its offer"}
			}
		}
		if decide.Choice == AskOptionID(declineOption) {
			if payload != nil {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "declining a round grant cannot carry a review decision payload"}
			}
			return nil
		}
		if decide.Choice != AskOptionID(grantOption) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("REVIEW_ROUND_GRANT choice %q invalid (closed set: grant, decline)", decide.Choice)}
		}
		if payload == nil || payload.RoundGrant == nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "granting a round grant requires the roundGrant decision branch"}
		}
	} else if branch == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s requires its review decision payload branch", ask.Control)}
	}
	reviewReject := func(err error) error {
		return &RejectedError{Code: CodeReviewDecisionRejected, Detail: err.Error()}
	}
	switch ask.Control {
	case ControlReviewRoundGrant:
		grant := payload.RoundGrant
		// §7.3:5 授权政策由引擎强制：自动容量未耗尽时拒绝额外 slot——
		// "达到上限后……增加一个 slot"不是调用方自律。
		target, seriesKnown := state.ReviewSeries.Series(grant.Action)
		if !seriesKnown {
			return reviewReject(fmt.Errorf("review: unknown pre-development review action %q", grant.Action))
		}
		if target.Completed < target.AutomaticLimit {
			return reviewReject(fmt.Errorf("review: %s automatic round capacity is not exhausted yet (completed %d of %d); a grant is premature",
				grant.Action, target.Completed, target.AutomaticLimit))
		}
		updated, err := review.AppendAuthorization(state.ReviewSeries, state.RunID, grant.Action, grant.Reason)
		if err != nil {
			return reviewReject(err)
		}
		state.ReviewSeries = updated
		return nil
	case ControlReviewFindingDecision:
		if branch != "correction" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "REVIEW_FINDING_DECISION takes the correction branch"}
		}
		correction := *payload.Correction
		if strings.HasPrefix(requestID, "review-finding:") {
			bound, current := state.ReviewFindingBinding(requestID)
			if !current || bound.Action != correction.Action || bound.ResultDigest != correction.ResultDigest || bound.FindingID != correction.FindingID || bound.DispatchID != correction.DispatchID {
				return reviewReject(fmt.Errorf("finding decision differs from its displayed automatic Ask"))
			}
		}
		decision, findingID, ok := parseDecisionChoice(decide.Choice)
		if !ok || strings.ToUpper(strings.TrimSpace(correction.Decision)) != decision || strings.TrimSpace(correction.FindingID) != findingID {
			return &RejectedError{
				Code:   CodeEventSchemaInvalid,
				Detail: fmt.Sprintf("correction payload (decision %q, finding %q) does not match the chosen option %q", correction.Decision, correction.FindingID, decide.Choice),
			}
		}
		// 纠正的 stale 门按 series 当前结论绑定的需求 revision 判定（不按
		// 当前权威）：需求修订后续用的结论绑旧 revision，其 finding 的处置
		// 纠正仍须可行。绑定 revision 必须经确认账本成员判定（carry 有效）
		// ——账本外的结论不可作为纠正目标。候选==effective digest 的精确
		// 绑定与 CAS 由内核不变。
		targetSeries, seriesKnown := state.ReviewSeries.Series(correction.Action)
		if !seriesKnown {
			return reviewReject(fmt.Errorf("review: unknown pre-development review action %q", correction.Action))
		}
		boundRevision := targetSeries.Effective.RequirementRevision
		if !state.confirmedRequirementRevision(boundRevision) {
			return reviewReject(fmt.Errorf("correction target on %s is bound to requirement revision %q outside the confirmed ledger", correction.Action, boundRevision))
		}
		correction.UserAuthorized = true
		correction.Source = "engine-decide:" + requestID
		updated, err := review.ApplyCorrection(state.ReviewSeries, state.RunID, boundRevision, correction)
		if err != nil {
			return reviewReject(err)
		}
		state.ReviewSeries = updated
		return nil
	case ControlConfirmTopology:
		if branch != "topology" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "CONFIRM_TOPOLOGY takes the topology branch"}
		}
		// 选项锚定（与 REVIEW_FINDING_DECISION 的 parseDecisionChoice 同
		// 理）：choice id 恰为 no-split/split 且与拓扑决定一致，否则所选项
		// 与落盘载荷说的不是同一个决定。
		if choice := string(decide.Choice); (choice != topologyNoSplit && choice != topologySplit) || choice != payload.Topology.Decision {
			return &RejectedError{
				Code:   CodeEventSchemaInvalid,
				Detail: fmt.Sprintf("topology payload decision %q does not match the chosen option %q (closed set: no-split, split)", payload.Topology.Decision, decide.Choice),
			}
		}
		if state.SliceChild != nil {
			return reviewReject(fmt.Errorf("a slice child inherits its master's topology binding; it is not re-cut (change goes through requirement change, reset, or abort)"))
		}
		if state.Topology != nil {
			return reviewReject(fmt.Errorf("topology is already bound and is the binding point; it is not re-cut (change goes through requirement change, reset, or abort)"))
		}
		if !state.PreDevelopmentReviewsSatisfied() || !state.readinessPreparationReady() {
			return reviewReject(fmt.Errorf("topology confirmation requires selected pre-development reviews and current development preparation"))
		}
		decision := payload.Topology
		binding, err := state.topologyBinding(decision)
		if err != nil {
			return reviewReject(err)
		}
		if decision.Decision == topologySplit {
			// 合并路线锁定后不再有常规路线选择：仍有未决 SELECT_ROUTE Ask
			// 时不绑定——先了结该 Ask，绑定后的 run 不携带不可能再决定的
			// 路线 Ask。
			for id, pending := range state.PendingAsks {
				if pending.Control == ControlSelectRoute && !pending.Resolved {
					return reviewReject(fmt.Errorf("route selection ask %s is still pending; settle it before binding the split topology (the binding locks the merge route)", id))
				}
			}
			if pipelineEnabled(state, e.cfg.Definition) {
				preview, err := state.splitPlanPreview(e.cfg.Definition, *decision)
				if err != nil {
					return err
				}
				if decision.DevelopmentPlan == nil || *decision.DevelopmentPlan != preview.Confirmation {
					return validationError("split topology must confirm the exact readiness development plan")
				}
			}
			// split 绑定同一事务锁定合并路线：保留总任务实例只跑强制的合并
			// 门＋合并 QA，不抬 SELECT_ROUTE；合并验证在合并收尾执行。
			state.RouteConfirmed = true
			state.SelectedGates = mergeRouteGates()
		}
		state.Topology = binding
		return nil
	case ControlSelectRoute:
		if branch != "route" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "SELECT_ROUTE takes the route branch"}
		}
		if decide.Choice != "approve" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "SELECT_ROUTE requires approval of the explicit gate selection"}
		}
		if !state.RouteConfirmationDue(e.cfg.Definition) {
			return reviewReject(fmt.Errorf("route is locked to merge verification by the split topology binding; it is not selected (change goes through requirement change, reset, or abort)"))
		}
		// 候选表必须与 Ask 钉住的全集集合相等（顺序无关）：少列或多列任一
		// 门，决定声称的候选表就不再是用户所见并批准的全集——可区分拒
		// 绝、零写，Ask 保持未决，修正后的决定仍可提交。
		if !sameGateSet(payload.Route.Gates, ask.RouteGates) {
			return reviewReject(fmt.Errorf("route candidate gates %v do not match the gate list pinned on ask %s (%v); the decide must repeat exactly the list the user approved", payload.Route.Gates, requestID, ask.RouteGates))
		}
		// A child selects only ordinary gates; merge QA belongs to the master.
		if state.SliceChild != nil {
			for _, gate := range payload.Route.Gates {
				if gate == "blackbox" || gate == "whitebox" {
					return reviewReject(fmt.Errorf("slice route candidates cannot carry the built-in QA mode %q; ordinary QA is not selected or executed per slice", gate))
				}
			}
		}
		route := payload.Route
		if state.Topology == nil {
			return reviewReject(fmt.Errorf("route selection requires a bound topology"))
		}
		if !state.PreDevelopmentReviewsSatisfied() || !state.readinessPreparationReady() {
			return reviewReject(fmt.Errorf("route selection requires selected pre-development reviews and current development preparation"))
		}
		if _, err := e.reconcileConfiguration(state, nextRevision); err != nil {
			return err
		}
		configuration := state.AppliedConfiguration()
		selected := (ConfigurationSelection{Configuration: configuration}).SelectedGates(ask.RouteGates)
		route.Selected = slices.Clone(selected)
		if err := e.validatePlanConfirmation(state, route); err != nil {
			return err
		}
		before := state.validationRouteDigest()
		state.selectConfiguration(configuration, ask.RouteGates)
		state.RouteConfirmed = true
		if before != state.validationRouteDigest() {
			e.rebindValidationRoute(state)
		}
		return nil
	}
	return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s is not a review control", ask.Control)}
}

// parseDecisionChoice 把 REVIEW_FINDING_DECISION 的选项 id 解析为
// (决定, findingID)：形态 "<decision>" 或 "<decision>:<findingID>"（option
// id 由请求创建方逐 finding 枚举给出）。INVALIDATE_RESULT 是整轮决定，
// 不带 finding 后缀。
func parseDecisionChoice(choice AskOptionID) (decision, findingID string, ok bool) {
	head, tail, _ := strings.Cut(strings.TrimSpace(string(choice)), ":")
	head = strings.ToUpper(head)
	switch head {
	case "CONFIRM", "DISMISS", "RESEVERITIZE", "INVALIDATE_FINDING", "INVALIDATE_RESULT":
	default:
		return "", "", false
	}
	return head, strings.TrimSpace(tail), true
}

// ---- run phase 门控与账本一致性 ----

// advanceReviewPhases follows the static phase edges using selected reviews,
// current preparation and confirmed topology/route facts. Disabled reviews do
// not acquire a review result; missing preparation leaves START_READINESS open.
func (s *State) advanceReviewPhases() error {
	for {
		var target runtime.RunPhase
		switch s.Phase {
		case runtime.PhaseIntakeRegistered:
			if s.IntakeReceipt != nil {
				target = runtime.PhaseProductReview
			}
		case runtime.PhaseProductReview:
			if s.preDevelopmentReviewSatisfied(review.ProductReview) {
				target = runtime.PhaseTechnicalReview
			}
		case runtime.PhaseTechnicalReview:
			if s.preDevelopmentReviewSatisfied(review.StartReadiness) {
				target = runtime.PhaseStartReadiness
			}
		case runtime.PhaseStartReadiness:
			if s.readinessPreparationReady() {
				target = runtime.PhaseTopologyAndRoute
			}
		case runtime.PhaseTopologyAndRoute:
			// The route stays closed until selected reviews and preparation are
			// satisfied. A retained merge master waits for its children.
			if s.Topology != nil && s.RouteConfirmed && !s.mergeMaster() && s.PreDevelopmentReviewsSatisfied() && s.readinessPreparationReady() {
				target = runtime.PhaseDevelopmentParallel
			}
		}
		if target == "" {
			return nil
		}
		if err := s.TransitionPhase(target); err != nil {
			return err
		}
	}
}

// phaseLedgerFloor 重算账本能证明的最远主线 phase：每个门留下的 residue
// 都是持久事实（受理回执、节点选择、完成过的审查/准备、拓扑/路线确认决定）。
// effective 投影会随新的阻塞轮或需求修订重走合法回落，当前绑定会按用户
// 勾选的重走范围解除，但不会抹掉"该阶段曾经走完"的事实，因此 residue
// 不以当前 effective 或当前绑定为准。child 的审查阶段 residue 由继承事实
// 等价成立（不伪造审查轮账目）。
func (s *State) phaseLedgerFloor() int {
	floor := 0
	if s.IntakeReceipt != nil {
		floor = 1
	}
	inherited := s.SliceChild != nil
	if s.hasReviewRounds(review.ProductReview) || inherited || floor >= 1 && s.flowNodeWasDisabled(string(review.ProductReview)) {
		floor = 2
	}
	if floor >= 2 && s.flowNodeWasDisabled(string(review.StartReadiness)) {
		floor = 3
		if s.flowNodeWasDisabled("development") || s.stepCompleted("development.plan") || s.readinessPreparationReady() {
			floor = 4
		}
	}
	if s.hasReviewRounds(review.StartReadiness) || inherited {
		// start-readiness 的 TECHNICAL_REVIEW→START_READINESS→
		// TOPOLOGY_AND_ROUTE 两边同条件，residue 同源；且其候选接纳
		// 前置（双审顺序守卫）隐含接纳时 product-review 曾 effective PASS。
		floor = 4
	}
	if s.topologyRouteConfirmed() {
		floor = 5
	}
	return floor
}

// hasReviewRounds 报告 series 是否记录过审查候选（审查轮发生过；轮次是否
// 有效由账本完整性校验负责）。需求 revision 不参与判定：旧 revision 下的
// 审查轮同样是"该阶段曾经完成"的持久事实，修订重走不会把它抹掉。
func (s *State) hasReviewRounds(action review.Action) bool {
	series, ok := s.ReviewSeries.Series(action)
	if !ok {
		return false
	}
	return len(series.RawCandidates) > 0
}

// topologyRouteConfirmed 报告拓扑与路线是否都曾被用户确认过：决定台账
// （Decisions）不可变且不因重走解除，因此它是比当前绑定更强的单调
// residue——需求修订勾选重走拓扑/路线会清空当前绑定，但已进入过开发的
// run phase 仍须被账本证明合法。child 的拓扑 residue 来自继承绑定（与用
// 户确认同级：同样不可重切），路线仍由自身决定台账证明。
func (s *State) topologyRouteConfirmed() bool {
	var topology, route bool
	if s.SliceChild != nil {
		topology = true
	}
	for _, decision := range s.Decisions {
		switch decision.Control {
		case ControlConfirmTopology:
			topology = true
		case ControlSelectRoute:
			route = decision.Choice == "approve" && decision.Review != nil && decision.Review.Route != nil || route
		}
	}
	return topology && route
}

// phaseRank 给主线前置段排序；DEVELOPMENT_PARALLEL 之后的 phase 排在
// 本阶段账本可证明的范围之外，TERMINAL 由终止闭包豁免（任意 phase 都
// 可直接进入 cleanup 终态）。
func phaseRank(phase runtime.RunPhase) int {
	switch phase {
	case runtime.PhasePreparing, runtime.PhaseIntakeRegistered:
		return 0
	case runtime.PhaseProductReview:
		return 1
	case runtime.PhaseTechnicalReview:
		return 2
	case runtime.PhaseStartReadiness:
		return 3
	case runtime.PhaseTopologyAndRoute:
		return 4
	case runtime.PhaseDevelopmentParallel:
		return 5
	case runtime.PhaseTerminal:
		return -1
	default:
		return 6
	}
}

// verifyPhaseAgainstLedgers 校验 phase 不超过持久账本证明的 floor；
// TERMINAL、有 receipt 的 SLICE_READY 和已开始清理的 FINALIZING_CLEANUP 豁免。
// phase 可以落后于 floor，后续提交仍按门控推进。
func (s *State) verifyPhaseAgainstLedgers() error {
	if s.Phase == runtime.PhaseTerminal {
		return nil
	}
	if s.Phase == runtime.PhaseSliceReady && s.SliceReceipt != nil {
		return nil
	}
	if s.Phase == runtime.PhaseFinalizingCleanup && s.cleanupStarted(false) {
		return nil
	}
	if phaseRank(s.Phase) > s.phaseLedgerFloor() {
		return &RejectedError{
			Code:   CodeStateIntegrityInvalid,
			Detail: fmt.Sprintf("run phase %s is beyond what the durable ledgers justify (floor rank %d); hand-dialing past a gate is rejected", s.Phase, s.phaseLedgerFloor()),
		}
	}
	return nil
}

// verifyReviewIntegrity 在加载/提交边界重算审查账本的全部投影与身份链、
// 需求 revision 账本的结构链并校验 phase 一致性：篡改的账本或越门的
// phase 在此可区分拒绝。
func verifyReviewIntegrity(state *State) error {
	if err := ValidateFlowSelection(state.FlowSelection); err != nil {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: err.Error()}
	}
	if err := review.Validate(state.RunID, state.ReviewSeries); err != nil {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: fmt.Sprintf("review ledger: %v", err)}
	}
	if err := verifyRequirementLedger(state); err != nil {
		return err
	}
	// FreshFloor 只认确认账本：每条 series 的 floor 必须等于覆盖该 series
	// 的最后一次决定的 state 代数（无覆盖为零）——手改 floor 可检出。
	expectedFloor := func(action review.Action) uint64 {
		var floor uint64
		for _, entry := range state.workAuthorizations() {
			if reset, ok := state.resetDecision(); ok && reset.Reset.Applied && entry.StateRevision <= reset.Revision {
				continue
			}
			if entry.StateRevision > floor && slices.Contains(entry.DecidedScope, string(action)) {
				floor = entry.StateRevision
			}
		}
		return floor
	}
	// A redo is discharged only by a candidate from an execution authorized
	// at or after its confirmation generation. Exclusion changes capacity,
	// but does not erase the fact that this fresh execution completed.
	expectedOwed := func(action review.Action, series review.Series) bool {
		floor := expectedFloor(action)
		if floor == 0 {
			return false
		}
		for _, candidate := range series.RawCandidates {
			issued := state.issuedRevisionOf(candidate.DispatchID)
			generation, confirmed := state.confirmedRevisionGeneration(candidate.RequirementRevision)
			if confirmed && issued >= floor && generation >= floor {
				return false
			}
		}
		return true
	}
	product, readiness := state.ReviewSeries.ProductReview, state.ReviewSeries.StartReadiness
	if product.FreshFloor != expectedFloor(review.ProductReview) || readiness.FreshFloor != expectedFloor(review.StartReadiness) {
		return &RejectedError{
			Code:   CodeStateIntegrityInvalid,
			Detail: "review series fresh floors do not match the requirement confirmation ledger",
		}
	}
	if product.FreshOwed != expectedOwed(review.ProductReview, product) || readiness.FreshOwed != expectedOwed(review.StartReadiness, readiness) {
		return &RejectedError{
			Code:   CodeStateIntegrityInvalid,
			Detail: "review series fresh-round obligations do not match the requirement confirmation ledger",
		}
	}
	return state.verifyPhaseAgainstLedgers()
}

// ReviewVerificationObservation binds independent semantic judgments to the
// unchanged worker result. Finding judgments follow its original finding order.
type ReviewVerificationObservation struct {
	ResultDigest  string                      `json:"resultDigest"`
	Verification  review.OperatorVerification `json:"verification"`
	Findings      []ReviewFindingVerification `json:"findings"`
	InvalidReason string                      `json:"invalidReason,omitempty"`
}

type ReviewFindingVerification struct {
	Valid             bool   `json:"valid"`
	Reason            string `json:"reason"`
	Severity          string `json:"severity,omitempty"`
	SeverityRationale string `json:"severityRationale,omitempty"`
}

// PendingReviewVerifications derives semantic duties from completed source
// results. The admitted candidate and existing rejection own their disposition.
func (s *State) PendingReviewVerifications() []WorkerResult {
	results := []WorkerResult{}
	if s.Phase == runtime.PhaseTerminal || s.requirementChangePending() || s.abortRequested() {
		return results
	}
	for id, result := range s.Results {
		if result.Review == nil || result.Outcome != OutcomePass {
			continue
		}
		if _, retired := s.ObsoleteActions[id]; retired {
			continue
		}
		if receipt, receipted := s.SpawnReceipts[id]; !receipted || !inflightWitnessReceipt(receipt.Status) {
			continue
		}
		series, ok := s.ReviewSeries.Series(result.Review.Action)
		if !ok || slices.ContainsFunc(series.RawCandidates, func(candidate review.RawCandidate) bool { return candidate.DispatchID == id }) {
			continue
		}
		generation, confirmed := s.confirmedRevisionGeneration(result.Review.RequirementRevision)
		if !confirmed || generation < series.FreshFloor || s.issuedRevisionOf(id) < series.FreshFloor {
			continue
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b WorkerResult) int { return strings.Compare(a.ActionID, b.ActionID) })
	return results
}

func (s *State) reviewVerificationPending(action review.Action) bool {
	return slices.ContainsFunc(s.PendingReviewVerifications(), func(result WorkerResult) bool { return result.Review.Action == action })
}

func (e *Engine) acceptReviewVerification(s *State, observation *OperatorObservationPayload, nextRevision uint64) (string, error) {
	for _, accepted := range s.OperatorObservations {
		if accepted.Subject == observation.Subject && accepted.Review != nil {
			if digestOfCanonical(accepted.Review) == digestOfCanonical(observation.Review) {
				return "DUPLICATE", nil
			}
			return "", &RejectedError{Code: CodeReceiptConflict, Detail: "independent verification already disposed this original result; use the finding correction contract"}
		}
	}
	index := slices.IndexFunc(s.PendingReviewVerifications(), func(result WorkerResult) bool { return result.ActionID == observation.Subject })
	if index < 0 {
		return "", &RejectedError{Code: CodeEventNotCurrent, Detail: "review verification has no current completed source result"}
	}
	result := s.Results[observation.Subject]
	judgment := observation.Review
	if judgment.ResultDigest != result.Digest {
		return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "review verification differs from the original result digest"}
	}
	if strings.TrimSpace(judgment.InvalidReason) != "" {
		if len(judgment.Findings) != 0 {
			return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "invalid review result cannot contain finding judgments"}
		}
		return "REJECTED_INVALID_RESULT", s.endInvalidAttempt(result.ActionID, "REJECTED_INVALID_RESULT", judgment.InvalidReason, result, nextRevision)
	}
	if strings.TrimSpace(judgment.Verification.Evidence) == "" {
		return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "independent review verification requires original evidence"}
	}
	if len(judgment.Findings) != len(result.Review.Findings) {
		return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "independently verify each original finding in order"}
	}
	evidence := *result.Review
	evidence.Findings = []review.Finding{}
	verification := judgment.Verification
	for i, verified := range judgment.Findings {
		if strings.TrimSpace(verified.Reason) == "" {
			return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "each finding requires independent verification evidence"}
		}
		verification.Evidence += fmt.Sprintf("\nfinding %d valid=%t: %s", i+1, verified.Valid, verified.Reason)
		if !verified.Valid {
			continue
		}
		finding := result.Review.Findings[i]
		if verified.Severity != "" {
			if strings.TrimSpace(verified.SeverityRationale) == "" {
				return "", &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "an independently revised severity requires its rationale"}
			}
			finding.Severity, finding.SeverityRationale = verified.Severity, verified.SeverityRationale
		}
		evidence.Findings = append(evidence.Findings, finding)
	}
	updated, err := s.admitReviewEvidence(&evidence, verification, result.ActionID, s.issuedRevisionOf(result.ActionID))
	if err != nil {
		return "", err
	}
	s.ReviewSeries = updated
	delete(s.EndedAttempts, s.SpawnReceipts[result.ActionID].Task)
	if err := s.settleFrontierSteps(e.cfg.Definition, e.cfg.InternalSteps); err != nil {
		return "", err
	}
	s.routingReleasedCapacity = true
	return "ACCEPTED", nil
}

// ReviewFindingRevised derives remedy completion from its confirmed requirement
// delta. Finding corrections replace the disposition, not that confirmation.
func (s *State) ReviewFindingRevised(finding review.Finding) bool {
	for _, entry := range s.RequirementRevisions {
		ask := s.PendingAsks[entry.RequestID]
		if ask.Package != nil && ask.Package.FindingDigest == finding.Digest {
			return true
		}
	}
	return false
}

func (s *State) reviewDispositionPending(series review.Series) bool {
	for _, candidate := range series.RawCandidates {
		if review.ResultExcluded(series, candidate.ResultDigest) || candidate.RequirementRevision != series.Effective.RequirementRevision {
			continue
		}
		for _, finding := range candidate.Findings {
			disposition, _, _ := review.EffectiveFindingDecision(series, candidate.ResultDigest, finding)
			if disposition == "" || (disposition == "CONFIRM" || disposition == "RESEVERITIZE") && !s.ReviewFindingRevised(finding) {
				return true
			}
		}
	}
	return false
}

// PendingAskCurrent projects unresolved controls whose required input is ready.
// Development confirmations use the same preparation fact as their admission.
func (s *State) PendingAskCurrent(ask PendingAsk) bool {
	if ask.Resolved {
		return false
	}
	if s.Route == "regular" && (ask.Control == ControlConfirmTopology || ask.Control == ControlSelectRoute) {
		return s.PreDevelopmentReviewsSatisfied() && s.readinessPreparationReady()
	}
	return true
}

// reconcileReviewAsks publishes each established finding through the existing
// user-disposition contract. Each request retains its candidate/finding identity;
// source results and original findings remain immutable.
func (e *Engine) reconcileReviewAsks(s *State) bool {
	changed := e.reconcileReviewGrantAsks(s)
	if s.requirementChangePending() || s.abortRequested() {
		return changed
	}
	for id, ask := range s.PendingAsks {
		if !ask.Resolved && !s.PendingAskCurrent(ask) {
			ask.Resolved = true
			s.PendingAsks[id] = ask
			changed = true
		}
	}
	offers := map[string][]AskOption{}
	for _, series := range []review.Series{s.ReviewSeries.ProductReview, s.ReviewSeries.StartReadiness} {
		for _, candidate := range series.RawCandidates {
			if review.ResultExcluded(series, candidate.ResultDigest) || candidate.RequirementRevision != series.Effective.RequirementRevision {
				continue
			}
			for _, finding := range candidate.Findings {
				disposition, _, _ := review.EffectiveFindingDecision(series, candidate.ResultDigest, finding)
				if disposition != "" {
					continue
				}
				id := reviewFindingAskID(candidate, finding)
				offers[id] = []AskOption{{ID: AskOptionID("confirm:" + finding.ID), Label: "Confirm the problem; its correction requires the complete review delta"}, {ID: AskOptionID("dismiss:" + finding.ID), Label: "Dismiss this finding"}}
			}
		}
	}
	for id, ask := range s.PendingAsks {
		if !strings.HasPrefix(id, "review-finding:") || ask.Resolved {
			continue
		}
		if _, current := offers[id]; !current {
			ask.Resolved = true
			s.PendingAsks[id] = ask
			changed = true
		}
	}
	for id, options := range offers {
		if _, exists := s.PendingAsks[id]; exists {
			continue
		}
		s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlReviewFindingDecision, Options: options}
		changed = true
	}
	return changed
}

func reviewFindingAskID(candidate review.RawCandidate, finding review.Finding) string {
	return "review-finding:" + digestOfCanonical([]string{candidate.ResultDigest, finding.ID})
}

// ReviewFindingBinding supplies the exact source of one current automatic Ask.
func (s *State) ReviewFindingBinding(requestID string) (review.CorrectionInput, bool) {
	for _, series := range []review.Series{s.ReviewSeries.ProductReview, s.ReviewSeries.StartReadiness} {
		for _, candidate := range series.RawCandidates {
			for _, finding := range candidate.Findings {
				if requestID == reviewFindingAskID(candidate, finding) {
					return review.CorrectionInput{Action: series.Action, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, FindingID: finding.ID, RequirementRevision: candidate.RequirementRevision, ExpectedEffectiveRevision: series.Effective.Revision}, true
				}
			}
		}
	}
	return review.CorrectionInput{}, false
}

func (s *State) reviewResultAwaitingVerification(actionID string) bool {
	return slices.ContainsFunc(s.PendingReviewVerifications(), func(result WorkerResult) bool { return result.ActionID == actionID })
}

func (s *State) pendingReviewCorrections() []review.Finding {
	findings := []review.Finding{}
	for _, series := range []review.Series{s.ReviewSeries.ProductReview, s.ReviewSeries.StartReadiness} {
		for _, candidate := range series.RawCandidates {
			if review.ResultExcluded(series, candidate.ResultDigest) || candidate.RequirementRevision != series.Effective.RequirementRevision {
				continue
			}
			for _, finding := range candidate.Findings {
				disposition, _, _ := review.EffectiveFindingDecision(series, candidate.ResultDigest, finding)
				if (disposition == "CONFIRM" || disposition == "RESEVERITIZE") && !s.ReviewFindingRevised(finding) {
					findings = append(findings, finding)
				}
			}
		}
	}
	return findings
}
