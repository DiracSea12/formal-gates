package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type HandoffOptions struct {
	Root           string
	RunDir         string
	File           string
	WorkflowID     string
	ChangeSnapshot string
}

type HandoffComposeOptions struct {
	Root, RunDir, WorkflowID, ChangeSnapshot, Output string
	RequirementTarget, VerificationRequirements      string
	BudgetStopTriggers, BudgetExpansionApprovalPath  string
	ForbiddenContext, FormalFlowMode, TriggerSource  string
	TaskType                                         string
	QACaseSet, DesignReview                          string
	MaxNet, MaxNewProdFiles, MaxProdInsertions       int
}

type handoffLineScalar struct {
	name  string
	value string
}

func rejectHandoffLineBreaks(result *Result, values ...handoffLineScalar) {
	for _, value := range values {
		if strings.ContainsAny(value.value, "\r\n") {
			result.add("handoff", "--"+value.name+" must not contain CR/LF")
		}
	}
}

func ComposeHandoff(options HandoffComposeOptions) (EvidenceRef, Result) {
	var result Result
	root := cleanRoot(options.Root)
	worktree := slash(absPath(root))
	rejectHandoffLineBreaks(&result,
		handoffLineScalar{"workflow-id", options.WorkflowID},
		handoffLineScalar{"change-snapshot", options.ChangeSnapshot},
		handoffLineScalar{"requirement-target", options.RequirementTarget},
		handoffLineScalar{"verification-requirements", options.VerificationRequirements},
		handoffLineScalar{"budget-stop-triggers", options.BudgetStopTriggers},
		handoffLineScalar{"budget-expansion-approval-path", options.BudgetExpansionApprovalPath},
		handoffLineScalar{"forbidden-context", options.ForbiddenContext},
		handoffLineScalar{"formal-flow-mode", options.FormalFlowMode},
		handoffLineScalar{"trigger-source", options.TriggerSource},
		handoffLineScalar{"task-type", options.TaskType},
		handoffLineScalar{"worktree", worktree},
	)
	for _, value := range []handoffLineScalar{
		{"workflow-id", options.WorkflowID}, {"change-snapshot", options.ChangeSnapshot},
		{"requirement-target", options.RequirementTarget}, {"verification-requirements", options.VerificationRequirements},
		{"budget-stop-triggers", options.BudgetStopTriggers}, {"budget-expansion-approval-path", options.BudgetExpansionApprovalPath},
		{"forbidden-context", options.ForbiddenContext}, {"trigger-source", options.TriggerSource},
	} {
		if !meaningful(value.value) {
			result.add("handoff", "--"+value.name+" is required")
		}
	}
	if strings.TrimSpace(options.FormalFlowMode) == "" {
		result.add("handoff", "--formal-flow-mode is required")
	}
	if options.MaxNet < 0 || options.MaxNewProdFiles < 0 || options.MaxProdInsertions < 0 {
		result.add("handoff", "complexity budgets must be non-negative")
	}
	if !complexityTaskTypes[options.TaskType] {
		result.add("handoff", "--task-type must be one of: "+strings.Join(sortedComplexityTaskTypes(), ", "))
	}
	if !result.OK() {
		return EvidenceRef{}, result
	}
	runDir, err := resolveWorkflowRunDir(root, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return EvidenceRef{}, result
	}
	formal := options.FormalFlowMode == "four-gate" || options.FormalFlowMode == "release" || options.FormalFlowMode == "seal"
	caseText, reviewText := "NOT_APPLICABLE", "NOT_APPLICABLE"
	if formal {
		caseRef, refErr := registeredEvidenceRef(root, runDir, options.QACaseSet)
		if refErr != nil {
			result.add(options.QACaseSet, refErr.Error())
		} else {
			caseText = "path=" + caseRef.Path + " sha256=" + caseRef.SHA256
		}
		reviewRef, refErr := registeredEvidenceRef(root, runDir, options.DesignReview)
		if refErr != nil {
			result.add(options.DesignReview, refErr.Error())
		} else {
			reviewText = "path=" + reviewRef.Path + " sha256=" + reviewRef.SHA256
		}
	}
	rejectHandoffLineBreaks(&result,
		handoffLineScalar{"qa-case-set", caseText},
		handoffLineScalar{"design-review", reviewText},
	)
	if !result.OK() {
		return EvidenceRef{}, result
	}
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		result.add("handoff", err.Error())
		return EvidenceRef{}, result
	}
	outputPath, err := prospectiveRestrictedPath(runDir, options.Output)
	if err != nil {
		result.add(options.Output, "handoff output must be under the active run restricted directory")
		return EvidenceRef{}, result
	}
	if _, err := os.Lstat(outputPath); err == nil || !os.IsNotExist(err) {
		result.add(options.Output, "generated handoff already exists")
		return EvidenceRef{}, result
	}
	budget := fmt.Sprintf("max-net=%d max-new-prod-files=%d max-prod-insertions=%d", options.MaxNet, options.MaxNewProdFiles, options.MaxProdInsertions)
	command := fmt.Sprintf("formal-gates complexity check --task-type %s --worktree %s --max-net %d --max-new-prod-files %d --max-prod-insertions %d", options.TaskType, quoteCommandArg(worktree), options.MaxNet, options.MaxNewProdFiles, options.MaxProdInsertions)
	text := "Gate Handoff Request\n" +
		"WorkflowId: " + options.WorkflowID + "\n" +
		"Change snapshot: " + options.ChangeSnapshot + "\n" +
		"Worktree: " + worktree + "\n" +
		"Requirement document target or OpenSpec change: " + options.RequirementTarget + "\n" +
		"Verification requirements: " + options.VerificationRequirements + "\n" +
		"Development-time complexity budget: " + budget + "\n" +
		"Complexity check command: " + command + "\n" +
		"Budget stop triggers: " + options.BudgetStopTriggers + "\n" +
		"Budget expansion approval path: " + options.BudgetExpansionApprovalPath + "\n" +
		"Forbidden context: " + options.ForbiddenContext + "\n" +
		"Formal flow mode: " + options.FormalFlowMode + "\n" +
		"Trigger source: " + options.TriggerSource + "\n" +
		"QA case design artifact: " + caseText + "\n" +
		"Approved QA case set: " + caseText + "\n" +
		"Accepted Design Review closure: " + reviewText + "\n"
	if err := writeBytesExclusive(outputPath, []byte(text)); err != nil {
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	logical, _ := logicalPathInRun(runDir, outputPath)
	artifactRef := EvidenceRef{Path: logical, SHA256: sha256File(outputPath)}
	proofRef, err := writeCompositionProof(root, runDir, "handoff.v1", options.WorkflowID, options.ChangeSnapshot, outputPath, []EvidenceRef{artifactRef})
	if err != nil {
		_ = os.Remove(outputPath)
		result.add(options.Output, err.Error())
		return EvidenceRef{}, result
	}
	validation := Handoff(HandoffOptions{Root: root, RunDir: runDir, File: relativePath(root, outputPath), WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot})
	if !validation.OK() {
		_ = os.Remove(outputPath)
		_ = os.Remove(filepath.Join(runDir, filepath.FromSlash(proofRef.Path)))
		return EvidenceRef{}, validation
	}
	return artifactRef, result
}

