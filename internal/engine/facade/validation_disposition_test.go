package facade

import (
	"errors"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

func TestDisposedValidationTargetRetiresEveryRetryDelivery(t *testing.T) {
	for _, delivery := range []string{"staged", "receipted", "terminal", "late-runtime-error"} {
		t.Run(delivery, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			id, in := d.gateAction("quality")
			d.receipt(id)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "tool-unavailable"}})
			d.drive()
			retry, ri := d.gateAction("quality")
			result := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: retry, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "completed-gate-retry", Validation: &protocol.ValidationResult{Binding: ri.Binding, Status: "PASS"}}}
			if delivery == "staged" {
				if a := d.submit(result); a.Status != "STAGED" {
					t.Fatalf("not staged: %+v", a)
				}
			} else {
				d.receipt(retry)
				if delivery == "terminal" {
					attempt := d.run.PendingActions[retry].AttemptID
					for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
						event, err := protocol.NewTaskEvent(d.eventID(), ri.Binding.Task, attempt, status)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(event)
					}
				}
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "DISPOSE", Target: in.Binding.Task.String()}, "")
			d.reopen()
			if _, pending := d.run.PendingActions[retry]; pending {
				t.Fatal("disposed target retained live retry")
			}
			if delivery == "late-runtime-error" {
				result.Result.Outcome = protocol.OutcomeRuntimeError
				result.Result.FailureClass = "TRANSIENT_ENGINE_ERROR"
				result.Result.Validation = nil
			}
			if delivery == "staged" {
				event, err := protocol.NewSpawnReceiptEvent(d.eventID(), retry, DefaultProvider, retry, protocol.SpawnStatusSpawned)
				if err != nil {
					t.Fatal(err)
				}
				revision := d.run.Revision
				_, _, err = d.f.Submit(event)
				var rejected *protocol.RejectedError
				if !errors.As(err, &rejected) || rejected.Code != protocol.CodeUnknownAction {
					t.Fatalf("retired receipt error = %v", err)
				}
				d.reopen()
				if d.run.Revision != revision {
					t.Fatal("rejected late receipt changed state")
				}
			}
			d.submit(result)
			d.submit(result)
			result.ID = d.eventID()
			d.submit(result)
			d.reopen()
			d.drive()
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			state := snapshot.State
			target := state.Validation.Waves[0].Targets[in.Binding.Task.String()]
			if target.Status != "DISPOSED" || target.DispositionReason == "" {
				t.Fatal("late retry changed disposition")
			}
			if _, ended := state.EndedAttempts[in.Binding.Task]; ended {
				t.Fatal("late evidence recreated retry eligibility")
			}
			if _, current := state.Results[retry]; current {
				t.Fatal("retired result became current")
			}
			if len(state.ObsoleteResults[retry]) != 1 || state.ObsoleteResults[retry][0].PayloadDigest != result.Result.PayloadDigest {
				t.Fatal("late result audit is missing or duplicated")
			}
			if receipt, ok := state.SpawnReceipts[retry]; delivery == "staged" {
				if ok {
					t.Fatal("rejected late receipt was recorded")
				}
			} else if !ok || receipt.Task != ri.Binding.Task {
				t.Fatal("original receipt lost its exact task identity")
			}
			for _, input := range d.run.ValidationInputs {
				if input.Binding.Task == ri.Binding.Task {
					t.Fatal("disposed task was reissued")
				}
			}
			qa, qi := d.action("engine.qa.execution.v2")
			report := executionFor(qi)
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			d.reopen()
			d.drive()
			if d.run.Validation.Waves[0].Status != "COMPLETE" {
				t.Fatal("independent QA could not finish disposed wave")
			}
		})
	}
}

