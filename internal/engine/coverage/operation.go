package coverage

import (
	"fmt"
	"strings"
)

// OperationKind is the closed invocation domain authored in a QA manifest.
type OperationKind string

const (
	OperationArgv  OperationKind = "ARGV"
	OperationShell OperationKind = "SHELL"
)

func OperationKinds() []string {
	return []string{string(OperationArgv), string(OperationShell)}
}

// Operation preserves approved argument values or shell text and optional input.
// Its workspace is supplied by the registered QA work binding.
type Operation struct {
	Kind    OperationKind `json:"kind"`
	Argv    []string      `json:"argv,omitempty"`
	Command string        `json:"command,omitempty"`
	Stdin   string        `json:"stdin,omitempty"`
}

func validateOperations(operations []Operation, path string) error {
	for i, operation := range operations {
		valid := false
		switch operation.Kind {
		case OperationArgv:
			valid = len(operation.Argv) > 0 && strings.TrimSpace(operation.Argv[0]) != "" && operation.Command == ""
		case OperationShell:
			valid = strings.TrimSpace(operation.Command) != "" && len(operation.Argv) == 0
		}
		if !valid {
			return invalid(CodeInvalidCaseBinding, fmt.Sprintf("%s[%d]", path, i), "operation requires exactly ARGV with a nonempty executable or SHELL with a nonempty command")
		}
	}
	return nil
}

func isGoTestRef(ref string) bool {
	file, _, found := strings.Cut(ref, "::")
	return found && strings.HasSuffix(file, "_test.go")
}
