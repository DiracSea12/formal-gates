package worker

import (
	"fmt"
	"strings"

	"formal-gates/internal/requirement"
)

// Location selects published source evidence; it never carries a physical path.
type Location struct {
	Read            int `json:"read,omitempty"`
	Listing         int `json:"listing,omitempty"`
	Entry           int `json:"entry,omitempty"`
	Line            int `json:"line,omitempty"`
	RequirementLine int `json:"requirementLine,omitempty"`
}

// LocationFact is carried from the original verified read or listing evidence.
type LocationFact struct {
	Kind    string   `json:"kind"`
	Paths   []string `json:"paths,omitempty"`
	Content string   `json:"content,omitempty"`
}

// ResultSource carries the native verifier's answer and source-bound facts.
type ResultSource struct {
	Answer        []byte
	Digest        string
	Identity      string
	LocationFacts []LocationFact
}

func LocationSchema(withRequirement bool) map[string]any {
	selection := map[string]any{"type": "integer", "minimum": 1}
	properties := map[string]any{"read": selection, "listing": selection, "entry": selection, "line": selection}
	if withRequirement {
		properties["requirementLine"] = selection
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "description": "Select one original read number (optional line), or listing/entry numbers. requirementLine, when offered, selects a line in the original current requirement. The program supplies the source path."}
}

func LocationPaths(locations []Location, facts []LocationFact, document requirement.FrozenRequirementProjection) ([]string, error) {
	paths := []string{}
	for _, location := range locations {
		path, content, line := "", "", location.Line
		if location.RequirementLine != 0 {
			if location.Read != 0 || location.Listing != 0 || location.Entry != 0 || location.Line != 0 || document.Path == "" {
				return nil, fmt.Errorf("requirement location selects only an original requirement line")
			}
			path, content, line = document.Path, document.Content, location.RequirementLine
		} else {
			number, kind := location.Read, "read"
			if location.Read == 0 {
				number, kind = location.Listing, "listing"
			} else if location.Listing != 0 || location.Entry != 0 {
				return nil, fmt.Errorf("location selects one original read or listing entry")
			}
			if number < 1 || number > len(facts) || facts[number-1].Kind != kind {
				return nil, fmt.Errorf("location requires an original published %s", kind)
			}
			fact := facts[number-1]
			entry := location.Entry
			if kind == "read" {
				entry = 1
			} else if location.Line != 0 {
				return nil, fmt.Errorf("a line location requires an original read")
			}
			if entry < 1 || entry > len(fact.Paths) {
				return nil, fmt.Errorf("location selects an original published entry")
			}
			path, content = fact.Paths[entry-1], fact.Content
		}
		if line != 0 {
			if line < 1 || content == "" || line > len(strings.Split(strings.TrimSuffix(content, "\n"), "\n")) {
				return nil, fmt.Errorf("location line is outside the original published content")
			}
			path += fmt.Sprintf(":%d", line)
		}
		paths = append(paths, path)
	}
	return paths, nil
}
