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
| Editing OpenSpec, PRD, SDD, phase docs, development plans, handoff docs, or other requirement-like documents | First do lightweight semantic routing. Non-semantic edits ask no questions and create no artifacts. Semantic changes need clarification before formal requirement text is written. |
| User explicitly asks for requirements alignment, pre-development review, or start-readiness review | Use the document/start-readiness route below; build or request an alignment table or equivalent artifact before drafting, and do not approve readiness before user-confirmed alignment evidence exists. |
| User explicitly asks for formal development handoff | Require OpenSpec or slice coverage, Complexity Contract or Ledger, and development handoff evidence; main agent does not implement directly inside that formal workflow. |
| User explicitly asks for formal review, the four gates, final validation, release, or seal | Follow the fixed post-development sequence; never accept chat-only PASS. The four gates are `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, and `code-quality-gate`. |
| Install, hooks, canary, A/B, candidate package testing, or host integration | Read `references/install-and-hooks.md` only for that task. |

## Formal Flow Router

Before any formal handoff or gate dispatch, classify the work as `none`, `four-gate`, `release`, `seal`, or `start-readiness-only`.

- `none`: ordinary small edits, explanations, research, informal/vibe coding, and mere OpenSpec or development intent.
- `start-readiness-only`: explicit user request or project requirement; no QA case design unless four-gate/release/seal is also claimed.
- `four-gate`/`release`/`seal`: QA black-box case design is required before implementation handoff.

Lightweight semantic routing for requirement-like document edits is not a formal flow by itself. It does not record PASS, create gate artifacts, dispatch reviewers, or require start-readiness review. Use it to decide whether the requested edit is non-semantic, low-risk clarification, semantic change, or blocked; read `references/requirements-clarification-gate.md` when the edit can change requirement meaning.

First run rule: classify once, read the matching Load Map entry, collect the required evidence or block on the missing artifact, and record a gate only after evidence exists.

## Blacklist

Never claim formal PASS from chat, self-review, developer self-test, focused tests, gate-state alone, hook config, installed scripts, or direct script tests.

Never invent or add user-unapproved requirements, mechanisms, checks, fields, stages, hooks, or review criteria by calling them optimization, hardening, gap-filling, cleanup, or overengineering prevention. If independent evaluation shows no behavior gain, stop instead of expanding the entrypoint.

A reviewer finding may affect a verdict only when it is caused by the current change and concretely evidenced to violate a confirmed requirement, observable behavior, the reviewing gate's existing responsibilities, or a mandatory rule. Wording, naming, formatting, equivalent-design preferences, purely hypothetical risks, and unrequested hardening are advisory; if only advisory comments remain, the reviewer must PASS. Fix an in-scope defect directly; if the smallest fix would change approved scope, return it for a user decision instead of repairing automatically.

A complete formal result must finish every safe in-scope check owned by that review and report all current blockers in one result; finding the first blocker is not a reason to stop. Stop early only when required context is missing or contaminated, continuing would be unsafe or destructive, or a failed prerequisite makes the remaining checks impossible.

Reviewer findings do not automatically become requirements or repair tasks. The main agent must discard unsupported and advisory findings, merge duplicates, and separate in-scope repairs from findings that need a user decision; the same root cause restated is one finding. For each post-development gate, run at most three completed automatic review-repair cycles in one delivery attempt. `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, and `code-quality-gate` keep separate counts; a cycle consumed by one gate does not reduce another gate's allowance. A cycle starts only when an independent reviewer returns a complete formal result and counts only after the main agent has processed that result, completed the accepted in-scope repairs, and run the required re-verification. The result, its repairs, and that re-verification are one cycle, not separate cycles. Developer self-check fixes, dispatch failures, interrupted runs, and attempts without a complete formal result do not count. After one gate reaches three completed cycles, stop automatic review and repair for that gate, present only deduplicated evidence-backed blockers with attempted fixes and the reason they remain unresolved, and let the user decide whether to change scope or requirements, defer or accept the risk, authorize another round for that gate, or stop delivery.

`receipt register` atomically reserves the remaining standard capacity for the same workflow, gate, and stage. An active open dispatch holds a slot; after its stop event, an unfinalized failed attempt releases dispatch capacity, while `receipt finalize` atomically rechecks the completed-review limit so it still cannot commit a fourth unauthorized result. `--user-authorized-extra-review` may be used only after the user explicitly approves another round; the main agent must never set it on its own.

Never start the four post-development gates, QA case design, or pre-development readiness review unless the Formal Flow Router requires it.

