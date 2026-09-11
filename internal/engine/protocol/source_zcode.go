package protocol

import (
	"encoding/json"
	"strings"
)

func zcodeTranscriptIdentity(path, identity string) error {
	return checkStreamTranscriptIdentity(path, identity, func(raw json.RawMessage) string {
		var row struct {
			ID     string `json:"id"`
			Result struct {
				Session string `json:"sessionId"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &row) != nil || !strings.HasSuffix(row.ID, ":usage") {
			return ""
		}
		return row.Result.Session
	})
}
