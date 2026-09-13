package host

import (
	"encoding/json"
	"fmt"
	"io"
)

var workbuddyDescriptor = &Descriptor{
	ID: WorkBuddy, InstallName: "workbuddy", ManifestName: "WorkBuddy", Aliases: []string{"workbuddy", "codebuddy", "cbc"}, Installable: true,
	Paths: PathLayout{GlobalBase: ".codebuddy/skills", GlobalManaged: ".codebuddy/CODEBUDDY.md", ProjectBase: ".codebuddy/skills", ProjectManaged: "CODEBUDDY.md"},
	// Native engine actions carry their tool and lifecycle boundary directly.
	// Installation supplies the native skill entry; it adds no global hook.
	CostProvider:    WorkBuddy,
	Configuration:   []ConfigField{StringConfig("workbuddyExecutable", "codebuddy"), StringConfig("workbuddyProductConfig", ""), StringConfig("workbuddyHome", "")},
	ReadUsage:       parseWorkBuddyTranscript,
	CheckIdentity:   workBuddyTranscriptIdentity,
	ObserveRecovery: workBuddyRecoveryOutcome,
}

// parseWorkBuddyTranscript consumes SDK AssistantMessage records. ResultMessage
// usage is a summary, and TaskUsage cannot supply the required token categories.
// Neither is added to assistant usage. Optional cache reads default to zero per
// the SDK Usage type; missing mandatory input/output counts remain unavailable.
func parseWorkBuddyTranscript(reader io.Reader) (Usage, error) {
	total := Usage{}
	seen := map[string]Usage{}
	session := ""
	var invalid error
	err := scanTranscriptLines(reader, func(line []byte) bool {
		var e struct {
			Type    string `json:"type"`
			UUID    string `json:"uuid"`
			Session string `json:"session_id"`
			Message *struct {
				Usage *struct {
					Input  *int64 `json:"input_tokens"`
					Output *int64 `json:"output_tokens"`
					Cache  *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" {
			return false
		}
		if e.UUID == "" || e.Session == "" || e.Message == nil || e.Message.Usage == nil {
			invalid = fmt.Errorf("workbuddy assistant identity or usage unavailable")
			return false
		}
		u := e.Message.Usage
		if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output < 0 || (u.Cache != nil && *u.Cache < 0) {
			invalid = fmt.Errorf("workbuddy assistant token categories unavailable")
			return false
		}
		if session != "" && session != e.Session {
			invalid = fmt.Errorf("workbuddy transcript contains multiple sessions")
			return false
		}
		session = e.Session
		v := Usage{InputCacheMissTokens: *u.Input, OutputTokens: *u.Output, InputCacheHitTokens: nonNegative(u.Cache)}
		if old, ok := seen[e.UUID]; ok {
			if old != v {
				invalid = fmt.Errorf("workbuddy duplicate message has conflicting usage")
			}
			return false
		}
		seen[e.UUID] = v
		total.InputCacheMissTokens += v.InputCacheMissTokens
		total.InputCacheHitTokens += v.InputCacheHitTokens
		total.OutputTokens += v.OutputTokens
		return true
	})
	if invalid != nil {
		return Usage{}, invalid
	}
	if err != nil {
		return Usage{}, err
	}
	return total, nil
}

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

func workBuddyRecoveryOutcome(r RecoveryObservation) string {
	if r.NativeMethod != "session/store" {
		return "UNKNOWN"
	}
	var response struct {
		SessionID string `json:"sessionId"`
		Present   bool   `json:"present"`
	}
	if json.Unmarshal(r.NativeResponse, &response) != nil || !response.Present || response.SessionID != r.Identity {
		return "UNKNOWN"
	}
	return "RESUMABLE"
}
