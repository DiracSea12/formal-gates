//go:build !windows

package harness

// A local app-server peer checks complete native RPC parameters and original
// reply identities. It persists native history across real process exits;
// inputs select distinct threads, turns, answers and usage. No installed host
// or model is started.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type codexPeerInput struct {
	Workspace    string         `json:"workspace"`
	Home         string         `json:"home"`
	ThreadParams map[string]any `json:"thread_params"`
	TurnParams   map[string]any `json:"turn_params"`
	Mode         string         `json:"mode"`
	Reverse      []any          `json:"reverse"`
	Replies      []any          `json:"replies"`
}

func codexPeerID(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x-%x-7%03x-8%03x-%x", sum[0:4], sum[4:6], (int(sum[6])<<8|int(sum[7]))&0xfff, (int(sum[8])<<8|int(sum[9]))&0xfff, sum[10:16])
}

func TestCodexRPCPeer(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "--stdio" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "peer.json"))
	if err != nil {
		panic(err)
	}
	var peer codexPeerInput
	if err = json.Unmarshal(raw, &peer); err != nil {
		panic(err)
	}
	cwd, _ := os.Getwd()
	if cwd != peer.Workspace || os.Getenv("HOME") != peer.Home {
		panic("native environment/workspace differs")
	}
	if os.Args[len(os.Args)-2] != "app-server" {
		panic("native argv differs")
	}
	log, err := os.OpenFile(filepath.Join(os.Getenv("CODEX_HOME"), "rpc.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	emit := func(frame any) { line, _ := json.Marshal(frame); os.Stdout.Write(append(line, '\n')) }
	reply := func(frame any, result any) { emit(map[string]any{"id": getOf(frame, "id"), "result": result}) }
	scanner := bufio.NewScanner(os.Stdin)
	read := func() any {
		if !scanner.Scan() {
			panic("unexpected input EOF")
		}
		line := scanner.Bytes()
		log.Write(append(append([]byte{}, line...), '\n'))
		value, err := decodeNativeJSON(line)
		if err != nil {
			panic(err)
		}
		return value
	}
	check := func(got, want any) {
		if !valuesEqual(got, want) {
			panic(fmt.Sprintf("native input differs: %s != %s", textOf(got), textOf(want)))
		}
	}
	frame := read()
	check(getOf(frame, "method"), "initialize")
	check(getOf(frame, "params"), map[string]any{"clientInfo": map[string]any{"name": "formal-gates", "title": "Formal Gates", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}})
	reply(frame, map[string]any{"userAgent": "codex-cli/0.154.0", "codexHome": os.Getenv("CODEX_HOME"), "platformFamily": "unix", "platformOs": "macos"})
	frame = read()
	check(frame, map[string]any{"method": "initialized", "params": map[string]any{}})
	if peer.Mode == "probe" {
		if scanner.Scan() {
			panic("probe started native work")
		}
		os.Exit(0)
	}
	frame = read()
	method := getOf(frame, "method")
	check(getOf(frame, "params"), peer.ThreadParams)
	thread := codexPeerID(peer.Workspace + textOf(peer.TurnParams))
	historyPath := func(id string) string { return filepath.Join(os.Getenv("CODEX_HOME"), id+".json") }
	var history []string
	if valuesEqual(method, "thread/resume") {
		thread = textOf(peer.ThreadParams["threadId"])
		historyRaw, err := os.ReadFile(historyPath(thread))
		if err != nil || peer.Mode == "auth" {
			message := "peer reports stored thread missing"
			if peer.Mode == "auth" {
				message = "authentication required"
			}
			emit(map[string]any{"id": getOf(frame, "id"), "error": map[string]any{"code": -32000, "message": message, "data": map[string]any{"threadId": thread}}})
			os.Exit(0)
		}
		json.Unmarshal(historyRaw, &history)
	} else {
		check(method, "thread/start")
	}
	originalThread := thread
	if peer.Mode == "resume_identity" {
		thread = codexPeerID("another thread")
	}
	nativeThread := map[string]any{"id": thread, "sessionId": codexPeerID("shared tree"), "cliVersion": "0.154.0", "createdAt": 1, "updatedAt": 1, "cwd": cwd, "ephemeral": false, "modelProvider": "peer", "preview": "peer history", "projectId": nil, "source": "appServer", "status": map[string]any{"type": "idle"}, "turns": []any{}, "path": historyPath(thread)}
	sandboxType := "workspaceWrite"
	if valuesEqual(peer.ThreadParams["sandbox"], "read-only") {
		sandboxType = "readOnly"
	}
	model := getOrOf(peer.ThreadParams, "model", "peer-default")
	if peer.Mode == "selection" {
		model = "wrong-model"
	}
	reply(frame, map[string]any{"thread": nativeThread, "model": model, "modelProvider": "peer", "reasoningEffort": "medium", "cwd": cwd, "sandbox": map[string]any{"type": sandboxType}, "approvalPolicy": "on-request", "approvalsReviewer": "user"})
	if peer.Mode == "resume_identity" || peer.Mode == "selection" {
		for scanner.Scan() {
		}
		os.Exit(0)
	}
	frame = read()
	check(getOf(frame, "method"), "turn/start")
	expected := map[string]any{}
	for k, v := range peer.TurnParams {
		expected[k] = v
	}
	expected["threadId"] = thread
	check(getOf(frame, "params"), expected)
	turn := codexPeerID(thread + textOf(expected))
	reply(frame, map[string]any{"turn": map[string]any{"id": turn, "items": []any{}, "status": "inProgress"}})
	// Persist the original prompt immediately: interrupt/transport close leaves
	// this context available to the next native thread/resume process.
	prompt := textOf(getOf(listOf(expected["input"])[0], "text"))
	history = append(history, prompt)
	historyRaw, _ := json.Marshal(history)
	os.WriteFile(historyPath(originalThread), historyRaw, 0o644)
	for i, reverse := range peer.Reverse {
		params := objectOrEmpty(getOf(reverse, "params"))
		params["threadId"] = thread
		params["turnId"] = turn
		ident := fmt.Sprintf("question-%d", i)
		emit(map[string]any{"id": ident, "method": getOf(reverse, "method"), "params": params})
		answered := read()
		check(getOf(answered, "id"), ident)
		check(getOf(answered, "result"), peer.Replies[i])
		emit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"type": "commandExecution", "id": getOf(params, "itemId"), "command": "echo", "cwd": cwd, "status": "completed", "commandActions": []any{}}, "completedAtMs": 1}})
	}
	status := "completed"
	if peer.Mode == "wait" {
		frame = read()
		check(getOf(frame, "method"), "turn/interrupt")
		check(getOf(frame, "params"), map[string]any{"threadId": thread, "turnId": turn})
		reply(frame, map[string]any{})
		status = "interrupted"
	}
	if peer.Mode == "failed" || peer.Mode == "interrupted" {
		status = peer.Mode
	}
	answer := map[string]any{"status": "PASS", "summary": strings.Join(history, " / "), "findings": []any{}, "check_ids": []any{}}
	text, _ := json.Marshal(answer)
	if peer.Mode == "bad_json" {
		text = []byte("invalid business json")
	}
	phase := any("final_answer")
	if peer.Mode == "unknown" {
		phase = nil
	}
	if peer.Mode == "commentary" {
		phase = "commentary"
	}
	item := map[string]any{"id": codexPeerID(turn + "message"), "type": "agentMessage", "text": string(text), "phase": phase}
	eventThread, eventTurn := thread, turn
	if peer.Mode == "wrong_thread" {
		eventThread = codexPeerID("child")
	}
	if peer.Mode == "wrong_turn" {
		eventTurn = codexPeerID("other turn")
	}
	emit(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": eventThread, "turnId": eventTurn, "item": item, "completedAtMs": 2}})
	usage := map[string]any{"last": map[string]any{"inputTokens": len(prompt), "cachedInputTokens": 0, "outputTokens": len(text), "reasoningOutputTokens": 0, "totalTokens": len(prompt) + len(text)}, "total": map[string]any{"inputTokens": len(strings.Join(history, "")), "cachedInputTokens": 0, "outputTokens": len(text), "reasoningOutputTokens": 0, "totalTokens": len(prompt) + len(text)}, "modelContextWindow": 10000}
	for i := 0; i < 2; i++ {
		emit(map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": eventThread, "turnId": eventTurn, "tokenUsage": usage}})
	}
	if peer.Mode == "drop" || peer.Mode == "wrong_thread" || peer.Mode == "wrong_turn" {
		os.Exit(0)
	}
	emit(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": eventThread, "turn": map[string]any{"id": eventTurn, "status": status, "items": []any{map[string]any{"id": codexPeerID(turn + "comment"), "type": "agentMessage", "text": "late commentary", "phase": "commentary"}, item}}}})
	frame = read()
	check(getOf(frame, "method"), "thread/unsubscribe")
	check(getOf(frame, "params"), map[string]any{"threadId": thread})
	reply(frame, map[string]any{})
	if scanner.Scan() {
		panic("unexpected work after unsubscribe")
	}
	if peer.Mode == "nonzero" {
		os.Exit(3)
	}
	os.Exit(0)
}

type codexScenario struct {
	request                            map[string]any
	peer                               codexPeerInput
	directory, workspace, home, config string
}

func newCodexScenario(t *testing.T) *codexScenario {
	t.Helper()
	root := resolveNativePath(t.TempDir())
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	config := filepath.Join(home, "codex")
	directory := filepath.Join(root, "job")
	for _, p := range []string{workspace, config, directory} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable, _ := os.Executable()
	script := filepath.Join(root, "codex")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run=^TestCodexRPCPeer$ -- \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	binding, err := buildCaptureBinding("codex", map[string]any{"executable": script})
	if err != nil {
		t.Fatal(err)
	}
	binding["environment"] = map[string]any{"HOME": home, "CODEX_HOME": config}
	schema := map[string]any{"type": "object"}
	request := map[string]any{"provider": "codex", "binding": binding, "workspace": workspace, "prompt": "original prompt", "schema": schema, "read_only": false, "model": "peer-model", "reasoning_effort": "high"}
	s := &codexScenario{request: request, directory: directory, workspace: workspace, home: home, config: config}
	// Retain raw local peer evidence when the execution harness requests it.
	// This is test output only; the production adapter has no fixture input.
	if evidence := os.Getenv("CODEX_NATIVE_TEST_EVIDENCE"); evidence != "" {
		target := filepath.Join(evidence, strings.ReplaceAll(t.Name(), "/", "_")+"_"+filepath.Base(root))
		t.Cleanup(func() {
			err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				output := filepath.Join(target, relative)
				if entry.IsDir() {
					return os.MkdirAll(output, 0o755)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return os.WriteFile(output, data, 0o644)
			})
			if err != nil {
				t.Errorf("retain raw evidence: %v", err)
			}
		})
	}
	s.peer = codexPeerInput{Workspace: workspace, Home: home, ThreadParams: map[string]any{}, TurnParams: map[string]any{}}
	return s
}

func (s *codexScenario) save(t *testing.T) {
	t.Helper()
	sandbox := "workspace-write"
	if s.request["read_only"] == true {
		sandbox = "read-only"
	}
	s.peer.ThreadParams = map[string]any{"cwd": s.workspace, "sandbox": sandbox, "approvalPolicy": "on-request", "approvalsReviewer": "user"}
	if model, _ := s.request["model"].(string); model != "" {
		s.peer.ThreadParams["model"] = model
	}
	if id, _ := s.request["identity"].(string); id != "" {
		s.peer.ThreadParams["threadId"] = id
		s.peer.ThreadParams["excludeTurns"] = true
	} else {
		s.peer.ThreadParams["ephemeral"] = false
	}
	s.peer.TurnParams = map[string]any{"input": []any{map[string]any{"type": "text", "text": s.request["prompt"]}}, "outputSchema": s.request["schema"]}
	for _, pair := range [][2]string{{"model", "model"}, {"reasoning_effort", "effort"}} {
		if v, _ := s.request[pair[0]].(string); v != "" {
			s.peer.TurnParams[pair[1]] = v
		}
	}
	if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), s.request); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(s.config, "peer.json"), s.peer); err != nil {
		t.Fatal(err)
	}
}

