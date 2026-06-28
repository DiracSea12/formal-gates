package validate

import (
	"fmt"
	"path/filepath"
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
		for _, flag := range []string{"--max-net", "--max-new-prod-files", "--max-prod-insertions"} {
			if !strings.Contains(command, flag) {
				result.add(options.File, "Complexity check command missing "+flag)
			}
		}
	}
	return result
}
