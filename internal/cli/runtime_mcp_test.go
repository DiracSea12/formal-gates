package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestMCPDriveFlagsRejectBeforeOpeningRun(t *testing.T) {
	for _, args := range [][]string{
		{"--operation-mcp"},
		{"--operation-mcp", "--action-id", "a"},
		{"--operation-mcp", "--action-id", "a", "--native-identity", "n", "--resume"},
		{"--operation-mcp", "--action-id", "a", "--native-identity", "n", "--operation-request", "missing.json"},
		{"--native-identity", "n"},
	} {
		var out, stderr bytes.Buffer
		code := Run("formal-gates", append([]string{"workflow", "drive", "--root", t.TempDir()}, args...), IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr})
		if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--operation-mcp") {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestNativeOperationDriveFlagsRejectBeforeOpeningRun(t *testing.T) {
	for _, args := range [][]string{
		{"--operation-native", "missing.json"},
		{"--operation-native", "missing.json", "--action-id", "a"},
		{"--operation-native", "missing.json", "--action-id", "a", "--native-identity", "n", "--resume"},
		{"--operation-native", "missing.json", "--action-id", "a", "--native-identity", "n", "--operation-request", "other.json"},
	} {
		var out, stderr bytes.Buffer
		code := Run("formal-gates", append([]string{"workflow", "drive", "--root", t.TempDir()}, args...), IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &stderr})
		if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "--operation-native") {
			t.Fatalf("args=%v code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
}
