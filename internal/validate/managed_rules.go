package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
)

const managedRuleSourceRelativePath = "SKILL.md"
const hostInstructionsStartMarker = "<formal-gates:host-instructions:start>"
const hostInstructionsEndMarker = "<formal-gates:host-instructions:end>"

// LoadManagedRule extracts the single canonical marker block from SKILL.md.
// Uninstall only needs the marker pair and therefore does not depend on source.
func LoadManagedRule(root string) (string, error) {
	path := filepath.Join(lifecycle.CleanRoot(root), filepath.FromSlash(managedRuleSourceRelativePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("managed rule source: %w", err)
	}
	return extractManagedRule(string(data))
}

func extractManagedRule(text string) (string, error) {
	lines := splitManagedFileLines(text)
	start := -1
	end := -1
	for index, line := range lines {
		switch line.content {
		case hostInstructionsStartMarker:
			if start >= 0 {
				return "", fmt.Errorf("SKILL.md contains more than one managed rule start marker")
			}
			if end >= 0 {
				return "", fmt.Errorf("SKILL.md contains a managed rule start marker after the end marker")
			}
			start = index
		case hostInstructionsEndMarker:
			if start < 0 {
				return "", fmt.Errorf("SKILL.md managed rule end marker has no matching start marker")
			}
			if end >= 0 {
				return "", fmt.Errorf("SKILL.md contains more than one managed rule end marker")
			}
			end = index
		}
	}
	if start < 0 {
		return "", fmt.Errorf("SKILL.md managed rule start marker is missing")
	}
	if end < 0 {
		return "", fmt.Errorf("SKILL.md managed rule end marker is missing")
	}
	if end <= start+1 {
		return "", fmt.Errorf("SKILL.md managed rule block is empty")
	}

	content := make([]string, 0, end-start-1)
	for _, line := range lines[start+1 : end] {
		content = append(content, line.content)
	}
	rule := strings.Join(content, "\n")
	if err := validateManagedRule(rule); err != nil {
		return "", fmt.Errorf("SKILL.md managed rule block: %w", err)
	}
	return rule, nil
}

func validateManagedRule(rule string) error {
	if strings.TrimSpace(rule) == "" {
		return fmt.Errorf("current rule is empty")
	}
	if rule != strings.TrimSpace(rule) {
		return fmt.Errorf("current rule must not have leading or trailing whitespace")
	}
	if strings.Contains(rule, "\r") {
		return fmt.Errorf("current rule must use LF line endings")
	}
	if strings.Contains(rule, hostInstructionsStartMarker) || strings.Contains(rule, hostInstructionsEndMarker) {
		return fmt.Errorf("current rule must not contain managed rule markers")
	}
	return nil
}

type managedFileLine struct {
	content string
	raw     string
}

func replaceManagedRuleBlock(text, rule string) (string, error) {
	if err := validateManagedRule(rule); err != nil {
		return "", err
	}
	updated, found, err := rewriteManagedRuleBlocks(text, rule, true)
	if err != nil {
		return "", err
	}
	if found {
		return updated, nil
	}
	return appendManagedRuleBlock(updated, rule), nil
}

func removeManagedRuleBlocks(text string) (string, bool, error) {
	return rewriteManagedRuleBlocks(text, "", false)
}

func rewriteManagedRuleBlocks(text, rule string, install bool) (string, bool, error) {
	lines := splitManagedFileLines(text)
	var ruleLines []string
	if install {
		ruleLines = managedRuleLines(rule)
	}

	var builder strings.Builder
	found := false
	wroteReplacement := false
	for index := 0; index < len(lines); {
		switch lines[index].content {
		case hostInstructionsStartMarker:
			end := -1
			for candidate := index + 1; candidate < len(lines); candidate++ {
				switch lines[candidate].content {
				case hostInstructionsStartMarker:
					return "", false, fmt.Errorf("managed rule block has a nested start marker at line %d", candidate+1)
				case hostInstructionsEndMarker:
					end = candidate
				}
				if end >= 0 {
					break
				}
			}
			if end < 0 {
				return "", false, fmt.Errorf("managed rule block start marker at line %d has no matching end marker", index+1)
			}
			found = true
			if install && !wroteReplacement {
				builder.WriteString(formatManagedRuleBlock(rule, managedFileNewline(text)))
				wroteReplacement = true
			}
			index = end + 1
			continue
		case hostInstructionsEndMarker:
			return "", false, fmt.Errorf("managed rule block end marker at line %d has no matching start marker", index+1)
		}

		// Migrate an unmarked copy of the current rule when switching to the
		// marker format. Other text is never guessed to be installer-owned.
		if install && managedBlockMatches(lines, index, ruleLines) {
			found = true
			if !wroteReplacement {
				builder.WriteString(formatManagedRuleBlock(rule, managedFileNewline(text)))
				wroteReplacement = true
			}
			index += len(ruleLines)
			continue
		}

		builder.WriteString(lines[index].raw)
		index++
	}
	return builder.String(), found, nil
}

func appendManagedRuleBlock(text, rule string) string {
	separator := managedFileNewline(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += separator
	}
	return text + formatManagedRuleBlock(rule, separator)
}

func formatManagedRuleBlock(rule, separator string) string {
	rule = strings.ReplaceAll(rule, "\r\n", "\n")
	rule = strings.ReplaceAll(rule, "\n", separator)
	return hostInstructionsStartMarker + separator + rule + separator + hostInstructionsEndMarker + separator
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

func manageManagedRuleFile(path, rule string) (bool, error) {
	data, err := os.ReadFile(path)
	existsAlready := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	current := string(data)
	updated, err := replaceManagedRuleBlock(current, rule)
	if err != nil {
		return false, err
	}
	if existsAlready && updated == current {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	return true, writeAtomic(path, []byte(updated), 0o600)
}

func removeManagedRuleFile(path string, removeEmpty bool) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, removed, err := removeManagedRuleBlocks(string(data))
	if err != nil {
		return err
	}
	if !removed {
		return nil
	}
	if removeEmpty && strings.TrimSpace(updated) == "" {
		return os.Remove(path)
	}
	return writeAtomic(path, []byte(updated), 0o600)
}
