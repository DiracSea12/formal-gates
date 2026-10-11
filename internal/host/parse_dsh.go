package host

import (
	"encoding/json"
	"fmt"
	"io"
)

// DSH committed messages report cache-miss input and cache reads separately.
// Reasoning is included in output; repeated sequence numbers are replayed events.
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
			invalid = fmt.Errorf("DSH interrupted model attempt has no committed usage")
			return false
		}
		if raw.Event.Type != "assistant/message" && !(raw.Event.Type == "compaction/summary" && raw.Event.Data.LLM) {
			return false
		}
		u := raw.Event.Data.Usage
		if raw.Session == "" || raw.Event.Seq == nil || *raw.Event.Seq < 0 || u == nil || u.Input == nil || u.Output == nil || u.Cache == nil || *u.Input < 0 || *u.Output < 0 || *u.Cache < 0 {
			invalid = fmt.Errorf("DSH committed model event identity or usage is unavailable")
			return false
		}
		if session != "" && session != raw.Session {
			invalid = fmt.Errorf("DSH transcript contains multiple sessions")
			return false
		}
		session = raw.Session
		value := Usage{InputCacheMissTokens: *u.Input, InputCacheHitTokens: *u.Cache, OutputTokens: *u.Output}
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
	if invalid != nil {
		return Usage{}, invalid
	}
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}
