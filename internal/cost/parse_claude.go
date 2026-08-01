package cost

import (
	"encoding/json"
)

// parseClaudeTranscript sums per-message usage over the assistant lines of a
// Claude Code subagent transcript (agent-<id>.jsonl). Transcript entries can
// be duplicated across rewrites, so lines are deduped by their uuid.
// Unknown line shapes and missing optional fields are skipped, never fatal;
// a transcript with no assistant line carrying usage is a structural failure.
func parseClaudeTranscript(path string) (Usage, error) {
	total := Usage{}
	seen := map[string]bool{}
	err := scanTranscriptLines(path, func(line []byte) bool {
		var entry struct {
			Type    string `json:"type"`
			UUID    string `json:"uuid"`
			Message *struct {
				Usage *struct {
					InputTokens          *int64 `json:"input_tokens"`
					OutputTokens         *int64 `json:"output_tokens"`
					CacheReadInputTokens *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return false // unknown line shape: skip, never fatal
		}
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			return false
		}
		if entry.UUID != "" {
			if seen[entry.UUID] {
				return false // duplicated across rewrites: count once
			}
			seen[entry.UUID] = true
		}
		// Priced categories only: cache hit maps to cache_read_input_tokens,
		// cache miss to input_tokens, output to output_tokens. Cache
		// creation tokens are not a priced category and are not recorded.
		total.InputCacheMissTokens += nonNegative(entry.Message.Usage.InputTokens)
		total.OutputTokens += nonNegative(entry.Message.Usage.OutputTokens)
		total.InputCacheHitTokens += nonNegative(entry.Message.Usage.CacheReadInputTokens)
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}

// nonNegative returns the pointer value, or zero for a missing or negative
// field. A corrupted negative count is treated as absent rather than
// subtracted.
func nonNegative(value *int64) int64 {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}