func Handoff(options HandoffOptions) Result {
	root := cleanRoot(options.Root)
	var result Result
	if strings.TrimSpace(options.File) == "" {
		result.add("handoff", "--file is required")
		return result
	}
	path := options.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	text, err := readText(path)
	if err != nil {
		result.add(options.File, fmt.Sprintf("cannot read handoff artifact: %v", err))
		return result
	}
	workflowID := firstNonEmpty(options.WorkflowID, fieldValue(text, "WorkflowId"))
	snapshot := firstNonEmpty(options.ChangeSnapshot, fieldValue(text, "Change snapshot"))
	runDirValue := options.RunDir
	if strings.TrimSpace(runDirValue) == "" {
		inferred := artifactRunDir(ArtifactOptions{Root: root, File: options.File}, workflowID)
		if activeWorkflowRun(root, inferred) {
			runDirValue = inferred
		}
	}
	runDir, runErr := resolveWorkflowRunDir(root, workflowID, runDirValue)
	if runErr != nil {
		result.add(options.File, runErr.Error())
	} else {
		logical, logicalErr := logicalPathInRun(runDir, path)
		proofPath, proofErr := compositionProofPath(root, runDir, "handoff.v1", path)
		data, readErr := os.ReadFile(proofPath)
		var proof CompositionProof
		if logicalErr != nil || proofErr != nil || readErr != nil || strictContractJSON(data, &proof) != nil || proof.ProofVersion != 1 || proof.Composer != "handoff.v1" || proof.WorkflowID != workflowID || proof.ChangeSnapshot != snapshot || len(proof.Outputs) != 1 || proof.Outputs[0] != (EvidenceRef{Path: logical, SHA256: sha256File(path)}) {
			result.add(options.File, "CLI handoff composition proof is missing or invalid")
		}
	}
	for _, field := range []string{
		"Gate Handoff Request",
		"WorkflowId:",
		"Change snapshot:",
		"Worktree:",
		"Requirement document target or OpenSpec change:",
		"Verification requirements:",
		"Development-time complexity budget:",
		"Complexity check command:",
		"Budget stop triggers:",
		"Budget expansion approval path:",
		"Forbidden context:",
	} {
		if !strings.Contains(text, field) {
			result.add(options.File, "missing required handoff field text: "+field)
		}
	}
	for _, field := range []string{
		"WorkflowId",
		"Change snapshot",
		"Worktree",
		"Requirement document target or OpenSpec change",
		"Verification requirements",
		"Development-time complexity budget",
		"Complexity check command",
		"Budget stop triggers",
		"Budget expansion approval path",
		"Forbidden context",
	} {
		if !meaningful(fieldValue(text, field)) {
			result.add(options.File, "field has no meaningful value: "+field)
		}
	}
	if options.WorkflowID != "" && fieldValue(text, "WorkflowId") != options.WorkflowID {
		result.add(options.File, "WorkflowId does not match --workflow-id")
	}
	if options.ChangeSnapshot != "" && fieldValue(text, "Change snapshot") != options.ChangeSnapshot {
		result.add(options.File, "Change snapshot does not match --change-snapshot")
	}
	command := fieldValue(text, "Complexity check command")
	if meaningful(command) {
		if !strings.Contains(command, "complexity check") {
			result.add(options.File, "Complexity check command must run formal-gates complexity check")
		}
		taskType, ok := handoffCommandTaskType(command)
		if !ok {
			result.add(options.File, "Complexity check command must contain exactly one --task-type")
		} else if !complexityTaskTypes[taskType] {
			result.add(options.File, "Complexity check command has unsupported --task-type: "+taskType)
		}
	}
	budget := fieldValue(text, "Development-time complexity budget")
	for _, name := range []string{"max-net", "max-new-prod-files", "max-prod-insertions"} {
		budgetValue, budgetOK := handoffBudgetValue(budget, name)
		commandValue, commandOK := handoffCommandBudgetValue(command, name)
		if !budgetOK {
			result.add(options.File, "Development-time complexity budget missing numeric "+name)
		}
		if meaningful(command) && !commandOK {
			result.add(options.File, "Complexity check command missing numeric --"+name)
		}
		if budgetOK && commandOK && budgetValue != commandValue {
			result.add(options.File, fmt.Sprintf("Development-time complexity budget %s=%d does not match Complexity check command --%s=%d", name, budgetValue, name, commandValue))
		}
	}
	formalFlowMode := strings.ToLower(strings.TrimSpace(fieldValue(text, "Formal flow mode")))
	switch formalFlowMode {
	case "none", "four-gate", "release", "seal":
	default:
		result.add(options.File, "Formal flow mode must be one of: none, four-gate, release, seal")
	}
	if formalFlowMode == "four-gate" || formalFlowMode == "release" || formalFlowMode == "seal" {
		for _, field := range []string{"QA case design artifact", "Approved QA case set", "Accepted Design Review closure"} {
			if !meaningful(fieldValue(text, field)) {
				result.add(options.File, "field has no meaningful value: "+field)
			}
		}
		designCases, designOK := handoffEvidenceRef(fieldValue(text, "QA case design artifact"))
		approvedCases, approvedOK := handoffEvidenceRef(fieldValue(text, "Approved QA case set"))
		designReview, reviewOK := handoffEvidenceRef(fieldValue(text, "Accepted Design Review closure"))
		if !designOK || !approvedOK || !reviewOK {
			result.add(options.File, "formal QA handoff fields must use path=<run-relative-path> sha256=<lowercase-64-hex>")
		} else if designCases != approvedCases {
			result.add(options.File, "QA case design artifact and Approved QA case set must be the same exact EvidenceRef")
		} else {
			workflowID := firstNonEmpty(options.WorkflowID, fieldValue(text, "WorkflowId"))
			snapshot := firstNonEmpty(options.ChangeSnapshot, fieldValue(text, "Change snapshot"))
			if runErr != nil {
				result.add(options.File, runErr.Error())
			} else {
				envelope := FormalGateEvidence{WorkflowID: workflowID, ChangeSnapshot: snapshot}
				artifact := decodedArtifact{Envelope: envelope, References: map[string][]EvidenceRef{}, RunDir: runDir}
				validateAcceptedDesignReview(ArtifactOptions{Root: root, File: options.File, WorkflowID: workflowID, ChangeSnapshot: snapshot}, &artifact, designReview, approvedCases, snapshot, &result)
			}
		}
	}
	return result
}

