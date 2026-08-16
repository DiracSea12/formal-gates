package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestHookResolvesDshAgentTypeFromLifecycleBinding(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".gates", "tmp", "run-1")
	state := map[string]any{
		"status": "ACTIVE",
		"actions": map[string]any{
			"development-worker": map[string]any{"status": developmentPrepared},
		},
		"dispatches": map[string]any{
			"dispatch-dev":      map[string]any{"target": "development-worker", "targetKind": "action"},
			"dispatch-reviewer": map[string]any{"target": "custom-gate", "targetKind": "gate"},
		},
	}
	writeStateJSON(t, runDir, state)
	lifecycleDir := filepath.Join(runDir, "lifecycle")
	if err := os.MkdirAll(lifecycleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeBindingJSON(t, filepath.Join(lifecycleDir, "dev.json"), "dispatch-dev", "dsh-agent-dev")
	writeBindingJSON(t, filepath.Join(lifecycleDir, "reviewer.json"), "dispatch-reviewer", "dsh-agent-reviewer")

	// DSH 载荷只有 agent_id；绑定反查应把 id 归一化为派发目标。
	dev := dshWritePayload(root, "dsh-agent-dev")
	if decision, err := Hook(dev); err != nil || decision.PermissionDecision != "allow" {
		t.Fatalf("development-worker via DSH binding should be allowed: %#v %v", decision, err)
	}
	reviewer := dshWritePayload(root, "dsh-agent-reviewer")
	if decision, err := Hook(reviewer); err != nil || decision.PermissionDecision != "deny" {
		t.Fatalf("gate reviewer via DSH binding should be blocked: %#v %v", decision, err)
	}
	// 没有绑定的 DSH 子代理不是审查类，维持"其余代理放行"的既有规则。
	other := dshWritePayload(root, "dsh-agent-other")
	if decision, err := Hook(other); err != nil || decision.PermissionDecision != "allow" {
		t.Fatalf("unbound DSH subagent should be allowed: %#v %v", decision, err)
	}
}

func writeStateJSON(t *testing.T, runDir string, state map[string]any) {
	t.Helper()
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBindingJSON(t *testing.T, path, dispatchID, identity string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"runId":      "run-1",
		"dispatchId": dispatchID,
		"provider":   "deepseek-harness",
		"identity":   identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dshWritePayload(root, identity string) []byte {
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_id":%q}`, root, filepath.ToSlash(filepath.Join(root, "internal", "code.go")), identity)
	return []byte(payload)
}
