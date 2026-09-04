package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const defaultDispatchMaxAttempts = 2

const (
	FindingCategoryRequirementConflict = "REQUIREMENT_CONFLICT"
	FindingCategoryResponsibility      = "RESPONSIBILITY_OR_DEPENDENCY"
	FindingCategoryMissingEntry        = "MISSING_CORE_ACCEPTANCE_ENTRY"
	FindingCategoryImplementation      = "IMPLEMENTATION_DETAIL"
	FindingCategoryOther               = "OTHER"
)

var findingCategories = map[string]bool{
	FindingCategoryRequirementConflict: true,
	FindingCategoryResponsibility:      true,
	FindingCategoryMissingEntry:        true,
	FindingCategoryImplementation:      true,
	FindingCategoryOther:               true,
}

var blockingStartReadinessCategories = map[string]bool{
	FindingCategoryRequirementConflict: true,
	FindingCategoryResponsibility:      true,
	FindingCategoryMissingEntry:        true,
}

// DispatchObligation is the bounded retry owner for one external TaskKey.
// AttemptsUsed is advanced when a dispatch is prepared, so an abandoned ticket
// cannot silently restore capacity. MaxAttempts starts at two (initial plus one
// retry) and may be increased exactly once by AuthorizeDispatchRetry.
type DispatchObligation struct {
	TaskKey                string   `json:"taskKey"`
	Signature              string   `json:"signature"`
	TargetKind             string   `json:"targetKind"`
	Target                 string   `json:"target"`
	Mode                   string   `json:"mode,omitempty"`
	MaxAttempts            int      `json:"maxAttempts"`
	AttemptsUsed           int      `json:"attemptsUsed"`
	DispatchIDs            []string `json:"dispatchIds"`
	Status                 string   `json:"status"`
	LastFailure            string   `json:"lastFailure,omitempty"`
	NextStep               string   `json:"nextStep"`
	ExtraAttemptAuthorized bool     `json:"extraAttemptAuthorized,omitempty"`
	AuthorizationReason    string   `json:"authorizationReason,omitempty"`
	BasisDigest            string   `json:"basisDigest"`
}

// RejectedResultReceipt is the sole durable effect of an invalid reviewer
// result. It binds the rejected fields to the exact dispatch attempt and closes
// that attempt without creating a semantic result.
type RejectedResultReceipt struct {
	ID               string   `json:"id"`
	DispatchID       string   `json:"dispatchId"`
	Attempt          int      `json:"attempt"`
	TaskKey          string   `json:"taskKey"`
	Signature        string   `json:"signature"`
	TargetKind       string   `json:"targetKind"`
	Target           string   `json:"target"`
	Mode             string   `json:"mode,omitempty"`
	Reason           string   `json:"reason"`
	RejectedFields   []string `json:"rejectedFields"`
	InvalidLocations []string `json:"invalidLocations,omitempty"`
}

type ReviewDeltaSource struct {
	Action    string `json:"action"`
	FindingID string `json:"findingId"`
	Decision  string `json:"decision"`
}

type ReviewDeltaHunk struct {
	ID               string   `json:"id"`
	Affected         []string `json:"affected"`
	SourceFindingIDs []string `json:"sourceFindingIds"`
	Disposition      string   `json:"disposition"`
	Impact           string   `json:"impact"`
	Verification     string   `json:"verification"`
}

// ReviewDeltaPackage is produced by the main agent from a base text captured
// before editing. The CLI validates bindings and the complete candidate but
// deliberately does not store or reconstruct the base text or diff.
type ReviewDeltaPackage struct {
	BaseRevision        string              `json:"baseRevision"`
	CandidateRevision   string              `json:"candidateRevision"`
	Digest              string              `json:"digest"`
	Sources             []ReviewDeltaSource `json:"sources"`
	Hunks               []ReviewDeltaHunk   `json:"hunks"`
	AffectedActions     []string            `json:"affectedActions,omitempty"`
	ConfirmationScope   string              `json:"confirmationScope"`
	UserConfirmation    string              `json:"userConfirmation"`
	ImpactPreviewDigest string              `json:"impactPreviewDigest,omitempty"`
}

type ReviewDeltaHunkReceipt struct {
	ID                 string   `json:"id"`
	Affected           []string `json:"affected"`
	SourceFindingIDs   []string `json:"sourceFindingIds"`
	VerificationDigest string   `json:"verificationDigest"`
}

