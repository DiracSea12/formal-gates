package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestExternalExecutorFlagsRequireExplicitAction(t *testing.T) {
	for _, args := range [][]string{
		{"--external-executor"},
		{"--external-executor", "--action-id", "a", "--resume"},
		{"--external-executor", "--action-id", "a", "--operation-request", "missing.json"},
	} {
		var out, stderr bytes.Buffer
		code := Run("formal-gates", append([]string{"workflow", "drive", "--root", t.TempDir()}, args...), IO{Stdout: &out, Stderr: &stderr})
		if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--external-executor") {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestContinuousFlagsRejectSingleActionModes(t *testing.T) {
	for _, args := range [][]string{{"--action-id", "a"}, {"--resume"}, {"--external-executor"}, {"--operation-request", "missing.json"}} {
		var out, stderr bytes.Buffer
		argv := append([]string{"workflow", "drive", "--continuous", "--root", t.TempDir()}, args...)
		if code := Run("formal-gates", argv, IO{Stdout: &out, Stderr: &stderr}); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--continuous") {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), stderr.String())
		}
	}
}
