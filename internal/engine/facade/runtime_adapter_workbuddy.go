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

type workBuddyRuntimeAdapter struct{ streamRuntimeAdapter }

func (workBuddyRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.NativeString("workbuddyExecutable")
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
func (workBuddyRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	return validateStreamContext(binding, "workbuddy")
}

func (workBuddyRuntimeAdapter) probe(q nativeProbe) (string, json.RawMessage, error) {
	identity, binding, workspace, job := q.Identity, q.Context, q.Workspace, q.Job

	if err := os.MkdirAll(job, 0700); err != nil {
		return "session/load", nil, err
	}
	configPath := filepath.Join(job, "mcp.json")
	if err := writeJSONAtomic(configPath, map[string]any{"mcpServers": map[string]any{}}); err != nil {
		return "session/load", nil, err
	}
	cmd := exec.Command(binding.Executable, "--acp", "--tools", "", "--strict-mcp-config", "--mcp-config", configPath, "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", "bypassPermissions")
	cmd.Dir = workspace
	if err := runtimeCommandContext(cmd, binding); err != nil {
		return "session/load", nil, err
	}
	raw, err := probeACPRecovery(cmd, job, q.ActionID, workspace, identity, "session/load")
	return "session/load", raw, err
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
func (workBuddyRuntimeAdapter) runModel(ctx context.Context, config settings.Values, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
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
		settings := config
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

func init() { registerBuiltinRuntime("workbuddy", workBuddyRuntimeAdapter{}) }

func (workBuddyRuntimeAdapter) readIdentity(job, action string) (string, error) {
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
}
