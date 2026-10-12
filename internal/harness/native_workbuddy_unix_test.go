//go:build !windows

package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/settings"
)

// The print-mode peer checks native CLI arguments, stdin, scoped configuration,
// and same-session resume. It never contacts a model or account service.
func TestWorkBuddyCLI(t *testing.T) {
	spec := os.Getenv("WORKBUDDY_PEER_SPEC")
	if spec == "" {
		return
	}
	data, err := os.ReadFile(spec)
	if err != nil {
		panic(err)
	}
	var expected map[string]string
	if err := json.Unmarshal(data, &expected); err != nil {
		panic(err)
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	args = args[1:]
	if slices.Equal(args, []string{"--help"}) {
		fmt.Println("--resume --session-id --json-schema stream-json --mcp-config --tools --permission-mode dontAsk")
		os.Exit(0)
	}
	fail := func(reason string) { fmt.Fprintln(os.Stderr, reason); os.Exit(91) }
	values := map[string]string{}
	for index := 0; index < len(args); {
		name := args[index]
		if name == "--print" || name == "--verbose" || name == "--strict-mcp-config" {
			values[name] = "present"
			index++
			continue
		}
		if index+1 >= len(args) {
			fail("missing argument value")
		}
		values[name] = args[index+1]
		index += 2
	}
	want := map[string]string{"--print": "present", "--verbose": "present", "--strict-mcp-config": "present", "--output-format": "stream-json", "--input-format": "text", "--tools": "", "--allowedTools": "mcp__formal-gates__workspace,mcp__formal-gates__submit_semantics", "--setting-sources": "", "--settings": `{"disableAllHooks":true}`, "--permission-mode": "dontAsk", "--model": "selected-model", "--effort": "high"}
	for name, value := range want {
		if values[name] != value {
			fail("wrong native option " + name)
		}
	}
	if len(values) != len(want)+2 {
		fail("unexpected native option")
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil || string(prompt) != "Review current input" {
		fail("wrong prompt stdin")
	}
	for _, key := range []string{"HOME", "CODEBUDDY_CONFIG_DIR", "WORKBUDDY_CONFIG_DIR", "ACC_PRODUCT_CONFIG_PATH"} {
		if os.Getenv(key) != expected[key] {
			fail("wrong native config " + key)
		}
	}
	identity := values["--session-id"]
	if resume := values["--resume"]; resume != "" {
		identity = resume
		if values["--session-id"] != "" {
			fail("resume allocated another session")
		}
		old, e := os.ReadFile(expected["history"])
		if e != nil || string(old) != identity {
			fail("wrong native resume identity")
		}
	} else {
		if identity == "" {
			fail("missing fresh session identity")
		}
		if err := os.WriteFile(expected["history"], []byte(identity), 0600); err != nil {
			fail(err.Error())
		}
	}
	cwd, _ := os.Getwd()
	if cwd != resolveNativePath(filepath.Join(os.TempDir(), "formal-gates-workbuddy", identity)) {
		fail("wrong native carrier cwd")
	}
	raw, e := os.ReadFile(values["--mcp-config"])
	if e != nil {
		fail("missing MCP config")
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil || getOf(getOf(getOf(config, "mcpServers"), "formal-gates"), "command") == nil {
		fail("wrong MCP server")
	}
	if getOf(getOf(getOf(config, "mcpServers"), "formal-gates"), "alwaysLoad") != true {
		fail("managed MCP tools deferred behind disabled discovery")
	}
	calls, _ := os.OpenFile(expected["calls"], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	fmt.Fprintln(calls, identity)
	calls.Close()
	store := filepath.Join(expected["CODEBUDDY_CONFIG_DIR"], "projects", "native-project")
	if err := os.MkdirAll(store, 0700); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(filepath.Join(store, identity+".jsonl"), []byte("native session "+identity), 0600); err != nil {
		fail(err.Error())
	}
	encoder := json.NewEncoder(os.Stdout)
	initialization := map[string]any{"type": "system", "subtype": "init", "session_id": identity, "uuid": identity, "cwd": cwd, "model": "selected-model", "effort": "high", "__timestamp": "2026-01-01T00:00:00Z"}
	encoder.Encode(initialization)
	if expected["wait"] == "true" {
		time.Sleep(time.Hour)
	}
	encoder.Encode(map[string]any{"type": "assistant", "uuid": "response-" + values["--resume"], "session_id": identity, "message": map[string]any{"usage": map[string]any{"input_tokens": 11, "output_tokens": 7, "cache_read_input_tokens": 2}}})
	server := getOf(getOf(config, "mcpServers"), "formal-gates")
	peerSubmitSemantics(textOf(listOf(getOf(server, "args"))[2]), map[string]any{"summary": "native " + identity})
	encoder.Encode(map[string]any{"type": "result", "subtype": "success", "uuid": "result-" + identity, "session_id": identity, "is_error": false, "structured_output": map[string]any{"summary": "native " + identity}})
	initialization["__timestamp"] = "2026-01-01T00:00:01Z"
	encoder.Encode(initialization)
	os.Exit(0)
}

func TestWorkBuddyPrintLifecyclePreservesNativeIdentityAndUsage(t *testing.T) {
	home, workspace, job := resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir()), resolveNativePath(t.TempDir())
	product := filepath.Join(home, ".workbuddy", "cache", "acc-product-config-v3.json")
	if err := os.MkdirAll(filepath.Dir(product), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(product, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(home, "peer.json")
	history := filepath.Join(home, "session")
	calls := filepath.Join(home, "calls")
	expected := map[string]string{"HOME": home, "CODEBUDDY_CONFIG_DIR": filepath.Join(home, ".workbuddy"), "WORKBUDDY_CONFIG_DIR": filepath.Join(home, ".workbuddy"), "ACC_PRODUCT_CONFIG_PATH": product, "history": history, "calls": calls}
	body, _ := json.Marshal(expected)
	os.WriteFile(spec, body, 0600)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	peer := filepath.Join(home, "codebuddy")
	os.WriteFile(peer, []byte("#!/bin/sh\nexec "+("'"+strings.ReplaceAll(executable, "'", "'\"'\"'")+"'")+" -test.run '^TestWorkBuddyCLI$' -- \"$@\"\n"), 0700)
	t.Setenv("WORKBUDDY_PEER_SPEC", spec)
	binding, err := NativeBinding("workbuddy", settings.Values{Native: map[string]string{"workbuddyExecutable": peer, "workbuddyHome": home}})
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": "workbuddy", "binding": binding, "workspace": workspace, "prompt": "Review current input", "schema": map[string]any{"type": "object"}, "read_only": true, "model": "selected-model", "reasoning_effort": "high"}
	writeAtomicJSON(filepath.Join(job, "native-request.json"), request)
	source, err := workbuddyExecute(request, job, false)
	if err != nil {
		raw, _ := os.ReadFile(filepath.Join(job, "host.stderr"))
		t.Fatalf("%v: %s", err, raw)
	}
	identity := textOf(getOf(source, "identity"))
	if identity == "" {
		t.Fatal("missing observed identity")
	}
	if err := workbuddyVerifySource(job, source); err != nil {
		t.Fatal(err)
	}
	usage, err := cost.ParseTranscript("workbuddy", filepath.Join(job, "native-events.jsonl"))
	if err != nil || usage.InputCacheMissTokens != 11 || usage.OutputTokens != 7 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	eventsPath := filepath.Join(job, "native-events.jsonl")
	originalEvents, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := decodeNativeJSON([]byte(strings.Split(string(originalEvents), "\n")[0]))
	if err != nil {
		t.Fatal(err)
	}
	changedInit := objectOrEmpty(initial)
	changedInit["model"] = "other-model"
	for _, late := range []struct {
		name  string
		event any
	}{
		{"assistant", map[string]any{"type": "assistant", "session_id": identity, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "additional answer"}}}}},
		{"result", getOf(source, "native_result")},
		{"tool", map[string]any{"type": "tool", "session_id": identity}},
		{"changed-init", changedInit},
	} {
		t.Run(late.name, func(t *testing.T) {
			body, err := CanonicalJSON(late.event)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(eventsPath, append(append([]byte{}, originalEvents...), append(body, '\n')...), 0600); err != nil {
				t.Fatal(err)
			}
			for _, observe := range []func() (map[string]any, error){
				func() (map[string]any, error) { return streamObserve(workbuddyStream, job) },
				func() (map[string]any, error) {
					return workbuddyReconcile(job, map[string]any{"exit_code": 0, "error": ""})
				},
			} {
				if _, err := observe(); err == nil || !strings.Contains(err.Error(), "root message after terminal") {
					t.Fatalf("late root event accepted: %v", err)
				}
			}
		})
	}
	if err := os.WriteFile(eventsPath, originalEvents, 0600); err != nil {
		t.Fatal(err)
	}
	resumed := t.TempDir()
	request["identity"] = identity
	writeAtomicJSON(filepath.Join(resumed, "native-request.json"), request)
	continued, err := workbuddyExecute(request, resumed, false)
	if err != nil || getOf(continued, "identity") != identity {
		t.Fatalf("resume=%v err=%v", continued, err)
	}
	if _, err := os.Stat(filepath.Join(os.TempDir(), "formal-gates-workbuddy", identity)); !os.IsNotExist(err) {
		t.Fatal("carrier cleanup missing")
	}
	raw, err := os.ReadFile(calls)
	if err != nil || strings.Count(string(raw), "\n") != 2 {
		t.Fatalf("actual native calls=%q err=%v", raw, err)
	}
	// A lost response is observed through the same native evidence, with no call.
	os.Remove(filepath.Join(resumed, "source.json"))
	observed, err := workbuddyReconcile(resumed, map[string]any{"exit_code": 0, "error": ""})
	if err != nil || getOf(observed, "identity") != identity {
		t.Fatalf("recovery=%v err=%v", observed, err)
	}
	missing := t.TempDir()
	os.Remove(filepath.Join(home, ".workbuddy", "projects", "native-project", identity+".jsonl"))
	writeAtomicJSON(filepath.Join(missing, "native-request.json"), request)
	if _, err := workbuddyExecute(request, missing, false); err == nil {
		t.Fatal("missing original native session did not block resume")
	}
	if _, err := os.Stat(filepath.Join(missing, "native-process.json")); !os.IsNotExist(err) {
		t.Fatal("offline session refusal started a native invocation")
	}
	after, _ := os.ReadFile(calls)
	if string(after) != string(raw) {
		t.Fatal("observation repeated native invocation")
	}
	// Cancellation goes through the actual detached supervisor and original receipt.
	expected["wait"] = "true"
	body, _ = json.Marshal(expected)
	os.WriteFile(spec, body, 0600)
	owned := t.TempDir()
	cancelled := filepath.Join(owned, "action", "call")
	os.MkdirAll(cancelled, 0700)
	delete(request, "identity")
	materialPath := filepath.Join(owned, "material.json")
	material := worker.Material{ReadOnly: true, Input: protocol.WorkerInput{Pending: protocol.PendingAction{ActionID: "native-cancel", AttemptID: "native-attempt"}}}
	writeAtomicJSON(materialPath, material)
	ref := map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": "native-cancel", "attemptId": "native-attempt"}}
	request["engine"] = ref
	writeAtomicJSON(filepath.Join(cancelled, "native-request.json"), request)
	bin := filepath.Join(owned, "formal-gates")
	os.WriteFile(bin, []byte("#!/bin/sh\nexec "+("'"+strings.ReplaceAll(executable, "'", "'\"'\"'")+"'")+" -test.run '^TestWorkBuddyHarness$' -- \"$@\"\n"), 0700)
	writeRequest(t, cancelled, map[string]any{"kind": "native", "argv": []string{bin, "harness", "native", cancelled}, "cwd": workspace, "stdin": "", "engine": ref})
	cmd := exec.Command(bin, "harness", "supervise", cancelled)
	DetachSupervisor(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	waitForFile(t, filepath.Join(cancelled, "identity.json"))
	start, err := readJSONFile(filepath.Join(cancelled, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := StopNativeJob(cancelled); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt := readReceipt(t, cancelled)
	if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
		t.Fatalf("native cancellation receipt=%v", receipt)
	}
	if NativeJobAlive(cancelled) {
		t.Fatal("cancelled WorkBuddy supervisor remains running")
	}
	if _, err := NativeJobSource("workbuddy", cancelled); err == nil {
		t.Fatal("cancelled native invocation became successful result evidence")
	}
	if _, err := os.Stat(textOf(getOf(start, "nativeWorkspace"))); !os.IsNotExist(err) {
		t.Fatal("cancelled native carrier not cleaned")
	}

}

// This test process supplies the synthetic engine admission prerequisite; the
// supervisor itself creates and validates the real request digest and owner.
func TestWorkBuddyHarness(t *testing.T) {
	if os.Getenv("WORKBUDDY_PEER_SPEC") == "" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		return
	}
	args = args[1:]
	if len(args) == 0 || args[0] != "harness" {
		return
	}
	services := ExecutionServices{Admit: func(string, map[string]any) (func(), error) { return func() {}, nil }, ClaimMessage: func(string, string, bool) (*protocol.TaskMessage, error) { return nil, nil }, ReceiptMessage: func(string, protocol.TaskMessage) error { return nil }}
	os.Exit(RunCommand(args[1:], os.Stdin, os.Stdout, os.Stderr, services))
}
