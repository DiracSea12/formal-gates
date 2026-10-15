//go:build !windows

package harness

// These tests use a print-mode CLI peer with native Claude message shapes.
// The peer checks complete argv, environment and session inputs, and exits as
// an actual process. No product model or installed host is invoked.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type claudePeer struct {
	Args        []string          `json:"args"`
	Environment map[string]string `json:"environment"`
	Workspace   string            `json:"workspace"`
	Calls       string            `json:"calls"`
	Events      []any             `json:"events"`
	Semantics   any               `json:"semantics"`
	Stderr      string            `json:"stderr"`
	Exit        int               `json:"exit"`
	Wait        bool              `json:"wait"`
	History     string            `json:"history"`
	Resume      string            `json:"resume"`
}

func TestClaudeCLI(t *testing.T) {
	path := os.Getenv("CLAUDE_PEER_SCRIPT")
	if path == "" {
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	var peer claudePeer
	if err := json.Unmarshal(body, &peer); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("2.1.268 (Claude Code)")
		os.Exit(0)
	}
	if !reflect.DeepEqual(args, peer.Args) {
		fmt.Fprintf(os.Stderr, "native argv differs: %q != %q\n", args, peer.Args)
		os.Exit(91)
	}
	for key, expected := range peer.Environment {
		if os.Getenv(key) != expected {
			fmt.Fprintf(os.Stderr, "native env %s differs\n", key)
			os.Exit(91)
		}
	}
	cwd, _ := os.Getwd()
	if cwd != peer.Workspace {
		fmt.Fprintf(os.Stderr, "native cwd differs: %s\n", cwd)
		os.Exit(91)
	}
	if peer.Resume != "" {
		prior, err := os.ReadFile(peer.History)
		if err != nil || string(prior) != peer.Resume {
			fmt.Fprintln(os.Stderr, "resume did not retain native context")
			os.Exit(91)
		}
	}
	calls, _ := os.OpenFile(peer.Calls, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	line, _ := json.Marshal(map[string]any{"argv": args, "cwd": cwd, "resume": peer.Resume})
	calls.Write(append(line, '\n'))
	calls.Close()
	if peer.History != "" && peer.Resume == "" {
		os.WriteFile(peer.History, []byte(textOf(getOf(peer.Events[0], "session_id"))), 0o644)
	}
	fmt.Fprint(os.Stderr, peer.Stderr)
	for _, event := range peer.Events {
		if getOf(event, "type") == "result" && getOf(event, "is_error") == false && peer.Semantics != nil {
			peerSubmitSemantics(textOf(listOf(getOf(getOf(getOf(decodeTestJSON(t, argsFlag(peer.Args, "--mcp-config")), "mcpServers"), "formal-gates"), "args"))[2]), peer.Semantics)
		}
	}
	for _, event := range peer.Events {
		body, _ := json.Marshal(event)
		os.Stdout.Write(append(body, '\n'))
	}
	if peer.Wait {
		// The fixture remains active after init. Its process must be stopped
		// through the existing supervisor cancellation path.
		time.Sleep(60 * time.Second)
	}
	os.Exit(peer.Exit)
}

type claudeScenario struct {
	request   map[string]any
	peer      claudePeer
	directory string
	workspace string
	script    string
}

func newClaudeScenario(t *testing.T, identity string, readOnly bool) *claudeScenario {
	t.Helper()
	root := resolveNativePath(t.TempDir())
	workspace := filepath.Join(root, "workspace")
	directory := filepath.Join(root, "job")
	home := filepath.Join(root, "home")
	config := filepath.Join(home, ".claude")
	for _, path := range []string{workspace, directory, config} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"hooks":{"PreToolUse":[]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "claude")
	wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(binary, "'", "'\\''") + "' -test.run='^TestClaudeCLI$' -- \"$@\"\n"
	if err := os.WriteFile(executable, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	binding, err := buildCaptureBinding("claude", map[string]any{"executable": executable})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "peer.json")
	binding["environment"] = map[string]any{"HOME": home, "CLAUDE_CONFIG_DIR": config, "CLAUDE_PEER_SCRIPT": script}
	prompt := "Review the bound task for " + identity
	schema, err := worker.ResultSchema(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", Validation: &protocol.ValidationInput{}})
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
	request := map[string]any{"provider": "claude", "binding": binding, "workspace": workspace, "read_only": readOnly,
		"identity": "", "model": "claude-fixture-model", "reasoning_effort": "high", "prompt": prompt, "schema": plainSchema}
	init := map[string]any{"type": "system", "subtype": "init", "session_id": identity, "cwd": workspace,
		"model": "claude-fixture-model", "effort": "high", "uuid": "init-" + identity}
	answer := map[string]any{"findings": []any{map[string]any{"severity": "P3", "message": "native answer " + identity}}}
	terminal := map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": identity,
		"uuid": "result-" + identity, "num_turns": 3, "result": "Completed the semantic tool submission.",
		"usage": map[string]any{"input_tokens": 12, "output_tokens": 3}, "modelUsage": map[string]any{"claude-fixture-model": map[string]any{"inputTokens": 12}}}
	peer := claudePeer{Workspace: workspace, Environment: map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": config},
		Calls: filepath.Join(root, "calls.jsonl"), Events: []any{init, terminal}, Semantics: answer}
	scenario := &claudeScenario{request: request, peer: peer, directory: directory, workspace: workspace, script: script}
	scenario.save(t)
	return scenario
}

func (s *claudeScenario) save(t *testing.T, toolExecutable ...string) {
	t.Helper()
	s.peer.Args = []string{"-p", textOf(s.request["prompt"]), "--output-format", "stream-json", "--verbose", "--include-hook-events"}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if len(toolExecutable) != 0 {
		executable = toolExecutable[0]
	}
	mcpConfig := mustCanonical(t, map[string]any{"mcpServers": map[string]any{"formal-gates": map[string]any{"command": executable, "args": []any{"harness", "native-tools", s.directory}}}})
	s.peer.Args = append(s.peer.Args, "--tools", "", "--strict-mcp-config", "--mcp-config", mcpConfig)
	for _, option := range []struct{ key, flag string }{{"identity", "--resume"}, {"model", "--model"}, {"reasoning_effort", "--effort"}} {
		if value, _ := s.request[option.key].(string); value != "" {
			s.peer.Args = append(s.peer.Args, option.flag, value)
		}
	}
	s.peer.Args = append(s.peer.Args, "--settings", filepath.Join(s.peer.Environment["CLAUDE_CONFIG_DIR"], "settings.json"))
	body, err := json.Marshal(s.peer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.script, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(s.directory, "native-request.json"), s.request); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCaptureAndProbe(t *testing.T) {
	s := newClaudeScenario(t, "probe", true)
	executable := textOf(getOf(getOf(s.request["binding"], "launch"), "executable"))
	t.Setenv("PATH", filepath.Dir(executable)+":"+os.Getenv("PATH"))
	t.Setenv("HOME", s.peer.Environment["HOME"])
	t.Setenv("CLAUDE_CONFIG_DIR", s.peer.Environment["CLAUDE_CONFIG_DIR"])
	binding, err := claudeCaptureBinding()
	if err != nil || !valuesEqual(getOf(binding, "files"), getOf(s.request["binding"], "files")) {
		t.Fatalf("capture = %v, %v", binding, err)
	}
	s.request["probe"] = true
	s.save(t)
	source, err := runNativeJob(s.directory)
	if err != nil || !valuesEqual(source["selection"], map[string]any{}) || !valuesEqual(source["model_calls"], 0) || getOf(source["capabilities"], "selection_observed") != false {
		t.Fatalf("CLI-only probe = %v, %v", source, err)
	}
	if _, err := os.Stat(s.peer.Calls); !os.IsNotExist(err) {
		t.Fatal("probe started a native session")
	}
	if err := os.WriteFile(executable, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := claudeValidateBinding(binding); err == nil || !strings.Contains(err.Error(), "HOST_CHANGED") {
		t.Fatalf("changed binding = %v", err)
	}
}

func TestClaudeNativeCompletionAndObservation(t *testing.T) {
	t.Parallel()
	for _, identity := range []string{"session-a", "session-b"} {
		t.Run(identity, func(t *testing.T) {
			s := newClaudeScenario(t, identity, identity == "session-a")
			// A different subagent identity/result cannot become the root answer.
			s.peer.Events = append([]any{map[string]any{"type": "assistant", "session_id": "child-" + identity, "parent_tool_use_id": "tool-1"}}, s.peer.Events...)
			s.peer.Events = append(s.peer.Events[:2], map[string]any{"type": "result", "is_error": false, "session_id": "child-" + identity, "parent_tool_use_id": "tool-1", "result": "wrong child answer"}, s.peer.Events[2])
			s.save(t)
			source, err := runNativeJob(s.directory)
			if err != nil || source["identity"] != identity {
				t.Fatalf("source = %v, %v", source, err)
			}
			if err := claudeVerifySource(s.directory, source); err != nil {
				t.Fatal(err)
			}
			if source["usage_evidence"] == nil || !valuesEqual(getOf(source["native_result"], "uuid"), "result-"+identity) {
				t.Fatalf("native metadata = %v", source)
			}
			nativeSource, _ := readJSONFile(filepath.Join(s.directory, "native-source.json"))
			if !valuesEqual(getOf(nativeSource, "process"), map[string]any{"exit_code": 0, "error": ""}) {
				t.Fatalf("native exit = %v", nativeSource)
			}
			answer, _ := readJSONFile(filepath.Join(s.directory, "answer.json"))
			if getOf(listOf(getOf(answer, "findings"))[0], "message") != "native answer "+identity {
				t.Fatalf("root answer = %v", answer)
			}
			body, _ := json.Marshal(answer)
			if _, err := worker.DecodeResultWithFacts(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", Provider: "claude", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}, "result", body, "sha256:source", nil, nil); err != nil {
				t.Fatal(err)
			}
			before := proofsOf(t, s.directory, nativeSourceFiles)
			observed, err := streamObserve(claudeStream, s.directory)
			if err != nil || !valuesEqual(source, observed) || !valuesEqual(before, proofsOf(t, s.directory, nativeSourceFiles)) {
				t.Fatalf("original observation = %v, %v", observed, err)
			}
			if calls := bytes.Count(readBytes(t, s.peer.Calls), []byte("\n")); calls != 1 {
				t.Fatalf("observer re-executed: %d calls", calls)
			}
		})
	}
}

func TestClaudeNativeRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, want string
		change     func(*claudeScenario)
	}{
		{"native-error", "CLAUDE_RESULT", func(s *claudeScenario) { objectOrEmpty(s.peer.Events[1])["is_error"] = true }},
		{"nonzero-exit", "CLAUDE_PROCESS", func(s *claudeScenario) { s.peer.Exit = 7 }},
		{"no-terminal", "terminal result is missing", func(s *claudeScenario) { s.peer.Events = s.peer.Events[:1] }},
		{"no-native-identity", "identity is missing", func(s *claudeScenario) { s.peer.Events = nil }},
		{"invalid-answer", "NATIVE_RESULT", func(s *claudeScenario) {
			s.peer.Semantics = nil
			objectOrEmpty(s.peer.Events[1])["result"] = "not role JSON"
		}},
		{"wrong-model", "model selection was not applied", func(s *claudeScenario) { objectOrEmpty(s.peer.Events[0])["model"] = "claude-different-model" }},
		{"wrong-effort", "effort selection was not applied", func(s *claudeScenario) { objectOrEmpty(s.peer.Events[0])["effort"] = "low" }},
		{"changed-resume", "resumed native session identity changed", func(s *claudeScenario) { s.request["identity"] = "prior-native" }},
		{"mixed-session", "mixed native session identities", func(s *claudeScenario) { objectOrEmpty(s.peer.Events[1])["session_id"] = "foreign-session" }},
		{"after-terminal", "root message after terminal", func(s *claudeScenario) { s.peer.Events = append(s.peer.Events, s.peer.Events[0]) }},
		{"invalid-protocol", "native message is not an object", func(s *claudeScenario) { s.peer.Events = append(s.peer.Events[:1], []any{1}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newClaudeScenario(t, "refused-session", true)
			tc.change(s)
			s.save(t)
			_, err := runNativeJob(s.directory)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
				t.Fatal("refusal produced a source receipt")
			}
			if tc.name == "changed-resume" {
				if _, err := os.Stat(filepath.Join(s.directory, "identity.json")); !os.IsNotExist(err) {
					t.Fatal("rejected native identity was published for continuation")
				}
				observed, err := readJSONFile(filepath.Join(s.directory, "native-source.json"))
				if err != nil || getOf(observed, "sessionId") != "refused-session" {
					t.Fatalf("actual rejected identity evidence = %v, %v", observed, err)
				}
			}
			writeNativeError(s.directory, err, &bytes.Buffer{})
			if _, err := claudeReconcile(s.directory, map[string]any{"exit_code": 1, "error": ""}); err == nil {
				t.Fatal("unchanged native failure was reconciled")
			}
		})
	}
}

func TestClaudeResumeAndMissingSession(t *testing.T) {
	t.Parallel()
	s := newClaudeScenario(t, "retained-session", false)
	s.peer.History = filepath.Join(filepath.Dir(s.script), "native-history")
	s.save(t)
	first, err := runNativeJob(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	s.directory = filepath.Join(filepath.Dir(s.directory), "continuation")
	os.MkdirAll(s.directory, 0o755)
	s.request["identity"] = first["identity"]
	s.request["prompt"] = "Continue retained context"
	s.peer.Resume = "retained-session"
	objectOrEmpty(s.peer.Events[1])["uuid"] = "continued-native-result"
	objectOrEmpty(s.peer.Events[1])["usage"] = map[string]any{"input_tokens": 21, "output_tokens": 6}
	s.save(t)
	continued, err := runNativeJob(s.directory)
	if err != nil || continued["identity"] != first["identity"] || valuesEqual(continued["usage_evidence"], first["usage_evidence"]) {
		t.Fatalf("continuation = %v, %v", continued, err)
	}
	for _, rejection := range []string{"No conversation found with session ID: requested-id", "No conversation found with session ID: another-id", "Failed to resume session requested-id", "Authentication failed", "Network error"} {
		t.Run(rejection, func(t *testing.T) {
			s := newClaudeScenario(t, "missing", true)
			s.request["identity"] = "requested-id"
			s.peer.Events = nil
			s.peer.Exit = 1
			s.peer.Stderr = rejection + "\n"
			s.save(t)
			_, err := runNativeJob(s.directory)
			_, unavailable := err.(*sessionUnavailableError)
			if err == nil || unavailable != (rejection == "No conversation found with session ID: requested-id") {
				t.Fatalf("native refusal = %T %v", err, err)
			}
			if unavailable {
				writeNativeError(s.directory, err, &bytes.Buffer{})
				detail, _ := readJSONFile(filepath.Join(s.directory, "native-error.json"))
				if getOf(detail, "identity") != "requested-id" || getOf(detail, "native_code") != nil || getOf(detail, "method") != "--resume" {
					t.Fatalf("native refusal values = %v", detail)
				}
			}
		})
	}
}

func TestClaudeOriginalSupervisorReceiptAndCancellation(t *testing.T) {
	t.Parallel()
	bin := supervisorBinary(t)
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			s := newClaudeScenario(t, "supervised", true)
			if cancel {
				s.peer.Events = s.peer.Events[:1]
				s.peer.Wait = true
			}
			s.save(t, bin)
			writeRequest(t, s.directory, map[string]any{"kind": "native", "argv": []string{bin, "harness", "native", s.directory}, "cwd": s.workspace, "stdin": ""})
			command := exec.Command(bin, "harness", "supervise", s.directory)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			if cancel {
				waitForFile(t, filepath.Join(s.directory, "identity.json"))
				identity, _ := readJSONFile(filepath.Join(s.directory, "identity.json"))
				if getOf(identity, "identity") != "supervised" {
					t.Fatalf("live persisted identity = %v", identity)
				}
				if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			receipt := readReceipt(t, s.directory)
			if cancel {
				if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
					t.Fatalf("cancel receipt = %v", receipt)
				}
				if _, err := settleNativeReceipt(s.directory, receipt); err == nil {
					t.Fatal("cancelled native invocation admitted")
				}
				process, _ := readJSONFile(filepath.Join(s.directory, "native-process.json"))
				number, _ := getOf(process, "pid").(json.Number).Int64()
				pid := int(number)
				if syscall.Kill(pid, 0) == nil {
					t.Fatal("native process survived group cancellation")
				}
			} else {
				first, err := settleNativeReceipt(s.directory, receipt)
				if err != nil {
					t.Fatal(err)
				}
				// Recovering a lost caller response reads the original receipt;
				// the same admission path neither launches nor meters a new call.
				second, err := settleNativeReceipt(s.directory, readReceipt(t, s.directory))
				if err != nil || !valuesEqual(first, second) || bytes.Count(readBytes(t, s.peer.Calls), []byte("\n")) != 1 {
					t.Fatalf("receipt replay = %v, %v", second, err)
				}
			}
		})
	}
}

