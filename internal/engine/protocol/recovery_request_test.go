package protocol

import (
	"encoding/json"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

// The persisted failure is a normal admission fact, independent of when Drive
// first binds its recovery request. Multiple observations retain one duty.
func TestRecoveryAskBindsPersistedWorkerFailure(t *testing.T) {
	t.Parallel()
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "terminal"}[terminal], func(t *testing.T) {
			task := runtime.TaskKey{Node: "review", Step: "review.worker"}
			pending := PendingAction{ActionID: "review-action", Task: task, AttemptID: "review-attempt"}
			failure := RecoveryRecord{ActionID: pending.ActionID, Task: task, AttemptID: pending.AttemptID, Class: "USER_ACTION_REQUIRED", Action: RecoveryAsk, InterruptionReason: "native configuration requires attention", Revision: 8}
			s := State{PendingActions: map[string]PendingAction{pending.ActionID: pending}, Attempts: map[runtime.TaskKey]Attempt{task: {IssuedAction: decision.IssuedAction{ActionID: pending.ActionID, Task: task}, ID: pending.AttemptID, Business: true, MaxAttempts: 2}}, PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}, RecoveryRecords: []RecoveryRecord{failure}}
			later := failure
			later.Revision = 9
			s.RecoveryRecords = append(s.RecoveryRecords, later)
			s.Phase = runtime.PhaseProductReview
			if terminal {
				s.Phase = runtime.PhaseTerminal
				s.PendingActions = map[string]PendingAction{}
				s.Attempts = map[runtime.TaskKey]Attempt{}
			}
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var reopened State
			if err := json.Unmarshal(raw, &reopened); err != nil {
				t.Fatal(err)
			}
			e := Engine{cfg: Config{Definition: compiledCanonicalWorkflow(t)}}
			if err := e.advanceRecoveries(&reopened, 10); err != nil {
				t.Fatal(err)
			}
			if terminal {
				if len(reopened.PendingAsks) != 0 || reopened.RecoveryRecords[0].RequestID != "" {
					t.Fatal("terminal worker failure reopened recovery")
				}
				return
			}
			if len(reopened.PendingAsks) != 1 || reopened.RecoveryRecords[0].RequestID == "" || reopened.RecoveryRecords[0].RequestID != reopened.RecoveryRecords[1].RequestID {
				t.Fatal("persisted observations opened missing or parallel requests")
			}
			recorded := reopened.RecoveryRecords[0]
			recorded.RequestID = ""
			if !reflect.DeepEqual(recorded, failure) {
				t.Fatal("request binding rewrote original failure")
			}
			before, err := json.Marshal(reopened)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.advanceRecoveries(&reopened, 11); err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(reopened)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("repeated Drive changed an undecided recovery")
			}
		})
	}
}
