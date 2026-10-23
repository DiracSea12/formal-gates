package validate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The fake control service is this test binary re-executed; init() catches
// the env marker before the testing framework parses the child's fixed
// "workflow serve" arguments.
func init() {
	if os.Getenv("FORMAL_GATES_FAKE_MCP") == "1" {
		fakeMCPService()
		os.Exit(0)
	}
}

// TestDSHPluginConfigureClientLifecycle runs the plugin's control-client path
// in Node against a fake MCP control service. The helper is installed at the
// plugin's stable launcher path inside an isolated home so the production
// resolution code is exercised exactly as installed. It proves
// request-scoped cancellation, cached-client reuse, and shutdown cleanup
// without touching a real service or any browser.
func TestDSHPluginConfigureClientLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var binary string
	if runtime.GOOS == "windows" {
		binary = filepath.Join(home, "AppData", "Local", "formal-gates", "bin", "formal-gates.exe")
	} else {
		binary = filepath.Join(home, ".local", "bin", "formal-gates")
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, content, 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(t.TempDir(), "fake-mcp.log")
	cmd := exec.Command(node, filepath.Join("testdata", "dsh_plugin_client.test.mjs"), logFile)
	cmd.Env = append(os.Environ(),
		"FORMAL_GATES_FAKE_MCP=1",
		"FAKE_MCP_LOG="+logFile,
		"HOME="+home,
		"USERPROFILE="+home,
		"LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
	)
	out, err := cmd.CombinedOutput()
	t.Logf("plugin client test output:\n%s", out)
	if err != nil {
		log, _ := os.ReadFile(logFile)
		t.Logf("control protocol:\n%s", log)
		t.Fatalf("plugin client test failed: %v", err)
	}
}

// fakeMCPService checks bidirectional JSON-RPC identities, liveness during
// configuration and held control calls, and request-scoped cancellation.
func fakeMCPService() {
	log := func(line string) {
		path := os.Getenv("FAKE_MCP_LOG")
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		fmt.Fprintln(f, line)
		f.Close()
	}
	log(fmt.Sprintf("SPAWN %d", os.Getpid()))
	send := func(value any) {
		body, _ := json.Marshal(value)
		fmt.Println(string(body))
	}
	fail := func(id json.RawMessage, message string) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32602, "message": message}})
	}
	pingReplies := map[string]func(){}
	ping := func(id string, next func()) {
		pingReplies[id] = next
		send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "ping"})
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	pending := map[float64]bool{}
	for scanner.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			continue
		}
		switch frame.Method {
		case "":
			var id string
			_ = json.Unmarshal(frame.ID, &id)
			if next, ok := pingReplies[id]; ok {
				delete(pingReplies, id)
				if string(frame.Result) != "{}" || len(frame.Error) != 0 {
					log(fmt.Sprintf("PING_REJECTED %s %s", id, scanner.Bytes()))
					continue
				}
				log("PING_OK " + id)
				next()
			} else if id == "unknown-method" {
				var response struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(frame.Error, &response)
				log(fmt.Sprintf("UNKNOWN_REPLY %d", response.Code))
			}
		case "initialize":
			log(fmt.Sprintf("INIT %s", frame.ID))
			ping("ping-initialize", func() {
				send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{
					"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "fake", "version": "1"},
				}})
			})
		case "tools/list":
			log(fmt.Sprintf("LIST %s", frame.ID))
			ping("ping-discovery", func() {
				send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{"tools": []any{
					map[string]any{"name": "configure", "description": "fake configure", "inputSchema": map[string]any{"type": "object"}},
					map[string]any{"name": "confirm_control", "description": "fake confirm", "inputSchema": map[string]any{"type": "object"}},
				}}})
			})
		case "tools/call":
			var id float64
			_ = json.Unmarshal(frame.ID, &id)
			var params struct {
				Name      string `json:"name"`
				Arguments struct {
					Force     bool   `json:"force"`
					Hold      bool   `json:"hold"`
					Root      string `json:"root"`
					Candidate int    `json:"candidate"`
				} `json:"arguments"`
				Metadata struct {
					Owner struct {
						Session    string `json:"session"`
						Transcript string `json:"transcript"`
						Provider   string `json:"provider"`
					} `json:"formal-gates/owner"`
				} `json:"_meta"`
			}
			_ = json.Unmarshal(frame.Params, &params)
			log(fmt.Sprintf("CALL %v name=%s root=%s", id, params.Name, params.Arguments.Root))
			if params.Name == "confirm_control" {
				if params.Arguments.Root != "/project" || params.Arguments.Candidate != 3 || params.Metadata.Owner.Session != "session-1" || params.Metadata.Owner.Transcript != "" || params.Metadata.Owner.Provider != "" {
					fail(frame.ID, "original control identity changed")
					continue
				}
				pending[id] = true
				log(fmt.Sprintf("HELD_CONTROL %v candidate=%d session=%s", id, params.Arguments.Candidate, params.Metadata.Owner.Session))
				ping("ping-held-control", func() {
					send(map[string]any{"jsonrpc": "2.0", "id": "unknown-method", "method": "unsupported/method"})
				})
				continue
			}
			if params.Name != "configure" || !params.Arguments.Force {
				fail(frame.ID, "unexpected tool or arguments")
				continue
			}
			if params.Arguments.Hold {
				pending[id] = true
				ping("ping-held-configure", func() {})
				continue
			}
			ping(fmt.Sprintf("ping-configure-%v", id), func() {
				text, _ := json.Marshal(map[string]any{
					"status": "selected", "name": params.Name,
					"root": params.Arguments.Root, "requestId": id,
				})
				send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": string(text)}}, "isError": false,
				}})
			})
		case "notifications/cancelled":
			var params struct {
				RequestID float64 `json:"requestId"`
			}
			_ = json.Unmarshal(frame.Params, &params)
			if pending[params.RequestID] {
				delete(pending, params.RequestID)
				log(fmt.Sprintf("CANCELLED %v", params.RequestID))
				log(fmt.Sprintf("LATE_REPLY %v", params.RequestID))
				send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(fmt.Sprint(params.RequestID)), "result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": `{"status":"selected","late":true}`}}, "isError": false,
				}})
			}
		}
	}
	log("EXIT")
	os.Exit(0)
}
