package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"formal-gates/internal/engine/protocol"
	"strings"
	"testing"
)

func TestNativeMCPTaskBoundary(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"requestId":"stable-1","kind":"qa.target","caseId":"CASE-1","prompt":""}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"requestId":"stable-1","kind":"qa.target","caseId":"CASE-1","prompt":""}}}
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"kind":"qa.prepare"}}}
{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"shell","arguments":{"requestId":"bad"}}}
{"jsonrpc":"2.0","method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"requestId":"notification"}}}
`
	var out bytes.Buffer
	var ids []string
	err := serveNativeOperationMCP(strings.NewReader(input), &out, "blackbox-execution", "operations", func(id string, raw json.RawMessage) (any, error) {
		ids = append(ids, id)
		var args map[string]string
		if err := json.Unmarshal(raw, &args); err != nil {
			t.Fatal(err)
		}
		if args["kind"] != "qa.target" || args["caseId"] != "CASE-1" {
			t.Fatalf("changed arguments: %s", raw)
		}
		if len(ids) > 1 {
			return nil, errors.New("DUPLICATE_ACTION: already claimed")
		}
		return map[string]string{"status": "COMPLETED"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "stable-1" || ids[1] != ids[0] {
		t.Fatalf("dispatch IDs=%v", ids)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("responses=%s", out.String())
	}
	var responses []map[string]json.RawMessage
	for _, line := range lines {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, msg)
	}
	if !bytes.Contains(responses[0]["result"], []byte(`"protocolVersion":"2025-03-26"`)) {
		t.Fatal(string(responses[0]["result"]))
	}
	if !bytes.Contains(responses[1]["result"], []byte(`"requestId"`)) {
		t.Fatal("missing stable operation ID schema")
	}
	if !bytes.Contains(responses[2]["result"], []byte(`"isError":false`)) {
		t.Fatal("success lost")
	}
	if !bytes.Contains(responses[3]["result"], []byte(`DUPLICATE_ACTION`)) || !bytes.Contains(responses[3]["result"], []byte(`"isError":true`)) {
		t.Fatal("engine rejection lost")
	}
	if len(responses[4]["error"]) == 0 || len(responses[5]["error"]) == 0 {
		t.Fatal("invalid calls accepted")
	}
}

func TestNativeMCPRejectsBrokenTransport(t *testing.T) {
	for _, input := range []string{"{", `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`} {
		err := serveNativeOperationMCP(strings.NewReader(input), &bytes.Buffer{}, "development-worker", "", func(string, json.RawMessage) (any, error) { t.Fatal("unexpected operation"); return nil, nil })
		if err == nil {
			t.Fatal("broken transport accepted")
		}
	}
}

func TestNativeMCPFacadeBinding(t *testing.T) {
	f, action := operationFixture(t)
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	identity := run.Executions[action].Identity
	for _, pair := range [][2]string{{"unknown", identity}, {action, ""}, {action, "wrong"}} {
		var out bytes.Buffer
		err := f.ServeNativeOperationMCP(pair[0], pair[1], strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), &out)
		if err == nil || out.Len() != 0 {
			t.Fatalf("unbound server err=%v output=%s", err, out.String())
		}
	}
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"requestId":"before-prepare","kind":"qa.target","caseId":"BLACKBOX:C1","prompt":""}}}`
	if err := f.ServeNativeOperationMCP(action, identity, strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PREREQUISITE_FAILED") || !strings.Contains(out.String(), `"isError":true`) {
		t.Fatalf("engine rejection not returned: %s", out.String())
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Executions[action].Operations) != 0 {
		t.Fatal("rejected call created operation")
	}
}

func TestNativeMCPBeforeAttachCannotExecute(t *testing.T) {
	f := frozenVerificationFixture(t, false)
	execution := facadeExecution(t, f, "blackbox-execution")
	preparationSubmit(t, f, protocol.RegularEvent{Operation: "CLAIM", Claim: &execution.Claim})
	var out bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"formal_gates_operation","arguments":{"requestId":"early","kind":"qa.target","caseId":"BLACKBOX:C1","prompt":""}}}`
	if err := f.ServeNativeOperationMCP(execution.Claim.ActionID, "requested-session", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"tools"`) || !strings.Contains(out.String(), "INTENT_MISMATCH") {
		t.Fatalf("bootstrap=%s", out.String())
	}
	after, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	if after.Executions[execution.Claim.ActionID].Identity != "" || len(after.Executions[execution.Claim.ActionID].Operations) != 0 {
		t.Fatal("requested identity became observed evidence")
	}
}
