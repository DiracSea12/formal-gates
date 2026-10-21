package harness

// The DSH ACP adapter serves `formal-gates harness native <dir>`.
// One native job is one process: read
// native-request.json, run the ACP sequence, land every evidence file, print
// the result JSON to stdout as one canonical line, and on any execution error
// write native-error.json and exit 1.

import (
	"fmt"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

// DSH decides when a turn ends; the transport has no total execution budget.
// A dead reader is a transport error. Permission requests wait for the user.
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
		envelope:        acpEnvelope,
		classify:        dshClassifySessionUnavailable,
	}
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

func dshSelectionOptions(raw any) host.SelectionCatalogue {
	catalogue := host.SelectionCatalogue{Source: "ACP configOptions"}
	for _, option := range listOf(getOf(raw, "configOptions")) {
		switch getOf(option, "id") {
		case "model":
			catalogue.Current.Model = nativeSelectionString(getOf(option, "currentValue"))
			var collect func(any)
			collect = func(options any) {
				for _, entry := range listOf(options) {
					if nested, ok := entryOf(entry, "options"); ok {
						collect(nested)
					} else if value := nativeSelectionString(getOf(entry, "value")); value != "" {
						catalogue.Models = append(catalogue.Models, host.ModelOption{Value: value, Label: nativeSelectionString(getOf(entry, "name"))})
					}
				}
			}
			collect(getOf(option, "options"))
		case "reasoning_effort":
			catalogue.Current.Effort = nativeSelectionString(getOf(option, "currentValue"))
		}
	}
	catalogue.Default = catalogue.Current
	for i := range catalogue.Models {
		if catalogue.Models[i].Value != catalogue.Current.Model {
			continue
		}
		catalogue.Models[i].Efforts = []string{}
		catalogue.Models[i].DefaultEffort = catalogue.Current.Effort
		for _, option := range listOf(getOf(raw, "configOptions")) {
			if getOf(option, "id") == "reasoning_effort" {
				catalogue.Models[i].Efforts = nativeStringValues(getOf(option, "options"), "value")
			}
		}
	}
	return catalogue
}

// nativePackageRoot locates the package supplying the native host assets.
func nativePackageRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

// dshPluginPath resolves the package-owned DSH native plugin.
func dshPluginPath() (string, error) {
	root, err := nativePackageRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "internal", "harness", "native_dsh.mjs"), nil
}

// makeDSHPatch writes profile.json: the harness-native plugin (read_only adds
// sandboxPolicy to its inject list) and the ACP overlay; a probe additionally
// disables session-title-llm. The native hook registers the original-request
// workspace tool and restricts builtins; engine admission owns every operation.
func makeDSHPatch(directory string, readOnly bool, probe bool, services ...ExecutionServices) (string, error) {
	var schemas []any
	if !probe {
		tools, err := newNativeWorkspaceTools(directory, services...)
		if err != nil {
			return "", err
		}
		schemas = tools.schemas()
	}
	plugin, err := dshPluginPath()
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
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
			"config": map[string]any{"directory": directory, "read_only": readOnly, "executable": executable, "tools": schemas, "managed_tools": !probe},
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
// receipt. The events file is streamed line by line: only
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
	if err := writeSubmittedAnswer(directory); err != nil {
		return nil, err
	}
	var usageEvidence any
	if len(usage) > 0 {
		usageEvidence = usage
	}
	return evidenceSource(directory, "dsh", identity, map[string]any{
		"native_turn":    turn,
		"transcript":     filepath.Join(directory, "native-events.jsonl"),
		"usage_evidence": usageEvidence,
	})
}

// Recovery retains the original session, workspace and host selection.
func dshReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
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
	return recoverNativeSource(directory, process, "DSH", recover)
}

// dshSessionCapabilities is the host's advertised session-capability object.
func dshSessionCapabilities(initialize any) map[string]any {
	return objectOrEmpty(getOf(objectOrEmpty(getOf(initialize, "agentCapabilities")), "sessionCapabilities"))
}

// dshCapable is DSH's handshake gate over the host's initialize answer: this
// adapter's session lifecycle relies on the host's resume and close
// capabilities under ACP protocol version 1.
func dshCapable(initialize any) error {
	capabilities := dshSessionCapabilities(initialize)
	_, hasResume := capabilities["resume"]
	_, hasClose := capabilities["close"]
	if !valuesEqual(getOf(initialize, "protocolVersion"), 1) || !hasResume || !hasClose {
		return &blockedError{message: "DSH_CAPABILITY: ACP v1 resume/close required"}
	}
	return nil
}

