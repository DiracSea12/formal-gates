package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// WorkBuddy evidence keeps the selected SDK item IDs and native usage fields.
// The SDK's rawUsage or normalized usage distinguishes total from uncached input.
func parseWorkBuddyTranscript(reader io.Reader) (Usage, error) {
	total := Usage{}
	seen := map[string]Usage{}
	session := ""
	var invalid error
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var records []struct {
			ID      string `json:"id"`
			Session string `json:"session_id"`
			Data    struct {
				Usage map[string]json.RawMessage `json:"usage"`
				Raw   map[string]json.RawMessage `json:"rawUsage"`
			} `json:"providerData"`
		}
		if json.Unmarshal(line, &records) != nil {
			return false
		}
		parsed := false
		for _, e := range records {
			if e.ID == "" || e.Session == "" || e.Data.Usage == nil {
				invalid = fmt.Errorf("workbuddy native identity or usage unavailable")
				continue
			}
			var input, output *int64
			if json.Unmarshal(e.Data.Usage["inputTokens"], &input) != nil || json.Unmarshal(e.Data.Usage["outputTokens"], &output) != nil || input == nil || output == nil || *input < 0 || *output < 0 {
				invalid = fmt.Errorf("workbuddy native input/output categories unavailable")
				continue
			}
			raw := e.Data.Raw
			if raw == nil {
				raw = e.Data.Usage
			}
			cache, err := workbuddyCacheRead(raw)
			if err != nil {
				invalid = err
				continue
			}
			creation, err := workbuddyFirstPositive(raw, "cache_creation_input_tokens", "cacheCreationInputTokens", "prompt_cache_write_tokens")
			if err != nil {
				invalid = err
				continue
			}
			miss := *input
			for _, key := range []string{"prompt_tokens", "prompt_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens", "inputTokensDetails"} {
				if _, includesCache := raw[key]; includesCache {
					miss = *input - cache - creation
					break
				}
			}
			if miss < 0 {
				invalid = fmt.Errorf("workbuddy native cache reads exceed total input")
				continue
			}
			if session != "" && session != e.Session {
				invalid = fmt.Errorf("workbuddy transcript contains multiple sessions")
				continue
			}
			session = e.Session
			value := Usage{InputCacheMissTokens: miss, InputCacheHitTokens: cache, OutputTokens: *output}
			if previous, exists := seen[e.ID]; exists {
				if previous != value {
					invalid = fmt.Errorf("workbuddy duplicate item has conflicting usage")
				}
				continue
			}
			seen[e.ID] = value
			total.InputCacheMissTokens += value.InputCacheMissTokens
			total.InputCacheHitTokens += value.InputCacheHitTokens
			total.OutputTokens += value.OutputTokens
			parsed = true
		}
		return parsed
	})
	if invalid != nil {
		return Usage{}, invalid
	}
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}

func workbuddyFirstPositive(fields map[string]json.RawMessage, keys ...string) (int64, error) {
	for _, key := range keys {
		raw := fields[key]
		if raw == nil {
			continue
		}
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil || value < 0 {
			return 0, fmt.Errorf("workbuddy native %s category unavailable", key)
		}
		if value > 0 {
			return value, nil
		}
	}
	return 0, nil
}

func workbuddyCacheRead(fields map[string]json.RawMessage) (int64, error) {
	value, err := workbuddyFirstPositive(fields, "cache_read_input_tokens", "cacheReadInputTokens")
	if err != nil || value > 0 {
		return value, err
	}
	for _, key := range []string{"inputTokensDetails", "prompt_tokens_details"} {
		raw := bytes.TrimSpace(fields[key])
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		var items []map[string]json.RawMessage
		if raw[0] == '[' {
			if err := json.Unmarshal(raw, &items); err != nil {
				return 0, err
			}
		} else {
			var item map[string]json.RawMessage
			if err := json.Unmarshal(raw, &item); err != nil {
				return 0, err
			}
			items = []map[string]json.RawMessage{item}
		}
		var cached int64
		for _, item := range items {
			value, err := workbuddyFirstPositive(item, "cached_tokens")
			if err != nil {
				return 0, err
			}
			cached += value
		}
		if cached > 0 {
			return cached, nil
		}
	}
	return workbuddyFirstPositive(fields, "prompt_cache_hit_tokens")
}
