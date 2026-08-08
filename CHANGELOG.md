# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Incorporate gate P2 review findings: the RQ-014 parallel reminder shares one
  emission point (`emitParallelReminder`) across the workflow commands and the
  lifecycle hook; `RecordQAReview` reads the same case view the review prompt
  uses (per-mode key with merged-`""` fallback) and writes the decided status back
  to the same storage key; the inline authorize-repair `--qa-scope`/`--qa-cases`/
  `--qa-reason` flags accept an empty `<mode>` (`=FULL`) for the merged QA set,
  and the auto-carried AFFECTED decision at the review limit can be overridden by
  an explicit host decision (e.g. upgrade to FULL).
- Fully decouple blackbox and whitebox QA (RQ-012): QA execution results and
  prior authoritative results are stored per mode (`qaExecutionByMode` /
  `priorQAExecutionByMode`, empty mode = merged/single-dispatch), so one mode's
  design/execution/record never clears or affects the other mode's execution
  result or rerun base; the qa-design review lock is per mode (a blackbox review
  in flight does not lock the whitebox design and vice versa); and every read
  (required case set, AFFECTED inheritance, rerun scope enforcement,
  selectedQAModesRecorded, repair blockers, Seal resolution) is per mode while
  keeping the merged empty-mode semantics. Legacy single `qaExecution` /
  `priorQAExecution` state files migrate into the merged key on load.
- Require an explicit QA execution rerun scope decision: a mode whose QA
  execution reruns under a new snapshot (a prior authoritative result exists at
  an earlier snapshot) must record a `workflow qa-execution-scope --mode
  <blackbox|whitebox|""> --decision FULL|AFFECTED` decision before
  `prepare-action qa-execution` (CLI-enforced; first execution needs none and
  defaults to the full set). AFFECTED reruns execute only the host-judged subset
  (must be a non-empty approved subset including every prior FAIL case), inherit
  the untouched approved cases as PASS from the base snapshot (marked `inherited`,
  excluded from FAIL aggregation), and lock the subset before dispatch. The
  authoritative prior result (with its FAIL case set) survives repair snapshot
  advances in `PriorQAExecution` until replaced; a RUNTIME_ERROR is never
  preserved and never evicts it. At the exhausted review-wave limit,
  `authorize-repair` bundles the rerun scope decision in the same interaction
  (per selected mode; `--qa-scope/--qa-cases/--qa-reason`, Source
  AUTHORIZE_REPAIR), auto-carries a prior user-chosen AFFECTED decision as
  CARRY_FORWARD without re-asking, and still grants exactly one extra wave.
- Allow `qa-design` re-records to add/update the case set until that mode's
  `qa-review` dispatch is prepared (design locked once a review dispatch is
  OPEN/CLAIMED), preserving existing approved cases incrementally.
- Store QA cases per dispatch mode (`qaCasesByMode`, blackbox/whitebox separate,
  empty mode for the merged/单派发 flow): a `qa-design` round only replaces its
  own mode's case list, never touching another mode's existing cases (their
  review PASS status and recorded execution results are preserved), fixing the
  data loss where designing whitebox replaced the whole table and cleared
  blackbox cases. Legacy single-list state files migrate into the merged key on
  load; case IDs stay unique across modes.
