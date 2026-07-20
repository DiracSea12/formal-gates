package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const RequirementsSemanticValuesPerAlignment = 8

type RequirementsAlignmentSubmission struct {
	Position              int
	RequirementOrQuestion string
	Source                string
	WhyItMatters          string
	Status                string
	UserAnswer            string
	DownstreamEffect      string
	DocumentImpact        string
	EvidenceNeeded        string
}

type RequirementsDimensionSubmission struct {
	Position               int
	Status                 string
	AlignmentItemPositions []int
	Message                string
}

type ComposeRequirementsOptions struct {
	Root               string
	RunDir             string
	WorkflowID         string
	ChangeSnapshot     string
	OutputDir          string
	PreviousAlignment  string
	ApprovedDroppedIDs []string
	RequirementSource  string
	AlignmentIDs       []string
	CoveredTargets     []string
	Alignments         []RequirementsAlignmentSubmission
	UserOriginal       string
	OpenBlockers       []string
	CoverageScan       string
	ScopePreservation  PassOrNA
	TaskProof          PassOrNA
	Dimensions         []RequirementsDimensionSubmission
}

type ComposeRequirementsOutput struct {
	Alignment    EvidenceRef `json:"alignment"`
	Decision     EvidenceRef `json:"decision"`
	Requirements EvidenceRef `json:"requirements"`
}