func TestClaudeSemanticSubmissionUsageUnavailableAndRoleValidation(t *testing.T) {
	t.Parallel()
	for _, invalidRole := range []bool{false, true} {
		s := newClaudeScenario(t, "text-session", true)
		terminal := objectOrEmpty(s.peer.Events[1])
		delete(terminal, "usage")
		delete(terminal, "modelUsage")
		s.peer.Semantics = map[string]any{"findings": []any{}}
		if invalidRole {
			s.peer.Semantics = map[string]any{"status": "unexpected", "findings": []any{}}
		}
		s.save(t)
		source, err := runNativeJob(s.directory)
		if err != nil || source["usage_evidence"] != nil {
			t.Fatalf("semantic submission = %v, %v", source, err)
		}
		answer, _ := readJSONFile(filepath.Join(s.directory, "answer.json"))
		body, _ := json.Marshal(answer)
		_, err = worker.DecodeResultWithFacts(protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.gate.v3", Provider: "claude", Pending: protocol.PendingAction{ActionID: "issued", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{}}, "result", body, "sha256:source", nil, nil)
		if (err != nil) != invalidRole {
			t.Fatalf("role acceptance = %v, invalid=%t", err, invalidRole)
		}
	}
}

func TestClaudeReconcileOriginalEvidence(t *testing.T) {
	t.Parallel()
	s := newClaudeScenario(t, "recovered-session", true)
	peerSubmitSemantics(s.directory, s.peer.Semantics)
	nativeSource := map[string]any{"sessionId": "recovered-session", "workspace": s.workspace,
		"selection": map[string]any{"model": "claude-fixture-model", "reasoning_effort": "high"},
		"process":   map[string]any{"exit_code": 0, "error": ""}}
	if err := writeAtomicJSON(filepath.Join(s.directory, "native-source.json"), nativeSource); err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	for _, event := range s.peer.Events {
		stream.WriteString(mustCanonical(t, event) + "\n")
	}
	if err := os.WriteFile(filepath.Join(s.directory, "native-events.jsonl"), stream.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, filepath.Join(s.directory, "native-source.json"))
	toolsBefore := readBytes(t, filepath.Join(s.directory, nativeToolCallsFile))
	for _, process := range []map[string]any{{"exit_code": 1, "error": ""}, {"exit_code": 0, "error": "interrupted"}} {
		original := mustCanonical(t, process)
		if source, err := claudeReconcile(s.directory, process); err == nil || source != nil {
			t.Fatalf("unsuccessful original process admitted: %v, %v", source, err)
		}
		if mustCanonical(t, process) != original {
			t.Fatal("refusal rewrote the supervisor result")
		}
		if _, err := os.Stat(filepath.Join(s.directory, "source.json")); !os.IsNotExist(err) {
			t.Fatal("unsuccessful process produced a source receipt")
		}
	}
	process := map[string]any{"exit_code": 0, "error": ""}
	source, err := claudeReconcile(s.directory, process)
	if err != nil || source["identity"] != "recovered-session" {
		t.Fatalf("reconcile = %v, %v", source, err)
	}
	second, err := claudeReconcile(s.directory, process)
	if err != nil || !valuesEqual(source, second) || !bytes.Equal(before, readBytes(t, filepath.Join(s.directory, "native-source.json"))) || !bytes.Equal(stream.Bytes(), readBytes(t, filepath.Join(s.directory, "native-events.jsonl"))) || !bytes.Equal(toolsBefore, readBytes(t, filepath.Join(s.directory, nativeToolCallsFile))) {
		t.Fatalf("repeat observation = %v, %v", second, err)
	}
	if _, err := os.Stat(s.peer.Calls); !os.IsNotExist(err) {
		t.Fatal("reconcile launched a CLI")
	}
	if !valuesEqual(process, map[string]any{"exit_code": 0, "error": ""}) {
		t.Fatal("reconcile rewrote the supervisor result")
	}
}

func TestClaudeConcurrentSessionsDrainTheirOwnStream(t *testing.T) {
	t.Parallel()
	first := newClaudeScenario(t, "parallel-first", false)
	second := newClaudeScenario(t, "parallel-second", true)
	first.peer.Events = append(first.peer.Events[:1], map[string]any{"type": "assistant", "session_id": "parallel-first", "message": map[string]any{"content": strings.Repeat("native chunk", 12000)}}, first.peer.Events[1])
	first.save(t)
	type answer struct {
		source map[string]any
		err    error
	}
	completed := make(chan answer, 2)
	for _, s := range []*claudeScenario{first, second} {
		go func(s *claudeScenario) { source, err := runNativeJob(s.directory); completed <- answer{source, err} }(s)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		result := <-completed
		if result.err != nil {
			t.Fatal(result.err)
		}
		seen[textOf(result.source["identity"])] = true
	}
	if !seen["parallel-first"] || !seen["parallel-second"] {
		t.Fatalf("native identities crossed: %v", seen)
	}
	if !bytes.Contains(readBytes(t, filepath.Join(first.directory, "native-events.jsonl")), []byte(strings.Repeat("native chunk", 12000))) {
		t.Fatal("stdout was not fully drained")
	}
	for _, s := range []*claudeScenario{first, second} {
		if bytes.Count(readBytes(t, s.peer.Calls), []byte("\n")) != 1 {
			t.Fatal("parallel invocation repeated")
		}
	}
}

func TestNativeCloseReturnsTheOriginalExit(t *testing.T) {
	t.Parallel()
	client, err := newNativeClient(nativeHooks{}, []string{"/bin/sh", "-c", "exit 7"}, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.receive(nil)
	first := client.close()
	if first == nil || first != client.close() || client.cmd.ProcessState.ExitCode() != 7 {
		t.Fatalf("unique Wait result = %v", first)
	}
}

func TestClaudeNativeStreamProvidesMeteredSourceBinding(t *testing.T) {
	t.Parallel()
	s := newClaudeScenario(t, "metered-session", true)
	usage := map[string]any{"type": "assistant", "uuid": "native-usage-id", "session_id": "metered-session", "message": map[string]any{"usage": map[string]any{"input_tokens": 13, "cache_read_input_tokens": 4, "output_tokens": 3}}}
	s.peer.Events = append(s.peer.Events[:1], append([]any{usage}, s.peer.Events[1:]...)...)
	s.save(t)
	source, err := claudeExecute(s.request, s.directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if textOf(source["transcript"]) != filepath.Join(s.directory, "native-events.jsonl") {
		t.Fatalf("original metering path unbound: %#v", source)
	}
	got, err := cost.ParseTranscript("claude", textOf(source["transcript"]))
	if err != nil || got != (cost.Usage{InputCacheMissTokens: 13, InputCacheHitTokens: 4, OutputTokens: 3}) {
		t.Fatalf("original Claude stream usage=%+v err=%v", got, err)
	}
}
