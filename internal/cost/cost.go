// Package cost meters exact token usage of dispatched subagents and run owners.
//
// The ledger lives in the run state's single `cost` projection: run totals
// plus one entry per dispatched gate or action and an optional owner entry.
// Numbers come exclusively from host transcript parsing; an unverifiable source is
// marked unavailable and never receives a fabricated value.
package cost

// Source markers for a dispatch cost entry.
const (
	// SourceTranscript marks an entry backfilled from a parsed transcript.
	SourceTranscript = "transcript"
	// SourceUnavailable marks an entry without a complete measured source.
	// Known cumulative measurements remain; absent measurements serialize as null.
	SourceUnavailable = "unavailable"
)

// RunCost is the cost projection of a run: per-dispatch entries and the
// run totals accumulated from them, this run's owner and accepted child owners.
// Token categories follow the host pricing rules: input with cache hit, input
// without cache hit, and output.
// TotalInputTokens is the input-side total (cache hit + cache miss); output
// is recorded separately and never added into the total. Cache write tokens
// are not a priced category and are not recorded.
type RunCost struct {
	TotalInputTokens     int64                   `json:"totalInputTokens"`
	InputCacheHitTokens  int64                   `json:"inputCacheHitTokens"`
	InputCacheMissTokens int64                   `json:"inputCacheMissTokens"`
	OutputTokens         int64                   `json:"outputTokens"`
	Dispatches           map[string]DispatchCost `json:"dispatches"`
	Owner                *OwnerCost              `json:"owner,omitempty"`
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
	Reason               string `json:"reason,omitempty"`
	// Codex retains the exact source and native task timing on this same entry.
	TranscriptPath string      `json:"transcriptPath,omitempty"`
	Timing         *TaskTiming `json:"timing,omitempty"`
}

// TaskTiming is the active model-task wall time, including tool waits. Start
// and completion identify the outer boundaries; separate-turn idle is excluded.
type TaskTiming struct {
	StartedAt      string `json:"startedAt"`
	CompletedAt    string `json:"completedAt"`
	WallTimeMillis int64  `json:"wallTimeMillis"`
}

// Usage is the exact metered token usage parsed from one host transcript,
// normalized to the pricing categories shared by the supported hosts.
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
	run.addTokens(entry.TotalInputTokens, entry.InputCacheHitTokens, entry.InputCacheMissTokens, entry.OutputTokens)
}

// RecordCumulative updates one active dispatch from its original source's
// cumulative usage. An unavailable continuation preserves the measured prefix.
func RecordCumulative(run *RunCost, dispatchID string, entry DispatchCost) {
	if run == nil {
		return
	}
	previous, recorded := run.Dispatches[dispatchID]
	if !recorded {
		Record(run, dispatchID, entry)
		return
	}
	if entry.Source == SourceUnavailable {
		previous.Source, previous.Reason = entry.Source, entry.Reason
		entry = previous
	}
	run.Dispatches[dispatchID] = entry
	run.addTokens(entry.TotalInputTokens-previous.TotalInputTokens, entry.InputCacheHitTokens-previous.InputCacheHitTokens, entry.InputCacheMissTokens-previous.InputCacheMissTokens, entry.OutputTokens-previous.OutputTokens)
}

// addTokens is shared by dispatch and owner recording. Output stays separate
// from the input total; neither path changes the other path's entries.
func (run *RunCost) addTokens(input, hit, miss, output int64) {
	run.TotalInputTokens += input
	run.InputCacheHitTokens += hit
	run.InputCacheMissTokens += miss
	run.OutputTokens += output
}

// Merge folds another run's cost ledger into this one, namespacing the
// incoming dispatch entries under "<ns>/<dispatch-id>" so dispatch ids from
// different runs never collide. Totals are accumulated per entry; an entry
// already merged under the same namespaced key is not re-added (idempotent),
// so re-merging a run never double-counts. Owner contributions remain separate.
func Merge(run *RunCost, ns string, other *RunCost) {
	if run == nil || other == nil {
		return
	}
	if run.Dispatches == nil {
		run.Dispatches = map[string]DispatchCost{}
	}
	for id, entry := range other.Dispatches {
		key := id
		if ns != "" {
			key = ns + "/" + id
		}
		Record(run, key, entry)
	}
}
