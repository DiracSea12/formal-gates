# Complexity Gate

Use when the user asks for formal complexity review, start-readiness review, formal development handoff, or an already-authorized four-gate/release/seal flow reaches this gate. It sets a Complexity Contract and development-time budget before an authorized formal handoff. After QA Execution PASS, it reviews diff shape, new concepts, public/config surface, minimum sufficient implementation, reuse/deletion, and overengineering; it does not enforce development-time numeric budgets as the post-development gate threshold.

## Applicability

- `code-implementation` / `refactor-cleanup`: when the user authorized formal handoff, write a contract before coding; formal delivery review requires prior `qa-test-gate` formal Execution PASS.
- `test-only`: use when harness, fixtures, runners, or evidence flow starts growing.
- `openspec-spec` / `prd` / `design-spec`: scope-review requirements, scenarios, schema, acceptance, compatibility promises, and extension hooks.
- `architecture-plan`: use when the plan adds components, state, public contract, or ownership.
- `conversation-only`: do not run.

Do not run just because a code task, document task, OpenSpec, or implementation intent exists. Formal complexity review and start-readiness review are user-authorized or project-required flows. Once the user has authorized a complete formal flow, later gates do not need separate per-gate user approval.

## Complexity Contract

Write before code changes or worker dispatch:

```text
Complexity Contract
Task type:
Goal in one sentence:
Expected diff shape:
Production file budget:
Public API/config budget:
New subsystem budget:
Allowed new concepts:
Forbidden concepts:
Existing structures to reuse first:
Expansion evidence required:
Stop triggers:
```

Task type must be one of `delete-or-consolidate`, `bugfix`, `small-feature`, `refactor`, or `new-system`. Default narrow; never quietly upgrade work into `new-system`.

## Development-Time Budget Rules

Development-time budgets are task-specific implementation controls. They exist before and during formal development handoff only. `formal-gates complexity check` has no built-in numeric default budget: if no numeric budget is passed, it only reports diff statistics and non-budget review signals. The main agent must set the development-time budget from the actual requirement, planned slice, expected diff shape, reused/deleted code, allowed production files, and test/documentation needs before formal development starts. Explicit numeric budget thresholds are development-time alarms, not design truth or post-development gate criteria.

Budget compliance is not complexity approval. A diff can stay within line/file budgets and still FAIL or REVIEW because it adds unnecessary concepts, expands public/config surface, avoids deletion/reuse, or is not the minimum sufficient implementation for the request. Post-development `complexity-gate` artifacts must not use line/file threshold compliance as the basis for PASS.

Line budgets must not be gamed by reducing readability. If code is compressed to fit the budget, such as unrelated statements packed onto one line, unclear short names, merged responsibilities, hidden branching, or removed useful comments/error handling, treat it as complexity budget evasion and fail or review even when numeric counts are within budget.

Development-time budget control is mandatory inside a formal development handoff or equivalent project process. The handoff must give the worker the active Complexity Contract, the exact budget numbers passed to `formal-gates complexity check`, stop triggers, and the budget expansion path. At minimum, the handoff budget must state numeric `max-net`, `max-new-prod-files`, and `max-prod-insertions` values, and those values must match the supplied check command. Allowed-file or forbidden-file scope is necessary, but it is not a substitute for numeric budget thresholds. The worker must check the live diff against that contract before continuing after meaningful growth and before returning implementation. If the active budget is exceeded, the worker must stop: either shrink the diff back inside budget or obtain independent Anti-Complexity Review approval before continuing. Waiting until the post-development complexity gate to explain the excess is a development-process failure.

Do not derive a formal development budget from tool defaults. If the main agent cannot justify the numbers from the current requirement and planned work, the handoff is not ready.

If a worker needs to exceed the active dynamic budget, it must stop and submit:

```text
Budget Expansion Request
Current Complexity Contract:
Current budget:
Current diff:
Exceeded item:
Why the excess is necessary for current scope:
What was deleted/reused/simplified first:
Why current scope cannot be completed well without expansion:
Cheaper alternatives considered:
Why cheaper alternatives fail:
Proposed new budget:
Files affected:
Risk if denied:
```

Before approval, verify shrink-before-grow: delete old logic, reuse existing structures, narrow fields/reports/config/tests, drop future completeness shells, and explain which current requirement or quality bar fails without expansion.

Without that proof, deny expansion.

Budget expansion requires independent anti-complexity review:

```text
Anti-Complexity Review
Verdict: APPROVE / DENY / APPROVE_SMALLER
Reason:
Unproven assumptions:
Shrink-before-grow check:
Unnecessary concepts to delete:
Approved budget, if any:
Expiration: this task only
```

Only `APPROVE` or `APPROVE_SMALLER` changes the active budget, and only for the current task.

## Post-Development Gate Boundary

The post-development `complexity-gate` is separate from development-time budget control.

For a post-development four-gate/release/seal complexity review:

