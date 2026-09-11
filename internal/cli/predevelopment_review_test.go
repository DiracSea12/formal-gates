package cli

import (
	"bytes"
	"encoding/json"
	"os"

	"strings"
	"testing"
)

func cliP1FindingEvidence(location string, gate bool) []string {
	args := []string{
		"--severity-rationale", "The documented normal path is blocked, so this finding independently warrants P1.",
		"--category", "REQUIREMENT_CONFLICT",
		"--requirement", "REQ-001",
		"--normal-path", "run the documented public workflow from a clean checkout",
		"--deferral-impact", "users remain blocked from completing the documented workflow",
		"--verify-scope", "checked the complete requested scope",
		"--verify-severity", "confirmed that the normal path is blocked",
		"--verify-causality", "traced the observed failure to the cited evidence",
		"--verify-reproduction", "reproduced the failure on the current candidate",
		"--verify-classification", "confirmed this is a requirement conflict rather than an implementation preference",
		"--location", location,
	}
	if gate {
		args = append(args, "--actual-error", "the public command exits nonzero before producing the required result")
	}
	return args
}

func failingCLI(t *testing.T, args ...string) string {
	t.Helper()
	clearHostEnv(t)
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatalf("expected CLI failure for %s, got stdout=%s", strings.Join(args, " "), stdout.String())
	}
	return stderr.String()
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