func handoffEvidenceRef(value string) (EvidenceRef, bool) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndex(value, " sha256=")
	if separator < 0 || !strings.HasPrefix(value, "path=") {
		return EvidenceRef{}, false
	}
	ref := EvidenceRef{
		Path:   strings.Trim(strings.TrimSpace(value[len("path="):separator]), "`'\""),
		SHA256: strings.Trim(strings.TrimSpace(value[separator+len(" sha256="):]), "`'\"(),;"),
	}
	return ref, ref.Path != "" && isSHA256(ref.SHA256)
}

func handoffBudgetValue(text, name string) (int, bool) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `[ \t]*(?:[:=]|[ \t])[ \t]*(-?\d+)(?:[^0-9]|$)`),
		regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_-])--` + regexp.QuoteMeta(name) + `[ \t]*(?:=|[ \t])[ \t]*(-?\d+)(?:[^0-9]|$)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(text); len(match) == 2 {
			value, err := strconv.Atoi(match[1])
			if err == nil {
				return value, true
			}
		}
	}
	return 0, false
}

func handoffCommandBudgetValue(text, name string) (int, bool) {
	pattern := regexp.MustCompile(`(?i)(?:^|[ \t])--` + regexp.QuoteMeta(name) + `[ \t]*(?:=|[ \t])[ \t]*(-?\d+)(?:[ \t]|$)`)
	match := pattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	return value, err == nil
}

func handoffCommandTaskType(text string) (string, bool) {
	pattern := regexp.MustCompile(`(?i)(?:^|[ \t])--task-type[ \t]*(?:=|[ \t])[ \t]*([^ \t]+)(?:[ \t]|$)`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 || len(matches[0]) != 2 {
		return "", false
	}
	return matches[0][1], true
}

func sortedComplexityTaskTypes() []string {
	values := make([]string, 0, len(complexityTaskTypes))
	for value := range complexityTaskTypes {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