- do not create a new numeric budget;
- do not pass `--max-net`, `--max-new-prod-files`, or `--max-prod-insertions` for the formal gate script evidence;
- do not turn a line/file count threshold into the gate's PASS/REVIEW/FAIL criterion;
- do not request or approve budget expansion as part of this gate;
- do not include development-time budget history, budget status, or budget expansion fields in the post-development gate artifact;
- do use statistics-only diff output, changed-files artifacts, QA evidence, and the requirement scope to judge whether the implementation is the simplest sufficient solution.

Development-time budget data belongs only to the formal development handoff, worker result, and anti-complexity approval path. It must disappear from the post-development complexity artifact. An unapproved development-time budget overrun is a development handoff/process problem, not a post-development gate field. The post-development verdict must be based on scope size, unnecessary concepts, public/config surface growth, failure to reuse/delete, overengineering, and whether the implementation is minimum sufficient for the request.

## Diff Script

Run only when there is a diff to review:

```bash
bin/formal-gates complexity check --task-type <type> --worktree <repo> --vcs auto
```

Use `--json` for machine output and `--staged` only for staged review. Post-development complexity gate evidence must omit all three numeric budget flags and use the command as statistics plus non-budget review signals. Formal handoff and development-time checks must pass all three numeric budget flags together. The native checker uses git, SVN, or manual-evidence REVIEW when neither VCS is detected.

In non-git worktrees, script totals may include stale logs, generated files, or old changes. Cross-check changed files against the Complexity Contract, task brief, or OpenSpec change. Record which counts are working-copy noise versus this task. Do not dismiss REVIEW/FAIL as noise without that subtraction.

Exit codes: `0` PASS alarm state or stats-only success, `2` REVIEW alarm state, `1` FAIL alarm state. In post-development gate use, a numeric-budget REVIEW/FAIL result means the wrong command was used; rerun statistics-only instead of recording that result.

Script PASS does not mean design PASS. REVIEW/FAIL in formal flow blocks downstream gates.

## Impact Surface Review

Review the post-change affected surface, not only diff count: changed production/test/script/spec/doc files, direct module/owner/public contract/test harness touched, new call chain, config surface, state lifecycle, fixture/runner/evidence flow.

Do not borrow this as a license to clean the whole repo. Historical debt is residual risk unless this change worsens it.

Also judge whether the current implementation is the smallest sufficient implementation for the stated request. Prefer reuse, deletion, and local simplification before adding new files, types, fields, config, scripts, stages, or reports. Do not be mechanical: when the explicit task is refactor, cleanup, or simplification, the right answer may be a clear restructure rather than the smallest diff.

Within the current task scope, complexity review must also look for redundant, stale, unused, unnecessarily complex, over-designed, or shrinkable logic, wording, tests, documents, scripts, and code.

## Stop Smells

Stop when the current contract did not budget:

- New subsystem-ish names: `Manager`, `Service`, `Report`, `Evidence`, `Policy`, `Registry`, `Cache`, `Context`, `Provider`, `Orchestrator`.
- New global mutable state, process cache, config, report layer, state machine, or generic framework.
- Delete/consolidate/bugfix work with obvious net growth.
- Tests that assert fields, non-empty strings, or log text instead of behavior.
- “future-proof”, “extensible”, “later”, “generic”, “framework”, “platform”, or “complete” without current demand evidence.

## Formal PASS

Post-development formal complexity review can run only after `qa-test-gate` formal Execution PASS for the same workflow and snapshot.

Start-readiness complexity review runs after `requirements-clarification-gate` PASS for the same workflow and snapshot. Record and verify start-readiness reviews with `--mode start-readiness`; do not invent QA Execution evidence before code exists.

Record PASS with `references/post-development-artifacts.md`, using `formal-gates workflow record-stage --gate complexity-gate`. For start-readiness, include `--mode start-readiness`. Formal PASS artifacts must include the shared zero-context fields plus these complexity-specific fields:

```text
Script result:
Diff shape judgment:
Impact surface health:
Public/config surface:
New concepts:
Minimum sufficient implementation:
Shrink opportunities:
Decision evidence:
```

Post-development complexity artifacts must not include `Development-time budget history`, `Budget/expansion status`, `Budget status`, or `Budget expansion approval`. If any of these fields appear, reject the artifact and regenerate it without development-time budget material.

## Output

```text
Complexity Gate Judgment
Verdict: PASS / REVIEW / FAIL / BLOCKED
Proceed to architecture: YES / NO
Requirement verification status:
Script result:
Diff shape judgment:
Minimum sufficient implementation:
Stop triggers:
Shrink opportunities:
Decision evidence:
gate_route:
```

Also include:

```text
Complexity Ledger
New concepts:
Deleted concepts:
Net complexity:
Impact surface health:
Stop triggers hit:
Things deliberately not built:
Still shrinkable:
```
