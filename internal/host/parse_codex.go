package host

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// parseCodexTranscript meters native subagent rollouts over their local turns.
// Other Codex sources support token_usage_record and event_msg/token_count.
// Request records are deduplicated by response ID; token_count events mirror
// them once requests are present. Token_count-only sources use cumulative deltas
// and timestamp/model/usage keys. Any preceding cumulative prefix remains counted.
func parseCodexTranscript(reader io.Reader) (Usage, error) {
	buffered := bufio.NewReader(reader)
	var prefix bytes.Buffer
	for {
		line, err := buffered.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return Usage{}, err
		}
		prefix.Write(line)
		if len(bytes.TrimSpace(line)) != 0 || err == io.EOF {
			break
		}
	}
	reader = io.MultiReader(bytes.NewReader(prefix.Bytes()), buffered)
	var first struct {
		Type    string `json:"type"`
		Payload struct {
			Source struct {
				Subagent *struct {
					ThreadSpawn json.RawMessage `json:"thread_spawn"`
				} `json:"subagent"`
			} `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(bytes.TrimSpace(prefix.Bytes()), &first) == nil && first.Type == "session_meta" && first.Payload.Source.Subagent != nil && len(first.Payload.Source.Subagent.ThreadSpawn) != 0 && string(first.Payload.Source.Subagent.ThreadSpawn) != "null" {
		raw, err := io.ReadAll(reader)
		if err != nil {
			return Usage{}, err
		}
		thread, err := parseCodexThread(raw)
		if err != nil {
			return Usage{}, err
		}
		total, available := Usage{}, false
		for _, turn := range thread.Turns {
			total.InputCacheHitTokens += turn.Usage.InputCacheHitTokens
			total.InputCacheMissTokens += turn.Usage.InputCacheMissTokens
			total.OutputTokens += turn.Usage.OutputTokens
			available = available || turn.UsageAvailable
		}
		if !available {
			return Usage{}, fmt.Errorf("Codex subagent transcript contains no local response token usage")
		}
		return total, nil
	}
	return parseCodexUsageRecords(reader)
}

func parseCodexUsageRecords(reader io.Reader) (Usage, error) {
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
			// An equal first thread total is already counted by its cumulative mirror.
			// Other requests contribute their explicit per-response usage.
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
		// Record the last-only key so a total+last event and its last-only mirror
		// at the same timestamp contribute once.
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
