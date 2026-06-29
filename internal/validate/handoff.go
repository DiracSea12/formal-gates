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
	return result
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
