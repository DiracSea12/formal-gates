package harness

// Claude's print command owns the session, permissions and persistent history.
// This adapter records its native stream and process exit, then uses the same
// observer for live completion and recovery; it never replays a prompt.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func claudeCaptureBinding() (map[string]any, error) {
	executable, err := exec.LookPath("claude")
	if err != nil {
		return nil, &blockedError{message: "CLAUDE_UNAVAILABLE: claude is not on PATH"}
	}
	binding, err := buildCaptureBinding("claude", map[string]any{"executable": resolveNativePath(executable)})
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(home, ".claude")
	}
	binding["environment"] = map[string]any{"HOME": resolveNativePath(home), "CLAUDE_CONFIG_DIR": resolveNativePath(config)}
	return binding, nil
}

func claudeValidateBinding(binding any) error { return validateNativeBinding(binding, "claude") }
func claudeVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "claude")
}

func claudeSelection(event any) map[string]any {
	return map[string]any{"model": getOf(event, "model"), "reasoning_effort": getOf(event, "effort")}
}

func claudeCheckSelection(request any, selection map[string]any) error {
	model, _ := selection["model"].(string)
	if model == "" {
		return &blockedError{message: "CLAUDE_CONFIG: native selected model is missing"}
	}
	requested, _ := getOf(request, "model").(string)
	if requested != "" && requested != model {
		// Native --model accepts these family aliases as well as full model
		// names. The receipt keeps the resolved native name, never the alias.
		switch requested {
		case "sonnet", "opus", "haiku", "fable":
			if !strings.HasPrefix(model, "claude-"+requested+"-") {
				return &blockedError{message: "CLAUDE_CONFIG: model selection was not applied"}
			}
		default:
			return &blockedError{message: "CLAUDE_CONFIG: model selection was not applied"}
		}
	}
	if effort, _ := getOf(request, "reasoning_effort").(string); effort != "" && !valuesEqual(selection["reasoning_effort"], effort) {
		return &blockedError{message: "CLAUDE_CONFIG: effort selection was not applied"}
	}
	return nil
}

// A subagent's messages are native evidence, but never the root session's
// identity or final answer. Claude identifies them with parent_tool_use_id.
func claudeSubagent(event any) bool { return getOf(event, "parent_tool_use_id") != nil }

func claudeSessionUnavailable(identity, text string) error {
	if identity == "" {
		return nil
	}
	for _, line := range strings.Split(normalizeNewlines(text), "\n") {
		if strings.TrimSpace(line) == "No conversation found with session ID: "+identity {
			return &sessionUnavailableError{blockedError: blockedError{message: strings.TrimSpace(line)},
				identity: identity, method: "--resume", code: nil}
		}
	}
	return nil
}

