package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"slices"
	"testing"
)

func TestRecoveryDecisionAppliesOriginalAttemptChoice(t *testing.T) {
	for _, choice := range []string{"fresh", "resume", "abort"} {
		for _, reopen := range []bool{false, true} {
			t.Run(choice+map[bool]string{false: "/normal", true: "/reopen"}[reopen], func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, input := d.action("engine.development.batch")
				d.receipt(id)
				original := d.run.Attempts[input.Binding.Task]
				started, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(started)
				plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{}, emptyFingerprint())
				if err != nil {
					t.Fatal(err)
				}
				if plan.Action != protocol.RecoveryAsk {
					t.Fatalf("expected actual recovery Ask: %+v", plan)
				}
				if reopen {
					d.reopen()
				}
				token, err := d.f.Freshness(plan.RequestID)
				if err != nil {
					t.Fatal(err)
				}
				event, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(plan.RequestID), token, protocol.AskOptionID(choice))
				if err != nil {
					t.Fatal(err)
				}
				accepted := d.submit(event)
				if choice != "abort" {
					d.drive()
					snapshot, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					delivered := false
					for action, intent := range snapshot.State.PendingHostActions {
						identity := ""
						kind := ""
						if choice == "fresh" && intent.Terminate != nil && intent.Terminate.WorkerActionID == id {
							identity = intent.Terminate.Identity
							kind = protocol.LifecycleStop
						}
						if choice == "resume" && intent.Resume != nil && intent.Resume.WorkerActionID == id {
							identity = intent.Resume.Identity
							kind = protocol.LifecycleStart
						}
						if identity == "" {
							continue
						}
						if identity != id {
							t.Fatal("host operation lost original session identity")
						}
						if reopen {
							d.reopen()
						}
						observed, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, identity, kind)
						if err != nil {
							t.Fatal(err)
						}
						d.submit(observed)
						receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), action, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: identity, Event: kind})
						if err != nil {
							t.Fatal(err)
						}
						d.submit(receipt)
						d.drive()
						delivered = true
					}
					if choice == "resume" && !delivered {
						t.Fatal("resume accepted without native resume operation")
					}
				}
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if choice == "fresh" {
					next, ok := snapshot.State.Attempts[input.Binding.Task]
					if !ok || next.ActionID == original.ActionID || next.Attempts != original.Attempts+1 || next.MaxAttempts != original.MaxAttempts {
						t.Fatalf("fresh was accepted without fresh original-task attempt: acceptance=%+v original=%+v after=%+v", accepted, original, next)
					}
					if snapshot.State.DispatchBindings[next.ActionID] != input.Binding {
						t.Fatal("fresh recovery changed original work binding")
					}
				} else if choice == "resume" {
					next, ok := snapshot.State.Attempts[input.Binding.Task]
					if !ok || next.ID != original.ID || next.ActionID != original.ActionID || next.MaxAttempts != original.MaxAttempts {
						t.Fatalf("resume replaced original attempt: %+v", next)
					}
					if snapshot.State.DispatchBindings[next.ActionID] != input.Binding {
						t.Fatal("resume changed original work binding")
					}
				} else {
					if _, ok := snapshot.State.AbortDecision(); !ok {
						t.Fatalf("abort was accepted without original abort authority: %+v", accepted)
					}
				}
			})
		}
	}
}

