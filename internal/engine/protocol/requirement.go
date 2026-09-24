// requirement.go 是用户需求变化（总需求 §6.3.2、§7.1、§7.2）在提交协议层
// 的嫁接点：REQUEST_REQUIREMENT_CHANGE 的两段 Ask（Ask 即用户确认边界，
// 持久化 typed 决定包）、确认 revision 账本的登记与完整性校验、修订屏障
// （Phase A）的单一处置表与持久隔离账及其原子重分类。含义分类与重走范
// 围由用户在 Ask 包中逐项确认，引擎只做机械接纳与机械执行——不自动扩大
// 失效范围，也不把 P2/P3 修订自行升级。所有写入经 Engine 的事务紧随其后
// 原子完成，本文件只做校验与 state 变换、不做 I/O。
package protocol

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
)

// 需求修订来源与含义分类的封闭词汇（持久化契约的一部分）。INTAKE 是账本
// 初始账目专用值，不出现在决定包中。
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
	// RequirementClassificationPreserved：含义不变——默认不清审、不重审、
	// 不耗审查轮次（§7.1.3）；用户仍可携重走 scope（保留产品含义＋重做
	// 受影响技术审是合法组合，§7.2.1）。
	RequirementClassificationPreserved = "PRESERVED"
	// RequirementClassificationChanged：含义变化。两类分类的重走 scope
	// 计算与机械执行完全一致，差别只在默认语义与 P1 源硬规则。
	RequirementClassificationChanged = "CHANGED"

	// requirement 决定 choice id 的封闭两值（与 REVIEW_ROUND_GRANT 的
	// grant/decline 同理的固定项锚定）：abort 放弃变更（不得携载荷），
	// proceed 确认变更包（必携 typed 载荷）。
	requirementProceedOption = "proceed"
	requirementAbortOption   = "abort"
)

// requirementScopeItems 是 DecidedScope 的封闭重走项集合（固定顺序，前两
// 项即 review.Action 的持久化值）。
var requirementScopeItems = []string{
	string(review.ProductReview),
	string(review.StartReadiness),
	"topology",
	"route",
}

// validRequirementSource 报告来源是否属于决定包可用的封闭三值。
func validRequirementSource(source string) bool {
	switch source {
	case RequirementSourceUserInitiated, RequirementSourceReviewFindingP2, RequirementSourceReviewFindingP1:
		return true
	}
	return false
}

// validRequirementClassification 报告分类是否属于决定包可用的封闭两值。
func validRequirementClassification(classification string) bool {
	return classification == RequirementClassificationPreserved || classification == RequirementClassificationChanged
}

// ---- 事件 schema（无状态形状校验，由 Event.Validate 调用） ----

// validateRequirementPackage 校验变更包的形状：候选 revision 与 delta 指纹
// 非空；来源与分类封闭；发现项驱动的来源必携 finding digest，用户主动发
// 起必空。来源↔分类↔真实裁决的对照与账目绑定在接纳层执行。
func validateRequirementPackage(pkg *RequirementDecisionPackage) error {
	if strings.TrimSpace(pkg.CandidateRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the candidate revision binding"}
	}
	if strings.TrimSpace(pkg.DeltaDigest) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the review-delta digest binding"}
	}
	if !validRequirementSource(pkg.Source) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("requirement decision source %q invalid (closed set: USER_INITIATED, REVIEW_FINDING_P2, REVIEW_FINDING_P1)", pkg.Source)}
	}
	if !validRequirementClassification(pkg.Classification) {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("requirement decision classification %q invalid (closed set: PRESERVED, CHANGED)", pkg.Classification)}
	}
	findingBound := strings.TrimSpace(pkg.FindingDigest) != ""
	if pkg.Source == RequirementSourceUserInitiated && findingBound {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a user-initiated requirement decision cannot bind a review finding digest"}
	}
	if pkg.Source != RequirementSourceUserInitiated && !findingBound {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a finding-driven requirement decision must bind its finding digest"}
	}
	return nil
}

// validateRequirementDecisionPayload 校验 DECIDE 载荷的形状：包字段复用
// validateRequirementPackage，另加 CAS 基线（非空且≠候选）与决定理由非空。
func validateRequirementDecisionPayload(payload *RequirementDecisionPayload) error {
	if strings.TrimSpace(payload.BaseRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the base revision it is decided against"}
	}
	if payload.CandidateRevision == payload.BaseRevision {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision candidate revision must differ from its base"}
	}
	if strings.TrimSpace(payload.Reason) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires a non-empty reason"}
	}
	pkg := payload.packageView()
	return validateRequirementPackage(&pkg)
}

// ---- 权威 revision 读取与修订屏障 ----

