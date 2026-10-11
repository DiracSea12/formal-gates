//go:build !windows

package harness

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"formal-gates/internal/engine/protocol"
)

func canaryStopPrivateBackend(t *testing.T, pidFile string) {
	t.Helper()
	body, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Error(err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Error(err)
		return
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		t.Error(err)
	}
}

func canarySubprocessOutput(cmd *exec.Cmd) (output []byte, err error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	defer func() {
		if err != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
	return cmd.CombinedOutput()
}

type canaryLocalExecutor struct {
	t      *testing.T
	canary *engineCanary
}

func canaryLocalPlan(split bool) *protocol.GranularityReview {
	batch := func(id, artifact, deliverable string) protocol.DevelopmentBatch {
		return protocol.DevelopmentBatch{ID: id, Artifacts: []string{artifact}, Subtasks: []protocol.DevelopmentSubtask{{ID: id + "-implementation", Description: deliverable}}, Deliverable: deliverable, PublicEntry: "frob word", Interfaces: "function called by seeded main", StateBoundary: "no persistent state", Risk: "wrong text result", DoD: []protocol.DevelopmentCriterion{{ID: "output", Description: deliverable}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "all-tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 -count=1 ./..."}}, Rollback: "restore artifact from base", Recovery: "repeat declared implementation", Handoff: "function ready for command integration", ContextCost: "one function contract"}
	}
	plan := &protocol.GranularityReview{Reason: "independent declared artifacts", VerificationTier: "FINAL"}
	if split {
		plan.Batches = []protocol.DevelopmentBatch{batch("uppercase", "uppercase.go", "UpperWord returns Unicode uppercase including empty input"), batch("word-size", "size.go", "WordSize returns UTF-8 byte length including empty input")}
		for i := range plan.Batches {
			plan.Batches[i].Owner = plan.Batches[i].ID
		}
		integrated := batch("integration", "main.go", "The integrated command satisfies the confirmed acceptance conditions")
		integrated.Owner = "master-merge"
		plan.Batches = append(plan.Batches, integrated)
	} else {
		plan.Batches = []protocol.DevelopmentBatch{batch("usage", "analytics.go", "usage prints the byte count and main invokes it")}
	}
	return plan
}

func canaryBytesRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Each native call owns its peer script. The wrapper fixes that script before
// process launch, including DSH whose adapter uses the calling environment.
func canaryPeerWrapper(testName, key, script string) string {
	binary, _ := os.Executable()
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }
	prefix := ""
	if key != "" {
		prefix = "export " + key + "=" + quote(script) + "\n"
	}
	return "#!/bin/sh\n" + prefix + "exec " + quote(binary) + " -test.run='^" + testName + "$' -- \"$@\"\n"
}

