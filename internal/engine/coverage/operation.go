package coverage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// ShellCommand preserves approved shell text in the platform's command shell.
func ShellCommand(command string) []string {
	if runtime.GOOS == "windows" {
		shell := os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
		return []string{shell, "/D", "/S", "/C", command}
	}
	return []string{"/bin/sh", "-c", command}
}

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

// ValidateOperations checks authored invocation values without executing them.
func ValidateOperations(operations []Operation) error {
	return validateOperations(operations, "operations")
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

func validateCaseOperations(qaCase AcceptanceCase, path string) error {
	if err := validateOperations(qaCase.Operations, path+".operations"); err != nil {
		return err
	}
	if isGoTestRef(qaCase.TestRef) && len(qaCase.Operations) != 0 {
		return invalid(CodeInvalidCaseBinding, path+".operations", "Go test references derive their operation and cannot also declare operations")
	}
	return nil
}

func isGoTestRef(ref string) bool {
	file, _, found := strings.Cut(ref, "::")
	return found && strings.HasSuffix(file, "_test.go")
}

func validateTestFiles(files []string, path string) error {
	seen := map[string]bool{}
	for _, file := range files {
		if file == "." || !filepath.IsLocal(filepath.FromSlash(file)) || file != filepath.ToSlash(filepath.Clean(filepath.FromSlash(file))) || seen[file] {
			return invalid(CodeInvalidCaseBinding, path, "test files require unique repository-relative file paths")
		}
		seen[file] = true
	}
	return nil
}

// TestFiles projects only files associated with cases or their referenced
// shared steps; a delivery path declaration does not establish this relation.
func (manifest AcceptanceManifest) TestFiles() []string {
	files := map[string]struct{}{}
	for _, c := range manifest.Cases {
		for _, file := range c.Files {
			files[file] = struct{}{}
		}
		for _, step := range manifest.SharedSteps {
			if slices.Contains(c.SharedSteps, step.ID) {
				for _, file := range step.Files {
					files[file] = struct{}{}
				}
			}
		}
	}
	return sortedKeys(files)
}
