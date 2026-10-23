package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/host"
)

func TestHostWorkerManagedToolsRetainOriginalCallerAndCompletion(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	const child = "11111111-1111-4111-8111-111111111111"
	const parent = "22222222-2222-4222-8222-222222222222"
	const turn = "33333333-3333-4333-8333-333333333333"
	rollout := filepath.Join(t.TempDir(), "child.jsonl")
	appendRecord := func(kind string, payload any) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"type": kind, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(rollout, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(append(body, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(writeErr, closeErr)
		}
	}
	appendRecord("session_meta", map[string]any{"id": child, "source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "agent_path": "/root/worker"}}}})
	appendRecord("event_msg", map[string]any{"type": "task_started", "turn_id": turn})
	appendRecord("turn_context", map[string]any{"turn_id": turn, "model": "original-model", "effort": "high"})
	requestValue, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	request := objectOrEmpty(requestValue)
	request["provider"] = "codex"
	request["host"] = host.CodexInvocation{Transcript: rollout, Session: child, Parent: parent, Turn: turn}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	if _, err := InvokeHostWorkerTool(directory, "submit", parent, json.RawMessage(`{"findings":[]}`)); err == nil {
		t.Fatal("parent caller submitted another child's semantics")
	}
	body := json.RawMessage(`{"findings":[]}`)
	for range 2 {
		if _, err := InvokeHostWorkerTool(directory, "submit", child, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InvokeHostWorkerTool(directory, "submit", child, json.RawMessage(`{"findings":[{"message":"different"}]}`)); err == nil {
		t.Fatal("different repeated semantics replaced original body")
	}
	if _, err := HostWorkerResultSource(directory); err == nil {
		t.Fatal("running native child became a completed result")
	}
	appendRecord("event_msg", map[string]any{"type": "task_complete", "turn_id": turn})
	source, err := HostWorkerResultSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := HostWorkerResultEvidence("codex", directory, source)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Identity != child || string(evidence.Answer) != string(body) {
		t.Fatalf("original child/body changed: %+v", evidence)
	}
	if _, _, _, err := NativeFailureEvidence("codex", directory); err == nil {
		t.Fatal("successful submission became a missing-result failure")
	}
	for _, name := range []string{"owner", "process.json", "exit.json", "identity.json", "native-source.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("host worker fabricated independent process evidence: %s %v", name, err)
		}
	}
	repeated, err := HostWorkerResultSource(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !hostEvidenceEqual(source, repeated) {
		t.Fatal("observing completed host worker changed original source")
	}
	if _, err := InvokeHostWorkerTool(directory, "submit", child, body); err == nil {
		t.Fatal("completed native turn issued another tool call")
	}
	source["identity"] = parent
	if _, err := HostWorkerResultEvidence("codex", directory, source); err == nil {
		t.Fatal("different child identity admitted")
	}
}

func TestHostWorkerMissingSemanticsFailurePreservesSourceErrors(t *testing.T) {
	directory := nativeSemanticToolFixture(t, map[string]any{"type": "object"})
	const child = "11111111-1111-4111-8111-111111111111"
	const parent = "22222222-2222-4222-8222-222222222222"
	const turn = "33333333-3333-4333-8333-333333333333"
	path := filepath.Join(t.TempDir(), "child.jsonl")
	var rollout []byte
	for _, record := range []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": child, "source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent}}}}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": turn}},
		{"type": "turn_context", "payload": map[string]any{"turn_id": turn, "model": "original-model", "effort": "high"}},
	} {
		body, _ := json.Marshal(record)
		rollout = append(rollout, append(body, '\n')...)
	}
	if err := os.WriteFile(path, rollout, 0600); err != nil {
		t.Fatal(err)
	}
	value, err := readJSONFile(filepath.Join(directory, "native-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	request := objectOrEmpty(value)
	request["provider"], request["host"] = "codex", host.CodexInvocation{Transcript: path, Session: child, Parent: parent, Turn: turn}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NativeFailureEvidence("codex", directory); err == nil {
		t.Fatal("running turn became a completed failure")
	}
	complete, _ := json.Marshal(map[string]any{"type": "event_msg", "payload": map[string]any{"type": "task_complete", "turn_id": turn}})
	rollout = append(rollout, append(complete, '\n')...)
	if err := os.WriteFile(path, rollout, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := HostWorkerResultSource(directory); !errors.Is(err, ErrSemanticSubmissionMissing) {
		t.Fatal("missing result lost its exact failure", err)
	}
	first, digest, identity, err := NativeFailureEvidence("codex", directory)
	if err != nil || identity != child || digest == "" {
		t.Fatal(identity, digest, err)
	}
	second, repeated, _, err := NativeFailureEvidence("codex", directory)
	if err != nil || string(first) != string(second) || repeated != digest {
		t.Fatal("failure reentry changed original evidence", err)
	}
	if err := os.WriteFile(filepath.Join(directory, nativeToolCallsFile), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := HostWorkerResultSource(directory); err == nil || errors.Is(err, ErrSemanticSubmissionMissing) {
		t.Fatal("missing tool context became a model omission", err)
	}
	if _, _, _, err := NativeFailureEvidence("codex", directory); err == nil {
		t.Fatal("unavailable tool evidence became an accepted failure")
	}
	if err := os.Remove(filepath.Join(directory, nativeToolCallsFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, nativeToolContextFile), []byte(`{"facts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := NativeFailureEvidence("codex", directory); err == nil {
		t.Fatal("lost original tool calls became a model omission")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := HostWorkerResultSource(directory); err == nil || errors.Is(err, ErrSemanticSubmissionMissing) {
		t.Fatal("lost native source became a model omission", err)
	}
	for _, name := range []string{"process.json", "exit.json", "native-error.json", "source.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatal("failure fabricated independent evidence", name, err)
		}
	}
}
