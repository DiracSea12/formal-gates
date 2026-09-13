package facade

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestZCodeRuntimePreferencesPreserveBoundNativeExchange(t *testing.T) {
	for _, resume := range []string{"", "native"} {
		t.Run("resume="+resume, func(t *testing.T) {
			input := `{"id":"server-1","method":"session/requestRuntimePreferences","params":{"sessionId":"native","scope":"runtime-materialization"}}
{"id":"request","result":{"session":{"sessionId":"native"}}}
{"id":"request:subscribe","result":{"sessionId":"native","events":[]}}
{"id":"request:send","result":{"sessionId":"native","accepted":true}}
{"id":"server-2","method":"session/requestRuntimePreferences","params":{"sessionId":"native","scope":"user-execution"}}
{"method":"session/event","params":{"sessionId":"native","seq":1,"turnId":"turn","type":"turn.started","payload":{"inputId":"input"}}}
{"method":"session/event","params":{"sessionId":"native","seq":2,"turnId":"turn","type":"turn.completed","payload":{"inputId":"input","resultType":"success","response":"{\"status\":\"CONTINUE\"}"}}}
{"id":"request:close","result":{}}
`
			request, err := zcodeSessionRequest("request", "/work", resume, "yolo", []string{})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			attached := false
			identity, answer, err := runZCodeSession(strings.NewReader(input), &out, request, "input", "bound prompt", func(id string) error {
				if id != "native" || strings.Contains(out.String(), "session/send") {
					t.Fatal("native model input preceded identity attachment")
				}
				attached = true
				return nil
			})
			if err != nil || !attached || identity != "native" || string(answer) != `{"status":"CONTINUE"}` {
				t.Fatalf("identity=%s answer=%s error=%v", identity, answer, err)
			}
			decoder := json.NewDecoder(&out)
			replies, sends := 0, 0
			for {
				var message map[string]any
				if err := decoder.Decode(&message); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if message["method"] == "session/send" {
					sends++
				}
				if message["id"] == "server-1" || message["id"] == "server-2" {
					replies++
					result := message["result"].(map[string]any)
					if result["nativeSearchEnhancementsEnabled"] != false || result["memoryEnabled"] != false || result["askUserQuestionAutoResolutionEnabled"] != false || result["modelContextBudgetStrategy"] != "preflight-v1" || len(result) != 4 {
						t.Fatal("runtime preferences granted ambient access, automatic decisions or an unrequested model", result)
					}
				}
			}
			if replies != 2 || sends != 1 {
				t.Fatalf("preference replies=%d native inputs=%d", replies, sends)
			}
		})
	}
}

func TestZCodeUnknownInteractionStillRefusesBeforeModelInput(t *testing.T) {
	request, _ := zcodeSessionRequest("request", "/work", "", "yolo", []string{})
	var out bytes.Buffer
	_, answer, err := runZCodeSession(strings.NewReader(`{"id":"permission","method":"session/requestPermission","params":{}}`), &out, request, "input", "prompt", func(string) error { t.Fatal("unexpected attachment"); return nil })
	if err == nil || !strings.Contains(err.Error(), "ZCODE_INTERACTION") || answer != nil || strings.Contains(out.String(), "session/send") || strings.Contains(out.String(), `"result"`) {
		t.Fatalf("unknown interaction silently approved: %s %v", out.String(), err)
	}
}
