package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DispatchPromptOptions struct {
	Root       string
	PromptText string
	ConfigPath string
	FinalSend  bool
}

type PrepareDispatchPromptOptions struct {
	Root, OutputFile, ConfigPath                       string
	Gate, Stage, CurrentRequirement, CurrentDiff       string
	Worktree, ChangeSnapshot, ReviewArtifact, PolicyID string
	ContextBundle                                      string
}

type PreparedDispatchPrompt struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type DispatchPromptViolation struct {
	Type        string `json:"type"`
	Matched     string `json:"matched"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type pollutionConfig struct {
	English struct {
		PatternGroups []pollutionPatternGroup `json:"patternGroups"`
	} `json:"english"`
	Chinese struct {
		TermGroups []pollutionTermGroup `json:"termGroups"`
	} `json:"chinese"`
}

type pollutionPatternGroup struct {
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Patterns    []string `json:"patterns"`
}

type pollutionTermGroup struct {
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Terms       []string `json:"terms"`
}

func DispatchPrompt(options DispatchPromptOptions) Result {
	result, _ := DispatchPromptWithViolations(options)
	return result
}

func DispatchPromptWithViolations(options DispatchPromptOptions) (Result, []DispatchPromptViolation) {
	root := cleanRoot(options.Root)
	configPath := strings.TrimSpace(options.ConfigPath)
	if configPath == "" {
		configPath = filepath.Join(root, "hooks", "pollution-patterns.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if executable, executableErr := os.Executable(); executableErr == nil {
				installedPath := filepath.Join(filepath.Dir(filepath.Dir(executable)), "hooks", "pollution-patterns.json")
				if _, installedErr := os.Stat(installedPath); installedErr == nil {
					configPath = installedPath
				}
			}
		}
	} else if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(root, filepath.FromSlash(configPath))
	}

	var result Result
	configText, err := readText(configPath)
	if err != nil {
		result.add(slash(configPath), fmt.Sprintf("cannot read pollution patterns config: %v", err))
		return result, nil
	}
	var config pollutionConfig
	if err := json.Unmarshal([]byte(configText), &config); err != nil {
		result.add(slash(configPath), fmt.Sprintf("cannot parse pollution patterns config: %v", err))
		return result, nil
	}

	violations := findDispatchPromptViolations(root, options.PromptText, config, options.FinalSend, &result)
	for _, violation := range violations {
		if violation.Type == "missing field" {
			result.add("dispatch-prompt", fmt.Sprintf("prompt is missing required field %q", violation.Matched))
			continue
		}
		if violation.Type == "unknown field" || violation.Type == "duplicate field" || violation.Type == "content" || violation.Type == "placeholder" {
			result.add("dispatch-prompt", violation.Description+": "+violation.Matched)
			continue
		}
		result.add("dispatch-prompt", fmt.Sprintf("prompt contains prohibited anchoring %s %q (%s)", violation.Type, violation.Matched, violation.Label))
	}
	return result, violations
}

func PrepareDispatchPrompt(options PrepareDispatchPromptOptions) (PreparedDispatchPrompt, Result) {
	root := cleanRoot(options.Root)
	var result Result
	for name, value := range map[string]string{
		"output": options.OutputFile, "gate": options.Gate, "current-requirement": options.CurrentRequirement,
		"current-diff": options.CurrentDiff, "worktree": options.Worktree, "change-snapshot": options.ChangeSnapshot,
		"review-artifact": options.ReviewArtifact, "policy-id": options.PolicyID, "context-bundle": options.ContextBundle,
	} {
		if strings.TrimSpace(value) == "" {
			result.add("prompt", "--"+name+" is required")
		}
	}
	if !result.OK() {
		return PreparedDispatchPrompt{}, result
	}
	if !samePath(cleanWorktree(options.Worktree), root) {
		result.add("prompt", "--worktree must match --root")
		return PreparedDispatchPrompt{}, result
	}
	runDir, outputLogical, err := promptRunRelativePath(root, options.OutputFile)
	if err != nil {
		result.add(slash(options.OutputFile), err.Error())
		return PreparedDispatchPrompt{}, result
	}
	reviewRunDir, _, err := promptRunRelativePath(root, options.ReviewArtifact)
	if err != nil || !samePath(runDir, reviewRunDir) {
		result.add(slash(options.ReviewArtifact), "review artifact must belong to the prepared prompt run")
		return PreparedDispatchPrompt{}, result
	}
	bundleRunDir, bundleLogical, err := promptRunRelativePath(root, options.ContextBundle)
	if err != nil || !samePath(runDir, bundleRunDir) {
		result.add(slash(options.ContextBundle), "context bundle must belong to the prepared prompt run")
		return PreparedDispatchPrompt{}, result
	}
	bundlePath := resolvePath(root, options.ContextBundle)
	if !isFile(bundlePath) {
		result.add(slash(options.ContextBundle), "context bundle does not exist")
		return PreparedDispatchPrompt{}, result
	}
	role, policies := dispatchOutputContracts(options.Gate, options.Stage)
	if role == "" || !contains(policies, options.PolicyID) {
		result.add("prompt", "--policy-id does not match --gate/--stage")
		return PreparedDispatchPrompt{}, result
	}
	fields := map[string]string{
		"formal_gate_dispatch":            expectedDispatchRole(options.Gate, options.Stage),
		"current requirement":             options.CurrentRequirement,
		"current diff or proposed change": options.CurrentDiff,
		"worktree":                        slash(absPath(options.Worktree)),
		"base commit or snapshot":         options.ChangeSnapshot,
		"output path":                     slash(options.ReviewArtifact),
		"output format":                   "CLI-owned schema-version-2 " + role + " JSON for " + options.PolicyID + "; submit semantic values with formal-gates receipt submit; routing-only bindings contextBundle=" + bundleLogical + " sha256=" + sha256File(bundlePath) + "; do not read these files",
	}
	var builder strings.Builder
	for _, field := range strictDispatchPromptFieldOrder {
		builder.WriteString(field)
		builder.WriteString(": ")
		builder.WriteString(fields[strings.ToLower(field)])
		builder.WriteByte('\n')
	}
	preparedText := builder.String()
	preparedResult, _ := DispatchPromptWithViolations(DispatchPromptOptions{
		Root:       root,
		PromptText: preparedText,
		ConfigPath: options.ConfigPath,
		FinalSend:  true,
	})
	if !preparedResult.OK() {
		return PreparedDispatchPrompt{}, preparedResult
	}
	outputPath := resolvePath(root, options.OutputFile)
	if err := writeFileAtomic(outputPath, []byte(preparedText), 0o600); err != nil {
		var result Result
		result.add(slash(outputLogical), fmt.Sprintf("cannot write prepared prompt: %v", err))
		return PreparedDispatchPrompt{}, result
	}
	return PreparedDispatchPrompt{File: slash(options.OutputFile), SHA256: sha256Bytes([]byte(preparedText))}, Result{}
}

func findDispatchPromptViolations(root, prompt string, config pollutionConfig, finalSend bool, result *Result) []DispatchPromptViolation {
	violations := findDispatchPromptFieldViolations(prompt)
	if finalSend {
		violations = findFinalDispatchPromptFieldViolations(root, prompt)
	}
	for _, group := range config.English.PatternGroups {
		for _, pattern := range group.Patterns {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				result.add("hooks/pollution-patterns.json", fmt.Sprintf("invalid regex for %s: %v", group.Label, err))
				continue
			}
			match := compiled.FindString(prompt)
			if match == "" {
				continue
			}
			violations = append(violations, DispatchPromptViolation{
				Type:        "pattern",
				Matched:     match,
				Label:       group.Label,
				Description: group.Description,
			})
		}
	}
	for _, group := range config.Chinese.TermGroups {
		for _, term := range group.Terms {
			if !strings.Contains(prompt, term) {
				continue
			}
			violations = append(violations, DispatchPromptViolation{
				Type:        "term",
				Matched:     term,
				Label:       group.Label,
				Description: group.Description,
			})
		}
	}
	return violations
}

var strictDispatchPromptFieldOrder = []string{
	"formal_gate_dispatch",
	"Current requirement",
	"Current diff or proposed change",
	"Worktree",
	"Base commit or snapshot",
	"Output path",
	"Output format",
}

func findFinalDispatchPromptFieldViolations(root, prompt string) []DispatchPromptViolation {
	allowed := map[string]string{}
	for _, field := range strictDispatchPromptFieldOrder {
		allowed[field] = strings.ToLower(field)
	}
	counts := map[string]int{}
	values := map[string]string{}
	violations := []DispatchPromptViolation{}
	for _, rawLine := range strings.Split(prompt, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		label, value, ok := strings.Cut(line, ":")
		label = strings.TrimSpace(label)
		if !ok {
			violations = append(violations, DispatchPromptViolation{
				Type: "content", Matched: line, Label: "final send format",
				Description: "final reviewer prompt may contain only the seven named fields",
			})
			continue
		}
		key, ok := allowed[label]
		if !ok {
			violations = append(violations, DispatchPromptViolation{
				Type: "unknown field", Matched: label + ":", Label: "final send format",
				Description: "final reviewer prompt contains an unknown field",
			})
			continue
		}
		counts[key]++
		if counts[key] > 1 {
			violations = append(violations, DispatchPromptViolation{
				Type: "duplicate field", Matched: label + ":", Label: "final send format",
				Description: "final reviewer prompt contains a duplicate field",
			})
		}
		if counts[key] == 1 {
			values[key] = strings.TrimSpace(value)
		}
	}
	for _, field := range strictDispatchPromptFieldOrder {
		key := strings.ToLower(field)
		if counts[key] == 1 && values[key] != "" {
			continue
		}
		violations = append(violations, DispatchPromptViolation{
			Type: "missing field", Matched: field + ":", Label: "final send format",
			Description: "final reviewer prompt requires every allowed field exactly once with a non-empty value",
		})
	}
	if regexp.MustCompile(`\bPENDING\b`).FindString(prompt) != "" {
		violations = append(violations, DispatchPromptViolation{
			Type: "placeholder", Matched: "PENDING", Label: "final send format",
			Description: "final reviewer prompt contains an unresolved placeholder",
		})
	}
	for _, field := range []string{"current requirement", "current diff or proposed change"} {
		normalizedValue := strings.ToLower(strings.ReplaceAll(values[field], "\\", "/"))
		if !strings.Contains(normalizedValue, ".claude/gates/runs/") {
			continue
		}
		if field == "current diff or proposed change" && allowedQADesignReviewCaseSetPrompt(root, values) {
			continue
		}
		violations = append(violations, DispatchPromptViolation{
			Type: "path", Matched: field + ": .claude/gates/runs/", Label: "workflow-run artifact",
			Description: "formal reviewer input must not expose a workflow-run artifact",
		})
	}
	return violations
}

func allowedQADesignReviewCaseSetPrompt(root string, fields map[string]string) bool {
	if fields["formal_gate_dispatch"] != "qa-test-gate" || !strings.Contains(fields["output format"], "qa.design-review.v2") {
		return false
	}
	runDir, _, err := promptRunRelativePath(root, fields["output path"])
	if err != nil {
		return false
	}
	casePath := resolvePath(root, fields["current diff or proposed change"])
	if requireAbsPathUnderRunDir(runDir, "QA Design Review case set", casePath) != nil || !isFile(casePath) {
		return false
	}
	data, err := os.ReadFile(casePath)
	return err == nil && len(qaCaseIDPattern.FindAllStringSubmatch(string(data), -1)) > 0
}

func strictDispatchPromptFields(prompt string) map[string]string {
	fields := map[string]string{}
	for _, rawLine := range strings.Split(prompt, "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(rawLine), ":")
		if ok {
			fields[strings.ToLower(strings.TrimSpace(label))] = strings.TrimSpace(value)
		}
	}
	return fields
}

func promptRunRelativePath(root, value string) (string, string, error) {
	rootAbs, err := filepath.Abs(cleanRoot(root))
	if err != nil {
		return "", "", err
	}
	path := resolvePath(rootAbs, value)
	runsRoot := filepath.Join(rootAbs, ".claude", "gates", "runs")
	rel, err := filepath.Rel(runsRoot, path)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 || parts[0] == "" || parts[0] == "." || parts[0] == ".." || parts[1] != "restricted" {
		return "", "", fmt.Errorf("prompt files and bindings must be under one active run's restricted directory")
	}
	runDir := filepath.Join(runsRoot, filepath.FromSlash(parts[0]))
	return runDir, strings.Join(parts[1:], "/"), nil
}

func findDispatchPromptFieldViolations(prompt string) []DispatchPromptViolation {
	fields := dispatchPromptFields(prompt)
	violations := []DispatchPromptViolation{}
	for _, field := range []string{
		"formal_gate_dispatch",
		"Current requirement",
		"Current diff or proposed change",
	} {
		if strings.TrimSpace(fields[strings.ToLower(field)]) != "" {
			continue
		}
		violations = append(violations, DispatchPromptViolation{
			Type:        "missing field",
			Matched:     field + ":",
			Label:       "current reviewer context",
			Description: "formal reviewer context requires a non-empty current context field",
		})
	}

	for _, field := range []string{
		"context bundle", "manifest", "related artifacts", "verification summary",
		"test report", "gate state", "receipt", "closure",
		"prior verdict", "prior verdicts", "previous verdict", "previous verdicts",
		"prior findings", "previous findings",
		"repair narrative", "repair narratives", "repair history", "transition chain", "rerun history", "carry history",
		"main-agent summary", "chat history",
		"target conclusion", "target conclusions",
		"directed focus", "focus items", "what to verify",
	} {
		if _, exists := fields[field]; !exists {
			continue
		}
		violations = append(violations, DispatchPromptViolation{
			Type:        "field",
			Matched:     field + ":",
			Label:       "extra reviewer context",
			Description: "formal reviewer context is limited to the current requirement, reviewer role, and current diff or proposed change",
		})
	}

	for _, field := range []string{"current requirement", "current diff or proposed change"} {
		normalizedValue := strings.ToLower(strings.ReplaceAll(fields[field], "\\", "/"))
		if !strings.Contains(normalizedValue, ".claude/gates/runs/") {
			continue
		}
		violations = append(violations, DispatchPromptViolation{
			Type:        "path",
			Matched:     field + ": .claude/gates/runs/",
			Label:       "workflow-run artifact",
			Description: "formal reviewer input must not expose a workflow-run artifact",
		})
	}
	return violations
}

func dispatchPromptFields(prompt string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(prompt, "\n") {
		label, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(label))
		if current := strings.TrimSpace(fields[key]); current == "" {
			fields[key] = strings.TrimSpace(value)
		}
	}
	return fields
}