// RequirementRevision 返回当前权威需求 revision：确认账本的末条；账本为空
// （受理回执尚未落账的窗口或账本字段之前的旧 run）时回退受理确认回执。
// 一切 revision 比对（审查证据绑定、决定账目、结论 carry 有效性）都读它，
// 需求变化登记后自动跟随新权威。
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
// REQUEST_REQUIREMENT_CHANGE Ask。
func (s *State) requirementChangePending() bool {
	for _, ask := range s.PendingAsks {
		if ask.Control == ControlRequestRequirementChange && !ask.Resolved {
			return true
		}
	}
	return false
}

// ---- 修订屏障的单一处置表（§7.2.2 Phase A，S2） ----

// errPhaseAQuarantine 是处置表对 WORKER_RESULT 的哨兵：区别于 nil（照常
// 接纳）与拒绝错误，表示进入持久隔离账（S3）。
var errPhaseAQuarantine = errors.New("phase A quarantine")

// admitInCurrentState 是修订屏障在位时的单一处置表：一切 admit 分支与
// HostAction intent 落账都经它，不存在表外的屏障检查。
//
//	照常接纳：观察类（lifecycle/task-progress/operator observation）、在途
//	          回执（spawn/HostAction，可到 terminal）、Ask 创建（惰性，其
//	          DECIDE 另经表）、需求变化自身的 REQUEST/DECIDE、RESET/ABORT。
//	持久隔离：WORKER_RESULT（在途计算结果，S3 原子重分类）。
//	拒绝    ：其余一切 DECIDE（拓扑/路线/审查决定/grant/恢复）与新的
//	          HostAction intent（停止新的权威写入）。
//
// hostActionIntent=true 表示 Submit 之外的 HostAction intent 落账路径
// （Event 为零值）。
func (s *State) admitInCurrentState(ev Event, hostActionIntent bool) error {
	if hostActionIntent {
		return &RejectedError{
			Code:   CodeChangeBarrierActive,
			Detail: "host action intents are frozen while a requirement change ask is pending",
		}
	}
	switch ev.Kind {
	case KindLifecycleEvent, KindTaskProgress, KindOperatorObservation, KindSpawnReceipt, KindHostActionReceipt, KindRequestControl:
		return nil
	case KindDecide:
		if ask, ok := s.PendingAsks[string(ev.Decide.Request)]; ok {
			switch ask.Control {
			case ControlRequestRequirementChange, ControlReset, ControlAbort:
				return nil
			}
			return &RejectedError{
				Code:   CodeChangeBarrierActive,
				Detail: fmt.Sprintf("deciding %s is frozen while a requirement change ask is pending", ask.Control),
			}
		}
		return &RejectedError{
			Code:   CodeChangeBarrierActive,
			Detail: fmt.Sprintf("decide on request %q is frozen while a requirement change ask is pending", ev.Decide.Request),
		}
	case KindWorkerResult:
		return errPhaseAQuarantine
	}
	return &RejectedError{
		Code:   CodeChangeBarrierActive,
		Detail: fmt.Sprintf("event kind %s is frozen while a requirement change ask is pending", ev.Kind),
	}
}

// quarantineWorkerResult 把屏障期的 worker result 原子落入持久隔离账
// （按 eventID，事件同时进台账防重放）：不推进、不入 Results，接纳回执
// Status=QUARANTINED。重分类发生在需求决定落账的同一事务内
// （replayQuarantinedResults），进程重启不丢失。
func (e *Engine) quarantineWorkerResult(state *State, ev Event, digest string, nextRevision uint64) (Acceptance, *State, bool, error) {
	state.QuarantinedResults[string(ev.ID)] = WorkerResult{
		ActionID:      ev.Result.ActionID,
		Provider:      ev.Result.Provider,
		Outcome:       ev.Result.Outcome,
		PayloadDigest: ev.Result.PayloadDigest,
		FailureClass:  ev.Result.FailureClass,
		Review:        ev.Result.Review,
		Digest:        digest,
	}
	return Acceptance{
		EventID: string(ev.ID), Kind: string(ev.Kind), Status: "QUARANTINED",
		Revision: nextRevision, ActionID: ev.Result.ActionID,
	}, state, true, nil
}

