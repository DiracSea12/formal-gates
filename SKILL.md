---
name: formal-gates
description: Use when asked to run, package, install, test, or diagnose the formal-gates workflow, four fixed post-development gates, requirements-clarification gate, release/seal validation, GateWorkflow hooks, host canaries, or A/B checks. Do not use for ordinary code review, implementation, debugging, brainstorming, wording edits, or casual requirement discussion unless the task is explicitly about formal-gates orchestration.
---

# Formal Gates

Thin router only. Classify the flow first, then load one referenced detail file for the first missing evidence; do not read the whole package by default.

## Fast Route

| Request | Route |
|---|---|
| Chat, brainstorming, explanation, typo, wording, or tiny low-risk change | Do not activate formal gates. |
| User explicitly asks for requirements alignment, pre-development review, or start-readiness review | Use the document/start-readiness route below; build or request an alignment table or equivalent artifact before drafting, and do not approve readiness before user-confirmed alignment evidence exists. |
| User explicitly asks for formal development handoff | Require OpenSpec or slice coverage, Complexity Contract or Ledger, and development handoff evidence; main agent does not implement directly inside that formal workflow. |
| User explicitly asks for formal review, four gates, final validation, release, or seal | Follow the fixed post-development sequence; never accept chat-only PASS. |
| Install, hooks, canary, A/B, candidate package testing, or host integration | Read `references/install-and-hooks.md` only for that task. |

## Formal Flow Router

Before any formal handoff or gate dispatch, classify the work as `none`, `four-gate`, `release`, `seal`, or `start-readiness-only`.

- `none`: ordinary small edits, explanations, research, informal/vibe coding, and mere OpenSpec or development intent.
- `start-readiness-only`: explicit user request or project requirement; no QA case design unless four-gate/release/seal is also claimed.
- `four-gate`/`release`/`seal`: QA black-box case design is required before implementation handoff.

First run rule: classify once, read the matching Load Map entry, collect the required evidence or block on the missing artifact, and record a gate only after evidence exists.

## Blacklist

Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests.

Never invent or add user-unapproved requirements, mechanisms, checks, fields, stages, hooks, or review criteria by calling them optimization, hardening, gap-filling, cleanup, or overengineering prevention. If independent evaluation shows no behavior gain, stop instead of expanding the entrypoint.

Reviewer findings do not automatically become requirements. Before fixing a finding, the main agent must decide whether the fix stays inside the user's stated scope and existing rules. If the smallest fix would add or change requirements, externally visible behavior, data formats, process steps, integration boundaries, validation rules, or acceptance criteria, stop and ask for explicit user approval instead of implementing the expansion.

Never start the four post-development gates, QA case design, or pre-development readiness review unless the Formal Flow Router requires it.

Never let the main agent implement non-trivial work inside a user-authorized formal development handoff, contaminate zero-context prompts, reuse PASS after snapshot change, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, or expand this entrypoint when details belong in `references/`.

If a formal workflow is represented as passed after direct implementation, skipped independent gates, or self-stamped gate verdicts, stop with `PROCESS_VIOLATION`.

## Load Map

| Need | Read | First action |
|---|---|---|
| Requirements/document alignment | `references/requirements-clarification-gate.md` | Build or request user-confirmed alignment evidence before drafting or approving readiness. |
| Mapping OpenSpec, PRD, SDD, issue, design brief, or markdown bundles | `references/requirement-document-adapters.md` | Map source documents to formal requirement fields before gate review. |
| Requirements PASS recording or artifact validation | `references/requirements-clarification-artifacts.md` | Validate the alignment artifact and decision record before recording PASS. |
| Formal implementation worker dispatch | `agents/development-worker.md` | Validate handoff first; development-time complexity budget checks trigger automatically during formal implementation. |
| Budget expansion request during development | `agents/anti-complexity-review.md` | Run independent anti-complexity review before any larger budget is used. |
| QA case design, execution, final execution, or white-box adequacy | `references/qa-test-gate.md` | Start with QA Design for pre-handoff formal flows, or QA Execution evidence for post-development review. |
| Scope, budget, over-engineering, or Complexity Contract | `references/complexity-gate.md` | Check QA or readiness prerequisites before dispatching complexity review. |
| Module boundaries, ownership, dependencies, lifecycle, failure semantics | `references/architecture-health-gate.md` | Run only after the required previous gate for the active flow has passed. |
| Correctness, maintainability, tests, dead code, overfitting, residual risk | `references/code-quality-gate.md` | Run only after QA, complexity, and architecture evidence are complete for the same snapshot. |
| Post-development artifact fields and recording commands | `references/post-development-artifacts.md` | Use the artifact templates and native commands when recording or verifying workflow state. |
| Install, hooks, canaries, manifests, host support | `references/install-and-hooks.md` | Run native install, preflight, or same-host canary checks; config alone is not proof. |

