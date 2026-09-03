package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	preDevelopmentReviewAutomaticLimit = 3
	preDevelopmentReviewActivation     = "WORKFLOW_START"
)

var preDevelopmentReviewActions = []string{"product-review", "start-readiness"}

// PreDevelopmentReviewSeries is the durable, action-specific semantic review
// ledger. Completed is a projection of the immutable accepted candidates minus
// explicit whole-result invalidation receipts; it is never reconstructed from a
// dispatch attempt, the Actions map, or a transcript.
type PreDevelopmentReviewSeries struct {
	Action                string                     `json:"action"`
	ActivationSource      string                     `json:"activationSource"`
	AutomaticLimit        int                        `json:"automaticLimit"`
	Completed             int                        `json:"completed"`
	CurrentLimit          int                        `json:"currentLimit"`
	RemainingAutomatic    int                        `json:"remainingAutomatic"`
	RemainingCapacity     int                        `json:"remainingCapacity"`
	NextRoundRequirement  string                     `json:"nextRoundRequirement"`
	ExtraAuthorized       int                        `json:"extraAuthorized"`
	ExtraConsumed         int                        `json:"extraConsumed"`
	Authorizations        []ReviewRoundAuthorization `json:"authorizations"`
	RawCandidates         []SemanticReviewCandidate  `json:"rawCandidates"`
	Adjudications         []ReviewAdjudication       `json:"adjudications"`
	Corrections           []ReviewCorrectionReceipt  `json:"corrections"`
	EffectiveReviewStatus EffectiveReviewStatus      `json:"effectiveReviewStatus"`
}

// ReviewRoundAuthorization records one action-scoped, user-requested extra
// semantic round. A runtime failure may bind more than one retry dispatch to the
// same authorization, but at most one non-invalid complete result occupies it.
type ReviewRoundAuthorization struct {
	ID                    string   `json:"id"`
	Action                string   `json:"action"`
	Source                string   `json:"source"`
	Reason                string   `json:"reason"`
	DispatchIDs           []string `json:"dispatchIds"`
	RecordedResultDigests []string `json:"recordedResultDigests"`
}

// SemanticReviewCandidate is the immutable reviewer evidence accepted through
// record-action after lifecycle, binding, semantic-shape, and Operator checks.
// Effective status is deliberately projected elsewhere and never written back
// over this raw result.
type SemanticReviewCandidate struct {
	ID                   string                  `json:"id"`
	Action               string                  `json:"action"`
	DispatchID           string                  `json:"dispatchId"`
	ResultDigest         string                  `json:"resultDigest"`
	RequirementRevision  string                  `json:"requirementRevision"`
	RawStatus            string                  `json:"rawStatus"`
	Message              string                  `json:"message,omitempty"`
	Findings             []SemanticReviewFinding `json:"findings"`
	Completeness         string                  `json:"completeness"`
	AuthorizationID      string                  `json:"authorizationId,omitempty"`
	OperatorVerification OperatorVerification    `json:"operatorVerification"`
}

// SemanticReviewFinding gives every raw finding a stable identity independent
// of later user disposition or severity correction.
type SemanticReviewFinding struct {
	ID        string   `json:"id"`
	Digest    string   `json:"digest"`
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Locations []string `json:"locations,omitempty"`
}

