package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"formal-gates/internal/engine/coverage"
)

// mergeQADesign applies the author's increment to the retained case set. The
// original event remains the author receipt; the accepted result is this merged
// projection, which reviews, recovery and source inheritance already consume.
func mergeQADesign(previous *QADesign, delta QADesign, mode string, state *State) (QADesign, map[string]string, error) {
	clone := func(d QADesign) QADesign {
		b, _ := json.Marshal(d)
		var c QADesign
		_ = json.Unmarshal(b, &c)
		return c
	}
	delta = clone(delta)
	base := QADesign{Manifest: coverage.AcceptanceManifest{Binding: delta.Manifest.Binding}}
	if previous != nil {
		base = clone(*previous)
	}
	old := clone(base)
	if previous != nil && previous.Manifest.Binding != delta.Manifest.Binding {
		return QADesign{}, nil, fmt.Errorf("QA increment binding differs from retained design")
	}
	if delta.Replace {
		base = QADesign{Manifest: coverage.AcceptanceManifest{Binding: delta.Manifest.Binding}}
	}
	if delta.Replace && (len(delta.RemoveCases) != 0 || len(delta.RemoveSharedSteps) != 0) {
		return QADesign{}, nil, fmt.Errorf("replacement cannot also delete individual entries")
	}
	prefix := strings.ToUpper(mode) + ":C"
	next := 1
	observe := func(d *QADesign) {
		if d != nil {
			for _, c := range d.Manifest.Cases {
				if strings.HasPrefix(c.CaseID, prefix) {
					n, e := strconv.Atoi(strings.TrimPrefix(c.CaseID, prefix))
					if e == nil && n >= next {
						next = n + 1
					}
				}
			}
		}
	}
	observe(previous)
	if state != nil {
		for id, x := range state.RegularExecutions {
			if x.PreparationResult != nil && state.Results[id].Outcome != "" {
				observe(x.PreparationResult.Design)
			}
		}
	}
	known := map[string]bool{}
	for _, c := range old.Manifest.Cases {
		known[c.CaseID] = true
	}
	ids := map[string]string{}
	for i := range delta.Manifest.Cases {
		c := &delta.Manifest.Cases[i]
		local := c.CaseID
		if strings.TrimSpace(local) == "" {
			return QADesign{}, nil, fmt.Errorf("new QA cases require a unique local caseId for edge and dependency references")
		}
		if _, dup := ids[local]; dup {
			return QADesign{}, nil, fmt.Errorf("duplicate QA increment caseId %s", local)
		}
		id := local
		if !known[local] {
			id = fmt.Sprintf("%s%d", prefix, next)
			next++
		}
		ids[local] = id
		c.CaseID = id
	}
	for i := range delta.Manifest.Cases {
		for j, dep := range delta.Manifest.Cases[i].Dependencies {
			if id, ok := ids[dep]; ok {
				delta.Manifest.Cases[i].Dependencies[j] = id
			}
		}
	}
	for i := range delta.Manifest.Edges {
		if id, ok := ids[delta.Manifest.Edges[i].CaseID]; ok {
			delta.Manifest.Edges[i].CaseID = id
		}
	}
	var err error
	base.Manifest.Cases, err = mergeQAItems(base.Manifest.Cases, delta.Manifest.Cases, delta.RemoveCases, func(c coverage.AcceptanceCase) string { return c.CaseID })
	if err != nil {
		return QADesign{}, nil, err
	}
	base.Manifest.Cases, err = orderQACases(base.Manifest.Cases)
	if err != nil {
		return QADesign{}, nil, err
	}
	base.Manifest.SharedSteps, err = mergeQAItems(base.Manifest.SharedSteps, delta.Manifest.SharedSteps, delta.RemoveSharedSteps, func(s coverage.SharedStep) string { return s.ID })
	if err != nil {
		return QADesign{}, nil, err
	}
	base.Manifest.Points, err = mergeQAItems(base.Manifest.Points, delta.Manifest.Points, nil, func(p coverage.AcceptancePoint) string { return p.PointID })
	if err != nil {
		return QADesign{}, nil, err
	}
	replaceEdges := map[string]bool{}
	for _, e := range delta.Manifest.Edges {
		replaceEdges[e.CaseID] = true
	}
	for _, id := range delta.RemoveCases {
		replaceEdges[id] = true
	}
	edges := []coverage.CoverageEdge{}
	for _, e := range base.Manifest.Edges {
		if !replaceEdges[e.CaseID] {
			edges = append(edges, e)
		}
	}
	base.Manifest.Edges = append(edges, delta.Manifest.Edges...)
	base.Manifest.Sources = mergeQAStrings(base.Manifest.Sources, delta.Manifest.Sources)
	// testFiles is the existing explicit complete ownership declaration, not case content.
	base.TestFiles = delta.TestFiles
	base.Alternatives, err = mergeQAItems(base.Alternatives, delta.Alternatives, nil, func(a coverage.AlternativeVerification) string { return a.SourceID })
	if err != nil {
		return QADesign{}, nil, err
	}
	// Coverage is the independent reviewer's judgment. Author prose is not work.
	base.CoverageJudgment = ""
	base.Replace = false
	base.RemoveCases = nil
	base.RemoveSharedSteps = nil
	old.CoverageJudgment = ""
	if digestOfCanonical(old) == digestOfCanonical(base) {
		return QADesign{}, nil, fmt.Errorf("QA increment has no substantive additions, modifications or removals; repair the reported gaps")
	}
	if len(base.Manifest.Cases) == 0 && len(base.Manifest.SharedSteps) == 0 && previous == nil {
		return QADesign{}, nil, fmt.Errorf("initial QA draft requires executable case or shared preparation content")
	}
	return base, ids, nil
}