type ReviewDeltaReceipt struct {
	ID                       string                   `json:"id"`
	BaseRevision             string                   `json:"baseRevision"`
	CandidateRevision        string                   `json:"candidateRevision"`
	DeltaDigest              string                   `json:"deltaDigest"`
	Sources                  []ReviewDeltaSource      `json:"sources"`
	HunkVerifications        []ReviewDeltaHunkReceipt `json:"hunkVerifications"`
	AffectedActions          []string                 `json:"affectedActions,omitempty"`
	ConfirmationScope        string                   `json:"confirmationScope"`
	ConfirmationSource       string                   `json:"confirmationSource"`
	ImpactPreviewDigest      string                   `json:"impactPreviewDigest,omitempty"`
	ImpactStateRevision      string                   `json:"impactStateRevision,omitempty"`
	ImpactConfirmationSource string                   `json:"impactConfirmationSource,omitempty"`
	Impact                   *RequirementImpactInput  `json:"impact,omitempty"`
}

type RequirementImpactInput struct {
	ProductGoal        string `json:"productGoal,omitempty"`
	Scope              string `json:"scope,omitempty"`
	ExternalBehavior   string `json:"externalBehavior,omitempty"`
	AcceptanceCriteria string `json:"acceptanceCriteria,omitempty"`
}

type RequirementImpactPreview struct {
	BaseRevision                 string                 `json:"baseRevision"`
	CandidateRevision            string                 `json:"candidateRevision"`
	StateRevision                string                 `json:"stateRevision"`
	Impact                       RequirementImpactInput `json:"impact"`
	InvalidatedPasses            []string               `json:"invalidatedPasses"`
	RollbackSteps                []string               `json:"rollbackSteps"`
	ProductReviewCompleted       int                    `json:"productReviewCompleted"`
	ProductReviewRemaining       int                    `json:"productReviewRemaining"`
	ProductReviewNextRequirement string                 `json:"productReviewNextRequirement"`
	ReviewDeltaDigest            string                 `json:"reviewDeltaDigest,omitempty"`
	Digest                       string                 `json:"digest"`
}

type RequirementImpactAuthorization struct {
	PreviewDigest    string
	StateRevision    string
	UserConfirmation string
	Impact           RequirementImpactInput
}

// ResultContractError is intentionally narrow: only errors in the reviewer
// payload itself become REJECTED_INVALID_RESULT. Binding, lifecycle, transition,
// and native snapshot errors remain ordinary zero-write command failures.
type ResultContractError struct {
	Reason           string
	Fields           []string
	InvalidLocations []string
}

func (e *ResultContractError) Error() string { return e.Reason }

type committedStateError struct{ err error }

func (e *committedStateError) Error() string { return e.err.Error() }
func (e *committedStateError) Unwrap() error { return e.err }

func commitStateAndFail(err error) error { return &committedStateError{err: err} }

