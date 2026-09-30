// review.go 是开发前审查纯内核（internal/engine/review）在提交协议层的
// 嫁接点：审查证据随 WORKER_RESULT 接纳入账、四个审查类业务控制的
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

	routeModeFull   = "full"
	routeModeCustom = "custom"

	grantOption   = "grant"
	declineOption = "decline"
)

// validReviewAction 报告动作是否属于开发前审查的封闭两值集合。
func validReviewAction(action review.Action) bool {
	return action == review.ProductReview || action == review.StartReadiness
}

// ---- 事件 schema（无状态形状校验，由 Event.Validate 调用） ----

// validateReviewEvidence 校验审查证据变体的形状：动作封闭、派发与需求
// revision 绑定非空、语义状态恰为 PASS/FAIL。finding 内容与 Operator 核
// 实的完整性由 review.RecordCandidate 在接纳时拒绝。
func validateReviewEvidence(evidence *ReviewResultEvidence) error {
	if !validReviewAction(evidence.Action) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("review evidence action %q is not a pre-development review action", evidence.Action)}
	}
	if strings.TrimSpace(evidence.DispatchID) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "review evidence dispatch id is empty"}
	}
	if strings.TrimSpace(evidence.RequirementRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "review evidence requirement revision is empty"}
	}
	if evidence.Status != OutcomePass && evidence.Status != OutcomeFail {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("review evidence status %q cannot become a review candidate (only PASS or FAIL)", evidence.Status)}
	}
	return nil
}

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

// validateTopologyDecision 校验拓扑决定的形状：no-split 必带理由 note 且
// 不携带拆分参数；split 必有 SplitCount≥2、等长非空 Slices 与并行度。
func validateTopologyDecision(topology *TopologyDecision) error {
	switch topology.Decision {
	case topologyNoSplit:
		if strings.TrimSpace(topology.Note) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a no-split topology requires the mandatory reason note"}
		}
		if topology.SplitCount != 0 || len(topology.Slices) != 0 || strings.TrimSpace(topology.Parallel) != "" {
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
			if strings.TrimSpace(slice) == "" {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a split topology cannot carry an empty slice name"}
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
// 实例（merge-gate/merge-qa）只在拆分运行中自动附加，不进正常选择列表。命名权威
// internal/validate/catalog.go:28-31；references/formal-flow.md「合并门/合并 QA
// 不进正常选择列表」。
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

// validateRouteDecision 校验路线决定的形状：候选列表不得含合并验证（合并
// 验证只在拆分运行中自动附加）；full 携带显式完整门全集、必含内建黑盒与
// 白盒 QA 且 Selected 为空；custom 携带非空、无重复、逐项属于候选列表的
// 选择，可选全集（sliced-runs-confirmation-and-qa-refactor 需求9：至少选
// 一项，可省略任一）。
func validateRouteDecision(route *RouteDecision) error {
	switch route.Mode {
	case routeModeFull:
		if len(route.Selected) != 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "full route selects the complete gate list without a subset"}
		}
		if !slices.Contains(route.Gates, "blackbox") || !slices.Contains(route.Gates, "whitebox") {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "full route must carry both built-in QA modes (blackbox and whitebox) alongside every discovered gate"}
		}
	case routeModeCustom:
		if len(route.Selected) == 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "custom route must select at least one candidate"}
		}
		seen := make(map[string]bool, len(route.Selected))
		for _, gate := range route.Selected {
			if seen[gate] {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("custom route selects gate %q twice", gate)}
			}
			seen[gate] = true
		}
		for _, gate := range route.Selected {
			if !slices.Contains(route.Gates, gate) {
				return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("custom route selects gate %q outside the explicit candidate list", gate)}
			}
		}
	default:
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("route mode %q invalid for SELECT_ROUTE (closed set: full, custom)", route.Mode)}
	}
	return validateRouteGateCandidates(route.Gates)
}

// ---- 审查证据接纳（WORKER_RESULT 同事务） ----

// confirmedRevisionGeneration finds the latest confirmation of this content.
// Content may recur; execution freshness is checked separately against the
// dispatch's issuance generation, so reconfirmation cannot refresh old work.
func (s *State) confirmedRevisionGeneration(revision string) (uint64, bool) {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		entry := s.RequirementRevisions[i]
		if entry.Revision == revision {
			return entry.StateRevision, true
		}
	}
	return 0, len(s.RequirementRevisions) == 0 && revision != "" && revision == s.RequirementRevision()
}

