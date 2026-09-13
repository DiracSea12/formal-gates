package facade

import (
	"encoding/json"
	"fmt"
)

// zcodeTurnObservation follows the native app-server event envelope. A send
// acknowledgement only queues input; the matching completed turn owns the answer.
type zcodeTurnObservation struct {
	sessionID string
	inputID   string
	turnID    string
	sequence  int64
	seen      bool
	completed bool
	answer    json.RawMessage
}

func newZCodeTurnObservation(sessionID, inputID string) (*zcodeTurnObservation, error) {
	if sessionID == "" || inputID == "" {
		return nil, fmt.Errorf("ZCODE_BINDING: session and input identity are required")
	}
	return &zcodeTurnObservation{sessionID: sessionID, inputID: inputID}, nil
}

func (o *zcodeTurnObservation) observe(raw json.RawMessage) error {
	var event struct {
		SessionID string `json:"sessionId"`
		TurnID    string `json:"turnId"`
		Seq       *int64 `json:"seq"`
		Type      string `json:"type"`
		Payload   struct {
			InputID    string `json:"inputId"`
			Response   string `json:"response"`
			ResultType string `json:"resultType"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return fmt.Errorf("ZCODE_EVENT: %w", err)
	}
	if event.SessionID != o.sessionID || event.Seq == nil || *event.Seq < 0 {
		return fmt.Errorf("ZCODE_EVENT: session or sequence binding differs")
	}
	// Replayed events can be returned on subscribe. They are not another turn.
	if o.seen && *event.Seq <= o.sequence {
		return nil
	}
	o.seen, o.sequence = true, *event.Seq
	switch event.Type {
	case "turn.started":
		if event.Payload.InputID != o.inputID {
			return nil
		}
		if event.TurnID == "" || o.turnID != "" {
			return fmt.Errorf("ZCODE_EVENT: duplicate or missing turn identity")
		}
		o.turnID = event.TurnID
	case "turn.failed", "turn.completed":
		if event.Payload.InputID != o.inputID {
			return nil
		}
		if o.turnID == "" || event.TurnID != o.turnID || o.completed {
			return fmt.Errorf("ZCODE_EVENT: terminal turn binding differs")
		}
		if event.Type == "turn.failed" || event.Payload.ResultType != "success" {
			return fmt.Errorf("ZCODE_EXECUTION: native turn failed (%s)", event.Payload.ResultType)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(event.Payload.Response), &object); err != nil || object == nil {
			return fmt.Errorf("ZCODE_RESULT: completed response is not a result object")
		}
		o.answer = json.RawMessage(event.Payload.Response)
		o.completed = true
	}
	return nil
}

func (o *zcodeTurnObservation) result() (json.RawMessage, error) {
	if !o.completed {
		return nil, fmt.Errorf("UNKNOWN: no successful matching ZCode turn observed")
	}
	return append(json.RawMessage(nil), o.answer...), nil
}
