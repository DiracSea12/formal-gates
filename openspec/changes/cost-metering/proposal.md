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
Hosts without a parseable transcript (Cursor) are marked UNAVAILABLE. Cost
data never changes a PASS/FAIL decision and never blocks anything.

Out of scope: token estimation or guessing of any kind, USD conversion,
budgets or alerts, and gate-file front matter.
