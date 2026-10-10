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
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type codexPeerInput struct {
	Workspace     string         `json:"workspace"`
	Home          string         `json:"home"`
	Config        string         `json:"config"`
	ThreadParams  map[string]any `json:"thread_params"`
	ToolArgument  string         `json:"tool_argument"`
	TurnParams    map[string]any `json:"turn_params"`
	Configuration map[string]any `json:"configuration"`
	Mode          string         `json:"mode"`
	StrictSchema  bool           `json:"strict_schema,omitempty"`
	Reverse       []any          `json:"reverse"`
	Answer        map[string]any `json:"answer,omitempty"`
	Replies       []any          `json:"replies"`
	WorkerAction  string         `json:"worker_action,omitempty"`
	WorkerAttempt string         `json:"worker_attempt,omitempty"`
}

func codexPeerID(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x-%x-7%03x-8%03x-%x", sum[0:4], sum[4:6], (int(sum[6])<<8|int(sum[7]))&0xfff, (int(sum[8])<<8|int(sum[9]))&0xfff, sum[10:16])
}

func TestCodexRPCPeer(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return
	}
	args = args[1:]
	if len(args) != 4 || !reflect.DeepEqual(args[:3], []string{"app-server", "--stdio", "-c"}) {
		panic("native argv differs")
	}
	script := os.Getenv("CODEX_PEER_SCRIPT")
	if script == "" {
		script = filepath.Join(os.Getenv("CODEX_HOME"), "peer.json")
	}
	raw, err := os.ReadFile(script)
	if err != nil {
		panic(err)
	}
	var peer codexPeerInput
	if err = json.Unmarshal(raw, &peer); err != nil {
		panic(err)
	}
	if args[3] != peer.ToolArgument {
		panic(fmt.Sprintf("native hook argument differs: %s != %s", args[3], peer.ToolArgument))
	}
	cwd, _ := os.Getwd()
	workflow := peer.Mode == "workflow-progress" || peer.Mode == "workflow-decision" || peer.Mode == "workflow-invalid-review" || peer.Mode == "workflow-held"
	if (!workflow && resolveNativePath(cwd) != resolveNativePath(peer.Workspace)) || os.Getenv("HOME") != peer.Home {
		panic("native environment/workspace differs")
	}
	if os.Getenv("CODEX_HOME") != peer.Config {
		panic("native CODEX_HOME differs")
	}
	log, err := os.OpenFile(filepath.Join(os.Getenv("CODEX_HOME"), "rpc.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	defer log.Close()
	emit := func(frame any) { line, _ := json.Marshal(frame); os.Stdout.Write(append(line, '\n')) }
	reply := func(frame any, result any) { emit(map[string]any{"id": getOf(frame, "id"), "result": result}) }
	reader := bufio.NewReader(os.Stdin)
	read := func() any {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			panic(err)
		}
		log.Write(line)
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
		line, err := reader.ReadBytes('\n')
		if len(line) != 0 {
			panic("probe started native work")
		}
		if err != io.EOF {
			panic(err)
		}
		os.Exit(0)
	}
	frame = read()
	method := getOf(frame, "method")
	if workflow {
		params := objectOrEmpty(getOf(frame, "params"))
		check(params["cwd"], cwd)
		peer.ThreadParams = params
	} else {
		check(getOf(frame, "params"), peer.ThreadParams)
	}
	thread := codexPeerID(peer.Workspace + textOf(peer.TurnParams))
	if workflow {
		thread = codexPeerID(cwd + fmt.Sprint(os.Getpid()) + textOf(peer.ThreadParams))
	}
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
	if workflow {
		nativeThread["path"] = filepath.Join(peer.Config, thread+".jsonl")
	}
	configuration := map[string]any{}
	for key, value := range peer.Configuration {
		configuration[key] = value
	}
	if valuesEqual(peer.ThreadParams["sandbox"], "read-only") && peer.Mode != "writable_review" {
		configuration["sandbox"] = map[string]any{"type": "readOnly"}
	}
	configuration["model"] = getOrOf(peer.ThreadParams, "model", peer.Configuration["model"])
	if peer.Mode == "selection" {
		configuration["model"] = "wrong-model"
	}
	configuration["thread"] = nativeThread
	configuration["modelProvider"] = "peer"
	configuration["cwd"] = cwd
	reply(frame, configuration)
	if peer.Mode == "resume_identity" || peer.Mode == "selection" || peer.Mode == "writable_review" {
		if _, err := io.Copy(io.Discard, reader); err != nil {
			panic(err)
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
	if workflow {
		expected = objectOrEmpty(getOf(frame, "params"))
		check(expected["threadId"], thread)
	} else {
		check(getOf(frame, "params"), expected)
	}
	if peer.StrictSchema {
		if err := codexPeerStrictSchema(getOf(getOf(frame, "params"), "outputSchema")); err != nil {
			emit(map[string]any{"id": getOf(frame, "id"), "error": map[string]any{"code": 400, "message": "invalid_json_schema: " + err.Error()}})
			os.Exit(0)
		}
	}
	turn := codexPeerID(thread + textOf(expected) + fmt.Sprint(len(history)))
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
	if peer.Mode == "workflow-held" {
		if err := os.WriteFile(filepath.Join(peer.Config, "held-turn"), []byte(thread+"\n"+turn), 0600); err != nil {
			panic(err)
		}
		for {
			if _, err := os.Stat(filepath.Join(peer.Config, "release-turn")); err == nil {
				break
			} else if !os.IsNotExist(err) {
				panic(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
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
	answer := map[string]any{"status": "PASS", "summary": strings.Join(history, " / "), "findings": []any{}, "check_reviews": map[string]any{}}
	if peer.Answer != nil {
		answer = peer.Answer
	}
	if workflow {
		start := strings.Index(prompt, "[Action input]\n")
		end := strings.Index(prompt, "\n\n[Dispatch]")
		if start < 0 || end <= start {
			panic("native prompt has no original worker input")
		}
		var input protocol.WorkerInput
		if err := json.Unmarshal([]byte(prompt[start+len("[Action input]\n"):end]), &input); err != nil {
			panic(err)
		}
		if peer.Mode != "workflow-invalid-review" && (input.Validation == nil || input.Validation.Workspace == nil || resolveNativePath(input.Validation.Workspace.Path) != resolveNativePath(cwd)) {
			panic("native worker workspace differs from original input")
		}
		if input.Pending.ActionID == "" || input.Pending.AttemptID == "" || input.Provider != "codex" {
			panic("native worker has no original dispatch identity")
		}
		domain := map[string]any{"interruptionReason": "The gate peer needs an explicit user decision"}
		if peer.Mode == "workflow-invalid-review" {
			if input.Review == nil || input.Handler != "engine.review.worker" {
				panic("invalid review fixture received another duty")
			}
			verification := map[string]any{}
			for _, key := range review.RequiredVerificationChecks() {
				verification[key] = "Checked current " + input.Pending.ActionID + "/" + input.Pending.AttemptID
			}
			verification[review.RequiredVerificationChecks()[0]] = " "
			domain = map[string]any{"status": "PASS", "findings": []any{}, "verification": verification}
		}
		if peer.Mode == "workflow-held" && input.Handler == "engine.development.batch" {
			check(input.Pending.ActionID, peer.WorkerAction)
			check(input.Pending.AttemptID, peer.WorkerAttempt)
			if err := os.WriteFile(filepath.Join(cwd, "app.go"), []byte("package main\nfunc main() { println(\"completed\") }\n"), 0600); err != nil {
				panic(err)
			}
			dod := map[string]string{}
			for _, item := range input.Validation.Development.Batch.DoD {
				dod[item.ID] = "Delivered actual app.go"
			}
			domain = map[string]any{"paths": []string{"app.go"}, "summary": "Delivered held original task", "completion": map[string]any{"dod": dod, "handoff": "Ready for approved checks"}}
		}

		if input.Handler == "engine.development.batch" && peer.Mode == "workflow-progress" {
			check(input.Pending.ActionID, peer.WorkerAction)
			check(input.Pending.AttemptID, peer.WorkerAttempt)
			if len(history) > 1 && (input.Validation.Development.Result == nil || input.Validation.Development.Result.Validation == nil || input.Validation.Development.Result.Validation.Development == nil || input.Validation.Development.Result.Validation.Development.RemainingWork == "") {
				panic("native continuation lost original remaining work")
			}
			if len(history) <= 2 {
				domain = map[string]any{"paths": []string{}, "summary": fmt.Sprintf("Completed part %d of the same task", len(history)), "remainingWork": "Finish the same app.go delivery"}
			} else {
				if len(history) != 3 {
					panic("native worker repeated completed scope")
				}
				if err := os.WriteFile(filepath.Join(cwd, "app.go"), []byte("package main\nfunc main() { println(\"completed\") }\n"), 0600); err != nil {
					panic(err)
				}
				dod := map[string]string{}
				for _, item := range input.Validation.Development.Batch.DoD {
					dod[item.ID] = "The fixture delivered the actual app.go file"
				}
				domain = map[string]any{"paths": []string{"app.go"}, "summary": "Delivered app.go", "completion": map[string]any{"dod": dod, "handoff": "Ready for the approved project checks"}}
			}
		}
		schema, err := worker.ResultSchema(input)
		if err != nil {
			panic(err)
		}
		plain, err := json.Marshal(schema)
		if err != nil {
			panic(err)
		}
		originalSchema, err := decodeNativeJSON(plain)
		if err != nil {
			panic(err)
		}
		answer = codexPeerAnswer(originalSchema, domain).(map[string]any)
	}
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
	if workflow {
		path := filepath.Join(peer.Config, thread+".jsonl")
		previousOutput := 0
		if body, err := os.ReadFile(path); err == nil {
			for _, line := range bytes.Split(body, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				record, err := decodeNativeJSON(line)
				if err != nil {
					panic(err)
				}
				previousOutput += intOf(getOf(getOf(getOf(record, "payload"), "usage"), "output_tokens"))
			}
		} else if !os.IsNotExist(err) {
			panic(err)
		}
		cumulativeOutput := previousOutput + len(text)
		usage["total"] = map[string]any{"inputTokens": len(strings.Join(history, "")), "cachedInputTokens": 0, "outputTokens": cumulativeOutput, "reasoningOutputTokens": 0, "totalTokens": len(strings.Join(history, "")) + cumulativeOutput}
		record := map[string]any{"type": "token_usage_record", "payload": map[string]any{"response_id": turn, "model": "peer-default", "usage": map[string]any{"input_tokens": len(prompt), "cached_input_tokens": 0, "output_tokens": len(text)}, "thread_token_usage": map[string]any{"input_tokens": len(strings.Join(history, "")), "cached_input_tokens": 0, "output_tokens": cumulativeOutput}}}
		line, err := json.Marshal(record)
		if err != nil {
			panic(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			panic(err)
		}
		if err := file.Close(); err != nil {
			panic(err)
		}
	}
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
	line, err := reader.ReadBytes('\n')
	if len(line) != 0 {
		panic("unexpected work after unsubscribe")
	}
	if err != io.EOF {
		panic(err)
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
	runtime                            string
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
	s.peer = codexPeerInput{Workspace: workspace, Home: home, Config: config, ThreadParams: map[string]any{}, TurnParams: map[string]any{}, Configuration: map[string]any{"model": "peer-default", "reasoningEffort": "medium", "sandbox": map[string]any{"type": "readOnly"}, "approvalPolicy": "never", "approvalsReviewer": "user"}}
	return s
}

func (s *codexScenario) configurePeerInputs(t *testing.T) {
	t.Helper()
	s.peer.Config = s.config
	s.peer.ThreadParams = map[string]any{"cwd": s.workspace, "config": map[string]any{"bypass_hook_trust": true, "features.shell_tool": false, "features.unified_exec": false, "tools.view_image": false, "web_search": "disabled"}}
	executable := s.runtime
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	command := quote(executable) + " harness codex-tool-permission " + quote(s.directory)
	s.peer.ToolArgument = `hooks.PreToolUse=[{matcher="*",hooks=[{type="command",command=` + strconv.Quote(command) + `}]}]`
	if s.request["read_only"] == true {
		s.peer.ThreadParams["sandbox"] = "read-only"
	}
	if model, _ := s.request["model"].(string); model != "" {
		s.peer.ThreadParams["model"] = model
	}
	if id, _ := s.request["identity"].(string); id != "" {
		s.peer.ThreadParams["threadId"] = id
		s.peer.ThreadParams["excludeTurns"] = true
	} else {
		s.peer.ThreadParams["ephemeral"] = false
		s.peer.ThreadParams["dynamicTools"] = []any{nativeWorkspaceToolSchema(s.request["read_only"] == true)}
	}
	params, err := codexTurnParameters(s.request, "")
	if err != nil {
		t.Fatal(err)
	}
	delete(params, "threadId")
	s.peer.TurnParams = params
}

func (s *codexScenario) save(t *testing.T) {
	t.Helper()
	s.configurePeerInputs(t)
	if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), s.request); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(s.config, "peer.json"), s.peer); err != nil {
		t.Fatal(err)
	}
}

func (s *codexScenario) execute(t *testing.T) (map[string]any, error) {
	t.Helper()
	s.runtime = ""
	s.save(t)
	return codexExecute(s.request, s.directory, s.peer.Mode == "probe")
}

func TestCodexNativeLargeFramePreservesExactParameters(t *testing.T) {
	s := newCodexScenario(t)
	s.request["prompt"] = "Read the complete native execution evidence:\n" + strings.Repeat("command output: \"value\" ✓\n", 4096)
	s.request["schema"] = WorkerSchema()
	s.request["read_only"] = true
	s.peer.StrictSchema = true
	domain := map[string]any{"summary": "compared complete native evidence", "findings": []any{}, "check_reviews": []any{}}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, s.request["schema"]), domain).(map[string]any)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	if err := codexVerifySource(s.directory, source); err != nil {
		t.Fatal(err)
	}
	rpc, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var turnFrame []byte
	for _, line := range bytes.Split(bytes.TrimSpace(rpc), []byte{'\n'}) {
		frame, err := decodeNativeJSON(line)
		if err != nil {
			t.Fatal(err)
		}
		if getOf(frame, "method") != "turn/start" {
			continue
		}
		if turnFrame != nil {
			t.Fatal("native work was duplicated")
		}
		turnFrame = line
		params, err := codexTurnParameters(s.request, textOf(source["identity"]))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(map[string]any{"id": getOf(frame, "id"), "method": "turn/start", "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(line, expected) {
			t.Fatal("large native frame lost exact prompt, schema or original thread binding")
		}
	}
	if len(turnFrame) <= bufio.MaxScanTokenSize {
		t.Fatalf("fixture frame is not large: %d", len(turnFrame))
	}
	answer, err := readJSONFile(filepath.Join(s.directory, "answer.json"))
	if err != nil || !valuesEqual(answer, domain) {
		t.Fatalf("large input changed semantic answer: %v %v", answer, err)
	}
	observed, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
	if err != nil || !valuesEqual(source, observed) {
		t.Fatalf("large input recovery lost original source: %v %v", observed, err)
	}
	repeated, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if err != nil || !bytes.Equal(rpc, repeated) {
		t.Fatal("large frame recovery issued new native work")
	}
	t.Logf("complete native turn/start frame=%d bytes", len(turnFrame))
}

func TestCodexFindingLocationsUseVerifiedOriginalToolEvidence(t *testing.T) {
	s := newCodexScenario(t)
	s.request["schema"] = WorkerSchema()
	s.request["read_only"] = true
	if err := os.WriteFile(filepath.Join(s.workspace, "app.go"), []byte("package app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.peer.Reverse = []any{
		map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "list", "tool": "workspace", "arguments": map[string]any{"action": "list"}}},
		map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "read", "tool": "workspace", "arguments": map[string]any{"action": "read", "listing": 1, "entry": 1}}},
	}
	s.peer.Replies = []any{
		map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"entries":[{"directory":false,"entry":1,"name":"app.go"}],"listing":1}`}}},
		map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"lines":["package app\n"],"read":2}`}}},
	}
	domain := map[string]any{"summary": "Reviewed original code", "findings": []any{map[string]any{"severity": "P3", "message": "The declared package is app", "locations": []any{map[string]any{"read": 2}}}}, "check_reviews": []any{}}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, s.request["schema"]), domain).(map[string]any)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	accept := func(directory string, receipt map[string]any, result map[string]any) (WorkerResult, error) {
		payload, _ := json.Marshal(map[string]any{"role": "implementation-quality-gate", "checks": []any{}, "result": result, "nativeDirectory": directory, "nativeSource": receipt})
		checked, err := dispatch(envelope{Operation: "validate-worker", Payload: payload})
		if err != nil {
			return WorkerResult{}, err
		}
		return checked.(map[string]any)["result"].(WorkerResult), nil
	}
	result, err := accept(s.directory, source, domain)
	if err != nil || len(result.Findings) != 1 || !reflect.DeepEqual(result.Findings[0].Paths, []string{"app.go"}) {
		t.Fatalf("original location: %+v %v", result, err)
	}
	before := nativePeerTurns(t, s)
	recovered, err := NativeJobSource("codex", s.directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.workspace, "earlier.go"), []byte("different path\n"), 0644); err != nil {
		t.Fatal(err)
	}
	replayed, err := accept(s.directory, recovered, domain)
	if err != nil || !reflect.DeepEqual(replayed, result) || nativePeerTurns(t, s) != before {
		t.Fatalf("recovery changed original finding or started a model turn: %+v %v", replayed, err)
	}
	if _, err := accept(t.TempDir(), source, domain); err == nil {
		t.Fatal("arbitrary directory supplied location facts")
	}
	changed := map[string]any{"summary": "different model answer", "findings": domain["findings"], "check_reviews": []any{}}
	if _, err := accept(s.directory, source, changed); err == nil {
		t.Fatal("semantic answer did not belong to verified native source")
	}
	domain["findings"].([]any)[0].(map[string]any)["paths"] = []any{"app.go"}
	if _, err := accept(s.directory, source, domain); err == nil {
		t.Fatal("raw path wire fallback was accepted")
	}
	delete(domain["findings"].([]any)[0].(map[string]any), "paths")
	if err := os.WriteFile(filepath.Join(s.directory, nativeToolContextFile), []byte(`{"facts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := accept(s.directory, source, domain); err == nil {
		t.Fatal("unverified tool context supplied paths")
	}
}

func TestCodexFormalFindingLocationsUseVerifiedSourceAcrossRecovery(t *testing.T) {
	s := newCodexScenario(t)
	input := protocol.WorkerInput{Handler: "engine.gate", Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	s.request["schema"], s.request["read_only"] = schema, true
	if err := os.WriteFile(filepath.Join(s.workspace, "app.go"), []byte("package app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	s.peer.Reverse = []any{
		map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "list", "tool": "workspace", "arguments": map[string]any{"action": "list"}}},
		map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "read", "tool": "workspace", "arguments": map[string]any{"action": "read", "listing": 1, "entry": 1}}},
	}
	s.peer.Replies = []any{
		map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"entries":[{"directory":false,"entry":1,"name":"app.go"}],"listing":1}`}}},
		map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"lines":["package app\n"],"read":2}`}}},
	}
	domain := map[string]any{"findings": []any{map[string]any{"severity": "P3", "message": "Original declaration is app", "locations": []any{map[string]any{"read": 2, "line": 1}}}}}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, schema), domain).(map[string]any)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	decode := func(source map[string]any) protocol.Event {
		t.Helper()
		evidence, err := NativeWorkerResultEvidence("codex", s.directory, source)
		if err != nil {
			t.Fatal(err)
		}
		event, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, evidence.LocationFacts)
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	original := decode(source)
	if !reflect.DeepEqual(original.Result.Validation.Findings[0].Locations, []string{"app.go:1"}) {
		t.Fatal("formal worker did not assemble native source location", original)
	}
	turns := nativePeerTurns(t, s)
	if err := os.WriteFile(filepath.Join(s.workspace, "earlier.go"), []byte("package other\n"), 0644); err != nil {
		t.Fatal(err)
	}
	recovered, err := NativeJobSource("codex", s.directory)
	if err != nil {
		t.Fatal(err)
	}
	if replay := decode(recovered); !reflect.DeepEqual(original, replay) || nativePeerTurns(t, s) != turns {
		t.Fatal("formal recovery changed original paths or invoked another turn", replay)
	}
	if _, err := NativeWorkerResultEvidence("codex", t.TempDir(), source); err == nil {
		t.Fatal("location evidence accepted another source directory")
	}
	if err := os.WriteFile(filepath.Join(s.directory, nativeToolContextFile), []byte(`{"facts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NativeWorkerResultEvidence("codex", s.directory, source); err == nil {
		t.Fatal("formal worker accepted tool facts outside its original source")
	}
}

