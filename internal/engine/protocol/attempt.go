package protocol

import (
	"fmt"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

type RejectedResult struct {
	ActionID  string          `json:"actionId"`
	AttemptID string          `json:"attemptId"`
	Task      runtime.TaskKey `json:"task"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason"`
	Payload   WorkerResult    `json:"payload"`
}

func (e *Engine) businessTask(key runtime.TaskKey) bool {
	for _, step := range e.cfg.Definition.Steps {
		if step.Header.ID == key.Step {
			_, ok := step.Payload.(compiler.CompiledAgentStep)
			return ok
		}
	}
	return false
}

// endInvalidAttempt closes one identified business execution. The ended Attempt
// owns its retry policy until it is replaced; it never records a business PASS.
func (s *State) endInvalidAttempt(actionID, status, reason string, record WorkerResult, nextRevision uint64) error {
	pending, ok := s.PendingActions[actionID]
	if !ok {
		return &RejectedError{Code: CodeUnknownAction, Detail: "invalid result has no current dispatch"}
	}
	attempt := s.Attempts[pending.Task]
	s.RejectedResults[actionID] = RejectedResult{ActionID: actionID, AttemptID: attempt.ID, Task: pending.Task, Status: status, Reason: reason, Payload: record}
	attempt.Attempts++
	attempt.RetryExhausted = attempt.Attempts >= attempt.MaxAttempts
	s.endBusinessAttempt(pending, attempt, status+": "+reason, nextRevision)
	return nil
}

// endBusinessAttempt owns finite execution completion; wire evidence stays in
// its receipt or worker-result owner. The caller accounts for this failure once.
func (s *State) endBusinessAttempt(pending PendingAction, attempt Attempt, reason string, nextRevision uint64) {
	actionID := pending.ActionID
	s.retireAction(actionID, "", reason)
	s.EndedAttempts[pending.Task] = attempt
	// Tasks projects the scheduling position of the current execution. The old
	// execution is immutable in EndedAttempts/ObsoleteActions before re-queuing.
	s.Tasks[pending.Task] = runtime.TaskQueued
	s.routingReleasedCapacity = true
	s.RecoveryRecords = append(s.RecoveryRecords, RecoveryRecord{ActionID: actionID, Task: pending.Task, AttemptID: attempt.ID, Class: authoring.FailureTransientEngine, Action: RecoveryWait, InterruptionReason: s.RejectedResults[actionID].Payload.InterruptionReason, Detail: fmt.Sprintf("%s; attempts %d/%d", reason, attempt.Attempts, attempt.MaxAttempts), Revision: nextRevision})
	if wave := s.currentValidationWave(); wave != nil && wave.Status == "COLLECTING" {
		for _, target := range wave.Targets {
			if target.Task == pending.Task {
				target.Status = "RUNTIME_ERROR"
				if attempt.RetryExhausted {
					target.Status = "RETRY_EXHAUSTED"
				}
			}
		}
	}
}

// endedWorkCurrent separates an ended execution from its unfinished duty.
// Task replacement and each business owner decide applicability; temporary
// barriers and dispatch capacity do not terminate the duty.
func (s *State) endedWorkCurrent(a Attempt, cd *compiler.CompiledDefinition) bool {
	if !a.Business || s.ObsoleteActions[a.ActionID].ReplacedBy != "" {
		return false
	}
	if result, ok := s.Results[a.ActionID]; ok && result.Outcome == OutcomePass {
		return false
	}
	for i := len(s.RequirementRevisions) - 1; i >= 0; i-- {
		for _, choice := range s.RequirementRevisions[i].WorkDecisions {
			if choice.Work.Task == a.Task && choice.Disposition == RequirementWorkRedo {
				return false
			}
		}
	}
	if series, ok := s.ReviewSeries.Series(s.PendingReviewSeries(a.Task, cd)); ok && s.issuedRevisionOf(a.ActionID) < series.FreshFloor {
		return false
	}
	if a.Work == nil {
		return true
	}
	if !s.prewaveWorkCurrent(*a.Work, cd) || s.caseSetWaiver(s.validationMode(a.Task)) != "" {
		return false
	}
	return s.validationWorkOwed(*a.Work, cd)
}

func (s *State) projectRetryTasks(view *decision.State, cd *compiler.CompiledDefinition) {
	for key, attempt := range s.EndedAttempts {
		if !s.endedWorkCurrent(attempt, cd) || attempt.AwaitingResult || s.validationDispatchBlocked(attempt.ActionID) {
			continue
		}
		blocked := decision.WaitReason("")
		if attempt.RetryExhausted {
			blocked = decision.WaitCapacity
		}
		// A retry fulfills its original logical obligation, including a pre-wave
		// review; it does not request the next semantic review round.
		tasks := view.RequiredTasks[key.Step]
		replaced := false
		for i := range tasks {
			if tasks[i].Task == key {
				tasks[i].BlockedBy = blocked
				replaced = true
			}
		}
		if !replaced {
			// Review series have one semantic obligation. The pending retry
			// continues it instead of advancing the series round.
			if s.reviewSeriesBooked(s.PendingReviewSeries(key, cd)) {
				tasks = nil
			}
			tasks = append(tasks, decision.RequiredTask{Task: key, BlockedBy: blocked})
		}
		view.RequiredTasks[key.Step] = tasks
	}
}

// validationGrantUsed 报告某验证 Ask 通道对 target 的一次性授权决定是否已用
// （同 kind＋choice＋target 扫已落决定）。RETRY Ask（approve）与预算处置 Ask
// （retry）两通道各自一次性；跨通道先后两次都是用户显式授权的独立一次性增量，
// 非预先累积。
func (s *State) validationGrantUsed(kind, choice, target string) bool {
	for id, d := range s.Decisions {
		a := s.PendingAsks[id]
		if d.Choice == AskOptionID(choice) && a.Validation != nil && a.Validation.Kind == kind && a.Validation.Target == target {
			return true
		}
	}
	return false
}
func retryReason(record RejectedResult) string { return strings.TrimSpace(record.Reason) }

// restoreRetryBinding transfers the ended execution's exact logical identity.
func (s *State) restoreRetryBinding(action decision.IssuedAction, nextRevision uint64) {
	old, ok := s.EndedAttempts[action.Task]
	if !ok {
		return
	}
	current := s.Attempts[action.Task]
	current.Work = old.Work
	current.Business = old.Business
	current.Attempts = old.Attempts
	current.MaxAttempts = old.MaxAttempts
	current.PreviousAction = old.ActionID
	s.Attempts[action.Task] = current
	if pending := s.PendingActions[action.ActionID]; s.reviewSeriesBooked(pending.ReviewSeries) {
		s.ReviewSeries = review.BindReplacementDispatch(s.ReviewSeries, s.RunID, pending.ReviewSeries, old.ActionID, action.ActionID)
	}
	obsolete := s.ObsoleteActions[old.ActionID]
	obsolete.ReplacedBy = action.ActionID
	s.ObsoleteActions[old.ActionID] = obsolete
	delete(s.EndedAttempts, action.Task)
}

func (s *State) PendingReviewSeries(key runtime.TaskKey, cd *compiler.CompiledDefinition) review.Action {
	for _, step := range cd.Steps {
		if step.Header.ID == key.Step {
			if a, ok := step.Payload.(compiler.CompiledAgentStep); ok {
				return review.Action(a.ReviewSeries)
			}
		}
	}
	return ""
}

func (e *Engine) bindBusinessAttempt(s *State, key runtime.TaskKey) {
	a, ok := s.Attempts[key]
	if !ok {
		return
	}
	a.Business = e.businessTask(key)
	if s.validationRole(key, e.cfg.Definition) != "" && a.Work == nil {
		b := s.validationBinding(key, e.cfg.Definition)
		a.Work = &b
	}
	s.Attempts[key] = a
	if a.Work != nil {
		s.DispatchBindings[a.ActionID] = *a.Work
	}
}
