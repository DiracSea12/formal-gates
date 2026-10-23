package harness

// Codex app-server owns persistent threads, turn identity and permissions.
// This adapter speaks its native stdio protocol and observes the same raw
// evidence on live completion and recovery. Recovery never starts a turn.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Codex reads session hooks from its original config overrides. The thread's
// bypass_hook_trust enables these program-owned hooks without a trust store.
func codexToolArguments(directory string) ([]string, error) {
	server, err := nativeToolServer(directory)
	if err != nil {
		return nil, err
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	command := quote(textOf(server["command"])) + " harness codex-tool-permission " + quote(directory)
	return []string{"-c", `hooks.PreToolUse=[{matcher=".*",hooks=[{type="command",command=` + strconv.Quote(command) + `}]}]`}, nil
}

func CodexToolPermission(directory string, in io.Reader, out, errout io.Writer, services ...ExecutionServices) int {
	var input struct {
		Event string `json:"hook_event_name"`
		Tool  string `json:"tool_name"`
		Cwd   string `json:"cwd"`
	}
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	tools, err := newNativeWorkspaceTools(directory, services...)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	release, err := admitExecution(directory, objectOrEmpty(request), services)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	if release != nil {
		defer release()
	}
	output := map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "Use the managed workspace interface instead of " + input.Tool}}
	if getOf(request, "provider") == "codex" && input.Event == "PreToolUse" && resolveNativePath(input.Cwd) == tools.workspace && (input.Tool == nativeWorkspaceTool || input.Tool == nativeSemanticTool || input.Tool == "exec" || input.Tool == "wait") {
		output = map[string]any{}
	}
	if err := json.NewEncoder(out).Encode(output); err != nil {
		fmt.Fprintln(errout, err)
		return 2
	}
	return 0
}

func codexWorkspaceReply(tools *nativeWorkspaceTools, params any, thread, turn string) (*nativeReply, error) {
	callID, _ := getOf(params, "callId").(string)
	namespace := getOf(params, "namespace")
	if getOf(params, "threadId") != thread || getOf(params, "turnId") != turn || callID == "" || (getOf(params, "tool") != nativeWorkspaceTool && getOf(params, "tool") != nativeSemanticTool) || (namespace != nil && namespace != "") {
		return nil, &blockedError{message: "CODEX_BINDING: dynamic tool differs from original thread/turn/registration"}
	}
	raw, err := json.Marshal(getOf(params, "arguments"))
	if err != nil {
		return nil, err
	}
	value, callErr := tools.invoke(textOf(getOf(params, "tool")), raw)
	content := ""
	if callErr != nil {
		content = callErr.Error()
		if getOf(params, "tool") == nativeSemanticTool {
			body, err := json.Marshal(map[string]any{"error": content, "inputSchema": getOf(tools.schemas()[1], "inputSchema")})
			if err != nil {
				return nil, err
			}
			content = string(body)
		}
	} else {
		body, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		content = string(body)
	}
	result := map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": content}}, "success": callErr == nil}
	request, err := json.Marshal(map[string]any{"method": "item/tool/call", "params": params})
	if err != nil {
		return nil, err
	}
	if err := tools.recordCall(request, result); err != nil {
		return nil, err
	}
	return &nativeReply{payload: map[string]any{"result": result}, status: "tool-result"}, nil
}

// Dynamic tool declarations cover the role's native continuations. The current
// issued material controls workspace writes and semantic submissions.
func codexToolSchemas(tools *nativeWorkspaceTools) ([]any, error) {
	schemas := tools.schemas()
	if tools.material != nil {
		if tools.material.Input.Handler == "engine.development.batch.v6" {
			schemas[0] = nativeWorkspaceToolSchema(false, tools.material)
		}
		schema, err := worker.StableResultSchema(tools.material.Input)
		if err != nil {
			return nil, err
		}
		objectOrEmpty(schemas[1])["inputSchema"] = schema
	}
	body, err := json.Marshal(schemas)
	if err != nil {
		return nil, err
	}
	plain, err := decodeNativeJSON(body)
	if err != nil {
		return nil, err
	}
	return listOf(plain), nil
}

