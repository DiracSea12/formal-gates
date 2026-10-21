package harness

// The Devin adapter serves `formal-gates harness native <dir>` for the devin
// provider. The native host is the standalone agent binary's `acp`
// subcommand: one child process speaks newline-delimited JSON-RPC 2.0 over
// stdio and this adapter is the ACP client. A dispatch is initialize,
// authenticate when an API key resolves, session/new (session/load for a
// carried identity), session/set_mode when a
// read-only or permission-mapped request applies, session/set_config_option
// for a requested model or effort, and session/prompt with the issued prompt
// text; the turn ends when the host answers stopReason "end_turn".
//
// Turn evidence is the original protocol traffic: every inbound frame lands
// in native-events.jsonl verbatim through the received hook. Client-directed
// session/request_permission calls are answered inline with the host's own
// allow option under the host permission policy - they are never held for a
// user decision - and the answer lands in interaction-status.json.
//
// This devin build answers session/close and session/resume with "method not
// found" and session/load answers ACP-created sessions with -32016
// session_not_found, so no close call is issued and ACP sessions do not
// outlive their process; a carried identity is still attempted through
// session/load and the host's answer classifies the typed unavailable record.
// The Electron desktop shell (Devin.exe, devin-desktop) is never launched:
// the only child ever spawned is `<devinExecutable> acp`.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

// devinStartupTimeout bounds ACP startup only; a prompt waits for the host to
// finish its own turn. A variable only so tests can shorten it.
var devinStartupTimeout = 40 * time.Second

// Devin decides when a turn ends; the transport has no total execution budget.
// A dead reader is a transport error. The one client-directed request the
// host can send - session/request_permission - is answered inline: the host
// permission policy allows its own offered allow option, so no frame is ever
// held for a user decision.
func devinHooks() nativeHooks {
	return nativeHooks{
		transportPrefix: "DEVIN_TRANSPORT",
		requestPrefix:   "DEVIN_NATIVE",
		defaultTimeout:  devinStartupTimeout,
		bounded:         false,
		closedKey:       "transport_error",
		closedMessage:   "native ACP process closed its output",
		invalidMessage:  "non-JSON ACP output",
		identify:        func(c *nativeClient) any { return c.nextCounter() },
		envelope:        acpEnvelope,
		respond:         devinRespondPermission,
		classify:        devinClassifySessionUnavailable,
	}
}

