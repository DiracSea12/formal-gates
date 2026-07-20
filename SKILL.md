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
| User explicitly asks for formal review, the four gates, final validation, release, or seal | Run the four fixed post-development gate IDs against the target snapshot; they may execute independently and finalization still requires all four. Never accept chat-only PASS. |
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
| Scope, over-engineering, post-development complexity review, development-time budget, or Complexity Contract | `references/complexity-gate.md` | Run the independent review on the current target snapshot; do not carry development-time numeric budgets into post-development gate evidence. |
| Module boundaries, ownership, dependencies, lifecycle, failure semantics | `references/architecture-health-gate.md` | Run the independent review on the current target snapshot; finalization aggregates all four gates. |
| Correctness, maintainability, tests, dead code, overfitting, residual risk | `references/code-quality-gate.md` | Run the independent review on the current target snapshot; it has no post-development gate prerequisite. |
| Post-development artifact fields and recording commands | `references/post-development-artifacts.md` | Use the generated artifacts/composers and native commands when recording or verifying workflow state. |
| Install, hooks, canaries, manifests, host support | `references/install-and-hooks.md` | Run native install, preflight, or same-host canary checks; config alone is not proof. |

## Fixed Gate IDs

Post-development gate IDs cannot be renamed:

- `qa-test-gate`
- `complexity-gate`
- `architecture-health-gate`
- `code-quality-gate`

The pre-document gate is `requirements-clarification-gate`. It is not a fifth post-development gate and does not belong to the four gates.

## Authorized Formal Flow Order

Use these flow shapes only after the router activates the matching formal flow. Project hook-enforced document gates count only with explicit opt-in and same-host live canary proof.

| Flow | Order |
|---|---|
| Optional document/start-readiness review | requirements clarification with user-confirmed alignment evidence -> parallel `complexity-gate`, `architecture-health-gate`, and cold-water start-readiness checks -> readiness aggregation. Independent zero-context conclusions are required before calling a formal readiness review passed. |
| Pre-development test design | For `four-gate`, `release`, or `seal`: receipt-bound QA `Design` -> receipt-bound independent `Design Review` -> semantic rework through a new Design registration/submission when needed. This QA lane may run alongside isolated candidate development from the same frozen requirement; Design Rework is not a machine role. |
| Candidate development lane | After the requirement is frozen, candidate development may start before QA case approval. The candidate developer reads no QA draft, Design Review result, or repair record. QA Design, Design Review, and case editing read no production implementation, diff, existing tests, developer self-tests, implementation notes, or developer explanations. This mutual blindness preserves independent case design, and candidate code cannot claim formal acceptance. |
| Formal development handoff | After the approved case chain exists, validate the handoff and dispatch `agents/development-worker.md`; the worker may adopt, modify, or delete the candidate code and must produce current-snapshot verification. Development-time complexity budget checks are automatic inside the handoff. |
| Post-development release/seal | initial `Verification Run` -> independent QA `Execution` and the three post-development reviewers in parallel -> main-agent/CLI mechanical validation -> serialized authoritative state recording -> final `Verification Run` -> `FinalExecution` -> seal. Reviewer work stays parallel, but only the orchestrator records results; gate-state mutation also uses a cross-process lock so concurrent normal invocations cannot lose an update. The post-development `complexity-gate` is not a development-time budget gate: use statistics-only diff evidence and judge scope shape, new concepts, public/config surface, reuse/deletion, and minimum sufficient implementation. Every post-development result must belong to the same `workflowId` and target `changeSnapshot`; the accepted Design Review closure may retain its pre-development snapshot only for the same workflow and exact case reference. After all four post-development gates have target-bound PASS results for that workflow and snapshot, each either fresh or admitted by an accepted per-gate Carry decision, `FinalExecution` may be a main-agent mechanical closeout that only checks existing records and final verification evidence. It must not add QA judgment, replace missing gates, reuse stale snapshots, or claim independent review. White-box Adequacy is not registered. |
| Rerun after implementation change | Preserve the exact pre-repair source, then refresh `changeSnapshot`. Dispatch a fresh Carry Arbiter on the cumulative diff produced by that repair, excluding unrelated local worktree changes. If the source was not preserved well enough to reproduce that exact diff, do not propose Carry; rerun affected gates. The Arbiter decides each eligible prior PASS gate independently as carry, rerun, or blocked; the main agent cannot approve, aggregate, or override those decisions. Record the terminal decision set, rerun only named gates, and let finalization consume only target-bound fresh or accepted-carried rows. Review the full requirement and current diff, not only the repair patch. |

