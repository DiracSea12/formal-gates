package validate

import "testing"

func TestHookDeniesPassWithoutCurrentRunBinding(t *testing.T) {
	for _, command := range []string{
		`formal-gates workflow record-gate --gate quality --status PASS --run-id run`,
		`formal-gates workflow record-gate --status PASS --run-id run --dispatch dispatch-1`,
		`formal-gates workflow record-gate --gate quality --status PASS --dispatch dispatch-1`,
		`formal-gates workflow record-gate --gate quality --status PASS --run-id run --live-snapshot current`,
	} {
		decision, err := Hook([]byte(`{"command":"` + command + `"}`))
		if err != nil {
			t.Fatal(err)
		}
		if decision.PermissionDecision != "deny" {
			t.Fatalf("command was allowed: %s", command)
		}
	}
}

func TestHookAllowsBoundPassAndOtherCommands(t *testing.T) {
	for _, payload := range []string{
		`{"command":"formal-gates workflow record-gate --gate quality --status PASS --run-id run --dispatch dispatch-1"}`,
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
	decision, err := Hook([]byte(`{"command":"formal-gates workflow record-gate --gate quality --status PASS --run-id run"}`))
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

func TestHookRejectsLegacyScriptCommands(t *testing.T) {
	for _, command := range []string{"pwsh -File scripts/gate-workflow.ps1", "pwsh -File hooks/capture-subagent-receipt.ps1"} {
		decision, err := Hook([]byte(`{"input":{"command":"` + command + `"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if decision.PermissionDecision != "deny" {
			t.Fatalf("legacy command was allowed: %s", command)
		}
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