- Detect parallelism and remind the main agent to fill parallel dispatches
  (RQ-014): a hard-coded, decoupled "stage → should-parallel task" data table
  drives the check (post-development = blackbox QA execution + whitebox QA
  execution + each selected gate; repair rounds re-review gates and QA in
  parallel; blackbox QA design/review runs in parallel with development; the
  rules do not depend on whether tasks are prepared). Every state-changing
  workflow command (prepare-action / prepare-gate / claim-dispatch / record-* /
  qa-* / snapshot / seal) and the lifecycle hook (subagent start/stop) triggers a
  cheap, read-only check; when the in-flight claimed dispatch count is below the
  should-parallel set size the CLI prints a stderr reminder ("可并行 X 项…当前并行
  Y 项，建议补足") with cooldown/dedup, never polluting stdout machine JSON, and
  never touching dispatches, lifecycle events, or review results.
- Run blackbox QA design/review/repair in a dedicated QA isolation worktree in
  parallel with development: development start no longer waits for the blackbox
  QA review, blackbox qa-design/qa-review resolve native identity against the
  isolation worktree (always the base snapshot, never the development code), the
  snapshot gate requires development complete 且 blackbox qa-review PASS, the
  snapshot may be manually released only through an explicit user authorization
  (recorded with its source; unapproved blackbox cases then count as PASS and
  qa-execution covers only approved cases), blackbox review PASS clears the
  worktree, and a 3-consecutive-FAIL recovery path surfaces the blocker for the
  user's decision. `workflow qa-worktree` registers the worktree (native identity
  == base, injected requirement revision verified); resume re-verifies it.
- Squash the git base→current commit range into a single commit at seal when it
  holds more than one commit (`--squash-message`, preserving the final tree, as
  the last VCS operation, requiring a clean working tree; single-commit or empty
  ranges are untouched, SVN/P4 are never squashed).
- Replace the QA case `kind`(STATIC/LIVE) with `mode`(blackbox/whitebox), route
  qa-design/qa-review dispatches by mode, drop the mechanical per-mode quality
  floor (case-set sufficiency is a qa-review set-level coverage judgment; a
  selected mode with zero cases flows to that review as a P1 coverage omission),
  and write the severity classification (P2 = suggestion only; coverage omission
  = P1, blocking) into the QA design/review/execution prompts.
- Enforce the review rules for product-review and start-readiness in the CLI
  (only the user can break them): a P2-only round records PASS with the P2
  suggestions visible; confirmed P0/P1 findings set a needs-re-review marker and
  record-action PASS is rejected until a re-review round returns PASS, while
  dismissed P0/P1 findings do not block; user overrides are recorded with their
  source, and all dispositions are registered through
  `workflow settle-findings --confirm/--dismiss`.
- Extend native installation with ordered managed intake-rule migration and
  symmetric uninstall cleanup for runtime directories, hooks, and host guidance
  files across Claude Code, Codex, and Cursor project installs.
- Resume interrupted claimed dispatches through the host's resume mechanism
  (e.g. Claude Code `SendMessage`) to complete the same dispatch instead of
  restarting: a CLAIMED dispatch with no recorded result keeps its claim,
  identity, and one-result mapping across the resume, falling back to the
  stale-and-redispatch path with a fresh zero-context agent only when resume is
  unavailable (host does not provide it, recovery fails, or the lifecycle
  cannot authenticate the resumed completion), and hosts whose lifecycle cannot
  verify the resume (Codex/Cursor providers) are not documented as
  resume-enabled.
- Make `requirements-clarification` the acceptance-phase clarification guide:
  the Q&A runs in the acceptance phase before the formal flow starts (one
  consequential decision at a time, from repository facts, no fixed
  questionnaire) and is mandatory unless no decision genuinely affects public
  behavior, acceptance, or architecture (then record "无剩余缺口"); the main
  agent presents the complete integrated requirement and technical solution,
  obtains the user's final confirmation ("确认" = presenting the integrated
  requirement and solution and receiving the final confirmation), and writes the
  integrated requirement and decisions to the requirement file in the acceptance
  phase as the run's acceptance input and source of truth. PASS records only what
  the user actually confirmed; the prompt names no later flow step and claims no
  registration or content-checking duties.
- Accept an ancestor (or equal) `--base-snapshot` at `workflow start`, making
  already-committed in-flight work fall inside the base-to-current review diff
  and enabling a documented takeover path for interrupted runs.
- Add a takeover flow (cheap build/test sanity check, then re-clarify and
  persist the aligned requirement, then `workflow start --base-snapshot <B0>`)
  with `resume` demoted to a backup path; fix the formal step-2 wording so it
  only registers already-aligned requirements and records PASS.
