package review

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/encoder"
)

// ---- 容量与轮数投影 ----

// completedRounds 从不可变候选集合投影已完成语义轮数：仅 COMPLETE 且未被
// 整轮失效回执排除的候选计一轮。finding 后续的确认、驳回、改严重度或
// effective PASS/FAIL 变化都不影响计数。
func completedRounds(s Series) int {
	count := 0
	for _, candidate := range s.RawCandidates {
		if candidate.Completeness == completenessComplete && !ResultExcluded(s, candidate.ResultDigest) {
			count++
		}
	}
	return count
}

// OccupiedAuthorizationIDs 返回被有效（未被整轮失效排除）候选占用的授权
// 身份集合——授权占用态的权威投影，审查公开面与容量投影共用。
func OccupiedAuthorizationIDs(s Series) map[string]bool {
	occupied := map[string]bool{}
	for _, candidate := range s.RawCandidates {
		if candidate.AuthorizationID != "" && !ResultExcluded(s, candidate.ResultDigest) {
			occupied[candidate.AuthorizationID] = true
		}
	}
	return occupied
}

// occupiedExtraAuthorizations 统计被有效（未排除）候选占用的不同授权数。
func occupiedExtraAuthorizations(s Series) int {
	return len(OccupiedAuthorizationIDs(s))
}

// unoccupiedAuthorizations 统计未被有效候选占用的授权数：准入上可立即
// 使用的空槽数。
func unoccupiedAuthorizations(s Series) int {
	occupied := OccupiedAuthorizationIDs(s)
	count := 0
	for _, authorization := range s.Authorizations {
		if !occupied[authorization.ID] {
			count++
		}
	}
	return count
}

// availableAuthorization 返回第一个未被有效候选占用的授权下标；没有空槽时
// 第二返回值为 false。它承载准入语义（授权是否还有未消费轮、下一轮形态
// 投影），与绑定的选槽语义区分。
func availableAuthorization(s Series) (int, bool) {
	occupied := OccupiedAuthorizationIDs(s)
	for index, authorization := range s.Authorizations {
		if !occupied[authorization.ID] {
			return index, true
		}
	}
	return 0, false
}

// bindableAuthorization 返回第一个可承载背书绑定的授权下标；没有时第二
// 返回值为 false。可承载的槽是干净槽：既无有效结果占用，也不承载任何
// 在途派发（"在途"＝未落定，判定谓词由协议层传入——见协议层 liveDispatch）。
// 超额轮签发与换新继承共用此选槽规则：每个在途派发独占其授权槽。
func bindableAuthorization(s Series, live func(dispatchID string) bool) (int, bool) {
	occupied := OccupiedAuthorizationIDs(s)
	for index, authorization := range s.Authorizations {
		if !occupied[authorization.ID] && !slices.ContainsFunc(authorization.DispatchIDs, live) {
			return index, true
		}
	}
	return 0, false
}

// AuthorizationOfDispatch 返回派发当前绑定的授权 ID。绑定是独占的：同一
// 派发至多出现在一条授权的 DispatchIDs（完整性校验的结构负控）；未绑定时
// 返回空串。签发闸以它区分在途派发是否已持背书槽，换新以它定位被换新旧
// 派发的原槽。
func AuthorizationOfDispatch(s Series, dispatchID string) string {
	for _, authorization := range s.Authorizations {
		if slices.Contains(authorization.DispatchIDs, dispatchID) {
			return authorization.ID
		}
	}
	return ""
}

// ResultExcluded 判断一条结果是否已被整轮 INVALIDATE_RESULT 回执排除。
func ResultExcluded(s Series, resultDigest string) bool {
	for _, correction := range s.Corrections {
		if correction.ResultDigest == resultDigest && correction.Decision == decisionInvalidateResult {
			return true
		}
	}
	return false
}

// nextRoundRequirement 依次判定下一轮的准入形态：自动容量未满→无需任何
// 授权；存在空槽授权→可直接使用；否则须用户决定（typed Ask，不由本包
// 触发）。
func nextRoundRequirement(s Series) string {
	if s.Completed < s.AutomaticLimit {
		return nextRequirementAutomatic
	}
	if _, ok := availableAuthorization(s); ok {
		return nextRequirementAuthorized
	}
	return nextRequirementUser
}

