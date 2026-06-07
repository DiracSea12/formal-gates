package main

import (
	"flag"
	"fmt"
	"os"

	"formal-gates/internal/validate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
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
			return err
		}
		return printResult("package", validate.Package(*root))
	case "artifact":
		fs := flag.NewFlagSet("artifact", flag.ContinueOnError)
		root := fs.String("root", ".", "repository root for relative artifact references")
		file := fs.String("file", "", "artifact file to validate")
		gate := fs.String("gate", "", "gate id")
		workflowID := fs.String("workflow-id", "", "expected workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "expected change snapshot")
		stage := fs.String("stage", "", "expected QA stage, when relevant")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return printResult("artifact", validate.Artifact(validate.ArtifactOptions{
			Root:           *root,
			File:           *file,
			Gate:           *gate,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Stage:          *stage,
		}))
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", command)
	}
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

The portable validator performs deterministic package and artifact checks. It is not a workflow engine or hook runtime.`)
}
