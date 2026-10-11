package cost

import (
	"strings"
	"testing"
)

func TestWorkBuddyUsageUsesAssistantMessagesAndDeduplicatesResponses(t *testing.T) {
	first := `{"type":"assistant","uuid":"m1","session_id":"s1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":2}}}`
	next := `{"type":"assistant","uuid":"m2","session_id":"s1","message":{"usage":{"input_tokens":3,"output_tokens":5}}}`
	result := `{"type":"result","session_id":"s1","usage":{"input_tokens":99,"output_tokens":99}}`
	got, err := parseWorkBuddyTranscript(strings.NewReader(strings.Join([]string{first, first, next, result}, "\n")))
	if err != nil || got.InputCacheMissTokens != 14 || got.OutputTokens != 12 || got.InputCacheHitTokens != 2 {
		t.Fatalf("usage=%+v err=%v", got, err)
	}
	for _, line := range []string{`{"type":"assistant","uuid":"m1","session_id":"s2","message":{"usage":{"input_tokens":11,"output_tokens":7}}}`, `{"type":"assistant","uuid":"m1","session_id":"s1","message":{"usage":{"input_tokens":12,"output_tokens":7}}}`, `{"type":"assistant","uuid":"m3","session_id":"s1","message":{"usage":{"input_tokens":4}}}`} {
		if _, err := parseWorkBuddyTranscript(strings.NewReader(first + "\n" + line)); err == nil {
			t.Fatalf("accepted conflicting/unavailable usage: %s", line)
		}
	}
}
