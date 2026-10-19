package facade

import (
	"os/exec"
	"strings"
	"testing"
)

func nativePython(t testing.TB) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		output, err := exec.Command(path, "--version").CombinedOutput()
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(output)), "Python 3.") {
			return path
		}
	}
	t.Fatal("fixture requires a runnable Python 3 interpreter (python3 or python)")
	return ""
}
