// requirement.go 是用户需求变化（总需求 §6.3.2、§7.1、§7.2）在提交协议层
// 的嫁接点：REQUEST_REQUIREMENT_CHANGE 先建立屏障，准备好的完整包
// 经新鲜 token 绑定到同一 Ask，再由用户确认、确认 revision 账本的登记与完整性校验、修订屏障
// （Phase A）的单一处置表与持久隔离账及其原子重分类。含义分类与重走范
// 围由用户在 Ask 包中逐项确认，引擎只做机械接纳与机械执行——不自动扩大
// 失效范围，也不把 P2/P3 修订自行升级。所有写入经 Engine 的事务紧随其后
// 原子完成，本文件只做校验与 state 变换、不做 I/O。
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

// 需求修订来源与含义分类的封闭词汇（持久化契约的一部分）。INTAKE 是账本
// 初始账目专用值，不出现在决定包中。
const (
	// RequirementSourceIntake 标记受理回执回填的初始账目。
	RequirementSourceIntake = "INTAKE"
	RequirementSourceReset  = "RESET"
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
	case RequirementSourceUserInitiated, RequirementSourceReviewFindingP2, RequirementSourceReviewFindingP1, RequirementSourceReset:
		return true
	}
	return false
}

// validRequirementClassification 报告分类是否属于决定包可用的封闭两值。
func validRequirementClassification(classification string) bool {
	return classification == RequirementClassificationPreserved || classification == RequirementClassificationChanged
}

// ---- 事件 schema（无状态形状校验，由 Event.Validate 调用） ----

// validateRequirementPackage 校验变更包的形状：候选 revision、delta 指纹
// 与影响预览指纹非空；来源与分类封闭；发现项驱动的来源必携 finding
// digest，用户主动发起必空。来源↔分类↔真实裁决的对照与账目绑定在接纳
// 层执行。
func validateRequirementPackage(pkg *RequirementDecisionPackage) error {
	if strings.TrimSpace(pkg.CandidateRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the candidate revision binding"}
	}
	if strings.TrimSpace(pkg.DeltaDigest) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the review-delta digest binding"}
	}
	if strings.TrimSpace(pkg.ImpactDigest) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the impact-preview digest binding"}
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
	if pkg.Source != RequirementSourceUserInitiated && pkg.Source != RequirementSourceReset && !findingBound {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a finding-driven requirement decision must bind its finding digest"}
	}
	return validateWorkDecisionShape(pkg.WorkDecisions)
}

// validateRequirementDecisionPayload 校验 DECIDE 载荷的形状：包字段复用
// validateRequirementPackage，另加 CAS 基线（非空且≠候选）与决定理由非空。
func validateRequirementDecisionPayload(payload *RequirementDecisionPayload) error {
	if strings.TrimSpace(payload.BaseRevision) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires the base revision it is decided against"}
	}
	if payload.CandidateRevision == payload.BaseRevision && payload.Source != RequirementSourceReset {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision candidate revision must differ from its base"}
	}
	if strings.TrimSpace(payload.Reason) == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "requirement decision requires a non-empty reason"}
	}
	pkg := payload.packageView()
	return validateRequirementPackage(&pkg)
}

// ---- 权威 revision 读取与修订屏障 ----

// RequirementRevision reads the latest local confirmation. A child's initial
// authority is its inherited binding; its intake remains the original receipt.
func (s *State) RequirementRevision() string {
	if n := len(s.RequirementRevisions); n > 0 && (s.SliceChild == nil || n > 1) {
		return s.RequirementRevisions[n-1].Revision
	}
	return s.initialRequirementRevision()
}

func (s *State) initialRequirementRevision() string {
	if s.SliceChild != nil {
		return s.SliceChild.RequirementRevision
	}
	if s.IntakeConfirmationReceipt == nil {
		return ""
	}
	return s.IntakeConfirmationReceipt.RequirementRevision
}

// requirementRevisionAt keeps historical work bound to its local issue time.
func (s *State) requirementRevisionAt(stateRevision uint64) string {
	revision := s.initialRequirementRevision()
	for i, entry := range s.RequirementRevisions {
		if (s.SliceChild == nil || i > 0) && entry.StateRevision <= stateRevision {
			revision = entry.Revision
		}
	}
	return revision
}

