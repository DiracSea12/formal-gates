package review

import (
	"fmt"
	"slices"
	"strings"
)

// AppendAuthorization 追加一条用户请求的额外语义轮授权，并把它绑定到首个
// dispatch。授权身份由 (runID, action, reason, 序号) 派生；是否越过自动上限
// 才允许授权，由调用方的工作流政策决定，本函数只做账目变换。存在未占用的
// 授权时拒绝——用户每次只授权恰好一轮，不得预先累计。
func AppendAuthorization(set Set, runID string, action Action, reason, dispatchID string) (Set, error) {
	if err := Validate(runID, set); err != nil {
		return Set{}, err
	}
	series, err := set.seriesFor(action)
	if err != nil {
		return Set{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Set{}, fmt.Errorf("review: %s extra round authorization requires a non-empty reason", action)
	}
	if strings.TrimSpace(dispatchID) == "" {
		return Set{}, fmt.Errorf("review: %s extra round authorization requires a dispatch binding", action)
	}
	if _, ok := availableAuthorization(*series); ok {
		return Set{}, fmt.Errorf("review: %s has an unconsumed extra-round authorization; consume the granted round before requesting another", action)
	}
	authorization := RoundAuthorization{
		ID:                    "review-authorization-" + shortDigest(authorizationDigest(runID, action, reason, len(series.Authorizations)+1)),
		Action:                action,
		Source:                authorizationUserSource,
		Reason:                reason,
		DispatchIDs:           []string{dispatchID},
		RecordedResultDigests: []string{},
	}
	series.Authorizations = append(slices.Clone(series.Authorizations), authorization)
	refresh(series)
	return set, nil
}

// BindDispatch 把一个 dispatch 追加到第一条未被有效结果占用的授权上（运行
// 期重试共用同一授权）；没有空槽授权时拒绝。
func BindDispatch(set Set, runID string, action Action, dispatchID string) (Set, error) {
	if err := Validate(runID, set); err != nil {
		return Set{}, err
	}
	series, err := set.seriesFor(action)
	if err != nil {
		return Set{}, err
	}
	if strings.TrimSpace(dispatchID) == "" {
		return Set{}, fmt.Errorf("review: dispatch binding for %s requires a dispatch id", action)
	}
	index, ok := availableAuthorization(*series)
	if !ok {
		return Set{}, fmt.Errorf("review: %s has no unoccupied extra-round authorization for dispatch %s", action, dispatchID)
	}
	series.Authorizations = slices.Clone(series.Authorizations)
	authorization := series.Authorizations[index]
	authorization.DispatchIDs = appendUnique(slices.Clone(authorization.DispatchIDs), dispatchID)
	series.Authorizations[index] = authorization
	refresh(series)
	return set, nil
}

// RecordCandidate 接纳一条完整、有效的语义审查结果：构造 finding 身份→查
// 重→授权绑定（越过自动上限的结果必须携带绑定其 dispatch 的有效授权）→
// 追加不可变候选→把 effective 重投影到新候选→刷新容量。RUNTIME_ERROR 等
// 非语义状态在此即被拒绝，根本不建账。
func RecordCandidate(set Set, runID string, action Action, dispatch Dispatch, result Result, verification OperatorVerification) (Set, EffectiveStatus, error) {
	series, err := set.seriesFor(action)
	if err != nil {
		return Set{}, EffectiveStatus{}, err
	}
	if result.Status != statusPass && result.Status != statusFail {
		return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result status %q cannot become a review candidate (only PASS or FAIL)", action, result.Status)
	}
	verification, err = normalizeOperatorVerification(verification)
	if err != nil {
		return Set{}, EffectiveStatus{}, err
	}
	if err := Validate(runID, set); err != nil {
		return Set{}, EffectiveStatus{}, err
	}
	if strings.TrimSpace(dispatch.ID) == "" {
		return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s candidate requires a dispatch binding", action)
	}
	if strings.TrimSpace(dispatch.RequirementRevision) == "" {
		return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s candidate requires a requirement revision", action)
	}
	findings := make([]Finding, 0, len(result.Findings))
	findingIDs := map[string]bool{}
	for _, raw := range result.Findings {
		if raw.Message == "" || !validSeverity(raw.Severity) {
			return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result finding %q is malformed: severity must be P0-P3 and message non-empty", action, raw.Message)
		}
		finding := raw
		finding.Digest = findingDigest(finding)
		finding.ID = "finding-" + shortDigest(finding.Digest)
		if findingIDs[finding.ID] {
			return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result repeats finding identity %s for identical content", action, finding.ID)
		}
		findingIDs[finding.ID] = true
		findings = append(findings, finding)
	}
	resultDigest := candidateDigest(runID, RawCandidate{
		Action: action, DispatchID: dispatch.ID, RequirementRevision: dispatch.RequirementRevision,
		RawStatus: result.Status, Message: result.Message, Findings: findings,
	})
	for _, existing := range series.RawCandidates {
		if existing.ResultDigest == resultDigest {
			return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result %s is already recorded", action, resultDigest)
		}
	}
	authorizationID := dispatch.AuthorizationID
	if series.Completed >= series.AutomaticLimit && authorizationID == "" {
		return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result exceeds the automatic round limit without an action-specific authorization", action)
	}
	if authorizationID != "" {
		index := slices.IndexFunc(series.Authorizations, func(a RoundAuthorization) bool { return a.ID == authorizationID })
		if index < 0 || !slices.Contains(series.Authorizations[index].DispatchIDs, dispatch.ID) {
			return Set{}, EffectiveStatus{}, fmt.Errorf("review: %s result is not bound to a valid review authorization", action)
		}
		for _, recorded := range series.Authorizations[index].RecordedResultDigests {
			if !resultExcluded(*series, recorded) {
				return Set{}, EffectiveStatus{}, fmt.Errorf("review: authorization %s already endorses a valid result", authorizationID)
			}
		}
		series.Authorizations = slices.Clone(series.Authorizations)
		authorization := series.Authorizations[index]
		authorization.RecordedResultDigests = appendUnique(slices.Clone(authorization.RecordedResultDigests), resultDigest)
		series.Authorizations[index] = authorization
	}
	previousEffective := series.Effective
	candidate := RawCandidate{
		ID:                   "review-result-" + shortDigest(resultDigest),
		Action:               action,
		DispatchID:           dispatch.ID,
		ResultDigest:         resultDigest,
		RequirementRevision:  dispatch.RequirementRevision,
		RawStatus:            result.Status,
		Message:              result.Message,
		Findings:             findings,
		Completeness:         completenessComplete,
		AuthorizationID:      authorizationID,
		OperatorVerification: verification,
	}
	series.RawCandidates = append(slices.Clone(series.RawCandidates), candidate)
	// 新一轮审查重建本 series 自己的结论：上游失效标记随之清除。
	series.UpstreamVoid = false
	series.Effective = Effective(*series, candidate, series.Effective.Revision+1)
	refresh(series)
	carryDownstreamImpact(series, action, previousEffective)
	return set, series.Effective, nil
}

// ApplyCorrection 施加一条用户纠正：先做 CAS 与精确绑定校验，再按决定分派
// ——无先例时写首裁 Adjudication，有先例时必须精确 supersede 当前最新决定
// 并写 CorrectionReceipt，INVALIDATE_RESULT 把整轮结果判为失效。product-
// review 的 effective 由 PASS 变阻塞时，start-readiness 的投影被级联刷为
// PENDING；start-readiness 自身的变化不触碰 product-review。
func ApplyCorrection(set Set, runID, requirementRevision string, input CorrectionInput) (Set, error) {
	input.Decision = strings.ToUpper(strings.TrimSpace(input.Decision))
	input.Severity = strings.ToUpper(strings.TrimSpace(input.Severity))
	input.SeverityRationale = strings.TrimSpace(input.SeverityRationale)
	input.Invalidity = strings.ToUpper(strings.TrimSpace(input.Invalidity))
	input.DispatchID = strings.TrimSpace(input.DispatchID)
	input.ResultDigest = strings.TrimSpace(input.ResultDigest)
	input.RequirementRevision = strings.TrimSpace(input.RequirementRevision)
	input.FindingID = strings.TrimSpace(input.FindingID)
	input.Supersedes = strings.TrimSpace(input.Supersedes)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Source = strings.TrimSpace(input.Source)
	series, err := set.seriesFor(input.Action)
	if err != nil {
		return Set{}, err
	}
	if !input.UserAuthorized {
		return Set{}, fmt.Errorf("review: correction requires explicit user authorization")
	}
	if input.Reason == "" || input.Source == "" {
		return Set{}, fmt.Errorf("review: correction requires a non-empty reason and source")
	}
	if err := Validate(runID, set); err != nil {
		return Set{}, err
	}
	if input.ExpectedEffectiveRevision != series.Effective.Revision {
		return Set{}, fmt.Errorf("review: correction conflict on %s: expected effective revision %d, current revision is %d", input.Action, input.ExpectedEffectiveRevision, series.Effective.Revision)
	}
	candidate, ok := candidateByDigest(*series, input.ResultDigest)
	if !ok || candidate.DispatchID != input.DispatchID || candidate.RequirementRevision != input.RequirementRevision {
		return Set{}, fmt.Errorf("review: correction binding does not match the recorded candidate")
	}
	// 决定账本切片先克隆到本系列再动笔（值进值出）。
	series.Adjudications = slices.Clone(series.Adjudications)
	series.Corrections = slices.Clone(series.Corrections)
	previousStatus := series.Effective.Status
	previousImpact := append([]string{}, series.Effective.DownstreamImpact...)
	nextRevision := series.Effective.Revision + 1
	if input.Decision == decisionInvalidateResult {
		receipt, invalidatesEffective, err := invalidateResult(series, runID, candidate, input, nextRevision)
		if err != nil {
			return Set{}, err
		}
		if input.Action == ProductReview && invalidatesEffective && previousStatus == statusPass {
			markUpstreamStale(&set, receipt.ID)
		}
		return set, nil
	}
	if err := correctFinding(series, runID, requirementRevision, candidate, input, previousStatus, previousImpact, nextRevision); err != nil {
		return Set{}, err
	}
	if input.Action == ProductReview && previousStatus == statusPass && series.Effective.Status == statusFail {
		markUpstreamStale(&set, series.Effective.Source)
		return set, nil
	}
	// 上游失效可逆：产品审经纠正重投影回 PASS 且再无已确认阻塞时，同一
	// 事务内解除 start-readiness 的 UpstreamVoid 并恢复其失效前结论原文
	//——不为被驳回/降级的 finding 让下游多耗一轮（§7.3.3）。
	if input.Action == ProductReview && series.Effective.Status == statusPass && !HasConfirmedBlocking(*series) {
		ReleaseUpstreamVoid(&set, nextRevision)
	}
	return set, nil
}

// invalidateResult 处理整轮失效：目标可为任意未被排除的候选；命中当前
// effective 候选时投影回到 PENDING（保留指向证据的 digest），否则只推进投
// 影代数。变更就地写入 series，返回回执与是否命中当前投影。
func invalidateResult(series *Series, runID string, candidate RawCandidate, input CorrectionInput, nextRevision int) (CorrectionReceipt, bool, error) {
	invalidatesEffective := candidate.ResultDigest == series.Effective.ResultDigest
	if input.FindingID != "" {
		return CorrectionReceipt{}, false, fmt.Errorf("review: whole-result invalidation does not take a finding id")
	}
	if input.Severity != "" || input.SeverityRationale != "" || input.Supersedes != "" {
		return CorrectionReceipt{}, false, fmt.Errorf("review: whole-result invalidation does not take severity or supersedes")
	}
	if !validInvalidity(input.Invalidity) {
		return CorrectionReceipt{}, false, fmt.Errorf("review: whole-result invalidation must classify invalidity as INVALID, INCOMPLETE, STALE, or MISBOUND")
	}
	if resultExcluded(*series, candidate.ResultDigest) {
		return CorrectionReceipt{}, false, fmt.Errorf("review: result %s is already invalidated", candidate.ResultDigest)
	}
	receipt := newCorrectionReceipt(runID, candidate, input, nextRevision, "VALID_RESULT:"+candidate.RawStatus, "INVALID_RESULT:"+input.Invalidity)
	series.Corrections = append(series.Corrections, receipt)
	if invalidatesEffective {
		series.Effective = EffectiveStatus{
			Status:              statusPending,
			RawStatus:           candidate.RawStatus,
			ResultDigest:        candidate.ResultDigest,
			RequirementRevision: candidate.RequirementRevision,
			Source:              receipt.ID,
			Revision:            nextRevision,
			BlockingFindingIDs:  []string{},
			AdvisoryFindingIDs:  []string{},
			DownstreamImpact:    downstreamImpact(input.Action),
		}
	} else {
		series.Effective.Revision = nextRevision
	}
	refresh(series)
	return receipt, invalidatesEffective, nil
}

// correctFinding 处理 finding 级纠正：目标必须是当前 effective 候选且其需
// 求 revision 等于当前需求 revision；首裁不得带 supersedes，后续纠正必须精
// 确命中当前最新决定。变更就地写入 series。
func correctFinding(series *Series, runID, requirementRevision string, candidate RawCandidate, input CorrectionInput, previousStatus string, previousImpact []string, nextRevision int) error {
	if candidate.ResultDigest != series.Effective.ResultDigest || candidate.RequirementRevision != requirementRevision {
		return fmt.Errorf("review: correction is stale: the target must be the current effective result at the current requirement revision")
	}
	if resultExcluded(*series, candidate.ResultDigest) {
		return fmt.Errorf("review: result %s was invalidated as a whole; its findings can no longer be corrected", candidate.ResultDigest)
	}
	finding, ok := findingByID(candidate, input.FindingID)
	if !ok {
		return fmt.Errorf("review: correction finding %q is not part of the bound result", input.FindingID)
	}
	if !validFindingDecision(input.Decision) {
		return fmt.Errorf("review: correction decision must be CONFIRM, DISMISS, RESEVERITIZE, INVALIDATE_FINDING, or INVALIDATE_RESULT")
	}
	if input.Invalidity != "" {
		return fmt.Errorf("review: finding correction does not take whole-result invalidity")
	}
	if input.Decision == decisionReseveritize && !validSeverity(input.Severity) {
		return fmt.Errorf("review: RESEVERITIZE requires a severity of P0, P1, P2, or P3")
	}
	if input.Decision != decisionReseveritize && input.Severity != "" {
		return fmt.Errorf("review: severity is accepted only for RESEVERITIZE")
	}
	if input.Decision == decisionReseveritize && blockingSeverity(input.Severity) && placeholderRationale(input.SeverityRationale) {
		return fmt.Errorf("review: re-severity to %s requires an independent non-placeholder severity rationale", input.Severity)
	}
	if input.Decision != decisionReseveritize && input.SeverityRationale != "" {
		return fmt.Errorf("review: severity rationale is accepted only for RESEVERITIZE")
	}
	latestID := LatestDecisionID(*series, candidate.ResultDigest, finding.ID)
	if latestID == "" {
		if input.Supersedes != "" {
			return fmt.Errorf("review: correction cannot supersede %q: the finding has no prior adjudication", input.Supersedes)
		}
		series.Adjudications = append(series.Adjudications, newAdjudication(runID, candidate, finding, input, nextRevision))
	} else {
		if input.Supersedes != latestID {
			return fmt.Errorf("review: correction conflict: supersedes must name the current decision %s", latestID)
		}
		previousDecision, previousSeverity, _ := EffectiveFindingDecision(*series, candidate.ResultDigest, finding)
		series.Corrections = append(series.Corrections, newCorrectionReceipt(
			runID, candidate, input, nextRevision,
			decisionValue(previousDecision, previousSeverity),
			decisionValue(input.Decision, correctedSeverity(input, previousSeverity)),
		))
	}
	series.Effective = Effective(*series, candidate, nextRevision)
	if series.UpstreamVoid {
		holdUpstreamVoid(series)
	} else {
		carryDownstreamImpact(series, input.Action, EffectiveStatus{Status: previousStatus, DownstreamImpact: previousImpact})
	}
	refresh(series)
	return nil
}

// MarkStale 在外部失效（需求 revision 变化、impact invalidation、carry、
// adopt-external、普通结果失效）后把双 series 的 effective 刷为 PENDING：
// 记录来源、revision+1、清空 finding 投影、各自打下游影响标。四本账与计轮
// 不动；显式 workflow reset 或新 run 才重建额度。
func MarkStale(set Set, source string) Set {
	for _, series := range []*Series{&set.ProductReview, &set.StartReadiness} {
		series.Effective.Status = statusPending
		series.Effective.Source = source
		series.Effective.Revision++
		series.Effective.BlockingFindingIDs = []string{}
		series.Effective.AdvisoryFindingIDs = []string{}
		series.Effective.DownstreamImpact = downstreamImpact(series.Action)
	}
	return set
}

// StaleSeries 在用户确认的需求修订重走范围内把单个动作的 series 刷为
// PENDING：与 MarkStale 同形（来源、revision+1、投影清空、该动作的下游
// 影响表打标），四本账与计轮不动，并落 freshFloor＝本次决定的 state 代数
// ——重走态下的新鲜轮只认签发不早于该代数的派发。不做跨 series 级联
// ——用户未勾选重走的 series 即明确续用其结论（§7.2.3/§7.2.4"不勾不
// 失效"），其 PASS 由协议层的 carry 判定继续有效；上游结论被同 revision
// 纠正推翻的依赖语义（markUpstreamStale）只属于纠正路径。
func StaleSeries(set Set, action Action, source string, freshFloor uint64) Set {
	series, err := set.seriesFor(action)
	if err != nil {
		return set
	}
	series.Effective.Status = statusPending
	series.Effective.Source = source
	series.Effective.Revision++
	series.Effective.BlockingFindingIDs = []string{}
	series.Effective.AdvisoryFindingIDs = []string{}
	series.Effective.DownstreamImpact = downstreamImpact(action)
	series.FreshFloor = freshFloor
	series.FreshOwed = true
	return set
}

// ReleaseUpstreamVoid 在上游阻塞被清除（上游系列重投影回 PASS 且无其他
// 已确认阻塞）的同一纠正事务内解除下游的 UpstreamVoid：恢复的是失效前
// 的 effective 结论原文（Revision 取当前投影代数保持单调），它从未被新
// 证据推翻，只是被上游冻结（§7.3.3"只清除错误阻塞时立即开放下一合法节
// 点"）。无快照时不动作。
func ReleaseUpstreamVoid(set *Set, nextRevision int) {
	series := &set.StartReadiness
	if !series.UpstreamVoid || series.VoidedEffective == nil {
		return
	}
	restored := *series.VoidedEffective
	restored.Revision = nextRevision
	series.Effective = restored
	series.UpstreamVoid = false
	series.VoidedEffective = nil
	refresh(series)
}

// DischargeFreshOwed 清偿 series 的新鲜轮义务——满足 floor 的新鲜轮完成
// 接纳后由协议层调用，是 FreshOwed 的唯一清除点；纠正路径永不触碰。
func DischargeFreshOwed(set Set, action Action) Set {
	series, err := set.seriesFor(action)
	if err != nil {
		return set
	}
	series.FreshOwed = false
	return set
}

// markUpstreamStale 在 product-review 的纠正把此前 PASS 的结论变为阻塞时，
// 把 start-readiness 的 effective 刷为 PENDING：其 PASS 依赖的上游结论已不
// 成立。start-readiness 的证据账本与计轮保持不动。UpstreamVoid 随之置位：
// 在新一轮 start-readiness candidate 重建自身结论前，任何重投影都不得把
// effective 复活为 PASS。
func markUpstreamStale(set *Set, source string) {
	series := &set.StartReadiness
	snapshot := series.Effective
	series.VoidedEffective = &snapshot
	series.UpstreamVoid = true
	series.Effective.Status = statusPending
	series.Effective.Source = "UPSTREAM_CORRECTION:" + source
	series.Effective.Revision++
	series.Effective.BlockingFindingIDs = []string{}
	series.Effective.AdvisoryFindingIDs = []string{}
	series.Effective.DownstreamImpact = downstreamImpact(StartReadiness)
}

// holdUpstreamVoid 把重投影后的 effective 压回上游失效形态：结论保持
// PENDING、digest 保留指向证据、投影清空、下游影响保持该动作的表——
// 重投影只推进 revision 与 source。
func holdUpstreamVoid(series *Series) {
	series.Effective.Status = statusPending
	series.Effective.BlockingFindingIDs = []string{}
	series.Effective.AdvisoryFindingIDs = []string{}
	series.Effective.DownstreamImpact = downstreamImpact(series.Action)
}

// carryDownstreamImpact 延续 FAIL 态的下游证据标记：PASS→FAIL 打满该动作的
// 下游表；FAIL→FAIL 保留旧表；其余情况投影本身就是空表。PENDING→FAIL 属
// 于首次开阻塞，不携带历史标记。
func carryDownstreamImpact(series *Series, action Action, previous EffectiveStatus) {
	if previous.Status == statusPass && series.Effective.Status == statusFail {
		series.Effective.DownstreamImpact = downstreamImpact(action)
		return
	}
	if previous.Status == statusFail && series.Effective.Status == statusFail && len(previous.DownstreamImpact) != 0 {
		series.Effective.DownstreamImpact = append([]string{}, previous.DownstreamImpact...)
	}
}

// newAdjudication 构造首裁记录；身份摘要覆盖除 ID 外的全部字段。
func newAdjudication(runID string, candidate RawCandidate, finding Finding, input CorrectionInput, revision int) Adjudication {
	adjudication := Adjudication{
		RunID:               runID,
		Action:              input.Action,
		DispatchID:          candidate.DispatchID,
		ResultDigest:        candidate.ResultDigest,
		RequirementRevision: candidate.RequirementRevision,
		FindingID:           finding.ID,
		Decision:            input.Decision,
		Severity:            input.Severity,
		SeverityRationale:   input.SeverityRationale,
		OldValue:            decisionValue("", finding.Severity),
		NewValue:            decisionValue(input.Decision, correctedSeverity(input, finding.Severity)),
		Reason:              input.Reason,
		Source:              input.Source,
		UserAuthorized:      input.UserAuthorized,
		EffectiveRevision:   revision,
	}
	adjudication.ID = "review-adjudication-" + shortDigest(digestValue(adjudication))
	return adjudication
}

// newCorrectionReceipt 构造纠正回执；身份摘要覆盖除 ID 外的全部字段。
func newCorrectionReceipt(runID string, candidate RawCandidate, input CorrectionInput, revision int, oldValue, newValue string) CorrectionReceipt {
	receipt := CorrectionReceipt{
		RunID:               runID,
		Action:              input.Action,
		DispatchID:          candidate.DispatchID,
		ResultDigest:        candidate.ResultDigest,
		RequirementRevision: candidate.RequirementRevision,
		FindingID:           input.FindingID,
		Decision:            input.Decision,
		Severity:            input.Severity,
		SeverityRationale:   input.SeverityRationale,
		Invalidity:          input.Invalidity,
		Supersedes:          input.Supersedes,
		OldValue:            oldValue,
		NewValue:            newValue,
		Reason:              input.Reason,
		Source:              input.Source,
		UserAuthorized:      input.UserAuthorized,
		EffectiveRevision:   revision,
	}
	receipt.ID = "review-correction-" + shortDigest(digestValue(receipt))
	return receipt
}
