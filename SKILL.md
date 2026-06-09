---
name: formal-gates
description: Use when the main orchestrating agent is explicitly asked to run, package, install, test, or diagnose the formal-gates workflow, four-gate sequence, requirement clarification gate, release/seal validation, GateWorkflow hooks, host canaries, or formal-gates A/B testing. Do not use for ordinary code review, zero-context review subagent tasks, reading OpenSpec files, implementation, debugging, brainstorming, wording edits, or casual requirement discussion unless this agent is being asked to orchestrate formal-gates.
---

# Formal Gates

This is the entry point for the formal-gates process. It handles routing, red lines, and handoff only. Load one detail file on demand; do not read the whole package during normal gate work.

## Fast Route

- Ordinary chat, brainstorming, explanations, wording edits, typo fixes, or small low-risk changes: do not activate formal gates.
- Requirement-document work, including OpenSpec/PRD/SDD/phase/start-readiness documents: proactively run `requirements-clarification-gate` before drafting, status judgment, gate dispatch, QA, or development; output per `Output Standards`.
- Non-trivial implementation backed by a requirement document: code/script/test/config/API/schema/persistence/security/multi-file behavior changes need development handoff evidence defined in `Output Standards`.
- Post-development gates do not auto-trigger after ordinary work. Run the four-gate sequence only when the user asks for formal gates/release/seal/final validation, or when an already-authorized formal run reaches post-development review.
- Formal review, start-readiness judgment, release/seal, four gates, or gate processes: follow the matching route below and never accept chat-only PASS.
- Install, hooks, canary, A/B, or host integration: read `references/install-and-hooks.md` only when that is the task.

## Cost Control

- Default to the lightest route that can answer the current request. Do not load `references/`, `agents/`, scripts, hooks, OpenSpec files, or logs unless that file is needed for the active route.
- Tool output is part of the cost. Prefer file names, line numbers, counts, small excerpts, exit codes, and artifact paths. Do not paste full logs, full scripts, whole repo packs, or long generated artifacts into chat.
- A formal dispatch bundle should be manifest-first: bundle path and SHA, base commit or snapshot, changed-files or raw-diff artifact, requirement target, acceptance criteria, verification artifacts, and forbidden files. Do not feed the whole repository when an allowlisted bundle is enough.
- Final answers and gate summaries must be short: verdict, blocking findings, evidence paths, commands run, and remaining risk. Keep long reasoning and raw evidence in artifacts.

## Blacklist

Never use this skill for casual discussion or tiny edits unless the user asks for formal review. Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests. Never let the main agent implement non-trivial work, contaminate zero-context review prompts, reuse PASS after snapshot change, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, or expand this entrypoint after independent evaluation shows no behavior gain; keep details in references and delete or merge stale wording instead of adding compensating text.

## Must-say Anchors

Use these exact anchors when the matching risk appears; do not hide them in generic wording.

| Trigger | Must say |
|---|---|
| Files changed after a gate PASS | Refresh `changeSnapshot`; old downstream PASS is invalid; choose earliest rerun gate by impact surface; review full requirement and current diff, not only repair patch. |
| Manifest extension gate | Cannot override built-in gate IDs; extension gates require `manifestPath`, manifest-bound prerequisite records, and `manifestHash`; old non-manifest records cannot admit extension gates. |
| Hook config without live payload | Config is not proof; require same-host live canary with `PreToolUse` payload and blocked invalid command; if hook closure is unproven, fall back to `gate-workflow.ps1` / `gate-state.ps1` validation. |
| Independent reviewer unavailable | Formal PASS cannot be self-issued; output `Gate Handoff Request` with workflow, snapshot, artifacts, required gates, and forbidden context; do not continue as if gates passed. |

## Load Map

- Requirements/document alignment: read `references/requirements-clarification-gate.md`; read `references/requirement-document-adapters.md` only when mapping OpenSpec, PRD, SDD, issue, design brief, or markdown bundles into common requirement fields; read `references/requirements-clarification-artifacts.md` only for PASS recording or artifact validation.
- Document/start-readiness review: use the section below plus `references/requirements-clarification-gate.md`.
- Post-development/release review: follow the fixed sequence below; read only the active gate reference.
- Active gate details: `references/qa-test-gate.md`, `references/complexity-gate.md`, `references/architecture-health-gate.md`, or `references/code-quality-gate.md`.
- Formal post-development artifact fields and recording commands: read `references/post-development-artifacts.md` only when preparing or validating machine-recorded artifacts.
- Install, hooks, canary, A/B, candidate package testing, Claude/Codex/Cursor integration: cold path; read `references/install-and-hooks.md` only for those tasks.

