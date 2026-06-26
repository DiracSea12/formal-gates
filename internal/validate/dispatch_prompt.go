package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type DispatchPromptOptions struct {
	Root       string
	PromptText string
	ConfigPath string
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

	violations := findDispatchPromptViolations(options.PromptText, config, &result)
	for _, violation := range violations {
		result.add("dispatch-prompt", fmt.Sprintf("prompt contains prohibited anchoring %s %q (%s)", violation.Type, violation.Matched, violation.Label))
	}
	return result, violations
}

func findDispatchPromptViolations(prompt string, config pollutionConfig, result *Result) []DispatchPromptViolation {
	violations := []DispatchPromptViolation{}
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
