package protocol

import (
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

// WorkerInput is a projection of an issued action, not another work ledger.
// Review and Validation carry their existing owners' exact business context.
type WorkerInput struct {
	RunID               string                                  `json:"runId"`
	Provider            string                                  `json:"provider"`
	VCS                 string                                  `json:"vcs"`
	Pending             PendingAction                           `json:"pending"`
	Handler             string                                  `json:"handler"`
	RequirementDocument requirement.FrozenRequirementProjection `json:"requirementDocument"`
	Review              *WorkerReviewInput                      `json:"review,omitempty"`
	Validation          *ValidationInput                        `json:"validation,omitempty"`
}

type WorkerReviewInput struct {
	Action              review.Action `json:"action"`
	RequirementRevision string        `json:"requirementRevision"`
	AuthorizationID     string        `json:"authorizationId,omitempty"`
}

func (s *State) WorkerInputs(cd *compiler.CompiledDefinition) map[string]WorkerInput {
	values := map[string]WorkerInput{}
	if s.Validation == nil || s.Validation.Document.Content == "" || s.requirementChangePending() || s.abortRequested() {
		return values
	}
	validation := s.ValidationInputs(cd)
	for id, pending := range s.PendingActions {
		attempt, ok := s.Attempts[pending.Task]
		if !ok || attempt.ID != pending.AttemptID || attempt.ActionID != id || attempt.AwaitingResult {
			continue
		}
		if _, rejected := s.RejectedResults[id]; rejected {
			continue
		}
		if result, delivered := s.Results[id]; delivered && result.Validation != nil && s.validationRole(pending.Task, cd) == "engine.development.batch" && !s.developmentContinuation(id, cd) {
			continue
		}
		input := WorkerInput{RunID: s.RunID, Provider: s.RunProvider, VCS: s.VCSName, Pending: pending, RequirementDocument: s.Validation.Document}
		if value, ok := validation[id]; ok {
			input.Handler = value.Handler
			input.Validation = &value
		} else if pending.ReviewSeries != "" {
			series, ok := s.ReviewSeries.Series(pending.ReviewSeries)
			if !ok {
				continue
			}
			input.Review = &WorkerReviewInput{Action: pending.ReviewSeries, RequirementRevision: s.requirementRevisionAt(s.DispatchIssueRevisions[id]), AuthorizationID: review.AuthorizationOfDispatch(series, id)}
			for _, step := range cd.Steps {
				if step.Header.ID == pending.Task.Step {
					if payload, ok := step.Payload.(compiler.CompiledAgentStep); ok {
						input.Handler = string(payload.Handler)
					}
					break
				}
			}
		} else {
			continue
		}
		values[id] = input
	}
	return values
}
