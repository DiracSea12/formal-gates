package facade

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// codexThreadRequest invokes Codex's native local protocol without a turn/start
// or fresh thread request. A resume probe loads only the specified thread; its
// native missing-rollout error is different from a timeout or transport failure.
func codexThreadRequest(executable, method, identity string, binding *protocol.HostRuntimeContext, workspace string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "app-server", "--stdio", "--disable", "enable_mcp_apps", "--disable", "multi_agent", "--disable", "multi_agent_v2")

	if err := runtimeCommandContext(cmd, binding); err != nil {
		return nil, err
	}
	cmd.Dir = workspace
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	enc := json.NewEncoder(input)
	scan := bufio.NewScanner(output)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	request := func(id int, method string, params any) (json.RawMessage, error) {
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for scan.Scan() {
			var response struct {
				ID int `json:"id"`
			}
			if json.Unmarshal(scan.Bytes(), &response) == nil && response.ID == id {
				return append(json.RawMessage(nil), scan.Bytes()...), nil
			}
		}
		return nil, fmt.Errorf("native %s response unavailable: scan=%v context=%v", method, scan.Err(), ctx.Err())
	}
	initialized, err := request(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "formal-gates", "version": "4"}})
	if err != nil {
		return nil, err
	}
	var initResponse struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(initialized, &initResponse) != nil || initResponse.Error != nil {
		return nil, fmt.Errorf("native initialization failed: %s", initialized)
	}
	configResponse, err := request(2, "config/read", map[string]any{"includeLayers": false})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(configResponse, &envelope) != nil {
		return nil, fmt.Errorf("native configuration response invalid")
	}
	config, err := runtimeToolConfig(binding.Provider, envelope.Result)
	if err != nil {
		return nil, err
	}
	return request(3, method, map[string]any{"threadId": identity, "config": config})
}

func (f *Facade) developerRecoveryPath(action string, interrupted bool) string {
	job := f.codexJob(action)
	if interrupted {
		job = filepath.Join(job, "resume")
	}
	return filepath.Join(job, "developer-recovery.json")
}

func (f *Facade) observeDeveloperRecovery(run Run, x protocol.RegularExecution, interrupted bool) (protocol.DeveloperRecovery, error) {
	return f.observeNativeDeveloperRecovery(run, x, interrupted, "thread/resume", func(identity string, binding *protocol.HostRuntimeContext, workspace string) (json.RawMessage, error) {
		return codexThreadRequest(binding.Executable, "thread/resume", identity, binding, workspace)
	})
}

func (f *Facade) observeNativeDeveloperRecovery(run Run, x protocol.RegularExecution, interrupted bool, method string, probe func(string, *protocol.HostRuntimeContext, string) (json.RawMessage, error)) (protocol.DeveloperRecovery, error) {
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
	receipt := protocol.DeveloperRecovery{Claim: x.Claim, Identity: x.Identity, StoppedAction: old.Claim.ActionID, ProcessID: process.PID, WorkspaceView: view.Digest, NativeMethod: method}
	receipt.NativeResponse, err = probe(receipt.Identity, x.Claim.Context, p.Resources["development"])
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