func strictFinding(input FindingInput, actionID string, gateResult bool, root string, state RunState) (Finding, error) {
	severity := strings.ToUpper(strings.TrimSpace(input.Severity))
	category := strings.ToUpper(strings.TrimSpace(input.Category))
	finding := Finding{
		Severity: severity, SeverityRationale: strings.TrimSpace(input.SeverityRationale),
		Category: category, Requirement: strings.TrimSpace(input.Requirement),
		NormalPath: strings.TrimSpace(input.NormalPath), ActualError: strings.TrimSpace(input.ActualError),
		DeferralImpact: strings.TrimSpace(input.DeferralImpact), Message: strings.TrimSpace(input.Message),
		Locations: append([]string{}, input.Locations...), Verification: input.Verification,
	}
	finding.Verification.Scope = strings.TrimSpace(finding.Verification.Scope)
	finding.Verification.Severity = strings.TrimSpace(finding.Verification.Severity)
	finding.Verification.Causality = strings.TrimSpace(finding.Verification.Causality)
	finding.Verification.Reproduction = strings.TrimSpace(finding.Verification.Reproduction)
	finding.Verification.Classification = strings.TrimSpace(finding.Verification.Classification)
	var missing []string
	if actionID == "start-readiness" {
		if !findingCategories[category] {
			missing = append(missing, "category")
		}
		if (severity == "P0" || severity == "P1") && !blockingStartReadinessCategories[category] {
			return Finding{}, &ResultContractError{Reason: fmt.Sprintf("start-readiness category %q cannot carry %s; implementation/detail findings are at most P2", category, severity), Fields: []string{"category", "severity"}}
		}
	}
	if severity == "P0" || severity == "P1" {
		for name, value := range map[string]string{
			"severityRationale":         finding.SeverityRationale,
			"requirement":               finding.Requirement,
			"normalPath":                finding.NormalPath,
			"deferralImpact":            finding.DeferralImpact,
			"verification.scope":        finding.Verification.Scope,
			"verification.severity":     finding.Verification.Severity,
			"verification.causality":    finding.Verification.Causality,
			"verification.reproduction": finding.Verification.Reproduction,
		} {
			if value == "" {
				missing = append(missing, name)
			}
		}
		if actionID == "start-readiness" && finding.Verification.Classification == "" {
			missing = append(missing, "verification.classification")
		}
		if len(finding.Locations) == 0 {
			missing = append(missing, "locations")
		}
		if gateResult && finding.ActualError == "" {
			missing = append(missing, "actualError")
		}
		if rationalePlaceholder(finding.SeverityRationale) {
			missing = append(missing, "severityRationale(non-placeholder)")
		}
		if !requirementExists(state, finding.Requirement) {
			missing = append(missing, "requirement(valid binding)")
		}
	}
	invalidLocations := []string{}
	for _, location := range finding.Locations {
		if err := validateFindingLocation(location); err != nil {
			invalidLocations = append(invalidLocations, strings.TrimSpace(location))
			continue
		}
		if err := validateBoundFindingLocation(root, location); err != nil {
			invalidLocations = append(invalidLocations, strings.TrimSpace(location))
		}
	}
	if len(missing) != 0 || len(invalidLocations) != 0 {
		sort.Strings(missing)
		sort.Strings(invalidLocations)
		return Finding{}, &ResultContractError{Reason: "finding result contract is incomplete or misbound", Fields: missing, InvalidLocations: invalidLocations}
	}
	finding.ID = findingStableID(finding)
	return finding, nil
}

func validateStrictSemanticResult(actionID, status, message string, findings []FindingInput, gateResult bool, root string, state RunState) (string, []Finding, error) {
	normalized, _, err := validateSemanticResult(actionID, status, message, findings, gateResult)
	if err != nil {
		return "", nil, &ResultContractError{Reason: err.Error(), Fields: []string{"result"}}
	}
	converted := make([]Finding, 0, len(findings))
	for _, input := range findings {
		finding, err := strictFinding(input, actionID, gateResult, root, state)
		if err != nil {
			return "", nil, err
		}
		converted = append(converted, finding)
	}
	return normalized, converted, nil
}

func rationalePlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "" || normalized == "p0" || normalized == "p1" || normalized == "item fail" || normalized == "requirement violated" || normalized == "violates requirement"
}

func requirementExists(state RunState, requirement string) bool {
	if strings.TrimSpace(requirement) == "" {
		return false
	}
	if state.RequirementGuarantee == nil || state.RequirementGuarantee.Projection == nil {
		return true
	}
	for _, item := range state.RequirementGuarantee.Projection.Requirements {
		if item.ID == requirement {
			return true
		}
	}
	return false
}

func validateBoundFindingLocation(root, location string) error {
	path, line := findingLocationParts(location)
	info, err := os.Stat(resolveFromRoot(root, path))
	if err != nil || info.IsDir() {
		return fmt.Errorf("finding location %s does not resolve to a repository file", location)
	}
	if line <= 0 {
		return nil
	}
	data, err := os.ReadFile(resolveFromRoot(root, path))
	if err != nil {
		return err
	}
	lines := 1 + strings.Count(string(data), "\n")
	if line > lines {
		return fmt.Errorf("finding location %s points past line %d", location, lines)
	}
	return nil
}

func findingLocationParts(location string) (string, int) {
	path := strings.TrimSpace(location)
	line := 0
	for count := 0; count < 2; count++ {
		index := strings.LastIndex(path, ":")
		if index <= 0 || !suffixIsDigits(path[index+1:]) {
			break
		}
		value, _ := strconv.Atoi(path[index+1:])
		line = value
		path = path[:index]
	}
	return filepath.ToSlash(filepath.Clean(path)), line
}

func findingStableID(finding Finding) string {
	copy := finding
	copy.ID = ""
	return "finding-" + shortDigest(digestValue(copy))
}

