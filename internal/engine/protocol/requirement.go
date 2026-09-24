// requirement.go 是用户需求变化（总需求 §6.3.2、§7.1、§7.2）在提交协议层
// 的嫁接点：REQUEST_REQUIREMENT_CHANGE 的两段 Ask（Ask 即用户确认边界，
// 持久化 typed 决定包）、确认 revision 账本的登记与完整性校验、修订屏障
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
	return s.requirementChangePendingExcept("")
}

// requirementChangePendingExcept 报告除 requestID 外是否仍有未决的需求
// 变化 Ask：需求决定事务内以它判定屏障解除边界——只有最后一个未决 Ask
// 的解决才触发隔离账重分类，此前隔离继续持有。
func (s *State) requirementChangePendingExcept(requestID string) bool {
	for id, ask := range s.PendingAsks {
		if id != requestID && ask.Control == ControlRequestRequirementChange && !ask.Resolved {
			return true
		}
	}
	return false
}

// requirementChangeSourcePrefix 是重走失效来源的前缀（后接授权它的
// requestID）；seriesInRedoStale 以它识别重走态。
const requirementChangeSourcePrefix = "requirement-change:"

// seriesInRedoStale 报告 series 是否处于用户确认重走后的待新鲜轮态
// （effective PENDING 且失效来源为某次需求决定）。该态下证据只认绑当前
// 权威 revision 且派发不早于 series 的 fresh floor；未处于该态的 series
// 一律按 carry 语义接纳已确认 revision 的证据。
func seriesInRedoStale(series review.Series) bool {
	return series.Effective.Status == "PENDING" && strings.HasPrefix(series.Effective.Source, requirementChangeSourcePrefix)
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
// 携原 revision 接纳并留痕；单条失败经快照恢复只落该条的 obsolete＋留痕
// （零半套账目），其余记录照常继续。重放按 eventID 排序保持确定性；重放
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
			_, _, admitErr = e.admitWorkerResultRecord(state, record.ActionID, workerResultPayloadOf(record), eventID, nextRevision, nextRevision, expectedFingerprint)
		}
		if cloneErr != nil || admitErr != nil {
			if snapshot != nil {
				*state = *snapshot
			}
			state.ObsoleteResults[record.ActionID] = record
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail: fmt.Sprintf("quarantined result could not be readmitted after the requirement decision %s: %v",
					requestID, errors.Join(cloneErr, admitErr)),
				Revision: nextRevision,
			})
			continue
		}
		if record.Review != nil && record.Review.RequirementRevision != state.RequirementRevision() {
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
				ActionID: record.ActionID,
				Detail: fmt.Sprintf("carried by requirement decision %s: %s evidence bound to confirmed revision %q admitted unchanged",
					requestID, record.Review.Action, record.Review.RequirementRevision),
				Revision: nextRevision,
			})
		}
	}
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

// replaceDispatch 退位在途派发并由同任务的新派发替换（签发代数＝
// nextRevision，满足重走 series 的 fresh floor，系列标记随步继承）。派发
// 不在途时零变化返回 ok=false；重签发失败（结构性不可达）返回 err——
// 派发已退位，由调用方决定处置。
func (e *Engine) replaceDispatch(state *State, actionID, reason string, nextRevision uint64, expectedFingerprint string) (string, bool, error) {
	pending, ok := state.PendingActions[actionID]
	if !ok {
		return "", false, nil
	}
	attempt := state.Attempts[pending.Task]
	retired, _ := state.retireAction(actionID, "", reason)
	bindings := AttemptBindings{Task: retired.Task, Snapshot: expectedFingerprint, Responsibility: state.RunProvider}
	replacement, err := state.installReplacement(retired, nextRevision, bindings, attempt.Plan, attempt.Attempts, attempt.MaxAttempts)
	if err != nil {
		return "", true, err
	}
	obsolete := state.ObsoleteActions[actionID]
	obsolete.ReplacedBy = replacement.ActionID
	state.ObsoleteActions[actionID] = obsolete
	return replacement.ActionID, true, nil
}

// terminateDeclaredDispatch 机械终止一个证据已声明重走系列身份的在途派
// 发（§7.2.2:4 Phase B 的 terminate/stale）：结果判不可纳、入迟到证据账
// （obsolete）＋留痕；派发经 replaceDispatch 替换，派发已不在（任务已到
// 终态）时只落迟到证据与留痕。
func (e *Engine) terminateDeclaredDispatch(state *State, actionID string, record WorkerResult, requestID string, nextRevision uint64, expectedFingerprint string) {
	state.ObsoleteResults[actionID] = record
	detail := fmt.Sprintf("the %s series was redone by requirement decision %s; the dispatch was terminated by declaration and its evidence is superseded",
		evidenceActionName(record.Review), requestID)
	if replacement, inFlight, err := e.replaceDispatch(state, actionID, "superseded by the confirmed requirement redo", nextRevision, expectedFingerprint); inFlight && err == nil {
		detail = fmt.Sprintf("%s; replacement dispatch %s", detail, replacement)
	}
	state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
		ActionID: actionID, Action: RecoveryNewAttempt, Detail: detail, Revision: nextRevision,
	})
}

