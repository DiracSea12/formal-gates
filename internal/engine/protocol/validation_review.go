package protocol

import (
	"formal-gates/internal/engine/coverage"
	"slices"
)

// caseReviewIdentity includes every referenced shared instruction and mapping.
// An unchanged case body alone cannot carry approval across a shared change.
func caseReviewIdentity(m coverage.AcceptanceManifest, id string) string {
	var item *coverage.AcceptanceCase
	for i := range m.Cases {
		if m.Cases[i].CaseID == id {
			item = &m.Cases[i]
		}
	}
	if item == nil {
		return ""
	}
	steps := []coverage.SharedStep{}
	for _, step := range m.SharedSteps {
		if slices.Contains(item.SharedSteps, step.ID) {
			steps = append(steps, step)
		}
	}
	edges := []coverage.CoverageEdge{}
	for _, edge := range m.Edges {
		if edge.CaseID == id {
			edges = append(edges, edge)
		}
	}
	return digestOfCanonical(struct {
		Case  coverage.AcceptanceCase
		Steps []coverage.SharedStep
		Edges []coverage.CoverageEdge
	}{*item, steps, edges})
}
func inheritableCases(set *CaseSet) []string {
	if len(set.ReviewHistory) == 0 {
		return nil
	}
	prior := set.ReviewHistory[len(set.ReviewHistory)-1]
	ids := []string{}
	for _, d := range prior.Review.CaseDecisions {
		if !slices.Contains(set.AffectedCases, d.ID) && d.Status == coverage.StatusPass && caseReviewIdentity(prior.Manifest, d.ID) == caseReviewIdentity(set.Manifest, d.ID) && caseReviewIdentity(set.Manifest, d.ID) != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids
}
func validateReviewInheritance(set *CaseSet, result coverage.ReviewResult) error {
	allowed := inheritableCases(set)
	for _, d := range result.CaseDecisions {
		if d.Inherited && (d.Status != coverage.StatusPass || !slices.Contains(allowed, d.ID)) {
			return validationError("case approval cannot be inherited after an affected change: " + d.ID)
		}
	}
	// Aggregate and shared-step decisions remain explicit for this manifest.
	for _, list := range [][]coverage.ReviewDecision{result.SourceDecisions, result.PointDecisions, result.SharedStepDecisions} {
		for _, d := range list {
			if d.Inherited {
				return validationError("aggregate and shared-step decisions require current review")
			}
		}
	}
	return nil
}
