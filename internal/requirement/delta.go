package requirement

import "strings"

// TextHunk preserves the exact removed and added lines, including final newlines.
type TextHunk struct {
	BaseStart      int      `json:"baseStart"`
	CandidateStart int      `json:"candidateStart"`
	Removed        []string `json:"removed"`
	Added          []string `json:"added"`
}

// TextDelta groups contiguous changes between complete normalized documents.
func TextDelta(base, candidate string) []TextHunk {
	if base == candidate {
		return nil
	}
	lines := func(text string) []string {
		parts := strings.SplitAfter(text, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		return parts
	}
	a, b := lines(base), lines(candidate)
	lengths := make([][]int, len(a)+1)
	for i := range lengths {
		lengths[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}
	var hunks []TextHunk
	for i, j := 0, 0; i < len(a) || j < len(b); {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i++
			j++
			continue
		}
		hunk := TextHunk{BaseStart: i + 1, CandidateStart: j + 1, Removed: []string{}, Added: []string{}}
		for (i < len(a) || j < len(b)) && !(i < len(a) && j < len(b) && a[i] == b[j]) {
			if i < len(a) && (j == len(b) || lengths[i+1][j] >= lengths[i][j+1]) {
				hunk.Removed = append(hunk.Removed, a[i])
				i++
			} else {
				hunk.Added = append(hunk.Added, b[j])
				j++
			}
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}
