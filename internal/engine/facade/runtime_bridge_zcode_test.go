package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestZCodeSessionNativeExchange(t *testing.T) {
	for _, failAttach := range []bool{false, true} {
		var inbound, outbound bytes.Buffer
		encode := json.NewEncoder(&inbound)
		for _, msg := range []any{
			map[string]any{"id": "request", "result": map[string]any{"session": map[string]any{"sessionId": "native"}}},
			map[string]any{"id": "request:subscribe", "result": map[string]any{"sessionId": "native", "events": []any{}}},
			map[string]any{"id": "request:send", "result": map[string]any{"sessionId": "native", "accepted": true}},
			map[string]any{"method": "session/event", "params": map[string]any{"sessionId": "native", "seq": 1, "turnId": "turn", "type": "turn.started", "payload": map[string]any{"inputId": "input"}}},
			map[string]any{"method": "session/event", "params": map[string]any{"sessionId": "native", "seq": 2, "turnId": "turn", "type": "turn.completed", "payload": map[string]any{"inputId": "input", "resultType": "success", "response": `{"status":"CONTINUE"}`}}},
			map[string]any{"id": "request:close", "result": map[string]any{}},
		} {
			if err := encode.Encode(msg); err != nil {
				t.Fatal(err)
			}
		}
		request, _ := zcodeSessionRequest("request", "/work", "", []string{})
		called := false
		identity, answer, err := runZCodeSession(&inbound, &outbound, request, "input", "bound prompt", func(id string) error {
			called = true
			if id != "native" || strings.Contains(outbound.String(), "session/send") {
				t.Fatal("input sent before native identity attachment")
			}
			if failAttach {
				return errors.New("engine refused attachment")
			}
			return nil
		})
		if !called {
			t.Fatal("native identity was not observed")
		}
		if failAttach {
			if err == nil || strings.Contains(outbound.String(), "session/send") {
				t.Fatal("attachment refusal did not stop input")
			}
			continue
		}
		if err != nil || identity != "native" || string(answer) != `{"status":"CONTINUE"}` {
			t.Fatalf("%s %s %v", identity, answer, err)
		}
		decoder := json.NewDecoder(&outbound)
		for _, method := range []string{"session/create", "session/subscribe", "session/send", "session/close"} {
			var r zcodeRequest
			if err := decoder.Decode(&r); err != nil || r.Method != method {
				t.Fatalf("request=%+v err=%v want=%s", r, err, method)
			}
			if method == "session/send" && (r.Params["inputId"] != "input" || r.Params["content"] != "bound prompt") {
				t.Fatal("input binding changed")
			}
		}
	}
}

func TestZCodeAcceptedIsNotCompleted(t *testing.T) {
	input := `{"id":"request","result":{"session":{"sessionId":"native"}}}
{"id":"request:subscribe","result":{"sessionId":"native","events":[]}}
{"id":"request:send","result":{"sessionId":"native","accepted":true}}
`
	request, _ := zcodeSessionRequest("request", "/work", "", []string{})
	var out bytes.Buffer
	_, answer, err := runZCodeSession(strings.NewReader(input), &out, request, "input", "prompt", func(string) error { return nil })
	if err == nil || answer != nil {
		t.Fatal("queued input reported complete")
	}
}

func TestZCodeBoundToolsConfigurationAndUsage(t *testing.T) {
	var in, out bytes.Buffer
	enc := json.NewEncoder(&in)
	snapshot := map[string]any{"session": map[string]any{"sessionId": "native"}, "settings": map[string]any{"model": map[string]any{"current": map[string]any{"providerId": "provider"}}}}
	replies := []struct {
		id     string
		result any
	}{
		{"request", snapshot}, {"request:configure-close", map[string]any{}}, {"request:configure-resume", snapshot},
		{"request:mode", map[string]any{}}, {"request:model", map[string]any{}}, {"request:effort", map[string]any{}},
		{"request:subscribe", map[string]any{"sessionId": "native", "events": []any{}}},
		{"request:send", map[string]any{"sessionId": "native", "accepted": true}},
	}
	for _, r := range replies {
		if err := enc.Encode(map[string]any{"id": r.id, "result": r.result}); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []map[string]any{
		{"sessionId": "native", "seq": 1, "turnId": "turn", "type": "turn.started", "payload": map[string]any{"inputId": "input"}},
		{"sessionId": "native", "seq": 2, "turnId": "turn", "type": "turn.completed", "payload": map[string]any{"inputId": "input", "resultType": "success", "response": `{"output":"done"}`}},
	} {
		if err := enc.Encode(map[string]any{"method": "session/event", "params": event}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"request:usage", "request:close"} {
		if err := enc.Encode(map[string]any{"id": id, "result": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	request, _ := zcodeSessionRequest("request", "/work", "", []string{"Read"})
	attached := false
	identity, answer, err := runZCodeSession(&in, &out, request, "input", "prompt", func(string) error { attached = true; return nil }, zcodeSessionOptions{
		CaptureUsage: true, EnforceMode: true, Model: "chosen", Effort: "high", PrepareTools: func(id string) ([]any, error) {
			if !attached || id != "native" || strings.Contains(out.String(), "session/send") {
				t.Fatal("tools configured before binding")
			}
			return []any{map[string]any{"name": "formal-gates"}}, nil
		},
	})
	if err != nil || identity != "native" || string(answer) != `{"output":"done"}` {
		t.Fatalf("%s %s %v", identity, answer, err)
	}
	dec := json.NewDecoder(&out)
	for _, method := range []string{"session/create", "session/close", "session/resume", "session/setMode", "session/setModel", "session/setThoughtLevel", "session/subscribe", "session/send", "session/usage", "session/close"} {
		var r zcodeRequest
		if err := dec.Decode(&r); err != nil || r.Method != method {
			t.Fatalf("%+v %v", r, err)
		}
		if method == "session/resume" && (r.Params["sessionId"] != "native" || len(r.Params["mcpServers"].([]any)) != 1) {
			t.Fatal("bound configuration lost")
		}
		if method == "session/setModel" && (r.Params["model"].(map[string]any)["modelId"] != "chosen" || r.Params["persistAsWorkspaceLastUsed"] != false) {
			t.Fatal("model selection changed")
		}
	}
}
