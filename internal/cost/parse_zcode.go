package cost

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// session/usage is cumulative. Its documented no-backend response contains
// zero model requests and must never be reported as actual zero consumption.
func parseZCodeTranscript(reader io.Reader) (Usage, error) {
	latest := Usage{}
	session := ""
	var requests int64
	var invalid error
	seen := false
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var raw struct {
			ID     string `json:"id"`
			Result *struct {
				Session  string `json:"sessionId"`
				Requests *int64 `json:"modelRequestCount"`
				Input    *int64 `json:"inputTokens"`
				Output   *int64 `json:"outputTokens"`
				Cache    *int64 `json:"cacheReadTokens"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &raw) != nil || !strings.HasSuffix(raw.ID, ":usage") {
			return false
		}
		r := raw.Result
		if r == nil || r.Session == "" || r.Requests == nil || *r.Requests <= 0 || r.Input == nil || r.Output == nil || r.Cache == nil || *r.Input < 0 || *r.Output < 0 || *r.Cache < 0 || *r.Cache > *r.Input {
			invalid = fmt.Errorf("ZCode cumulative billing source unavailable (including no-backend zero fallback)")
			return false
		}
		if session != "" && session != r.Session {
			invalid = fmt.Errorf("ZCode transcript contains multiple sessions")
			return false
		}
		session = r.Session
		value := Usage{InputCacheMissTokens: *r.Input - *r.Cache, InputCacheHitTokens: *r.Cache, OutputTokens: *r.Output}
		if seen && (*r.Requests < requests || value.InputCacheMissTokens < latest.InputCacheMissTokens || value.InputCacheHitTokens < latest.InputCacheHitTokens || value.OutputTokens < latest.OutputTokens) {
			invalid = fmt.Errorf("ZCode cumulative usage regressed")
			return false
		}
		latest = value
		requests = *r.Requests
		seen = true
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	if invalid != nil {
		return Usage{}, invalid
	}
	if !seen {
		return Usage{}, fmt.Errorf("ZCode cumulative billing source unavailable")
	}
	return latest, nil
}