func rejectInvalidResult(state *RunState, dispatch PreparedDispatch, contractErr error) error {
	resultErr, ok := contractErr.(*ResultContractError)
	if !ok {
		resultErr = &ResultContractError{Reason: contractErr.Error(), Fields: []string{"result"}}
	}
	receipt := RejectedResultReceipt{
		DispatchID: dispatch.ID, Attempt: dispatch.Attempt, TaskKey: dispatch.TaskKey,
		Signature: dispatch.Signature, TargetKind: dispatch.TargetKind, Target: dispatch.Target,
		Mode: dispatch.Mode, Reason: resultErr.Reason,
		RejectedFields:   append([]string{}, resultErr.Fields...),
		InvalidLocations: append([]string{}, resultErr.InvalidLocations...),
	}
	sort.Strings(receipt.RejectedFields)
	sort.Strings(receipt.InvalidLocations)
	receipt.ID = "rejected-result-" + shortDigest(digestValue(receipt))
	for _, existing := range state.RejectedResults {
		if existing.ID == receipt.ID {
			return commitStateAndFail(fmt.Errorf("REJECTED_INVALID_RESULT %s: %s", receipt.ID, resultErr.Reason))
		}
	}
	state.RejectedResults = append(state.RejectedResults, receipt)
	finishDispatchAttempt(state, dispatch.ID, "REJECTED_INVALID_RESULT", resultErr.Reason)
	return commitStateAndFail(fmt.Errorf("REJECTED_INVALID_RESULT %s: %s", receipt.ID, resultErr.Reason))
}

func dispatchPromptRevision(state RunState, catalog PromptCatalog, targetKind, target string) string {
	key := targetKind + ":" + target
	if value := catalogPromptHashes(catalog)[key]; value != "" {
		return value
	}
	return state.BasePromptRevision
}

func qaCaseSetVersion(state RunState, mode string) string {
	if mode == "" {
		return digestValue(state.QACasesByMode)
	}
	return digestValue(state.qaCases(mode))
}

func dispatchWorkflowContext(state RunState, targetKind, target string) string {
	if targetKind == "action" && isPreDevelopmentReviewAction(target) {
		series := state.PreDevelopmentReviewSeries[target]
		return digestValue(struct {
			EffectiveRevision int
			EffectiveStatus   string
			Completed         int
			NeedsReReview     string
		}{series.EffectiveReviewStatus.Revision, series.EffectiveReviewStatus.Status, series.Completed, state.NeedsReReview[target]})
	}
	return ""
}

func dispatchIdentity(state RunState, catalog PromptCatalog, targetKind, target, mode, actionInput string) (string, string) {
	base := struct {
		TargetKind, Target, Mode, BaseSnapshot, CurrentSnapshot string
		RequirementRevision, CaseSetVersion, CatalogRevision    string
		PromptRevision, ActionInput, WorkflowContext            string
	}{targetKind, target, mode, state.BaseSnapshot, state.CurrentSnapshot, state.RequirementRevision, qaCaseSetVersion(state, mode), catalog.CatalogRevision, dispatchPromptRevision(state, catalog, targetKind, target), actionInput, dispatchWorkflowContext(state, targetKind, target)}
	taskKey := "task-" + shortDigest(digestValue(base))
	signature := "dispatch-signature-" + shortDigest(digestValue(struct {
		Base           any
		ActionIdentity string
	}{base, targetKind + ":" + target + ":" + mode}))
	return taskKey, signature
}

func duplicateDispatchPrompt(state RunState, signature string) (string, bool, error) {
	for _, dispatch := range state.Dispatches {
		if dispatch.Signature != signature {
			continue
		}
		switch dispatch.Status {
		case "OPEN", "CLAIMED":
			data, err := os.ReadFile(dispatch.PromptFile)
			if err != nil {
				return "", false, err
			}
			return string(data), true, nil
		case "COMPLETED":
			if dispatch.ResultStatus == "PASS" {
				return "", false, fmt.Errorf("dispatch signature %s already has an authoritative PASS on dispatch %s", signature, dispatch.ID)
			}
			if dispatch.ResultStatus == "FAIL" {
				return "", false, fmt.Errorf("dispatch signature %s already has an authoritative FAIL on dispatch %s; follow the existing %s remediation path", signature, dispatch.ID, dispatch.Target)
			}
		}
	}
	return "", false, nil
}

