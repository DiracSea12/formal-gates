package host

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
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

// parseCodexTranscript prefers current request records, deduplicated by response
// ID. Once they appear, legacy token_count events are mirrors, not extra usage:
// after CLI resume their cumulative counter may restart while the request
// records still carry the full thread total. Older-only transcripts retain the
// cumulative delta / timestamp fallback. An older prefix is kept when a thread
// continues after upgrading Codex.
func parseCodexTranscript(reader io.Reader) (Usage, error) {
	total := Usage{}
	seen := map[string]bool{}
	previousTotal := codexTokenUsage{}
	hasRequests := false
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var record struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				Type             string           `json:"type"`
				Model            string           `json:"model"`
				ResponseID       string           `json:"response_id"`
				Usage            *codexTokenUsage `json:"usage"`
				ThreadTokenUsage *codexTokenUsage `json:"thread_token_usage"`
				Info             *struct {
					LastTokenUsage  *codexTokenUsage `json:"last_token_usage"`
					TotalTokenUsage *codexTokenUsage `json:"total_token_usage"`
					Model           string           `json:"model"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &record) != nil {
			return false
		}
		payload := record.Payload
		var last, cumulative *codexTokenUsage
		key := ""
		switch {
		case record.Type == "token_usage_record" && payload.Usage != nil:
			last, cumulative = payload.Usage, payload.ThreadTokenUsage
			if payload.ResponseID != "" {
				key = "response:" + payload.ResponseID
			}
		case record.Type == "event_msg" && payload.Type == "token_count" && payload.Info != nil:
			if hasRequests {
				return false
			}
			last, cumulative = payload.Info.LastTokenUsage, payload.Info.TotalTokenUsage
			if payload.Model == "" {
				payload.Model = payload.Info.Model
			}
		default:
			return false
		}
		if last == nil && cumulative == nil {
			return false
		}
		if key != "" && seen[key] {
			return false
		}
		var usage codexTokenUsage
		if record.Type == "token_usage_record" {
			usage = *last
			// If an older mirror already reported exactly this first thread total,
			// its request is already counted. Subsequent current requests always use
			// their explicit per-response usage; a CLI-local mirror cannot reset it.
			if !hasRequests && cumulative != nil && *cumulative == previousTotal {
				usage = codexTokenUsage{}
			}
			hasRequests = true
		} else if cumulative != nil {
			usage = cumulativeDelta(*cumulative, previousTotal)
			previousTotal = *cumulative
		} else {
			usage = *last
		}
		if key == "" {
			key = record.Timestamp + "\x00" + payload.Model + "\x00" + codexUsageKey(usage)
		}
		if seen[key] {
			return false
		}
		seen[key] = true
		// Mark the incremental mirror too, for older transcripts that repeat a
		// total+last event as a last-only event at the same timestamp.
		if last != nil && cumulative != nil {
			seen[record.Timestamp+"\x00"+payload.Model+"\x00"+codexUsageKey(*last)] = true
		}
		// Cached input is a subset of input. Output already includes reasoning.
		input, cached := nonNegative(&usage.InputTokens), nonNegative(&usage.CachedInputTokens)
		miss := input - cached
		if miss < 0 {
			miss = 0
		}
		total.InputCacheHitTokens += cached
		total.InputCacheMissTokens += miss
		total.OutputTokens += nonNegative(&usage.OutputTokens)
		return true
	})
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}

// CodexTaskTiming measures completed model tasks from their native rollout
// boundaries. Tool execution and waits inside a task are included; idle time
// between separate turns is excluded. Any unfinished task leaves timing absent.
func CodexTaskTiming(path string) (TaskTiming, error) {
	file, err := os.Open(path)
	if err != nil {
		return TaskTiming{}, err
	}
	defer file.Close()
	starts := map[string]time.Time{}
	completed := map[string]bool{}
	var first, last time.Time
	var elapsed time.Duration
	err = scanTranscriptLines(file, func(line []byte) bool {
		var record struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Payload   struct {
				Type   string `json:"type"`
				TurnID string `json:"turn_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &record) != nil || record.Type != "event_msg" || record.Timestamp.IsZero() {
			return false
		}
		id := record.Payload.TurnID
		switch record.Payload.Type {
		case "task_started":
			if _, exists := starts[id]; !exists {
				starts[id] = record.Timestamp
			}
		case "task_complete", "turn_aborted":
			start, exists := starts[id]
			if !exists || completed[id] || record.Timestamp.Before(start) {
				return false
			}
			completed[id] = true
			if first.IsZero() || start.Before(first) {
				first = start
			}
			if record.Timestamp.After(last) {
				last = record.Timestamp
			}
			elapsed += record.Timestamp.Sub(start)
			return true
		}
		return false
	})
	if err != nil {
		return TaskTiming{}, err
	}
	if len(completed) != len(starts) {
		return TaskTiming{}, fmt.Errorf("Codex transcript contains unfinished tasks")
	}
	return TaskTiming{StartedAt: first.Format(time.RFC3339Nano), CompletedAt: last.Format(time.RFC3339Nano), WallTimeMillis: elapsed.Milliseconds()}, nil
}

// cumulativeDelta computes the per-field delta between consecutive
// cumulative total_token_usage values. A reset (current below previous) is
// used as the next baseline; the decrease itself is not charged.
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