// OperatorVerification records the explicit checks performed before a semantic
// review candidate is accepted. Source/Reason remain readable for run files
// produced before the public verification inputs were introduced.
type OperatorVerification struct {
	Checks   []string `json:"checks,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
	Source   string   `json:"source,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

var requiredOperatorVerificationChecks = []string{
	"binding",
	"completeness",
	"evidence",
	"locations",
	"normal-entry",
	"requirement-match",
	"scope",
	"severity",
}

func legacyOperatorVerification() OperatorVerification {
	return OperatorVerification{
		Source: "internal RecordAction caller",
		Reason: "compatibility path for trusted in-process callers",
	}
}

func normalizeOperatorVerification(input OperatorVerification) (OperatorVerification, error) {
	input.Source = strings.TrimSpace(input.Source)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Evidence = strings.TrimSpace(input.Evidence)
	if input.Source != "" || input.Reason != "" {
		if len(input.Checks) != 0 || input.Evidence != "" || input.Source == "" || input.Reason == "" {
			return OperatorVerification{}, fmt.Errorf("operator verification cannot mix legacy source/reason with explicit checks/evidence")
		}
		return input, nil
	}
	if input.Evidence == "" {
		return OperatorVerification{}, fmt.Errorf("operator verification requires non-empty --operator-evidence")
	}
	wanted := map[string]bool{}
	for _, check := range requiredOperatorVerificationChecks {
		wanted[check] = true
	}
	seen := map[string]bool{}
	checks := make([]string, 0, len(input.Checks))
	for _, raw := range input.Checks {
		check := strings.TrimSpace(raw)
		if !wanted[check] {
			return OperatorVerification{}, fmt.Errorf("unknown operator verification check %q", check)
		}
		if seen[check] {
			return OperatorVerification{}, fmt.Errorf("duplicate operator verification check %q", check)
		}
		seen[check] = true
		checks = append(checks, check)
	}
	var missing []string
	for _, check := range requiredOperatorVerificationChecks {
		if !seen[check] {
			missing = append(missing, check)
		}
	}
	if len(missing) != 0 {
		return OperatorVerification{}, fmt.Errorf("operator verification is incomplete; missing --operator-check values: %s", strings.Join(missing, ", "))
	}
	sort.Strings(checks)
	return OperatorVerification{Checks: checks, Evidence: input.Evidence}, nil
}

// ReviewAdjudication is the first explicit user disposition of one raw finding.
// Later changes never overwrite it; they append ReviewCorrectionReceipt values.
type ReviewAdjudication struct {
	ID                  string `json:"id"`
	RunID               string `json:"runId"`
	Action              string `json:"action"`
	DispatchID          string `json:"dispatchId"`
	ResultDigest        string `json:"resultDigest"`
	RequirementRevision string `json:"requirementRevision"`
	FindingID           string `json:"findingId"`
	Decision            string `json:"decision"`
	Severity            string `json:"severity,omitempty"`
	OldValue            string `json:"oldValue"`
	NewValue            string `json:"newValue"`
	Reason              string `json:"reason"`
	Source              string `json:"source"`
	UserAuthorized      bool   `json:"userAuthorized"`
	EffectiveRevision   int    `json:"effectiveRevision"`
}

// ReviewCorrectionReceipt supersedes a prior adjudication/correction or
// invalidates one complete result as a whole. Raw candidates remain untouched.
type ReviewCorrectionReceipt struct {
	ID                  string `json:"id"`
	RunID               string `json:"runId"`
	Action              string `json:"action"`
	DispatchID          string `json:"dispatchId"`
	ResultDigest        string `json:"resultDigest"`
	RequirementRevision string `json:"requirementRevision"`
	FindingID           string `json:"findingId,omitempty"`
	Decision            string `json:"decision"`
	Severity            string `json:"severity,omitempty"`
	Invalidity          string `json:"invalidity,omitempty"`
	Supersedes          string `json:"supersedes,omitempty"`
	OldValue            string `json:"oldValue"`
	NewValue            string `json:"newValue"`
	Reason              string `json:"reason"`
	Source              string `json:"source"`
	UserAuthorized      bool   `json:"userAuthorized"`
	EffectiveRevision   int    `json:"effectiveRevision"`
}

// EffectiveReviewStatus is the separately projected workflow conclusion. It
// also exposes the downstream evidence affected by a PASS-to-blocking change.
type EffectiveReviewStatus struct {
	Status              string   `json:"status"`
	RawStatus           string   `json:"rawStatus,omitempty"`
	ResultDigest        string   `json:"resultDigest,omitempty"`
	RequirementRevision string   `json:"requirementRevision,omitempty"`
	Source              string   `json:"source"`
	Revision            int      `json:"revision"`
	BlockingFindingIDs  []string `json:"blockingFindingIds"`
	AdvisoryFindingIDs  []string `json:"advisoryFindingIds"`
	DownstreamImpact    []string `json:"downstreamImpact"`
}

// ReviewCorrectionInput is the exact-binding/CAS contract used by the public
// settle-findings correction path.
type ReviewCorrectionInput struct {
	Action                    string
	DispatchID                string
	ResultDigest              string
	RequirementRevision       string
	FindingID                 string
	Decision                  string
	Severity                  string
	Invalidity                string
	Supersedes                string
	Reason                    string
	Source                    string
	UserAuthorized            bool
	ExpectedEffectiveRevision int
}

func newPreDevelopmentReviewSeries() map[string]PreDevelopmentReviewSeries {
	series := map[string]PreDevelopmentReviewSeries{}
	for _, action := range preDevelopmentReviewActions {
		series[action] = PreDevelopmentReviewSeries{
			Action:                action,
			ActivationSource:      preDevelopmentReviewActivation,
			AutomaticLimit:        preDevelopmentReviewAutomaticLimit,
			CurrentLimit:          preDevelopmentReviewAutomaticLimit,
			RemainingAutomatic:    preDevelopmentReviewAutomaticLimit,
			RemainingCapacity:     preDevelopmentReviewAutomaticLimit,
			NextRoundRequirement:  "AUTOMATIC_CAPACITY",
			Authorizations:        []ReviewRoundAuthorization{},
			RawCandidates:         []SemanticReviewCandidate{},
			Adjudications:         []ReviewAdjudication{},
			Corrections:           []ReviewCorrectionReceipt{},
			EffectiveReviewStatus: EffectiveReviewStatus{Status: "PENDING", Source: preDevelopmentReviewActivation, BlockingFindingIDs: []string{}, AdvisoryFindingIDs: []string{}, DownstreamImpact: []string{}},
		}
	}
	return series
}

func isPreDevelopmentReviewAction(action string) bool {
	return action == "product-review" || action == "start-readiness"
}

func validatePreDevelopmentReviewState(state RunState) error {
	if state.PreDevelopmentReviewSeries == nil {
		return fmt.Errorf("pre-development review series state is missing")
	}
	if len(state.PreDevelopmentReviewSeries) != len(preDevelopmentReviewActions) {
		return fmt.Errorf("pre-development review series state is inconsistent: expected exactly product-review and start-readiness")
	}
	for _, action := range preDevelopmentReviewActions {
		series, ok := state.PreDevelopmentReviewSeries[action]
		if !ok {
			return fmt.Errorf("pre-development review series state is incomplete: %s is missing", action)
		}
		if err := validatePreDevelopmentReviewSeries(state.RunID, action, series); err != nil {
			return err
		}
	}
	return nil
}

func validatePreDevelopmentReviewSeries(runID, action string, series PreDevelopmentReviewSeries) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("pre-development review series %s is inconsistent: %s", action, fmt.Sprintf(format, args...))
	}
	if series.Action != action || !isPreDevelopmentReviewAction(series.Action) {
		return fail("action binding is %q", series.Action)
	}
	if series.ActivationSource != preDevelopmentReviewActivation {
		return fail("activation source is %q, want %q", series.ActivationSource, preDevelopmentReviewActivation)
	}
	if series.AutomaticLimit != preDevelopmentReviewAutomaticLimit {
		return fail("automatic limit is %d, want %d", series.AutomaticLimit, preDevelopmentReviewAutomaticLimit)
	}
	if series.Authorizations == nil || series.RawCandidates == nil || series.Adjudications == nil || series.Corrections == nil {
		return fail("one or more required ledgers are missing")
	}
	if series.EffectiveReviewStatus.BlockingFindingIDs == nil || series.EffectiveReviewStatus.AdvisoryFindingIDs == nil || series.EffectiveReviewStatus.DownstreamImpact == nil {
		return fail("effective status projections are partially initialized")
	}
	authorizations := map[string]ReviewRoundAuthorization{}
	for index, authorization := range series.Authorizations {
		if authorization.Action != action || authorization.Source != "USER_REQUESTED" || strings.TrimSpace(authorization.Reason) == "" || len(authorization.DispatchIDs) == 0 || authorization.RecordedResultDigests == nil {
			return fail("authorization binding/source/reason is incomplete")
		}
		expectedID := "review-authorization-" + shortDigest(digestValue(struct {
			RunID   string
			Action  string
			Reason  string
			Ordinal int
		}{runID, action, authorization.Reason, index + 1}))
		if authorization.ID != expectedID {
			return fail("authorization identity is invalid")
		}
		if _, duplicate := authorizations[authorization.ID]; duplicate {
			return fail("duplicate authorization %s", authorization.ID)
		}
		dispatches := map[string]bool{}
		for _, dispatchID := range authorization.DispatchIDs {
			if strings.TrimSpace(dispatchID) == "" || dispatches[dispatchID] {
				return fail("authorization dispatch bindings are empty or duplicated")
			}
			dispatches[dispatchID] = true
		}
		authorizations[authorization.ID] = authorization
	}
	candidateDigests := map[string]bool{}
	for _, candidate := range series.RawCandidates {
		if candidate.Action != action || candidate.DispatchID == "" || candidate.RequirementRevision == "" || candidate.Completeness != "COMPLETE" || candidate.Findings == nil {
			return fail("raw candidate binding/completeness is invalid")
		}
		if candidate.RawStatus != "PASS" && candidate.RawStatus != "FAIL" {
			return fail("raw candidate status %q is invalid", candidate.RawStatus)
		}
		if candidate.ResultDigest == "" || candidate.ID != "review-result-"+shortDigest(candidate.ResultDigest) {
			return fail("raw candidate identity is invalid")
		}
		if candidateDigests[candidate.ResultDigest] {
			return fail("duplicate raw candidate digest %s", candidate.ResultDigest)
		}
		candidateDigests[candidate.ResultDigest] = true
		if candidate.AuthorizationID != "" {
			if _, ok := authorizations[candidate.AuthorizationID]; !ok {
				return fail("candidate references unknown authorization %s", candidate.AuthorizationID)
			}
		}
		if _, err := normalizeOperatorVerification(candidate.OperatorVerification); err != nil {
			return fail("operator verification is invalid: %v", err)
		}
		findingIDs := map[string]bool{}
		for _, finding := range candidate.Findings {
			expectedFindingDigest := semanticFindingDigest(finding)
			if finding.ID == "" || finding.Digest != expectedFindingDigest || finding.ID != "finding-"+shortDigest(finding.Digest) || finding.Message == "" || (finding.Severity != "P0" && finding.Severity != "P1" && finding.Severity != "P2" && finding.Severity != "P3") {
				return fail("raw finding identity is invalid")
			}
			if findingIDs[finding.ID] {
				return fail("duplicate finding identity %s", finding.ID)
			}
			findingIDs[finding.ID] = true
		}
		if candidate.ResultDigest != semanticReviewCandidateDigest(runID, candidate) {
			return fail("raw candidate digest does not match its immutable evidence")
		}
	}
	for _, authorization := range series.Authorizations {
		recorded := map[string]bool{}
		for _, resultDigest := range authorization.RecordedResultDigests {
			candidate, ok := semanticCandidateByDigest(series, resultDigest)
			if !ok || candidate.AuthorizationID != authorization.ID || recorded[resultDigest] {
				return fail("authorization result bindings are missing, duplicated, or misbound")
			}
			recorded[resultDigest] = true
		}
		for _, candidate := range series.RawCandidates {
			if candidate.AuthorizationID == authorization.ID && !recorded[candidate.ResultDigest] {
				return fail("authorized candidate is absent from its authorization receipt")
			}
		}
	}
	decisionIDs := map[string]bool{}
	decisionTargets := map[string]string{}
	decisionNewValues := map[string]string{}
	decisionRevisions := map[int]bool{}
	for _, adjudication := range series.Adjudications {
		candidate, ok := semanticCandidateByDigest(series, adjudication.ResultDigest)
		if !ok || adjudication.RunID != runID || adjudication.Action != action || adjudication.DispatchID != candidate.DispatchID || adjudication.RequirementRevision != candidate.RequirementRevision {
			return fail("adjudication binding is invalid")
		}
		finding, findingOK := semanticFindingByID(candidate, adjudication.FindingID)
		if !findingOK || !adjudication.UserAuthorized || adjudication.Source == "" || strings.TrimSpace(adjudication.Reason) == "" || adjudication.OldValue == "" || adjudication.NewValue == "" || adjudication.EffectiveRevision <= 0 || adjudication.EffectiveRevision > series.EffectiveReviewStatus.Revision {
			return fail("adjudication target/authorization/source is incomplete")
		}
		if adjudication.Decision != "CONFIRM" && adjudication.Decision != "DISMISS" && adjudication.Decision != "RESEVERITIZE" && adjudication.Decision != "INVALIDATE_FINDING" {
			return fail("adjudication decision %q is invalid", adjudication.Decision)
		}
		if adjudication.Decision == "RESEVERITIZE" && adjudication.Severity != "P0" && adjudication.Severity != "P1" && adjudication.Severity != "P2" && adjudication.Severity != "P3" {
			return fail("adjudication severity is invalid")
		}
		if adjudication.Decision != "RESEVERITIZE" && adjudication.Severity != "" {
			return fail("non-severity adjudication carries a severity")
		}
		expectedOld := reviewDecisionValue("", finding.Severity)
		expectedNew := reviewDecisionValue(adjudication.Decision, correctedSeverity(ReviewCorrectionInput{Decision: adjudication.Decision, Severity: adjudication.Severity}, finding.Severity))
		if adjudication.OldValue != expectedOld || adjudication.NewValue != expectedNew {
			return fail("adjudication old/new values do not match its decision")
		}
		copy := adjudication
		copy.ID = ""
		if adjudication.ID != "review-adjudication-"+shortDigest(digestValue(copy)) || decisionIDs[adjudication.ID] || decisionRevisions[adjudication.EffectiveRevision] {
			return fail("adjudication identity is invalid or duplicated")
		}
		decisionIDs[adjudication.ID] = true
		decisionTargets[adjudication.ID] = adjudication.ResultDigest + ":" + adjudication.FindingID
		decisionNewValues[adjudication.ID] = adjudication.NewValue
		decisionRevisions[adjudication.EffectiveRevision] = true
	}
	for _, correction := range series.Corrections {
		candidate, ok := semanticCandidateByDigest(series, correction.ResultDigest)
		if !ok || correction.RunID != runID || correction.Action != action || correction.DispatchID != candidate.DispatchID || correction.RequirementRevision != candidate.RequirementRevision {
			return fail("correction binding is invalid")
		}
		if correction.Decision != "CONFIRM" && correction.Decision != "DISMISS" && correction.Decision != "RESEVERITIZE" && correction.Decision != "INVALIDATE_FINDING" && correction.Decision != "INVALIDATE_RESULT" {
			return fail("correction decision %q is invalid", correction.Decision)
		}
		if correction.Decision != "INVALIDATE_RESULT" {
			if _, ok := semanticFindingByID(candidate, correction.FindingID); !ok {
				return fail("correction finding target is invalid")
			}
			if correction.Supersedes == "" || decisionTargets[correction.Supersedes] != correction.ResultDigest+":"+correction.FindingID {
				return fail("correction does not supersede the prior decision for its exact target")
			}
			if correction.Invalidity != "" {
				return fail("finding correction carries whole-result invalidity")
			}
			if correction.Decision == "RESEVERITIZE" && correction.Severity != "P0" && correction.Severity != "P1" && correction.Severity != "P2" && correction.Severity != "P3" {
				return fail("correction severity is invalid")
			}
			if correction.Decision != "RESEVERITIZE" && correction.Severity != "" {
				return fail("non-severity correction carries a severity")
			}
			previousValue := decisionNewValues[correction.Supersedes]
			previousSeverity := reviewDecisionValueSeverity(previousValue)
			expectedNew := reviewDecisionValue(correction.Decision, correctedSeverity(ReviewCorrectionInput{Decision: correction.Decision, Severity: correction.Severity}, previousSeverity))
			if correction.OldValue != previousValue || correction.NewValue != expectedNew {
				return fail("correction old/new values do not match the superseded decision")
			}
		} else if correction.FindingID != "" || correction.Supersedes != "" || correction.Severity != "" || (correction.Invalidity != "INVALID" && correction.Invalidity != "INCOMPLETE" && correction.Invalidity != "STALE" && correction.Invalidity != "MISBOUND") {
			return fail("whole-result invalidation receipt is malformed")
		} else if correction.OldValue != "VALID_RESULT:"+candidate.RawStatus || correction.NewValue != "INVALID_RESULT:"+correction.Invalidity {
			return fail("whole-result invalidation old/new values are inconsistent")
		}
		if !correction.UserAuthorized || correction.Source == "" || strings.TrimSpace(correction.Reason) == "" || correction.OldValue == "" || correction.NewValue == "" || correction.EffectiveRevision <= 0 || correction.EffectiveRevision > series.EffectiveReviewStatus.Revision {
			return fail("correction authorization/source is incomplete")
		}
		copy := correction
		copy.ID = ""
		if correction.ID != "review-correction-"+shortDigest(digestValue(copy)) || decisionIDs[correction.ID] || decisionRevisions[correction.EffectiveRevision] {
			return fail("correction identity is invalid or duplicated")
		}
		if correction.Supersedes != "" && !decisionIDs[correction.Supersedes] {
			return fail("correction supersedes unknown or non-prior decision %s", correction.Supersedes)
		}
		decisionIDs[correction.ID] = true
		decisionTargets[correction.ID] = correction.ResultDigest + ":" + correction.FindingID
		decisionNewValues[correction.ID] = correction.NewValue
		decisionRevisions[correction.EffectiveRevision] = true
	}
	for _, authorization := range series.Authorizations {
		validResults := 0
		for _, resultDigest := range authorization.RecordedResultDigests {
			if !candidateExcluded(series, resultDigest) {
				validResults++
			}
		}
		if validResults > 1 {
			return fail("one extra-round authorization backs more than one valid result")
		}
	}
	projectedCompleted := completedSemanticReviewRounds(series)
	if series.Completed != projectedCompleted {
		return fail("completed=%d does not match accepted candidate projection %d", series.Completed, projectedCompleted)
	}
	currentLimit := series.AutomaticLimit + len(series.Authorizations)
	remainingAutomatic := maxInt(0, series.AutomaticLimit-series.Completed)
	remainingCapacity := maxInt(0, currentLimit-series.Completed)
	extraConsumed := occupiedExtraAuthorizations(series)
	if series.CurrentLimit != currentLimit || series.RemainingAutomatic != remainingAutomatic || series.RemainingCapacity != remainingCapacity || series.ExtraAuthorized != len(series.Authorizations) || series.ExtraConsumed != extraConsumed {
		return fail("capacity projection does not match candidates/authorizations")
	}
	expectedNext := preDevelopmentNextRoundRequirement(series)
	if series.NextRoundRequirement != expectedNext {
		return fail("next-round requirement is %q, want %q", series.NextRoundRequirement, expectedNext)
	}
	if series.Completed > currentLimit {
		return fail("completed rounds exceed the effective limit")
	}
	if occupiedExtraAuthorizations(series) < maxInt(0, series.Completed-series.AutomaticLimit) {
		return fail("completed extra rounds are not backed by distinct action authorizations")
	}
	if err := validateEffectiveReviewProjection(action, series); err != nil {
		return fail("%v", err)
	}
	return nil
}

