// Package requirement parses the confirmed finite REQ/AC document.
package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type FrozenRequirementProjection struct {
	Path          string              `json:"path"`
	ContentDigest string              `json:"contentDigest"`
	Requirements  []FrozenRequirement `json:"requirements"`
}

type FrozenRequirement struct {
	ID                   string                      `json:"id"`
	Title                string                      `json:"title"`
	Requirement          string                      `json:"requirement"`
	Source               string                      `json:"source"`
	AcceptanceConditions []FrozenAcceptanceCondition `json:"acceptanceConditions"`
}

type FrozenAcceptanceCondition struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Line int    `json:"line"`
}

type RequirementPrecheckIssue struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Requirement string `json:"requirementId,omitempty"`
	Rule        string `json:"rule"`
}

type RequirementPrecheckError struct{ Issues []RequirementPrecheckIssue }

func (e *RequirementPrecheckError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		prefix := fmt.Sprintf("%s:%d", issue.Path, issue.Line)
		if issue.Requirement != "" {
			prefix += " [" + issue.Requirement + "]"
		}
		parts = append(parts, prefix+": "+issue.Rule)
	}
	return "requirement structure precheck failed: " + strings.Join(parts, "; ")
}

var (
	requirementIDPattern       = regexp.MustCompile(`^(REQ-[0-9]{3,})：(.+)$`)
	requirementIdentityPattern = regexp.MustCompile(`^(REQ-[0-9]+)(?:：|$)`)
	acceptancePattern          = regexp.MustCompile(`^(AC-[0-9]{3,})：(.+)$`)
	acceptancePartsPattern     = regexp.MustCompile(`^AC-([0-9]+)：(.*)$`)
)

type parsedRequirement struct {
	requirement FrozenRequirement
	field       string
	fieldLines  map[string]int
	fieldText   map[string][]string
	fieldOrder  []string
}