func (s *State) requirementGeneration() uint64 {
	if len(s.RequirementRevisions) == 0 || s.SliceChild != nil && len(s.RequirementRevisions) == 1 {
		return 0
	}
	return s.RequirementRevisions[len(s.RequirementRevisions)-1].StateRevision
}

// bindRequirementPackage updates only the pending Ask. The barrier remains
// active; no revision, carry decision or task disposition is executed here.
func (e *Engine) bindRequirementPackage(state *State, ev Event, revision, nextRevision uint64) (Acceptance, *State, bool, error) {
	requestID := string(ev.Request.Request)
	ask, exists := state.PendingAsks[requestID]
	if !exists || ask.Control != ControlRequestRequirementChange {
		return Acceptance{}, nil, false, &RejectedError{Code: CodeUnknownRequest, Detail: "requirement package update must name an existing requirement Ask"}
	}
	if ask.Resolved {
		return Acceptance{}, nil, false, &RejectedError{Code: CodeRequestResolved, Detail: "requirement Ask is already resolved"}
	}
	if ev.Request.Token != freshnessToken(revision, ev.Request.Request) {
		return Acceptance{}, nil, false, &RejectedError{Code: CodeStaleFreshness, Detail: "requirement package update token is stale"}
	}
	if ask.BaseRevision != state.RequirementRevision() || ask.BaseGeneration != state.requirementGeneration() {
		return Acceptance{}, nil, false, &RejectedError{Code: CodeRequirementAskExpired, Detail: "requirement package update is based on a superseded confirmation"}
	}
	if err := e.validateRequirementWork(state, ev.Request.Package.WorkDecisions, true); err != nil {
		return Acceptance{}, nil, false, err
	}
	ask.Package = ev.Request.Package
	state.PendingAsks[requestID] = ask
	return Acceptance{EventID: string(ev.ID), Kind: string(ev.Kind), Status: "ACCEPTED", Revision: nextRevision,
		RequestID: requestID, FreshnessToken: freshnessToken(nextRevision, ev.Request.Request)}, state, true, nil
}

// requirementChangePending 报告修订屏障是否在位：存在未决的
// REQUEST_REQUIREMENT_CHANGE Ask。
func (s *State) requirementChangePending() bool {
	return s.requirementChangePendingExcept("")
}

// errWriteBarrier 是权威写入口共用的屏障闸判定（FA 单一判定函数）：屏
// 障在位时返回可区分拒绝，不在位返回 nil。一切 API 头部闸（签发、恢复、
// 对账、host intent）与提交路径的挂起判定共用 requirementChangePending
// 这一个事实源。
func (s *State) errWriteBarrier(entry string) error {
	if !s.requirementChangePending() {
		return nil
	}
	return &RejectedError{
		Code:   CodeChangeBarrierActive,
		Detail: fmt.Sprintf("%s is frozen while a requirement change ask is pending", entry),
	}
}

// requirementChangePendingExcept 报告除 requestID 外是否仍有未决的需求
// 变化 Ask：需求决定事务内以它判定屏障解除边界——只有最后一个未决 Ask
// 的解决才触发隔离账重分类，此前隔离继续持有。
func (s *State) requirementChangePendingExcept(requestID string) bool {
	if s.SliceChild != nil && s.SliceChild.ParentChange != nil && s.SliceChild.ParentChange.Pending {
		return true
	}
	if s.resetPending() {
		return true
	}
	for id, ask := range s.PendingAsks {
		if id != requestID && ask.Control == ControlRequestRequirementChange && !ask.Resolved {
			return true
		}
	}
	return false
}

// requirementChangeSourcePrefix 标识重走失效的授权来源（后接 requestID）。
const requirementChangeSourcePrefix = "requirement-change:"

// seriesInRedoStale 报告 series 是否负有用户确认重走后的未清偿新鲜轮
// 义务（FreshOwed——义务事实，不活在 effective 投影里，纠正不能解除）。
// 该态下证据只认绑当前权威 revision 且派发不早于 series 的 fresh floor，
// 新鲜轮完成接纳即清偿；不负义务的 series 按 carry 语义接纳已确认
// revision 的证据，但重走过的系列（FreshFloor 留存）另受"revision 下界
// ＝最后一次覆盖它的重走决定"约束（见 admitReviewEvidence）。
func seriesInRedoStale(series review.Series) bool {
	return series.FreshOwed
}

