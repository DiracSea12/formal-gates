---
name: formal-gates
description: Use when asked to run, package, install, test, or diagnose the formal-gates workflow, four fixed post-development gates, requirements-clarification gate, release/seal validation, GateWorkflow hooks, host canaries, or A/B checks. Do not use for ordinary code review, implementation, debugging, brainstorming, wording edits, or casual requirement discussion unless the task is explicitly about formal-gates orchestration.
---

# Formal Gates

Thin router only. Load one referenced detail file when the active route needs it; do not read the whole package by default.

## Fast Route

| Request | Route |
|---|---|
| Chat, brainstorming, explanation, typo, wording, or tiny low-risk change | Do not activate formal gates. |
| OpenSpec / PRD / SDD / phase / start-readiness document work | Run `requirements-clarification-gate` before drafting, status judgment, QA, gate dispatch, or development. |
| Non-trivial code/script/test/config/API/schema/persistence/security/multi-file behavior work | Require requirement coverage plus development handoff evidence; main agent does not implement directly. |
| Formal review, four gates, final validation, release, or seal | Follow the fixed post-development sequence; never accept chat-only PASS. |
| Install, hooks, canary, A/B, candidate package testing, or host integration | Read `references/install-and-hooks.md` only for that task. |

## Blacklist

Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests.

Never let the main agent implement non-trivial work, contaminate zero-context prompts with anchoring patterns (previous findings, fixes, focus areas, expected outcomes), reuse PASS after snapshot change, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, allow cross-workflow gate reuse, or expand this entrypoint when details belong in `references/`.

If direct implementation, skipped independent gates, self-stamped gate verdicts, or contaminated dispatch prompts are discovered, stop with `PROCESS_VIOLATION`.

## Load Map

| Need | Read |
|---|---|
| Requirements/document alignment | `references/requirements-clarification-gate.md` |
| Mapping OpenSpec, PRD, SDD, issue, design brief, or markdown bundles | `references/requirement-document-adapters.md` |
| Requirements PASS recording or artifact validation | `references/requirements-clarification-artifacts.md` |
| QA case design, execution, final execution, or white-box adequacy | `references/qa-test-gate.md` |
| Scope, budget, over-engineering, or Complexity Contract | `references/complexity-gate.md` |
| Module boundaries, ownership, dependencies, lifecycle, failure semantics | `references/architecture-health-gate.md` |
| Correctness, maintainability, tests, dead code, overfitting, residual risk | `references/code-quality-gate.md` |
| Post-development artifact fields and recording commands | `references/post-development-artifacts.md` |
| Install, hooks, canaries, manifests, host support | `references/install-and-hooks.md` |

## Fixed Gate IDs

Post-development gate IDs cannot be renamed:

- `qa-test-gate`
- `complexity-gate`
- `architecture-health-gate`
- `code-quality-gate`

The pre-document gate is `requirements-clarification-gate`. It is not a fifth post-development gate.

## Required Order

| Flow | Order |
|---|---|
| Pre-development OpenSpec review | requirements clarification → complexity → architecture-health → cold-water. QA gate is NOT required. System detects pre-development when requirements artifact contains `Downstream permission: READY_TO_DRAFT`. |
| Post-development code review | QA `Design` -> `Design Review` -> `Design Rework` -> initial `Verification Run` -> QA `Execution` -> `complexity-gate` -> `architecture-health-gate` -> `code-quality-gate` -> final `Verification Run` -> QA `FinalExecution` -> optional QA `White-box Adequacy` -> seal. QA gate IS required before complexity. |
| Rerun after implementation change | Refresh `changeSnapshot`; old downstream PASS is invalid; choose earliest rerun gate by impact surface; review full requirement and current diff, not only repair patch. |

Pre-development and post-development workflows are isolated. All prerequisite gates and their transitive dependencies must belong to the same workflowId and changeSnapshot. Cross-workflow gate reuse is blocked.

If QA evidence is incomplete, do not enter complexity / architecture / code-quality in post-development flow. If complexity does not pass, do not enter architecture. If architecture does not pass, do not enter code-quality. Without QA final release/seal judgment, say `focused evidence pending full gate`.

## Zero-context Handoff

Zero-context is not empty context. Dispatch must include bundle or manifest path and SHA, worktree, base commit or snapshot id, requirement target, exact scope, forbidden files/items/context, Complexity Contract or Ledger when applicable, related artifacts, output template, and required verification.

Prompts must not include main-agent conclusions, suspicions, previous findings, expected answers, target verdicts, or focus items. Contaminated prompts are automatically detected and blocked. Formal review dispatch uses the matching file under `agents/` and must tell the reviewer not to load or execute skills including `formal-gates`.

## GateWorkflow Minimum

Formal records need structured `GateWorkflow` with:

- `workflowId`
- `changeSnapshot`
- `worktree` or `statePath`
- current `gate`
- current `stage` for QA or manifest-extended gates

`GateWorkflow.gate` must be `requirements-clarification-gate`, one fixed post-development gate ID, or a manifest-defined extension gate. Free-text workflow hints are not formal records. Explicit `singleGateAuthorized=true` is advisory only and cannot progress release/seal.

## Host And Hook Caveats

Claude Code, Codex, and Cursor are separate host targets. Do not rank them as primary versus compatibility in public guidance.

Config is not proof; require same-host live canary with `PreToolUse` payload and blocked invalid command. If hook closure is unproven, fall back to `gate-workflow.ps1` / `gate-state.ps1` validation. For Codex, proof requires a real `codex exec` run that writes a `PreToolUse` payload, blocks a bad formal PASS command, and leaves the canary marker uncreated.

## Output Standards

| Situation | Required response |
|---|---|
| Missing complete gate evidence | `focused evidence pending full gate` |
| Missing independent gate artifact | blocked or `CONDITIONAL_PASS`, not formal PASS |
| Requirements narrowed without authorization | `REQUIREMENTS_SCOPE_MISMATCH` |
| Contaminated process or prompt | `PROCESS_VIOLATION` |
| Independent reviewer unavailable | Output the full `Gate Handoff Request` below. |

Hooks or gate-state only prove sequence and recording, not code quality. Quality verdict still requires independent gate artifact.

## Gate Handoff Request

```text
Gate Handoff Request
Reason:
Skill source path:
Copied skill path:
WorkflowId:
Change snapshot:
Worktree:
Base commit:
Snapshot id:
Requirement document target or OpenSpec change:
Required independent gates:
Artifacts to provide:
Bundle or manifest path:
Verification requirements:
Forbidden context:
Continue after:
```

Development handoff must first collect worktree, base commit or snapshot id, `changeSnapshot`, existing bundle or manifest path plus hash, requirement coverage, Complexity Contract, forbidden items, and verification requirements. Mark only genuinely missing facts as `BLOCKING_MISSING:<field> - how to obtain`.
