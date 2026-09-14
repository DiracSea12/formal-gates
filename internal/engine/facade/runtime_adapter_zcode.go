package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

const (
	zcodeScriptBinding = "FORMAL_GATES_ZCODE_SCRIPT"
)

type zcodeRuntimeAdapter struct{ streamRuntimeAdapter }

func (zcodeRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	script := config.NativeString("zcodeScript")
	if script == "" {
		script = "/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs"
	}
	script, err := filepath.Abs(script)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("ZCODE_CONFIG: CLI script unavailable: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("ZCODE_CONFIG: CLI script is a directory")
	}
	home := config.NativeString("zcodeHome")
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("ZCODE_CONFIG: native home directory unavailable at %s", home)
	}
	environment := map[string]string{zcodeScriptBinding: script, "HOME": home}
	if runtime.GOOS == "windows" {
		environment["USERPROFILE"] = home
	}
	if value := os.Getenv("ZCODE_HOME"); value != "" {
		environment["ZCODE_HOME"] = value
	}
	return captureStreamContext("zcode", config.NativeString("zcodeExecutable"), environment)
}
func (zcodeRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if err := validateStreamContext(binding, "zcode"); err != nil {
		return err
	}
	if !filepath.IsAbs(binding.Environment[zcodeScriptBinding]) {
		return fmt.Errorf("ZCODE_CONFIG: original script binding missing")
	}
	if _, err := os.Stat(binding.Environment[zcodeScriptBinding]); err != nil {
		return err
	}
	if home := binding.Environment["HOME"]; home == "" || !filepath.IsAbs(home) {
		return fmt.Errorf("ZCODE_CONFIG: original native home binding missing")
	} else if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return fmt.Errorf("ZCODE_CONFIG: original native home unavailable at %s", home)
	}
	return nil
}

// zcodeRoleDeniedTools preserves the native default tool set. The host's plan
// mode remains responsible for command-level read-only checks.
func zcodeRoleDeniedTools(step string, coverage bool) []string {
	denied := []string{"Agent", "Task", "Workflow", "ScheduleWakeup", "EnterWorktree", "ExitWorktree"}
	if coverage || nativeRoleAccess(step) != nativeWorkspaceWrite {
		denied = append(denied, "Write", "Edit", "NotebookEdit", "EnterPlanMode", "ExitPlanMode")
	}
	if !coverage && nativeRoleAccess(step) == nativeOperationOnly {
		denied = append(denied, "Bash")
	}
	if coverage || !nativeAllowsOperation(step) {
		denied = append(denied, "formal_gates_operation", "mcp__formal-gates__formal_gates_operation")
	}
	return denied
}

func zcodeRoleMode(step string, coverage bool) string {
	if coverage || nativeRoleAccess(step) == nativeReadOnly {
		return "plan"
	}
	return "yolo"
}
func (zcodeRuntimeAdapter) turn(q nativeTurn) (string, error) {
	claim, resume, job, workspace, prompt, schema, coverage := q.Claim, q.Resume, q.Job, q.Workspace, q.Prompt, q.Schema, q.NoTools

	identity := ""
	if resume {
		identity = claim.Identity
		if identity == "" {
			return "", fmt.Errorf("ZCODE_BINDING: resume identity missing")
		}
	}
	mode := zcodeRoleMode(string(claim.Task.Step), coverage)
	session, err := zcodeSessionRequest(claim.ActionID, workspace, identity, mode, zcodeRoleDeniedTools(string(claim.Task.Step), coverage))
	if err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(job, "session-request.json"), session); err != nil {
		return "", err
	}
	cmd, err := zcodeCommand(claim.Context.Executable, claim.Context.Environment[zcodeScriptBinding], workspace)
	if err != nil {
		return "", err
	}
	if err := runtimeCommandContext(cmd, claim.Context); err != nil {
		return "", err
	}
	options := zcodeSessionOptions{Mode: mode, CaptureUsage: true, Model: q.Settings.Model, Effort: q.Settings.ReasoningEffort}
	if q.allowsOperation() {
		options.PrepareTools = func(identity string) ([]any, error) {
			executable, err := os.Executable()
			if err != nil {
				return nil, err
			}
			env := []any{}
			for name, value := range engineToolEnvironment() {
				env = append(env, map[string]any{"name": name, "value": value})
			}
			return []any{map[string]any{"name": "formal-gates", "command": executable, "args": []string{"workflow", "drive", "--root", q.Root, "--run-id", q.RunID, "--action-id", claim.ActionID, "--operation-mcp", "--native-identity", identity}, "env": env}}, nil
		}
	}
	encoded, _ := json.Marshal(schema)
	result, err := runZCodeProcess(context.Background(), cmd, job, claim.ActionID, session, claim.ActionID+":input", prompt+"\nReturn one JSON object matching this schema:\n"+string(encoded), q.Attached, options)
	return result.Identity, err
}

