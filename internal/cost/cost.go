// Package cost meters exact token usage of dispatched subagents.
//
// The ledger lives in the run state's single `cost` projection: run totals
// plus one entry per dispatched gate or action. Numbers come exclusively
// from host transcript parsing; a dispatch with no parseable transcript is
// marked unavailable and never receives a fabricated value.
package cost

// Source markers for a dispatch cost entry.
const (
	// SourceTranscript marks an entry backfilled from a parsed transcript.
	SourceTranscript = "transcript"
	// SourceUnavailable marks an entry with no parseable transcript; it
	// carries zero numbers because the real usage is explicitly absent.
	SourceUnavailable = "unavailable"
)

// RunCost is the cost projection of a run: per-dispatch entries and the
// run totals accumulated from them. Token categories follow the host pricing
// rules: input with cache hit, input without cache hit, and output.
// TotalInputTokens is the input-side total (cache hit + cache miss); output
// is recorded separately and never added into the total. Cache write tokens
// are not a priced category and are not recorded.
type RunCost struct {
	TotalInputTokens     int64                   `json:"totalInputTokens"`
	InputCacheHitTokens  int64                   `json:"inputCacheHitTokens"`
	InputCacheMissTokens int64                   `json:"inputCacheMissTokens"`
	OutputTokens         int64                   `json:"outputTokens"`
	Dispatches           map[string]DispatchCost `json:"dispatches"`
}

// DispatchCost is the exact metered token usage of one dispatched gate or
// action, backfilled from its host transcript when available.
type DispatchCost struct {
	Target               string `json:"target"`
	Kind                 string `json:"kind"` // "gate" | "action"
	InputCacheHitTokens  int64  `json:"inputCacheHitTokens"`
	InputCacheMissTokens int64  `json:"inputCacheMissTokens"`
	OutputTokens         int64  `json:"outputTokens"`
	TotalInputTokens     int64  `json:"totalInputTokens"`
	Source               string `json:"source"` // "transcript" | "unavailable"
}

// Usage is the exact metered token usage parsed from one host transcript,
// normalized to the pricing categories shared by both hosts.
type Usage struct {
	InputCacheHitTokens  int64
	InputCacheMissTokens int64
	OutputTokens         int64
}

// Record adds the dispatch entry to the run ledger and accumulates the run
// totals. A dispatch is recorded at most once: recording the same dispatch
// id again is a no-op, so re-recording a completion never re-adds numbers.
func Record(run *RunCost, dispatchID string, entry DispatchCost) {
	if run == nil {
		return
	}
	if run.Dispatches == nil {
		run.Dispatches = map[string]DispatchCost{}
	}
	if _, recorded := run.Dispatches[dispatchID]; recorded {
		return
	}
	run.Dispatches[dispatchID] = entry
	run.InputCacheHitTokens += entry.InputCacheHitTokens
	run.InputCacheMissTokens += entry.InputCacheMissTokens
	run.OutputTokens += entry.OutputTokens
	run.TotalInputTokens += entry.TotalInputTokens
}
