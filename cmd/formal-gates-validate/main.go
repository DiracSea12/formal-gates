package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"formal-gates/internal/validate"
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if code != 0 {
		os.Exit(code)
	}
}

func run(args []string) (int, error) {
	command := "package"
	if len(args) > 0 && args[0] != "-h" && args[0] != "--help" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "package":
		fs := flag.NewFlagSet("package", flag.ContinueOnError)
		root := fs.String("root", ".", "formal-gates package root")
		if err := fs.Parse(args); err != nil {
			return 1, err
		}
		return printValidationResult("package", validate.Package(*root))
	case "artifact":
		fs := flag.NewFlagSet("artifact", flag.ContinueOnError)
		root := fs.String("root", ".", "repository root for relative artifact references")
		file := fs.String("file", "", "artifact file to validate")
		gate := fs.String("gate", "", "gate id")
		workflowID := fs.String("workflow-id", "", "expected workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "expected change snapshot")
		stage := fs.String("stage", "", "expected QA stage, when relevant")
		if err := fs.Parse(args); err != nil {
			return 1, err
		}
		return printValidationResult("artifact", validate.Artifact(validate.ArtifactOptions{
			Root:           *root,
			File:           *file,
			Gate:           *gate,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Stage:          *stage,
		}))
	case "hook":
		decision, err := readHookDecision(os.Stdin)
		if err != nil {
			return 1, err
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return 1, err
		}
		fmt.Println(string(encoded))
		if decision.Decision == "deny" {
			return 2, nil
		}
		return 0, nil
	case "help", "-h", "--help":
		printUsage()
		return 0, nil
	default:
		printUsage()
		return 1, fmt.Errorf("unknown command: %s", command)
	}
}

func printValidationResult(name string, result validate.Result) (int, error) {
	if err := printResult(name, result); err != nil {
		return 1, err
	}
	return 0, nil
}

func readHookDecision(input io.Reader) (validate.HookDecision, error) {
	payload, err := io.ReadAll(input)
	if err != nil {
		return validate.HookDecision{}, err
	}
	if len(payload) == 0 {
		return validate.HookDecision{}, fmt.Errorf("hook payload is required on stdin")
	}
	return validate.Hook(payload)
}

func printResult(name string, result validate.Result) error {
	if result.OK() {
		fmt.Printf("PASS formal-gates %s validation\n", name)
		return nil
	}
	for _, failure := range result.Failures {
		fmt.Printf("FAIL %s: %s\n", failure.Path, failure.Message)
	}
	return fmt.Errorf("formal-gates %s validation failed with %d issue(s)", name, len(result.Failures))
}

func printUsage() {
	fmt.Println(`formal-gates-validate

Usage:
  formal-gates-validate package  --root <formal-gates>
  formal-gates-validate artifact --root <repo> --file <artifact> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot>
  formal-gates-validate hook     < payload.json

The portable validator performs deterministic package, artifact, and hook decision checks. It is not a workflow engine or hook runtime.`)
}