// dshExecute runs DSH’s ACP sequence for one request.
func dshExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	dispatch, err := newACPDispatch(request, directory, dshCaptureBinding)
	if err != nil {
		return nil, err
	}
	patch, err := makeDSHPatch(directory, dispatch.readOnly, probe, services...)
	if err != nil {
		return nil, err
	}
	session, err := dispatch.connect(acpSessionSpec{
		prefix: "DSH",
		argv:   dshArgv(objectOrEmpty(getOf(dispatch.binding, "launch")), patch),
		hooks:  dshHooks(),
		resume: "session/resume",
		// The tool surface mounts in the agent scope through the profile
		// patch; the empty list removes nothing from the native profile.
		servers:      []any{},
		sessionClose: true,
		capable:      dshCapable,
	})
	if err != nil {
		return nil, err
	}
	defer session.close()
	if err := session.open(); err != nil {
		return nil, err
	}
	if getOf(request, "selection_options") == true {
		catalogue := dshSelectionOptions(session.created)
		target := nativeSelectionString(getOf(request, "selection_model"))
		for _, option := range catalogue.Models {
			if option.Value != target || target == catalogue.Current.Model {
				continue
			}
			changed, err := session.client.request("session/set_config_option", map[string]any{"sessionId": session.identity, "configId": "model", "value": target}, nativeUnbounded)
			if err != nil {
				return nil, err
			}
			updated := dshSelectionOptions(changed)
			if updated.Current.Model != target {
				return nil, fmt.Errorf("DSH_CONFIG: queried model selection was not applied")
			}
			catalogue.Models = updated.Models
			break
		}
		if err := session.detach(nativeUnbounded); err != nil {
			return nil, err
		}
		return map[string]any{"selection_options": catalogue, "model_calls": 0}, nil
	}
	if err := session.configure(); err != nil {
		return nil, err
	}
	if probe {
		if err := session.detach(); err != nil {
			return nil, err
		}
		return map[string]any{
			"provider":     "dsh",
			"executable":   resolveNativePath(dispatch.executable),
			"capabilities": dshSessionCapabilities(session.initialize),
			"selection":    session.configured,
			"model_calls":  0,
		}, nil
	}
	response, err := session.turn(services)
	if err != nil {
		return nil, err
	}
	if err := session.endTurn(response); err != nil {
		return nil, err
	}
	// close flushes the native source. It does not delete persisted session
	// history.
	if err := session.detach(); err != nil {
		return nil, err
	}
	source, err := observeNative(directory, session.identity)
	if err != nil {
		return nil, err
	}
	source["selection"] = session.configured
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

var dshCaptureBinding = nativeCapture(dshResolvePaths, dshConfiguredBinding)

// dshDesktopInstall names the installed desktop shell's executable and the
// resources directory its harness entry ships under; a variable so tests can
// point it at a fixture install. Linux has no desktop install.
var dshDesktopInstall = func() (exe, resources string) {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			exe = filepath.Join(local, "Programs", "DSH Desktop", "DSH Desktop.exe")
			return exe, filepath.Join(filepath.Dir(exe), "resources")
		}
	case "darwin":
		exe = filepath.Join("/Applications", "DSH Desktop.app", "Contents", "MacOS", "DSH Desktop")
		return exe, filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources")
	}
	return "", ""
}

// dshArgv is the session spawn argv: the bound launch, then — for the
// desktop-shell form — the bundled ACP app next to the harness entry in the
// resources tree, then the acp profile flags.
func dshArgv(launch map[string]any, patch string) []string {
	argv := nativeArgv(launch)
	if script, _ := getOf(launch, "script").(string); script != "" {
		argv = append(argv, filepath.Join(filepath.Dir(script), "app.asar", "node_modules", "@deepseek-ai", "dsh", "lib", "bin.js"))
	}
	return append(argv, "--profile", "acp", "--patch", patch)
}