// ---- 修订屏障的单一处置表（§7.2.2 Phase A，S2） ----

// errPhaseAQuarantine 是处置表对 WORKER_RESULT 的哨兵：区别于 nil（照常
// 接纳）与拒绝错误，表示进入持久隔离账。
var errPhaseAQuarantine = errors.New("phase A quarantine")

// admitInCurrentState 是修订屏障在位时的单一处置表：一切 admit 分支与
// HostAction intent 落账都经它，不存在表外的屏障检查。
//
//	照常接纳：观察类（lifecycle/task-progress/operator observation）、在途
//	          回执（spawn/HostAction，可到 terminal）、Ask 创建（惰性，其
//	          DECIDE 另经表）、需求变化自身的 REQUEST/DECIDE、RESET/ABORT。
//	持久隔离：WORKER_RESULT（在途计算结果，屏障解除边界上原子重分类）。
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
// Status=QUARANTINED。摘要与正常接纳路径同源（resultPayloadDigest，不含
// 事件 ID）——重分类与事后重发都按结果级身份判幂等/冲突。重分类发生在
// 需求决定落账的同一事务内（replayQuarantinedResults），进程重启不丢失。
func (e *Engine) quarantineWorkerResult(state *State, ev Event, nextRevision uint64) (Acceptance, *State, bool, error) {
	state.QuarantinedResults[string(ev.ID)] = workerResultRecord(ev.Result)
	return Acceptance{
		EventID: string(ev.ID), Kind: string(ev.Kind), Status: "QUARANTINED",
		Revision: nextRevision, ActionID: ev.Result.ActionID,
	}, state, true, nil
}

// replayQuarantinedResults 在需求决定的屏障解除（最后一个未决 Ask 解决）
// 的同一事务内逐条原子重分类隔离账（§7.2.2 的恢复侧）：每条记录经与
// Submit 完全相同的接纳核心重放——未勾选 series 的审查证据按 carry 语义
// 携原 revision 接纳并留痕；顺序门未开的记录回暂存挂起（回执已在账，
// 开门迁移点补配对）；其余失败经快照恢复只落该条的 obsolete＋留痕（零
// 半套账目），其余记录照常继续。重放按 eventID 排序保持确定性；重放
// 内部的补位签发由 refill 的既有屏障扣发持住，决定提交后的统一 refill
// 补进。
func (e *Engine) replayQuarantinedResults(state *State, requestID string, nextRevision uint64, expectedFingerprint string) {
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
		snapshot, cloneErr := cloneState(state)
		var admitErr error
		if cloneErr == nil {
			_, _, admitErr = e.admitWorkerResultRecord(state, record.ActionID, workerResultPayloadOf(record), eventID, requestID, nextRevision, nextRevision, expectedFingerprint)
		}
		if cloneErr != nil || admitErr != nil {
			if snapshot != nil {
				*state = *snapshot
			}
			if isSequenceGate(admitErr) {
				// 顺序门未开：回暂存挂起（回执已在账），开门迁移点补配对
				//——不作废。
				state.StagedResults[record.ActionID] = record
				state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
					ActionID: record.ActionID,
					Detail:   "quarantined result returned to staging; pairing stays suspended until the product-review conclusion re-passes",
					Revision: nextRevision,
				})
				continue
			}
			state.archiveObsoleteResult(record)
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail: fmt.Sprintf("quarantined result could not be readmitted after the requirement decision %s: %v",
					requestID, errors.Join(cloneErr, admitErr)),
				Revision: nextRevision,
			})
			continue
		}
		if record.Review != nil && record.Review.RequirementRevision != state.RequirementRevision() {
			// carry 留痕携原 revision 与用户理由（理由落在确认账目条目上，
			// 放弃路径无账目即不携）。
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail: fmt.Sprintf("carried by requirement decision %s (%s): %s evidence bound to confirmed revision %q admitted unchanged",
					requestID, confirmedReasonOf(state, requestID), record.Review.Action, record.Review.RequirementRevision),
				Revision: nextRevision,
			})
		}
	}
}

