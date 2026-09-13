package facade

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func continuousPreparationFixture(t *testing.T) (*Facade, *fixtureRuntime) {
	f, a := continuousFixture(t)
	a.answer = func(q nativeTurn) any {
		c := q.Claim
		r := protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "local protocol fixture only", Findings: []protocol.ReviewFinding{}}
		if c.Task.Step == "start-readiness" {
			r.GranularityReview = &protocol.DevelopmentPlan{RequirementRevision: c.RequirementRevision, Rationale: "single fixture batch", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "hello", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "hello"}}, Dependencies: []string{}, Interfaces: "stdout", StateBoundary: "one command", Risks: "wrong output", DefinitionOfDone: "hello", Validation: "run", Recovery: "same identity", Handoff: "none", ContextCost: "tiny"}}}
		}
		return r
	}
	for _, step := range []string{"product-review", "start-readiness"} {
		result, err := f.DriveContinuous(context.Background())
		if err != nil || result.Boundary != "OPERATOR" {
			t.Fatalf("setup %s: %s %v %v", step, result.Boundary, result.Blocked, err)
		}
		preparationSubmit(t, f, protocol.RegularEvent{Operation: "ADJUDICATE", Adjudication: &protocol.ReviewAdjudication{Action: step, ResultDigest: result.Run.Regular.Reviews[step].CurrentDigest, Findings: []protocol.FindingAdjudication{}, CompletenessEvidence: "deterministic fixture, not real review evidence"}})
	}
	run, _ := f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", PlanDigest: run.Regular.Plan.Digest, Reason: "single fixture batch"})
	facadeChoice(t, f, protocol.RegularRequest{Action: "route", Route: &protocol.RegularRoute{Kind: "custom", QA: []string{"blackbox"}}})
	// Set up the real Git workspaces before installing a native-completion
	// fault. The shared fault point also covers Git; interrupting that earlier
	// operation would test preparation recovery, not independent native turns.
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range run.Executions {
		if !x.Finished {
			t.Logf("issued %s claimed=%v guard=%s next=%s", x.Claim.Task.Step, x.Claimed, run.Regular.Guard.Status, run.Next.Kind)
		}
	}
	return f, a
}

func TestContinuousIndependentActionsRunTogether(t *testing.T) {
	f, a := continuousPreparationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	var turns atomic.Int32
	a.answer = func(q nativeTurn) any {
		turns.Add(1)
		started <- string(q.Claim.Task.Step)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return map[string]string{"message": "fixture interrupted before result admission"}
	}
	f.fault = func(p persistence.FaultPoint) error {
		if p == persistence.FaultExecuteAfter {
			return fmt.Errorf("fixture interruption preserves completed native journal")
		}
		return nil
	}
	finished := make(chan ContinuousResult, 1)
	failure := make(chan error, 1)
	go func() { r, e := f.DriveContinuous(ctx); finished <- r; failure <- e }()
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case step := <-started:
			seen[step] = true
			t.Logf("native turn entered: %s", step)
		case result := <-finished:
			close(release)
			t.Fatalf("consumer returned before independent turns: seen=%v boundary=%s blocked=%v error=%v", seen, result.Boundary, result.Blocked, <-failure)
		case <-ctx.Done():
			close(release)
			t.Fatalf("independent issued actions did not overlap: seen=%v", seen)
		}
	}
	close(release)
	result := <-finished
	err := <-failure
	if err != nil || !seen["development-worker"] || !seen["blackbox-design"] || turns.Load() != 2 || len(result.Blocked) != 2 {
		t.Fatalf("seen=%v turns=%d blocked=%v err=%v", seen, turns.Load(), result.Blocked, err)
	}
}

func TestContinuousQARecoveryDoesNotInterruptActiveDeveloper(t *testing.T) {
	f, a := continuousPreparationFixture(t)
	developerEntered, release := make(chan struct{}), make(chan struct{})
	var blackboxTurns atomic.Int32
	a.answer = func(q nativeTurn) any {
		if q.Claim.Task.Step == "development-worker" {
			close(developerEntered)
			<-release
		} else {
			blackboxTurns.Add(1)
		}
		c := q.Claim
		return protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "RUNTIME_ERROR", Message: "deterministic fixture runtime failure"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type outcome struct {
		result ContinuousResult
		err    error
	}
	returned := make(chan outcome, 1)
	go func() { r, err := f.DriveContinuous(ctx); returned <- outcome{r, err} }()
	select {
	case <-developerEntered:
	case <-ctx.Done():
		close(release)
		t.Fatal("developer not started")
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case got := <-returned:
			t.Fatalf("consumer exited with independent developer active: %s %v", got.result.Boundary, got.err)
		case <-deadline.C:
			t.Fatal("QA recovery did not become visible")
		case <-time.After(50 * time.Millisecond):
			r, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			if r.Next.Kind == "ASK" && (r.Regular.Ask == nil || r.Regular.Ask.Kind == "qa-recovery") {
				if blackboxTurns.Load() == 0 {
					t.Fatal("no blackbox turn executed")
				}
				close(release)
				select {
				case got := <-returned:
					if got.err != nil {
						t.Fatal(got.err)
					}
				case <-ctx.Done():
					t.Fatal("consumer did not settle after developer released")
				}
				return
			}
		}
	}
}
