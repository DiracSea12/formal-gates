## Status Meaning

These checkboxes are manual progress hints only. They do not prove formal PASS
or authorize delivery. Checkbox edits follow the ordinary snapshot rule for
their document; no checkbox-specific normalization is allowed.

## Phase 0. Review Convergence

- [x] 0.1 Tighten the existing central and reviewer instructions so only concrete current-change requirement, behavior, or mandatory-rule defects can block; advisory-only results PASS and duplicate root causes count once.
- [x] 0.2 Make the main agent filter reviewer output before creating repair work or clarification items, limit each gate in one delivery attempt to three completed automatic review-repair cycles, count only a complete formal result plus its processed repairs and required re-verification as one cycle, then stop that gate's automation and return deduplicated evidence-backed blockers for a user decision.
- [x] 0.3 Repurpose an existing behavior case, validate the package, and obtain one independent read-only review of this Phase 0 diff before user review. Add no product code, schema, command, role, state, or file.

At the end of each implementation phase, fix that phase's deliverable snapshot,
run its authorized post-development sequence, obtain any required carry
decision, complete final verification, and mechanically finalize from the four
gate evidence packages before the next phase begins.

## Phase 1. Machine Evidence Foundation And Cutover

Before Phase 1 start-readiness review or development handoff, run a static
repository scan against the then-current fixed snapshot and put the exact
producer/consumer file list in the existing Phase 1 handoff material. Cover
validator and CLI entrypoints, tests, gate agents and references, canaries,
examples, and operational documentation. Re-run the scan on the Phase 1
completion snapshot; every new or remaining surface must be migrated, deleted,
or explicitly shown not to consume or produce machine gate evidence. This is
phase-local handoff scope, not a runtime manifest, product feature, new CLI, or
long-lived inventory file.

Phase 1 fixes the requirements PASS and typed-policy defects already present in
the local diff directly in the final version-2 contract. It has no separately
deliverable Markdown checkpoint, partial schema-v1 path, or mixed-format state.

- [x] 1.1 Enforce every requirements-clarification PASS blocker already promised by the references in typed data: per-item fields and statuses, item counts, prior-ID continuity, decision approval, open blockers, precise covered targets, workflow/snapshot binding, and PASS verdict; add focused accepting and rejecting tests for each failure.
- [x] 1.2 Make `policy show --format json` project the same typed Go rule objects used by admission and artifact validation into the documented three-field Phase 1 output and eight exact policy IDs. Require the selected policy flow and every prerequisite flow to match the existing recording/admission mode and persisted state; add cross-flow rejection tests without adding an artifact field or CLI flag. Remove free-form rule strings, maps, and future-role placeholders; add accepting/rejecting behavior tests for every exported policy and check ID touched by the current diff and cutover.
- [x] 1.3 Implement the closed Phase 1 mapping from existing Markdown fields to the version-2 envelope and complete typed payloads for requirements, QA Execution, complexity, architecture, code-quality, and mechanical FinalExecution. Match the documented types, required/omitted rules, enums, nested objects, and role/gate/stage combinations; allow non-PASS verdicts only for reviewer roles and reject them for `REQUIREMENTS_PASS`, `QA_EXECUTION`, and `FINAL_EXECUTION` without producing an artifact or changing state. Reject duplicate/unknown fields, `null`, invalid UTF-8, wrong types, schema v1, and role conflicts. Admit only current-snapshot gate evidence, make each FinalExecution row reference that gate's immutable closure as `gateEvidence`, record the exact CLI-generated FinalExecution artifact path/hash without creating a fifth closure, and disable existing transition-based cross-snapshot PASS reuse until Phase 2 delivers Carry arbitration. Do not register Design Review, White-box Adequacy, or Carry Arbiter.
- [x] 1.4 Have each complexity, architecture, and code-quality reviewer write the shared typed JSON payload directly and move every current judgment label to its documented policy-owned `checks[]` ID; use the existing CLI as the authoritative decoder, validator, aggregate checker, and recorder, without an artifact-generation command, intermediate schema, conversion layer, or self-reported `reviewSessionId`. Enforce required, known, unique IDs, legal status, allowed `NOT_APPLICABLE`, typed evidence references, and findings/locations; keep parallel identity in the matching external dispatch/lifecycle/output receipt and reject ambiguous shared output paths.
- [x] 1.5 For Phase 1 QA Execution, use a dedicated mechanical payload that directly references the approved case set, QA-owned results, case-result binding, changed files, and verification. Have the main agent submit it and the CLI validate exact case coverage, PASS results and executions, hashes, workflow, snapshot, and binding without reviewer dispatch, checks, or receipt. Keep the complexity statistics-only report on its matching reviewer check. Update every producing and consuming agent, reference, README/operation guide, example, canary, fixture, policy output, and test in this phase; then remove old judgment fields, reviewer self-certification booleans, Markdown PASS parsing, schema-v1 evidence, and compatibility fixtures.
- [x] 1.6 Implement deterministic typed JSON bytes for CLI-owned mechanical artifacts and accept semantic-owner JSON regardless of object key order while hashing its exact submitted bytes. Have the orchestrating main agent write the single documented context-bundle JSON before reviewer dispatch, validate it statically with the existing CLI path, and require the reviewer to reference that exact unchanged bundle; do not add another manifest or generation command. Use run-local evidence references with `/` paths; reject absolute paths, URIs, traversal, backslashes, cross-run references, and symlink escape, with Windows and macOS/Linux fixtures. Build and re-verify one recursive closure root per requirements or post-development gate PASS containing its top-level artifact and all typed transitive inputs, plus a matching receipt only when the artifact role requires one; reject missing, conflicting, or cyclic references. Write a completed receipt first and atomically finalize its still-open dispatch registration last so any earlier failure remains retryable and cannot authorize PASS.
- [x] 1.7 Complete all deterministic validation before state mutation and change the existing gate-state writer to write a same-directory temporary file completely before replacement. Bind requirements and post-development gate PASS records to their closure path/hash; bind the finalization entry to the exact deterministic FinalExecution artifact path/hash, re-verify its four gate closures and final verification before seal, and never accept it as a gate prerequisite. Keep receipt, closure, gate-state, and final-verification wire fields CLI-internal and typed rather than expanding them into hand-authored public schemas; reject old workflow files instead of adding compatibility.
- [x] 1.8 Keep clarification interaction guidance in the existing reference/agent only: inspect repository facts first, ask one consequential question with a recommendation, and persist each user decision before the next question. Remove stale or contradictory wording and run the focused Go, behavior, package, documentation, and strict OpenSpec checks for the complete phase.