func ComposeRequirements(options ComposeRequirementsOptions) (ComposeRequirementsOutput, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return ComposeRequirementsOutput{}, result
	}
	if !meaningful(options.WorkflowID) || !meaningful(options.ChangeSnapshot) || strings.TrimSpace(options.OutputDir) == "" || strings.TrimSpace(options.RequirementSource) == "" || len(options.CoveredTargets) == 0 {
		result.add("compose", "--workflow-id, --change-snapshot, --output-dir, --requirement-source, and at least one --covered-target are required")
		return ComposeRequirementsOutput{}, result
	}
	if len(options.AlignmentIDs) == 0 || len(options.AlignmentIDs) != len(options.Alignments) {
		result.add("alignment", "exactly one positioned alignment submission is required for each --alignment-id")
		return ComposeRequirementsOutput{}, result
	}
	requirementSource := strings.TrimSpace(options.RequirementSource)
	coveredTargets := make([]string, len(options.CoveredTargets))
	for i, target := range options.CoveredTargets {
		coveredTargets[i] = strings.TrimSpace(target)
	}
	sort.Strings(coveredTargets)
	validateCoveredTargets(coveredTargets, &result, "covered-target")
	if !result.OK() {
		return ComposeRequirementsOutput{}, result
	}
	items := make([]AlignmentItem, len(options.Alignments))
	seenIDs := map[string]bool{}
	seenPositions := map[int]bool{}
	validAlignmentStatuses := map[string]bool{"CONFIRMED": true, "DEFERRED": true, "DROPPED": true, "WITHDRAWN": true}
	for _, submission := range options.Alignments {
		if submission.Position < 1 || submission.Position > len(items) || seenPositions[submission.Position] {
			result.add("alignment", "alignment positions must be unique and cover 1 through the number of --alignment-id values")
			continue
		}
		seenPositions[submission.Position] = true
		i := submission.Position - 1
		id := strings.TrimSpace(options.AlignmentIDs[i])
		if !regexp.MustCompile(`^RQ-[0-9]{3}$`).MatchString(id) || seenIDs[id] {
			result.add("alignment-id", "--alignment-id values must be unique RQ-### identifiers")
			continue
		}
		seenIDs[id] = true
		values := []string{submission.RequirementOrQuestion, submission.Source, submission.WhyItMatters, submission.Status, submission.UserAnswer, submission.DownstreamEffect, submission.DocumentImpact, submission.EvidenceNeeded}
		if !allMeaningful(values) || containsPending(values) || !validAlignmentStatuses[submission.Status] {
			result.add("alignment", fmt.Sprintf("alignment position %d requires eight non-empty values and an approved status", submission.Position))
			continue
		}
		items[i] = AlignmentItem{
			ID: id, RequirementOrQuestion: submission.RequirementOrQuestion, Source: submission.Source,
			WhyItMatters: submission.WhyItMatters, Status: submission.Status, UserAnswer: submission.UserAnswer,
			DownstreamEffect: submission.DownstreamEffect, DocumentImpact: submission.DocumentImpact, EvidenceNeeded: submission.EvidenceNeeded,
		}
	}
	if len(seenPositions) != len(items) {
		result.add("alignment", "alignment positions must cover every --alignment-id exactly once")
	}
	if strings.TrimSpace(options.UserOriginal) == "" || containsPending([]string{options.UserOriginal}) || options.CoverageScan != "PASS" || len(options.OpenBlockers) != 0 {
		result.add("requirements-semantics", "--user-original is required, --coverage-scan must be PASS, and Requirements PASS cannot contain open blockers")
	}
	validateComposePassOrNA("scope", options.ScopePreservation, &result)
	validateComposePassOrNA("task", options.TaskProof, &result)
	if len(options.Dimensions) != len(dimensionIDs) {
		result.add("dimension", "exactly 13 positioned dimension submissions are required")
	}
	dimensions := make([]DimensionCoverage, len(dimensionIDs))
	seenDimensions := map[int]bool{}
	for _, submission := range options.Dimensions {
		if submission.Position < 1 || submission.Position > len(dimensionIDs) || seenDimensions[submission.Position] {
			result.add("dimension", "dimension positions must be unique and cover 1 through 13")
			continue
		}
		seenDimensions[submission.Position] = true
		if !map[string]bool{"COVERED": true, "DEFERRED": true, "NOT_APPLICABLE": true}[submission.Status] || strings.TrimSpace(submission.Message) == "" || containsPending([]string{submission.Message}) || len(submission.AlignmentItemPositions) == 0 {
			result.add("dimension", fmt.Sprintf("dimension position %d requires a valid status, message, and alignment item", submission.Position))
			continue
		}
		alignmentIDs := make([]string, 0, len(submission.AlignmentItemPositions))
		seenNumbers := map[int]bool{}
		for _, number := range submission.AlignmentItemPositions {
			if number < 1 || number > len(items) || seenNumbers[number] {
				result.add("dimension", fmt.Sprintf("dimension position %d must reference unique valid alignment positions", submission.Position))
				continue
			}
			seenNumbers[number] = true
			alignmentIDs = append(alignmentIDs, items[number-1].ID)
		}
		sort.Strings(alignmentIDs)
		dimensions[submission.Position-1] = DimensionCoverage{ID: dimensionIDs[submission.Position-1], Status: submission.Status, AlignmentIDs: alignmentIDs, Message: submission.Message}
	}
	if len(seenDimensions) != len(dimensionIDs) {
		result.add("dimension", "dimension positions must cover 1 through 13 exactly once")
	}
	if !result.OK() {
		return ComposeRequirementsOutput{}, result
	}
	var previous *EvidenceRef
	removedIDs := []string{}
	if strings.TrimSpace(options.PreviousAlignment) != "" {
		ref, refErr := registeredEvidenceRef(root, runDir, options.PreviousAlignment)
		if refErr != nil {
			result.add(options.PreviousAlignment, refErr.Error())
			return ComposeRequirementsOutput{}, result
		}
		previous = &ref
		previousData, readErr := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(ref.Path)))
		if readErr != nil {
			result.add(options.PreviousAlignment, readErr.Error())
			return ComposeRequirementsOutput{}, result
		}
		var previousArtifact AlignmentArtifact
		if decodeErr := strictContractJSON(previousData, &previousArtifact); decodeErr != nil {
			result.add(options.PreviousAlignment, "previous alignment JSON is invalid: "+decodeErr.Error())
			return ComposeRequirementsOutput{}, result
		}
		currentIDs := alignmentIDSet(items)
		for _, item := range previousArtifact.Items {
			if !currentIDs[item.ID] {
				removedIDs = append(removedIDs, item.ID)
			}
		}
		sort.Strings(removedIDs)
		approvedDroppedIDs := append([]string{}, options.ApprovedDroppedIDs...)
		sort.Strings(approvedDroppedIDs)
		if !equalStringSlices(approvedDroppedIDs, removedIDs) {
			result.add("approved-dropped-id", "every alignment ID removed from previous-alignment requires explicit per-item approval")
			return ComposeRequirementsOutput{}, result
		}
	} else if len(options.ApprovedDroppedIDs) != 0 {
		result.add("approved-dropped-id", "--approved-dropped-id requires --previous-alignment")
		return ComposeRequirementsOutput{}, result
	}
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		result.add("run-dir", err.Error())
		return ComposeRequirementsOutput{}, result
	}
	outputDir, err := prospectiveRestrictedPath(runDir, options.OutputDir)
	if err != nil {
		result.add(options.OutputDir, "output directory must be under the active run restricted directory")
		return ComposeRequirementsOutput{}, result
	}
	alignmentPath := filepath.Join(outputDir, "alignment.json")
	decisionPath := filepath.Join(outputDir, "decision.json")
	requirementsPath := filepath.Join(outputDir, "requirements.json")
	for _, path := range []string{alignmentPath, decisionPath, requirementsPath} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			result.add(relativePath(root, path), "generated requirements output already exists")
		}
	}
	if !result.OK() {
		return ComposeRequirementsOutput{}, result
	}
	alignment := AlignmentArtifact{SchemaVersion: 2, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Items: items}
	if err := writeJSONExclusive(alignmentPath, alignment); err != nil {
		result.add(relativePath(root, alignmentPath), err.Error())
		return ComposeRequirementsOutput{}, result
	}
	proofPath := ""
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(alignmentPath)
			_ = os.Remove(decisionPath)
			_ = os.Remove(requirementsPath)
			if proofPath != "" {
				_ = os.Remove(proofPath)
			}
			_ = os.Remove(outputDir)
		}
	}()
	alignmentLogical, _ := logicalPathInRun(runDir, alignmentPath)
	alignmentRef := EvidenceRef{Path: alignmentLogical, SHA256: sha256File(alignmentPath)}
	approvedIDs := make([]string, 0, len(items))
	openQuestionIDs := []string{}
	for _, item := range items {
		approvedIDs = append(approvedIDs, item.ID)
		if item.Status == "OPEN" {
			openQuestionIDs = append(openQuestionIDs, item.ID)
		}
	}
	decision := RequirementsDecision{
		SchemaVersion: 2, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
		DecisionType: "USER_CONFIRMATION", UserConfirmation: true, UserOriginal: options.UserOriginal,
		Alignment: alignmentRef, ApprovedAlignmentIDs: approvedIDs,
		ApprovedDroppedIDs: removedIDs, ApprovalScope: "requirements-clarification-gate",
	}
	if err := writeJSONExclusive(decisionPath, decision); err != nil {
		result.add(relativePath(root, decisionPath), err.Error())
		return ComposeRequirementsOutput{}, result
	}
	decisionLogical, _ := logicalPathInRun(runDir, decisionPath)
	decisionRef := EvidenceRef{Path: decisionLogical, SHA256: sha256File(decisionPath)}
	payload := RequirementsPayload{
		RequirementSource: requirementSource, Alignment: alignmentRef,
		TotalAlignmentItems: len(items), OpenQuestionIDs: openQuestionIDs,
		OpenBlockers: append([]string{}, options.OpenBlockers...), DroppedQuestionIDs: removedIDs,
		DroppedQuestionApproval: len(removedIDs) > 0, UserConfirmation: true,
		CoverageScan: options.CoverageScan, ScopePreservation: options.ScopePreservation,
		TaskProof: options.TaskProof, DimensionCoverage: dimensions, Decision: decisionRef,
		CoveredTargets: coveredTargets, DownstreamPermission: "READY_TO_DRAFT",
	}
	payload.PreviousAlignment = previous
	payloadBytes, _ := json.Marshal(payload)
	requirements := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "REQUIREMENTS_PASS", WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Gate: "requirements-clarification-gate", Stage: "", Verdict: "PASS", Payload: payloadBytes}
	if err := writeJSONExclusive(requirementsPath, requirements); err != nil {
		result.add(relativePath(root, requirementsPath), err.Error())
		return ComposeRequirementsOutput{}, result
	}
	requirementsLogical, _ := logicalPathInRun(runDir, requirementsPath)
	requirementsRef := EvidenceRef{Path: requirementsLogical, SHA256: sha256File(requirementsPath)}
	proofRef, err := writeCompositionProof(root, runDir, "requirements.v1", options.WorkflowID, options.ChangeSnapshot, requirementsPath, []EvidenceRef{alignmentRef, decisionRef, requirementsRef})
	if err != nil {
		result.add(relativePath(root, requirementsPath), "cannot write requirements composition proof: "+err.Error())
		return ComposeRequirementsOutput{}, result
	}
	proofPath = filepath.Join(runDir, filepath.FromSlash(proofRef.Path))
	artifactResult := Artifact(ArtifactOptions{Root: root, RunDir: runDir, File: relativePath(root, requirementsPath), Gate: "requirements-clarification-gate", WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot})
	if !artifactResult.OK() {
		result.Failures = append(result.Failures, artifactResult.Failures...)
		return ComposeRequirementsOutput{}, result
	}
	cleanup = false
	return ComposeRequirementsOutput{Alignment: alignmentRef, Decision: decisionRef, Requirements: requirementsRef}, result
}

