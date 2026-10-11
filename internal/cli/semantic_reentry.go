package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
	"formal-gates/internal/validate"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func selectStandaloneContext(root string, position int, out io.Writer) (string, bool, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".gates", "tmp", "gate-review", "review-*.json"))
	if err != nil {
		return "", true, err
	}
	choices := []map[string]any{}
	for i, path := range paths {
		var context validate.StandaloneGateContext
		if err := readJSONFileInto(path, &context); err != nil {
			return "", true, err
		}
		choices = append(choices, map[string]any{"review": i + 1, "vcs": context.VCS, "scope": context.Scope, "base": context.Base})
	}
	if position == 0 && len(paths) == 1 {
		position = 1
	}
	if position == 0 {
		_, err := printJSON(out, choices)
		return "", false, err
	}
	if position < 1 || position > len(paths) {
		return "", true, fmt.Errorf("select an original displayed review")
	}
	return paths[position-1], true, nil
}

// Native interaction payloads belong to the original pending request directory.
func runNativeInteraction(args []string, streams IO) (int, error) {
	if len(args) == 0 || (args[0] != "show" && args[0] != "respond") {
		return 1, fmt.Errorf("native show or respond is required")
	}
	command, args := args[0], args[1:]
	if command == "respond" && workflowFlag(args, "decision-file", "") != "" {
		return harness.NativeRespond(args, streams.Stderr), nil
	}
	root := workflowFlag(args, "root", ".")
	id := workflowFlag(args, "run-id", "")
	choice, err := strconv.Atoi(workflowFlag(args, "run", "0"))
	if err != nil {
		return 1, err
	}
	f, selected, err := selectWorkflowRun(root, &id, choice, streams.Stdout)
	if err != nil {
		return 1, err
	}
	if !selected {
		return 0, nil
	}
	if f == nil {
		return 1, fmt.Errorf("current native run is unavailable")
	}
	fullRoot, err := filepath.Abs(root)
	if err != nil {
		return 1, err
	}
	directory := filepath.Join(fullRoot, facade.EngineNamespace, id)
	original := removeNativeWrapperArguments(args)
	if command == "show" {
		return harness.NativeShow(append(original, "--run", directory), streams.Stdout, streams.Stderr), nil
	}
	question, err := strconv.Atoi(workflowFlag(args, "question", "1"))
	if err != nil {
		return 1, err
	}
	payloadDirectory, err := nativeRequestPayloadDirectory(directory, question)
	if err != nil {
		return 1, err
	}
	decision, err := strconv.Atoi(workflowFlag(args, "decision", "0"))
	if err != nil {
		return 1, err
	}
	if hasFlagArgument(args, "decision") || hasFlagArgument(args, "confirm") || hasFlagArgument(args, "decisions") {
		allowed := map[string]bool{"root": true, "run": true, "run-id": true, "question": true, "decision": true, "confirm": true, "decisions": true}
		for _, arg := range args {
			if strings.HasPrefix(arg, "--") {
				name, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
				if !allowed[name] {
					return 1, fmt.Errorf("confirm only the original selected decision")
				}
			}
		}
		if hasFlagArgument(args, "decisions") {
			return listOriginalPayloads(filepath.Join(payloadDirectory, "decisions"), streams.Stdout)
		}
		path, done, err := selectOriginalPayload(filepath.Join(payloadDirectory, "decisions"), decision, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}

		return harness.NativeRespond([]string{"--decision-file", path}, streams.Stderr), nil
	}
	original, textDirectory, selected, err := nativeTextPayloadArguments(original, payloadDirectory, streams.Stdout)
	if err != nil {
		return 1, err
	}
	if !selected {
		return 0, nil
	}
	if hasFlagArgument(args, "prepare-text") {
		if hasFlagArgument(args, "text-payload") || hasFlagArgument(args, "texts") {
			return 1, fmt.Errorf("prepare or select original text")
		}
		body, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		_, position, err := saveOriginalPayload(textDirectory, body)
		return printValue(streams.Stdout, map[string]any{"textPayload": position, "text": string(body)}, err)
	}
	if hasFlagArgument(args, "texts") {
		return listOriginalPayloads(textDirectory, streams.Stdout)
	}
	save := workflowFlag(args, "save-decision", "")
	if save != "" {
		return harness.NativeRespond(append(original, "--run", directory), streams.Stderr), nil
	}
	if err := os.MkdirAll(payloadDirectory, 0700); err != nil {
		return 1, err
	}
	temporary, err := os.CreateTemp(payloadDirectory, "decision-*")
	if err != nil {
		return 1, err
	}
	save = temporary.Name()
	if err := temporary.Close(); err != nil {
		return 1, err
	}
	defer os.Remove(save)
	code := harness.NativeRespond(append(original, "--run", directory, "--save-decision", save), streams.Stderr)
	if code != 0 {
		return code, nil
	}
	body, err := os.ReadFile(save)
	if err != nil {
		return 1, err
	}
	_, position, err := saveOriginalPayload(filepath.Join(payloadDirectory, "decisions"), body)
	if err != nil {
		return 1, err
	}
	var candidate struct {
		Pending   json.RawMessage `json:"pending"`
		Selection json.RawMessage `json:"selection"`
	}
	if err := json.Unmarshal(body, &candidate); err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, map[string]any{"decision": position, "question": candidate.Pending, "selection": candidate.Selection})

}

