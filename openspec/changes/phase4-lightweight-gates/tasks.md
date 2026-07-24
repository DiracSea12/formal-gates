# Tasks

## Discovery and runner

- [x] Add `prompts/reviewer-base.md` and migrate the shared reviewer rules into
  it without anchoring to a particular requirement.
- [x] Move independent gate-specific prompts to `gates/*.md` and delete the
  old duplicated gate catalog and dispatch paths.
- [x] Implement the GateRunner contract: CLI discovery, validation, prompt
  assembly, result recording, and aggregation plus host-owned independent-agent
  dispatch. Do not add a provider adapter or direct-agent SDK to Go.
- [x] Add focused prompt-assembly and gate-discovery tests.
- [x] Add `prompts/` and `gates/` to package manifest, installer, package
  validation, installed-host tests, and maintained documentation.

## Workflow simplification

- [x] Replace heavy receipt/closure/context/Carry/gate-state paths with the
  one-file interruption state, simple result contracts, and one final summary.
- [x] Bind repair impact to native VCS snapshots and keep cumulative and
  repair-only comparisons distinct: Carry receives pre-repair-to-current,
  while every fresh or rerun gate reads base-to-current.
- [x] Keep QA design, QA execution, requirements clarification, lightweight
  start-readiness, Carry decision, and final aggregation as explicit actions.
- [x] Implement the single flow matrix, requirement-bound QA cases, current-
  snapshot QA execution, immediate per-gate `INHERIT`/`RERUN`, and atomic
  start/show/resume/abort/seal lifecycle.
- [x] Keep QA, readiness, and gate execution optional at seal; retain whichever
  statuses exist without requiring review completion or PASS.
- [x] Retain the existing cross-process state lock around full reload/mutate/
  replace transactions; do not persist `RUNNING` or add a scheduler.
- [x] Bind results to CLI-generated requirement and prompt/catalog revisions,
  invalidate requirement-dependent results on requirement change, and require
  a new run after an installed prompt/catalog change.
- [x] Replace closure-bound development handoff with state admission and the
  host-owned development-worker action; keep approved QA cases hidden from the
  worker.
- [x] Add one maintained VCS command reference for Git/SVN/P4 snapshot identity
  and native comparison; the host verifies current identity before review and
  before/after seal while the CLI only compares supplied identities.
- [x] Remove obsolete commands, compatibility readers, docs, fixtures, and
  tests in the same change; do not maintain two implementations.
- [x] Replace the old public workflow with one concise README/SKILL sequence
  covering install, start, resume, QA, review, repair, abort, and seal.

## Verification

- [x] Run formatting, unit, race, vet, package, supported-host, strict OpenSpec,
  and repository convergence checks.
- [x] Run an independent development-readiness review against the complete
  Phase 4 requirements and current diff before implementation.