func allMeaningful(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateComposePassOrNA(label string, value PassOrNA, result *Result) {
	if (value.Status != "PASS" && value.Status != "NOT_APPLICABLE") || strings.TrimSpace(value.Message) == "" || containsPending([]string{value.Message}) {
		result.add(label, "status must be PASS or NOT_APPLICABLE and message must be non-empty")
	}
}

type ComposeQAExecutionOptions struct {
	Root, RunDir, WorkflowID, ChangeSnapshot, Output string
	ApprovedCaseSet, DesignReview, QAOwnedResults    string
	CaseResultBinding, ChangedFiles, Verification    string
}

func ComposeQAExecution(options ComposeQAExecutionOptions) (EvidenceRef, Result) {
	var result Result
	root := cleanWorktree(options.Root)
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	resolve := func(label, logical string) EvidenceRef {
		if strings.TrimSpace(logical) == "" {
			result.add("compose", "--"+label+" is required")
			return EvidenceRef{}
		}
		ref, refErr := registeredEvidenceRef(root, runDir, logical)
		if refErr != nil {
			result.add(logical, refErr.Error())
			return ref
		}
		composer := map[string]string{"changed-files": "changed-files.v1", "verification": "verification.v1"}[label]
		if composer != "" {
			if proofErr := validateStandaloneCompositionProof(root, runDir, composer, options.WorkflowID, options.ChangeSnapshot, ref); proofErr != nil {
				result.add(logical, proofErr.Error())
			}
		}
		return ref
	}
	payload := QAExecutionPayload{
		ApprovedCaseSet:   resolve("approved-case-set", options.ApprovedCaseSet),
		DesignReview:      resolve("design-review", options.DesignReview),
		QAOwnedResults:    resolve("qa-owned-results", options.QAOwnedResults),
		CaseResultBinding: resolve("case-result-binding", options.CaseResultBinding),
		ChangedFiles:      resolve("changed-files", options.ChangedFiles),
		Verification:      resolve("verification", options.Verification),
	}
	if result.OK() && meaningful(options.WorkflowID) && meaningful(options.ChangeSnapshot) {
		qaOwnedPair := []EvidenceRef{payload.QAOwnedResults, payload.CaseResultBinding}
		if proofErr := validateCompositionProofOutputs(root, runDir, "qa-owned-evidence.v1", options.WorkflowID, options.ChangeSnapshot, payload.QAOwnedResults, qaOwnedPair); proofErr != nil {
			result.add(options.QAOwnedResults, proofErr.Error())
		}
	}
	if !result.OK() || !meaningful(options.ChangeSnapshot) {
		if !meaningful(options.ChangeSnapshot) {
			result.add("compose", "--change-snapshot is required")
		}
		return EvidenceRef{}, result
	}
	outputPath, err := prospectiveRestrictedPath(runDir, options.Output)
	if err != nil {
		result.add(options.Output, "output must be under the active run restricted directory")
		return EvidenceRef{}, result
	}
	if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
		result.add(options.Output, "generated QA Execution output already exists")
		return EvidenceRef{}, result
	}
	payloadBytes, _ := json.Marshal(payload)
	envelope := FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS", Payload: payloadBytes}
	if err := writeJSONExclusive(outputPath, envelope); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	logical, _ := logicalPathInRun(runDir, outputPath)
	artifactRef := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	proofRef, err := writeCompositionProof(root, runDir, "qa-execution.v1", options.WorkflowID, options.ChangeSnapshot, outputPath, []EvidenceRef{artifactRef})
	if err != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, "cannot write QA Execution composition proof: "+err.Error())
		return EvidenceRef{}, result
	}
	artifactResult := Artifact(ArtifactOptions{Root: root, RunDir: runDir, File: relativePath(root, outputPath), Gate: "qa-test-gate", Stage: "Execution", WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot})
	if !artifactResult.OK() {
		_ = os.Remove(outputPath)
		_ = os.Remove(filepath.Join(runDir, filepath.FromSlash(proofRef.Path)))
		result.Failures = append(result.Failures, artifactResult.Failures...)
		return EvidenceRef{}, result
	}
	return artifactRef, result
}

