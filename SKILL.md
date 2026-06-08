---
name: formal-gates
description: Use when the main orchestrating agent is explicitly asked to run, package, install, test, or diagnose the formal-gates workflow, four-gate sequence, requirement clarification gate, release/seal validation, GateWorkflow hooks, host canaries, or formal-gates A/B testing. Do not use for ordinary code review, zero-context review subagent tasks, reading OpenSpec files, implementation, debugging, brainstorming, wording edits, or casual requirement discussion unless this agent is being asked to orchestrate formal-gates.
---

# Formal Gates

This is the entry point for the formal-gates process. It handles routing, red lines, and handoff only. Load one detail file on demand; do not read the whole package during normal gate work.

## Fast Route

- Ordinary chat, brainstorming, explanations, wording edits, typo fixes, or small low-risk changes: do not activate formal gates.
- Requirement-document work, including OpenSpec/PRD/SDD/phase/start-readiness documents: proactively run `requirements-clarification-gate` before drafting, status judgment, gate dispatch, QA, or development.
- Non-trivial implementation backed by a requirement document: code/script/test/config/API/schema/persistence/security/multi-file behavior changes need requirement-document or slice coverage, bundle/snapshot, base commit, Complexity Contract, forbidden items, and verification requirements before development handoff.
- Post-development gates do not auto-trigger after ordinary work. Run the four-gate sequence only when the user asks for formal gates/release/seal/final validation, or when an already-authorized formal run reaches post-development review.
- Formal review, start-readiness judgment, release/seal, four gates, or gate processes: follow the matching route below and never accept chat-only PASS.
- Install, hooks, canary, A/B, or host integration: read `references/install-and-hooks.md` only when that is the task.

## Blacklist

Never use this skill for casual discussion or tiny edits unless the user asks for formal review. Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests. Never let the main agent implement non-trivial work, contaminate zero-context review prompts, reuse PASS after snapshot change, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, or expand this entrypoint after independent evaluation shows no behavior gain.

## Load Map

- Requirements/document alignment: read `references/requirements-clarification-gate.md`; read `references/requirement-document-adapters.md` only when mapping OpenSpec, PRD, SDD, issue, design brief, or markdown bundles into common requirement fields; read `references/requirements-clarification-artifacts.md` only for PASS recording or artifact validation.
- Document/start-readiness review: use the section below plus `references/requirements-clarification-gate.md`.
- Post-development/release review: follow the fixed sequence below; read only the active gate reference.
- Active gate details: `references/qa-test-gate.md`, `references/complexity-gate.md`, `references/architecture-health-gate.md`, or `references/code-quality-gate.md`.
- Formal post-development artifact fields and recording commands: read `references/post-development-artifacts.md` only when preparing or validating machine-recorded artifacts.
- Install, hooks, canary, A/B, candidate package testing, Claude/Codex/Cursor integration: cold path; read `references/install-and-hooks.md` only for those tasks.

Claude Code, Codex, and Cursor are separate host targets. Do not rank them as primary versus compatibility in public guidance. Any hook enforcement claim must be proven with a live canary on the specific host, and a passing canary on one host does not prove another host. Config files, installed scripts, or direct script tests are not hook enforcement proof. For Codex, proof requires a real `codex exec` run that writes a `PreToolUse` payload, blocks a bad formal PASS command, and leaves the canary marker uncreated.

## Checkpoints

Use these visible stops before moving to the next route. They are checkpoints, not new gates.

| Stop | Do not continue until | If missing |
|---|---|---|
| `CHECKPOINT / STOP: requirements` | User-confirmed alignment is recorded for formal document work. | Output `DRAFT_BLOCKED` or record `SKIPPED_BY_USER` risk only; do not draft or dispatch gates. |
| `CHECKPOINT / STOP: development handoff` | Requirement-document or slice coverage, bundle/snapshot, base commit, Complexity Contract, forbidden items, and verification requirements are ready. | Output `Gate Handoff Request`; do not let the main agent implement. |
| `CHECKPOINT / STOP: independent review` | The matching `agents/<gate>.md` prompt is used without findings, suspicions, expected answers, or focus items. | Output `PROCESS_VIOLATION` and rebuild the dispatch prompt. |
| `CHECKPOINT / STOP: seal` | Final verification, FinalExecution QA evidence, all required independent gate artifacts, and unchanged snapshot are verified. | Say `focused evidence pending full gate`; do not claim Final QA PASS or seal. |

## Red Lines

