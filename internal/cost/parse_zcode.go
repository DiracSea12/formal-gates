package cost

import (
	"encoding/json"
	"fmt"
	"io"
)

// ZCode terminal events carry provider usage for a turn. Input includes cache
// reads; other token counters are breakdowns, not additional consumption.
func parseZCodeTranscript(reader io.Reader) (Usage, error) {
	total := Usage{}
	seen := map[string]Usage{}
	session := ""
	var invalid error
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var raw struct {
			Session string `json:"sessionId"`
			Turn    string `json:"turnId"`
			Type    string `json:"type"`
			Payload struct {
				Usage *struct {
					Source   string `json:"source"`
					Requests *int64 `json:"modelRequestCount"`
					Input    *int64 `json:"inputTokens"`
					Output   *int64 `json:"outputTokens"`
					Cache    *int64 `json:"cacheReadTokens"`
				} `json:"usage"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &raw) != nil || raw.Type != "turn.completed" && raw.Type != "turn.failed" {
			return false
		}
		u := raw.Payload.Usage
		if raw.Session == "" || raw.Turn == "" || u == nil || u.Source != "provider" || u.Requests == nil || *u.Requests <= 0 || u.Input == nil || u.Output == nil || u.Cache == nil || *u.Input < 0 || *u.Output < 0 || *u.Cache < 0 || *u.Cache > *u.Input {
			invalid = fmt.Errorf("ZCode terminal provider usage is unavailable (including no-backend zero fallback)")
			return false
		}
		if session != "" && session != raw.Session {
			invalid = fmt.Errorf("ZCode transcript contains multiple sessions")
			return false
		}
		session = raw.Session
		value := Usage{InputCacheMissTokens: *u.Input - *u.Cache, InputCacheHitTokens: *u.Cache, OutputTokens: *u.Output}
		if old, ok := seen[raw.Turn]; ok {
			if old != value {
				invalid = fmt.Errorf("ZCode turn has conflicting repeated usage")
			}
			return false
		}
		seen[raw.Turn] = value
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
