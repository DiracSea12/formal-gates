package protocol

import (
	"encoding/json"
	"testing"
)

func TestDevelopmentCompletionRequiresExactIssuedEvidence(t *testing.T) {
	batch := DevelopmentBatch{ID: "owned", DoD: []DevelopmentCriterion{{ID: "behavior", Description: "declared behavior"}}, Checks: []DevelopmentCheck{{ID: "build", Command: "go build ./..."}}}
	for _, name := range []string{"exact", "extra-dod", "missing-dod", "blank-dod", "blank-handoff"} {
		t.Run(name, func(t *testing.T) {
			evidence := &DevelopmentEvidence{Completion: &DevelopmentCompletion{DoD: map[string]string{"behavior": "verified"}, Handoff: "ready"}}
			switch name {
			case "extra-dod":
				evidence.Completion.DoD["unissued"] = "verified"
			case "missing-dod":
				delete(evidence.Completion.DoD, "behavior")
			case "blank-dod":
				evidence.Completion.DoD["behavior"] = "  "
			case "blank-handoff":
				evidence.Completion.Handoff = "  "
			}
			before, _ := json.Marshal(evidence)
			err := validateDevelopmentCompletion(batch, evidence)
			if (name == "exact") != (err == nil) {
				t.Fatalf("completion %s: %v", name, err)
			}
			after, _ := json.Marshal(evidence)
			if string(before) != string(after) {
				t.Fatal("core rewrote submitted completion")
			}
		})
	}
}
