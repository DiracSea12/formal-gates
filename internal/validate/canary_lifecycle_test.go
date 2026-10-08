package validate

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

func TestPortableZCodeLifecycleUsesIssuedToolCallIdentity(t *testing.T) {
	root := t.TempDir()
	input := protocol.WorkerInput{RunID: "portable-zcode", Provider: host.ZCode, Handler: "engine.qa.execution", Pending: protocol.PendingAction{ActionID: "issued-action"}}
	payload, err := portableLifecyclePayload(root, input)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(payload, &values); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"agent_id": input.Pending.ActionID, "parentConversationId": input.RunID, "generationId": input.Pending.ActionID, "subagentType": input.Handler, "task": input.Pending.ActionID, "cwd": root, "tool_name": "Agent", "tool_use_id": input.Pending.ActionID}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("issued lifecycle context changed: %v", values)
	}
	if err := lifecycle.BeginRun(root, input.RunID); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.BindDispatchWithProvider(root, input.RunID, input.Pending.ActionID, input.Pending.ActionID, input.Provider); err != nil {
		t.Fatal(err)
	}
	delete(values, "tool_use_id")
	missingToolID, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Capture(root, input.Provider, "PreToolUse", missingToolID); err == nil || err.Error() != "zcode PreToolUse payload is missing a host agent identity" {
		t.Fatalf("agent identity replaced the required tool-call identity: %v", err)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		captured, err := lifecycle.Capture(root, input.Provider, event, payload)
		if err != nil || captured.Identity != input.Pending.ActionID || captured.Provider != input.Provider || len(captured.Roots) != 1 {
			t.Fatalf("host event %s lost its issued identity: %+v %v", event, captured, err)
		}
	}
	duplicate, err := lifecycle.Capture(root, input.Provider, "PreToolUse", payload)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate changed the original host event: %+v %v", duplicate, err)
	}
	verification, err := lifecycle.VerifyDispatch(root, input.RunID, input.Pending.ActionID)
	if err != nil || verification.Outcome != lifecycle.Verified || verification.Identity != input.Pending.ActionID || !verification.StartObserved || !verification.StopObserved {
		t.Fatalf("issued dispatch lacks its paired native events: %+v %v", verification, err)
	}
}
