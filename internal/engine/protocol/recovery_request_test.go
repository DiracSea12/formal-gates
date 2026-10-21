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

func TestUnknownStoppedWorkerFreshPreservesIdentityAndRetryAccounting(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	action := issueReady(t, engine)[0]
	if _, err := engine.Submit(unknownSpawnReceipt(t, "claim", action, "native-directory"), Observation{}); err != nil {
		t.Fatal(err)
	}
	engine.cfg.WorkerExecutionStopped = func(PendingAction, Attempt) (bool, error) { return true, nil }
	plan, _, err := engine.RecoverAttempt(action, Interruption{}, Observation{})
	if err != nil || plan.Action != RecoveryAsk {
		t.Fatal(plan, err)
	}
	decide := func(id EventID, choice AskOptionID) error {
		token, err := engine.Freshness(RequestID(plan.RequestID))
		if err != nil {
			return err
		}
		ev, err := NewDecideEvent(id, RequestID(plan.RequestID), token, choice)
		if err != nil {
			return err
		}
		_, err = engine.Submit(ev, Observation{})
		return err
	}
	if err := decide("resume-without-identity", "resume"); err == nil {
		t.Fatal("resume accepted without original identity")
	}
	before := engine.LoadMustSucceed(t).State
	if before.PendingAsks[plan.RequestID].Resolved {
		t.Fatal("rejected resume consumed original Ask")
	}
	original := before.PendingActions[action]
	if err := decide("fresh-stopped", "fresh"); err != nil {
		t.Fatal(err)
	}
	after := engine.LoadMustSucceed(t).State
	current := after.Attempts[original.Task]
	if current.ActionID == action || current.Attempts != 1 || current.MaxAttempts != 2 {
		t.Fatal("fresh did not replace and account original execution", current)
	}
	if len(after.LifecycleEvents) != 0 || len(after.PendingHostActions) != 0 {
		t.Fatal("fresh manufactured native identity or lifecycle", after.LifecycleEvents, after.PendingHostActions)
	}
	if after.ObsoleteActions[action].ReplacedBy != current.ActionID {
		t.Fatal("original obligation lost", after.ObsoleteActions[action])
	}
}

func TestStoppedNativeEffectSettlesOriginalRecoveryWithoutLifecycle(t *testing.T) {
	for _, choice := range []AskOptionID{"resume", "fresh"} {
		t.Run(string(choice), func(t *testing.T) {
			engine, _, _, _ := preparedEngine(t)
			action := issueReady(t, engine)[0]
			submitSpawnReceipt(t, engine, "original-start", action)
			stopped := false
			engine.cfg.WorkerExecutionStopped = func(PendingAction, Attempt) (bool, error) { return stopped, nil }
			plan, _, err := engine.RecoverAttempt(action, Interruption{}, Observation{})
			if err != nil || plan.Action != RecoveryAsk {
				t.Fatal(plan, err)
			}
			token, err := engine.Freshness(RequestID(plan.RequestID))
			if err != nil {
				t.Fatal(err)
			}
			ev, err := NewDecideEvent("decide-native", RequestID(plan.RequestID), token, choice)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Submit(ev, Observation{}); err != nil {
				t.Fatal(err)
			}
			before := engine.LoadMustSucceed(t)
			var intent HostActionIntent
			for _, v := range before.State.PendingHostActions {
				if v.Correlation == plan.RequestID {
					intent = v
				}
			}
			if intent.ActionID == "" {
				t.Fatal("original native intent not issued")
			}
			waiting, revision, err := engine.ReconcileHostAction(intent.ActionID, "", false, false, Observation{})
			if err != nil || waiting.Action != RecoveryWait || revision != before.Revision {
				t.Fatal("unproven stop changed native intent", waiting, revision, err)
			}
			stopped = true
			reconciled, _, err := engine.ReconcileHostAction(intent.ActionID, "", false, false, Observation{})
			if err != nil || reconciled.Action != RecoveryReconcile {
				t.Fatal(reconciled, err)
			}
			after := engine.LoadMustSucceed(t)
			effect := after.State.ReconciledEffects[intent.ActionID]
			if effect.Intent == nil || effect.Intent.Correlation != plan.RequestID || effect.ObservationDigest == "" || len(after.State.LifecycleEvents) != 0 {
				t.Fatal("settlement lost original evidence or manufactured lifecycle", effect)
			}
			if _, exists := after.State.PendingHostActions[intent.ActionID]; exists {
				t.Fatal("settled intent remains pending")
			}
			if choice == "resume" {
				next, _, err := engine.RecoverAttempt(action, Interruption{}, Observation{})
				if err != nil || next.Action != RecoveryAsk || next.RequestID == plan.RequestID {
					t.Fatal("dead resume still owns recovery", next, err)
				}
			} else {
				if after.State.ObsoleteActions[action].ReplacedBy == "" {
					t.Fatal("dead terminate did not finish original fresh decision")
				}
			}
		})
	}
}

