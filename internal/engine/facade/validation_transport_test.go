package facade

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
)

func candidateAtStage(t *testing.T, stage string) (*validationDriver, protocol.HostActionIntent) {
	t.Helper()
	d := newValidationDriver(t, "whitebox", "quality")
	dev, di := d.action("engine.development.batch.v6")
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "input validation"}})
	if stage != "DEVELOPMENT" {
		d.materialize()
	}
	if stage != "DEVELOPMENT" && stage != "AUTHORING" {
		d.materialize()
		id, in := d.action("engine.qa.design.v6")
		m := in.Manifest
		m.Cases = []coverage.AcceptanceCase{{CaseID: "normal", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestNormal", Oracle: "valid accepted"}, {CaseID: "invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid rejected"}}
		for _, c := range m.Cases {
			m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
		}
		d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
		if stage == "REVIEW_READY" {
			d.materialize()
			id, in = d.action("engine.qa.review.rationale.v2")
			r := approvedReview(in.Manifest)
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Review: &r})
		}
	}
	d.drive()
	return d, onlyCandidate(t, d, stage)
}
func assertCandidateStep(t *testing.T, d *validationDriver, intent protocol.HostActionIntent) {
	t.Helper()
	for _, step := range d.f.definition.Steps {
		if payload, ok := step.Payload.(compiler.CompiledHostActionStep); ok && payload.Operation == intent.Adapter.Operation {
			if intent.Step != step.Header.ID {
				t.Fatalf("candidate intent lost compiled step: got %q want %q", intent.Step, step.Header.ID)
			}
			return
		}
	}
	t.Fatal("candidate operation has no compiled step")
}

func onlyCandidate(t *testing.T, d *validationDriver, stage string) protocol.HostActionIntent {
	t.Helper()
	if len(d.run.AdapterIntents) != 1 {
		t.Fatalf("one executable candidate required: %+v", d.run.AdapterIntents)
	}
	for _, intent := range d.run.AdapterIntents {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" || intent.Adapter.Params["stage"] != stage {
			t.Fatalf("wrong typed candidate: %+v", intent)
		}
		assertCandidateStep(t, d, intent)
		return intent
	}
	panic("unreachable")
}
func candidateFailure(t *testing.T, d *validationDriver, intent protocol.HostActionIntent, class authoring.FailureClass) protocol.Event {
	t.Helper()
	e, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusFailed, class, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(e)
	return e
}
func candidateCompletion(t *testing.T, d *validationDriver, intent protocol.HostActionIntent) protocol.Event {
	t.Helper()
	data, _ := json.Marshal(intent.Adapter.Params)
	var p protocol.CandidateParameters
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return d.candidateReceipt(intent.ActionID, intent, p)
}
func holdRequirement(t *testing.T, d *validationDriver) protocol.Event {
	t.Helper()
	e, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlRequestRequirementChange, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(e)
	return e
}
func releaseRequirement(t *testing.T, d *validationDriver, request protocol.Event) {
	t.Helper()
	ask := d.run.PendingRequirementAsk
	if ask == nil || ask.RequestID != string(request.ID) {
		t.Fatal("missing held requirement")
	}
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(request.ID), Token: ask.Token, Choice: "abort"}})
}