func registerDispatchAttempt(state *RunState, dispatchID, taskKey, signature, targetKind, target, mode string) (int, error) {
	if state.DispatchObligations == nil {
		state.DispatchObligations = map[string]DispatchObligation{}
	}
	obligation, exists := state.DispatchObligations[taskKey]
	if !exists {
		obligation = DispatchObligation{TaskKey: taskKey, Signature: signature, TargetKind: targetKind, Target: target, Mode: mode, MaxAttempts: defaultDispatchMaxAttempts, Status: "READY", NextStep: "prepare and execute the obligation", DispatchIDs: []string{}}
	}
	if obligation.Signature != signature || obligation.TargetKind != targetKind || obligation.Target != target || obligation.Mode != mode {
		return 0, fmt.Errorf("TaskKey %s conflicts with its existing authoritative obligation", taskKey)
	}
	if obligation.AttemptsUsed >= obligation.MaxAttempts {
		obligation.Status = "RETRY_EXHAUSTED"
		obligation.NextStep = "authorize one final attempt with workflow authorize-retry, or change the confirmed plan/binding to create a new TaskKey"
		obligation.BasisDigest = dispatchObligationDigest(obligation)
		state.DispatchObligations[taskKey] = obligation
		return 0, fmt.Errorf("TaskKey %s is RETRY_EXHAUSTED (%d/%d attempts used)", taskKey, obligation.AttemptsUsed, obligation.MaxAttempts)
	}
	obligation.AttemptsUsed++
	obligation.DispatchIDs = append(obligation.DispatchIDs, dispatchID)
	obligation.Status = "IN_FLIGHT"
	obligation.NextStep = "record this dispatch result"
	obligation.BasisDigest = dispatchObligationDigest(obligation)
	state.DispatchObligations[taskKey] = obligation
	return obligation.AttemptsUsed, nil
}

func finishDispatchAttempt(state *RunState, dispatchID, resultStatus, failure string) {
	dispatch := state.Dispatches[dispatchID]
	dispatch.ResultStatus = resultStatus
	switch resultStatus {
	case "RUNTIME_ERROR", "REJECTED_INVALID_RESULT":
		dispatch.Status = resultStatus
	default:
		dispatch.Status = "COMPLETED"
	}
	state.Dispatches[dispatchID] = dispatch
	if dispatch.TaskKey == "" {
		return
	}
	obligation := state.DispatchObligations[dispatch.TaskKey]
	switch resultStatus {
	case "PASS", "FAIL":
		obligation.Status = "COMPLETED"
		obligation.NextStep = "use the recorded authoritative result"
	case "RUNTIME_ERROR", "REJECTED_INVALID_RESULT":
		obligation.LastFailure = failure
		if obligation.AttemptsUsed >= obligation.MaxAttempts {
			obligation.Status = "RETRY_EXHAUSTED"
			if obligation.ExtraAttemptAuthorized {
				obligation.NextStep = "abort/cleanup, or confirm a plan that changes binding, snapshot, or responsibility and therefore creates a new TaskKey"
			} else {
				obligation.NextStep = "authorize one final attempt with workflow authorize-retry --task-key and the current state revision"
			}
		} else {
			obligation.Status = "RETRYABLE"
			obligation.NextStep = "prepare the same logical obligation again; it will reuse this TaskKey"
		}
	}
	obligation.BasisDigest = dispatchObligationDigest(obligation)
	state.DispatchObligations[dispatch.TaskKey] = obligation
}

func dispatchObligationDigest(obligation DispatchObligation) string {
	copy := obligation
	copy.BasisDigest = ""
	return digestValue(copy)
}

func AuthorizeDispatchRetry(root, packageRoot, runID, taskKey, stateRevision, reason string) (RunState, error) {
	return mutateRun(root, runID, func(state *RunState) error {
		if _, err := requireCurrentCatalog(*state, packageRoot); err != nil {
			return err
		}
		if strings.TrimSpace(stateRevision) == "" || stateRevision != state.StateIntegrity {
			return fmt.Errorf("retry authorization state revision is stale or missing")
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return fmt.Errorf("retry authorization requires a non-empty reason")
		}
		obligation, ok := state.DispatchObligations[strings.TrimSpace(taskKey)]
		if !ok {
			return fmt.Errorf("unknown TaskKey %q", taskKey)
		}
		if obligation.ExtraAttemptAuthorized {
			return fmt.Errorf("TaskKey %s already used its one additional attempt authorization", taskKey)
		}
		if obligation.Status != "RETRY_EXHAUSTED" || obligation.AttemptsUsed != obligation.MaxAttempts {
			return fmt.Errorf("TaskKey %s has not exhausted its current retry policy", taskKey)
		}
		obligation.MaxAttempts++
		obligation.ExtraAttemptAuthorized = true
		obligation.AuthorizationReason = reason
		obligation.Status = "RETRYABLE"
		obligation.NextStep = "prepare the authorized final attempt"
		obligation.BasisDigest = dispatchObligationDigest(obligation)
		state.DispatchObligations[obligation.TaskKey] = obligation
		return nil
	})
}

