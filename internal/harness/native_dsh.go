package harness

// The DSH ACP adapter (the port of formal_gates_harness/native_dsh.py, whose
// verbatim copy lives in tests/reference_native_dsh.py for the differential),
// the provider dispatch behind `formal-gates harness native <dir>` and the
// four native bridge operations. One native job is one process: read
// native-request.json, run the ACP sequence, land every evidence file, print
// the result JSON to stdout, and on any execution error write
// native-error.json with the Python exception text and exit 1.

import (
	"encoding/json"
	"fmt"
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

// executionAdapters lists the providers this Go binary implements, in the
// order the unsupported-host refusal names them (the Python-side registry
// still lists zcode because its entry stays Python; the Go command never sees
// zcode jobs).
var executionAdapters = []string{"dsh"}

// dshDispatch refuses every provider this binary does not implement.
func dshDispatch(provider string) error {
	for _, name := range executionAdapters {
		if provider == name {
			return nil
		}
	}
	return &pyRuntimeBlocked{message: fmt.Sprintf(
		"UNSUPPORTED_NATIVE_HOST: %s; execution adapters: %s", provider, strings.Join(executionAdapters, ", "))}
}

// dshHooks is DSH's transport configuration: the host decides when a turn
// ends, so no request carries a total budget (bounded is false: the
// supervisor-style total deadline discipline must not leak in), and a dead
// reader is a transport error rather than a clean close.
func dshHooks() nativeHooks {
	return nativeHooks{
		transportPrefix: "DSH_TRANSPORT",
		requestPrefix:   "DSH_NATIVE",
		defaultTimeout:  dshStartupTimeout,
		bounded:         false,
		closedKey:       "transport_error",
		closedMessage:   "native ACP process closed its output",
		invalidMessage:  "non-JSON ACP output",
		identify:        func(c *nativeClient) any { return c.nextCounter() },
		envelope: func(value ordObj) ordObj {
			framed := dict("jsonrpc", "2.0")
			for _, key := range value.Keys() {
				item, _ := value.Get(key)
				framed.Set(key, item)
			}
			return framed
		},
		respond: func(method string, params any) (*nativeReply, error) {
			if method == "session/request_permission" {
				// A user decision belongs to the user; never approve or deny it
				// here, and never answer or retry.
				return nil, &pyRuntimeBlocked{
					message: "DSH_INTERACTION_REQUIRED: native permission needs a user decision; none was sent",
				}
			}
			return nil, nil
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
func dshClassifySessionUnavailable(method string, errValue any, params ordObj) error {
	message := "unknown error"
	if value, present := entryOf(errValue, "message"); present {
		message = pyStr(value)
	}
	missing := strings.HasPrefix(message, "Invalid params: session is not resumable:") ||
		strings.HasPrefix(message, "Session not found:")
	code := getOf(errValue, "code")
	if method == "session/resume" &&
		(pyEquals(code, -32602) || pyEquals(code, -32004)) && missing {
		return &pySessionUnavailable{
			pyRuntimeBlocked: pyRuntimeBlocked{message: "DSH_SESSION_UNAVAILABLE: " + message},
			identity:         getOrOf(params, "sessionId", ""),
			method:           "session/resume",
			code:             code,
		}
	}
	return nil
}

// dshSelection maps configOptions to {id: currentValue}, a missing
// currentValue becoming null.
func dshSelection(raw any) (ordObj, error) {
	out := dict()
	for _, option := range asList(getOf(raw, "configOptions")) {
		id, err := mustEntry(option, "id")
		if err != nil {
			return ordObj{}, err
		}
		out.Set(pyStr(id), getOf(option, "currentValue"))
	}
	return out, nil
}

func asList(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
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
	return "", &pyHarnessError{message: "harness runtime not found; use scripts/harness setup"}
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
func makeDSHPatch(directory string, readOnly any, probe bool) (string, error) {
	plugin, err := dshPluginPath()
	if err != nil {
		return "", err
	}
	inject := []any{"tools"}
	if pyTruthy(readOnly) {
		inject = append(inject, "sandboxPolicy")
	}
	patch := []any{
		dict("insert", []any{dict(
			"id", "harness-native",
			"name", plugin,
			"inject", inject,
			"config", dict("directory", directory, "read_only", readOnly),
		)}),
		dict("id", "acp", "inject", []any{"acpAppStartup", "harnessNativeReady"}),
	}
	if probe {
		patch = append(patch, dict("id", "session-title-llm", "disabled", true))
	}
	path := filepath.Join(directory, "profile.json")
	if err := writeNativeJSON(path, patch); err != nil {
		return "", err
	}
	return path, nil
}

// observeNative turns committed native turn evidence into the bound source
// receipt (native_dsh.observe).
func observeNative(directory string, identity any) (ordObj, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return ordObj{}, err
	}
	turn := getOf(source, "turn")
	if !pyEquals(getOf(source, "sessionId"), identity) || !pyIsInt(turn) {
		return ordObj{}, &pyRuntimeBlocked{message: "DSH_BINDING: native source does not match the session"}
	}
	answer := ""
	ended := false
	var usage []any
	events, err := sessionEvents(directory, identity)
	if err != nil {
		return ordObj{}, err
	}
	for _, record := range events {
		event, err := mustEntry(record, "event")
		if err != nil {
			return ordObj{}, err
		}
		eventType, err := mustEntry(event, "type")
		if err != nil {
			return ordObj{}, err
		}
		data := getOrOf(event, "data", dict())
		if pyEquals(eventType, "compaction/summary") {
			continue
		}
		if !pyEquals(getOf(data, "turn"), turn) {
			continue
		}
		switch pyStr(eventType) {
		case "assistant/message":
			if ended {
				return ordObj{}, &pyRuntimeBlocked{message: "DSH_RESULT: message after turn ended"}
			}
			if _, isDict := getOf(data, "usage").(ordObj); isDict {
				usage = append(usage, dict(
					"turn", turn,
					"step", getOf(data, "step"),
					"event", "assistant/message",
					"usage", getOf(data, "usage"),
				))
			}
			var text strings.Builder
			for _, part := range asList(getOf(getOrOf(data, "message", dict()), "content")) {
				if pyEquals(getOf(part, "type"), "text") {
					text.WriteString(pyStr(getOrOf(part, "text", "")))
				}
			}
			answer = text.String()
		case "assistant/attempt":
			// Preserve native usage, never infer or fill a missing counter with
			// zero (truthiness, not presence, gates an attempt's usage).
			if attemptUsage := getOf(data, "usage"); pyTruthy(attemptUsage) {
				usage = append(usage, dict(
					"turn", turn,
					"step", getOf(data, "step"),
					"event", "assistant/attempt",
					"usage", attemptUsage,
				))
			}
		case "turn/end":
			reason := getOf(data, "reason")
			kind := getOf(getOrOf(data, "reason", dict()), "kind")
			if ended || !pyEquals(kind, "completed") {
				return ordObj{}, &pyRuntimeBlocked{
					message: "DSH_RESULT: turn was not completed: " + pyStr(reason),
				}
			}
			ended = true
		}
	}
	if !ended {
		return ordObj{}, &pyRuntimeBlocked{message: "DSH_RESULT: committed turn/end evidence is missing"}
	}
	// Keep original bytes; reconciliation never rewrites the native evidence.
	if err := writeNativeAnswer(directory, answer, "DSH"); err != nil {
		return ordObj{}, err
	}
	var usageEvidence any
	if len(usage) > 0 {
		usageEvidence = usage
	}
	return evidenceSource(directory, "dsh", identity, dict(
		"native_turn", turn,
		"transcript", getOrOf(source, "transcriptPath", ""),
		"usage_evidence", usageEvidence,
	))
}

// dshReconcile recovers only a completed native turn rejected by the old
// formatting parser (native_dsh.reconcile_completed_output). Original process
// exit/error records remain unchanged; no host is started and no model is
// called; the caller must still accept the recovered business result.
func dshReconcile(directory string, process any) (ordObj, error) {
	legacyError := "RuntimeBlocked: DSH_RESULT: final message is not a JSON result object"
	eligible := func(original string) bool {
		return original == legacyError || strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:")
	}
	recover := func() (ordObj, error) {
		identity, err := readJSONFile(filepath.Join(directory, "identity.json"))
		if err != nil {
			return ordObj{}, err
		}
		request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
		if err != nil {
			return ordObj{}, err
		}
		workspace, err := mustEntry(identity, "workspace")
		if err != nil {
			return ordObj{}, err
		}
		requestWorkspace, err := mustEntry(request, "workspace")
		if err != nil {
			return ordObj{}, err
		}
		if !pyEquals(workspace, requestWorkspace) {
			return ordObj{}, &pyRuntimeBlocked{message: "DSH_RECONCILE: workspace changed"}
		}
		for _, option := range []string{"model", "reasoning_effort"} {
			requested := getOf(request, option)
			if pyTruthy(requested) &&
				!pyEquals(getOf(getOrOf(identity, "selection", dict()), option), requested) {
				return ordObj{}, &pyRuntimeBlocked{message: "DSH_RECONCILE: original model selection differs"}
			}
		}
		source, err := observeNative(directory, getOf(identity, "identity"))
		if err != nil {
			return ordObj{}, err
		}
		source.Set("selection", getOrOf(identity, "selection", dict()))
		return source, nil
	}
	return reconcileCompletedOutput(directory, process, "DSH", eligible, recover)
}

// dshExecute runs DSH's ACP sequence for one request (native_dsh.execute).
func dshExecute(request any, directory string, probe bool) (result ordObj, err error) {
	if mkdirErr := os.MkdirAll(directory, 0o755); mkdirErr != nil {
		return ordObj{}, mkdirErr
	}
	binding := getOf(request, "binding")
	var executable string
	if pyTruthy(binding) {
		launch, entryErr := mustEntry(binding, "launch")
		if entryErr != nil {
			return ordObj{}, entryErr
		}
		executableValue, entryErr := mustEntry(launch, "executable")
		if entryErr != nil {
			return ordObj{}, entryErr
		}
		executable = pyStr(executableValue)
	} else {
		captured, captureErr := dshCaptureBinding()
		if captureErr != nil {
			return ordObj{}, captureErr
		}
		launch := getOf(captured, "launch")
		executableValue := getOf(launch, "executable")
		executable = pyStr(executableValue)
	}
	workspaceValue, entryErr := mustEntry(request, "workspace")
	if entryErr != nil {
		return ordObj{}, entryErr
	}
	workspace := resolveNativePath(pyStr(workspaceValue))
	// request['read_only'] is a plain lookup in the reference implementation:
	// a request without it is the same KeyError class as a missing workspace.
	readOnly, entryErr := mustEntry(request, "read_only")
	if entryErr != nil {
		return ordObj{}, entryErr
	}
	patch, err := makeDSHPatch(directory, readOnly, probe)
	if err != nil {
		return ordObj{}, err
	}
	client, err := newDSHClient([]string{executable, "--profile", "acp", "--patch", patch}, workspace, directory)
	if err != nil {
		return ordObj{}, err
	}
	identity := pyStr(getOrOf(request, "identity", ""))
	attached := false
	var configured ordObj
	defer func() {
		if attached {
			closeNativeSession(client, identity)
		}
		client.close()
	}()
	run := func() (ordObj, error) {
		initialize, err := client.request("initialize",
			dict("protocolVersion", 1, "clientCapabilities", dict()))
		if err != nil {
			return ordObj{}, err
		}
		agentCapabilities := dshObject(getOf(initialize, "agentCapabilities"))
		capabilities := dshObject(getOf(agentCapabilities, "sessionCapabilities"))
		if !pyEquals(getOf(initialize, "protocolVersion"), 1) ||
			!capabilities.Has("resume") || !capabilities.Has("close") {
			return ordObj{}, &pyRuntimeBlocked{message: "DSH_CAPABILITY: ACP v1 resume/close required"}
		}
		// ACP requires the additional server list. DSH mounts it in the agent
		// scope; an empty list does not remove servers from the native profile.
		params := dict("cwd", workspace, "mcpServers", []any{})
		if identity != "" {
			params.Set("sessionId", identity) // appended last, like the dict insert
		}
		method := "session/new"
		if identity != "" {
			method = "session/resume"
		}
		created, err := client.request(method, params)
		if err != nil {
			return ordObj{}, err
		}
		observed := getOrOf(created, "sessionId", identity)
		if !pyTruthy(observed) || (identity != "" && !pyEquals(observed, identity)) {
			return ordObj{}, &pyRuntimeBlocked{message: "DSH_BINDING: missing or changed identity"}
		}
		identity = pyStr(observed)
		attached = true
		configured, err = dshSelection(created)
		if err != nil {
			return ordObj{}, err
		}
		if err := writeNativeJSON(filepath.Join(directory, "identity.json"), dict(
			"identity", identity, "workspace", workspace, "selection", configured)); err != nil {
			return ordObj{}, err
		}
		for _, name := range []string{"model", "reasoning_effort"} {
			requested := getOrOf(request, name, "")
			if pyTruthy(requested) && !pyEquals(getOf(configured, name), requested) {
				changed, err := client.request("session/set_config_option",
					dict("sessionId", identity, "configId", name, "value", requested))
				if err != nil {
					return ordObj{}, err
				}
				updated, err := dshSelection(changed)
				if err != nil {
					return ordObj{}, err
				}
				for _, key := range updated.Keys() {
					value, _ := updated.Get(key)
					configured.Set(key, value)
				}
				if !pyEquals(getOf(configured, name), requested) {
					return ordObj{}, &pyRuntimeBlocked{
						message: "DSH_CONFIG: " + name + " selection was not applied",
					}
				}
			}
		}
		if probe {
			if _, err := client.request("session/close", dict("sessionId", identity)); err != nil {
				return ordObj{}, err
			}
			attached = false
			return dict(
				"provider", "dsh",
				"executable", resolveNativePath(executable),
				"capabilities", capabilities,
				"selection", configured,
				"model_calls", 0,
			), nil
		}
		promptValue, entryErr := mustEntry(request, "prompt")
		if entryErr != nil {
			return ordObj{}, entryErr
		}
		// The reference concatenation evaluates request['prompt'] then
		// request['schema'] left to right, so a missing key is its own KeyError,
		// never a silently null schema dump. json.dumps of the decoded schema
		// (document order, Python separators, float repr) is byte-identical to
		// PyDumpsRaw of the file's raw subtree, because native-request.json is
		// written canonical by the one writer both sides share.
		schemaValue, entryErr := mustEntry(request, "schema")
		if entryErr != nil {
			return ordObj{}, entryErr
		}
		promptText, strErr := pyStrConcat(promptValue)
		if strErr != nil {
			return ordObj{}, strErr
		}
		schema, dumpErr := pyDumps(schemaValue)
		if dumpErr != nil {
			return ordObj{}, dumpErr
		}
		prompt := promptText + "\nReturn ONLY one JSON object matching this result schema:\n" + schema
		response, err := client.request("session/prompt",
			dict("sessionId", identity, "prompt", []any{dict("type", "text", "text", prompt)}),
			nativeUnbounded)
		if err != nil {
			return ordObj{}, err
		}
		if !pyEquals(getOf(response, "stopReason"), "end_turn") {
			return ordObj{}, &pyRuntimeBlocked{message: "DSH_STOP: " + pyStr(getOf(response, "stopReason"))}
		}
		// close flushes the native source. It does not delete persisted session
		// history.
		if _, err := client.request("session/close", dict("sessionId", identity)); err != nil {
			return ordObj{}, err
		}
		attached = false
		source, err := observeNative(directory, identity)
		if err != nil {
			return ordObj{}, err
		}
		source.Set("selection", configured)
		if err := writeNativeJSON(filepath.Join(directory, "source.json"), source); err != nil {
			return ordObj{}, err
		}
		return source, nil
	}
	return run()
}

// pyStrConcat is the str operand of Python's `+`: a non-string prompt is the
// TypeError the reference concatenation raises (unreachable through workers,
// which always write a string).
func pyStrConcat(v any) (string, error) {
	if text, ok := v.(string); ok {
		return text, nil
	}
	return "", &pyError{name: "TypeError", message: fmt.Sprintf(
		"can only concatenate str (not %s) to str", pyReprTypeName(v))}
}

// dshObject coerces a decoded value to an ordered object: absent or null
// becomes empty (the reference implementation's .get chains on live hosts
// always see objects here).
func dshObject(v any) ordObj {
	if object, ok := v.(ordObj); ok {
		return object
	}
	return dict()
}

// dshCaptureBinding binds dsh to the resolved binary it will execute
// (native_dsh.capture_binding): PATH lookup, symlink resolution, then the
// shared launch/files proof shape.
func dshCaptureBinding() (ordObj, error) {
	executable, err := exec.LookPath("dsh")
	if err != nil {
		return ordObj{}, &pyRuntimeBlocked{message: "DSH_UNAVAILABLE: dsh is not on PATH"}
	}
	resolved := resolveNativePath(executable)
	return buildCaptureBinding("dsh", dict("executable", resolved))
}

// dshValidateBinding is the registry's validate_binding: the provider must
// match and every bound file proof must still hold.
func dshValidateBinding(binding any) error {
	object, ok := binding.(ordObj)
	if !ok {
		return pyAttrError(binding, "get")
	}
	if !pyEquals(getOf(object, "provider"), "dsh") {
		return &pyHarnessError{message: "HOST_BINDING: provider differs"}
	}
	files := dshObject(getOrOf(object, "files", nil))
	for _, name := range files.Keys() {
		proof, err := fileProofOf(pyStr(name))
		if err != nil {
			return err
		}
		bound, _ := files.Get(name)
		if !pyDictEqual(proof, bound) {
			return &pyRuntimeBlocked{
				message: "HOST_CHANGED: bound native executable/script changed: " + pyStr(name),
			}
		}
	}
	return nil
}

// dshVerifySource is the registry's verify_source: provider/identity must
// match, the proof set must be exactly the four evidence files, and each proof
// must still hold against the directory.
func dshVerifySource(directory string, source any) error {
	object, ok := source.(ordObj)
	if !ok {
		return pyAttrError(source, "get")
	}
	if !pyEquals(getOf(object, "provider"), "dsh") || !pyTruthy(getOf(object, "identity")) {
		return &pyRuntimeBlocked{message: "NATIVE_BINDING: missing or different provider/identity"}
	}
	proofs := dshObject(getOrOf(object, "files", nil))
	if !sameKeySet(proofs, nativeSourceFiles) {
		return &pyRuntimeBlocked{message: "EVIDENCE_MISSING: native source proofs absent"}
	}
	for _, name := range proofs.Keys() {
		proof, err := fileProofOf(filepath.Join(directory, pyStr(name)))
		if err != nil {
			return err
		}
		bound, _ := proofs.Get(name)
		if !pyDictEqual(proof, bound) {
			return &pyRuntimeBlocked{message: "EVIDENCE_CHANGED: " + pyStr(name)}
		}
	}
	return nil
}

func sameKeySet(object ordObj, names []string) bool {
	if object.Len() != len(names) {
		return false
	}
	for _, name := range names {
		if !object.Has(name) {
			return false
		}
	}
	return true
}

// --- the narrow entry command --------------------------------------------------

// RunNative is `formal-gates harness native <job-directory>` (the port of
// native_entry.main): one native job, one process. It reads
// native-request.json, dispatches by provider, prints the result JSON to
// stdout as one canonical line (registered bounded difference: consumers parse
// by value), and on ANY execution error writes native-error.json (canonical)
// with the Python exception text, echoes the same text to stderr and returns
// 1.
func RunNative(directory string, errout io.Writer) int {
	resolved := resolveNativePath(directory)
	result, err := runNativeJob(resolved)
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	encoded, err := CanonicalJSON(canonicalValue(result))
	if err != nil {
		writeNativeError(resolved, err, errout)
		return 1
	}
	fmt.Fprintln(os.Stdout, string(encoded))
	return 0
}

func runNativeJob(directory string) (ordObj, error) {
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return ordObj{}, err
	}
	if _, isObject := request.(ordObj); !isObject {
		return ordObj{}, pySubscriptError(request)
	}
	providerValue, present := entryOf(request, "provider")
	if !present {
		return ordObj{}, &pyError{name: "KeyError", message: "'provider'"}
	}
	provider, isString := providerValue.(string)
	if !isString {
		// A non-string provider raises KeyError(<value>) in Python; its str()
		// is the repr.
		return ordObj{}, &pyError{name: "KeyError", message: pyReprOf(providerValue)}
	}
	if err := dshDispatch(provider); err != nil {
		return ordObj{}, err
	}
	// The registry's execute wrapper: a truthy binding is validated before the
	// implementation runs (NativeAdapter.execute).
	if binding := getOf(request, "binding"); pyTruthy(binding) {
		if err := dshValidateBinding(binding); err != nil {
			return ordObj{}, err
		}
	}
	return dshExecute(request, directory, pyTruthy(getOf(request, "probe")))
}

// writeNativeError lands the Python-shaped failure record and echoes it to
// stderr. SessionUnavailable carries its four extra keys with the host's
// original code value (null included).
func writeNativeError(directory string, err error, errout io.Writer) {
	text := nativeErrorText(err)
	detail := dict("error", text)
	if unavailable, ok := err.(*pySessionUnavailable); ok {
		method := unavailable.method
		if method == "" {
			method = "session/resume"
		}
		detail.Set("error_kind", "SESSION_UNAVAILABLE")
		detail.Set("identity", unavailable.identity)
		detail.Set("method", method)
		detail.Set("native_code", unavailable.code)
	}
	if writeErr := writeNativeJSON(filepath.Join(directory, "native-error.json"), detail); writeErr != nil {
		fmt.Fprintf(errout, "%s\n", text)
		fmt.Fprintf(errout, "cannot write native-error.json: %v\n", writeErr)
		return
	}
	fmt.Fprintln(errout, text)
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
// bare message (str(exc), without the class-name prefix that only the
// native-error.json record adds), the RuntimeBlocked family (SessionUnavailable
// included) keeps kind "", the HarnessError family reports kind "binding".
// Anything else (a Python TypeError/KeyError leaking through a malformed
// payload) stays a bridge failure.
func asNativeRefusal(err error) error {
	if err == nil {
		return nil
	}
	switch typed := err.(type) {
	case *pySessionUnavailable:
		return &nativeRefusal{kind: "", refusal: typed.message}
	case *pyRuntimeBlocked:
		return &nativeRefusal{kind: "", refusal: typed.message}
	case *pyHarnessError:
		return &nativeRefusal{kind: "binding", refusal: typed.message}
	}
	return err
}

// nativeCapture is the native-capture operation: bind the provider to the
// exact binaries it will execute.
func nativeCapture(provider string) (any, error) {
	if err := dshDispatch(provider); err != nil {
		return nil, asNativeRefusal(err)
	}
	binding, err := dshCaptureBinding()
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	return binding, nil
}

// nativeVerifyBinding is the native-verify-binding operation.
func nativeVerifyBinding(provider string, binding json.RawMessage) error {
	if err := dshDispatch(provider); err != nil {
		return asNativeRefusal(err)
	}
	value, err := decodeOrdered(binding)
	if err != nil {
		return err
	}
	if err := dshValidateBinding(value); err != nil {
		return asNativeRefusal(err)
	}
	return nil
}

// nativeVerifySource is the native-verify-source operation.
func nativeVerifySource(provider string, directory string, source json.RawMessage) error {
	if err := dshDispatch(provider); err != nil {
		return asNativeRefusal(err)
	}
	value, err := decodeOrdered(source)
	if err != nil {
		return err
	}
	if err := dshVerifySource(directory, value); err != nil {
		return asNativeRefusal(err)
	}
	return nil
}

// nativeReconcile is the native-reconcile operation: recover an eligible
// completed-output failure.
func nativeReconcile(provider string, directory string, process json.RawMessage) (any, error) {
	if err := dshDispatch(provider); err != nil {
		return nil, asNativeRefusal(err)
	}
	value, err := decodeOrdered(process)
	if err != nil {
		return nil, err
	}
	source, err := dshReconcile(directory, value)
	if err != nil {
		return nil, asNativeRefusal(err)
	}
	return source, nil
}
