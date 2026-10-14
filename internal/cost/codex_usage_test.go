package cost

import (
	"encoding/json"
	"strings"
	"testing"
)

func codexUsageLine(kind, id, stamp string, input, cached, output int64) string {
	usage := map[string]int64{"input_tokens": input, "cached_input_tokens": cached, "output_tokens": output, "reasoning_output_tokens": output / 2}
	payload := map[string]any{"model": "same-model"}
	if kind == "token_usage_record" {
		payload["response_id"] = id
		payload["usage"] = usage
	} else {
		payload["type"] = "token_count"
		payload["info"] = map[string]any{"total_token_usage": usage, "last_token_usage": usage}
	}
	line, _ := json.Marshal(map[string]any{"type": kind, "timestamp": stamp, "payload": payload})
	return string(line) + "\n"
}

func TestCodexRequestsAndMirrors(t *testing.T) {
	request := codexUsageLine("token_usage_record", "response-a", "2026-09-01T00:00:01Z", 110, 30, 12)
	duplicate := codexUsageLine("token_usage_record", "response-a", "2026-09-01T00:00:09Z", 110, 30, 12)
	second := codexUsageLine("token_usage_record", "response-b", "2026-09-01T00:00:11Z", 70, 20, 8)
	mirror := codexUsageLine("event_msg", "", "2026-09-01T00:00:12Z", 180, 50, 20)
	u, err := parseTranscript("codex", strings.NewReader(request+duplicate+mirror+second+mirror))
	if err != nil || u != (Usage{InputCacheHitTokens: 50, InputCacheMissTokens: 130, OutputTokens: 20}) {
		t.Fatalf("current response and mirror reconciliation: %+v %v", u, err)
	}
	// Equal numbers on distinct native requests are separate charges.
	u, err = parseTranscript("codex", strings.NewReader(request+strings.ReplaceAll(request, "response-a", "response-c")))
	if err != nil || u != (Usage{InputCacheHitTokens: 60, InputCacheMissTokens: 160, OutputTokens: 24}) {
		t.Fatalf("distinct response IDs collapsed: %+v %v", u, err)
	}
}

func TestCodexUpgradeAndLegacyCounters(t *testing.T) {
	legacy := codexUsageLine("event_msg", "", "2026-09-01T00:00:01Z", 110, 30, 12)
	first := codexUsageLine("token_usage_record", "response-a", "2026-09-01T00:00:02Z", 110, 30, 12)
	var record map[string]any
	if err := json.Unmarshal([]byte(first), &record); err != nil {
		t.Fatal(err)
	}
	payload := record["payload"].(map[string]any)
	payload["thread_token_usage"] = payload["usage"]
	matched, _ := json.Marshal(record)
	next := codexUsageLine("token_usage_record", "response-b", "2026-09-01T00:00:04Z", 70, 20, 8)
	for name, source := range map[string]string{
		"matching first request mirror":        legacy + string(matched) + "\n" + next,
		"older prefix then new request":        legacy + next,
		"legacy cumulative repeated timestamp": legacy + codexUsageLine("event_msg", "", "2026-09-01T00:00:02Z", 110, 30, 12) + codexUsageLine("event_msg", "", "2026-09-01T00:00:03Z", 180, 50, 20),
	} {
		t.Run(name, func(t *testing.T) {
			u, err := parseTranscript("codex", strings.NewReader(source))
			if err != nil || u != (Usage{InputCacheHitTokens: 50, InputCacheMissTokens: 130, OutputTokens: 20}) {
				t.Fatalf("prefix/cumulative count mismatch: %+v %v", u, err)
			}
		})
	}
	if _, err := parseTranscript("codex", strings.NewReader("{\"type\":\"session_meta\",\"payload\":{}}\n")); err == nil {
		t.Fatal("no native usage was treated as measured zero")
	}
}
