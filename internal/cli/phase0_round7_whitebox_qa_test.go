package cli

// Independent whitebox QA for the stage-0 public mutation surface. Registry
// records are owned by the install transaction; the CLI must not expose a
// second registry writer or allow direct registry-path injection.

import (
	"bytes"
	"strings"
	"testing"
)

func TestWhiteboxPhase0Round7RegistryMutationSurfaceOwnedByInstaller(t *testing.T) {
	for _, subcommand := range []string{"bootstrap", "register"} {
		t.Run(subcommand, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", []string{"registry", subcommand, "--path", "/tmp/round7-registry.json"}, IO{Stdout: &stdout, Stderr: &stderr})
			if code == 0 || !strings.Contains(stderr.String(), "unknown registry subcommand") {
				t.Fatalf("removed registry writer %q was still callable: code=%d stdout=%s stderr=%s", subcommand, code, stdout.String(), stderr.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"registry", "--help"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("registry help failed: code=%d stderr=%s", code, stderr.String())
	}
	help := stdout.String()
	if strings.Contains(help, "\n  bootstrap ") || strings.Contains(help, "\n  register ") || !strings.Contains(help, "admit") || !strings.Contains(help, "show") {
		t.Fatalf("registry help exposes the wrong mutation surface: %s", help)
	}

	for _, command := range [][]string{{"install", "--help"}, {"uninstall", "--help"}, {"workflow", "start", "--help"}} {
		stdout.Reset()
		stderr.Reset()
		code = Run("formal-gates", command, IO{Stdout: &stdout, Stderr: &stderr})
		if code != 0 {
			t.Fatalf("%s help failed: code=%d stderr=%s", strings.Join(command, " "), code, stderr.String())
		}
		if strings.Contains(stdout.String(), "--registry") || strings.Contains(stdout.String(), "--registry-record") {
			t.Fatalf("%s still exposes direct registry injection: %s", strings.Join(command, " "), stdout.String())
		}
	}
}
