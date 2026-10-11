package coverage

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestShellCommandExecutesApprovedTextOnCurrentPlatform(t *testing.T) {
	command, expected := "printf '%s' 'two words'", "two words"
	if runtime.GOOS == "windows" {
		command, expected = "echo two words", "two words\r\n"
	}
	argv := ShellCommand(command)
	cmd := exec.Command(argv[0], argv[1:]...)
	body, err := cmd.Output()
	if err != nil || string(body) != expected {
		t.Fatalf("platform shell execution: %q %v", body, err)
	}
}
