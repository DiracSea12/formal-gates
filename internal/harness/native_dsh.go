package harness

// The DSH ACP adapter (its verbatim Python reference lives in
// tests/reference_native_dsh.py for the differential),
// the provider dispatch behind `formal-gates harness native <dir>` and the
// four native bridge operations. One native job is one process: read
// native-request.json, run the ACP sequence, land every evidence file, print
// the result JSON to stdout as one canonical line, and on any execution error
// write native-error.json and exit 1.

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/authoring"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// startupTimeout bounds ACP startup only; a prompt waits for the host to
// finish its own turn.
const dshStartupTimeout = 40 * time.Second

// dshPermissionInteraction declares DSH's one held client-directed request.
// Every frame shape below is reverse-read from the host's own protocol stack
// (2026-09-20, dsh 0.1.5-rc.1):
//
//   - Request params: @agentclientprotocol/sdk 1.4.0 (the SDK the dsh acp app
//     imports) schema/zod.gen.js zRequestPermissionRequest -
//     {sessionId, toolCall: {toolCallId, ...}, options: [{optionId, name,
//     kind: allow_once|allow_always|reject_once|reject_always}]}. The dsh acp
//     app builds exactly this on its approval/request hook
//     (@deepseek-ai/dsh-acp lib/index.js, "approval/request" handler) with
//     option ids "allow-once" and "reject-once".
//   - Request identity: ACP has no id inside the params; the JSON-RPC frame id
//     is it (sdk dist/jsonrpc.js prepareRequest: {jsonrpc: "2.0", id:
//     nextRequestId++, method, params}; one connection assigns each id once
//     and ACP never re-announces).
//   - Reply shape: zRequestPermissionResponse - {outcome: {outcome:
//     "selected", optionId}} or {outcome: {outcome: "cancelled"}}. The dsh
//     acp app reads exactly `outcome.outcome === "cancelled"` and
//     `outcome.optionId === "allow-once"` off the reply.
var dshPermissionInteraction = nativeInteractionSpec{
	method: "session/request_permission",
	requestID: func(params map[string]any, frameID any) (string, error) {
		if frameID == nil {
			return "", &bindingError{message: "DSH_INTERACTION: permission request carries no frame id"}
		}
		return nativeSerializeID(frameID), nil
	},
	summary: func(params map[string]any) string {
		toolCall := objectOrEmpty(getOf(params, "toolCall"))
		if id := textOf(getOf(toolCall, "toolCallId")); id != "" && id != "null" {
			return "permission for tool call " + id
		}
		return "permission request"
	},
	options: func(params map[string]any) any { return getOf(params, "options") },
	body: func(params map[string]any) map[string]any {
		// The toolCall names the operation being approved (its toolCallId and
		// whatever else the host put in it) and persists verbatim.
		return nativePickParams(params, "toolCall")
	},
	reply: func(params map[string]any, choice nativeAnswerChoice) (map[string]any, error) {
		if choice.Cancel {
			return map[string]any{"result": map[string]any{
				"outcome": map[string]any{"outcome": "cancelled"}}}, nil
		}
		for _, option := range listOf(getOf(params, "options")) {
			if valuesEqual(getOf(option, "optionId"), choice.OptionID) {
				return map[string]any{"result": map[string]any{"outcome": map[string]any{
					"outcome": "selected", "optionId": choice.OptionID}}}, nil
			}
		}
		return nil, &blockedError{message: "DSH_INTERACTION: option is not one the host offered: " + choice.OptionID}
	},
}

