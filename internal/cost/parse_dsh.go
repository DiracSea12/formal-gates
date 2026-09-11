package cost

import (
	"encoding/json"
	"fmt"
	"io"
)

// DSH emits TokenUsage on committed assistant messages and LLM compactions.
// Input is the reported total prompt count; cache reads are its cached subset.
func parseDSHTranscript(reader io.Reader) (Usage, error) {
	total := Usage{}
	seen := map[int64]Usage{}
	session := ""
	var invalid error
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var raw struct {
			Session string `json:"sessionId"`
			Event   struct {
				Type string `json:"type"`
				Seq  *int64 `json:"seq"`
				Data struct {
					LLM   bool `json:"llmStreamCall"`
					Usage *struct {
						Input  *int64 `json:"inputTokens"`
						Output *int64 `json:"outputTokens"`
						Cache  *int64 `json:"cacheReadTokens"`
					} `json:"usage"`
				} `json:"data"`
			} `json:"event"`
		}
		if json.Unmarshal(line, &raw) != nil {
			return false
		}
		if raw.Event.Type == "assistant/attempt" {
			invalid = fmt.Errorf("DSH interrupted model attempt has no authoritative usage")
			return false
		}
		if raw.Event.Type != "assistant/message" && !(raw.Event.Type == "compaction/summary" && raw.Event.Data.LLM) {
			return false
		}
		usage := raw.Event.Data.Usage
		if raw.Session == "" || raw.Event.Seq == nil || *raw.Event.Seq < 0 || usage == nil || usage.Input == nil || usage.Output == nil || *usage.Input < 0 || *usage.Output < 0 || usage.Cache != nil && (*usage.Cache < 0 || *usage.Cache > *usage.Input) {
			invalid = fmt.Errorf("DSH committed model event identity or usage unavailable")
			return false
		}
		if session != "" && session != raw.Session {
			invalid = fmt.Errorf("DSH transcript contains multiple sessions")
			return false
		}
		session = raw.Session
		cache := nonNegative(usage.Cache)
		value := Usage{InputCacheMissTokens: *usage.Input - cache, InputCacheHitTokens: cache, OutputTokens: *usage.Output}
		if old, ok := seen[*raw.Event.Seq]; ok {
			if old != value {
				invalid = fmt.Errorf("DSH event has conflicting repeated usage")
			}
			return false
		}
		seen[*raw.Event.Seq] = value
		total.InputCacheMissTokens += value.InputCacheMissTokens
		total.InputCacheHitTokens += value.InputCacheHitTokens
		total.OutputTokens += value.OutputTokens
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	if invalid != nil {
		return Usage{}, invalid
	}
	if len(seen) == 0 {
		return Usage{}, fmt.Errorf("DSH committed model usage unavailable")
	}
	return total, nil
}