func (s *codexScenario) execute(t *testing.T) (map[string]any, error) {
	t.Helper()
	s.save(t)
	return codexExecute(s.request, s.directory, s.peer.Mode == "probe")
}

func TestCodexNativeContinuationAndRecovery(t *testing.T) {
	s := newCodexScenario(t)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	if source["provider"] != "codex" || getOf(source, "identity") == "" || valuesEqual(source["identity"], getOf(getOf(source, "native_result"), "id")) {
		t.Fatalf("source: %v", source)
	}
	if err := codexVerifySource(s.directory, source); err != nil {
		t.Fatal(err)
	}
	if valuesEqual(getOf(getOf(source, "usage_evidence"), "last"), nil) || !valuesEqual(getOf(source, "selection"), map[string]any{"model": "peer-model", "reasoning_effort": "high"}) {
		t.Fatalf("usage/selection: %v", source)
	}
	firstLog, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	recovered, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
	if err != nil || !valuesEqual(source, recovered) {
		t.Fatalf("recovery %v: %v", err, recovered)
	}
	secondLog, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if !bytes.Equal(firstLog, secondLog) {
		t.Fatal("recovery replayed native work")
	}
	firstID := source["identity"]
	s.directory = filepath.Join(filepath.Dir(s.directory), "continued")
	os.MkdirAll(s.directory, 0o755)
	s.request["identity"] = firstID
	s.request["prompt"] = "continue prompt"
	s.request["model"] = "another-model"
	s.request["reasoning_effort"] = "low"
	continued, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(continued["identity"], firstID) || valuesEqual(continued["native_turn"], source["native_turn"]) {
		t.Fatalf("continuation: %v", continued)
	}
	answer, _ := readJSONFile(filepath.Join(s.directory, "answer.json"))
	if getOf(answer, "summary") != "original prompt / continue prompt" {
		t.Fatalf("history: %v", answer)
	}
	if valuesEqual(source["usage_evidence"], continued["usage_evidence"]) {
		t.Fatal("usage did not follow original input")
	}
}

