package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexHookProbeRecordsWithoutMakingHookDecision(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":"formal-gates workflow record-gate --gate complexity-gate --status PASS --run-id wf"}}`)

	probe, result := CodexHookProbe(CodexHookProbeOptions{
		PayloadDir: dir,
		Payload:    payload,
	})
	if !result.OK() {
		t.Fatalf("expected hook probe to pass, got %#v", result.Failures)
	}
	if probe.ExitCode != 0 {
		t.Fatalf("expected passive recorder to exit successfully, got %#v", probe)
	}
	if _, err := os.Stat(filepath.FromSlash(probe.PayloadPath)); err != nil {
		t.Fatalf("expected payload artifact: %v", err)
	}
}