func TestCandidateFailureRoutesKeepTheirOriginalEffect(t *testing.T) {
	for _, stage := range []string{"DEVELOPMENT", "AUTHORING", "QA_ARTIFACT", "REVIEW_READY"} {
		for _, class := range []authoring.FailureClass{authoring.FailureBlockedBug, authoring.FailureInvariantViolation, authoring.FailureBusinessReject, authoring.FailureUserActionRequired, authoring.FailureSideEffectUnknown} {
			for _, held := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/held=%t", stage, class, held), func(t *testing.T) {
					d, intent := candidateAtStage(t, stage)
					var request protocol.Event
					if held {
						request = holdRequirement(t, d)
					}
					ev := candidateFailure(t, d, intent, class)
					if held {
						releaseRequirement(t, d, request)
					}
					d.reopen()
					d.drive()
					if len(d.run.AdapterIntents) != 0 {
						t.Fatal("failed or unknown effect was executable again")
					}
					snap, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(snap.State.PendingHostActions[intent.ActionID], intent) || len(snap.State.HostActionFailures[intent.ActionID]) != 1 {
						t.Fatal("original intent or wire failure lost")
					}
					raw := snap.State.HostActionFailures[intent.ActionID][0]
					if raw.Correlation != intent.Correlation || raw.Step != intent.Step || raw.PayloadDigest != intent.PayloadDigest || raw.FailureClass != class {
						t.Fatal("raw failure changed")
					}
					accepted := d.submit(ev)
					before := d.run.Revision
					d.reopen()
					d.drive()
					if d.run.Revision != before {
						t.Fatal("failed effect recovery changed state repeatedly")
					}
					// Receipt transport identity cannot replace the original failed obligation.
					beforeSnapshot, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					conflicting := ev
					payload := *ev.HostAction
					payload.Correlation = "different-transport"
					conflicting.HostAction = &payload
					conflicting.ID = d.eventID()
					if _, _, err := d.f.Submit(conflicting); err == nil {
						t.Fatal("receipt correlation bypassed existing failed obligation")
					}
					afterSnapshot, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(beforeSnapshot, afterSnapshot) {
						t.Fatal("rejected receipt correlation changed original state or revision")
					}
					if replay := d.submit(ev); !reflect.DeepEqual(accepted, replay) {
						t.Fatal("original failure replay changed acceptance")
					}
					replayed, err := d.f.engine.Load()
					if err != nil || !reflect.DeepEqual(afterSnapshot, replayed) {
						t.Fatalf("original failure replay changed durable state: %v", err)
					}

				})
			}
		}
	}
}

func TestCandidateTransientAttemptsRequireOneBoundExtra(t *testing.T) {
	for _, stage := range []string{"DEVELOPMENT", "AUTHORING", "QA_ARTIFACT", "REVIEW_READY"} {
		t.Run(stage, func(t *testing.T) {
			d, intent := candidateAtStage(t, stage)
			original := intent
			first := candidateFailure(t, d, intent, authoring.FailureTransientEngine)
			second := onlyCandidate(t, d, stage)
			if second.ActionID == intent.ActionID || second.Correlation != intent.Correlation {
				t.Fatal("retry lost its logical responsibility")
			}
			d.submit(first)
			if onlyCandidate(t, d, stage).ActionID != second.ActionID {
				t.Fatal("duplicate failure repeated execution")
			}
			before := driverState(t, d)
			for i := 0; i < 3; i++ {
				d.drive()
				d.reopen()
				if onlyCandidate(t, d, stage).ActionID != second.ActionID {
					t.Fatal("unfailed retry was signed again by drive or recovery")
				}
			}
			after := driverState(t, d)
			if !reflect.DeepEqual(before.PendingHostActions, after.PendingHostActions) || !reflect.DeepEqual(before.HostActionFailures, after.HostActionFailures) {
				t.Fatal("observation consumed or reset the finite native allowance")
			}
			candidateFailure(t, d, second, authoring.FailureTransientEngine)
			d.reopen()
			d.drive()
			if len(d.run.AdapterIntents) != 0 {
				t.Fatal("exhaustion repeated candidate")
			}
			ask := d.validationAsk("RETRY")
			decideValidation(d, ask, "approve")
			third := onlyCandidate(t, d, stage)
			if third.Correlation != original.Correlation || third.ActionID == second.ActionID {
				t.Fatal("extra attempt changed obligation")
			}
			candidateFailure(t, d, third, authoring.FailureTransientEngine)
			d.reopen()
			d.drive()
			ask = d.validationAsk("RETRY")
			for _, o := range ask.Options {
				if o.ID == "approve" {
					t.Fatal("candidate received a second extra attempt")
				}
			}
			if len(d.run.AdapterIntents) != 0 {
				t.Fatal("third failure silently retried")
			}
			snap, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, records := range snap.State.HostActionFailures {
				for _, r := range records {
					if r.Correlation == original.Correlation {
						count++
					}
				}
			}
			if count != 3 {
				t.Fatalf("wire failures=%d, want three", count)
			}
		})
	}
}