func TestUnknownWorkerAbortWaitsForConfirmedOriginalStop(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	action := issueReady(t, engine)[0]
	if _, err := engine.Submit(unknownSpawnReceipt(t, "unknown-native", action, "native-directory"), Observation{}); err != nil {
		t.Fatal(err)
	}
	stopped := false
	engine.cfg.WorkerExecutionStopped = func(p PendingAction, a Attempt) (bool, error) {
		if p.ActionID != action {
			return false, nil
		}
		if p.AttemptID != a.ID {
			t.Fatal("stop sampled another attempt", p, a)
		}
		return stopped, nil
	}
	plan, _, err := engine.RecoverAttempt(action, Interruption{}, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	token, err := engine.Freshness(RequestID(plan.RequestID))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := NewDecideEvent("abort-original", RequestID(plan.RequestID), token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Submit(ev, Observation{}); err != nil {
		t.Fatal(err)
	}
	state := engine.LoadMustSucceed(t).State
	if _, ok := state.PendingActions[action]; !ok {
		t.Fatal("unproven native stop retired original")
	}
	stopped = true
	if _, _, err := engine.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state = engine.LoadMustSucceed(t).State
	if _, ok := state.PendingActions[action]; ok {
		t.Fatal("confirmed original stop still holds abort")
	}
	if len(state.LifecycleEvents) != 0 {
		t.Fatal("abort manufactured identity lifecycle", state.LifecycleEvents)
	}
	if _, ok := state.ObsoleteActions[action]; !ok {
		t.Fatal("abort lost original worker responsibility")
	}
}

func TestUnknownWorkerResetWaitsForConfirmedOriginalStop(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	action := issueReady(t, engine)[0]
	if _, err := engine.Submit(unknownSpawnReceipt(t, "unknown-reset", action, "native-directory"), Observation{}); err != nil {
		t.Fatal(err)
	}
	stopped := false
	engine.cfg.WorkerExecutionStopped = func(p PendingAction, a Attempt) (bool, error) {
		return p.ActionID == action && p.AttemptID == a.ID && stopped, nil
	}
	request, err := NewRequestEvent("reset-interrupted", ControlReset, AskOption{ID: "proceed", Label: "Reset run"})
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithResetPreview(engine.LoadMustSucceed(t).State.ResetPreview())
	if _, err := engine.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := engine.Freshness(RequestID(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewDecideEvent("confirm-reset-interrupted", RequestID(request.ID), token, "proceed")
	if err != nil {
		t.Fatal(err)
	}
	state := engine.LoadMustSucceed(t).State
	event.Decide.Reset = &ResetDecision{PreviewDigest: state.PendingAsks[string(request.ID)].Reset.Digest, Reason: "Reset interrupted original execution"}
	if _, err := engine.Submit(event, Observation{}); err != nil {
		t.Fatal(err)
	}
	state = engine.LoadMustSucceed(t).State
	if _, ok := state.PendingActions[action]; !ok {
		t.Fatal("reset retired an unproven original stop")
	}
	stopped = true
	if _, _, err := engine.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	state = engine.LoadMustSucceed(t).State
	if _, ok := state.PendingActions[action]; ok {
		t.Fatal("confirmed original stop still holds reset")
	}
	if len(state.LifecycleEvents) != 0 {
		t.Fatal("reset manufactured identity lifecycle")
	}
	if _, ok := state.ObsoleteActions[action]; !ok {
		t.Fatal("reset lost original worker obligation")
	}
}
