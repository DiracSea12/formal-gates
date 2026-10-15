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
	testOriginalAttemptRecovery(t, false)
}

func TestWorkerFailureCreatesRecoveryForOriginalAttempt(t *testing.T) {
	testOriginalAttemptRecovery(t, true)
}

func testOriginalAttemptRecovery(t *testing.T, workerFailure bool) {
	t.Helper()
	for _, choice := range []string{"fresh", "resume", "abort"} {
		for _, reopen := range []bool{false, true} {
			t.Run(choice+map[bool]string{false: "/normal", true: "/reopen"}[reopen], func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, input := d.action("engine.development.batch.v6")
				d.receipt(id)
				original := d.run.Attempts[input.Binding.Task]
				started, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(started)
				var plan protocol.RecoveryPlan
				if workerFailure {
					failure := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, PayloadDigest: "native permanent failure", FailureClass: "USER_ACTION_REQUIRED", InterruptionReason: "native model is unsupported for this account"}}
					first := d.submit(failure)
					failedRevision := d.run.Revision
					if duplicate := d.submit(failure); !reflect.DeepEqual(duplicate, first) || d.run.Revision != failedRevision {
						t.Fatal("repeated failure changed its acceptance or revision")
					}
					d.drive()
					var request string
					for _, ask := range d.run.PendingAsks {
						if ask.Control == protocol.ControlRecovery {
							if request != "" {
								t.Fatal("failure opened parallel recovery requests")
							}
							request = ask.RequestID
						}
					}
					if request == "" {
						t.Fatal("normal user-action failure has no recovery Ask")
					}
					plan, _, err = d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "USER_ACTION_REQUIRED"}, protocol.Observation{})
					if err != nil || plan.RequestID != request {
						t.Fatalf("explicit recovery did not reuse the failure request: %+v %v", plan, err)
					}
					snap, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if len(snap.State.PendingHostActions) != 0 {
						t.Fatal("failure signed native recovery before a user decision")
					}
					failed := snap.State.RecoverableResults[id]
					if len(failed) != 1 || failed[0].InterruptionReason != failure.Result.InterruptionReason {
						t.Fatal("failure lost original evidence")
					}
				} else {
					plan, _, err = d.f.engine.RecoverAttempt(id, protocol.Interruption{}, protocol.Observation{})
					if err != nil || plan.Action != protocol.RecoveryAsk {
						t.Fatalf("expected actual recovery Ask: %+v %v", plan, err)
					}
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
					for _, intent := range snapshot.State.PendingHostActions {
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
						if intent.Resume != nil {
							d.submit(d.resumeReceipt(intent))
						} else {
							observed, err := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, identity, kind)
							if err != nil {
								t.Fatal(err)
							}
							d.submit(observed)
							receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: identity, Event: kind})
							if err != nil {
								t.Fatal(err)
							}
							d.submit(receipt)
						}
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
				if workerFailure && choice != "abort" {
					if snapshot.State.Guard.UsedRetries != 1 {
						t.Fatalf("executed %s recovery lost its retry consumption: %+v", choice, snapshot.State.Guard)
					}
					retry := snapshot.State.Attempts[input.Binding.Task].ActionID
					d.business(retry, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
					d.drive()
					completed, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if completed.State.Guard.UsedRetries != 1 || completed.State.Attempts[input.Binding.Task].ID != "" {
						t.Fatalf("successful recovery reclaimed consumed retry: %+v", completed.State.Guard)
					}
					d.reopen()
					d.drive()
					reloaded, err := d.f.engine.Load()
					if err != nil || reloaded.State.Guard.UsedRetries != 1 {
						t.Fatalf("reload changed successful retry consumption: %+v %v", reloaded, err)
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
			id, in := d.action("engine.development.batch.v6")
			d.receipt(id)
			start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
			d.submit(start)
			original := d.run.Attempts[in.Binding.Task]
			for used := 1; used <= 2; used++ {
				plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
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
				repeat, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
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
				if used == 2 {
					// 有限边界：不创建任何宿主效果，落预算处置 Ask（继续执行
					// 只能由用户授权一次有限增量）。
					if intent.Resume != nil || intent.Terminate != nil {
						t.Fatalf("finite boundary created a native effect: %+v", intent)
					}
					shown, err := d.f.Show()
					if err != nil {
						t.Fatal(err)
					}
					d.run = shown
					var ask PendingAskFace
					for _, face := range shown.PendingAsks {
						if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
							ask = face
						}
					}
					if ask.RequestID == "" {
						t.Fatal("finite recovery boundary did not land a budget disposition ask")
					}
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
						Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "retry",
						Validation: &protocol.ValidationDecision{Reason: "one more resume for the interrupted agent", Digest: ask.Validation.Digest, StateRevision: shown.Revision},
					}})
					authorized, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					for _, p := range authorized.State.PendingHostActions {
						if p.Correlation == plan.RequestID {
							intent = p
						}
					}
					if intent.Resume == nil {
						t.Fatalf("authorized retry did not resume the original attempt: %+v", intent)
					}
				} else if intent.Resume == nil {
					t.Fatalf("finite recovery effect within budget: %+v", intent)
				}
				receipt := d.resumeReceipt(intent)
				a := d.submit(receipt)
				same, r, err := d.f.Submit(receipt)
				if err != nil || !reflect.DeepEqual(a, same) {
					t.Fatal("native receipt replay changed acceptance")
				}
				d.run = r
				d.drive()
			}
			// 两次恢复执行都计入同一 TaskKey 的 Attempt 账本：身份不变、计数
			// 2、额度只多出用户授权的一个槽位。
			final, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			continued := final.State.Attempts[in.Binding.Task]
			if continued.ID != original.ID || continued.Attempts != 2 || continued.MaxAttempts != original.MaxAttempts+1 {
				t.Fatalf("recovery changed allowance or attempt identity: original=%+v continued=%+v", original, continued)
			}
			// 预算通道的 retry 授权一次性：再次到达边界后的第二个 retry 决定被拒。
			if _, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			shown, err := d.f.Show()
			if err != nil {
				t.Fatal(err)
			}
			var ask PendingAskFace
			for _, face := range shown.PendingAsks {
				if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
					ask = face
				}
			}
			if ask.RequestID == "" {
				t.Fatal("the second finite boundary did not land a budget disposition ask")
			}
			_, _, err = d.f.Submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
				Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "retry",
				Validation: &protocol.ValidationDecision{Reason: "a second extension of the same boundary", Digest: ask.Validation.Digest, StateRevision: shown.Revision},
			}})
			if err == nil {
				t.Fatal("a second budget retry authorization extended the same boundary action")
			}
		})
	}
}

