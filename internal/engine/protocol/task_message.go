package protocol

import (
	"fmt"
	"strings"
)

// TaskMessage is a temporary user instruction for one issued action. Receiving
// it never changes the task's authorization or constitutes execution evidence.
type TaskMessage struct {
	ID        string `json:"id"`
	ActionID  string `json:"actionId"`
	AttemptID string `json:"attemptId"`
	Text      string `json:"text"`
	Status    string `json:"status,omitempty"`
	Identity  string `json:"identity,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func acceptTaskMessage(s *State, message TaskMessage, receipt bool) error {
	x, ok := s.RegularExecutions[message.ActionID]
	if !ok || message.AttemptID != x.Claim.AttemptID || message.ID == "" {
		return fmt.Errorf("MESSAGE_BINDING: exact issued action, attempt and message ID required")
	}
	old, exists := x.Messages[message.ID]
	if !receipt {
		if exists || !x.Claimed || x.Identity == "" || x.Finished || x.SupersededBy != "" || strings.TrimSpace(message.Text) == "" || message.Status != "" || message.Identity != "" || message.Reason != "" {
			return fmt.Errorf("MESSAGE_BINDING: unique message for an unfinished action required")
		}
		message.Status = "PENDING"
	} else {
		if !exists || old.Text != message.Text || !x.Claimed || message.Identity != x.Identity || message.Identity == "" {
			return fmt.Errorf("MESSAGE_BINDING: receipt differs from pending message or native identity")
		}
		switch message.Status {
		case "SENDING":
			if old.Status != "PENDING" {
				return fmt.Errorf("MESSAGE_STATE: message already attempted")
			}
		case "ACCEPTED", "FAILED":
			if old.Status != "SENDING" {
				return fmt.Errorf("MESSAGE_STATE: no delivery intent")
			}
			if message.Status == "FAILED" && strings.TrimSpace(message.Reason) == "" {
				return fmt.Errorf("MESSAGE_STATE: failure requires reason")
			}
		default:
			return fmt.Errorf("MESSAGE_STATE: unsupported receipt status")
		}
	}
	if x.Messages == nil {
		x.Messages = map[string]TaskMessage{}
	}
	x.Messages[message.ID] = message
	s.RegularExecutions[message.ActionID] = x
	return nil
}
