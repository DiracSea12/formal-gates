package protocol

import (
	"encoding/json"
	"fmt"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/review"
)

// ReviewGrantRequest binds one offered extra round to the exact review and
// confirmation facts shown to the user. PendingAsk owns the offer; the series
// authorization ledger owns the accepted grant, never the offer itself.
type ReviewGrantRequest struct {
	Action                 review.Action `json:"action"`
	RequirementRevision    string        `json:"requirementRevision"`
	ConfirmationGeneration uint64        `json:"confirmationGeneration"`
	Completed              int           `json:"completed"`
	AutomaticLimit         int           `json:"automaticLimit"`
	ExtraAuthorized        int           `json:"extraAuthorized"`
	Issued                 int           `json:"issued"`
}

func (s *State) reviewGrantBinding(action review.Action) ReviewGrantRequest {
	series, _ := s.ReviewSeries.Series(action)
	return ReviewGrantRequest{Action: action, RequirementRevision: s.RequirementRevision(), ConfirmationGeneration: s.requirementGeneration(),
		Completed: series.Completed, AutomaticLimit: series.AutomaticLimit, ExtraAuthorized: series.ExtraAuthorized, Issued: series.Issued}
}

// reconcileReviewGrantAsks shares the normal transaction and Drive lifecycle.
// Stable offer identities preserve a decline across re-entry; changed facts
// retire stale offers without recording a user decision on their behalf.
func (e *Engine) reconcileReviewGrantAsks(state *State) bool {
	if state.requirementChangePending() {
		return false
	}
	changed := false
	view := state.decisionView(e.cfg.Definition)
	offers := map[string]ReviewGrantRequest{}
	for stepID, tasks := range view.RequiredTasks {
		mark := e.reviewSeriesOf(stepID)
		series, ok := state.ReviewSeries.Series(mark)
		if !ok || !state.reviewRoundOwed(mark) || series.RemainingCapacity != 0 {
			continue
		}
		for _, task := range tasks {
			if task.BlockedBy != decision.WaitCapacity {
				continue
			}
			eligible := true
			for _, step := range e.cfg.Definition.Steps {
				if step.Header.ID != stepID {
					continue
				}
				for _, dep := range step.Header.Dependencies {
					if !view.StepSatisfied(dep, e.cfg.Definition) {
						eligible = false
						break
					}
				}
			}
			if !eligible {
				continue
			}
			offer := state.reviewGrantBinding(mark)
			data, _ := json.Marshal(offer)
			id := "review-grant:" + encoder.Digest(data)
			offers[id] = offer
		}
	}
	for id, ask := range state.PendingAsks {
		if ask.RoundGrant == nil || ask.Resolved {
			continue
		}
		if _, current := offers[id]; !current {
			ask.Resolved = true
			state.PendingAsks[id] = ask
			changed = true
		}
	}
	for id, offer := range offers {
		if _, exists := state.PendingAsks[id]; exists {
			continue
		}
		state.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlReviewRoundGrant, RoundGrant: &offer,
			Options: []AskOption{{ID: grantOption, Label: fmt.Sprintf("Authorize one additional %s round", offer.Action)}, {ID: declineOption, Label: "Decline the additional round"}}}
		changed = true
	}
	return changed
}