func validateEffectiveReviewProjection(action string, series PreDevelopmentReviewSeries) error {
	effective := series.EffectiveReviewStatus
	if effective.Status != "PENDING" && effective.Status != "PASS" && effective.Status != "FAIL" {
		return fmt.Errorf("effective status %q is invalid", effective.Status)
	}
	if effective.Source == "" || effective.Revision < 0 {
		return fmt.Errorf("effective status source/revision is incomplete")
	}
	if effective.Status == "PENDING" {
		if effective.ResultDigest != "" {
			if _, ok := semanticCandidateByDigest(series, effective.ResultDigest); !ok {
				return fmt.Errorf("pending effective status references an unknown raw result")
			}
		}
		if len(effective.BlockingFindingIDs) != 0 || len(effective.AdvisoryFindingIDs) != 0 {
			return fmt.Errorf("pending effective status retains active finding projections")
		}
		return nil
	}
	candidate, ok := semanticCandidateByDigest(series, effective.ResultDigest)
	if !ok || candidateExcluded(series, candidate.ResultDigest) || candidate.RequirementRevision != effective.RequirementRevision {
		return fmt.Errorf("effective result binding is missing, excluded, or inconsistent")
	}
	expected := projectEffectiveReviewStatus(series, candidate, effective.Revision)
	if expected.Status != effective.Status || expected.RawStatus != effective.RawStatus || expected.Source != effective.Source || !sameOrderedStrings(expected.BlockingFindingIDs, effective.BlockingFindingIDs) || !sameOrderedStrings(expected.AdvisoryFindingIDs, effective.AdvisoryFindingIDs) {
		return fmt.Errorf("effective status does not match the deterministic raw/adjudication projection")
	}
	if len(effective.DownstreamImpact) != 0 && (effective.Status != "FAIL" || !sameOrderedStrings(effective.DownstreamImpact, semanticReviewDownstreamImpact(action))) {
		return fmt.Errorf("effective downstream impact is inconsistent")
	}
	return nil
}

