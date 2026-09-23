package review

import (
	"fmt"
	"slices"
	"strings"
)

// Validate 重算 Set 内两条 series 的全部投影与身份链并逐项比对：动作与激
// 活绑定、四本账非空壳、授权身份/绑定/背书、候选与 finding 身份、决定链
// （supersedes 指向、old/new 值、EffectiveRevision 唯一）、容量与计轮投影、
// effective 投影与下游标记。任一不符即拒绝，用于接纳前的防篡改前置校验。
func Validate(runID string, set Set) error {
	if err := set.ProductReview.validate(runID, ProductReview); err != nil {
		return err
	}
	return set.StartReadiness.validate(runID, StartReadiness)
}

func (s Series) validate(runID string, action Action) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("review series %s: %s", action, fmt.Sprintf(format, args...))
	}
	if s.Action != action {
		return fail("action binding is %q", s.Action)
	}
	if s.ActivationSource != activationWorkflowStart {
		return fail("activation source is %q, want %q", s.ActivationSource, activationWorkflowStart)
	}
	if s.AutomaticLimit != automaticRoundLimit {
		return fail("automatic limit is %d, want %d", s.AutomaticLimit, automaticRoundLimit)
	}
	if s.Authorizations == nil || s.RawCandidates == nil || s.Adjudications == nil || s.Corrections == nil {
		return fail("one of the four ledgers is missing")
	}
	if s.Effective.BlockingFindingIDs == nil || s.Effective.AdvisoryFindingIDs == nil || s.Effective.DownstreamImpact == nil {
		return fail("effective status projections are partially initialized")
	}
	authorizationByID := map[string]bool{}
	for index, authorization := range s.Authorizations {
		if authorization.Action != action || authorization.Source != authorizationUserSource || strings.TrimSpace(authorization.Reason) == "" || len(authorization.DispatchIDs) == 0 || authorization.RecordedResultDigests == nil {
			return fail("authorization %d has an incomplete binding, source, or reason", index+1)
		}
		expectedID := "review-authorization-" + shortDigest(authorizationDigest(runID, action, authorization.Reason, index+1))
		if authorization.ID != expectedID {
			return fail("authorization %d has a forged identity", index+1)
		}
		if authorizationByID[authorization.ID] {
			return fail("authorization %s is recorded twice", authorization.ID)
		}
		boundDispatches := map[string]bool{}
		for _, dispatchID := range authorization.DispatchIDs {
			if strings.TrimSpace(dispatchID) == "" || boundDispatches[dispatchID] {
				return fail("authorization %s binds an empty or duplicated dispatch", authorization.ID)
			}
			boundDispatches[dispatchID] = true
		}
		authorizationByID[authorization.ID] = true
	}
	candidateDigests := map[string]bool{}
	for _, candidate := range s.RawCandidates {
		if candidate.Action != action || candidate.DispatchID == "" || candidate.RequirementRevision == "" || candidate.Completeness != completenessComplete || candidate.Findings == nil {
			return fail("candidate %s has an invalid binding or completeness", candidate.ID)
		}
		if candidate.RawStatus != statusPass && candidate.RawStatus != statusFail {
			return fail("candidate %s raw status %q is invalid", candidate.ID, candidate.RawStatus)
		}
		if candidate.ResultDigest == "" || candidate.ID != "review-result-"+shortDigest(candidate.ResultDigest) {
			return fail("candidate identity is invalid")
		}
		if candidateDigests[candidate.ResultDigest] {
			return fail("candidate digest %s is recorded twice", candidate.ResultDigest)
		}
		candidateDigests[candidate.ResultDigest] = true
		if candidate.AuthorizationID != "" && !authorizationByID[candidate.AuthorizationID] {
			return fail("candidate %s references unknown authorization %s", candidate.ID, candidate.AuthorizationID)
		}
		if _, err := normalizeOperatorVerification(candidate.OperatorVerification); err != nil {
			return fail("candidate %s operator verification is invalid: %v", candidate.ID, err)
		}
		findingIDs := map[string]bool{}
		for _, finding := range candidate.Findings {
			if finding.ID == "" || finding.Digest != findingDigest(finding) || finding.ID != "finding-"+shortDigest(finding.Digest) || finding.Message == "" || !validSeverity(finding.Severity) {
				return fail("finding identity in candidate %s is invalid", candidate.ID)
			}
			if findingIDs[finding.ID] {
				return fail("finding %s is recorded twice in candidate %s", finding.ID, candidate.ID)
			}
			findingIDs[finding.ID] = true
		}
		if candidate.ResultDigest != candidateDigest(runID, candidate) {
			return fail("candidate %s digest does not match its immutable evidence", candidate.ID)
		}
	}
	for _, authorization := range s.Authorizations {
		recorded := map[string]bool{}
		for _, resultDigest := range authorization.RecordedResultDigests {
			candidate, ok := candidateByDigest(s, resultDigest)
			if !ok || candidate.AuthorizationID != authorization.ID || recorded[resultDigest] {
				return fail("authorization %s records a missing, duplicated, or misbound result", authorization.ID)
			}
			recorded[resultDigest] = true
		}
		for _, candidate := range s.RawCandidates {
			if candidate.AuthorizationID == authorization.ID && !recorded[candidate.ResultDigest] {
				return fail("authorized candidate %s is absent from its authorization receipt", candidate.ID)
			}
		}
	}
	decisionIDs := map[string]bool{}
	decisionTargets := map[string]string{}
	decisionValues := map[string]string{}
	decisionRevisions := map[int]bool{}
	for _, adjudication := range s.Adjudications {
		candidate, ok := candidateByDigest(s, adjudication.ResultDigest)
		if !ok || adjudication.RunID != runID || adjudication.Action != action || adjudication.DispatchID != candidate.DispatchID || adjudication.RequirementRevision != candidate.RequirementRevision {
			return fail("adjudication %s binding is invalid", adjudication.ID)
		}
		finding, findingOK := findingByID(candidate, adjudication.FindingID)
		if !findingOK || !adjudication.UserAuthorized || adjudication.Source == "" || strings.TrimSpace(adjudication.Reason) == "" || adjudication.OldValue == "" || adjudication.NewValue == "" || adjudication.EffectiveRevision <= 0 || adjudication.EffectiveRevision > s.Effective.Revision {
			return fail("adjudication %s target, authorization, or source is incomplete", adjudication.ID)
		}
		if !validFindingDecision(adjudication.Decision) {
			return fail("adjudication %s decision %q is invalid", adjudication.ID, adjudication.Decision)
		}
		if adjudication.Decision == decisionReseveritize && !validSeverity(adjudication.Severity) {
			return fail("adjudication %s re-severity target is invalid", adjudication.ID)
		}
		if adjudication.Decision == decisionReseveritize && blockingSeverity(adjudication.Severity) && placeholderRationale(adjudication.SeverityRationale) {
			return fail("adjudication %s raises a blocking severity without an independent rationale", adjudication.ID)
		}
		if adjudication.Decision != decisionReseveritize && (adjudication.Severity != "" || adjudication.SeverityRationale != "") {
			return fail("adjudication %s carries severity fields outside RESEVERITIZE", adjudication.ID)
		}
		expectedOld := decisionValue("", finding.Severity)
		expectedNew := decisionValue(adjudication.Decision, correctedSeverity(CorrectionInput{Decision: adjudication.Decision, Severity: adjudication.Severity}, finding.Severity))
		if adjudication.OldValue != expectedOld || adjudication.NewValue != expectedNew {
			return fail("adjudication %s old/new values do not match its decision", adjudication.ID)
		}
		identity := adjudication
		identity.ID = ""
		if adjudication.ID != "review-adjudication-"+shortDigest(digestValue(identity)) || decisionIDs[adjudication.ID] || decisionRevisions[adjudication.EffectiveRevision] {
			return fail("adjudication %s identity is invalid or duplicated", adjudication.ID)
		}
		decisionIDs[adjudication.ID] = true
		decisionTargets[adjudication.ID] = adjudication.ResultDigest + ":" + adjudication.FindingID
		decisionValues[adjudication.ID] = adjudication.NewValue
		decisionRevisions[adjudication.EffectiveRevision] = true
	}
	for _, correction := range s.Corrections {
		candidate, ok := candidateByDigest(s, correction.ResultDigest)
		if !ok || correction.RunID != runID || correction.Action != action || correction.DispatchID != candidate.DispatchID || correction.RequirementRevision != candidate.RequirementRevision {
			return fail("correction %s binding is invalid", correction.ID)
		}
		if !validFindingDecision(correction.Decision) && correction.Decision != decisionInvalidateResult {
			return fail("correction %s decision %q is invalid", correction.ID, correction.Decision)
		}
		if correction.Decision != decisionInvalidateResult {
			if _, ok := findingByID(candidate, correction.FindingID); !ok {
				return fail("correction %s finding target is invalid", correction.ID)
			}
			if correction.Supersedes == "" || decisionTargets[correction.Supersedes] != correction.ResultDigest+":"+correction.FindingID {
				return fail("correction %s does not supersede the prior decision for its exact target", correction.ID)
			}
			if correction.Invalidity != "" {
				return fail("finding correction %s carries whole-result invalidity", correction.ID)
			}
			if correction.Decision == decisionReseveritize && !validSeverity(correction.Severity) {
				return fail("correction %s re-severity target is invalid", correction.ID)
			}
			if correction.Decision == decisionReseveritize && blockingSeverity(correction.Severity) && placeholderRationale(correction.SeverityRationale) {
				return fail("correction %s raises a blocking severity without an independent rationale", correction.ID)
			}
			if correction.Decision != decisionReseveritize && (correction.Severity != "" || correction.SeverityRationale != "") {
				return fail("correction %s carries severity fields outside RESEVERITIZE", correction.ID)
			}
			previousValue := decisionValues[correction.Supersedes]
			expectedNew := decisionValue(correction.Decision, correctedSeverity(CorrectionInput{Decision: correction.Decision, Severity: correction.Severity}, decisionValueSeverity(previousValue)))
			if correction.OldValue != previousValue || correction.NewValue != expectedNew {
				return fail("correction %s old/new values do not match the superseded decision", correction.ID)
			}
		} else if correction.FindingID != "" || correction.Supersedes != "" || correction.Severity != "" || correction.SeverityRationale != "" || !validInvalidity(correction.Invalidity) {
			return fail("whole-result invalidation %s is malformed", correction.ID)
		} else if correction.OldValue != "VALID_RESULT:"+candidate.RawStatus || correction.NewValue != "INVALID_RESULT:"+correction.Invalidity {
			return fail("whole-result invalidation %s old/new values are inconsistent", correction.ID)
		}
		if !correction.UserAuthorized || correction.Source == "" || strings.TrimSpace(correction.Reason) == "" || correction.OldValue == "" || correction.NewValue == "" || correction.EffectiveRevision <= 0 || correction.EffectiveRevision > s.Effective.Revision {
			return fail("correction %s authorization or source is incomplete", correction.ID)
		}
		identity := correction
		identity.ID = ""
		if correction.ID != "review-correction-"+shortDigest(digestValue(identity)) || decisionIDs[correction.ID] || decisionRevisions[correction.EffectiveRevision] {
			return fail("correction %s identity is invalid or duplicated", correction.ID)
		}
		decisionIDs[correction.ID] = true
		decisionTargets[correction.ID] = correction.ResultDigest + ":" + correction.FindingID
		decisionValues[correction.ID] = correction.NewValue
		decisionRevisions[correction.EffectiveRevision] = true
	}
	for _, authorization := range s.Authorizations {
		validResults := 0
		for _, resultDigest := range authorization.RecordedResultDigests {
			if !resultExcluded(s, resultDigest) {
				validResults++
			}
		}
		if validResults > 1 {
			return fail("authorization %s backs more than one valid result", authorization.ID)
		}
	}
	projectedCompleted := completedRounds(s)
	if s.Completed != projectedCompleted {
		return fail("completed=%d does not match the candidate projection %d", s.Completed, projectedCompleted)
	}
	currentLimit := s.AutomaticLimit + len(s.Authorizations)
	remainingAutomatic := max(0, s.AutomaticLimit-s.Completed)
	remainingCapacity := remainingAutomatic + unoccupiedAuthorizations(s)
	if s.CurrentLimit != currentLimit || s.RemainingAutomatic != remainingAutomatic || s.RemainingCapacity != remainingCapacity || s.ExtraAuthorized != len(s.Authorizations) || s.ExtraConsumed != occupiedExtraAuthorizations(s) {
		return fail("capacity projection does not match candidates and authorizations")
	}
	if s.NextRoundRequirement != nextRoundRequirement(s) {
		return fail("next-round requirement is %q, want %q", s.NextRoundRequirement, nextRoundRequirement(s))
	}
	if s.Completed > currentLimit {
		return fail("completed rounds exceed the effective limit")
	}
	if occupiedExtraAuthorizations(s) < max(0, s.Completed-s.AutomaticLimit) {
		return fail("completed extra rounds lack distinct action authorizations")
	}
	if err := validateEffectiveProjection(action, s); err != nil {
		return fail("%v", err)
	}
	return nil
}

