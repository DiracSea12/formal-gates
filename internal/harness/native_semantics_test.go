package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
)

func TestNativeSemanticSubmissionPreservesOriginalToolEvidence(t *testing.T) {
	workspace, directory := t.TempDir(), t.TempDir()
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: worker.OutputCodec, Handler: "engine.review.worker.v3", Provider: "dsh", Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Review: &protocol.WorkerReviewInput{Action: review.ProductReview, RequirementRevision: "confirmed"}}
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
	services := ExecutionServices{Admit: func(string, map[string]any) (func(), error) { return func() {}, nil }}
	call := func(arguments any) bool {
		t.Helper()
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeSemanticTool, "arguments": arguments}})
		if err != nil {
			t.Fatal(err)
		}
		var out, errout bytes.Buffer
		if code := NativeTools(directory, bytes.NewReader(wire), &out, &errout, services); code != 0 {
			t.Fatalf("tool exit %d: %s", code, &errout)
		}
		var reply map[string]any
		if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return getOf(getOf(reply, "result"), "isError") == false
	}
	value := map[string]any{"findings": []any{}}
	if !call(value) || !call(value) {
		t.Fatal("original and repeated submission rejected")
	}
	if call(map[string]any{"interruptionReason": "different result"}) {
		t.Fatal("same invocation changed its semantics")
	}
	if err := os.WriteFile(filepath.Join(directory, "native-source.json"), []byte(`{"sessionId":"original-session","turn":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	events := `{"sessionId":"original-session","event":{"type":"assistant/message","data":{"turn":1,"message":{"content":[{"type":"text","text":"Done; no JSON result."}]},"usage":{"inputTokens":23,"outputTokens":7}}}}` + "\n" + `{"sessionId":"original-session","event":{"type":"turn/end","data":{"turn":1,"reason":{"kind":"completed"}}}}` + "\n"
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
	expected, _ := json.Marshal(value)
	if !bytes.Equal(evidence.Answer, expected) {
		t.Fatalf("canonical semantic bytes differ: %s", evidence.Answer)
	}
	event, err := worker.DecodeResultWithFacts(input, "result", evidence.Answer, evidence.Digest, nil, evidence.LocationFacts)
	if err != nil {
		t.Fatal(err)
	}
	if event.Result.ActionID != input.Pending.ActionID || event.Result.Review.RequirementRevision != "confirmed" {
		t.Fatal("program-owned review binding changed")
	}
	before, err := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := observeNative(directory, "original-session")
	if err != nil || !valuesEqual(source, recovered) {
		t.Fatal("recovery changed original source", err)
	}
	after, _ := os.ReadFile(filepath.Join(directory, nativeToolCallsFile))
	if !bytes.Equal(before, after) {
		t.Fatal("recovery submitted new semantics")
	}
	if _, err := os.Stat(filepath.Join(directory, "answer.raw.txt")); !os.IsNotExist(err) {
		t.Fatal("prose answer path remains")
	}
	if err := os.WriteFile(filepath.Join(directory, nativeToolCallsFile), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NativeWorkerResultEvidence("dsh", directory, source); err == nil {
		t.Fatal("missing original tool proof accepted")
	}
}

func nativeSemanticToolFixture(t *testing.T, schema any) string {
	t.Helper()
	directory := t.TempDir()
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), map[string]any{"workspace": t.TempDir(), "read_only": true, "schema": schema}); err != nil {
		t.Fatal(err)
	}
	return directory
}

func nativeSemanticToolSchemaFixture(t *testing.T, schema any) (any, error) {
	t.Helper()
	directory := nativeSemanticToolFixture(t, schema)
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	var out, errout bytes.Buffer
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout); code != 0 {
		return nil, fmt.Errorf("tool list: %s", &errout)
	}
	reply, err := decodeNativeJSON(out.Bytes())
	if err != nil {
		return nil, err
	}
	for _, tool := range listOf(getOf(getOf(reply, "result"), "tools")) {
		if getOf(tool, "name") == nativeSemanticTool {
			return getOf(tool, "inputSchema"), nil
		}
	}
	return nil, fmt.Errorf("semantic tool was not declared")
}

func nativeSemanticToolRoundTrip(t *testing.T, schema, value any) (any, error) {
	t.Helper()
	directory := nativeSemanticToolFixture(t, schema)
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeSemanticTool, "arguments": value}})
	if err != nil {
		return nil, err
	}
	var out, errout bytes.Buffer
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout); code != 0 {
		return nil, fmt.Errorf("semantic tool: %s", &errout)
	}
	body, err := submittedSemantics(directory)
	if err != nil {
		return nil, err
	}
	return decodeNativeJSON(body)
}

// Scripted hosts invoke the original MCP boundary, never write result facts.
func peerSubmitSemantics(directory string, value any) {
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": nativeSemanticTool, "arguments": value}})
	if err != nil {
		panic(err)
	}
	var out, errout bytes.Buffer
	services := ExecutionServices{Admit: func(string, map[string]any) (func(), error) { return func() {}, nil }}
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout, services); code != 0 {
		panic(errout.String())
	}
	reply, err := decodeNativeJSON(out.Bytes())
	if err != nil {
		panic(err)
	}
	if getOf(getOf(reply, "result"), "isError") != false {
		panic(textOf(reply))
	}
}

func argsFlag(args []string, flag string) string {
	for i, v := range args {
		if v == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