func LoadReviewDeltaPackage(path string) (ReviewDeltaPackage, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ReviewDeltaPackage{}, err
	}
	var result ReviewDeltaPackage
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return ReviewDeltaPackage{}, fmt.Errorf("review-delta JSON is invalid: %w", err)
	}
	return result, nil
}

func reviewDeltaDigest(input ReviewDeltaPackage) string {
	input.Digest = ""
	// The digest binds the complete change package shown to the user, not the
	// later response or (for a combined confirmation) the impact-preview digest
	// that itself binds this delta digest. Excluding those two fields avoids a
	// circular digest while the final receipt still binds both explicitly.
	input.UserConfirmation = ""
	input.ImpactPreviewDigest = ""
	return digestValue(input)
}

func PreviewReviewDelta(root, packageRoot, runID, source string, artifactPaths []string, input ReviewDeltaPackage) (ReviewDeltaPackage, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return ReviewDeltaPackage{}, err
	}
	if _, err := requireCurrentCatalog(state, packageRoot); err != nil {
		return ReviewDeltaPackage{}, err
	}
	if strings.TrimSpace(source) == "" {
		source = state.RequirementSource
	}
	source = normalizeArtifactPath(root, source)
	additional := artifactPaths
	if additional == nil {
		for _, artifact := range state.RequirementArtifacts {
			if artifact.Path != state.RequirementSource && artifact.Path != source {
				additional = append(additional, artifact.Path)
			}
		}
	}
	artifacts, err := requirementArtifactSet(root, source, additional)
	if err != nil {
		return ReviewDeltaPackage{}, err
	}
	candidateRevision := artifactRevision(artifacts, source)
	if candidateRevision == state.RequirementRevision {
		return ReviewDeltaPackage{}, fmt.Errorf("review-delta preview requires a changed candidate revision")
	}
	if len(artifacts) == 1 {
		projection, err := frozenProjection(root, source)
		if err != nil {
			return ReviewDeltaPackage{}, err
		}
		if projection.ContentDigest != candidateRevision {
			return ReviewDeltaPackage{}, fmt.Errorf("candidate requirement digest does not match its structural projection")
		}
	}
	input.BaseRevision = state.RequirementRevision
	input.CandidateRevision = candidateRevision
	input.Digest = reviewDeltaDigest(input)
	check := input
	check.UserConfirmation = "preview-shape-validation"
	if _, err := validateReviewDelta(state, candidateRevision, &check); err != nil {
		return ReviewDeltaPackage{}, err
	}
	input.UserConfirmation = ""
	input.ImpactPreviewDigest = ""
	return input, nil
}

