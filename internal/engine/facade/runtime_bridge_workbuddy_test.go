package facade

import (
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkBuddyBridgeRejectsOtherProviderBeforeProcess(t *testing.T) {
	f, action := operationFixture(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	claim := run.Executions[action].Claim
	job := filepath.Join(t.TempDir(), "must-not-create")
	if _, err := f.callNativeTurn(run, claim, false, job, job, "", map[string]any{"type": "object"}, "", false, workBuddyRuntimeAdapter{}); err == nil {
		t.Fatal("accepted Codex binding")
	}
	claim.Provider = "workbuddy"
	claim.Context = &protocol.HostRuntimeContext{Provider: "workbuddy", Executable: "/must-not-execute"}
	if _, err := f.callNativeTurn(run, claim, false, job, job, "", map[string]any{"type": "object"}, "", false, workBuddyRuntimeAdapter{}); err == nil {
		t.Fatal("accepted modified provider/context")
	}
	if _, err := os.Stat(job); !os.IsNotExist(err) {
		t.Fatalf("rejected binding created job: %v", err)
	}
}
