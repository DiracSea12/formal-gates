package protocol

import (
	"fmt"
	"sort"
	"strings"
)

// TaskMessage is supplementary user text bound to an already issued action.
// It changes neither its confirmed scope nor task, attempt, result or budgets.
type TaskMessage struct {
	ID        string `json:"id"`
	ActionID  string `json:"actionId"`
	AttemptID string `json:"attemptId"`
	Identity  string `json:"identity"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Revision  uint64 `json:"revision"`
}

func (e *Engine) QueueTaskMessage(action, text string, revision uint64, fingerprint Observation) (TaskMessage, error) {
	e = e.observing(fingerprint)
	current, state, err := e.load()
	if err != nil {
		return TaskMessage{}, err
	}
	if state == nil || current != revision {
		return TaskMessage{}, fmt.Errorf("MESSAGE_BINDING: current run revision required")
	}
	pending, active := state.PendingActions[action]
	spawn, started := state.SpawnReceipts[action]
	if _, issued := state.WorkerInput(action, e.cfg.Definition); !issued || !active || !started || !inflightWitnessReceipt(spawn.Status) || spawn.agentIdentity() == "" || strings.TrimSpace(text) == "" || state.abortRequested() {
		return TaskMessage{}, fmt.Errorf("MESSAGE_BINDING: supplementary text requires a started unfinished action")
	}
	message := TaskMessage{ID: digestOfCanonical(struct {
		Run, Action, Text string
		Revision          uint64
	}{state.RunID, action, text, current}), ActionID: action, AttemptID: pending.AttemptID, Identity: spawn.agentIdentity(), Text: text, Status: "PENDING", Revision: current + 1}
	if state.TaskMessages == nil {
		state.TaskMessages = map[string]TaskMessage{}
	}
	state.TaskMessages[message.ID] = message
	_, err = e.commit(state, current, fingerprint)
	return message, err
}
func (e *Engine) ClaimTaskMessage(action, attempt, identity string, fingerprint Observation, begin bool) (*TaskMessage, error) {
	e = e.observing(fingerprint)
	revision, state, err := e.load()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("MESSAGE_BINDING: run unavailable")
	}
	needed := false
	for _, message := range state.TaskMessages {
		if message.ActionID == action && message.AttemptID == attempt && (message.Status == "PENDING" || message.Status == "SENDING") {
			needed = true
			break
		}
	}
	if !needed {
		return nil, nil
	}
	spawn, known := state.SpawnReceipts[action]
	if !known || spawn.agentIdentity() != identity {
		return nil, fmt.Errorf("MESSAGE_BINDING: original native identity required")
	}
	abandoned := false
	if begin {
		for id, message := range state.TaskMessages {
			if message.ActionID == action && message.AttemptID == attempt && message.Identity == identity && message.Status == "SENDING" {
				message.Status = "FAILED"
				message.Reason = "prior delivery has no acknowledgement; delivery outcome unknown; not automatically resent"
				state.TaskMessages[id] = message
				abandoned = true
			}
		}
	}
	messages := []TaskMessage{}
	for _, message := range state.TaskMessages {
		if message.ActionID == action && message.AttemptID == attempt && message.Identity == identity && message.Status == "PENDING" {
			messages = append(messages, message)
		}
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].Revision < messages[j].Revision })
	if len(messages) == 0 {
		if abandoned {
			_, err = e.commit(state, revision, fingerprint)
			return nil, err
		}
		return nil, nil
	}
	message := messages[0]
	message.Status = "SENDING"
	state.TaskMessages[message.ID] = message
	if _, err := e.commit(state, revision, fingerprint); err != nil {
		return nil, err
	}
	return &message, nil
}
func (e *Engine) RecordTaskMessage(message TaskMessage, fingerprint Observation) error {
	e = e.observing(fingerprint)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("MESSAGE_BINDING: run unavailable")
	}
	original, found := state.TaskMessages[message.ID]
	if !found || message.ActionID != original.ActionID || message.AttemptID != original.AttemptID || message.Identity != original.Identity || message.Text != original.Text || message.Revision != original.Revision {
		return fmt.Errorf("MESSAGE_BINDING: receipt differs from original supplementary text")
	}
	if original.Status == message.Status && original.Reason == message.Reason {
		return nil
	}
	// A later invocation failure cannot revoke observed delivery.
	if original.Status == "ACCEPTED" && message.Status == "FAILED" {
		return nil
	}
	if original.Status != "SENDING" || message.Status != "ACCEPTED" && message.Status != "FAILED" || message.Status == "FAILED" && strings.TrimSpace(message.Reason) == "" {
		return fmt.Errorf("MESSAGE_STATE: receipt requires an actual pending delivery outcome")
	}
	state.TaskMessages[message.ID] = message
	_, err = e.commit(state, revision, fingerprint)
	return err
}

// A closed action cannot consume queued supplementary text. In-flight delivery
// remains owned by its native acknowledgement or interrupted-delivery handling.
func (s *State) retireTaskMessages() {
	for id, message := range s.TaskMessages {
		if message.Status != "PENDING" {
			continue
		}
		if _, active := s.PendingActions[message.ActionID]; !active {
			message.Status = "FAILED"
			message.Reason = "target action finished before delivery acknowledgement; not automatically resent"
			s.TaskMessages[id] = message
		}
	}
}