// dshObservesDelivered is DSH's attribution rule for a delivered hold's
// continuation (hooks.observes). Two kinds of fact may confirm it:
//
//   - A session/update of the held session whose tool-card update names the
//     held request's toolCallId. The host's own protocol stack (dsh-acp
//     lib/index.js toolCallUpdate/toolResultUpdate) puts toolCallId at the
//     update's top level with sessionUpdate "tool_call" or "tool_call_update"
//     and content an array; metadata updates - available_commands_update, mode
//     or title traffic - name no tool call and never confirm.
//   - A plain response frame. While a hold is delivered this adapter's request
//     loop has exactly one request in flight - that session's prompt, because
//     request sends one frame and blocks on its matching reply - so a response
//     arriving then is the original operation answering.
func dshObservesDelivered(hold *nativePendingHold, message map[string]any) bool {
	if !isLiveMethod(getOf(message, "method")) {
		return true
	}
	if !valuesEqual(message["method"], "session/update") {
		return false
	}
	params := objectOrEmpty(getOf(message, "params"))
	if !valuesEqual(getOf(params, "sessionId"), getOf(hold.params, "sessionId")) {
		return false
	}
	toolCallID, _ := getOf(objectOrEmpty(getOf(hold.params, "toolCall")), "toolCallId").(string)
	update := objectOrEmpty(getOf(params, "update"))
	kind := getOf(update, "sessionUpdate")
	if !valuesEqual(kind, "tool_call") && !valuesEqual(kind, "tool_call_update") {
		return false
	}
	return toolCallID != "" && valuesEqual(getOf(update, "toolCallId"), toolCallID)
}

// dshHooks is DSH's transport configuration: the host decides when a turn
// ends, so no request carries a total budget (bounded is false: the
// supervisor-style total deadline discipline must not leak in), and a dead
// reader is a transport error rather than a clean close. The permission
// request is held for a user decision, never answered here.
func dshHooks() nativeHooks {
	return nativeHooks{
		transportPrefix: "DSH_TRANSPORT",
		requestPrefix:   "DSH_NATIVE",
		defaultTimeout:  dshStartupTimeout,
		bounded:         false,
		closedKey:       "transport_error",
		closedMessage:   "native ACP process closed its output",
		invalidMessage:  "non-JSON ACP output",
		interactions:    []nativeInteractionSpec{dshPermissionInteraction},
		observes:        dshObservesDelivered,
		identify:        func(c *nativeClient) any { return c.nextCounter() },
		envelope: func(value map[string]any) map[string]any {
			framed := map[string]any{"jsonrpc": "2.0"}
			for key, item := range value {
				framed[key] = item
			}
			return framed
		},
		classify: dshClassifySessionUnavailable,
	}
}

// newDSHClient spawns the host with DSH's transport configuration.
func newDSHClient(argv []string, workspace, directory string) (*nativeClient, error) {
	return newNativeClient(dshHooks(), argv, workspace, directory)
}

// dshClassifySessionUnavailable is the narrow DSH error classification: never
// treat a timeout or a model error as proof that a session was destroyed.
func dshClassifySessionUnavailable(method string, errValue any, params map[string]any) error {
	message := "unknown error"
	if value, present := entryOf(errValue, "message"); present {
		if text, isString := value.(string); isString {
			message = text
		}
	}
	missing := strings.HasPrefix(message, "Invalid params: session is not resumable:") ||
		strings.HasPrefix(message, "Session not found:")
	code := getOf(errValue, "code")
	if method == "session/resume" &&
		(valuesEqual(code, -32602) || valuesEqual(code, -32004)) && missing {
		return &sessionUnavailableError{
			blockedError: blockedError{message: "DSH_SESSION_UNAVAILABLE: " + message},
			identity:     getOrOf(params, "sessionId", ""),
			method:       "session/resume",
			code:         code,
		}
	}
	return nil
}

// dshSelection maps configOptions to {id: currentValue}, a missing
// currentValue becoming null.
func dshSelection(raw any) (map[string]any, error) {
	out := map[string]any{}
	for _, option := range listOf(getOf(raw, "configOptions")) {
		id, err := mustEntry(option, "id")
		if err != nil {
			return nil, err
		}
		out[textOf(id)] = getOf(option, "currentValue")
	}
	return out, nil
}

// dshHarnessRoot mirrors command.go's root discovery exactly:
// FORMAL_GATES_HARNESS_ROOT, else the nearest ancestor of the executable
// holding runtime/langgraph/formal_gates_harness/__main__.py.
func dshHarnessRoot() (string, error) {
	if root := os.Getenv("FORMAL_GATES_HARNESS_ROOT"); root != "" {
		return root, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	for dir := filepath.Dir(exe); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "runtime", "langgraph", "formal_gates_harness", "__main__.py")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return dir, nil
		}
	}
	return "", &bindingError{message: "harness runtime not found; use scripts/harness setup"}
}

