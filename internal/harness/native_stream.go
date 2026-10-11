package harness

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// streamAdapter supplies platform differences to the shared print-mode lifecycle.
type streamAdapter struct {
	stdinPrompt        bool
	checkResult        func(any) error
	checkMessage       func(any) error
	provider, prefix   string
	capture            func() (map[string]any, error)
	probe              func(string, string, map[string]string) (map[string]any, error)
	arguments          func(any, string, string, string, map[string]string, string, string) ([]string, string, error)
	selection          func(any) map[string]any
	checkSelection     func(any, map[string]any) error
	sessionUnavailable func(string, string) error
	cleanup            func(string)
}

func streamExecute(adapter streamAdapter, request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		var err error
		binding, err = adapter.capture()
		if err != nil {
			return nil, err
		}
	}
	executable, _ := getOf(getOf(binding, "launch"), "executable").(string)
	if executable == "" {
		return nil, &bindingError{message: adapter.prefix + "_BINDING: missing executable"}
	}
	environment := map[string]string{}
	for key, value := range objectOrEmpty(getOf(binding, "environment")) {
		environment[key] = textOf(value)
	}
	workspace, _ := getOf(request, "workspace").(string)
	if probe {
		return adapter.probe(executable, workspace, environment)
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
	_, ok = getOf(request, "read_only").(bool)
	if !ok {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	argv, nativeWorkspace, err := adapter.arguments(request, directory, executable, workspace, environment, prompt, string(schema))
	if err != nil {
		return nil, err
	}
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	client, err := newNativeClientWithEnv(nativeHooks{transportPrefix: adapter.prefix + "_NATIVE", closedMessage: "Native output closed"}, argv, nativeWorkspace, directory, environment)
	if err != nil {
		return nil, err
	}
	defer client.close()
	if adapter.cleanup != nil {
		defer adapter.cleanup(nativeWorkspace)
	}
	// Closing stdin after the prompt input lets print mode start without
	// waiting for additional piped text; no SDK control input is used.
	if adapter.stdinPrompt {
		if _, err := client.stdin.Write([]byte(prompt)); err != nil {
			return nil, err
		}
	}
	client.stdin.Close()
	identity := ""
	selection := map[string]any{}
	nativeSource := map[string]any{"sessionId": identity, "workspace": workspace, "nativeWorkspace": nativeWorkspace, "selection": selection}
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	var streamErr error
	var nativeErrors strings.Builder
	for {
		message, err := client.receive(nil)
		if err != nil {
			if err.Error() != adapter.prefix+"_NATIVE: Native output closed" {
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
		if adapter.checkMessage != nil {
			if err := adapter.checkMessage(message); err != nil {
				return nil, err
			}
		}
		if streamSubagent(message) {
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
				streamErr = &blockedError{message: adapter.prefix + "_BINDING: mixed native session identities"}
				break
			}
			if valuesEqual(getOf(message, "type"), "system") && valuesEqual(getOf(message, "subtype"), "init") {
				selection = adapter.selection(message)
				nativeSource["selection"] = selection
				changed = true
			}
			if requested, _ := getOf(request, "identity").(string); requested != "" && requested != identity {
				// Keep the observed identity as evidence, without publishing it
				// as the consumer's continuation of the requested session.
				if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
					return nil, err
				}
				streamErr = &blockedError{message: adapter.prefix + "_BINDING: resumed native session identity changed"}
				break
			}
			if changed {
				if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
					"identity": identity, "workspace": workspace, "nativeWorkspace": nativeWorkspace, "selection": selection, "acknowledgement": message}); err != nil {
					return nil, err
				}
				if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
					return nil, err
				}
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
		if unavailable := adapter.sessionUnavailable(requested, nativeErrors.String()); unavailable != nil {
			return nil, unavailable
		}
	}
	if streamErr != nil {
		return nil, streamErr
	}
	source, err := streamObserve(adapter, directory)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// streamObserve only consumes this invocation's original native evidence.
// Reaching EOF is useful for draining the stream, never a completion proof.
func streamObserve(adapter streamAdapter, directory string) (map[string]any, error) {
	source, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	identity, _ := getOf(source, "sessionId").(string)
	if identity == "" {
		return nil, &blockedError{message: adapter.prefix + "_BINDING: native session identity is missing"}
	}
	request, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(source, "workspace"), getOf(request, "workspace")) {
		return nil, &blockedError{message: adapter.prefix + "_BINDING: workspace changed"}
	}
	if requested, _ := getOf(request, "identity").(string); requested != "" && requested != identity {
		return nil, &blockedError{message: adapter.prefix + "_BINDING: resumed native session identity changed"}
	}
	process := getOf(source, "process")
	if !valuesEqual(getOf(process, "exit_code"), 0) || !valuesEqual(getOf(process, "error"), "") {
		return nil, &blockedError{message: adapter.prefix + "_PROCESS: native process did not exit successfully: " + textOf(process)}
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
				return nil, &blockedError{message: adapter.prefix + "_NATIVE: native message is not an object"}
			}
			if !streamSubagent(event) {
				if terminal != nil {
					return nil, &blockedError{message: adapter.prefix + "_RESULT: root message after terminal result"}
				}
				if session, _ := event["session_id"].(string); session != "" && session != identity {
					return nil, &blockedError{message: adapter.prefix + "_BINDING: mixed native session identities"}
				}
				if valuesEqual(event["type"], "system") && valuesEqual(event["subtype"], "init") {
					if !valuesEqual(event["session_id"], identity) || !valuesEqual(resolveNativePath(textOf(event["cwd"])), resolveNativePath(textOf(getOrOf(source, "nativeWorkspace", getOf(source, "workspace"))))) {
						return nil, &blockedError{message: adapter.prefix + "_BINDING: native init does not match the session workspace"}
					}
					selection = adapter.selection(event)
				}
				if valuesEqual(event["type"], "result") {
					if !valuesEqual(event["session_id"], identity) || !valuesEqual(event["is_error"], false) {
						return nil, &blockedError{message: adapter.prefix + "_RESULT: native result failed: " + textOf(event)}
					}
					if adapter.checkResult != nil {
						if err := adapter.checkResult(event); err != nil {
							return nil, err
						}
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
		return nil, &blockedError{message: adapter.prefix + "_RESULT: native terminal result is missing"}
	}
	if err := adapter.checkSelection(request, selection); err != nil {
		return nil, err
	}
	if !valuesEqual(selection, getOf(source, "selection")) {
		return nil, &blockedError{message: adapter.prefix + "_BINDING: native selection differs from recorded identity"}
	}
	answer, _ := terminal["result"].(string)
	if structured, exists := terminal["structured_output"]; exists {
		encoded, err := CanonicalJSON(structured)
		if err != nil {
			return nil, err
		}
		answer = string(encoded)
	}
	if err := writeNativeAnswer(directory, answer, adapter.prefix); err != nil {
		return nil, err
	}
	var usage any
	if terminal["usage"] != nil || terminal["modelUsage"] != nil {
		usage = nativePickParams(terminal, "usage", "modelUsage")
	}
	return evidenceSource(directory, adapter.provider, identity, map[string]any{
		"selection": selection, "native_result": terminal, "usage_evidence": usage,
	})
}

func streamReconcile(adapter streamAdapter, directory string, process any) (map[string]any, error) {
	if valuesEqual(getOf(process, "exit_code"), 0) && getOf(process, "error") == "" {
		source, err := streamObserve(adapter, directory)
		if err != nil {
			return nil, err
		}
		if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
			return nil, err
		}
		return source, nil
	}

	return reconcileCompletedOutput(directory, process, adapter.prefix, func(original string) bool {
		return strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:")
	}, func() (map[string]any, error) { return streamObserve(adapter, directory) })
}

// A subagent's messages are native evidence, but never the root session's
// identity or final answer. Claude identifies them with parent_tool_use_id.
func streamSubagent(event any) bool { return getOf(event, "parent_tool_use_id") != nil }

func streamSelection(event any) map[string]any {
	return map[string]any{"model": getOf(event, "model"), "reasoning_effort": getOf(event, "effort")}
}

func streamSessionUnavailable(identity, text string) error {
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
