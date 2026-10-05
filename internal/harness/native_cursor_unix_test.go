//go:build !windows

package harness

// A native Cursor CLI peer checks its actual argv, environment, cwd and saved
// context. Every session/model input owns different native evidence; no model
// or installed host is invoked.
import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/runner"
)

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
		fmt.Println("--mode ask --model bracket overrides --resume stream-json create-chat")
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
		if !peer.SkipStore {
			sum := md5.Sum([]byte(cwd))
			store := filepath.Join(os.Getenv("CURSOR_CONFIG_DIR"), "chats", hex.EncodeToString(sum[:]), peer.UUID, "store.db")
			os.MkdirAll(filepath.Dir(store), 0o755)
			os.WriteFile(store, []byte(peer.UUID+":created"), 0o644)
		}
		fmt.Println(peer.UUID)
		os.Exit(0)
	}
	if !reflect.DeepEqual(args, peer.Args) {
		fmt.Fprintf(os.Stderr, "wrong argv: %q != %q\n", args, peer.Args)
		os.Exit(91)
	}
	prior, err := os.ReadFile(peer.Store)
	if err != nil || string(prior) != peer.ExpectedContext {
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
	if peer.Exit == 0 {
		os.WriteFile(peer.Store, []byte(peer.UUID+":completed"), 0o644)
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
	request := map[string]any{"provider": "cursor", "binding": binding, "workspace": workspace, "identity": "", "model": model, "reasoning_effort": effort, "read_only": readOnly, "prompt": "Review " + uuid, "schema": plainWorkerSchema()}
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
	answer := map[string]any{"status": "PASS", "summary": "native " + uuid + " " + model, "findings": []any{}, "check_ids": []any{}}
	terminal := map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": uuid, "request_id": "request-" + uuid, "result": mustCanonical(t, answer), "usage": map[string]any{"inputTokens": len(uuid), "outputTokens": len(model)}}
	sum := md5.Sum([]byte(workspace))
	store := filepath.Join(config, "chats", hex.EncodeToString(sum[:]), uuid, "store.db")
	s := &cursorScenario{request: request, directory: directory, workspace: workspace, script: script, peer: cursorPeer{UUID: uuid, Workspace: workspace, Environment: environment, Calls: filepath.Join(root, "calls.jsonl"), Store: store, ExpectedContext: uuid + ":created", Config: nativeConfig, Events: []any{init, terminal}}}
	s.save(t)
	return s
}
func (s *cursorScenario) save(t *testing.T) {
	t.Helper()
	model := textOf(s.request["model"])
	effort := textOf(s.request["reasoning_effort"])
	if effort != "" && !strings.Contains(model, "effort=") {
		if strings.Contains(model, "[") {
			model = model[:len(model)-1] + ",effort=" + effort + "]"
		} else {
			model += "[effort=" + effort + "]"
		}
	}
	s.peer.Args = []string{"-p", "--output-format", "stream-json", "--workspace", s.workspace, "--resume", s.peer.UUID}
	if s.request["read_only"] == true {
		s.peer.Args = append(s.peer.Args, "--mode", "ask")
	}
	if model != "" {
		s.peer.Args = append(s.peer.Args, "--model", model)
	}
	schema, _ := schemaForPrompt(s.request["schema"])
	s.peer.Args = append(s.peer.Args, textOf(s.request["prompt"])+"\nReturn ONLY one JSON object matching this schema:\n"+string(schema))
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
			source, err := runNativeJob(s.directory)
			if err != nil {
				t.Fatal(err)
			}
			if source["identity"] != uuid || source["native_store"] != s.peer.Store || source["native_request"] != "request-"+uuid {
				t.Fatalf("native binding %v", source)
			}
			if err := cursorVerifySource(s.directory, source); err != nil {
				t.Fatal(err)
			}
			before := proofsOf(t, s.directory, nativeSourceFiles)
			observed, err := cursorObserve(s.directory, false)
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
		{"create-store-absent", "native-resource-missing", func(s *cursorScenario) { s.peer.SkipStore = true }},
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
		{"invalid-answer", "NATIVE_RESULT", func(s *cursorScenario) { objectOrEmpty(s.peer.Events[1])["result"] = "not JSON" }},
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
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	sourceBytes := readBytes(t, filepath.Join(s.directory, "native-source.json"))
	eventBytes := readBytes(t, filepath.Join(s.directory, "native-events.jsonl"))
	os.Remove(filepath.Join(s.directory, "source.json"))
	os.Remove(filepath.Join(s.directory, "answer.json"))
	os.Remove(filepath.Join(s.directory, "answer.raw.txt"))
	writeAtomicJSON(filepath.Join(s.directory, "native-error.json"), map[string]any{"error": "RuntimeBlocked: NATIVE_RESULT: could not frame output"})
	source, err := cursorReconcile(s.directory, map[string]any{"exit_code": 1, "error": ""})
	if err != nil || getOf(source["reconciliation"], "new_model_calls") != 0 {
		t.Fatalf("reconcile %v %v", source, err)
	}
	if !bytes.Equal(sourceBytes, readBytes(t, filepath.Join(s.directory, "native-source.json"))) || !bytes.Equal(eventBytes, readBytes(t, filepath.Join(s.directory, "native-events.jsonl"))) || cursorCalls(t, s) != 2 {
		t.Fatal("reconciliation changed original facts or replayed")
	}
	writeAtomicJSON(filepath.Join(s.directory, "native-error.json"), map[string]any{"error": "RuntimeBlocked: CURSOR_PROCESS: interrupted"})
	if _, err := cursorReconcile(s.directory, map[string]any{"exit_code": 1, "error": ""}); err == nil {
		t.Fatal("interruption laundered")
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
		source, err := cursorObserve(s.directory, false)
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
				source, err := cursorObserve(s.directory, false)
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
				s.request["identity"] = s.peer.UUID
				s.request["native_store"] = s.peer.Store
				s.peer.Wait = false
				s.peer.BeforeInit = ""
				s.peer.Events = append(s.peer.Events, map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": s.peer.UUID, "request_id": "reopened", "result": `{"status":"PASS","summary":"reopened","findings":[],"check_ids":[]}`})
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

func TestCursorRequestCarriesExactSavedSource(t *testing.T) {
	s := newCursorScenario(t, "11111111-1111-4111-8111-111111111111", "model-a", "high", true)
	if _, err := runNativeJob(s.directory); err != nil {
		t.Fatal(err)
	}
	c := &Compose{dir: filepath.Dir(s.directory)}
	binding := objectOrEmpty(s.request["binding"])
	brief := workerBrief{Role: "repair"}
	target := filepath.Join(c.dir, "request-only")
	if err := c.writeNativeRequest(target, brief, s.workspace, binding, s.peer.UUID, "job", workerMaterials{}); err != nil {
		t.Fatal(err)
	}
	request, _ := readJSONFile(filepath.Join(target, "native-request.json"))
	if getOf(request, "native_store") != s.peer.Store || getOf(request, "identity") != s.peer.UUID {
		t.Fatalf("source carrier %v", request)
	}
	if err := c.writeNativeRequest(target, brief, s.workspace, binding, "22222222-2222-4222-8222-222222222222", "job", workerMaterials{}); err == nil {
		t.Fatal("another session's source accepted")
	}
	// The kernel retains both carriers across task generations and owner reopen.
	directory := t.TempDir()
	store, err := runner.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	spec := runner.TaskSpec{TaskID: "worker", Input: []byte("input")}
	if err := store.CreateRun(ctx, "cursor", []runner.TaskSpec{spec}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionSource(ctx, "worker", s.peer.UUID, "job"); err != nil {
		t.Fatal(err)
	}
	spec.Input = []byte("repair")
	if err := store.ReviseTask(ctx, "worker", spec); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = runner.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity, sourceJob, err := store.SessionSource(ctx, "worker")
	if err != nil || identity != s.peer.UUID || sourceJob != "job" {
		t.Fatalf("reopen carrier %q %q %v", identity, sourceJob, err)
	}
	if err := store.ClearSession(ctx, "worker"); err != nil {
		t.Fatal(err)
	}
	identity, sourceJob, err = store.SessionSource(ctx, "worker")
	if err != nil || identity != "" || sourceJob != "" {
		t.Fatal("restart retained native source")
	}
}
