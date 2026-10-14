package facade

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSemanticRejectionFeedbackFollowsOriginalRetry(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	id, input := d.action("engine.qa.design.v5")
	for attempt := 0; attempt < 2; attempt++ {
		manifest := simpleManifest(input)
		manifest.Edges = nil
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
		rejected := driverState(t, d).RejectedResults[id]
		if rejected.Status != "REJECTED_INVALID_RESULT" || rejected.Payload.Outcome != protocol.OutcomePass || rejected.Reason == "" {
			t.Fatal("fixture did not produce an original semantic rejection", rejected)
		}
		if attempt == 1 {
			decideValidation(d, d.validationAsk("RETRY"), "approve")
		}
		d.drive()
		previous := id
		id, input = d.action("engine.qa.design.v5")
		state := driverState(t, d)
		if state.Attempts[input.Binding.Task].PreviousAction != previous || input.Interruption == nil || input.Interruption.ActionID != previous || !strings.Contains(input.Interruption.Detail, rejected.Reason) {
			t.Fatal("retry lost its own rejection feedback", input.Interruption)
		}
		refs, err := d.f.workerMaterials(state, true)
		if err != nil {
			t.Fatal(err)
		}
		ref := refs[id]
		body, err := os.ReadFile(ref.Path)
		if err != nil {
			t.Fatal(err)
		}
		var material worker.Material
		if err := json.Unmarshal(body, &material); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(material.Input.Validation.Interruption, input.Interruption) || !strings.Contains(material.Prompt, rejected.Reason) {
			t.Fatal("material lost original retry feedback")
		}
		d.reopen()
		d.drive()
		reopened, err := d.f.workerMaterials(driverState(t, d), true)
		if err != nil || reopened[id] != ref {
			t.Fatal("reopen changed original retry material", err)
		}
		after, err := os.ReadFile(ref.Path)
		if err != nil || !bytes.Equal(body, after) {
			t.Fatal("reopen overwrote original retry material", err)
		}
	}
}

func TestInterruptedExecutionKeepsCauseAndCompletedRows(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "normal"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			d.fixtureQAResults = map[string]protocol.CheckEvidence{"invalid": {ExitCode: 0, StdoutText: "invalid input accepted"}}
			d.reviewReady()
			id, input := d.action("engine.qa.execution.v3")
			original := d.run.Attempts[input.Binding.Task]
			full := executionFor(input)
			partial := full
			partial.Records = append([]coverage.ExecutionRecord(nil), full.Records[:1]...)
			partial.Binding.ActualCaseIDs = []string{partial.Records[0].CaseID}
			partial.Records[0].Result = coverage.ExecutedFail
			partial.Records[0].Comparison = "approved oracle requires rejection with documented output"
			cause := "工具退出 137\nwhile opening the next case fixture  "
			if held {
				cause = "fixture unavailable\n临时目录不存在  "
			} else {
				d.receipt(id)
			}
			interrupted := nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Execution: &partial, Checkpoint: true})
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "interrupted-with-complete-case", InterruptionReason: cause, Validation: &interrupted}}
			d.submit(event)
			if held {
				d.reopen()
				d.receipt(id)
			}
			before, _ := d.f.engine.Load()
			d.submit(event)
			after, _ := d.f.engine.Load()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("duplicate interruption changed evidence or attempt accounting")
			}
			conflicting := event
			payload := *event.Result
			payload.InterruptionReason = "different actual cause"
			conflicting.Result = &payload
			_, _, conflict := d.f.Submit(conflicting)
			unchanged, _ := d.f.engine.Load()
			if conflict == nil || !reflect.DeepEqual(after, unchanged) {
				t.Fatal("same event different cause did not reject without writes")
			}

			d.reopen()
			d.drive()
			retry, next := d.action("engine.qa.execution.v3")
			if next.Binding != input.Binding || retry == id || next.Interruption == nil || next.Interruption.ActionID != id || next.Interruption.InterruptionReason != cause || next.Interruption.Detail == cause {
				t.Fatal("retry lost original responsibility or confused cause with routing explanation")
			}
			if !reflect.DeepEqual(next.Completed, partial.Records) || len(next.PendingCases) != 1 || next.PendingCases[0] == partial.Records[0].CaseID {
				t.Fatal("interrupted completed evidence was lost or pending case fabricated")
			}
			snap, _ := d.f.engine.Load()
			if snap.State.Attempts[input.Binding.Task].Attempts != original.Attempts+1 || snap.State.RejectedResults[id].Payload.InterruptionReason != cause {
				t.Fatal("actual cause or finite attempt accounting changed")
			}
			if len(d.run.RecoveryRecords) == 0 || d.run.RecoveryRecords[len(d.run.RecoveryRecords)-1].InterruptionReason != cause {
				t.Fatal("public view omitted original cause")
			}
			remaining := full
			remaining.Records = full.Records[1:]
			remaining.Binding.ActualCaseIDs = []string{remaining.Records[0].CaseID}
			d.business(retry, nativeExecutionResult(next, protocol.ValidationResult{Binding: next.Binding, Status: "PASS", Execution: &remaining}))
			d.reopen()
			d.drive()
			target := d.run.Validation.Waves[0].Targets[input.Binding.Task.String()]
			if target.Status != "FAIL" || len(target.Execution.Records) != 2 || len(d.run.Validation.Findings) != 1 {
				t.Fatal("final completion lost original FAIL or invented premature PASS")
			}
			for _, f := range d.run.Validation.Findings {
				if f.ExecutionFailure == nil || f.ExecutionFailure.ResultAction != retry || f.ExecutionFailure.CaseID != partial.Records[0].CaseID {
					t.Fatal("completed cumulative report lost the earlier failed case")
				}
			}
		})
	}
}