func (e *canaryLocalExecutor) prepareNativePeer(request map[string]any, dir string, answer map[string]any) error {
	c := e.canary
	workspace := resolveNativePath(textOf(request["workspace"]))
	identity := codexPeerID(dir + workspace + textOf(request["prompt"]))
	binding := maps.Clone(objectOrEmpty(request["binding"]))
	environment := maps.Clone(objectOrEmpty(binding["environment"]))
	launch := maps.Clone(objectOrEmpty(binding["launch"]))
	script := filepath.Join(dir, "peer.json")
	wrapper := filepath.Join(dir, "native-peer")
	testName, key := "TestScriptedPeer", "FORMAL_GATES_NATIVE_PEER_SCRIPT"
	switch c.nativeName() {
	case "claude":
		testName, key = "TestClaudeCLI", "CLAUDE_PEER_SCRIPT"
	case "codex":
		testName, key = "TestCodexRPCPeer", "CODEX_PEER_SCRIPT"
	case "cursor":
		testName, key = "TestCursorCLI", "CURSOR_PEER_SCRIPT"
	}
	if err := os.WriteFile(wrapper, []byte(canaryPeerWrapper(testName, key, script)), 0755); err != nil {
		return err
	}
	launch["executable"] = wrapper
	var err error
	binding, err = buildCaptureBinding(c.nativeName(), launch)
	if err != nil {
		return err
	}
	binding["environment"] = environment
	request["binding"] = binding
	paths := c.hostPaths()
	answerJSON := mustCanonical(e.t, answer)
	switch c.nativeName() {
	case "claude":
		peer := claudePeer{Workspace: workspace, Calls: filepath.Join(dir, "peer-calls.jsonl"), Environment: map[string]string{"HOME": c.home, "CLAUDE_CONFIG_DIR": paths["config"]}, Events: []any{
			map[string]any{"type": "system", "subtype": "init", "session_id": identity, "cwd": workspace, "model": "claude-fixture-model", "effort": "high", "uuid": "init-" + identity},
			map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": identity, "uuid": "result-" + identity, "num_turns": 1, "structured_output": answer, "usage": map[string]any{"input_tokens": len(textOf(request["prompt"])), "output_tokens": len(answerJSON)}},
		}}
		s := claudeScenario{request: request, directory: dir, workspace: workspace, script: script, peer: peer}
		s.save(e.t)
	case "codex":
		s := codexScenario{request: request, directory: dir, workspace: workspace, home: c.home, config: paths["config"], runtime: filepath.Join(c.installed.Targets[0].TargetPath, "bin", "formal-gates"), peer: codexPeerInput{Workspace: workspace, Home: c.home, Config: paths["config"], Answer: answer,
			StrictSchema: true, Configuration: map[string]any{"model": "peer-model", "reasoningEffort": "high", "sandbox": map[string]any{"type": "workspaceWrite", "writableRoots": []any{workspace}, "networkAccess": false}, "approvalPolicy": "never", "approvalsReviewer": "user"}}}
		s.peer.Answer = answer
		s.configurePeerInputs(e.t)
		if err := writeAtomicJSON(script, s.peer); err != nil {
			return err
		}
	case "cursor":
		sum := md5.Sum([]byte(workspace))
		peer := cursorPeer{Semantics: answer, UUID: identity, Workspace: workspace, Environment: map[string]string{"HOME": c.home, "CURSOR_CONFIG_DIR": paths["config"], "CURSOR_DATA_DIR": paths["data"], "NODE_COMPILE_CACHE": paths["cache"]}, Calls: filepath.Join(dir, "peer-calls.jsonl"), Store: filepath.Join(paths["config"], "chats", hex.EncodeToString(sum[:]), identity, "store.db"),
			Config: map[string]any{"model": map[string]any{"modelId": "model-a", "displayModelId": "model-a", "displayName": "model-a high"}, "selectedModel": map[string]any{"modelId": "model-a", "parameters": []any{map[string]any{"id": "effort", "value": "high"}}}},
			Events: []any{map[string]any{"type": "system", "subtype": "init", "session_id": identity, "cwd": workspace, "model": "model-a high", "permissionMode": "default"}, map[string]any{"type": "result", "subtype": "success", "is_error": false, "session_id": identity, "request_id": "request-" + identity, "result": answerJSON, "usage": map[string]any{"inputTokens": len(textOf(request["prompt"])), "outputTokens": len(answerJSON)}}}}
		s := cursorScenario{request: request, directory: dir, workspace: workspace, script: script, peer: peer}
		s.save(e.t)
	case "dsh":
		peerEnvironment := map[string]string{}
		for _, key := range []string{"HOME", "DSH_HOME", "DSH_PROJECT_DIR"} {
			peerEnvironment[key] = environment[key].(string)
		}
		events := peerEvent(identity, "assistant/message", map[string]any{"turn": 1, "step": 0, "usage": map[string]any{"inputTokens": len(textOf(request["prompt"])), "outputTokens": len(answerJSON)}, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": answerJSON}}}}) + "\n" + peerEvent(identity, "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
		peer := peerScript{Args: []string{"--profile", "acp", "--patch", filepath.Join(dir, "profile.json")}, Environment: peerEnvironment, Workspace: workspace, Echo: filepath.Join(dir, "peer-input.jsonl"), Steps: []peerStep{
			{On: `"method":"initialize"`, Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"sessionCapabilities":{"resume":{},"close":{}}}}}`},
			{On: `"method":"session/new"`, Params: map[string]any{"cwd": workspace, "mcpServers": []any{}}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `","configOptions":[{"id":"model","currentValue":"test-model"},{"id":"reasoning_effort","currentValue":"high"}]}}`},
			{On: `"method":"session/prompt"`, SemanticJob: dir, Semantics: answer, Params: map[string]any{"sessionId": identity, "prompt": []any{map[string]any{"type": "text", "text": textOf(request["prompt"])}}}, Files: []peerFile{{Path: filepath.Join(dir, "native-source.json"), Body: `{"sessionId":"` + identity + `","inputId":"input-` + identity + `","turn":1}`}, {Path: filepath.Join(dir, "native-events.jsonl"), Body: events}}, Send: `{"id":"@ID@","result":{"stopReason":"end_turn"}}`},
			{On: `"method":"session/close"`, Params: map[string]any{"sessionId": identity}, Send: `{"id":"@ID@","result":{}}`},
		}}
		if err := writeAtomicJSON(script, peer); err != nil {
			return err
		}
	case "zcode":
		peerEnvironment := map[string]string{}
		for _, key := range []string{"HOME", "USERPROFILE", "ZCODE_DATA_BASE_DIR", "ZCODE_HOME", "ZCODE_STORAGE_DIR", "ZCODE_SESSION_DB_PATH", "ZCODE_SESSION_DB", "ZCODE_LOG_DIR", "ZCODE_MAILBOX_ROOT", zcodeBuiltinCatalogueEnv, zcodePersonalConfigEnv, zcodeCredentialHelperEnv, "ZCODE_BUILTIN_PROVIDER_BUNDLED_CONFIG_FILE"} {
			peerEnvironment[key] = environment[key].(string)
		}
		input := "formal-gates-" + filepath.Base(dir)
		turn := "turn-" + identity
		create := map[string]any{"workspace": map[string]any{"workspacePath": workspace, "workspaceKey": workspace}, "persistence": "immediate"}
		if request["read_only"] == true {
			create["toolAllowlist"] = zcodeManagedTools
		}
		steps := []peerStep{
			{On: `"method":"runtime/capabilities"`, Params: map[string]any{}, Send: `{"id":"@ID@","result":{}}`},
			{On: `"method":"session/create"`, Params: create, Send: zcodeSnapshotFrame(identity, zcodeSettings)},
			{On: `"method":"session/subscribe"`, Params: map[string]any{"sessionId": identity, "deliveryKind": "desktop-continuous", "includeSnapshot": false}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `"}}`},
			{On: `"method":"session/send"`, SemanticJob: dir, Semantics: answer, Params: map[string]any{"sessionId": identity, "inputId": input, "content": textOf(request["prompt"])}, Send: `{"id":"@ID@","result":{"sessionId":"` + identity + `","accepted":true}}` + "\n" + zcodeEventLine(identity, 1, "turn.started", turn, zcodeTurnPayload(input, "")) + "\n" + zcodeEventLine(identity, 2, "turn.completed", turn, zcodeTurnPayload(input, `,"resultType":"success","response":`+mustCanonical(e.t, answerJSON)+`,"usage":{"inputTokens":`+strconv.Itoa(len(textOf(request["prompt"])))+`,"outputTokens":`+strconv.Itoa(len(answerJSON))+`}`))},
			{On: `"method":"session/close"`, Params: map[string]any{"sessionId": identity}, Send: `{"id":"@ID@","result":{}}`},
		}
		if mode, _ := request["permission_mode"].(string); mode != "" {
			steps = append(steps, peerStep{On: `"method":"session/setMode"`, Params: map[string]any{"sessionId": identity, "mode": mode}, Send: `{"id":"@ID@","result":{"settings":{"mode":{"current":` + strconv.Quote(mode) + `}}}}`})
		}
		delivered, err := zcodeAccountProviderConfiguration(paths["catalogue"], nil)
		if err != nil {
			return err
		}
		if delivered != nil {
			steps = append(steps, peerStep{On: `"method":"` + zcodeAccountConfigMethod + `"`, Params: delivered["configuration"], Send: `{"id":"@ID@","result":{"status":"received","providerCount":` + strconv.Itoa(len(listOf(delivered["providers"]))) + `}}`})
		}
		if err := writeAtomicJSON(script, peerScript{Args: []string{textOf(launch["script"]), "--cwd", workspace, "app-server"}, Environment: peerEnvironment, Workspace: workspace, Echo: filepath.Join(dir, "peer-input.jsonl"), Steps: steps}); err != nil {
			return err
		}
	}
	return writeAtomicJSON(filepath.Join(dir, "native-request.json"), request)
}

