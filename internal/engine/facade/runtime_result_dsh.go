package facade

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DSH maps aborted, blocked and errored turns to ACP end_turn as well. Inspect
// the host's committed turn/end event, rather than promoting that stop reason.
func observeDSHResult(reader io.Reader, identity string, turn int) (json.RawMessage, error) {
	if identity == "" || turn < 0 {
		return nil, fmt.Errorf("DSH_BINDING: missing session or turn")
	}
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 65536), 32*1024*1024)
	var answer string
	ended := false
	for scan.Scan() {
		if len(bytes.TrimSpace(scan.Bytes())) == 0 {
			continue
		}
		var record struct {
			SessionID string `json:"sessionId"`
			Event     struct {
				Type string `json:"type"`
				Data struct {
					Turn   *int `json:"turn"`
					Reason struct {
						Kind string `json:"kind"`
					} `json:"reason"`
					Message struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
				} `json:"data"`
			} `json:"event"`
		}
		if err := json.Unmarshal(scan.Bytes(), &record); err != nil {
			return nil, err
		}
		if record.Event.Type == "compaction/summary" {
			if record.SessionID != identity {
				return nil, fmt.Errorf("DSH_BINDING: compaction belongs to another session")
			}
			continue
		}
		if record.SessionID != identity || record.Event.Data.Turn == nil {
			return nil, fmt.Errorf("DSH_BINDING: native event belongs to another session or turn")
		}
		if *record.Event.Data.Turn != turn {
			continue
		}
		switch record.Event.Type {
		case "assistant/message":
			if ended {
				return nil, fmt.Errorf("DSH_RESULT: message after turn ended")
			}
			answer = ""
			for _, part := range record.Event.Data.Message.Content {
				if part.Type == "text" {
					answer += part.Text
				}
			}
		case "turn/end":
			if ended || record.Event.Data.Reason.Kind != "completed" {
				return nil, fmt.Errorf("DSH_RESULT: native turn did not complete (%s)", record.Event.Data.Reason.Kind)
			}
			ended = true
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	if !ended {
		return nil, fmt.Errorf("UNKNOWN: DSH committed turn/end missing")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(answer), &object) != nil || object == nil {
		return nil, fmt.Errorf("DSH_RESULT: final native message is not a result object")
	}
	return json.RawMessage(answer), nil
}