func validateReviewDelta(state RunState, candidateRevision string, input *ReviewDeltaPackage) (ReviewDeltaReceipt, error) {
	if input == nil {
		return ReviewDeltaReceipt{}, fmt.Errorf("a review-delta package is required for a review-driven requirement revision")
	}
	if input.BaseRevision != state.RequirementRevision || input.CandidateRevision != candidateRevision {
		return ReviewDeltaReceipt{}, fmt.Errorf("review-delta base/candidate revision binding is stale")
	}
	if input.Digest == "" || input.Digest != reviewDeltaDigest(*input) {
		return ReviewDeltaReceipt{}, fmt.Errorf("review-delta digest does not match the complete package")
	}
	if input.ConfirmationScope != "REVIEW_DELTA" && input.ConfirmationScope != "REVIEW_DELTA_AND_REQUIREMENT_IMPACT" {
		return ReviewDeltaReceipt{}, fmt.Errorf("review-delta confirmationScope is invalid")
	}
	if strings.TrimSpace(input.UserConfirmation) == "" || len(input.Sources) == 0 || len(input.Hunks) == 0 {
		return ReviewDeltaReceipt{}, fmt.Errorf("review-delta requires user confirmation, sources, and at least one verified hunk")
	}
	sourceIDs := map[string]bool{}
	for _, source := range input.Sources {
		action := strings.TrimSpace(source.Action)
		findingID := strings.TrimSpace(source.FindingID)
		if !isPreDevelopmentReviewAction(action) || strings.ToUpper(strings.TrimSpace(source.Decision)) != "CONFIRM" || !confirmedReviewFinding(state, action, findingID) {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta source %s/%s is not a currently confirmed product/start-readiness finding", action, findingID)
		}
		if sourceIDs[findingID] {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta source finding %s is duplicated", findingID)
		}
		sourceIDs[findingID] = true
	}
	hunkIDs := map[string]bool{}
	usedSources := map[string]bool{}
	receipt := ReviewDeltaReceipt{BaseRevision: input.BaseRevision, CandidateRevision: input.CandidateRevision, DeltaDigest: input.Digest, Sources: append([]ReviewDeltaSource{}, input.Sources...), AffectedActions: append([]string{}, input.AffectedActions...), ConfirmationScope: input.ConfirmationScope, ConfirmationSource: strings.TrimSpace(input.UserConfirmation)}
	for _, hunk := range input.Hunks {
		if strings.TrimSpace(hunk.ID) == "" || hunkIDs[hunk.ID] || len(hunk.Affected) == 0 || len(hunk.SourceFindingIDs) == 0 || strings.ToUpper(strings.TrimSpace(hunk.Disposition)) != "CONFIRM" || strings.TrimSpace(hunk.Impact) == "" || strings.TrimSpace(hunk.Verification) == "" {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta hunk %q is incomplete, duplicated, or not bound to CONFIRM", hunk.ID)
		}
		hunkIDs[hunk.ID] = true
		for _, findingID := range hunk.SourceFindingIDs {
			if !sourceIDs[findingID] {
				return ReviewDeltaReceipt{}, fmt.Errorf("review-delta hunk %s references unrelated finding %s", hunk.ID, findingID)
			}
			usedSources[findingID] = true
		}
		receipt.HunkVerifications = append(receipt.HunkVerifications, ReviewDeltaHunkReceipt{ID: hunk.ID, Affected: append([]string{}, hunk.Affected...), SourceFindingIDs: append([]string{}, hunk.SourceFindingIDs...), VerificationDigest: digestValue(hunk.Verification)})
	}
	for findingID := range sourceIDs {
		if !usedSources[findingID] {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta source finding %s is not assigned to any verified hunk", findingID)
		}
	}
	if len(input.AffectedActions) == 0 {
		return ReviewDeltaReceipt{}, fmt.Errorf("review-delta must identify every affected product-review/start-readiness action")
	}
	affectedActions := map[string]bool{}
	for _, action := range input.AffectedActions {
		if action != "product-review" && action != "start-readiness" {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta affected action %q is not product-review or start-readiness", action)
		}
		if affectedActions[action] {
			return ReviewDeltaReceipt{}, fmt.Errorf("review-delta affected action %q is duplicated", action)
		}
		affectedActions[action] = true
	}
	receipt.ID = "review-delta-" + shortDigest(digestValue(receipt))
	return receipt, nil
}

func confirmedReviewFinding(state RunState, action, findingID string) bool {
	series := state.PreDevelopmentReviewSeries[action]
	candidate, ok := semanticCandidateByDigest(series, series.EffectiveReviewStatus.ResultDigest)
	if !ok || candidate.RequirementRevision != state.RequirementRevision {
		return false
	}
	for _, finding := range candidate.Findings {
		if finding.ID == findingID {
			decision, _, _ := effectiveFindingDecision(series, candidate.ResultDigest, finding)
			return decision == "CONFIRM"
		}
	}
	return false
}

func hasConfirmedReviewFinding(state RunState) bool {
	for _, action := range preDevelopmentReviewActions {
		series := state.PreDevelopmentReviewSeries[action]
		candidate, ok := semanticCandidateByDigest(series, series.EffectiveReviewStatus.ResultDigest)
		if !ok || candidate.RequirementRevision != state.RequirementRevision {
			continue
		}
		for _, finding := range candidate.Findings {
			decision, _, _ := effectiveFindingDecision(series, candidate.ResultDigest, finding)
			if decision == "CONFIRM" {
				return true
			}
		}
	}
	return false
}

