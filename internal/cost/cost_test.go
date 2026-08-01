package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// claudeFixture mirrors the current real Claude Code subagent transcript
// shape: per-line events with type, uuid, and message.usage; assistant
// entries can be duplicated across rewrites and optional fields may be
// missing.
const claudeFixture = `{"parentUuid":null,"isSidechain":true,"agentId":"agent-1","type":"user","message":{"role":"user","content":"hello"}}
{"uuid":"u-0001","isSidechain":true,"agentId":"agent-1","type":"assistant","message":{"type":"message","role":"assistant","usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":40,"service_tier":"standard"}}}
{"uuid":"u-0002","type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5}}}
{"uuid":"u-0001","type":"assistant","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":40}}}
{"type":"assistant","message":{"usage":{"input_tokens":1,"output_tokens":2}}}
{"type":"assistant","message":{"content":[{"type":"text","text":"no usage here"}]}}
{"type":"tool_result","message":{"usage":{"input_tokens":999,"output_tokens":999}}}
{"type":"assistant","message":{"usage":{"input_tokens":5,"output_tokens":-3}}}
{"broken json line
`

// codexFixture mirrors the current real Codex rollout shape: event_msg
// records with payload.type == "token_count", incremental last_token_usage,
// and cumulative total_token_usage; the last event carries only the
// cumulative counter to exercise the delta fallback.
const codexFixture = `{"timestamp":"2026-07-19T23:17:09.078Z","type":"session_meta","payload":{"session_id":"s-1"}}
{"timestamp":"2026-07-19T23:17:10.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"last_token_usage":{"input_tokens":100,"cached_input_tokens":10,"cache_write_input_tokens":0,"output_tokens":30,"reasoning_output_tokens":5,"total_tokens":130},"model_context_window":353400},"rate_limits":{"limit_id":"codex"}}}
{"timestamp":"2026-07-19T23:17:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":10,"cache_write_input_tokens":2,"output_tokens":40,"reasoning_output_tokens":8,"total_tokens":190},"last_token_usage":{"input_tokens":50,"cached_input_tokens":0,"cache_write_input_tokens":2,"output_tokens":10,"reasoning_output_tokens":3,"total_tokens":60},"model_context_window":353400}}}
{"timestamp":"2026-07-19T23:17:11.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":50,"cached_input_tokens":0,"cache_write_input_tokens":2,"output_tokens":10,"reasoning_output_tokens":3,"total_tokens":60}}}}
{"timestamp":"2026-07-19T23:17:12.000Z","type":"response_item","payload":{"type":"message"}}
{"timestamp":"2026-07-19T23:17:13.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":40,"cache_write_input_tokens":2,"output_tokens":80,"reasoning_output_tokens":12,"total_tokens":380}}}}
{"timestamp":"2026-07-19T23:17:13.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":300,"cached_input_tokens":40,"cache_write_input_tokens":2,"output_tokens":80,"reasoning_output_tokens":12,"total_tokens":380}}}}
{"not valid json
`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseClaudeTranscriptSumsDedupedAssistantUsage(t *testing.T) {
	usage, err := ParseTranscript("claude-code", writeFixture(t, claudeFixture))
	if err != nil {
		t.Fatal(err)
	}
	// Priced categories only: cache hit maps to cache_read_input_tokens
	// (30), cache miss to input_tokens (100+10+1+5=116, the negative 3
	// counts as absent), output to output_tokens (40+5+2=47). Cache
	// creation tokens (20) are not a priced category and are not recorded.
	want := Usage{InputCacheHitTokens: 30, InputCacheMissTokens: 116, OutputTokens: 47}
	if usage != want {
		t.Fatalf("usage=%+v want %+v", usage, want)
	}
	// The unified total is the input side only: cache hit plus cache miss.
	if got := usage.InputCacheHitTokens + usage.InputCacheMissTokens; got != 146 {
		t.Fatalf("total input=%d want 146", got)
	}
}

