# Architecture Health Gate

Use as an independent reviewer in a user-authorized four-gate/release/seal flow, or when the user asks for architecture consultation. Review boundaries, ownership, public surface, dependencies, state/cache lifecycle, failure semantics, compatibility, performance shape, and maintainability.

Do not use architecture review to hide scope creep. Report architecture-owned
findings independently; complexity scope findings remain with the complexity
gate and do not serialize this review.

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

Post-development architecture is an independent gate. Verify its own
target-bound reviewer evidence:

```bash
bin/formal-gates workflow verify-admission --worktree <repo> --gate architecture-health-gate --workflow-id <id> --change-snapshot <snapshot>
```

No gate-state, stale snapshot, missing artifact, or non-formal evidence means
`BLOCKED`. Post-development architecture has no gate-to-gate prerequisite;
finalization checks the complete four-gate set.

Start-readiness architecture review requires same-workflow, same-snapshot
`requirements-clarification-gate` PASS recorded with `--mode requirements` (or
the default requirements mode); it runs independently of the start-readiness
complexity review. Verify and record the architecture result with
`--mode start-readiness`; do not require QA Execution before code exists.

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

New abstraction/framework/manager/service must be justified by the current
requirement and delete or replace old complexity where applicable.

## Formal PASS

Record PASS with `references/post-development-artifacts.md`, using `formal-gates workflow record-stage --gate architecture-health-gate`. Shared machine fields and evidence substitutions live there; this file only defines the architecture judgment.

## Output

Receipt registration generates the read-only schema-version-2 `ARCHITECTURE_REVIEW` catalog, matching policy, check IDs, and typed evidence bindings. Submit only ordered semantic statuses, messages, findings, and locations through `formal-gates receipt submit`; never edit the JSON. The CLI constructs the nested artifact and proof, and finalization derives the verdict and receipt. A non-PASS architecture result blocks only this gate; independent code-quality review may continue, while finalization still requires all four PASS results.