// refresh derives capacity from the review ledgers. RemainingCapacity counts
// automatic slots plus unoccupied authorizations; protocol planning combines
// it with the outstanding obligation and live dispatches to decide readiness.
// FirstPass latches the first effective PASS and never rolls back.
func refresh(s *Series) {
	s.Completed = completedRounds(*s)
	s.CurrentLimit = s.AutomaticLimit + len(s.Authorizations)
	s.RemainingAutomatic = max(0, s.AutomaticLimit-s.Completed)
	s.RemainingCapacity = max(0, s.AutomaticLimit-s.Completed) + unoccupiedAuthorizations(*s)
	s.ExtraAuthorized = len(s.Authorizations)
	s.ExtraConsumed = occupiedExtraAuthorizations(*s)
	s.NextRoundRequirement = nextRoundRequirement(*s)
	if !s.FirstPass && s.Effective.Status == statusPass {
		s.FirstPass = true
	}
}

// ---- effective 投影与决定查询 ----

// Effective 把系列内全部未被整轮排除、且与最新候选绑定同一需求 revision 的
// 候选 findings 求并集，投影为 effective status：DISMISS/INVALIDATE_FINDING
// 的 finding 跳过；其余按有效严重度计入——未裁决保持原始严重度，未处置或
// 已确认的 P0/P1 阻塞即 FAIL；仅 P2/P3 为 PASS+advisory；全部 P0/P1 被驳回
// /判无效且无其他 blocker 时直接 PASS，不签发只为改聚合状态的 fresh review
// （refactor-plan/final-implementation-draft.md §4.4）。绑定字段（RawStatus/
// ResultDigest/RequirementRevision）锚最新候选：旧 revision 的证据随
// revision 前移不再拦新轮，与 HasConfirmedBlocking 同一 revision 界；
// 最新候选整轮失效时投影为 PENDING 并携带下游影响；其余状态的
// DownstreamImpact 由接纳路径按状态迁移标记。
func Effective(s Series, revision int) EffectiveStatus {
	blocking, advisory := []string{}, []string{}
	// The latest candidate owns the conclusion. Invalidating it requires a new
	// review; an older PASS cannot replace its missing evidence.
	if len(s.RawCandidates) == 0 {
		return EffectiveStatus{Status: statusPending, Source: s.Effective.Source, Revision: revision,
			BlockingFindingIDs: blocking, AdvisoryFindingIDs: advisory, DownstreamImpact: []string{}}
	}
	anchor := s.RawCandidates[len(s.RawCandidates)-1]
	for _, correction := range s.Corrections {
		if correction.ResultDigest == anchor.ResultDigest && correction.Decision == decisionInvalidateResult {
			return EffectiveStatus{
				Status: statusPending, RawStatus: anchor.RawStatus, ResultDigest: anchor.ResultDigest,
				RequirementRevision: anchor.RequirementRevision, Source: correction.ID, Revision: revision,
				BlockingFindingIDs: blocking, AdvisoryFindingIDs: advisory, DownstreamImpact: downstreamImpact(s.Action),
			}
		}
	}
	source := anchor.ResultDigest
	for _, candidate := range s.RawCandidates {
		if candidate.RequirementRevision != anchor.RequirementRevision || ResultExcluded(s, candidate.ResultDigest) {
			continue
		}
		for _, finding := range candidate.Findings {
			decision, severity, decisionSource := EffectiveFindingDecision(s, candidate.ResultDigest, finding)
			if decisionSource != "" {
				source = decisionSource
			}
			if decision == decisionDismiss || decision == decisionInvalidateFinding {
				continue
			}
			if severity == "P0" || severity == "P1" {
				blocking = appendUnique(blocking, finding.ID)
			} else {
				advisory = appendUnique(advisory, finding.ID)
			}
		}
	}
	status := statusPass
	if len(blocking) != 0 {
		status = statusFail
	}
	return EffectiveStatus{
		Status:              status,
		RawStatus:           anchor.RawStatus,
		ResultDigest:        anchor.ResultDigest,
		RequirementRevision: anchor.RequirementRevision,
		Source:              source,
		Revision:            revision,
		BlockingFindingIDs:  blocking,
		AdvisoryFindingIDs:  advisory,
		DownstreamImpact:    []string{},
	}
}