// validateEffectiveProjection 强制 effective 与账本投影一致：PENDING 不得残
// 留 finding 投影（指向的证据必须存在）；PASS/FAIL 必须命中一个未排除、
// revision 一致的候选且各字段等于重投影结果；下游标记只允许出现在 FAIL
// 态且等于该动作的下游表。
func validateEffectiveProjection(action Action, s Series) error {
	effective := s.Effective
	if effective.Status != statusPending && effective.Status != statusPass && effective.Status != statusFail {
		return fmt.Errorf("effective status %q is invalid", effective.Status)
	}
	if effective.Source == "" || effective.Revision < 0 {
		return fmt.Errorf("effective status source or revision is incomplete")
	}
	if s.UpstreamVoid && effective.Status != statusPending {
		return fmt.Errorf("upstream void must hold the effective status at PENDING")
	}
	if effective.Status == statusPending {
		if effective.ResultDigest != "" {
			if _, ok := candidateByDigest(s, effective.ResultDigest); !ok {
				return fmt.Errorf("pending effective status references an unknown result %s", effective.ResultDigest)
			}
		}
		if len(effective.BlockingFindingIDs) != 0 || len(effective.AdvisoryFindingIDs) != 0 {
			return fmt.Errorf("pending effective status retains finding projections")
		}
		return nil
	}
	candidate, ok := candidateByDigest(s, effective.ResultDigest)
	if !ok || resultExcluded(s, candidate.ResultDigest) || candidate.RequirementRevision != effective.RequirementRevision {
		return fmt.Errorf("effective result binding is missing, excluded, or inconsistent")
	}
	expected := Effective(s, candidate, effective.Revision)
	if expected.Status != effective.Status || expected.RawStatus != effective.RawStatus || expected.Source != effective.Source || !slices.Equal(expected.BlockingFindingIDs, effective.BlockingFindingIDs) || !slices.Equal(expected.AdvisoryFindingIDs, effective.AdvisoryFindingIDs) {
		return fmt.Errorf("effective status does not match the raw/adjudication projection")
	}
	if len(effective.DownstreamImpact) != 0 && (effective.Status != statusFail || !slices.Equal(effective.DownstreamImpact, downstreamImpact(action))) {
		return fmt.Errorf("effective downstream impact is inconsistent")
	}
	return nil
}
