package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeepSeekLifecycleCaptureAndVerification(t *testing.T) {
	for _, alias := range []string{ProviderDeepSeek, "dsh", "deepseek-harness"} {
		if adapter, err := adapterFor(alias); err != nil || adapter.Name != ProviderDeepSeek {
			t.Fatalf("adapterFor(%q)=%+v,%v", alias, adapter, err)
		}
	}
	root := t.TempDir()
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"agent_id": "dsh-agent-1", "cwd": root}
	data, _ := json.Marshal(payload)
	if _, err := Capture(root, ProviderDeepSeek, "SubagentStart", data); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatchWithProvider(root, "run-1", "dispatch-1", "dsh-agent-1", ProviderDeepSeek); err != nil {
		t.Fatal(err)
	}
	payload["stop_reason"] = "HTTP 429 rate limited"
	data, _ = json.Marshal(payload)
	if _, err := Capture(root, ProviderDeepSeek, "SubagentStop", data); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified || verification.Provider != ProviderDeepSeek || !verification.StartObserved || !verification.StopObserved {
		t.Fatalf("expected VERIFIED DeepSeek pairing, got %+v", verification)
	}
	reason, err := DispatchInterruptionReason(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "429") {
		t.Fatalf("DeepSeek stop reason was not recorded: %q", reason)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "tmp", "run-1")); err != nil {
		t.Fatalf("run artifacts missing: %v", err)
	}
}
