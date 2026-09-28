package protocol

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
)

// Candidate receipts must match the issued case content even when the current
// manifest has acquired a newly confirmed ordinary-gate route binding.
func TestCandidateReceiptRequiresIssuedCaseContent(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "route context only"
		if changed {
			name = "case content changed"
		}
		t.Run(name, func(t *testing.T) {
			manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox, RouteScope: "original-route"}, Cases: []coverage.AcceptanceCase{{CaseID: "valid", Mode: coverage.CaseWhitebox, TestRef: "app_test.go::TestValid", Oracle: "valid input accepted"}}}
			params := CandidateParameters{Stage: "QA_ARTIFACT", RequirementRevision: "confirmed", Generation: 1, RouteScope: "original-route", CaseVersions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: digestOfCanonical(manifest)}, Paths: []string{"app_test.go"}}
			manifest.Binding.RouteScope = "added-ordinary-gate"
			if changed {
				manifest.Cases[0].TestRef = "app_test.go::TestDifferentBehavior"
			}
			s := &State{Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewWhitebox: {Manifest: manifest, Version: digestOfCanonical(manifest)}}}}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			fields := map[string]any{}
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			intent := HostActionIntent{PayloadDigest: "issued-parameters", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: fields}}
			receipt := HostActionReceipt{AdapterEvidence: &AdapterEvidence{Values: map[string]any{"parametersDigest": "issued-parameters", "identity": "candidate-with-tests", "digest": "candidate-content", "paths": []string{"app_test.go"}, "testOnly": true, "productionViewEqual": true}}}
			engine := &Engine{}
			err = engine.applyCandidateReceipt(s, intent, receipt)
			if changed {
				if err == nil || s.Validation.Sets[coverage.ReviewWhitebox].Candidate != "" {
					t.Fatal("changed case content accepted an old artifact receipt")
				}
			} else if err != nil || s.Validation.Sets[coverage.ReviewWhitebox].Candidate != "candidate-with-tests" {
				t.Fatalf("route-only projection rejected exact artifact: %v", err)
			}
		})
	}
}