func TestNativeResumeUsesLatestActualInterruption(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	d.reviewReady()
	id, input := d.action("engine.qa.execution.v3")
	d.receipt(id)
	start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
	d.submit(start)
	original := d.run.Attempts[input.Binding.Task]
	for _, cause := range []string{"worker tool interrupted\n尚未完成下一项  ", ""} {
		plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR", InterruptionReason: cause}, protocol.Observation{})
		if err != nil {
			t.Fatal(err)
		}
		snap, _ := d.f.engine.Load()
		var intent protocol.HostActionIntent
		for _, candidate := range snap.State.PendingHostActions {
			if candidate.Correlation == plan.RequestID {
				intent = candidate
			}
		}
		if cause == "" {
			d.reopen()
			latest := d.run.ValidationInputs[id].Interruption
			if intent.Resume != nil || intent.Terminate != nil {
				t.Fatal("exhausted boundary resumed or silently replaced the attempt")
			}
			if latest == nil || latest.RequestID != plan.RequestID || latest.InterruptionReason != "" {
				t.Fatal("latest unknown cause was replaced")
			}
			var ask PendingAskFace
			for _, face := range d.run.PendingAsks {
				if face.Validation != nil && face.Validation.Kind == protocol.KindDispatchBudgetDisposition {
					ask = face
				}
			}
			if ask.RequestID == "" {
				t.Fatal("finite recovery boundary did not land a budget disposition ask")
			}
			// 「retry」一次性追加一个槽位：授权后的 resume 继续原逻辑执行，
			// 输入仍携带最新（未知）中断事实。
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
				Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "retry",
				Validation: &protocol.ValidationDecision{Reason: "one more resume for the unknown interruption", Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
			}})
			snap, _ = d.f.engine.Load()
			intent = protocol.HostActionIntent{}
			for _, candidate := range snap.State.PendingHostActions {
				if candidate.Correlation == plan.RequestID {
					intent = candidate
				}
			}
			if intent.Resume == nil || intent.Resume.Input == nil || intent.Resume.Input.Interruption == nil || intent.Resume.Input.Interruption.InterruptionReason != "" || intent.Resume.Input.Interruption.RequestID != plan.RequestID {
				t.Fatal("authorized retry lost the latest unknown cause")
			}
			start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
			d.submit(start)
			receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: protocol.LifecycleStart})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(receipt)
			if d.run.Attempts[input.Binding.Task].ID != original.ID {
				t.Fatal("cause metadata created a new logical execution")
			}
			return
		}
		if intent.Resume == nil || intent.Resume.Input == nil || intent.Resume.Input.Interruption == nil || intent.Resume.Input.Interruption.InterruptionReason != cause || intent.Resume.Input.Interruption.RequestID != plan.RequestID {
			t.Fatal("native resume lost actual interruption evidence")
		}

		d.reopen()
		again, _ := d.f.engine.Load()
		if !reflect.DeepEqual(snap, again) {
			t.Fatal("reopen rewrote cause evidence")
		}
		receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: protocol.LifecycleStart})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(receipt)
		if d.run.Attempts[input.Binding.Task].ID != original.ID {
			t.Fatal("cause metadata created a new logical execution")
		}
	}
}