func authorizePreDevelopmentReviewDispatch(state *RunState, action, dispatchID string, userRequested bool, reason string) error {
	if !isPreDevelopmentReviewAction(action) {
		return nil
	}
	if err := validatePreDevelopmentReviewState(*state); err != nil {
		return err
	}
	series := state.PreDevelopmentReviewSeries[action]
	refreshPreDevelopmentReviewSeries(&series)
	if series.Completed < series.AutomaticLimit {
		state.PreDevelopmentReviewSeries[action] = series
		return nil
	}
	if authorizationIndex := availableReviewAuthorization(series); authorizationIndex >= 0 {
		authorization := series.Authorizations[authorizationIndex]
		authorization.DispatchIDs = appendUniqueString(authorization.DispatchIDs, dispatchID)
		series.Authorizations[authorizationIndex] = authorization
		refreshPreDevelopmentReviewSeries(&series)
		state.PreDevelopmentReviewSeries[action] = series
		return nil
	}
	if !userRequested {
		return fmt.Errorf("%s has completed %d semantic review rounds and exhausted its current limit %d; the next round requires --user-requested with a non-empty --user-reason", action, series.Completed, series.CurrentLimit)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("%s extra semantic review round authorization requires a non-empty --user-reason", action)
	}
	authorization := ReviewRoundAuthorization{
		Action:                action,
		Source:                "USER_REQUESTED",
		Reason:                reason,
		DispatchIDs:           []string{dispatchID},
		RecordedResultDigests: []string{},
	}
	authorization.ID = "review-authorization-" + shortDigest(digestValue(struct {
		RunID   string
		Action  string
		Reason  string
		Ordinal int
	}{state.RunID, action, reason, len(series.Authorizations) + 1}))
	series.Authorizations = append(series.Authorizations, authorization)
	refreshPreDevelopmentReviewSeries(&series)
	state.PreDevelopmentReviewSeries[action] = series
	return nil
}

func requirePreDevelopmentReviewPreparation(state RunState, action string, userRequested bool, reason string) error {
	if !isPreDevelopmentReviewAction(action) {
		return nil
	}
	if err := validatePreDevelopmentReviewState(state); err != nil {
		return err
	}
	series := state.PreDevelopmentReviewSeries[action]
	refreshPreDevelopmentReviewSeries(&series)
	// A user-confirmed P0/P1 is a requirement defect to repair, not a reason to
	// sample another reviewer against identical inputs. Only a changed bound
	// requirement revision can open the fresh semantic review that clears this
	// marker; --user-requested controls capacity, not this evidence precondition.
	if state.NeedsReReview[action] != "" && series.EffectiveReviewStatus.RequirementRevision == state.RequirementRevision {
		return fmt.Errorf("confirmed P0/P1 finding %q requires a changed and reconfirmed requirement revision before a fresh %s", state.NeedsReReview[action], action)
	}
	if series.Completed < series.CurrentLimit || availableReviewAuthorization(series) >= 0 {
		return nil
	}
	if !userRequested {
		return fmt.Errorf("%s has completed %d semantic review rounds and exhausted its current limit %d; the next round requires --user-requested with a non-empty --user-reason", action, series.Completed, series.CurrentLimit)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%s extra semantic review round authorization requires a non-empty --user-reason", action)
	}
	return nil
}

func reviewAuthorizationForDispatch(state RunState, action, dispatchID string) string {
	series := state.PreDevelopmentReviewSeries[action]
	for _, authorization := range series.Authorizations {
		for _, bound := range authorization.DispatchIDs {
			if bound == dispatchID {
				return authorization.ID
			}
		}
	}
	return ""
}