// prospectiveRestrictedPath resolves a run-local output that may not exist yet.
// Existing path components are canonicalized so a normal symlinked workspace
// cannot move generated evidence outside the active run's restricted directory.
func prospectiveRestrictedPath(runDir, logical string) (string, error) {
	logical = strings.TrimSpace(logical)
	if logical == "" || logical == "." || filepath.IsAbs(logical) || strings.Contains(logical, "\\") {
		return "", fmt.Errorf("unsafe run-local output path")
	}
	parts := strings.Split(logical, "/")
	if len(parts) < 2 || parts[0] != "restricted" {
		return "", fmt.Errorf("output path must start with restricted/")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe run-local output path")
		}
	}
	restricted, err := filepath.EvalSymlinks(filepath.Join(absPath(runDir), "restricted"))
	if err != nil {
		return "", err
	}
	candidate := restricted
	for index, part := range parts[1:] {
		next := filepath.Join(candidate, part)
		resolved, resolveErr := filepath.EvalSymlinks(next)
		if os.IsNotExist(resolveErr) {
			candidate = filepath.Join(candidate, filepath.Join(parts[index+1:]...))
			break
		}
		if resolveErr != nil {
			return "", resolveErr
		}
		candidate = resolved
		if !samePath(candidate, restricted) && !pathUnder(candidate, restricted) {
			return "", fmt.Errorf("output path escapes restricted directory")
		}
	}
	if samePath(candidate, restricted) || !pathUnder(candidate, restricted) {
		return "", fmt.Errorf("output path must be below restricted directory")
	}
	return candidate, nil
}
