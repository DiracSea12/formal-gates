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

type DispatchPromptBinding struct {
	Name string
	Path string
}

type PrepareDispatchPromptOptions struct {
	Root         string
	DispatchFile string
	OutputFile   string
	ConfigPath   string
	Bindings     []DispatchPromptBinding
}

type PreparedDispatchPrompt struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

const dispatchStaticValidationPrefix = "static-validation=PASS sha256="

var dispatchStaticValidationPattern = regexp.MustCompile(`static-validation=PASS sha256=([a-f0-9]{64})`)

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

	violations := findDispatchPromptViolations(options.PromptText, config, options.FinalSend, &result)
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
	dispatchPath := resolvePath(root, options.DispatchFile)
	dispatchBytes, err := os.ReadFile(dispatchPath)
	if err != nil {
		var result Result
		result.add(slash(options.DispatchFile), fmt.Sprintf("cannot read prompt template: %v", err))
		return PreparedDispatchPrompt{}, result
	}

	template := string(dispatchBytes)
	templateResult, _ := DispatchPromptWithViolations(DispatchPromptOptions{
		Root:       root,
		PromptText: template,
		ConfigPath: options.ConfigPath,
		FinalSend:  true,
	})
	if !templateResult.OK() {
		return PreparedDispatchPrompt{}, templateResult
	}

	runDir, _, err := promptRunRelativePath(root, options.DispatchFile)
	if err != nil {
		var result Result
		result.add(slash(options.DispatchFile), err.Error())
		return PreparedDispatchPrompt{}, result
	}
	outputRunDir, outputLogical, err := promptRunRelativePath(root, options.OutputFile)
	if err != nil {
		var result Result
		result.add(slash(options.OutputFile), err.Error())
		return PreparedDispatchPrompt{}, result
	}
	if !samePath(runDir, outputRunDir) {
		var result Result
		result.add(slash(options.OutputFile), "prepared prompt output must belong to the prompt template run")
		return PreparedDispatchPrompt{}, result
	}
	if samePath(dispatchPath, resolvePath(root, options.OutputFile)) {
		var result Result
		result.add(slash(options.OutputFile), "prepared prompt output must not overwrite the prompt template")
		return PreparedDispatchPrompt{}, result
	}

	fields := strictDispatchPromptFields(template)
	formatLower := strings.ToLower(fields["output format"])
	if strings.Contains(formatLower, "routing-only bindings") || strings.Contains(formatLower, "static-validation=") {
		var result Result
		result.add("dispatch-prompt", "prompt template Output format must not contain prebuilt routing-only bindings or static validation")
		return PreparedDispatchPrompt{}, result
	}

	bindingText := []string{}
	seen := map[string]bool{"dispatch": true}
	for _, binding := range options.Bindings {
		name := strings.TrimSpace(binding.Name)
		if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`).MatchString(name) {
			var result Result
			result.add("binding", fmt.Sprintf("invalid binding name %q", binding.Name))
			return PreparedDispatchPrompt{}, result
		}
		key := strings.ToLower(name)
		if seen[key] {
			var result Result
			result.add("binding", fmt.Sprintf("duplicate binding name %q", name))
			return PreparedDispatchPrompt{}, result
		}
		seen[key] = true
		bindingRunDir, logical, err := promptRunRelativePath(root, binding.Path)
		if err != nil {
			var result Result
			result.add(slash(binding.Path), err.Error())
			return PreparedDispatchPrompt{}, result
		}
		if !samePath(runDir, bindingRunDir) {
			var result Result
			result.add(slash(binding.Path), "prompt binding must belong to the prompt template run")
			return PreparedDispatchPrompt{}, result
		}
		data, err := os.ReadFile(resolvePath(root, binding.Path))
		if err != nil {
			var result Result
			result.add(slash(binding.Path), fmt.Sprintf("cannot read prompt binding: %v", err))
			return PreparedDispatchPrompt{}, result
		}
		bindingText = append(bindingText, name+"="+logical+" sha256="+sha256Bytes(data))
	}

	fields["output format"] = strings.TrimSuffix(strings.TrimSpace(fields["output format"]), ";")
	if len(bindingText) > 0 {
		fields["output format"] += "; routing-only bindings " + strings.Join(bindingText, ", ") + "; do not read these files"
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

func addDispatchStaticValidation(prompt string) (string, error) {
	fields := strictDispatchPromptFields(prompt)
	format := fields["output format"]
	format = dispatchStaticValidationPattern.ReplaceAllString(format, "")
	if strings.Contains(strings.ToLower(format), "static-validation=") {
		return "", fmt.Errorf("final-send prompt contains a malformed static-validation binding")
	}
	format = strings.TrimSpace(strings.Trim(strings.TrimSpace(format), ";"))
	fields["output format"] = format + "; " + dispatchStaticValidationPrefix + strings.Repeat("0", 64)
	var builder strings.Builder
	for _, field := range strictDispatchPromptFieldOrder {
		builder.WriteString(field)
		builder.WriteString(": ")
		builder.WriteString(fields[strings.ToLower(field)])
		builder.WriteByte('\n')
	}
	return sealDispatchStaticValidation(builder.String()), nil
}

func sealDispatchStaticValidation(prompt string) string {
	placeholder := dispatchStaticValidationPrefix + strings.Repeat("0", 64)
	if strings.Count(prompt, placeholder) != 1 {
		return prompt
	}
	return strings.Replace(prompt, placeholder, dispatchStaticValidationPrefix+sha256Bytes([]byte(prompt)), 1)
}

func validateDispatchStaticMarker(prompt string, result *Result, where string) bool {
	matches := dispatchStaticValidationPattern.FindAllStringSubmatchIndex(prompt, -1)
	if len(matches) != 1 {
		result.add(where, "final-send prompt must contain exactly one machine-generated static-validation PASS binding")
		return false
	}
	match := matches[0]
	want := prompt[match[2]:match[3]]
	normalized := prompt[:match[2]] + strings.Repeat("0", 64) + prompt[match[3]:]
	if sha256Bytes([]byte(normalized)) != want {
		result.add(where, "final-send prompt static-validation binding does not match its exact fields")
		return false
	}
	return true
}

func findDispatchPromptViolations(prompt string, config pollutionConfig, finalSend bool, result *Result) []DispatchPromptViolation {
	violations := findDispatchPromptFieldViolations(prompt)
	if finalSend {
		violations = findFinalDispatchPromptFieldViolations(prompt)
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

func findFinalDispatchPromptFieldViolations(prompt string) []DispatchPromptViolation {
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
		violations = append(violations, DispatchPromptViolation{
			Type: "path", Matched: field + ": .claude/gates/runs/", Label: "workflow-run artifact",
			Description: "formal reviewer input must not expose a workflow-run artifact",
		})
	}
	return violations
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
