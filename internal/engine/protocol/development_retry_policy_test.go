package protocol

import (
	"formal-gates/internal/engine/runtime"
	"reflect"
	"testing"
)

func TestDeveloperValidationContinuationUsesStatusAndPreservesChecks(t *testing.T) {
	for _, status := range []string{"CONTINUE", "FAIL"} {
		t.Run(status, func(t *testing.T) {
			e := preparationEngine(t)
			gitReceipt(t, e, "base")
			x := currentExecution(t, e, "development-worker")
			claimContinuingDeveloper(t, e, x)
			raw := preparationResult(x)
			raw.Identity = continuityIdentity(x)
			raw.Status = status
			// Neither the word FAIL nor PASS decides completion or retry credit.
			// Only the typed result does; partial successful checks remain useful
			// evidence even when the remaining check has not completed or failed.
			raw.Message = "PASS and FAIL are fixture text; consult the typed status"
			raw.Validation = []BatchValidation{{SubtaskID: "S1", Procedure: "completed bounded check", Observation: "check passed; remaining validation is separate", Result: "PASS"}}
			submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &raw})
			settleContinuingDeveloper(t, e, x)
			snap, err := e.Load()
			if err != nil {
				t.Fatal(err)
			}
			p := snap.State.Regular.Preparation
			if len(p.Batches) != 0 || len(p.Commits) != 0 || p.Rework != 0 {
				t.Fatal("partial validation completed or restarted the Batch")
			}
			if !reflect.DeepEqual(snap.State.RegularExecutions[x.Claim.ActionID].PreparationResult, &raw) {
				t.Fatal("original checks, handoff or identity were rewritten")
			}
			next := currentExecution(t, e, "development-worker")
			if next.PreviousAction != x.Claim.ActionID || next.Claim.Task != x.Claim.Task || next.Identity != raw.Identity || next.Claim.Operation != "resume" {
				t.Fatal("validation continuation lost its original work/session")
			}
			credit := 0
			if status == "CONTINUE" {
				credit = 1
			}
			if continuationCredit(snap.State, x.Claim.Task) != credit {
				t.Fatal("message text overrode typed failure/continuation")
			}
		})
	}
}

func TestAcceptedContinuationsPreserveFailureBudgetAndLedger(t *testing.T) {
	e := preparationEngine(t)
	gitReceipt(t, e, "git-base")
	var task runtime.TaskKey
	for n := 1; n <= 5; n++ {
		x := currentExecution(t, e, "development-worker")
		task = x.Claim.Task
		if x.Number != n {
			t.Fatalf("dispatch number = %d, want %d", x.Number, n)
		}
		claimContinuingDeveloper(t, e, x)
		raw := preparationResult(x)
		raw.Identity = continuityIdentity(x)
		raw.Status, raw.Message = "CONTINUE", "next subtask remains"
		submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &raw})
		settleContinuingDeveloper(t, e, x)
	}
	snap, _ := e.Load()
	s := snap.State
	if continuationCredit(s, task) != 5 || preparationMax(s, task) != 8 {
		t.Fatal("continuations consumed failure allowance")
	}
	if s.Regular.Guard.UsedAttempts < 5 || len(s.Cost.Dispatches) < 5 {
		t.Fatal("continuation dispatch costs disappeared")
	}
	// A persisted exhaustion projection from the old policy must not block resume.
	at := s.Attempts[task]
	at.RetryExhausted = true
	s.Attempts[task] = at
	e.refreshRegular(s)
	if s.Attempts[task].RetryExhausted {
		t.Fatal("old exhaustion projection survived recomputation")
	}
	for n := 6; n <= 8; n++ {
		x := currentExecution(t, e, "development-worker")
		claimContinuingDeveloper(t, e, x)
		raw := preparationResult(x)
		raw.Identity = continuityIdentity(x)
		raw.Status, raw.Message = "FAIL", "actual self-test failure"
		if n == 6 {
			raw.Status, raw.Candidate = "CONTINUE", "invalid-candidate"
		}
		if n == 8 {
			raw.Status = "RUNTIME_ERROR"
		}

		submitRegular(t, e, RegularEvent{Operation: "PREPARATION_RESULT", PreparationResult: &raw})
		settleContinuingDeveloper(t, e, x)
	}
	snap, _ = e.Load()
	if !snap.State.Attempts[task].RetryExhausted || continuationCredit(snap.State, task) != 5 {
		t.Fatal("three failures did not exhaust allowance")
	}
}

func continuityIdentity(x RegularExecution) string {
	if x.Identity != "" {
		return x.Identity
	}
	return "native-" + x.Claim.ActionID
}
func claimContinuingDeveloper(t *testing.T, e *Engine, x RegularExecution) {
	submitRegular(t, e, RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: continuityIdentity(x), Status: "RUNNING"}})
}
func settleContinuingDeveloper(t *testing.T, e *Engine, x RegularExecution) {
	submitRegular(t, e, RegularEvent{Operation: "SOURCE", Source: &ExecutionSource{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: continuityIdentity(x), Status: "UNAVAILABLE", Reason: "fixture source"}})
}
