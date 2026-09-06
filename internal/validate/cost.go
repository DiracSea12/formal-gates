package validate

import (
	"path/filepath"
	"strings"

	"formal-gates/internal/coordination"
	"formal-gates/internal/cost"
	"formal-gates/internal/lifecycle"
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

// This uses the existing crash-reclaimable coordination lock, not a second
// cost store. Always take it before terminal state/admission locks.
func lockOwnerCost(root string) (func(), error) {
	return coordination.AcquirePath(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", "owner-cost.lock"), "owner cost")
}

func beginOwnerCost(root string, state *RunState) error {
	if state.OwnerTranscript == "" {
		return nil
	}
	// Keep the original host identity: the write wall also compares that exact
	// string. Resolve aliases only for the cost interval comparison below.
	state.OwnerCostBaseline, _ = cost.CaptureBaseline(state.OwnerProvider, state.OwnerTranscript)
	paths, err := filepath.Glob(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", "*", "state.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		runID := filepath.Base(filepath.Dir(path))
		if runID == state.RunID {
			continue
		}
		other, err := LoadRunState(root, runID)
		if err != nil {
			// An unreadable active interval cannot be excluded from attribution.
			state.OwnerCostOverlapped = true
			continue
		}
		if other.Status != "ACTIVE" || other.OwnerTranscript == "" || canonicalRegistryPath(other.OwnerTranscript) != canonicalRegistryPath(state.OwnerTranscript) {
			continue
		}
		state.OwnerCostOverlapped = true
		// Persist on both runs now: either one may finish and delete its state
		// before the other reaches Seal/Abort. This changes only cost metadata.
		if _, err := mutateRun(root, runID, func(peer *RunState) error {
			peer.OwnerCostOverlapped = true
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func finishOwnerCost(state *RunState) {
	if state.OwnerCostCompleted {
		return
	}
	if state.Cost == nil {
		state.Cost = &cost.RunCost{}
	}
	entry := cost.OwnerCost{Source: cost.SourceUnavailable}
	if !state.OwnerCostOverlapped {
		if usage, err := cost.MeasureOwner(state.OwnerProvider, state.OwnerTranscript, state.OwnerCostBaseline); err == nil {
			entry = cost.OwnerCost{
				InputCacheHitTokens:  usage.InputCacheHitTokens,
				InputCacheMissTokens: usage.InputCacheMissTokens,
				OutputTokens:         usage.OutputTokens,
				TotalInputTokens:     usage.InputCacheHitTokens + usage.InputCacheMissTokens,
				Source:               cost.SourceTranscript,
			}
		}
	}
	cost.RecordOwner(state.Cost, entry)
	state.OwnerCostCompleted = true
}
