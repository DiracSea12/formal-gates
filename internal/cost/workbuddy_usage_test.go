package cost

import (
	"strings"
	"testing"
)

func TestWorkBuddyUsagePreservesNativeInputCategoriesAndDeduplicatesItems(t *testing.T) {
	normalized := `[{"id":"m1","session_id":"s1","providerData":{"usage":{"inputTokens":11,"outputTokens":7,"inputTokensDetails":[{"cached_tokens":2}]}}}]`
	exclusive := `[{"id":"m2","session_id":"s1","providerData":{"usage":{"inputTokens":3,"outputTokens":5,"inputTokensDetails":[]},"rawUsage":{"input_tokens":3,"cache_read_input_tokens":1,"cache_creation_input_tokens":4}}}]`
	inclusive := `[{"id":"m3","session_id":"s1","providerData":{"usage":{"inputTokens":8,"outputTokens":4,"inputTokensDetails":[]},"rawUsage":{"prompt_tokens":8,"prompt_tokens_details":{"cached_tokens":3},"cache_creation_input_tokens":2}}}]`
	got, err := parseTranscript("workbuddy", strings.NewReader(strings.Join([]string{normalized, normalized, exclusive, inclusive}, "\n")))
	want := Usage{InputCacheMissTokens: 15, InputCacheHitTokens: 6, OutputTokens: 16}
	if err != nil || got != want || got.InputCacheMissTokens+got.InputCacheHitTokens != 21 {
		t.Fatalf("usage=%+v err=%v want=%+v; priced input=11+(3+1)+(8-2); cache creation is excluded", got, err, want)
	}
	for _, line := range []string{
		strings.ReplaceAll(normalized, `"s1"`, `"s2"`),
		strings.ReplaceAll(normalized, `"inputTokens":11`, `"inputTokens":12`),
		`[{"id":"m4","session_id":"s1","providerData":{"usage":{"inputTokens":4}}}]`,
	} {
		if _, err := parseTranscript("workbuddy", strings.NewReader(normalized+"\n"+line)); err == nil {
			t.Fatalf("accepted conflicting/unavailable native usage: %s", line)
		}
	}
	// The print message does not distinguish inclusive and exclusive native input.
	if _, err := parseTranscript("workbuddy", strings.NewReader(`{"type":"assistant","uuid":"m1","session_id":"s1","message":{"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":2}}}`)); err == nil {
		t.Fatal("ambiguous print counters were treated as classified native usage")
	}
}