var codexCaptureBinding = nativeCapture(codexResolvePaths, codexConfiguredBinding)

// codexExtensionBinDirs is the chatgpt extension's platform binary directory
// naming: GOOS/GOARCH mapped onto the bundle's own triples.
var codexExtensionBinDirs = map[string]string{
	"windows/amd64": "windows-x86_64",
	"windows/arm64": "windows-aarch64",
	"darwin/amd64":  "darwin-x86_64",
	"darwin/arm64":  "darwin-aarch64",
	"linux/amd64":   "linux-x86_64",
	"linux/arm64":   "linux-aarch64",
}

// codexBundleCandidates names the bundled install forms in probe order: the
// chatgpt editor extension's platform binary under every editor root, then
// the desktop app's versioned release directory.
func codexBundleCandidates() []string {
	binary := "codex"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	candidates := []string{}
	if platform := codexExtensionBinDirs[runtime.GOOS+"/"+runtime.GOARCH]; platform != "" {
		for _, root := range nativeEditorExtensionRoots() {
			candidates = append(candidates, filepath.Join(root, "openai.chatgpt-*", "bin", platform, binary))
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates, filepath.Join(local, "OpenAI", "Codex", "bin", "*", "codex.exe"))
	}
	return candidates
}

func codexResolvePaths(native, sources map[string]string) error {
	config := settings.Values{Native: native}
	executable := config.NativeString("codexExecutable")
	path, executableSource, err := nativeExecutablePath(executable)
	if err != nil && executable == "codex" {
		// The extension and desktop-app bundles are other install forms of
		// the same CLI.
		if candidate, source, ok := nativeFixedLaunch(codexBundleCandidates()...); ok {
			path, executableSource, err = candidate, source, nil
		}
	}
	if err != nil {
		return &blockedError{message: "CODEX_UNAVAILABLE: codex is not on PATH"}
	}
	home := config.NativeString("codexHome")
	explicitHome := home != ""
	home, homeSource, err := nativeUserHome(home)
	if err != nil {
		return err
	}
	home = resolveNativePath(home)
	nativeHome := native["codexConfigRoot"]
	rootSource := "codexHome"
	if nativeHome == "" {
		if !explicitHome {
			nativeHome = os.Getenv("CODEX_HOME")
			if nativeHome != "" {
				rootSource = "CODEX_HOME"
			}
		}
		if nativeHome == "" {
			nativeHome = filepath.Join(home, ".codex")
		}
	}
	recordNativePaths(native, sources,
		map[string]string{"codexExecutable": resolveNativePath(path), "codexHome": home, "codexConfigRoot": resolveNativePath(nativeHome)},
		map[string]string{"codexExecutable": executableSource, "codexHome": homeSource, "codexConfigRoot": rootSource},
	)
	return nil
}

func codexConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable, home, nativeHome := config.Native["codexExecutable"], config.Native["codexHome"], config.Native["codexConfigRoot"]
	if executable == "" || home == "" || nativeHome == "" {
		return nil, &blockedError{message: "CODEX_CONFIG: frozen native paths required"}
	}
	binding, err := buildCaptureBinding("codex", map[string]any{"executable": executable})
	if err != nil {
		return nil, err
	}
	// Native config/auth resolution stays with app-server. Its paths and the
	// public host permission profile are pinned without copying credentials.
	environment := map[string]any{"HOME": home, "CODEX_HOME": nativeHome}
	if profile := os.Getenv("CODEX_PERMISSION_PROFILE"); profile != "" {
		environment["CODEX_PERMISSION_PROFILE"] = profile
	}
	binding["environment"] = environment
	return binding, nil
}