## Fixed Gate IDs

Post-development gate IDs cannot be renamed:

- `qa-test-gate`
- `complexity-gate`
- `architecture-health-gate`
- `code-quality-gate`

The pre-document gate is `requirements-clarification-gate`. It is not a fifth post-development gate.

## Authorized Formal Flow Order

Use these orders only after the router activates the matching formal flow. Project hook-enforced document gates count only with explicit opt-in and same-host live canary proof.

| Flow | Order |
|---|---|
| Optional document/start-readiness review | requirements clarification with user-confirmed alignment evidence -> `complexity-gate` -> `architecture-health-gate` -> cold-water start-readiness. Independent zero-context complexity, architecture-health, and cold-water conclusions are required before calling a formal readiness review passed. |
| Pre-development test design | For `four-gate`, `release`, or `seal`: QA `Design` -> `Design Review` -> `Design Rework` -> approved case set before implementation handoff. |
| Formal development handoff | Validate handoff -> dispatch `agents/development-worker.md`; development-time complexity budget checks are automatic inside the handoff and do not need a separate user request. Budget expansion routes to `agents/anti-complexity-review.md` before work continues. |
| Post-development release/seal | initial `Verification Run` -> QA `Execution` -> `complexity-gate` -> `architecture-health-gate` -> `code-quality-gate` -> final `Verification Run` -> `FinalExecution` -> optional QA `White-box Adequacy` -> seal. Every prerequisite must belong to the same `workflowId` and `changeSnapshot`. After the four post-development gates have recorded PASS for the same unchanged snapshot, `FinalExecution` may be a main-agent mechanical closeout that only checks existing records and final verification evidence. It must not add QA judgment, replace missing gates, reuse stale snapshots, or claim independent review. |
| Rerun after implementation change | Refresh `changeSnapshot`; old downstream PASS is invalid; choose earliest rerun gate by impact surface; review full requirement and current diff, not only repair patch. |

Do not enter complexity / architecture / code-quality until QA evidence is complete and preceding gates pass. Without QA final release/seal judgment, say `focused evidence pending full gate`.

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
- `manifestPath` and `manifestHash` for manifest-extended gates

`GateWorkflow.gate` must be `requirements-clarification-gate`, one fixed post-development gate ID, or a manifest-defined extension gate. Free-text workflow hints are not formal records. Extension gate prerequisites must be bound to the same manifest path and hash. Cross-workflow, cross-snapshot, or cross-manifest PASS reuse is invalid. Explicit `singleGateAuthorized=true` is advisory only and cannot progress release/seal.

## Host And Hook Caveats

Claude Code, Codex, and Cursor are separate host targets. Do not rank them as primary versus compatibility in public guidance.

Config is not proof; require same-host live canary with `PreToolUse` payload and blocked invalid command. If hook closure is unproven, fall back to explicit `formal-gates workflow` / `formal-gates gate` validation. For Codex, proof requires a real `codex exec` run that writes a `PreToolUse` payload, blocks a bad formal PASS command, and leaves the canary marker uncreated.

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
Development-time complexity budget:
Complexity check command:
Budget stop triggers:
Budget expansion approval path:
Forbidden context:
Formal flow mode:
Trigger source:
QA case design artifact:
Approved QA case set:
Continue after:
```

Formal development handoff is optional and user-authorized. When used, collect the template fields above, OpenSpec or slice coverage, and the Complexity Contract. The development-time complexity budget is active during implementation: the worker must run or update the supplied complexity check before continuing after meaningful growth and before returning implementation. If the active budget is exceeded, the worker must stop, shrink, or obtain independent Anti-Complexity Review approval before continuing. For `four-gate`, `release`, or `seal`, include approved QA case references before implementation starts. If no development subagent is available, output this `Gate Handoff Request` instead of implementing locally. Mark only genuinely missing facts as `BLOCKING_MISSING:<field> - how to obtain`.

`Development-time complexity budget` must include numeric `max-net`, `max-new-prod-files`, and `max-prod-insertions` values matching the `Complexity check command`. Qualitative scope boundaries are useful constraints but do not count as the numeric budget.
