package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

type dshRuntimeAdapter struct{}

func (dshRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.DSHExecutable
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if executable == "dsh" {
		if _, err := exec.LookPath(executable); err != nil {
			candidate := filepath.Join(home, ".local", "bin", "dsh")
			if _, err := os.Stat(candidate); err == nil {
				executable = candidate
			}
		}
	}
	dshHome := os.Getenv("DSH_HOME")
	if dshHome == "" {
		dshHome = filepath.Join(home, ".dsh")
	}
	dshHome, err = filepath.Abs(dshHome)
	if err != nil {
		return nil, err
	}
	return captureStreamContext("deepseek-harness", executable, map[string]string{"DSH_HOME": dshHome})
}
func (dshRuntimeAdapter) bindResources(binding *protocol.HostRuntimeContext, root string) {
	bindStreamResources(binding, root)
}
func (dshRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if err := validateStreamContext(binding, "deepseek-harness"); err != nil {
		return err
	}
	if !filepath.IsAbs(binding.Environment["DSH_HOME"]) {
		return fmt.Errorf("DSH_CONFIG: original DSH_HOME missing")
	}
	return nil
}
func (dshRuntimeAdapter) transcript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	return streamTranscript(identity, binding)
}
func (dshRuntimeAdapter) toolConfig(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (a dshRuntimeAdapter) execute(f *Facade, action string, resume bool) (Run, error) {
	return f.executeNativeTransport(action, resume, a)
}
func dshRoleTools(step string, noTools bool) []string {
	if noTools {
		return []string{}
	}
	read := []string{"Read", "read", "read_file", "Glob", "glob", "Grep", "grep", "list_directory"}
	switch step {
	case "development-worker", "repair-worker", "whitebox-design":
		return append(read, "Bash", "bash", "shell", "Write", "write", "write_file", "Edit", "edit", "edit_file", "apply_patch")
	case "product-review", "start-readiness", "blackbox-execution", "whitebox-execution":
		return []string{}
	default:
		return read
	}
}
func prepareDSHProcess(binding *protocol.HostRuntimeContext, job, workspace, root, runID, actionID, step string, noTools bool) (*exec.Cmd, error) {
	if err := os.MkdirAll(filepath.Join(job, "native-requests"), 0700); err != nil {
		return nil, err
	}
	pluginPath := filepath.Join(job, "native-tools.mjs")
	if err := os.WriteFile(pluginPath, dshNativeOperationPlugin, 0600); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	patch, err := dshOperationPatch(pluginPath, executable, root, runID, actionID, filepath.Join(job, "native-requests"), step, "Use only the engine-issued operation boundary. Preserve each requestId across recovery.", dshRoleTools(step, noTools))
	if err != nil {
		return nil, err
	}
	plugin := patch[0].(map[string]any)["insert"].([]any)[0].(map[string]any)
	plugin["config"].(map[string]any)["exposeOperation"] = !noTools
	mode := "read-only"
	if !noTools && (step == "development-worker" || step == "repair-worker" || step == "whitebox-design") {
		mode = "workspace-write"
	}
	patch = append(patch,
		map[string]any{"id": "sandbox-policy", "config": map[string]any{"mode": mode, "workspaceRoot": workspace}},
		map[string]any{"id": "approval", "config": map[string]any{"policy": "never"}},
		map[string]any{"id": "permission", "config": map[string]any{"defaultPreset": mode, "presets": map[string]any{
			"read-only":       map[string]any{"sandbox": "read-only", "approval": "never"},
			"workspace-write": map[string]any{"sandbox": "workspace-write", "approval": "never"},
		}}},
	)
	patchPath := filepath.Join(job, "native-profile.json")
	if err := writeJSONAtomic(patchPath, patch); err != nil {
		return nil, err
	}
	cmd, err := dshCommand(binding.Executable, workspace, patchPath)
	if err != nil {
		return nil, err
	}
	if err := runtimeCommandContext(cmd, binding); err != nil {
		return nil, err
	}
	return cmd, nil
}
func (dshRuntimeAdapter) bridge(f *Facade, run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any) (string, error) {
	return f.runNativeBridgeTurns(run, claim, resume, job, workspace, prompt, schema, func(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, coverage bool) (string, error) {
		identity := ""
		if resume {
			identity = claim.Identity
			if identity == "" {
				return "", fmt.Errorf("DSH_BINDING: original native session required")
			}
		}
		cmd, err := prepareDSHProcess(claim.Context, job, workspace, f.root, run.RunID, claim.ActionID, string(claim.Task.Step), coverage || nativeTextOnlyRole(string(claim.Task.Step)))
		if err != nil {
			return "", err
		}
		encoded, _ := json.Marshal(schema)
		result, err := runNativeStreamProcess(context.Background(), cmd, job, claim.ActionID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
			return runDSHSession(in, out, job, workspace, identity, prompt+"\nReturn one JSON object matching this schema:\n"+string(encoded), func(identity string) error { return f.attachStreamIdentity(claim, identity) }, dshSessionOptions{Model: run.Regular.Settings.Values.Model, Effort: run.Regular.Settings.Values.ReasoningEffort})
		})
		if err == nil && coverage {
			err = finishStreamCoverage(job, result.Identity, claim.Context, "native-events.jsonl")
		}
		return result.Identity, err
	})
}
func (dshRuntimeAdapter) finish(f *Facade, action string, claim protocol.ExecutionClaim, identity, job string, waitErr error) (Run, error) {
	return f.finishStreamNative(action, claim, identity, job, "native-events.jsonl", waitErr)
}
func (a dshRuntimeAdapter) reconcile(f *Facade, action string, x protocol.RegularExecution) (Run, bool, error) {
	return f.reconcileStreamNative(action, x, func(job string) (string, error) {
		file, err := os.Open(filepath.Join(job, "stdout.jsonl"))
		if err != nil {
			return "", err
		}
		defer file.Close()
		decoder := json.NewDecoder(file)
		for {
			var raw struct {
				ID     *int `json:"id"`
				Result struct {
					ID string `json:"sessionId"`
				} `json:"result"`
			}
			if err := decoder.Decode(&raw); err != nil {
				return "", err
			}
			if raw.ID != nil && *raw.ID == 3 && raw.Result.ID != "" {
				return raw.Result.ID, nil
			}
		}
	}, func(identity, job string, err error) (Run, error) {
		return a.finish(f, action, x.Claim, identity, job, err)
	})
}
func (dshRuntimeAdapter) observeRecovery(f *Facade, run Run, x protocol.RegularExecution, interrupted bool) (protocol.DeveloperRecovery, error) {
	return f.observeNativeDeveloperRecovery(run, x, interrupted, "session/resume", func(identity string, binding *protocol.HostRuntimeContext, workspace string) (json.RawMessage, error) {
		job := filepath.Join(filepath.Dir(f.developerRecoveryPath(x.Claim.ActionID, interrupted)), "native-probe")
		cmd, err := prepareDSHProcess(binding, job, workspace, f.root, run.RunID, x.Claim.ActionID, string(x.Claim.Task.Step), true)
		if err != nil {
			return nil, err
		}
		return probeACPRecovery(cmd, job, x.Claim.ActionID, workspace, identity, "session/resume")
	})
}
func (dshRuntimeAdapter) runModel(ctx context.Context, run Run, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	invoke := func() error {
		binding := op.Request.Binding.Context
		cmd, err := prepareDSHProcess(binding, job, job, binding.Environment["FORMAL_GATES_ENGINE_ROOT"], run.RunID, op.Request.Binding.ActionID, "model-run", true)
		if err != nil {
			return err
		}
		_, err = runNativeStreamProcess(ctx, cmd, job, op.ID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
			return runDSHSession(in, out, job, job, "", nativeModelPrompt(op.Request.Prompt), func(string) error { return nil }, dshSessionOptions{Model: run.Regular.Settings.Values.Model, Effort: run.Regular.Settings.Values.ReasoningEffort})
		})
		return err
	}
	return completeStreamModel(op, job, "native-events.jsonl", result, invoke())
}
func (dshRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	return observeStreamModel(op, job, "native-events.jsonl", result)
}