// devinRespondPermission is the host-permission answer: pick the host's own
// allow_once option first, then any allow_* kind; a request offering no
// allow option is cancelled. Every answer is recorded with its outcome in
// interaction-status.json.
func devinRespondPermission(method string, params any) (*nativeReply, error) {
	if method != "session/request_permission" {
		return nil, nil
	}
	options := listOf(getOf(params, "options"))
	selected := func(option any) *nativeReply {
		return &nativeReply{
			payload: map[string]any{"result": map[string]any{"outcome": map[string]any{
				"outcome": "selected", "optionId": getOf(option, "optionId")}}},
			status: "permission-selected",
		}
	}
	for _, option := range options {
		if textOf(getOf(option, "kind")) == "allow_once" {
			return selected(option), nil
		}
	}
	for _, option := range options {
		if strings.HasPrefix(textOf(getOf(option, "kind")), "allow") {
			return selected(option), nil
		}
	}
	return &nativeReply{
		payload: map[string]any{"result": map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"}}},
		status: "permission-cancelled",
	}, nil
}

// devinClassifySessionUnavailable is the narrow Devin error classification:
// the host's own session_not_found answer (or a load method it does not
// implement) for a carried identity is the typed unavailable record; never
// treat a timeout or a model error as proof that a session was destroyed.
func devinClassifySessionUnavailable(method string, errValue any, params map[string]any) error {
	if method != "session/load" {
		return nil
	}
	code := getOf(errValue, "code")
	message := textOf(getOf(errValue, "message"))
	kind := textOf(getOf(getOf(errValue, "data"), "cognition.ai/errorKind"))
	missing := kind == "session_not_found" || strings.HasPrefix(message, "Session not found") ||
		valuesEqual(code, -32601)
	if missing {
		return &sessionUnavailableError{
			blockedError: blockedError{message: "DEVIN_SESSION_UNAVAILABLE: " + message},
			identity:     getOrOf(params, "sessionId", ""),
			method:       method,
			code:         code,
		}
	}
	return nil
}

// devinCapable is Devin's handshake gate over the host's initialize answer:
// ACP protocol version 1.
func devinCapable(initialize any) error {
	if !valuesEqual(getOf(initialize, "protocolVersion"), 1) {
		return &blockedError{message: "DEVIN_CAPABILITY: ACP protocol version 1 required"}
	}
	return nil
}

// devinAuthenticate is this host's extra handshake step: the ACP service
// refuses session/new until an authenticate request carries the account's
// API key. With no resolvable key the step is skipped and session/new
// returns the host's own authentication answer.
func devinAuthenticate(client *nativeClient) error {
	key := devinAPIKey()
	if key == "" {
		return nil
	}
	_, err := client.request("authenticate", map[string]any{
		"methodId": "devin-api-key", "meta": map[string]any{"api_key": key}})
	return err
}

// devinAPIKey resolves the authenticate credential: DEVIN_API_KEY first,
// then the windsurf_api_key line of the standalone CLI's credentials file.
func devinAPIKey() string {
	if key := os.Getenv("DEVIN_API_KEY"); key != "" {
		return key
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(dir, "devin", "credentials.toml"))
	if err != nil {
		return ""
	}
	return devinCredential(string(body), "windsurf_api_key")
}

// devinCredential reads one name's value out of simple key = value lines;
// surrounding quotes on the value are stripped.
func devinCredential(body, name string) string {
	for _, line := range strings.Split(body, "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != name {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return ""
}

func devinExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	dispatch, err := newACPDispatch(request, directory, devinCaptureBinding)
	if err != nil {
		return nil, err
	}
	if getOf(request, "selection_options") == true {
		return devinSelectionOptions(dispatch.executable, dispatch.workspace, dispatch.environment)
	}

	// The events transcript opens before the child does: the host may send
	// notifications ahead of the initialize answer, and every inbound frame
	// lands verbatim through the received hook.
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	var eventsErr error
	hooks := devinHooks()
	hooks.received = func(message any) {
		if eventsErr != nil {
			return
		}
		line, canonErr := CanonicalJSON(message)
		if canonErr != nil {
			eventsErr = canonErr
			return
		}
		if _, err := events.Write(append(line, '\n')); err != nil {
			eventsErr = err
		}
	}
	// ACP names a stdio server by name/command/args/env; the host's own
	// transport keys must not ride along. A probe mounts no server.
	servers := []any{}
	if !probe {
		server, err := nativeToolServer(directory)
		if err != nil {
			return nil, err
		}
		servers = []any{nativePickParams(server, "name", "command", "args", "env")}
	}
	session, err := dispatch.connect(acpSessionSpec{
		prefix:       "DEVIN",
		argv:         []string{dispatch.executable, "acp"},
		hooks:        hooks,
		resume:       "session/load",
		servers:      servers,
		capable:      devinCapable,
		authenticate: devinAuthenticate,
	})
	if err != nil {
		return nil, err
	}
	defer session.close()
	if err := session.open(); err != nil {
		return nil, err
	}
	if err := session.configure(); err != nil {
		return nil, err
	}
	if probe {
		return map[string]any{
			"provider":     "devin",
			"executable":   resolveNativePath(dispatch.executable),
			"capabilities": getOf(session.initialize, "agentCapabilities"),
			"selection":    session.configured,
			"model_calls":  0,
		}, nil
	}
	// The mode is applied before the first turn only: a read-only request
	// takes the host's own no-code-changes mode, an explicit
	// permission_mode is set verbatim, and any other request leaves the
	// session in its host default. The availableModes the session
	// reported validate the name when they are advertised; the host's
	// set_mode answer does not reject unknown modes.
	mode, _ := getOrOf(request, "permission_mode", "").(string)
	if dispatch.readOnly {
		mode = "ask"
	}
	if mode != "" {
		if advertised := nativeStringValues(getOf(getOf(session.created, "modes"), "availableModes"), "id"); len(advertised) > 0 {
			found := false
			for _, id := range advertised {
				if id == mode {
					found = true
					break
				}
			}
			if !found {
				return nil, &blockedError{
					message: "DEVIN_CONFIG: host does not offer session mode " + mode,
				}
			}
		}
		if _, err := session.client.request("session/set_mode",
			map[string]any{"sessionId": session.identity, "modeId": mode}); err != nil {
			return nil, err
		}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), map[string]any{
		"sessionId": session.identity, "workspace": dispatch.workspace, "selection": session.configured}); err != nil {
		return nil, err
	}
	response, err := session.turn(services)
	if err != nil {
		return nil, err
	}
	if eventsErr != nil {
		return nil, eventsErr
	}
	if err := session.endTurn(response); err != nil {
		return nil, err
	}
	source, err := devinObserve(directory, session.identity)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// devinObserve turns the recorded ACP traffic into the bound source receipt.
// The terminal fact is the host's own prompt answer: one result frame carrying
// stopReason, the session-bearing frames all bound to this session, and no
// second terminal result after it.
func devinObserve(directory string, identity any) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(source, "sessionId"), identity) {
		return nil, &blockedError{message: "DEVIN_BINDING: native source does not match the session"}
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(source, "workspace"), getOf(request, "workspace")) {
		return nil, &blockedError{message: "DEVIN_BINDING: workspace changed"}
	}
	if requested, _ := getOf(request, "identity").(string); requested != "" && requested != identity {
		return nil, &blockedError{message: "DEVIN_BINDING: resumed native session identity changed"}
	}
	var terminal map[string]any
	err = forEachNativeEvent(directory, func(value any) error {
		frame, ok := value.(map[string]any)
		if !ok {
			return &blockedError{message: "DEVIN_NATIVE: native message is not an object"}
		}
		if session, _ := getOf(getOf(frame, "params"), "sessionId").(string); session != "" && !valuesEqual(session, identity) {
			return &blockedError{message: "DEVIN_BINDING: mixed native session identities"}
		}
		if isLiveMethod(frame["method"]) {
			return nil
		}
		result := objectOrEmpty(getOf(frame, "result"))
		if _, hasStop := entryOf(result, "stopReason"); !hasStop {
			return nil
		}
		if terminal != nil {
			return &blockedError{message: "DEVIN_RESULT: result after terminal prompt answer"}
		}
		terminal = result
		return nil
	})
	if err != nil {
		return nil, err
	}
	if terminal == nil {
		return nil, &blockedError{message: "DEVIN_RESULT: terminal prompt result is missing"}
	}
	if !valuesEqual(terminal["stopReason"], "end_turn") {
		return nil, &blockedError{message: "DEVIN_RESULT: native turn was not completed: " + textOf(terminal["stopReason"])}
	}
	selection := getOf(source, "selection")
	for _, name := range []string{"model", "reasoning_effort"} {
		requested, _ := getOrOf(request, name, "").(string)
		if requested == "" {
			continue
		}
		selected, _ := getOf(selection, name).(string)
		if selected != requested {
			return nil, &blockedError{message: "DEVIN_BINDING: native model selection differs"}
		}
	}
	if err := writeSubmittedAnswer(directory); err != nil {
		return nil, err
	}
	var usage any
	if terminal["usage"] != nil {
		usage = []any{map[string]any{"event": "session/prompt", "usage": terminal["usage"]}}
	}
	return evidenceSource(directory, "devin", identity, map[string]any{
		"selection":      selection,
		"native_result":  terminal,
		"usage_evidence": usage,
		"transcript":     filepath.Join(directory, "native-events.jsonl"),
	})
}

