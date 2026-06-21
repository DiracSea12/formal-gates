# Code Quality Gate

Use after architecture PASS in a user-authorized four-gate/release/seal flow, or when the user asks for code quality consultation. Review correctness, maintainability, local coupling, performance, test quality, dead/redundant code, overfitting, encoding, and validation integrity.

Do not bless unfinished requirements, oversized scope, or bad architecture.

## Applicability

Run when an authorized formal flow reaches this gate, or when the user asks for formal code-quality review or code quality consultation and the work includes:

- Non-trivial implementation or refactor-cleanup before delivery.
- Test-only changes, to review assertion quality, fixture quality, and overfitting.
- Config/schema/examples/docs that are executable or user-consumed.
- Docs with commands, code snippets, API examples, migration steps, config examples, or operational procedures.

Do not run for pure OpenSpec/PRD/SDD/design prose with no executable content. If invoked there, output `BLOCKED` as not applicable instead of inventing fake correctness findings.

## Formal Entry

Formal flow requires machine-verifiable PASS for:

- `qa-test-gate` formal Stage=`Execution`
- `complexity-gate`
- `architecture-health-gate`

Verify:

```powershell
<ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate code-quality-gate -WorkflowId <id> -ChangeSnapshot <snapshot>
```

Missing state, stale snapshot, missing artifact, or any non-PASS prerequisite is `BLOCKED` or `GATE_SEQUENCE_ERROR`.

## Required Review

Review live diff, affected files/modules, and actual verification evidence.

- Correctness: behavior matches current request/spec, including error and fallback paths.
- Boundary cases: empty input, missing resources, invalid data, duplicates, concurrency/lifecycle as relevant.
- Platform/runtime safety: null checks, resource loading, ownership, threading/async/global state, include/import, module ownership.
- Error handling: do not swallow errors or blur warning/error/fallback.
- Maintainability: names are clear, functions focused, comments match behavior, no clever unreadable code.
- Local coupling: keep reasonable local coupling, simplify in place when enough, extract only when it removes real duplication/ripple/testing pain. If coupling is already degrading maintainability, split it.
- Performance: no obvious avoidable hot-path work, repeated I/O, unbounded scans, lifecycle leaks, excessive allocation, or needless recomputation for the changed behavior.
- Deletion hygiene: no dead include/function/test/config/doc/orphan.
- Test quality: tests verify behavior, not field existence, non-empty strings, log text, or source layout.
- Overfitting: no case-name, node-id, filename, path, threshold, or fixture-specific branch without explicit current requirement.
- Validation integrity: no hidden local fallback, baseline mutation, validation exemption, mock/bypass/headless/fake-provider pretending to be final user-visible proof.
- Runtime/user-visible proof: headless, unattended, mock, bypass, fake provider, generated baseline mutation, or log replay is not final proof for user-visible behavior.
- Platform/source boundary: do not edit third-party or platform source to hide a project bug unless the task explicitly owns that source.
- Encoding: preserve project encoding expectations.

## Verification

List actual commands, artifacts, and results. No verification evidence means no PASS. For executable docs you could not verify, verdict is at most REVIEW.

## Formal PASS

Record PASS with `references/post-development-artifacts.md`, using `-Gate code-quality-gate`. Shared machine fields and evidence substitutions live there. PASS only proceeds to Final Verification Run and final QA Execution; it is not final seal.

## Output

```text
Code Quality Gate
Verdict: PASS / REVIEW / FAIL / BLOCKED
Proceed to final QA: YES / NO
Work type:
Requirement verification status:
Previous gate status:
Correctness blockers:
Maintainability blockers:
Local coupling judgment:
Performance risks:
Test quality blockers:
Dead/redundant code:
Overfitting checks:
Validation/encoding checks:
Verification run:
Verification not run:
Evidence artifacts:
Required verification:
Residual risk:
gate_route:
```

Findings first. If there is nothing to report, say you cannot find a fault and list remaining verification gaps. Do not write “overall looks good.”
