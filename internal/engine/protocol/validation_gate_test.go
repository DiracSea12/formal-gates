package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// A normal gate consumes the REVIEW_READY range proof for its frozen source
// tasks. A repair materialization can share the native identity with that proof.
func TestGateInputUsesTheFrozenReviewRangeProof(t *testing.T) {
	t.Parallel()
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

func TestMergeGateRangesReprojectOriginalReceiptsAfterFreshDispatch(t *testing.T) {
	t.Parallel()
	task := runtime.TaskKey{Node: "validation", Step: "validation.gate", Scope: "merge"}
	cd := &compiler.CompiledDefinition{Steps: []compiler.CompiledStep{{Header: compiler.CompiledHeader{ID: task.Step, NodeID: task.Node}, Payload: compiler.CompiledAgentStep{Handler: "engine.gate.v3"}}}}
	development := runtime.TaskKey{Node: "validation", Step: "development.worker", Scope: "command"}
	candidate := coverage.ValidationCandidate{Identity: "merged", Digest: "tree"}
	wave := ValidationWave{ID: "wave", Candidate: candidate, SourceTasks: []runtime.TaskKey{development}, Targets: map[string]*ValidationTarget{task.String(): {Task: task, Kind: "GATE", Gate: mergeGateID}}}
	binding := WorkBinding{WaveID: wave.ID, Task: task, Candidate: candidate.Identity}
	state := &State{BaseSnapshot: "master-base", Topology: &TopologyBinding{Decision: "split", Slices: []string{"left", "right"}}, ChildCreation: &ChildCreation{Source: "slice-source"}, SliceChildren: map[string]string{"left": "child-left", "right": "child-right"}, SliceReceipts: map[string]SliceReceipt{}, Validation: &Validation{RequirementRevision: "confirmed", Generation: 1, Candidate: &candidate, Waves: []ValidationWave{wave}}, HostActionReceipts: map[string]HostActionReceipt{}, ReconciledEffects: map[string]ReconciledEffect{}, DispatchBindings: map[string]WorkBinding{"original": binding, "fresh": binding}}
	views := map[string]ChildValidationView{}
	for _, slice := range state.Topology.Slices {
		id := state.SliceChildren[slice]
		state.SliceReceipts[id] = SliceReceipt{Digest: "receipt-" + slice, Receipt: SliceTerminalReceipt{ChildRunID: id, SliceID: slice, Snapshot: "snapshot-" + slice}}
		views[id] = ChildValidationView{BaseSnapshot: state.BaseSnapshot, ReceiptDigest: "receipt-" + slice}
	}
	params := CandidateParameters{Stage: "REVIEW_READY", RequirementRevision: "confirmed", Generation: 1, ReviewBase: state.BaseSnapshot, SourceTasks: wave.SourceTasks}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	intent := HostActionIntent{ActionID: "ready", Revision: 2, PayloadDigest: "candidate-parameters", Adapter: &AdapterHostAction{Operation: "op.candidate", Params: fields}}
	state.ReconciledEffects[intent.ActionID] = ReconciledEffect{Intent: &intent}
	state.HostActionReceipts[intent.ActionID] = HostActionReceipt{ActionID: intent.ActionID, AdapterOperation: "op.candidate", Status: HostActionStatusExecuted, AdapterEvidence: &AdapterEvidence{Values: map[string]any{"identity": candidate.Identity, "digest": candidate.Digest, "parametersDigest": intent.PayloadDigest, "gateView": GateReviewView{TestOwnedPaths: []string{}}, "childViews": views}}}
	oldInput := []byte(`{"gate":{"gate":"merge-gate","baseSnapshot":"master-base","candidate":{"identity":"merged","digest":"tree"},"testOwnedPaths":[]}}`)
	var original ValidationInput
	if err := json.Unmarshal(oldInput, &original); err != nil || len(original.Gate.Slices) != 0 {
		t.Fatal("old issued input fixture changed", err)
	}
	wanted := []SliceComparison{{SliceID: "left", BaseSnapshot: "slice-source", Snapshot: "snapshot-left"}, {SliceID: "right", BaseSnapshot: "slice-source", Snapshot: "snapshot-right"}}
	for range 2 {
		encoded, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var reopened State
		if err := json.Unmarshal(encoded, &reopened); err != nil {
			t.Fatal(err)
		}
		state = &reopened
		fresh := state.validationInput(PendingAction{ActionID: "fresh", Task: task}, cd)
		if fresh.Gate == nil || !reflect.DeepEqual(fresh.Gate.Slices, wanted) || fresh.Binding != binding || len(original.Gate.Slices) != 0 {
			t.Fatalf("fresh input reused immutable old data or another source: %+v", fresh.Gate)
		}
	}
	receipt := state.SliceReceipts["child-left"]
	receipt.Digest = "another-receipt"
	state.SliceReceipts["child-left"] = receipt
	if input := state.gateInput(binding, task); input != nil {
		t.Fatal("another receipt was substituted into the signed candidate range")
	}
}