func markPreDevelopmentReviewActionStale(state *RunState, action, source string) {
	series := state.PreDevelopmentReviewSeries[action]
	if series.Action == "" {
		return
	}
	series.EffectiveReviewStatus.Status = "PENDING"
	series.EffectiveReviewStatus.Source = source
	series.EffectiveReviewStatus.Revision++
	series.EffectiveReviewStatus.BlockingFindingIDs = []string{}
	series.EffectiveReviewStatus.AdvisoryFindingIDs = []string{}
	series.EffectiveReviewStatus.DownstreamImpact = semanticReviewDownstreamImpact(action)
	state.PreDevelopmentReviewSeries[action] = series
}

func requirementImpactPreview(state RunState, candidateRevision, reviewDeltaDigest string, impact RequirementImpactInput) (RequirementImpactPreview, error) {
	impact.ProductGoal = strings.TrimSpace(impact.ProductGoal)
	impact.Scope = strings.TrimSpace(impact.Scope)
	impact.ExternalBehavior = strings.TrimSpace(impact.ExternalBehavior)
	impact.AcceptanceCriteria = strings.TrimSpace(impact.AcceptanceCriteria)
	if impact.ProductGoal == "" && impact.Scope == "" && impact.ExternalBehavior == "" && impact.AcceptanceCriteria == "" {
		return RequirementImpactPreview{}, fmt.Errorf("meaning-changed preview requires at least one concrete product-goal, scope, external-behavior, or acceptance impact")
	}
	if strings.TrimSpace(state.StateIntegrity) == "" {
		return RequirementImpactPreview{}, fmt.Errorf("current run state has no immutable state revision")
	}
	invalidated := []string{}
	for _, action := range []string{"product-review", "start-readiness", "development-worker"} {
		if state.Actions[action].Status == "PASS" || state.Actions[action].Status == developmentComplete || state.Actions[action].Status == developmentVerified {
			invalidated = append(invalidated, action+":"+state.Actions[action].Status)
		}
	}
	for mode, result := range state.QAReviewByMode {
		if result.Status == "PASS" {
			invalidated = append(invalidated, "qa-review:"+mode+":PASS")
		}
	}
	for id, result := range state.Gates {
		if result.Status == "PASS" {
			invalidated = append(invalidated, "gate:"+id+":PASS")
		}
	}
	sort.Strings(invalidated)
	series := state.PreDevelopmentReviewSeries["product-review"]
	preview := RequirementImpactPreview{
		BaseRevision: state.RequirementRevision, CandidateRevision: candidateRevision,
		StateRevision: state.StateIntegrity, Impact: impact, InvalidatedPasses: invalidated,
		RollbackSteps:          []string{"register the complete candidate requirement revision", "invalidate product-review PASS and dependent results", "return product-review to PENDING"},
		ProductReviewCompleted: series.Completed, ProductReviewRemaining: series.RemainingCapacity,
		ProductReviewNextRequirement: series.NextRoundRequirement, ReviewDeltaDigest: reviewDeltaDigest,
	}
	copy := preview
	copy.Digest = ""
	preview.Digest = digestValue(copy)
	return preview, nil
}

func PreviewRequirementImpact(root, packageRoot, runID, source string, artifactPaths []string, reviewDeltaDigest string, impact RequirementImpactInput) (RequirementImpactPreview, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RequirementImpactPreview{}, err
	}
	if _, err := requireCurrentCatalog(state, packageRoot); err != nil {
		return RequirementImpactPreview{}, err
	}
	if strings.TrimSpace(source) == "" {
		source = state.RequirementSource
	}
	source = normalizeArtifactPath(root, source)
	additional := artifactPaths
	if additional == nil {
		for _, artifact := range state.RequirementArtifacts {
			if artifact.Path != state.RequirementSource && artifact.Path != source {
				additional = append(additional, artifact.Path)
			}
		}
	}
	artifacts, err := requirementArtifactSet(root, source, additional)
	if err != nil {
		return RequirementImpactPreview{}, err
	}
	candidateRevision := artifactRevision(artifacts, source)
	if candidateRevision == state.RequirementRevision {
		return RequirementImpactPreview{}, fmt.Errorf("requirement-impact preview requires a changed candidate revision")
	}
	if len(artifacts) == 1 {
		projection, err := frozenProjection(root, source)
		if err != nil {
			return RequirementImpactPreview{}, err
		}
		if projection.ContentDigest != candidateRevision {
			return RequirementImpactPreview{}, fmt.Errorf("candidate requirement digest does not match its structural projection")
		}
	}
	return requirementImpactPreview(state, candidateRevision, strings.TrimSpace(reviewDeltaDigest), impact)
}
