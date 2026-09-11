package protocol

import "encoding/json"

func dshTranscriptIdentity(path, identity string) error {
	return checkStreamTranscriptIdentity(path, identity, func(raw json.RawMessage) string {
		var row struct {
			Session string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return ""
		}
		return row.Session
	})
}
