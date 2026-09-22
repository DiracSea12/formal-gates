package harness

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
	if len(args) > 0 && args[0] == "supervise" {
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness supervise requires a job directory")
			return 2
		}
		return Supervise(args[1])
	}
	if len(args) > 0 && args[0] == "native" {
		// One native job, one process.
		// The entry's own argument check mirrors supervise's: exit 2 with a
		// note on stderr and no native-error.json.
		if len(args) < 2 {
			fmt.Fprintln(errout, "harness native requires a job directory")
			return 2
		}
		return RunNative(args[1], errout)
	}
	if len(args) > 0 && args[0] == "native-respond" {
		// Answer one held native interaction from the user's own shell.
		return NativeRespond(args[1:], errout)
	}
	if len(args) > 0 && args[0] == "native-show" {
		// Read-only listing of a job's held native interactions.
		return NativeShow(args[1:], out, errout)
	}
	exe, e := os.Executable()
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	root := os.Getenv("FORMAL_GATES_HARNESS_ROOT")
	if root == "" {
		for p := filepath.Dir(exe); p != filepath.Dir(p); p = filepath.Dir(p) {
			if st, e := os.Stat(filepath.Join(p, "runtime", "langgraph", "formal_gates_harness", "__main__.py")); e == nil && !st.IsDir() {
				root = p
				break
			}
		}
	}
	if root == "" {
		fmt.Fprintln(errout, "harness runtime not found; use scripts/harness setup")
		return 2
	}
	python := filepath.Join(root, ".harness", "venv", "bin", "python")
	if _, e := os.Stat(python); e != nil {
		python = filepath.Join(root, ".harness", "venv", "Scripts", "python.exe")
	}
	if _, e := os.Stat(python); e != nil {
		fmt.Fprintln(errout, "run scripts/harness setup first")
		return 2
	}
	cmd := exec.Command(python, append([]string{"-m", "formal_gates_harness"}, args...)...)
	cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "runtime", "langgraph"), "FORMAL_GATES_HARNESS_ROOT="+root, "FORMAL_GATES_HARNESS_BINARY="+exe, "LANGSMITH_TRACING=false", "LANGCHAIN_TRACING_V2=false")
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
