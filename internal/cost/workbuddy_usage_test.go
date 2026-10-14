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

func TestWorkBuddyUsagePreservesPromptCacheMissTokens(t *testing.T) {
	native := `[{"id":"01a13c53-6bb4-75ef-ac97-6d75dcb8c12f","providerData":{"rawUsage":{"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"prompt_cache_hit_tokens":2944,"prompt_cache_miss_tokens":10603,"prompt_cache_write_tokens":0,"prompt_tokens":13547,"prompt_tokens_details":{"cached_tokens":2944}},"usage":{"inputTokens":13547,"inputTokensDetails":[{"cached_tokens":2944}],"outputTokens":114}},"session_id":"c2265922-d61d-4f28-bf43-0c773cbd8119"}]`
	got, err := parseTranscript("workbuddy", strings.NewReader(native))
	want := Usage{InputCacheMissTokens: 10603, InputCacheHitTokens: 2944, OutputTokens: 114}
	if err != nil || got != want || got.InputCacheMissTokens+got.InputCacheHitTokens != 13547 {
		t.Fatalf("usage=%+v err=%v want=%+v; prompt input includes cache hits and misses", got, err, want)
	}
}