func TestCodexNativeTerminalBoundaries(t *testing.T) {
	for _, mode := range []string{"drop", "wrong_thread", "wrong_turn", "failed", "interrupted", "commentary", "bad_json", "selection", "resume_identity", "nonzero", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s := newCodexScenario(t)
			if mode == "resume_identity" {
				first, err := s.execute(t)
				if err != nil {
					t.Fatal(err)
				}
				s.directory = filepath.Join(filepath.Dir(s.directory), "continued")
				os.MkdirAll(s.directory, 0o755)
				s.request["identity"] = first["identity"]
			}
			s.peer.Mode = mode
			source, err := s.execute(t)
			if mode == "unknown" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || source != nil {
				t.Fatalf("%s accepted: %v", mode, source)
			}
			if mode == "bad_json" {
				if !strings.Contains(err.Error(), "NATIVE_RESULT:") {
					t.Fatal(err)
				}
				writeAtomicJSON(filepath.Join(s.directory, "native-error.json"), map[string]any{"error": nativeErrorText(err)})
				if _, err = codexReconcile(s.directory, map[string]any{"exit_code": 1, "error": ""}); err == nil {
					t.Fatal("invalid business output became valid")
				}
			}
			if _, err = os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
				t.Fatal("failure published source")
			}
		})
	}
}