- Record per-gate and per-action prompt content hashes in run state at start
  (old state files without the field load compatibly) and report per-entry
  catalog deltas on resume instead of killing the run; unselected-only catalog
  changes do not block, and selected-gate changes are judged per result.
- Extend `workflow resume --adopt-external --reason` to explicitly rebind the
  current snapshot to a drifted native head with recorded provenance, leaving
  unaffected PASS results eligible for a Carry inheritance decision.
- Allow meaning-preserved requirement rebinding after development starts when
  the main agent records the unchanged-meaning assertion and the user confirms,
  retaining unaffected PASS results and rebinding the snapshot.
- Make Carry the unified inheritance entry: the main-agent `--main-agent
  --main-reason` decision now records INHERIT/RERUN at any rebinding moment
  (repair, adoption, catalog-delta acceptance) and accepts the new catalog.
- Require rerun gate reviews to cover the complete base-to-current delivery,
  declaring that scope explicitly in the composed prompt, and require every
  gate review to report the compared snapshot pair in its result contract,
  discarding mismatched reports.
- Record adopted external-change provenance in the Carry record instead of a
  write-only dedicated field, keep a selected gate whose prompt moved
  re-dispatchable after a main-agent Carry accepts the catalog delta, detect
  per-gate prompt changes against the shared reviewer base, and retire the
  production-dead Resume helper in favor of ResumeReport.