func claudeExecute(request any, directory string, probe bool) (map[string]any, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		var err error
		binding, err = claudeCaptureBinding()
		if err != nil {
			return nil, err
		}
	}
	executable, _ := getOf(getOf(binding, "launch"), "executable").(string)
	if executable == "" {
		return nil, &bindingError{message: "CLAUDE_BINDING: missing executable"}
	}
	environment := map[string]string{}
	for key, value := range objectOrEmpty(getOf(binding, "environment")) {
		environment[key] = textOf(value)
	}
	workspace, _ := getOf(request, "workspace").(string)
	if probe {
		command := exec.Command(executable, "--version")
		command.Dir = workspace
		command.Env = os.Environ()
		for key, value := range environment {
			command.Env = append(command.Env, key+"="+value)
		}
		version, err := command.Output()
		if err != nil {
			return nil, err
		}
		return map[string]any{"provider": "claude", "executable": executable, "version": strings.TrimSpace(string(version)),
			"capabilities": map[string]any{"source": "cli-version", "selection_observed": false},
			"selection":    map[string]any{}, "model_calls": 0}, nil
	}
	prompt, ok := getOf(request, "prompt").(string)
	if !ok {
		return nil, fmt.Errorf("prompt must be a string")
	}
	schemaValue, err := mustEntry(request, "schema")
	if err != nil {
		return nil, err
	}
	schema, err := CanonicalJSON(schemaValue)
	if err != nil {
		return nil, err
	}
	readOnly, ok := getOf(request, "read_only").(bool)
	if !ok {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	argv := []string{executable, "-p", prompt, "--output-format", "stream-json", "--verbose", "--include-hook-events", "--json-schema", string(schema)}
	if readOnly {
		argv = append(argv, "--tools", "Read,Glob,Grep")
	} else {
		argv = append(argv, "--allowedTools", "Read,Glob,Grep,Bash,Edit,Write,Agent")
	}
	for _, option := range []struct{ key, flag string }{{"identity", "--resume"}, {"model", "--model"}, {"reasoning_effort", "--effort"}} {
		if value, _ := getOf(request, option.key).(string); value != "" {
			argv = append(argv, option.flag, value)
		}
	}
	config := environment["CLAUDE_CONFIG_DIR"]
	if config == "" {
		config = os.Getenv("CLAUDE_CONFIG_DIR")
		if config == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			config = filepath.Join(home, ".claude")
		}
	}
	settings := filepath.Join(config, "settings.json")
	if _, err := os.Stat(settings); err == nil {
		argv = append(argv, "--settings", settings)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	client, err := newNativeClientWithEnv(nativeHooks{transportPrefix: "CLAUDE_NATIVE", closedMessage: "Claude output closed"}, argv, workspace, directory, environment)
	if err != nil {
		return nil, err
	}
	defer client.close()
	// The prompt is an argv value. Closing stdin lets print mode start without
	// waiting for additional piped text; no SDK control input is used.
	client.stdin.Close()
	identity := ""
	selection := map[string]any{}
	nativeSource := map[string]any{"sessionId": identity, "workspace": workspace, "selection": selection}
	var streamErr error
	var nativeErrors strings.Builder
	for {
		message, err := client.receive(nil)
		if err != nil {
			if err.Error() != "CLAUDE_NATIVE: Claude output closed" {
				streamErr = err
			}
			break
		}
		line, err := CanonicalJSON(message)
		if err != nil {
			return nil, err
		}
		if _, err := events.Write(append(line, '\n')); err != nil {
			return nil, err
		}
		if claudeSubagent(message) {
			continue
		}
		for _, value := range listOf(getOf(message, "errors")) {
			if text, ok := value.(string); ok {
				nativeErrors.WriteString(text + "\n")
			}
		}
		if observed, _ := getOf(message, "session_id").(string); observed != "" {
			changed := identity == ""
			if identity == "" {
				identity = observed
				nativeSource["sessionId"] = identity
			}
			if identity != observed {
				streamErr = &blockedError{message: "CLAUDE_BINDING: mixed native session identities"}
				break
			}
			if valuesEqual(getOf(message, "type"), "system") && valuesEqual(getOf(message, "subtype"), "init") {
				selection = claudeSelection(message)
				nativeSource["selection"] = selection
				changed = true
			}
			if changed {
				if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
					"identity": identity, "workspace": workspace, "selection": selection}); err != nil {
					return nil, err
				}
				if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
					return nil, err
				}
			}
			if requested, _ := getOf(request, "identity").(string); requested != "" && requested != identity {
				streamErr = &blockedError{message: "CLAUDE_BINDING: resumed native session identity changed"}
				break
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
	if stderr, err := os.ReadFile(filepath.Join(directory, "host.stderr")); err == nil {
		nativeErrors.Write(stderr)
	}
	requested, _ := getOf(request, "identity").(string)
	if waitErr != nil {
		if unavailable := claudeSessionUnavailable(requested, nativeErrors.String()); unavailable != nil {
			return nil, unavailable
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	source, err := claudeObserve(directory)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// claudeObserve only consumes this invocation's original native evidence.
// Reaching EOF is useful for draining the stream, never a completion proof.
func claudeObserve(directory string) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	identity, _ := getOf(source, "sessionId").(string)
	if identity == "" {
		return nil, &blockedError{message: "CLAUDE_BINDING: native session identity is missing"}
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(source, "workspace"), getOf(request, "workspace")) {
		return nil, &blockedError{message: "CLAUDE_BINDING: workspace changed"}
	}
	if requested, _ := getOf(request, "identity").(string); requested != "" && requested != identity {
		return nil, &blockedError{message: "CLAUDE_BINDING: resumed native session identity changed"}
	}
	process := getOf(source, "process")
	if !valuesEqual(getOf(process, "exit_code"), 0) || !valuesEqual(getOf(process, "error"), "") {
		return nil, &blockedError{message: "CLAUDE_PROCESS: native process did not exit successfully: " + textOf(process)}
	}
	file, err := os.Open(filepath.Join(directory, "native-events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	var terminal map[string]any
	var selection map[string]any
	for {
		line, readErr := readNativeLine(reader)
		if line != "" || readErr == nil {
			value, err := decodeNativeJSON([]byte(line))
			if err != nil {
				return nil, err
			}
			event, ok := value.(map[string]any)
			if !ok {
				return nil, &blockedError{message: "CLAUDE_NATIVE: native message is not an object"}
			}
			if !claudeSubagent(event) {
				if terminal != nil {
					return nil, &blockedError{message: "CLAUDE_RESULT: root message after terminal result"}
				}
				if session, _ := event["session_id"].(string); session != "" && session != identity {
					return nil, &blockedError{message: "CLAUDE_BINDING: mixed native session identities"}
				}
				if valuesEqual(event["type"], "system") && valuesEqual(event["subtype"], "init") {
					if !valuesEqual(event["session_id"], identity) || !valuesEqual(resolveNativePath(textOf(event["cwd"])), resolveNativePath(textOf(getOf(source, "workspace")))) {
						return nil, &blockedError{message: "CLAUDE_BINDING: native init does not match the session workspace"}
					}
					selection = claudeSelection(event)
				}
				if valuesEqual(event["type"], "result") {
					if !valuesEqual(event["session_id"], identity) || !valuesEqual(event["is_error"], false) {
						return nil, &blockedError{message: "CLAUDE_RESULT: native result failed: " + textOf(event)}
					}
					terminal = event
				}
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
		return nil, &blockedError{message: "CLAUDE_RESULT: native terminal result is missing"}
	}
	if err := claudeCheckSelection(request, selection); err != nil {
		return nil, err
	}
	if !valuesEqual(selection, getOf(source, "selection")) {
		return nil, &blockedError{message: "CLAUDE_BINDING: native selection differs from recorded identity"}
	}
	answer, _ := terminal["result"].(string)
	if structured, exists := terminal["structured_output"]; exists {
		encoded, err := CanonicalJSON(structured)
		if err != nil {
			return nil, err
		}
		answer = string(encoded)
	}
	if err := writeNativeAnswer(directory, answer, "CLAUDE"); err != nil {
		return nil, err
	}
	var usage any
	if terminal["usage"] != nil || terminal["modelUsage"] != nil {
		usage = nativePickParams(terminal, "usage", "modelUsage")
	}
	return evidenceSource(directory, "claude", identity, map[string]any{
		"selection": selection, "native_result": terminal, "usage_evidence": usage,
	})
}

func claudeReconcile(directory string, process any) (map[string]any, error) {
	return reconcileCompletedOutput(directory, process, "CLAUDE", func(original string) bool {
		return strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:")
	}, func() (map[string]any, error) { return claudeObserve(directory) })
}