func (s *State) confirmedRequirementRevision(revision string) bool {
	_, confirmed := s.confirmedRevisionGeneration(revision)
	return confirmed
}

// effectivePass accepts a PASS at confirmed content only after any fresh-round
// obligation is discharged. Requirement confirmations either request redo or
// carry the existing conclusion; the content hash need not be unique in history.
func (s *State) effectivePass(series review.Series) bool {
	// FreshOwed 在则该系列不算 PASS：重走义务未清偿时，即便投影被纠正
	// 刷成携已确认 revision 的 PASS，门也不开。
	return series.Effective.Status == reviewStatusPass && !series.FreshOwed && s.confirmedRequirementRevision(series.Effective.RequirementRevision)
}

// dualReviewPass 报告双 series 的 effective 结论是否都是携带已确认
// revision 的 PASS——进入开发的门（SELECT_ROUTE 接纳与 TOPOLOGY_AND_ROUTE
// 推进）以它为准：任一审查重新阻塞或被重走置 PENDING 时门保持关闭。
func (s *State) dualReviewPass() bool {
	return s.effectivePass(s.ReviewSeries.ProductReview) && s.effectivePass(s.ReviewSeries.StartReadiness)
}

// reviewAdmission checks the signed dispatch, authorization, confirmed revision
// and redo floor before any worker content can consume an attempt. A carried
// dispatch keeps its original confirmed revision; a redo requires fresh input.
// The product-review sequence gate is temporary and can suspend result pairing.
func (s *State) reviewAdmission(evidence *ReviewResultEvidence, actionID string, issuedRevision uint64) (review.Set, error) {
	// 派发身份唯一事实：证据的 DispatchID 必须就是提交结果的 actionID
	//（授权占用按真实派发判）。
	if evidence.DispatchID != actionID {
		return review.Set{}, &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("review evidence dispatch id %q does not match the dispatching action %q",
				evidence.DispatchID, actionID),
		}
	}
	series, _ := s.ReviewSeries.Series(evidence.Action)
	if evidence.AuthorizationID != review.AuthorizationOfDispatch(series, actionID) && (evidence.AuthorizationID != "" || series.Completed >= series.AutomaticLimit) {
		return review.Set{}, &RejectedError{Code: CodeReviewEvidenceRejected, Detail: "review authorization differs from its signed dispatch"}
	}
	if seriesInRedoStale(series) {
		if evidence.RequirementRevision != s.RequirementRevision() {
			return review.Set{}, &RejectedError{
				Code: CodeReviewEvidenceRejected,
				Detail: fmt.Sprintf("%s is being redone; its fresh round must bind the current authoritative revision %q (evidence binds %q)",
					evidence.Action, s.RequirementRevision(), evidence.RequirementRevision),
			}
		}
		if issuedRevision < series.FreshFloor {
			return review.Set{}, &RejectedError{
				Code: CodeReviewEvidenceRejected,
				Detail: fmt.Sprintf("%s fresh round requires a dispatch issued at or after the redo decision (state revision >= %d; this dispatch was issued at %d)",
					evidence.Action, series.FreshFloor, issuedRevision),
			}
		}
	} else if generation, confirmed := s.confirmedRevisionGeneration(evidence.RequirementRevision); !confirmed {
		return review.Set{}, &RejectedError{
			Code: CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("review evidence requirement revision %q is not a confirmed revision (current authoritative %q)",
				evidence.RequirementRevision, s.RequirementRevision()),
		}
	} else if generation < series.FreshFloor || issuedRevision < series.FreshFloor {
		// 重走系列的下界为最后一次重走确认代次；内容确认和派发签发
		// 都不得早于它。内容 hash 的重复确认不改变历史派发的签发代次。
		// floor 为零（从未重走）的系列两个比较恒假，carry 语义原样。
		return review.Set{}, &RejectedError{
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
		return review.Set{}, &RejectedError{
			Code:   CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("confirmed blocking finding on %s requires a changed and reconfirmed requirement revision before a fresh round", evidence.Action),
		}
	}
	if evidence.Action == review.StartReadiness && !s.effectivePass(s.ReviewSeries.ProductReview) {
		return review.Set{}, &RejectedError{
			Code: CodeReviewSequenceGate,
			Detail: fmt.Sprintf("start-readiness evidence requires a carried product-review PASS at a confirmed requirement revision (current: %s bound to %q)",
				s.ReviewSeries.ProductReview.Effective.Status, s.ReviewSeries.ProductReview.Effective.RequirementRevision),
		}
	}
	return s.ReviewSeries, nil
}

