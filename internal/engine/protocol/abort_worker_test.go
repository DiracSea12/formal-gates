package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/runtime"
)

// Abort observes the issued worker's native owner before retiring unstarted work
// or requesting termination. Unknown execution remains an unresolved effect.
func TestAbortUsesOriginalWorkerExecutionObservation(t *testing.T) {
	for _, status := range []string{"UNCLAIMED", SpawnStatusUnknown, SpawnStatusSpawned} {
		t.Run(status, func(t *testing.T) {
			e, directory := developmentPreparationEngine(t)
			if _, _, err := e.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			action := pendingActionByStep(t, e, "development.plan")
			original := e.LoadMustSucceed(t).State.PendingActions[action]
			observed := 0
			observe := func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
				if p != original || a.ID != original.AttemptID || a.Task != original.Task {
					t.Fatalf("abort observed a different worker: %+v %+v", p, a)
				}
				observed++
				if status == "UNCLAIMED" {
					return nil, nil
				}
				return &SpawnReceiptPayload{ActionID: action, Provider: testProvider, Correlation: "original-native-owner", Status: status}, nil
			}
			e.cfg.WorkerExecution = observe
			request, err := NewRequestEvent("abort-preparation", ControlAbort, AskOption{ID: "abort", Label: "Abort the issued preparation"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Submit(request, Observation{}); err != nil {
				t.Fatal(err)
			}
			token, err := e.Freshness(RequestID(request.ID))
			if err != nil {
				t.Fatal(err)
			}
			decision, err := NewDecideEvent("confirm-abort-preparation", RequestID(request.ID), token, "abort")
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := e.Submit(decision, Observation{})
			if err != nil {
				t.Fatal(err)
			}
			s := e.LoadMustSucceed(t).State
			if observed == 0 {
				t.Fatal("abort left issued work without consulting its original execution owner")
			}
			switch status {
			case "UNCLAIMED":
				if s.Phase != runtime.PhaseTerminal || len(s.PendingActions) != 0 || len(s.Attempts) != 0 || len(s.PendingHostActions) != 0 || s.ObsoleteActions[action].Reason != "run aborted by "+string(request.ID) {
					t.Fatal("unclaimed preparation prevented abort or changed its retirement authority")
				}
			case SpawnStatusUnknown:
				if s.Phase == runtime.PhaseTerminal || s.PendingActions[action] != original || len(s.PendingHostActions) != 0 || s.SpawnReceipts[action].Status != SpawnStatusUnknown {
					t.Fatal("unknown native execution was guessed or retired")
				}
			case SpawnStatusSpawned:
				if s.Phase == runtime.PhaseTerminal || s.PendingActions[action] != original || len(s.PendingHostActions) != 1 || s.SpawnReceipts[action].Status != SpawnStatusSpawned {
					t.Fatal("started native execution did not retain its original termination duty")
				}
				for _, intent := range s.PendingHostActions {
					if intent.Terminate == nil || intent.Terminate.WorkerActionID != action || intent.Terminate.AttemptID != original.AttemptID || intent.Terminate.Identity != "original-native-owner" || intent.Terminate.Reason != "User aborted the run" {
						t.Fatal("abort changed the original typed termination identity or reason", intent)
					}
				}
			}
			reopened, err := New(mustStore(t, directory), Config{Definition: e.cfg.Definition, Registry: definition.Registry(), Capacity: 4, WorkerExecution: observe})
			if err != nil {
				t.Fatal(err)
			}
			if replay, err := reopened.Submit(decision, Observation{}); err != nil || !reflect.DeepEqual(replay, accepted) {
				t.Fatal("reopened abort did not replay its original decision", replay, err)
			}
			if _, _, err := reopened.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			recovered := reopened.LoadMustSucceed(t).State
			if !reflect.DeepEqual(recovered.PendingActions, s.PendingActions) || !reflect.DeepEqual(recovered.PendingHostActions, s.PendingHostActions) || !reflect.DeepEqual(recovered.Decisions, s.Decisions) || len(recovered.DispatchIssueRevisions) != 1 {
				t.Fatal("reopened abort repeated work or changed its original stop authority")
			}
		})
	}
}