Post-development QA, complexity, architecture, and code-quality results are independent judgments on the same target snapshot. Finalization still requires all four target-bound PASS results; without that complete evidence, say `focused evidence pending full gate`.

Before post-development complexity dispatch, generate its statistics with
`complexity check --run-dir ... --workflow-id ... --change-snapshot ...
--output restricted/...`. This formal mode is budget-free and writes the
complete report plus the existing `complexity-statistics.v1` proof. `--json`
stdout, redirected or handwritten JSON, partial reports, and missing or stale
proofs cannot satisfy `receipt register --complexity-statistics`. Registration,
finalization, receipt validation, and artifact validation recheck the same
report and proof.

## Reviewer Context Boundary

For every independent review, the only substantive context given to the reviewer is the confirmed current requirement, the reviewer's current role, and the current diff or proposed change. Worktree, base revision, output location, and output format may be supplied only as operational routing; they must not carry conclusions, evidence summaries, or review direction. The reviewer reads current requirement and changed repository files directly and runs any checks it needs.

Do not give a reviewer another gate's result, PASS/FAIL state, receipt, closure, gate state, dispatch record, context bundle, manifest, verification summary, test report from another actor, repair/rerun/carry history, main-agent summary, chat history, copied project rules, or copied repository documents. The main agent and CLI verify local flow prerequisites and record evidence outside the reviewer's context; no post-development reviewer consumes another gate's PASS. Do not add material merely because it may be useful, complete, rigorous, or convenient.

Every review-related file written under a workflow run belongs under `.claude/gates/runs/<workflow-id>/restricted/`, without exception. This includes current and old dispatch copies, bundles or manifests, reviewer outputs, QA results, receipts, lifecycle events, closures, reports, logs, statistics, verification records, repair history, Carry material, and summaries. A reviewer may read only the CLI-generated catalog at its assigned output path and must submit judgment values through `formal-gates receipt submit`; it never edits reviewer or Carry JSON directly. QA Design likewise submits only ordered case values through that command and never edits the generated case Markdown. It must not open referenced evidence or any other workflow-run artifact, except that Design Review may read the exact prompt-bound case set.

Prompts must not include main-agent conclusions, suspicions, previous findings, expected answers, target verdicts, focus items, or workflow-run artifacts. The one narrow exception is `qa-test-gate` at `Design Review` under `qa.design-review.v2`: `Current diff or proposed change` may name only the exact receipt-bound QA case-set file in the same active run restricted directory. `Current requirement` remains forbidden from naming run artifacts, and every other run artifact, gate, stage, policy, and prompt field remains forbidden. Formal review dispatch uses the matching file under `agents/` and keeps the reviewer separate from gate orchestration: the reviewer may follow host-required skill instructions, but must not run gate orchestration, record PASS, or let a skill replace direct review of the requirement and diff.

Before every reviewer dispatch, use `formal-gates prompt prepare` with gate/stage, requirement/diff target, worktree, snapshot, output, policy, and context-bundle arguments to generate the exact seven-field message, then use `receipt register --prompt` as the single full pre-dispatch static check and generated-artifact composer. Registration accepts only role-specific evidence paths: QA Design Review takes its case set and Design receipt, post-development complexity takes its statistics report, and Carry takes repeatable source-closure paths from which the CLI derives fixed gates. It never accepts caller-authored check IDs or gate/path bindings. Registration must reject any mismatch in role, seven fields, worktree, snapshot, gate/stage output contract, output path, context-bundle schema/path/hash, policy-specific input placement, unresolved `PENDING`, or pollution. It binds the exact-send prompt and generates the assigned reviewer or Carry artifact with all envelope fields, policy/check IDs, evidence references, routes, paths, hashes, and bindings already populated. Send the registered prompt bytes verbatim and append nothing. The reviewer calls `formal-gates receipt submit` with only ordered semantic status/message/finding/location values; a Carry Arbiter supplies only ordered decision/reason values. QA Design calls the same command with one generated case position and exactly seven ordered semantic values per case; the CLI owns its Case IDs, field labels, separators, and final newline layout. The CLI rejects unknown, duplicate, missing, or invalid semantic slots before atomically writing the assigned artifact, so the AI never writes JSON keys, arrays, types, check IDs, gates, bindings, verdicts, or formal-file formatting. At finalization the CLI verifies the generated projection, mechanically derives the aggregate verdict where applicable, writes the final artifact and receipt, and leaves an invalid submission unfinalized and retryable within the same dispatch. Semantic prompt anchoring remains a reviewer judgment; static prompt structure is checked only by the CLI. QA Design is a non-judgment lifecycle and does not require a reviewer prompt binding. Design Rework uses a new Design registration and semantic submission rather than editing submitted case bytes.

