package cli

import (
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

// Native interaction files remain payload evidence of the selected run.
func runNativeInteraction(args []string, streams IO) (int, error) {
	if len(args) == 0 || (args[0] != "show" && args[0] != "respond") {
		return 1, fmt.Errorf("native show or respond is required")
	}
	command, args := args[0], args[1:]
	if workflowFlag(args, "decision-file", "") != "" {
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
	original := []string{}
	for i := 0; i < len(args); i++ {
		name, _, inline := strings.Cut(args[i], "=")
		if name == "--root" || name == "--run" || name == "--run-id" {
			if !inline {
				i++
			}
			continue
		}
		original = append(original, args[i])
	}
	fullRoot, err := filepath.Abs(root)
	if err != nil {
		return 1, err
	}
	directory := filepath.Join(fullRoot, facade.EngineNamespace, id)
	if _, err := os.Stat(directory); err != nil {
		return 1, err
	}
	original = append(original, "--run", directory)
	if command == "show" {
		return harness.NativeShow(original, streams.Stdout, streams.Stderr), nil
	}
	return harness.NativeRespond(original, streams.Stderr), nil
}