Claude Code, Codex, and Cursor are separate host targets. Do not rank them as primary versus compatibility in public guidance. Hook enforcement requires same-host live canary proof; config files, installed scripts, direct script tests, or another host's canary are not proof. For Codex, proof requires a real `codex exec` run that writes a `PreToolUse` payload, blocks a bad formal PASS command, and leaves the canary marker uncreated. If hook closure is unproven, explicitly say runtime hook enforcement is not proven and fall back to `gate-workflow.ps1` / `gate-state.ps1` validation.

## Checkpoints

Use these visible stops before moving to the next route. They are checkpoints, not new gates.

| Stop | Do not continue until | If missing |
|---|---|---|
| `CHECKPOINT / STOP: requirements` | User-confirmed alignment is recorded for formal document work. | Output `DRAFT_BLOCKED` or record `SKIPPED_BY_USER` risk only; do not draft or dispatch gates. |
| `CHECKPOINT / STOP: development handoff` | The development handoff evidence required by `Output Standards` is ready. | Output `Gate Handoff Request`; do not let the main agent implement. |
| `CHECKPOINT / STOP: independent review` | The matching `agents/<gate>.md` prompt is used without findings, suspicions, expected answers, or focus items. | Output `PROCESS_VIOLATION` and rebuild the dispatch prompt. |
| `CHECKPOINT / STOP: seal` | The seal evidence required by `Output Standards` is verified. | Say `focused evidence pending full gate`; do not claim Final QA PASS or seal. |

## Red Lines

- Non-trivial development must have requirement-document or slice coverage first; the main agent delegates implementation to zero-context development subagents instead of editing directly. If delegation is unavailable, use the development handoff row in `Output Standards`.
- Development/review subagents receive the exact bundle/manifest, worktree, base commit or non-git snapshot id, requirement-document target or OpenSpec change, task scope, forbidden items, Complexity Contract, and verification requirements. Development return must include Complexity Ledger, changed files, covered requirements, verification artifacts, and budget pressure.
- Formal PASS/FAIL/REVIEW verdicts must come from independent zero-context subagents; main agent cannot self-judge pass. If independent agents are unavailable, output `Gate Handoff Request`.
- Formal review dispatch must use the matching file under `agents/`, must say the reviewer must not load or execute skills including `formal-gates`, and must not include findings, suspicions, expected answers, or focus items; violations are `PROCESS_VIOLATION`.
- Requirement-document proposal/design/spec/tasks/start-readiness "can-develop/can-start/pass" verdicts require independent zero-context complexity, architecture-health, and cold-water reviews.
- If main agent is found directly writing code, skipping independent gates, or self-stamping gate verdicts, immediately stop and output `PROCESS_VIOLATION`.

## Four Fixed Gate IDs

