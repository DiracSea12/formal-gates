package facade

import (
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestWorkBuddyRecoveryUsesNativePrintSessionStore(t *testing.T) {
	root := t.TempDir()
	binding := &protocol.HostRuntimeContext{Provider: "workbuddy", Environment: map[string]string{"CODEBUDDY_CONFIG_DIR": root}}
	identity := "native-session"
	project := filepath.Join(root, "projects", "portable-project-key")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if ok, err := workBuddySessionRecordAvailable(binding, identity); err != nil || ok {
		t.Fatalf("missing record reported available: %v %v", ok, err)
	}
	if err := os.WriteFile(filepath.Join(project, identity+".jsonl"), []byte("native record\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := workBuddySessionRecordAvailable(binding, identity); err != nil || !ok {
		t.Fatalf("native record unavailable: %v %v", ok, err)
	}
	method, raw, err := (workBuddyRuntimeAdapter{}).probe(nativeProbe{Context: binding, Identity: identity})
	if err != nil || method != "session/store" || len(raw) == 0 {
		t.Fatalf("probe=%s %s %v", method, raw, err)
	}
}
