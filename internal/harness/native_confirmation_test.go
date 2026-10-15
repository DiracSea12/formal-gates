package harness

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeConfirmationAcceptsOnlyCurrentHostResponse(t *testing.T) {
	for _, test := range []struct {
		name, capabilities, response, problem string
		accepted                              bool
	}{
		{"accept", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"legacy form capability", `{"elicitation":{}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"decline", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"decline"}}`, "not executed", false},
		{"cancel", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"cancel"}}`, "not executed", false},
		{"missing capability", `{}`, "", "NATIVE_CONFIRMATION_UNAVAILABLE", false},
		{"url only", `{"elicitation":{"url":{}}}`, "", "NATIVE_CONFIRMATION_UNAVAILABLE", false},
		{"another identity", `{"elicitation":{"form":{}}}`, `{"id":"another-request","result":{"action":"accept","content":{"confirmOperation":true}}}`, "another request", false},
		{"request cannot answer", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","method":"tools/call","result":{"action":"accept","content":{"confirmOperation":true}}}`, "not received", false},
		{"disconnect", `{"elicitation":{"form":{}}}`, "", "not received", false},
		{"empty acceptance", `{"elicitation":{"form":{}}}`, `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{}}}`, "explicitly confirm", false},
		{"cancelled original call", `{"elicitation":{"form":{}}}`, `{"method":"notifications/cancelled","params":{"requestId":2}}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "not executed", false},
		{"another cancellation", `{"elicitation":{"form":{}}}`, `{"method":"notifications/cancelled","params":{"requestId":99}}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
		{"host ping", `{"elicitation":{"form":{}}}`, `{"id":"host-ping","method":"ping"}` + "\n" + `{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":` + test.capabilities + `}}` + "\n" +
				`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"confirm","arguments":{}}}` + "\n" + test.response + "\n"
			var out, errors bytes.Buffer
			executed := false
			code := ServeTools(strings.NewReader(wire), &out, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
				if name != "confirm" || string(arguments) != "{}" {
					t.Fatalf("unexpected original call: %s %s", name, arguments)
				}
				if err := confirm(map[string]any{"operation": "original operation", "revision": "original revision"}); err != nil {
					return nil, err
				}
				executed = true
				return map[string]any{"executed": true}, nil
			}, nil)
			if code != 0 || executed != test.accepted || errors.Len() != 0 {
				t.Fatalf("code=%d executed=%v stderr=%s stdout=%s", code, executed, errors.String(), out.String())
			}
			if test.problem != "" && !strings.Contains(out.String(), test.problem) {
				t.Fatal("missing rejection evidence", out.String())
			}
			if test.name == "host ping" && !strings.Contains(out.String(), `"id":"host-ping","jsonrpc":"2.0","result":{}`) {
				t.Fatal("host ping was not answered while waiting", out.String())
			}
			if test.name != "missing capability" && test.name != "url only" {
				var requests []map[string]any
				decoder := json.NewDecoder(&out)
				for decoder.More() {
					var message map[string]any
					if err := decoder.Decode(&message); err != nil {
						t.Fatal(err)
					}
					if message["method"] == "elicitation/create" {
						requests = append(requests, message)
					}
				}
				if len(requests) != 1 || requests[0]["id"] != "formal-gates-confirmation-1" || getOf(getOf(requests[0], "params"), "mode") != "form" {
					t.Fatal("confirmation did not use its original MCP request", requests)
				}
				message := textOf(getOf(getOf(requests[0], "params"), "message"))
				if !strings.Contains(message, "original operation") || !strings.Contains(message, "original revision") {
					t.Fatal("host did not receive the exact operation", message)
				}
			}
		})
	}
}

func TestNativeConfirmationKeepsOriginalCallWhileAnotherCallIsBusy(t *testing.T) {
	wire := `{"id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{"elicitation":{"form":{}}}}}` + "\n" +
		`{"id":2,"method":"tools/call","params":{"name":"original","arguments":{"operation":"first"}}}` + "\n" +
		`{"id":3,"method":"tools/call","params":{"name":"second","arguments":{"operation":"second"}}}` + "\n" +
		`{"id":4,"method":"ping"}` + "\n" +
		`{"id":"formal-gates-confirmation-1","result":{"action":"accept","content":{"confirmOperation":true}}}` + "\n"
	var output, errors bytes.Buffer
	calls := 0
	code := ServeTools(strings.NewReader(wire), &output, &errors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
		calls++
		if name != "original" || string(arguments) != `{"operation":"first"}` {
			t.Fatalf("another request replaced the original operation: %s %s", name, arguments)
		}
		if err := confirm(json.RawMessage(arguments)); err != nil {
			return nil, err
		}
		return "original accepted", nil
	}, nil)
	if code != 0 || calls != 1 || errors.Len() != 0 {
		t.Fatal(code, calls, errors.String())
	}
	responses := map[float64]map[string]any{}
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if id, ok := frame["id"].(float64); ok {
			responses[id] = frame
		}
	}
	if getOf(responses[2]["result"], "isError") != false || getOf(responses[3]["error"], "code") != float64(-32000) || responses[4]["result"] == nil {
		t.Fatal("pending confirmation did not preserve each caller's response", responses)
	}
}