func recordSemanticReviewCandidate(state *RunState, action string, dispatch PreparedDispatch, result ActionResult, verifications ...OperatorVerification) (ActionResult, error) {
	if !isPreDevelopmentReviewAction(action) || (result.Status != "PASS" && result.Status != "FAIL") {
		return result, nil
	}
	verification := legacyOperatorVerification()
	if len(verifications) > 1 {
		return ActionResult{}, fmt.Errorf("exactly one operator verification record is allowed")
	}
	if len(verifications) == 1 {
		verification = verifications[0]
	}
	verification, err := normalizeOperatorVerification(verification)
	if err != nil {
		return ActionResult{}, err
	}
	if err := validatePreDevelopmentReviewState(*state); err != nil {
		return ActionResult{}, err
	}
	series := state.PreDevelopmentReviewSeries[action]
	previousEffective := series.EffectiveReviewStatus
	findings := make([]SemanticReviewFinding, 0, len(result.Findings))
	for _, raw := range result.Findings {
		finding := SemanticReviewFinding{Severity: raw.Severity, Message: raw.Message, Locations: append([]string{}, raw.Locations...)}
		finding.Digest = semanticFindingDigest(finding)
		finding.ID = "finding-" + shortDigest(finding.Digest)
		findings = append(findings, finding)
	}
	resultDigest := semanticReviewCandidateDigest(state.RunID, SemanticReviewCandidate{Action: action, DispatchID: dispatch.ID, RequirementRevision: dispatch.RequirementRevision, RawStatus: result.Status, Message: result.Message, Findings: findings})
	for _, existing := range series.RawCandidates {
		if existing.ResultDigest == resultDigest {
			return ActionResult{}, fmt.Errorf("semantic review result %s is already recorded", resultDigest)
		}
	}
	authorizationID := dispatch.ReviewAuthorizationID
	if series.Completed >= series.AutomaticLimit && authorizationID == "" {
		return ActionResult{}, fmt.Errorf("%s result exceeds the automatic review limit without an action-specific authorization", action)
	}
	if authorizationID != "" {
		index := authorizationIndex(series, authorizationID)
		if index < 0 || !containsExactString(series.Authorizations[index].DispatchIDs, dispatch.ID) {
			return ActionResult{}, fmt.Errorf("%s result is not bound to a valid review authorization", action)
		}
		authorization := series.Authorizations[index]
		authorization.RecordedResultDigests = appendUniqueString(authorization.RecordedResultDigests, resultDigest)
		series.Authorizations[index] = authorization
	}
	candidate := SemanticReviewCandidate{
		ID:                   "review-result-" + shortDigest(resultDigest),
		Action:               action,
		DispatchID:           dispatch.ID,
		ResultDigest:         resultDigest,
		RequirementRevision:  dispatch.RequirementRevision,
		RawStatus:            result.Status,
		Message:              result.Message,
		Findings:             findings,
		Completeness:         "COMPLETE",
		AuthorizationID:      authorizationID,
		OperatorVerification: verification,
	}
	series.RawCandidates = append(series.RawCandidates, candidate)
	series.EffectiveReviewStatus = projectEffectiveReviewStatus(series, candidate, series.EffectiveReviewStatus.Revision+1)
	refreshPreDevelopmentReviewSeries(&series)
	if previousEffective.Status == "PASS" && series.EffectiveReviewStatus.Status == "FAIL" {
		series.EffectiveReviewStatus.DownstreamImpact = semanticReviewDownstreamImpact(action)
	} else if previousEffective.Status == "FAIL" && series.EffectiveReviewStatus.Status == "FAIL" && len(previousEffective.DownstreamImpact) != 0 {
		series.EffectiveReviewStatus.DownstreamImpact = append([]string{}, previousEffective.DownstreamImpact...)
	}
	state.PreDevelopmentReviewSeries[action] = series
	return projectedActionResult(candidate, series.EffectiveReviewStatus), nil
}

func RecordReviewCorrection(root, packageRoot, runID string, input ReviewCorrectionInput) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentCatalog(*state, packageRoot); err != nil {
			return err
		}
		return applyReviewCorrection(state, input)
	})
}