// confirmedReasonOf 返回授权 requestID 的确认账目条目的用户理由（无该
// 条目——放弃路径——为空）。
func confirmedReasonOf(state *State, requestID string) string {
	for _, entry := range state.RequirementRevisions {
		if entry.RequestID == requestID {
			return entry.Reason
		}
	}
	return ""
}

// cloneState 深拷贝整个 state（重放的逐条原子性：单条失败恢复到该条重放
// 之前的完整快照）。state 本身就是 JSON 权威编码，往返即深拷贝；错误视
// 为不可重放。
func cloneState(state *State) (*State, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	out := &State{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, err
	}
	return out, nil
}

// retireReviewDispatch closes the superseded task. FreshOwed owns the new
// requirement's review obligation; normal issuance assigns its TaskKey, plan,
// and retry budget after every requirement-change barrier has cleared.
func (s *State) retireReviewDispatch(actionID string, mark review.Action, requestID string, nextRevision uint64) error {
	if retired, exists := s.ObsoleteActions[actionID]; exists {
		if ended, owed := s.EndedAttempts[retired.Task]; owed && ended.ActionID == actionID {
			delete(s.EndedAttempts, retired.Task)
			return s.terminalizeTask(retired.Task)
		}
		return nil
	}
	detail := fmt.Sprintf("the %s-series dispatch was terminated by the redo declared in requirement decision %s", mark, requestID)
	s.archiveDispatchResults(actionID)
	pending, known := s.PendingActions[actionID]
	if known {
		s.ObsoleteActions[actionID] = ObsoleteAction{ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID, Reason: detail}
	} else {
		pending, known = s.retireAction(actionID, "", detail)
	}
	if !known {
		return &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("review dispatch %q is unknown", actionID)}
	}
	if err := s.terminalizeTask(pending.Task); err != nil {
		return err
	}
	s.RecoveryRecords = append(s.RecoveryRecords, RecoveryRecord{
		ActionID: actionID, Task: pending.Task, AttemptID: pending.AttemptID,
		Action: RecoveryReconcile, RequestID: requestID, Detail: detail, Revision: nextRevision,
	})
	return nil
}

// ---- 决定的账目变换（DECIDE 接纳路径） ----

