package validate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type EvidenceRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type FormalGateEvidence struct {
	SchemaVersion  int             `json:"schemaVersion"`
	ArtifactRole   string          `json:"artifactRole"`
	WorkflowID     string          `json:"workflowId"`
	ChangeSnapshot string          `json:"changeSnapshot"`
	Gate           string          `json:"gate"`
	Stage          string          `json:"stage"`
	Verdict        string          `json:"verdict"`
	Payload        json.RawMessage `json:"payload"`
}
type PassOrNA struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}
type DimensionCoverage struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	AlignmentIDs []string `json:"alignmentIds"`
	Message      string   `json:"message"`
}
type RequirementsPayload struct {
	RequirementSource       string              `json:"requirementSource"`
	Alignment               EvidenceRef         `json:"alignment"`
	TotalAlignmentItems     int                 `json:"totalAlignmentItems"`
	PreviousAlignment       *EvidenceRef        `json:"previousAlignment,omitempty"`
	OpenQuestionIDs         []string            `json:"openQuestionIds"`
	OpenBlockers            []string            `json:"openBlockers"`
	DroppedQuestionIDs      []string            `json:"droppedQuestionIds"`
	DroppedQuestionApproval bool                `json:"droppedQuestionApproval"`
	UserConfirmation        bool                `json:"userConfirmation"`
	CoverageScan            string              `json:"coverageScan"`
	ScopePreservation       PassOrNA            `json:"scopePreservation"`
	TaskProof               PassOrNA            `json:"taskProof"`
	DimensionCoverage       []DimensionCoverage `json:"dimensionCoverage"`
	Decision                EvidenceRef         `json:"decision"`
	CoveredTargets          []string            `json:"coveredTargets"`
	DownstreamPermission    string              `json:"downstreamPermission"`
}
type AlignmentArtifact struct {
	SchemaVersion  int             `json:"schemaVersion"`
	WorkflowID     string          `json:"workflowId"`
	ChangeSnapshot string          `json:"changeSnapshot"`
	Items          []AlignmentItem `json:"items"`
}
type AlignmentItem struct {
	ID                    string `json:"id"`
	RequirementOrQuestion string `json:"requirementOrQuestion"`
	Source                string `json:"source"`
	WhyItMatters          string `json:"whyItMatters"`
	Status                string `json:"status"`
	UserAnswer            string `json:"userAnswer"`
	DownstreamEffect      string `json:"downstreamEffect"`
	DocumentImpact        string `json:"documentImpact"`
	EvidenceNeeded        string `json:"evidenceNeeded"`
}
type RequirementsDecision struct {
	SchemaVersion        int         `json:"schemaVersion"`
	WorkflowID           string      `json:"workflowId"`
	ChangeSnapshot       string      `json:"changeSnapshot"`
	DecisionType         string      `json:"decisionType"`
	UserConfirmation     bool        `json:"userConfirmation"`
	UserOriginal         string      `json:"userOriginal"`
	Alignment            EvidenceRef `json:"alignment"`
	ApprovedAlignmentIDs []string    `json:"approvedAlignmentIds"`
	ApprovedDroppedIDs   []string    `json:"approvedDroppedIds"`
	ApprovalScope        string      `json:"approvalScope"`
}
type Location struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}
type Finding struct {
	Message   string     `json:"message"`
	Locations []Location `json:"locations"`
}
type ReviewCheck struct {
	ID           string        `json:"id"`
	Status       string        `json:"status"`
	Message      string        `json:"message"`
	EvidenceRefs []EvidenceRef `json:"evidenceRefs"`
	Findings     []Finding     `json:"findings"`
}
type ReviewerPayload struct {
	Dispatch       EvidenceRef   `json:"dispatch"`
	ContextBundle  EvidenceRef   `json:"contextBundle"`
	ReviewPolicyID string        `json:"reviewPolicyId"`
	Checks         []ReviewCheck `json:"checks"`
	ChangedFiles   *EvidenceRef  `json:"changedFiles,omitempty"`
	Verification   *EvidenceRef  `json:"verification,omitempty"`
}
type QAExecutionPayload struct {
	ApprovedCaseSet   EvidenceRef `json:"approvedCaseSet"`
	QAOwnedResults    EvidenceRef `json:"qaOwnedResults"`
	CaseResultBinding EvidenceRef `json:"caseResultBinding"`
	ChangedFiles      EvidenceRef `json:"changedFiles"`
	Verification      EvidenceRef `json:"verification"`
}
type qaResultsArtifact struct {
	Owner          string `json:"owner"`
	WorkflowID     string `json:"workflowId"`
	ChangeSnapshot string `json:"changeSnapshot"`
	Stage          string `json:"stage"`
	Status         string `json:"status"`
	OverallOutcome string `json:"overallOutcome"`
	Executions     []struct {
		ID        string `json:"id"`
		Outcome   string `json:"outcome"`
		Procedure string `json:"procedure"`
		Result    string `json:"result"`
	} `json:"executions"`
	CaseResults []struct {
		CaseID     string   `json:"caseId"`
		Status     string   `json:"status"`
		Procedures []string `json:"procedures"`
		Oracle     string   `json:"oracle"`
	} `json:"caseResults"`
}
type qaCaseBindingArtifact struct {
	WorkflowID      string          `json:"workflowId"`
	ChangeSnapshot  string          `json:"changeSnapshot"`
	ApprovedCaseSet EvidenceRef     `json:"approvedCaseSet"`
	QAOwnedResults  EvidenceRef     `json:"qaOwnedResults"`
	Complete        bool            `json:"complete"`
	Bindings        []qaCaseBinding `json:"bindings"`
}
type qaCaseBinding struct {
	CaseID        string   `json:"caseId"`
	ResultPointer string   `json:"resultPointer"`
	Status        string   `json:"status"`
	ExecutionRefs []string `json:"executionRefs"`
	Procedures    []string `json:"procedures"`
	Oracle        string   `json:"oracle"`
}
type ContextBundle struct {
	BundleVersion  int           `json:"bundleVersion"`
	WorkflowID     string        `json:"workflowId"`
	ChangeSnapshot string        `json:"changeSnapshot"`
	Inputs         []EvidenceRef `json:"inputs"`
}
type FinalExecutionPayload struct {
	Mode              string         `json:"mode"`
	GateMatrix        []FinalGateRow `json:"gateMatrix"`
	FinalVerification EvidenceRef    `json:"finalVerification"`
	ReleaseJudgment   string         `json:"releaseJudgment"`
}
type FinalGateRow struct {
	Gate         string      `json:"gate"`
	GateEvidence EvidenceRef `json:"gateEvidence"`
}
type decodedArtifact struct {
	Envelope     FormalGateEvidence
	Policy       ArtifactPolicy
	Requirements *RequirementsPayload
	QAExecution  *QAExecutionPayload
	Reviewer     *ReviewerPayload
	Final        *FinalExecutionPayload
	References   map[string][]EvidenceRef
	RunDir       string
}