func codexValidateBinding(binding any) error { return validateNativeBinding(binding, "codex") }
func codexVerifySource(directory string, source any) error {
	if err := verifyNativeSource(directory, source, "codex"); err != nil {
		return err
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return err
	}
	expectedAnswer, err := submittedSemantics(directory)
	if err != nil {
		return err
	}
	decoded, err := os.ReadFile(filepath.Join(directory, "answer.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedAnswer, decoded) {
		return &blockedError{message: "CODEX_BINDING: result differs from original semantic submission"}
	}
	original, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return err
	}
	effective, err := requestWithNativeMessages(directory, request)
	if err != nil {
		return err
	}
	expected, err := codexTurnParameters(effective, textOf(getOf(source, "identity")))
	if err != nil {
		return err
	}
	if !valuesEqual(expected, getOf(original, "turn_request")) {
		return &blockedError{message: "CODEX_BINDING: original turn request differs"}
	}
	return nil
}

func codexInteractionID(frameID any) (string, error) {
	if frameID == nil {
		return "", &blockedError{message: "CODEX_INTERACTION: missing native request id"}
	}
	return nativeSerializeID(frameID), nil
}

func codexOptions(decisions []any) []any {
	options := make([]any, 0, len(decisions))
	for _, decision := range decisions {
		options = append(options, map[string]any{"value": textOf(decision), "decision": decision})
	}
	return options
}

// The primary CommandExecutionApprovalDecision/FileChangeApprovalDecision
// schemas define the choices; command amendments contain the host's original
// proposal. They are displayed and returned as values, not interpreted here.
func codexApprovalDecisions(params map[string]any, command bool) []any {
	decisions := []any{"accept", "acceptForSession", "decline", "cancel"}
	if command {
		if proposal := params["proposedExecpolicyAmendment"]; proposal != nil {
			decisions = append(decisions, map[string]any{"acceptWithExecpolicyAmendment": map[string]any{"execpolicy_amendment": proposal}})
		}
		for _, proposal := range listOf(params["proposedNetworkPolicyAmendments"]) {
			decisions = append(decisions, map[string]any{"applyNetworkPolicyAmendment": map[string]any{"network_policy_amendment": proposal}})
		}
	}
	return decisions
}