// dshPluginPath is the host-side plugin path: native_dsh.py's sibling .mjs,
// resolved next to the production package.
func dshPluginPath() (string, error) {
	root, err := dshHarnessRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "runtime", "langgraph", "formal_gates_harness", "native_dsh.mjs"), nil
}

// makeDSHPatch writes profile.json: the harness-native plugin (read_only adds
// sandboxPolicy to its inject list) and the ACP overlay; a probe additionally
// disables session-title-llm. The read-only tool surface itself is enforced by
// the unchanged native_dsh.mjs inside the host - there is no second permission
// engine here.
func makeDSHPatch(directory string, readOnly bool, probe bool) (string, error) {
	plugin, err := dshPluginPath()
	if err != nil {
		return "", err
	}
	inject := []any{"tools"}
	if readOnly {
		inject = append(inject, "sandboxPolicy")
	}
	patch := []any{
		map[string]any{"insert": []any{map[string]any{
			"id":     "harness-native",
			"name":   plugin,
			"inject": inject,
			"config": map[string]any{"directory": directory, "read_only": readOnly},
		}}},
		map[string]any{"id": "acp", "inject": []any{"acpAppStartup", "harnessNativeReady"}},
	}
	if probe {
		patch = append(patch, map[string]any{"id": "session-title-llm", "disabled": true})
	}
	path := filepath.Join(directory, "profile.json")
	if err := writeAtomicJSON(path, patch); err != nil {
		return "", err
	}
	return path, nil
}

