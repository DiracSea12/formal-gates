package facade

import (
	"io"
	"strings"
	"testing"
	"time"
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

func TestWorkBuddyAuthenticationFailureDoesNotWaitForTerminalResult(t *testing.T) {
	const init = `{"type":"system","subtype":"init","session_id":"s1"}`
	const rejection = `{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"Authentication required. Please use /login command to sign in to your account"}],"usage":{"input_tokens":0,"output_tokens":0}}}`
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	finished := make(chan error, 1)
	go func() {
		got, err := observeWorkBuddyResult(reader, "s1")
		if got.Identity != "s1" || len(got.Answer) != 0 {
			t.Errorf("authentication rejection changed identity or produced an answer: %+v", got)
		}
		finished <- err
	}()
	if _, err := io.WriteString(writer, init+"\n"+rejection+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "WORKBUDDY_AUTH:") {
			t.Fatalf("authentication rejection=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native authentication rejection waited for a result or EOF")
	}
	// Ordinary assistant text is still not terminal evidence, and discussing an
	// authentication error during real model work does not cancel the invocation.
	result := `{"type":"result","subtype":"success","session_id":"s1","uuid":"r1","is_error":false,"result":"{\"status\":\"PASS\"}"}`
	for _, text := range []string{strings.Replace(rejection, `"input_tokens":0`, `"input_tokens":1`, 1), strings.Replace(rejection, "Authentication required.", "Observed: Authentication required.", 1)} {
		if _, err := observeWorkBuddyResult(strings.NewReader(init+"\n"+text+"\n"+result), "s1"); err != nil {
			t.Fatal("ordinary assistant output was treated as a native authentication failure", err)
		}
	}
}

func TestWorkBuddyNativeAuthenticationFailureIsNotAnIndefiniteWait(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"native"}
{"type":"assistant","session_id":"native","message":{"content":[{"type":"text","text":"Authentication required. Please use /login command to sign in to your account"}]}}
`
	result, err := observeWorkBuddyResult(strings.NewReader(stream), "native")
	if err == nil || !strings.Contains(err.Error(), "WORKBUDDY_AUTH") || result.Identity != "native" || result.Answer != nil {
		t.Fatalf("authentication treated as a verdict or missing terminal wait: %+v %v", result, err)
	}
}
