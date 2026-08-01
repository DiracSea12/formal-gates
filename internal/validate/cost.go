package validate

import (
	"strings"

	"formal-gates/internal/cost"
)

// backfillDispatchCost records the dispatch's exact token cost at result
// recording, right after lifecycle verification. It reads the transcript
// path stored on the dispatch's stop event and parses it through the
// provider adapter. A missing path, a parse failure, or a host without a
// parseable transcript (Cursor) yields an entry with
// source "unavailable" and zero numbers. Idempotence is owned by
// cost.Record alone: the first completion records the dispatch and repeats
// are no-ops. Cost data is display-only and never affects the recording
// outcome.
func backfillDispatchCost(root string, state *RunState, dispatch PreparedDispatch) {
	if state.Cost == nil {
		state.Cost = &cost.RunCost{}
	}
	entry := cost.DispatchCost{Target: dispatch.Target, Kind: dispatch.TargetKind, Source: cost.SourceUnavailable}
	provider, transcriptPath, err := workflowLifecycle.TranscriptPath(root, state.RunID, dispatch.ID)
	if err == nil && strings.TrimSpace(transcriptPath) != "" {
		if usage, parseErr := cost.ParseTranscript(provider, transcriptPath); parseErr == nil {
			entry = cost.DispatchCost{
				Target:               dispatch.Target,
				Kind:                 dispatch.TargetKind,
				InputCacheHitTokens:  usage.InputCacheHitTokens,
				InputCacheMissTokens: usage.InputCacheMissTokens,
				OutputTokens:         usage.OutputTokens,
				TotalInputTokens:     usage.InputCacheHitTokens + usage.InputCacheMissTokens,
				Source:               cost.SourceTranscript,
			}
		}
	}
	cost.Record(state.Cost, dispatch.ID, entry)
}
