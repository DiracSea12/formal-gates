package requirement

import (
	"fmt"
	"strings"
)

// SemanticChange selects existing items by their published positions. Omitted
// fields retain their source bytes; new items have no caller-supplied identity.
type SemanticChange struct {
	Item            int
	Remove          bool
	Title           *string
	Requirement     *string
	Source          *string
	Context         *string
	Solution        *string
	Condition       int
	Acceptance      *string
	RemoveCondition bool
}

// RenderSemanticChange owns new Markdown structure and identities. Existing
// edits splice only the selected semantic fields into the original bytes.
func RenderSemanticChange(path string, data []byte, change SemanticChange) ([]byte, error) {
	var document FrozenRequirementProjection
	var layout documentLayout
	initial := len(data) == 0
	if initial {
		context, solution := "", ""
		if change.Context != nil {
			context = *change.Context
		}
		if change.Solution != nil {
			solution = *change.Solution
		}
		prefix := "# 需求与方案\n\n## 背景与目标\n\n" + context + "\n\n## 需求点\n\n"
		data = []byte(prefix + "## 方案\n\n" + solution + "\n")
		// Initial creation has no item for the finite projection to precheck yet.
		layout.end = len(strings.SplitAfter(prefix, "\n")) - 1
	} else {
		if change.Context != nil || change.Solution != nil {
			return nil, fmt.Errorf("context and solution prose are only available when creating the document")
		}
		var err error
		document, err = parseRequirementProjection(path, data, &layout)
		if err != nil {
			return nil, err
		}
	}
	if change.Item < 0 || change.Item > len(document.Requirements) {
		return nil, fmt.Errorf("requirement position must be 1..%d, or 0 for a new item", len(document.Requirements))
	}
	if change.Item == 0 && (change.Remove || change.Condition != 0 || change.RemoveCondition) {
		return nil, fmt.Errorf("a new requirement cannot select an existing item or condition")
	}
	if change.Remove && (change.Title != nil || change.Requirement != nil || change.Source != nil || change.Acceptance != nil || change.RemoveCondition || change.Condition != 0) {
		return nil, fmt.Errorf("removal cannot also change requirement fields")
	}
	for _, text := range []*string{change.Title, change.Acceptance} {
		if text != nil && strings.ContainsAny(*text, "\r\n") {
			return nil, fmt.Errorf("title and acceptance condition must be one line")
		}
	}
	lines := strings.SplitAfter(string(data), "\n")
	ending := "\n"
	if strings.Contains(string(data), "\r\n") {
		ending = "\r\n"
	}
	nextID := func(prefix string) string {
		used := map[string]bool{}
		for _, item := range document.Requirements {
			if prefix == "REQ" {
				used[item.ID] = true
			} else {
				for _, condition := range item.AcceptanceConditions {
					used[condition.ID] = true
				}
			}
		}
		for i := 1; ; i++ {
			id := fmt.Sprintf("%s-%03d", prefix, i)
			if !used[id] {
				return id
			}
		}
	}
	expected := append([]FrozenRequirement(nil), document.Requirements...)
	if change.Item == 0 {
		if change.Title == nil || change.Requirement == nil || change.Source == nil || change.Acceptance == nil {
			return nil, fmt.Errorf("new requirement needs title, requirement, acceptance and source prose")
		}
		block := fmt.Sprintf("\n### %s：%s\n\n#### 要求\n\n%s\n\n#### 验收条件\n\n- %s：%s\n\n#### 来源\n\n%s\n\n", nextID("REQ"), *change.Title, *change.Requirement, nextID("AC"), *change.Acceptance, *change.Source)
		block = strings.ReplaceAll(block, "\n", ending)
		if layout.end > 0 && !strings.HasSuffix(lines[layout.end-1], "\n") {
			block = ending + block
		}
		lines = spliceLines(lines, layout.end, layout.end, block)
	} else {
		item := document.Requirements[change.Item-1]
		span := layout.items[change.Item-1]
		if change.Condition < 0 || change.Condition > len(item.AcceptanceConditions) {
			return nil, fmt.Errorf("condition position must be 1..%d, or 0 to append", len(item.AcceptanceConditions))
		}
		if change.RemoveCondition && change.Condition == 0 {
			return nil, fmt.Errorf("condition removal requires an existing condition position")
		}
		if change.Condition != 0 && change.Acceptance == nil && !change.RemoveCondition {
			return nil, fmt.Errorf("condition selection requires replacement text or removal")
		}
		if change.Remove {
			lines = spliceLines(lines, span.heading, span.end, "")
			expected = append(expected[:change.Item-1], expected[change.Item:]...)
		} else {
			if change.Source != nil {
				lines = replaceProse(lines, span.fields["来源"]+1, span.end, *change.Source, ending)
			}
			if change.Condition == 0 && change.Acceptance != nil {
				lines = spliceLines(lines, span.fields["来源"], span.fields["来源"], "- "+nextID("AC")+"："+*change.Acceptance+ending)
			} else if change.Condition != 0 {
				condition := item.AcceptanceConditions[change.Condition-1]
				at := condition.Line - 1
				if change.RemoveCondition {
					lines = spliceLines(lines, at, at+1, "")
					expected[change.Item-1].AcceptanceConditions = append(append([]FrozenAcceptanceCondition(nil), item.AcceptanceConditions[:change.Condition-1]...), item.AcceptanceConditions[change.Condition:]...)
				} else {
					replacement := *change.Acceptance
					originalText, _ := markdownDirectList(lines[at])
					if acceptancePattern.FindStringSubmatch(originalText) == nil && strings.HasPrefix(replacement, "AC-") {
						replacement = condition.ID + "：" + replacement
					}
					lines[at] = replaceSemanticText(lines[at], condition.Text, replacement)
				}
			}
			if change.Requirement != nil {
				lines = replaceProse(lines, span.fields["要求"]+1, span.fields["验收条件"], *change.Requirement, ending)
			}
			if change.Title != nil {
				replacement := *change.Title
				_, originalHeading, _ := markdownHeading(lines[span.heading])
				if requirementIDPattern.FindStringSubmatch(originalHeading) == nil && strings.HasPrefix(replacement, "REQ-") {
					replacement = item.ID + "：" + replacement
				}
				lines[span.heading] = replaceSemanticText(lines[span.heading], item.Title, replacement)
			}
		}
	}
	result := []byte(strings.Join(lines, ""))
	if change.Remove || change.RemoveCondition {
		// Only identities whose implicit positions changed need an explicit marker.
		projection, err := parseRequirementProjection(path, result, &layout)
		if err != nil {
			return nil, err
		}
		lines = strings.SplitAfter(string(result), "\n")
		for i, item := range projection.Requirements {
			if item.ID != expected[i].ID {
				_, text, _ := markdownHeading(lines[layout.items[i].heading])
				lines[layout.items[i].heading] = replaceSemanticText(lines[layout.items[i].heading], text, expected[i].ID+"："+text)
			}
			for j, condition := range item.AcceptanceConditions {
				if condition.ID != expected[i].AcceptanceConditions[j].ID {
					text, _ := markdownDirectList(lines[condition.Line-1])
					lines[condition.Line-1] = replaceSemanticText(lines[condition.Line-1], text, expected[i].AcceptanceConditions[j].ID+"："+text)
				}
			}
		}
		result = []byte(strings.Join(lines, ""))
	}
	if _, err := ParseRequirementProjection(path, result); err != nil {
		return nil, err
	}
	return result, nil
}

func spliceLines(lines []string, start, end int, text string) []string {
	result := append([]string(nil), lines[:start]...)
	if text != "" {
		result = append(result, text)
	}
	return append(result, lines[end:]...)
}

func replaceProse(lines []string, start, end int, text, ending string) []string {
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", ending)
	if end > start && strings.HasSuffix(lines[end-1], "\n") {
		text += ending
	}
	return spliceLines(lines, start, end, text)
}

func replaceSemanticText(line, original, replacement string) string {
	text := strings.TrimSpace(line)
	if _, heading, ok := markdownHeading(line); ok {
		text = heading
	} else if item, ok := markdownDirectList(line); ok {
		text = item
	}
	at := strings.Index(line, text) + strings.LastIndex(text, original)
	return line[:at] + replacement + line[at+len(original):]
}
