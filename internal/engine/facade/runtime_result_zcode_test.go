package facade

import (
	"encoding/json"
	"testing"
)

func TestZCodeTurnResultBinding(t *testing.T) {
	event := func(seq int, kind, input, turn, result, response string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"sessionId": "native-session", "seq": seq, "turnId": turn, "type": kind, "payload": map[string]any{"inputId": input, "resultType": result, "response": response}})
		return raw
	}
	for _, terminal := range []string{"success", "cancelled", "error_max_turns", "error_max_budget", "error_during_execution", "error_max_tool_calls"} {
		t.Run(terminal, func(t *testing.T) {
			o, err := newZCodeTurnObservation("native-session", "issued-input")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := o.result(); err == nil {
				t.Fatal("unstarted input accepted")
			}
			if err := o.observe(event(1, "turn.completed", "previous-input", "old-turn", "success", `{"status":"PASS"}`)); err != nil {
				t.Fatal(err)
			}
			start := event(2, "turn.started", "issued-input", "new-turn", "", "")
			if err := o.observe(start); err != nil {
				t.Fatal(err)
			}
			if err := o.observe(start); err != nil {
				t.Fatal("replay was treated as fresh work", err)
			}
			err = o.observe(event(3, "turn.completed", "issued-input", "new-turn", terminal, `{"status":"CONTINUE"}`))
			answer, resultErr := o.result()
			if terminal == "success" {
				if err != nil || resultErr != nil || string(answer) != `{"status":"CONTINUE"}` {
					t.Fatalf("%s %v %v", answer, err, resultErr)
				}
			} else if err == nil || resultErr == nil {
				t.Fatal("native failure accepted")
			}
		})
	}
	o, _ := newZCodeTurnObservation("native-session", "issued-input")
	if err := o.observe(event(1, "turn.completed", "issued-input", "turn", "success", `{}`)); err == nil {
		t.Fatal("completion without matching start accepted")
	}
	o, _ = newZCodeTurnObservation("native-session", "issued-input")
	if err := o.observe(json.RawMessage(`{"sessionId":"other","seq":1,"type":"turn.started"}`)); err == nil {
		t.Fatal("other session accepted")
	}
}