func applyReviewCorrection(state *RunState, input ReviewCorrectionInput) error {
	input.Action = strings.TrimSpace(input.Action)
	input.Decision = strings.ToUpper(strings.TrimSpace(input.Decision))
	input.Severity = strings.ToUpper(strings.TrimSpace(input.Severity))
	input.Invalidity = strings.ToUpper(strings.TrimSpace(input.Invalidity))
	input.Reason = strings.TrimSpace(input.Reason)
	input.Source = strings.TrimSpace(input.Source)
	if !isPreDevelopmentReviewAction(input.Action) {
		return fmt.Errorf("review correction action must be product-review or start-readiness")
	}
	if !input.UserAuthorized {
		return fmt.Errorf("review correction requires explicit user authorization")
	}
	if input.Reason == "" || input.Source == "" {
		return fmt.Errorf("review correction requires non-empty reason and authorization source")
	}
	if err := validatePreDevelopmentReviewState(*state); err != nil {
		return err
	}
	if state.NeedsReReview == nil {
		state.NeedsReReview = map[string]string{}
	}
	if state.ReReviewDispatch == nil {
		state.ReReviewDispatch = map[string]string{}
	}
	series := state.PreDevelopmentReviewSeries[input.Action]
	if input.ExpectedEffectiveRevision != series.EffectiveReviewStatus.Revision {
		return fmt.Errorf("review correction conflict: expected effective revision %d, current revision is %d", input.ExpectedEffectiveRevision, series.EffectiveReviewStatus.Revision)
	}
	candidate, ok := semanticCandidateByDigest(series, input.ResultDigest)
	if !ok || candidate.DispatchID != strings.TrimSpace(input.DispatchID) || candidate.RequirementRevision != strings.TrimSpace(input.RequirementRevision) {
		return fmt.Errorf("review correction binding does not match the raw candidate")
	}
	previousStatus := series.EffectiveReviewStatus.Status
	previousImpact := append([]string{}, series.EffectiveReviewStatus.DownstreamImpact...)
	nextRevision := series.EffectiveReviewStatus.Revision + 1
	if input.Decision == "INVALIDATE_RESULT" {
		invalidatesEffective := candidate.ResultDigest == series.EffectiveReviewStatus.ResultDigest
		if input.FindingID != "" {
			return fmt.Errorf("whole-result invalidation does not accept a finding id")
		}
		if input.Severity != "" || strings.TrimSpace(input.Supersedes) != "" {
			return fmt.Errorf("whole-result invalidation does not accept severity or supersedes")
		}
		if input.Invalidity != "INVALID" && input.Invalidity != "INCOMPLETE" && input.Invalidity != "STALE" && input.Invalidity != "MISBOUND" {
			return fmt.Errorf("whole-result invalidation must classify invalidity as INVALID, INCOMPLETE, STALE, or MISBOUND")
		}
		if candidateExcluded(series, candidate.ResultDigest) {
			return fmt.Errorf("review result %s is already invalidated", candidate.ResultDigest)
		}
		receipt := correctionReceipt(*state, candidate, input, nextRevision, "VALID_RESULT:"+candidate.RawStatus, "INVALID_RESULT:"+input.Invalidity)
		series.Corrections = append(series.Corrections, receipt)
		if invalidatesEffective {
			series.EffectiveReviewStatus = EffectiveReviewStatus{Status: "PENDING", RawStatus: candidate.RawStatus, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Source: receipt.ID, Revision: nextRevision, BlockingFindingIDs: []string{}, AdvisoryFindingIDs: []string{}, DownstreamImpact: semanticReviewDownstreamImpact(input.Action)}
		} else {
			series.EffectiveReviewStatus.Revision = nextRevision
		}
		refreshPreDevelopmentReviewSeries(&series)
		state.PreDevelopmentReviewSeries[input.Action] = series
		if invalidatesEffective {
			state.Actions[input.Action] = ActionResult{Status: "PENDING"}
			syncSemanticReviewLegacyProjection(state, input.Action)
			if previousStatus == "PASS" {
				invalidateSemanticReviewDependents(state, input.Action, receipt.ID)
			}
		}
		return nil
	}
	if candidate.ResultDigest != series.EffectiveReviewStatus.ResultDigest || candidate.RequirementRevision != state.RequirementRevision {
		return fmt.Errorf("review correction is stale: the target is not the current action result and requirement revision")
	}
	finding, ok := semanticFindingByID(candidate, strings.TrimSpace(input.FindingID))
	if !ok {
		return fmt.Errorf("review correction finding %q is not in the bound raw candidate", input.FindingID)
	}
	if input.Decision != "CONFIRM" && input.Decision != "DISMISS" && input.Decision != "RESEVERITIZE" && input.Decision != "INVALIDATE_FINDING" {
		return fmt.Errorf("review correction decision must be CONFIRM, DISMISS, RESEVERITIZE, INVALIDATE_FINDING, or INVALIDATE_RESULT")
	}
	if input.Invalidity != "" {
		return fmt.Errorf("finding disposition/correction does not accept whole-result invalidity")
	}
	if input.Decision == "RESEVERITIZE" && input.Severity != "P0" && input.Severity != "P1" && input.Severity != "P2" && input.Severity != "P3" {
		return fmt.Errorf("RESEVERITIZE requires --severity P0, P1, P2, or P3")
	}
	if input.Decision != "RESEVERITIZE" && input.Severity != "" {
		return fmt.Errorf("severity is accepted only for RESEVERITIZE")
	}
	latestID := latestFindingDecisionID(series, candidate.ResultDigest, finding.ID)
	if latestID == "" {
		if strings.TrimSpace(input.Supersedes) != "" {
			return fmt.Errorf("review correction cannot supersede %q because the finding has no prior adjudication", input.Supersedes)
		}
		adjudication := reviewAdjudication(*state, candidate, finding, input, nextRevision)
		series.Adjudications = append(series.Adjudications, adjudication)
	} else {
		if strings.TrimSpace(input.Supersedes) != latestID {
			return fmt.Errorf("review correction conflict: --supersedes must name current adjudication/correction %s", latestID)
		}
		previousDecision, previousSeverity, _ := effectiveFindingDecision(series, candidate.ResultDigest, finding)
		receipt := correctionReceipt(*state, candidate, input, nextRevision, reviewDecisionValue(previousDecision, previousSeverity), reviewDecisionValue(input.Decision, correctedSeverity(input, previousSeverity)))
		series.Corrections = append(series.Corrections, receipt)
	}
	series.EffectiveReviewStatus = projectEffectiveReviewStatus(series, candidate, nextRevision)
	if previousStatus == "PASS" && series.EffectiveReviewStatus.Status == "FAIL" {
		series.EffectiveReviewStatus.DownstreamImpact = semanticReviewDownstreamImpact(input.Action)
	} else if previousStatus == "FAIL" && series.EffectiveReviewStatus.Status == "FAIL" && len(previousImpact) != 0 {
		series.EffectiveReviewStatus.DownstreamImpact = previousImpact
	}
	refreshPreDevelopmentReviewSeries(&series)
	state.PreDevelopmentReviewSeries[input.Action] = series
	state.Actions[input.Action] = projectedActionResult(candidate, series.EffectiveReviewStatus)
	syncSemanticReviewLegacyProjection(state, input.Action)
	if previousStatus == "PASS" && series.EffectiveReviewStatus.Status == "FAIL" {
		invalidateSemanticReviewDependents(state, input.Action, series.EffectiveReviewStatus.Source)
	}
	return nil
}

// syncSemanticReviewLegacyProjection keeps the established prompt/re-review
// compatibility fields as a projection of the typed adjudication ledger. It
// writes at most one current disposition per finding, so an older confirm can
// never coexist with a later dismiss, and raw candidate severity can never
// override a corrected effective severity.
func syncSemanticReviewLegacyProjection(state *RunState, action string) {
	if state.SettledFindings == nil {
		state.SettledFindings = map[string][]SettledFinding{}
	}
	if state.NeedsReReview == nil {
		state.NeedsReReview = map[string]string{}
	}
	if state.ReReviewDispatch == nil {
		state.ReReviewDispatch = map[string]string{}
	}
	series := state.PreDevelopmentReviewSeries[action]
	candidate, ok := semanticCandidateByDigest(series, series.EffectiveReviewStatus.ResultDigest)
	if !ok || candidateExcluded(series, candidate.ResultDigest) {
		delete(state.SettledFindings, action)
		delete(state.NeedsReReview, action)
		delete(state.ReReviewDispatch, action)
		return
	}

	settled := []SettledFinding{}
	blockingMessage := ""
	for _, finding := range candidate.Findings {
		decision, severity, _ := effectiveFindingDecision(series, candidate.ResultDigest, finding)
		disposition := ""
		switch decision {
		case "CONFIRM":
			disposition = "confirm"
		case "DISMISS", "INVALIDATE_FINDING":
			disposition = "dismiss"
		case "RESEVERITIZE":
			disposition = "re-severity:" + severity
		}
		if disposition != "" {
			settled = append(settled, SettledFinding{Message: finding.Message, Disposition: disposition})
		}
		if blockingMessage == "" && (decision == "CONFIRM" || decision == "RESEVERITIZE") && (severity == "P0" || severity == "P1") {
			blockingMessage = finding.Message
		}
	}
	if len(settled) == 0 {
		delete(state.SettledFindings, action)
	} else {
		state.SettledFindings[action] = settled
	}
	if blockingMessage == "" {
		delete(state.NeedsReReview, action)
		delete(state.ReReviewDispatch, action)
	} else {
		state.NeedsReReview[action] = blockingMessage
		delete(state.ReReviewDispatch, action)
	}
}

