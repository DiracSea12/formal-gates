package protocol

import (
	"formal-gates/internal/cost"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSourcesSettleAndRetainIncrementalUsage(t *testing.T) {
	fixtures := map[string][2]string{
		"workbuddy":        {`{"type":"assistant","uuid":"one","session_id":"native","message":{"usage":{"input_tokens":60,"output_tokens":20,"cache_read_input_tokens":40}}}` + "\n", `{"type":"assistant","uuid":"two","session_id":"native","message":{"usage":{"input_tokens":30,"output_tokens":5,"cache_read_input_tokens":10}}}` + "\n"},
		"deepseek-harness": {`{"sessionId":"native","event":{"seq":1,"type":"assistant/message","data":{"usage":{"inputTokens":100,"outputTokens":20,"cacheReadTokens":40}}}}` + "\n", `{"sessionId":"native","event":{"seq":2,"type":"assistant/message","data":{"usage":{"inputTokens":40,"outputTokens":5,"cacheReadTokens":10}}}}` + "\n"},
		"zcode":            {`{"id":"first:usage","result":{"sessionId":"native","modelRequestCount":1,"inputTokens":100,"outputTokens":20,"cacheReadTokens":40}}` + "\n", `{"id":"second:usage","result":{"sessionId":"native","modelRequestCount":2,"inputTokens":140,"outputTokens":25,"cacheReadTokens":50}}` + "\n"},
	}
	for provider, lines := range fixtures {
		t.Run(provider, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.jsonl")
			write := func(data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			claim := ExecutionClaim{Provider: provider}
			s := &State{Cost: &cost.RunCost{}, RegularExecutions: map[string]RegularExecution{"first": {Identity: "native", Claim: claim}, "second": {Identity: "native", Claim: claim}}}
			src := ExecutionSource{ActionID: "first", Provider: provider, Identity: "native", Status: "STOPPED", TranscriptPath: path}
			write(lines[0])
			if err := settleRegularSource(s, src, s.RegularExecutions["first"]); err != nil {
				t.Fatal(err)
			}
			write(lines[0] + lines[1])
			src.ActionID = "second"
			if err := settleRegularSource(s, src, s.RegularExecutions["second"]); err != nil {
				t.Fatal(err)
			}
			got := s.Cost.Dispatches["second"]
			if got.TotalInputTokens != 40 || got.InputCacheHitTokens != 10 || got.OutputTokens != 5 || s.Cost.TotalInputTokens != 140 {
				t.Fatalf("delta=%+v total=%+v", got, s.Cost)
			}
			if err := checkNativeTranscriptIdentity(provider, path, "other"); err == nil {
				t.Fatal("wrong native identity accepted")
			}
		})
	}
}
