package validate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	engineRuntime "formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/host"
)

func TestHostWorkerHookKeepsManagedToolBoundaryAcrossDirectories(t *testing.T) {
	f := newHostHookFixture(t)
	for _, tc := range []struct {
		name, tool, command string
		input               any
		want                string
	}{
		{"native patch", "functions.apply_patch", "", "*** Begin Patch\n*** End Patch", "deny"},
		{"native Python", "functions.exec_command", "python -c \"open('file.py','w').write('x')\"", nil, "deny"},
		{"native PowerShell", "functions.exec_command", "Set-Content -LiteralPath file.py -Value x", nil, "deny"},
		{"native read", "functions.exec_command", "Get-Content file.py", nil, "deny"},
		{"managed worker", "mcp__formal_gates__worker", "", map[string]any{"operation": "workspace"}, "allow"},
		{"managed semantic", "mcp__formal-gates__submit_semantics", "", map[string]any{}, "allow"},
		{"native control confirmation", "mcp__formal_gates__confirm_control", "", map[string]any{}, "allow"},
		{"native interaction confirmation", "mcp__formal_gates__confirm_native", "", map[string]any{}, "allow"},
		{"code mode container", "functions.exec", "", "await tools.mcp__formal_gates__worker({operation:'workspace'})", "allow"},
		{"code mode wait", "functions.wait", "", map[string]any{"cell_id": "original"}, "allow"},
		{"managed CLI", "functions.exec_command", `formal-gates workflow worker --operation workspace --root "project"`, nil, "allow"},
		{"PowerShell managed CLI", "functions.exec_command", `& 'C:/Program Files/formal-gates/formal-gates.exe' workflow worker --material 'C:/project/.gates/engine/run/materials/original.json' --operation workspace --arguments '{"action":"comparison"}'`, nil, "allow"},
		{"PowerShell managed submit", "functions.exec_command", `  & 'C:/tools/formal-gates.exe' workflow worker --operation submit --arguments '{"message":"quoted | data & text; retained"}'`, nil, "allow"},
		{"PowerShell other executable", "functions.exec_command", `& 'C:/tools/python.exe' -c "print(1)"`, nil, "deny"},
		{"PowerShell repeated call operator", "functions.exec_command", `& & 'C:/tools/formal-gates.exe' workflow worker --operation workspace`, nil, "deny"},
		{"PowerShell leading conjunction", "functions.exec_command", `&& 'C:/tools/formal-gates.exe' workflow worker --operation workspace`, nil, "deny"},
		{"PowerShell CLI then conjunction", "functions.exec_command", `& 'C:/tools/formal-gates.exe' workflow worker --operation workspace && Get-Date`, nil, "deny"},
		{"PowerShell CLI then semicolon", "functions.exec_command", `& 'C:/tools/formal-gates.exe' workflow worker --operation workspace; Get-Date`, nil, "deny"},
		{"PowerShell CLI then pipeline", "functions.exec_command", `& 'C:/tools/formal-gates.exe' workflow worker --operation workspace | Set-Content file.py`, nil, "deny"},
		{"PowerShell CLI redirected", "functions.exec_command", `& 'C:/tools/formal-gates.exe' workflow worker --operation workspace > file.py`, nil, "deny"},
		{"CLI followed by write", "functions.exec_command", `formal-gates workflow worker --operation workspace; Set-Content file.py x`, nil, "deny"},
		{"CLI redirected to code", "functions.exec_command", `formal-gates workflow worker --operation workspace > file.py`, nil, "deny"},
		{"other CLI command", "functions.exec_command", `formal-gates harness native job`, nil, "deny"},
		{"unrelated MCP workspace", "mcp__other__workspace", "", map[string]any{}, "deny"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := f.payload(tc.tool, tc.command, tc.input)
			decision, err := Hook(payload, "codex")
			if err != nil || decision.PermissionDecision != tc.want {
				t.Fatalf("tool=%s: decision=%+v, error=%v, want=%s", tc.tool, decision, err, tc.want)
			}
		})
	}
}

func TestHostWorkerHookDoesNotTreatOtherChildrenOrReferencesAsBindings(t *testing.T) {
	for _, change := range []string{"other turn", "other entity", "reference only", "other attempt", "terminal run", "parent only"} {
		t.Run(change, func(t *testing.T) {
			f := newHostHookFixture(t)
			switch change {
			case "other turn":
				f.turn = "another-turn"
			case "other entity":
				f.writeTranscript("22222222-2222-4222-8222-222222222222", f.parent)
			case "reference only":
				delete(f.state.SpawnReceipts, "issued-worker")
				f.saveState()
			case "other attempt":
				p := f.state.PendingActions["issued-worker"]
				p.AttemptID = "another-attempt"
				f.state.PendingActions["issued-worker"] = p
				f.saveState()
			case "terminal run":
				f.state.Phase = engineRuntime.PhaseTerminal
				f.saveState()
			case "parent only":
				f.writeTranscript(f.parent, "")
			}
			decision, err := Hook(f.payload("functions.exec_command", "Set-Content file.py x", nil), "codex")
			if err != nil || decision.PermissionDecision != "allow" {
				t.Fatalf("unbound child was restricted: decision=%+v error=%v", decision, err)
			}
		})
	}
}

