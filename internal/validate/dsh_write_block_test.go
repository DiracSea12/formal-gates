package validate

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	engineRuntime "formal-gates/internal/engine/runtime"
	"os"
	"path/filepath"
	"testing"
)

func TestHookResolvesDshAgentTypeFromLifecycleBinding(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".gates", "tmp", "run-1")
	writeCurrentHookFixture(t, root, "run-1", true, true, "", "")
	statePath := filepath.Join(root, ".gates", "engine", "run-1", "state.json")
	state, err := readHookEngineState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []struct{ id, handler string }{{"dispatch-dev", "engine.development.batch.v4"}, {"dispatch-reviewer", "engine.gate.v2"}} {
		for _, step := range cd.Steps {
			agent, ok := step.Payload.(compiler.CompiledAgentStep)
			if ok && string(agent.Handler) == wanted.handler {
				state.SpawnReceipts[wanted.id] = protocol.SpawnReceipt{ActionID: wanted.id, Provider: "dsh", Correlation: wanted.id, Status: protocol.SpawnStatusSpawned, Task: engineRuntime.TaskKey{Node: "scope", Step: step.Header.ID}}
				break
			}
		}
	}
	store, err := persistence.NewStore(filepath.Dir(statePath), persistence.Config{PackageDigest: "sha256:hook-fixture", DefinitionSource: "definitions/workflow.json", InstalledTargetIdentity: "hook-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(persistence.Transaction{ExpectedRevision: snap.Revision, ExpectedFingerprint: "fixture", CollectFingerprint: func() (string, error) { return "fixture", nil }, Content: state}); err != nil {
		t.Fatal(err)
	}
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