- Non-trivial development must have requirement-document or slice coverage first; the main agent delegates implementation to zero-context development subagents instead of editing directly.
- Development/review subagents receive the exact bundle/manifest, worktree, base commit or non-git snapshot id, requirement-document target or OpenSpec change, task scope, forbidden items, Complexity Contract, and verification requirements. Development return must include Complexity Ledger, changed files, covered requirements, verification artifacts, and budget pressure.
- Subagents must verify their starting snapshot: git reports `git rev-parse --short HEAD`; SVN or non-git reports the provided `changeSnapshot`. Dirty formal snapshots use the current worktree named by `GateWorkflow.worktree`; do not silently start from `origin/main`, `origin/master`, or a guessed remote base.
- Formal PASS/FAIL/REVIEW verdicts must come from independent zero-context subagents; main agent cannot self-judge pass.
- Formal review dispatch must use the matching file under `agents/`. Any self-written formal review prompt or anchored prompt is `PROCESS_VIOLATION`.
- Formal review dispatch must explicitly say the reviewer must not load, invoke, or execute any skills, including `formal-gates`. 中文：派审查子代理时必须写明“不要加载、调用或执行任何技能，包括 formal-gates”。
- Main agent must verify that independent gate artifact opinions are evidence-backed; main agent has veto power, not self-approval power. When hard factual conflicts exist with independent gate conclusions, record evidence and re-dispatch independent gate agent for review—cannot self-override to formal verdict.
- For requirement-document proposal/design/spec/tasks/start-readiness "can-develop/can-start/pass" verdicts, must first have independent zero-context complexity review, architecture-health review, and cold-water review.
- Independent gates require external orchestration. If current agent cannot dispatch independent subagents, cannot forge gate PASS, and should not treat entire requirement as failed implementation. Output `Gate Handoff Request` and hand off to main agent or external orchestrator to dispatch independent gate agent.
- If main agent is found directly writing code, skipping independent gates, or self-stamping gate verdicts, immediately stop and output `PROCESS_VIOLATION`.

## Four Fixed Gate IDs

