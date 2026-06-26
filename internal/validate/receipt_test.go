package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiptCaptureSavesLifecycleEvent(t *testing.T) {
	dir := t.TempDir()
	event, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "codex",
		Event:    "SubagentStart",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"dispatch-1"}`),
	})
	if !result.OK() {
		t.Fatalf("expected capture to pass, got %#v", result.Failures)
	}
	if event.NormalizedEvent != "subagent_start" {
		t.Fatalf("unexpected normalized event: %#v", event)
	}
	if !strings.HasPrefix(event.EventArtifact, ".claude/gates/proofs/events/") {
		t.Fatalf("unexpected event artifact path: %q", event.EventArtifact)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(event.EventArtifact))); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptCaptureRejectsUnknownProviderAndEvent(t *testing.T) {
	dir := t.TempDir()
	if _, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "unknown",
		Event:    "SubagentStart",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"dispatch-1"}`),
	}); result.OK() {
		t.Fatal("expected unknown provider to fail")
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{
		Worktree: dir,
		Provider: "codex",
		Event:    "TaskStarted",
		Payload:  []byte(`{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"dispatch-1"}`),
	}); result.OK() {
		t.Fatal("expected unknown event to fail")
	}
}

func TestReceiptFinalizeAndValidate(t *testing.T) {
	dir := t.TempDir()
	artifact := writeReceiptArtifactFixture(t, dir)
	dispatch, result := ReceiptRegisterDispatch(ReceiptRegisterOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if !result.OK() {
		t.Fatalf("expected dispatch registration to pass, got %#v", result.Failures)
	}
	capturePayload := `{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(capturePayload)}); !result.OK() {
		t.Fatalf("expected start capture to pass, got %#v", result.Failures)
	}
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: []byte(capturePayload)}); !result.OK() {
		t.Fatalf("expected stop capture to pass, got %#v", result.Failures)
	}
	receipt, result := ReceiptFinalize(ReceiptFinalizeOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if !result.OK() {
		t.Fatalf("expected receipt finalize to pass, got %#v", result.Failures)
	}
	result = ReceiptValidate(ReceiptValidateOptions{
		Worktree:       dir,
		Receipt:        receipt.ReceiptArtifact,
		Artifact:       artifact,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "snap",
	})
	if !result.OK() {
		t.Fatalf("expected receipt validation to pass, got %#v", result.Failures)
	}
	result = ReceiptValidate(ReceiptValidateOptions{
		Worktree:       dir,
		Receipt:        receipt.ReceiptArtifact,
		Artifact:       artifact,
		Gate:           "complexity-gate",
		WorkflowID:     "wf",
		ChangeSnapshot: "other-snapshot",
	})
	if result.OK() {
		t.Fatal("expected snapshot mismatch to fail")
	}
}

func TestReceiptFinalizeMissingLifecycleIsUnproven(t *testing.T) {
	dir := t.TempDir()
	artifact := writeReceiptArtifactFixture(t, dir)
	dispatch, result := ReceiptRegisterDispatch(ReceiptRegisterOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if !result.OK() {
		t.Fatalf("expected dispatch registration to pass, got %#v", result.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","stage":"","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
	if _, result := ReceiptCapture(ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: []byte(payload)}); !result.OK() {
		t.Fatalf("expected start capture to pass, got %#v", result.Failures)
	}
	_, result = ReceiptFinalize(ReceiptFinalizeOptions{
		Worktree:   dir,
		Provider:   "codex",
		WorkflowID: "wf",
		Gate:       "complexity-gate",
		Artifact:   artifact,
	})
	if result.OK() {
		t.Fatal("expected missing stop event to be unproven")
	}
	if len(result.Failures) == 0 || !strings.Contains(result.Failures[0].Message, "UNPROVEN") {
		t.Fatalf("expected UNPROVEN failure, got %#v", result.Failures)
	}
}

func TestReceiptPreflightReadsNativeHookConfig(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(dir, ".codex", "hooks.json")
	mustWrite(t, config, `{
  "hooks": {
    "SubagentStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"formal-gates\" receipt capture --provider codex --event SubagentStart --worktree ."
          }
        ]
      }
    ],
    "SubagentStop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "\"formal-gates\" receipt capture --provider codex --event SubagentStop --worktree ."
          }
        ]
      }
    ]
  }
}`)
	report, result := ReceiptPreflight(ReceiptPreflightOptions{Host: "codex", Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected preflight diagnostic to pass, got %#v", result.Failures)
	}
	if report.Status != "UNSUPPORTED_HOST_RECEIPT" {
		t.Fatalf("unexpected status: %#v", report)
	}
	if report.ConfigPath == "" || !strings.Contains(report.ConfigPath, ".codex/hooks.json") {
		t.Fatalf("expected config path, got %#v", report)
	}
	if len(report.ConfiguredLifecycleHooks["SubagentStart"]) != 1 || len(report.ConfiguredLifecycleHooks["SubagentStop"]) != 1 {
		t.Fatalf("expected lifecycle hook commands, got %#v", report.ConfiguredLifecycleHooks)
	}
	for _, missing := range report.Missing {
		if strings.Contains(missing, "receipt capture hook") {
			t.Fatalf("did not expect hook-missing diagnostic when config contains hooks: %#v", report.Missing)
		}
	}
}

func TestReceiptPreflightReportsMissingHookConfig(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	report, result := ReceiptPreflight(ReceiptPreflightOptions{Host: "cursor", Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected preflight diagnostic to pass, got %#v", result.Failures)
	}
	if report.ConfigPath != "" {
		t.Fatalf("expected no config path, got %#v", report)
	}
	if len(report.Missing) == 0 || !strings.Contains(strings.Join(report.Missing, "\n"), "Cursor hooks.json") {
		t.Fatalf("expected missing Cursor config diagnostic, got %#v", report.Missing)
	}
}

func writeReceiptArtifactFixture(t *testing.T, dir string) string {
	t.Helper()
	bundle := filepath.Join(dir, "bundle.md")
	prompt := filepath.Join(dir, "prompt.md")
	artifact := filepath.Join(dir, "complexity.md")
	mustWrite(t, bundle, "bundle")
	mustWrite(t, prompt, "formal_gate_dispatch: true\n")
	text := complexityArtifactText(
		"wf",
		"snap",
		"bundle.md sha256="+sha256FileForTest(t, bundle),
		"prompt.md sha256="+sha256FileForTest(t, prompt),
		"",
	)
	mustWrite(t, artifact, text)
	return artifact
}
