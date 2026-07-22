# Complexity Gate

Use when the user asks for formal complexity review or start-readiness review,
or an already-authorized four-gate/release/seal flow reaches this gate. It checks
whether the proposal and actual change use the simplest sufficient solution,
modify or simplify existing structures before adding new ones, and keep code
volume proportionate to the confirmed requirement. Post-development complexity
reviews the same target snapshot independently of QA, architecture, and code
quality.

## Applicability

- Code implementation or refactor cleanup: formal delivery review runs
  complexity independently alongside the other post-development gates.
- `test-only`: use when harness, fixtures, runners, or evidence flow starts growing.
- `openspec-spec` / `prd` / `design-spec`: scope-review requirements, scenarios, schema, acceptance, compatibility promises, and extension hooks.
- `architecture-plan`: use when the plan adds components, state, public contract, or ownership.
- `conversation-only`: do not run.

Do not run just because a code task, document task, OpenSpec, or implementation intent exists. Formal complexity review and start-readiness review are user-authorized or project-required flows. Once the user has authorized a complete formal flow, later gates do not need separate per-gate user approval.

## Post-Development Gate Boundary

For a post-development four-gate/release/seal complexity review:

- review the generated `Current diff` directly;
- use the on-site Git, SVN, P4, or equivalent VCS to inspect its native diff,
  diff stat output and changed file contents when useful;
- make the size judgment semantically from the native VCS comparison and the
  confirmed requirement;
- judge scope size, code volume versus requirement size, unnecessary concepts,
  public/config surface growth, failure to modify/reuse/delete, and whether the
  implementation is the simplest sufficient solution.

The reviewer owns this semantic judgment and obtains its change information
directly from the on-site VCS. formal-gates records the workflow evidence and
decision.

## Impact Surface Review

Review the post-change affected surface, not only diff count: changed production/test/script/spec/doc files, direct module/owner/public contract/test harness touched, new call chain, config surface, state lifecycle, fixture/runner/evidence flow.

Do not borrow this as a license to clean the whole repo. Historical debt is residual risk unless this change worsens it.

For start-readiness, judge the proposed design and tasks; after development, judge the actual diff. In both modes, require each new file, type, field, validation branch, state, stage, wrapper, config, script, report, or evidence layer to enable a current observable requirement that modifying the existing owner cannot satisfy. Prefer modification, reuse, deletion, and local simplification. Do not be mechanical: when the explicit task is refactor, cleanup, or simplification, the right answer may be a clear restructure rather than the smallest diff.

Within the current task scope, complexity review must also look for redundant, stale, unused, unnecessarily complex, over-designed, or shrinkable logic, wording, tests, documents, scripts, and code.

## Stop Smells

Stop when the current requirement does not justify:

- New subsystem-ish names: `Manager`, `Service`, `Report`, `Evidence`, `Policy`, `Registry`, `Cache`, `Context`, `Provider`, `Orchestrator`.
- New global mutable state, process cache, config, report layer, state machine, or generic framework.
- Delete/consolidate/bugfix work with obvious net growth.
- Tests that assert fields, non-empty strings, or log text instead of behavior.
- “rigorous”, “strict”, “robust”, “secure”, “future-proof”, “extensible”, “later”, “generic”, “framework”, “platform”, or “complete” without a current observable behavior that requires the addition.

## Formal PASS

Post-development formal complexity review runs independently on the same workflow and target snapshot as QA, architecture, and code quality; finalization requires all four results.

Start-readiness complexity review runs after `requirements-clarification-gate` PASS for the same workflow and snapshot. Record and verify start-readiness reviews with `--mode start-readiness`; do not invent QA Execution evidence before code exists.

Record PASS with `references/post-development-artifacts.md`, using `formal-gates workflow record-stage --gate complexity-gate`. For start-readiness, include `--mode start-readiness`. Receipt registration generates the matching read-only `COMPLEXITY_REVIEW` catalog and all `complexity.*` check IDs. The reviewer supplies only ordered semantic statuses, messages, findings, and locations through `formal-gates receipt submit`; the CLI constructs every JSON object and finalization derives the verdict.

## Output

Do not edit or patch the assigned JSON. Use `formal-gates receipt submit` as documented in `references/post-development-artifacts.md`; invalid or incomplete input leaves the generated artifact unchanged. Human prose may summarize the judgment, but only the CLI-finalized artifact and submission proof determine admission.