// invalidateSemanticReviewDependents invalidates only evidence downstream of a
// product-review/start-readiness effective PASS that was later corrected to a
// blocking state. The corrected action and all immutable review ledgers remain
// intact. A product-review correction additionally stales start-readiness's
// effective projection, while a start-readiness correction leaves product
// review untouched.
func invalidateSemanticReviewDependents(state *RunState, action, source string) {
	if action == "product-review" {
		series := state.PreDevelopmentReviewSeries["start-readiness"]
		series.EffectiveReviewStatus.Status = "PENDING"
		series.EffectiveReviewStatus.Source = "UPSTREAM_CORRECTION:" + source
		series.EffectiveReviewStatus.Revision++
		series.EffectiveReviewStatus.BlockingFindingIDs = []string{}
		series.EffectiveReviewStatus.AdvisoryFindingIDs = []string{}
		series.EffectiveReviewStatus.DownstreamImpact = semanticReviewDownstreamImpact("start-readiness")
		state.PreDevelopmentReviewSeries["start-readiness"] = series
		state.Actions["start-readiness"] = ActionResult{Status: "PENDING"}
		delete(state.NeedsReReview, "start-readiness")
		delete(state.ReReviewDispatch, "start-readiness")
		delete(state.ReviewItemsByAction, "start-readiness")
	}

	// Slicing and route decisions, development, QA, gates, carry and seal
	// readiness all depend on both pre-development reviews. Preserve requirement
	// evidence, snapshots, raw review ledgers and the unrelated upstream review;
	// reopen only this dependent workflow surface.
	state.Slicing = nil
	state.RouteMode = ""
	state.SelectedGates = []string{}
	state.SkipAuthorizations = map[string]SkipAuthorization{}
	state.Actions["development-worker"] = ActionResult{Status: developmentPending}
	delete(state.Actions, "carry")
	state.QAReviewByMode = map[string]ActionResult{}
	state.QADesignByMode = map[string]ActionResult{}
	state.QADesignChangesByMode = map[string]QADesignChange{}
	if len(state.QACasesByMode) != 0 {
		reset := map[string][]QACase{}
		for mode, cases := range state.QACasesByMode {
			updated := make([]QACase, 0, len(cases))
			for _, testCase := range cases {
				testCase.ReviewStatus = "PENDING"
				testCase.ApprovedSource = ""
				updated = append(updated, testCase)
			}
			reset[mode] = updated
		}
		state.QACasesByMode = reset
	}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	if state.RequirementGuarantee != nil {
		state.RequirementGuarantee.ReviewsByMode = map[string]GuaranteeReviewRecord{}
		state.RequirementGuarantee.Report = RequirementGuaranteeReport{}
	}
	state.ExecutionScopes = map[string]QAExecutionScope{}
	state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	state.Carry = map[string]CarryResult{}
	for id := range state.Gates {
		state.Gates[id] = GateResult{Status: "PENDING"}
	}
	state.QAWorktree = ""
	state.SnapshotOverride = nil
	state.BlackboxReviewFails = 0
	state.PreRepairSnapshot = ""
	staleAllDispatches(state)
}

func settleSemanticReviewFindings(state *RunState, action string, confirm, dismiss []string) error {
	if err := validatePreDevelopmentReviewState(*state); err != nil {
		return err
	}
	series := state.PreDevelopmentReviewSeries[action]
	candidate, ok := semanticCandidateByDigest(series, series.EffectiveReviewStatus.ResultDigest)
	if !ok || candidate.RequirementRevision != state.RequirementRevision {
		return fmt.Errorf("settled finding target is stale or missing; record a current semantic review result first")
	}
	apply := func(message, decision string) error {
		var matched *SemanticReviewFinding
		for index := range candidate.Findings {
			if candidate.Findings[index].Message == strings.TrimSpace(message) {
				if matched != nil {
					return fmt.Errorf("finding message %q is ambiguous; use the exact-binding correction flags and stable finding id", message)
				}
				copy := candidate.Findings[index]
				matched = &copy
			}
		}
		if matched == nil {
			return fmt.Errorf("finding %q is not in the recorded %s result", strings.TrimSpace(message), action)
		}
		latest := latestFindingDecisionID(series, candidate.ResultDigest, matched.ID)
		input := ReviewCorrectionInput{
			Action:                    action,
			DispatchID:                candidate.DispatchID,
			ResultDigest:              candidate.ResultDigest,
			RequirementRevision:       candidate.RequirementRevision,
			FindingID:                 matched.ID,
			Decision:                  strings.ToUpper(decision),
			Supersedes:                latest,
			Reason:                    "user " + strings.ToLower(decision) + "ed finding: " + matched.Message,
			Source:                    "workflow settle-findings",
			UserAuthorized:            true,
			ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision,
		}
		if err := applyReviewCorrection(state, input); err != nil {
			return err
		}
		series = state.PreDevelopmentReviewSeries[action]
		return nil
	}
	for _, message := range confirm {
		if err := apply(message, "CONFIRM"); err != nil {
			return err
		}
	}
	for _, message := range dismiss {
		if err := apply(message, "DISMISS"); err != nil {
			return err
		}
	}
	return nil
}

func markPreDevelopmentReviewsStale(state *RunState, source string) {
	for _, action := range preDevelopmentReviewActions {
		series := state.PreDevelopmentReviewSeries[action]
		if series.Action == "" {
			continue
		}
		series.EffectiveReviewStatus.Status = "PENDING"
		series.EffectiveReviewStatus.Source = source
		series.EffectiveReviewStatus.Revision++
		series.EffectiveReviewStatus.BlockingFindingIDs = []string{}
		series.EffectiveReviewStatus.AdvisoryFindingIDs = []string{}
		series.EffectiveReviewStatus.DownstreamImpact = semanticReviewDownstreamImpact(action)
		state.PreDevelopmentReviewSeries[action] = series
	}
}

func refreshPreDevelopmentReviewSeries(series *PreDevelopmentReviewSeries) {
	series.Completed = completedSemanticReviewRounds(*series)
	series.CurrentLimit = series.AutomaticLimit + len(series.Authorizations)
	series.RemainingAutomatic = maxInt(0, series.AutomaticLimit-series.Completed)
	series.RemainingCapacity = maxInt(0, series.CurrentLimit-series.Completed)
	series.ExtraAuthorized = len(series.Authorizations)
	series.ExtraConsumed = occupiedExtraAuthorizations(*series)
	series.NextRoundRequirement = preDevelopmentNextRoundRequirement(*series)
}

func preDevelopmentNextRoundRequirement(series PreDevelopmentReviewSeries) string {
	if series.Completed < series.AutomaticLimit {
		return "AUTOMATIC_CAPACITY"
	}
	if availableReviewAuthorization(series) >= 0 {
		return "AUTHORIZED_EXTRA_ROUND"
	}
	return "USER_DECISION_REQUIRED"
}

func completedSemanticReviewRounds(series PreDevelopmentReviewSeries) int {
	count := 0
	for _, candidate := range series.RawCandidates {
		if candidate.Completeness == "COMPLETE" && !candidateExcluded(series, candidate.ResultDigest) {
			count++
		}
	}
	return count
}

func occupiedExtraAuthorizations(series PreDevelopmentReviewSeries) int {
	occupied := map[string]bool{}
	for _, candidate := range series.RawCandidates {
		if candidate.AuthorizationID != "" && !candidateExcluded(series, candidate.ResultDigest) {
			occupied[candidate.AuthorizationID] = true
		}
	}
	return len(occupied)
}

func availableReviewAuthorization(series PreDevelopmentReviewSeries) int {
	occupied := map[string]bool{}
	for _, candidate := range series.RawCandidates {
		if candidate.AuthorizationID != "" && !candidateExcluded(series, candidate.ResultDigest) {
			occupied[candidate.AuthorizationID] = true
		}
	}
	for index, authorization := range series.Authorizations {
		if !occupied[authorization.ID] {
			return index
		}
	}
	return -1
}