func TestInvalidInterruptedRowsDoNotReplaceProgress(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	d.reviewReady()
	id, input := d.action("engine.qa.execution.v3")
	d.receipt(id)
	report := executionFor(input)
	report.Records[0].CaseID = "unknown"
	event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "invalid-partial", InterruptionReason: "worker interrupted", Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Checkpoint: true, Tool: "command", Environment: "clean"}}}
	if a := d.submit(event); a.Status != "REJECTED_INVALID_RESULT" {
		t.Fatal(a)
	}
	if d.run.Validation.Waves[0].Targets[input.Binding.Task.String()].Execution != nil {
		t.Fatal("invalid partial rows became authoritative")
	}
	d.drive()
	id, input = d.action("engine.qa.execution.v3")
	before, _ := d.f.engine.Load()
	event.ID = d.eventID()
	event.Result.ActionID = id
	event.Result.Outcome = protocol.OutcomePass
	event.Result.FailureClass = ""
	event.Result.Validation = nil
	_, _, err := d.f.Submit(event)
	after, _ := d.f.engine.Load()
	if err == nil || !reflect.DeepEqual(before, after) {
		t.Fatal("ordinary result forged interruption evidence")
	}
}

func TestLatestRuntimeCauseSurvivesInvalidRowsAndEndsAtRedo(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	d.reviewReady()
	id, input := d.action("engine.qa.execution.v3")
	candidate := d.run.Validation.Waves[0].Candidate
	for i, cause := range []string{"first tool exit", "second tool exit with incomplete evidence"} {
		d.receipt(id)
		payload := &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: cause, InterruptionReason: cause}
		if i == 1 {
			bad := executionFor(input)
			bad.Records[0].CaseID = "unknown"
			payload.Validation = &protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &bad, Checkpoint: true, Tool: "command", Environment: "clean"}
		}
		d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: payload})
		d.drive()
		if i == 0 {
			id, input = d.action("engine.qa.execution.v3")
		}
	}
	decideValidation(d, d.validationAsk("RETRY"), "approve")
	d.drive()
	id, input = d.action("engine.qa.execution.v3")
	if input.Interruption == nil || input.Interruption.InterruptionReason != "second tool exit with incomplete evidence" {
		t.Fatal("controller rejection classification selected a stale cause")
	}
	original := input.Binding.Task
	confirmValidationWork(d, "")
	d.reopen()
	d.drive()
	_, carried := d.action("engine.qa.execution.v3")
	if carried.Binding.Task != original || !reflect.DeepEqual(carried.Interruption, input.Interruption) {
		t.Fatal("explicit CARRY lost original interruption evidence")
	}
	// A later confirmed revision explicitly replaces this QA duty.
	confirmValidationDocument(d, id, nil, validationDocument+"\n\n")
	d.reopen()
	d.drive()
	if len(d.run.AdapterIntents) > 0 {
		materializeUnchangedCandidate(d, candidate)
	}
	d.drive()
	_, redone := d.action("engine.qa.execution.v3")
	if redone.Binding.Task == original || redone.Interruption != nil {
		t.Fatal("REDO inherited another task's interruption cause")
	}
}
