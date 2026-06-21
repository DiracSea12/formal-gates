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
| User explicitly asks for requirements alignment, pre-development review, or start-readiness review | Use the document/start-readiness route below. |
| User explicitly asks for formal development handoff | Require requirement coverage plus development handoff evidence; main agent does not implement directly inside that formal workflow. |
| User explicitly asks for formal review, four gates, final validation, release, or seal | Follow the fixed post-development sequence; never accept chat-only PASS. |
| Install, hooks, canary, A/B, candidate package testing, or host integration | Read `references/install-and-hooks.md` only for that task. |

## Blacklist

Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests.

Never invent or add user-unapproved requirements, mechanisms, checks, fields, stages, hooks, or review criteria by calling them optimization, hardening, gap-filling, cleanup, or overengineering prevention. If broader scope seems necessary, ask the user first and get explicit permission.

Never start the four post-development gates or pre-development readiness review unless the user explicitly asks for formal gates, formal review, readiness review, release, seal, or equivalent wording.

Never let the main agent implement non-trivial work inside a user-authorized formal development handoff, contaminate zero-context prompts, reuse PASS after snapshot change, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, or expand this entrypoint when details belong in `references/`.

If a formal workflow is represented as passed after direct implementation, skipped independent gates, or self-stamped gate verdicts, stop with `PROCESS_VIOLATION`.

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

## Authorized Formal Flow Order

Use these orders only after the user has explicitly asked for the matching formal gate flow. Ordinary implementation or document work does not enter these flows by default.

| Flow | Order |
|---|---|
| Optional document/start-readiness review | requirements clarification, architecture shape, complexity/scope, cold-water start-readiness. Independent zero-context complexity, architecture-health, and cold-water conclusions are required before calling a formal readiness review passed. |
| Pre-development test design | QA `Design` -> `Design Review` -> `Design Rework` -> approved case set. |
| Post-development release/seal | initial `Verification Run` -> QA `Execution` -> `complexity-gate` -> `architecture-health-gate` -> `code-quality-gate` -> final `Verification Run` -> QA `FinalExecution` -> optional QA `White-box Adequacy` -> seal. |
| Rerun after implementation change | Refresh `changeSnapshot`; old downstream PASS is invalid; choose earliest rerun gate by impact surface; review full requirement and current diff, not only repair patch. |

If QA evidence is incomplete, do not enter complexity / architecture / code-quality. If complexity does not pass, do not enter architecture. If architecture does not pass, do not enter code-quality. Without QA final release/seal judgment, say `focused evidence pending full gate`.

## Zero-context Handoff

Zero-context is not empty context. Dispatch must include bundle or manifest path and SHA, worktree, base commit or snapshot id, requirement target, exact scope, forbidden files/items/context, Complexity Contract or Ledger when applicable, related artifacts, output template, and required verification.

Prompts must not include main-agent conclusions, suspicions, previous findings, expected answers, target verdicts, or focus items. Formal review dispatch uses the matching file under `agents/` and must keep the reviewer separate from gate orchestration: the reviewer may follow host-required skill instructions, but must not run gate orchestration, record PASS, or let a skill replace the supplied evidence.

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

Formal development handoff is optional and user-authorized. When used, it must first collect worktree, base commit or snapshot id, `changeSnapshot`, existing bundle or manifest path plus hash, requirement coverage, Complexity Contract, forbidden items, and verification requirements. Mark only genuinely missing facts as `BLOCKING_MISSING:<field> - how to obtain`.
