package protocol

import (
	"encoding/json"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// A completed artifact can project across route context changes; superseded
// case inputs settle without promoting the old artifact.
func TestCandidateReceiptRequiresIssuedCaseContent(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "route context only"
		if changed {
			name = "case content changed"
		}
		t.Run(name, func(t *testing.T) {
			manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox, RouteScope: "original-route"}, Cases: []coverage.AcceptanceCase{{CaseID: "valid", Mode: coverage.CaseWhitebox, TestRef: "app_test.go::TestValid", Oracle: "valid input accepted"}}}
			design := runtime.TaskKey{Node: "validation", Step: "qa.design", Scope: "design"}
			params := CandidateParameters{DesignTasks: map[coverage.ReviewKind]runtime.TaskKey{coverage.ReviewWhitebox: design}, Stage: "QA_ARTIFACT", RequirementRevision: "confirmed", Generation: 1, RouteScope: "original-route", CaseVersions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: digestOfCanonical(manifest)}, Paths: []string{"app_test.go"}}
			manifest.Binding.RouteScope = "added-ordinary-gate"
			if changed {
				manifest.Cases[0].TestRef = "app_test.go::TestDifferentBehavior"
			}
			s := &State{Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewWhitebox: {DesignTask: design, Status: "REVIEW", Manifest: manifest, Version: digestOfCanonical(manifest)}}}}
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
				if err != nil || s.Validation.Sets[coverage.ReviewWhitebox].Candidate != "" {
					t.Fatal("changed case content accepted an old artifact receipt")
				}
			} else if err != nil || s.Validation.Sets[coverage.ReviewWhitebox].Candidate != "candidate-with-tests" {
				t.Fatalf("route-only projection rejected exact artifact: %v", err)
			}
		})
	}
}

// View validation binds proof to the signed target without guessing dependency
// completeness from names or the number of inputs.
func TestCandidateReviewViewsRequireSignedTargets(t *testing.T) {
	for _, variant := range []string{"single-input", "unknown-case", "wrong-test", "empty-view", "empty-path", "empty-content"} {
		t.Run(variant, func(t *testing.T) {
			params := CandidateParameters{Stage: "QA_ARTIFACT", Paths: []string{"test.js"}, ReviewCases: map[string]string{"negative": "test.js#reject"}}
			raw, _ := json.Marshal(params)
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			intent := HostActionIntent{PayloadDigest: "signed-input", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: fields}}
			view := CaseReviewView{TestRef: "test.js#reject", Inputs: map[string]string{"test.js": "sha256:test-contents"}}
			id := "negative"
			switch variant {
			case "unknown-case":
				id = "not-issued"
			case "wrong-test":
				view.TestRef = "test.js#other"
			case "empty-view":
				view.Inputs = nil
			case "empty-path":
				view.Inputs = map[string]string{" ": "content"}
			case "empty-content":
				view.Inputs = map[string]string{"test.js": " "}
			}
			evidence := CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: "native-tree", Digest: "sha256:tree", Paths: params.Paths, TestOnly: true, ProductionViewEqual: true, ReviewViews: map[string]CaseReviewView{id: view}}
			raw, _ = json.Marshal(evidence)
			var values map[string]any
			json.Unmarshal(raw, &values)
			_, got, err := validateCandidateReceipt(intent, HostActionReceipt{AdapterEvidence: &AdapterEvidence{Values: values}})
			if variant == "single-input" {
				if err != nil || len(got.ReviewViews[id].Inputs) != 1 {
					t.Fatalf("complete single-input view rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("unbound or empty review proof accepted")
			}
		})
	}
}
