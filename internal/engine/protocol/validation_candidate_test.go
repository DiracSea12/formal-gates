package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// A carried artifact needs a complete original version witness and unchanged
// source-to-case content. Independent QA progress does not change its inputs.
func TestCarriedCandidateReceiptVerifiesFullManifest(t *testing.T) {
	for _, change := range []string{"route", "requirement", "source", "point", "edge", "test-reference", "missing-witness"} {
		t.Run(change, func(t *testing.T) {
			original := coverage.AcceptanceManifest{
				Binding: coverage.ManifestBinding{SourceBinding: coverage.SourceBinding{RunID: "run", RequirementRevision: "signed-requirement", SolutionRevision: "signed-solution"}, ReviewKind: coverage.ReviewWhitebox, RouteScope: "signed-route", TopologyScope: "signed-topology"},
				Sources: []string{"requirement"},
				Points:  []coverage.AcceptancePoint{{PointID: "behavior", ObservableBehavior: "valid input is accepted", Oracle: "accepted"}},
				Cases:   []coverage.AcceptanceCase{{CaseID: "valid", Mode: coverage.CaseWhitebox, TestRef: "app_test.go::TestValid", Oracle: "accepted"}},
				Edges:   []coverage.CoverageEdge{{SourceID: "requirement", PointID: "behavior", CaseID: "valid"}},
			}
			development := runtime.TaskKey{Node: "validation", Step: "development.worker", Scope: "plan:command"}
			design := runtime.TaskKey{Node: "validation", Step: "qa.design", Scope: "1:WHITEBOX"}
			binding := WorkBinding{Task: design, RequirementRevision: "signed-requirement", Generation: 1, RouteScope: "signed-route", Candidate: "development"}
			params := CandidateParameters{
				Stage: "QA_ARTIFACT", Base: "development", RequirementRevision: "signed-requirement", Generation: 1, RouteScope: "signed-route",
				Paths: []string{"app_test.go"}, SourceTasks: []runtime.TaskKey{development},
				DesignTasks:  map[coverage.ReviewKind]runtime.TaskKey{coverage.ReviewWhitebox: design},
				CaseVersions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: digestOfCanonical(original)},
			}
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var current coverage.AcceptanceManifest
			if err := json.Unmarshal(data, &current); err != nil {
				t.Fatal(err)
			}
			current.Binding.RouteScope = "added-gate-route"
			generation := uint64(1)
			revision := "signed-requirement"
			if change != "route" {
				generation, revision = 2, "confirmed-requirement"
				current.Binding.RequirementRevision = revision
			}
			switch change {
			case "source":
				current.Sources[0] = "different-requirement"
			case "point":
				current.Points[0].Oracle = "different-outcome"
			case "edge":
				current.Edges[0].PointID = "different-behavior"
			case "test-reference":
				current.Cases[0].TestRef = "app_test.go::TestDifferent"
			}
			result := &ValidationResult{Binding: binding, Status: "PASS", Manifest: &original}
			state := &State{
				Validation: &Validation{
					RequirementRevision: revision, Generation: generation, DevelopmentSnapshot: "development",
					Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewWhitebox: {DesignTask: design, Status: "REVIEW", Manifest: current, Version: digestOfCanonical(current)}},
				},
				DispatchBindings: map[string]WorkBinding{"design": binding},
				Results:          map[string]WorkerResult{"design": {ActionID: "design", Outcome: OutcomePass, Digest: digestOfCanonical(result), Validation: result}},
				RequirementRevisions: []ConfirmedRevision{{Revision: revision, WorkDecisions: []RequirementWorkDecision{
					{Work: RequirementWork{ActionID: "development", Task: development, IssuedRevision: 1, RequirementRevision: "signed-requirement"}, Disposition: RequirementWorkCarry, Reason: "retain unchanged development"},
					{Work: RequirementWork{ActionID: "design", Task: design, IssuedRevision: 2, RequirementRevision: "signed-requirement"}, Disposition: RequirementWorkCarry, Reason: "retain unchanged design"},
				}}},
			}
			if change == "missing-witness" {
				state.Results = nil
			}
			raw, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			intent := HostActionIntent{PayloadDigest: "signed-parameters", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: fields}}
			receipt := HostActionReceipt{AdapterEvidence: &AdapterEvidence{Values: map[string]any{"parametersDigest": "signed-parameters", "identity": "qa-artifact", "digest": "artifact-content", "paths": params.Paths, "testOnly": true, "productionViewEqual": true}}}
			engine := &Engine{}
			if err := engine.applyCandidateReceipt(state, intent, receipt); err != nil {
				t.Fatal(err)
			}
			want := ""
			if change == "route" || change == "requirement" {
				want = "qa-artifact"
			}
			if got := state.Validation.Sets[coverage.ReviewWhitebox].Candidate; got != want {
				t.Fatalf("artifact projection = %q, want %q", got, want)
			}
			if !reflect.DeepEqual(state.Validation.Sets[coverage.ReviewWhitebox].Manifest, current) || change != "missing-witness" && !reflect.DeepEqual(*state.Results["design"].Validation.Manifest, original) {
				t.Fatal("receipt changed its original or current manifest evidence")
			}
		})
	}
}

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
			_, got, err := validateCandidateReceipt("git", intent, HostActionReceipt{AdapterEvidence: &AdapterEvidence{Values: values}})
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

func TestCandidateFilesRequireOriginalManifestRelations(t *testing.T) {
	design := runtime.TaskKey{Node: "validation", Step: "qa.design", Scope: "1:WHITEBOX"}
	manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: coverage.ReviewWhitebox}, Cases: []coverage.AcceptanceCase{{CaseID: "case", TestRef: "test_app.py::valid", Files: []string{"assertion.data"}, SharedSteps: []string{"setup"}}}, SharedSteps: []coverage.SharedStep{{ID: "setup", Files: []string{"shared.data"}}, {ID: "unreferenced", Files: []string{"unrelated.data"}}}}
	set := &CaseSet{DesignTask: design, Manifest: manifest}
	s := &State{Validation: &Validation{Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{coverage.ReviewWhitebox: set}}}
	p := CandidateParameters{Generation: 1, DesignTasks: map[coverage.ReviewKind]runtime.TaskKey{coverage.ReviewWhitebox: design}, CaseVersions: map[coverage.ReviewKind]string{coverage.ReviewWhitebox: digestOfCanonical(manifest)}, TestFiles: []string{"assertion.data", "shared.data"}}
	if !s.candidateManifestCurrent(p, coverage.ReviewWhitebox) {
		t.Fatal("exact original file relation rejected")
	}
	p.TestFiles = append(p.TestFiles, "unrelated.data")
	if s.candidateManifestCurrent(p, coverage.ReviewWhitebox) {
		t.Fatal("unrelated shared material gained signed scope")
	}
	p.TestFiles = []string{"assertion.data", "shared.data"}
	set.Manifest.SharedSteps[0].Files = []string{"changed.data"}
	if s.candidateManifestCurrent(p, coverage.ReviewWhitebox) {
		t.Fatal("changed file relation reused original candidate identity")
	}
}
