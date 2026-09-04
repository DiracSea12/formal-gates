package validate

// This file owns the post-development convergence projection introduced by
// REQ-003 through REQ-010.  The projection is deliberately kept in the legacy
// run state: reviewers still produce ordinary action/gate results, while the
// controller freezes their target set, records the main-agent adjudication and
// permits exactly one repair obligation for each review-ready snapshot.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	caseSetDraft              = "DRAFT"
	caseSetReviewed           = "REVIEWED_PENDING_SUGGESTIONS"
	caseSetFrozen             = "FROZEN"
	caseSetThawed             = "THAWED"
	reviewWaveCollecting      = "COLLECTING"
	reviewWaveAdjudicating    = "AWAITING_ADJUDICATION"
	reviewWaveReadyForRepair  = "READY_FOR_REPAIR"
	reviewWaveComplete        = "COMPLETE"
	reviewWaveInvalidated     = "INVALIDATED"
	reviewTargetUnprepared    = "UNPREPARED"
	reviewTargetPrepared      = "PREPARED"
	reviewTargetInFlight      = "IN_FLIGHT"
	reviewTargetPass          = "PASS"
	reviewTargetFail          = "FAIL"
	reviewTargetRuntimeError  = "RUNTIME_ERROR"
	reviewTargetInvalidResult = "REJECTED_INVALID_RESULT"
	reviewTargetRetryExhaust  = "RETRY_EXHAUSTED"
	reviewTargetDisposed      = "DISPOSED"
)

var qaThawCategories = map[string]bool{
	"REQUIREMENT_MEANING":      true,
	"EXTERNAL_BEHAVIOR":        true,
	"SUCCESS_FAILURE_CRITERIA": true,
	"CASE_EXECUTABILITY":       true,
}

// QASuggestion is a stable, review-bound P2/P3 suggestion.  Its ID is not a
// semantic duplicate detector; it is only an immutable reference for the QA
// designer's explicit absorption record.
type QASuggestion struct {
	ID                   string   `json:"id"`
	Severity             string   `json:"severity"`
	Message              string   `json:"message"`
	Locations            []string `json:"locations,omitempty"`
	Status               string   `json:"status"`
	ReviewDispatchID     string   `json:"reviewDispatchId"`
	AbsorbedByDispatchID string   `json:"absorbedByDispatchId,omitempty"`
}

type QAThawRecord struct {
	ID             string   `json:"id"`
	Category       string   `json:"category"`
	Reason         string   `json:"reason"`
	SourceRevision string   `json:"sourceRevision"`
	CaseIDs        []string `json:"caseIds"`
	PriorVersion   string   `json:"priorVersion"`
}

// QACaseSetState separates approval/freeze identity from the mutable case
// slice.  Ordinary implementation snapshots only rebind BoundSnapshot; they do
// not mint a new Version or reopen QA design.
type QACaseSetState struct {
	Mode                   string         `json:"mode"`
	Status                 string         `json:"status"`
	Version                string         `json:"version,omitempty"`
	BoundSnapshot          string         `json:"boundSnapshot,omitempty"`
	ReviewDispatchID       string         `json:"reviewDispatchId,omitempty"`
	PendingSnapshotAdvance bool           `json:"pendingSnapshotAdvance,omitempty"`
	Suggestions            []QASuggestion `json:"suggestions,omitempty"`
	Thaws                  []QAThawRecord `json:"thaws,omitempty"`
}

type ReviewWaveTarget struct {
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Target         string `json:"target"`
	Mode           string `json:"mode,omitempty"`
	Status         string `json:"status"`
	DispatchID     string `json:"dispatchId,omitempty"`
	TaskKey        string `json:"taskKey,omitempty"`
	ResultStatus   string `json:"resultStatus,omitempty"`
	Disposition    string `json:"disposition,omitempty"`
	DispositionWhy string `json:"dispositionReason,omitempty"`
}

