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
	// Codex 的 PreToolUse 响应是 hookSpecificOutput.permissionDecision（deny），
	// 不是旧式 decision:"block"。
	if !strings.Contains(codexOut.String(), `"permissionDecision":"deny"`) {
		t.Fatalf("Codex hook did not emit permissionDecision:deny: %q", codexOut.String())
	}
	if !strings.Contains(codexOut.String(), `"hookSpecificOutput"`) {
		t.Fatalf("Codex hook did not wrap in hookSpecificOutput: %q", codexOut.String())
	}
	if strings.Contains(codexOut.String(), `"decision":"`) {
		t.Fatalf("Codex hook emitted legacy decision field: %q", codexOut.String())
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