These four post-development gate IDs cannot be renamed: `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, `code-quality-gate`. Scripts, hooks, artifacts, and GateWorkflow must use them exactly.

Document work also has one built-in pre-document gate id: `requirements-clarification-gate`. It is not a fifth post-development gate. It is recorded only when the user's requirement alignment is complete enough to draft; its PASS evidence is user-confirmed alignment, not independent zero-context review.

## Post-development Formal Sequence

The formal release/seal sequence must be complete—it's not "seal after running four gates":

1. `qa-test-gate` Stage=`Design`: Design test cases and oracles. Read `references/qa-test-gate.md` when QA details needed.
2. `qa-test-gate` Stage=`Design Review`: Review cases, decide `ACCEPT / REWORK / DROP / SPLIT / MERGE`.
3. `qa-test-gate` Stage=`Design Rework`: Only modify cases and oracles until executable.
4. Initial `Verification Run`: QA-owned or QA-supervised verification, not faking with developer self-tests.
5. `qa-test-gate` Stage=`Execution`: Bind test results and artifacts to reviewed cases.
6. `complexity-gate`: Check for bloat, whether net additions are reasonable, signs of new system smell. Read `references/complexity-gate.md` for budget and diff rules.
7. `architecture-health-gate`: Check module boundaries, ownership, public surface, dependency directions, state/cache lifecycles. Read `references/architecture-health-gate.md` for architecture details.
8. `code-quality-gate`: Finally check correctness, edge cases, test quality, dead code, overfitting, maintainability. Read `references/code-quality-gate.md` for code quality details.
9. Final `Verification Run`: Re-run necessary verification on final diff/snapshot, aggregate accepted attempt artifacts for `gate-workflow.ps1 -Action record-final-verification`.
10. `qa-test-gate` Stage=`FinalExecution`: Preferably generated and recorded by `record-final-verification -RecordFinalQa`, don't fake final verification aggregation with manual `record-stage FinalExecution`.
11. `qa-test-gate` Stage=`White-box Adequacy`: When needed, supplement internal risk coverage.
12. Final seal decision: Can only be made when all above evidence is complete and snapshot hasn't changed.

If complexity doesn't pass, cannot enter architecture gate. If architecture doesn't pass, cannot enter code quality gate. Don't use "code is fine" to hide scope bloat, and don't use "architecture is more complete" to hide over-engineering.

Feature Developer self-test is not a gate stage. `Execution` is QA execution before downstream gates, `FinalExecution` is QA execution after final verification; these two stages cannot be mixed.

Any implementation change invalidates downstream PASS from old snapshots. After modifying code, scripts, tests, configs, OpenSpec, or gate artifacts, must refresh `changeSnapshot` and re-run from the `rerun_from` specified by the blocking gate—cannot reuse old PASS.

When user has authorized formal run, proceed continuously following the above sequence. Only stop for genuine blockers, gate failures, missing machine metadata, budget expansion, snapshot changes, destructive/shared-state actions without authorization, or unclear requirements.

## Requirement Document Start-readiness Review

For requirement-document work, first read `references/requirements-clarification-gate.md`. Use `references/requirement-document-adapters.md` when a format-specific mapping is needed. Document review uses four checks: requirements clarification, architecture shape, complexity/scope, and cold-water start-readiness. The user-confirmed requirement source remains authoritative; plan/tasks/Contract may decompose delivery but must not narrow user requirements without approval.

`READY_FOR_ZERO_CONTEXT_REVIEW` is not development approval. Formal requirement-document/start-readiness approval still requires independent zero-context complexity, architecture-health, and cold-water reviews. If unauthorized narrowing is found, output `REQUIREMENTS_SCOPE_MISMATCH`.

## GateWorkflow Minimum Information

Formal processes need structured `GateWorkflow`. Minimum fields:

- `workflowId`
- `changeSnapshot`
- `worktree` or `statePath`
- Current `gate`
- Current `stage` for QA gate or manifest-extended gates

`GateWorkflow.gate` must be `requirements-clarification-gate`, one of the four fixed post-development gate IDs, or an extended gate defined in manifest. Free-text `WorkflowId=... ChangeSnapshot=...` is only a hint, not a formal record.

Without minimum workflow information, cannot record formal PASS—only advisory review. Explicit `singleGateAuthorized=true` is also advisory; it cannot be recorded, reused, or used to progress release/seal.

Requirements clarification PASS is machine-recorded with `gate-workflow.ps1 record-stage -Gate requirements-clarification-gate`; it does not require independent reviewer fields. Field templates and validator details live in `references/requirements-clarification-artifacts.md`.

Post-development review PASS must come from an independent zero-context artifact and be machine-recorded with `gate-workflow.ps1 record-stage`. Complete post-development field templates, contamination blockers, `gate_route`, PowerShell prefixes, and recording commands live in `references/post-development-artifacts.md`. Manifest extension, host hooks, canary, and dual-installation rules live in `references/install-and-hooks.md`.

## Gate Handoff Request

When current agent cannot dispatch independent review agents, handoff using this template:

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
Forbidden context:
Continue after:
```

`Required independent gates` must list which gates need review. `Forbidden context` must exclude main-agent conclusions, suspicions, previous findings, and expected answers. Continue only after receiving the independent artifact.

## Zero-context Is Not Empty-context

When dispatching subagents, must provide sufficient local facts:

- bundle/manifest path and SHA;
- worktree and base commit or non-git snapshot id;
- Requirement document target or OpenSpec change, task scope, forbidden files, forbidden expansion items;
- Complexity Contract for development handoff, and required Complexity Ledger for development return;
- Related spec/design/tasks/case/diff/evidence artifacts;
- Output template and required verifications.

Formal review dispatch must use the matching file under `agents/`: `qa-test-gate.md`, `complexity-gate.md`, `architecture-health-gate.md`, `code-quality-gate.md`, or `cold-water-review.md`. If a review agent receives anchored dispatch, it must stop with `PROCESS_VIOLATION: main agent contaminated zero-context review`; it must not continue review or output PASS/FAIL/REVIEW.

Review isolation / 审查隔离: every formal review dispatch prompt must state that the reviewer is an independent reviewer, not the formal-gates orchestrator, and must not load, invoke, or execute any skills. 审查者只读派工材料、bundle 和允许的仓库文件；不能把 formal-gates 流程知识当作审查依据。

## Output Standards

- Without complete gate evidence: Can only say `focused evidence pending full gate`.
- Without independent gate artifact: Can only say `blocked` or `CONDITIONAL_PASS`, cannot formal PASS.
- If requirements were narrowed without authorization: Output `REQUIREMENTS_SCOPE_MISMATCH`.
- If process was contaminated: Output `PROCESS_VIOLATION`.
- Hooks or gate-state only prove sequence and recording, not code quality; quality verdict still requires independent gate artifact.