func codexInteractionSpecs(thread string, turn *string) []nativeInteractionSpec {
	body := func(params map[string]any) map[string]any { return params }
	identity := func(params map[string]any, frameID any) (string, error) {
		if !valuesEqual(params["threadId"], thread) || (params["turnId"] != nil && !valuesEqual(params["turnId"], *turn)) {
			return "", &blockedError{message: "CODEX_BINDING: interaction differs from original thread/turn"}
		}
		return codexInteractionID(frameID)
	}
	specs := []nativeInteractionSpec{}
	for _, method := range []string{"item/commandExecution/requestApproval", "item/fileChange/requestApproval"} {
		command := method == "item/commandExecution/requestApproval"
		specs = append(specs, nativeInteractionSpec{method: method, requestID: identity, body: body,
			summary: func(p map[string]any) string {
				return textOf(getOrOf(p, "reason", getOrOf(p, "command", "Codex approval")))
			},
			options: func(p map[string]any) any { return codexOptions(codexApprovalDecisions(p, command)) },
			reply: func(p map[string]any, c nativeAnswerChoice) (map[string]any, error) {
				if c.Cancel {
					return map[string]any{"result": map[string]any{"decision": "cancel"}}, nil
				}
				for _, d := range codexApprovalDecisions(p, command) {
					if textOf(d) == c.OptionID {
						return map[string]any{"result": map[string]any{"decision": d}}, nil
					}
				}
				return nil, &blockedError{message: "CODEX_INTERACTION: decision was not offered"}
			},
		})
	}
	specs = append(specs, nativeInteractionSpec{method: "item/tool/requestUserInput", requestID: identity, body: body,
		summary: func(p map[string]any) string { return textOf(p["questions"]) },
		options: func(p map[string]any) any { return p["questions"] },
		reply: func(p map[string]any, c nativeAnswerChoice) (map[string]any, error) {
			answers := map[string]any{}
			if !c.Cancel {
				answers = objectOrEmpty(c.Answers)
				if len(answers) != len(listOf(p["questions"])) {
					return nil, &blockedError{message: "CODEX_INPUT: answer each original question"}
				}
				for _, q := range listOf(p["questions"]) {
					id := textOf(getOf(q, "id"))
					a, ok := answers[id].(map[string]any)
					if !ok {
						return nil, &blockedError{message: "CODEX_INPUT: missing question " + id}
					}
					selections, ok := a["answers"].([]any)
					if !ok || len(selections) == 0 {
						return nil, &blockedError{message: "CODEX_INPUT: answers must be a nonempty string list"}
					}
					for _, selection := range selections {
						text, ok := selection.(string)
						if !ok {
							return nil, &blockedError{message: "CODEX_INPUT: answer must be a string"}
						}
						offered := getOf(q, "options") == nil || getOf(q, "isOther") == true
						for _, option := range listOf(getOf(q, "options")) {
							offered = offered || valuesEqual(getOf(option, "label"), text)
						}
						if !offered {
							return nil, &blockedError{message: "CODEX_INPUT: answer was not offered for " + id}
						}
					}
				}
			}
			return map[string]any{"result": map[string]any{"answers": answers}}, nil
		},
	})
	specs = append(specs, nativeInteractionSpec{method: "item/permissions/requestApproval", requestID: identity, body: body,
		summary: func(p map[string]any) string { return textOf(p["reason"]) },
		options: func(p map[string]any) any { return codexOptions([]any{"accept", "decline"}) },
		reply: func(p map[string]any, c nativeAnswerChoice) (map[string]any, error) {
			permissions := any(map[string]any{})
			if !c.Cancel && c.OptionID == "accept" {
				permissions = p["permissions"]
			} else if !c.Cancel && c.OptionID != "decline" {
				return nil, &blockedError{message: "CODEX_INTERACTION: decision was not offered"}
			}
			return map[string]any{"result": map[string]any{"permissions": permissions, "scope": "turn"}}, nil
		},
	})
	specs = append(specs, nativeInteractionSpec{method: "mcpServer/elicitation/request", requestID: identity, body: body,
		summary: func(p map[string]any) string { return textOf(p["message"]) },
		options: func(p map[string]any) any { return codexOptions([]any{"accept", "decline", "cancel"}) },
		reply: func(p map[string]any, c nativeAnswerChoice) (map[string]any, error) {
			action := c.OptionID
			if c.Cancel {
				action = "cancel"
			}
			if action != "accept" && action != "decline" && action != "cancel" {
				return nil, &blockedError{message: "CODEX_INTERACTION: action was not offered"}
			}
			result := map[string]any{"action": action}
			if action == "accept" {
				if !valuesEqual(p["mode"], "url") && c.Answers == nil {
					return nil, &blockedError{message: "CODEX_INPUT: elicitation needs structured content"}
				}
				result["content"] = c.Answers
			}
			return map[string]any{"result": result}, nil
		},
	})
	return specs
}

func codexInteractionObserved(hold *nativePendingHold, message map[string]any) bool {
	p := objectOrEmpty(message["params"])
	if !valuesEqual(p["threadId"], hold.params["threadId"]) {
		return false
	}
	if valuesEqual(message["method"], "turn/completed") {
		return valuesEqual(getOf(p["turn"], "id"), hold.params["turnId"])
	}
	return valuesEqual(message["method"], "item/completed") && valuesEqual(p["turnId"], hold.params["turnId"]) && valuesEqual(getOf(p["item"], "id"), hold.params["itemId"])
}

