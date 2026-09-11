package facade

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

type workBuddyRuntimeAdapter struct{}

func (workBuddyRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.WorkBuddyExecutable
	if executable == "codebuddy" {
		if _, err := exec.LookPath(executable); err != nil {
			candidate := "/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy"
			if _, err := os.Stat(candidate); err == nil {
				executable = candidate
			}
		}
	}
	return captureStreamContext("workbuddy", executable, nil)
}
func (workBuddyRuntimeAdapter) bindResources(binding *protocol.HostRuntimeContext, root string) {
	bindStreamResources(binding, root)
}
func (workBuddyRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	return validateStreamContext(binding, "workbuddy")
}
func (workBuddyRuntimeAdapter) transcript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	return streamTranscript(identity, binding)
}
func (workBuddyRuntimeAdapter) toolConfig(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (a workBuddyRuntimeAdapter) execute(f *Facade, action string, resume bool) (Run, error) {
	return f.executeNativeTransport(action, resume, a)
}
func (workBuddyRuntimeAdapter) bridge(f *Facade, run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any) (string, error) {
	return f.runNativeBridgeTurns(run, claim, resume, job, workspace, prompt, schema, func(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, coverage bool) (string, error) {
		identity, err := f.runWorkBuddyBridge(context.Background(), run, claim, resume, job, workspace, prompt, schema, coverage)
		if err == nil && coverage {
			err = finishStreamCoverage(job, identity, claim.Context, "stdout.jsonl")
		}
		return identity, err
	})
}

func (workBuddyRuntimeAdapter) finish(f *Facade, action string, claim protocol.ExecutionClaim, identity, job string, waitErr error) (Run, error) {
	return f.finishStreamNative(action, claim, identity, job, "stdout.jsonl", waitErr)
}
func (a workBuddyRuntimeAdapter) reconcile(f *Facade, action string, x protocol.RegularExecution) (Run, bool, error) {
	return f.reconcileStreamNative(action, x, func(job string) (string, error) {
		file, err := os.Open(filepath.Join(job, "stdout.jsonl"))
		if err != nil {
			return "", err
		}
		defer file.Close()
		observed, err := observeWorkBuddyResult(file, "")
		if observed.Identity != "" {
			return observed.Identity, nil
		}
		return "", err
	}, func(identity, job string, err error) (Run, error) {
		return a.finish(f, action, x.Claim, identity, job, err)
	})
}
func (workBuddyRuntimeAdapter) observeRecovery(f *Facade, run Run, x protocol.RegularExecution, interrupted bool) (protocol.DeveloperRecovery, error) {
	return f.observeNativeDeveloperRecovery(run, x, interrupted, "session/load", func(identity string, binding *protocol.HostRuntimeContext, workspace string) (json.RawMessage, error) {
		job := filepath.Join(filepath.Dir(f.developerRecoveryPath(x.Claim.ActionID, interrupted)), "native-probe")
		if err := os.MkdirAll(job, 0700); err != nil {
			return nil, err
		}
		configPath := filepath.Join(job, "mcp.json")
		if err := writeJSONAtomic(configPath, map[string]any{"mcpServers": map[string]any{}}); err != nil {
			return nil, err
		}
		cmd := exec.Command(binding.Executable, "--acp", "--tools", "", "--strict-mcp-config", "--mcp-config", configPath, "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", "bypassPermissions")
		cmd.Dir = workspace
		if err := runtimeCommandContext(cmd, binding); err != nil {
			return nil, err
		}
		return probeACPRecovery(cmd, job, x.Claim.ActionID, workspace, identity, "session/load")
	})
}
func requestedNativeID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 15) | 64
	value[8] = (value[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}
func (workBuddyRuntimeAdapter) runModel(ctx context.Context, run Run, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	invoke := func() error {
		identity, err := requestedNativeID()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(job, 0700); err != nil {
			return err
		}
		configPath := filepath.Join(job, "mcp.json")
		if err := writeJSONAtomic(configPath, map[string]any{"mcpServers": map[string]any{}}); err != nil {
			return err
		}
		schema, _ := json.Marshal(nativeModelSchema())
		settings := run.Regular.Settings.Values
		cmd, err := workBuddySessionCommand(op.Request.Binding.Context.Executable, job, settings.Model, settings.ReasoningEffort, identity, false, schema, configPath)
		if err != nil {
			return err
		}
		if err := runtimeCommandContext(cmd, op.Request.Binding.Context); err != nil {
			return err
		}
		_, err = runWorkBuddyProcess(ctx, cmd, job, op.ID, identity, nativeModelPrompt(op.Request.Prompt), func(string) error { return nil })
		return err
	}
	return completeStreamModel(op, job, "stdout.jsonl", result, invoke())
}
func (workBuddyRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	return observeStreamModel(op, job, "stdout.jsonl", result)
}
