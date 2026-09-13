package protocol

import (
	"reflect"
	"testing"
)

func TestVerificationDeltaRetainsAndRejectsConflicts(t *testing.T) {
	old := VerificationResult{ActionID: "original-action", Identity: "original-native", Cases: []CaseExecution{{CaseID: "a", Status: "PASS", Output: "original"}, {CaseID: "b", Status: "FAIL", Output: "failure"}}, Findings: []ReviewFinding{{ID: "b-finding", Requirement: "b", Problem: "original failure"}}}
	next := VerificationResult{Cases: []CaseExecution{{CaseID: "c", Status: "PASS", Output: "new"}}, Remaining: []string{"d"}}
	merged, err := mergeVerificationDelta(old, next)
	if err != nil || len(merged.Cases) != 3 || !reflect.DeepEqual(merged.Cases[:2], old.Cases) || !reflect.DeepEqual(merged.Findings, old.Findings) {
		t.Fatal(merged, err)
	}
	for _, bad := range []VerificationResult{
		{Cases: []CaseExecution{{CaseID: "c"}, {CaseID: "c"}}},
		{Cases: []CaseExecution{{CaseID: "a", Status: "PASS", Output: "rewritten"}}},
		{Findings: []ReviewFinding{{ID: "b-finding", Problem: "rewritten"}}},
	} {
		if _, err := mergeVerificationDelta(old, bad); err == nil {
			t.Fatal("conflict admitted", bad)
		}
	}
	// Only an explicit invalidation removes an old row; validation of the exact
	// changed tool/environment and remaining set stays with the existing writer.
	changed, err := mergeVerificationDelta(old, VerificationResult{Invalidated: []CheckpointInvalidation{{CaseID: "b"}}, Remaining: []string{"b"}})
	if err != nil || len(changed.Cases) != 1 || len(changed.Findings) != 0 {
		t.Fatal(changed, err)
	}
}