// ThreadStartResponse and ThreadResumeResponse expose effective configuration.
// Non-readonly roles use the captured host permission profile when supplied;
// read-only roles constrain the sandbox. Execution and recovery check the same
// native response against the original issued request.
func codexCheckConfiguration(request, configuration any, thread string) error {
	workspace, _ := getOf(request, "workspace").(string)
	sandboxType, _ := getOf(getOf(configuration, "sandbox"), "type").(string)
	reviewer, _ := getOf(configuration, "approvalsReviewer").(string)
	nativeThread := getOf(configuration, "thread")
	if !valuesEqual(getOf(nativeThread, "id"), thread) || !valuesEqual(getOf(nativeThread, "ephemeral"), false) || !valuesEqual(resolveNativePath(textOf(getOf(configuration, "cwd"))), resolveNativePath(workspace)) || sandboxType == "" || getOf(configuration, "approvalPolicy") == nil || reviewer == "" || (getOf(request, "read_only") == true && sandboxType != "readOnly") {
		return &blockedError{message: "CODEX_CONFIG: native workspace/persistence/permissions differ"}
	}
	profile := nativeBindingEnvironment(getOf(request, "binding"))["CODEX_PERMISSION_PROFILE"]
	if getOf(request, "read_only") != true && profile != "" && !valuesEqual(getOf(getOf(configuration, "activePermissionProfile"), "id"), profile) {
		return &blockedError{message: "CODEX_CONFIG: native permission profile differs or is missing"}
	}
	selected, _ := getOf(configuration, "model").(string)
	requested, _ := getOf(request, "model").(string)
	if selected == "" || (requested != "" && selected != requested) {
		return &blockedError{message: "CODEX_CONFIG: native model selection differs or is missing"}
	}
	return nil
}

