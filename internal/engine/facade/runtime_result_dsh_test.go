package facade

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDSHNativeEndReason(t *testing.T) {
	for _, reason := range []string{"completed", "aborted", "blocked", "error", "max-tokens", "interrupted"} {
		var stream bytes.Buffer
		encoder := json.NewEncoder(&stream)
		_ = encoder.Encode(map[string]any{"sessionId": "native", "event": map[string]any{"type": "assistant/message", "data": map[string]any{"turn": 1, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"status":"PASS"}`}}}}}})
		_ = encoder.Encode(map[string]any{"sessionId": "native", "event": map[string]any{"type": "turn/end", "data": map[string]any{"turn": 1, "reason": map[string]any{"kind": reason}}}})
		answer, err := observeDSHResult(&stream, "native", 1)
		if reason == "completed" {
			if err != nil || string(answer) != `{"status":"PASS"}` {
				t.Fatalf("%s %v", answer, err)
			}
		} else if err == nil {
			t.Fatalf("%s accepted", reason)
		}
	}
	if _, err := observeDSHResult(strings.NewReader(""), "native", 1); err == nil {
		t.Fatal("missing native turn end accepted")
	}
}
