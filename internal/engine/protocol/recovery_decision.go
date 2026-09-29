package protocol

import (
	"fmt"

	"formal-gates/internal/engine/runtime"
)

func (s *State) recoveryRequest(request string) (RecoveryRecord, bool) {
	for _, record := range s.RecoveryRecords {
		if record.Action == RecoveryAsk && record.RequestID == request {
			return record, true
		}
	}
	return RecoveryRecord{}, false
}

// recoveryAttempt keeps authorization on the original, still-owed execution.
func (e *Engine) recoveryAttempt(s *State, record RecoveryRecord) (PendingAction, Attempt, bool) {
	pending, exists := s.PendingActions[record.ActionID]
	attempt := s.Attempts[pending.Task]
	if !exists {
		attempt = s.EndedAttempts[record.Task]
		if !attempt.AwaitingResult {
			return PendingAction{}, Attempt{}, false
		}
		pending = PendingAction{ActionID: attempt.ActionID, Task: attempt.Task, Step: string(attempt.Step), AttemptID: attempt.ID}
	}
	if pending.ActionID != record.ActionID || attempt.ID != record.AttemptID || attempt.ActionID != record.ActionID {
		return PendingAction{}, Attempt{}, false
	}
	if attempt.Business && !s.endedWorkCurrent(attempt, e.cfg.Definition) {
		return PendingAction{}, Attempt{}, false
	}
	return pending, attempt, true
}

// advanceRecoveries applies original routes and user decisions through the
// same native host effects. Their durable receipts, not a second progress flag,
// determine whether the original session can resume or be replaced.
func (e *Engine) advanceRecoveries(s *State, revision uint64) error {
	for _, record := range s.RecoveryRecords {
		action := record.Action
		if record.RequestID == "" || (action != RecoveryAsk && action != RecoveryResumeAttempt && action != RecoveryNewAttempt) {
			continue
		}
		pending, attempt, current := e.recoveryAttempt(s, record)
		if action == RecoveryAsk {
			ask := s.PendingAsks[record.RequestID]
			if !current && !ask.Resolved {
				ask.Resolved = true
				s.PendingAsks[record.RequestID] = ask
			}
			d, decided := s.Decisions[record.RequestID]
			if !decided {
				continue
			}
			switch d.Choice {
			case "resume":
				action = RecoveryResumeAttempt
			case "fresh":
				action = RecoveryNewAttempt
			default:
				continue
			}
		}
		if !current {
			continue
		}
		correlation := record.RequestID
		if err := e.applyAttemptRecovery(s, pending, attempt, action, correlation, revision); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyAttemptRecovery(s *State, pending PendingAction, attempt Attempt, action RecoveryAction, correlation string, revision uint64) error {
	operation := HostActionResumeAgent
	if action == RecoveryNewAttempt || attempt.Attempts+1 >= attempt.MaxAttempts {
		operation = HostActionTerminateAgent
	}
	// An effect already signed for this recovery owns both its operation and cost.
	for _, intent := range s.PendingHostActions {
		if intent.Correlation == correlation {
			return nil
		}
	}
	var executed bool
	for _, receipt := range s.HostActionReceipts {
		if receipt.Correlation != correlation {
			continue
		}
		if receipt.Status != HostActionStatusExecuted || receipt.LifecycleEvidence == nil {
			return nil
		}
		operation = receipt.Operation
		executed = true
	}
	if !executed {
		for _, history := range s.HostActionFailures {
			for _, receipt := range history {
				if receipt.Correlation == correlation {
					return nil
				}
			}
		}
	}
	if executed && operation == HostActionResumeAgent {
		return nil
	}
	receipt, known := s.SpawnReceipts[pending.ActionID]
	if !known || receipt.Status == SpawnStatusUnknown {
		return nil
	}
	identity := receipt.agentIdentity()
	if !executed && (operation == HostActionResumeAgent || !s.hasLifecycleEvent(identity, LifecycleStop)) {
		reason := "Recover original attempt by " + string(action)
		intent, err := e.agentHostIntent(s, operation, pending.ActionID, identity, reason, correlation, revision)
		if err != nil {
			return err
		}
		if operation == HostActionResumeAgent {
			attempt.Attempts++
			s.Attempts[pending.Task] = attempt
		}
		s.PendingHostActions[intent.ActionID] = intent
		return nil
	}
	// Fresh execution follows observed termination and retains the original
	// finite policy and binding when the normal retry owner signs a replacement.
	attempt.Attempts++
	attempt.RetryExhausted = attempt.Attempts >= attempt.MaxAttempts
	attempt.AwaitingResult = false
	if attempt.Business {
		s.endBusinessAttempt(pending, attempt, fmt.Sprintf("interrupted execution resolved by %s", correlation), revision)
		return nil
	}
	retired, _ := s.retireAction(pending.ActionID, "", "interrupted execution resolved by "+correlation)
	if attempt.RetryExhausted {
		s.EndedAttempts[pending.Task] = attempt
		s.Tasks[pending.Task] = runtime.TaskQueued
		return nil
	}
	replacement, err := s.installReplacement(retired, revision, attempt.Bindings, attempt.Plan, attempt.Attempts, attempt.MaxAttempts)
	if err != nil {
		return err
	}
	replacement.Work, replacement.PreviousAction = attempt.Work, attempt.ActionID
	s.Attempts[pending.Task] = replacement
	e.bindBusinessAttempt(s, pending.Task)
	obsolete := s.ObsoleteActions[pending.ActionID]
	obsolete.ReplacedBy = replacement.ActionID
	s.ObsoleteActions[pending.ActionID] = obsolete
	return nil
}
