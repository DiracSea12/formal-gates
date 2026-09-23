// review.go 是开发前审查纯内核（internal/engine/review）在提交协议层的
// 嫁接点：审查证据随 WORKER_RESULT 接纳入账、四个审查类业务控制的
// DECIDE 账目变换、run phase 的门控推进与加载/提交边界的账本完整性
// 校验。所有持久化都经 Engine 的事务紧随其后原子完成，本文件只做
// 校验与 state 变换、不做 I/O。
package protocol

import (
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
		if strings.TrimSpace(grant.Reason) == "" || strings.TrimSpace(grant.DispatchID) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "round grant requires a non-empty reason and dispatch binding"}
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

// validateRouteDecision 校验路线决定的形状：full 携带显式完整门全集且
// Selected 为空；custom 携带非空、无重复、属于全集的真子集。
func validateRouteDecision(route *RouteDecision) error {
	switch route.Mode {
	case routeModeFull:
		if len(route.Selected) != 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "full route selects the complete gate list without a subset"}
		}
	case routeModeCustom:
		if len(route.Selected) == 0 {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "custom route must select a non-empty proper subset"}
		}
		if len(route.Selected) >= len(route.Gates) {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "custom route must select a proper subset; use full for the complete list"}
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
	if len(route.Gates) == 0 {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "route decision requires the explicit candidate gate list"}
	}
	seen := make(map[string]bool, len(route.Gates))
	for _, gate := range route.Gates {
		if strings.TrimSpace(gate) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "route candidate gates cannot be empty"}
		}
		if seen[gate] {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("route candidate gate %q listed twice", gate)}
		}
		seen[gate] = true
	}
	return nil
}

// ---- 审查证据接纳（WORKER_RESULT 同事务） ----

// requirementRevision 返回需求 revision 的权威来源：受理确认回执的
// RequirementRevision（本阶段内不变；需求变化控制属后续单元）。
func (s *State) requirementRevision() string {
	if s.IntakeConfirmationReceipt == nil {
		return ""
	}
	return s.IntakeConfirmationReceipt.RequirementRevision
}

// effectivePassAt 报告 series 的 effective 是否为 PASS 且绑定给定需求
// revision——双审顺序与拓扑门控都以它为准。
func effectivePassAt(series review.Series, revision string) bool {
	return series.Effective.Status == reviewStatusPass && series.Effective.RequirementRevision == revision
}

// dualReviewPassAt 报告双 series 的 effective 是否都在给定需求 revision 下
// PASS——进入开发的门（SELECT_ROUTE 接纳与 TOPOLOGY_AND_ROUTE 推进）以它
// 为准：任一审查重新阻塞时门保持关闭。
func (s *State) dualReviewPassAt(revision string) bool {
	return effectivePassAt(s.ReviewSeries.ProductReview, revision) && effectivePassAt(s.ReviewSeries.StartReadiness, revision)
}

// admitReviewEvidence 对一条审查证据执行完整可接纳性判定并返回记账后的
// Set：受理 revision 绑定（stale 绑定的结果不计）、start-readiness 的双审
// 顺序守卫（product-review 须同 revision effective PASS）、review 内核的
// 记录变换（重复摘要、越限无授权、核实缺项、finding 形状）。非空错误即
// CodeReviewEvidenceRejected 且零状态变化。暂存预检与回执配对后的权威
// 接纳共用本函数（单一事实源）：预检丢弃返回值只取错误——内核变换不改
// 写调用方输入（包契约并有回归测试），账本在暂存与回执之间可能变化，落
// 账仍以配对时的接纳为准。
func (s *State) admitReviewEvidence(evidence *ReviewResultEvidence) (review.Set, error) {
	if evidence.RequirementRevision != s.requirementRevision() {
		return review.Set{}, &RejectedError{
			Code:   CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("review evidence requirement revision %q is a stale binding (intake binds %q)", evidence.RequirementRevision, s.requirementRevision()),
		}
	}
	if evidence.Action == review.StartReadiness && !effectivePassAt(s.ReviewSeries.ProductReview, s.requirementRevision()) {
		return review.Set{}, &RejectedError{
			Code:   CodeReviewEvidenceRejected,
			Detail: fmt.Sprintf("start-readiness evidence requires product-review effective PASS at requirement revision %q (current: %s)", s.requirementRevision(), s.ReviewSeries.ProductReview.Effective.Status),
		}
	}
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
	return updated, nil
}

