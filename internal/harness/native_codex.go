package harness

// Codex app-server owns persistent threads, turn identity and permissions.
// This adapter speaks its native stdio protocol and observes the same raw
// evidence on live completion and recovery. Recovery never starts a turn.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func codexCaptureBinding() (map[string]any, error) {
	executable, err := exec.LookPath("codex")
	if err != nil {
		return nil, &blockedError{message: "CODEX_UNAVAILABLE: codex is not on PATH"}
	}
	binding, err := buildCaptureBinding("codex", map[string]any{"executable": resolveNativePath(executable)})
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	config := os.Getenv("CODEX_HOME")
	if config == "" {
		config = filepath.Join(home, ".codex")
	}
	// Native config/auth resolution stays with app-server. Only its path is
	// pinned; credentials are never copied into an evidence record.
	binding["environment"] = map[string]any{"HOME": resolveNativePath(home), "CODEX_HOME": resolveNativePath(config)}
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
	raw, err := os.ReadFile(filepath.Join(directory, "answer.raw.txt"))
	if err != nil {
		return err
	}
	result, refusal := FrameResult(string(raw))
	if refusal != "" {
		return &blockedError{message: refusal}
	}
	decoded, err := readJSONFile(filepath.Join(directory, "answer.json"))
	if err != nil {
		return err
	}
	projected, err := codexDomainResult(getOf(request, "schema"), result)
	if err != nil {
		return err
	}
	if !valuesEqual(projected, decoded) {
		return &blockedError{message: "CODEX_BINDING: domain result differs from original answer"}
	}
	original, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return err
	}
	expected, err := codexTurnParameters(request, textOf(getOf(source, "identity")))
	if err != nil {
		return err
	}
	if !valuesEqual(expected, getOf(original, "turn_request")) {
		return &blockedError{message: "CODEX_BINDING: original turn request differs"}
	}
	return nil
}

