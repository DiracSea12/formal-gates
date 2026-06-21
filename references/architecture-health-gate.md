# Architecture Health Gate

Use after complexity PASS in a user-authorized four-gate/release/seal flow, or when the user asks for architecture consultation. Review boundaries, ownership, public surface, dependencies, state/cache lifecycle, failure semantics, compatibility, performance shape, and maintainability.

Do not use architecture review to hide scope creep. If complexity is wrong, stop there.

## Applicability

Run when an authorized formal flow reaches this gate, or when the user asks for formal architecture review or architecture consultation and the work includes:

- Public header/interface/API, config surface, serialized contract changes.
- Module dependency, include/import direction, runtime/editor/server/client/test boundary changes.
- New or changed global state, cache, callback, singleton, registry, service, manager, report, or orchestration ownership.
- Files/classes starting to own multiple responsibilities.
- Broad refactor or cross-owner structure change.
- Specs/docs/plans that define architecture, state ownership, API contracts, deployment topology, failure semantics, compatibility promises, extension boundaries, or operational responsibility.
- Research notes that stop being observation and start recommending a concrete architecture or boundary.

Do not run for pure conversation-only work or ordinary test additions with no harness/ownership change.

For `test-only` work, run only when the harness, environment, fixture ownership, automation entrypoint, or evidence architecture changes. Ordinary behavior-test additions do not need an architecture gate.

For spec/doc/plan work, do not demand implementation evidence. Judge whether the proposed boundary, ownership, lifecycle, and failure semantics are implementable.

## Formal Entry

Formal flow requires machine-verifiable PASS for:

- `qa-test-gate` formal Stage=`Execution`
- `complexity-gate`

Verify:

```powershell
<ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate architecture-health-gate -WorkflowId <id> -ChangeSnapshot <snapshot>
```

No gate-state, stale snapshot, missing artifact, non-formal QA, or complexity REVIEW/FAIL/BLOCKED means `BLOCKED` or `GATE_SEQUENCE_ERROR`.

## Required Review

Review facts in the live diff/spec/doc/plan, not author intent.

- Module boundaries: no reverse dependency, private implementation leak, or layer mixing.
- Public surface: temporary implementation details must not become contract.
- Ownership: each changed class/function has one owner and one change reason.
- Data flow: writer, reader, reset point, lifecycle, and failure state are clear.
- Dependency direction: higher-level code must not drag lower layers into knowing its details.
- State/cache lifecycle: global/process state needs reset boundaries and tests.
- Failure semantics: exact, fallback, warning, error, ambiguous, and unsupported must not be blended into mush.
- Compatibility: old paths/fields/behaviors cannot be retained without explicit user approval.
- Performance shape: changed data flow, caching, lifecycle, polling, I/O, or cross-boundary calls must not add obvious avoidable hot-path cost.
- Post-change module health: affected files/modules must not become god files or catch-all helpers.

## Decoupling Judgment

Do not split reflexively. Choose one:

- `keep coupled`: same responsibility, lifecycle, and change reason.
- `simplify in place`: coupling is local but the code can be made clearer without new concepts.
- `extract narrowly`: extraction removes real duplication/ripple/testing pain with fewer concepts than it adds.
- `redesign boundary`: ownership or dependency direction is wrong enough that local cleanup is a lie.

If coupling is making responsibilities mixed, tests painful, rules duplicated, or changes ripple across modules, split it. “Avoid overengineering” is not a shield for a design that is rotting.

## Fix Order

1. Delete unnecessary responsibility.
2. Move logic back to the existing owner/boundary.
3. Narrow public surface.
4. Reuse existing owner/structure.
5. Keep reasonable local coupling or simplify in place.
6. Extract narrowly or redesign only when it reduces real risk.

New abstraction/framework/manager/service needs active Complexity Contract budget and deletion of old complexity as trade.

## Formal PASS

Record PASS with `references/post-development-artifacts.md`, using `-Gate architecture-health-gate`. Shared machine fields and evidence substitutions live there; this file only defines the architecture judgment.

## Output

```text
Architecture Health Gate
Verdict: PASS / REVIEW / FAIL / BLOCKED
Proceed to code-quality: YES / NO
Work type:
Requirement verification status:
Previous gate status:
Post-change module health:
Boundary violations:
Ownership leaks:
Public surface growth:
State/cache lifecycle risks:
Dependency direction risks:
Failure-semantics risks:
Compatibility retained:
Performance risks:
Decoupling judgment:
Simpler architecture available:
Must-fix before code-quality review:
gate_route:
```

`REVIEW`, `FAIL`, and `BLOCKED` cannot proceed to code-quality in formal flow.