func hasFlagArgument(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || strings.HasPrefix(arg, "--"+name+"=") {
			return true
		}
	}
	return false
}

func removeNativeWrapperArguments(args []string) []string {
	values := map[string]bool{"root": true, "run": true, "run-id": true, "decision": true}
	switches := map[string]bool{"confirm": true, "decisions": true, "prepare-text": true, "texts": true}
	original := []string{}
	for i := 0; i < len(args); i++ {
		name, _, inline := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		if values[name] {
			if !inline {
				i++
			}
			continue
		}
		if switches[name] {
			continue
		}
		original = append(original, args[i])
	}
	return original
}

func nativeRequestPayloadDirectory(run string, position int) (string, error) {
	files := []string{}
	for _, pattern := range []string{filepath.Join(run, "jobs", "*", "interaction-pending.json"), filepath.Join(run, "native", "*", "*", "interaction-pending.json")} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		files = append(files, paths...)
	}
	current := 0
	for _, path := range files {
		var records []map[string]any
		if err := readJSONFileInto(path, &records); err != nil {
			return "", err
		}
		for _, record := range records {
			if record["status"] != "pending" {
				continue
			}
			current++
			if current == position {
				request, err := json.Marshal([]any{record["connection"], record["request_id"]})
				if err != nil || record["request_id"] == nil {
					return "", fmt.Errorf("original native request is unavailable")
				}
				return filepath.Join(filepath.Dir(path), "input-payloads", base64.RawURLEncoding.EncodeToString(request)), nil
			}
		}
	}
	return "", fmt.Errorf("select a current displayed native question")
}

// Text selection follows the same input-question/field order as NativeRespond.
func nativeTextPayloadArguments(args []string, directory string, out io.Writer) ([]string, string, bool, error) {
	mode, position, target := "responses", "1", "response-text-file"
	original := []string{}
	for i := 0; i < len(args); i++ {
		name, value, inline := strings.Cut(args[i], "=")
		if name != "--response-question" && name != "--form-field" && name != "--text-payload" {
			original = append(original, args[i])
			continue
		}
		if !inline {
			if i+1 == len(args) {
				return nil, "", true, fmt.Errorf("%s requires a displayed position", name)
			}
			i++
			value = args[i]
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || name != "--text-payload" && n == 0 {
			return nil, "", true, fmt.Errorf("select an original displayed position")
		}
		if name == "--response-question" {
			mode, position, target = "responses", strconv.Itoa(n), "response-text-file"
		}
		if name == "--form-field" {
			mode, position, target = "fields", strconv.Itoa(n), "form-value-file"
		}
		if name != "--text-payload" {
			original = append(original, name, value)
			continue
		}
		path, done, err := selectOriginalPayload(filepath.Join(directory, mode, position), n, out)
		if err != nil || !done {
			return nil, "", done, err
		}
		original = append(original, "--"+target, path)
	}
	return original, filepath.Join(directory, mode, position), true, nil
}