func (s *State) admitReviewEvidence(evidence *ReviewResultEvidence, actionID string, issuedRevision uint64) (review.Set, error) {
	if _, err := s.reviewAdmission(evidence, actionID, issuedRevision); err != nil {
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
		evidence.Verification)
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
func (e *Engine) prepareReviewResult(state *State, evidence *ReviewResultEvidence, actionID string) (review.Set, error) {
	updated, err := state.admitReviewEvidence(evidence, actionID, state.issuedRevisionOf(actionID))
	if err != nil {
		return review.Set{}, err
	}
	if pipelineEnabled(state, e.cfg.Definition) && evidence.Action == review.StartReadiness && evidence.Status == "PASS" {
		if err := validateGranularity(evidence.GranularityReview); err != nil {
			return review.Set{}, &RejectedError{Code: CodeReviewEvidenceRejected, Detail: err.Error()}
		}
	}
	return updated, nil
}

// ---- 审查类决定的账目变换（DECIDE 接纳路径） ----

// admitReviewDecision 在选项合法后执行审查类业务控制的账目变换：校验
// 控制↔分支配对与选项一致性，再做对应变换。任一拒绝都发生在 state 变
// 更之前（零状态变化）。授权者身份固定：DECIDE 本身就是用户决定，
// UserAuthorized 恒真，Source 形如 engine-decide:<requestID>。
func (e *Engine) admitReviewDecision(state *State, ask PendingAsk, requestID string, decide *DecidePayload) error {
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
		if state.Topology != nil {
			return reviewReject(fmt.Errorf("topology is already bound and is the binding point; it is not re-cut (change goes through requirement change, reset, or abort)"))
		}
		if !state.effectivePass(state.ReviewSeries.StartReadiness) {
			return reviewReject(fmt.Errorf("topology confirmation requires a carried start-readiness PASS at a confirmed requirement revision (current: %s bound to %q)",
				state.ReviewSeries.StartReadiness.Effective.Status, state.ReviewSeries.StartReadiness.Effective.RequirementRevision))
		}
		decision := payload.Topology
		state.Topology = &TopologyBinding{
			Decision:   decision.Decision,
			Note:       strings.TrimSpace(decision.Note),
			SplitCount: decision.SplitCount,
			Slices:     append([]string{}, decision.Slices...),
			Parallel:   strings.TrimSpace(decision.Parallel),
		}
		return nil
	case ControlSelectRoute:
		if branch != "route" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "SELECT_ROUTE takes the route branch"}
		}
		// 选项锚定：choice id 恰为 full/custom 且与路线决定一致（同
		// CONFIRM_TOPOLOGY 的锚定理由）。
		if choice := string(decide.Choice); (choice != routeModeFull && choice != routeModeCustom) || choice != payload.Route.Mode {
			return &RejectedError{
				Code:   CodeEventSchemaInvalid,
				Detail: fmt.Sprintf("route payload mode %q does not match the chosen option %q (closed set: full, custom)", payload.Route.Mode, decide.Choice),
			}
		}
		// 候选表必须与 Ask 钉住的全集集合相等（顺序无关）：少列或多列任一
		// 门，决定声称的候选表就不再是用户所见并批准的全集——可区分拒
		// 绝、零写，Ask 保持未决，修正后的决定仍可提交。
		if !sameGateSet(payload.Route.Gates, ask.RouteGates) {
			return reviewReject(fmt.Errorf("route candidate gates %v do not match the gate list pinned on ask %s (%v); the decide must repeat exactly the list the user approved", payload.Route.Gates, requestID, ask.RouteGates))
		}
		if state.Topology == nil {
			return reviewReject(fmt.Errorf("route selection requires a bound topology (start-readiness PASS is not yet confirmed as topology)"))
		}
		// 进入开发的双审门：拓扑确认后任一审查重新阻塞（纠正升 P1、级联
		// 上游失效、重走置 PENDING）时，路线选择保持关闭，直到双 series
		// 重新携已确认 revision 的 effective PASS。
		if !state.dualReviewPass() {
			return reviewReject(fmt.Errorf("route selection requires both pre-development reviews effective PASS carried at confirmed requirement revisions (current: product %s bound to %q, start-readiness %s bound to %q)",
				state.ReviewSeries.ProductReview.Effective.Status, state.ReviewSeries.ProductReview.Effective.RequirementRevision,
				state.ReviewSeries.StartReadiness.Effective.Status, state.ReviewSeries.StartReadiness.Effective.RequirementRevision))
		}
		route := payload.Route
		if err := e.validatePlanConfirmation(state, route); err != nil {
			return err
		}
		selected := slices.Clone(route.Gates)
		if route.Mode == routeModeCustom {
			selected = nil
			for _, gate := range route.Gates {
				if slices.Contains(route.Selected, gate) {
					selected = append(selected, gate)
				}
			}
		}
		if err := e.validateRouteChange(state, selected); err != nil {
			return err
		}
		before := state.validationRouteDigest()
		state.RouteMode = route.Mode
		state.SelectedGates = selected
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

