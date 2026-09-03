# Proposal

Add exact token metering to the formal workflow. Each run's single state
ledger records the real token usage of every dispatched review and action,
read from the host's own session transcripts after the subagent stops, so a
sealed run summary answers "how many tokens did this round cost and which gate
was the most expensive" with real numbers.

Metering is exact or explicitly unavailable, never guessed. The CLI records
real usage when the transcript is present and parseable; when it is not (no
transcript, unsupported host, parse failure), the dispatch is marked
UNAVAILABLE and no number is fabricated.

Token categories follow the host's pricing rules. Three priced categories are
recorded separately: input with cache hit, input without cache hit, and
output. `totalInputTokens` is the input-side total (cache hit + cache miss);
output is recorded separately and never added into the total. Cache write
tokens are not a priced category and are not recorded as one. The recorded
schema is identical for both hosts; adapters normalize host-specific fields
(Claude reports cache read/cache creation as separate usage fields; Codex
reports cached input as a subset of input, which the adapter splits out).

The ledger stays in the existing `state.json` (`cost` field) and the seal
summary carries the same projection. No second accounting store is introduced.

Data source: the SubagentStop hook payload carries the subagent transcript
path; the lifecycle journal already correlates each stop event to its
dispatch. Transcript lines carry real per-message usage (input, output, cache
read, cache creation). The flow parses the transcript and backfills the
dispatch cost at result recording.

Scope: Claude Code and Codex hosts. Both expose `agent_transcript_path` on
SubagentStop; their transcript formats differ (Claude: per-message
`message.usage`; Codex: `token_count` events with incremental
`last_token_usage`). Each host gets its own transcript parser adapter.
Hosts without a parseable transcript (Cursor) are marked UNAVAILABLE. The
projection does not rewrite an already recorded PASS/FAIL decision. The
separate 3.5b runtime guard may read reliable usage to stop a future
dispatch; it does not create a second ledger.

The original metering implementation records dispatched gates/actions. The
3.5b follow-up extends the same projection with one optional owner entry for
each run when that run's already-captured owner transcript can be measured as
an exact start-to-terminal delta; it never counts the whole conversation as
run cost. The owner entry is report-only for the live dispatch guard. A child
keeps its owner entry in its own receipt/sidecar; phase 5's retained master
adds it once under a childRunID-keyed aggregation, while the master's own
owner entry remains separate. This is not a second ledger or a generic billing
namespace. Missing identity, unsupported format, overlapping run intervals,
or an unreliable delta remains UNAVAILABLE.

Out of scope for this metering module: token estimation or guessing of any
kind, provider pricing tables, budget policy, alerts, and gate-file front
matter. Runtime stop policy belongs to the 3.5b checkpoint.
