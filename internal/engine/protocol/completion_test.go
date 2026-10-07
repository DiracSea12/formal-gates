package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDevelopmentCompletionRequiresExactIssuedEvidence(t *testing.T) {
	batch := DevelopmentBatch{ID: "owned", DoD: []DevelopmentCriterion{{ID: "behavior", Description: "declared behavior"}}, Checks: []DevelopmentCheck{{ID: "build", Command: "go build ./..."}}}
	for _, name := range []string{"exact", "extra-check", "missing-check", "extra-dod", "missing-dod", "blank-check", "blank-dod", "blank-handoff"} {
		t.Run(name, func(t *testing.T) {
			evidence := &DevelopmentEvidence{Completion: &DevelopmentCompletion{DoD: map[string]string{"behavior": "verified"}, Checks: map[string]string{"build": "passed"}, Handoff: "ready"}}
			switch name {
			case "extra-check":
				evidence.Completion.Checks["SUPPLEMENTAL"] = "passed"
			case "missing-check":
				delete(evidence.Completion.Checks, "build")
			case "extra-dod":
				evidence.Completion.DoD["unissued"] = "verified"
			case "missing-dod":
				delete(evidence.Completion.DoD, "behavior")
			case "blank-check":
				evidence.Completion.Checks["build"] = "  "
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
			if name == "extra-check" && !strings.Contains(err.Error(), "batch completion must cover exactly its confirmed DoD and checks") {
				t.Fatalf("original extra-key refusal changed: %v", err)
			}
			after, _ := json.Marshal(evidence)
			if string(before) != string(after) {
				t.Fatal("core rewrote submitted completion")
			}
		})
	}
}
