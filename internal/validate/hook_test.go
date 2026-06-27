package validate

import "testing"

func TestHookDenyGateWorkflowPassWithoutArtifact(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name: "native workflow missing artifact",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "\"C:\\tools\\formal-gates\\bin\\formal-gates.exe\" workflow record-stage --gate complexity-gate --verdict PASS --workflow-id wf --change-snapshot snap"
				}
			}`,
		},
		{
			name: "native gate record missing artifact",
			payload: `{
				"tool_name": "shell_command",
				"arguments": {
					"command": "bin/formal-gates gate record --gate architecture-health-gate --verdict PASS --workflow-id wf --change-snapshot snap"
				}
			}`,
		},
		{
			name: "go run native workflow missing artifact",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "go run ./cmd/formal-gates workflow record-stage --gate code-quality-gate --verdict PASS --workflow-id wf --change-snapshot snap"
				}
			}`,
		},
		{
			name: "duplicate verdict cannot hide pass",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "formal-gates workflow record-stage --gate complexity-gate --verdict REVIEW --verdict PASS --workflow-id wf --change-snapshot snap"
				}
			}`,
		},
		{
			name: "equals verdict and empty artifact",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "formal-gates workflow record-stage --gate=complexity-gate --verdict=PASS --artifact= --workflow-id=wf --change-snapshot=snap"
				}
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := Hook([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Decision != "block" {
				t.Fatalf("expected block, got %#v", decision)
			}
			if decision.Permission != "deny" || decision.PermissionDecision != "deny" {
				t.Fatalf("expected deny-compatible host fields, got %#v", decision)
			}
		})
	}
}

func TestHookRejectsLegacyPowerShellCommands(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name: "legacy workflow with artifact",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "powershell -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict PASS -Artifact .claude/gates/artifacts/complexity.md"
				}
			}`,
		},
		{
			name: "legacy workflow review",
			payload: `{
				"tool_name": "shell_command",
				"arguments": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict REVIEW"
				}
			}`,
		},
		{
			name: "legacy gate-state",
			payload: `{
				"tool_name": "shell_command",
				"params": {
					"cmd": "pwsh -File ./scripts/gate-state.ps1 -Action assert-next -Gate complexity-gate"
				}
			}`,
		},
		{
			name: "legacy receipt hook",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "pwsh -File ./hooks/capture-subagent-receipt.ps1"
				}
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := Hook([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Decision != "block" {
				t.Fatalf("expected block, got %#v", decision)
			}
			if decision.Reason == "" || decision.PermissionDecision != "deny" {
				t.Fatalf("expected legacy deny reason and host fields, got %#v", decision)
			}
		})
	}
}

func TestHookAllowsRepresentativePayloads(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name: "native workflow command with artifact",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "formal-gates workflow record-stage --gate complexity-gate --verdict PASS --artifact=.claude/gates/artifacts/complexity.md --workflow-id wf --change-snapshot snap"
				}
			}`,
		},
		{
			name: "cursor validation command",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "go run ./cmd/formal-gates package validate --root ."
				}
			}`,
		},
		{
			name:    "unknown payload",
			payload: `{"event":"PreToolUse","value":{"text":"not a command"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := Hook([]byte(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Decision != "approve" {
				t.Fatalf("expected approve, got %#v", decision)
			}
			if decision.Permission != "allow" || decision.PermissionDecision != "allow" {
				t.Fatalf("expected allow-compatible host fields, got %#v", decision)
			}
		})
	}
}

func TestHookAllowsMalformedNonCommandFailureIsNotHidden(t *testing.T) {
	_, err := Hook([]byte(`{`))
	if err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
}

func TestHookAcceptsUTF8BOMPayload(t *testing.T) {
	payload := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{
		"command": "formal-gates workflow record-stage --gate complexity-gate --verdict PASS --workflow-id wf --change-snapshot snap"
	}`)...)

	decision, err := Hook(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "block" || decision.PermissionDecision != "deny" {
		t.Fatalf("expected denied hook decision, got %#v", decision)
	}
}
