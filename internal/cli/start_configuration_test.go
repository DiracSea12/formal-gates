package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func saveLightweightConfiguration(t *testing.T, root string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "轻量", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "轻量", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: protocol.FlowNodeIDs()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "轻量", nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowStartRejectsPerRunNodeParameters(t *testing.T) {
	for _, flags := range [][]string{{"--nodes"}, {"--disable-node", "1"}, {"--precheck", "--disable-node", "1"}} {
		root := t.TempDir()
		var out, stderr bytes.Buffer
		args := append([]string{"workflow", "start", "--root", root}, flags...)
		if code := Run("formal-gates", args, IO{Stdout: &out, Stderr: &stderr}); code == 0 {
			t.Fatal("start accepted a per-run node override", flags)
		}
		if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
			t.Fatal("removed parameter wrote state", err)
		}
	}
}

func TestStartConfirmationRejectsDisabledNodesBeforeHostConfirmation(t *testing.T) {
	called := false
	_, err := callConfirmationTool("confirm_start", json.RawMessage(`{"intake":1,"disabledNodes":[1]}`), IO{}, func(any) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("removed disabledNodes reached host confirmation", err)
	}
}
