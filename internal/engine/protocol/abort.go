package protocol

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// AbortDecision derives the terminal authority from the accepted user decision.
func (s *State) AbortDecision() (RecordedDecision, bool) {
	var chosen RecordedDecision
	for _, d := range s.Decisions {
		if d.Choice == "abort" && (d.Control == ControlAbort || d.Control == ControlValidation || d.Control == ControlRecovery) && (chosen.Revision == 0 || d.Revision < chosen.Revision) {
			chosen = d
		}
	}
	return chosen, chosen.Revision != 0
}
func (s *State) abortRequested() bool { _, ok := s.AbortDecision(); return ok }

// advanceAbort stops business progression and reconciles signed external work.
// Termination uses the same typed intent and receipt path as TerminateAgent.
func (e *Engine) advanceAbort(s *State, nextRevision uint64) (bool, error) {
	d, ok := s.AbortDecision()
	if !ok || s.Phase == runtime.PhaseTerminal {
		return false, nil
	}
	changed := false
	for id, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && len(s.HostActionFailures[id]) > 0 {
			if receipt, ok := s.HostActionReceipts[id]; !ok || receipt.Status != HostActionStatusUnknown {
				delete(s.PendingHostActions, id)
				changed = true
			}
		}
	}
	for id, ask := range s.PendingAsks {
		if !ask.Resolved {
			ask.Resolved = true
			s.PendingAsks[id] = ask
			changed = true
		}
	}
	changed = e.executeHeldFailureRoutes(s, nextRevision) || changed
	changed = e.executeHeldSettles(s, nextRevision) || changed
	actions := map[string]bool{}
	for id := range s.PendingActions {
		actions[id] = true
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult {
			actions[attempt.ActionID] = true
		}
	}
	ids := make([]string, 0, len(actions))
	for id := range actions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		receipt, known := s.SpawnReceipts[id]
		if !known || receipt.Status == SpawnStatusUnknown {
			continue
		}
		if receipt.Status == SpawnStatusFailed || s.hasLifecycleEvent(receipt.agentIdentity(), LifecycleStop) {
			pending, retired := s.retireAction(id, "", "run aborted by "+d.RequestID)
			delete(s.EndedAttempts, pending.Task)
			changed = retired || changed
			continue
		}
		exists := false
		for _, intent := range s.PendingHostActions {
			if intent.Terminate != nil && intent.Terminate.WorkerActionID == id {
				exists = true
			}
		}
		if exists {
			continue
		}
		reason := "User aborted the run"
		if d.Validation != nil {
			reason = d.Validation.Reason
		}
		intent, err := e.agentHostIntent(s, HostActionTerminateAgent, id, receipt.agentIdentity(), reason, "", nextRevision)
		if err != nil {
			return changed, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	waiting := len(s.PendingActions) > 0 || len(s.PendingHostActions) > 0
	for _, attempt := range s.EndedAttempts {
		waiting = waiting || attempt.AwaitingResult
	}
	if !waiting {
		if err := s.TransitionPhase(runtime.PhaseTerminal); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func (s *State) abortPlan(plan *decision.Plan) {
	if !s.abortRequested() || s.Phase == runtime.PhaseTerminal {
		return
	}
	plan.Frontier = nil
	plan.Next = decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitTasksInFlight}}
	if len(s.PendingHostActions) > 0 {
		plan.Next = decision.NextResult{Kind: decision.KindHostAction, HostAction: &decision.HostActionPayload{}}
	}
}

func (e *Engine) agentHostIntent(s *State, operation HostActionOperation, workerActionID, identity, reason, correlation string, revision uint64) (HostActionIntent, error) {
	if strings.TrimSpace(identity) == "" {
		return HostActionIntent{}, &RejectedError{Code: CodeEventSchemaInvalid, Detail: "agent host action identity is empty"}
	}
	pending, ok := s.PendingActions[workerActionID]
	attempt := s.Attempts[pending.Task]
	if !ok {
		for task, ended := range s.EndedAttempts {
			if ended.ActionID == workerActionID && ended.AwaitingResult {
				pending = PendingAction{ActionID: workerActionID, Task: task, AttemptID: ended.ID}
				attempt = ended
				ok = true
				break
			}
		}
	}
	if !ok {
		return HostActionIntent{}, &RejectedError{Code: CodeUnknownAction, Detail: fmt.Sprintf("unknown worker action %q", workerActionID)}
	}
	if attempt.ID != pending.AttemptID {
		return HostActionIntent{}, &RejectedError{Code: CodeStaleAttempt, Detail: "worker action has no matching current Attempt"}
	}
	receipt, known := s.SpawnReceipts[workerActionID]
	if !known || receipt.agentIdentity() != identity {
		return HostActionIntent{}, &RejectedError{Code: CodeIntentMismatch, Detail: "agent identity is not bound to worker action"}
	}
	payload := &AgentHostAction{WorkerActionID: workerActionID, AttemptID: attempt.ID, Identity: identity, Reason: reason}
	if operation == HostActionResumeAgent {
		if _, known := s.DispatchBindings[workerActionID]; known && s.Validation != nil {
			input := s.validationInput(pending, e.cfg.Definition)
			payload.Input = &input
		}
	}
	intent := HostActionIntent{ActionID: "hact:" + string(operation) + ":" + strconv.FormatUint(revision, 10), Operation: operation, PayloadDigest: digestOfCanonical(payload), Correlation: strings.TrimSpace(correlation), Revision: revision}
	if operation == HostActionResumeAgent {
		intent.Resume = payload
	} else {
		intent.Terminate = payload
	}
	return intent, nil
}

func (r SpawnReceipt) agentIdentity() string {
	if r.LifecycleIdentity != "" {
		return r.LifecycleIdentity
	}
	return r.Correlation
}
