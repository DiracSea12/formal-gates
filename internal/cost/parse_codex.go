package cost

import (
	"encoding/json"
	"fmt"
)

// codexTokenUsage mirrors the per-request usage object Codex writes into
// rollout token_count events. OutputTokens already includes
// ReasoningOutputTokens; the latter is a breakdown of the output side, not
// an additional charge. CacheWriteInputTokens is not a priced category and
// is not mirrored here.
type codexTokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// parseCodexTranscript sums token_count events from a Codex rollout JSONL.
// It prefers the incremental last_token_usage and falls back to the delta of
// the cumulative total_token_usage; events are deduped by
// (timestamp, model, usage tuple). Unknown line shapes and missing optional
// fields are skipped, never fatal; a transcript with no parseable usage is a
// structural failure.
func parseCodexTranscript(path string) (Usage, error) {
	type event struct {
		Timestamp string          `json:"timestamp"`
		Type      string          `json:"type"`
		Payload   json.RawMessage `json:"payload"`
	}
	total := Usage{}
	seen := map[string]bool{}
	previousTotal := map[string]codexTokenUsage{}
	err := scanTranscriptLines(path, func(line []byte) bool {
		var record event
		if err := json.Unmarshal(line, &record); err != nil {
			return false // unknown line shape: skip, never fatal
		}
		if record.Type != "event_msg" || len(record.Payload) == 0 {
			return false
		}
		var payload struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Info  *struct {
				LastTokenUsage  *codexTokenUsage `json:"last_token_usage"`
				TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
				Model           string           `json:"model"`
			} `json:"info"`
		}
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return false
		}
		if payload.Type != "token_count" || payload.Info == nil {
			return false
		}
		model := payload.Model
		if model == "" {
			model = payload.Info.Model
		}
		var usage codexTokenUsage
		if payload.Info.LastTokenUsage != nil {
			usage = *payload.Info.LastTokenUsage
		} else if payload.Info.TotalTokenUsage != nil {
			usage = cumulativeDelta(*payload.Info.TotalTokenUsage, previousTotal[model])
		} else {
			return false // token_count event without usage data
		}
		// Track the cumulative counter whenever it is present so a later
		// fallback event computes its delta against the last known value,
		// not against zero.
		if payload.Info.TotalTokenUsage != nil {
			previousTotal[model] = *payload.Info.TotalTokenUsage
		}
		key := record.Timestamp + "\x00" + model + "\x00" + codexUsageKey(usage)
		if seen[key] {
			return false // duplicated event: count once
		}
		seen[key] = true
		// Priced categories only: Codex reports cached input as a subset of
		// input_tokens, so the adapter splits it out — cache hit is the
		// cached part, cache miss is input minus cached (clamped at zero for
		// a corrupted negative delta).
		input := nonNegative(&usage.InputTokens)
		cached := nonNegative(&usage.CachedInputTokens)
		miss := input - cached
		if miss < 0 {
			miss = 0
		}
		total.InputCacheHitTokens += cached
		total.InputCacheMissTokens += miss
		// Codex output_tokens already includes reasoning_output_tokens, so
		// the reasoning count is a breakdown of output, never an extra
		// charge; adding it would double-count.
		total.OutputTokens += nonNegative(&usage.OutputTokens)
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}

// cumulativeDelta computes the per-field delta between consecutive
// cumulative total_token_usage values. A reset (current below previous) is
// treated as a new baseline, since the cumulative counter restarts.
func cumulativeDelta(current, previous codexTokenUsage) codexTokenUsage {
	delta := func(cur, prev int64) int64 {
		if cur < prev {
			return 0
		}
		return cur - prev
	}
	return codexTokenUsage{
		InputTokens:           delta(current.InputTokens, previous.InputTokens),
		CachedInputTokens:     delta(current.CachedInputTokens, previous.CachedInputTokens),
		OutputTokens:          delta(current.OutputTokens, previous.OutputTokens),
		ReasoningOutputTokens: delta(current.ReasoningOutputTokens, previous.ReasoningOutputTokens),
	}
}

func codexUsageKey(usage codexTokenUsage) string {
	return fmt.Sprintf("%d\x00%d\x00%d\x00%d", usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.ReasoningOutputTokens)
}