func TestCodexNativeProbeAndNativeErrors(t *testing.T) {
	t.Run("probe", func(t *testing.T) {
		s := newCodexScenario(t)
		s.peer.Mode = "probe"
		source, err := s.execute(t)
		if err != nil || len(objectOrEmpty(source["selection"])) != 0 || !valuesEqual(source["model_calls"], 0) {
			t.Fatalf("probe %v %v", source, err)
		}
	})
	for _, mode := range []string{"missing", "auth"} {
		t.Run(mode, func(t *testing.T) {
			s := newCodexScenario(t)
			s.request["identity"] = codexPeerID("missing")
			s.peer.Mode = mode
			_, err := s.execute(t)
			if err == nil {
				t.Fatal("error accepted")
			}
			if _, ok := err.(*sessionUnavailableError); ok {
				t.Fatal("unproven native error guessed as missing")
			}
			if !strings.Contains(err.Error(), "-32000") || !strings.Contains(err.Error(), "thread/resume") {
				t.Fatal(err)
			}
		})
	}
}

func TestCodexNativeParallelIsolation(t *testing.T) {
	scenarios := []*codexScenario{newCodexScenario(t), newCodexScenario(t)}
	scenarios[1].request["prompt"] = "independent input"
	scenarios[1].request["read_only"] = true
	for _, s := range scenarios {
		s.save(t)
	}
	sources := make([]map[string]any, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, s := range scenarios {
		wg.Add(1)
		go func() { defer wg.Done(); sources[i], errs[i] = codexExecute(s.request, s.directory, false) }()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if valuesEqual(sources[0]["identity"], sources[1]["identity"]) || valuesEqual(sources[0]["native_turn"], sources[1]["native_turn"]) {
		t.Fatal("parallel invocations shared native identity")
	}
	for i, s := range scenarios {
		source, err := codexObserve(s.directory)
		if err != nil || !valuesEqual(source, sources[i]) {
			t.Fatalf("parallel recovery %d: %v", i, err)
		}
	}
}

func waitCodexPending(t *testing.T, directory, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		records, err := readNativePendings(directory)
		if err == nil {
			for _, record := range records {
				if valuesEqual(getOf(record, "request_id"), id) && valuesEqual(getOf(record, "status"), "pending") {
					return objectOrEmpty(record)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pending %s did not land", id)
	return nil
}

func TestCodexNativeInteractionReplies(t *testing.T) {
	s := newCodexScenario(t)
	s.peer.Reverse = []any{
		map[string]any{"method": "item/commandExecution/requestApproval", "params": map[string]any{"itemId": "command", "startedAtMs": 1, "command": "echo value", "cwd": s.workspace, "reason": "native reason", "proposedExecpolicyAmendment": []any{"echo"}}},
		map[string]any{"method": "item/fileChange/requestApproval", "params": map[string]any{"itemId": "file", "startedAtMs": 2, "reason": "edit"}},
		map[string]any{"method": "item/tool/requestUserInput", "params": map[string]any{"itemId": "input", "isBlocking": true, "questions": []any{map[string]any{"id": "first", "header": "Choice", "question": "Which?", "options": []any{map[string]any{"label": "First", "description": "native option"}}}, map[string]any{"id": "second", "header": "Text", "question": "Explain", "options": nil}}}},
		map[string]any{"method": "item/permissions/requestApproval", "params": map[string]any{"itemId": "permissions", "startedAtMs": 3, "cwd": s.workspace, "permissions": map[string]any{"network": map[string]any{"enabled": true}}}},
		map[string]any{"method": "mcpServer/elicitation/request", "params": map[string]any{"serverName": "peer", "mode": "form", "message": "native form", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}}},
	}
	answers := map[string]any{"first": map[string]any{"answers": []any{"First"}}, "second": map[string]any{"answers": []any{"original text"}}}
	amendment := map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": []any{"echo"}}}
	s.peer.Replies = []any{map[string]any{"decision": amendment}, map[string]any{"decision": "decline"}, map[string]any{"answers": answers}, map[string]any{"permissions": map[string]any{"network": map[string]any{"enabled": true}}, "scope": "turn"}, map[string]any{"action": "accept", "content": map[string]any{"value": "form answer"}}}
	s.save(t)
	finished := make(chan error, 1)
	go func() { _, err := codexExecute(s.request, s.directory, false); finished <- err }()
	replies := [][]string{{"--option", textOf(amendment)}, {"--option", "decline"}, {"--answers", textOf(answers)}, {"--option", "accept"}, {"--option", "accept", "--answers", `{"value":"form answer"}`}}
	for i, args := range replies {
		id := fmt.Sprintf("question-%d", i)
		pending := waitCodexPending(t, s.directory, id)

		if valuesEqual(getOf(pending, "method"), "item/tool/requestUserInput") && !valuesEqual(pending["questions"], getOf(s.peer.Reverse[i], "params").(map[string]any)["questions"]) {
			t.Fatal("original questions were changed")
		}
		var errout bytes.Buffer
		if code := NativeRespond(append([]string{"--job", s.directory, "--request", id}, args...), &errout); code != 0 {
			t.Fatalf("respond %d: %s", code, errout.String())
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	records, err := readNativePendings(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if getOf(record, "status") != "observed" {
			t.Fatalf("not observed: %v", record)
		}
	}
}

func TestCodexNativeSupervisorAndInterrupt(t *testing.T) {
	bin := supervisorBinary(t)
	for _, mode := range []string{"complete", "interrupt", "supervisor_cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := newCodexScenario(t)
			if mode != "complete" {
				s.peer.Mode = "wait"
			}
			s.save(t)
			args := []string{"harness", "native", s.directory}
			if mode != "interrupt" {
				writeRequest(t, s.directory, map[string]any{"kind": "native", "argv": []string{bin, "harness", "native", s.directory}, "cwd": s.workspace, "stdin": ""})
				args = []string{"harness", "supervise", s.directory}
			}
			command := exec.Command(bin, args...)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var out, errout bytes.Buffer
			command.Stdout = &out
			command.Stderr = &errout
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if command.ProcessState == nil {
					syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
					command.Wait()
				}
			})
			if mode != "complete" {
				deadline := time.Now().Add(10 * time.Second)
				ready := false
				for time.Now().Before(deadline) {
					source, err := readJSONFile(filepath.Join(s.directory, "native-source.json"))
					if err == nil && getOf(source, "turnId") != nil {
						if _, err = os.Stat(textOf(getOf(getOf(source, "thread"), "path"))); err == nil {
							ready = true
							break
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !ready {
					t.Fatal("original native turn/history was not persisted")
				}
				if mode == "interrupt" {
					if err := command.Process.Signal(syscall.SIGTERM); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
						t.Fatal(err)
					}
				}
			}
			waitErr := command.Wait()
			if mode == "interrupt" {
				if waitErr == nil {
					t.Fatal("interrupted turn succeeded")
				}
				detail, err := readJSONFile(filepath.Join(s.directory, "native-error.json"))
				if err != nil || !strings.Contains(textOf(getOf(detail, "error")), "interrupted") {
					t.Fatalf("interrupt: %v %v %s", detail, err, errout.String())
				}
				log, _ := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
				if !bytes.Contains(log, []byte(`"method":"turn/interrupt"`)) {
					t.Fatal("original native turn was not interrupted")
				}
			} else {
				if waitErr != nil {
					t.Fatalf("supervisor %v: %s", waitErr, errout.String())
				}
				receipt := readReceipt(t, s.directory)
				if mode == "complete" {
					if !valuesEqual(receipt["exit_code"], 0) {
						t.Fatalf("receipt: %v", receipt)
					}
					source, err := settleNativeReceipt(s.directory, receipt)
					if err != nil || getOf(source, "status") != "PASS" {
						t.Fatalf("receipt source %v %v", source, err)
					}
				} else {
					if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
						t.Fatalf("cancel receipt: %v", receipt)
					}
					if _, err := settleNativeReceipt(s.directory, receipt); err == nil {
						t.Fatal("cancelled receipt accepted")
					}
				}
			}
			if mode != "complete" {
				source, err := readJSONFile(filepath.Join(s.directory, "native-source.json"))
				if err != nil {
					t.Fatal(err)
				}
				s.request["identity"] = getOf(source, "threadId")
				s.request["prompt"] = "after interrupt"
				s.directory = filepath.Join(filepath.Dir(s.directory), "resumed")
				os.MkdirAll(s.directory, 0o755)
				s.peer.Mode = ""
				if _, err := s.execute(t); err != nil {
					t.Fatal(err)
				}
				answer, _ := readJSONFile(filepath.Join(s.directory, "answer.json"))
				if getOf(answer, "summary") != "original prompt / after interrupt" {
					t.Fatalf("cancelled history missing: %v", answer)
				}
			}
		})
	}
}

func TestCodexNativeCaptureAndEvidence(t *testing.T) {
	s := newCodexScenario(t)
	t.Setenv("PATH", filepath.Dir(textOf(getOf(getOf(getOf(s.request, "binding"), "launch"), "executable"))))
	t.Setenv("HOME", s.home)
	t.Setenv("CODEX_HOME", s.config)
	binding, err := codexCaptureBinding()
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(binding, s.request["binding"]) {
		t.Fatalf("candidate capture differs: %v", binding)
	}
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	if err = codexVerifySource(s.directory, source); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.directory, "answer.raw.txt"), []byte("changed"), 0o644)
	if err = codexVerifySource(s.directory, source); err == nil || !strings.Contains(err.Error(), "EVIDENCE_CHANGED") {
		t.Fatalf("proof did not bind original bytes: %v", err)
	}
}
