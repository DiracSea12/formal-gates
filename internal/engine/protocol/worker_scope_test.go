package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChildWorkerScopePreservesConfirmedResponsibilitiesOnRecovery(t *testing.T) {
	e, state, pending := unfinishedDevelopmentState(t, "engine.development.batch")
	state.SliceChild = &SliceChildBinding{SliceID: "slice-2"}
	state.Topology = &TopologyBinding{Decision: "split", ACOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge", "AC-004": "master-merge"}}
	want := &WorkerScope{Owner: "slice-2", AcceptanceOwners: map[string]string{"AC-001": "slice-1", "AC-002": "slice-2", "AC-003": "master-merge", "AC-004": "master-merge"}}
	input := state.WorkerInputs(e.cfg.Definition)[pending.ActionID]
	if !reflect.DeepEqual(input.Scope, want) || input.RequirementDocument.Content != state.Validation.Document.Content {
		t.Fatalf("child lacks its original responsibility or complete requirement: %+v", input)
	}
	stored, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var recovered State
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, recovered.WorkerInputs(e.cfg.Definition)[pending.ActionID]) {
		t.Fatal("recovery changed the issued responsibility projection")
	}
	input.Scope.AcceptanceOwners["AC-001"] = "slice-2"
	if state.Topology.ACOwners["AC-001"] != "slice-1" || !reflect.DeepEqual(state.WorkerInputs(e.cfg.Definition)[pending.ActionID].Scope, want) {
		t.Fatal("worker scope changed the authoritative topology")
	}
	state.SliceChild = nil
	if state.WorkerInputs(e.cfg.Definition)[pending.ActionID].Scope != nil {
		t.Fatal("merge worker narrowed full-project acceptance to master-owned conditions")
	}
}