// replayQuarantinedResults 在需求决定（含放弃）落账的同一事务内原子重分
// 类隔离账（§7.2.2 的恢复侧）：
//   - CHANGED：绑旧权威 revision 的审查证据经 F3 永不可纳——直接作废入
//     ObsoleteResults 并留痕（scope 覆盖与否都作废，这是不变量不是选择）。
//   - 其余（放弃/PRESERVED 的全部、CHANGED 下的非 Review 结果）：逐条走
//     正常接纳路径重放——回执未到则回到暂存，已配对则完成；接纳错误
//     （结构缺陷、revision 失配等）不否决决定，把该结果判为不可纳、入
//     ObsoleteResults 并留痕。
//
// 重放按 eventID 排序保持确定性；重放内部的补位签发由 refill 的既有屏障
// 扣发持住，决定提交后的统一 refill 补进。
func (e *Engine) replayQuarantinedResults(state *State, classification, newRevision string, nextRevision uint64, expectedFingerprint string) {
	if len(state.QuarantinedResults) == 0 {
		return
	}
	eventIDs := make([]string, 0, len(state.QuarantinedResults))
	for eventID := range state.QuarantinedResults {
		eventIDs = append(eventIDs, eventID)
	}
	slices.Sort(eventIDs)
	for _, eventID := range eventIDs {
		record := state.QuarantinedResults[eventID]
		delete(state.QuarantinedResults, eventID)
		if record.Review != nil && classification == RequirementClassificationChanged && record.Review.RequirementRevision != newRevision {
			state.ObsoleteResults[record.ActionID] = record
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail:   fmt.Sprintf("quarantined %s evidence bound to superseded requirement revision %q is permanently inadmissible and was voided by the confirmed change", record.Review.Action, record.Review.RequirementRevision),
				Revision: nextRevision,
			})
			continue
		}
		if err := e.readmitWorkerResult(state, record, nextRevision, expectedFingerprint); err != nil {
			state.ObsoleteResults[record.ActionID] = record
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail:   fmt.Sprintf("quarantined result could not be readmitted after the requirement decision: %v", err),
				Revision: nextRevision,
			})
		}
	}
}

// readmitWorkerResult 以正常接纳语义重放一条结果：回执未到（或 UNKNOWN）
// 则按 result-before-receipt 回到暂存（携审查证据的先过完整可接纳性预
// 检），已配对则完成该 expected 项。
func (e *Engine) readmitWorkerResult(state *State, record WorkerResult, nextRevision uint64, expectedFingerprint string) error {
	if existing, seen := state.Results[record.ActionID]; seen {
		if existing.Digest == record.Digest {
			return nil
		}
		return fmt.Errorf("action %q already has a different admitted result", record.ActionID)
	}
	if receipt, receipted := state.SpawnReceipts[record.ActionID]; !receipted || receipt.Status == SpawnStatusUnknown {
		if record.Review != nil {
			if _, err := state.admitReviewEvidence(record.Review); err != nil {
				return err
			}
		}
		if staged, exists := state.StagedResults[record.ActionID]; exists {
			if staged.Digest == record.Digest {
				return nil
			}
			return fmt.Errorf("action %q already has a different staged worker result", record.ActionID)
		}
		state.StagedResults[record.ActionID] = record
		return nil
	}
	_, err := e.completeResult(state, record, nextRevision, expectedFingerprint)
	return err
}

// ---- 决定的账目变换（DECIDE 接纳路径） ----