func decodeArtifact(options ArtifactOptions, data []byte, result *Result) decodedArtifact {
	var out decodedArtifact
	if err := strictContractJSON(data, &out.Envelope); err != nil {
		result.add(options.File, "formal artifact JSON is invalid: "+err.Error())
		return out
	}
	e := out.Envelope
	if e.SchemaVersion != 2 {
		result.add(options.File, "schemaVersion must be 2")
	}
	if e.WorkflowID == "" || e.ChangeSnapshot == "" {
		result.add(options.File, "workflowId and changeSnapshot are required")
	}
	if options.WorkflowID != "" && e.WorkflowID != options.WorkflowID {
		result.add(options.File, "workflowId must match --workflow-id")
	}
	if options.ChangeSnapshot != "" && e.ChangeSnapshot != options.ChangeSnapshot {
		result.add(options.File, "changeSnapshot must match --change-snapshot")
	}
	if e.Gate != options.Gate || normalizeStage(e.Stage) != normalizeStage(options.Stage) {
		result.add(options.File, "artifact role, gate, or stage does not match the requested gate")
	}
	out.RunDir = artifactRunDir(options, e.WorkflowID)
	out.References = map[string][]EvidenceRef{}
	switch e.ArtifactRole {
	case "REQUIREMENTS_PASS":
		if e.Verdict != "PASS" {
			result.add(options.File, "REQUIREMENTS_PASS accepts only PASS")
		}
		policy, ok := fixedPolicy(e.ArtifactRole, e.Gate, e.Stage)
		if !ok {
			result.add(options.File, "unsupported Phase 1 artifact role/gate/stage")
			break
		}
		out.Policy = policy
		var payload RequirementsPayload
		if err := strictContractJSON(e.Payload, &payload); err != nil {
			result.add(options.File, "requirements payload is invalid: "+err.Error())
			break
		}
		out.Requirements = &payload
		validateRequirements(options, &out, result)
	case "QA_EXECUTION":
		if e.Verdict != "PASS" {
			result.add(options.File, "QA_EXECUTION accepts only PASS")
		}
		policy, ok := fixedPolicy(e.ArtifactRole, e.Gate, e.Stage)
		if !ok {
			result.add(options.File, "unsupported Phase 1 artifact role/gate/stage")
			break
		}
		out.Policy = policy
		var payload QAExecutionPayload
		if err := strictContractJSON(e.Payload, &payload); err != nil {
			result.add(options.File, "QA Execution payload is invalid: "+err.Error())
			break
		}
		out.QAExecution = &payload
		validateQAExecution(options, &out, result)
	case "COMPLEXITY_REVIEW", "ARCHITECTURE_REVIEW", "CODE_QUALITY_REVIEW":
		if !reviewVerdict[e.Verdict] {
			result.add(options.File, "reviewer verdict must be PASS, REVIEW, FAIL, or BLOCKED")
		}
		var payload ReviewerPayload
		if err := strictContractJSON(e.Payload, &payload); err != nil {
			result.add(options.File, "reviewer payload is invalid: "+err.Error())
			break
		}
		out.Reviewer = &payload
		validateReviewer(options, &out, result)
	case "FINAL_EXECUTION":
		if e.Verdict != "PASS" {
			result.add(options.File, "FINAL_EXECUTION accepts only PASS")
		}
		policy, ok := fixedPolicy(e.ArtifactRole, e.Gate, e.Stage)
		if !ok {
			result.add(options.File, "unsupported Phase 1 artifact role/gate/stage")
			break
		}
		out.Policy = policy
		var payload FinalExecutionPayload
		if err := strictContractJSON(e.Payload, &payload); err != nil {
			result.add(options.File, "FinalExecution payload is invalid: "+err.Error())
			break
		}
		out.Final = &payload
		validateFinalExecution(options, &out, result)
	default:
		result.add(options.File, "unsupported Phase 1 artifactRole: "+e.ArtifactRole)
	}
	if out.Policy.ID != "" && options.Flow != "" && out.Policy.Flow != options.Flow {
		result.add(options.File, fmt.Sprintf("policy flow %s does not match recording flow %s", out.Policy.Flow, options.Flow))
	}
	return out
}