func TestResumeCarriesCheckpointAndCompletesOnlyRemainingCases(t *testing.T) {
	for _, interruption := range []string{"direct", "worker-failure"} {
		for _, reopen := range []bool{false, true} {
			t.Run(interruption+"/"+map[bool]string{false: "normal", true: "reopen"}[reopen], func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				d.reviewReady()
				id, in := d.action("engine.qa.execution.v4")
				full := executionFor(in)
				tool, environment := protocol.ExecutionIdentities(in.ExecutionFacts)
				original := d.run.Attempts[in.Binding.Task]
				part := full
				part.Records = append([]coverage.ExecutionRecord(nil), full.Records[:1]...)
				part.Binding.ActualCaseIDs = []string{part.Records[0].CaseID}
				d.receipt(id)
				start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
				d.submit(start)
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "first actual case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &part, Tool: tool, Environment: environment, Checkpoint: true}}})
				if reopen {
					d.reopen()
				}
				var plan protocol.RecoveryPlan
				if interruption == "worker-failure" {
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, PayloadDigest: "native interruption after the completed case", FailureClass: "USER_ACTION_REQUIRED", InterruptionReason: "native configuration needs attention"}})
					for _, ask := range d.run.PendingAsks {
						if ask.Control == protocol.ControlRecovery {
							plan.RequestID = ask.RequestID
						}
					}
					if plan.RequestID == "" {
						t.Fatal("partial worker failure lost its recovery Ask")
					}
					token, err := d.f.Freshness(plan.RequestID)
					if err != nil {
						t.Fatal(err)
					}
					decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(plan.RequestID), token, "resume")
					if err != nil {
						t.Fatal(err)
					}
					d.submit(decision)
					d.drive()
				} else {
					var err error
					plan, _, err = d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
					if err != nil {
						t.Fatal(err)
					}
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
				d.submit(d.resumeReceipt(intent))
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
				d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "remaining actual case", Validation: &protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &remaining, Tool: tool, Environment: environment}}})
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
}