func TestCodexComparisonBlobLocationsRetainOriginalProjectPaths(t *testing.T) {
	s := newCodexScenario(t)
	materials := filepath.Join(s.workspace, ".formal-gates-input", "current")
	for path, body := range map[string]string{"base/removed.go": "original base\n", "candidate/renamed.go": "original candidate\n"} {
		file := filepath.Join(materials, "comparison", filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s.request["materials"], s.request["schema"], s.request["read_only"] = materials, WorkerSchema(), true
	step := func(arguments map[string]any, response map[string]any) {
		s.peer.Reverse = append(s.peer.Reverse, map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": fmt.Sprint(len(s.peer.Reverse) + 1), "tool": "workspace", "arguments": arguments}})
		body, _ := json.Marshal(response)
		s.peer.Replies = append(s.peer.Replies, map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": string(body)}}})
	}
	listing := func(number int, names ...string) map[string]any {
		entries := []any{}
		for i, name := range names {
			entries = append(entries, map[string]any{"entry": i + 1, "name": name, "directory": !strings.HasSuffix(name, ".go")})
		}
		return map[string]any{"listing": number, "entries": entries}
	}
	step(map[string]any{"action": "list"}, listing(1, ".formal-gates-input"))
	step(map[string]any{"action": "list", "listing": 1, "entry": 1}, listing(2, "current"))
	step(map[string]any{"action": "list", "listing": 2, "entry": 1}, listing(3, "comparison"))
	step(map[string]any{"action": "list", "listing": 3, "entry": 1}, listing(4, "base", "candidate"))
	step(map[string]any{"action": "list", "listing": 4, "entry": 1}, listing(5, "removed.go"))
	step(map[string]any{"action": "read", "listing": 5, "entry": 1}, map[string]any{"read": 6, "lines": []any{"original base\n"}})
	step(map[string]any{"action": "list", "listing": 4, "entry": 2}, listing(7, "renamed.go"))
	step(map[string]any{"action": "read", "listing": 7, "entry": 1}, map[string]any{"read": 8, "lines": []any{"original candidate\n"}})
	domain := map[string]any{"summary": "Reviewed both original versions", "findings": []any{map[string]any{"severity": "P3", "message": "Original paths remain attributable", "locations": []any{map[string]any{"read": 6}, map[string]any{"read": 8}}}}, "check_reviews": []any{}}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, s.request["schema"]), domain).(map[string]any)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.directory, nativeToolContextFile))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"role": "implementation-quality-gate", "checks": []any{}, "result": domain, "nativeDirectory": s.directory, "nativeSource": source})
	checked, err := dispatch(envelope{Operation: "validate-worker", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	result := checked.(map[string]any)["result"].(WorkerResult)
	if !reflect.DeepEqual(result.Findings[0].Paths, []string{"removed.go", "renamed.go"}) {
		t.Fatal("locations exposed temporary materials", result)
	}
	after, err := os.ReadFile(filepath.Join(s.directory, nativeToolContextFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("location assembly rewrote its original source", err)
	}
}

func TestCodexManagedToolAdmissionAndSignedResume(t *testing.T) {
	f, ref, s, binary := engineNativeFixture(t, true)
	entries, err := os.ReadDir(s.workspace)
	if err != nil {
		t.Fatal(err)
	}
	choices := []any{}
	appEntry := 0
	for i, entry := range entries {
		choices = append(choices, map[string]any{"entry": i + 1, "name": entry.Name(), "directory": entry.IsDir()})
		if entry.Name() == "app.go" {
			appEntry = i + 1
		}
	}
	listing, _ := json.Marshal(map[string]any{"listing": 1, "entries": choices})
	s.peer.Reverse = []any{map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "original-list", "namespace": nil, "tool": "workspace", "arguments": map[string]any{"action": "list"}}}}
	s.peer.Replies = []any{map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": string(listing)}}, "success": true}}
	s.save(t)
	call := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(binary, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("original installed CLI %v: %v\n%s", args, err, out)
		}
		t.Logf("original CLI result: %s", out)
		return out
	}
	call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
	originalDirectory := s.directory
	originalSource, err := NativeJobSource("codex", originalDirectory)
	if err != nil || nativePeerTurns(t, s) != 1 {
		for _, name := range []string{"native-error.json", "exit.json", "native.stderr"} {
			if body, readErr := os.ReadFile(filepath.Join(originalDirectory, name)); readErr == nil {
				t.Logf("original %s: %s", name, body)
			}
		}
		t.Fatalf("original managed call: %v %v", originalSource, err)
	}
	verifySource := func(directory string, source map[string]any, resume bool) {
		t.Helper()
		if err := codexVerifySource(directory, source); err != nil {
			t.Fatal(err)
		}
		value, err := readJSONFile(filepath.Join(directory, "native-source.json"))
		if err != nil || getOf(getOf(value, "thread_request"), "config").(map[string]any)["bypass_hook_trust"] != true {
			t.Fatalf("native request did not preserve original hook configuration: %v %v", value, err)
		}
		registered := getOf(getOf(value, "thread_request"), "dynamicTools")
		if resume && registered != nil || !resume && len(listOf(registered)) != 1 {
			t.Fatalf("original registration/resume contract changed: %v", value)
		}
	}
	verifySource(originalDirectory, originalSource, false)
	rejectLate := func(directory string) {
		t.Helper()
		before, err := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
		if err != nil {
			t.Fatal(err)
		}
		input, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "workspace", "cwd": s.workspace})
		cmd := exec.Command(binary, "harness", "codex-tool-permission", directory)
		cmd.Stdin = bytes.NewReader(input)
		if output, err := cmd.CombinedOutput(); err == nil || len(output) == 0 {
			t.Fatalf("closed execution admitted an old hook: %v %s", err, output)
		}
		after, err := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected callback altered original native tool evidence", err)
		}
	}
	rejectLate(originalDirectory)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	confirm := func(input facade.ControlInput) facade.Run {
		t.Helper()
		candidate, err := f.PrepareControl(input)
		if err != nil {
			t.Fatal(err)
		}
		_, updated, err := f.SubmitControl(candidate)
		if err != nil {
			t.Fatal(err)
		}
		return updated
	}
	position := func(run facade.Run, control protocol.ControlKind) int {
		t.Helper()
		for i, ask := range run.PendingAsks {
			if ask.Control == control {
				return i + 1
			}
		}
		t.Fatalf("missing original %s Ask", control)
		return 0
	}
	run = confirm(facade.ControlInput{Ask: position(run, protocol.ControlRecovery), Choice: "resume"})
	resume := ""
	for _, intent := range run.AdapterIntents {
		if intent.Resume != nil {
			resume = intent.ActionID
		}
	}
	if resume == "" {
		t.Fatal("original resume was not signed")
	}
	request, _, directory, err := f.WorkerExecutionRequest(ref, resume)
	if err != nil {
		t.Fatal(err)
	}
	request["binding"] = s.request["binding"]
	s.request, s.directory = request, directory
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(s.workspace, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(body), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	read, _ := json.Marshal(map[string]any{"read": 2, "lines": lines})
	s.peer.Reverse = []any{map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "resumed-read", "namespace": nil, "tool": "workspace", "arguments": map[string]any{"action": "read", "listing": 1, "entry": appEntry}}}}
	s.peer.Replies = []any{map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": string(read)}}, "success": true}}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, s.request["schema"]), map[string]any{"interruptionReason": "The original resumed read finished; this peer now needs an operator decision"}).(map[string]any)
	s.save(t)
	call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
	continued, err := NativeJobSource("codex", directory)
	if err != nil || continued["identity"] != originalSource["identity"] || valuesEqual(continued["native_turn"], originalSource["native_turn"]) || nativePeerTurns(t, s) != 2 {
		t.Fatalf("original resumed tool context: %v %v", continued, err)
	}
	verifySource(directory, continued, true)
	verifySource(originalDirectory, originalSource, false)
	context, err := readJSONFile(filepath.Join(directory, nativeToolContextFile))
	if err != nil || getOf(listOf(getOf(context, "facts"))[0], "job") != originalDirectory {
		t.Fatalf("resume rebound original published facts: %v %v", context, err)
	}
	run = confirm(facade.ControlInput{Request: protocol.ControlAbort})
	run = confirm(facade.ControlInput{Ask: position(run, protocol.ControlAbort), Choice: "abort"})
	call("workflow", "drive", "--root", s.workspace, "--run-id", "native-execution")
	run, err = f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ABORTED" {
		t.Fatalf("original abort was not consumed: %s", run.Status)
	}
	rejectLate(directory)
	verifySource(directory, continued, true)
	if nativePeerTurns(t, s) != 2 {
		t.Fatal("closed tool request started another native turn")
	}
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