// terminateMarkedDispatch 终止一个签发时已标记重走系列身份的在途派发
// （无证据可作废）：退位＋同任务重签发＋留痕。决定事务内使用——重签发
// 失败只留痕不否决决定（确认登记优先，容量补位由后续 refill 兜住）。
func (e *Engine) terminateMarkedDispatch(state *State, actionID string, mark review.Action, requestID string, nextRevision uint64, expectedFingerprint string) {
	detail := fmt.Sprintf("the %s-series dispatch was terminated by the redo declared in requirement decision %s", mark, requestID)
	replacement, inFlight, err := e.replaceDispatch(state, actionID, "superseded by the confirmed requirement redo", nextRevision, expectedFingerprint)
	switch {
	case !inFlight:
		return
	case err != nil:
		detail = fmt.Sprintf("%s; the replacement dispatch could not be installed: %v", detail, err)
	default:
		detail = fmt.Sprintf("%s; replacement dispatch %s", detail, replacement)
	}
	state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{
		ActionID: actionID, Action: RecoveryNewAttempt, Detail: detail, Revision: nextRevision,
	})
}

// evidenceActionName 返回证据声明的审查动作名（无审查证据即非审查派发）。
func evidenceActionName(evidence *ReviewResultEvidence) string {
	if evidence == nil {
		return "non-review"
	}
	return string(evidence.Action)
}

// ---- 决定的账目变换（DECIDE 接纳路径） ----

// admitRequirementDecision 在选项合法后执行需求变化控制的账目变换。
// choice id 封闭为 proceed/abort（固定项锚定）：abort 不得携载荷——放弃
// 即不登记；proceed 必携 typed 载荷且与 Ask 落账的确认包逐字段相等（Ask
// 即用户确认边界，包外内容不存在）。基线三方相等：载荷基线＝Ask 落账基
// 线＝当前权威 revision，任何不等即 Ask 过期（可区分拒绝、零状态、须在
// 新基线上重建）。前置校验全部通过后，同一事务内：登记 ConfirmedRevision
// → 机械执行 scope（两类分类一致：勾选的 series stale＋fresh floor、拓
// 扑/路线解绑、重走范围内已声明系列身份的在途派发按声明终止）→ 屏障解
// 除边界上的隔离账重分类（未勾选 series 携原 revision carry）。
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
	if payload.packageView() != *ask.Package {
		return reject(fmt.Sprintf("the decide payload does not match the package confirmed on ask %s (conflict)", requestID))
	}
	if state.IntakeReceipt == nil {
		return reject("requirement change requires the recorded intake receipt (the run has not started)")
	}
	// 基线三方相等：Ask 创建时的权威基线、载荷 CAS 基线与当前权威必须
	// 是同一 revision——任何后续确认都使旧 Ask 过期。
	if current := state.RequirementRevision(); ask.BaseRevision == "" || ask.BaseRevision != current || payload.BaseRevision != ask.BaseRevision {
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
		state.RouteMode = ""
		state.SelectedGates = nil
	}
	// Phase B 的在途处置（§7.2.2:4）：重走范围内的在途派发按声明终止
	//——已声明系列身份的（暂存或隔离证据携带 action↔series 绑定）与签发
	// 时已标记系列身份的（定义断言，无需等证据）；两者皆无的在途派发由
	// 接纳核心的 floor 比对＋按声明终止兜底。未勾选的 series 一律续用：
	// 暂存保留（迟到回执照常配对）、隔离结果携原 revision carry。
	inScope := func(action review.Action) bool {
		return (action == review.ProductReview && payload.RedoProductReview) ||
			(action == review.StartReadiness && payload.RedoStartReadiness)
	}
	declaredInScope := func(evidence *ReviewResultEvidence) bool {
		return evidence != nil && inScope(evidence.Action)
	}
	stagedIDs := make([]string, 0, len(state.StagedResults))
	for actionID := range state.StagedResults {
		stagedIDs = append(stagedIDs, actionID)
	}
	slices.Sort(stagedIDs)
	for _, actionID := range stagedIDs {
		if staged := state.StagedResults[actionID]; declaredInScope(staged.Review) {
			delete(state.StagedResults, actionID)
			e.terminateDeclaredDispatch(state, actionID, staged, requestID, nextRevision, expectedFingerprint)
		}
	}
	quarantinedIDs := make([]string, 0, len(state.QuarantinedResults))
	for eventID := range state.QuarantinedResults {
		quarantinedIDs = append(quarantinedIDs, eventID)
	}
	slices.Sort(quarantinedIDs)
	for _, eventID := range quarantinedIDs {
		if record := state.QuarantinedResults[eventID]; declaredInScope(record.Review) {
			delete(state.QuarantinedResults, eventID)
			e.terminateDeclaredDispatch(state, record.ActionID, record, requestID, nextRevision, expectedFingerprint)
		}
	}
	// 签发时已标记系列身份的在途派发（此前两轮处置已退位其证据已声明的
	// 条目）：按标记主动终止，无需等任何证据到达。只终止决定前签发的派
	// 发——本事务内刚装上的替换派发（签发代数＝本次决定）就是重走要的
	// 新鲜派发，不在终止之列。
	pendingIDs := make([]string, 0, len(state.PendingActions))
	for actionID := range state.PendingActions {
		pendingIDs = append(pendingIDs, actionID)
	}
	slices.Sort(pendingIDs)
	for _, actionID := range pendingIDs {
		mark := state.PendingActions[actionID].ReviewSeries
		if !inScope(mark) || state.issuedRevisionOf(actionID) >= nextRevision {
			continue
		}
		e.terminateMarkedDispatch(state, actionID, mark, requestID, nextRevision, expectedFingerprint)
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
