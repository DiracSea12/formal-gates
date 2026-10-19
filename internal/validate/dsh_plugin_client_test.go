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
		t.Fatalf("plugin client test failed: %v", err)
	}
}

// fakeMCPService is the helper-process half of the test: a line-delimited
// JSON-RPC peer that records every request identity it receives and rejects
// anything but the configure tool with its expected arguments.
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
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	pending := map[float64]bool{}
	for scanner.Scan() {
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			continue
		}
		switch frame.Method {
		case "initialize":
			log(fmt.Sprintf("INIT %s", frame.ID))
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{
				"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "fake", "version": "1"},
			}})
		case "tools/list":
			log(fmt.Sprintf("LIST %s", frame.ID))
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{"tools": []any{
				map[string]any{"name": "configure", "description": "fake configure", "inputSchema": map[string]any{"type": "object"}},
				map[string]any{"name": "confirm_start", "description": "fake confirm", "inputSchema": map[string]any{"type": "object"}},
			}}})
		case "tools/call":
			var id float64
			_ = json.Unmarshal(frame.ID, &id)
			var params struct {
				Name      string `json:"name"`
				Arguments struct {
					Force bool   `json:"force"`
					Hold  bool   `json:"hold"`
					Root  string `json:"root"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(frame.Params, &params)
			log(fmt.Sprintf("CALL %v name=%s root=%s", id, params.Name, params.Arguments.Root))
			if params.Name != "configure" || !params.Arguments.Force {
				fail(frame.ID, "unexpected tool or arguments")
				continue
			}
			if params.Arguments.Hold {
				pending[id] = true
				continue
			}
			text, _ := json.Marshal(map[string]any{
				"status": "selected", "name": params.Name,
				"root": params.Arguments.Root, "requestId": id,
			})
			send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(frame.ID), "result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": string(text)}}, "isError": false,
			}})
		case "notifications/cancelled":
			var params struct {
				RequestID float64 `json:"requestId"`
			}
			_ = json.Unmarshal(frame.Params, &params)
			if pending[params.RequestID] {
				delete(pending, params.RequestID)
				log(fmt.Sprintf("CANCELLED %v", params.RequestID))
				send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(fmt.Sprint(params.RequestID)), "result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": `{"status":"cancelled"}`}}, "isError": false,
				}})
			}
		}
	}
	log("EXIT")
	os.Exit(0)
}
