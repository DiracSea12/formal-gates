package validate

import "testing"

func TestHookDenyGateWorkflowPassWithoutArtifact(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{
			name: "claude missing artifact switch",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict PASS -WorkflowId wf -ChangeSnapshot snap"
				}
			}`,
		},
		{
			name: "codex missing artifact switch",
			payload: `{
				"tool_name": "shell_command",
				"arguments": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate architecture-health-gate -Verdict PASS"
				}
			}`,
		},
		{
			name: "cursor missing artifact switch",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "powershell -File .cursor/formal-gates/scripts/gate-workflow.ps1 -Action record-stage -Gate code-quality-gate -Verdict PASS"
				}
			}`,
		},
		{
			name: "artifact switch without value",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict PASS -Artifact -Actor dev"
				}
			}`,
		},
		{
			name: "colon action and verdict missing artifact",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action:record-stage -Gate:complexity-gate -Verdict:PASS"
				}
			}`,
		},
		{
			name: "space action and colon verdict missing artifact",
			payload: `{
				"tool_name": "shell_command",
				"arguments": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict:PASS"
				}
			}`,
		},
		{
			name: "colon action and space verdict missing artifact",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action:record-stage -Gate complexity-gate -Verdict PASS"
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
			if decision.Decision != "deny" {
				t.Fatalf("expected deny, got %#v", decision)
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
			name: "claude command with artifact",
			payload: `{
				"tool_name": "Bash",
				"tool_input": {
					"command": "powershell -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict PASS -Artifact .claude/gates/artifacts/complexity.md"
				}
			}`,
		},
		{
			name: "codex non-pass command",
			payload: `{
				"tool_name": "shell_command",
				"arguments": {
					"command": "pwsh -File ./scripts/gate-workflow.ps1 -Action record-stage -Gate complexity-gate -Verdict REVIEW"
				}
			}`,
		},
		{
			name: "codex gate-state command",
			payload: `{
				"tool_name": "shell_command",
				"params": {
					"cmd": "pwsh -File ./scripts/gate-state.ps1 -Action assert-next -Gate complexity-gate"
				}
			}`,
		},
		{
			name: "cursor validation command",
			payload: `{
				"tool_name": "Shell",
				"input": {
					"command": "go run ./cmd/formal-gates-validate package --root ."
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
			if decision.Decision != "allow" {
				t.Fatalf("expected allow, got %#v", decision)
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
