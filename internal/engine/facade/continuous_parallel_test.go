package facade

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func continuousPreparationFixture(t *testing.T, extraBatches ...protocol.DevelopmentBatch) (*Facade, *fixtureRuntime) {
	return continuousPreparationFixtureQA(t, []string{"blackbox"}, extraBatches...)
}

func continuousPreparationFixtureQA(t *testing.T, modes []string, extraBatches ...protocol.DevelopmentBatch) (*Facade, *fixtureRuntime) {
	t.Helper()
	f, a := continuousFixture(t)
	a.answer = func(q nativeTurn) any {
		c := q.Claim
		r := protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "local protocol fixture only", Findings: []protocol.ReviewFinding{}}
		if c.Task.Step == "start-readiness" {
			r.GranularityReview = &protocol.DevelopmentPlan{RequirementRevision: c.RequirementRevision, Rationale: "single fixture batch", Batches: []protocol.DevelopmentBatch{{ID: "B1", Task: "hello", Subtasks: []protocol.DevelopmentSubtask{{ID: "S1", Task: "hello"}}, Dependencies: []string{}, Interfaces: "stdout", StateBoundary: "one command", Risks: "wrong output", DefinitionOfDone: "hello", Validation: "run", Recovery: "same identity", Handoff: "none", ContextCost: "tiny"}}}
			r.GranularityReview.Batches = append(r.GranularityReview.Batches, extraBatches...)
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
	route := &protocol.RegularRoute{Kind: "custom", QA: modes}
	if len(modes) == 0 {
		route.Gates = []string{"implementation-quality-gate"}
	}
	facadeChoice(t, f, protocol.RegularRequest{Action: "route", Route: route})
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
	var developerStarted sync.Once
	a.answer = func(q nativeTurn) any {
		if q.Claim.Task.Step == "development-worker" {
			developerStarted.Do(func() { close(developerEntered) })
			<-release
		} else {
			blackboxTurns.Add(1)
		}
		c := q.Claim
		r := protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "RUNTIME_ERROR", Message: "deterministic fixture runtime failure"}
		if c.Task.Step == "development-worker" {
			writePreparationFile(t, q.Workspace, "hello.sh", "printf hello\n")
			cmd := exec.Command("sh", "hello.sh")
			cmd.Dir = q.Workspace
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != "hello" {
				t.Errorf("actual developer command: %q %v", out, err)
			}
			r.Status = "PASS"
			r.Message = "actual shell command completed"
			r.Handoff = "no remaining fixture work"
			r.Manifest = []string{"hello.sh"}
			r.Validation = []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "sh hello.sh", Observation: string(out), Result: "PASS"}}
		}
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	deadline := time.NewTimer(2 * time.Minute)
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
					if got.result.Run.Regular.Preparation.Commits["B1"].After == "" {
						t.Fatalf("released developer did not commit: errors=%+v blocked=%v", got.result.Run.Regular.PreparationErrors, got.result.Blocked)
					}
				case <-ctx.Done():
					t.Fatal("consumer did not settle after developer released")
				}
				return
			}
		}
	}
}

func TestContinuousLockHeldDoesNotInterruptIndependentActions(t *testing.T) {
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
	var completions atomic.Int32
	f.fault = func(p persistence.FaultPoint) error {
		if p == persistence.FaultExecuteAfter {
			if completions.Add(1) == 1 {
				return fmt.Errorf("source submission: %w", &persistence.LockHeldError{Path: "fixture-state.lock"})
			}
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
	for _, reason := range result.Blocked {
		if strings.Contains(reason, "LOCK_HELD") {
			t.Fatalf("transient source lock became terminal: %s", reason)
		}
	}
	if err != nil || len(seen) != 2 || turns.Load() != 2 || len(result.Blocked) != 2 {
		t.Fatalf("seen=%v turns=%d blocked=%v err=%v", seen, turns.Load(), result.Blocked, err)
	}
}

func TestContinuousStartsIndependentDevelopmentIncrement(t *testing.T) {
	extra := protocol.DevelopmentBatch{ID: "B2", Task: "independent output", Subtasks: []protocol.DevelopmentSubtask{{ID: "S2", Task: "independent output"}}, Interfaces: "output", StateBoundary: "independent file", Risks: "wrong output", DefinitionOfDone: "output checked", Validation: "fixture", Recovery: "same identity", Handoff: "none", ContextCost: "small"}
	f, a := continuousPreparationFixtureQA(t, nil, extra)
	started := make(chan nativeTurn, 2)
	release := make(chan struct{})
	var turns atomic.Int32
	a.answer = func(q nativeTurn) any {
		if q.Claim.Task.Step != "development-worker" {
			c := q.Claim
			return protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "fixture gate", Findings: []protocol.ReviewFinding{}}
		}
		n := turns.Add(1)
		if n > 1 {
			started <- q
			<-release
		}
		c := q.Claim
		subtask := "S1"
		if n == 2 {
			subtask = "S2"
		}
		return protocol.PreparationResult{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Status: "PASS", Manifest: []string{}, Handoff: "No unresolved decisions", Validation: []protocol.BatchValidation{{SubtaskID: subtask, Procedure: "local adapter fixture", Observation: "empty fixture increment", Result: "PASS"}}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var released sync.Once
	defer released.Do(func() { close(release) })
	type outcome struct {
		result ContinuousResult
		err    error
	}
	returned := make(chan outcome, 1)
	consume := func() { r, err := f.DriveContinuous(ctx); returned <- outcome{r, err} }
	go consume()
	var primary nativeTurn
	select {
	case primary = <-started:
	case got := <-returned:
		t.Fatalf("no primary: %s %v", got.result.Boundary, got.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	run := preparationSubmit(t, f, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "development-increment", Target: "B1", Meaning: "preserved", Reason: "independent correction", Increment: &protocol.DevelopmentIncrement{Kind: "optimize", BatchID: "B1"}}})
	select {
	case got := <-returned:
		if got.err != nil || got.result.Boundary != "ASK" {
			t.Fatalf("decision boundary: %s %v", got.result.Boundary, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ask := run.Regular.Ask
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "DECIDE", Decision: &protocol.RegularDecision{RequestID: ask.ID, Token: ask.Token, Digest: ask.Digest, Choice: "confirm", Reason: "confirmed fixture correction"}})
	go consume()
	select {
	case other := <-started:
		if other.Claim.Task == primary.Claim.Task || other.Workspace == primary.Workspace {
			t.Fatal("executor did not start an isolated independent task")
		}
	case got := <-returned:
		t.Fatalf("no independent developer: %s %v %v", got.result.Boundary, got.result.Blocked, got.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	released.Do(func() { close(release) })
	select {
	case got := <-returned:
		if got.err != nil || len(got.result.Blocked) > 0 || len(got.result.Run.Regular.Preparation.Commits) != 2 || turns.Load() != 3 {
			t.Fatalf("completion: turns=%d commits=%v blocked=%v err=%v", turns.Load(), got.result.Run.Regular.Preparation.Commits, got.result.Blocked, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