// observeNative turns committed native turn evidence into the bound source
// receipt (native_dsh.observe). The events file is streamed line by line: only
// one decoded event is alive at a time.
func observeNative(directory string, identity any) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	turn := getOf(source, "turn")
	// The source must belong to this session, and the turn must be the host's
	// integer turn counter.
	if !valuesEqual(getOf(source, "sessionId"), identity) || !isIntegerLiteral(turn) {
		return nil, &blockedError{message: "DSH_BINDING: native source does not match the session"}
	}
	answer := ""
	ended := false
	var usage []any
	err = forEachSessionEvent(directory, identity, func(record map[string]any) error {
		event, err := mustEntry(record, "event")
		if err != nil {
			return err
		}
		eventType, err := mustEntry(event, "type")
		if err != nil {
			return err
		}
		data := objectOrEmpty(getOrOf(event, "data", map[string]any{}))
		if valuesEqual(eventType, "compaction/summary") {
			return nil
		}
		if !valuesEqual(data["turn"], turn) {
			return nil
		}
		switch {
		case valuesEqual(eventType, "assistant/message"):
			if ended {
				return &blockedError{message: "DSH_RESULT: message after turn ended"}
			}
			// A message's usage counts by presence: an object, empty or not.
			if eventUsage, isObject := getOf(data, "usage").(map[string]any); isObject {
				usage = append(usage, map[string]any{
					"turn":  turn,
					"step":  getOf(data, "step"),
					"event": "assistant/message",
					"usage": eventUsage,
				})
			}
			var text strings.Builder
			for _, part := range listOf(getOf(objectOrEmpty(getOrOf(data, "message", map[string]any{})), "content")) {
				if valuesEqual(getOf(part, "type"), "text") {
					if segment, isString := getOf(part, "text").(string); isString {
						text.WriteString(segment)
					}
				}
			}
			answer = text.String()
		case valuesEqual(eventType, "assistant/attempt"):
			// Preserve native usage, never infer or fill a missing counter with
			// zero: an attempt counts only when its usage object has entries.
			if attemptUsage, isObject := getOf(data, "usage").(map[string]any); isObject && len(attemptUsage) > 0 {
				usage = append(usage, map[string]any{
					"turn":  turn,
					"step":  getOf(data, "step"),
					"event": "assistant/attempt",
					"usage": attemptUsage,
				})
			}
		case valuesEqual(eventType, "turn/end"):
			reason := getOf(data, "reason")
			kind := getOf(objectOrEmpty(getOrOf(data, "reason", map[string]any{})), "kind")
			if ended || !valuesEqual(kind, "completed") {
				return &blockedError{message: "DSH_RESULT: turn was not completed: " + textOf(reason)}
			}
			ended = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !ended {
		return nil, &blockedError{message: "DSH_RESULT: committed turn/end evidence is missing"}
	}
	// Keep original bytes; reconciliation never rewrites the native evidence.
	if err := writeNativeAnswer(directory, answer, "DSH"); err != nil {
		return nil, err
	}
	var usageEvidence any
	if len(usage) > 0 {
		usageEvidence = usage
	}
	return evidenceSource(directory, "dsh", identity, map[string]any{
		"native_turn":    turn,
		"transcript":     getOrOf(source, "transcriptPath", ""),
		"usage_evidence": usageEvidence,
	})
}

// dshReconcile recovers only a completed native turn whose result framing
// was refused. Original process
// exit/error records remain unchanged; no host is started and no model is
// called; the caller must still accept the recovered business result.
func dshReconcile(directory string, process any) (map[string]any, error) {
	legacyError := "RuntimeBlocked: DSH_RESULT: final message is not a JSON result object"
	eligible := func(original string) bool {
		return original == legacyError || strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:")
	}
	recover := func() (map[string]any, error) {
		identity, err := readJSONFile(filepath.Join(directory, "identity.json"))
		if err != nil {
			return nil, err
		}
		request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			return nil, err
		}
		workspace, err := mustEntry(identity, "workspace")
		if err != nil {
			return nil, err
		}
		requestWorkspace, err := mustEntry(request, "workspace")
		if err != nil {
			return nil, err
		}
		if !valuesEqual(workspace, requestWorkspace) {
			return nil, &blockedError{message: "DSH_RECONCILE: workspace changed"}
		}
		selection := objectOrEmpty(getOrOf(identity, "selection", map[string]any{}))
		for _, option := range []string{"model", "reasoning_effort"} {
			requested, _ := getOrOf(request, option, "").(string)
			if requested == "" {
				continue
			}
			selected, _ := selection[option].(string)
			if selected != requested {
				return nil, &blockedError{message: "DSH_RECONCILE: original model selection differs"}
			}
		}
		source, err := observeNative(directory, getOf(identity, "identity"))
		if err != nil {
			return nil, err
		}
		source["selection"] = getOrOf(identity, "selection", map[string]any{})
		return source, nil
	}
	return reconcileCompletedOutput(directory, process, "DSH", eligible, recover)
}

// configuredIs reports whether the selection currently holds the requested
// value for one option; selection values are strings or null.
func configuredIs(configured map[string]any, option, requested string) bool {
	current, _ := configured[option].(string)
	return current == requested
}

// dshExecute runs DSH's ACP sequence for one request (native_dsh.execute).
func dshExecute(request any, directory string, probe bool) (result map[string]any, err error) {
	if mkdirErr := os.MkdirAll(directory, 0o755); mkdirErr != nil {
		return nil, mkdirErr
	}
	// A binding with entries is used as captured; an absent or empty one means
	// nothing was captured yet and the provider is bound now.
	binding := objectOrEmpty(getOf(request, "binding"))
	var executable string
	if len(binding) > 0 {
		launch, entryErr := mustEntry(binding, "launch")
		if entryErr != nil {
			return nil, entryErr
		}
		executableValue, entryErr := mustEntry(launch, "executable")
		if entryErr != nil {
			return nil, entryErr
		}
		executable = textOf(executableValue)
	} else {
		captured, captureErr := dshCaptureBinding()
		if captureErr != nil {
			return nil, captureErr
		}
		executable = textOf(getOf(getOf(captured, "launch"), "executable"))
	}
	workspaceValue, entryErr := mustEntry(request, "workspace")
	if entryErr != nil {
		return nil, entryErr
	}
	workspace := resolveNativePath(textOf(workspaceValue))
	readOnlyValue, entryErr := mustEntry(request, "read_only")
	if entryErr != nil {
		return nil, entryErr
	}
	readOnly, isBool := readOnlyValue.(bool)
	if !isBool {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	patch, err := makeDSHPatch(directory, readOnly, probe)
	if err != nil {
		return nil, err
	}
	client, err := newDSHClient([]string{executable, "--profile", "acp", "--patch", patch}, workspace, directory)
	if err != nil {
		return nil, err
	}
	identity := textOf(getOrOf(request, "identity", ""))
	attached := false
	var configured map[string]any
	defer func() {
		if attached {
			closeNativeSession(client, identity)
		}
		client.close()
	}()
	run := func() (map[string]any, error) {
		initialize, err := client.request("initialize",
			map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
		if err != nil {
			return nil, err
		}
		capabilities := objectOrEmpty(getOf(
			objectOrEmpty(getOf(initialize, "agentCapabilities")), "sessionCapabilities"))
		_, hasResume := capabilities["resume"]
		_, hasClose := capabilities["close"]
		if !valuesEqual(getOf(initialize, "protocolVersion"), 1) || !hasResume || !hasClose {
			return nil, &blockedError{message: "DSH_CAPABILITY: ACP v1 resume/close required"}
		}
		// ACP requires the additional server list. DSH mounts it in the agent
		// scope; an empty list does not remove servers from the native profile.
		params := map[string]any{"cwd": workspace, "mcpServers": []any{}}
		method := "session/new"
		if identity != "" {
			params["sessionId"] = identity
			method = "session/resume"
		}
		created, err := client.request(method, params)
		if err != nil {
			return nil, err
		}
		observed, _ := getOrOf(created, "sessionId", any(identity)).(string)
		if observed == "" || (identity != "" && observed != identity) {
			return nil, &blockedError{message: "DSH_BINDING: missing or changed identity"}
		}
		identity = observed
		attached = true
		configured, err = dshSelection(created)
		if err != nil {
			return nil, err
		}
		if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
			"identity": identity, "workspace": workspace, "selection": configured, "acknowledgement": created}); err != nil {
			return nil, err
		}
		for _, name := range []string{"model", "reasoning_effort"} {
			requested, _ := getOrOf(request, name, "").(string)
			if requested == "" || configuredIs(configured, name, requested) {
				continue
			}
			changed, err := client.request("session/set_config_option",
				map[string]any{"sessionId": identity, "configId": name, "value": requested})
			if err != nil {
				return nil, err
			}
			updated, err := dshSelection(changed)
			if err != nil {
				return nil, err
			}
			for key, value := range updated {
				configured[key] = value
			}
			if !configuredIs(configured, name, requested) {
				return nil, &blockedError{
					message: "DSH_CONFIG: " + name + " selection was not applied",
				}
			}
		}
		if probe {
			if _, err := client.request("session/close", map[string]any{"sessionId": identity}); err != nil {
				return nil, err
			}
			attached = false
			return map[string]any{
				"provider":     "dsh",
				"executable":   resolveNativePath(executable),
				"capabilities": capabilities,
				"selection":    configured,
				"model_calls":  0,
			}, nil
		}
		promptValue, entryErr := mustEntry(request, "prompt")
		if entryErr != nil {
			return nil, entryErr
		}
		promptText, isString := promptValue.(string)
		if !isString {
			return nil, fmt.Errorf("prompt must be a string")
		}
		// The reference concatenation evaluates request['prompt'] then
		// request['schema'] left to right, so a missing key is its own error,
		// never a silently null schema dump. The schema is embedded in its
		// canonical JSON form (registered bounded difference: value-equal to
		// the reference's default-separator rendering).
		schemaValue, entryErr := mustEntry(request, "schema")
		if entryErr != nil {
			return nil, entryErr
		}
		schema, dumpErr := CanonicalJSON(schemaValue)
		if dumpErr != nil {
			return nil, dumpErr
		}
		prompt := promptText + "\nReturn ONLY one JSON object matching this result schema:\n" + string(schema)
		response, err := client.request("session/prompt",
			map[string]any{"sessionId": identity,
				"prompt": []any{map[string]any{"type": "text", "text": prompt}}},
			nativeUnbounded)
		if err != nil {
			return nil, err
		}
		if !valuesEqual(getOf(response, "stopReason"), "end_turn") {
			return nil, &blockedError{message: "DSH_STOP: " + textOf(getOf(response, "stopReason"))}
		}
		// close flushes the native source. It does not delete persisted session
		// history.
		if _, err := client.request("session/close", map[string]any{"sessionId": identity}); err != nil {
			return nil, err
		}
		attached = false
		source, err := observeNative(directory, identity)
		if err != nil {
			return nil, err
		}
		source["selection"] = configured
		if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
			return nil, err
		}
		return source, nil
	}
	return run()
}