func TestRecoveryRequestReusesPendingEffectAndOriginalAllowance(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			id, in := d.action("engine.development.batch")
			d.receipt(id)
			start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
			d.submit(start)
			original := d.run.Attempts[in.Binding.Task]
			for used := 1; used <= 2; used++ {
				plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint())
				if err != nil {
					t.Fatal(err)
				}
				if plan.RequestID == "" {
					t.Fatal("recovery has no original request reference")
				}
				before, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				wire, _ := json.Marshal(before)
				repeat, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint())
				if err != nil || repeat.RequestID != plan.RequestID {
					t.Fatalf("pending effect was replaced: %+v %v", repeat, err)
				}
				after, _ := d.f.engine.Load()
				again, _ := json.Marshal(after)
				if string(wire) != string(again) {
					t.Fatal("duplicate recovery changed state or spent allowance")
				}
				if reopen {
					d.reopen()
				}
				var intent protocol.HostActionIntent
				for _, p := range after.State.PendingHostActions {
					if p.Correlation == plan.RequestID {
						intent = p
					}
				}
				kind := protocol.LifecycleStart
				if used == 2 {
					kind = protocol.LifecycleStop
				}
				if used == 1 && intent.Resume == nil || used == 2 && intent.Terminate == nil {
					t.Fatalf("wrong finite recovery effect: %+v", intent)
				}
				observed, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, kind)
				d.submit(observed)
				receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: kind})
				if err != nil {
					t.Fatal(err)
				}
				a := d.submit(receipt)
				same, r, err := d.f.Submit(receipt)
				if err != nil || !reflect.DeepEqual(a, same) {
					t.Fatal("native receipt replay changed acceptance")
				}
				d.run = r
				d.drive()
			}
			ended := d.run.EndedAttempts[in.Binding.Task]
			if ended.Attempts != 2 || ended.MaxAttempts != original.MaxAttempts || !ended.RetryExhausted {
				t.Fatalf("recovery changed allowance: %+v", ended)
			}
			ask := d.validationAsk("RETRY")
			decideValidation(d, ask, "approve")
			fresh, fi := d.action("engine.development.batch")
			if fresh == id || fi.Binding != in.Binding {
				t.Fatal("extra authorization replaced duty or signed binding")
			}
			d.business(fresh, protocol.ValidationResult{Binding: fi.Binding, Status: "PASS", Development: batchEvidence(fi)})
			d.reopen()
			if d.run.Validation.DevelopmentSnapshot != "" {
				t.Fatal("worker completion invented a materialized candidate")
			}
		})
	}
}

func TestResumeCarriesCheckpointAndCompletesOnlyRemainingCases(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			id, in := d.action("engine.qa.execution")
			full := executionFor(in)
			original := d.run.Attempts[in.Binding.Task]
			part := full
			part.Records = append([]coverage.ExecutionRecord(nil), full.Records[:1]...)
			part.Binding.ActualCaseIDs = []string{part.Records[0].CaseID}
			d.receipt(id)
			start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
			d.submit(start)
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "first actual case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &part, Tool: "command", Environment: "clean project", Checkpoint: true}}})
			if reopen {
				d.reopen()
			}
			plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, emptyFingerprint())
			if err != nil {
				t.Fatal(err)
			}
			snap, _ := d.f.engine.Load()
			var intent protocol.HostActionIntent
			for _, p := range snap.State.PendingHostActions {
				if p.Correlation == plan.RequestID {
					intent = p
				}
			}
			if intent.Resume == nil || intent.Resume.Input == nil {
				t.Fatal("resume lacks original unfinished input")
			}
			input := *intent.Resume.Input
			if input.Binding != in.Binding || !reflect.DeepEqual(input.Completed, part.Records) || len(input.PendingCases) != 1 || input.PendingCases[0] == part.Records[0].CaseID {
				t.Fatalf("resume changed actual completed/remaining partition: %+v", input)
			}
			receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: protocol.LifecycleStart})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(receipt)
			next := d.run.Attempts[in.Binding.Task]
			if next.ID != original.ID || next.ActionID != id || next.Attempts != 1 {
				t.Fatal("resume replaced original checkpoint attempt")
			}
			remaining := full
			remaining.Records = append([]coverage.ExecutionRecord(nil), full.Records[1:]...)
			remaining.Binding.ActualCaseIDs = nil
			for _, row := range remaining.Records {
				if !slices.Contains(remaining.Binding.ActualCaseIDs, row.CaseID) {
					remaining.Binding.ActualCaseIDs = append(remaining.Binding.ActualCaseIDs, row.CaseID)
				}
			}
			d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "remaining actual case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &remaining, Tool: "command", Environment: "clean project"}}})
			gate, gi := d.gateAction("quality")
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
			d.drive()
			d.reopen()
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			target := wave.Targets[in.Binding.Task.String()]
			if wave.Status != "COMPLETE" || len(target.Execution.Records) != len(full.Records) {
				t.Fatal("remaining evidence failed to complete the original wave")
			}
			for _, row := range part.Records {
				if !slices.ContainsFunc(target.Execution.Records, func(saved coverage.ExecutionRecord) bool { return reflect.DeepEqual(saved, row) }) {
					t.Fatal("resume rewrote completed case content")
				}
			}
		})
	}
}