Requirements and QA Execution follow the same scalar-only boundary. `artifact compose-requirements` accepts positioned alignment values, global judgment scalars, and 13 positioned dimension judgments; it generates every RQ/DIM mapping and JSON structure. `artifact compose-qa-owned-evidence` reads Case IDs from `--approved-case-set` and accepts only each case's 1-based position, PASS/FAIL outcome, procedure, observation, and oracle result; it generates Execution IDs, references, results, bindings, and proof. Never create a requirements or QA semantic JSON file or edit a generated artifact. Both commands reject incomplete, duplicate, out-of-range, empty, `PENDING`, or illegal submissions before writing any target or proof.

Carry arbitration follows the same boundary. The Arbiter reviews the cumulative source-to-target diff and decides every eligible prior PASS gate independently. Transition-chain composition receives ordered per-hop snapshot and evidence-path scalars and generates the typed hop objects; receipt registration receives source closure paths and derives their fixed gates. The CLI validates transition hops, old PASS closures, receipts, and other Carry material outside the Arbiter prompt; none of that workflow history is reviewer input. Until CLI tree binding is delivered, the orchestrator must preserve the exact pre-repair source; a missing or unreproducible source makes Carry unavailable rather than permitting a guessed whole-file diff.

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
| Independent reviewer unavailable | Report the missing independent review as blocked; do not self-review, invent PASS, or handwrite a formal handoff. |

Hooks or gate-state only prove recording, not code quality. Complexity, architecture, and code-quality verdicts still require independent reviewer artifacts; QA Execution requires independent QA-owned results and complete case binding.

## CLI-Generated Development Handoff

The formal development handoff must be generated by the CLI. The CLI owns the
field catalog, workflow/snapshot, evidence paths/hashes, approved-case and
Design Review bindings, and complexity-check command shape. The main agent may
provide only semantic scope, verification expectations, numeric budget choices,
stop conditions, isolation boundaries, and trigger source; it must not handwrite the static
handoff envelope or bindings.

```bash
formal-gates handoff compose --root <repo> --run-dir <run-dir> \
  --workflow-id <id> --change-snapshot <snapshot> \
  --output <restricted/handoff.md> \
  --requirement-target <requirement-or-openspec-target> \
  --verification-requirements <semantic-verification-requirements> \
  --budget-stop-triggers <semantic-stop-conditions> \
  --budget-expansion-approval-path agents/anti-complexity-review.md \
  --forbidden-context <semantic-isolation-boundary> \
  --formal-flow-mode <none|four-gate|release|seal> \
  --trigger-source <semantic-trigger-source> \
  --task-type <delete-or-consolidate|bugfix|small-feature|refactor|new-system> \
  --max-net <n> --max-new-prod-files <n> --max-prod-insertions <n> \
  [--qa-case-set <restricted/cases.md> \
   --design-review <restricted/design-review-closure.json>]
```

Formal development handoff is optional and user-authorized. When used, supply the semantic inputs for the generated handoff artifact, OpenSpec or slice coverage, and the Complexity Contract. The development-time complexity budget is active during implementation: the worker must run or update the supplied complexity check before continuing after meaningful growth and before returning implementation. If the active budget is exceeded, the worker must stop, shrink, or obtain independent Anti-Complexity Review approval before continuing. For `four-gate`, `release`, or `seal`, `QA case design artifact` and `Approved QA case set` must contain the same CLI-generated `path=<run-relative-path> sha256=<hash>` reference, and `Accepted Design Review closure` must contain its exact generated evidence reference. The CLI revalidates the Design and reviewer receipts and exact case bytes before implementation starts. If no development subagent is available, return this CLI-generated `Gate Handoff Request` instead of implementing locally. Mark only genuinely missing semantic facts as `BLOCKING_MISSING:<field> - how to obtain`; do not reconstruct its static fields by hand.

Changed-files evidence remains CLI-owned after handoff. In working-tree mode,
tracked range/staged/unstaged changes are automatic, while each untracked file
must be explicitly submitted with repeatable `--include-untracked <path>`;
unlisted untracked files are not part of the changed-files output.

`Development-time complexity budget` must include numeric `max-net`, `max-new-prod-files`, and `max-prod-insertions` values matching the `Complexity check command`. The generated command must use the supplied existing complexity task type; unsupported task types are rejected before the handoff or its proof is written. Qualitative scope boundaries are useful constraints but do not count as the numeric budget.