- Require a cheap sanity check (the target project's own documented validation)
  before dispatching gate/QA reviews after repair; obvious failures return to
  repair without subagents.
- Require the development worker to self-test before delivering, including the
  target project's own build/test (compilation must pass).
- Strengthen the implementation-quality gate's performance review for observable
  regressions.
- Discover independent review gates from package-local `gates/*.md` files.
- Compose every gate task from one shared reviewer base and one gate-specific
  prompt.
- Replace the receipt/closure workflow with one resumable run-state file and
  one retained seal or abort summary.
- Run selected QA Execution and selected independent gates in parallel on the
  current native VCS snapshot.
- Bind QA Review and gate results to unique prepared dispatches, reserve a fresh
  zero-context reviewer identity before recording, and source static result
  bindings from the dispatch rather than repeated operator flags.
- Resolve and verify immutable identities through the selected Git, SVN, or P4
  command for preparation, snapshots, results, Carry, and Seal.
- Register and freeze the complete requirement and solution artifact set at
  development start, retaining it as acceptance input while excluding it from
  ordinary post-development review targets.
- Require every formal QA set to include STATIC and LIVE cases, store per-case
  QA Review outcomes, and preserve exact unchanged passing cases across rework.
- Activate one pre-write intake for every project-content modification, require
  confirmation of the complete outcome and solution, then ask once for
  lightweight, full, or custom execution over the dynamic gate catalog.
- Keep lightweight execution stateless while full and custom require durable
  format-neutral requirements and solution documentation plus Start Readiness.
- Retain the chosen route across added requirements and dependency-aware formal
  slices, with one explicitly marked original-base overall run adopting initial
  and repaired merged snapshots for integration review while slice runs retain
  implementation ownership.
- Classify changed requirement revisions explicitly as meaning-preserving or
  meaning-changing, rebind the current native VCS identity before results are
  preserved or invalidated, and retain exact passing QA cases for unaffected
  coverage while new or changed cases become pending.
- Route P0/P1/P2 findings through one shared three-review-wave limit, keep
  Carry limited to previously passing selected gates, treat semantic results as
  authoritative for their snapshot, and persist snapshot-bound Seal skips.
- Allow a reasoned main-agent Carry shortcut for a bounded repair that cannot
  affect any prior selected PASS, inheriting both QA and discovered-gate PASS
  results while retaining independent Carry for uncertain or wider changes.
- Recompose interrupted prepared development tasks without moving their recorded
  development boundary.
- Treat independent results as candidate input until the main agent validates
  their confirmed premise, normal public reproduction, and evidence before
  recording or presenting a blocker.
- Restore independent QA Review between QA Design and development, with failed
  review returning the retained complete case set to Design rework.
- Reserve `qa` for the built-in QA flow so a discovered gate cannot collide
  with its routing and result ownership.
- Use the selected Git, SVN, or P4 command directly for cumulative and repair
  comparisons.
- Reject Seal while selected work is pending or lacks a matching PASS or
  permitted user skip authorization.

### Removed

- The formal `none` route and its empty-run branches; lightweight execution now
  occurs before workflow state exists.
- Fixed four-gate registries, extension manifests, prompt copies, context
  bundles, receipt and closure graphs, recursive Carry chains, generated
  handoffs, detailed gate-state trees, and their compatibility paths.
- The duplicate `formal-gates-validate` command, old evidence demos, and the
  standalone prompt-pollution pattern catalog.

### Fixed

- Apply per-mode precedence when reading QA cases across storage layouts
  (qaModeCases/allQACases): a concrete mode's own per-mode key, when non-empty,
  is authoritative and its legacy cases left in the merged "" key are ignored.
  A run that redesigned per-mode after a legacy single-list migration no longer
  double-counts the stale merged cases or lets a stale PENDING blackbox case
  block the snapshot's blackbox QA gate; the merged (empty-mode) view remains
  the full current set and merged-only runs keep the "" key's cases unchanged.
- Fix dispatch invalidation (RQ-013): prepare SHALL NOT invalidate any dispatch;
  the remaining invalidation is mode-scoped across every target (a blackbox
  dispatch never stales a whitebox dispatch of the same target, fixing the
  "whitebox review prepare staled the blackbox review" defect). Claiming a fresh
  same-function dispatch rejects an already-CLAIMED dispatch (no two parallel
  same-function subagents) unless its subagent was manually terminated by the
  main agent — the lifecycle stop event / interruption reason marks the prior
  STALE and allows the claim (the manual-termination exception, no new CLI
  command) — and automatically stales old OPEN empty tickets (no subagent / no
  start event) so they never block a claim. A STALE dispatch the reviewer had
  already claimed can still record its produced result (reviewer identity and
  content verified, no re-dispatch, lands on the current snapshot) as long as no
  same-function replacement is in flight (double-record guard); source-binding
  mismatches from a changed snapshot stay conservatively rejected.
- Share one stale-dispatch helper across adoption, requirement invalidation, and
  snapshot rebinding instead of three byte-identical loops.
- Resolve an adopted external change that needs no real rerun without counting a
  new automatic review wave; real reruns after an adoption are still counted.
- Keep run states loaded without prompt hashes reporting no catalog delta for an
  unmoved catalog even after their first mutation, instead of mis-reporting every
  entry after a partial per-gate backfill.

---

## [0.1.0] — 2026-06-13

### Added
- Portable `formal-gates-validate` Go CLI for cross-platform package and artifact validation
- Phase 2B host canary results for Claude / Codex / Cursor
- Darwin (macOS) strict test prompts
- `requirements-clarification-gate` with `DRAFT_BLOCKED` enforcement
- `complexity-gate` for scope creep and over-engineering prevention
- `architecture-health-gate` for module boundary and dependency health checks
- `code-quality-gate` for correctness, dead code, and test quality review
- `qa-test-gate` for test case design and evidence validation
- `enforce-gate-sequence.ps1` hook for machine-layer gate enforcement
- `gate-workflow.ps1` for recording gate workflow artifacts
- English translations for README and SKILL

### Changed
- Trim non-runtime package materials from distribution
- Improve public validation summary output
- Optimize formal-gates skill workflow convergence stop rule
- Harden formal gates hook validation logic
- Clarify Codex hook enforcement boundary (auxiliary guardrail only)
- Refine formal gates packaging for public release
- Bind gate reviews to dispatch prompts

### Fixed
- Formal gates hook and workflow regressions in Phase 2B
- Document write gate for OpenSpec proposal phase

---

## [0.0.1] — 2026-06-05

### Added
- Initial release with core four-gate system
- SKILL.md entry point for AI routing
- Gate-specific reference documents (`references/`)
- PowerShell installation and canary scripts
- `examples/` with GateWorkflow and behavior-check prompt samples
