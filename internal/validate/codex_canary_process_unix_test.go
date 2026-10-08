//go:build !windows

package validate

import (
	"os"
	"path/filepath"
	"testing"
)

// The CLI process owns its terminal exit and original streams, including a
// quiet interval after it acknowledges the prompt.
func TestCodexCanaryWaitsForItsProcessExit(t *testing.T) {
	directory := t.TempDir()
	command := filepath.Join(directory, "codex")
	script := `#!/bin/sh
[ "$1" = exec ] || exit 91
case "$*" in *"--json"*"--profile"*"canary-profile"*) ;; *) exit 92 ;; esac
[ "$(cat)" = "original prompt" ] || exit 93
printf 'accepted\n'
sleep 0.1
printf 'actual stderr\n' >&2
exit 7
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(directory, "prompt.txt")
	stdout, stderr := filepath.Join(directory, "stdout"), filepath.Join(directory, "stderr")
	if err := os.WriteFile(prompt, []byte("original prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, err := runCodexCanary(command, "--profile", "canary-profile", directory, prompt, stdout, stderr, filepath.Join(directory, "final"))
	if err != nil || code != 7 {
		t.Fatalf("actual process terminal: code=%d err=%v", code, err)
	}
	for path, expected := range map[string]string{stdout: "accepted\n", stderr: "actual stderr\n"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != expected {
			t.Fatalf("actual stream %s: %q %v", path, data, err)
		}
	}
}