func dshResolvePaths(native, sources map[string]string) error {
	executable := settings.Values{Native: native}.NativeString("dshExecutable")
	path, executableSource, err := nativeExecutablePath(executable)
	script := ""
	if err != nil && executable == "dsh" {
		// The desktop shell is another install form: it runs as node over its
		// bundled harness entry (ELECTRON_RUN_AS_NODE).
		if exe, resources := dshDesktopInstall(); exe != "" {
			entry := filepath.Join(resources, "harness-node-entry.mjs")
			if resolved, source, ok := nativeFixedLaunch(exe); ok && nativeRegularFile(entry) {
				path, script, executableSource, err = resolved, resolveNativePath(entry), source, nil
			}
		}
	}
	if err != nil {
		return &blockedError{message: "DSH_UNAVAILABLE: dsh is not on PATH"}
	}
	home, homeSource, err := nativeUserHome("")
	if err != nil {
		return err
	}
	home = resolveNativePath(home)
	nativeHome := native["dshConfigRoot"]
	rootSource := "DSH_HOME"
	if nativeHome == "" {
		nativeHome = os.Getenv("DSH_HOME")
		if strings.TrimSpace(nativeHome) == "" {
			nativeHome = filepath.Join(home, ".dsh")
			rootSource = "dshHome"
		}
		if nativeHome == "~" {
			nativeHome = home
		} else if strings.HasPrefix(nativeHome, "~/") || strings.HasPrefix(nativeHome, `~\`) {
			nativeHome = filepath.Join(home, nativeHome[2:])
		}
	}
	paths := map[string]string{"dshExecutable": resolveNativePath(path), "dshHome": home, "dshConfigRoot": resolveNativePath(nativeHome)}
	origins := map[string]string{"dshExecutable": executableSource, "dshHome": homeSource, "dshConfigRoot": rootSource}
	if script != "" {
		paths["dshScript"], origins["dshScript"] = script, "bundle"
	}
	recordNativePaths(native, sources, paths, origins)
	return nil
}

func dshConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable, home, nativeHome := config.Native["dshExecutable"], config.Native["dshHome"], config.Native["dshConfigRoot"]
	if executable == "" || home == "" || nativeHome == "" {
		return nil, &blockedError{message: "DSH_CONFIG: frozen native paths required"}
	}
	// A desktop-shell launch runs the Electron binary as node over the
	// bundled harness entry, so the binding names the script operand and the
	// official run-as-node switch.
	launch := map[string]any{"executable": executable}
	environment := map[string]any{"HOME": home, "DSH_HOME": nativeHome}
	if script := config.Native["dshScript"]; script != "" {
		launch["script"] = script
		environment["ELECTRON_RUN_AS_NODE"] = "1"
	}
	binding, err := buildCaptureBinding("dsh", launch)
	if err != nil {
		return nil, err
	}
	binding["environment"] = environment
	if runtime.GOOS == "windows" {
		binding["environment"].(map[string]any)["USERPROFILE"] = home
	}
	return binding, nil
}

// dshValidateBinding is the registry's validate_binding: the provider must
// match and every bound file proof must still hold.
func dshValidateBinding(binding any) error {
	return validateNativeBinding(binding, "dsh")
}

// dshVerifySource checks the bound provider, identity and required evidence
// files against the original directory.
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
func RunNative(directory string, errout io.Writer, services ...ExecutionServices) int {
	resolved := resolveNativePath(directory)
	result, err := runNativeJob(resolved, services...)
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	encoded, err := canonicalBody(result)
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(encoded))
	return 0
}

func runNativeJob(directory string, services ...ExecutionServices) (map[string]any, error) {
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
			return NativeJobSource(provider, directory, services...)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(directory, "exit.json")); err == nil {
			provider, _ := object["provider"].(string)
			return NativeJobSource(provider, directory, services...)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	release, err := admitExecution(directory, object, services)
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
	if object["engine"] != nil && (len(services) == 0 || services[0].ClaimMessage == nil || services[0].ReceiptMessage == nil) {
		return nil, fmt.Errorf("UNSUPPORTED_EXECUTION: current engine message services are unavailable")
	}
	prepared, err := queueNativeMessages(directory, object, services)
	if err != nil {
		return nil, err
	}
	source, err := impl.execute(prepared, directory, getOf(request, "probe") == true, services...)
	if messageErr := settleQueuedMessages(directory, object, err, services); messageErr != nil {
		return nil, messageErr
	}

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
func NativeBinding(provider string, configs ...settings.Values) (map[string]any, error) {
	impl, err := nativeImplementationFor(provider)
	if err != nil {
		return nil, err
	}
	var binding map[string]any
	if len(configs) != 0 && impl.captureConfigured != nil {
		binding, err = impl.captureConfigured(configs[0])
	} else {
		binding, err = impl.capture()
	}
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
