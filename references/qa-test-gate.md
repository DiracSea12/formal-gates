# QA Test Gate

Use for QA test gate, test plan review, acceptance criteria/testability review, release validation, test adequacy review, PR validation, test-only changes, and spec/design/document testability review.

This gate asks whether the deliverable can be trusted by tests. It does not replace developer self-test, and it is not a test framework.

## Activation

Run when any are true:

- Public API, CLI, config schema, serialized contract, persistence, migration, security, permission, privacy, safety, external dependency behavior, or user-visible acceptance changes.
- Release/PR validation, P0/P1 bugfix, or 3+ file behavior change.
- Test harness, fixture, runner, evidence flow, or validation architecture changes.
- The user or reviewer asks for QA gate, test plan review, acceptance evidence review, or test adequacy review.

Skip for pure formatting, comments, typo fixes, conversation-only analysis, or a single-file low-risk bugfix with targeted existing coverage and no new acceptance claim.

## Modes

- `formal`: Case Designer, QA Reviewer, and Feature Developer are different agent/thread/person. Only formal mode can PASS.
- `solo`: Same agent self-stages the work. Maximum verdict is `CONDITIONAL_PASS`.
- `advisory`: Missing independence or evidence. Maximum verdict is `REVIEW`.

## Stages

- `Design`: read only requirements, specs, public contracts, user flows, or bug reports. Produce cases and oracles. Do not inspect implementation diff to invent cases.
- `Design Review`: before verification, review candidate cases as `ACCEPT / REWORK / DROP / SPLIT / MERGE`. If rework is needed, route to `Design Rework`; do not stop and wait for the user unless the claim itself is unclear.
- `Design Rework`: edit cases and oracle only. Do not run tests or change implementation. After three failed rework loops, stop and split, merge, delete, or redefine the claim.
- `Execution`: bind approved cases to commands, artifacts, manual observation, review records, or acceptance procedures. QA-owned verification evidence is mandatory. `REVIEW` / `FAIL` / `BLOCKED` routes to implementation, test evidence, or case rework; it does not enter downstream gates.
- `FinalExecution`: after downstream gates and final verification, bind final QA evidence to the unchanged snapshot before release/seal. Do not reuse the earlier `Execution` artifact as final QA.
- `White-box Adequacy`: after the deliverable shape and code-quality result are stable, review internal risk coverage when needed.

Authorized formal runs should continue across normal stage transitions. Stop only for true blockers: unapproved cases, missing QA-owned evidence, failed verification, stale workflow/snapshot, scope change, destructive/shared-state action not authorized, or unclear requirement.

`Design`, `Design Review`, `Design Rework`, and `White-box Adequacy` produce QA artifacts and review records. They do not satisfy downstream machine admission unless a workflow manifest explicitly defines them as extension-gate prerequisites. Built-in machine admission uses formal `Execution` before downstream gates and formal `FinalExecution` before seal.

## Workflow Artifacts

A formal run needs one `workflowId` and one current `changeSnapshot`. Preserve separate artifacts for:

- approved QA cases
- developer self-test
- initial QA verification
- final QA verification
- each formal gate verdict

If the snapshot changes after a PASS, the old PASS is stale. Do not reuse it. Use structured `GateWorkflow={...}` metadata and the router machine checks; free-text `WorkflowId=... ChangeSnapshot=...` is advisory only.

Development/rework handoffs must also preserve the snapshot's worktree reality. For dirty or uncommitted formal-gate rework, dispatch development subagents to the current worktree named by `GateWorkflow.worktree` unless an isolated worktree has been explicitly prepared from the exact intended base and loaded with the same required snapshot/artifacts. A fresh worktree that silently starts from `origin/master`, `origin/main`, or a default remote ref is not valid formal-gate context. Every development subagent must verify `git rev-parse --short HEAD` against the intended base before editing and must stop on mismatch.

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

Black-box design can use public API/interface contracts, but not private implementation details, diffs, developer explanations, or main-agent expected answers. Design Review must happen before Verification Run; unreviewed cases are not cases, they are decoration.

## Evidence Rules

Developer self-test is not QA verification. QA may use similar commands, but the run must be QA-owned or QA-supervised and bound to approved cases.

Mock, bypass, headless, fake provider, or exploratory evidence can support diagnosis, but cannot close user-visible final acceptance when real behavior evidence is required.

If final acceptance needs real runtime behavior and the run is not real, keep it as a gap. Do not perfume a gap into PASS.

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
- Independent zero-context QA reviewer artifact.
- Machine-recorded PASS using `gate-workflow.ps1 record-stage`.

For formal Execution PASS:

```powershell
pwsh <formal-gates>/scripts/gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate qa-test-gate -Verdict PASS -Mode formal -Stage Execution -Artifact <qa-artifact> -Actor <qa-reviewer> -WorkflowId <id> -ChangeSnapshot <snapshot>
```

For formal FinalExecution PASS:

```powershell
$attempts = '[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-verification-run.json","reviewerAgentId":"qa-final-agent","contextBundle":".claude/bundles/<bundle>.zip sha256=<bundle-sha256>"}]'
pwsh <formal-gates>/scripts/gate-workflow.ps1 -Action record-final-verification -Worktree <repo> -WorkflowId <id> -ChangeSnapshot <snapshot> -AttemptsJson $attempts -OutputArtifact .claude/gates/artifacts/final-verification.json -FinalQaArtifact .claude/gates/artifacts/final-qa-execution.md -RecordFinalQa -Actor <qa-reviewer>
```

`AttemptsJson` entries need `status`, `accepted`, `artifact`, `reviewerAgentId`, and `contextBundle`. `contextBundle` must include `sha256=<bundle-sha256>`. A PASS attempt is accepted only when `status=PASS`, `accepted=true`, the artifact path exists and is non-empty, and any `gate_route` inside it matches the same workflow/snapshot. With `-RecordFinalQa`, the wrapper writes the final verification aggregate and records `qa-test-gate` `Stage=FinalExecution`; plain `record-stage FinalExecution` is only a manual fallback when an equivalent aggregate already exists.

## Output

```text
QA Test Gate
Verdict: PASS / CONDITIONAL_PASS / REVIEW / FAIL / BLOCKED
Mode: formal / solo / advisory
Stage: Design / Design Review / Design Rework / Execution / FinalExecution / White-box Adequacy
Work type:
Findings:
Case review summary:
Evidence:
Approved case set:
QA-owned evidence:
Case-to-artifact binding:
Required rework:
Release judgment:
gate_route:
```

`PASS` is formal only. `CONDITIONAL_PASS`, `REVIEW`, `FAIL`, and `BLOCKED` are hard blockers in formal seal/release flow.
