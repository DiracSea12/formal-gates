package coverage_test

import (
	"os/exec"
	"runtime"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/harness"
)

func TestShellCommandExecutesApprovedTextOnCurrentPlatform(t *testing.T) {
	command, expected := "printf '%s' 'two words'", "two words"
	if runtime.GOOS == "windows" {
		command, expected = "echo two words", "two words\r\n"
	}
	argv := coverage.ShellCommand(command)
	cmd := exec.Command(argv[0], argv[1:]...)
	harness.ApplyNativeCommandLine(cmd)
	body, err := cmd.Output()
	if err != nil || string(body) != expected {
		t.Fatalf("platform shell execution: %q %v", body, err)
	}
}