Never let the main agent implement non-trivial work inside a user-authorized formal development handoff, contaminate zero-context prompts, reuse PASS after snapshot change without a fresh target-bound Carry Arbiter decision recorded by the CLI, rename fixed gate IDs, treat `requirements-clarification-gate` as a fifth post-development gate, claim host hook enforcement without same-host live canary, seal without complete final evidence, or expand this entrypoint when details belong in `references/`. Independent reviewer and receipt rules apply only to stages that make a review judgment. QA Execution is performed independently from the feature developer, then the main agent and CLI mechanically validate and record its evidence without a second QA reviewer.

If a formal workflow is represented as passed after direct implementation, skipped independent gates, or self-stamped gate verdicts, stop with `PROCESS_VIOLATION`.

## Load Map

| Need | Read | First action |
|---|---|---|
| Requirements/document alignment | `references/requirements-clarification-gate.md` | Build or request user-confirmed alignment evidence before drafting or approving readiness. |
| Mapping OpenSpec, PRD, SDD, issue, design brief, or markdown bundles | `references/requirement-document-adapters.md` | Map source documents to formal requirement fields before gate review. |
| Requirements PASS recording or artifact validation | `references/requirements-clarification-artifacts.md` | Validate the alignment artifact and decision record before recording PASS. |
| Formal implementation worker dispatch | `agents/development-worker.md` | Validate handoff first; development-time complexity budget checks trigger automatically during formal implementation. |
| Budget expansion request during development | `agents/anti-complexity-review.md` | Run independent anti-complexity review before any larger budget is used. |
| QA case design, Design Review, execution, or final execution | `references/qa-test-gate.md` | Start with receipt-bound QA Design and independent Design Review for pre-handoff formal flows, or QA Execution evidence for post-development review. |
| Scope, over-engineering, post-development complexity review, development-time budget, or Complexity Contract | `references/complexity-gate.md` | Check QA or readiness prerequisites before dispatching complexity review; do not carry development-time numeric budgets into post-development gate evidence. |
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

The pre-document gate is `requirements-clarification-gate`. It is not a fifth post-development gate and does not belong to the four gates.

## Authorized Formal Flow Order

Use these orders only after the router activates the matching formal flow. Project hook-enforced document gates count only with explicit opt-in and same-host live canary proof.

| Flow | Order |
|---|---|
| Optional document/start-readiness review | requirements clarification with user-confirmed alignment evidence -> `complexity-gate` -> `architecture-health-gate` -> cold-water start-readiness. Independent zero-context complexity, architecture-health, and cold-water conclusions are required before calling a formal readiness review passed. |
| Pre-development test design | For `four-gate`, `release`, or `seal`: receipt-bound QA `Design` -> receipt-bound independent `Design Review` -> editing rework when needed -> accepted Design Review closure before implementation handoff. Design Rework is not a machine role. |
| Formal development handoff | Validate handoff -> dispatch `agents/development-worker.md`; development-time complexity budget checks are automatic inside the handoff and do not need a separate user request. Budget expansion routes to `agents/anti-complexity-review.md` before work continues. |
| Post-development release/seal | initial `Verification Run` -> independent QA `Execution` -> main-agent/CLI mechanical QA evidence check and record -> `complexity-gate` -> `architecture-health-gate` -> `code-quality-gate` -> final `Verification Run` -> `FinalExecution` -> seal. The post-development `complexity-gate` is not a development-time budget gate: use statistics-only diff evidence and judge scope shape, new concepts, public/config surface, reuse/deletion, and minimum sufficient implementation. Every post-development prerequisite must belong to the same `workflowId` and target `changeSnapshot`; the accepted Design Review closure may retain its pre-development snapshot only for the same workflow and exact case reference. After all four post-development gates have target-bound PASS results for that workflow and snapshot, each either fresh or admitted by an accepted Carry transition, `FinalExecution` may be a main-agent mechanical closeout that only checks existing records and final verification evidence. It must not add QA judgment, replace missing gates, reuse stale snapshots, or claim independent review. White-box Adequacy is not registered. |
| Rerun after implementation change | Refresh `changeSnapshot`. The main agent may propose a carried prefix but cannot approve it. Before the first fresh downstream gate relies on any old PASS, dispatch a fresh Carry Arbiter on the cumulative diff and let the CLI record only an accepted target-bound transition. If carry is not proposed or accepted, rerun from the earliest required gate and refresh every downstream gate. Review the full requirement and current diff, not only the repair patch. |

