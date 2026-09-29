package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestInterruptedExecutionKeepsCauseAndCompletedRows(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "normal"
		if held {
			name = "held"
		}
		t.Run(name, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox")
			d.reviewReady()
			id, input := d.action("engine.qa.execution")
			original := d.run.Attempts[input.Binding.Task]
			full := executionFor(input)
			partial := full
			partial.Records = append([]coverage.ExecutionRecord(nil), full.Records[:1]...)
			partial.Binding.ActualCaseIDs = []string{partial.Records[0].CaseID}
			partial.Records[0].Result = coverage.ExecutedFail
			partial.Records[0].Observation = "command returned an unexpected exit code"
			partial.Records[0].Comparison = "approved oracle requires rejection with documented output"
			cause := "工具退出 137\nwhile opening the next case fixture  "
			if held {
				cause = "fixture unavailable\n临时目录不存在  "
			} else {
				d.receipt(id)
			}
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomeRuntimeError, FailureClass: "TRANSIENT_ENGINE_ERROR", PayloadDigest: "interrupted-with-complete-case", InterruptionReason: cause, Validation: &protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Execution: &partial, Checkpoint: true, Tool: "command", Environment: "clean-project"}}}
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
			retry, next := d.action("engine.qa.execution")
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
			d.business(retry, protocol.ValidationResult{Binding: next.Binding, Status: "PASS", Execution: &remaining, Tool: "command", Environment: "clean-project"})
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
	id, input := d.action("engine.qa.execution")
	d.receipt(id)
	start, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStart)
	d.submit(start)
	original := d.run.Attempts[input.Binding.Task]
	for _, cause := range []string{"worker tool interrupted\n尚未完成下一项  ", ""} {
		plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR", InterruptionReason: cause}, emptyFingerprint())
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
			if intent.Terminate == nil || latest == nil || latest.RequestID != plan.RequestID || latest.InterruptionReason != "" {
				t.Fatal("latest unknown cause was replaced or exhausted attempt resumed")
			}
			stop, _ := protocol.NewLifecycleEventEvent(d.eventID(), DefaultProvider, id, protocol.LifecycleStop)
			d.submit(stop)
			receipt, err := protocol.NewAgentHostActionReceiptEvent(d.eventID(), intent.ActionID, intent.Operation, DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", protocol.LifecycleEvidence{Identity: id, Event: protocol.LifecycleStop})
			if err != nil {
				t.Fatal(err)
			}
			d.submit(receipt)
			d.drive()
			if !d.run.EndedAttempts[input.Binding.Task].RetryExhausted {
				t.Fatal("unknown cause bypassed finite recovery allowance")
			}
			d.validationAsk("RETRY")
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
	id, input := d.action("engine.qa.execution")
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
	id, input = d.action("engine.qa.execution")
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
	id, input := d.action("engine.qa.execution")
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
			id, input = d.action("engine.qa.execution")
		}
	}
	decideValidation(d, d.validationAsk("RETRY"), "approve")
	d.drive()
	id, input = d.action("engine.qa.execution")
	if input.Interruption == nil || input.Interruption.InterruptionReason != "second tool exit with incomplete evidence" {
		t.Fatal("controller rejection classification selected a stale cause")
	}
	original := input.Binding.Task
	confirmValidationWork(d, "")
	d.reopen()
	d.drive()
	_, carried := d.action("engine.qa.execution")
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
	_, redone := d.action("engine.qa.execution")
	if redone.Binding.Task == original || redone.Interruption != nil {
		t.Fatal("REDO inherited another task's interruption cause")
	}
}
