package facade

import (
	"strings"
	"testing"
)

// These SDK-schema fixtures do not execute WorkBuddy or establish native support.
func TestWorkBuddyResultObservation(t *testing.T) {
	success := `{"type":"result","subtype":"success","session_id":"s1","uuid":"r1","is_error":false,"result":"{\"status\":\"PASS\"}"}`
	for _, tc := range []struct {
		name, stream, identity string
		wantError              bool
	}{
		{"fresh", success, "", false},
		{"resume", success, "s1", false},
		{"wrong resume", success, "s2", true},
		{"structured", `{"type":"result","subtype":"success","session_id":"s1","uuid":"r1","is_error":false,"structured_output":{"status":"PASS"},"result":"ignored"}`, "", false},
		{"interrupted", `{"type":"system","subtype":"init","session_id":"s1"}`, "", true},
		{"assistant only", `{"type":"assistant","session_id":"s1","message":{"content":"PASS"}}`, "", true},
		{"native error", strings.Replace(success, `"is_error":false`, `"is_error":true`, 1), "", true},
		{"missing error flag", strings.Replace(success, `"is_error":false,`, "", 1), "", true},
		{"budget stop", strings.Replace(success, `"subtype":"success"`, `"subtype":"error_max_budget_usd"`, 1), "", true},
		{"missing identity", strings.Replace(success, `"session_id":"s1",`, "", 1), "s1", true},
		{"plain text", strings.Replace(success, `{\"status\":\"PASS\"}`, `PASS`, 1), "", true},
		{"duplicate terminal", success + "\n" + success, "", true},
		{"mixed sessions", `{"type":"system","session_id":"s2"}` + "\n" + success, "", true},
		{"truncated tail", success + "\n{", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := observeWorkBuddyResult(strings.NewReader(tc.stream), tc.identity)
			if (err != nil) != tc.wantError {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if err == nil && (got.Identity != "s1" || string(got.Answer) != `{"status":"PASS"}`) {
				t.Fatalf("result=%+v", got)
			}
		})
	}
}
