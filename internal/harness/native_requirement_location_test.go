package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
)

func TestNativeRequirementFindingUsesOriginalReadAcrossSourceRecovery(t *testing.T) {
	workspace := t.TempDir()
	directory := filepath.Join(t.TempDir(), "action", "call")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	physical := []byte("Physical file differs from the confirmed requirement\n")
	if err := os.WriteFile(filepath.Join(workspace, "requirements.md"), physical, 0600); err != nil {
		t.Fatal(err)
	}
	document := requirement.FrozenRequirementProjection{Path: "requirements.md", Content: "# Frozen requirement\r\nOriginal behavior.  \r\n"}
	input := protocol.WorkerInput{Handler: "engine.review.worker.v2", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Review: &protocol.WorkerReviewInput{Action: review.ProductReview, RequirementRevision: "original-revision"}, RequirementDocument: document}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	materialPath := filepath.Join(directory, "material.json")
	if err := writeAtomicJSON(materialPath, worker.Material{Input: input, ReadOnly: true, ResultSchema: schema}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"provider": "dsh", "workspace": workspace, "read_only": true, "schema": schema, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}}); err != nil {
		t.Fatal(err)
	}
	call := func(name string, arguments any) any {
		t.Helper()
		request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(request), &out, &errout, ExecutionServices{Admit: nativeToolAdmission}); code != 0 {
			t.Fatalf("original MCP exit %d: %s", code, &errout)
		}
		reply, err := decodeNativeJSON(out.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return getOf(reply, "result")
	}
	read := call(nativeWorkspaceTool, map[string]any{"action": "requirement", "from": 2, "to": 2})
	var published struct {
		Read int `json:"read"`
	}
	if getOf(read, "isError") != false || json.Unmarshal([]byte(textOf(getOf(listOf(getOf(read, "content"))[0], "text"))), &published) != nil || published.Read != 1 {
		t.Fatal("original requirement read was not published", read)
	}
	value := worker.Review{Findings: []worker.Finding{{Severity: "P3", Message: "Frozen wording needs clarification", Locations: []worker.Location{{Read: published.Read, Line: 3}}}}, Verification: []string{}}
	for range review.RequiredVerificationChecks() {
		value.Verification = append(value.Verification, "Inspected the exact frozen original requirement")
	}
	invalid := call(nativeSemanticTool, value)
	if getOf(invalid, "isError") != true {
		t.Fatal("invalid finding location was published before mechanical assembly", invalid)
	}
	if _, err := submittedSemantics(directory); err == nil {
		t.Fatal("rejected location manufactured accepted semantic facts")
	}
	value.Findings[0].Locations[0].Line = 2
	for range 2 {
		if result := call(nativeSemanticTool, value); getOf(result, "isError") != false {
			t.Fatal("original frozen finding or exact replay rejected", result)
		}
	}
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
	contextBefore := readTestFileBytes(t, filepath.Join(directory, nativeToolContextFile))
	materialBefore := readTestFileBytes(t, materialPath)
	for range 2 {
		evidence, err := NativeWorkerResultEvidence("dsh", directory, source)
		if err != nil {
			t.Fatal("original source verifier rejected unchanged source", err)
		}
		if evidence.LocationFacts[0].Kind != "requirement" || len(evidence.LocationFacts[0].Paths) != 0 || string(evidence.LocationFacts[0].Content) != document.Content {
			t.Fatal("original requirement fact gained physical permission or changed bytes")
		}
		before, _ := json.Marshal(evidence.LocationFacts)
		event, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, evidence.LocationFacts)
		if err != nil || !reflect.DeepEqual(event.Result.Review.Findings[0].Locations, []string{"requirements.md:2"}) {
			t.Fatal("verified original read failed finding assembly", err)
		}
		after, _ := json.Marshal(evidence.LocationFacts)
		if !bytes.Equal(before, after) {
			t.Fatal("assembler mutated original verified facts")
		}
		if _, err := newNativeWorkspaceTools(directory); err != nil {
			t.Fatal("reopened original published read lost its identity", err)
		}
		if !bytes.Equal(contextBefore, readTestFileBytes(t, filepath.Join(directory, nativeToolContextFile))) || !bytes.Equal(materialBefore, readTestFileBytes(t, materialPath)) {
			t.Fatal("recovery rewrote original fact or frozen material evidence")
		}
	}
	if actual, err := os.ReadFile(filepath.Join(workspace, "requirements.md")); err != nil || !bytes.Equal(actual, physical) {
		t.Fatal("semantic requirement read edited the physical document", err)
	}
}
