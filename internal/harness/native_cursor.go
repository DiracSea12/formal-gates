package harness

// Cursor owns its chat store and model selection. The adapter preserves its
// native stream and exit and observes those same originals after execution.
import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func cursorCaptureBinding() (map[string]any, error) {
	executable, err := exec.LookPath("cursor-agent")
	if err != nil {
		return nil, &blockedError{message: "CURSOR_UNAVAILABLE: cursor-agent is not on PATH"}
	}
	executable = resolveNativePath(executable)
	launch := map[string]any{"executable": executable}
	// The native wrapper executes these siblings and loads chunks relative to
	// index.js. Proof their bytes without relocating the host's runtime layout.
	for _, item := range []struct{ key, name string }{{"node", "node"}, {"script", "index.js"}} {
		path := filepath.Join(filepath.Dir(executable), item.name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			launch[item.key] = resolveNativePath(path)
		}
	}
	binding, err := buildCaptureBinding("cursor", launch)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config := os.Getenv("CURSOR_CONFIG_DIR")
	if config == "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			config = filepath.Join(xdg, "cursor")
		} else {
			config = filepath.Join(home, ".cursor")
		}
	}
	data := os.Getenv("CURSOR_DATA_DIR")
	if data == "" {
		data = filepath.Join(home, ".cursor")
	}
	cache := os.Getenv("NODE_COMPILE_CACHE")
	if cache == "" {
		if runtime.GOOS == "darwin" {
			cache = filepath.Join(home, "Library", "Caches", "cursor-compile-cache")
		} else {
			cache = filepath.Join(home, ".cache", "cursor-compile-cache")
		}
	}
	binding["environment"] = map[string]any{"HOME": resolveNativePath(home), "CURSOR_CONFIG_DIR": resolveNativePath(config), "CURSOR_DATA_DIR": resolveNativePath(data), "NODE_COMPILE_CACHE": resolveNativePath(cache)}
	return binding, nil
}
func cursorValidateBinding(binding any) error { return validateNativeBinding(binding, "cursor") }
func cursorVerifySource(directory string, source any) error {
	if err := verifyNativeSource(directory, source, "cursor"); err != nil {
		return err
	}
	observed, err := cursorObserve(directory, false)
	if err != nil {
		return err
	}
	for key, value := range observed {
		if !valuesEqual(value, getOf(source, key)) {
			return &blockedError{message: "CURSOR_BINDING: receipt differs from original native source: " + key}
		}
	}
	return nil
}

// Bracket parameters are Cursor's public model syntax, including effort.
// Keep all supplied parameters and refuse an ambiguous effort declaration.
func cursorModelArgument(request any) (string, map[string]string, error) {
	model, _ := getOf(request, "model").(string)
	effort, _ := getOf(request, "reasoning_effort").(string)
	parameters := map[string]string{}
	base := model
	if start := strings.IndexByte(model, '['); start >= 0 {
		if !strings.HasSuffix(model, "]") {
			return "", nil, &blockedError{message: "CURSOR_CONFIG: malformed model parameters"}
		}
		base = model[:start]
		for _, part := range strings.Split(model[start+1:len(model)-1], ",") {
			key, value, ok := strings.Cut(part, "=")
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if !ok || key == "" || value == "" {
				return "", nil, &blockedError{message: "CURSOR_CONFIG: malformed model parameters"}
			}
			if _, exists := parameters[key]; exists {
				return "", nil, &blockedError{message: "CURSOR_CONFIG: duplicate model parameter"}
			}
			parameters[key] = value
		}
	}
	if effort != "" {
		if base == "" {
			return "", nil, &blockedError{message: "CURSOR_CONFIG: effort requires an explicit model"}
		}
		if value := parameters["effort"]; value != "" && value != effort {
			return "", nil, &blockedError{message: "CURSOR_CONFIG: conflicting effort parameters"}
		}
		if parameters["effort"] == "" {
			if len(parameters) == 0 {
				model = base + "[effort=" + effort + "]"
			} else {
				model = model[:len(model)-1] + ",effort=" + effort + "]"
			}
			parameters["effort"] = effort
		}
	}
	return model, parameters, nil
}

func cursorStore(config, workspace, identity string) string {
	sum := md5.Sum([]byte(workspace))
	return resolveNativePath(filepath.Join(config, "chats", hex.EncodeToString(sum[:]), identity, "store.db"))
}

var cursorUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Capture only native model fields. cli-config also contains authentication
// data, which has no place in invocation evidence. Cursor persists the selected
// parameters before its system/init frame; init's displayName ties the snapshot
// to this process, so another native selection cannot silently substitute it.
func cursorSelectionSnapshot(config string, event any) (map[string]any, error) {
	value, err := readJSONFile(filepath.Join(config, "cli-config.json"))
	if err != nil {
		return nil, &blockedError{message: "CURSOR_CONFIG: actual native selection is unavailable"}
	}
	model := objectOrEmpty(getOf(value, "model"))
	selected := objectOrEmpty(getOf(value, "selectedModel"))
	if !valuesEqual(getOf(model, "displayName"), getOf(event, "model")) {
		return nil, &blockedError{message: "CURSOR_CONFIG: native selection does not match init"}
	}
	parameters := []any{}
	for _, parameter := range listOf(getOf(selected, "parameters")) {
		parameters = append(parameters, nativePickParams(objectOrEmpty(parameter), "id", "value"))
	}
	return map[string]any{"model": nativePickParams(model, "modelId", "displayModelId", "displayName", "aliases"), "selectedModel": map[string]any{"modelId": getOf(selected, "modelId"), "parameters": parameters}}, nil
}
func cursorCheckSelection(request, snapshot any) (map[string]any, error) {
	model := objectOrEmpty(getOf(snapshot, "model"))
	selected := objectOrEmpty(getOf(snapshot, "selectedModel"))
	actual, _ := getOf(model, "modelId").(string)
	if selectedID, _ := getOf(selected, "modelId").(string); selectedID != "" && selectedID != actual {
		return nil, &blockedError{message: "CURSOR_CONFIG: native model and parameters differ"}
	}
	if actual == "" {
		return nil, &blockedError{message: "CURSOR_CONFIG: native selected model is missing"}
	}
	argument, wanted, err := cursorModelArgument(request)
	if err != nil {
		return nil, err
	}
	base := strings.SplitN(argument, "[", 2)[0]
	matches := base == "" || base == actual || valuesEqual(base, getOf(model, "displayModelId")) || valuesEqual(base, getOf(selected, "modelId"))
	for _, alias := range listOf(getOf(model, "aliases")) {
		matches = matches || valuesEqual(alias, base)
	}
	if !matches {
		return nil, &blockedError{message: "CURSOR_CONFIG: model selection was not applied"}
	}
	parameters := map[string]string{}
	for _, value := range listOf(getOf(selected, "parameters")) {
		key, _ := getOf(value, "id").(string)
		v, _ := getOf(value, "value").(string)
		parameters[key] = v
	}
	for key, value := range wanted {
		if parameters[key] != value {
			return nil, &blockedError{message: "CURSOR_CONFIG: model parameter was not applied: " + key}
		}
	}
	return map[string]any{"model": actual, "reasoning_effort": parameters["effort"], "parameters": getOf(selected, "parameters"), "display_name": getOf(model, "displayName")}, nil
}