func candidateExcluded(series PreDevelopmentReviewSeries, resultDigest string) bool {
	for _, correction := range series.Corrections {
		if correction.ResultDigest == resultDigest && correction.Decision == "INVALIDATE_RESULT" {
			return true
		}
	}
	return false
}

func projectEffectiveReviewStatus(series PreDevelopmentReviewSeries, candidate SemanticReviewCandidate, revision int) EffectiveReviewStatus {
	blocking, advisory := []string{}, []string{}
	source := candidate.ResultDigest
	for _, finding := range candidate.Findings {
		decision, severity, decisionSource := effectiveFindingDecision(series, candidate.ResultDigest, finding)
		if decisionSource != "" {
			source = decisionSource
		}
		if decision == "DISMISS" || decision == "INVALIDATE_FINDING" {
			continue
		}
		if severity == "P0" || severity == "P1" {
			blocking = append(blocking, finding.ID)
		} else {
			advisory = append(advisory, finding.ID)
		}
	}
	status := "PASS"
	if len(blocking) != 0 {
		status = "FAIL"
	}
	return EffectiveReviewStatus{Status: status, RawStatus: candidate.RawStatus, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, Source: source, Revision: revision, BlockingFindingIDs: blocking, AdvisoryFindingIDs: advisory, DownstreamImpact: []string{}}
}

func effectiveFindingDecision(series PreDevelopmentReviewSeries, resultDigest string, finding SemanticReviewFinding) (decision, severity, source string) {
	severity = finding.Severity
	latestRevision := -1
	for _, adjudication := range series.Adjudications {
		if adjudication.ResultDigest == resultDigest && adjudication.FindingID == finding.ID && adjudication.EffectiveRevision > latestRevision {
			decision, source, latestRevision = adjudication.Decision, adjudication.ID, adjudication.EffectiveRevision
			if adjudication.Severity != "" {
				severity = adjudication.Severity
			}
		}
	}
	for _, correction := range series.Corrections {
		if correction.ResultDigest == resultDigest && correction.FindingID == finding.ID && correction.EffectiveRevision > latestRevision {
			decision, source, latestRevision = correction.Decision, correction.ID, correction.EffectiveRevision
			if correction.Severity != "" {
				severity = correction.Severity
			}
		}
	}
	return decision, severity, source
}

func reviewAdjudication(state RunState, candidate SemanticReviewCandidate, finding SemanticReviewFinding, input ReviewCorrectionInput, revision int) ReviewAdjudication {
	adjudication := ReviewAdjudication{RunID: state.RunID, Action: input.Action, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: finding.ID, Decision: input.Decision, Severity: input.Severity, OldValue: reviewDecisionValue("", finding.Severity), NewValue: reviewDecisionValue(input.Decision, correctedSeverity(input, finding.Severity)), Reason: input.Reason, Source: input.Source, UserAuthorized: input.UserAuthorized, EffectiveRevision: revision}
	adjudication.ID = "review-adjudication-" + shortDigest(digestValue(adjudication))
	return adjudication
}

func correctionReceipt(state RunState, candidate SemanticReviewCandidate, input ReviewCorrectionInput, revision int, oldValue, newValue string) ReviewCorrectionReceipt {
	receipt := ReviewCorrectionReceipt{RunID: state.RunID, Action: input.Action, DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: candidate.RequirementRevision, FindingID: input.FindingID, Decision: input.Decision, Severity: input.Severity, Invalidity: input.Invalidity, Supersedes: input.Supersedes, OldValue: oldValue, NewValue: newValue, Reason: input.Reason, Source: input.Source, UserAuthorized: input.UserAuthorized, EffectiveRevision: revision}
	receipt.ID = "review-correction-" + shortDigest(digestValue(receipt))
	return receipt
}

func correctedSeverity(input ReviewCorrectionInput, original string) string {
	if input.Decision == "RESEVERITIZE" {
		return input.Severity
	}
	return original
}

func reviewDecisionValue(decision, severity string) string {
	if decision == "" {
		return "UNADJUDICATED:" + severity
	}
	if severity != "" {
		return decision + ":" + severity
	}
	return decision
}

func reviewDecisionValueSeverity(value string) string {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func latestFindingDecisionID(series PreDevelopmentReviewSeries, resultDigest, findingID string) string {
	latestID := ""
	latestRevision := -1
	for _, adjudication := range series.Adjudications {
		if adjudication.ResultDigest == resultDigest && adjudication.FindingID == findingID && adjudication.EffectiveRevision > latestRevision {
			latestID, latestRevision = adjudication.ID, adjudication.EffectiveRevision
		}
	}
	for _, correction := range series.Corrections {
		if correction.ResultDigest == resultDigest && correction.FindingID == findingID && correction.EffectiveRevision > latestRevision {
			latestID, latestRevision = correction.ID, correction.EffectiveRevision
		}
	}
	return latestID
}

func semanticCandidateByDigest(series PreDevelopmentReviewSeries, digest string) (SemanticReviewCandidate, bool) {
	for _, candidate := range series.RawCandidates {
		if candidate.ResultDigest == strings.TrimSpace(digest) {
			return candidate, true
		}
	}
	return SemanticReviewCandidate{}, false
}

func semanticFindingByID(candidate SemanticReviewCandidate, id string) (SemanticReviewFinding, bool) {
	for _, finding := range candidate.Findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return SemanticReviewFinding{}, false
}

func authorizationIndex(series PreDevelopmentReviewSeries, id string) int {
	for index, authorization := range series.Authorizations {
		if authorization.ID == id {
			return index
		}
	}
	return -1
}

func projectedActionResult(candidate SemanticReviewCandidate, effective EffectiveReviewStatus) ActionResult {
	findings := make([]Finding, 0, len(candidate.Findings))
	for _, finding := range candidate.Findings {
		findings = append(findings, Finding{Severity: finding.Severity, Message: finding.Message, Locations: append([]string{}, finding.Locations...)})
	}
	return ActionResult{Status: effective.Status, Message: candidate.Message, Findings: findings, DispatchID: candidate.DispatchID}
}

func semanticReviewDownstreamImpact(action string) []string {
	if action == "product-review" {
		return []string{"start-readiness", "slicing", "route", "development", "qa", "gates", "seal"}
	}
	return []string{"slicing", "route", "development", "qa", "gates", "seal"}
}

func digestValue(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func semanticFindingDigest(finding SemanticReviewFinding) string {
	locations := finding.Locations
	if len(locations) == 0 {
		locations = nil
	}
	return digestValue(struct {
		Severity  string
		Message   string
		Locations []string
	}{finding.Severity, finding.Message, locations})
}

func semanticReviewCandidateDigest(runID string, candidate SemanticReviewCandidate) string {
	return digestValue(struct {
		RunID               string
		Action              string
		DispatchID          string
		RequirementRevision string
		Status              string
		Message             string
		Findings            []SemanticReviewFinding
	}{runID, candidate.Action, candidate.DispatchID, candidate.RequirementRevision, candidate.RawStatus, candidate.Message, candidate.Findings})
}

func shortDigest(digest string) string {
	if len(digest) <= 16 {
		return digest
	}
	return digest[:16]
}

func appendUniqueString(values []string, value string) []string {
	if containsExactString(values, value) {
		return values
	}
	return append(values, value)
}

func containsExactString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