type ReviewWaveLateResult struct {
	DispatchID string `json:"dispatchId"`
	TargetKey  string `json:"targetKey"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
}

type ProblemDimensions struct {
	Requirement       string `json:"requirement"`
	NormalFailurePath string `json:"normalFailurePath"`
	AffectedModule    string `json:"affectedModule"`
	RootCause         string `json:"rootCause"`
	ActualConsequence string `json:"actualConsequence"`
}

type FindingAdjudication struct {
	Revision      int                 `json:"revision"`
	Decision      string              `json:"decision"`
	ProblemID     string              `json:"problemId,omitempty"`
	Dimensions    ProblemDimensions   `json:"dimensions"`
	Verification  FindingVerification `json:"verification"`
	References    []string            `json:"references"`
	Reason        string              `json:"reason"`
	StateRevision string              `json:"stateRevision"`
	Supersedes    int                 `json:"supersedes,omitempty"`
}

type PostReviewFinding struct {
	ID              string                `json:"id"`
	WaveID          string                `json:"waveId"`
	SourceTargetKey string                `json:"sourceTargetKey"`
	SourceKind      string                `json:"sourceKind"`
	SourceTarget    string                `json:"sourceTarget"`
	Mode            string                `json:"mode,omitempty"`
	Severity        string                `json:"severity,omitempty"`
	Message         string                `json:"message"`
	Locations       []string              `json:"locations,omitempty"`
	FirstSeenWave   int                   `json:"firstSeenWave"`
	MissedEarlier   bool                  `json:"missedEarlier,omitempty"`
	Adjudications   []FindingAdjudication `json:"adjudications,omitempty"`
}

type CanonicalProblem struct {
	ID            string            `json:"id"`
	Dimensions    ProblemDimensions `json:"dimensions"`
	Status        string            `json:"status"`
	FindingIDs    []string          `json:"findingIds"`
	Evidence      []string          `json:"evidence"`
	CreatedWaveID string            `json:"createdWaveId"`
	ResolvedAt    string            `json:"resolvedAtSnapshot,omitempty"`
}

type UnifiedRepairPlan struct {
	ID             string   `json:"id"`
	WaveID         string   `json:"waveId"`
	Snapshot       string   `json:"snapshot"`
	ProblemIDs     []string `json:"problemIds"`
	Status         string   `json:"status"`
	DispatchID     string   `json:"dispatchId,omitempty"`
	RepairSnapshot string   `json:"repairSnapshot,omitempty"`
}

type P2CompletionCredential struct {
	ID             string   `json:"id"`
	WaveID         string   `json:"waveId"`
	GateID         string   `json:"gateId"`
	ProblemIDs     []string `json:"problemIds"`
	SourceSnapshot string   `json:"sourceSnapshot"`
	RepairSnapshot string   `json:"repairSnapshot"`
	BuildEvidence  []string `json:"buildEvidence"`
	TestEvidence   []string `json:"testEvidence"`
	AffectedCases  []string `json:"affectedCases,omitempty"`
}

type ReviewWave struct {
	ID                  string                      `json:"id"`
	Number              int                         `json:"number"`
	Snapshot            string                      `json:"snapshot"`
	BaseSnapshot        string                      `json:"baseSnapshot"`
	RequirementRevision string                      `json:"requirementRevision"`
	CaseSetVersions     map[string]string           `json:"caseSetVersions,omitempty"`
	Expected            map[string]ReviewWaveTarget `json:"expected"`
	ExpectedDigest      string                      `json:"expectedDigest"`
	Status              string                      `json:"status"`
	Counted             bool                        `json:"counted,omitempty"`
	RepairPlan          *UnifiedRepairPlan          `json:"repairPlan,omitempty"`
	LateResults         []ReviewWaveLateResult      `json:"lateResults,omitempty"`
}

type FindingAdjudicationInput struct {
	FindingID    string              `json:"findingId"`
	Decision     string              `json:"decision"`
	ProblemID    string              `json:"problemId,omitempty"`
	Dimensions   ProblemDimensions   `json:"dimensions"`
	Verification FindingVerification `json:"verification"`
	References   []string            `json:"references"`
	Reason       string              `json:"reason"`
}

type ReviewWaveAdjudicationInput struct {
	WaveID        string                     `json:"waveId"`
	StateRevision string                     `json:"stateRevision"`
	Findings      []FindingAdjudicationInput `json:"findings"`
}

type RepairRerunEstimate struct {
	BuildTargets []string            `json:"buildTargets"`
	TestTargets  []string            `json:"testTargets"`
	QACases      map[string][]string `json:"qaCases"`
}

type RepairAuthorizationPreview struct {
	RunID                   string              `json:"runId"`
	StateRevision           string              `json:"stateRevision"`
	ReviewWave              int                 `json:"reviewWave"`
	RemainingAutomaticWaves int                 `json:"remainingAutomaticReviewWaves"`
	DispatchAttempts        int                 `json:"dispatchAttempts"`
	Runtime                 string              `json:"runtime"`
	QAFailures              []string            `json:"qaFailures"`
	BlockingFindings        []string            `json:"blockingFindings"`
	GateSuggestions         map[string]int      `json:"gateSuggestions"`
	QAReviewSuggestions     map[string]int      `json:"qaReviewSuggestions"`
	Estimate                RepairRerunEstimate `json:"estimate"`
	EngineAllowance         string              `json:"engineAllowance"`
	Digest                  string              `json:"digest"`
}

type RepairAuthorizationInput struct {
	PreviewDigest string
	StateRevision string
	Estimate      RepairRerunEstimate
}

func ensureConvergenceState(state *RunState) {
	if state.QACaseSets == nil {
		state.QACaseSets = map[string]QACaseSetState{}
	}
	if state.ReviewWaves == nil {
		state.ReviewWaves = map[string]ReviewWave{}
	}
	if state.PostReviewFindings == nil {
		state.PostReviewFindings = map[string]PostReviewFinding{}
	}
	if state.Problems == nil {
		state.Problems = map[string]CanonicalProblem{}
	}
	if state.P2CompletionCredentials == nil {
		state.P2CompletionCredentials = []P2CompletionCredential{}
	}
}

func convergenceEnabled(state RunState) bool {
	return len(state.ReviewWaves) != 0
}

func caseSetVersion(state RunState, mode string) string {
	return digestValue(struct {
		RequirementRevision string
		Mode                string
		Cases               []QACase
	}{state.RequirementRevision, mode, state.qaModeCases(mode)})
}

func suggestionID(dispatchID string, finding Finding, index int) string {
	return "suggestion-" + shortDigest(digestValue(struct {
		DispatchID string
		FindingID  string
		Index      int
	}{dispatchID, finding.ID, index}))
}

func recordQAReviewCaseSet(state *RunState, mode string, result ActionResult, dispatch PreparedDispatch) error {
	ensureConvergenceState(state)
	current := state.QACaseSets[mode]
	current.Mode = mode
	current.ReviewDispatchID = dispatch.ID
	current.Suggestions = nil
	for index, finding := range result.Findings {
		if finding.Severity != "P2" && finding.Severity != "P3" {
			continue
		}
		current.Suggestions = append(current.Suggestions, QASuggestion{
			ID: suggestionID(dispatch.ID, finding, index), Severity: finding.Severity,
			Message: finding.Message, Locations: append([]string{}, finding.Locations...),
			Status: "PENDING", ReviewDispatchID: dispatch.ID,
		})
	}
	if result.Status != "PASS" {
		current.Status = caseSetDraft
		current.Version = ""
		current.BoundSnapshot = ""
		current.PendingSnapshotAdvance = false
		state.QACaseSets[mode] = current
		return nil
	}
	if len(current.Suggestions) != 0 {
		current.Status = caseSetReviewed
		current.Version = ""
		current.BoundSnapshot = ""
		current.PendingSnapshotAdvance = false
		state.QACaseSets[mode] = current
		return nil
	}
	if err := freezeQACaseSet(state, mode, &current, false); err != nil {
		return err
	}
	state.QACaseSets[mode] = current
	return nil
}

func freezeQACaseSet(state *RunState, mode string, current *QACaseSetState, pendingSnapshotAdvance bool) error {
	for _, testCase := range state.qaModeCases(mode) {
		if testCase.ReviewStatus != "PASS" {
			return fmt.Errorf("QA case-set %q cannot freeze while case %s is %s", mode, testCase.ID, testCase.ReviewStatus)
		}
	}
	for _, suggestion := range current.Suggestions {
		if suggestion.Status != "ABSORBED" {
			return fmt.Errorf("QA case-set %q cannot freeze while suggestion %s is not absorbed", mode, suggestion.ID)
		}
	}
	if err := validateGuaranteeCaseSetFreeze(state, mode); err != nil {
		return err
	}
	current.Status = caseSetFrozen
	current.Version = caseSetVersion(*state, mode)
	current.PendingSnapshotAdvance = pendingSnapshotAdvance
	if pendingSnapshotAdvance {
		current.BoundSnapshot = ""
	} else {
		current.BoundSnapshot = state.CurrentSnapshot
	}
	return nil
}

func absorbQAReviewSuggestions(state *RunState, dispatch PreparedDispatch, suggestionIDs []string, changed bool) error {
	ensureConvergenceState(state)
	current, ok := state.QACaseSets[dispatch.Mode]
	if !ok || current.Status != caseSetReviewed || current.ReviewDispatchID == "" {
		return fmt.Errorf("suggestion absorption requires a PASS QA Review with pending P2/P3 suggestions")
	}
	if !changed {
		return fmt.Errorf("suggestion absorption must add, modify, or remove at least one QA case")
	}
	wanted := map[string]bool{}
	for _, raw := range suggestionIDs {
		id := strings.TrimSpace(raw)
		if id == "" || wanted[id] {
			return fmt.Errorf("suggestion absorption contains an empty or duplicate suggestion id")
		}
		wanted[id] = true
	}
	if len(wanted) == 0 {
		return fmt.Errorf("suggestion absorption requires at least one --suggestion-id")
	}
	for index := range current.Suggestions {
		suggestion := &current.Suggestions[index]
		if !wanted[suggestion.ID] {
			continue
		}
		if suggestion.Status == "ABSORBED" {
			return fmt.Errorf("suggestion %s was already absorbed", suggestion.ID)
		}
		suggestion.Status = "ABSORBED"
		suggestion.AbsorbedByDispatchID = dispatch.ID
		delete(wanted, suggestion.ID)
	}
	if len(wanted) != 0 {
		ids := make([]string, 0, len(wanted))
		for id := range wanted {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf("suggestion ids do not belong to the bound QA Review: %s", strings.Join(ids, ", "))
	}
	allAbsorbed := true
	for _, suggestion := range current.Suggestions {
		if suggestion.Status != "ABSORBED" {
			allAbsorbed = false
			break
		}
	}
	if allAbsorbed {
		if err := refreshGuaranteeReviewAfterSuggestion(state, dispatch.Mode); err != nil {
			return err
		}
		if err := freezeQACaseSet(state, dispatch.Mode, &current, dispatch.Mode == whiteboxQAID); err != nil {
			return err
		}
	}
	state.QACaseSets[dispatch.Mode] = current
	return nil
}

func bindFrozenCaseSetsToSnapshot(state *RunState) {
	ensureConvergenceState(state)
	for mode, caseSet := range state.QACaseSets {
		if caseSet.Status != caseSetFrozen {
			continue
		}
		caseSet.BoundSnapshot = state.CurrentSnapshot
		caseSet.PendingSnapshotAdvance = false
		state.QACaseSets[mode] = caseSet
	}
}

func invalidateActiveReviewWave(state *RunState, reason string) {
	if state.ActiveReviewWaveID == "" {
		return
	}
	wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
	if !ok || wave.Status == reviewWaveInvalidated {
		return
	}
	wave.Status = reviewWaveInvalidated
	for key, target := range wave.Expected {
		if target.Status == reviewTargetUnprepared || target.Status == reviewTargetPrepared || target.Status == reviewTargetInFlight {
			target.Disposition = "INVALIDATED"
			target.DispositionWhy = strings.TrimSpace(reason)
			wave.Expected[key] = target
			if dispatch, exists := state.Dispatches[target.DispatchID]; exists && dispatch.Status != "COMPLETED" {
				dispatch.Status = "STALE"
				state.Dispatches[target.DispatchID] = dispatch
			}
		}
	}
	state.ReviewWaves[wave.ID] = wave
	state.ActiveReviewWaveID = ""
}

// ThawQACaseSet is the only ordinary path back into QA design after FROZEN.
// The four categories are the documented product/acceptance changes; a
// transient environment failure is therefore never a valid thaw category.
func ThawQACaseSet(root, packageRoot, runID, mode, category, reason, sourceRevision string, caseIDs []string) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, false, func(state *RunState, _ PromptCatalog) error {
		ensureConvergenceState(state)
		mode = strings.TrimSpace(mode)
		category = strings.ToUpper(strings.TrimSpace(category))
		if !qaThawCategories[category] {
			return fmt.Errorf("QA case-set thaw category must be REQUIREMENT_MEANING, EXTERNAL_BEHAVIOR, SUCCESS_FAILURE_CRITERIA, or CASE_EXECUTABILITY")
		}
		if strings.TrimSpace(reason) == "" || strings.TrimSpace(sourceRevision) == "" {
			return fmt.Errorf("QA case-set thaw requires a reason and source revision")
		}
		caseSet, ok := state.QACaseSets[mode]
		if !ok || caseSet.Status != caseSetFrozen {
			return fmt.Errorf("QA case-set %q is not FROZEN", mode)
		}
		wanted := map[string]bool{}
		for _, raw := range caseIDs {
			id := strings.TrimSpace(raw)
			if id != "" {
				wanted[id] = true
			}
		}
		if len(wanted) == 0 {
			return fmt.Errorf("QA case-set thaw requires at least one affected case id")
		}
		storageKey, cases := state.qaModeCasesWithKey(mode)
		for index := range cases {
			if wanted[cases[index].ID] {
				cases[index].ReviewStatus = "PENDING"
				cases[index].ApprovedSource = ""
				cases[index].SuggestionIDs = nil
				delete(wanted, cases[index].ID)
			}
		}
		if len(wanted) != 0 {
			return fmt.Errorf("QA case-set thaw references a case outside mode %q", mode)
		}
		priorVersion := caseSet.Version
		thaw := QAThawRecord{Category: category, Reason: strings.TrimSpace(reason), SourceRevision: strings.TrimSpace(sourceRevision), CaseIDs: append([]string{}, caseIDs...), PriorVersion: priorVersion}
		thaw.ID = "thaw-" + shortDigest(digestValue(thaw))
		caseSet.Thaws = append(caseSet.Thaws, thaw)
		caseSet.Status, caseSet.Version, caseSet.BoundSnapshot = caseSetThawed, "", ""
		caseSet.PendingSnapshotAdvance = false
		caseSet.Suggestions = nil
		state.QACaseSets[mode] = caseSet
		state.setQACasesForReview(mode, storageKey, cases)
		state.setQADesign(mode, ActionResult{Status: "PENDING"})
		state.setQAReview(mode, ActionResult{Status: "PENDING"})
		state.deleteQAExecution(mode)
		delete(state.PriorQAExecutionByMode, mode)
		delete(state.ExecutionScopes, mode)
		delete(state.Carry, qaSelectionID(mode))
		invalidateActiveReviewWave(state, "QA case-set thawed: "+thaw.ID)
		return nil
	})
}

func qaSelectionID(mode string) string {
	switch mode {
	case "blackbox":
		return blackboxQAID
	case "whitebox":
		return whiteboxQAID
	default:
		return mergeQAID
	}
}

func reviewWaveTargetKey(kind, target, mode string) string {
	if mode != "" {
		return kind + ":" + target + ":" + mode
	}
	return kind + ":" + target
}

func expectedReviewWaveTargets(state RunState) map[string]ReviewWaveTarget {
	expected := map[string]ReviewWaveTarget{}
	for _, id := range state.SelectedGates {
		if isQAMode(id) {
			mode := qaDispatchMode(id)
			result := qaModeResult(state, mode)
			if result.Status == "PASS" && result.Snapshot == state.CurrentSnapshot {
				continue
			}
			key := reviewWaveTargetKey("action", "qa-execution", mode)
			expected[key] = ReviewWaveTarget{Key: key, Kind: "action", Target: "qa-execution", Mode: mode, Status: reviewTargetUnprepared}
			continue
		}
		result := state.Gates[id]
		if result.Status == "PASS" && result.Snapshot == state.CurrentSnapshot {
			continue
		}
		key := reviewWaveTargetKey("gate", id, "")
		expected[key] = ReviewWaveTarget{Key: key, Kind: "gate", Target: id, Status: reviewTargetUnprepared}
	}
	return expected
}

func reviewWaveExpectedDigest(state RunState, versions map[string]string, expected map[string]ReviewWaveTarget) string {
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return digestValue(struct {
		Snapshot            string
		RequirementRevision string
		CaseSetVersions     map[string]string
		Targets             []string
	}{state.CurrentSnapshot, state.RequirementRevision, versions, keys})
}

func initializeReviewWave(root, packageRoot, runID string) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, false, func(state *RunState, _ PromptCatalog) error {
		ensureConvergenceState(state)
		if !hasDevelopmentSnapshot(*state) {
			return fmt.Errorf("an immutable development snapshot is required before review-wave initialization")
		}
		if _, err := requireNativeCurrent(root, *state); err != nil {
			return err
		}
		versions := map[string]string{}
		for _, id := range state.SelectedGates {
			if !isQAMode(id) {
				continue
			}
			mode := qaDispatchMode(id)
			caseSet, ok := state.QACaseSets[mode]
			if !ok || caseSet.Status != caseSetFrozen || caseSet.Version == "" || caseSet.PendingSnapshotAdvance || caseSet.BoundSnapshot != state.CurrentSnapshot {
				return fmt.Errorf("review-ready snapshot requires mode %q to have a FROZEN case-set version bound to %s", mode, state.CurrentSnapshot)
			}
			versions[mode] = caseSet.Version
		}
		expected := expectedReviewWaveTargets(*state)
		digest := reviewWaveExpectedDigest(*state, versions, expected)
		for _, existing := range state.ReviewWaves {
			if existing.Snapshot != state.CurrentSnapshot || existing.Status == reviewWaveInvalidated {
				continue
			}
			if existing.ExpectedDigest != digest {
				return fmt.Errorf("review-wave target or case-set identity changed for snapshot %s; the old wave must be invalidated by a legal input change before rebuilding", state.CurrentSnapshot)
			}
			state.ActiveReviewWaveID = existing.ID
			return nil
		}
		number := state.CompletedReviewWaves + 1
		wave := ReviewWave{Number: number, Snapshot: state.CurrentSnapshot, BaseSnapshot: state.BaseSnapshot, RequirementRevision: state.RequirementRevision, CaseSetVersions: versions, Expected: expected, ExpectedDigest: digest, Status: reviewWaveCollecting}
		wave.ID = "review-wave-" + shortDigest(digestValue(struct{ RunID, Snapshot, Digest string }{state.RunID, state.CurrentSnapshot, digest}))
		state.ReviewWaves[wave.ID] = wave
		state.ActiveReviewWaveID = wave.ID
		return nil
	})
}

// PrepareReviewWave is the acceptance-surface for AC-009/AC-012: one call
// freezes the expected set and prepares every eligible QA execution and gate.
// The individual tickets remain independent and can be claimed in parallel.
func PrepareReviewWave(root, packageRoot, runID string) (ReviewWave, error) {
	state, err := initializeReviewWave(root, packageRoot, runID)
	if err != nil {
		return ReviewWave{}, err
	}
	wave := state.ReviewWaves[state.ActiveReviewWaveID]
	keys := make([]string, 0, len(wave.Expected))
	for key := range wave.Expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		target := wave.Expected[key]
		prepareTarget := target.Status == reviewTargetUnprepared
		if target.Status == reviewTargetRuntimeError || target.Status == reviewTargetInvalidResult || target.Status == reviewTargetRetryExhaust {
			prepareTarget = state.DispatchObligations[target.TaskKey].Status == "RETRYABLE"
		}
		if !prepareTarget {
			continue
		}
		if target.Kind == "gate" {
			if _, err := PrepareGate(root, packageRoot, runID, target.Target, false, ""); err != nil {
				return LoadActiveReviewWave(root, runID, err)
			}
		} else {
			if _, err := PrepareAction(root, packageRoot, runID, target.Target, target.Mode, false, ""); err != nil {
				return LoadActiveReviewWave(root, runID, err)
			}
		}
		state, err = LoadRunState(root, runID)
		if err != nil {
			return ReviewWave{}, err
		}
		wave = state.ReviewWaves[state.ActiveReviewWaveID]
	}
	if len(wave.Expected) == 0 {
		_, err = mutateRun(root, runID, func(state *RunState) error {
			maybeCollectReviewWave(state)
			return nil
		})
		if err != nil {
			return ReviewWave{}, err
		}
	}
	return LoadActiveReviewWave(root, runID, nil)
}

func LoadActiveReviewWave(root, runID string, prior error) (ReviewWave, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return ReviewWave{}, err
	}
	wave := state.ReviewWaves[state.ActiveReviewWaveID]
	if prior != nil {
		return wave, prior
	}
	return wave, nil
}

func expectedTargetForDispatch(state RunState, dispatch PreparedDispatch) (ReviewWave, string, ReviewWaveTarget, bool) {
	if state.ActiveReviewWaveID == "" {
		return ReviewWave{}, "", ReviewWaveTarget{}, false
	}
	wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
	if !ok || wave.Snapshot != dispatch.SourceSnapshot {
		return ReviewWave{}, "", ReviewWaveTarget{}, false
	}
	key := reviewWaveTargetKey(dispatch.TargetKind, dispatch.Target, dispatch.Mode)
	target, ok := wave.Expected[key]
	return wave, key, target, ok
}

func requireReviewWaveDispatch(state RunState, targetKind, target, mode string) error {
	if !convergenceEnabled(state) || (targetKind != "gate" && !(targetKind == "action" && target == "qa-execution")) {
		return nil
	}
	if targetKind == "action" && target == "qa-execution" && state.ActiveReviewWaveID != "" {
		wave := state.ReviewWaves[state.ActiveReviewWaveID]
		if wave.RepairPlan != nil && wave.RepairPlan.Status == "P2_COMPLETED" {
			// Pure gate-P2 repairs do not open a second review wave.  Their
			// bounded AFFECTED run and optional final FULL guarantee regression
			// are repair verification, not a gate re-review.
			return nil
		}
	}
	if state.ActiveReviewWaveID == "" {
		return fmt.Errorf("review-ready QA execution and gates must be prepared together with workflow prepare-review-wave")
	}
	wave := state.ReviewWaves[state.ActiveReviewWaveID]
	if wave.Status != reviewWaveCollecting || wave.Snapshot != state.CurrentSnapshot {
		return fmt.Errorf("the active review-wave is not COLLECTING for the current snapshot")
	}
	key := reviewWaveTargetKey(targetKind, target, mode)
	if _, ok := wave.Expected[key]; !ok {
		return fmt.Errorf("target %s is not in the frozen expected-set for review-wave %s", key, wave.ID)
	}
	return nil
}

func bindDispatchToReviewWave(state *RunState, dispatch PreparedDispatch) {
	ensureConvergenceState(state)
	if wave, key, target, ok := expectedTargetForDispatch(*state, dispatch); ok {
		if target.DispatchID == "" || target.Status == reviewTargetRuntimeError || target.Status == reviewTargetInvalidResult || target.Status == reviewTargetRetryExhaust {
			target.DispatchID = dispatch.ID
			target.TaskKey = dispatch.TaskKey
			target.Status = reviewTargetPrepared
			wave.Expected[key] = target
			state.ReviewWaves[wave.ID] = wave
		}
		return
	}
	if dispatch.TargetKind == "action" && dispatch.Target == "development-worker" && state.ActiveReviewWaveID != "" {
		wave := state.ReviewWaves[state.ActiveReviewWaveID]
		if wave.RepairPlan != nil && wave.RepairPlan.DispatchID == "" {
			wave.RepairPlan.DispatchID = dispatch.ID
			wave.RepairPlan.Status = "DISPATCHED"
			state.ReviewWaves[wave.ID] = wave
		}
	}
}

func markReviewWaveDispatchClaimed(state *RunState, dispatch PreparedDispatch) {
	if wave, key, target, ok := expectedTargetForDispatch(*state, dispatch); ok && wave.Status == reviewWaveCollecting {
		target.Status = reviewTargetInFlight
		wave.Expected[key] = target
		state.ReviewWaves[wave.ID] = wave
	}
}

func recordReviewWaveAttempt(state *RunState, dispatch PreparedDispatch, resultStatus, failure string) {
	wave, key, target, ok := expectedTargetForDispatch(*state, dispatch)
	if !ok {
		return
	}
	if wave.Status != reviewWaveCollecting || target.Status == reviewTargetDisposed || (target.DispatchID != "" && target.DispatchID != dispatch.ID) {
		wave.LateResults = append(wave.LateResults, ReviewWaveLateResult{DispatchID: dispatch.ID, TargetKey: key, Status: resultStatus, Reason: strings.TrimSpace(failure)})
		state.ReviewWaves[wave.ID] = wave
		return
	}
	target.DispatchID = dispatch.ID
	target.TaskKey = dispatch.TaskKey
	target.ResultStatus = resultStatus
	switch resultStatus {
	case "PASS":
		target.Status = reviewTargetPass
	case "FAIL":
		target.Status = reviewTargetFail
	case "RUNTIME_ERROR":
		target.Status = reviewTargetRuntimeError
	case "REJECTED_INVALID_RESULT":
		target.Status = reviewTargetInvalidResult
	}
	if obligation, exists := state.DispatchObligations[dispatch.TaskKey]; exists && obligation.Status == "RETRY_EXHAUSTED" {
		target.Status = reviewTargetRetryExhaust
	}
	wave.Expected[key] = target
	state.ReviewWaves[wave.ID] = wave
	maybeCollectReviewWave(state)
}

func DisposeReviewWaveTarget(root, packageRoot, runID, targetKey, stateRevision, reason string) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, false, func(state *RunState, _ PromptCatalog) error {
		ensureConvergenceState(state)
		if strings.TrimSpace(stateRevision) == "" || stateRevision != state.StateIntegrity {
			return fmt.Errorf("review-wave target disposition state revision is stale or missing")
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("review-wave target disposition requires a non-empty reason")
		}
		wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
		if !ok || wave.Status != reviewWaveCollecting {
			return fmt.Errorf("there is no COLLECTING review-wave")
		}
		target, ok := wave.Expected[strings.TrimSpace(targetKey)]
		if !ok {
			return fmt.Errorf("review-wave target %q is not in the frozen expected-set", targetKey)
		}
		if target.Status != reviewTargetRuntimeError && target.Status != reviewTargetInvalidResult && target.Status != reviewTargetRetryExhaust {
			return fmt.Errorf("review-wave target %s is %s and cannot be disposed as an execution error", target.Key, target.Status)
		}
		target.Status = reviewTargetDisposed
		target.Disposition = "USER_DISPOSED_ERROR"
		target.DispositionWhy = strings.TrimSpace(reason)
		wave.Expected[target.Key] = target
		state.ReviewWaves[wave.ID] = wave
		maybeCollectReviewWave(state)
		return nil
	})
}

func syncReviewWaveFindings(state *RunState, wave ReviewWave) []string {
	ensureConvergenceState(state)
	ids := []string{}
	keys := make([]string, 0, len(wave.Expected))
	for key := range wave.Expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		target := wave.Expected[key]
		var findings []Finding
		if target.Kind == "gate" {
			findings = state.Gates[target.Target].Findings
		} else if target.Target == "qa-execution" {
			findings = qaModeResult(*state, target.Mode).Findings
		}
		for index, finding := range findings {
			id := "finding-" + shortDigest(digestValue(struct {
				WaveID, TargetKey, RawFindingID, Message string
				Index                                    int
			}{wave.ID, key, finding.ID, finding.Message, index}))
			if _, exists := state.PostReviewFindings[id]; !exists {
				severity := finding.Severity
				if target.Target == "qa-execution" && severity == "" {
					severity = "QA_FAIL"
				}
				state.PostReviewFindings[id] = PostReviewFinding{
					ID: id, WaveID: wave.ID, SourceTargetKey: key, SourceKind: target.Kind,
					SourceTarget: target.Target, Mode: target.Mode, Severity: severity,
					Message: finding.Message, Locations: append([]string{}, finding.Locations...),
					FirstSeenWave: wave.Number, MissedEarlier: wave.Number > 1 && (severity == "P0" || severity == "P1"),
				}
			}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func maybeCollectReviewWave(state *RunState) {
	if state.ActiveReviewWaveID == "" {
		return
	}
	wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
	if !ok || wave.Status != reviewWaveCollecting {
		return
	}
	for _, target := range wave.Expected {
		switch target.Status {
		case reviewTargetPass, reviewTargetFail, reviewTargetDisposed:
		default:
			return
		}
	}
	findingIDs := syncReviewWaveFindings(state, wave)
	if !wave.Counted {
		state.CompletedReviewWaves++
		wave.Counted = true
	}
	state.Actions["development-worker"] = ActionResult{Status: developmentVerified}
	state.PreRepairSnapshot = ""
	if len(findingIDs) == 0 {
		wave.Status = reviewWaveComplete
	} else {
		wave.Status = reviewWaveAdjudicating
	}
	state.ReviewWaves[wave.ID] = wave
}

func effectiveFindingAdjudication(finding PostReviewFinding) (FindingAdjudication, bool) {
	if len(finding.Adjudications) == 0 {
		return FindingAdjudication{}, false
	}
	return finding.Adjudications[len(finding.Adjudications)-1], true
}

func validateProblemDimensions(dimensions ProblemDimensions) error {
	for name, value := range map[string]string{
		"requirement": dimensions.Requirement, "normal failure path": dimensions.NormalFailurePath,
		"affected module": dimensions.AffectedModule, "root cause": dimensions.RootCause,
		"actual consequence": dimensions.ActualConsequence,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("finding adjudication %s is required", name)
		}
	}
	return nil
}

func validatePostReviewVerification(value FindingVerification) error {
	for name, evidence := range map[string]string{
		"scope": value.Scope, "severity": value.Severity,
		"causality": value.Causality, "reproduction": value.Reproduction,
	} {
		if strings.TrimSpace(evidence) == "" {
			return fmt.Errorf("finding adjudication %s verification is required", name)
		}
	}
	return nil
}

func AdjudicateReviewWave(root, packageRoot, runID string, input ReviewWaveAdjudicationInput) (RunState, error) {
	return openRecord(root, packageRoot, runID, true, false, func(state *RunState, _ PromptCatalog) error {
		ensureConvergenceState(state)
		if input.StateRevision == "" || input.StateRevision != state.StateIntegrity {
			return fmt.Errorf("review-wave adjudication state revision is stale or missing")
		}
		wave, ok := state.ReviewWaves[input.WaveID]
		if !ok || (wave.Status != reviewWaveAdjudicating && wave.Status != reviewWaveReadyForRepair && wave.Status != reviewWaveComplete) {
			return fmt.Errorf("review-wave %q is not ready for adjudication or correction", input.WaveID)
		}
		waveFindingIDs := map[string]bool{}
		for id, finding := range state.PostReviewFindings {
			if finding.WaveID == wave.ID {
				waveFindingIDs[id] = true
			}
		}
		seen := map[string]bool{}
		for _, item := range input.Findings {
			id := strings.TrimSpace(item.FindingID)
			if !waveFindingIDs[id] || seen[id] {
				return fmt.Errorf("finding %q is outside review-wave %s or duplicated", id, wave.ID)
			}
			seen[id] = true
			decision := strings.ToUpper(strings.TrimSpace(item.Decision))
			if decision != "NEW" && decision != "SAME_AS" && decision != "DISMISSED" {
				return fmt.Errorf("finding %s decision must be NEW, SAME_AS, or DISMISSED", id)
			}
			if err := validateProblemDimensions(item.Dimensions); err != nil {
				return err
			}
			if err := validatePostReviewVerification(item.Verification); err != nil {
				return err
			}
			if strings.TrimSpace(item.Reason) == "" || len(item.References) == 0 {
				return fmt.Errorf("finding %s adjudication requires a non-empty reason and references", id)
			}
			finding := state.PostReviewFindings[id]
			problemID := strings.TrimSpace(item.ProblemID)
			switch decision {
			case "NEW":
				if problemID != "" {
					return fmt.Errorf("finding %s NEW decision must not supply an existing problem id", id)
				}
				problemID = "problem-" + shortDigest(digestValue(struct{ WaveID, FindingID string }{wave.ID, id}))
				if _, exists := state.Problems[problemID]; !exists {
					state.Problems[problemID] = CanonicalProblem{ID: problemID, Dimensions: item.Dimensions, Status: "OPEN", FindingIDs: []string{id}, Evidence: append([]string{}, item.References...), CreatedWaveID: wave.ID}
				}
			case "SAME_AS":
				problem, exists := state.Problems[problemID]
				if problemID == "" || !exists {
					return fmt.Errorf("finding %s SAME_AS requires an existing problem id", id)
				}
				if !convergenceContainsString(problem.FindingIDs, id) {
					problem.FindingIDs = append(problem.FindingIDs, id)
				}
				problem.Evidence = appendUniqueStrings(problem.Evidence, item.References...)
				state.Problems[problemID] = problem
			case "DISMISSED":
				if problemID != "" {
					return fmt.Errorf("finding %s DISMISSED decision cannot create or bind a problem", id)
				}
			}
			revision := len(finding.Adjudications) + 1
			adjudication := FindingAdjudication{Revision: revision, Decision: decision, ProblemID: problemID, Dimensions: item.Dimensions, Verification: item.Verification, References: append([]string{}, item.References...), Reason: strings.TrimSpace(item.Reason), StateRevision: input.StateRevision}
			if revision > 1 {
				adjudication.Supersedes = revision - 1
			}
			finding.Adjudications = append(finding.Adjudications, adjudication)
			state.PostReviewFindings[id] = finding
		}
		allAdjudicated := true
		repairable := false
		for id := range waveFindingIDs {
			finding := state.PostReviewFindings[id]
			adjudication, exists := effectiveFindingAdjudication(finding)
			if !exists {
				allAdjudicated = false
				continue
			}
			if adjudication.Decision == "DISMISSED" {
				continue
			}
			problem := state.Problems[adjudication.ProblemID]
			if problem.Status == "OPEN" && postReviewFindingRequiresRepair(finding) {
				repairable = true
			}
		}
		if !allAdjudicated {
			wave.Status = reviewWaveAdjudicating
		} else if repairable {
			wave.Status = reviewWaveReadyForRepair
		} else {
			wave.Status = reviewWaveComplete
		}
		state.ReviewWaves[wave.ID] = wave
		return nil
	})
}

func postReviewFindingRequiresRepair(finding PostReviewFinding) bool {
	if finding.SourceTarget == "qa-execution" {
		return true
	}
	return finding.Severity == "P0" || finding.Severity == "P1" || finding.Severity == "P2"
}

func ensureUnifiedRepairPlan(state *RunState) error {
	ensureConvergenceState(state)
	wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
	if !ok || wave.Status != reviewWaveReadyForRepair {
		return fmt.Errorf("the current review-wave has not collected and adjudicated every expected result")
	}
	if wave.RepairPlan != nil {
		return nil
	}
	problemSet := map[string]bool{}
	for _, finding := range state.PostReviewFindings {
		if finding.WaveID != wave.ID || !postReviewFindingRequiresRepair(finding) {
			continue
		}
		adjudication, exists := effectiveFindingAdjudication(finding)
		if !exists || adjudication.Decision == "DISMISSED" {
			continue
		}
		if problem := state.Problems[adjudication.ProblemID]; problem.Status == "OPEN" {
			problemSet[problem.ID] = true
		}
	}
	problemIDs := make([]string, 0, len(problemSet))
	for id := range problemSet {
		problemIDs = append(problemIDs, id)
	}
	sort.Strings(problemIDs)
	if len(problemIDs) == 0 {
		return fmt.Errorf("the adjudicated review-wave has no OPEN problem requiring repair")
	}
	plan := &UnifiedRepairPlan{WaveID: wave.ID, Snapshot: wave.Snapshot, ProblemIDs: problemIDs, Status: "PLANNED"}
	plan.ID = "repair-plan-" + shortDigest(digestValue(plan))
	wave.RepairPlan = plan
	state.ReviewWaves[wave.ID] = wave
	return nil
}

func convergenceRepairInput(state RunState) (string, bool) {
	wave, ok := state.ReviewWaves[state.ActiveReviewWaveID]
	if !ok || wave.RepairPlan == nil {
		return "", false
	}
	lines := []string{"Repair this review-ready snapshot's single adjudicated problem set. Do not add findings that the main agent dismissed or merged into a resolved problem."}
	for _, id := range wave.RepairPlan.ProblemIDs {
		problem := state.Problems[id]
		lines = append(lines, fmt.Sprintf("%s: requirement=%s; path=%s; module=%s; root cause=%s; consequence=%s", id, problem.Dimensions.Requirement, problem.Dimensions.NormalFailurePath, problem.Dimensions.AffectedModule, problem.Dimensions.RootCause, problem.Dimensions.ActualConsequence))
	}
	return strings.Join(lines, "\n"), true
}

func CompleteGateP2Repair(root, packageRoot, runID, waveID string, buildEvidence, testEvidence, affectedCases []string) (RunState, error) {
	return openRecord(root, packageRoot, runID, false, false, func(state *RunState, _ PromptCatalog) error {
		ensureConvergenceState(state)
		wave, ok := state.ReviewWaves[waveID]
		if !ok || wave.RepairPlan == nil || wave.RepairPlan.RepairSnapshot != state.CurrentSnapshot {
			return fmt.Errorf("gate P2 completion requires the repair snapshot for the named unified repair plan")
		}
		if len(buildEvidence) == 0 || len(testEvidence) == 0 {
			return fmt.Errorf("gate P2 completion requires complete build and full project-test evidence")
		}
		for _, credential := range state.P2CompletionCredentials {
			if credential.WaveID == wave.ID {
				return fmt.Errorf("gate P2 repair for review-wave %s is already completed", wave.ID)
			}
		}
		for _, evidence := range append(append([]string{}, buildEvidence...), testEvidence...) {
			if strings.TrimSpace(evidence) == "" {
				return fmt.Errorf("gate P2 completion evidence cannot be empty")
			}
		}
		approvedCases := map[string]bool{}
		for _, testCase := range state.allQACases() {
			if testCase.ReviewStatus == "PASS" {
				approvedCases[testCase.ID] = true
			}
		}
		seenCases := map[string]bool{}
		for _, caseID := range affectedCases {
			caseID = strings.TrimSpace(caseID)
			if caseID == "" || seenCases[caseID] || !approvedCases[caseID] {
				return fmt.Errorf("gate P2 affected-case evidence must name distinct approved QA cases")
			}
			seenCases[caseID] = true
		}
		gateProblems := map[string][]string{}
		for _, problemID := range wave.RepairPlan.ProblemIDs {
			for _, finding := range state.PostReviewFindings {
				adjudication, exists := effectiveFindingAdjudication(finding)
				if !exists || adjudication.ProblemID != problemID || finding.WaveID != wave.ID {
					continue
				}
				if finding.SourceKind != "gate" || finding.Severity != "P2" {
					return fmt.Errorf("repair plan %s is not a pure gate-P2 repair", wave.RepairPlan.ID)
				}
				gateProblems[finding.SourceTarget] = appendUniqueStrings(gateProblems[finding.SourceTarget], problemID)
			}
		}
		if len(gateProblems) == 0 {
			return fmt.Errorf("repair plan %s contains no gate P2 problem", wave.RepairPlan.ID)
		}
		for gateID, problemIDs := range gateProblems {
			prior := state.Gates[gateID]
			prior.SourceSnapshot = wave.Snapshot
			prior.Snapshot = state.CurrentSnapshot
			prior.Status = "PASS"
			state.Gates[gateID] = prior
			state.Carry[gateID] = CarryResult{Decision: "INHERIT", Origin: "GATE_P2_COMPLETION", SourceSnapshot: wave.Snapshot, TargetSnapshot: state.CurrentSnapshot, Message: "gate P2 completed with full developer self-test; no gate re-review"}
			credential := P2CompletionCredential{WaveID: wave.ID, GateID: gateID, ProblemIDs: append([]string{}, problemIDs...), SourceSnapshot: wave.Snapshot, RepairSnapshot: state.CurrentSnapshot, BuildEvidence: append([]string{}, buildEvidence...), TestEvidence: append([]string{}, testEvidence...), AffectedCases: append([]string{}, affectedCases...)}
			credential.ID = "p2-carry-" + shortDigest(digestValue(credential))
			state.P2CompletionCredentials = append(state.P2CompletionCredentials, credential)
			for _, problemID := range problemIDs {
				problem := state.Problems[problemID]
				problem.Status = "RESOLVED"
				problem.ResolvedAt = state.CurrentSnapshot
				state.Problems[problemID] = problem
			}
		}
		wave.RepairPlan.Status = "P2_COMPLETED"
		wave.Status = reviewWaveComplete
		state.ReviewWaves[wave.ID] = wave
		return nil
	})
}

func convergenceContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return values
}

func LoadReviewWaveAdjudication(path string) (ReviewWaveAdjudicationInput, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return ReviewWaveAdjudicationInput{}, err
	}
	var input ReviewWaveAdjudicationInput
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return ReviewWaveAdjudicationInput{}, fmt.Errorf("review-wave adjudication JSON is invalid: %w", err)
	}
	return input, nil
}

func repairRuntime(state RunState) string {
	if strings.TrimSpace(state.StartedAt) == "" {
		return "UNAVAILABLE"
	}
	started, err := time.Parse(time.RFC3339Nano, state.StartedAt)
	if err != nil {
		return "UNAVAILABLE"
	}
	return time.Since(started).Round(time.Second).String()
}

func PreviewRepairAuthorization(root, packageRoot, runID string, estimate RepairRerunEstimate) (RepairAuthorizationPreview, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RepairAuthorizationPreview{}, err
	}
	if _, err := requireCurrentDefinitions(root, state, packageRoot); err != nil {
		return RepairAuthorizationPreview{}, err
	}
	return repairAuthorizationPreviewFromState(state, estimate), nil
}

func repairAuthorizationPreviewFromState(state RunState, estimate RepairRerunEstimate) RepairAuthorizationPreview {
	preview := RepairAuthorizationPreview{RunID: state.RunID, StateRevision: state.StateIntegrity, ReviewWave: state.CompletedReviewWaves, RemainingAutomaticWaves: effectiveReviewWaveLimit(state) - state.CompletedReviewWaves, Runtime: repairRuntime(state), GateSuggestions: map[string]int{}, QAReviewSuggestions: map[string]int{}, Estimate: estimate, EngineAllowance: "UNAVAILABLE"}
	if preview.RemainingAutomaticWaves < 0 {
		preview.RemainingAutomaticWaves = 0
	}
	for _, obligation := range state.DispatchObligations {
		preview.DispatchAttempts += obligation.AttemptsUsed
	}
	for _, id := range state.SelectedGates {
		if isQAMode(id) {
			result := qaModeResult(state, qaDispatchMode(id))
			for _, finding := range result.Findings {
				preview.QAFailures = append(preview.QAFailures, finding.Message)
			}
			caseSet := state.QACaseSets[qaDispatchMode(id)]
			for _, suggestion := range caseSet.Suggestions {
				preview.QAReviewSuggestions[suggestion.Severity+":"+suggestion.Status]++
			}
			continue
		}
		for _, finding := range state.Gates[id].Findings {
			if finding.Severity == "P0" || finding.Severity == "P1" {
				preview.BlockingFindings = append(preview.BlockingFindings, finding.ID+": "+finding.Message)
			} else if finding.Severity == "P2" || finding.Severity == "P3" {
				status := "UNADJUDICATED"
				for _, recorded := range state.PostReviewFindings {
					if recorded.SourceKind != "gate" || recorded.SourceTarget != id || recorded.Message != finding.Message {
						continue
					}
					if adjudication, ok := effectiveFindingAdjudication(recorded); ok {
						status = adjudication.Decision
						if problem := state.Problems[adjudication.ProblemID]; problem.Status != "" {
							status += ":" + problem.Status
						}
					}
					break
				}
				preview.GateSuggestions[finding.Severity+":"+status]++
			}
		}
	}
	sort.Strings(preview.QAFailures)
	sort.Strings(preview.BlockingFindings)
	copy := preview
	copy.Digest = ""
	preview.Digest = digestValue(copy)
	return preview
}

func validateRepairAuthorizationPreview(state RunState, preview RepairAuthorizationPreview, authorization RepairAuthorizationInput) error {
	if authorization.StateRevision == "" || authorization.StateRevision != state.StateIntegrity || preview.StateRevision != state.StateIntegrity {
		return fmt.Errorf("repair authorization preview is stale")
	}
	if authorization.PreviewDigest == "" || authorization.PreviewDigest != preview.Digest {
		return fmt.Errorf("repair authorization requires the current preview digest")
	}
	return nil
}

func unresolvedGateP3(state RunState) []PostReviewFinding {
	var advisories []PostReviewFinding
	for _, finding := range state.PostReviewFindings {
		if finding.SourceKind != "gate" || finding.Severity != "P3" {
			continue
		}
		adjudication, exists := effectiveFindingAdjudication(finding)
		if exists && adjudication.Decision == "DISMISSED" {
			continue
		}
		if exists && adjudication.ProblemID != "" && state.Problems[adjudication.ProblemID].Status == "RESOLVED" {
			continue
		}
		advisories = append(advisories, finding)
	}
	sort.Slice(advisories, func(i, j int) bool { return advisories[i].ID < advisories[j].ID })
	return advisories
}