var reviewVerdict = map[string]bool{"PASS": true, "REVIEW": true, "FAIL": true, "BLOCKED": true}

func validateRequirements(options ArtifactOptions, artifact *decodedArtifact, result *Result) {
	p := artifact.Requirements
	e := artifact.Envelope
	if strings.TrimSpace(p.RequirementSource) == "" || p.TotalAlignmentItems <= 0 {
		result.add(options.File, "requirementSource must be non-empty and totalAlignmentItems must be positive")
	}
	if p.OpenQuestionIDs == nil || p.OpenBlockers == nil || p.DroppedQuestionIDs == nil || p.DimensionCoverage == nil || p.CoveredTargets == nil {
		result.add(options.File, "required requirements arrays must be present")
	}
	if len(p.OpenQuestionIDs) != 0 || len(p.OpenBlockers) != 0 || !p.UserConfirmation || p.CoverageScan != "PASS" || p.DownstreamPermission != "READY_TO_DRAFT" {
		result.add(options.File, "requirements PASS eligibility fields are not complete")
	}
	validatePassOrNA(options.File, "scopePreservation", p.ScopePreservation, result)
	validatePassOrNA(options.File, "taskProof", p.TaskProof, result)
	alignmentData, ok := readEvidenceRef(options, artifact.RunDir, p.Alignment, result)
	if !ok {
		return
	}
	var alignment AlignmentArtifact
	if err := strictContractJSON(alignmentData, &alignment); err != nil {
		result.add(p.Alignment.Path, "alignment JSON is invalid: "+err.Error())
		return
	}
	artifact.References[options.File] = append(artifact.References[options.File], p.Alignment)
	currentIDs := validateAlignment(e, alignment, p.TotalAlignmentItems, result, p.Alignment.Path)
	removed := []string{}
	if p.PreviousAlignment != nil {
		data, valid := readEvidenceRef(options, artifact.RunDir, *p.PreviousAlignment, result)
		if valid {
			var previous AlignmentArtifact
			if err := strictContractJSON(data, &previous); err != nil {
				result.add(p.PreviousAlignment.Path, "previous alignment JSON is invalid: "+err.Error())
			} else {
				previousIDs := alignmentIDSet(previous.Items)
				for id := range previousIDs {
					if !currentIDs[id] {
						removed = append(removed, id)
					}
				}
				sort.Strings(removed)
			}
		}
		artifact.References[options.File] = append(artifact.References[options.File], *p.PreviousAlignment)
	}
	decisionData, ok := readEvidenceRef(options, artifact.RunDir, p.Decision, result)
	if !ok {
		return
	}
	var decision RequirementsDecision
	if err := strictContractJSON(decisionData, &decision); err != nil {
		result.add(p.Decision.Path, "decision JSON is invalid: "+err.Error())
		return
	}
	artifact.References[options.File] = append(artifact.References[options.File], p.Decision)
	artifact.References[p.Decision.Path] = []EvidenceRef{decision.Alignment}
	validateRequirementsDecision(e, p, decision, currentIDs, removed, result, p.Decision.Path)
	validateDimensionCatalog(p.DimensionCoverage, currentIDs, result, options.File)
	validateCoveredTargets(p.CoveredTargets, result, options.File)
}
func validatePassOrNA(file, field string, value PassOrNA, result *Result) {
	if value.Status != "PASS" && value.Status != "NOT_APPLICABLE" {
		result.add(file, field+" status must be PASS or NOT_APPLICABLE")
	}
	if value.Status == "NOT_APPLICABLE" && strings.TrimSpace(value.Message) == "" {
		result.add(file, field+" NOT_APPLICABLE requires a message")
	}
}
func validateAlignment(e FormalGateEvidence, a AlignmentArtifact, count int, result *Result, file string) map[string]bool {
	ids := map[string]bool{}
	if a.SchemaVersion != 2 || a.WorkflowID != e.WorkflowID || a.ChangeSnapshot != e.ChangeSnapshot || len(a.Items) == 0 {
		result.add(file, "alignment schema, workflow, snapshot, and non-empty items must match")
	}
	for i, item := range a.Items {
		where := fmt.Sprintf("%s items[%d]", file, i)
		if !regexp.MustCompile(`^RQ-[0-9]{3}$`).MatchString(item.ID) || ids[item.ID] {
			result.add(where, "id must be unique RQ-###")
		}
		ids[item.ID] = true
		if !map[string]bool{"OPEN": true, "CONFIRMED": true, "DEFERRED": true, "DROPPED": true, "WITHDRAWN": true}[item.Status] {
			result.add(where, "status is invalid")
		}
		fields := []string{item.ID, item.RequirementOrQuestion, item.Source, item.WhyItMatters, item.DownstreamEffect, item.DocumentImpact, item.EvidenceNeeded}
		for _, value := range fields {
			if strings.TrimSpace(value) == "" {
				result.add(where, "all PASS item strings must be non-empty")
				break
			}
		}
		if item.Status == "OPEN" || strings.TrimSpace(item.UserAnswer) == "" {
			result.add(where, "PASS item must have an approved disposition and non-empty userAnswer")
		}
	}
	if len(ids) != count {
		result.add(file, "totalAlignmentItems does not match unique alignment IDs")
	}
	return ids
}
func validateRequirementsDecision(e FormalGateEvidence, p *RequirementsPayload, d RequirementsDecision, current map[string]bool, removed []string, result *Result, file string) {
	if d.SchemaVersion != 2 || d.WorkflowID != e.WorkflowID || d.ChangeSnapshot != e.ChangeSnapshot || d.DecisionType != "USER_CONFIRMATION" || !d.UserConfirmation || strings.TrimSpace(d.UserOriginal) == "" || d.ApprovalScope != "requirements-clarification-gate" {
		result.add(file, "decision binding and PASS fields are invalid")
	}
	if d.Alignment != p.Alignment {
		result.add(file, "decision alignment reference must match requirements alignment")
	}
	if !sameIDSet(d.ApprovedAlignmentIDs, current) {
		result.add(file, "approvedAlignmentIds must equal all current alignment IDs")
	}
	removedSet := stringSet(removed)
	if !sameIDSet(d.ApprovedDroppedIDs, removedSet) || !sameIDSet(p.DroppedQuestionIDs, removedSet) {
		result.add(file, "removed IDs must match approvedDroppedIds and droppedQuestionIds")
	}
	if p.DroppedQuestionApproval != (len(removed) > 0) {
		result.add(file, "droppedQuestionApproval does not match removed ID set")
	}
	for _, id := range d.ApprovedDroppedIDs {
		if current[id] {
			result.add(file, "current alignment ID cannot be approved as removed: "+id)
		}
	}
}

