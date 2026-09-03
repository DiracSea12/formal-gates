# Requirements Alignment

Date: 2026-08-01
Status: confirmed

## RQ-001 - Run ledger carries exact per-dispatch token cost

Each run SHALL record a `cost` projection in its single state ledger: run
totals and a per-dispatch entry with target, kind, token counts, and a source
marker. Token categories SHALL follow the host pricing rules: input with
cache hit, input without cache hit, and output, each recorded separately.
`totalInputTokens` SHALL equal cache hit + cache miss (the input-side total);
output SHALL be recorded separately and SHALL NOT be added into the total.
Cache write tokens SHALL NOT be recorded as a priced category. Existing state
files without `cost` SHALL load without error.

## RQ-002 - Numbers come from transcripts, never from guessing

A dispatch SHALL be backfilled with real usage parsed from its subagent
transcript when the transcript is present and parseable. When it is not
(missing path, parse failure, unsupported host), the dispatch SHALL be marked
`source: "unavailable"` and SHALL NOT carry fabricated numbers. No token
estimation of any kind SHALL be introduced.

## RQ-003 - Claude and Codex transcripts are parsed

Claude Code SHALL be parsed from per-message `message.usage` in the subagent
transcript (deduped by line uuid). Codex SHALL be parsed from `token_count`
events (`last_token_usage` incremental, `total_token_usage` delta fallback).
Both parsers SHALL tolerate unknown line shapes and missing optional fields.

## RQ-004 - Transcript path captured via lifecycle

The Claude and Codex lifecycle capture SHALL extract
`agent_transcript_path` from the SubagentStop payload and store it on the
matched dispatch. The existing lifecycle correlation SHALL be reused; no new
correlation machinery. Cursor dispatches SHALL remain UNAVAILABLE.

## RQ-005 - Backfill at result recording, once per dispatch

Result recording SHALL backfill the dispatch cost from the stored transcript
path. Backfill SHALL happen once per dispatch; repeated recording of the same
dispatch SHALL NOT re-add numbers.

## RQ-006 - Seal summary shows cost

The seal summary SHALL include the same cost projection so a sealed run shows
total tokens and per-gate tokens.

## RQ-007 - Decoupled, pluggable cost module

Cost metering SHALL live in a standalone package with per-provider parser
adapters selected by host name. Cost data SHALL NOT rewrite an already
recorded PASS/FAIL decision. The separate 3.5b runtime guard MAY consume
reliable recorded usage to block a future dispatch; it SHALL NOT introduce a
second accounting ledger.

The 3.5b owner extension SHALL use the same package and projection: it may add
one optional owner entry only when a start-to-terminal transcript delta is
exactly identifiable. It SHALL mark missing identity, unsupported format,
rewritten/overlapping intervals as unavailable rather than counting the full
conversation or guessing. The owner entry is local to the run that owns the
transcript and is report-only for live dispatch guarding. In phase 5, a
retained master SHALL aggregate each child owner once by childRunID while
retaining the master's own owner entry separately; repeated child receipts
SHALL be idempotent. Engine runs receive this projection through the phase-4
engine source bridge; the bridge and its submit/backfill/refill order are not
implemented by a second accounting path.

## RQ-008 - Boundary

This metering change SHALL NOT add token estimation, guessed usage, provider
pricing tables, budget policy, alerts, or gate-file front matter. Runtime stop
policy is a separate 3.5b checkpoint and is not a second cost ledger.
