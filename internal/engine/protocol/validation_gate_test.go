package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// A normal gate consumes the REVIEW_READY range proof for its frozen source
// tasks. A repair materialization can share the native identity with that proof.
func TestGateInputUsesTheFrozenReviewRangeProof(t *testing.T) {
	development := runtime.TaskKey{Node: "validation", Step: "development.worker", Scope: "command"}
	gate := runtime.TaskKey{Node: "validation", Step: "validation.gate", Scope: "quality"}
	candidate := coverage.ValidationCandidate{Identity: "candidate", Digest: "tree"}
	wave := ValidationWave{ID: "wave", Candidate: candidate, SourceTasks: []runtime.TaskKey{development}, Targets: map[string]*ValidationTarget{gate.String(): {Task: gate, Kind: "GATE", Gate: "quality"}}}
	binding := WorkBinding{WaveID: wave.ID, Task: gate, Candidate: candidate.Identity}
	for _, variant := range []string{"repair-stage", "different-source", "different-range", "different-payload", "unconfirmed-revision", "uncarried-generation", "carried-generation"} {
		t.Run(variant, func(t *testing.T) {
			state := &State{BaseSnapshot: "base", Validation: &Validation{RequirementRevision: "confirmed", Generation: 2, Waves: []ValidationWave{wave}}, HostActionReceipts: map[string]HostActionReceipt{}, ReconciledEffects: map[string]ReconciledEffect{}}
			add := func(id string, p CandidateParameters, owned []string, revision uint64) {
				t.Helper()
				body, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(body, &fields); err != nil {
					t.Fatal(err)
				}
				intent := HostActionIntent{ActionID: id, Revision: revision, PayloadDigest: "payload:" + id, Adapter: &AdapterHostAction{Operation: "op.candidate", Params: fields}}
				state.ReconciledEffects[id] = ReconciledEffect{Intent: &intent}
				state.HostActionReceipts[id] = HostActionReceipt{ActionID: id, AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, PayloadDigest: intent.PayloadDigest, AdapterEvidence: &AdapterEvidence{Values: map[string]any{"identity": candidate.Identity, "digest": candidate.Digest, "parametersDigest": intent.PayloadDigest, "gateView": GateReviewView{TestOwnedPaths: owned}}}}
			}
			params := CandidateParameters{Stage: "REVIEW_READY", RequirementRevision: "confirmed", Generation: 2, ReviewBase: "base", SourceTasks: []runtime.TaskKey{development}}
			owned := []string{"merge-cases.md"}
			if variant == "carried-generation" {
				params.RequirementRevision, params.Generation, params.RouteScope = "original", 1, "original-route"
				state.RequirementRevisions = []ConfirmedRevision{{Revision: "confirmed", StateRevision: 2, WorkDecisions: []RequirementWorkDecision{{Work: RequirementWork{Task: development, RequirementRevision: "original"}, Disposition: RequirementWorkCarry}}}}
			}
			add("review-range", params, owned, 3)
			switch variant {
			case "repair-stage":
				params.Stage, params.RepairTask = "DEVELOPMENT", &development
			case "different-source":
				params.SourceTasks = []runtime.TaskKey{runtime.TaskKey{Node: "validation", Step: "development.worker", Scope: "other"}}
			case "different-range":
				params.ReviewBase = "other-base"
			case "unconfirmed-revision", "carried-generation":
				params.RequirementRevision, params.Generation = "unconfirmed", 2
			case "uncarried-generation":
				params.RequirementRevision, params.Generation = "original", 1
			}
			add("other-effect", params, []string{}, 4)
			if variant == "different-payload" {
				receipt := state.HostActionReceipts["other-effect"]
				receipt.AdapterEvidence.Values["parametersDigest"] = "other-parameters"
				state.HostActionReceipts["other-effect"] = receipt
			}
			for range 64 {
				input := state.gateInput(binding, gate)
				if input == nil || input.Candidate != candidate || input.BaseSnapshot != "base" || !reflect.DeepEqual(input.TestOwnedPaths, owned) {
					t.Fatalf("same native identity selected another effect's ownership: %+v", input)
				}
				body, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				var reopened State
				if err := json.Unmarshal(body, &reopened); err != nil {
					t.Fatal(err)
				}
				state = &reopened
			}
		})
	}
}