// Cancellation uses the same public supervisor and native job as completion.
func TestCanaryACPAndAppServerCancellation(t *testing.T) {
	binary := supervisorBinary(t)
	for _, nativeHost := range []string{"dsh", "zcode"} {
		t.Run(nativeHost, func(t *testing.T) {
			var request map[string]any
			var dir, workspace string
			if nativeHost == "dsh" {
				steps := successSteps()
				steps[3] = peerStep{On: `"method":"session/prompt"`, IgnoreEOF: true}
				s := newDSHScenario(t, steps, nil)
				request, dir, workspace = s.request, s.directory, s.workspace
			} else {
				s := newZcodeScenario(t, func(input string) []peerStep {
					steps := zcodeSuccessSteps(input)
					steps = append(steps[:2], peerStep{On: `"method":"session/send"`, IgnoreEOF: true})
					return steps
				}, nil)
				request, dir, workspace = s.request, s.directory, s.workspace
			}
			writeRequest(t, dir, map[string]any{"kind": "native", "argv": []string{binary, "harness", "native", dir}, "cwd": workspace, "stdin": ""})
			command := exec.Command(binary, "harness", "supervise", dir)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waitForFile(t, filepath.Join(dir, "identity.json"))
			if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err != nil {
				t.Fatal(err)
			}
			receipt := readReceipt(t, dir)
			if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
				t.Fatalf("cancel receipt: %v", receipt)
			}
			if _, err := settleNativeReceipt(dir, receipt); err == nil {
				t.Fatal("cancelled native output admitted")
			}
			process, err := readJSONFile(filepath.Join(dir, "native-process.json"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(textOf(getOf(process, "pid")))
			if err != nil || syscall.Kill(pid, 0) == nil {
				t.Fatalf("native process survived: %v %v", process, err)
			}
			if got, err := readJSONFile(filepath.Join(dir, "native-request.json")); err != nil || !valuesEqual(got, request) {
				t.Fatalf("cancel rewrote request: %v %v", got, err)
			}
		})
	}
}

// Public native jobs reject each wrong launch input before the peer can emit
// its canned session or write successful evidence; binding proofs stay valid.
func TestCanaryNativePeersValidateLaunchInputs(t *testing.T) {
	binary := supervisorBinary(t)
	for _, nativeHost := range []string{"dsh", "zcode"} {
		t.Run(nativeHost, func(t *testing.T) {
			cases := []string{"valid", "argv", "cwd", "home"}
			if nativeHost == "dsh" {
				cases = append(cases, "dsh-home")
			}
			for _, dimension := range cases {
				t.Run(dimension, func(t *testing.T) {
					home := resolveNativePath(t.TempDir())
					t.Setenv("HOME", home)
					t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
					var request map[string]any
					var dir, workspace, echo string
					if nativeHost == "dsh" {
						s := newDSHScenario(t, successSteps(), func(request map[string]any) { request["model"], request["reasoning_effort"] = "test-model", "high" })
						request, dir, workspace, echo = s.request, s.directory, s.workspace, s.echoPath
					} else {
						s := newZcodeScenario(t, zcodeSuccessSteps, nil)
						request, dir, workspace, echo = s.request, s.directory, s.workspace, s.echoPath
					}
					binding := objectOrEmpty(request["binding"])
					launch := objectOrEmpty(binding["launch"])
					expected := peerScript{Workspace: workspace, Environment: map[string]string{"HOME": home}}
					if nativeHost == "dsh" {
						expected.Args = []string{"--profile", "acp", "--patch", filepath.Join(dir, "profile.json")}
						expected.Environment["DSH_HOME"] = filepath.Join(home, ".dsh")
					} else {
						expected.Args = []string{textOf(launch["script"]), "--cwd", workspace, "app-server"}
						binding["environment"] = map[string]any{"HOME": home}
					}
					scriptPath := os.Getenv("FORMAL_GATES_NATIVE_PEER_SCRIPT")
					body := readTestFileBytes(t, scriptPath)
					var script peerScript
					if err := json.Unmarshal(body, &script); err != nil {
						t.Fatal(err)
					}
					script.Args, script.Environment, script.Workspace = expected.Args, expected.Environment, expected.Workspace
					if err := writeAtomicJSON(scriptPath, script); err != nil {
						t.Fatal(err)
					}
					wrapper := textOf(launch["executable"])
					wrapperBody := readTestFile(t, wrapper)
					prefix, suffix, refusal := "", "", ""
					switch dimension {
					case "argv":
						suffix, refusal = " --unexpected", "native argv differs"
					case "cwd":
						prefix, refusal = "cd '"+resolveNativePath(t.TempDir())+"'\n", "native cwd differs"
					case "home":
						prefix, refusal = "export HOME='"+resolveNativePath(t.TempDir())+"'\n", "native env HOME differs"
					case "dsh-home":
						prefix, refusal = "export DSH_HOME='"+resolveNativePath(t.TempDir())+"'\n", "native env DSH_HOME differs"
					}
					wrapperBody = strings.Replace(wrapperBody, "#!/bin/sh\n", "#!/bin/sh\n"+prefix, 1)
					wrapperBody = strings.Replace(wrapperBody, "\"$@\"\n", "\"$@\""+suffix+"\n", 1)
					if err := os.WriteFile(wrapper, []byte(wrapperBody), 0755); err != nil {
						t.Fatal(err)
					}
					environment := binding["environment"]
					bound, err := buildCaptureBinding(nativeHost, launch)
					if err != nil {
						t.Fatal(err)
					}
					if environment != nil {
						bound["environment"] = environment
					}
					request["binding"] = bound
					impl, err := nativeImplementationFor(nativeHost)
					if err != nil {
						t.Fatal(err)
					}
					if err := impl.validateBinding(bound); err != nil {
						t.Fatalf("negative control has invalid binding: %v", err)
					}
					if err := writeAtomicJSON(filepath.Join(dir, "native-request.json"), request); err != nil {
						t.Fatal(err)
					}
					command := exec.Command(binary, "harness", "native", dir)
					command.Dir = workspace
					output, err := command.CombinedOutput()
					t.Logf("public native host=%s input=%s exit=%v output=%s", nativeHost, dimension, err, output)
					if dimension == "valid" {
						if err != nil {
							t.Fatal(err)
						}
						source, err := readJSONFile(filepath.Join(dir, "source.json"))
						if err != nil || getOf(source, "provider") != nativeHost {
							t.Fatalf("valid launch source: %v %v", source, err)
						}
						if err := impl.verifySource(dir, source); err != nil {
							t.Fatal(err)
						}
					} else {
						if err == nil {
							t.Fatal("wrong launch returned successful source")
						}
						stderr := readTestFile(t, filepath.Join(dir, "host.stderr"))
						if !strings.Contains(stderr, refusal) {
							t.Fatalf("expected peer launch refusal %q, got %s", refusal, stderr)
						}
						if _, err := os.Stat(filepath.Join(dir, "source.json")); !os.IsNotExist(err) {
							t.Fatal("wrong launch wrote successful source")
						}
						if _, err := os.Stat(filepath.Join(dir, "identity.json")); !os.IsNotExist(err) {
							t.Fatal("wrong launch emitted a session identity")
						}
						if _, err := os.Stat(echo); !os.IsNotExist(err) {
							t.Fatal("wrong launch processed RPC before refusal")
						}
					}
				})
			}
		})
	}
}

// The synthetic Codex peer emits the native spelling required by its actual
// output schema. This runs before RPC output; original native bytes stay intact.
