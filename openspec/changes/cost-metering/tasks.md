# Tasks

- [ ] Add `internal/cost` package: `RunCost`/`DispatchCost` types, transcript
  parsing entry with provider adapters, backfill math, no estimation.
- [ ] Claude transcript adapter: parse `message.usage` per assistant line,
  dedupe by line uuid, tolerate missing fields.
- [ ] Codex transcript adapter: parse `token_count` events,
  `last_token_usage` incremental with `total_token_usage` delta fallback,
  event dedupe.
- [ ] Extract and store `agent_transcript_path` in Claude and Codex lifecycle
  capture; leave Cursor UNAVAILABLE.
- [ ] Backfill dispatch cost at result recording (record-gate /
  record-action / development snapshot), idempotent per dispatch; parse
  failure or missing path marks the dispatch UNAVAILABLE.
- [ ] Add `RunState.Cost` and `RunSummary.Cost` projection with
  backward-compatible nil handling for existing state files.
- [ ] Unit tests: both adapters on real-format fixtures, backfill
  idempotence, JSON round-trip with and without `cost`, summary projection.
