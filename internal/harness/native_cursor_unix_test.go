//go:build !windows

package harness

// A native Cursor CLI peer checks its actual argv, environment, cwd and saved
// context. Every session/model input owns different native evidence; no model
// or installed host is invoked.
import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCursorManagedToolsUseOriginalWorkspaceAndPolicy(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the source package")
	}
	binary := filepath.Join(t.TempDir(), "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build current CLI: %v\n%s", err, output)
	}
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%t", readOnly), func(t *testing.T) {
			scenario := newCursorScenario(t, "88888888-8888-4888-8888-888888888888", "model-a", "high", readOnly)
			workspace, directory := scenario.workspace, scenario.directory
			path := filepath.Join(workspace, "input.txt")
			if err := os.WriteFile(path, []byte("original\nkeep\n"), 0600); err != nil {
				t.Fatal(err)
			}
			native := exec.Command(binary, "harness", "native", directory)
			if output, err := native.CombinedOutput(); err != nil {
				t.Fatalf("original native plugin registration: %v\n%s", err, output)
			}
			plugin := filepath.Join(directory, "cursor-tools")
			hooks, err := readJSONFile(filepath.Join(plugin, "hooks/hooks.json"))
			if err != nil {
				t.Fatal(err)
			}
			mcp, err := readJSONFile(filepath.Join(plugin, ".mcp.json"))
			if err != nil {
				t.Fatal(err)
			}
			server := getOf(getOf(mcp, "mcpServers"), "formal-gates")
			invoke := func(entry string, input any) map[string]any {
				t.Helper()
				body, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				var command *exec.Cmd
				if entry == "native-tools" {
					args := []string{}
					for _, arg := range listOf(getOf(server, "args")) {
						args = append(args, textOf(arg))
					}
					command = exec.Command(textOf(getOf(server, "command")), args...)
				} else {
					event := textOf(getOf(input, "hook_event_name"))
					rows := listOf(getOf(getOf(hooks, "hooks"), event))
					if len(rows) != 1 {
						t.Fatalf("wrong registered hook: %#v", hooks)
					}
					command = exec.Command("sh", "-c", textOf(getOf(rows[0], "command")))
				}
				command.Stdin = bytes.NewReader(body)
				var stderr bytes.Buffer
				command.Stderr = &stderr
				output, err := command.Output()
				if err != nil {
					t.Fatalf("%s: %v\n%s", entry, err, stderr.String())
				}
				var result map[string]any
				if err := json.Unmarshal(output, &result); err != nil {
					t.Fatalf("%s returned invalid JSON: %v\n%s", entry, err, output)
				}
				return result
			}
			permission := func(event, tool, server, cwd string) bool {
				t.Helper()
				result := invoke("cursor-tool-permission", map[string]any{"hook_event_name": event, "tool_name": tool, "mcp_server_name": server, "cwd": cwd, "tool_input": map[string]any{}, "tool_use_id": "original-call"})
				if len(result) != 1 || (result["permission"] != "allow" && result["permission"] != "deny") {
					t.Fatalf("invalid native permission response: %#v", result)
				}
				return result["permission"] == "allow"
			}
			for _, tool := range []string{"Read", "Write", "Shell", "Grep", "Glob", "LS", "SemanticSearch"} {
				if permission("preToolUse", tool, "", workspace) {
					t.Fatalf("builtin %s was allowed", tool)
				}
			}
			if permission("preToolUse", "MCP:workspace", "", t.TempDir()) {
				t.Fatal("another workspace was allowed")
			}
			call := func(server string, arguments any) map[string]any {
				t.Helper()
				if !permission("preToolUse", "MCP:workspace", "", workspace) {
					t.Fatal("native MCP candidate was rejected before server check")
				}
				if !permission("beforeMCPExecution", "workspace", server, "") {
					return nil
				}
				response := invoke("native-tools", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "workspace", "arguments": arguments}})
				if response["jsonrpc"] != "2.0" || response["id"] != float64(1) || response["error"] != nil {
					t.Fatalf("wrong MCP response identity: %#v", response)
				}
				return objectOrEmpty(response["result"])
			}
			beforeDenied := readBytes(t, filepath.Join(directory, nativeToolCallsFile))
			if call("other-server", map[string]any{"action": "list"}) != nil {
				t.Fatal("same-name MCP tool from another server executed")
			}
			if !bytes.Equal(beforeDenied, readBytes(t, filepath.Join(directory, nativeToolCallsFile))) {
				t.Fatal("denied server changed original tool call evidence")
			}
			decode := func(result map[string]any) map[string]any {
				t.Helper()
				if result["isError"] != false {
					t.Fatalf("managed tool failed: %#v", result)
				}
				content := listOf(result["content"])
				if len(content) != 1 || getOf(content[0], "type") != "text" {
					t.Fatalf("invalid MCP content: %#v", result)
				}
				var value map[string]any
				if err := json.Unmarshal([]byte(textOf(getOf(content[0], "text"))), &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			listing := decode(call("formal-gates", map[string]any{"action": "list"}))
			entries := listOf(listing["entries"])
			if len(entries) != 1 || getOf(entries[0], "name") != "input.txt" {
				t.Fatalf("wrong original listing: %#v", listing)
			}
			read := decode(call("formal-gates", map[string]any{"action": "read", "listing": listing["listing"], "entry": getOf(entries[0], "entry")}))
			if !valuesEqual(read["lines"], []any{"original\n", "keep\n"}) {
				t.Fatalf("wrong original read: %#v", read)
			}
			result := call("formal-gates", map[string]any{"action": "replace", "read": read["read"], "from": 1, "to": 1, "content": "changed\n"})
			want := "changed\nkeep\n"
			if readOnly {
				want = "original\nkeep\n"
				if result["isError"] != true || !strings.Contains(textOf(getOf(listOf(result["content"])[0], "text")), "read-only") {
					t.Fatalf("read-only policy did not reject the write: %#v", result)
				}
			} else {
				decode(result)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != want {
				t.Fatalf("actual workspace effects: %q, %v; want %q", body, err, want)
			}
			if err := verifyNativeToolEvidence(directory); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type cursorPeer struct {
	UUID            string            `json:"uuid"`
	Entry           string            `json:"entry"`
	Args            []string          `json:"args"`
	Environment     map[string]string `json:"environment"`
	Workspace       string            `json:"workspace"`
	Calls           string            `json:"calls"`
	Store           string            `json:"store"`
	ExpectedContext string            `json:"expected_context"`
	Events          []any             `json:"events"`
	Semantics       any               `json:"semantics"`
	Config          map[string]any    `json:"config"`
	Exit            int               `json:"exit"`
	Stderr          string            `json:"stderr"`
	Wait            bool              `json:"wait"`
	CreateExit      int               `json:"create_exit"`
	SkipStore       bool              `json:"skip_store"`
	ConfigPublished string            `json:"config_published"`
	BeforeInit      string            `json:"before_init"`
	BeforeTerminal  string            `json:"before_terminal"`
}

func TestCursorCLI(t *testing.T) {
	script := os.Getenv("CURSOR_PEER_SCRIPT")
	if script == "" {
		return
	}
	body, err := os.ReadFile(script)
	if err != nil {
		os.Exit(91)
	}
	var peer cursorPeer
	if json.Unmarshal(body, &peer) != nil {
		os.Exit(91)
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if peer.Entry != "" {
		if len(args) == 0 || args[0] != peer.Entry {
			fmt.Fprintln(os.Stderr, "wrong native entry")
			os.Exit(91)
		}
		args = args[1:]
	}
	cwd, _ := os.Getwd()
	if cwd != peer.Workspace {
		fmt.Fprintln(os.Stderr, "wrong cwd")
		os.Exit(91)
	}
	for key, value := range peer.Environment {
		if os.Getenv(key) != value {
			fmt.Fprintln(os.Stderr, "wrong env", key)
			os.Exit(91)
		}
	}
	if reflect.DeepEqual(args, []string{"--version"}) {
		fmt.Println("2026.08.04-fixture")
		os.Exit(0)
	}
	if reflect.DeepEqual(args, []string{"--help"}) {
		fmt.Println("--mode ask --model bracket overrides --resume stream-json create-chat --plugin-dir")
		os.Exit(0)
	}
	call, err := os.OpenFile(peer.Calls, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		os.Exit(91)
	}
	line, _ := json.Marshal(map[string]any{"argv": args, "entry": peer.Entry, "cwd": cwd, "uuid": peer.UUID})
	call.Write(append(line, '\n'))
	call.Close()
	if reflect.DeepEqual(args, []string{"create-chat"}) {
		if peer.CreateExit != 0 {
			fmt.Fprintln(os.Stderr, "Failed to create chat")
			os.Exit(peer.CreateExit)
		}
		fmt.Println(peer.UUID)
		os.Exit(0)
	}
	if !reflect.DeepEqual(args, peer.Args) {
		fmt.Fprintf(os.Stderr, "wrong argv: %q != %q\n", args, peer.Args)
		os.Exit(91)
	}
	plugin := ""
	for index, arg := range args {
		if arg == "--plugin-dir" && index+1 < len(args) {
			plugin = args[index+1]
		}
	}
	readPlugin := func(name string) map[string]any {
		body, err := os.ReadFile(filepath.Join(plugin, name))
		var value map[string]any
		if err != nil || json.Unmarshal(body, &value) != nil {
			fmt.Fprintln(os.Stderr, "native plugin file is missing or invalid", name)
			os.Exit(91)
		}
		return value
	}
	manifest := readPlugin(".cursor-plugin/plugin.json")
	if manifest["hooks"] != "hooks/hooks.json" {
		fmt.Fprintln(os.Stderr, "wrong native hook reference")
		os.Exit(91)
	}
	hooks := readPlugin("hooks/hooks.json")
	for _, name := range []string{"preToolUse", "beforeMCPExecution"} {
		rows := listOf(getOf(getOf(hooks, "hooks"), name))
		if len(rows) != 1 || getOf(rows[0], "matcher") != "*" || getOf(rows[0], "failClosed") != true || getOf(rows[0], "timeout") != nil || !strings.Contains(textOf(getOf(rows[0], "command")), "harness cursor-tool-permission") {
			fmt.Fprintln(os.Stderr, "wrong native permission hook", name)
			os.Exit(91)
		}
	}
	server := getOf(getOf(readPlugin(".mcp.json"), "mcpServers"), "formal-gates")
	if !valuesEqual(getOf(server, "args"), []any{"harness", "native-tools", filepath.Dir(plugin)}) {
		fmt.Fprintln(os.Stderr, "wrong native MCP owner")
		os.Exit(91)
	}
	prior, err := os.ReadFile(peer.Store)
	if peer.ExpectedContext == "" {
		if !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "new native store already exists")
			os.Exit(91)
		}
		if !peer.SkipStore {
			if err := os.MkdirAll(filepath.Dir(peer.Store), 0o755); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(91)
			}
			if err := os.WriteFile(peer.Store, []byte(peer.UUID+":initialized"), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(91)
			}
		}
	} else if err != nil || string(prior) != peer.ExpectedContext {
		fmt.Fprintln(os.Stderr, "native context differs")
		os.Exit(91)
	}
	config, _ := json.Marshal(peer.Config)
	if err := os.WriteFile(filepath.Join(os.Getenv("CURSOR_CONFIG_DIR"), "cli-config.json"), config, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	if peer.ConfigPublished != "" {
		if err := os.WriteFile(peer.ConfigPublished, []byte(peer.UUID), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(91)
		}
	}
	waitMarker := func(path string) {
		if path == "" {
			return
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(path); err == nil {
				return
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "native peer barrier timeout")
				os.Exit(91)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitMarker(peer.BeforeInit)
	fmt.Fprint(os.Stderr, peer.Stderr)
	if peer.Semantics != nil {
		peerSubmitSemantics(textOf(listOf(getOf(server, "args"))[2]), peer.Semantics)
	}
	for _, event := range peer.Events {
		if getOf(event, "type") == "result" {
			waitMarker(peer.BeforeTerminal)
		}
		line, _ := json.Marshal(event)
		os.Stdout.Write(append(line, '\n'))
	}
	if peer.Wait {
		time.Sleep(60 * time.Second)
	}
	if peer.Exit == 0 && !peer.SkipStore {
		if err := os.WriteFile(peer.Store, []byte(peer.UUID+":completed"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(91)
		}
	}
	os.Exit(peer.Exit)
}

type cursorScenario struct {
	request                      map[string]any
	peer                         cursorPeer
	directory, workspace, script string
}

func newCursorScenario(t *testing.T, uuid, model, effort string, readOnly bool) *cursorScenario {
	t.Helper()
	root := resolveNativePath(t.TempDir())
	workspace := filepath.Join(root, "ws")
	directory := filepath.Join(root, "job")
	home := filepath.Join(root, "home")
	config := filepath.Join(root, "config")
	data := filepath.Join(root, "data")
	cache := filepath.Join(root, "cache")
	for _, path := range []string{workspace, directory, home, config, data, cache} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "cursor-agent")
	wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\\''") + "' -test.run='^TestCursorCLI$' -- \"$@\"\n"
	if err := os.WriteFile(executable, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	binding, err := buildCaptureBinding("cursor", map[string]any{"executable": executable})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "peer.json")
	environment := map[string]string{"HOME": home, "CURSOR_CONFIG_DIR": config, "CURSOR_DATA_DIR": data, "NODE_COMPILE_CACHE": cache, "CURSOR_PEER_SCRIPT": script}
	bound := map[string]any{}
	for k, v := range environment {
		bound[k] = v
	}
	binding["environment"] = bound
	schema, err := worker.ResultSchema(protocol.WorkerInput{Handler: "engine.gate.v2", Validation: &protocol.ValidationInput{}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	plainSchema, err := decodeNativeJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": "cursor", "binding": binding, "workspace": workspace, "identity": "", "model": model, "reasoning_effort": effort, "read_only": readOnly, "prompt": "Review " + uuid, "schema": plainSchema}
	parameters := []any{}
	if effort != "" {
		parameters = append(parameters, map[string]any{"id": "effort", "value": effort})
	}
	display := model
	if effort != "" {
		display += " " + effort
	}
	nativeConfig := map[string]any{"model": map[string]any{"modelId": model, "displayModelId": model, "displayName": display, "aliases": []any{"model-alias"}}, "selectedModel": map[string]any{"modelId": model, "parameters": parameters}, "apiKey": "secret-not-evidence"}
	init := map[string]any{"type": "system", "subtype": "init", "session_id": uuid, "cwd": workspace, "model": display, "permissionMode": "default"}
	answer := map[string]any{"findings": []any{map[string]any{"severity": "P3", "message": "native " + uuid + " " + model}}}
	terminal := map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": uuid, "request_id": "request-" + uuid, "result": "Semantic result submitted.", "usage": map[string]any{"inputTokens": len(uuid), "outputTokens": len(model)}}
	sum := md5.Sum([]byte(workspace))
	store := filepath.Join(config, "chats", hex.EncodeToString(sum[:]), uuid, "store.db")
	s := &cursorScenario{request: request, directory: directory, workspace: workspace, script: script, peer: cursorPeer{UUID: uuid, Workspace: workspace, Environment: environment, Calls: filepath.Join(root, "calls.jsonl"), Store: store, Config: nativeConfig, Events: []any{init, terminal}, Semantics: answer}}
	s.save(t)
	return s
}
func (s *cursorScenario) save(t *testing.T) {
	t.Helper()
	model, _ := s.request["model"].(string)
	effort, _ := s.request["reasoning_effort"].(string)
	if effort != "" && !strings.Contains(model, "effort=") {
		if strings.Contains(model, "[") {
			model = model[:len(model)-1] + ",effort=" + effort + "]"
		} else {
			model += "[effort=" + effort + "]"
		}
	}
	s.peer.Args = []string{"-p", "--output-format", "stream-json", "--workspace", s.workspace, "--resume", s.peer.UUID, "--plugin-dir", filepath.Join(s.directory, "cursor-tools")}
	if s.request["read_only"] == true {
		s.peer.Args = append(s.peer.Args, "--mode", "ask")
	}
	if model != "" {
		s.peer.Args = append(s.peer.Args, "--model", model)
	}
	s.peer.Args = append(s.peer.Args, textOf(s.request["prompt"]))
	body, _ := json.Marshal(s.peer)
	if err := os.WriteFile(s.script, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), s.request); err != nil {
		t.Fatal(err)
	}
}
func cursorCalls(t *testing.T, s *cursorScenario) int {
	t.Helper()
	body, err := os.ReadFile(s.peer.Calls)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(body, []byte("\n"))
}
func (s *cursorScenario) resume(t *testing.T) {
	t.Helper()
	s.request["identity"] = s.peer.UUID
	s.request["native_store"] = s.peer.Store
	s.peer.ExpectedContext = s.peer.UUID + ":completed"
	s.directory = filepath.Join(filepath.Dir(s.directory), "continued")
	os.MkdirAll(s.directory, 0o755)
	s.save(t)
}

func TestCursorCaptureProbeAndClosure(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
	executable := textOf(getOf(getOf(s.request["binding"], "launch"), "executable"))
	t.Setenv("PATH", filepath.Dir(executable)+":"+os.Getenv("PATH"))
	for k, v := range s.peer.Environment {
		t.Setenv(k, v)
	}
	binding, err := cursorCaptureBinding()
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(binding["files"], getOf(s.request["binding"], "files")) {
		t.Fatal("wrong executable capture")
	}
	s.request["probe"] = true
	s.save(t)
	report, err := runNativeJob(s.directory)
	if err != nil || !valuesEqual(report["selection"], map[string]any{}) || !valuesEqual(report["model_calls"], 0) {
		t.Fatalf("probe %v %v", report, err)
	}
	if cursorCalls(t, s) != 0 {
		t.Fatal("probe created or prompted a session")
	}
	for _, name := range []string{"node", "index.js"} {
		os.WriteFile(filepath.Join(filepath.Dir(executable), name), []byte("native-"+name), 0o755)
	}
	bound, err := cursorCaptureBinding()
	if err != nil || len(objectOrEmpty(bound["files"])) != 3 {
		t.Fatalf("wrapper closure %v %v", bound, err)
	}
	os.WriteFile(filepath.Join(filepath.Dir(executable), "index.js"), []byte("changed"), 0o755)
	if err := cursorValidateBinding(bound); err == nil || !strings.Contains(err.Error(), "HOST_CHANGED") {
		t.Fatal("changed native entry was accepted")
	}
}
func TestCursorNewResumeAndOriginalObservation(t *testing.T) {
	for i, uuid := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		t.Run(uuid, func(t *testing.T) {
			model := fmt.Sprintf("model-%d", i)
			s := newCursorScenario(t, uuid, model, "high", i == 0)
			if _, err := os.Stat(s.peer.Store); !os.IsNotExist(err) {
				t.Fatal("new identity has a store before its native prompt")
			}
			source, err := runNativeJob(s.directory)
			if err != nil {
				t.Fatal(err)
			}
			if source["identity"] != uuid || source["native_store"] != s.peer.Store || source["native_request"] != "request-"+uuid {
				t.Fatalf("native binding %v", source)
			}
			if string(readBytes(t, s.peer.Store)) != uuid+":completed" {
				t.Fatal("native prompt did not persist the session context")
			}
			identity, err := readJSONFile(filepath.Join(s.directory, "identity.json"))
			if err != nil || !valuesEqual(identity, map[string]any{"provider": "cursor", "identity": uuid, "native_store": s.peer.Store, "workspace": s.workspace, "selection": source["selection"], "acknowledgement": s.peer.Events[0]}) {
				t.Fatalf("persisted identity %v %v", identity, err)
			}
			if err := cursorVerifySource(s.directory, source); err != nil {
				t.Fatal(err)
			}
			before := proofsOf(t, s.directory, nativeSourceFiles)
			observed, err := cursorObserve(s.directory)
			if err != nil || !valuesEqual(source, observed) || !valuesEqual(before, proofsOf(t, s.directory, nativeSourceFiles)) {
				t.Fatalf("observe %v %v", observed, err)
			}
			native := readBytes(t, filepath.Join(s.directory, "native-source.json"))
			if bytes.Contains(native, []byte("secret-not-evidence")) {
				t.Fatal("authentication leaked into native evidence")
			}
			for _, flag := range []string{"--force", "--yolo", "--approve-mcps", "--auto-review", "--tools", "--json-schema", "--new-session-id"} {
				if strings.Contains(strings.Join(s.peer.Args, "\n"), flag) {
					t.Fatal("invented or expanded native argv", flag)
				}
			}
			s.resume(t)
			continued, err := runNativeJob(s.directory)
			if err != nil || continued["identity"] != uuid {
				t.Fatalf("resume %v %v", continued, err)
			}
			if cursorCalls(t, s) != 3 {
				t.Fatal("resume created another chat or observer prompted")
			}
			for key, value := range map[string]any{"identity": "33333333-3333-4333-8333-333333333333", "native_store": "wrong", "native_request": "wrong", "selection": map[string]any{}} {
				copy := map[string]any{}
				for k, v := range continued {
					copy[k] = v
				}
				copy[key] = value
				if err := cursorVerifySource(s.directory, copy); err == nil {
					t.Fatal("forged receipt accepted", key)
				}
			}
		})
	}
}

func TestCursorDefaultSelectionAndFreshIdentity(t *testing.T) {
	first := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "native-default", "", true)
	first.request["model"] = ""
	first.save(t)
	source, err := runNativeJob(first.directory)
	if err != nil || getOf(source["selection"], "model") != "native-default" || getOf(source["selection"], "reasoning_effort") != "" {
		t.Fatalf("default selection %v %v", source, err)
	}
	if strings.Contains(strings.Join(first.peer.Args, "\n"), "--model") {
		t.Fatal("default request invented a native model override")
	}
	first.resume(t)
	if _, err := runNativeJob(first.directory); err != nil {
		t.Fatal(err)
	}
	second := newCursorScenario(t, "22222222-2222-4222-8222-222222222222", "model-b", "low", false)
	second.workspace = first.workspace
	second.request["workspace"] = first.workspace
	second.peer.Workspace = first.workspace
	objectOrEmpty(second.peer.Events[0])["cwd"] = first.workspace
	config := first.peer.Environment["CURSOR_CONFIG_DIR"]
	second.peer.Environment["CURSOR_CONFIG_DIR"] = config
	getOf(second.request["binding"], "environment").(map[string]any)["CURSOR_CONFIG_DIR"] = config
	second.peer.Store = cursorStore(config, first.workspace, second.peer.UUID)
	second.save(t)
	fresh, err := runNativeJob(second.directory)
	if err != nil || fresh["identity"] != second.peer.UUID || fresh["native_store"] != second.peer.Store {
		t.Fatalf("fresh native identity %v %v", fresh, err)
	}
	for _, s := range []*cursorScenario{first, second} {
		if string(readBytes(t, s.peer.Store)) != s.peer.UUID+":completed" {
			t.Fatal("fresh native identity replaced another session's context")
		}
	}
	if cursorCalls(t, first) != 3 || cursorCalls(t, second) != 2 {
		t.Fatal("native identity allocation or continuation replayed")
	}
}

func TestCursorDirectEntryCarriesEveryCommand(t *testing.T) {
	s := newCursorScenario(t, "33333333-3333-4333-8333-333333333333", "model-direct", "low", false)
	entry := filepath.Join(filepath.Dir(s.script), "index.js")
	if err := os.WriteFile(entry, []byte("native entry"), 0o644); err != nil {
		t.Fatal(err)
	}
	binding := objectOrEmpty(s.request["binding"])
	executable := getOf(getOf(binding, "launch"), "executable")
	direct, err := buildCaptureBinding("cursor", map[string]any{"executable": executable, "script": entry})
	if err != nil {
		t.Fatal(err)
	}
	direct["environment"] = binding["environment"]
	s.request["binding"] = direct
	s.peer.Entry = entry
	s.request["probe"] = true
	s.save(t)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	delete(s.request, "probe")
	s.save(t)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	s.resume(t)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	if cursorCalls(t, s) != 3 {
		t.Fatal("direct entry changed native invocation count")
	}
}
func TestCursorMissingStoreBeforePrompt(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	s.resume(t)
	os.Remove(s.peer.Store)
	_, err := runNativeJob(s.directory)
	unavailable, ok := err.(*sessionUnavailableError)
	if !ok || unavailable.identity != s.peer.UUID || unavailable.method != "--resume" || unavailable.code != nil {
		t.Fatalf("missing store %T %v", err, err)
	}
	if cursorCalls(t, s) != 2 {
		t.Fatal("missing store sent a prompt")
	}
	if _, err := os.Stat(filepath.Join(s.directory, "identity.json")); !os.IsNotExist(err) {
		t.Fatal("rejected session published")
	}
	writeNativeError(s.directory, err, &bytes.Buffer{})
	record, _ := readJSONFile(filepath.Join(s.directory, "native-error.json"))
	if getOf(record, "error_kind") != "SESSION_UNAVAILABLE" || getOf(record, "native_code") != nil {
		t.Fatalf("native refusal %v", record)
	}
	raw, _ := readJSONFile(filepath.Join(s.directory, "native-source.json"))
	if getOf(getOf(raw, "native_resource_missing"), "path") != s.peer.Store {
		t.Fatal("missing original resource evidence")
	}
}
func TestCursorNativeRefusals(t *testing.T) {
	cases := []struct {
		name, want string
		change     func(*cursorScenario)
	}{
		{"create-fails", "CURSOR_CREATE", func(s *cursorScenario) { s.peer.CreateExit = 7 }},
		{"create-not-uuid", "native UUID", func(s *cursorScenario) { s.peer.UUID = "not-a-uuid" }},
		{"init-store-absent", "native init has no persisted store", func(s *cursorScenario) { s.peer.SkipStore = true }},
		{"init-other-session", "native init", func(s *cursorScenario) {
			objectOrEmpty(s.peer.Events[0])["session_id"] = "22222222-2222-4222-8222-222222222222"
		}},
		{"init-other-workspace", "native init", func(s *cursorScenario) { objectOrEmpty(s.peer.Events[0])["cwd"] = "/another/workspace" }},
		{"repeated-init", "native init", func(s *cursorScenario) {
			s.peer.Events = []any{s.peer.Events[0], s.peer.Events[0], s.peer.Events[1]}
		}},
		{"no-init", "CURSOR_RESULT", func(s *cursorScenario) { s.peer.Events = s.peer.Events[1:] }},
		{"no-terminal", "terminal result is missing", func(s *cursorScenario) { s.peer.Events = s.peer.Events[:1] }},
		{"hook-only", "terminal result is missing", func(s *cursorScenario) {
			s.peer.Events = []any{map[string]any{"hook_event_name": "subagentStop", "conversation_id": s.peer.UUID, "subagent_id": "child", "summary": "PASS"}}
		}},
		{"foreign-terminal", "mixed native session", func(s *cursorScenario) {
			objectOrEmpty(s.peer.Events[1])["session_id"] = "22222222-2222-4222-8222-222222222222"
		}},
		{"error-result", "native result failed", func(s *cursorScenario) { objectOrEmpty(s.peer.Events[1])["is_error"] = true }},
		{"failure-subtype", "native result failed", func(s *cursorScenario) { objectOrEmpty(s.peer.Events[1])["subtype"] = "error" }},
		{"request-missing", "request identity", func(s *cursorScenario) { delete(objectOrEmpty(s.peer.Events[1]), "request_id") }},
		{"request-not-text", "request identity", func(s *cursorScenario) { objectOrEmpty(s.peer.Events[1])["request_id"] = true }},
		{"nonzero-after-success", "CURSOR_PROCESS", func(s *cursorScenario) { s.peer.Exit = 7 }},
		{"native-error", "CURSOR_PROCESS", func(s *cursorScenario) { s.peer.Exit = 1; s.peer.Events = nil; s.peer.Stderr = "Operation cancelled\n" }},
		{"after-terminal", "after terminal", func(s *cursorScenario) { s.peer.Events = append(s.peer.Events, s.peer.Events[0]) }},
		{"wrong-model", "model selection was not applied", func(s *cursorScenario) {
			getOf(s.peer.Config, "model").(map[string]any)["modelId"] = "other"
			getOf(s.peer.Config, "model").(map[string]any)["displayModelId"] = "other"
			getOf(s.peer.Config, "model").(map[string]any)["displayName"] = "other high"
			getOf(s.peer.Config, "selectedModel").(map[string]any)["modelId"] = "other"
			objectOrEmpty(s.peer.Events[0])["model"] = "other high"
		}},
		{"stale-config-display", "does not match init", func(s *cursorScenario) { getOf(s.peer.Config, "model").(map[string]any)["displayName"] = "other" }},
		{"wrong-effort", "parameter was not applied", func(s *cursorScenario) {
			getOf(s.peer.Config, "selectedModel").(map[string]any)["parameters"] = []any{map[string]any{"id": "effort", "value": "low"}}
		}},
		{"unsupported-effort", "parameter was not applied", func(s *cursorScenario) {
			getOf(s.peer.Config, "selectedModel").(map[string]any)["parameters"] = []any{}
		}},
		{"stale-parameter-model", "model and parameters differ", func(s *cursorScenario) { getOf(s.peer.Config, "selectedModel").(map[string]any)["modelId"] = "other" }},
		{"conflicting-effort", "conflicting effort", func(s *cursorScenario) { s.request["model"] = "model-a[effort=low]" }},
		{"effort-without-model", "requires an explicit model", func(s *cursorScenario) { s.request["model"] = "" }},
		{"wrong-resume-store", "saved native store", func(s *cursorScenario) {
			s.request["identity"] = s.peer.UUID
			s.request["native_store"] = "/another/store.db"
		}},
		{"missing-submission", "NATIVE_RESULT", func(s *cursorScenario) { s.peer.Semantics = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
			tc.change(s)
			s.save(t)
			_, err := runNativeJob(s.directory)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v want %s", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
				t.Fatal("refusal became success")
			}
			if tc.name == "init-store-absent" {
				if cursorCalls(t, s) != 2 {
					t.Fatal("new native identity never reached its first prompt")
				}
				if _, err := os.Stat(filepath.Join(s.directory, "identity.json")); !os.IsNotExist(err) {
					t.Fatal("unpersisted native identity was published")
				}
				if _, ok := err.(*bindingError); !ok {
					t.Fatalf("native initialization failure %T %v", err, err)
				}
				writeNativeError(s.directory, err, &bytes.Buffer{})
				failure, err := readJSONFile(filepath.Join(s.directory, "native-error.json"))
				if err != nil || getOf(failure, "error_kind") == "SESSION_UNAVAILABLE" {
					t.Fatal("native initialization failure became a lost saved session")
				}
			}
			if tc.name == "wrong-resume-store" || tc.name == "conflicting-effort" || tc.name == "effort-without-model" {
				if cursorCalls(t, s) != 0 {
					t.Fatal("invalid inputs prompted")
				}
			}
		})
	}
}
func TestCursorBracketParametersAndAlias(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", false)
	s.request["model"] = "model-a[context=1m]"
	selected := getOf(s.peer.Config, "selectedModel").(map[string]any)
	selected["parameters"] = append(listOf(selected["parameters"]), map[string]any{"id": "context", "value": "1m"})
	s.save(t)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(s.peer.Args, "\n"), "model-a[context=1m,effort=high]") {
		t.Fatal("native bracket syntax absent")
	}
	s.resume(t)
	s.request["model"] = "model-alias"
	s.save(t)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
}
func TestCursorReconcileDoesNotReplayPrompt(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
	original, err := runNativeJob(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	names := append(append([]string{}, nativeSourceFiles...), nativeToolCallsFile, nativeToolContextFile)
	originalProofs := proofsOf(t, s.directory, names)
	if err := os.Remove(filepath.Join(s.directory, "source.json")); err != nil {
		t.Fatal(err)
	}
	source, err := cursorReconcile(s.directory, map[string]any{"exit_code": 0, "error": ""})
	if err != nil || !valuesEqual(source, original) {
		t.Fatalf("reconcile %v %v", source, err)
	}
	if !valuesEqual(originalProofs, proofsOf(t, s.directory, names)) || cursorCalls(t, s) != 2 {
		t.Fatal("reconciliation changed original facts or replayed")
	}
	for _, process := range []map[string]any{{"exit_code": 1, "error": ""}, {"exit_code": 0, "error": "interrupted"}} {
		if err := os.Remove(filepath.Join(s.directory, "source.json")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if _, err := cursorReconcile(s.directory, process); err == nil || err.Error() != "CURSOR_RECONCILE: original native process did not complete successfully" {
			t.Fatal("failed original process laundered", err)
		}
		if _, err := os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
			t.Fatal("failed original process became a recovered success")
		}
	}
}

func TestCursorParallelSourcesRemainIndependent(t *testing.T) {
	first := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", false)
	second := newCursorScenario(t, "22222222-2222-4222-8222-222222222222", "model-b", "low", true)
	done := make(chan error, 2)
	for _, s := range []*cursorScenario{first, second} {
		go func(s *cursorScenario) { _, err := runNativeJob(s.directory); done <- err }(s)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*cursorScenario{first, second} {
		source, err := cursorObserve(s.directory)
		if err != nil || source["identity"] != s.peer.UUID || source["native_store"] != s.peer.Store || getOf(source["selection"], "model") != getOf(s.peer.Config["model"], "modelId") {
			t.Fatalf("crossed source %v %v", source, err)
		}
	}
}

// Both peers use the same bound config. The first native selection is held
// before init; its terminal waits for the second selection to prove overlap.
func TestCursorSharedConfigSelectionAndConcurrentTurns(t *testing.T) {
	for _, secondModel := range []string{"model-a", "model-b"} {
		t.Run(secondModel, func(t *testing.T) {
			first := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", false)
			second := newCursorScenario(t, "22222222-2222-4222-8222-222222222222", secondModel, "low", true)
			shared := first.peer.Environment["CURSOR_CONFIG_DIR"]
			second.peer.Environment["CURSOR_CONFIG_DIR"] = shared
			getOf(second.request["binding"], "environment").(map[string]any)["CURSOR_CONFIG_DIR"] = shared
			second.peer.Store = cursorStore(shared, second.workspace, second.peer.UUID)
			first.peer.ConfigPublished = filepath.Join(shared, "first-selected")
			first.peer.BeforeInit = filepath.Join(shared, "allow-init")
			second.peer.ConfigPublished = filepath.Join(shared, "second-selected")
			first.peer.BeforeTerminal = second.peer.ConfigPublished
			first.save(t)
			second.save(t)
			done := make(chan error, 2)
			go func() { _, err := runNativeJob(first.directory); done <- err }()
			waitForFile(t, first.peer.ConfigPublished)
			go func() { _, err := runNativeJob(second.directory); done <- err }()
			select {
			case err := <-done:
				t.Fatalf("native call ended before either init: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if _, err := os.Stat(second.peer.ConfigPublished); !os.IsNotExist(err) {
				t.Fatal("another initialization replaced the first selection")
			}
			if err := os.WriteFile(first.peer.BeforeInit, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			for _, s := range []*cursorScenario{first, second} {
				source, err := cursorObserve(s.directory)
				if err != nil || source["identity"] != s.peer.UUID || source["native_store"] != s.peer.Store || getOf(source["selection"], "model") != getOf(s.peer.Config["model"], "modelId") || getOf(source["selection"], "reasoning_effort") != s.request["reasoning_effort"] {
					t.Fatalf("call selection or identity crossed: %v %v", source, err)
				}
				if err := cursorVerifySource(s.directory, source); err != nil {
					t.Fatal(err)
				}
				if cursorCalls(t, s) != 2 {
					t.Fatal("parallel turn replayed or created another chat")
				}
			}
		})
	}
}
func TestCursorOriginalSupervisorReceiptAndCancellation(t *testing.T) {
	bin := supervisorBinary(t)
	for _, cancel := range []string{"completed", "initializing", "initialized"} {
		t.Run(cancel, func(t *testing.T) {
			s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
			if cancel != "completed" {
				s.peer.Events = s.peer.Events[:1]
				s.peer.Wait = true
				if cancel == "initializing" {
					s.peer.ConfigPublished = filepath.Join(filepath.Dir(s.script), "selected")
					s.peer.BeforeInit = filepath.Join(filepath.Dir(s.script), "allow-init")
				}
			}
			s.save(t)
			writeRequest(t, s.directory, map[string]any{"kind": "native", "argv": []string{bin, "harness", "native", s.directory}, "cwd": s.workspace, "stdin": ""})
			command := exec.Command(bin, "harness", "supervise", s.directory)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			if cancel != "completed" {
				if cancel == "initializing" {
					waitForFile(t, s.peer.ConfigPublished)
				} else {
					waitForFile(t, filepath.Join(s.directory, "identity.json"))
				}
				if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			receipt := readReceipt(t, s.directory)
			if cancel != "completed" {
				if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
					t.Fatalf("cancel %v", receipt)
				}
				if _, err := settleNativeReceipt(s.directory, receipt); err == nil {
					t.Fatal("cancel admitted")
				}
				if _, err := os.Stat(s.peer.Store); err != nil {
					t.Fatal("cancel deleted store")
				}
				if string(readBytes(t, s.peer.Store)) != s.peer.UUID+":initialized" {
					t.Fatal("cancelled native initialization changed its context")
				}
				_, identityErr := os.Stat(filepath.Join(s.directory, "identity.json"))
				if cancel == "initializing" && !os.IsNotExist(identityErr) || cancel == "initialized" && identityErr != nil {
					t.Fatal("cancel crossed the native identity publication boundary")
				}
				if cancel == "initialized" {
					identity, err := readJSONFile(filepath.Join(s.directory, "identity.json"))
					if err != nil || getOf(identity, "identity") != s.peer.UUID || getOf(identity, "native_store") != s.peer.Store {
						t.Fatal("cancel recovery lost the initialized native identity")
					}
				}
				if source, err := readJSONFile(filepath.Join(s.directory, "source.json")); err == nil && textOf(getOf(source, "identity")) != "" {
					t.Fatal("cancel recovery treated an unaccepted native identity as a completed turn")
				}
				if _, err := cursorReconcile(s.directory, receipt); err == nil {
					t.Fatal("cancelled native trajectory reconciled as completed")
				}
				if _, err := os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
					t.Fatal("cancelled native trajectory published an accepted source")
				}
				s.request["identity"] = s.peer.UUID
				s.request["native_store"] = s.peer.Store
				s.peer.ExpectedContext = s.peer.UUID + ":initialized"
				s.peer.Wait = false
				s.peer.BeforeInit = ""
				s.peer.Events = append(s.peer.Events, map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": s.peer.UUID, "request_id": "reopened", "result": `{"findings":[{"severity":"P3","message":"reopened"}]}`})
				s.directory = filepath.Join(filepath.Dir(s.directory), "reopened")
				os.MkdirAll(s.directory, 0o755)
				s.save(t)
				if _, err := runNativeJob(s.directory); err != nil {
					t.Fatal(err)
				}
			} else {
				first, err := settleNativeReceipt(s.directory, receipt)
				if err != nil {
					t.Fatal(err)
				}
				second, err := settleNativeReceipt(s.directory, readReceipt(t, s.directory))
				if err != nil || !valuesEqual(first, second) || cursorCalls(t, s) != 2 {
					t.Fatal("original receipt replayed prompt")
				}
			}
		})
	}
}

// Absent overrides retain the CLI's native selection and saved context.
func TestCursorAbsentModelOptionsUseNativeSelection(t *testing.T) {
	s := newCursorScenario(t, "33333333-3333-4333-8333-333333333333", "native-default", "", true)
	delete(s.request, "model")
	delete(s.request, "reasoning_effort")
	s.save(t)
	source, err := runNativeJob(s.directory)
	if err != nil || getOf(source["selection"], "model") != "native-default" || getOf(source["selection"], "reasoning_effort") != "" || strings.Contains(strings.Join(s.peer.Args, "\n"), "--model") {
		t.Fatalf("absent native overrides: %v %v argv=%q", source, err, s.peer.Args)
	}
	s.resume(t)
	continued, err := runNativeJob(s.directory)
	if err != nil || continued["identity"] != source["identity"] || continued["native_store"] != source["native_store"] || getOf(continued["selection"], "model") != "native-default" || cursorCalls(t, s) != 3 {
		t.Fatalf("absent native continuation: %v %v", continued, err)
	}
	if err := cursorVerifySource(s.directory, continued); err != nil {
		t.Fatal(err)
	}
}

func TestCursorOriginalInvocationCarriesSavedStore(t *testing.T) {
	for _, completed := range []bool{true, false} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			s := newCursorScenario(t, "33333333-3333-4333-8333-333333333333", "native-default", "", true)
			action := filepath.Join(filepath.Dir(s.directory), "action")
			s.directory = filepath.Join(action, "original")
			if err := os.MkdirAll(s.directory, 0755); err != nil {
				t.Fatal(err)
			}
			delete(s.request, "model")
			delete(s.request, "reasoning_effort")
			materialPath := filepath.Join(filepath.Dir(action), "material.json")
			input := protocol.WorkerInput{Handler: "engine.gate.v2", Pending: protocol.PendingAction{ActionID: "issued-action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}
			if err := writeAtomicJSON(materialPath, worker.Material{ReadOnly: true, Input: input}); err != nil {
				t.Fatal(err)
			}
			s.request["engine"] = map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}
			if !completed {
				s.peer.Exit = 7
			}
			s.save(t)
			originalDirectory := s.directory
			if completed {
				if source, err := cursorExecute(s.request, s.directory, false); err != nil {
					t.Fatalf("original completion: %v %v", source, err)
				}
			} else {
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				writeRequest(t, s.directory, map[string]any{"kind": "native", "argv": []string{binary, "-test.run=^TestCursorNativeInvocation$"}, "cwd": s.workspace, "stdin": "", "environment": map[string]any{"FORMAL_GATES_CURSOR_INVOCATION": s.directory}, "engine": s.request["engine"]})
				supervisor := exec.Command(binary, "-test.run=^TestCursorInvocationSupervisor$")
				supervisor.Env = append(os.Environ(), "FORMAL_GATES_CURSOR_SUPERVISOR="+s.directory)
				if output, err := supervisor.CombinedOutput(); err != nil {
					t.Fatalf("original failed supervisor: %v %s", err, output)
				}
				body, digest, identity, err := NativeFailureEvidence("cursor", s.directory)
				if err != nil || len(body) == 0 || digest == "" || identity != s.peer.UUID {
					t.Fatalf("original failed invocation proof: %q %q %s %v", identity, digest, body, err)
				}
				t.Logf("original failed invocation identity=%s digest=%s failure=%s receipt=%v", identity, digest, body, readReceipt(t, s.directory))
			}
			identity, err := NativeStartEvidence("cursor", originalDirectory)
			if err != nil || identity != s.peer.UUID {
				t.Fatalf("original ACK: %q %v", identity, err)
			}
			s.resume(t)
			delete(s.request, "identity")
			delete(s.request, "native_store")
			impl, err := nativeImplementationFor("cursor")
			if err != nil {
				t.Fatal(err)
			}
			if completed {
				wrong := map[string]any{"provider": "cursor", "engine": s.request["engine"], "identity": "11111111-1111-4111-8111-111111111111"}
				if err := inheritNativeIdentity(s.directory, wrong, impl); err == nil || !strings.Contains(err.Error(), "differs from original source identity") {
					t.Fatalf("different requested identity: %v", err)
				}
				if err := inheritNativeIdentity(s.directory, s.request, impl); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := os.Stat(filepath.Join(originalDirectory, "source.json")); !os.IsNotExist(err) {
					t.Fatal("failed process published a completed source")
				}
				if string(readBytes(t, s.peer.Store)) != s.peer.UUID+":initialized" {
					t.Fatal("failed original process changed its initialized context")
				}
				s.peer.ExpectedContext = s.peer.UUID + ":initialized"
			}
			if !completed {
				if err := inheritNativeIdentity(s.directory, s.request, impl); err != nil {
					t.Fatal(err)
				}
			}
			if s.request["identity"] != s.peer.UUID || s.request["native_store"] != s.peer.Store || cursorCalls(t, s) != 2 {
				t.Fatalf("original invocation carrier: %v", s.request)
			}
			s.peer.Exit = 0
			s.save(t)
			continued, err := cursorExecute(s.request, s.directory, false)
			if err != nil || continued["identity"] != identity || continued["native_store"] != s.peer.Store || getOf(continued["selection"], "model") != "native-default" || cursorCalls(t, s) != 3 {
				t.Fatalf("original context continuation: %v %v", continued, err)
			}
			if err := cursorVerifySource(s.directory, continued); err != nil {
				t.Fatal(err)
			}
			t.Logf("continued identity=%s store=%s selection=%v calls=%d context=%s", continued["identity"], continued["native_store"], continued["selection"], cursorCalls(t, s), readBytes(t, s.peer.Store))
		})
	}
}

func cursorInvocationServices(directory string) ExecutionServices {
	return ExecutionServices{
		Admit: func(owned string, request map[string]any) (func(), error) {
			original, err := readJSONFile(filepath.Join(directory, "native-request.json"))
			if err != nil {
				return nil, err
			}
			if owned != directory || !valuesEqual(request["engine"], getOf(original, "engine")) {
				return nil, fmt.Errorf("Cursor invocation admission differs from original action/attempt")
			}
			return nil, nil
		},
		ClaimMessage:   func(string, string, bool) (*protocol.TaskMessage, error) { return nil, nil },
		ReceiptMessage: func(string, protocol.TaskMessage) error { return nil },
	}
}

func TestCursorNativeInvocation(t *testing.T) {
	directory := os.Getenv("FORMAL_GATES_CURSOR_INVOCATION")
	if directory == "" {
		return
	}
	os.Exit(RunNative(directory, os.Stderr, cursorInvocationServices(directory)))
}

func TestCursorInvocationSupervisor(t *testing.T) {
	directory := os.Getenv("FORMAL_GATES_CURSOR_SUPERVISOR")
	if directory == "" {
		return
	}
	os.Exit(Supervise(directory, cursorInvocationServices(directory)))
}

// Credential-store selection is native input, independent of the caller's
// ambient environment when a captured invocation is resumed.
func TestCursorCredentialStoreRemainsBound(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
	executable := textOf(getOf(getOf(s.request["binding"], "launch"), "executable"))
	t.Setenv("PATH", filepath.Dir(executable)+":"+os.Getenv("PATH"))
	s.peer.Environment["AGENT_CLI_CREDENTIAL_STORE"] = "file"
	for key, value := range s.peer.Environment {
		t.Setenv(key, value)
	}
	binding, err := cursorCaptureBinding()
	if err != nil {
		t.Fatal(err)
	}
	s.request["binding"] = binding
	s.request["probe"] = true
	s.save(t)
	t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "memory")
	report, err := runNativeJob(s.directory)
	if err != nil {
		t.Fatalf("captured native credential selection was lost: %v", err)
	}
	if getOf(getOf(binding, "environment"), "AGENT_CLI_CREDENTIAL_STORE") != "file" || !valuesEqual(getOf(report, "model_calls"), 0) {
		t.Fatalf("captured credential store/probe changed: %v %v", binding, report)
	}
	if cursorCalls(t, s) != 0 {
		t.Fatal("credential probe created or prompted a session")
	}
}

func TestCursorFreshConfigPathIdentity(t *testing.T) {
	for _, alias := range []bool{false, true} {
		t.Run(fmt.Sprintf("alias=%t", alias), func(t *testing.T) {
			s := newCursorScenario(t, "99999999-9999-4999-8999-999999999999", "model-a", "high", true)
			root := resolveNativePath(t.TempDir())
			real := filepath.Join(root, "real")
			if err := os.MkdirAll(real, 0755); err != nil {
				t.Fatal(err)
			}
			prefix := real
			if alias {
				prefix = filepath.Join(root, "link")
				if err := os.Symlink(real, prefix); err != nil {
					t.Fatal(err)
				}
			}
			requestedConfig := filepath.Join(prefix, "fresh", "config")
			t.Setenv("CURSOR_CONFIG_DIR", requestedConfig)
			t.Setenv("PATH", filepath.Dir(textOf(getOf(getOf(s.request["binding"], "launch"), "executable")))+string(os.PathListSeparator)+os.Getenv("PATH"))
			for key, value := range s.peer.Environment {
				if key != "CURSOR_CONFIG_DIR" && key != "CURSOR_PEER_SCRIPT" {
					t.Setenv(key, value)
				}
			}
			binding, captureErr := cursorCaptureBinding()
			if captureErr != nil {
				t.Fatal(captureErr)
			}
			binding["environment"].(map[string]any)["CURSOR_PEER_SCRIPT"] = s.script
			s.request["binding"] = binding
			config := textOf(getOf(getOf(binding, "environment"), "CURSOR_CONFIG_DIR"))
			t.Logf("original captured config=%s", config)
			s.peer.Environment["CURSOR_CONFIG_DIR"] = config
			s.request["binding"].(map[string]any)["environment"].(map[string]any)["CURSOR_CONFIG_DIR"] = config
			sum := md5.Sum([]byte(s.workspace))
			s.peer.Store = filepath.Join(config, "chats", hex.EncodeToString(sum[:]), s.peer.UUID, "store.db")
			s.save(t)
			source, err := runNativeJob(s.directory)
			t.Logf("original native calls=%d persisted-context=%q error=%v", cursorCalls(t, s), string(readBytes(t, s.peer.Store)), err)
			if err != nil {
				t.Fatalf("normal fresh Cursor config must complete: %v", err)
			}
			if source["identity"] != s.peer.UUID {
				t.Fatal("wrong original identity")
			}
			if string(readBytes(t, s.peer.Store)) != s.peer.UUID+":completed" {
				t.Fatal("native peer did not persist actual completion")
			}
		})
	}
}
