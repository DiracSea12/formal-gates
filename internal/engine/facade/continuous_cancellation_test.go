package facade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

// This native fixture emits identity/usage, then stays alive until its driver's
// input closes. Killing the driver leaves a real stale RUNNING process receipt.
func TestCancelledNativeProcess(t *testing.T) {
	if os.Getenv("CANCEL_FIXTURE_NATIVE") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("CANCEL_FIXTURE_STATE_FILE"), []byte(os.Getenv("CANCEL_FIXTURE_IDENTITY")), 0600); err != nil {
		os.Exit(4)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"version": 1, "identity": os.Getenv("CANCEL_FIXTURE_IDENTITY"), "answer": map[string]string{}, "usage": []int64{11, 3}})
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

type cancelledNativeFixture struct{ *fixtureRuntime }

func (a *cancelledNativeFixture) turn(q nativeTurn) (string, error) {
	cmd := exec.Command(q.Claim.Context.Executable, "-test.run=^TestCancelledNativeProcess$")
	cmd.Dir = q.Job
	cmd.Env = append(os.Environ(), "CANCEL_FIXTURE_NATIVE=1", "CANCEL_FIXTURE_IDENTITY="+fixtureSessionID(q.Claim.ActionID), "CANCEL_FIXTURE_STATE_FILE="+fixtureStateFile(q.Claim.Context, q.Claim.ActionID, ""))
	out, err := runNativeStreamProcess(context.Background(), cmd, q.Job, q.Claim.ActionID, func(in io.Reader, _ io.WriteCloser) (nativeProcessResult, error) {
		var raw json.RawMessage
		if err := json.NewDecoder(in).Decode(&raw); err != nil {
			return nativeProcessResult{}, err
		}
		r, err := a.decode(raw)
		if err != nil {
			return nativeProcessResult{}, err
		}
		if err := q.Attached(r.Identity); err != nil {
			return nativeProcessResult{}, err
		}
		_, err = io.Copy(io.Discard, in)
		return nativeProcessResult{Identity: r.Identity, Answer: r.Answer}, err
	})
	return out.Identity, err
}

func TestCancelledNativeOwner(t *testing.T) {
	if os.Getenv("CANCEL_FIXTURE_OWNER") != "1" {
		return
	}
	a := &cancelledNativeFixture{&fixtureRuntime{id: os.Getenv("CANCEL_FIXTURE_PROVIDER"), executableOption: os.Getenv("CANCEL_FIXTURE_EXEC_OPTION"), stateOption: os.Getenv("CANCEL_FIXTURE_STATE_OPTION"), serverVersion: 1, adapterVersion: 1}}
	if err := registerNativePlatform(a.descriptor(), a); err != nil {
		t.Fatal(err)
	}
	f, err := Open(os.Getenv("CANCEL_FIXTURE_ROOT"), "fixture-public")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ExecuteNative(os.Getenv("CANCEL_FIXTURE_ACTION"), false); err != nil {
		t.Fatal(err)
	}
}

