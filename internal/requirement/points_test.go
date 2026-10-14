package requirement

import (
	"reflect"
	"testing"
)

func TestPointsRetainConfirmedParentMeaningAndOrder(t *testing.T) {
	document := FrozenRequirementProjection{Requirements: []FrozenRequirement{{ID: "REQ-001", Requirement: "Preserve the original behavior", Source: "confirmed user request", AcceptanceConditions: []FrozenAcceptanceCondition{{ID: "AC-001", Text: "First behavior"}, {ID: "AC-002", Text: "Second behavior"}}}}}
	points, links := ProjectPoints(document)
	if len(points) != 2 || points[0].Requirement != document.Requirements[0].Requirement || points[0].Source != document.Requirements[0].Source || points[1].Text != "Second behavior" || points[1].RequirementID != "REQ-001" || links[1] != (SourcePoint{SourceID: "REQ-001", PointID: "AC-002"}) {
		t.Fatalf("lost confirmed requirement semantics: %+v %+v", points, links)
	}
	if got := SelectPoints(points, []string{"AC-002", "AC-001"}); !reflect.DeepEqual(got, points) {
		t.Fatalf("selection changed confirmation order: %+v", got)
	}
}

func TestPointResultsRequireExactIndependentEvidence(t *testing.T) {
	valid := []PointResult{{PointID: "one", Status: "PASS", Evidence: "original observation"}, {PointID: "two", Status: "FAIL", Evidence: "observed mismatch"}}
	if err := ValidatePointResults([]string{"one", "two"}, valid); err != nil {
		t.Fatal(err)
	}
	for _, rows := range [][]PointResult{valid[:1], {valid[0], valid[0]}, {{PointID: "unknown", Status: "PASS", Evidence: "observation"}, valid[1]}, {{PointID: "one", Status: "PENDING", Evidence: "observation"}, valid[1]}} {
		if ValidatePointResults([]string{"one", "two"}, rows) == nil {
			t.Fatalf("accepted incomplete point evidence: %+v", rows)
		}
	}
}
