package validate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type HandoffOptions struct {
	Root           string
	File           string
	WorkflowID     string
	ChangeSnapshot string
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
			runDir, err := resolveWorkflowRunDir(root, workflowID, "")
			if err != nil {
				result.add(options.File, err.Error())
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