Do not enter complexity / architecture / code-quality until QA evidence is complete and preceding gates pass. Without QA final release/seal judgment, say `focused evidence pending full gate`.

## Reviewer Context Boundary

For every independent review, the only substantive context given to the reviewer is the confirmed current requirement, the reviewer's current role, and the current diff or proposed change. Worktree, base revision, output location, and output format may be supplied only as operational routing; they must not carry conclusions, evidence summaries, or review direction. The reviewer reads current requirement and changed repository files directly and runs any checks it needs.

Do not give a reviewer another gate's result, PASS/FAIL state, receipt, closure, gate state, dispatch record, context bundle, manifest, verification summary, test report from another actor, repair/rerun/carry history, main-agent summary, chat history, copied project rules, or copied repository documents. The main agent and CLI verify prerequisites and record evidence outside the reviewer's context. Do not add material merely because it may be useful, complete, rigorous, or convenient.

Every review-related file written under a workflow run belongs under `.claude/gates/runs/<workflow-id>/restricted/`, without exception. This includes current and old dispatch copies, bundles or manifests, reviewer outputs, QA results, receipts, lifecycle events, closures, reports, logs, statistics, verification records, repair history, Carry material, and summaries. A reviewer must not read any workflow-run artifact; it may only write its assigned output there.

Prompts must not include main-agent conclusions, suspicions, previous findings, expected answers, target verdicts, focus items, or any workflow-run artifact. Formal review dispatch uses the matching file under `agents/` and keeps the reviewer separate from gate orchestration: the reviewer may follow host-required skill instructions, but must not run gate orchestration, record PASS, or let a skill replace direct review of the requirement and diff.

Before every reviewer dispatch, use `formal-gates prompt prepare` to generate the exact seven-field message from a generation-only template, then use `receipt register --prompt` as the single full pre-dispatch static check. Registration must reject any mismatch in role, seven fields, worktree, snapshot, gate/stage output contract, output path, context-bundle schema/path/hash, policy-specific input placement, unresolved `PENDING`, or pollution. Only after every check passes does `receipt register` write the machine-generated `static-validation=PASS` binding into the final prompt and bind those exact bytes. The reviewer must confirm that binding in its prompt-field check without reading bound files. Send the registered bytes verbatim and append nothing. Reviewer and Carry JSON contain no dispatch or prompt evidence field. At finalization the CLI writes the dispatch-owned route fields in memory and validates the complete reviewer artifact before locking the receipt; an invalid artifact remains unfinalized and retryable within the same dispatch. QA Design is a non-judgment lifecycle and does not require this binding.

Carry arbitration follows the same boundary. The Arbiter reviews the cumulative source-to-target diff against the proposed carried gates. The CLI validates transition hops, old PASS closures, receipts, and other Carry material outside the Arbiter prompt; none of that workflow history is reviewer input.

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
| Missing required independent reviewer artifact or QA-owned Execution evidence | blocked or `CONDITIONAL_PASS`, not formal PASS |
| Requirements narrowed without authorization | `REQUIREMENTS_SCOPE_MISMATCH` |
| Contaminated process or prompt | `PROCESS_VIOLATION` |
| Independent reviewer unavailable | Output the full `Gate Handoff Request` below. |

Hooks or gate-state only prove sequence and recording, not code quality. Complexity, architecture, and code-quality verdicts still require independent reviewer artifacts; QA Execution requires independent QA-owned results and complete case binding.

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
Accepted Design Review closure:
Continue after:
```

Formal development handoff is optional and user-authorized. When used, collect the template fields above, OpenSpec or slice coverage, and the Complexity Contract. The development-time complexity budget is active during implementation: the worker must run or update the supplied complexity check before continuing after meaningful growth and before returning implementation. If the active budget is exceeded, the worker must stop, shrink, or obtain independent Anti-Complexity Review approval before continuing. For `four-gate`, `release`, or `seal`, `QA case design artifact` and `Approved QA case set` must contain the same `path=<run-relative-path> sha256=<hash>` reference, and `Accepted Design Review closure` must contain its exact evidence reference. The CLI revalidates the Design and reviewer receipts and exact case bytes before implementation starts. If no development subagent is available, output this `Gate Handoff Request` instead of implementing locally. Mark only genuinely missing facts as `BLOCKING_MISSING:<field> - how to obtain`.

`Development-time complexity budget` must include numeric `max-net`, `max-new-prod-files`, and `max-prod-insertions` values matching the `Complexity check command`. Qualitative scope boundaries are useful constraints but do not count as the numeric budget.