// admitRequirementDecision 在选项合法后执行需求变化控制的账目变换。
// choice id 封闭为 proceed/abort（固定项锚定）：abort 不得携载荷——放弃
// 即不登记，但隔离账仍须重分类（屏障解除）；proceed 必携 typed 载荷且与
// Ask 落账的确认包逐字段相等（Ask 即用户确认边界，包外内容不存在）。
// 前置校验全部通过后，同一事务内：登记 ConfirmedRevision → 机械执行
// scope（两类分类一致：勾选的 series stale、拓扑/路线解绑）→ 统一按
// revision 绑定作废旧权威审查暂存 → 原子重分类隔离账。
func (e *Engine) admitRequirementDecision(state *State, ask PendingAsk, requestID string, decide *DecidePayload, nextRevision uint64, expectedFingerprint string) error {
	if ask.Control != ControlRequestRequirementChange {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s does not take a requirement decision payload", ask.Control)}
	}
	switch choice := string(decide.Choice); choice {
	case requirementAbortOption:
		if decide.Requirement != nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "aborting a requirement change cannot carry a decision payload"}
		}
		// 放弃：不登记 revision、不动 scope，隔离账按现行权威重放（应被纳）。
		e.replayQuarantinedResults(state, "", state.RequirementRevision(), nextRevision, expectedFingerprint)
		return nil
	case requirementProceedOption:
		if decide.Requirement == nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "proceeding with a requirement change requires the typed requirement payload"}
		}
	default:
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("requirement change choice %q invalid (closed set: proceed, abort)", choice)}
	}
	payload := decide.Requirement
	reject := func(detail string) error {
		return &RejectedError{Code: CodeRequirementDecisionRejected, Detail: detail}
	}
	// S1：DECIDE 载荷必须与 Ask 落账的确认包逐字段相等——同一 Ask 不可
	// 登记不同于用户所见所确认的变更内容。
	if ask.Package == nil {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "the requirement change ask does not carry the confirmed package"}
	}
	if payload.packageView() != *ask.Package {
		return reject(fmt.Sprintf("the decide payload does not match the package confirmed on ask %s (conflict)", requestID))
	}
	if state.IntakeReceipt == nil {
		return reject("requirement change requires the recorded intake receipt (the run has not started)")
	}
	current := state.RequirementRevision()
	if payload.BaseRevision != current {
		return reject(fmt.Sprintf("requirement decision base revision %q is not the current authoritative revision %q (conflict)", payload.BaseRevision, current))
	}
	// 来源必须绑用户的 finding 处置（§7.1 的引擎强制）：
	//   - 未裁决（raw）的 finding 不能驱动任何修订来源——先处置再发起。
	//   - REVIEW_FINDING_P1 ⇔ 有效裁决 CONFIRM/RESEVERITIZE 且有效严重度
	//     P0/P1，且必须 CHANGED、重走范围包含 finding 所在 series——确认
	//     P0/P1 的新鲜复审义务，用户不可选 PRESERVED。
	//   - REVIEW_FINDING_P2 ⇔ 有效裁决 CONFIRM/RESEVERITIZE 且有效严重度
	//     P2/P3（P2/P3 处置默认 meaning-preserved；升级为 CHANGED 走 P1
	//     来源，标签必须真实）。
	//   - USER_INITIATED 不绑 finding，两种分类皆可。
	if payload.Source != RequirementSourceUserInitiated {
		owner, decision, severity, found := review.FindingDecision(state.ReviewSeries, payload.FindingDigest)
		if !found {
			return reject(fmt.Sprintf("requirement decision binds finding digest %q that no recorded review evidence contains", payload.FindingDigest))
		}
		if decision != "CONFIRM" && decision != "RESEVERITIZE" {
			return reject(fmt.Sprintf("an unadjudicated finding (effective decision %q) cannot drive a requirement change; adjudicate the disposition first", decision))
		}
		blocking := severity == "P0" || severity == "P1"
		if payload.Source == RequirementSourceReviewFindingP1 {
			if !blocking {
				return reject(fmt.Sprintf("REVIEW_FINDING_P1 requires the finding to be a confirmed blocking finding (effective decision %q at severity %q)", decision, severity))
			}
			if payload.Classification != RequirementClassificationChanged {
				return reject("a REVIEW_FINDING_P1 requirement decision must be classified CHANGED (a confirmed P0/P1 requires the fresh re-review)")
			}
			if (owner == review.ProductReview && !payload.RedoProductReview) || (owner == review.StartReadiness && !payload.RedoStartReadiness) {
				return reject(fmt.Sprintf("a REVIEW_FINDING_P1 requirement decision must redo the owning review series %s", owner))
			}
		} else if blocking {
			return reject("REVIEW_FINDING_P2 cannot label a confirmed blocking finding; declare the REVIEW_FINDING_P1 source (the CHANGED upgrade stays available there)")
		}
	}
	// scope 的计算与应用对两类分类完全一致（§7.2.1：PRESERVED 保留产品含
	// 义＋重做受影响技术审是合法组合）；唯一硬规则是上面的 P1 源两约束。
	scope := make([]string, 0, len(requirementScopeItems))
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
	// 机械执行 scope：勾选的 series 调单 series stale 原语（不跨 series
	// 级联：未勾选即续用）；勾选拓扑/路线即解绑，重新确认走既有
	// CONFIRM_TOPOLOGY/SELECT_ROUTE 路径。phase 与推进门不变。
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
	// 统一的暂存规则：任何已确认 revision 变化（PRESERVED 或 CHANGED）都
	// 使绑旧权威 revision 的审查暂存条目经 F3 永不可纳——一律清除＋留痕
	//（规则只认 revision 绑定，不认 scope）；非 Review 暂存不动。
	for actionID, staged := range state.StagedResults {
		if staged.Review == nil || staged.Review.RequirementRevision != payload.BaseRevision {
			continue
		}
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
			ActionID: actionID,
			Detail:   fmt.Sprintf("staged %s evidence bound to superseded requirement revision %q invalidated by the confirmed requirement change %s", staged.Review.Action, payload.BaseRevision, payload.CandidateRevision),
			Revision: nextRevision,
		})
		delete(state.StagedResults, actionID)
	}
	// 原子重分类隔离账。
	e.replayQuarantinedResults(state, payload.Classification, payload.CandidateRevision, nextRevision, expectedFingerprint)
	return nil
}

// ---- 账本完整性（加载/提交边界） ----

// verifyRequirementLedger 重算确认 revision 账本的结构链：首条必须是与受
// 理回执一致的 INTAKE 初始账目；其后每条 base 链精确衔接、来源/分类/重走
// 项封闭、state 代数严格单调，且 P1 源不得声明 PRESERVED（伪造分类可检）。
// PRESERVED 携 scope 合法（§7.2.1 组合）。账本为空的旧 run 跳过（由解码
// 边界的受理回执回填向前兼容）。
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
