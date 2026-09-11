package facade

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// workBuddyResult observes one native invocation's stream. It does not admit an
// engine result: the existing result writer must still check the action binding.
type workBuddyResult struct {
	Identity string
	Answer   json.RawMessage
}

// observeWorkBuddyResult is shared by live collection and interrupted-process
// recovery. A resume must supply the previously bound native session identity.
// SDK task notifications and assistant text are never terminal result evidence.
func observeWorkBuddyResult(stream io.Reader, expectedIdentity string) (workBuddyResult, error) {
	return observeWorkBuddyStream(stream, expectedIdentity, nil)
}

func observeWorkBuddyStream(stream io.Reader, expectedIdentity string, attached func(string) error) (workBuddyResult, error) {
	attachedIdentity := false
	observed := workBuddyResult{Identity: expectedIdentity}
	scan := bufio.NewScanner(stream)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	complete := false
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg struct {
			Type       string          `json:"type"`
			Subtype    string          `json:"subtype"`
			SessionID  string          `json:"session_id"`
			UUID       string          `json:"uuid"`
			IsError    *bool           `json:"is_error"`
			Result     string          `json:"result"`
			Structured json.RawMessage `json:"structured_output"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			return observed, fmt.Errorf("WORKBUDDY_STREAM: %w", err)
		}
		if msg.SessionID != "" {
			if observed.Identity != "" && observed.Identity != msg.SessionID {
				return observed, fmt.Errorf("WORKBUDDY_IDENTITY: stream session differs from bound identity")
			}
			observed.Identity = msg.SessionID
		}
		if !attachedIdentity && msg.Type == "system" && msg.Subtype == "init" && msg.SessionID != "" {
			if attached != nil {
				if err := attached(msg.SessionID); err != nil {
					return observed, err
				}
			}
			attachedIdentity = true
		}
		if msg.Type != "result" {
			continue
		}
		if complete {
			return observed, fmt.Errorf("WORKBUDDY_RESULT: multiple terminal results in one invocation")
		}
		if msg.SessionID == "" || msg.UUID == "" {
			return observed, fmt.Errorf("WORKBUDDY_RESULT: missing native identity or result UUID")
		}
		if msg.Subtype != "success" || msg.IsError == nil || *msg.IsError {
			return observed, fmt.Errorf("WORKBUDDY_RESULT: unsuccessful native result (%s)", msg.Subtype)
		}
		answer := bytes.TrimSpace(msg.Structured)
		if len(answer) == 0 || bytes.Equal(answer, []byte("null")) {
			answer = bytes.TrimSpace([]byte(msg.Result))
		}
		// Engine action results are JSON objects, not free-form completion text.
		var object map[string]json.RawMessage
		if err := json.Unmarshal(answer, &object); err != nil || object == nil {
			return observed, fmt.Errorf("WORKBUDDY_RESULT: result is not a JSON object")
		}
		observed.Answer = append(json.RawMessage(nil), answer...)
		complete = true
	}
	if err := scan.Err(); err != nil {
		return observed, fmt.Errorf("WORKBUDDY_STREAM: %w", err)
	}
	if !complete {
		return observed, fmt.Errorf("WORKBUDDY_RESULT: terminal result not observed; do not repeat the native call")
	}
	return observed, nil
}