// Recovery retains the original session, workspace and host selection.
func devinReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
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
			return nil, &blockedError{message: "DEVIN_RECONCILE: workspace changed"}
		}
		source, err := devinObserve(directory, getOf(identity, "identity"))
		if err != nil {
			return nil, err
		}
		source["selection"] = getOrOf(identity, "selection", map[string]any{})
		return source, nil
	}
	return recoverNativeSource(directory, process, "DEVIN", recover)
}

// devinSelectionOptions is the honest model catalogue: the CLI's own
// `models list --format json` output, never ACP config options and never an
// invented list. An unauthenticated account, a missing executable or an
// unparseable answer reports an empty catalogue whose source names the
// observed reason.
func devinSelectionOptions(executable, workspace string, environment map[string]string) (map[string]any, error) {
	catalogue := host.SelectionCatalogue{Source: "devin models list --format json"}
	cmd := exec.Command(executable, "models", "list", "--format", "json")
	cmd.Dir = workspace
	cmd.Env = nativeChildEnvironment(environment)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		catalogue.Source += ": " + reason
		return map[string]any{"selection_options": catalogue, "model_calls": 0}, nil
	}
	value, err := decodeNativeJSON(out)
	if err != nil {
		catalogue.Source += ": unparseable output"
		return map[string]any{"selection_options": catalogue, "model_calls": 0}, nil
	}
	seen := map[string]bool{}
	for _, model := range devinListedModels(value) {
		uid := nativeSelectionString(getOf(model, "model_uid"))
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		label := nativeSelectionString(getOf(model, "label"))
		if label == "" {
			label = uid
		}
		catalogue.Models = append(catalogue.Models, host.ModelOption{
			Value: uid,
			Label: label,
		})
	}
	if len(catalogue.Models) == 0 {
		catalogue.Source += ": no models reported"
	}
	return map[string]any{"selection_options": catalogue, "model_calls": 0}, nil
}

