package review

import (
	"fmt"
	"maps"
	"slices"
)

// LimitInterval applies from Revision, including that revision, until the next
// entry. Revision is a state sequence, not an issued or completed round count.
type LimitInterval struct {
	Value    int    `json:"value"`
	Revision uint64 `json:"revision"`
}

// RoundExecution preserves the logical round's admission facts across dispatch
// replacement. Completed is a snapshot of the owner's existing business count,
// not another counter. Revision selects the limit in effect at admission.
type RoundExecution struct {
	Revision  uint64 `json:"roundRevision,omitempty"`
	Completed int    `json:"roundsCompleted,omitempty"`
}

func LimitAt(history []LimitInterval, revision uint64) (int, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Revision <= revision {
			return history[i].Value, true
		}
	}
	return 0, false
}

func ValidateLimits(history []LimitInterval) error {
	if len(history) == 0 {
		return fmt.Errorf("round limit history is missing")
	}
	for i, entry := range history {
		if entry.Value < 1 || entry.Value > 64 {
			return fmt.Errorf("round limit must be 1..64")
		}
		if i > 0 && entry.Revision <= history[i-1].Revision {
			return fmt.Errorf("round limit revisions are not increasing")
		}
	}
	return nil
}

// AppendLimit records changed values only. Callers creating a series supply its
// creation revision; callers applying configuration supply its application revision.
func AppendLimit(history []LimitInterval, value int, revision uint64) []LimitInterval {
	if len(history) != 0 && history[len(history)-1].Value == value {
		return history
	}
	if len(history) != 0 && history[len(history)-1].Revision == revision {
		history = slices.Clone(history)
		history[len(history)-1].Value = value
		return history
	}
	return append(slices.Clone(history), LimitInterval{Value: value, Revision: revision})
}

func SetLimit(set Set, action Action, value int, revision uint64) Set {
	series, err := set.seriesFor(action)
	if err != nil {
		return set
	}
	series.LimitHistory = AppendLimit(series.LimitHistory, value, revision)
	series.AutomaticLimit = value
	refresh(series)
	return set
}

// AdmitRound records the capacity promised to a dispatch. It does not advance
// Issued; that counter belongs to IssueRound, not retry or replacement.
func AdmitRound(set Set, action Action, dispatchID string, revision uint64) Set {
	series, err := set.seriesFor(action)
	if err != nil {
		return set
	}
	if _, recorded := series.DispatchRounds[dispatchID]; recorded {
		return set
	}
	series.DispatchRounds = maps.Clone(series.DispatchRounds)
	if series.DispatchRounds == nil {
		series.DispatchRounds = map[string]RoundExecution{}
	}
	series.DispatchRounds[dispatchID] = RoundExecution{Revision: revision, Completed: series.Completed}
	return set
}

func (s Series) validateRoundCapacity() error {
	historicalAutomatic := 0
	for _, candidate := range s.RawCandidates {
		execution, issued := s.DispatchRounds[candidate.DispatchID]
		limit, known := LimitAt(s.LimitHistory, execution.Revision)
		if !issued || !known || execution.Completed >= limit && candidate.AuthorizationID == "" {
			return fmt.Errorf("candidate %s exceeds its automatic round limit without authorization", candidate.ID)
		}
		if !ResultExcluded(s, candidate.ResultDigest) {
			historicalAutomatic = max(historicalAutomatic, limit)
		}
	}
	// Admission snapshots cannot multiply automatic slots. The bound comes
	// from executed intervals, not a later increase or decrease in policy.
	if s.Completed > historicalAutomatic+len(s.Authorizations) {
		return fmt.Errorf("completed rounds exceed their historical automatic round limit")
	}
	if occupiedExtraAuthorizations(s) < max(0, s.Completed-historicalAutomatic) {
		return fmt.Errorf("completed extra rounds lack distinct action authorizations")
	}
	return nil
}