func TestParseCodexTranscriptPrefersLastUsageAndDeltasCumulative(t *testing.T) {
	usage, err := ParseTranscript("codex", writeFixture(t, codexFixture))
	if err != nil {
		t.Fatal(err)
	}
	// Reasoning output is a breakdown of output_tokens, never added on top.
	// Priced categories only: hit = cached_input_tokens, miss = input minus
	// cached, output = output_tokens. Cache write tokens (2) are not a
	// priced category and are not recorded.
	// Event 1: last usage input 100 / cached 10 / output 30 -> hit 10, miss 90.
	// Event 2: last usage input 50 / cached 0 / output 10 -> hit 0, miss 50.
	// Event 3: duplicate of event 2 -> deduped.
	// Event 5: cumulative only; delta from event 2's cumulative
	//          150/10/40 is 150/30/40 -> hit 30, miss 120.
	// Event 6: duplicate of event 5 -> deduped.
	want := Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 260, OutputTokens: 80}
	if usage != want {
		t.Fatalf("usage=%+v want %+v", usage, want)
	}
	// The unified total is the input side only: cache hit plus cache miss,
	// never output.
	if got := usage.InputCacheHitTokens + usage.InputCacheMissTokens; got != 300 {
		t.Fatalf("total input=%d want 300", got)
	}
}

func TestParseCodexClampsNegativeMissAndIgnoresCacheWrite(t *testing.T) {
	// A corrupted usage where cached exceeds input must yield a zero miss,
	// never a negative number; cache write tokens must not be recorded at
	// all.
	fixture := `{"timestamp":"t1","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":5,"cached_input_tokens":9,"cache_write_input_tokens":50,"output_tokens":2,"reasoning_output_tokens":1}}}}
`
	usage, err := ParseTranscript("codex", writeFixture(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{InputCacheHitTokens: 9, InputCacheMissTokens: 0, OutputTokens: 2}
	if usage != want {
		t.Fatalf("usage=%+v want %+v", usage, want)
	}
}

func TestParseTranscriptStructuralFailuresReturnErrors(t *testing.T) {
	for name, path := range map[string]string{
		"missing file": filepath.Join(t.TempDir(), "absent.jsonl"),
		"empty file":   writeFixture(t, ""),
		"no usage":     writeFixture(t, "{\"timestamp\":\"t\",\"type\":\"session_meta\"}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTranscript("codex", path); err == nil {
				t.Fatal("expected structural failure to return an error")
			}
		})
	}
	if _, err := ParseTranscript("cursor", writeFixture(t, codexFixture)); err == nil {
		t.Fatal("unsupported provider must return an error")
	}
}

func TestParseTranscriptAcceptsProviderAliases(t *testing.T) {
	for _, provider := range []string{"claude-code", "claude", "claude code"} {
		if _, err := ParseTranscript(provider, writeFixture(t, claudeFixture)); err != nil {
			t.Fatalf("provider %q failed: %v", provider, err)
		}
	}
}

func TestRecordIsIdempotentPerDispatch(t *testing.T) {
	run := &RunCost{}
	entry := DispatchCost{Target: "quality", Kind: "gate", InputCacheHitTokens: 2, InputCacheMissTokens: 8, OutputTokens: 5, TotalInputTokens: 10, Source: SourceTranscript}
	Record(run, "dispatch-1", entry)
	Record(run, "dispatch-1", entry) // repeat must be a no-op
	Record(run, "dispatch-2", DispatchCost{Target: "qa-design", Kind: "action", InputCacheHitTokens: 1, InputCacheMissTokens: 2, OutputTokens: 4, TotalInputTokens: 3, Source: SourceUnavailable})
	want := RunCost{TotalInputTokens: 13, InputCacheHitTokens: 3, InputCacheMissTokens: 10, OutputTokens: 9, Dispatches: map[string]DispatchCost{"dispatch-1": entry, "dispatch-2": {Target: "qa-design", Kind: "action", InputCacheHitTokens: 1, InputCacheMissTokens: 2, OutputTokens: 4, TotalInputTokens: 3, Source: SourceUnavailable}}}
	if !reflect.DeepEqual(*run, want) {
		t.Fatalf("run=%+v want %+v", *run, want)
	}
}

func TestRunCostJSONRoundTripAndOmission(t *testing.T) {
	run := &RunCost{TotalInputTokens: 13, InputCacheHitTokens: 3, InputCacheMissTokens: 10, OutputTokens: 9, Dispatches: map[string]DispatchCost{"d": {Target: "quality", Kind: "gate", TotalInputTokens: 13, Source: SourceTranscript}}}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RunCost
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, *run) {
		t.Fatalf("round trip=%+v want %+v", decoded, *run)
	}
	// A nil projection is omitted rather than emitted as a null object.
	wrapped := struct {
		Cost *RunCost `json:"cost,omitempty"`
	}{Cost: nil}
	data, err = json.Marshal(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cost") {
		t.Fatalf("nil cost projection was not omitted: %s", data)
	}
}
