package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

const zcodeScriptBinding = "FORMAL_GATES_ZCODE_SCRIPT"

type zcodeRuntimeAdapter struct{}

func (zcodeRuntimeAdapter) capture(config settings.Values) (*protocol.HostRuntimeContext, error) {
	script := config.ZCodeScript
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
	return captureStreamContext("zcode", config.ZCodeExecutable, map[string]string{zcodeScriptBinding: script, "ZCODE_HOME": os.Getenv("ZCODE_HOME")})
}
func (zcodeRuntimeAdapter) bindResources(binding *protocol.HostRuntimeContext, root string) {
	bindStreamResources(binding, root)
}
func (zcodeRuntimeAdapter) validate(binding *protocol.HostRuntimeContext) error {
	if err := validateStreamContext(binding, "zcode"); err != nil {
		return err
	}
	if !filepath.IsAbs(binding.Environment[zcodeScriptBinding]) {
		return fmt.Errorf("ZCODE_CONFIG: original script binding missing")
	}
	_, err := os.Stat(binding.Environment[zcodeScriptBinding])
	return err
}
func (zcodeRuntimeAdapter) transcript(identity string, binding *protocol.HostRuntimeContext) (string, error) {
	return streamTranscript(identity, binding)
}
func (zcodeRuntimeAdapter) toolConfig(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}
func (a zcodeRuntimeAdapter) execute(f *Facade, action string, resume bool) (Run, error) {
	return f.executeNativeTransport(action, resume, a)
}
func zcodeRoleTools(step string, coverage bool) []string {
	if coverage || nativeTextOnlyRole(step) {
		return []string{}
	}
	tools := []string{"formal_gates_operation", "mcp__formal-gates__formal_gates_operation"}
	switch step {
	case "development-worker", "repair-worker", "whitebox-design":
		tools = append(tools, "Bash", "Read", "Write", "Edit", "Glob", "Grep")
	case "product-review", "start-readiness", "blackbox-execution", "whitebox-execution":
	default:
		tools = append(tools, "Read", "Glob", "Grep")
	}
	return tools
}
func (zcodeRuntimeAdapter) bridge(f *Facade, run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any) (string, error) {
	return f.runNativeBridgeTurns(run, claim, resume, job, workspace, prompt, schema, func(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, coverage bool) (string, error) {
		identity := ""
		if resume {
			identity = claim.Identity
			if identity == "" {
				return "", fmt.Errorf("ZCODE_BINDING: resume identity missing")
			}
		}
		session, err := zcodeSessionRequest(claim.ActionID, workspace, identity, zcodeRoleTools(string(claim.Task.Step), coverage))
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
		options := zcodeSessionOptions{EnforceMode: true, CaptureUsage: true, Model: run.Regular.Settings.Values.Model, Effort: run.Regular.Settings.Values.ReasoningEffort}
		if !coverage && !nativeTextOnlyRole(string(claim.Task.Step)) {
			options.PrepareTools = func(identity string) ([]any, error) {
				executable, err := os.Executable()
				if err != nil {
					return nil, err
				}
				return []any{map[string]any{"name": "formal-gates", "command": executable, "args": []string{"workflow", "drive", "--root", f.root, "--run-id", run.RunID, "--action-id", claim.ActionID, "--operation-mcp", "--native-identity", identity}, "env": []any{}}}, nil
			}
		}
		encoded, _ := json.Marshal(schema)
		result, err := runZCodeProcess(context.Background(), cmd, job, claim.ActionID, session, claim.ActionID+":input", prompt+"\nReturn one JSON object matching this schema:\n"+string(encoded), func(identity string) error { return f.attachStreamIdentity(claim, identity) }, options)
		if err == nil && coverage {
			err = finishStreamCoverage(job, result.Identity, claim.Context, "stdout.jsonl")
		}
		return result.Identity, err
	})
}
func (zcodeRuntimeAdapter) finish(f *Facade, action string, claim protocol.ExecutionClaim, identity, job string, waitErr error) (Run, error) {
	return f.finishStreamNative(action, claim, identity, job, "stdout.jsonl", waitErr)
}
func (a zcodeRuntimeAdapter) reconcile(f *Facade, action string, x protocol.RegularExecution) (Run, bool, error) {
	return f.reconcileStreamNative(action, x, func(job string) (string, error) {
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
	}, func(identity, job string, err error) (Run, error) {
		return a.finish(f, action, x.Claim, identity, job, err)
	})
}
func (zcodeRuntimeAdapter) observeRecovery(f *Facade, run Run, x protocol.RegularExecution, interrupted bool) (protocol.DeveloperRecovery, error) {
	return f.observeNativeDeveloperRecovery(run, x, interrupted, "session/resume", func(identity string, binding *protocol.HostRuntimeContext, workspace string) (json.RawMessage, error) {
		job := filepath.Join(filepath.Dir(f.developerRecoveryPath(x.Claim.ActionID, interrupted)), "native-probe")
		cmd, err := zcodeCommand(binding.Executable, binding.Environment[zcodeScriptBinding], workspace)
		if err != nil {
			return nil, err
		}
		if err := runtimeCommandContext(cmd, binding); err != nil {
			return nil, err
		}
		session, err := zcodeSessionRequest("recovery", workspace, identity, []string{})
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var response json.RawMessage
		_, err = runNativeStreamProcess(ctx, cmd, job, x.Claim.ActionID, func(in io.Reader, out io.WriteCloser) (nativeProcessResult, error) {
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
		return response, err
	})
}
func (zcodeRuntimeAdapter) runModel(ctx context.Context, run Run, op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) protocol.TaskOperationReceipt {
	invoke := func() error {
		session, err := zcodeSessionRequest(op.ID, job, "", []string{})
		if err != nil {
			return err
		}
		binding := op.Request.Binding.Context
		cmd, err := zcodeCommand(binding.Executable, binding.Environment[zcodeScriptBinding], job)
		if err != nil {
			return err
		}
		if err := runtimeCommandContext(cmd, binding); err != nil {
			return err
		}
		_, err = runZCodeProcess(ctx, cmd, job, op.ID, session, op.ID+":input", nativeModelPrompt(op.Request.Prompt), func(string) error { return nil }, zcodeSessionOptions{CaptureUsage: true, Model: run.Regular.Settings.Values.Model, Effort: run.Regular.Settings.Values.ReasoningEffort})
		return err
	}
	return completeStreamModel(op, job, "stdout.jsonl", result, invoke())
}
func (zcodeRuntimeAdapter) observeModel(op protocol.TaskOperation, job string, result protocol.TaskOperationReceipt) (protocol.TaskOperationReceipt, error) {
	return observeStreamModel(op, job, "stdout.jsonl", result)
}