// EffectiveFindingDecision 返回一条 finding 的当前有效裁决：按
// EffectiveRevision 取最新的 adjudication 或 correction；severity 仅被
// RESEVERITIZE 改写，否则保持原始值。
func EffectiveFindingDecision(s Series, resultDigest string, finding Finding) (decision, severity, source string) {
	severity = finding.Severity
	latestRevision := -1
	for _, adjudication := range s.Adjudications {
		if adjudication.ResultDigest == resultDigest && adjudication.FindingID == finding.ID && adjudication.EffectiveRevision > latestRevision {
			decision, source, latestRevision = adjudication.Decision, adjudication.ID, adjudication.EffectiveRevision
			if adjudication.Severity != "" {
				severity = adjudication.Severity
			}
		}
	}
	for _, correction := range s.Corrections {
		if correction.ResultDigest == resultDigest && correction.FindingID == finding.ID && correction.EffectiveRevision > latestRevision {
			decision, source, latestRevision = correction.Decision, correction.ID, correction.EffectiveRevision
			if correction.Severity != "" {
				severity = correction.Severity
			}
		}
	}
	return decision, severity, source
}

// EffectiveFindingRationale 返回一条 finding 当前生效的严重度理由：最新的
// 非空裁决理由覆盖原始理由。
func EffectiveFindingRationale(s Series, resultDigest string, finding Finding) string {
	rationale := finding.SeverityRationale
	latestRevision := -1
	for _, adjudication := range s.Adjudications {
		if adjudication.ResultDigest == resultDigest && adjudication.FindingID == finding.ID && adjudication.EffectiveRevision > latestRevision && adjudication.SeverityRationale != "" {
			rationale, latestRevision = adjudication.SeverityRationale, adjudication.EffectiveRevision
		}
	}
	for _, correction := range s.Corrections {
		if correction.ResultDigest == resultDigest && correction.FindingID == finding.ID && correction.EffectiveRevision > latestRevision && correction.SeverityRationale != "" {
			rationale, latestRevision = correction.SeverityRationale, correction.EffectiveRevision
		}
	}
	return rationale
}

// LatestDecisionID 返回 (resultDigest, findingID) 当前最新决定的 ID；无任何
// 决定时为空。纠正的 supersedes 必须精确命中它。
func LatestDecisionID(s Series, resultDigest, findingID string) string {
	latestID := ""
	latestRevision := -1
	for _, adjudication := range s.Adjudications {
		if adjudication.ResultDigest == resultDigest && adjudication.FindingID == findingID && adjudication.EffectiveRevision > latestRevision {
			latestID, latestRevision = adjudication.ID, adjudication.EffectiveRevision
		}
	}
	for _, correction := range s.Corrections {
		if correction.ResultDigest == resultDigest && correction.FindingID == findingID && correction.EffectiveRevision > latestRevision {
			latestID, latestRevision = correction.ID, correction.EffectiveRevision
		}
	}
	return latestID
}

// HasConfirmedBlocking 报告 series 是否存在"已确认阻塞"的 finding：有效
// 裁决为 CONFIRM 或 RESEVERITIZE、且有效严重度 P0/P1。只统计与当前
// effective 绑定同一需求 revision、未被整轮失效排除的证据——需求 revision
// 前移后，旧 revision 下确认的阻塞不再拦新轮（§7.1.2 的新鲜复审义务由
// 换 revision 解除）；未被裁决的 P0/P1 只阻塞 effective，不构成确认。
func HasConfirmedBlocking(s Series) bool {
	revision := s.Effective.RequirementRevision
	for _, candidate := range s.RawCandidates {
		if candidate.RequirementRevision != revision || ResultExcluded(s, candidate.ResultDigest) {
			continue
		}
		for _, finding := range candidate.Findings {
			decision, severity, _ := EffectiveFindingDecision(s, candidate.ResultDigest, finding)
			if (decision == decisionConfirm || decision == decisionReseveritize) && blockingSeverity(severity) {
				return true
			}
		}
	}
	return false
}