func TestCandidateUnknownCompletionUsesTypedEvidence(t *testing.T) {
	for _, stage := range []string{"DEVELOPMENT", "AUTHORING", "QA_ARTIFACT", "REVIEW_READY"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/held=%t", stage, held), func(t *testing.T) {
				d, intent := candidateAtStage(t, stage)
				unknown, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusUnknown, "", map[string]any{})
				if err != nil {
					t.Fatal(err)
				}
				d.submit(unknown)
				if len(d.run.AdapterIntents) != 0 {
					t.Fatal("unknown effect exposed for reexecution")
				}
				before, _ := d.f.engine.Load()
				plan, _, err := d.f.engine.ReconcileHostAction(intent.ActionID, "observed", true, false, protocol.Observation{})
				if err != nil || plan.Action != protocol.RecoveryWait {
					t.Fatalf("untyped reconciliation=%+v %v", plan, err)
				}
				after, _ := d.f.engine.Load()
				if !reflect.DeepEqual(before.State.Validation, after.State.Validation) {
					t.Fatal("boolean observation promoted candidate")
				}
				good := candidateCompletion(t, d, intent)
				bad := good
				payload := *good.HostAction
				values := map[string]any{}
				for k, v := range payload.AdapterEvidence.Values {
					values[k] = v
				}
				values["parametersDigest"] = "wrong"
				payload.AdapterEvidence = &protocol.AdapterEvidence{Values: values}
				bad.HostAction = &payload
				bad.ID = d.eventID()
				before, _ = d.f.engine.Load()
				if _, _, err := d.f.Submit(bad); err == nil {
					t.Fatal("unknown completion admitted wrong intent proof")
				}
				after, _ = d.f.engine.Load()
				if !reflect.DeepEqual(before.State, after.State) {
					t.Fatal("invalid completion changed state")
				}
				var request protocol.Event
				if held {
					request = holdRequirement(t, d)
				}
				d.submit(good)
				if held {
					releaseRequirement(t, d, request)
				}
				d.reopen()
				d.drive()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, pending := snap.State.PendingHostActions[intent.ActionID]; pending {
					t.Fatal("typed observed effect did not settle")
				}
				fact := snap.State.ReconciledEffects[intent.ActionID]
				if fact.UnknownReceipt == nil || fact.UnknownReceipt.Status != protocol.HostActionStatusUnknown || fact.UnknownReceipt.PayloadDigest != intent.PayloadDigest {
					t.Fatal("original unknown receipt not preserved")
				}
				raw := snap.State.HostActionReceipts[intent.ActionID]
				if raw.Step != intent.Step {
					t.Fatal("reconciled candidate receipt lost original step")
				}
				if raw.AdapterEvidence.Values["identity"] != good.HostAction.AdapterEvidence.Values["identity"] {
					t.Fatal("typed completed content not retained")
				}
				d.submit(good)
				d.submit(unknown)
				revision := d.run.Revision
				d.reopen()
				d.drive()
				if d.run.Revision != revision {
					t.Fatal("completion replay repeated side effects")
				}
				conflicting := candidateCompletion(t, d, intent)
				conflicting.ID = d.eventID()
				conflicting.HostAction.AdapterEvidence.Values["identity"] = "different-completion"
				if _, _, err := d.f.Submit(conflicting); err == nil {
					t.Fatal("conflicting completed fact accepted")
				}
			})
		}
	}
}