These four post-development gate IDs cannot be renamed: `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, `code-quality-gate`. Scripts, hooks, artifacts, and GateWorkflow must use them exactly.

Document work also has one built-in pre-document gate id: `requirements-clarification-gate`. It is not a fifth post-development gate. It is recorded only when the user's requirement alignment is complete enough to draft; its PASS evidence is user-confirmed alignment, not independent zero-context review.

## Post-development Formal Sequence

Complete release/seal order: QA `Design` -> `Design Review` -> `Design Rework` -> initial `Verification Run` -> QA `Execution` -> `complexity-gate` -> `architecture-health-gate` -> `code-quality-gate` -> final `Verification Run` -> QA `FinalExecution` -> optional QA `White-box Adequacy` -> seal. Load the active gate reference only when its details are needed.

If complexity doesn't pass, cannot enter architecture gate. If architecture doesn't pass, cannot enter code quality gate. Don't use "code is fine" to hide scope bloat, and don't use "architecture is more complete" to hide over-engineering.

Feature Developer self-test is not a gate stage. `Execution` is QA execution before downstream gates, `FinalExecution` is QA execution after final verification; these two stages cannot be mixed.

Any implementation change invalidates downstream PASS from old snapshots. Use the `Must-say Anchors` rerun line, then follow detailed rerun-scope rules in `references/post-development-artifacts.md`; machine seal still requires gate-state-compatible records.

When user has authorized formal run, proceed continuously following the above sequence. Only stop for genuine blockers, gate failures, missing machine metadata, budget expansion, snapshot changes, destructive/shared-state actions without authorization, or unclear requirements.

## Requirement Document Start-readiness Review

For requirement-document work, first read `references/requirements-clarification-gate.md`. Use `references/requirement-document-adapters.md` when a format-specific mapping is needed. Document review uses four checks: requirements clarification, architecture shape, complexity/scope, and cold-water start-readiness. The user-confirmed requirement source remains authoritative; plan/tasks/Contract may decompose delivery but must not narrow user requirements without approval.

`READY_FOR_ZERO_CONTEXT_REVIEW` is not development approval; use `Output Standards` for required start-readiness evidence. If unauthorized narrowing is found, output `REQUIREMENTS_SCOPE_MISMATCH`.

## GateWorkflow Minimum Information

Formal processes need structured `GateWorkflow`. Minimum fields:

- `workflowId`
- `changeSnapshot`
- `worktree` or `statePath`
- Current `gate`
- Current `stage` for QA gate or manifest-extended gates

`GateWorkflow.gate` must be `requirements-clarification-gate`, one of the four fixed post-development gate IDs, or an extended gate defined in manifest. Free-text `WorkflowId=... ChangeSnapshot=...` is only a hint, not a formal record.

Without minimum workflow information, cannot record formal PASS; only advisory review is allowed. Explicit `singleGateAuthorized=true` is also advisory and cannot progress release/seal.

Requirements clarification PASS is machine-recorded with `gate-workflow.ps1 record-stage -Gate requirements-clarification-gate`; it does not require independent reviewer fields. Field templates and validator details live in `references/requirements-clarification-artifacts.md`.

Post-development review PASS must come from an independent zero-context artifact and be machine-recorded with `gate-workflow.ps1 record-stage` using GateWorkflow metadata and the matching artifact binding. Complete post-development field templates, contamination blockers, `gate_route`, PowerShell prefixes, and recording commands live in `references/post-development-artifacts.md`; manifest and hook wording must use the `Must-say Anchors`. Manifest extension, host hooks, canary, and dual-installation rules live in `references/install-and-hooks.md`.

## Gate Handoff Request

When current agent cannot dispatch independent review agents, handoff using this template. Do not abbreviate it: every handoff must enumerate workflow, snapshot, worktree/base, requirement target, artifacts, required gates, forbidden context, and continue conditions; development handoff must also include verification requirements.

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

`Required independent gates` must list which gates need review. `Forbidden context` must exclude main-agent conclusions, suspicions, previous findings, expected answers, and focus items. Continue only after receiving the independent artifact.

## Zero-context Is Not Empty-context

Subagent dispatch must include bundle/manifest path and SHA, worktree and base commit or snapshot id, requirement target, scope, forbidden files/items/context, Complexity Contract or Ledger as applicable, related artifacts, output template, and required verifications.

Formal review dispatch must use the matching file under `agents/`. If a review agent receives anchored dispatch, it must stop with `PROCESS_VIOLATION: main agent contaminated zero-context review`; it must not continue review or output PASS/FAIL/REVIEW.

## Output Standards

- Without complete gate evidence: Can only say `focused evidence pending full gate`.
- Without independent gate artifact: Can only say `blocked` or `CONDITIONAL_PASS`, cannot formal PASS.
- If requirements were narrowed without authorization: Output `REQUIREMENTS_SCOPE_MISMATCH`.
- If process was contaminated: Output `PROCESS_VIOLATION`.
- Hooks or gate-state only prove sequence and recording, not code quality; quality verdict still requires independent gate artifact.

Formal responses must include the hard evidence, not only the refusal:

| Request type | Required output |
|---|---|
| Requirement-document start | Classify the mode; run `requirements-clarification-gate`; create or request a requirement-alignment table; stop before drafting until alignment is confirmed or explicitly blocked. |
| Development handoff | Refuse direct edits; require requirement coverage, bundle or snapshot, base commit, Complexity Contract, forbidden items, verification requirements, and zero-context delegation or `Gate Handoff Request`. |
| Self-issued PASS or seal | Reject self-PASS; require independent zero-context artifacts, GateWorkflow metadata, final verification / FinalExecution evidence, and unchanged snapshot. |
| Start-readiness approval | Prepare or request a zero-context review bundle; require independent complexity, architecture-health, and cold-water conclusions before development. |
| Skip process / force seal | Refuse bypass; require requirement-document or slice coverage, zero-context development handoff, independent gate artifacts, and final verification before seal. |

Development handoff must not be placeholder-only: first run light fact collection for worktree, git base commit or snapshot id, changeSnapshot, and existing bundle/manifest path plus hash; fill those facts, mark only genuinely missing requirement facts as `BLOCKING_MISSING:<field> - how to obtain`, and include a requirement-coverage table plus a Complexity Contract block even when blocked.
