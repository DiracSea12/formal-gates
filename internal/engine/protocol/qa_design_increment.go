package protocol

import (
	"fmt"
	"slices"

	"formal-gates/internal/engine/coverage"
)

// QADesignChange names explicit removals and coverage replacements. Omitted
// content remains in the CaseSet manifest; this value carries no second state.
type QADesignChange struct {
	Replace           bool     `json:"replace,omitempty"`
	RemoveCases       []string `json:"removeCases,omitempty"`
	RemoveSharedSteps []string `json:"removeSharedSteps,omitempty"`
	CoveragePoints    []string `json:"coveragePoints,omitempty"`
}

// MergeQADesign applies an authored increment to the existing manifest.
// Stable IDs, original order and untouched coverage retain their original values.
func MergeQADesign(previous, delta coverage.AcceptanceManifest, change QADesignChange) (coverage.AcceptanceManifest, error) {
	if change.Replace && (len(change.RemoveCases) > 0 || len(change.RemoveSharedSteps) > 0) {
		return coverage.AcceptanceManifest{}, fmt.Errorf("replacement cannot also remove individual QA entries")
	}
	base := previous
	base.Binding = delta.Binding
	if change.Replace {
		base = coverage.AcceptanceManifest{Binding: delta.Binding}
	}
	var err error
	base.Cases, err = mergeQAItems(base.Cases, delta.Cases, change.RemoveCases, func(item coverage.AcceptanceCase) string { return item.CaseID })
	if err != nil {
		return coverage.AcceptanceManifest{}, err
	}
	base.SharedSteps, err = mergeQAItems(base.SharedSteps, delta.SharedSteps, change.RemoveSharedSteps, func(item coverage.SharedStep) string { return item.ID })
	if err != nil {
		return coverage.AcceptanceManifest{}, err
	}
	base.Points, err = mergeQAItems(base.Points, delta.Points, nil, func(item coverage.AcceptancePoint) string { return item.PointID })
	if err != nil {
		return coverage.AcceptanceManifest{}, err
	}
	base.Sources = slices.Clone(base.Sources)
	for _, source := range delta.Sources {
		if !slices.Contains(base.Sources, source) {
			base.Sources = append(base.Sources, source)
		}
	}
	base.Edges = nil
	if !change.Replace {
		for _, edge := range previous.Edges {
			if !slices.Contains(change.RemoveCases, edge.CaseID) && !slices.Contains(change.CoveragePoints, edge.PointID) {
				base.Edges = append(base.Edges, edge)
			}
		}
	}
	for _, edge := range delta.Edges {
		if !slices.Contains(base.Edges, edge) {
			base.Edges = append(base.Edges, edge)
		}
	}
	return base, nil
}

func mergeQAItems[T any](original, authored []T, removed []string, key func(T) string) ([]T, error) {
	items := slices.Clone(original)
	seen := map[string]bool{}
	for _, id := range removed {
		if seen[id] || !slices.ContainsFunc(original, func(item T) bool { return key(item) == id }) {
			return nil, fmt.Errorf("unknown or duplicate QA removal: %s", id)
		}
		seen[id] = true
	}
	for _, item := range authored {
		id := key(item)
		if id == "" || seen[id] {
			return nil, fmt.Errorf("duplicate or conflicting QA increment entry: %s", id)
		}
		seen[id] = true
		index := slices.IndexFunc(items, func(old T) bool { return key(old) == id })
		if index >= 0 {
			items[index] = item
		} else {
			items = append(items, item)
		}
	}
	return slices.DeleteFunc(items, func(item T) bool { return slices.Contains(removed, key(item)) }), nil
}

func qaDesignContent(manifest coverage.AcceptanceManifest) string {
	return digestOfCanonical(struct {
		Cases  []coverage.AcceptanceCase
		Shared []coverage.SharedStep
		Edges  []coverage.CoverageEdge
	}{append([]coverage.AcceptanceCase{}, manifest.Cases...), append([]coverage.SharedStep{}, manifest.SharedSteps...), append([]coverage.CoverageEdge{}, manifest.Edges...)})
}