// admitRequirementDecision 在选项合法后执行需求变化控制的账目变换。
// choice id 封闭为 proceed/abort（固定项锚定）：abort 不得携载荷——放弃
// 即不登记；proceed 必携 typed 载荷且与 Ask 落账的确认包逐字段相等（Ask
// 即用户确认边界，包外内容不存在）。基线三方相等：载荷基线＝Ask 落账基
// 线＝当前权威 revision，任何不等即 Ask 过期（可区分拒绝、零状态、须在
// 新基线上重建）。前置校验全部通过后，同一事务内：登记 ConfirmedRevision
// → 机械执行 scope（两类分类一致：勾选的 series stale＋fresh floor、拓
// 扑/路线解绑、重走范围内已标记系列身份的派发退位）与逐项 WorkDecisions
// （REDO 保留旧证据并登记新任务义务；CARRY 精确绑定续用工作）→ 屏障解除
// 边界上的隔离账重分类（未勾选 series 携原
// revision carry）。
func (e *Engine) admitRequirementDecision(state *State, ask PendingAsk, requestID string, decide *DecidePayload, nextRevision uint64, expectedFingerprint string) error {
	if ask.Control != ControlRequestRequirementChange {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: fmt.Sprintf("control %s does not take a requirement decision payload", ask.Control)}
	}
	switch choice := string(decide.Choice); choice {
	case requirementAbortOption:
		if decide.Requirement != nil {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "aborting a requirement change cannot carry a decision payload"}
		}
		// 放弃：不登记 revision、不动 scope；最后一个未决 Ask 解决时隔离
		// 账按现行权威重放（应被纳），否则继续持有。
		return e.replayWhenBarrierLifts(state, requestID, nextRevision, expectedFingerprint)
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
	// DECIDE 载荷必须与 Ask 落账的确认包逐字段相等——同一 Ask 不可
	// 登记不同于用户所见所确认的变更内容。
	if ask.Package == nil {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "the requirement change ask does not carry the confirmed package"}
	}
	if !sameRequirementPackage(payload.packageView(), *ask.Package) {
		return reject(fmt.Sprintf("the decide payload does not match the package confirmed on ask %s (conflict)", requestID))
	}
	if state.IntakeReceipt == nil {
		return reject("requirement change requires the recorded intake receipt (the run has not started)")
	}
	// 基线三方相等：Ask 创建时的权威基线、载荷 CAS 基线与当前权威必须
	// 是同一 revision——任何后续确认都使旧 Ask 过期。
	if current := state.RequirementRevision(); ask.BaseRevision == "" || ask.BaseRevision != current || payload.BaseRevision != ask.BaseRevision || ask.BaseGeneration != state.requirementGeneration() {
		return &RejectedError{
			Code: CodeRequirementAskExpired,
			Detail: fmt.Sprintf("the requirement change ask %s is expired: it was raised at base %q, the decide carries %q, the current authoritative revision is %q; rebuild the ask on the current base",
				requestID, ask.BaseRevision, payload.BaseRevision, current),
		}
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
	if payload.Source == RequirementSourceReset && (ask.ResetRequestID == "" || payload.ResetRequestID != ask.ResetRequestID) {
		return reject("reset confirmation must bind the accepted reset request")
	}
	parentConfirmed := state.SliceChild != nil && state.SliceChild.ParentChange != nil && state.SliceChild.ParentChange.Confirmation != nil && state.SliceChild.ParentChange.Confirmation.RequestID == requestID && state.SliceChild.ParentChange.Confirmation.Revision == payload.CandidateRevision
	if payload.Source != RequirementSourceUserInitiated && payload.Source != RequirementSourceReset && !parentConfirmed {
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
	if err := e.validateRequirementWork(state, payload.WorkDecisions, false); err != nil {
		return err
	}
	if payload.Source == RequirementSourceReset {
		d := state.Decisions[payload.ResetRequestID]
		if d.Reset == nil || !d.Reset.Applied || d.Reset.Confirmed {
			return reject("reset confirmation has no pending applied authority")
		}
		d.Reset.Confirmed = true
		state.Decisions[d.RequestID] = d
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
		Coverage: payload.Coverage,
		Children: payload.Children, Impact: payload.Impact, ResetRequestID: payload.ResetRequestID,
		Revision:       payload.CandidateRevision,
		DeltaDigest:    payload.DeltaDigest,
		BaseRevision:   payload.BaseRevision,
		Source:         payload.Source,
		Classification: payload.Classification,
		DecidedScope:   scope,
		WorkDecisions:  slices.Clone(payload.WorkDecisions),
		RequestID:      requestID,
		Reason:         payload.Reason,
		StateRevision:  nextRevision,
	})
	if state.RequirementGuarantee != nil {
		state.RequirementGuarantee.RequirementRevision = payload.CandidateRevision
		state.RequirementGuarantee.ManifestDigest = ""
	}
	// 机械执行 scope：勾选的 series 调单 series stale 原语（不跨 series
	// 级联：未勾选即续用），fresh floor＝本次决定的 state 代数——重走态
	// 的新鲜轮只认不早于它的派发；勾选拓扑/路线即解绑，重新确认走既有
	// CONFIRM_TOPOLOGY/SELECT_ROUTE 路径。phase 与推进门不变。
	source := requirementChangeSourcePrefix + requestID
	if payload.RedoProductReview {
		state.ReviewSeries = review.StaleSeries(state.ReviewSeries, review.ProductReview, source, nextRevision)
	}
	if payload.RedoStartReadiness {
		state.ReviewSeries = review.StaleSeries(state.ReviewSeries, review.StartReadiness, source, nextRevision)
	}
	if payload.RedoTopology {
		state.Topology = nil
	}
	if payload.RedoRoute {
		// 解除的是绑定（RouteMode 清空，重新确认走既有 SELECT_ROUTE 路径），
		// 不清 SelectedGates：已生效义务在重选窗口内仍然在册，重选对它保持
		// add-only（validateRouteChange 的基线），已派发工作与波次不动。
		state.RouteMode = ""
	}
	// Issuance owns series identity. Select affected live dispatches from that
	// fact before archiving their results, including unvalidated quarantine data.
	inScope := func(action review.Action) bool {
		return (action == review.ProductReview && payload.RedoProductReview) ||
			(action == review.StartReadiness && payload.RedoStartReadiness)
	}
	marked := map[string]review.Action{}
	state.eachSeriesLiveDispatch(e.reviewSeriesOf, func(actionID string, mark review.Action) { marked[actionID] = mark })
	for _, attempt := range state.EndedAttempts {
		if mark := e.reviewSeriesOf(attempt.Task.Step); inScope(mark) {
			marked[attempt.ActionID] = mark
		}
	}
	pendingIDs := make([]string, 0, len(marked))
	for actionID := range marked {
		pendingIDs = append(pendingIDs, actionID)
	}
	slices.Sort(pendingIDs)
	for _, actionID := range pendingIDs {
		mark := marked[actionID]
		if !inScope(mark) || state.issuedRevisionOf(actionID) >= nextRevision {
			continue
		}
		if err := state.retireReviewDispatch(actionID, mark, requestID, nextRevision); err != nil {
			return err
		}
	}
	if err := e.applyRequirementWork(state, payload.WorkDecisions, requestID); err != nil {
		return err
	}
	// 屏障解除边界上的隔离账重分类。
	return e.replayWhenBarrierLifts(state, requestID, nextRevision, expectedFingerprint)
}

