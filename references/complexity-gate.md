# Complexity Gate

Use before implementation/worker handoff to set a Complexity Contract, and after QA Execution PASS to review diff shape, budget, new concepts, and overengineering.

## Applicability

- `code-implementation` / `refactor-cleanup`: write a contract before coding; formal delivery review requires prior `qa-test-gate` formal Execution PASS.
- `test-only`: use when harness, fixtures, runners, or evidence flow starts growing.
- `openspec-spec` / `prd` / `design-spec`: scope-review requirements, scenarios, schema, acceptance, compatibility promises, and extension hooks.
- `architecture-plan`: use when the plan adds components, state, public contract, or ownership.
- `conversation-only`: do not run.

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

## Budget Rules

Budgets are task-specific. Fallback thresholds in `complexity_gate.py` are alarms, not design truth.

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

## Diff Script

Run only when there is a diff to review:

```powershell
<ps> -File <formal-gates>/scripts/run-complexity-gate.ps1 --task-type <type> --max-net <n> --max-new-prod-files <n> --max-prod-insertions <n> --worktree <repo> --vcs auto
```

Use `--json` for machine output and `--staged` only for staged review. The wrapper chooses an available Python host and uses git, SVN, or manual-evidence REVIEW when neither VCS is detected.

In non-git worktrees, script totals may include stale logs, generated files, or old changes. Cross-check changed files against the Complexity Contract, task brief, or OpenSpec change. Record which counts are working-copy noise versus this task. Do not dismiss REVIEW/FAIL as noise without that subtraction.

Exit codes: `0` PASS alarm state, `2` REVIEW alarm state, `1` FAIL alarm state.

Script PASS does not mean design PASS. REVIEW/FAIL in formal flow blocks downstream gates.

## Impact Surface Review

Review the post-change affected surface, not only diff count: changed production/test/script/spec/doc files, direct module/owner/public contract/test harness touched, new call chain, config surface, state lifecycle, fixture/runner/evidence flow.

Do not borrow this as a license to clean the whole repo. Historical debt is residual risk unless this change worsens it.

## Stop Smells

Stop when the current contract did not budget:

- New subsystem-ish names: `Manager`, `Service`, `Report`, `Evidence`, `Policy`, `Registry`, `Cache`, `Context`, `Provider`, `Orchestrator`.
- New global mutable state, process cache, config, report layer, state machine, or generic framework.
- Delete/consolidate/bugfix work with obvious net growth.
- Tests that assert fields, non-empty strings, or log text instead of behavior.
- “future-proof”, “extensible”, “later”, “generic”, “framework”, “platform”, or “complete” without current demand evidence.

## Formal PASS

Formal complexity review can run only after `qa-test-gate` formal Execution PASS for the same workflow and snapshot.

Record PASS with the shared command in `references/post-development-artifacts.md`, using `-Gate complexity-gate`.

Formal PASS artifacts must include the shared zero-context fields plus:

```text
Script result:
Diff shape judgment:
Impact surface health:
Public/config surface:
New concepts:
Shrink opportunities:
Decision evidence:
Changed files artifact:
Verification artifact:
```

These fields follow the shared post-development artifact rules: `Changed files artifact` 可替换为 `Raw diff artifact`；`Verification artifact` 可替换为 `Developer self-test artifact`。路径必须存在，正式机器记录会校验。

## Output

```text
Complexity Gate Judgment
Verdict: PASS / REVIEW / FAIL / BLOCKED
Proceed to architecture: YES / NO
Requirement verification status:
Script result:
Diff shape judgment:
Budget/expansion status:
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
Budget status:
Impact surface health:
Stop triggers hit:
Things deliberately not built:
Still shrinkable:
```