func cursorExecute(request any, directory string, probe bool) (map[string]any, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		var err error
		binding, err = cursorCaptureBinding()
		if err != nil {
			return nil, err
		}
	}
	launch := objectOrEmpty(getOf(binding, "launch"))
	executable, _ := getOf(launch, "executable").(string)
	if executable == "" {
		return nil, &bindingError{message: "CURSOR_BINDING: missing executable"}
	}
	prefix := []string{executable}
	// A wrapper names its sibling node for closure proofs. A direct node launch
	// names only executable and script and must pass that entry to every command.
	if script, _ := launch["script"].(string); script != "" && getOf(launch, "node") == nil {
		prefix = append(prefix, script)
	}
	environment := map[string]string{}
	for key, value := range objectOrEmpty(getOf(binding, "environment")) {
		environment[key] = textOf(value)
	}
	workspace := resolveNativePath(textOf(getOf(request, "workspace")))
	command := func(args ...string) *exec.Cmd {
		cmd := exec.Command(prefix[0], append(append([]string{}, prefix[1:]...), args...)...)
		cmd.Dir = workspace
		cmd.Env = os.Environ()
		for key, value := range environment {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		return cmd
	}
	if probe {
		version, err := command("--version").Output()
		if err != nil {
			return nil, err
		}
		help, err := command("--help").Output()
		if err != nil {
			return nil, err
		}
		for _, capability := range []string{"--mode", "--model", "--resume", "stream-json", "create-chat"} {
			if !strings.Contains(string(help), capability) {
				return nil, &blockedError{message: "CURSOR_CAPABILITY: missing " + capability}
			}
		}
		return map[string]any{"provider": "cursor", "version": strings.TrimSpace(string(version)), "help": string(help), "selection": map[string]any{}, "model_calls": 0, "capabilities": map[string]any{"source": "cli-help-version", "selection_observed": false}}, nil
	}
	prompt, ok := getOf(request, "prompt").(string)
	if !ok {
		return nil, fmt.Errorf("prompt must be a string")
	}
	readOnly, ok := getOf(request, "read_only").(bool)
	if !ok {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	schemaValue, err := mustEntry(request, "schema")
	if err != nil {
		return nil, err
	}
	schema, err := schemaForPrompt(schemaValue)
	if err != nil {
		return nil, err
	}
	model, _, err := cursorModelArgument(request)
	if err != nil {
		return nil, err
	}
	config := environment["CURSOR_CONFIG_DIR"]
	if config == "" {
		return nil, &bindingError{message: "CURSOR_BINDING: native config directory is missing"}
	}
	identity, _ := getOf(request, "identity").(string)
	nativeSource := map[string]any{"sessionId": identity, "workspace": workspace, "selection": map[string]any{}, "native_config": config}
	store := ""
	if identity == "" {
		cmd := command("create-chat")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, createErr := cmd.Output()
		identity = strings.TrimSpace(string(output))
		nativeSource["create_chat"] = map[string]any{"stdout": string(output), "stderr": stderr.String(), "exit_code": nil}
		if cmd.ProcessState != nil {
			nativeSource["create_chat"].(map[string]any)["exit_code"] = cmd.ProcessState.ExitCode()
		}
		if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
			return nil, err
		}
		if createErr != nil {
			return nil, &blockedError{message: "CURSOR_CREATE: " + createErr.Error() + ": " + stderr.String()}
		}
		if !cursorUUID.MatchString(identity) {
			return nil, &blockedError{message: "CURSOR_BINDING: create-chat did not return a native UUID"}
		}
		store = cursorStore(config, workspace, identity)
	} else {
		store, _ = getOf(request, "native_store").(string)
		if !cursorUUID.MatchString(identity) || store == "" || resolveNativePath(store) != cursorStore(config, workspace, identity) {
			return nil, &blockedError{message: "CURSOR_BINDING: saved native store does not match the session workspace"}
		}
		store = resolveNativePath(store)
	}
	nativeSource["sessionId"] = identity
	nativeSource["native_store"] = store
	if info, err := os.Stat(store); err != nil || !info.Mode().IsRegular() {
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		nativeSource["native_resource_missing"] = map[string]any{"path": store, "identity": identity, "method": "--resume"}
		if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
			return nil, err
		}
		return nil, &sessionUnavailableError{blockedError: blockedError{message: "CURSOR_SESSION_UNAVAILABLE: native-resource-missing: " + store}, identity: identity, method: "--resume", code: nil}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	argv := append(append([]string{}, prefix...), "-p", "--output-format", "stream-json", "--workspace", workspace, "--resume", identity)
	if readOnly {
		argv = append(argv, "--mode", "ask")
	}
	if model != "" {
		argv = append(argv, "--model", model)
	}
	argv = append(argv, prompt+"\nReturn ONLY one JSON object matching this schema:\n"+string(schema))
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	client, err := newNativeClientWithEnv(nativeHooks{transportPrefix: "CURSOR_NATIVE", closedMessage: "Cursor output closed"}, argv, workspace, directory, environment)
	if err != nil {
		return nil, err
	}
	defer client.close()
	client.stdin.Close()
	var streamErr error
	for {
		event, err := client.receive(nil)
		if err != nil {
			if err.Error() != "CURSOR_NATIVE: Cursor output closed" {
				streamErr = err
			}
			break
		}
		line, err := CanonicalJSON(event)
		if err != nil {
			return nil, err
		}
		if _, err := events.Write(append(line, '\n')); err != nil {
			return nil, err
		}
		if valuesEqual(getOf(event, "type"), "system") && valuesEqual(getOf(event, "subtype"), "init") {
			if !valuesEqual(getOf(event, "session_id"), identity) || resolveNativePath(textOf(getOf(event, "cwd"))) != workspace {
				streamErr = &blockedError{message: "CURSOR_BINDING: native init does not match the created or resumed session"}
				break
			}
			snapshot, err := cursorSelectionSnapshot(config, event)
			if err != nil {
				streamErr = err
				break
			}
			selection, err := cursorCheckSelection(request, snapshot)
			if err != nil {
				streamErr = err
				break
			}
			nativeSource["native_selection"] = snapshot
			nativeSource["selection"] = selection
			if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
				return nil, err
			}
			if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"provider": "cursor", "identity": identity, "native_store": store, "workspace": workspace, "selection": selection}); err != nil {
				return nil, err
			}
		}
	}
	waitErr := client.close()
	process := map[string]any{"exit_code": client.cmd.ProcessState.ExitCode(), "error": ""}
	if waitErr != nil {
		process["error"] = waitErr.Error()
	}
	nativeSource["process"] = process
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	if streamErr != nil {
		return nil, streamErr
	}
	source, err := cursorObserve(directory, true)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

