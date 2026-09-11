package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHookPassesOwnerProviderToStartBridge(t *testing.T) {
	root := t.TempDir()
	payload := fmt.Sprintf(`{"tool_name":"Bash","tool_input":{"command":%q},"transcript_path":"/owner.jsonl","session_id":"owner"}`, "formal-gates workflow start --root "+fmt.Sprintf("%q", root)+" --split no")
	var out, errOut bytes.Buffer
	code := Run("formal-gates", []string{"hook", "decide", "--provider", "codex"}, IO{Stdin: strings.NewReader(payload), Stdout: &out, Stderr: &errOut})
	if code != 0 {
		t.Fatalf("hook failed: %s", errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".gates", "start-owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]string
	if err := json.Unmarshal(data, &source); err != nil || source["provider"] != "codex" || source["transcript"] != "/owner.jsonl" {
		t.Fatalf("lost explicit owner provider: %s, %v", data, err)
	}
}

func TestRunHookUsesCodexJSONDecisionProtocol(t *testing.T) {
	payload := `{"command":"formal-gates workflow record-gate --gate quality --status PASS --run-id run"}`

	var codexOut, codexErr bytes.Buffer
	codexCode := Run("formal-gates", []string{"hook", "decide", "--provider", "codex"}, IO{
		Stdin:  strings.NewReader(payload),
		Stdout: &codexOut,
		Stderr: &codexErr,
	})
	if codexCode != 0 {
		t.Fatalf("Codex denied hook should exit 0, code=%d stdout=%q stderr=%q", codexCode, codexOut.String(), codexErr.String())
	}
	if !strings.Contains(codexOut.String(), `"decision":"block"`) {
		t.Fatalf("Codex hook did not emit top-level decision:block: %q", codexOut.String())
	}
	if strings.Contains(codexOut.String(), `"hookSpecificOutput"`) || strings.Contains(codexOut.String(), `"permissionDecision"`) {
		t.Fatalf("Codex hook emitted the nested permission protocol: %q", codexOut.String())
	}

	var genericOut, genericErr bytes.Buffer
	genericCode := Run("formal-gates", []string{"hook", "decide"}, IO{
		Stdin:  strings.NewReader(payload),
		Stdout: &genericOut,
		Stderr: &genericErr,
	})
	if genericCode != 2 {
		t.Fatalf("generic denied hook should keep exit code 2, code=%d stdout=%q stderr=%q", genericCode, genericOut.String(), genericErr.String())
	}
}

func TestRunHookUsesZCodeNestedDecisionProtocol(t *testing.T) {
	payload := `{"tool_name":"Bash","tool_input":{"command":"formal-gates workflow record-gate --gate quality --status PASS"}}`
	var out, errOut bytes.Buffer
	code := Run("formal-gates", []string{"hook", "decide", "--provider", "zcode"}, IO{
		Stdin:  strings.NewReader(payload),
		Stdout: &out,
		Stderr: &errOut,
	})
	if code != 2 {
		t.Fatalf("ZCode denied hook should use documented exit-2 block shortcut, code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	for _, expected := range []string{`"hookSpecificOutput"`, `"hookEventName":"PreToolUse"`, `"permissionDecision":"deny"`} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("ZCode hook response missing %s: %q", expected, out.String())
		}
	}
}