func TestBusinessSpawnExhaustionUsesOriginalTaskAllowance(t *testing.T) {
	for _, held := range []bool{false, true} {
		for _, firstWorker := range []bool{false, true} {
			t.Run(fmt.Sprintf("held=%t/worker-first=%t", held, firstWorker), func(t *testing.T) {
				d := newValidationDriver(t, "blackbox", "quality")
				id, input := d.action("engine.development.batch.v6")
				key := input.Binding.Task
				if firstWorker {
					d.receipt(id)
					d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: authoring.FailureTransientEngine, PayloadDigest: "worker-environment-unavailable"}})
					id, input = d.action("engine.development.batch.v6")
					if input.Binding.Task != key {
						t.Fatal("worker retry replaced logical task")
					}
				}
				for n := 1; n <= 2; n++ {
					if firstWorker && n == 1 {
						continue
					}
					var request protocol.Event
					if held {
						request = holdRequirement(t, d)
					}
					ev, err := protocol.NewSpawnReceiptWithFailureClass(d.eventID(), id, DefaultProvider, fmt.Sprintf("transport-%d", n), protocol.SpawnStatusFailed, authoring.FailureTransientEngine)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(ev)
					if held {
						releaseRequirement(t, d, request)
					}
					if n == 1 {
						before, _ := d.f.engine.Load()
						d.submit(ev)
						after, _ := d.f.engine.Load()
						if !reflect.DeepEqual(before, after) {
							t.Fatal("duplicate spawn failure consumed allowance")
						}
					}
				}
				d.reopen()
				d.drive()
				snapshot, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				ended := snapshot.State.EndedAttempts[key]
				if snapshot.State.Guard.UsedRetries != 1 {
					t.Fatal("exhausted original execution facts lost spent retry")
				}
				if ended.Attempts != 2 || ended.MaxAttempts != 2 || !ended.RetryExhausted {
					t.Fatalf("finite endpoint=%+v", ended)
				}
				if _, active := snapshot.State.Attempts[key]; active {
					t.Fatal("exhausted spawn remained live")
				}
				wantWorker := 0
				if firstWorker {
					wantWorker = 1
				}
				if len(snapshot.State.RejectedResults) != wantWorker {
					t.Fatal("spawn manufactured a worker-result payload")
				}
				ask := d.validationAsk("RETRY")
				decideValidation(d, ask, "approve")
				next, ni := d.action("engine.development.batch.v6")
				if next == id || ni.Binding.Task != key {
					t.Fatal("extra execution did not retain original task")
				}
				ev, err := protocol.NewSpawnReceiptWithFailureClass(d.eventID(), next, DefaultProvider, "transport-extra", protocol.SpawnStatusFailed, authoring.FailureTransientEngine)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(ev)
				d.reopen()
				d.drive()
				snapshot, err = d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				ended = snapshot.State.EndedAttempts[key]
				if snapshot.State.Guard.UsedRetries != 2 {
					t.Fatal("additional actual spawn attempt lost spent retry")
				}
				if ended.Attempts != 3 || ended.MaxAttempts != 3 || !ended.RetryExhausted {
					t.Fatalf("extra endpoint=%+v", ended)
				}
				for _, option := range d.validationAsk("RETRY").Options {
					if option.ID == "approve" {
						t.Fatal("second extra execution offered")
					}
				}
				count := 0
				for _, records := range snapshot.State.SpawnFailures {
					for _, r := range records {
						count++
						if r.Status != protocol.SpawnStatusFailed || r.FailureClass != authoring.FailureTransientEngine {
							t.Fatal("original transport failure lost")
						}
					}
				}
				if count != 3-wantWorker || len(snapshot.State.RejectedResults) != wantWorker {
					t.Fatal("cumulative worker/spawn evidence diverged")
				}
			})
		}
	}
}

