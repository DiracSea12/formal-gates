//go:build qastructure && !windows

package validate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The local native peer executes the configured hook and MCP server. Its blocked
// notification reflects the real hook response; these are protocol fixtures,
// not evidence that an installed Codex host honors hooks.
func TestCodexHookCanaryBindsNativeCallAndHostDecision(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = filepath.Join("..", "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	peer := filepath.Join(dir, "codex-peer")
	writeExecutableFixture(t, peer, codexHookPeer)
	for _, mode := range []string{"honors-deny", "text-only-then-call", "client-failure", "unrelated", "failed-turn", "local-deny-only", "ignores-deny", "missing-call-identity", "multiple-targets", "wrong-turn", "wrong-feedback", "wrong-source", "text-only"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOOK_PEER_MODE", mode)
			worktree := t.TempDir()
			if mode == "ignores-deny" {
				cmd := exec.Command("git", "init", "--quiet", worktree)
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("isolated repository: %v %s", err, output)
				}
			}
			summary, result := CodexHookCanary(CodexHookCanaryOptions{Worktree: worktree, CodexCommand: peer, Binary: binary, KeepTemp: true})
			pass := mode == "honors-deny" || mode == "text-only-then-call"
			if (summary.Status == "PASS") != pass || result.OK() != pass {
				t.Fatalf("mode %s: %+v; %+v", mode, summary, result)
			}
			wantAttempts := 1
			if mode == "text-only" || mode == "text-only-then-call" {
				wantAttempts = 2
			}
			if summary.Attempts != wantAttempts {
				t.Fatalf("missing native attempt: %+v", summary)
			}
			wantThread := "thread-" + summary.Case + "-" + filepath.Base(filepath.Dir(summary.PayloadDir))
			if pass && (summary.ThreadID != wantThread || summary.TurnID != "turn-"+wantThread || summary.ToolUseID != "call-"+wantThread || summary.PreToolUsePayloadCount != 1 || summary.ExitCode != 0 || summary.MarkerExists) {
				t.Fatalf("proof binding: %+v", summary)
			}
			if mode == "ignores-deny" {
				body, err := os.ReadFile(filepath.Join(filepath.Dir(summary.PayloadDir), "workspace", "marker.go"))
				if err != nil || string(body) != "package canary\n" || !summary.MarkerExists {
					t.Fatalf("negative control must apply the program patch: %q %v %+v", body, err, summary)
				}
				body, err = os.ReadFile(filepath.Join(filepath.Dir(summary.PayloadDir), "target-call.json"))
				var fact struct {
					Request struct {
						Method string
						Params struct {
							Name      string
							Arguments map[string]any
						}
					}
					Result struct{ IsError bool }
				}
				if err != nil || json.Unmarshal(body, &fact) != nil || fact.Request.Method != "tools/call" || fact.Request.Params.Name != "apply_patch" || fact.Request.Params.Arguments == nil || len(fact.Request.Params.Arguments) != 0 || fact.Result.IsError {
					t.Fatalf("original handler call evidence differs: %s %v", body, err)
				}
			}
			if pass && isFile(filepath.Join(filepath.Dir(summary.PayloadDir), "target-call.json")) {
				t.Fatal("blocked handler executed")
			}
		})
	}
}