func TestContinuousCancelledDeveloperSettlesBeforeCurrentWork(t *testing.T) {
	f, a := continuousPreparationFixture(t)
	x := facadeExecution(t, f, "development-worker")
	// Capture the original context using the ordinary platform registration.
	// The owner subprocess uses the same registration ID with a holding transport.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestCancelledNativeOwner$", "-test.v")
	child.Env = append(os.Environ(), "CANCEL_FIXTURE_OWNER=1", "CANCEL_FIXTURE_ROOT="+f.root, "CANCEL_FIXTURE_PROVIDER="+a.id, "CANCEL_FIXTURE_ACTION="+x.Claim.ActionID)
	// The subprocess must capture exactly the same configuration field names.
	child.Env = append(child.Env, "CANCEL_FIXTURE_EXEC_OPTION="+a.executableOption, "CANCEL_FIXTURE_STATE_OPTION="+a.stateOption)
	var childLog bytes.Buffer
	child.Stdout, child.Stderr = &childLog, &childLog
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var process codexProcess
	for {
		r, err := f.Show()
		if err != nil {
			t.Fatal(err)
		}
		x = r.Executions[x.Claim.ActionID]
		if x.Source != nil && x.Source.Status == "RUNNING" {
			data, err := os.ReadFile(filepath.Join(f.codexJob(x.Claim.ActionID), "process.json"))
			if err != nil || json.Unmarshal(data, &process) != nil {
				t.Fatal("missing process receipt", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("native fixture did not attach")
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer stopCodexProcess(process.PID)
	if process.Stopped || !codexProcessAlive(process.PID) || !x.Claimed || x.Finished {
		t.Fatal("fixture was not claimed and actually running")
	}
	before, _ := f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "requirement-change", Origin: "USER_INITIATED_CHANGE", Reason: "fixture clarification retains work and requires current independent QA"})
	doc := protocol.Document(before.Regular.Requirement.Path, []byte(before.Regular.Requirement.Text+"\nClarification: preserve the documented hello behavior.\n"))
	writePreparationFile(t, f.root, doc.Path, doc.Text)
	facadeChoice(t, f, protocol.RegularRequest{Action: "apply-requirement", Origin: "USER_INITIATED_CHANGE", Meaning: "changed", Document: &doc, Keep: []string{"product-review", "start-readiness"}, Impact: map[string]string{"product-review": "unaffected", "start-readiness": "unaffected", "development": "unaffected", "blackbox-review": "affected"}, Reason: "explicit fixture impact decision"})
	r, _ := f.Show()
	facadeChoice(t, f, protocol.RegularRequest{Action: "topology", PlanDigest: r.Regular.Plan.Digest, Reason: "retain single fixture batch"})
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	intentID := "hact:" + x.Claim.ActionID + ":terminate"
	if _, ok := snapshot.State.PendingHostActions[intentID]; !ok {
		t.Fatal("requirement change did not issue cancellation")
	}
	if !codexProcessAlive(process.PID) {
		t.Fatal("process stopped before requirement cancellation")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	for codexProcessAlive(process.PID) {
		select {
		case <-ctx.Done():
			t.Fatal("native process did not stop after owner died")
		case <-time.After(20 * time.Millisecond):
		}
	}
	job := f.codexJob(x.Claim.ActionID)
	data, _ := os.ReadFile(filepath.Join(job, "process.json"))
	if json.Unmarshal(data, &process) != nil || process.Stopped {
		t.Fatal("fixture must retain stale RUNNING receipt after actual stop")
	}
	journal, err := os.ReadFile(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	f, err = Open(f.root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	started := map[string]string{}
	var orderingErr error
	a.answer = func(q nativeTurn) any {
		current, readErr := f.Show()
		mu.Lock()
		if readErr != nil {
			orderingErr = readErr
		} else if cancelled := current.Executions[x.Claim.ActionID]; !cancelled.Finished || cancelled.Source == nil || cancelled.Source.Status != "STOPPED" || current.Cost.Dispatches[x.Claim.ActionID].OutputTokens != 3 {
			orderingErr = fmt.Errorf("current native turn started before cancellation source/cost settlement")
		}
		started[q.Claim.ActionID] = string(q.Claim.Task.Step)
		mu.Unlock()
		return map[string]string{"message": "fixture stops after actual current native process completion"}
	}
	f.fault = func(p persistence.FaultPoint) error {
		mu.Lock()
		called := len(started) > 0
		mu.Unlock()
		if p == persistence.FaultExecuteAfter && called {
			return fmt.Errorf("fixture current invocation completed; result admission intentionally interrupted")
		}
		return nil
	}
	result, err := f.DriveContinuous(ctx)
	if err != nil {
		t.Fatal(err)
	}
	old := result.Run.Executions[x.Claim.ActionID]
	if result.Blocked[x.Claim.ActionID] != "" || !old.Finished || old.Source == nil || old.Source.Status != "STOPPED" {
		t.Fatalf("cancelled execution did not settle: blocked=%v finished=%v source=%+v", result.Blocked, old.Finished, old.Source)
	}
	if !reflect.DeepEqual(old.Claim, x.Claim) || old.Identity != x.Identity || old.Number != x.Number || old.ResumeCount != x.ResumeCount || old.ResumeClaimed || old.ReplacementClaimed {
		t.Fatal("cancellation changed historical identity/claim/attempt")
	}
	if usage := result.Run.Cost.Dispatches[x.Claim.ActionID]; usage.TotalInputTokens != 11 || usage.OutputTokens != 3 {
		t.Fatalf("lost original usage: %+v", usage)
	}
	for id, cost := range before.Cost.Dispatches {
		if !reflect.DeepEqual(result.Run.Cost.Dispatches[id], cost) {
			t.Fatal("changed prior cost", id)
		}
	}
	for id, previous := range before.Executions {
		if previous.Finished && !reflect.DeepEqual(result.Run.Executions[id], previous) {
			t.Fatal("changed historical result", id)
		}
	}
	snapshot, err = f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, pending := snapshot.State.PendingHostActions[intentID]; pending || snapshot.State.HostActionReceipts[intentID].Status != protocol.HostActionStatusExecuted {
		t.Fatal("terminate not receipted")
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(replayed.Executions[x.Claim.ActionID], old) || !reflect.DeepEqual(replayed.Cost.Dispatches[x.Claim.ActionID], result.Run.Cost.Dispatches[x.Claim.ActionID]) {
		t.Fatal("drive repeated cancellation collection or cost settlement")
	}
	currentJournal, _ := os.ReadFile(filepath.Join(job, "stdout.jsonl"))
	if !bytes.Equal(journal, currentJournal) {
		t.Fatal("old native output changed")
	}
	if _, err := os.Stat(codexResumeJob(job, 1)); !os.IsNotExist(err) {
		t.Fatal("cancelled execution created resume directory")
	}
	mu.Lock()
	defer mu.Unlock()
	if orderingErr != nil {
		t.Fatal(orderingErr)
	}
	seen := map[string]bool{}
	for id, step := range started {
		if id == x.Claim.ActionID || result.Run.Executions[id].Claim.RequirementRevision != doc.Revision {
			t.Fatal("old task made a new native call")
		}
		data, err := os.ReadFile(filepath.Join(f.codexJob(id), "process.json"))
		var done struct {
			Stopped  bool `json:"stopped"`
			ExitCode *int `json:"exitCode"`
		}
		if err != nil || json.Unmarshal(data, &done) != nil || !done.Stopped || done.ExitCode == nil || *done.ExitCode != 0 {
			t.Fatal("current action did not actually complete a native invocation", id, err)
		}
		seen[step] = true
		if step == "development-worker" && result.Run.Executions[id].Number != x.Number+1 {
			t.Fatal("cancellation spent extra development attempts")
		}
	}
	if !seen["development-worker"] || !seen["blackbox-design"] || len(started) != 2 {
		for id, ex := range result.Run.Executions {
			if ex.Claim.Task.Step == "development-worker" {
				t.Logf("developer %s: claimed=%v finished=%v result=%+v source=%+v", id, ex.Claimed, ex.Finished, ex.PreparationResult, ex.Source)
			}
		}
		t.Logf("nextAsk=%+v ask=%+v guard=%s", result.Run.Next.Ask, result.Run.Regular.Ask, result.Run.Regular.Guard.Status)
		t.Fatalf("independent current work not started: %v boundary=%s blocked=%v", started, result.Boundary, result.Blocked)
	}
	t.Logf("old process %d stopped with stale journal; terminate settled, same identity/attempt, usage=11/3, old resume calls=0; current developer and independent blackbox native processes both exited 0", process.PID)
}