// A completed candidate changes readiness in the same transaction; pure
// observation receipts are covered independently by the protocol tests.
func TestCandidateCompletionRefillsReadyWork(t *testing.T) {
	for _, stage := range []string{"DEVELOPMENT", "AUTHORING", "QA_ARTIFACT", "REVIEW_READY"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/held=%t", stage, held), func(t *testing.T) {
				d, intent := candidateAtStage(t, stage)
				var request protocol.Event
				if held {
					request = holdRequirement(t, d)
				}
				event := candidateCompletion(t, d, intent)
				accepted := d.submit(event)
				if held {
					snap, err := d.f.engine.Load()
					if err != nil {
						t.Fatal(err)
					}
					if len(snap.State.HeldHostSettles) != 1 || len(accepted.Refill) != 0 {
						t.Fatal("held completion advanced business")
					}
					d.reopen()
					releaseRequirement(t, d, request)
				}
				d.reopen()
				snap, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := snap.State.PendingHostActions[intent.ActionID]; ok || len(snap.State.HeldHostSettles) != 0 {
					t.Fatal("completed intent was not settled")
				}
				if raw := snap.State.HostActionReceipts[intent.ActionID]; raw.Step != intent.Step || raw.PayloadDigest != intent.PayloadDigest || raw.AdapterEvidence.Values["identity"] != event.HostAction.AdapterEvidence.Values["identity"] {
					t.Fatal("completion evidence changed")
				}
				expected := map[string]string{"AUTHORING": "engine.qa.design.v6", "QA_ARTIFACT": "engine.qa.review.rationale.v2", "REVIEW_READY": "engine.qa.execution.v4"}[stage]
				if stage == "DEVELOPMENT" {
					d.drive()
					onlyCandidate(t, d, "AUTHORING")
				} else {
					if stage == "REVIEW_READY" {
						if len(accepted.Refill) != 0 || len(snap.State.PendingActions) != 0 || d.run.Next.Kind != decision.KindHostAction {
							t.Fatal("final candidate dispatched workers before native workspaces were ready")
						}
					}
					id, in := d.action(expected)
					issued := snap
					if stage == "REVIEW_READY" {
						issued, err = d.f.engine.Load()
						if err != nil {
							t.Fatal(err)
						}
					}
					if issued.State.DispatchBindings[id] != in.Binding {
						t.Fatal("refill did not preserve signed input")
					}
					if !held && stage != "REVIEW_READY" && len(accepted.Refill) == 0 {
						t.Fatal("Submit omitted actual issued work")
					}
					if stage == "REVIEW_READY" {
						d.gateAction("quality")
						if len(d.run.ValidationInputs) != 2 {
							t.Fatal("QA and gate were not filled in parallel")
						}
					}
				}
				d.drive()
				before := d.run.Revision
				d.submit(event)
				d.reopen()
				d.drive()
				if d.run.Revision != before {
					t.Fatal("duplicate completion or recovery repeated work")
				}
			})
		}
	}
}

func TestSpawnTransportRetryKeepsSpentSlotAfterSuccess(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	id, input := d.action("engine.development.batch.v6")
	failed, err := protocol.NewSpawnReceiptWithFailureClass(d.eventID(), id, d.host(), "failed-transport", protocol.SpawnStatusFailed, authoring.FailureTransientEngine)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(failed)
	if driverState(t, d).Guard.UsedRetries != 0 {
		t.Fatal("first failed execution was charged as a retry")
	}
	d.receipt(id)
	if driverState(t, d).Guard.UsedRetries != 1 {
		t.Fatal("successful transport retry lost spent slot")
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.drive()
	d.reopen()
	d.drive()
	completed := driverState(t, d)
	if completed.Guard.UsedRetries != 1 || completed.Attempts[input.Binding.Task].ID != "" {
		t.Fatal("success/reload reclaimed spent transport retry")
	}
}