const codexHookPeer = `package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "app-server" || os.Args[2] != "--stdio" {
		panic("native entry differs")
	}
	var directory, hook string
	for _, a := range os.Args[3:] {
		if strings.HasPrefix(a, "mcp_servers.hook_canary=") {
			m := regexp.MustCompile("args=\\[\"harness\",\"codex-hook-target\",(\".*\")\\]}").FindStringSubmatch(a)
			if len(m) != 2 {
				panic("MCP args differ")
			}
			directory, _ = strconv.Unquote(m[1])
		}
		if strings.HasPrefix(a, "hooks.PreToolUse=") {
			if !strings.HasPrefix(a, "hooks.PreToolUse=[{matcher=\".*\",hooks=") {
				panic("hook matcher differs")
			}
			p := strings.Index(a, "command=") + 8
			q := strings.LastIndex(a, "}]}]")
			hook, _ = strconv.Unquote(a[p:q])
		}
	}
	if directory == "" || hook == "" {
		panic("missing original registration")
	}
	var request struct{ Binary, Workspace, Prompt string }
	body, _ := os.ReadFile(filepath.Join(directory, "request.json"))
	if json.Unmarshal(body, &request) != nil {
		panic("request")
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	if hook != quote(request.Binary)+" hook decide --provider codex --record-dir "+quote(filepath.Join(directory, "payloads")) {
		panic("configured hook differs")
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	send := func(method string, p any) { encoder.Encode(map[string]any{"method": method, "params": p}) }
	mode := os.Getenv("HOOK_PEER_MODE")
	thread := "thread-" + filepath.Base(filepath.Dir(directory)) + "-" + filepath.Base(directory)
	turn, call := "turn-"+thread, "call-"+thread
	for {
		var frame struct {
			ID     any
			Method string
			Params map[string]any
		}
		if decoder.Decode(&frame) != nil {
			return
		}
		if frame.ID == nil {
			continue
		}
		result := any(map[string]any{})
		switch frame.Method {
		case "initialize":
		case "thread/start":
			if frame.Params["cwd"] != request.Workspace || frame.Params["approvalPolicy"] != "never" || frame.Params["sandbox"] != "danger-full-access" {
				panic("thread input differs")
			}
			result = map[string]any{"thread": map[string]any{"id": thread}}
		case "turn/start":
			stateBody, err := os.ReadFile(filepath.Join(request.Workspace, ".gates", "engine", "hook-canary", "state.json"))
			if err != nil {
				panic(err)
			}
			var state map[string]any
			json.Unmarshal(stateBody, &state)
			if state["content"].(map[string]any)["ownerSession"] != thread {
				panic("owner differs")
			}
			if frame.Params["threadId"] != thread {
				panic("turn input differs")
			}
			encoder.Encode(map[string]any{"id": frame.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
			discovery := exec.Command(request.Binary, "harness", "codex-hook-target", directory)
			discovery.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2024-11-05\"}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
			listing, err := discovery.Output()
			if err != nil {
				panic(err)
			}
			d := json.NewDecoder(bytes.NewReader(listing))
			var init, list map[string]any
			d.Decode(&init)
			d.Decode(&list)
			tools := list["result"].(map[string]any)["tools"].([]any)
			if len(tools) != 1 {
				panic("tool registration differs")
			}
			schema := tools[0].(map[string]any)
			input := schema["inputSchema"].(map[string]any)
			if schema["name"] != "apply_patch" || input["additionalProperties"] != false || len(input["properties"].(map[string]any)) != 0 {
				panic("tool schema differs")
			}
			if mode != "text-only" && !(mode == "text-only-then-call" && filepath.Base(directory) == "invocation-1") {
				payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "mcp__hook_canary__apply_patch", "session_id": thread, "turn_id": turn, "tool_use_id": call, "cwd": request.Workspace, "tool_input": map[string]any{}}
				if mode == "unrelated" {
					payload["tool_name"] = "exec"
					payload["tool_input"] = map[string]any{"command": "pwd"}
				}
				if mode == "missing-call-identity" {
					payload["tool_use_id"] = ""
				}
				raw, _ := json.Marshal(payload)
				cmd := exec.Command("/bin/sh", "-c", hook)
				cmd.Stdin = bytes.NewReader(raw)
				cmd.Dir = request.Workspace
				decision, err := cmd.Output()
				if err != nil {
					panic(err)
				}
				var response map[string]any
				json.Unmarshal(decision, &response)
				denied := response["decision"] == "block"
				status := "completed"
				entries := []any{}
				if denied && mode != "ignores-deny" && mode != "local-deny-only" {
					status = "blocked"
					reason := response["reason"].(string)
					if mode == "wrong-feedback" {
						reason += " other call"
					}
					entries = append(entries, map[string]any{"kind": "feedback", "text": reason})
				}
				nativeTurn, source := turn, "sessionFlags"
				if mode == "wrong-turn" {
					nativeTurn = "other-turn"
				}
				if mode == "wrong-source" {
					source = "project"
				}
				if mode != "local-deny-only" {
					send("hook/completed", map[string]any{"threadId": thread, "turnId": nativeTurn, "run": map[string]any{"id": "original-hook", "eventName": "preToolUse", "status": status, "source": source, "entries": entries}})
				}
				if mode == "multiple-targets" {
					payload["tool_use_id"] = call + "-another"
					raw, _ := json.Marshal(payload)
					cmd := exec.Command("/bin/sh", "-c", hook)
					cmd.Stdin = bytes.NewReader(raw)
					cmd.Dir = request.Workspace
					if _, err := cmd.Output(); err != nil {
						panic(err)
					}
					send("hook/completed", map[string]any{"threadId": thread, "turnId": turn, "run": map[string]any{"id": "other-hook", "eventName": "preToolUse", "status": "blocked", "source": source, "entries": entries}})
				}
				if mode == "ignores-deny" {
					item := map[string]any{"type": "mcpToolCall", "id": call, "server": "hook_canary", "tool": "apply_patch", "arguments": map[string]any{}, "status": "inProgress"}
					params := map[string]any{"threadId": thread, "turnId": turn, "item": item}
					send("item/started", params)
					target := exec.Command(request.Binary, "harness", "codex-hook-target", directory)
					target.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"apply_patch\",\"arguments\":{}}}\n")
					out, err := target.CombinedOutput()
					if err != nil {
						panic(fmt.Sprint(err, string(out)))
					}
					item["status"] = "completed"
					send("item/completed", params)
				}
			}

			status := "completed"
			if mode == "failed-turn" {
				status = "failed"
			}
			send("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": status}})
			if mode == "client-failure" {
				os.Exit(1)
			}
			continue
		default:
			panic("unexpected method " + frame.Method)
		}
		encoder.Encode(map[string]any{"id": frame.ID, "result": result})
	}
}
`
