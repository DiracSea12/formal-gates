package protocol

import (
	"fmt"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

// WorkerExecutionIdentity admits a current worker or its signed resume. Job
// ownership remains with the native executor; issuance and recovery stay here.
func (s *State) WorkerExecutionIdentity(actionID, attemptID, resumeActionID string, cd *compiler.CompiledDefinition) (string, error) {
	if s.ResetPending() || !s.parentReviewReady() {
		return "", fmt.Errorf("worker execution is held by the current control boundary")
	}
	input, current := s.WorkerInputs(cd)[actionID]
	if !current || input.Pending.AttemptID != attemptID {
		return "", fmt.Errorf("worker execution has no current issued action/attempt")
	}
	if receipt, known := s.SpawnReceipts[actionID]; known && receipt.Status == SpawnStatusUnknown {
		return "", fmt.Errorf("worker execution is UNKNOWN; observe the original dispatch")
	}
	if resumeActionID == "" {
		if recovery, waiting := s.pendingAttemptRecovery(actionID, attemptID); waiting && (recovery.Action == RecoveryAsk || recovery.Action == RecoveryWait) {
			return "", fmt.Errorf("worker execution awaits its original recovery decision or native effect")
		}
		return "", nil
	}
	intent, issued := s.PendingHostActions[resumeActionID]
	if !issued || intent.Operation != HostActionResumeAgent || intent.Resume == nil || intent.Resume.WorkerActionID != actionID || intent.Resume.AttemptID != attemptID {
		return "", fmt.Errorf("worker resume has no matching issued intent")
	}
	if _, consumed := s.HostActionReceipts[resumeActionID]; consumed {
		return "", fmt.Errorf("worker resume intent is already consumed")
	}
	receipt, known := s.SpawnReceipts[actionID]
	if !known || receipt.Status != SpawnStatusSpawned || receipt.agentIdentity() != intent.Resume.Identity {
		return "", fmt.Errorf("worker resume differs from the original native identity")
	}
	return intent.Resume.Identity, nil
}

// WorkerInput is a projection of an issued action, not another work ledger.
// Review and Validation carry their existing owners' exact business context.
type WorkerInput struct {
	AvailableRouteCandidates []string                                `json:"availableRouteCandidates,omitempty"`
	RunID                    string                                  `json:"runId"`
	Provider                 string                                  `json:"provider"`
	VCS                      string                                  `json:"vcs"`
	Pending                  PendingAction                           `json:"pending"`
	Handler                  string                                  `json:"handler"`
	RequirementDocument      requirement.FrozenRequirementProjection `json:"requirementDocument"`
	Review                   *WorkerReviewInput                      `json:"review,omitempty"`
	Validation               *ValidationInput                        `json:"validation,omitempty"`
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
		if result, delivered := s.Results[id]; delivered && result.Validation != nil && (s.validationRole(pending.Task, cd) == "engine.development.batch" || s.validationRole(pending.Task, cd) == "engine.repair") && !s.developmentContinuation(id, cd) {
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
