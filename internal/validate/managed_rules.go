package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const managedRulesRelativePath = "references/managed-rules.json"

// LoadManagedRules reads the ordered ownership record used by both install and
// uninstall.
func LoadManagedRules(root string) ([]string, error) {
	path := filepath.Join(cleanRoot(root), filepath.FromSlash(managedRulesRelativePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("managed rule catalog: %w", err)
	}

	var versions []string
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, fmt.Errorf("managed rule catalog is invalid JSON: %w", err)
	}
	if err := validateManagedRuleVersions(versions); err != nil {
		return nil, fmt.Errorf("managed rule catalog: %w", err)
	}
	return versions, nil
}

func validateManagedRuleVersions(versions []string) error {
	if len(versions) == 0 {
		return fmt.Errorf("must contain at least one rule version")
	}
	seen := make(map[string]struct{}, len(versions))
	for index, version := range versions {
		if strings.TrimSpace(version) == "" {
			return fmt.Errorf("version %d is empty", index+1)
		}
		if version != strings.TrimSpace(version) {
			return fmt.Errorf("version %d is not a complete rule block", index+1)
		}
		if strings.Contains(version, "\r") {
			return fmt.Errorf("version %d must use LF line endings", index+1)
		}
		if _, ok := seen[version]; ok {
			return fmt.Errorf("version %d duplicates an earlier rule", index+1)
		}
		seen[version] = struct{}{}
	}
	return nil
}

type managedFileLine struct {
	content string
	raw     string
}

func replaceManagedRuleBlocks(text string, versions []string, latest string) (string, error) {
	if err := validateManagedRuleVersions(versions); err != nil {
		return "", err
	}
	if latest != versions[len(versions)-1] {
		return "", fmt.Errorf("latest managed rule must be the newest catalog entry")
	}
	cleaned, _ := removeManagedRuleBlocks(text, versions)
	return appendManagedRuleBlock(cleaned, latest), nil
}

func removeManagedRuleBlocks(text string, versions []string) (string, bool) {
	lines := splitManagedFileLines(text)
	patterns := make([][]string, 0, len(versions))
	for _, version := range versions {
		patterns = append(patterns, managedRuleLines(version))
	}

	var builder strings.Builder
	removed := false
	for index := 0; index < len(lines); {
		matchLength := 0
		for _, pattern := range patterns {
			if len(pattern) <= matchLength || !managedBlockMatches(lines, index, pattern) {
				continue
			}
			matchLength = len(pattern)
		}
		if matchLength > 0 {
			index += matchLength
			removed = true
			continue
		}
		builder.WriteString(lines[index].raw)
		index++
	}
	return builder.String(), removed
}

func appendManagedRuleBlock(text, rule string) string {
	separator := managedFileNewline(text)
	rule = strings.ReplaceAll(rule, "\r\n", "\n")
	rule = strings.ReplaceAll(rule, "\n", separator)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += separator
	}
	return text + rule + separator
}

func splitManagedFileLines(text string) []managedFileLine {
	if text == "" {
		return nil
	}
	lines := make([]managedFileLine, 0, strings.Count(text, "\n")+1)
	for len(text) > 0 {
		newline := strings.IndexByte(text, '\n')
		if newline < 0 {
			lines = append(lines, managedFileLine{content: text, raw: text})
			break
		}
		raw := text[:newline+1]
		content := raw[:newline]
		if strings.HasSuffix(content, "\r") {
			content = strings.TrimSuffix(content, "\r")
		}
		lines = append(lines, managedFileLine{content: content, raw: raw})
		text = text[newline+1:]
	}
	return lines
}

func managedRuleLines(rule string) []string {
	rule = strings.ReplaceAll(rule, "\r\n", "\n")
	return strings.Split(rule, "\n")
}

func managedBlockMatches(lines []managedFileLine, start int, pattern []string) bool {
	if len(pattern) == 0 || start+len(pattern) > len(lines) {
		return false
	}
	for offset, expected := range pattern {
		if lines[start+offset].content != expected {
			return false
		}
	}
	return true
}

func managedFileNewline(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		if index > 0 && text[index-1] == '\r' {
			return "\r\n"
		}
		return "\n"
	}
	return "\n"
}

func manageManagedRuleFile(path string, versions []string) error {
	if len(versions) == 0 {
		return fmt.Errorf("managed rule catalog is empty")
	}
	data, err := os.ReadFile(path)
	existsAlready := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	current := string(data)
	updated, err := replaceManagedRuleBlocks(current, versions, versions[len(versions)-1])
	if err != nil {
		return err
	}
	if existsAlready && updated == current {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}

func removeManagedRuleFile(path string, versions []string, removeEmpty bool) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed := removeManagedRuleBlocks(string(data), versions)
	if !removed {
		return nil
	}
	if removeEmpty && strings.TrimSpace(updated) == "" {
		return os.Remove(path)
	}
	return os.WriteFile(path, []byte(updated), 0o600)
}
