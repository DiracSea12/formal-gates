# Post-development Gate Artifacts

This file is the cold-path schema reference for formal post-development gate artifacts. Use it when recording `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, or `code-quality-gate` PASS.

Keep the four gate reference files focused on judgment rules. Keep host installation, hooks, and canaries in `references/install-and-hooks.md`.

## Formal Artifact Rule

缺机器元数据时不能记录正式 PASS，最多 advisory review。`singleGateAuthorized=true` 只允许显式单门 advisory，不能记录、复用或推进 release/seal。

所有正式 post-development gate artifact 必须写明：

```text
Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Prompt source: agents/<gate>.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id:
Context bundle: <bundle-path> sha256=<bundle-sha256>
No-anchor prompt: YES
```

`Reviewer agent id` 不能是空值或占位符。`Context bundle` 必须是存在的文件并带 sha256；机器会校验 hash。

如果 artifact 包含这些锚定字段，正式 PASS 会被拦截：

```text
Known issues:
Previous findings:
Just fixed:
Expected answer:
Focus items:
重点复查:
刚修了:
```

## Implementation Evidence Fields

implementation 后的复杂度、架构、代码质量正式 PASS 还必须写明：

```text
Changed files artifact: <path>
Verification artifact: <path>
```

`Changed files artifact` 可用 `Raw diff artifact` 代替，`Verification artifact` 可用 `Developer self-test artifact` 代替。路径都必须存在。

## QA Evidence Fields

`qa-test-gate` 正式 PASS 还必须写明：

```text
Approved case set:
QA-owned evidence:
Case-to-artifact binding:
```

## Gate Route

每个正式 gate 输出都必须带机器可读路线：

```yaml
gate_route:
  workflow_id: ""
  change_snapshot: ""
  next_action: proceed | rework | blocked | seal
  rework_owner: none | implementation | tests | architecture | qa-cases | scope
  rerun_from: none | qa-design | qa-verification | qa-execution | complexity | architecture | code-quality | final-verification
```

`REVIEW`、`FAIL`、`BLOCKED` 不能路由到 `proceed`。`FinalExecution` PASS 的 `next_action` 必须是 `seal`；非最终 PASS 的 `next_action` 必须是 `proceed`。

## Recording Commands

最小机器检查命令：

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate <gate-id> -WorkflowId <id> -ChangeSnapshot <snapshot>
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate <gate-id> -Verdict PASS -Artifact <artifact> -Actor <reviewer> -WorkflowId <id> -ChangeSnapshot <snapshot>
```

`<ps>` 代表当前可用的 PowerShell：Windows PowerShell 5 用 `powershell -NoProfile -ExecutionPolicy Bypass`，PowerShell 7 用 `pwsh -NoProfile`。包内脚本会继续使用当前 PowerShell，不要求必须有 PowerShell 7。

`qa-test-gate` 正式记录必须加 `-Mode formal -Stage Execution`；最终 QA 用 `-Mode formal -Stage FinalExecution`。不要直接照抄泛化命令漏掉 stage。

最终 QA 推荐用包装命令生成聚合 artifact 并记录 `FinalExecution`：

```powershell
$attempts = '[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/final-verification-run.json","reviewerAgentId":"qa-final-agent","contextBundle":".claude/bundles/<bundle>.zip sha256=<bundle-sha256>"}]'
$attemptsFile = '.claude/gates/artifacts/final-verification-attempts.json'
$attemptsPath = Join-Path '<repo>' $attemptsFile
$attempts | Set-Content -LiteralPath $attemptsPath -Encoding UTF8
<ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action record-final-verification -Worktree <repo> -WorkflowId <id> -ChangeSnapshot <snapshot> -AttemptsJsonFile $attemptsFile -OutputArtifact .claude/gates/artifacts/final-verification.json -FinalQaArtifact .claude/gates/artifacts/final-qa-execution.md -RecordFinalQa -Actor <qa-reviewer>
```

Attempt entries need `status`, `accepted`, `artifact`, `reviewerAgentId`, and `contextBundle`. `contextBundle` must include `sha256=<bundle-sha256>`. Prefer `-AttemptsJsonFile` on PowerShell 5 because raw JSON strings can lose quotes when passed through `powershell.exe -File`.

With `-RecordFinalQa`, the wrapper writes the final verification aggregate and records `qa-test-gate` `Stage=FinalExecution`; plain `record-stage FinalExecution` is only a manual fallback when an equivalent aggregate already exists.
