# QA Test Gate

Use for test plan review, acceptance/testability review, release validation, PR validation, test-only changes, and spec/design/document testability review. It judges whether the deliverable can be trusted by tests; it does not replace developer self-test.

## Activation

Run when the user asks for formal four-gate review, release/seal validation, QA gate, test plan review, acceptance evidence review, or test adequacy review, and any are true:

- Public API, CLI, config schema, serialized contract, persistence, migration, security, permission, privacy, safety, external dependency behavior, or user-visible acceptance changes.
- Release/PR validation, P0/P1 bugfix, or 3+ file behavior change.
- Test harness, fixture, runner, evidence flow, or validation architecture changes.
- The user or reviewer asks for QA gate, test plan review, acceptance evidence review, or test adequacy review.

Do not run only because a public API, behavior, OpenSpec, or document changed. Pre-implementation QA case design is mandatory only for a user-authorized `four-gate`, `release`, or `seal` flow. Skip for pure formatting, comments, typo fixes, conversation-only analysis, user-requested informal/vibe coding, or a single-file low-risk bugfix with targeted existing coverage and no new formal acceptance claim.

## Modes

- `formal`: Design Review is independent from the case designer, and QA Execution is independent from the feature developer. Only formal mode can PASS.
- `solo`: Same agent self-stages the work. Maximum verdict is `CONDITIONAL_PASS`.
- `advisory`: Missing independence or evidence. Maximum verdict is `REVIEW`.

## Stages

- `Design`: read only requirements, specs, public contracts, user flows, or bug reports. Produce cases and oracles. Do not inspect implementation diff to invent cases.
- `Design Review`: before verification, use receipt-bound `QA_REVIEW` and policy `qa.design-review.v2` to review every candidate case as `ACCEPT / REWORK / DROP / SPLIT / MERGE`. Block only when a case changes the target claim, cannot be executed, lacks an oracle, or lacks evidence binding. Wording polish, style, formatting, or non-execution-affecting phrasing is nonblocking. If rework is needed, route to the editing action; do not stop at the first rejected case or wait for the user unless the claim itself is unclear.
- `Design Rework`: edit cases and oracle only. Do not run tests or change implementation. After three failed rework loops, stop and split, merge, delete, or redefine the claim.
- `Execution`: an independent QA executor runs approved cases and binds them to commands, artifacts, manual observation, or acceptance procedures. QA-owned results and complete case binding are mandatory. Failed or incomplete evidence routes to implementation, test evidence, or case rework; it does not enter downstream gates. The main agent and CLI check the evidence mechanically and do not dispatch another QA reviewer.
- `FinalExecution`: after downstream gates and final verification, bind final release/seal evidence to the target snapshot before release/seal. When all four post-development gates have target-bound PASS results for the same workflow and snapshot, each fresh or admitted by an accepted Carry transition, the main agent may record this as a mechanical closeout: check the existing PASS records, final verification artifact, and route to seal. Do not add QA judgment, replace missing gates, reuse stale snapshots, or claim independent review.

Design Rework is not a machine role or recorded stage. White-box Adequacy is not registered by this phase.

Authorized formal runs should continue across normal stage transitions. Stop only for true blockers: unapproved cases, missing QA-owned evidence, failed verification, stale workflow/snapshot, scope change, destructive/shared-state action not authorized, or unclear requirement.

Design produces a receipt-bound case document without a gate PASS. Design Review records its receipt-bound `QA_REVIEW` closure with `--mode pre-development --stage "Design Review"` after same-workflow, same-snapshot requirements PASS. That closure is admission evidence for handoff and later QA Execution, not a current-snapshot post-development gate prerequisite. Built-in post-development admission still uses formal `Execution` before downstream gates and formal `FinalExecution` before seal.

## Workflow Artifacts

Preserve separate artifacts for:

- QA case document and Design-stage receipt
- accepted Design Review closure
- developer self-test
- initial QA verification
- final QA verification
- each formal gate verdict

If a repair changes the snapshot after a PASS, do not reuse the old PASS directly. It may satisfy the new target only when a fresh Carry Arbiter accepts that gate from the repair diff between the pre-repair and post-repair snapshots, excluding unrelated local worktree changes, and the CLI records the target-bound transition before downstream reliance; otherwise rerun it and every required downstream gate. A later repair invalidates the previous Carry decision. GateWorkflow and worktree rules live in `SKILL.md`; recording commands and machine fields live in `references/post-development-artifacts.md`.

## Case Requirements

Every important case needs:

```text
Case ID:
Claim:
Source:
Action:
Oracle:
Failure signal:
Evidence:
Gap:
```

Use the shorter `Case ID / Claim / Action / Oracle / Evidence` only for low-risk work where traceability and failure signal are obvious.

The machine identifier comes only from the documented `Case ID:` field. Markdown headings are descriptive, unrelated headings are ignored, and every approved case ID must be present and unique.

Black-box design can use public API/interface contracts, but not private implementation details, diffs, developer explanations, or main-agent expected answers. Design Review must happen before Verification Run; unreviewed cases are advisory only.

During Execution, continue after a failure through every remaining approved case that is safe and still meaningful, then return all failures together.

## Evidence Rules

Developer self-test is not QA verification. QA may use similar commands, but the run must be QA-owned or QA-supervised and bound to approved cases.

Mock, bypass, headless, fake provider, or exploratory evidence can support diagnosis, but cannot close user-visible final acceptance when real behavior evidence is required.

If final acceptance needs real runtime behavior and the run is not real, keep it as a gap. Do not treat a gap as PASS.

Evidence level must match the claim:

- code behavior: compile/static/unit/integration/runtime/manual evidence as applicable
- executable docs: command/schema/link/example validation
- user-visible behavior: real visible/manual/runtime observation, not a fake provider or headless substitute
- exploratory testing: useful for discovery, insufficient by itself for formal PASS

## Formal PASS

Formal PASS requires:

- Exact case set approved by an independent Design Review closure.
- QA-owned verification evidence.
- Binding from cases to artifacts/procedures/results.
- QA Execution performed independently from the feature developer.
- Main-agent and CLI validation of both Design and reviewer receipts, exact case hash, workflow, case coverage, PASS results, and case-result binding. The pre-development review snapshot may differ from the later implementation snapshot; no Execution reviewer receipt is required.
- Machine-recorded PASS using `formal-gates workflow record-stage`.

Record formal Execution PASS with `references/post-development-artifacts.md`, using `formal-gates workflow record-stage --gate qa-test-gate --mode formal --stage Execution`. Generate FinalExecution with `formal-gates workflow final-verification --record-final-qa --final-qa-artifact <output>` after all four post-development gates have target-bound PASS results (fresh or accepted-carried) and final verification exists.

## Output

The QA executor writes QA-owned results and case-result binding, not the gate artifact. The main agent writes direct schema-version-2 `QA_EXECUTION` JSON using policy `qa.execution.v2`. Its payload contains exactly `approvedCaseSet`, `designReview`, `qaOwnedResults`, `caseResultBinding`, `changedFiles`, and `verification` evidence references. `designReview` points to the accepted closure for the same workflow and exact case-set reference; case, oracle, or Case ID changes require another Design Review. `QA_EXECUTION` accepts only PASS and has no reviewer dispatch, prompt, context bundle, checks, findings, or receipt. QA Design itself is lifecycle-bound but makes no review judgment, so it also does not require a final-send reviewer prompt; Design Review does, through its external receipt.
