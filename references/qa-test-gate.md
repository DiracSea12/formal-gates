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
- `Design Review`: before verification, review every candidate case as `ACCEPT / REWORK / DROP / SPLIT / MERGE`. Block only when a case changes the target claim, cannot be executed, lacks an oracle, or lacks evidence binding. Wording polish, style, formatting, or non-execution-affecting phrasing is nonblocking. If rework is needed, route to `Design Rework`; do not stop at the first rejected case or wait for the user unless the claim itself is unclear.
- `Design Rework`: edit cases and oracle only. Do not run tests or change implementation. After three failed rework loops, stop and split, merge, delete, or redefine the claim.
- `Execution`: an independent QA executor runs approved cases and binds them to commands, artifacts, manual observation, or acceptance procedures. QA-owned results and complete case binding are mandatory. Failed or incomplete evidence routes to implementation, test evidence, or case rework; it does not enter downstream gates. The main agent and CLI check the evidence mechanically and do not dispatch another QA reviewer.
- `FinalExecution`: after downstream gates and final verification, bind final release/seal evidence to the unchanged snapshot before release/seal. When the four post-development gates already recorded PASS for the same workflow and unchanged snapshot, the main agent may record this as a mechanical closeout: check the existing PASS records, final verification artifact, and route to seal. Do not add QA judgment, replace missing gates, reuse stale snapshots, or claim independent review.
- `White-box Adequacy`: after the deliverable shape and code-quality result are stable, review internal risk coverage when needed.

Authorized formal runs should continue across normal stage transitions. Stop only for true blockers: unapproved cases, missing QA-owned evidence, failed verification, stale workflow/snapshot, scope change, destructive/shared-state action not authorized, or unclear requirement.

`Design`, `Design Review`, `Design Rework`, and `White-box Adequacy` produce QA artifacts and review records. They do not satisfy downstream machine admission unless a workflow manifest explicitly defines them as extension-gate prerequisites. Built-in machine admission uses formal `Execution` before downstream gates and formal `FinalExecution` before seal.

## Workflow Artifacts

Preserve separate artifacts for:

- approved QA cases
- developer self-test
- initial QA verification
- final QA verification
- each formal gate verdict

If the snapshot changes after a PASS, the old PASS is stale. Do not reuse it. GateWorkflow and worktree rules live in `SKILL.md`; recording commands and machine fields live in `references/post-development-artifacts.md`.

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

- Approved case set.
- QA-owned verification evidence.
- Binding from cases to artifacts/procedures/results.
- QA Execution performed independently from the feature developer.
- Main-agent and CLI validation of exact hashes, workflow, snapshot, case coverage, PASS results, and case-result binding; no Execution reviewer receipt is required.
- Machine-recorded PASS using `formal-gates workflow record-stage`.

Record formal Execution PASS with `references/post-development-artifacts.md`, using `formal-gates workflow record-stage --gate qa-test-gate --mode formal --stage Execution`. Generate FinalExecution with `formal-gates workflow final-verification --record-final-qa --final-qa-artifact <output>` after all four current-snapshot closures exist.

## Output

The QA executor writes QA-owned results and case-result binding, not the gate artifact. The main agent writes direct schema-version-2 `QA_EXECUTION` JSON using policy `qa.execution.v2`. Its payload contains exactly `approvedCaseSet`, `qaOwnedResults`, `caseResultBinding`, `changedFiles`, and `verification` evidence references. `QA_EXECUTION` accepts only PASS and has no reviewer dispatch, context bundle, checks, findings, or receipt. Design remains a case document; Design Review and White-box Adequacy are not Phase 1 machine roles.