func codexExecute(request any, directory string, probe bool, services ...ExecutionServices) (source map[string]any, resultErr error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	binding, err := nativeExecutionBinding(request, codexCaptureBinding)
	if err != nil {
		return nil, err
	}
	if err := codexValidateBinding(binding); err != nil {
		return nil, err
	}
	executable, _ := getOf(getOf(binding, "launch"), "executable").(string)
	if executable == "" {
		return nil, &bindingError{message: "CODEX_BINDING: missing executable"}
	}
	environment := nativeBindingEnvironment(binding)
	workspace, _ := getOf(request, "workspace").(string)
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	thread, turn := "", ""
	var tools *nativeWorkspaceTools
	var terminal any
	var recordErr error
	hooks := nativeHooks{transportPrefix: "CODEX_NATIVE", requestPrefix: "CODEX_RPC", bounded: true,
		identify: func(c *nativeClient) any { return c.nextCounter() },
		observes: codexInteractionObserved,
		respond: func(method string, params any) (*nativeReply, error) {
			if method == "item/tool/call" && tools != nil {
				return codexWorkspaceReply(tools, params, thread, turn)
			}
			return nil, &blockedError{message: "CODEX_INTERACTION: unsupported native request " + method + ": " + textOf(params)}
		},
		received: func(message any) {
			line, err := CanonicalJSON(message)
			if err == nil {
				_, err = events.Write(append(line, '\n'))
			}
			if err != nil {
				recordErr = err
			}
			if valuesEqual(getOf(message, "method"), "turn/completed") {
				terminal = getOf(message, "params")
			}
		},
	}
	arguments, err := codexToolArguments(directory)
	if err != nil {
		return nil, err
	}
	client, err := newNativeClientWithEnv(hooks, append([]string{executable, "app-server", "--stdio"}, arguments...), workspace, directory, environment)
	if err != nil {
		return nil, err
	}
	defer client.close()
	initialized, err := client.request("initialize", map[string]any{"clientInfo": map[string]any{"name": "formal-gates", "title": "Formal Gates", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if err != nil {
		return nil, err
	}
	if err = client.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	if probe {
		if getOf(request, "selection_options") == true {
			catalogue, err := codexSelectionOptions(client, workspace, nativeSelectionString(getOf(request, "selection_model")))
			return map[string]any{"selection_options": catalogue, "model_calls": 0}, err
		}
		if err = client.close(); err != nil {
			return nil, err
		}
		return map[string]any{"provider": "codex", "binding": binding, "initialize": initialized, "selection": map[string]any{}, "capabilities": map[string]any{"source": "app-server-initialize-and-local-schema", "selection_observed": false}, "model_calls": 0}, nil
	}
	_, ok := getOf(request, "prompt").(string)
	if !ok {
		return nil, fmt.Errorf("prompt must be a string")
	}
	readOnly, ok := getOf(request, "read_only").(bool)
	if !ok {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	tools, err = newNativeWorkspaceTools(directory, services...)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"cwd": workspace, "config": map[string]any{"bypass_hook_trust": true, "features.shell_tool": false, "features.unified_exec": false, "tools.view_image": false, "web_search": "disabled"}}
	if readOnly {
		params["sandbox"] = "read-only"
	} else if profile := environment["CODEX_PERMISSION_PROFILE"]; profile != "" {
		params["permissions"] = profile
	}
	model, _ := getOf(request, "model").(string)
	if model != "" {
		params["model"] = model
	}
	method := "thread/start"
	wanted, _ := getOf(request, "identity").(string)
	if wanted != "" {
		method = "thread/resume"
		params["threadId"] = wanted
		params["excludeTurns"] = true
	} else {
		params["ephemeral"] = false
		declarations, err := codexToolSchemas(tools)
		if err != nil {
			return nil, err
		}
		params["dynamicTools"] = declarations
	}
	opened, err := client.request(method, params)
	if err != nil {
		return nil, err
	}
	nativeThread := getOf(opened, "thread")
	thread, _ = getOf(nativeThread, "id").(string)
	if thread == "" || (wanted != "" && thread != wanted) {
		return nil, &blockedError{message: "CODEX_BINDING: resumed native thread identity differs or is missing"}
	}
	selection := map[string]any{"model": getOf(opened, "model"), "reasoning_effort": getOf(opened, "reasoningEffort")}
	nativeSource := map[string]any{"threadId": thread, "workspace": workspace, "thread": nativeThread, "configuration": opened, "selection": selection, "thread_request": params}
	if err = writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"identity": thread, "workspace": workspace, "selection": selection, "acknowledgement": opened}); err != nil {
		return nil, err
	}
	if err = writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	if err = codexCheckConfiguration(request, opened, thread); err != nil {
		return nil, err
	}
	turnParams, err := codexTurnParameters(request, thread)
	if err != nil {
		return nil, err
	}
	// TurnStartParams declares model/effort as overrides. Keep that request as
	// provenance; Thread.model/reasoningEffort are configuration, not telemetry.
	nativeSource["turn_request"] = turnParams
	client.hooks.interactions = codexInteractionSpecs(thread, &turn)
	started, err := client.request("turn/start", turnParams)
	if err != nil {
		return nil, err
	}
	nativeTurn := getOf(started, "turn")
	turn, _ = getOf(nativeTurn, "id").(string)
	if turn == "" {
		return nil, &blockedError{message: "CODEX_BINDING: native turn id is missing"}
	}
	nativeSource["turnId"] = turn
	nativeSource["turn_ack"] = nativeTurn
	if effort, _ := turnParams["effort"].(string); effort != "" {
		selection["reasoning_effort"] = effort
	}
	// The native job receives the supervisor's cancellation signal. The RPC
	// targets only this acknowledged turn; the supervisor still owns the final
	// cancellation receipt and process-group termination.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	done := make(chan struct{})
	defer close(done)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			_ = client.send(client.frame("interrupt", "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}))
		case <-done:
		}
	}()
	if err = writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"identity": thread, "workspace": workspace, "selection": selection, "acknowledgement": opened}); err != nil {
		return nil, err
	}
	if err = writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	if err = settleQueuedMessages(directory, request, nil, services); err != nil {
		return nil, err
	}
	var inflight *protocol.TaskMessage
	defer func() {
		if inflight != nil && len(services) > 0 && services[0].ReceiptMessage != nil {
			message := *inflight
			message.Status = "FAILED"
			message.Reason = "native turn ended before delivery acknowledgement; delivery outcome unknown; not automatically resent"
			resultErr = errors.Join(resultErr, services[0].ReceiptMessage(directory, message))
		}
	}()
	poll := 100 * time.Millisecond
	for !valuesEqual(getOf(terminal, "threadId"), thread) || !valuesEqual(getOf(getOf(terminal, "turn"), "id"), turn) {
		if inflight == nil && len(services) > 0 && services[0].ClaimMessage != nil && services[0].ReceiptMessage != nil && getOf(request, "engine") != nil {
			message, err := services[0].ClaimMessage(directory, thread, false)
			if err != nil {
				return nil, err
			}
			if message != nil {
				inflight = message
				if err := client.send(client.frame("message:"+message.ID, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": []any{map[string]any{"type": "text", "text": message.Text, "text_elements": []any{}}}})); err != nil {
					return nil, err
				}
			}
		}
		message, receiveErr := client.receive(&poll)
		if errors.Is(receiveErr, errNativeTimedOut) {
			continue
		}
		if receiveErr != nil {
			return nil, receiveErr
		}
		if inflight != nil && getOf(message, "id") == "message:"+inflight.ID {
			receipt := *inflight
			receipt.Status = "ACCEPTED"
			if getOf(message, "error") != nil {
				receipt.Status = "FAILED"
				receipt.Reason = textOf(getOf(message, "error"))
			} else if getOf(getOf(message, "result"), "turnId") != turn {
				receipt.Status = "FAILED"
				receipt.Reason = "native delivery acknowledgement differs from original turn"
			}
			if err := services[0].ReceiptMessage(directory, receipt); err != nil {
				return nil, err
			}
			inflight = nil
		}
		if recordErr != nil {
			return nil, recordErr
		}
	}
	if _, err = client.request("thread/unsubscribe", map[string]any{"threadId": thread}); err != nil {
		return nil, err
	}
	// EOF and Wait have the original nativeClient owner. Drain queued raw
	// messages before computing proofs so normal and recovery see one stream.
	client.stdin.Close()
	for {
		_, err = client.receive(nil)
		if err != nil {
			break
		}
	}
	if err.Error() != "CODEX_NATIVE: Native output closed" {
		return nil, err
	}
	if err = client.close(); err != nil {
		return nil, &blockedError{message: "CODEX_NATIVE: app-server exit: " + err.Error()}
	}
	if recordErr != nil {
		return nil, recordErr
	}
	source, err = codexObserve(directory)
	if err != nil {
		return nil, err
	}
	if err = writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

func codexSelectionOptions(client *nativeClient, workspace, target string) (host.SelectionCatalogue, error) {
	catalogue := host.SelectionCatalogue{Source: "Codex model/list and config/read"}
	configuration, err := client.request("config/read", map[string]any{"cwd": workspace, "includeLayers": false}, nativeUnbounded)
	if err != nil {
		return catalogue, err
	}
	config := getOf(configuration, "config")
	catalogue.Current = host.ModelSelection{Model: nativeSelectionString(getOf(config, "model")), Effort: nativeSelectionString(getOf(config, "model_reasoning_effort"))}
	cursor := ""
	for {
		params := map[string]any{"includeHidden": true}
		if cursor != "" {
			params["cursor"] = cursor
		}
		page, err := client.request("model/list", params, nativeUnbounded)
		if err != nil {
			return catalogue, err
		}
		for _, entry := range listOf(getOf(page, "data")) {
			value := nativeSelectionString(getOf(entry, "model"))
			if value == "" {
				value = nativeSelectionString(getOf(entry, "id"))
			}
			if catalogue.Current.Model == "" && getOf(entry, "isDefault") == true {
				catalogue.Current.Model = value
			}
			catalogue.Models = append(catalogue.Models, host.ModelOption{Value: value, Label: nativeSelectionString(getOf(entry, "displayName"))})
			if value == target || target == "" && value == catalogue.Current.Model {
				option := &catalogue.Models[len(catalogue.Models)-1]
				option.Efforts = nativeStringValues(getOf(entry, "supportedReasoningEfforts"), "reasoningEffort")
				option.DefaultEffort = nativeSelectionString(getOf(entry, "defaultReasoningEffort"))
			}
			if value == catalogue.Current.Model && catalogue.Current.Effort == "" {
				catalogue.Current.Effort = nativeSelectionString(getOf(entry, "defaultReasoningEffort"))
			}
		}
		cursor = nativeSelectionString(getOf(page, "nextCursor"))
		if cursor == "" {
			break
		}
	}
	catalogue.Default = catalogue.Current
	return catalogue, nil
}

// codexObserve uses the original job request, acknowledged thread/turn and raw
// completion evidence for both paths. Same-thread events from another turn
// and child-thread events cannot become this invocation's answer or usage.
func codexObserve(directory string, services ...ExecutionServices) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	thread, _ := getOf(source, "threadId").(string)
	turn, _ := getOf(source, "turnId").(string)
	if thread == "" || turn == "" {
		return nil, &blockedError{message: "CODEX_RECONCILE: original thread/turn is missing"}
	}
	if wanted, _ := getOf(request, "identity").(string); wanted != "" && wanted != thread {
		return nil, &blockedError{message: "CODEX_BINDING: original request thread differs"}
	}
	effective, err := requestWithNativeMessages(directory, request)
	if err != nil {
		return nil, err
	}
	expected, err := codexTurnParameters(effective, thread)
	if err != nil {
		return nil, err
	}
	if !valuesEqual(expected, getOf(source, "turn_request")) {
		return nil, &blockedError{message: "CODEX_BINDING: original turn request differs"}
	}
	if err = codexCheckConfiguration(request, getOf(source, "configuration"), thread); err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(source, "workspace"), getOf(request, "workspace")) || !valuesEqual(getOf(source, "thread"), getOf(getOf(source, "configuration"), "thread")) || !valuesEqual(getOf(getOf(source, "turn_ack"), "id"), turn) {
		return nil, &blockedError{message: "CODEX_BINDING: original configuration/turn acknowledgment differs"}
	}
	selection := map[string]any{"model": getOf(getOf(source, "configuration"), "model"), "reasoning_effort": getOf(getOf(source, "configuration"), "reasoningEffort")}
	if effort := expected["effort"]; effort != nil {
		selection["reasoning_effort"] = effort
	}
	if !valuesEqual(selection, getOf(source, "selection")) {
		return nil, &blockedError{message: "CODEX_BINDING: recorded selection differs from native provenance"}
	}
	configured, acknowledged := false, false
	var terminal, usage any
	err = forEachNativeEvent(directory, func(frame any) error {
		result := getOf(frame, "result")
		configured = configured || valuesEqual(result, getOf(source, "configuration"))
		acknowledged = acknowledged || valuesEqual(getOf(result, "turn"), getOf(source, "turn_ack"))
		p := getOf(frame, "params")
		method := getOf(frame, "method")
		if valuesEqual(getOf(p, "threadId"), thread) {
			if valuesEqual(method, "turn/completed") && valuesEqual(getOf(getOf(p, "turn"), "id"), turn) {
				terminal = getOf(p, "turn")
			}
			if valuesEqual(getOf(p, "turnId"), turn) {
				if valuesEqual(method, "thread/tokenUsage/updated") {
					usage = getOf(p, "tokenUsage")
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !configured || !acknowledged {
		return nil, &blockedError{message: "CODEX_BINDING: original native configuration/turn response is missing"}
	}
	if len(services) > 0 {
		if err := settleQueuedMessages(directory, request, nil, services); err != nil {
			return nil, err
		}
	}
	if terminal == nil {
		return nil, &blockedError{message: "CODEX_RESULT: original turn terminal is missing"}
	}
	if !valuesEqual(getOf(terminal, "status"), "completed") {
		return nil, &blockedError{message: "CODEX_RESULT: original turn did not complete: " + textOf(terminal)}
	}
	if err := writeSubmittedAnswer(directory); err != nil {
		return nil, err
	}
	return evidenceSource(directory, "codex", thread, map[string]any{"native_turn": turn, "native_result": terminal, "selection": getOf(source, "selection"), "selection_evidence": map[string]any{"configuration": getOf(source, "configuration"), "turn_request": getOf(source, "turn_request")}, "transcript": getOf(getOf(source, "thread"), "path"), "usage_evidence": usage})
}

func codexReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
	observe := func() (map[string]any, error) { return codexObserve(directory, services...) }
	if len(services) > 0 {
		source, err := observe()
		observe = func() (map[string]any, error) { return source, err }
	}
	return recoverNativeSource(directory, process, "CODEX", observe)
}
