package validate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type ArtifactOptions struct {
	Root           string
	File           string
	Gate           string
	WorkflowID     string
	ChangeSnapshot string
	Stage          string
}

var knownGates = map[string]bool{
	"requirements-clarification-gate": true,
	"qa-test-gate":                    true,
	"complexity-gate":                 true,
	"architecture-health-gate":        true,
	"code-quality-gate":               true,
}

var postDevelopmentCommon = []string{
	"Review mode: ZERO_CONTEXT_FORMAL",
	"Prompt contamination check: PASS",
	"Semantic anti-anchor check: PASS",
	"Zero-context reviewer: YES",
	"Independent agent: YES",
	"Reviewer agent id:",
	"Context bundle:",
	"Dispatch prompt artifact:",
	"No-anchor prompt: YES",
	"gate_route:",
}

func Artifact(options ArtifactOptions) Result {
	root := cleanRoot(options.Root)
	var result Result
	if strings.TrimSpace(options.File) == "" {
		result.add("artifact", "--file is required")
		return result
	}
	if strings.TrimSpace(options.Gate) == "" {
		result.add("artifact", "--gate is required")
		return result
	}
	if !knownGates[options.Gate] {
		result.add("artifact", "unknown built-in gate: "+options.Gate)
		return result
	}

	path := options.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	text, err := readText(path)
	if err != nil {
		result.add(options.File, fmt.Sprintf("cannot read artifact: %v", err))
		return result
	}

	required := requiredArtifactFields(options.Gate)
	for _, field := range required {
		if !strings.Contains(text, field) {
			result.add(options.File, "missing required field text: "+field)
		}
	}
	for _, field := range fieldsThatNeedValues(options.Gate) {
		value := fieldValue(text, field)
		if !meaningful(value) {
			result.add(options.File, "field has no meaningful value: "+field)
		}
	}
	validateRoute(options, text, &result)
	return result
}

func requiredArtifactFields(gate string) []string {
	if gate == "requirements-clarification-gate" {
		return []string{
			"Requirement source:",
			"Alignment table artifact:",
			"Total alignment items:",
			"Open question IDs:",
			"User confirmation:",
			"Dimension coverage:",
			"Decision record:",
			"Covered formal targets:",
			"Downstream permission:",
			"gate_route:",
		}
	}

	fields := append([]string{}, postDevelopmentCommon...)
	fields = append(fields, "Prompt source: "+expectedPromptSource(gate))
	if gate == "complexity-gate" {
		fields = append(fields,
			"Script result",
			"Diff shape judgment",
			"Impact surface health",
			"Public/config surface",
			"New concepts",
			"Shrink opportunities",
			"Decision evidence",
		)
	}
	if gate == "qa-test-gate" {
		fields = append(fields, "Approved case set:", "QA-owned evidence:", "Case-to-artifact binding:")
	}
	return fields
}

func fieldsThatNeedValues(gate string) []string {
	if gate == "requirements-clarification-gate" {
		return []string{"Requirement source", "Alignment table artifact", "Total alignment items", "Decision record", "Covered formal targets"}
	}
	return []string{"Reviewer agent id", "Context bundle", "Dispatch prompt artifact"}
}

func expectedPromptSource(gate string) string {
	switch gate {
	case "qa-test-gate":
		return "agents/qa-test-gate.md"
	case "complexity-gate":
		return "agents/complexity-gate.md"
	case "architecture-health-gate":
		return "agents/architecture-health-gate.md"
	case "code-quality-gate":
		return "agents/code-quality-gate.md"
	default:
		return "agents/" + gate + ".md"
	}
}

func validateRoute(options ArtifactOptions, text string, result *Result) {
	if options.WorkflowID != "" && routeValue(text, "workflow_id") != options.WorkflowID {
		result.add(options.File, "gate_route workflow_id does not match --workflow-id")
	}
	if options.ChangeSnapshot != "" && routeValue(text, "change_snapshot") != options.ChangeSnapshot {
		result.add(options.File, "gate_route change_snapshot does not match --change-snapshot")
	}
	next := routeValue(text, "next_action")
	if next == "" {
		result.add(options.File, "gate_route next_action is missing")
	}
	if options.Gate == "requirements-clarification-gate" {
		if strings.ToLower(strings.TrimSpace(fieldValue(text, "Open question IDs"))) != "none" {
			result.add(options.File, "Open question IDs must be none for PASS")
		}
		if !strings.EqualFold(strings.TrimSpace(fieldValue(text, "User confirmation")), "YES") {
			result.add(options.File, "User confirmation must be YES for PASS")
		}
	}
}

func fieldValue(text, field string) string {
	pattern := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*(.*?)[ \t]*$`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func routeValue(text, field string) string {
	pattern := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*"?([^"\r\n]+)"?[ \t]*$`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func meaningful(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if regexp.MustCompile(`<[^>\r\n]+>`).MatchString(value) {
		return false
	}
	switch strings.ToLower(value) {
	case "unavailable", "unknown", "none", "null", "n/a", "na", "todo", "tbd", "placeholder", "sample", "example":
		return false
	}
	return true
}
