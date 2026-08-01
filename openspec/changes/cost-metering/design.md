# Design

## Module layout

New package `internal/cost` with no dependency on `validate`; `validate`
imports `cost` (one direction, clean).

```
internal/cost/cost.go        types + recording operations
internal/cost/parse.go       transcript parsing entry (per-provider dispatch)
internal/cost/parse_claude.go   Claude transcript adapter
internal/cost/parse_codex.go    Codex transcript adapter
internal/cost/cost_test.go   unit tests (math, adapters, tolerance)
```

`internal/lifecycle` gains transcript-path extraction per provider;
`internal/validate` wires the backfill at result recording.

## Transcript capture (lifecycle)

SubagentStop hook payloads of both Claude and Codex carry
`agent_transcript_path` (string, may be null). The Claude/Codex provider
adapters extract it at capture and store it with the dispatch binding
(`lifecycle` directory, same store as bindings/events). The existing
identity/correlation matching already ties the stop event to exactly one
dispatch, so the path lands on the right dispatch without new correlation
machinery. Cursor's stop payload also carries a transcript path but its
transcripts contain no usage data, so Cursor dispatches stay UNAVAILABLE.

## Parsing (internal/cost)

One entry: `ParseTranscript(provider, path) (Usage, error)`; a provider
adapter is selected by provider name. Both adapters are defensive: unknown
line shapes and missing fields are skipped, never fatal; a structural failure
(no parseable usage at all) returns an error so the dispatch is marked
UNAVAILABLE rather than fabricated.

Claude adapter: read `<session>/subagents/agent-<id>.jsonl`; for each line
with `type: "assistant"`, sum `message.usage` fields
(input_tokens, output_tokens, cache_read_input_tokens,
cache_creation_input_tokens); dedupe by line `uuid` (transcript entries can
be duplicated across rewrites; per research, parallel subagents are separate
files with disjoint uuid spaces).

Codex adapter: read the rollout JSONL; parse `event_msg` records with
`payload.type == "token_count"`, prefer `payload.info.last_token_usage`
(incremental), fall back to the delta of `total_token_usage`; fields
input_tokens / cached_input_tokens / cache_write_input_tokens /
output_tokens / reasoning_output_tokens; dedupe by
(timestamp, model, token tuple). Codex writes no cost field, so cost stays
token-only for both hosts.

The transcript format is not a stable contract (official docs warn scripts
may break on any release); adapters must tolerate missing optional fields and
the parse test set covers the current real formats.

## Ledger

`RunState.Cost *cost.RunCost` with `json:"cost,omitempty"`; existing state
files without `cost` load fine.

```go
type RunCost struct {
    TotalInputTokens   int64                    `json:"totalInputTokens"`
    InputCacheHitTokens  int64                  `json:"inputCacheHitTokens"`
    InputCacheMissTokens int64                  `json:"inputCacheMissTokens"`
    OutputTokens       int64                    `json:"outputTokens"`
    Dispatches         map[string]DispatchCost  `json:"dispatches"`
}

type DispatchCost struct {
    Target              string `json:"target"`
    Kind                string `json:"kind"`          // "gate" | "action"
    InputCacheHitTokens   int64 `json:"inputCacheHitTokens"`
    InputCacheMissTokens  int64 `json:"inputCacheMissTokens"`
    OutputTokens        int64 `json:"outputTokens"`
    TotalInputTokens    int64 `json:"totalInputTokens"`
    Source              string `json:"source"`        // "transcript" | "unavailable"
}
```

Categories follow the host pricing rules: input with cache hit, input without
cache hit, and output. `TotalInputTokens = InputCacheHitTokens +
InputCacheMissTokens`; output is never added into the total. Cache write
tokens are not a priced category and are not recorded. The schema is uniform
across hosts: the Claude adapter maps `cache_read_input_tokens` to cache hit
and `input_tokens` to cache miss; the Codex adapter splits `cached_input_tokens`
out of `input_tokens` (hit = cached, miss = input − cached). No estimation, no
guessed output defaults: a dispatch is either backfilled from its transcript
or marked `source: "unavailable"`. Tokens are the only unit (no USD).

## Backfill (validate)

At result recording (`record-gate` / `record-action` / development
`snapshot --dispatch`), after lifecycle verification, read the dispatch's
stored transcript path; call `cost.ParseTranscript`; fill the dispatch entry
and run totals. Missing path, parse error, or Cursor provider → entry with
`source: "unavailable"`. Backfill happens once per dispatch (first
completion records it; repeats are no-ops).

## Summary

`runSummary()` copies `state.Cost` into `RunSummary.Cost`. No other summary
change.

## Boundary

- Cost data is display-only: no transition, gate, or decision reads it.
- No token estimation, no USD conversion, no budgets, no alerts, no gate
  front matter.
- UNAVAILABLE is an explicit absence, never a fabricated number.
