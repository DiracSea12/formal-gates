package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
)

func TestNativeReadOwnsDeclaredProjectCheckCommand(t *testing.T) {
	directory, workspace := nativeToolFixture(t)
	command := "./checks/project-check --label \"原命令 two words\" ''\nprintf 'next-line\\n'\n"
	if err := os.Mkdir(filepath.Join(workspace, "checks"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "checks", "project-check"), []byte("#!/bin/sh\nprintf '<%s>\\n' \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("Project checks:\n"+command), 0600); err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v4", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	material := worker.Material{Input: input, ReadOnly: true, ResultSchema: schema}
	materialPath := filepath.Join(directory, "material.json")
	if err := writeAtomicJSON(materialPath, material); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": "dsh", "workspace": workspace, "read_only": true, "schema": schema, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	call := func(name string, arguments any) map[string]any {
		t.Helper()
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(wire), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatalf("original MCP: %d %s", code, &errout)
		}
		reply, err := decodeNativeJSON(out.Bytes())
		if err != nil || getOf(getOf(reply, "result"), "isError") != false {
			t.Fatalf("original tool rejected: %s %v", &out, err)
		}
		content := listOf(getOf(getOf(reply, "result"), "content"))
		value, err := decodeNativeJSON([]byte(textOf(getOf(content[0], "text"))))
		if err != nil {
			t.Fatal(err)
		}
		return objectOrEmpty(value)
	}
	listing := call(nativeWorkspaceTool, map[string]any{"action": "list"})
	entry := 0
	for _, value := range listOf(listing["entries"]) {
		if getOf(value, "name") == "README.md" {
			entry = intOf(getOf(value, "entry"))
		}
	}
	if entry == 0 {
		t.Fatal("original project README not published")
	}
	read := call(nativeWorkspaceTool, map[string]any{"action": "read", "listing": intOf(listing["listing"]), "entry": entry})
	number := intOf(read["read"])
	verification := []string{}
	for range review.RequiredVerificationChecks() {
		verification = append(verification, "Explicit fixture evidence for the current project and plan")
	}
	checks := []any{}
	for _, purpose := range []string{protocol.CheckEdit, protocol.CheckFullBuild, protocol.CheckFullTests} {
		checks = append(checks, map[string]any{"read": number, "from": 2, "to": 3, "purpose": purpose})
	}
	batch := map[string]any{"subtasks": []any{map[string]any{"description": "Deliver the public command"}}, "deliverable": "Public command", "publicEntry": "Project command", "artifacts": []any{map[string]any{"read": number}}, "interfaces": "Public arguments", "stateBoundary": "Project source", "risk": "Keep existing behavior", "dod": []any{map[string]any{"description": "Public behavior matches the oracle"}}, "checks": checks, "rollback": "Restore project source", "recovery": "Continue the original task", "handoff": "Original command checks are available", "contextCost": "One capability"}
	value := map[string]any{"findings": []any{}, "verification": verification, "granularityReview": map[string]any{"reason": "One independent capability", "verificationTier": "FINAL", "batches": []any{batch}}}
	call(nativeSemanticTool, value)
	if err := os.WriteFile(filepath.Join(directory, "native-source.json"), []byte(`{"sessionId":"original-session","turn":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	events := `{"sessionId":"original-session","event":{"type":"assistant/message","data":{"turn":1,"message":{"content":[{"type":"text","text":"Done"}]},"usage":{"inputTokens":23,"outputTokens":7}}}}` + "\n" + `{"sessionId":"original-session","event":{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, "native-events.jsonl"), []byte(events), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := observeNative(directory, "original-session")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NativeWorkerResultEvidence("dsh", directory, source)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, evidence.LocationFacts)
	if err != nil {
		t.Fatal(err)
	}
	plan := event.Result.Review.GranularityReview
	if err := protocol.ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	for _, check := range plan.Batches[0].Checks {
		if check.Command != command {
			t.Fatalf("original command bytes changed: %q", check.Command)
		}
	}
	argv := coverage.ShellCommand(plan.Batches[0].Checks[0].Command)
	actual := exec.Command(argv[0], argv[1:]...)
	actual.Dir = workspace
	out, err := actual.CombinedOutput()
	if err != nil || string(out) != "<--label>\n<原命令 two words>\n<>\nnext-line\n" {
		t.Fatalf("original approved invocation: %q %v", out, err)
	}
	recovered, err := observeNative(directory, "original-session")
	if err != nil || !reflect.DeepEqual(source, recovered) {
		t.Fatal("native recovery changed source", err)
	}
	stored, _ := json.Marshal(evidence.LocationFacts)
	var facts []worker.LocationFact
	if err := json.Unmarshal(stored, &facts); err != nil {
		t.Fatal(err)
	}
	replayed, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, facts)
	if err != nil || !reflect.DeepEqual(event, replayed) {
		t.Fatal("source-bound replay changed command", err)
	}
	if err := os.WriteFile(filepath.Join(directory, nativeToolCallsFile), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NativeWorkerResultEvidence("dsh", directory, source); err == nil {
		t.Fatal("lost original read/tool evidence admitted")
	}
}