func TestCodexNativePermissionAndDefaultSelection(t *testing.T) {
	for _, policy := range []struct {
		sandbox, approval, reviewer string
	}{{"readOnly", "never", "user"}, {"workspaceWrite", "on-failure", "guardian"}} {
		t.Run(policy.sandbox, func(t *testing.T) {
			s := newCodexScenario(t)
			delete(s.request, "model")
			delete(s.request, "reasoning_effort")
			s.peer.Configuration["sandbox"] = map[string]any{"type": policy.sandbox}
			s.peer.Configuration["approvalPolicy"] = policy.approval
			s.peer.Configuration["approvalsReviewer"] = policy.reviewer
			var identity any
			for _, operation := range []string{"new", "continued", "review"} {
				t.Run(operation, func(t *testing.T) {
					if operation != "new" {
						s.directory = filepath.Join(filepath.Dir(s.directory), operation)
						if err := os.MkdirAll(s.directory, 0o755); err != nil {
							t.Fatal(err)
						}
						s.request["identity"] = identity
						s.request["prompt"] = operation + " prompt"
					}
					s.request["read_only"] = operation == "review"
					source, err := s.execute(t)
					if err != nil {
						t.Fatal(err)
					}
					if identity != nil && !valuesEqual(source["identity"], identity) {
						t.Fatal("continuation changed original identity")
					}
					identity = source["identity"]
					configuration := getOf(getOf(source, "selection_evidence"), "configuration")
					wantSandbox := policy.sandbox
					if operation == "review" {
						wantSandbox = "readOnly"
					}
					if !valuesEqual(getOf(getOf(configuration, "sandbox"), "type"), wantSandbox) || !valuesEqual(getOf(configuration, "approvalPolicy"), policy.approval) || !valuesEqual(getOf(configuration, "approvalsReviewer"), policy.reviewer) {
						t.Fatalf("native permissions differ: %v", configuration)
					}
					if !valuesEqual(source["selection"], map[string]any{"model": "peer-default", "reasoning_effort": "medium"}) {
						t.Fatalf("native defaults differ: %v", source["selection"])
					}
					log, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range bytes.Split(bytes.TrimSpace(log), []byte{'\n'}) {
						frame, err := decodeNativeJSON(line)
						if err != nil {
							t.Fatal(err)
						}
						params := getOf(frame, "params")
						if getOf(params, "approvalPolicy") != nil || getOf(params, "approvalsReviewer") != nil || getOf(params, "model") != nil || getOf(params, "effort") != nil || valuesEqual(getOf(params, "sandbox"), "workspace-write") {
							t.Fatalf("unrequested override: %v", frame)
						}
					}
					observed, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
					if err != nil || !valuesEqual(source, observed) {
						t.Fatalf("permission/default recovery: %v %v", observed, err)
					}
					after, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
					if err != nil || !bytes.Equal(log, after) {
						t.Fatal("recovery issued native work")
					}
				})
			}
		})
	}
}

