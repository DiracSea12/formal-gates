# Post-development Gate Artifacts

This file is the cold-path schema reference for formal gate artifacts. Use it when recording `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, or `code-quality-gate` PASS. Start-readiness records for complexity and architecture use the same shared fields with `--mode start-readiness`; they do not imply post-development release/seal progress.

Keep the four gate reference files focused on judgment rules. Keep host installation, hooks, and canaries in `references/install-and-hooks.md`.

## Formal Artifact Rule

Without machine metadata, a formal PASS cannot be recorded; the result is at most an advisory review. `singleGateAuthorized=true` is only for explicit single-gate advisory review. It cannot be recorded, reused, or used to advance release/seal.

Do not invent or add user-unapproved requirements, mechanisms, checks, fields, stages, hooks, or review criteria by calling them optimization, hardening, gap-filling, cleanup, or overengineering prevention. Ask the user first and get explicit permission.

Every formal post-development gate artifact must include the fields below, except post-four-gate mechanical `FinalExecution`, which uses the separate closeout shape after this block.

```text
Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/<gate>.md
Zero-context reviewer: YES
Independent agent: YES
Context bundle: <bundle-path> sha256=<bundle-sha256>
Dispatch prompt artifact: <dispatch-prompt-path> sha256=<dispatch-prompt-sha256>
No-anchor prompt: YES
```

Optional strong proof field:

```text
Reviewer proof receipt: <receipt-path> sha256=<receipt-sha256>
```

`Reviewer proof receipt` is a strict hash-bound lifecycle receipt for the artifact when it is present. It supports a host-observed subagent lifecycle claim only when the events were captured by the configured host lifecycle hooks and the same host has live-canary support for those events. Missing receipt means the artifact is not receipt-backed proof that a real subagent ran; it must not be self-stamped as receipt-backed zero-context proof, but missing receipt alone must not block every ordinary gate-state record. If the field is present, it must point to a finalized lifecycle receipt and include its SHA-256 hash; validators check it strictly. Old self-reported reviewer id fields do not count as proof.

`Context bundle` and `Dispatch prompt artifact` must point to existing files and include SHA-256 hashes; the machine validator checks those hashes. `Dispatch prompt artifact` is the actual dispatch prompt sent by the main agent to the review subagent, not a review-result summary.

If an artifact or dispatch prompt file contains these obvious anchoring field labels, formal PASS is blocked:

```text
Known issues:
Previous findings:
Just fixed:
Expected answer:
Expected PASS/FAIL:
Focus items:
suspicions:
what to verify:
Chinese-language focus/recheck field labels:
Chinese-language just-fixed field labels:
```

The machine check only scans line-leading field labels, including labels in Markdown lists and block quotes such as `- Focus items:` or `> what to verify:`. It does not classify semantic neutrality. Explanatory text that mentions `Known issues` or `Focus items` is not blocked by itself. Before starting, the review subagent must audit the dispatch prompt semantics: neutral task goals, acceptance criteria, scope, and evidence are allowed; main-agent suspicions, fix explanations, expected conclusions, or attention-directing wording such as "please focus on", "needs attention", or "please pay attention" are anchoring. Directed rechecks and advisory reviews may include focus items or what-to-verify text, but they must not pretend to be formal zero-context PASS and must not be recorded as formal gate progression.

## Implementation Evidence Fields

Formal complexity, architecture, and code-quality PASS after implementation must also include:

```text
Changed files artifact: <path>
Verification artifact: <path>
```

`Changed files artifact` can be replaced by `Raw diff artifact`; `Verification artifact` can be replaced by `Developer self-test artifact`. All paths must exist.

## QA Evidence Fields

Formal `qa-test-gate` PASS must also include:

```text
Approved case set:
QA-owned evidence:
Case-to-artifact binding:
```

## Gate Route

Every formal gate output must include this machine-readable route:

```yaml
gate_route:
  workflow_id: ""
  change_snapshot: ""
  next_action: proceed | rework | blocked | seal
  rework_owner: none | implementation | tests | architecture | qa-cases | scope
  rerun_from: none | qa-design | qa-verification | qa-execution | complexity | architecture | code-quality | final-verification