## Phase 2. Formal Review Chain

Phase 2 is not part of the Phase 1 development handoff. Before Phase 2 starts,
refresh its exact role, payload, check, and admission contracts against the
completed Phase 1 snapshot and pass a separate start-readiness review. Its
current lack of implementation-ready detail does not authorize guessing and
does not block Phase 1.

- [ ] 2.1 Put every review-related workflow file under `restricted/` without exception, including current dispatch/bundle/output, QA results, receipts, lifecycle events, closures, state, reports, logs, statistics, verification, repair and Carry material; reject any outside-restricted review artifact and preserve honest host capability claims.
- [ ] 2.2 Limit every independent reviewer to the confirmed current requirement, its current role, and the current diff or proposed change. Keep worktree/base/output details operational only; remove reviewer-visible workflow artifacts, copied requirements or rules, prior-gate results, verification summaries, repair history and other added context. Main-agent/CLI prerequisite and evidence handling stays outside reviewer context. Generate the exact seven-field final message, then use receipt registration as the single full pre-dispatch static check for every field, route, output contract, bundle binding and policy-specific input placement. Add the machine-generated static PASS binding that the reviewer must check without reading bound files. Send those bytes unchanged. Before receipt finalization, validate the completed reviewer JSON with dispatch-owned fields applied in memory and leave invalid output unfinalized. Do not impose prompt binding on non-judgment QA Design. Reject a fourth finalized review for the same workflow, gate, and stage unless the user explicitly authorizes another round.
- [ ] 2.3 Add `qa.design-review.v2` and `QA_REVIEW` at `qa-test-gate / Design Review` with the documented eight shared-payload checks, requirements prerequisite, Design-stage lifecycle receipt, independent reviewer receipt, exact case-hash binding, and positive/negative tests. Extend development handoff and the mechanical QA Execution payload with the accepted Design Review closure; normal implementation may move from the review snapshot to a later QA snapshot under the same workflow and exact case-set hash, while any case, oracle or Case ID change invalidates the review. Do not add a Design Rework role, copied approved-case artifact, White-box stage, or second QA Execution reviewer.
- [ ] 2.4 Add `carry.arbiter.v2` and the closed Carry payload, typed transition chain, per-gate decisions, receipt and Arbiter closure, and restore `workflow record-transition` using those typed inputs rather than Markdown fields. Extend admission and FinalExecution with the documented `FRESH_PASS`/`CARRIED_PASS` matrix rules, source/target snapshots, source gate closure and accepted Carry closure; add positive/negative tests for complete multi-hop chains, earliest rerun derivation, unchanged-target reuse, changed-target invalidation and conflicting transitions without adding a fifth gate PASS.

## Phase 2.5. Review Scheduling Evaluation

Phase 2.5 starts only after Phase 2 is complete. In Phase 2, QA
Design/Design Review/Design Rework and isolated candidate development run from
the same frozen requirements without sharing QA drafts, findings, or
implementation context; formal acceptance still waits for an approved case
set. The natural Phase 2 run doubles as the project sample, so do not repeat
development or gates merely to simulate another schedule. This phase adds no
product metrics, policy mode, role, field, state type, scheduler, or agent
runtime before the user chooses an option.

- [ ] 2.5.1 Summarize the natural Phase 2 run from existing run-local restricted material. Separate pre-development QA Design/Design Review/Design Rework from post-development QA Execution, and report available elapsed time, completed review-repair cycles, deduplicated blockers, snapshot changes, reruns, and host-reported token data without making unavailable metrics mandatory.
- [ ] 2.5.2 Evaluate the observed overlap between QA Design/Design Review/Design Rework and candidate development against the counterfactual serial wait. Separately compare post-development serial gates, QA-then-three-parallel, and one-four-gate-wave scheduling. Identify later blockers that evidence shows were already present before an earlier repair, distinguish actual observations from counterfactual estimates, and present both scheduling decisions to the user.
- [ ] 2.5.3 After the user confirms the future schedules, replace this evaluation-only contract with exact prerequisites, orchestration instructions, tests, and minimum implementation scope, then pass a separate start-readiness review before development. Reuse existing roles, envelopes, receipts, and state; do not add automatic selection or another review layer.

## Phase 3. Operational Convergence

- [ ] 3.1 Re-run the authoritative Phase 1 repository-wide completion scan after Phase 2 as a regression audit; prove no later change restored an operational surface that accepts or instructs the superseded machine vocabulary, and verify public documentation and threat-model mappings describe only implemented behavior. This task cannot defer or repair a missing Phase 1 migration surface.
- [ ] 3.2 Verify development-budget material cannot enter post-development complexity input directly or through evidence references; require a fresh statistics-only report with no budget fields or overrides.
- [ ] 3.3 Run formatting, unit, race, vet, focused fuzz/property, package, canary, behavior, strict OpenSpec, and supported-platform path checks required by the approved QA cases.