// replayWhenBarrierLifts 是隔离账重分类的屏障解除边界：本 Ask 解决后仍
// 有未决需求 Ask 时隔离继续持有（本次决定已生效的 scope 处置不受影响）
// 并留痕；这是最后一个未决 Ask 时在同一事务内逐条原子重放。
func (e *Engine) replayWhenBarrierLifts(state *State, requestID string, nextRevision uint64, expectedFingerprint string) error {
	if state.requirementChangePendingExcept(requestID) {
		state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
			Detail:   fmt.Sprintf("the quarantine ledger stays held: another requirement change ask is still pending after %s", requestID),
			Revision: nextRevision,
		})
		return nil
	}
	e.replayQuarantinedResults(state, requestID, nextRevision, expectedFingerprint)
	return nil
}

// ---- 账本完整性（加载/提交边界） ----

// verifyRequirementLedger preserves the original intake entry and validates
// each later local confirmation's base, scope and increasing state revision.
// A child starts its local base chain at the inherited binding, while its
// intake entry still matches its own original receipt and local receipt revision.
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
	if state.SliceChild != nil {
		previousRevision = state.SliceChild.RequirementRevision
	}
	previousState := intake.StateRevision
	for index, entry := range state.RequirementRevisions[1:] {
		if entry.BaseRevision != previousRevision {
			return fail("entry %d base revision %q breaks the confirmation chain (previous %q)", index+2, entry.BaseRevision, previousRevision)
		}
		if entry.Revision == "" || entry.Revision == entry.BaseRevision && entry.Source != RequirementSourceReset || entry.DeltaDigest == "" || entry.RequestID == "" {
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

// GuaranteeActivation is the finite requirement activation envelope. Validation
// owns all approved cases and execution records; they are never duplicated here.
type GuaranteeActivation struct {
	ActivationSource    string `json:"activationSource"`
	RequirementRevision string `json:"requirementRevision"`
	SolutionRevision    string `json:"solutionRevision"`
	ManifestDigest      string `json:"manifestDigest"`
}

func NewGuaranteeActivation(revision, solution string, document requirement.FrozenRequirementProjection) *GuaranteeActivation {
	return &GuaranteeActivation{ActivationSource: "EXPLICIT_REQUIREMENT_CONFIRMATION", RequirementRevision: revision, SolutionRevision: solution, ManifestDigest: digestOfCanonical(document.Requirements)}
}
func (s *State) GuaranteeActive() bool {
	return s.RequirementGuarantee != nil && s.RequirementGuarantee.RequirementRevision == s.RequirementRevision() && s.RequirementGuarantee.ManifestDigest != "" && s.RouteMode != "" && !s.routeHasNoQAModes() && !s.ValidationNotGuaranteed()
}
