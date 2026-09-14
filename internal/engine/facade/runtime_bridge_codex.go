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
	"unicode/utf8"
)

// runCodexBridge hosts Codex's typed dynamic tool. QA execution workers have no shell
// tool: their only target operation is the frozen argv consumed by the adapter.
// The same client owns fresh and resumed turns; the provider never chooses a
// workflow step or rewrites the action/attempt/candidate binding.
const codexInputCharacters = 1_048_576

func (codexRuntimeAdapter) validatePrompt(prompt string) error {
	if n := utf8.RuneCountInString(prompt); n > codexInputCharacters {
		return fmt.Errorf("INPUT_TOO_LARGE: Codex text input has %d characters; limit is %d", n, codexInputCharacters)
	}
	return nil
}

func (codexRuntimeAdapter) turn(q nativeTurn) (string, error) {
	if err := (codexRuntimeAdapter{}).validatePrompt(q.Prompt); err != nil {
		return "", err
	}
	claim, resume, job, workspace, prompt, schema := q.Claim, q.Resume, q.Job, q.Workspace, q.Prompt, q.Schema
	toolDescription := q.ToolDescription
	ctx := context.Background()
	args := []string{"app-server", "--stdio", "--disable", "multi_agent", "--disable", "multi_agent_v2", "--disable", "enable_mcp_apps", "-c", `web_search="disabled"`}

	cmd := exec.CommandContext(ctx, claim.Context.Executable, args...)
	if err := runtimeCommandContext(cmd, claim.Context); err != nil {
		return "", err
	}
	cmd.Dir = workspace
	isolateCodexProcess(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := os.Create(filepath.Join(job, "stderr.log"))
	if err != nil {
		return "", err
	}
	defer stderr.Close()
	cmd.Stderr = stderr
	journal, err := os.Create(filepath.Join(job, "stdout.jsonl"))
	if err != nil {
		return "", err
	}
	defer journal.Close()
	if err = cmd.Start(); err != nil {
		return "", err
	}
	defer func() { _ = in.Close(); _ = stopCodexProcess(cmd.Process.Pid); _ = cmd.Wait() }()
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err = writeJSONAtomic(filepath.Join(job, "process.json"), codexProcess{PID: cmd.Process.Pid, ActionID: claim.ActionID, StartedAt: startedAt}); err != nil {
		return "", err
	}
	enc := json.NewEncoder(in)
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	type message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	read := func() (message, error) {
		if !scan.Scan() {
			return message{}, fmt.Errorf("BRIDGE_UNAVAILABLE: native response unavailable: scan=%v context=%v", scan.Err(), ctx.Err())
		}
		raw := append([]byte(nil), scan.Bytes()...)
		if _, err := journal.Write(append(raw, '\n')); err != nil {
			return message{}, err
		}
		if err := journal.Sync(); err != nil {
			return message{}, err
		}
		var m message
		err := json.Unmarshal(raw, &m)
		return m, err
	}
	var queued []message
	request := func(id int, method string, params any) (json.RawMessage, error) {
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		for {
			m, err := read()
			if err != nil {
				return nil, err
			}
			if string(m.ID) == fmt.Sprint(id) {
				if len(m.Error) > 0 && string(m.Error) != "null" {
					return nil, fmt.Errorf("BRIDGE_UNAVAILABLE: %s: %s", method, m.Error)
				}
				return m.Result, nil
			}
			queued = append(queued, m)
		}
	}
	if _, err = request(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "formal-gates", "version": "5"}, "capabilities": map[string]any{"experimentalApi": true}}); err != nil {
		return "", err
	}
	if err = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "initialized"}); err != nil {
		return "", err
	}

	// The host setting's empty model means Codex's advertised default, just as
	// exec --ignore-user-config did. Do not import a personal app model selection
	// into this isolated workflow or silently replace an explicit model.
	model := q.Settings.Model
	effort := q.Settings.ReasoningEffort
	if model == "" {
		raw, err := request(4, "model/list", map[string]any{})
		if err != nil {
			return "", err
		}
		var catalog struct {
			Data []struct {
				Model   string `json:"model"`
				Default bool   `json:"isDefault"`
				Effort  string `json:"defaultReasoningEffort"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &catalog); err != nil {
			return "", err
		}
		for _, m := range catalog.Data {
			if m.Default {
				model = m.Model
				if effort == "" {
					effort = m.Effort
				}
				break
			}
		}
		if model == "" {
			return "", fmt.Errorf("BRIDGE_UNAVAILABLE: Codex did not advertise a default model")
		}
	}
	// Empty tables merge with host configuration. Explicitly disable each
	// configured server and plugin before either creating or resuming a thread.
	rawConfig, err := request(5, "config/read", map[string]any{"includeLayers": false, "cwd": workspace})
	if err != nil {
		return "", err
	}
	toolConfig, err := (codexRuntimeAdapter{}).toolConfig(rawConfig)
	if err != nil {
		return "", err
	}
	params := map[string]any{"cwd": workspace, "approvalPolicy": "never", "sandbox": "read-only", "config": toolConfig}
	if !q.NoTools && nativeRoleAccess(string(claim.Task.Step)) == nativeWorkspaceWrite {
		params["sandbox"] = "workspace-write"
	}

	if model != "" {
		params["model"] = model
	}
	method := "thread/start"
	if resume {
		method = "thread/resume"
		params["threadId"] = claim.Identity
	} else if q.allowsOperation() {
		// Dynamic tools are fixed at thread creation. Coverage starts without
		// workflow operations; subsequent whitebox authoring uses native file
		// and shell tools on that same thread, without the optional helper.
		params["dynamicTools"] = []any{nativeOperationTool(string(claim.Task.Step), toolDescription)}
	}
	raw, err := request(2, method, params)
	if err != nil {
		return "", err
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &started) != nil || started.Thread.ID == "" {
		return "", fmt.Errorf("UNKNOWN: native thread response has no identity")
	}
	identity := started.Thread.ID
	if resume && identity != claim.Identity {
		return identity, fmt.Errorf("INTENT_MISMATCH: native resumed identity differs")
	}
	if err = q.Attached(identity); err != nil {
		return identity, err
	}
	turn := map[string]any{"threadId": identity, "input": []any{map[string]any{"type": "text", "text": prompt, "text_elements": []any{}}}, "outputSchema": schema}
	if effort != "" {
		turn["effort"] = effort
	}
	if err := (codexRuntimeAdapter{}).validatePrompt(prompt); err != nil {
		return identity, err
	}
	if _, err = request(3, "turn/start", turn); err != nil {
		return identity, err
	}
	answer := ""
	for {
		var m message
		var err error
		if len(queued) > 0 {
			m = queued[0]
			queued = queued[1:]
		} else {
			m, err = read()
		}
		if err != nil {
			return identity, err
		}
		switch m.Method {
		case "item/tool/call":
			var call struct {
				Tool      string          `json:"tool"`
				CallID    string          `json:"callId"`
				ThreadID  string          `json:"threadId"`
				Arguments json.RawMessage `json:"arguments"`
			}
			callErr := json.Unmarshal(m.Params, &call)
			if callErr == nil && (call.Tool != "formal_gates_operation" || call.ThreadID != identity || call.CallID == "") {
				callErr = fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: unbound native tool")
			}
			var receipt any
			if callErr == nil && !q.allowsOperation() {
				// A resumed historical thread may still advertise its original
				// tool declaration. The current role must still permit execution.
				callErr = fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: workflow operations unavailable for this role")
			}
			if callErr == nil {
				receipt, callErr = q.Operation(identity, call.CallID, call.Arguments)
			}
			output, _ := json.Marshal(receipt)
			if callErr != nil {
				output, _ = json.Marshal(map[string]string{"error": callErr.Error()})
			}
			if err = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"success": callErr == nil, "contentItems": []any{map[string]any{"type": "inputText", "text": string(output)}}}}); err != nil {
				return identity, err
			}
		case "item/completed":
			var item struct {
				Item struct {
					Type  string `json:"type"`
					Text  string `json:"text"`
					Phase string `json:"phase"`
				} `json:"item"`
			}
			if json.Unmarshal(m.Params, &item) == nil && item.Item.Type == "agentMessage" {
				answer = item.Item.Text
			}
		case "turn/completed":
			var v struct {
				Turn struct {
					Status string          `json:"status"`
					Error  json.RawMessage `json:"error"`
				} `json:"turn"`
			}
			if json.Unmarshal(m.Params, &v) != nil || v.Turn.Status != "completed" {
				return identity, fmt.Errorf("native turn failed: %s", m.Params)
			}
			if answer == "" {
				return identity, fmt.Errorf("native turn completed without a final answer")
			}
			answerName := "answer.json"
			if err = os.WriteFile(filepath.Join(job, answerName), []byte(answer), 0600); err != nil {
				return identity, err
			}
			if err = writeJSONAtomic(filepath.Join(job, "process.json"), codexProcess{PID: cmd.Process.Pid, ActionID: claim.ActionID, Stopped: true, StartedAt: startedAt, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
				return identity, err
			}
			return identity, nil
		default:
			if len(m.ID) > 0 && m.Method != "" {
				if err = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "unsupported bridge request"}}); err != nil {
					return identity, err
				}
			}
		}
	}
}

// The first whitebox turn receives only requirements and production source, with
// native shell disabled. A durable operation receipt precedes the same identity's
// test-authoring turn, so existing tests cannot influence the coverage judgment.
