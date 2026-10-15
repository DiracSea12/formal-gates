package facade

import (
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

// Every revoking control retains the original resumed execution until that
// invocation's own stop arrives; a stop from its previous call is insufficient.
func TestControlWaitsForCurrentResumedExecution(t *testing.T) {
	for _, control := range []string{"reset", "requirement-redo", "node-continuation"} {
		t.Run(control, func(t *testing.T) {
			d := newValidationDriver(t, "quality")
			action, _ := d.action("engine.development.batch.v6")
			d.receipt(action)
			for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
				event, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, edge)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(event)
			}
			failure, err := protocol.NewWorkerResultEvent(d.eventID(), action, d.host(), protocol.OutcomeRuntimeError, "observed-original-native-ended", "USER_ACTION_REQUIRED")
			if err != nil {
				t.Fatal(err)
			}
			failure.Result.InterruptionReason = "The original native invocation ended before completing its semantic task"
			d.submit(failure)
			confirmation, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRecovery), Choice: "resume", Reason: "Continue the original native session"})
			if err != nil {
				t.Fatal(err)
			}
			_, d.run, err = submitControlWithHost(t, d.f, confirmation)
			if err != nil {
				t.Fatal(err)
			}
			var resume protocol.HostActionIntent
			for _, intent := range d.run.AdapterIntents {
				if intent.Resume != nil && intent.Resume.WorkerActionID == action {
					resume = intent
				}
			}
			if resume.Resume == nil {
				t.Fatal("the recovery decision did not sign its original resume")
			}
			start, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), resume.ActionID, action, protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(start)
			receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), resume.ActionID, resume.Operation, d.host(), resume.Correlation, resume.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: action, Event: protocol.LifecycleStart})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(receipt)
			switch control {
			case "reset":
				resetControl(t, d)
			case "requirement-redo":
				confirmValidationWork(d, action)
			case "node-continuation":
				continueBusinessNode(d, "development:command")
			}
			state := driverState(t, d)
			if state.PendingActions[action].AttemptID != resume.Resume.AttemptID {
				t.Fatal("control retired the current resume using the previous invocation's stop")
			}
			var termination protocol.HostActionIntent
			for _, intent := range state.PendingHostActions {
				if intent.Terminate != nil && intent.Terminate.WorkerActionID == action {
					termination = intent
				}
			}
			if termination.Terminate == nil || termination.Terminate.AttemptID != resume.Resume.AttemptID || termination.Terminate.Identity != resume.Resume.Identity {
				t.Fatal("control changed its original termination target", termination)
			}
			d.reopen()
			d.drive()
			reopened := driverState(t, d)
			if reopened.PendingActions[action] != state.PendingActions[action] || !reflect.DeepEqual(reopened.PendingHostActions[termination.ActionID], termination) {
				t.Fatal("reopen changed or repeated the original stop responsibility")
			}
			stop, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), resume.ActionID, action, protocol.LifecycleStop)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(stop)
			terminated, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), termination.ActionID, termination.Operation, d.host(), termination.ActionID, termination.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: action, Event: protocol.LifecycleStop})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(terminated)
			d.drive()
			final := driverState(t, d)
			if _, current := final.PendingActions[action]; current {
				t.Fatal("observed original resume stop did not retire its original task")
			}
			if effect := final.ReconciledEffects[resume.ActionID]; effect.Intent == nil || !reflect.DeepEqual(*effect.Intent, resume) {
				t.Fatal("control lost the original executed resume", effect)
			}
		})
	}
}