// FindingDecision 报告给定 digest 的 finding 的所属动作、有效裁决与有效
// 严重度（仅在未被整轮失效排除的证据中查找）。发现项驱动的需求修订以它
// 核对来源标签与真实裁决的一致性。
func FindingDecision(set Set, findingDigest string) (action Action, decision, severity string, ok bool) {
	for _, action := range []Action{ProductReview, StartReadiness} {
		series, _ := set.Series(action)
		for _, candidate := range series.RawCandidates {
			if ResultExcluded(series, candidate.ResultDigest) {
				continue
			}
			for _, finding := range candidate.Findings {
				if finding.Digest == findingDigest {
					decision, severity, _ = EffectiveFindingDecision(series, candidate.ResultDigest, finding)
					return action, decision, severity, true
				}
			}
		}
	}
	return "", "", "", false
}

// FindingOwner 报告哪条 series 的有效（未被整轮失效排除）证据中存在给定
// digest 的 finding；不存在的 finding 无法驱动修订。
func FindingOwner(set Set, findingDigest string) (Action, bool) {
	action, _, _, ok := FindingDecision(set, findingDigest)
	return action, ok
}

// candidateByDigest 按 result digest 查找候选。
func candidateByDigest(s Series, digest string) (RawCandidate, bool) {
	for _, candidate := range s.RawCandidates {
		if candidate.ResultDigest == strings.TrimSpace(digest) {
			return candidate, true
		}
	}
	return RawCandidate{}, false
}

// findingByID 在候选内按稳定 ID 查找 finding。
func findingByID(candidate RawCandidate, id string) (Finding, bool) {
	for _, finding := range candidate.Findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return Finding{}, false
}

// ---- 下游影响与决定值编码 ----

// downstreamImpact 列出一个审查动作下游受 PASS 变阻塞影响的证据面，顺序
// 固定；product-review 的表首项是 start-readiness 本身。
func downstreamImpact(action Action) []string {
	if action == ProductReview {
		return []string{"start-readiness", "slicing", "route", "development", "qa", "gates", "seal"}
	}
	return []string{"slicing", "route", "development", "qa", "gates", "seal"}
}

// decisionValue 编码决定前后的 finding 值：未裁决携带原始严重度；RESEVERITIZE
// 携带目标严重度；DISMISS/CONFIRM/INVALIDATE_FINDING 携带当时有效严重度。
func decisionValue(decision, severity string) string {
	if decision == "" {
		return "UNADJUDICATED:" + severity
	}
	if severity != "" {
		return decision + ":" + severity
	}
	return decision
}

// decisionValueSeverity 从决定值中取出严重度部分。
func decisionValueSeverity(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// correctedSeverity 计算决定生效后的严重度：仅 RESEVERITIZE 改写，其余保持
// 原值。
func correctedSeverity(input CorrectionInput, original string) string {
	if input.Decision == decisionReseveritize {
		return input.Severity
	}
	return original
}

func validSeverity(severity string) bool {
	return severity == "P0" || severity == "P1" || severity == "P2" || severity == "P3"
}

func blockingSeverity(severity string) bool {
	return severity == "P0" || severity == "P1"
}

// validFindingDecision 校验可用于首裁与 finding 纠正的四个决定。
func validFindingDecision(decision string) bool {
	switch decision {
	case decisionConfirm, decisionDismiss, decisionReseveritize, decisionInvalidateFinding:
		return true
	}
	return false
}

func validInvalidity(invalidity string) bool {
	switch invalidity {
	case invalidityInvalid, invalidityIncomplete, invalidityStale, invalidityMisbound:
		return true
	}
	return false
}

// placeholderRationale 机械地拒绝纯占位/复述定义的严重度理由；理由是否
// 实质证明了所称后果，仍是逐 finding 的人工核实责任。
func placeholderRationale(value string) bool {
	switch normalizedRationale(value) {
	case "", "p0", "p1", "item fail", "requirement violated", "violates requirement", "违反需求", "违反了需求",
		"p1：正常使用路径产生明确错误或阻塞核心验收", "p1:正常使用路径产生明确错误或阻塞核心验收",
		"正常使用路径产生明确错误或阻塞核心验收":
		return true
	}
	return false
}

func normalizedRationale(value string) string {
	return strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(value)), " "), ".。！!")
}

