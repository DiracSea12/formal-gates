package facade

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

func continuousFixture(t *testing.T) (*Facade, *fixtureRuntime) {
	t.Helper()
	suffix := strconv.FormatUint(fixtureRegistrationCounter.Add(1), 10)
	a := &fixtureRuntime{id: "continuous-fixture-" + suffix, executableOption: "continuousExecutable" + suffix, stateOption: "continuousState" + suffix, serverVersion: 1, adapterVersion: 1}
	if err := registerNativePlatform(a.descriptor(), a); err != nil {
		t.Fatal(err)
	}
	return startPlatformFixture(t, a), a
}

func TestContinuousPublicConsumptionAndRestart(t *testing.T) {
	f, a := continuousFixture(t)
	var turns atomic.Int32
	a.answer = func(q nativeTurn) any {
		turns.Add(1)
		c := q.Claim
		return protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "deterministic fixture; not independent review", Findings: []protocol.ReviewFinding{}}
	}
	interrupted := errors.New("fixture interruption after native completion")
	f.fault = func(p persistence.FaultPoint) error {
		if p == persistence.FaultExecuteAfter {
			return interrupted
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := f.DriveContinuous(ctx)
	if err != nil || first.Boundary != "BLOCKED" || len(first.Blocked) != 1 || turns.Load() != 1 {
		t.Fatalf("first=%+v err=%v turns=%d", first.Blocked, err, turns.Load())
	}
	var id string
	for key := range first.Blocked {
		id = key
	}
	original, err := os.ReadFile(filepath.Join(f.codexJob(id), "stdout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.DriveContinuous(ctx)
	if err != nil || recovered.Boundary != string(decision.KindOperator) || turns.Load() != 1 {
		t.Fatalf("restart boundary=%s calls=%d blocked=%v err=%v", recovered.Boundary, turns.Load(), recovered.Blocked, err)
	}
	x := recovered.Run.Executions[id]
	if !x.Finished || x.Source == nil || x.Source.Status != "STOPPED" || recovered.Run.Cost.Dispatches[id].OutputTokens != 3 {
		t.Fatal("original result/source not settled exactly once")
	}
	current, err := os.ReadFile(filepath.Join(f.codexJob(id), "stdout.jsonl"))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("restart repeated native side effects", err)
	}
	again, err := f.DriveContinuous(ctx)
	if err != nil || again.Run.Revision != recovered.Run.Revision || turns.Load() != 1 {
		t.Fatal("operator boundary replay changed state", err)
	}
}

func TestContinuousLeaseAndExternalBoundary(t *testing.T) {
	f := facadePreparation(t, nil)
	x := facadeExecution(t, f, "development-worker")
	facadeChoice(t, f, protocol.RegularRequest{Action: "executor-handoff", Target: x.Claim.ActionID, Reason: "fixture user delegates this batch externally", Handoff: &protocol.ExecutorHandoff{To: protocol.ExecutorTarget{Kind: "external", Provider: "chatgpt", Label: "fixture author"}}})
	before, _ := f.Show()
	release, err := coordination.AcquirePath(filepath.Join(f.stateDir, "executor.lock"), "fixture owner")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.DriveContinuous(context.Background())
	release()
	if err == nil {
		t.Fatal("two consumers acquired the same lease")
	}
	result, err := f.DriveContinuous(context.Background())
	if err != nil || result.Boundary != "EXTERNAL_EXECUTOR" || len(result.External) != 1 {
		t.Fatalf("%s %v %v", result.Boundary, result.Blocked, err)
	}
	if result.Run.Revision != before.Revision || result.Run.Executions[result.External[0]].Claimed {
		t.Fatal("consumer claimed external work or changed its engine identity")
	}
}

func TestContinuousUserAskInterruptsTransientWait(t *testing.T) {
	f, a := continuousFixture(t)
	x := facadeExecution(t, f, "product-review")
	release, err := coordination.AcquirePath(f.codexJob(x.Claim.ActionID)+".lock", "existing native owner fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var captures, turns atomic.Int32
	a.answer = func(q nativeTurn) any {
		turns.Add(1)
		return nil
	}
	retryStopped := make(chan struct{})
	a.beforeCapture = func() error {
		// Two captures precede the first per-action lock refusal. The next
		// capture is a retry of that waiter, so the Ask arrives after waiting
		// is established, without sleep-based ordering or a native model call.
		switch captures.Add(1) {
		case 3:
			preparationSubmit(t, f, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "fixture user requests a change while another native owner holds the action"}})
		case 4:
			// Ask can return while this retry is still active. Stop the fixture
			// before its lock/filesystem work so TempDir cleanup cannot race it.
			close(retryStopped)
			return errors.New("fixture retry stopped after user Ask")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	defer func() {
		select {
		case <-retryStopped:
		case <-ctx.Done():
			t.Error("fixture retry did not stop before cleanup")
		}
	}()
	result, err := f.DriveContinuous(ctx)
	if err != nil || result.Boundary != "ASK" || result.Run.Regular.Ask == nil || result.Run.Executions[x.Claim.ActionID].Claimed || turns.Load() != 0 {
		t.Fatalf("unanswered Ask did not stop the waiter: boundary=%s captures=%d turns=%d blocked=%v error=%v", result.Boundary, captures.Load(), turns.Load(), result.Blocked, err)
	}
	if result.Run.Regular.RequirementImpact != nil {
		t.Fatal("consumer invented a user impact decision")
	}
}

func TestContinuousUserAskReturnsWhileAdapterIsActive(t *testing.T) {
	f, a := continuousFixture(t)
	entered, release, nativeFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var turns atomic.Int32
	a.answer = func(q nativeTurn) any {
		turns.Add(1)
		close(entered)
		<-release
		c := q.Claim
		return protocol.RawReviewCandidate{ActionID: c.ActionID, AttemptID: c.AttemptID, Provider: c.Provider, RequirementRevision: c.RequirementRevision, Candidate: c.Candidate, Action: string(c.Task.Step), Status: "PASS", Complete: true, Message: "local protocol fixture only", Findings: []protocol.ReviewFinding{}}
	}
	f.fault = func(point persistence.FaultPoint) error {
		if point == persistence.FaultExecuteAfter {
			close(nativeFinished)
			return errors.New("fixture leaves completed native journal for later reconciliation")
		}
		return nil
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
	case <-entered:
	case <-ctx.Done():
		close(release)
		t.Fatal("fixture adapter did not start")
	}
	defer func() {
		close(release)
		select {
		case <-nativeFinished:
		case <-time.After(90 * time.Second):
			t.Error("released local adapter did not finish its native process")
		}
	}()
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "REQUEST", Request: &protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "fixture user changes requirements while an authorized adapter is active"}})
	select {
	case got := <-returned:
		if got.err != nil || got.result.Boundary != "ASK" || got.result.Run.Regular.Ask == nil || got.result.Run.Regular.RequirementImpact != nil {
			t.Fatalf("active adapter hid or answered the user Ask: %s %v", got.result.Boundary, got.err)
		}
		// Reopening an unanswered Ask neither waits for the old native call nor
		// claims a replacement. It also does not invent a user disposition.
		again, err := f.DriveContinuous(ctx)
		if err != nil || again.Boundary != "ASK" || again.Run.Revision != got.result.Run.Revision || turns.Load() != 1 {
			t.Fatalf("Ask replay changed authorized work: %s turns=%d error=%v", again.Boundary, turns.Load(), err)
		}
	case <-time.After(20 * time.Second):
		cancel()
		<-returned
		t.Fatal("user Ask was hidden until the active adapter completed")
	}
}