// dshCaptureBinding binds dsh to the resolved binary it will execute
// (native_dsh.capture_binding): PATH lookup, symlink resolution, then the
// shared launch/files proof shape.
func dshCaptureBinding() (map[string]any, error) {
	executable, err := exec.LookPath("dsh")
	if err != nil {
		return nil, &blockedError{message: "DSH_UNAVAILABLE: dsh is not on PATH"}
	}
	resolved := resolveNativePath(executable)
	return buildCaptureBinding("dsh", map[string]any{"executable": resolved})
}

// dshValidateBinding is the registry's validate_binding: the provider must
// match and every bound file proof must still hold.
func dshValidateBinding(binding any) error {
	return validateNativeBinding(binding, "dsh")
}

func dshVerifyStart(_ any, acknowledgement any, identity string) error {
	if getOf(acknowledgement, "sessionId") != identity {
		return fmt.Errorf("native session acknowledgement differs")
	}
	return nil
}

// dshVerifySource is the registry's verify_source: provider/identity must
// match, the proof set must be exactly the four evidence files, and each proof
// must still hold against the directory.
func dshVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "dsh")
}

func sameKeySet(object map[string]any, names []string) bool {
	if len(object) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := object[name]; !ok {
			return false
		}
	}
	return true
}

// --- the narrow entry command --------------------------------------------------

