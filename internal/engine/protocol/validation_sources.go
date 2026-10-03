package protocol

import (
	"fmt"

	"formal-gates/internal/engine/coverage"
)

// CoverageInventory registers only confirmed solution obligations and links.
// Product REQ/AC facts are always derived from the confirmed requirement text.
type CoverageInventory struct {
	Sources      []coverage.RequiredSource          `json:"sources,omitempty"`
	Points       []coverage.AcceptancePoint         `json:"points,omitempty"`
	Links        []SourcePoint                      `json:"links,omitempty"`
	Alternatives []coverage.AlternativeVerification `json:"alternatives,omitempty"`
}
type SourcePoint struct {
	SourceID string `json:"sourceId"`
	PointID  string `json:"pointId"`
}

func (s *State) confirmedCoverage() *CoverageInventory {
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		if v := s.RequirementRevisions[i].Coverage; v != nil {
			return v
		}
	}
	if s.IntakeConfirmationReceipt != nil {
		return s.IntakeConfirmationReceipt.Coverage
	}
	return nil
}
func (s *State) coveragePoints() ([]coverage.AcceptancePoint, []SourcePoint) {
	points := []coverage.AcceptancePoint{}
	links := []SourcePoint{}
	for _, req := range s.Validation.Document.Requirements {
		for _, ac := range req.AcceptanceConditions {
			points = append(points, coverage.AcceptancePoint{PointID: ac.ID, ObservableBehavior: ac.Text, Oracle: ac.Text})
			links = append(links, SourcePoint{req.ID, ac.ID})
		}
	}
	if inventory := s.confirmedCoverage(); inventory != nil {
		points = append(points, inventory.Points...)
		links = append(links, inventory.Links...)
	}
	return points, links
}
func (s *State) validateConfirmedCoverage() error {
	if inventory := s.confirmedCoverage(); inventory != nil {
		for _, source := range inventory.Sources {
			if source.Category != coverage.SolutionConstraint {
				return validationError("extra source inventory cannot replace product REQ facts")
			}
		}
	}
	c := s.coverageContract(nil)
	if err := coverage.ValidateRequiredSources(c.RequiredSources); err != nil {
		return validationReject(err)
	}
	points, links := s.coveragePoints()
	seen := map[string]bool{}
	sources := map[string]bool{}
	for _, src := range c.RequiredSources.Sources {
		sources[src.SourceID] = src.Applicability == coverage.QAApplicability
	}
	for _, p := range points {
		if seen[p.PointID] || p.PointID == "" || p.ObservableBehavior == "" || p.Oracle == "" {
			return validationError("confirmed point is duplicate or incomplete")
		}
		seen[p.PointID] = true
	}
	linked := map[string]bool{}
	covered := map[string]bool{}
	for _, link := range links {
		key := link.SourceID + "/" + link.PointID
		if linked[key] || !seen[link.PointID] || !sources[link.SourceID] {
			return validationError("confirmed source-point link is duplicate, unknown or non-QA")
		}
		linked[key] = true
		covered[link.PointID] = true
	}
	for id := range seen {
		if !covered[id] {
			return validationError("confirmed point has no source: " + id)
		}
	}
	return nil
}

// validateCoverageRelations checks the selected modes' union against the
// confirmed obligations; each mode may provide only part of that coverage.
func (s *State) validateCoverageRelations() error {
	if s.Validation == nil || s.routeHasNoQAModes() {
		return nil
	}
	covered := map[SourcePoint]bool{}
	for _, manifest := range s.coverageContract(nil).Manifests {
		for _, edge := range manifest.Edges {
			covered[SourcePoint{edge.SourceID, edge.PointID}] = true
		}
	}
	_, links := s.coveragePoints()
	for _, link := range links {
		if !covered[link] {
			return fmt.Errorf("confirmed relation %s/%s has no case in selected manifests", link.SourceID, link.PointID)
		}
	}
	return nil
}