type hostHookFixture struct {
	t                                          *testing.T
	root, cwd, transcript, child, parent, turn string
	state                                      *protocol.State
	statePath                                  string
}

func newHostHookFixture(t *testing.T) *hostHookFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	f := &hostHookFixture{t: t, root: t.TempDir(), cwd: t.TempDir(), transcript: filepath.Join(t.TempDir(), "rollout.jsonl"), child: "11111111-1111-4111-8111-111111111111", parent: "33333333-3333-4333-8333-333333333333", turn: "native-turn"}
	f.writeTranscript(f.child, f.parent)
	writeCurrentHookFixture(t, f.root, "host-hook", true, false, "", f.parent)
	f.statePath = filepath.Join(f.root, ".gates", "engine", "host-hook", "state.json")
	var err error
	f.state, err = readHookEngineState(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	pending := protocol.PendingAction{ActionID: "issued-worker", AttemptID: "issued-attempt"}
	f.state.PendingActions[pending.ActionID] = pending
	f.state.SpawnReceipts[pending.ActionID] = protocol.SpawnReceipt{ActionID: pending.ActionID, Provider: "codex", Correlation: f.child, LifecycleIdentity: f.child, Status: protocol.SpawnStatusSpawned}
	f.saveState()
	material := worker.Material{Input: protocol.WorkerInput{RunID: "host-hook", Provider: "codex", Pending: pending}, Prompt: "Original issued worker", ReadOnly: false}
	body, _ := json.Marshal(material)
	materialPath := filepath.Join(filepath.Dir(f.statePath), "materials", "issued.json")
	f.write(materialPath, body)
	ref := facade.WorkerMaterial{Path: materialPath, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(body)), ActionID: pending.ActionID, AttemptID: pending.AttemptID, ReadOnly: false}
	job := facade.WorkerExecutionDirectory(ref, "")
	f.json(filepath.Join(job, "native-request.json"), map[string]any{"provider": "codex", "host": host.CodexInvocation{Transcript: f.transcript, Session: f.child, Parent: f.parent, Turn: f.turn}, "workspace": f.root, "read_only": false, "engine": map[string]any{"root": f.root, "runId": "host-hook", "workerMaterial": ref}})
	stateRoot := filepath.Join(home, ".gates")
	f.json(filepath.Join(home, ".formal-gates", "registry.json"), RegistryDocument{SchemaVersion: RegistrySchemaVersion, Records: []RegistryRecord{{ID: "codex-global", Host: "codex", Status: "active", StateRoot: stateRoot}}})
	f.json(filepath.Join(stateRoot, "host-workers", f.child+".json"), map[string]string{"job": job})
	return f
}

func (f *hostHookFixture) payload(tool, command string, input any) []byte {
	f.t.Helper()
	if command != "" {
		input = map[string]any{"cmd": command}
	}
	body, _ := json.Marshal(map[string]any{"cwd": f.cwd, "session_id": f.parent, "transcript_path": f.transcript, "turn_id": f.turn, "tool_name": tool, "tool_input": input, "agent_type": "development-worker"})
	return body
}

func (f *hostHookFixture) writeTranscript(child, parent string) {
	source := any("vscode")
	if parent != "" {
		source = map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "agent_path": "/root/worker"}}}
	}
	first, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": child, "source": source}})
	inherited, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": f.parent, "source": "vscode"}})
	start, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": f.turn}})
	context, _ := json.Marshal(map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": f.turn, "model": "native-model", "effort": "high"}})
	f.write(f.transcript, []byte(fmt.Sprintf("%s\n%s\n%s\n%s\n", first, inherited, start, context)))
}

func (f *hostHookFixture) saveState() {
	body, err := os.ReadFile(f.statePath)
	if err != nil {
		f.t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		f.t.Fatal(err)
	}
	envelope["content"], _ = json.Marshal(f.state)
	f.json(f.statePath, envelope)
}

func (f *hostHookFixture) json(path string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	f.write(path, body)
}

func (f *hostHookFixture) write(path string, body []byte) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		f.t.Fatal(err)
	}
}