```

`REVIEW`, `FAIL`, and `BLOCKED` cannot route to `proceed`. A `FinalExecution` PASS must use `next_action: seal`; non-final PASS must use `next_action: proceed`.

## Rework Rerun Scope

When a gate routes to `rework`, the next dispatch or handoff artifact should include:

```text
Rerun Scope Decision
Previous blocking gate:
Previous rerun_from:
New change snapshot:
Rework changed files:
Rework changed requirements:
Earliest gate to rerun:
Gates not rerun:
Reason skipped gates still apply:
Full-scope review confirmed: YES
Machine limitation:
```

Rules:

- Always refresh `change_snapshot` after implementation, test, config, requirement, or gate-artifact changes.
- Rerun reviewers must review the full current diff and full requirement target, not only the repair patch.
- Rerun the failed gate and all downstream gates.
- Rerun earlier gates when the repair changes their judgment surface.
- Test, case, or oracle changes rerun from QA; scope, public surface, new concept, or budget changes rerun from complexity; ownership, dependency, lifecycle, boundary, or failure-semantics changes rerun from architecture; purely local correctness, edge-case, naming, dead-code, assertion, or encoding repairs can rerun from code-quality.
- Rerun scope is not a gate verdict and cannot convert an old artifact into a new PASS.
- Current built-in gate-state logic requires prerequisite PASS records for the same `change_snapshot`. Until carry-forward support exists, machine seal may still require rerunning earlier prerequisites even when human rerun scope is narrower.

## Recording Commands

Minimum machine-check commands:

```bash
bin/formal-gates workflow verify-admission --worktree <repo> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot>
bin/formal-gates workflow record-stage --worktree <repo> --gate <gate-id> --verdict PASS --artifact <artifact> --actor <reviewer> --workflow-id <id> --change-snapshot <snapshot>
bin/formal-gates workflow final-verification --worktree <repo> --attempts-file <attempts.json> --output .claude/gates/artifacts/final-verification.json --workflow-id <id> --change-snapshot <snapshot>
bin/formal-gates workflow final-verification --worktree <repo> --attempts-file <attempts.json> --output .claude/gates/artifacts/final-verification.json --record-final-qa --final-qa-artifact .claude/gates/artifacts/final-qa-execution.md --actor <qa-reviewer> --workflow-id <id> --change-snapshot <snapshot>
bin/formal-gates workflow cleanup --worktree <repo> --dry-run
bin/formal-gates receipt validate --worktree <repo> --receipt <receipt.json> --artifact <artifact> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> --stage <stage>
```

The native workflow commands cover snapshot, record-stage, verify-admission, show, final verification aggregation, FinalExecution recording from a supplied artifact, and dry-run-first cleanup. The native receipt commands cover dispatch registration, start/stop lifecycle event capture into `.claude/gates/proofs/events`, receipt finalization, standalone receipt validation, and diagnostic preflight. Same-host hook canaries are separate host evidence and do not replace these explicit native records.

Formal `qa-test-gate` recording must add `--mode formal --stage Execution`; final QA uses `--record-final-qa --final-qa-artifact <artifact>` to record `--stage FinalExecution` through the native workflow command. Do not copy the generic command and forget the stage.

For final QA, first write the attempts JSON file, then record a supplied `FinalQaArtifact`. After the four post-development gates have same-workflow, same-snapshot PASS records, this artifact is a main-agent mechanical closeout that only checks existing records and final verification evidence. It must not claim independent review, add QA judgment, replace missing gates, or reuse a stale snapshot. The machine validator rejects mechanical `FinalExecution` artifacts that include `Review mode: ZERO_CONTEXT_FORMAL`, `Zero-context reviewer: YES`, `Independent agent: YES`, or `Reviewer proof receipt:`.

```bash
bin/formal-gates workflow final-verification --worktree <repo> --workflow-id <id> --change-snapshot <snapshot> --attempts-file .claude/gates/artifacts/final-verification-attempts.json --output .claude/gates/artifacts/final-verification.json --record-final-qa --final-qa-artifact .claude/gates/artifacts/final-qa-execution.md --actor gate-workflow
```

Attempt entries need `status`, `accepted`, `artifact`, and `contextBundle`. `contextBundle` must include `sha256=<bundle-sha256>`.

`--record-final-qa` records a supplied, pre-existing `--final-qa-artifact`. It must not synthesize, overwrite, or repair a PASS-shaped FinalExecution report.

## Temporary Cleanup

After a single gate record or final verification succeeds, callers may run `formal-gates workflow cleanup --path <path> --execute` to delete scratch files from that run. Cleanup is intentionally narrow:

- allowed: descendants under `.artifacts/tmp/`, `.artifacts/scratch/`, `.artifacts/cleanup/`, or system temp paths whose leaf starts with `formal-gates-`, `portable-formal-gates-`, or `gate-workflow-`;
- refused: repo root, `.artifacts` root, `.claude/gates`, and any formal gate evidence path;
- cleanup scratch paths are disposable; formal evidence paths recorded or referenced by `record-stage` / `workflow final-verification` must not live there;
- command failures keep cleanup paths; callers should run `workflow cleanup --execute` only after the command whose scratch path they want to remove has passed.

The native `workflow cleanup` foundation is narrower than the legacy wrapper: it only lists or removes descendants under worktree-local `.artifacts/tmp/`, `.artifacts/scratch/`, and `.artifacts/cleanup/`. It defaults to dry-run; deletion requires `--execute`.
