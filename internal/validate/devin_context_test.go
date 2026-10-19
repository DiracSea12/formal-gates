package validate

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestDevinHookUsesNativeProjectDirectory(t *testing.T) {
	root := t.TempDir()
	writeCurrentHookFixture(t, root, "hook", true, true, "", "owner")
	t.Setenv("DEVIN_PROJECT_DIR", root)
	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "write",
		"tool_input":      map[string]any{"file_path": filepath.Join(root, "app.go")},
		"session_id":      "owner",
		"prompt_id":       "native-prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := Hook(payload, "devin")
	if err != nil || decision.Decision != "block" || HookExitCode("devin", decision) != 2 {
		t.Fatalf("native project directory did not protect the active run: %+v %v", decision, err)
	}
}

func TestDevinHookExplicitDirectoryPrecedesEnvironment(t *testing.T) {
	root := t.TempDir()
	writeCurrentHookFixture(t, root, "hook", true, true, "", "owner")
	t.Setenv("DEVIN_PROJECT_DIR", root)
	other := t.TempDir()
	payload, err := json.Marshal(map[string]any{
		"cwd":        other,
		"tool_name":  "write",
		"tool_input": map[string]any{"file_path": filepath.Join(other, "app.go")},
		"session_id": "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"devin", "codex"} {
		decision, err := Hook(payload, provider)
		if err != nil || decision.Decision != "approve" {
			t.Fatalf("%s ignored the explicit project directory: %+v %v", provider, decision, err)
		}
	}
	nativePayload := map[string]any{
		"tool_name":  "write",
		"tool_input": map[string]any{"file_path": filepath.Join(root, "app.go")},
		"session_id": "owner",
	}
	payload, err = json.Marshal(nativePayload)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := Hook(payload, "codex")
	if err != nil || decision.Decision != "approve" {
		t.Fatalf("Devin environment leaked into another provider: %+v %v", decision, err)
	}
}