func cursorObserve(directory string, materialize bool) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	identity, _ := getOf(source, "sessionId").(string)
	workspace := textOf(getOf(source, "workspace"))
	store := textOf(getOf(source, "native_store"))
	config := textOf(getOf(source, "native_config"))
	if bound := getOf(getOf(getOf(request, "binding"), "environment"), "CURSOR_CONFIG_DIR"); bound != nil && !valuesEqual(bound, config) {
		return nil, &blockedError{message: "CURSOR_BINDING: native config differs"}
	}
	if !cursorUUID.MatchString(identity) || store != cursorStore(config, workspace, identity) || workspace != resolveNativePath(textOf(getOf(request, "workspace"))) {
		return nil, &blockedError{message: "CURSOR_BINDING: source session workspace or native store differs"}
	}
	if previous, _ := getOf(request, "identity").(string); previous != "" && (previous != identity || !valuesEqual(getOf(request, "native_store"), store)) {
		return nil, &blockedError{message: "CURSOR_BINDING: resumed native session or store changed"}
	}
	if !valuesEqual(getOf(getOf(source, "process"), "exit_code"), 0) || !valuesEqual(getOf(getOf(source, "process"), "error"), "") {
		return nil, &blockedError{message: "CURSOR_PROCESS: native process did not exit successfully"}
	}
	file, err := os.Open(filepath.Join(directory, "native-events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	initialized := false
	var terminal map[string]any
	for {
		line, readErr := readNativeLine(reader)
		if line != "" || readErr == nil {
			value, err := decodeNativeJSON([]byte(line))
			if err != nil {
				return nil, err
			}
			event, ok := value.(map[string]any)
			if !ok {
				return nil, &blockedError{message: "CURSOR_NATIVE: native message is not an object"}
			}
			// Hook notifications carry their own conversation/subagent IDs. Only the
			// stream's native session_id can identify a root init or terminal result.
			if session, _ := event["session_id"].(string); session != "" && session != identity {
				return nil, &blockedError{message: "CURSOR_BINDING: mixed native session identities"}
			}
			if terminal != nil {
				return nil, &blockedError{message: "CURSOR_RESULT: native message after terminal result"}
			}
			if valuesEqual(event["type"], "system") && valuesEqual(event["subtype"], "init") {
				if initialized || !valuesEqual(event["session_id"], identity) || resolveNativePath(textOf(event["cwd"])) != workspace || !valuesEqual(event["model"], getOf(getOf(getOf(source, "native_selection"), "model"), "displayName")) {
					return nil, &blockedError{message: "CURSOR_BINDING: native init does not match source"}
				}
				initialized = true
			}
			if valuesEqual(event["type"], "result") {
				requestID, _ := event["request_id"].(string)
				if !initialized || !valuesEqual(event["session_id"], identity) || !valuesEqual(event["subtype"], "success") || !valuesEqual(event["is_error"], false) || requestID == "" {
					return nil, &blockedError{message: "CURSOR_RESULT: native result failed or lacks its request identity"}
				}
				terminal = event
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			break
		}
	}
	if terminal == nil {
		return nil, &blockedError{message: "CURSOR_RESULT: native terminal result is missing"}
	}
	selection, err := cursorCheckSelection(request, getOf(source, "native_selection"))
	if err != nil {
		return nil, err
	}
	if !valuesEqual(selection, getOf(source, "selection")) {
		return nil, &blockedError{message: "CURSOR_BINDING: native selection differs from source"}
	}
	answer, ok := terminal["result"].(string)
	if !ok {
		return nil, &blockedError{message: "CURSOR_RESULT: native result is not text"}
	}
	if materialize {
		if err := writeNativeAnswer(directory, answer, "CURSOR"); err != nil {
			return nil, err
		}
	} else {
		saved, err := os.ReadFile(filepath.Join(directory, "answer.raw.txt"))
		if err != nil {
			return nil, err
		}
		if normalizeNewlines(string(saved)) != answer {
			return nil, &blockedError{message: "CURSOR_RESULT: saved output differs"}
		}
	}
	return evidenceSource(directory, "cursor", identity, map[string]any{"native_store": store, "selection": selection, "native_request": terminal["request_id"], "native_result": terminal, "usage_evidence": terminal["usage"]})
}
func cursorReconcile(directory string, process any) (map[string]any, error) {
	return reconcileCompletedOutput(directory, process, "CURSOR", func(original string) bool { return strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:") }, func() (map[string]any, error) { return cursorObserve(directory, true) })
}
