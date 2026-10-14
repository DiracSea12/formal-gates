package requirement

import (
	"fmt"
)

// Point retains the parent requirement alongside its finite acceptance duty.
type Point struct {
	ID            string `json:"id"`
	RequirementID string `json:"requirementId,omitempty"`
	Requirement   string `json:"requirement,omitempty"`
	Source        string `json:"source,omitempty"`
	Text          string `json:"text"`
	Oracle        string `json:"oracle"`
}

type SourcePoint struct {
	SourceID string `json:"sourceId"`
	PointID  string `json:"pointId"`
}

type PointResult struct {
	PointID  string `json:"pointId"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

func ProjectPoints(document FrozenRequirementProjection) ([]Point, []SourcePoint) {
	points, links := []Point{}, []SourcePoint{}
	for _, req := range document.Requirements {
		for _, ac := range req.AcceptanceConditions {
			points = append(points, Point{ID: ac.ID, RequirementID: req.ID, Requirement: req.Requirement, Source: req.Source, Text: ac.Text, Oracle: ac.Text})
			links = append(links, SourcePoint{SourceID: req.ID, PointID: ac.ID})
		}
	}
	return points, links
}

func PointIDs(points []Point) []string {
	ids := make([]string, len(points))
	for i, point := range points {
		ids[i] = point.ID
	}
	return ids
}

// SelectPoints preserves the confirmed order for every consumer's binding.
func SelectPoints(points []Point, ids []string) []Point {
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	result := []Point{}
	for _, point := range points {
		if selected[point.ID] {
			result = append(result, point)
		}
	}
	return result
}

// PointResultError identifies a finite-set mismatch without assigning a role's policy.
type PointResultError struct {
	Kind    string
	Index   int
	PointID string
}

func (e *PointResultError) Error() string {
	if e.Kind == "INCOMPLETE" {
		return "point results must cover exactly the issued requirement points"
	}
	return fmt.Sprintf("requirement point result %s: %s", e.Kind, e.PointID)
}

func ValidatePointResults(expected []string, results []PointResult) error {
	if len(expected) != len(results) {
		return &PointResultError{Kind: "INCOMPLETE", Index: -1}
	}
	known := make(map[string]bool, len(expected))
	for _, id := range expected {
		known[id] = true
	}
	seen := make(map[string]bool, len(results))
	for i, result := range results {
		if !known[result.PointID] {
			return &PointResultError{Kind: "UNKNOWN", Index: i, PointID: result.PointID}
		}
		if seen[result.PointID] {
			return &PointResultError{Kind: "DUPLICATE", Index: i, PointID: result.PointID}
		}
		if result.Status != "PASS" && result.Status != "FAIL" {
			return &PointResultError{Kind: "STATUS", Index: i, PointID: result.PointID}
		}
		seen[result.PointID] = true
	}
	return nil
}
