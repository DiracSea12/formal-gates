package lifecycle

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestLifecycleCaptureAndVerification(t *testing.T) {
	root := t.TempDir()
	payload := []byte(`{"payload":{"agent_id":"agent-1"}}`)
	if _, err := Capture(root, ProviderClaude, "SubagentStop", payload); err != nil {
		t.Fatal(err)
	}
	start, err := Capture(root, ProviderClaude, "SubagentStart", payload)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := Capture(root, ProviderClaude, "SubagentStart", payload)
	if err != nil {
		t.Fatal(err)
	}
	if start.Event != eventStart || !duplicate.Duplicate {
		t.Fatalf("unexpected capture results: start=%+v duplicate=%+v", start, duplicate)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", ProviderClaude, "agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified || !verification.StartObserved || !verification.StopObserved {
		t.Fatalf("expected VERIFIED, got %+v", verification)
	}
}

func TestLifecycleRequiredProviderRejectsMissingOrMismatchedEvents(t *testing.T) {
	root := t.TempDir()
	if err := BindDispatch(root, "run-1", "dispatch-1", ProviderCursor, "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderCursor, "subagentStart", []byte(`{"agentId":"other-agent"}`)); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Rejected || verification.StartObserved || verification.StopObserved {
		t.Fatalf("expected missing matching events to reject, got %+v", verification)
	}
}

func TestLifecycleCodexIsUnavailable(t *testing.T) {
	root := t.TempDir()
	if err := BindDispatch(root, "run-1", "dispatch-1", ProviderCodex, "agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Unavailable {
		t.Fatalf("expected UNAVAILABLE, got %+v", verification)
	}
}

func TestProviderFromExecutable(t *testing.T) {
	tests := map[string]string{
		filepath.Join("tmp", ".claude", "skills", "formal-gates", "bin", "formal-gates"): ProviderClaude,
		filepath.Join("tmp", ".codex", "skills", "formal-gates", "bin", "formal-gates"):  ProviderCodex,
		filepath.Join("tmp", ".cursor", "formal-gates", "bin", "formal-gates"):           ProviderCursor,
		filepath.Join("tmp", "source", "formal-gates"):                                   ProviderCodex,
	}
	for path, want := range tests {
		t.Run(fmt.Sprintf("%s", want), func(t *testing.T) {
			if got := ProviderFromExecutable(path); got != want {
				t.Fatalf("ProviderFromExecutable(%q)=%q want %q", path, got, want)
			}
		})
	}
}