// devinListedModels collects every object carrying a model_uid string from
// the catalogue payload: the list answer is organized by model family, so the
// entries are found wherever the report puts them, never invented.
func devinListedModels(value any) []any {
	out := []any{}
	var walk func(v any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			if _, isString := typed["model_uid"].(string); isString {
				out = append(out, typed)
				return
			}
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(value)
	return out
}

func devinVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "devin")
}

var devinCaptureBinding = nativeCapture(devinResolvePaths, devinConfiguredBinding)

func devinResolvePaths(native, sources map[string]string) error {
	executable := settings.Values{Native: native}.NativeString("devinExecutable")
	path, executableSource, err := nativeExecutablePath(executable)
	if err != nil && executable == "devin" {
		// The desktop bundle ships the standalone agent at one fixed location
		// inside its own resources tree; the devin-desktop launcher on PATH is
		// only the anchor that names the install root, never a candidate.
		for _, name := range []string{"devin", "devin.exe"} {
			if candidate, _, ok := nativeAnchorLaunch([]string{"devin-desktop"}, "..", "resources", "app", "extensions", "windsurf", "devin", "bin", name); ok {
				path, executableSource, err = candidate, "desktop bundle", nil
				break
			}
		}
	}
	if err != nil {
		return &blockedError{message: "DEVIN_UNAVAILABLE: devin is not on PATH"}
	}
	recordNativePaths(native, sources,
		map[string]string{"devinExecutable": resolveNativePath(path)},
		map[string]string{"devinExecutable": executableSource})
	return nil
}

func devinConfiguredBinding(config settings.Values) (map[string]any, error) {
	executable := config.Native["devinExecutable"]
	if executable == "" {
		return nil, &blockedError{message: "DEVIN_CONFIG: frozen native paths required"}
	}
	binding, err := buildCaptureBinding("devin", map[string]any{"executable": executable})
	if err != nil {
		return nil, err
	}
	home, _, err := nativeUserHome("")
	if err != nil {
		return nil, err
	}
	binding["environment"] = map[string]any{"HOME": resolveNativePath(home)}
	if runtime.GOOS == "windows" {
		binding["environment"].(map[string]any)["USERPROFILE"] = resolveNativePath(home)
	}
	return binding, nil
}
