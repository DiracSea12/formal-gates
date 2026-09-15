package dogfood

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// RunCommand keeps the temporary Python runner behind a Go-owned boundary.
// Ordinary Go commands do not need Python installed.
func RunCommand(args []string, in io.Reader, out, errout io.Writer) int {
	if len(args) > 0 && args[0] == "bridge" {
		return Bridge(in, out)
	}
	exe, e := os.Executable()
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	root := os.Getenv("FORMAL_GATES_DOGFOOD_ROOT")
	if root == "" {
		for p := filepath.Dir(exe); p != filepath.Dir(p); p = filepath.Dir(p) {
			if st, e := os.Stat(filepath.Join(p, "runtime", "langgraph", "formal_gates_dogfood", "__main__.py")); e == nil && !st.IsDir() {
				root = p
				break
			}
		}
	}
	if root == "" {
		fmt.Fprintln(errout, "dogfood runtime not found; use scripts/dogfood setup")
		return 2
	}
	python := filepath.Join(root, ".dogfood", "venv", "bin", "python")
	if _, e := os.Stat(python); e != nil {
		python = filepath.Join(root, ".dogfood", "venv", "Scripts", "python.exe")
	}
	if _, e := os.Stat(python); e != nil {
		fmt.Fprintln(errout, "run scripts/dogfood setup first")
		return 2
	}
	cmd := exec.Command(python, append([]string{"-m", "formal_gates_dogfood"}, args...)...)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "runtime", "langgraph"), "FORMAL_GATES_DOGFOOD_ROOT="+root, "FORMAL_GATES_DOGFOOD_BINARY="+exe, "LANGSMITH_TRACING=false", "LANGCHAIN_TRACING_V2=false")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errout
	if e := cmd.Run(); e != nil {
		if ee, ok := e.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(errout, e)
		return 1
	}
	return 0
}
