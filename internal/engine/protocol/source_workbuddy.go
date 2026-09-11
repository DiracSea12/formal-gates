package protocol

import "encoding/json"

func workBuddyTranscriptIdentity(path, identity string) error {
	return checkStreamTranscriptIdentity(path, identity, func(raw json.RawMessage) string {
		var row struct {
			Session string `json:"session_id"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return ""
		}
		return row.Session
	})
}
