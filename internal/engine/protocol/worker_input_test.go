package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkerInputPreservesIssuedExecutionContext(t *testing.T) {
	for _, role := range []string{"engine.development.batch.v4", "engine.repair.v4", "engine.validation.observation.v2"} {
		t.Run(role, func(t *testing.T) {
			e, state, pending := unfinishedDevelopmentState(t, role)
			if role == "engine.validation.observation.v2" {
				e, state, pending, _ = adjudicationWorkerState(t)
			}
			want := state.WorkerInputs(e.cfg.Definition)[pending.ActionID]
			input, current := state.WorkerInput(pending.ActionID, e.cfg.Definition)
			if !current || !reflect.DeepEqual(input, want) {
				t.Fatal("selected input differs from complete issuance projection")
			}
			admitted, identity, err := state.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition)
			if err != nil || identity != "" || !reflect.DeepEqual(admitted, want) {
				t.Fatal("execution admission lost original context", err)
			}
			body, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			var reopened State
			if err := json.Unmarshal(body, &reopened); err != nil {
				t.Fatal(err)
			}
			admitted, _, err = reopened.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition)
			if err != nil || !reflect.DeepEqual(admitted, want) {
				t.Fatal("recovery changed admitted input", err)
			}
			if _, _, err := state.WorkerExecutionIdentity(pending.ActionID, "different-attempt", "", e.cfg.Definition); err == nil {
				t.Fatal("different attempt passed original issuance")
			}
			if _, _, err := state.WorkerExecutionIdentity("different-action", pending.AttemptID, "", e.cfg.Definition); err == nil {
				t.Fatal("different action passed original issuance")
			}
			original := state.SpawnReceipts[pending.ActionID]
			unknown := original
			unknown.Status = SpawnStatusUnknown
			state.SpawnReceipts[pending.ActionID] = unknown
			if _, _, err := state.WorkerExecutionIdentity(pending.ActionID, pending.AttemptID, "", e.cfg.Definition); err == nil {
				t.Fatal("UNKNOWN execution was admitted")
			}
			state.SpawnReceipts[pending.ActionID] = original
			state.PendingAsks = map[string]PendingAsk{"change": {Control: ControlRequestRequirementChange}}
			if _, current := state.WorkerInput(pending.ActionID, e.cfg.Definition); current {
				t.Fatal("requirement barrier admitted selected work")
			}
			if len(state.WorkerInputs(e.cfg.Definition)) != 0 {
				t.Fatal("requirement barrier admitted enumerated work")
			}
		})
	}
}