// RunNative is `formal-gates harness native <job-directory>`: one native
// job, one process. It reads
// native-request.json, dispatches by provider, prints the result JSON to
// stdout as one canonical line, and on ANY execution error writes
// native-error.json (canonical), echoes the same text to stderr and returns 1.
func RunNative(directory string, errout io.Writer, admissions ...ExecutionAdmission) int {
	resolved := resolveNativePath(directory)
	result, err := runNativeJob(resolved, admissions...)
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	encoded, err := CanonicalJSON(result)
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(encoded))
	return 0
}

func runNativeJob(directory string, admissions ...ExecutionAdmission) (map[string]any, error) {
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	object, isObject := request.(map[string]any)
	if !isObject {
		return nil, fmt.Errorf("native-request.json must contain a JSON object")
	}
	if _, managed := object["engine"]; managed {
		if source, err := readJSONFile(filepath.Join(directory, "source.json")); err == nil {
			provider, ok := getOf(source, "provider").(string)
			if !ok {
				return nil, fmt.Errorf("native source provider is missing")
			}
			impl, err := nativeImplementationFor(provider)
			if err != nil {
				return nil, err
			}
			if err := impl.verifySource(directory, source); err != nil {
				return nil, err
			}
			return source.(map[string]any), nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(directory, "exit.json")); err == nil {
			provider, _ := object["provider"].(string)
			return NativeJobSource(provider, directory)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	release, err := admitExecution(directory, object, admissions)
	if err != nil {
		return nil, err
	}
	if release != nil {
		defer func() {
			if release != nil {
				release()
			}
		}()
	}
	providerValue, present := entryOf(request, "provider")
	if !present {
		return nil, fmt.Errorf("missing JSON field %q", "provider")
	}
	provider, isString := providerValue.(string)
	if !isString {
		return nil, fmt.Errorf("provider must be a string")
	}
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, err
	}
	// The registry's execute wrapper: a captured binding is validated before
	// the implementation runs (NativeAdapter.execute). An empty binding
	// object is nothing captured, so there is nothing to validate.
	if binding := getOf(request, "binding"); binding != nil {
		object, isObject := binding.(map[string]any)
		if !isObject {
			return nil, fmt.Errorf("binding must be a JSON object")
		}
		if len(object) > 0 {
			if err := impl.validateBinding(object); err != nil {
				return nil, err
			}
		}
	}
	if release != nil {
		if err := prepareNativeInvocation(directory, object, impl); err != nil {
			return nil, err
		}
		release()
		release = nil
	}
	source, err := impl.execute(request, directory, getOf(request, "probe") == true)
	if _, managed := object["engine"]; managed && getOf(request, "probe") != true && err != nil {
		return nil, &nativeInvocationError{err}
	}
	return source, err
}

// writeNativeError lands the failure record and echoes it to stderr.
// SessionUnavailable carries its four extra keys with the host's original
// code value (null included).
func writeNativeError(directory string, err error, errout io.Writer) {
	text := nativeErrorText(err)
	detail := map[string]any{"error": text}
	if invocation, ok := err.(*nativeInvocationError); ok {
		detail["invoked"] = true
		detail["failure_class"] = invocation.failureClass()
		err = invocation.err
	}
	if unavailable, ok := err.(*sessionUnavailableError); ok {
		method := unavailable.method
		if method == "" {
			method = "session/resume"
		}
		detail["error_kind"] = "SESSION_UNAVAILABLE"
		detail["identity"] = unavailable.identity
		detail["method"] = method
		detail["native_code"] = unavailable.code
	}
	if writeErr := writeAtomicJSON(filepath.Join(directory, "native-error.json"), detail); writeErr != nil {
		fmt.Fprintf(errout, "%s\n", text)
		fmt.Fprintf(errout, "cannot write native-error.json: %v\n", writeErr)
		return
	}
	fmt.Fprintln(errout, text)
}

// NativeBinding uses the existing host registry's capture and proof validation.
func NativeBinding(provider string) (map[string]any, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, err
	}
	binding, err := impl.capture()
	if err != nil {
		return nil, err
	}
	if err := impl.validateBinding(binding); err != nil {
		return nil, err
	}
	return binding, nil
}

