package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// codexThreadRequest invokes Codex's native local protocol without a turn/start
// or fresh thread request. A resume probe loads only the specified thread; its
// native missing-rollout error is different from a timeout or transport failure.

func (f *Facade) developerRecoveryPath(action string, interrupted bool) string {
	job := f.codexJob(action)
	if interrupted {
		job = filepath.Join(job, "resume")
	}
	return filepath.Join(job, "developer-recovery.json")
}

func (f *Facade) observeNativeDeveloperRecovery(run Run, x protocol.RegularExecution, interrupted bool, adapter nativeExecutionTransport) (protocol.DeveloperRecovery, error) {
	old := x
	if x.PreviousAction != "" && !interrupted {
		old = run.Executions[x.PreviousAction]
	}
	var data []byte
	var err error
	for {
		job := f.codexJob(old.Claim.ActionID)
		if old.ResumeClaimed {
			job = filepath.Join(job, "resume")
		}
		data, err = os.ReadFile(filepath.Join(job, "process.json"))
		// An unavailable probe creates no model process. The next authorized
		// attempt reconciles the last actual process, retaining both probe facts.
		if !os.IsNotExist(err) || old.PreviousAction == "" || old.Recovery == nil || old.Recovery.Outcome() == "RESUMABLE" {
			break
		}
		old = run.Executions[old.PreviousAction]
	}
	if err != nil {
		return protocol.DeveloperRecovery{}, fmt.Errorf("UNKNOWN: previous developer process receipt unavailable: %w", err)
	}
	var process codexProcess
	if json.Unmarshal(data, &process) != nil || process.ActionID != old.Claim.ActionID || process.PID <= 0 {
		return protocol.DeveloperRecovery{}, fmt.Errorf("UNKNOWN: previous developer process binding differs")
	}
	if !process.Stopped && codexProcessAlive(process.PID) {
		return protocol.DeveloperRecovery{}, fmt.Errorf("HOST_ACTION_RUNNING: original developer is still running")
	}
	p := run.Regular.Preparation
	if p.Operation != nil {
		return protocol.DeveloperRecovery{}, fmt.Errorf("UNKNOWN: Git side effects require reconciliation")
	}
	head, err := gitprep.Head(p.Resources["development"])
	if err != nil || head != p.DevelopmentSnapshot {
		return protocol.DeveloperRecovery{}, fmt.Errorf("UNKNOWN: development workspace HEAD differs: %v", err)
	}
	view, err := gitprep.WorkingView(p.Resources["development"], head)
	if err != nil {
		return protocol.DeveloperRecovery{}, err
	}
	if old.Source == nil || old.Source.Status == "RUNNING" {
		path, err := findBoundTranscript(old.Identity, old.Claim.Context)
		if err != nil {
			return protocol.DeveloperRecovery{}, err
		}
		if err = f.submitSource(protocol.ExecutionSource{ActionID: old.Claim.ActionID, AttemptID: old.Claim.AttemptID, Provider: old.Claim.Provider, Identity: old.Identity, TranscriptPath: path, Status: "STOPPED"}); err != nil {
			return protocol.DeveloperRecovery{}, err
		}
	}
	receipt := protocol.DeveloperRecovery{Claim: x.Claim, Identity: x.Identity, StoppedAction: old.Claim.ActionID, ProcessID: process.PID, WorkspaceView: view.Digest}
	receipt.NativeMethod, receipt.NativeResponse, err = adapter.probe(nativeProbe{
		Context: x.Claim.Context, Root: f.root, RunID: run.RunID,
		Job:       filepath.Join(filepath.Dir(f.developerRecoveryPath(x.Claim.ActionID, interrupted)), "native-probe"),
		Workspace: p.Resources["development"], ActionID: x.Claim.ActionID,
		Step: string(x.Claim.Task.Step), Identity: receipt.Identity})
	if err != nil {
		receipt.TransportError = err.Error()
	}
	path := f.developerRecoveryPath(x.Claim.ActionID, interrupted)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return receipt, err
	}
	if err = writeJSONAtomic(path, receipt); err != nil {
		return receipt, err
	}
	_, _, err = f.submitAdapterRegular("recovery:"+x.Claim.ActionID, protocol.RegularEvent{Operation: "DEVELOPER_RECOVERY", DeveloperRecovery: &receipt})
	return receipt, err
}

func (f *Facade) verifyDeveloperRecovery(event protocol.Event) error {
	if event.Regular == nil || event.Regular.DeveloperRecovery == nil {
		return nil
	}
	receipt := event.Regular.DeveloperRecovery
	run, err := f.Show()
	if err != nil {
		return err
	}
	x := run.Executions[receipt.Claim.ActionID]
	data, err := os.ReadFile(f.developerRecoveryPath(receipt.Claim.ActionID, x.ResumeClaimed))
	if err != nil {
		return fmt.Errorf("DEVELOPMENT_CONTINUITY: native adapter recovery evidence is required: %w", err)
	}
	var actual protocol.DeveloperRecovery
	if json.Unmarshal(data, &actual) != nil || gitprep.Digest(actual) != gitprep.Digest(*receipt) {
		return fmt.Errorf("DEVELOPMENT_CONTINUITY: recovery differs from the native adapter receipt")
	}
	return nil
}