var dimensionIDs = []string{"DIM-01", "DIM-02", "DIM-03", "DIM-04", "DIM-05", "DIM-06", "DIM-07", "DIM-08", "DIM-09", "DIM-10", "DIM-11", "DIM-12", "DIM-13"}

func validateDimensionCatalog(values []DimensionCoverage, ids map[string]bool, result *Result, file string) {
	want, seen := stringSet(dimensionIDs), map[string]bool{}
	for _, value := range values {
		if !want[value.ID] || seen[value.ID] {
			result.add(file, "dimensionCoverage contains an unknown or duplicate id: "+value.ID)
		}
		seen[value.ID] = true
		if !map[string]bool{"COVERED": true, "DEFERRED": true, "NOT_APPLICABLE": true}[value.Status] {
			result.add(file, "dimensionCoverage status is invalid: "+value.ID)
		}
		if len(value.AlignmentIDs) == 0 || hasDuplicate(value.AlignmentIDs) {
			result.add(file, "dimensionCoverage alignmentIds must be non-empty and unique: "+value.ID)
		}
		for _, id := range value.AlignmentIDs {
			if !ids[id] {
				result.add(file, "dimensionCoverage references unknown alignment id: "+id)
			}
		}
		if value.Status != "COVERED" && strings.TrimSpace(value.Message) == "" {
			result.add(file, "deferred or not-applicable dimension requires a message: "+value.ID)
		}
	}
	if len(seen) != len(want) {
		result.add(file, "dimensionCoverage must contain all 13 dimensions exactly once")
	}
}
func validateCoveredTargets(targets []string, result *Result, file string) {
	if len(targets) == 0 || hasDuplicate(targets) {
		result.add(file, "coveredTargets must be non-empty and unique")
	}
	for _, target := range targets {
		invalidSegment := false
		parts := strings.Split(target, "/")
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				invalidSegment = true
			}
		}
		topLevelDocument := len(parts) == 1 && path.Ext(target) != ""
		invalidPrefix := strings.HasPrefix(target, "/") || regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`).MatchString(target)
		if target == "" || invalidPrefix || strings.ContainsAny(target, "*?\\") || invalidSegment || (len(parts) == 1 && !topLevelDocument) {
			result.add(file, "coveredTargets contains an imprecise target: "+target)
		}
	}
}

var qaCaseIDPattern = regexp.MustCompile(`(?m)^[ \t]*Case ID:[ \t]*([A-Za-z0-9][A-Za-z0-9._-]*)[ \t]*$`)

func validateQAExecution(options ArtifactOptions, artifact *decodedArtifact, result *Result) {
	p := artifact.QAExecution
	e := artifact.Envelope
	approvedData, approvedOK := readEvidenceRef(options, artifact.RunDir, p.ApprovedCaseSet, result)
	resultsData, resultsOK := readEvidenceRef(options, artifact.RunDir, p.QAOwnedResults, result)
	bindingData, bindingOK := readEvidenceRef(options, artifact.RunDir, p.CaseResultBinding, result)
	readEvidenceRef(options, artifact.RunDir, p.ChangedFiles, result)
	readEvidenceRef(options, artifact.RunDir, p.Verification, result)
	artifact.References[options.File] = append(artifact.References[options.File], p.ApprovedCaseSet, p.QAOwnedResults, p.CaseResultBinding, p.ChangedFiles, p.Verification)
	if !approvedOK || !resultsOK || !bindingOK {
		return
	}
	if !strings.Contains(string(approvedData), "Status: APPROVED_FOR_EXECUTION") {
		result.add(p.ApprovedCaseSet.Path, "approved case set is not approved for execution")
	}
	approvedIDs := []string{}
	for _, match := range qaCaseIDPattern.FindAllStringSubmatch(string(approvedData), -1) {
		approvedIDs = append(approvedIDs, match[1])
	}
	if len(approvedIDs) == 0 || hasDuplicate(approvedIDs) {
		result.add(p.ApprovedCaseSet.Path, "approved case set must contain unique case IDs")
	}
	approved := stringSet(approvedIDs)
	var results qaResultsArtifact
	if err := strictContractJSON(resultsData, &results); err != nil {
		result.add(p.QAOwnedResults.Path, "QA-owned results are invalid: "+err.Error())
		return
	}
	if results.Owner != "QA" || results.WorkflowID != e.WorkflowID || results.ChangeSnapshot != e.ChangeSnapshot || results.Stage != "Execution" || results.Status != "COMPLETE" || results.OverallOutcome != "PASS" || len(results.CaseResults) == 0 || len(results.Executions) == 0 {
		result.add(p.QAOwnedResults.Path, "QA-owned results are incomplete or do not match workflow and snapshot")
	}
	executions := map[string]bool{}
	for _, execution := range results.Executions {
		if execution.ID == "" || execution.Outcome != "PASS" || strings.TrimSpace(execution.Procedure) == "" || strings.TrimSpace(execution.Result) == "" || executions[execution.ID] {
			result.add(p.QAOwnedResults.Path, "QA executions must have unique IDs and PASS outcomes")
		}
		executions[execution.ID] = true
	}
	resultIDs := make([]string, len(results.CaseResults))
	resultPointers := map[string]int{}
	for i, caseResult := range results.CaseResults {
		resultIDs[i] = caseResult.CaseID
		resultPointers[fmt.Sprintf("/caseResults/%d", i)] = i
		if caseResult.CaseID == "" || caseResult.Status != "PASS" || len(caseResult.Procedures) == 0 || hasDuplicate(caseResult.Procedures) || strings.TrimSpace(caseResult.Oracle) == "" {
			result.add(p.QAOwnedResults.Path, "every QA case result must be a unique PASS with bound procedures")
		}
		for _, procedure := range caseResult.Procedures {
			if !executions[procedure] {
				result.add(p.QAOwnedResults.Path, "QA case result references a missing or failed execution: "+procedure)
			}
		}
	}
	if !sameIDSet(resultIDs, approved) {
		result.add(p.QAOwnedResults.Path, "QA case results must exactly cover the approved case set")
	}
	var binding qaCaseBindingArtifact
	if err := strictContractJSON(bindingData, &binding); err != nil {
		result.add(p.CaseResultBinding.Path, "case-result binding is invalid: "+err.Error())
		return
	}
	if binding.WorkflowID != e.WorkflowID || binding.ChangeSnapshot != e.ChangeSnapshot || !binding.Complete || binding.ApprovedCaseSet != p.ApprovedCaseSet || binding.QAOwnedResults != p.QAOwnedResults {
		result.add(p.CaseResultBinding.Path, "case-result binding does not match the approved cases, QA results, workflow, or snapshot")
	}
	bindingIDs := make([]string, len(binding.Bindings))
	for i, item := range binding.Bindings {
		bindingIDs[i] = item.CaseID
		index, ok := resultPointers[item.ResultPointer]
		if !ok {
			result.add(p.CaseResultBinding.Path, "case-result binding points to a missing result: "+item.CaseID)
			continue
		}
		caseResult := results.CaseResults[index]
		if item.CaseID != caseResult.CaseID || item.Status != caseResult.Status || item.Oracle != caseResult.Oracle || !reflect.DeepEqual(item.Procedures, caseResult.Procedures) || !reflect.DeepEqual(item.ExecutionRefs, caseResult.Procedures) {
			result.add(p.CaseResultBinding.Path, "case-result binding is incomplete or points to the wrong result: "+item.CaseID)
		}
	}
	if !sameIDSet(bindingIDs, approved) {
		result.add(p.CaseResultBinding.Path, "case-result bindings must exactly cover the approved case set")
	}
}

func validateReviewer(options ArtifactOptions, artifact *decodedArtifact, result *Result) {
	p := artifact.Reviewer
	e := artifact.Envelope
	policy, ok := policyByID(p.ReviewPolicyID)
	if !ok || policy.ArtifactRole != e.ArtifactRole || policy.Gate != e.Gate || policy.Stage != e.Stage {
		result.add(options.File, "reviewPolicyId is unknown or mismatched")
		return
	}
	artifact.Policy = policy
	if options.Flow != "" && policy.Flow != options.Flow {
		result.add(options.File, "reviewPolicyId flow does not match recording flow")
	}
	if policy.ChangedFilesRequired != (p.ChangedFiles != nil) || policy.VerificationRequired != (p.Verification != nil) {
		result.add(options.File, "changedFiles and verification presence does not match policy")
	}
	if p.Checks == nil {
		result.add(options.File, "checks array must be present")
	}
	rootRefs := []EvidenceRef{p.Dispatch, p.ContextBundle}
	if p.ChangedFiles != nil {
		rootRefs = append(rootRefs, *p.ChangedFiles)
	}
	if p.Verification != nil {
		rootRefs = append(rootRefs, *p.Verification)
	}
	for _, ref := range rootRefs {
		readEvidenceRef(options, artifact.RunDir, ref, result)
	}
	artifact.References[options.File] = append(artifact.References[options.File], rootRefs...)
	contextData, ok := readEvidenceRef(options, artifact.RunDir, p.ContextBundle, result)
	if ok {
		artifact.References[p.ContextBundle.Path] = validateContextBundle(options, artifact.RunDir, p.ContextBundle.Path, contextData, e.WorkflowID, e.ChangeSnapshot, result)
	}
	want, allowedNA, seen := stringSet(policy.RequiredCheckIDs), stringSet(policy.AllowedNotApplicableCheckIDs), map[string]bool{}
	aggregate := "PASS"
	priority := map[string]int{"PASS": 0, "NOT_APPLICABLE": 0, "REVIEW": 1, "FAIL": 2, "BLOCKED": 3}
	for i, check := range p.Checks {
		where := fmt.Sprintf("%s checks[%d]", options.File, i)
		if !want[check.ID] || seen[check.ID] {
			result.add(where, "check id is unknown or duplicate: "+check.ID)
		}
		seen[check.ID] = true
		if strings.TrimSpace(check.Message) == "" || !map[string]bool{"PASS": true, "REVIEW": true, "FAIL": true, "BLOCKED": true, "NOT_APPLICABLE": true}[check.Status] {
			result.add(where, "check status or message is invalid")
		}
		if check.Status == "NOT_APPLICABLE" && (!allowedNA[check.ID] || strings.TrimSpace(check.Message) == "") {
			result.add(where, "NOT_APPLICABLE is not allowed for this check")
		}
		if check.EvidenceRefs == nil || check.Findings == nil {
			result.add(where, "evidenceRefs and findings arrays must be present")
		}
		if priority[check.Status] > priority[aggregate] {
			aggregate = check.Status
		}
		for _, ref := range check.EvidenceRefs {
			readEvidenceRef(options, artifact.RunDir, ref, result)
		}
		artifact.References[options.File] = append(artifact.References[options.File], check.EvidenceRefs...)
		validateFindings(check.Findings, result, where)
		if policy.ID == "complexity.post-development.v2" && check.ID == "complexity.statistics" {
			validateStatisticsOnly(options, artifact.RunDir, check.EvidenceRefs, result)
		}
	}
	if len(seen) != len(want) {
		result.add(options.File, "checks must contain every policy check exactly once")
	}
	if e.Verdict != aggregate {
		result.add(options.File, "top-level verdict contradicts check aggregation")
	}
}
func validateContextBundle(options ArtifactOptions, runDir, file string, data []byte, workflowID, changeSnapshot string, result *Result) []EvidenceRef {
	var bundle ContextBundle
	if err := strictContractJSON(data, &bundle); err != nil {
		result.add(file, "context bundle JSON is invalid: "+err.Error())
		return nil
	}
	if bundle.BundleVersion != 1 || bundle.WorkflowID != workflowID || bundle.ChangeSnapshot != changeSnapshot || len(bundle.Inputs) == 0 {
		result.add(file, "context bundle binding or inputs are invalid")
	}
	paths, resolvedPaths := make([]string, 0, len(bundle.Inputs)), map[string]string{}
	for _, ref := range bundle.Inputs {
		paths = append(paths, ref.Path)
		if resolved, err := safeEvidencePath(runDir, ref.Path); err == nil {
			if previous, exists := resolvedPaths[resolved]; exists && previous != ref.Path {
				result.add(file, "context bundle input paths must not resolve to the same file")
			} else {
				resolvedPaths[resolved] = ref.Path
			}
		}
		readEvidenceRef(options, runDir, ref, result)
	}
	if hasDuplicate(paths) {
		result.add(file, "context bundle input paths must be unique")
	}
	return append([]EvidenceRef{}, bundle.Inputs...)
}
func validateFindings(findings []Finding, result *Result, file string) {
	for _, finding := range findings {
		if strings.TrimSpace(finding.Message) == "" {
			result.add(file, "finding message must be non-empty")
		}
		for _, location := range finding.Locations {
			invalidPath := strings.TrimSpace(location.Path) == "" ||
				strings.Contains(location.Path, "\\") ||
				strings.HasPrefix(location.Path, "/") ||
				filepath.IsAbs(location.Path) ||
				regexp.MustCompile(`^[A-Za-z]:|^[A-Za-z][A-Za-z0-9+.-]*:`).MatchString(location.Path)
			for _, part := range strings.Split(location.Path, "/") {
				if part == "" || part == "." || part == ".." {
					invalidPath = true
				}
			}
			if invalidPath || location.StartLine <= 0 || location.EndLine < location.StartLine {
				result.add(file, "finding location is invalid")
			}
		}
	}
}
func validateStatisticsOnly(options ArtifactOptions, runDir string, refs []EvidenceRef, result *Result) {
	if len(refs) == 0 {
		result.add(options.File, "complexity.statistics requires statistics evidence")
		return
	}
	found := false
	for _, ref := range refs {
		data, ok := readEvidenceRef(options, runDir, ref, result)
		if !ok {
			continue
		}
		var report ComplexityReport
		if err := strictJSON(data, &report); err != nil {
			continue
		}
		found = true
		if report.Budget != nil || report.BudgetSource != "none" || report.BudgetOverrides.MaxNet || report.BudgetOverrides.MaxNewProdFiles || report.BudgetOverrides.MaxProdInsertions {
			result.add(ref.Path, "post-development complexity evidence must be statistics-only")
		}
	}
	if !found {
		result.add(options.File, "complexity.statistics does not reference a valid statistics report")
	}
}
func validateFinalExecution(options ArtifactOptions, artifact *decodedArtifact, result *Result) {
	p, e := artifact.Final, artifact.Envelope
	if p.Mode != "MECHANICAL_CLOSEOUT" || p.ReleaseJudgment != "SEAL" {
		result.add(options.File, "FinalExecution mode and releaseJudgment are invalid")
	}
	want, seen := map[string]PolicyPrereq{}, map[string]bool{}
	for _, prerequisite := range artifact.Policy.Prerequisites {
		want[prerequisite.Gate] = prerequisite
	}
	for _, row := range p.GateMatrix {
		prerequisite, known := want[row.Gate]
		if !known || seen[row.Gate] {
			result.add(options.File, "gateMatrix contains an unknown or duplicate gate: "+row.Gate)
		}
		seen[row.Gate] = true
		data, ok := readEvidenceRef(options, artifact.RunDir, row.GateEvidence, result)
		if ok {
			var closure EvidenceClosure
			if err := strictJSON(data, &closure); err != nil || closure.Gate != row.Gate || normalizeStage(closure.Stage) != normalizeStage(prerequisite.Stage) || closure.WorkflowID != e.WorkflowID || closure.ChangeSnapshot != e.ChangeSnapshot || closure.Verdict != "PASS" {
				result.add(row.GateEvidence.Path, "gateEvidence is not a current-snapshot PASS closure")
			} else if err := verifyClosure(options, artifact.RunDir, closure); err != nil {
				result.add(row.GateEvidence.Path, err.Error())
			}
		}
	}
	if len(seen) != len(artifact.Policy.Prerequisites) {
		result.add(options.File, "gateMatrix must contain exactly the policy prerequisites")
	}
	data, ok := readEvidenceRef(options, artifact.RunDir, p.FinalVerification, result)
	if ok {
		var final WorkflowFinalVerificationArtifact
		if err := strictJSON(data, &final); err != nil || final.WorkflowID != e.WorkflowID || final.ChangeSnapshot != e.ChangeSnapshot || final.Status != "PASS" || len(final.AcceptedAttempts) == 0 {
			result.add(p.FinalVerification.Path, "finalVerification is not a current-snapshot PASS")
		}
	}
}
func strictJSON(data []byte, target any) error {
	if len(data) == 0 || !utf8.Valid(data) {
		return errors.New("input must be non-empty valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSON(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}
func strictContractJSON(data []byte, target any) error {
	if err := strictJSON(data, target); err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return requireContractFields(value, reflect.TypeOf(target), "")
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func requireContractFields(value any, target reflect.Type, path string) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == rawMessageType || target.Kind() == reflect.Interface {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for i := 0; i < target.NumField(); i++ {
			field := target.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			if name == "-" {
				continue
			}
			optional := false
			for _, option := range tag[1:] {
				optional = optional || option == "omitempty"
			}
			child, present := object[name]
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}
			if !present {
				if !optional {
					return fmt.Errorf("missing required field %s", childPath)
				}
				continue
			}
			if err := requireContractFields(child, field.Type, childPath); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		values, ok := value.([]any)
		if !ok {
			return nil
		}
		for i, child := range values {
			if err := requireContractFields(child, target.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
func scanJSON(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return errors.New("null is not allowed")
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = true
			if err := scanJSON(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for dec.More() {
			if err := scanJSON(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}
func readEvidenceRef(options ArtifactOptions, runDir string, ref EvidenceRef, result *Result) ([]byte, bool) {
	path, err := safeEvidencePath(runDir, ref.Path)
	if err != nil {
		result.add(options.File, err.Error())
		return nil, false
	}
	if !isSHA256(ref.SHA256) || ref.SHA256 != strings.ToLower(ref.SHA256) {
		result.add(options.File, "EvidenceRef sha256 must be lowercase 64-hex: "+ref.Path)
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.add(ref.Path, "cannot read evidence: "+err.Error())
		return nil, false
	}
	if sha256File(path) != ref.SHA256 {
		result.add(ref.Path, "evidence sha256 mismatch")
		return nil, false
	}
	return data, true
}
func safeEvidencePath(runDir, logical string) (string, error) {
	if logical == "" || logical == "." || strings.Contains(logical, "\\") || filepath.IsAbs(logical) || regexp.MustCompile(`^[A-Za-z]:|^[A-Za-z][A-Za-z0-9+.-]*:`).MatchString(logical) {
		return "", fmt.Errorf("unsafe run-local evidence path: %s", logical)
	}
	parts := strings.Split(logical, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe run-local evidence path: %s", logical)
		}
	}
	base, err := filepath.Abs(runDir)
	if err != nil {
		return "", err
	}
	if resolvedBase, err := filepath.EvalSymlinks(base); err == nil {
		base = resolvedBase
	}
	path := filepath.Join(base, filepath.FromSlash(logical))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence path escapes active run: %s", logical)
	}
	return resolved, nil
}
func artifactRunDir(options ArtifactOptions, _ string) string {
	if options.RunDir != "" {
		return options.RunDir
	}
	root := absPath(cleanRoot(options.Root))
	path := absPath(resolvePath(root, options.File))
	runsRoot := filepath.Join(root, ".claude", "gates", "runs")
	if relative, err := filepath.Rel(runsRoot, path); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) > 1 {
			return filepath.Join(runsRoot, parts[0])
		}
	}
	return root
}
func alignmentIDSet(items []AlignmentItem) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[item.ID] = true
	}
	return out
}
func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}
func sameIDSet(values []string, want map[string]bool) bool {
	if len(values) != len(want) || hasDuplicate(values) {
		return false
	}
	for _, value := range values {
		if !want[value] {
			return false
		}
	}
	return true
}
func hasDuplicate(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
