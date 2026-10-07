package validate

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestHookAllowsNonWriteCommands(t *testing.T) {
	for _, payload := range []string{
		`{"command":"go test ./..."}`,
		`{"event":"PreToolUse","value":{"text":"not a command"}}`,
	} {
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if decision.PermissionDecision != "allow" {
			t.Fatalf("payload was denied: %s %#v", payload, decision)
		}
	}
}

func TestHookExitCodeUsesCodexJSONDecisionProtocol(t *testing.T) {
	root := t.TempDir()
	writeCurrentHookFixture(t, root, "hook", true, true, "", "owner")
	decision, err := Hook([]byte(fmt.Sprintf(`{"cwd":%q,"session_id":"owner","tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.Join(root, "app.go"))))
	if err != nil {
		t.Fatal(err)
	}
	if got := HookExitCode("", decision); got != 2 {
		t.Fatalf("generic denied hook should exit 2, got %d", got)
	}
	if got := HookExitCode("codex", decision); got != 0 {
		t.Fatalf("Codex denied hook should exit 0 so its JSON block is consumed, got %d", got)
	}
	allowed, err := Hook([]byte(`{"command":"go test ./..."}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := HookExitCode("codex", allowed); got != 0 {
		t.Fatalf("Codex allowed hook should exit 0, got %d", got)
	}
}

func TestHookReportsMalformedJSON(t *testing.T) {
	if _, err := Hook([]byte(`{`)); err == nil {
		t.Fatal("malformed hook payload was accepted")
	}
}

func TestHookAcceptsUTF8BOM(t *testing.T) {
	payload := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"command":"go test ./..."}`)...)
	decision, err := Hook(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "allow" {
		t.Fatalf("BOM payload denied: %#v", decision)
	}
}
