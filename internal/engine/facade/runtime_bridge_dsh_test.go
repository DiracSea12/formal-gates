package facade

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDSHSessionLifecycleAndConfiguration(t *testing.T) {
	for _, resume := range []bool{false, true} {
		for _, reject := range []bool{false, true} {
			job := t.TempDir()
			if err := os.WriteFile(filepath.Join(job, "native-source.json"), []byte(`{"sessionId":"native","turn":2}`), 0600); err != nil {
				t.Fatal(err)
			}
			events := `{"sessionId":"native","event":{"type":"assistant/message","data":{"turn":2,"message":{"content":[{"type":"text","text":"{\"output\":\"done\"}"}]}}}}
{"sessionId":"native","event":{"type":"turn/end","data":{"turn":2,"reason":{"kind":"completed"}}}}
`
			if err := os.WriteFile(filepath.Join(job, "native-events.jsonl"), []byte(events), 0600); err != nil {
				t.Fatal(err)
			}
			var input, output bytes.Buffer
			enc := json.NewEncoder(&input)
			session := map[string]any{"configOptions": []any{map[string]any{"id": "model", "currentValue": `["provider","default"]`}}}
			if !resume {
				session["sessionId"] = "native"
			}
			for _, row := range []struct {
				id     int
				result any
			}{{1, map[string]any{"protocolVersion": 1}}, {3, session}, {10, map[string]any{}}, {11, map[string]any{}}, {4, map[string]any{"stopReason": "end_turn"}}, {5, map[string]any{}}} {
				if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": row.id, "result": row.result}); err != nil {
					t.Fatal(err)
				}
			}
			identity := ""
			if resume {
				identity = "native"
			}
			result, err := runDSHSession(&input, &output, job, "/work", identity, "prompt", func(id string) error {
				if id != "native" || strings.Contains(output.String(), "session/prompt") {
					t.Fatal("prompt before attachment")
				}
				if reject {
					return errors.New("attachment rejected")
				}
				return nil
			}, dshSessionOptions{Model: "chosen", Effort: "high"})
			if reject {
				if err == nil || strings.Contains(output.String(), "session/prompt") {
					t.Fatal("attachment rejection ignored")
				}
				continue
			}
			if err != nil || result.Identity != "native" || string(result.Answer) != `{"output":"done"}` {
				t.Fatalf("%+v %v", result, err)
			}
			methods := []string{"initialize", "session/new", "session/set_config_option", "session/set_config_option", "session/prompt", "session/close"}
			if resume {
				methods[1] = "session/resume"
			}
			dec := json.NewDecoder(&output)
			for i, method := range methods {
				var request struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := dec.Decode(&request); err != nil || request.Method != method {
					t.Fatalf("%+v %v", request, err)
				}
				if i == 2 && request.Params["value"] != `["provider","chosen"]` {
					t.Fatal("model/provider changed")
				}
				if i == 3 && request.Params["value"] != "high" {
					t.Fatal("effort changed")
				}
			}
		}
	}
}
