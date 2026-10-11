package protocol

import (
	"fmt"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
	"maps"
)

// WorkerExecutionIdentity admits a current worker or its signed resume. Job
// ownership remains with the native executor; issuance and recovery stay here.
func (s *State) WorkerExecutionIdentity(actionID, attemptID, resumeActionID string, cd *compiler.CompiledDefinition) (WorkerInput, string, error) {
	if s.ResetPending() || !s.parentReviewReady() {
		return WorkerInput{}, "", fmt.Errorf("worker execution is held by the current control boundary")
	}
	input, current := s.WorkerInput(actionID, cd)
	if !current || input.Pending.AttemptID != attemptID {
		return WorkerInput{}, "", fmt.Errorf("worker execution has no current issued action/attempt")
	}
	if receipt, known := s.SpawnReceipts[actionID]; known && receipt.Status == SpawnStatusUnknown {
		return WorkerInput{}, "", fmt.Errorf("worker execution is UNKNOWN; observe the original dispatch")
	}
	if resumeActionID == "" {
		if recovery, waiting := s.pendingAttemptRecovery(actionID, attemptID); waiting && (recovery.Action == RecoveryAsk || recovery.Action == RecoveryWait) {
			return WorkerInput{}, "", fmt.Errorf("worker execution awaits its original recovery decision or native effect")
		}
		return input, "", nil
	}
	intent, issued := s.PendingHostActions[resumeActionID]
	if !issued || intent.Operation != HostActionResumeAgent || intent.Resume == nil || intent.Resume.WorkerActionID != actionID || intent.Resume.AttemptID != attemptID {
		return WorkerInput{}, "", fmt.Errorf("worker resume has no matching issued intent")
	}
	if _, consumed := s.HostActionReceipts[resumeActionID]; consumed {
		return WorkerInput{}, "", fmt.Errorf("worker resume intent is already consumed")
	}
	receipt, known := s.SpawnReceipts[actionID]
	if !known || receipt.Status != SpawnStatusSpawned || receipt.agentIdentity() != intent.Resume.Identity {
		return WorkerInput{}, "", fmt.Errorf("worker resume differs from the original native identity")
	}
	return input, intent.Resume.Identity, nil
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
	Scope                    *WorkerScope                            `json:"scope,omitempty"`
	ApprovedPlan             *GranularityReview                      `json:"approvedPlan,omitempty"`
	ApprovedChecks           []DevelopmentCheck                      `json:"approvedChecks,omitempty"`
	Review                   *WorkerReviewInput                      `json:"review,omitempty"`
	Validation               *ValidationInput                        `json:"validation,omitempty"`
}

// WorkerScope carries confirmed child responsibilities without narrowing the
// complete requirement or the review of regressions caused by its changes.
type WorkerScope struct {
	Owner            string            `json:"owner"`
	AcceptanceOwners map[string]string `json:"acceptanceOwners"`
}

type WorkerReviewInput struct {
	Action              review.Action `json:"action"`
	RequirementRevision string        `json:"requirementRevision"`
	AuthorizationID     string        `json:"authorizationId,omitempty"`
}

func (s *State) WorkerInputs(cd *compiler.CompiledDefinition) map[string]WorkerInput {
	values := map[string]WorkerInput{}
	for id := range s.PendingActions {
		if input, ok := s.WorkerInput(id, cd); ok {
			values[id] = input
		}
	}
	return values
}

// WorkerInput projects one action through the same issuance owner as WorkerInputs.
func (s *State) WorkerInput(id string, cd *compiler.CompiledDefinition) (WorkerInput, bool) {
	if s.Validation == nil || s.Validation.Document.Content == "" || s.requirementChangePending() || s.abortRequested() {
		return WorkerInput{}, false
	}
	pending, issued := s.PendingActions[id]
	if !issued {
		return WorkerInput{}, false
	}
	attempt, ok := s.Attempts[pending.Task]
	if !ok || attempt.ID != pending.AttemptID || attempt.ActionID != id || attempt.AwaitingResult {
		return WorkerInput{}, false
	}
	if _, rejected := s.RejectedResults[id]; rejected {
		return WorkerInput{}, false
	}
	if result, delivered := s.Results[id]; delivered && result.Validation != nil && (s.validationRole(pending.Task, cd) == "engine.development.batch.v2" || s.validationRole(pending.Task, cd) == "engine.repair.v2") && !s.developmentContinuation(id, cd) {
		return WorkerInput{}, false
	}
	input := WorkerInput{RunID: s.RunID, Provider: s.RunProvider, VCS: s.VCSName, Pending: pending, RequirementDocument: s.Validation.Document}
	if s.SliceChild != nil {
		input.Scope = &WorkerScope{Owner: s.SliceChild.SliceID, AcceptanceOwners: maps.Clone(s.Topology.ACOwners)}
	}
	if value, ok := s.issuedValidationInput(id, pending, cd); ok {
		input.Handler = value.Handler
		input.Validation = &value
	} else if pending.ReviewSeries != "" {
		series, ok := s.ReviewSeries.Series(pending.ReviewSeries)
		if !ok {
			return WorkerInput{}, false
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
		return WorkerInput{}, false
	}
	if input.Handler == "engine.qa.execution.v2" {
		wave := s.validationWave(input.Validation.Binding.WaveID)
		if wave == nil || !s.qaComparisonReady(wave.Targets[pending.Task.String()], cd) {
			return WorkerInput{}, false
		}
	}
	if input.Handler == "engine.repair.v2" {
		input.ApprovedChecks = s.fullProjectChecks()
		_, plan, _ := s.confirmedDevelopmentPlan()
		if plan != nil {
			for _, batch := range plan.Batches {
				for _, check := range batch.Checks {
					if check.Purpose == "" {
						check.ID = batch.ID + ":" + check.ID
						input.ApprovedChecks = append(input.ApprovedChecks, check)
					}
				}
			}
		}
	}
	if input.Handler == "engine.repair.v2" || input.Review != nil && input.Review.Action == review.StartReadiness {
		_, input.ApprovedPlan, _ = s.confirmedDevelopmentPlan()
	}
	return input, true
}