// ---- Operator 核实规范化 ----

// requiredVerificationChecks 是接纳候选前必须逐项完成的八项封闭检查。
var requiredVerificationChecks = []string{
	"binding",
	"completeness",
	"evidence",
	"locations",
	"normal-entry",
	"requirement-match",
	"scope",
	"severity",
}

// normalizeOperatorVerification 规范化核实记录：证据非空，八项检查齐全、
// 无重复、无未知项；检查按固定序排序后存储。
func normalizeOperatorVerification(input OperatorVerification) (OperatorVerification, error) {
	input.Evidence = strings.TrimSpace(input.Evidence)
	if input.Evidence == "" {
		return OperatorVerification{}, fmt.Errorf("review: operator verification requires non-empty evidence")
	}
	wanted := map[string]bool{}
	for _, check := range requiredVerificationChecks {
		wanted[check] = true
	}
	seen := map[string]bool{}
	checks := make([]string, 0, len(input.Checks))
	for _, raw := range input.Checks {
		check := strings.TrimSpace(raw)
		if !wanted[check] {
			return OperatorVerification{}, fmt.Errorf("review: operator verification check %q is not one of the eight required checks", check)
		}
		if seen[check] {
			return OperatorVerification{}, fmt.Errorf("review: operator verification check %q is duplicated", check)
		}
		seen[check] = true
		checks = append(checks, check)
	}
	var missing []string
	for _, check := range requiredVerificationChecks {
		if !seen[check] {
			missing = append(missing, check)
		}
	}
	if len(missing) != 0 {
		return OperatorVerification{}, fmt.Errorf("review: operator verification is missing checks: %s", strings.Join(missing, ", "))
	}
	slices.Sort(checks)
	return OperatorVerification{Checks: checks, Evidence: input.Evidence}, nil
}

// ---- 摘要派生 ----

// digestValue 返回一个值的引擎摘要：值先编码为 canonical JSON（只含
// string/int 与有序切片，编码只是数据的函数），再取共享 encoder 的
// SHA-256 形态。
func digestValue(value any) string {
	data, _ := json.Marshal(value)
	return encoder.Digest(data)
}

// shortDigest 取摘要十六进制部分的前 16 位作为短身份。
func shortDigest(digest string) string {
	sum := strings.TrimPrefix(digest, "sha256:")
	if len(sum) <= 16 {
		return sum
	}
	return sum[:16]
}

// findingDigest 绑定 finding 的十个内容字段（空 Locations 归一为 nil 保持
// 编码确定性）；ID 与 Digest 均由它派生，与后续处置无关。
func findingDigest(finding Finding) string {
	locations := finding.Locations
	if len(locations) == 0 {
		locations = nil
	}
	return digestValue(struct {
		Severity, SeverityRationale, Category, Requirement, NormalPath, ActualError, DeferralImpact, Message string
		Verification                                                                                         FindingVerification
		Locations                                                                                            []string
	}{finding.Severity, finding.SeverityRationale, finding.Category, finding.Requirement, finding.NormalPath, finding.ActualError, finding.DeferralImpact, finding.Message, finding.Verification, locations})
}

// candidateDigest 绑定 runID、动作、派发、需求 revision、raw 状态、消息与
// 全部 findings（含其派生身份）；授权与核实字段不参与，同一证据在不同授权
// 下仍是同一结果。
func candidateDigest(runID string, candidate RawCandidate) string {
	return digestValue(struct {
		RunID               string
		Action              Action
		DispatchID          string
		RequirementRevision string
		Status              string
		Message             string
		Findings            []Finding
	}{runID, candidate.Action, candidate.DispatchID, candidate.RequirementRevision, candidate.RawStatus, candidate.Message, candidate.Findings})
}

// authorizationDigest 绑定授权身份四元组：run、动作、理由与序号。
func authorizationDigest(runID string, action Action, reason string, ordinal int) string {
	return digestValue(struct {
		RunID   string
		Action  Action
		Reason  string
		Ordinal int
	}{runID, action, reason, ordinal})
}

// appendUnique 追加尚不存在的字符串。
func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}