func mergeQAItems[T any](old, changes []T, removed []string, key func(T) string) ([]T, error) {
	index := map[string]int{}
	out := append([]T{}, old...)
	for i, v := range old {
		index[key(v)] = i
	}
	deletes := map[string]bool{}
	for _, id := range removed {
		if _, ok := index[id]; !ok || deletes[id] {
			return nil, fmt.Errorf("unknown or duplicate QA removal %s", id)
		}
		deletes[id] = true
	}
	seen := map[string]bool{}
	for _, v := range changes {
		id := key(v)
		if id == "" || seen[id] || deletes[id] {
			return nil, fmt.Errorf("invalid or conflicting QA increment ID %s", id)
		}
		seen[id] = true
		if i, ok := index[id]; ok {
			out[i] = v
		} else {
			index[id] = len(out)
			out = append(out, v)
		}
	}
	kept := out[:0]
	for _, v := range out {
		if !deletes[key(v)] {
			kept = append(kept, v)
		}
	}
	return kept, nil
}
func mergeQAStrings(old, added []string) []string {
	out := append([]string{}, old...)
	seen := map[string]bool{}
	for _, v := range old {
		seen[v] = true
	}
	for _, v := range added {
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
func mergeQAReuse(old, added []TestReuse, manifest coverage.AcceptanceManifest) []TestReuse {
	by := map[string]TestReuse{}
	for _, r := range old {
		by[r.CaseID] = r
	}
	for _, r := range added {
		by[r.CaseID] = r
	}
	out := []TestReuse{}
	for _, c := range manifest.Cases {
		if r, ok := by[c.CaseID]; ok {
			out = append(out, r)
		}
	}
	return out
}

// A retained trial is reusable only while its tested inputs remain unchanged.
func validateQAReuseIncrement(previous *QADesign, merged QADesign, supplied []TestReuse, changedFiles []string, editedCases ...string) error {
	records := map[string]bool{}
	for _, r := range supplied {
		if records[r.CaseID] {
			return fmt.Errorf("duplicate whitebox test run %s", r.CaseID)
		}
		records[r.CaseID] = true
	}
	// Reuse the same case/shared-step dependency comparison as review retention.
	// File ownership is explicitly reviewed; testRef is free text, not a path
	// grammar. With file edits, the submitted case increment declares their
	// affected cases; omitted cases retain their prior evidence.
	unchanged := map[string]bool{}
	if previous != nil {
		review := coverage.ReviewResult{}
		for _, c := range previous.Manifest.Cases {
			review.CaseDecisions = append(review.CaseDecisions, coverage.ReviewDecision{ID: c.CaseID})
		}
		for _, d := range ChangedQA(previous.Manifest, merged.Manifest, review).CaseDecisions {
			unchanged[d.ID] = true
		}
	}
	fileAffected := map[string]bool{}
	if len(changedFiles) > 0 {
		for _, id := range editedCases {
			fileAffected[id] = true
		}
	}
	for _, c := range merged.Manifest.Cases {
		changed := !unchanged[c.CaseID] || fileAffected[c.CaseID]
		if changed && !records[c.CaseID] {
			return fmt.Errorf("changed whitebox case %s requires fresh actual test evidence", c.CaseID)
		}
	}
	return nil
}

// Keep declared order when possible, moving only prerequisites before their users.
func orderQACases(cases []coverage.AcceptanceCase) ([]coverage.AcceptanceCase, error) {
	pending := append([]coverage.AcceptanceCase{}, cases...)
	ordered := make([]coverage.AcceptanceCase, 0, len(cases))
	done := map[string]bool{}
	for len(pending) > 0 {
		remaining := make([]coverage.AcceptanceCase, 0, len(pending))
		for _, c := range pending {
			ready := true
			for _, dep := range c.Dependencies {
				if !done[dep] {
					ready = false
					break
				}
			}
			if ready {
				ordered = append(ordered, c)
				done[c.CaseID] = true
			} else {
				remaining = append(remaining, c)
			}
		}
		if len(remaining) == len(pending) {
			return nil, fmt.Errorf("QA increment has an unknown or cyclic case dependency")
		}
		pending = remaining
	}
	return ordered, nil
}
