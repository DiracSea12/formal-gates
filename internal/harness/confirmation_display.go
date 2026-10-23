package harness

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// confirmationMessage bounds the host form to 800 Unicode characters and
// 12 logical lines. Only this display is shortened; callers retain the original
// candidate for confirmation, freshness checks and delivery.
func confirmationMessage(value any) (string, error) {
	if candidate, ok := value.(nativeAnswerCandidate); ok {
		view, err := nativeAnswerConfirmationView(candidate)
		if err != nil {
			return "", err
		}
		value = view
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	view, ok := decoded.(map[string]any)
	if !ok {
		view = map[string]any{"operation": decoded}
	}
	keys := []string{}
	for _, key := range []string{"answer", "selection", "decision", "input", "root", "run", "directory", "pending", "question", "operation", "planDigest", "revision"} {
		if _, ok := view[key]; ok {
			keys = append(keys, key)
		}
	}
	rest := []string{}
	for key := range view {
		if !slices.Contains(keys, key) {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)
	omitted := len(keys) > 6
	if omitted {
		keys = keys[:6]
	}
	lines := []string{}
	for _, key := range keys {
		name, _ := json.Marshal(confirmationExcerpt(key, 24))
		content, err := json.Marshal(view[key])
		if err != nil {
			return "", err
		}
		line := "  " + string(name) + ": " + string(content)
		text, ok := view[key].(string)
		if !ok {
			text = string(content)
		}
		limit := 70
		for utf8.RuneCountInString(line) > 110 {
			content, _ = json.Marshal(confirmationExcerpt(strings.Join(strings.Fields(text), " "), limit))
			line = "  " + string(name) + ": " + string(content)
			limit /= 2
			omitted = true
		}
		lines = append(lines, line)
	}
	if omitted {
		lines = append(lines, `  "_display": "长内容或其余项目已省略，请核对前文完整候选；确认后提交原完整对象与答复。"`)
	}
	return "Confirm this exact operation:\n{\n" + strings.Join(lines, ",\n") + "\n}", nil
}

func confirmationExcerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit < 5 {
		return "…"
	}
	head := (limit - 3) / 2
	return string(runes[:head]) + " … " + string(runes[len(runes)-(limit-3-head):])
}
