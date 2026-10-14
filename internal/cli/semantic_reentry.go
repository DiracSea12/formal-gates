package cli

import (
	"encoding/json"
	"flag"
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
		flags := flag.NewFlagSet("native show|respond", flag.ContinueOnError)
		flags.SetOutput(streams.Stderr)
		if code, err, done := parseFlagSet(flags, args, streams.Stdout); done {
			return code, err
		}
		return 1, fmt.Errorf("native show or respond is required")
	}
	command, args := args[0], args[1:]
	flags := flag.NewFlagSet("native show", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	var execute func() int
	mode, position := "responses", 1
	type textPayload struct {
		mode              string
		position, payload int
	}
	texts := []textPayload{}
	if command == "respond" {
		flags, execute = harness.NativeRespondFlags(streams.Stderr, func(name string, selected int) {
			mode, position = "responses", selected
			if name == "form-field" {
				mode = "fields"
			}
		})
		flags.Init("native respond", flag.ContinueOnError)
		flags.Lookup("run").DefValue = "0"
		if err := flags.Lookup("run").Value.Set("0"); err != nil {
			return 1, err
		}
		flags.Func("text-payload", "select original text for the current input question or field", func(value string) error {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return fmt.Errorf("select an original displayed position")
			}
			texts = append(texts, textPayload{mode, position, n})
			return nil
		})
	} else {
		flags.String("run", "0", "select a run by its displayed number")
	}
	flags.Lookup("run").Usage = "select a run by its displayed number"
	root := flags.String("root", ".", "repository root")
	id := flags.String("run-id", "", "program integration: exact engine run id")
	var decision int
	var confirm, listDecisions, prepareText, listTexts bool
	if command == "respond" {
		flags.IntVar(&decision, "decision", 0, "select an original saved decision")
		flags.BoolVar(&confirm, "confirm", false, "confirm the only original saved decision")
		flags.BoolVar(&listDecisions, "decisions", false, "list original saved decisions")
		flags.BoolVar(&prepareText, "prepare-text", false, "save original text from stdin")
		flags.BoolVar(&listTexts, "texts", false, "list original saved texts")
	}
	if code, err, done := parseFlagSet(flags, args, streams.Stdout); done {
		return code, err
	}
	if command == "respond" && flags.Lookup("decision-file").Value.String() != "" {
		return execute(), nil
	}
	choice, err := strconv.Atoi(flags.Lookup("run").Value.String())
	if err != nil {
		return 1, err
	}
	f, selected, err := selectWorkflowRun(*root, id, choice, streams.Stdout)
	if err != nil {
		return 1, err
	}
	if !selected {
		return 0, nil
	}
	if f == nil {
		return 1, fmt.Errorf("current native run is unavailable")
	}
	fullRoot, err := filepath.Abs(*root)
	if err != nil {
		return 1, err
	}
	directory := filepath.Join(fullRoot, facade.EngineNamespace, *id)
	if command == "show" {
		return harness.NativeShow([]string{"--run", directory}, streams.Stdout, streams.Stderr), nil
	}
	question := flags.Lookup("question").Value.(flag.Getter).Get().(int)
	payloadDirectory, err := nativeRequestPayloadDirectory(directory, question)
	if err != nil {
		return 1, err
	}
	decisionProvided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "decision" {
			decisionProvided = true
		}
	})
	if decisionProvided || confirm || listDecisions {
		var invalid bool
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "root", "run", "run-id", "question", "decision", "confirm", "decisions":
			default:
				invalid = true
			}
		})
		if invalid {
			return 1, fmt.Errorf("confirm only the original selected decision")
		}
		if listDecisions {
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
	textDirectory := filepath.Join(payloadDirectory, mode, strconv.Itoa(position))
	if prepareText {
		if len(texts) != 0 || listTexts {
			return 1, fmt.Errorf("prepare or select original text")
		}
		body, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		_, position, err := facade.SaveOriginalPayload(textDirectory, body)
		return printValue(streams.Stdout, map[string]any{"textPayload": position, "text": string(body)}, err)
	}
	if listTexts {
		return listOriginalPayloads(textDirectory, streams.Stdout)
	}
	for _, text := range texts {
		path, done, err := selectOriginalPayload(filepath.Join(payloadDirectory, text.mode, strconv.Itoa(text.position)), text.payload, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}
		selector, target := "response-question", "response-text-file"
		if text.mode == "fields" {
			selector, target = "form-field", "form-value-file"
		}
		if err := flags.Set(selector, strconv.Itoa(text.position)); err != nil {
			return 1, err
		}
		if err := flags.Set(target, path); err != nil {
			return 1, err
		}
	}
	if err := flags.Set("run", directory); err != nil {
		return 1, err
	}
	save := flags.Lookup("save-decision").Value.String()
	if save != "" {
		return execute(), nil
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
	if err := flags.Set("save-decision", save); err != nil {
		return 1, err
	}
	code := execute()
	if code != 0 {
		return code, nil
	}
	body, err := os.ReadFile(save)
	if err != nil {
		return 1, err
	}
	_, position, err = facade.SaveOriginalPayload(filepath.Join(payloadDirectory, "decisions"), body)
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

func nativeRequestPayloadDirectory(run string, position int) (string, error) {
	return harness.NativeRequestPayloadDirectory(run, position)
}
