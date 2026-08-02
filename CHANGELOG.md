# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

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