type nativeInvocationError struct{ err error }

func (e *nativeInvocationError) Error() string { return nativeErrorText(e.err) }
func (e *nativeInvocationError) Unwrap() error { return e.err }
func (e *nativeInvocationError) failureClass() authoring.FailureClass {
	switch e.err.(type) {
	case *bindingError:
		return authoring.FailureInvariantViolation
	case *blockedError, *sessionUnavailableError:
		return authoring.FailureUserActionRequired
	default:
		return authoring.FailureTransientEngine
	}
}

// --- the four native bridge operations -----------------------------------------

// nativeRefusal is a typed business refusal of a native bridge operation. It
// travels inside the operation's value as {"ok": false, "kind", "refusal"},
// exactly like the verify-files convention, so the Python caller's refuse()
// raises the same exception family the reference implementation raised: kind
// "binding" maps to the HarnessError family, anything else to RuntimeBlocked.
type nativeRefusal struct {
	kind    string
	refusal string
}

func (e *nativeRefusal) Error() string { return e.refusal }

// asNativeRefusal converts a native typed error into a bridge refusal carrying
// the exception family, mirroring Python's refuse(): the refusal text is the
// bare message (without the family prefix that only the native-error.json
// record adds), the RuntimeBlocked family (SessionUnavailable included) keeps
// kind "", the HarnessError family reports kind "binding". Anything else (a
// malformed payload leaking through) stays a bridge failure.
func asNativeRefusal(err error) error {
	if err == nil {
		return nil
	}
	switch typed := err.(type) {
	case *sessionUnavailableError:
		return &nativeRefusal{kind: "", refusal: typed.message}
	case *blockedError:
		return &nativeRefusal{kind: "", refusal: typed.message}
	case *bindingError:
		return &nativeRefusal{kind: "binding", refusal: typed.message}
	}
	return err
}

// nativeCapture is the native-capture operation: bind the provider to the
// exact binaries it will execute.
func nativeCapture(provider string) (any, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	binding, err := impl.capture()
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	return binding, nil
}

// nativeVerifyBinding is the native-verify-binding operation.
func nativeVerifyBinding(provider string, binding json.RawMessage) error {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return asNativeRefusal(err)
	}
	value, err := decodeNativeJSON(binding)
	if err != nil {
		return err
	}
	if err := impl.validateBinding(value); err != nil {
		return asNativeRefusal(err)
	}
	return nil
}

// nativeVerifySource is the native-verify-source operation.
func nativeVerifySource(provider string, directory string, source json.RawMessage) error {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return asNativeRefusal(err)
	}
	value, err := decodeNativeJSON(source)
	if err != nil {
		return err
	}
	if err := impl.verifySource(directory, value); err != nil {
		return asNativeRefusal(err)
	}
	return nil
}

// nativeReconcile is the native-reconcile operation: recover an eligible
// completed-output failure.
func nativeReconcile(provider string, directory string, process json.RawMessage) (any, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	value, err := decodeNativeJSON(process)
	if err != nil {
		return nil, err
	}
	source, err := impl.reconcile(directory, value)
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	return source, nil
}
