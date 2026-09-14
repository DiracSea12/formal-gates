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

type dshRuntimeAdapter struct{ streamRuntimeAdapter }

func (dshRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	executable := config.NativeString("dshExecutable")
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
func (dshRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if err := validateStreamContext(binding, "deepseek-harness"); err != nil {
		return err
	}
	if !filepath.IsAbs(binding.Environment["DSH_HOME"]) {
		return fmt.Errorf("DSH_CONFIG: original DSH_HOME missing")
	}
	return nil
}

// Native sandbox policy governs shell writes; only prohibited capabilities are
// removed from the host's default tool set.
func dshRoleDeniedTools(step string, coverage bool) []string {
	denied := []string{"Task", "Agent", "subagent", "agent", "spawn_agent", "EnterWorktree"}
	if coverage || nativeRoleAccess(step) != nativeWorkspaceWrite {
		denied = append(denied, "Write", "write", "write_file", "Edit", "edit", "edit_file", "apply_patch")
	}
	if !coverage && nativeRoleAccess(step) == nativeOperationOnly {
		denied = append(denied, "Bash", "bash", "shell")
	}
	return denied
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
	patch, err := dshOperationPatch(pluginPath, executable, root, runID, actionID, filepath.Join(job, "native-requests"), step, "Use only the engine-issued operation boundary. Preserve each requestId across recovery.", dshRoleDeniedTools(step, noTools))
	if err != nil {
		return nil, err
	}
	plugin := patch[0].(map[string]any)["insert"].([]any)[0].(map[string]any)
	plugin["config"].(map[string]any)["exposeOperation"] = !noTools && nativeAllowsOperation(step)
	plugin["config"].(map[string]any)["disableTools"] = step == "model-run"
	mode := "read-only"
	if !noTools && nativeRoleAccess(step) == nativeWorkspaceWrite {
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
func (dshRuntimeAdapter) turn(q nativeTurn) (string, error) {
	claim, resume, job, workspace, prompt, schema, coverage := q.Claim, q.Resume, q.Job, q.Workspace, q.Prompt, q.Schema, q.NoTools

	identity := ""
	if resume {
		identity = claim.Identity
		if identity == "" {
			return "", fmt.Errorf("DSH_BINDING: original native session required")
		}
	}
	cmd, err := prepareDSHProcess(claim.Context, job, workspace, q.Root, q.RunID, claim.ActionID, string(claim.Task.Step), coverage)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(schema)
	result, err := runNativeStreamProcess(context.Background(), cmd, job, claim.ActionID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
		return runDSHSession(in, out, job, workspace, identity, prompt+"\nReturn one JSON object matching this schema:\n"+string(encoded), q.Attached, dshSessionOptions{Model: q.Settings.Model, Effort: q.Settings.ReasoningEffort})
	})
	return result.Identity, err
}

func (dshRuntimeAdapter) probe(q nativeProbe) (string, json.RawMessage, error) {
	identity, binding, workspace, job := q.Identity, q.Context, q.Workspace, q.Job

	cmd, err := prepareDSHProcess(binding, job, workspace, q.Root, q.RunID, q.ActionID, q.Step, true)
	if err != nil {
		return "session/resume", nil, err
	}
	raw, err := probeACPRecovery(cmd, job, q.ActionID, workspace, identity, "session/resume")
	return "session/resume", raw, err
}
func (dshRuntimeAdapter) runModel(ctx context.Context, config settings.Values, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	invoke := func() error {
		binding := op.Request.Binding.Context
		cmd, err := prepareDSHProcess(binding, job, job, binding.Environment["FORMAL_GATES_ENGINE_ROOT"], op.Request.RunID, op.Request.Binding.ActionID, "model-run", true)
		if err != nil {
			return err
		}
		_, err = runNativeStreamProcess(ctx, cmd, job, op.ID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
			return runDSHSession(in, out, job, job, "", nativeModelPrompt(op.Request.Prompt), func(string) error { return nil }, dshSessionOptions{Model: config.Model, Effort: config.ReasoningEffort})
		})
		return err
	}
	return completeStreamModel(op, job, "native-events.jsonl", result, invoke())
}
func (dshRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	return observeStreamModel(op, job, "native-events.jsonl", result)
}

func init() { registerBuiltinRuntime("deepseek-harness", dshRuntimeAdapter{}) }

func (dshRuntimeAdapter) readIdentity(job, action string) (string, error) {
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
}

func (dshRuntimeAdapter) source(identity, job string, binding *protocol.HostRuntimeContext) (string, error) {
	return appendStreamTranscript(job, identity, "native-events.jsonl", binding)
}