// advanceReviewPhases 沿静态边表逐边推进 run phase：每个门的条件都能从
// 持久账本重算（受理回执、携已确认 revision 的双审 effective、拓扑/路线
// 绑定）。在会改变审查账本/拓扑/路线的同一提交事务内调用；条件不满足时
// 静默留置（这不是错误）。START_READINESS→TOPOLOGY_AND_ROUTE 与上一边
// 同条件连续放行（readiness 是确定性判定，无独立数据面）。
func (s *State) advanceReviewPhases() error {
	for {
		var target runtime.RunPhase
		switch s.Phase {
		case runtime.PhaseIntakeRegistered:
			if s.IntakeReceipt != nil {
				target = runtime.PhaseProductReview
			}
		case runtime.PhaseProductReview:
			if s.effectivePass(s.ReviewSeries.ProductReview) {
				target = runtime.PhaseTechnicalReview
			}
		case runtime.PhaseTechnicalReview:
			if s.effectivePass(s.ReviewSeries.StartReadiness) {
				target = runtime.PhaseStartReadiness
			}
		case runtime.PhaseStartReadiness:
			if s.effectivePass(s.ReviewSeries.StartReadiness) {
				target = runtime.PhaseTopologyAndRoute
			}
		case runtime.PhaseTopologyAndRoute:
			// 进入开发的双审门与 SELECT_ROUTE 接纳同条件：账本只能证明
			// 双 series 携已确认 revision 的 effective PASS 时才推进。
			if s.Topology != nil && s.RouteMode != "" && s.dualReviewPass() {
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
// 都是单调的持久事实（受理回执、完成过的审查轮、拓扑/路线确认决定）。
// effective 投影会随新的阻塞轮或需求修订重走合法回落，当前绑定会按用户
// 勾选的重走范围解除，但不会抹掉"该阶段曾经走完"的事实，因此 residue
// 不以当前 effective 或当前绑定为准。
func (s *State) phaseLedgerFloor() int {
	floor := 0
	if s.IntakeReceipt != nil {
		floor = 1
	}
	if s.hasReviewRounds(review.ProductReview) {
		floor = 2
	}
	if s.hasReviewRounds(review.StartReadiness) {
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
// run phase 仍须被账本证明合法。
func (s *State) topologyRouteConfirmed() bool {
	var topology, route bool
	for _, decision := range s.Decisions {
		switch decision.Control {
		case ControlConfirmTopology:
			topology = true
		case ControlSelectRoute:
			route = true
		}
	}
	return topology && route
}

// phaseRank 给主线前置段排序；DEVELOPMENT_PARALLEL 之后的 phase 排在
// 本阶段账本可证明的范围之外，TERMINAL 由终止闭包豁免（任意 phase 都
// 可直接进入 cleanup 终态）。
func phaseRank(phase runtime.RunPhase) int {
	switch phase {
	case runtime.PhaseIntakeRegistered:
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

// verifyPhaseAgainstLedgers 重算 phase 与账本的一致性：非 TERMINAL 的
// phase 越过账本 residue 能证明的最远位置（手工拨过门）即拒绝。落后于
// residue 的 phase 不拒绝——下一次提交会沿门控补进，兼容审查账本字段
// 上线前落盘的既有 run。
func (s *State) verifyPhaseAgainstLedgers() error {
	if s.Phase == runtime.PhaseTerminal {
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
		for _, entry := range state.RequirementRevisions {
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