func codexInteractionID(params map[string]any, frameID any) (string, error) {
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
		return codexInteractionID(params, frameID)
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
// App-server owns inherited permissions; only a read-only role adds a sandbox
// constraint. Both execution and recovery check this same native response.
func codexCheckConfiguration(request, configuration any, thread string) error {
	workspace, _ := getOf(request, "workspace").(string)
	sandboxType, _ := getOf(getOf(configuration, "sandbox"), "type").(string)
	reviewer, _ := getOf(configuration, "approvalsReviewer").(string)
	nativeThread := getOf(configuration, "thread")
	if !valuesEqual(getOf(nativeThread, "id"), thread) || !valuesEqual(getOf(nativeThread, "ephemeral"), false) || !valuesEqual(resolveNativePath(textOf(getOf(configuration, "cwd"))), resolveNativePath(workspace)) || sandboxType == "" || getOf(configuration, "approvalPolicy") == nil || reviewer == "" || (getOf(request, "read_only") == true && sandboxType != "readOnly") {
		return &blockedError{message: "CODEX_CONFIG: native workspace/persistence/permissions differ"}
	}
	selected, _ := getOf(configuration, "model").(string)
	requested, _ := getOf(request, "model").(string)
	if selected == "" || (requested != "" && selected != requested) {
		return &blockedError{message: "CODEX_CONFIG: native model selection differs or is missing"}
	}
	return nil
}

func codexExecute(request any, directory string, probe bool) (map[string]any, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		var err error
		binding, err = codexCaptureBinding()
		if err != nil {
			return nil, err
		}
	}
	if err := codexValidateBinding(binding); err != nil {
		return nil, err
	}
	executable, _ := getOf(getOf(binding, "launch"), "executable").(string)
	if executable == "" {
		return nil, &bindingError{message: "CODEX_BINDING: missing executable"}
	}
	environment := map[string]string{}
	for key, value := range objectOrEmpty(binding["environment"]) {
		environment[key] = textOf(value)
	}
	workspace, _ := getOf(request, "workspace").(string)
	events, err := os.OpenFile(filepath.Join(directory, "native-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer events.Close()
	thread, turn := "", ""
	var terminal any
	var recordErr error
	hooks := nativeHooks{transportPrefix: "CODEX_NATIVE", requestPrefix: "CODEX_RPC", bounded: true,
		identify: func(c *nativeClient) any { return c.nextCounter() },
		observes: codexInteractionObserved,
		respond: func(method string, params any) (*nativeReply, error) {
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
	client, err := newNativeClientWithEnv(hooks, []string{executable, "app-server", "--stdio"}, workspace, directory, environment)
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
	params := map[string]any{"cwd": workspace}
	if readOnly {
		params["sandbox"] = "read-only"
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
	nativeSource := map[string]any{"threadId": thread, "workspace": workspace, "thread": nativeThread, "configuration": opened, "selection": selection}
	if err = writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"identity": thread, "workspace": workspace, "selection": selection}); err != nil {
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
	if err = writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{"identity": thread, "workspace": workspace, "selection": selection}); err != nil {
		return nil, err
	}
	if err = writeAtomicJSON(filepath.Join(directory, "native-source.json"), nativeSource); err != nil {
		return nil, err
	}
	for !valuesEqual(getOf(terminal, "threadId"), thread) || !valuesEqual(getOf(getOf(terminal, "turn"), "id"), turn) {
		if _, err = client.receive(nil); err != nil {
			return nil, err
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
	source, err := codexObserve(directory)
	if err != nil {
		return nil, err
	}
	if err = writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// codexObserve uses the original job request, acknowledged thread/turn and raw
// completion evidence for both paths. Same-thread events from another turn
// and child-thread events cannot become this invocation's answer or usage.
func codexObserve(directory string) (map[string]any, error) {
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
	expected, err := codexTurnParameters(request, thread)
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
	file, err := os.Open(filepath.Join(directory, "native-events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	var terminal, usage any
	items := []any{}
	for {
		line, readErr := readNativeLine(reader)
		if line != "" || readErr == nil {
			frame, err := decodeNativeJSON([]byte(line))
			if err != nil {
				return nil, err
			}
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
					if valuesEqual(method, "item/completed") {
						items = append(items, getOf(p, "item"))
					}
					if valuesEqual(method, "thread/tokenUsage/updated") {
						usage = getOf(p, "tokenUsage")
					}
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
	if !configured || !acknowledged {
		return nil, &blockedError{message: "CODEX_BINDING: original native configuration/turn response is missing"}
	}
	if terminal == nil {
		return nil, &blockedError{message: "CODEX_RESULT: original turn terminal is missing"}
	}
	if !valuesEqual(getOf(terminal, "status"), "completed") {
		return nil, &blockedError{message: "CODEX_RESULT: original turn did not complete: " + textOf(terminal)}
	}
	items = append(items, listOf(getOf(terminal, "items"))...)
	var final, unknown any
	for _, item := range items {
		if valuesEqual(getOf(item, "type"), "agentMessage") {
			if valuesEqual(getOf(item, "phase"), "final_answer") {
				final = item
			} else if getOf(item, "phase") == nil {
				unknown = item
			}
		}
	}
	if final == nil {
		final = unknown
	}
	if final == nil {
		return nil, &blockedError{message: "CODEX_RESULT: completed turn has no final agent message"}
	}
	answer, ok := getOf(final, "text").(string)
	if !ok {
		return nil, &blockedError{message: "CODEX_RESULT: final agent message text is missing"}
	}
	if err = writeNativeAnswerProjected(directory, answer, "CODEX", func(result map[string]any) (map[string]any, error) {
		projected, err := codexDomainResult(getOf(request, "schema"), result)
		if err != nil {
			return nil, err
		}
		return projected.(map[string]any), nil
	}); err != nil {
		return nil, err
	}
	return evidenceSource(directory, "codex", thread, map[string]any{"native_turn": turn, "native_result": terminal, "selection": getOf(source, "selection"), "selection_evidence": map[string]any{"configuration": getOf(source, "configuration"), "turn_request": getOf(source, "turn_request")}, "transcript": getOf(getOf(source, "thread"), "path"), "usage_evidence": usage})
}

func codexReconcile(directory string, process any) (map[string]any, error) {
	// A supervisor may stop after the native terminal landed but before the
	// receipt was written. Observation needs no process or prompt replay.
	if valuesEqual(getOf(process, "exit_code"), 0) {
		source, err := codexObserve(directory)
		if err != nil {
			return nil, err
		}
		if err = writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
			return nil, err
		}
		return source, nil
	}
	return reconcileCompletedOutput(directory, process, "CODEX", func(original string) bool { return strings.HasPrefix(original, "RuntimeBlocked: NATIVE_RESULT:") }, func() (map[string]any, error) { return codexObserve(directory) })
}