func TestDispositionSuspendsSignedRepairUntilUserChooses(t *testing.T) {
	for _, retry := range []bool{false, true} {
		for _, choice := range []string{"REPAIR", "DISMISS", "decline"} {
			t.Run(fmt.Sprintf("retry=%v/%s", retry, choice), func(t *testing.T) {
				d, finding, wave := validationWithGateFinding(t)
				resolveGateFinding(d, finding, wave)
				id, input := d.action("engine.repair.v5")
				if retry {
					d.receipt(id)
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "unavailable-workspace"}})
					d.drive()
					id, input = d.action("engine.repair.v5")
				}
				result := gateRepairResult(input)
				delivered := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "verified-repair", Validation: &result}}
				d.submit(delivered)
				request := protocol.Event{ID: d.eventID(), Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave, Remedies: []string{"REPAIR", "DISMISS"}}}}
				d.submit(request)
				d.receipt(id)
				d.submit(delivered)
				d.reopen()
				d.drive()
				before, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if before.State.Results[id].Validation != nil || len(d.run.AdapterIntents) != 0 || before.State.StagedResults[id].PayloadDigest != "verified-repair" {
					t.Fatal("undecided repair advanced or lost its staged evidence")
				}
				if _, visible := d.run.ValidationInputs[id]; visible {
					t.Fatal("pending remedy exposes executable repair input")
				}
				ask := d.validationAsk("VALIDATION_DETAIL_DISPOSITION")
				decide := &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{StateRevision: d.run.Revision, Digest: ask.Validation.Digest, Remedy: choice, Reason: "confirmed the specific disposition"}}
				if choice == "decline" {
					decide.Choice = "decline"
					decide.Validation = nil
				}
				event := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: decide}
				d.submit(event)
				d.submit(event)
				d.reopen()
				d.drive()
				if choice == "REPAIR" {
					d.verifyRepair(result)
					d.materialize()
				}
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				current := after.State.Validation.Waves[0]
				switch choice {
				case "REPAIR":
					if current.Status != "REPAIRED" || after.State.Results[id].Validation == nil || after.State.Results[id].Validation.Development.Summary != result.Development.Summary || len(d.run.AdapterIntents) != 1 {
						t.Fatalf("approved repair lost its original result or frozen-source obligation: wave=%s result=%+v intents=%+v", current.Status, after.State.Results[id], d.run.AdapterIntents)
					}

				case "DISMISS":
					if current.Status != "COMPLETE" || len(d.run.AdapterIntents) != 0 || len(after.State.ObsoleteResults[id]) != 1 {
						t.Fatal("dismissal failed to retain obsolete evidence without materializing repair")
					}
				case "decline":
					if after.State.Results[id].Validation != nil || len(d.run.AdapterIntents) != 0 || after.State.StagedResults[id].PayloadDigest != "verified-repair" {
						t.Fatal("decline released a repair")
					}
				}
			})
		}
	}
}

func TestDismissalRetiresSignedRepairBeforeLateResult(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			d, finding, wave := validationWithGateFinding(t)
			resolveGateFinding(d, finding, wave)
			id, input := d.action("engine.repair.v5")
			d.receipt(id)
			if terminal {
				for _, status := range []runtime.TaskStatus{runtime.TaskRunning, runtime.TaskValidating, runtime.TaskTerminal} {
					ev, err := protocol.NewTaskEvent(d.eventID(), input.Binding.Task, d.run.PendingActions[id].AttemptID, status)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(ev)
				}
			}
			d.chooseValidation(protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave, Remedies: []string{"REPAIR", "DISMISS"}}, "DISMISS")
			d.reopen()
			d.drive()
			result := gateRepairResult(input)
			ev := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "late-completed-repair", Validation: &result}}
			accepted := d.submit(ev)
			if accepted.Status != "OBSOLETE_RESULT" {
				t.Fatalf("late signed evidence not quarantined: %+v", accepted)
			}
			before, _ := d.f.engine.Load()
			d.submit(ev)
			after, _ := d.f.engine.Load()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("duplicate late evidence changed state")
			}
			d.reopen()
			d.drive()
			snapshot, _ := d.f.engine.Load()
			if !reflect.DeepEqual(snapshot.State.ObsoleteResults[id][0].Validation, &result) || len(snapshot.State.ObsoleteResults[id]) != 1 {
				t.Fatal("retirement lost actual late payload")
			}
			if _, ok := snapshot.State.Results[id]; ok {
				t.Fatal("retired repair became an effective result")
			}
			if d.run.Validation.Waves[0].Status != "COMPLETE" || snapshot.State.Results[id].Validation != nil || len(d.run.AdapterIntents) != 0 {
				t.Fatal("late completion revived a disposed duty")
			}
		})
	}
}
