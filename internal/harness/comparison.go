package harness

import (
	"fmt"
	"strings"
)

// comparisonPlanInput is the payload for the comparison-plan bridge operation.
// It carries the already-decoded git --raw --no-abbrev -z output plus the
// metadata the Python side already computed (base, candidate, diff name). Git
// invocation, file I/O, tamper checks and proof collection remain in Python;
// this operation only owns the parse + representation rules.
type comparisonPlanInput struct {
	Base      string `json:"base"`
	Candidate string `json:"candidate"`
	Diff      string `json:"diff"`
	Raw       string `json:"raw"`
}

// planComparison parses git's raw diff output into the same `changes` shape the
// Python implementation produced, and returns canonical bytes for
// comparison.json. The returned `index` string is canonical({base, candidate,
// diff, changes}); `changes` reuses the very same structure so the two can never
// drift.
//
// The raw format (git diff --raw --no-abbrev -z) is a NUL-separated stream:
//
//	:src_mode dst_mode src_oid dst_oid status NUL src_path NUL [dst_path NUL]
//
// Rename/Copy records carry two paths; every other status carries one. The
// whole stream ends with a NUL, so splitting on NUL yields a trailing empty
// element that mirrors Python's raw.split(b'\0')[:-1] and is dropped here.
func planComparison(q comparisonPlanInput) (map[string]any, error) {
	records := strings.Split(q.Raw, "\x00")
	if len(records) > 0 {
		// Mirror Python's raw.split(b'\0')[:-1]: always drop the last element.
		records = records[:len(records)-1]
	}
	changes := []any{}
	i := 0
	for i < len(records) {
		header := records[i]
		i++
		fields := strings.Fields(header)
		if len(fields) != 5 {
			return nil, fmt.Errorf("comparison-plan: malformed record header %q", header)
		}
		beforeMode := strings.TrimPrefix(fields[0], ":")
		afterMode := fields[1]
		beforeOid := fields[2]
		afterOid := fields[3]
		status := fields[4]
		if status == "" {
			return nil, fmt.Errorf("comparison-plan: record %q has empty status", header)
		}
		if i >= len(records) {
			return nil, fmt.Errorf("comparison-plan: record %q is missing its old path", header)
		}
		oldPath := records[i]
		i++
		newPath := oldPath
		if status[0] == 'R' || status[0] == 'C' {
			if i >= len(records) {
				return nil, fmt.Errorf("comparison-plan: rename/copy record %q is missing its new path", header)
			}
			newPath = records[i]
			i++
		}
		change := map[string]any{
			"status":    status,
			"base":      sideFromRaw(beforeMode, beforeOid, oldPath, "base"),
			"candidate": sideFromRaw(afterMode, afterOid, newPath, "candidate"),
		}
		changes = append(changes, change)
	}
	indexValue := map[string]any{
		"base":      q.Base,
		"candidate": q.Candidate,
		"diff":      q.Diff,
		"changes":   changes,
	}
	indexBytes, err := CanonicalJSON(indexValue)
	if err != nil {
		return nil, err
	}
	return map[string]any{"changes": changes, "index": string(indexBytes)}, nil
}

// sideFromRaw builds one side of a change. A 000000 mode means the side is
// absent and becomes null. A 160000 gitlink carries identity only (a note, no
// material). Every other mode is a regular blob whose content is exported under
// "<side>/<path>".
func sideFromRaw(mode, oid, name, side string) any {
	if mode == "000000" {
		return nil
	}
	entry := map[string]any{
		"path":   name,
		"mode":   mode,
		"object": oid,
	}
	if mode == "160000" {
		entry["note"] = "Gitlink only; nested repository content is not included."
	} else {
		entry["material"] = side + "/" + name
	}
	return entry
}
