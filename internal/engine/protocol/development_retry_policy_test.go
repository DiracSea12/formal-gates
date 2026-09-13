package protocol

import (
	"formal-gates/internal/engine/runtime"
	"testing"
)

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