func (zcodeRuntimeAdapter) probe(q nativeProbe) (string, json.RawMessage, error) {
	identity, binding, workspace, job := q.Identity, q.Context, q.Workspace, q.Job

	cmd, err := zcodeCommand(binding.Executable, binding.Environment[zcodeScriptBinding], workspace)
	if err != nil {
		return "session/resume", nil, err
	}
	if err := runtimeCommandContext(cmd, binding); err != nil {
		return "session/resume", nil, err
	}
	session, err := zcodeSessionRequest("recovery", workspace, identity, "yolo", []string{})
	if err != nil {
		return "session/resume", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var response json.RawMessage
	_, err = runNativeStreamProcess(ctx, cmd, job, q.ActionID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
		if err := json.NewEncoder(out).Encode(session); err != nil {
			return nativeProcessResult{}, err
		}
		decoder := json.NewDecoder(in)
		for {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return nativeProcessResult{}, err
			}
			var msg struct {
				ID    string          `json:"id"`
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				return nativeProcessResult{}, fmt.Errorf("ZCODE_PROTOCOL: malformed recovery response")
			}
			if msg.ID == "recovery" {
				response = raw
				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					return nativeProcessResult{}, fmt.Errorf("ZCODE_NATIVE: %s", msg.Error)
				}
				break
			}
		}
		return nativeProcessResult{Identity: identity, Answer: json.RawMessage(`{}`)}, nil
	})
	return "session/resume", response, err
}
func (zcodeRuntimeAdapter) runModel(ctx context.Context, config settings.Values, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	invoke := func() error {
		session, err := zcodeSessionRequest(op.ID, job, "", "yolo", []string{})
		if err != nil {
			return err
		}
		// A text-only model helper has no role tools or workspace operations.
		session.Params["toolAllowlist"] = []string{}
		binding := op.Request.Binding.Context
		cmd, err := zcodeCommand(binding.Executable, binding.Environment[zcodeScriptBinding], job)
		if err != nil {
			return err
		}
		if err := runtimeCommandContext(cmd, binding); err != nil {
			return err
		}
		_, err = runZCodeProcess(ctx, cmd, job, op.ID, session, op.ID+":input", nativeModelPrompt(op.Request.Prompt), func(string) error { return nil }, zcodeSessionOptions{CaptureUsage: true, Model: config.Model, Effort: config.ReasoningEffort})
		return err
	}
	return completeStreamModel(op, job, "stdout.jsonl", result, invoke())
}
func (zcodeRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	return observeStreamModel(op, job, "stdout.jsonl", result)
}

func init() { registerBuiltinRuntime("zcode", zcodeRuntimeAdapter{}) }

func (zcodeRuntimeAdapter) readIdentity(job, action string) (string, error) {
	input, err := os.Open(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return "", err
	}
	defer input.Close()
	decoder := json.NewDecoder(input)
	for {
		var raw struct {
			ID     string `json:"id"`
			Result struct {
				Session struct {
					ID string `json:"sessionId"`
				} `json:"session"`
			} `json:"result"`
		}
		if err := decoder.Decode(&raw); err != nil {
			return "", err
		}
		if raw.ID == action && raw.Result.Session.ID != "" {
			return raw.Result.Session.ID, nil
		}
	}
}
