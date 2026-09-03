# Design

## Module layout

New package `internal/cost` with no dependency on `validate`; `validate`
imports `cost` (one direction, clean).

```
internal/cost/cost.go        types + recording operations
internal/cost/parse.go       transcript parsing entry (per-provider source)
internal/cost/parse_claude.go   Claude transcript adapter
internal/cost/parse_codex.go    Codex transcript adapter
internal/cost/cost_test.go   unit tests (math, adapters, tolerance)
```

`internal/lifecycle` gains transcript-path extraction per provider;
`internal/validate` wires the backfill at result recording.

The 3.5b extension keeps this direction. For legacy runs, lifecycle/validate
provide the dispatch or owner source boundary and write the existing
`RunState.Cost`; for engine runs, the phase-4 engine adapter provides the
source bridge and writes the same `cost` projection into the engine's
authoritative state. The bridge resolves a typed `(runID, actionID,
provider, correlation)` source reference to `READY`, `PENDING`, or
`UNAVAILABLE`; it owns lifecycle-sidecar lookup, not transcript parsing.
`internal/cost` parses and aggregates, while engine consumes only the
resulting projection/read-only usage signal. 3.5b does not assume that engine
calls legacy `validate`, and it does not create a second ledger. The parser
package does not import engine or validate.

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

Legacy runs use `RunState.Cost *cost.RunCost` with `json:"cost,omitempty"`;
existing state files without `cost` load fine. Phase 4 engine runs add the
same projection to the existing engine state/envelope and expose it through
the existing façade; this is one projection with two runtime owners during
the migration, not a second ledger.

```go
type RunCost struct {
    TotalInputTokens   int64                    `json:"totalInputTokens"`
    InputCacheHitTokens  int64                  `json:"inputCacheHitTokens"`
    InputCacheMissTokens int64                  `json:"inputCacheMissTokens"`
    OutputTokens       int64                    `json:"outputTokens"`
    Dispatches         map[string]DispatchCost  `json:"dispatches"`
    Owner              *OwnerCost               `json:"owner,omitempty"`
    // Phase 5 retained-master aggregation only; keyed by childRunID.
    ChildOwners        map[string]OwnerCost     `json:"childOwners,omitempty"`
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

type OwnerCost struct {
    InputCacheHitTokens  int64  `json:"inputCacheHitTokens"`
    InputCacheMissTokens int64  `json:"inputCacheMissTokens"`
    OutputTokens         int64  `json:"outputTokens"`
    TotalInputTokens     int64  `json:"totalInputTokens"`
    Source               string `json:"source"` // "transcript" | "unavailable"
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
or marked `source: "unavailable"`. The metering schema remains token-only; an
optional runtime cost guard belongs to the separate 3.5b checkpoint and does
not add a pricing field or a second ledger here.

For the owner entry, 3.5b records only the exact delta between a start
baseline and a Seal/Abort terminal snapshot of the same owner transcript. A
missing provider/identity, an unsupported format, a rewritten file, or an
overlapping run interval produces `source: "unavailable"`; the full transcript
is never treated as this run's usage. The owner entry is report-only in this
checkpoint: it is not read as an in-flight dispatch guard input.

## Backfill (legacy validate; engine adapter in phase 4)

For legacy result recording (`record-gate` / `record-action` / development
`snapshot --dispatch`), after lifecycle verification, read the dispatch's
stored transcript path; call `cost.ParseTranscript`; fill the dispatch entry
and run totals. Missing path, parse error, or Cursor provider → entry with
`source: "unavailable"`. Backfill happens once per dispatch (first
completion records it; repeats are no-ops). For engine runs, the phase-4
engine adapter supplies the equivalent source and performs this backfill in
the same write transaction as result acceptance; engine must not wait for a
later legacy `validate` pass. If lifecycle stop has not arrived, the bridge
returns `PENDING`; engine keeps the result at the receipt boundary and waits
for that lifecycle evidence before refill. If the source is definitively
missing or unsupported, it records `UNAVAILABLE` and the dispatch guard falls
back to its request/attempt limit.

Owner usage is captured at `workflow start` and finalized at Seal/Abort through
the same `Usage` parser and aggregate helper. Its baseline/provider/completed
marker live in the same run state (not a second ledger); the operation is
idempotent and does not change the dispatch backfill contract. `Owner` is
per-run: a child receipt carries that child's owner entry. In phase 5, a
retained master adds the child owner entry once under `ChildOwners[childRunID]`
when accepting that child receipt; repeated receipts with the same child ID and
digest are no-ops. The master `Owner` remains the master owner's own entry.

## Summary

Legacy `runSummary()` copies `state.Cost` into `RunSummary.Cost`. Phase 4
extends the existing engine `Run`/terminal summary to expose the same
projection; it does not introduce a parallel cost summary or ledger.

## Boundary

- Cost data does not rewrite a completed PASS/FAIL result. The separate 3.5b
  guard may consume reliable recorded usage before a future dispatch, but its
  engine state/source bridge and submit/refill ordering are a phase-4
  integration responsibility.
- No token estimation, guessed usage, provider pricing table, alerts, or gate
  front matter is added to this module. Owner terminal reporting does not
  become an in-flight hard stop.
- UNAVAILABLE is an explicit absence, never a fabricated number.