// ParseRequirementProjection performs the deterministic, fence-aware precheck
// and returns the exact finite REQ/AC projection from the single 需求点 section.
// It intentionally makes no semantic judgment about the prose.
func ParseRequirementProjection(path string, data []byte) (FrozenRequirementProjection, error) {
	if !utf8.Valid(data) {
		return FrozenRequirementProjection{}, &RequirementPrecheckError{Issues: []RequirementPrecheckIssue{{Path: path, Line: 1, Rule: "document is not valid UTF-8 Markdown"}}}
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	issues := []RequirementPrecheckIssue{}
	addIssue := func(line int, reqID, rule string) {
		if line < 1 {
			line = 1
		}
		issues = append(issues, RequirementPrecheckIssue{Path: path, Line: line, Requirement: reqID, Rule: rule})
	}

	var requirements []FrozenRequirement
	reqIDs, acIDs := map[string]int{}, map[string]int{}
	sectionCount, sectionLine := 0, 1
	inSection := false
	var current *parsedRequirement
	fenceChar := byte(0)
	fenceLength := 0

	finishRequirement := func(boundaryLine int) {
		if current == nil {
			return
		}
		reqID := current.requirement.ID
		expected := []string{"要求", "验收条件", "来源"}
		for _, field := range expected {
			if current.fieldLines[field] == 0 {
				addIssue(boundaryLine, reqID, "missing direct #### "+field+" field")
			}
		}
		if len(current.fieldOrder) == 3 {
			for i := range expected {
				if current.fieldOrder[i] != expected[i] {
					addIssue(current.fieldLines[current.fieldOrder[i]], reqID, "direct #### fields must appear exactly in 要求 → 验收条件 → 来源 order")
					break
				}
			}
		}
		requirementText := strings.TrimSpace(strings.Join(current.fieldText["要求"], "\n"))
		sourceText := strings.TrimSpace(strings.Join(current.fieldText["来源"], "\n"))
		if current.fieldLines["要求"] != 0 && requirementText == "" {
			addIssue(current.fieldLines["要求"], reqID, "要求 field must contain non-empty prose")
		}
		if current.fieldLines["来源"] != 0 && sourceText == "" {
			addIssue(current.fieldLines["来源"], reqID, "来源 field must contain non-empty prose")
		}
		if current.fieldLines["验收条件"] != 0 && len(current.requirement.AcceptanceConditions) == 0 {
			addIssue(current.fieldLines["验收条件"], reqID, "验收条件 must contain at least one direct - AC-nnn：... list item")
		}
		current.requirement.Requirement = requirementText
		current.requirement.Source = sourceText
		requirements = append(requirements, current.requirement)
		current = nil
	}

	for index, raw := range lines {
		lineNo := index + 1
		if marker, length, remainder, ok := markdownFence(raw); ok {
			if fenceChar == 0 {
				if marker != '`' || !strings.Contains(remainder, "`") {
					fenceChar, fenceLength = marker, length
				}
			} else if marker == fenceChar && length >= fenceLength && strings.TrimSpace(remainder) == "" {
				fenceChar, fenceLength = 0, 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}
		level, heading, isHeading := markdownHeading(raw)
		if isHeading && level == 2 {
			if inSection {
				finishRequirement(lineNo)
			}
			inSection = heading == "需求点"
			if inSection {
				sectionCount++
				sectionLine = lineNo
				if sectionCount > 1 {
					addIssue(lineNo, "", "document must contain exactly one ## 需求点 section")
				}
			}
			continue
		}
		if isHeading && level == 3 {
			if !inSection {
				if strings.HasPrefix(heading, "REQ-") {
					addIssue(lineNo, "", "REQ heading appears outside ## 需求点")
				}
				continue
			}
			finishRequirement(lineNo)
			match := requirementIDPattern.FindStringSubmatch(heading)
			if match == nil || strings.TrimSpace(match[2]) == "" {
				reqID := ""
				if identity := requirementIdentityPattern.FindStringSubmatch(heading); identity != nil {
					reqID = identity[1]
				}
				addIssue(lineNo, reqID, "every direct ### heading in ## 需求点 must match REQ-[0-9]{3,}：non-empty title")
				continue
			}
			reqID := match[1]
			if prior := reqIDs[reqID]; prior != 0 {
				addIssue(lineNo, reqID, fmt.Sprintf("duplicate REQ ID (first declared at line %d)", prior))
			} else {
				reqIDs[reqID] = lineNo
			}
			current = &parsedRequirement{
				requirement: FrozenRequirement{ID: reqID, Title: strings.TrimSpace(match[2])},
				fieldLines:  map[string]int{},
				fieldText:   map[string][]string{},
			}
			continue
		}
		if isHeading && level == 4 && inSection {
			if current == nil {
				addIssue(lineNo, "", "direct #### field appears before a valid REQ heading")
				continue
			}
			if heading != "要求" && heading != "验收条件" && heading != "来源" {
				addIssue(lineNo, current.requirement.ID, "REQ blocks may contain only direct #### 要求, #### 验收条件, and #### 来源 fields")
				current.field = ""
				continue
			}
			if current.fieldLines[heading] != 0 {
				addIssue(lineNo, current.requirement.ID, "duplicate direct #### "+heading+" field")
			} else {
				current.fieldLines[heading] = lineNo
				current.fieldOrder = append(current.fieldOrder, heading)
			}
			current.field = heading
			continue
		}

		listText, directList := markdownDirectList(raw)
		if directList {
			looksLikeAC := strings.HasPrefix(strings.TrimSpace(listText), "AC-")
			if !inSection || current == nil || current.field != "验收条件" {
				if looksLikeAC {
					addIssue(lineNo, requirementID(current), "AC list item appears outside its REQ's direct #### 验收条件 field")
				} else if current != nil && current.field != "" {
					current.fieldText[current.field] = append(current.fieldText[current.field], raw)
				}
			} else {
				trimmedList := strings.TrimSpace(listText)
				match := acceptancePattern.FindStringSubmatch(trimmedList)
				if match == nil || strings.TrimSpace(match[2]) == "" {
					rule := "every direct list item in 验收条件 must match - AC-[0-9]{3,}：non-empty condition"
					if parts := acceptancePartsPattern.FindStringSubmatch(trimmedList); parts != nil {
						switch {
						case len(parts[1]) < 3:
							rule = "AC number must contain at least three digits"
						case strings.TrimSpace(parts[2]) == "":
							rule = "AC condition text must be non-empty"
						}
					}
					addIssue(lineNo, current.requirement.ID, rule)
				} else {
					acID := match[1]
					if prior := acIDs[acID]; prior != 0 {
						addIssue(lineNo, current.requirement.ID, fmt.Sprintf("duplicate AC ID %s (first declared at line %d)", acID, prior))
					} else {
						acIDs[acID] = lineNo
					}
					current.requirement.AcceptanceConditions = append(current.requirement.AcceptanceConditions, FrozenAcceptanceCondition{ID: acID, Text: strings.TrimSpace(match[2]), Line: lineNo})
				}
			}
			continue
		}
		if current != nil && current.field != "" && strings.TrimSpace(raw) != "" {
			current.fieldText[current.field] = append(current.fieldText[current.field], raw)
		}
	}
	finishRequirement(len(lines))
	if sectionCount == 0 {
		addIssue(1, "", "document must contain exactly one ## 需求点 section")
	}
	if sectionCount == 1 && len(requirements) == 0 {
		addIssue(sectionLine, "", "## 需求点 must contain at least one valid direct REQ heading")
	}
	if len(issues) != 0 {
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Line < issues[j].Line })
		return FrozenRequirementProjection{}, &RequirementPrecheckError{Issues: issues}
	}
	sum := sha256.Sum256([]byte(normalized))
	return FrozenRequirementProjection{Path: filepath.ToSlash(path), ContentDigest: hex.EncodeToString(sum[:]), Requirements: requirements}, nil
}

func requirementID(current *parsedRequirement) string {
	if current == nil {
		return ""
	}
	return current.requirement.ID
}

func markdownFence(line string) (byte, int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return 0, 0, "", false
	}
	marker := trimmed[0]
	if marker != '`' && marker != '~' {
		return 0, 0, "", false
	}
	length := 0
	for length < len(trimmed) && trimmed[length] == marker {
		length++
	}
	if length < 3 {
		return 0, 0, "", false
	}
	return marker, length, trimmed[length:], true
}

func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || trimmed[0] != '#' {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level:])
	if cut := strings.LastIndex(text, " #"); cut >= 0 && strings.Trim(text[cut+1:], "#") == "" {
		text = strings.TrimSpace(text[:cut])
	}
	return level, text, true
}

func markdownDirectList(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 2 || trimmed[0] != '-' || (trimmed[1] != ' ' && trimmed[1] != '\t') {
		return "", false
	}
	return strings.TrimSpace(trimmed[2:]), true
}
