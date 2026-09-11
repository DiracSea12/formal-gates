package cost

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDSHCommittedUsage(t *testing.T) {
	event := func(seq int, kind string, input, output, cache int) string {
		raw, _ := json.Marshal(map[string]any{"sessionId": "native", "event": map[string]any{"type": kind, "seq": seq, "data": map[string]any{"llmStreamCall": true, "usage": map[string]any{"inputTokens": input, "outputTokens": output, "cacheReadTokens": cache}}}})
		return string(raw) + "\n"
	}
	first := event(1, "assistant/message", 100, 20, 40)
	total, err := parseDSHTranscript(strings.NewReader(first + first + event(2, "compaction/summary", 30, 5, 0)))
	if err != nil || total.InputCacheMissTokens != 90 || total.InputCacheHitTokens != 40 || total.OutputTokens != 25 {
		t.Fatalf("%+v %v", total, err)
	}
	for _, extra := range []string{event(1, "assistant/message", 110, 20, 40), `{"sessionId":"native","event":{"type":"assistant/message","seq":3,"data":{}}}`, `{"sessionId":"native","event":{"type":"assistant/attempt","seq":3,"data":{}}}`} {
		if _, err := parseDSHTranscript(strings.NewReader(first + extra)); err == nil {
			t.Fatal("unknown or conflicting usage accepted")
		}
	}
}

func TestZCodeCumulativeUsage(t *testing.T) {
	snapshot := func(requests, input, output, cache int) string {
		raw, _ := json.Marshal(map[string]any{"id": "action:usage", "result": map[string]any{"sessionId": "native", "modelRequestCount": requests, "inputTokens": input, "outputTokens": output, "cacheReadTokens": cache}})
		return string(raw) + "\n"
	}
	first := snapshot(1, 100, 20, 40)
	usage, err := parseZCodeTranscript(strings.NewReader(first + first + snapshot(2, 160, 30, 60)))
	if err != nil || usage.InputCacheMissTokens != 100 || usage.InputCacheHitTokens != 60 || usage.OutputTokens != 30 {
		t.Fatalf("%+v %v", usage, err)
	}
	for _, stream := range []string{snapshot(0, 0, 0, 0), first + snapshot(0, 0, 0, 0), first + snapshot(1, 90, 20, 30)} {
		if _, err := parseZCodeTranscript(strings.NewReader(stream)); err == nil {
			t.Fatal("unavailable/regressed native usage accepted")
		}
	}
}
