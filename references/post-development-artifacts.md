# Post-development Gate Artifacts

This file is the cold-path schema reference for formal post-development gate artifacts. Use it when recording `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, or `code-quality-gate` PASS.

Keep the four gate reference files focused on judgment rules. Keep host installation, hooks, and canaries in `references/install-and-hooks.md`.

## Formal Artifact Rule

Without machine metadata, a formal PASS cannot be recorded; the result is at most an advisory review. `singleGateAuthorized=true` is only for explicit single-gate advisory review. It cannot be recorded, reused, or used to advance release/seal.

Every formal post-development gate artifact must include:

```text
Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/<gate>.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id:
Context bundle: <bundle-path> sha256=<bundle-sha256>
Dispatch prompt artifact: <dispatch-prompt-path> sha256=<dispatch-prompt-sha256>
No-anchor prompt: YES
```

`Reviewer agent id` must not be empty or a placeholder. `Context bundle` and `Dispatch prompt artifact` must point to existing files and include SHA-256 hashes; the machine validator checks those hashes. `Dispatch prompt artifact` is the actual dispatch prompt sent by the main agent to the review subagent, not a review-result summary.

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

## Recording Commands

Minimum machine-check commands:

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate <gate-id> -WorkflowId <id> -ChangeSnapshot <snapshot>
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate <gate-id> -Verdict PASS -Artifact <artifact> -Actor <reviewer> -WorkflowId <id> -ChangeSnapshot <snapshot>
```

`<ps>` means the currently available PowerShell: Windows PowerShell 5 uses `powershell -NoProfile -ExecutionPolicy Bypass`; PowerShell 7 uses `pwsh -NoProfile`. Bundled scripts continue under the current PowerShell. PowerShell 7 is not required.

Formal `qa-test-gate` recording must add `-Mode formal -Stage Execution`; final QA uses `-Mode formal -Stage FinalExecution`. Do not copy the generic command and forget the stage.

Prefer the wrapper command for final QA because it generates the aggregate artifact and records `FinalExecution`:

```powershell
$attempts = '[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-verification-run.json","reviewerAgentId":"qa-final-agent","contextBundle":".claude/bundles/<bundle>.zip sha256=<bundle-sha256>"}]'
$attemptsFile = '.claude/gates/artifacts/final-verification-attempts.json'
$attemptsPath = Join-Path '<repo>' $attemptsFile
$attempts | Set-Content -LiteralPath $attemptsPath -Encoding UTF8
<ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action record-final-verification -Worktree <repo> -WorkflowId <id> -ChangeSnapshot <snapshot> -AttemptsJsonFile $attemptsFile -OutputArtifact .claude/gates/artifacts/final-verification.json -FinalQaArtifact .claude/gates/artifacts/final-qa-execution.md -RecordFinalQa -Actor <qa-reviewer>
```

Attempt entries need `status`, `accepted`, `artifact`, `reviewerAgentId`, and `contextBundle`. `contextBundle` must include `sha256=<bundle-sha256>`. Prefer `-AttemptsJsonFile` on PowerShell 5 because raw JSON strings can lose quotes when passed through `powershell.exe -File`.

With `-RecordFinalQa`, the wrapper writes the final verification aggregate and records `qa-test-gate` `Stage=FinalExecution`; plain `record-stage FinalExecution` is only a manual fallback when an equivalent aggregate already exists.