func TestConcurrentWorkerFailuresKeepIndependentRecoveryRequests(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	events := []protocol.Event{}
	for _, role := range []string{"engine.development.batch.v6", "engine.qa.design.v6"} {
		id, _ := d.action(role)
		d.receipt(id)
		events = append(events, protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "USER_ACTION_REQUIRED", PayloadDigest: "native failure:" + id, InterruptionReason: "native configuration requires attention"}})
	}
	results := make(chan error, len(events))
	for _, event := range events {
		go func(event protocol.Event) { _, _, err := d.f.Submit(event); results <- err }(event)
	}
	for range events {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	d.reopen()
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, record := range snapshot.State.RecoveryRecords {
		if record.Action != protocol.RecoveryAsk || record.RequestID == "" {
			continue
		}
		if prior := owners[record.ActionID]; prior != "" && prior != record.RequestID {
			t.Fatal("concurrent failure split one recovery owner")
		}
		owners[record.ActionID] = record.RequestID
	}
	if len(owners) != len(events) {
		t.Fatal("concurrent failures lost independent recovery requests")
	}
	requests := map[string]bool{}
	for _, event := range events {
		request := owners[event.Result.ActionID]
		if requests[request] || snapshot.State.PendingAsks[request].Resolved {
			t.Fatal("concurrent failures shared or resolved another duty's request")
		}
		requests[request] = true
		failed := snapshot.State.RecoverableResults[event.Result.ActionID]
		if len(failed) != 1 || failed[0].InterruptionReason != event.Result.InterruptionReason {
			t.Fatal("concurrent failure crossed original evidence owners")
		}
	}
	if len(snapshot.State.PendingHostActions) != 0 {
		t.Fatal("concurrent failure signed recovery without a decision")
	}
}

func TestWorkerRecoveryCarryKeepsOriginalAskAndWork(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, in := d.action("engine.development.batch.v6")
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "USER_ACTION_REQUIRED", PayloadDigest: "native unavailable configuration", InterruptionReason: "native configuration requires attention"}})
	var request string
	for _, ask := range d.run.PendingAsks {
		if ask.Control == protocol.ControlRecovery {
			request = ask.RequestID
		}
	}
	if request == "" {
		t.Fatal("worker failure has no recovery request")
	}
	original := d.run.Attempts[in.Binding.Task]
	confirmValidationWork(d, "")
	d.reopen()
	d.drive()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	ask, exists := snapshot.State.PendingAsks[request]
	if !exists || ask.Resolved || snapshot.State.DispatchBindings[id] != in.Binding {
		t.Fatal("CARRY replaced the original recovery request or work")
	}
	if got := snapshot.State.Attempts[in.Binding.Task]; got.ID != original.ID || got.MaxAttempts != original.MaxAttempts || got.Attempts != original.Attempts {
		t.Fatal("CARRY reset original attempt or allowance")
	}
	token, err := d.f.Freshness(request)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(request), token, "resume")
	if err != nil {
		t.Fatal(err)
	}
	d.submit(decision)
	d.drive()
	var resume *protocol.AgentHostAction
	for _, intent := range d.run.AdapterIntents {
		if intent.Resume != nil && intent.Correlation == request {
			resume = intent.Resume
		}
	}
	if resume == nil || resume.Input == nil || resume.Input.Binding != in.Binding || resume.Input.Workspace == nil || resume.Input.Workspace.Path != in.Workspace.Path {
		t.Fatal("carried recovery lost original work or workspace")
	}
}
