package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableCanaryPassesAgainstRepoRoot(t *testing.T) {
	root := repoRootForCanaryTest(t)
	report, result := PortableCanary(PortableCanaryOptions{Root: root})
	if !result.OK() {
		t.Fatalf("expected portable canary to pass, report=%#v failures=%#v", report, result.Failures)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected canary checks")
	}
	for _, check := range report.Checks {
		if check.Status != "PASS" {
			t.Fatalf("expected all checks to pass, got %#v", check)
		}
	}
}

func TestCodexHookProbeRecordsAndDeniesInvalidPass(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "formal-hook-output.txt")
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":"formal-gates workflow record-stage --gate complexity-gate --verdict PASS --workflow-id wf --change-snapshot snap"}}`)

	probe, result := CodexHookProbe(CodexHookProbeOptions{
		PayloadDir:       dir,
		FormalHookOutput: output,
		Payload:          payload,
	})
	if !result.OK() {
		t.Fatalf("expected hook probe to pass, got %#v", result.Failures)
	}
	if probe.ExitCode != 2 || probe.Decision == nil || probe.Decision.Decision != "deny" {
		t.Fatalf("expected denied hook decision, got %#v", probe)
	}
	if _, err := os.Stat(filepath.FromSlash(probe.PayloadPath)); err != nil {
		t.Fatalf("expected payload artifact: %v", err)
	}
	text, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "exit=2") || !strings.Contains(string(text), `"decision":"deny"`) {
		t.Fatalf("unexpected hook output: %q", text)
	}
}