// recordReviewCandidate 把审查证据经完整可接纳性判定后记账落回 state。
// RecordCandidate 的错误（重复摘要、越限无授权、核实不全）拒绝整个事
// 件。
func (e *Engine) recordReviewCandidate(state *State, evidence *ReviewResultEvidence) error {
	updated, err := state.admitReviewEvidence(evidence)
	if err != nil {
		return err
	}
	state.ReviewSeries = updated
	return nil
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
		updated, err := review.AppendAuthorization(state.ReviewSeries, state.RunID, grant.Action, grant.Reason, grant.DispatchID)
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
		correction.UserAuthorized = true
		correction.Source = "engine-decide:" + requestID
		updated, err := review.ApplyCorrection(state.ReviewSeries, state.RunID, state.requirementRevision(), correction)
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
		if !effectivePassAt(state.ReviewSeries.StartReadiness, state.requirementRevision()) {
			return reviewReject(fmt.Errorf("topology confirmation requires start-readiness effective PASS at requirement revision %q (current: %s)", state.requirementRevision(), state.ReviewSeries.StartReadiness.Effective.Status))
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
		if state.Topology == nil {
			return reviewReject(fmt.Errorf("route selection requires a bound topology (start-readiness PASS is not yet confirmed as topology)"))
		}
		// 进入开发的双审门：拓扑确认后任一审查重新阻塞（纠正升 P1、级联
		// 上游失效）时，路线选择保持关闭，直到双 series 重新 effective PASS。
		if !state.dualReviewPassAt(state.requirementRevision()) {
			return reviewReject(fmt.Errorf("route selection requires both pre-development reviews effective PASS at requirement revision %q (current: product %s, start-readiness %s)",
				state.requirementRevision(), state.ReviewSeries.ProductReview.Effective.Status, state.ReviewSeries.StartReadiness.Effective.Status))
		}
		route := payload.Route
		state.RouteMode = route.Mode
		if route.Mode == routeModeFull {
			state.SelectedGates = append([]string{}, route.Gates...)
			return nil
		}
		// custom：按候选全集顺序归一存储，选择集与全集的派生顺序无关。
		selected := make([]string, 0, len(route.Selected))
		for _, gate := range route.Gates {
			if slices.Contains(route.Selected, gate) {
				selected = append(selected, gate)
			}
		}
		state.SelectedGates = selected
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
// 持久账本重算（受理回执、双审 effective、拓扑/路线绑定）。在会改变审
// 查账本/拓扑/路线的同一提交事务内调用；条件不满足时静默留置（这不是
// 错误）。START_READINESS→TOPOLOGY_AND_ROUTE 与上一边同条件连续放行
// （readiness 是确定性判定，无独立数据面）。
func (s *State) advanceReviewPhases() error {
	for {
		var target runtime.RunPhase
		switch s.Phase {
		case runtime.PhaseIntakeRegistered:
			if s.IntakeReceipt != nil {
				target = runtime.PhaseProductReview
			}
		case runtime.PhaseProductReview:
			if effectivePassAt(s.ReviewSeries.ProductReview, s.requirementRevision()) {
				target = runtime.PhaseTechnicalReview
			}
		case runtime.PhaseTechnicalReview:
			if effectivePassAt(s.ReviewSeries.StartReadiness, s.requirementRevision()) {
				target = runtime.PhaseStartReadiness
			}
		case runtime.PhaseStartReadiness:
			if effectivePassAt(s.ReviewSeries.StartReadiness, s.requirementRevision()) {
				target = runtime.PhaseTopologyAndRoute
			}
		case runtime.PhaseTopologyAndRoute:
			// 进入开发的双审门与 SELECT_ROUTE 接纳同条件：账本只能证明
			// 双 series 当前 effective PASS 时才推进。
			if s.Topology != nil && s.RouteMode != "" && s.dualReviewPassAt(s.requirementRevision()) {
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
// 都是单调的持久事实（受理回执、当前需求 revision 下完成过的审查轮、
// 拓扑/路线绑定）。effective 投影会随新的阻塞轮合法回落，但不会抹掉
// "该阶段审查曾经完成"的事实，因此 residue 不以当前 effective 为准。
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
	if s.Topology != nil && s.RouteMode != "" {
		floor = 5
	}
	return floor
}

// hasReviewRounds 报告 series 在当前需求 revision 下是否记录过审查候选
// （审查轮发生过；轮次是否有效由账本完整性校验负责）。
func (s *State) hasReviewRounds(action review.Action) bool {
	series, ok := s.ReviewSeries.Series(action)
	if !ok {
		return false
	}
	revision := s.requirementRevision()
	for _, candidate := range series.RawCandidates {
		if candidate.RequirementRevision == revision {
			return true
		}
	}
	return false
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

// verifyReviewIntegrity 在加载/提交边界重算审查账本的全部投影与身份链
// 并校验 phase 一致性：篡改的账本或越门的 phase 在此可区分拒绝。
func verifyReviewIntegrity(state *State) error {
	if err := review.Validate(state.RunID, state.ReviewSeries); err != nil {
		return &RejectedError{Code: CodeStateIntegrityInvalid, Detail: fmt.Sprintf("review ledger: %v", err)}
	}
	return state.verifyPhaseAgainstLedgers()
}
