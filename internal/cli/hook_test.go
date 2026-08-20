package cli

import (
	"bytes"
	"strings"
	"testing"
)

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