func TestCodexNativeReadOnlyConstraintMustApply(t *testing.T) {
	s := newCodexScenario(t)
	s.request["read_only"] = true
	s.peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	s.peer.Mode = "writable_review"
	if source, err := s.execute(t); err == nil || source != nil || !strings.Contains(err.Error(), "CODEX_CONFIG") {
		t.Fatalf("writable review accepted: %v %v", source, err)
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
	scenarios[1].peer.Configuration["sandbox"] = map[string]any{"type": "workspaceWrite"}
	scenarios[1].peer.Configuration["approvalPolicy"] = "on-failure"
	scenarios[1].peer.Configuration["approvalsReviewer"] = "guardian"
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
		configuration := getOf(getOf(source, "selection_evidence"), "configuration")
		if !valuesEqual(getOf(configuration, "approvalPolicy"), s.peer.Configuration["approvalPolicy"]) || !valuesEqual(getOf(configuration, "approvalsReviewer"), s.peer.Configuration["approvalsReviewer"]) || !valuesEqual(getOf(getOf(configuration, "sandbox"), "type"), "readOnly") {
			t.Fatalf("parallel permissions %d: %v", i, configuration)
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
	replies := []nativeSelection{{Choice: 5}, {Choice: 3}, {Responses: []nativeQuestionResponse{{Choices: []int{1}}, {Text: "original text"}}}, {Choice: 1}, {Choice: 1, Values: []any{"form answer"}}}
	for i, args := range replies {
		id := fmt.Sprintf("question-%d", i)
		pending := waitCodexPending(t, s.directory, id)

		if valuesEqual(getOf(pending, "method"), "item/tool/requestUserInput") && !valuesEqual(pending["questions"], getOf(s.peer.Reverse[i], "params").(map[string]any)["questions"]) {
			t.Fatal("original questions were changed")
		}
		var errout bytes.Buffer
		if code := confirmHeldQuestion(t, s.directory, 1, args, &errout); code != 0 {
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
			s.runtime = bin
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

// Native launch checks reject each changed input before successful RPC replies.
// A valid explicit script and rebuilt wrapper proof isolate process validation.
func TestCodexNativePeerRejectsWrongLaunchInputs(t *testing.T) {
	binary := supervisorBinary(t)
	for _, dimension := range []string{"valid", "argv", "config", "home", "cwd"} {
		t.Run(dimension, func(t *testing.T) {
			s := newCodexScenario(t)
			s.runtime = binary
			s.save(t)
			binding := objectOrEmpty(s.request["binding"])
			launch := objectOrEmpty(binding["launch"])
			wrapper := textOf(launch["executable"])
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			prefix := "export CODEX_PEER_SCRIPT=" + quote(filepath.Join(s.config, "peer.json")) + "\n"
			refusal := ""
			switch dimension {
			case "config":
				prefix += "export CODEX_HOME=" + quote(resolveNativePath(t.TempDir())) + "\n"
				refusal = "native CODEX_HOME differs"
			case "home":
				prefix += "export HOME=" + quote(resolveNativePath(t.TempDir())) + "\n"
				refusal = "native environment/workspace differs"
			case "cwd":
				prefix += "cd " + quote(resolveNativePath(t.TempDir())) + "\n"
				refusal = "native environment/workspace differs"
			case "argv":
				refusal = "native argv differs"
			}
			body := strings.Replace(readTestFile(t, wrapper), "#!/bin/sh\n", "#!/bin/sh\n"+prefix, 1)
			if dimension == "argv" {
				body = strings.Replace(body, "-- \"$@\"", "-- --unexpected \"$@\"", 1)
			}
			if err := os.WriteFile(wrapper, []byte(body), 0755); err != nil {
				t.Fatal(err)
			}
			bound, err := buildCaptureBinding("codex", launch)
			if err != nil {
				t.Fatal(err)
			}
			bound["environment"] = binding["environment"]
			if err := codexValidateBinding(bound); err != nil {
				t.Fatalf("launch control has invalid binding: %v", err)
			}
			s.request["binding"] = bound
			if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), s.request); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, "harness", "native", s.directory)
			command.Dir = s.workspace
			output, err := command.CombinedOutput()
			t.Logf("public native input=%s exit=%v output=%s", dimension, err, output)
			if dimension == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				source, err := readJSONFile(filepath.Join(s.directory, "source.json"))
				if err != nil || getOf(source, "provider") != "codex" {
					t.Fatalf("valid native source: %v %v", source, err)
				}
				if err := codexVerifySource(s.directory, source); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("wrong launch obtained successful source")
				}
				stderr := readTestFile(t, filepath.Join(s.directory, "host.stderr"))
				t.Logf("peer rejection input=%s: %s", dimension, stderr)
				if !strings.Contains(stderr, refusal) {
					t.Fatalf("expected peer %q, got %s", refusal, stderr)
				}
				for _, name := range []string{"source.json", "identity.json"} {
					if _, err := os.Stat(filepath.Join(s.directory, name)); !os.IsNotExist(err) {
						t.Fatalf("wrong launch wrote %s", name)
					}
				}
				if _, err := os.Stat(filepath.Join(s.config, "rpc.jsonl")); !os.IsNotExist(err) {
					t.Fatal("wrong launch processed RPC")
				}
			}
		})
	}
}
