package host

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ScalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64, bool:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
	return ""
}

// ConversationSession reads the current host session from tools/call metadata.
func ConversationSession(metadata json.RawMessage) (string, error) {
	var context struct {
		ThreadID string          `json:"thread_id"`
		Thread   string          `json:"threadId"`
		Codex    json.RawMessage `json:"x-codex-turn-metadata"`
		ZCode    *struct {
			Session string `json:"session_id"`
		} `json:"com.zcode/request-context"`
	}
	if len(metadata) != 0 {
		if err := json.Unmarshal(metadata, &context); err != nil {
			return "", err
		}
	}
	if context.ZCode != nil && context.ZCode.Session != "" {
		return context.ZCode.Session, nil
	}
	session := context.ThreadID
	if session == "" {
		session = context.Thread
	}
	if session == "" && len(context.Codex) != 0 {
		var encoded string
		if json.Unmarshal(context.Codex, &encoded) == nil {
			context.Codex = json.RawMessage(encoded)
		}
		var turn struct {
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(context.Codex, &turn); err != nil {
			return "", err
		}
		session = turn.ThreadID
	}
	return session, nil
}
