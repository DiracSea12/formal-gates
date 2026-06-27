# 最小 Self-PASS 阻断 Demo

这个 demo 用最小的原生验证证明：如果没有提供证据 artifact，formal-gates 会阻止自封 PASS。

请在仓库根目录运行命令。Windows 使用 `bin\formal-gates.exe`，macOS 或 Linux 使用 `bin/formal-gates`。

## 这个 Demo 证明什么

- 没有 artifact 的正式 PASS 记录命令会被原生 hook decision 拒绝。
- 同样形状的命令在带有 artifact 参数时可以通过 hook decision。
- 有效的 QA Execution artifact 可以被记录，并作为下一道门的准入前提。

## 这个 Demo 不证明什么

- 不证明 Claude Code、Codex、Cursor 或其他宿主真的调用了 hook。
- 不证明代码质量，也不证明完整 release / seal 结论。
- 不替代独立门禁审查 artifact。

## 1. 基线检查

```powershell
.\bin\formal-gates.exe canary portable --root . --format json
```

预期结果：每个检查项都报告 `PASS`。

## 2. 错误路径：没有 Artifact 的 PASS

```powershell
$payload = '{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --workflow-id demo --change-snapshot snap"}'
$payload | .\bin\formal-gates.exe hook decide
```

预期结果：

```json
{"decision":"deny","reason":"formal gate PASS recording requires an artifact","permission":"deny","permissionDecision":"deny","permissionDecisionReason":"formal gate PASS recording requires an artifact"}
```

进程退出码是 `2`，表示拒绝。

## 3. 正确 Hook 路径：带 Artifact 参数

```powershell
$payload = '{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --artifact qa.md --workflow-id demo --change-snapshot snap"}'
$payload | .\bin\formal-gates.exe hook decide
```

预期结果：

```json
{"decision":"allow","reason":"command allowed","permission":"allow","permissionDecision":"allow","permissionDecisionReason":"command allowed"}
```

进程退出码是 `0`，表示允许。这只证明命令通过了 hook decision；artifact 仍然必须真实存在并通过校验，才能形成正式记录。

## 4. 正确 Workflow 路径：记录真实证据

创建一组一次性 demo artifact：

```powershell
$demoRoot = ".artifacts/tmp/minimal-self-pass-demo"
$demoWorktree = "$demoRoot/worktree"
New-Item -ItemType Directory -Force -Path $demoRoot | Out-Null
New-Item -ItemType Directory -Force -Path $demoWorktree | Out-Null

$bundle = "$demoWorktree/context-bundle.txt"
$dispatch = "$demoWorktree/dispatch-prompt.md"
$cases = "$demoWorktree/approved-cases.txt"
$qaEvidence = "$demoWorktree/qa-evidence.txt"
$caseBinding = "$demoWorktree/case-binding.txt"

Set-Content -Path $bundle -Encoding UTF8 -Value "Minimal demo context bundle."
Set-Content -Path $dispatch -Encoding UTF8 -Value "Neutral QA execution dispatch prompt for minimal demo."
Set-Content -Path $cases -Encoding UTF8 -Value "CASE-1: native hook decide denies PASS without artifact."
Set-Content -Path $qaEvidence -Encoding UTF8 -Value "Observed deny decision for PASS without artifact and allow decision with artifact argument."
Set-Content -Path $caseBinding -Encoding UTF8 -Value "CASE-1 -> qa-evidence.txt"

$bundleHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $bundle).Hash.ToLower()
$dispatchHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $dispatch).Hash.ToLower()
$artifact = "$demoWorktree/qa-execution.md"

@"
Gate: qa-test-gate
Verdict: PASS
Mode: formal
Stage: Execution
Workflow id: demo-self-pass
Change snapshot: demo-snapshot
Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/qa-test-gate.md
Zero-context reviewer: YES
Independent agent: YES
Context bundle: context-bundle.txt sha256=$bundleHash
Dispatch prompt artifact: dispatch-prompt.md sha256=$dispatchHash
No-anchor prompt: YES
Approved case set: approved-cases.txt
QA-owned evidence: qa-evidence.txt
Case-to-artifact binding: case-binding.txt

gate_route:
  workflow_id: "demo-self-pass"
  change_snapshot: "demo-snapshot"
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@ | Set-Content -Path $artifact -Encoding UTF8
```

记录 QA Execution：

```powershell
.\bin\formal-gates.exe workflow record-stage `
  --worktree .artifacts/tmp/minimal-self-pass-demo/worktree `
  --gate qa-test-gate `
  --verdict PASS `
  --mode formal `
  --stage Execution `
  --artifact qa-execution.md `
  --workflow-id demo-self-pass `
  --change-snapshot demo-snapshot `
  --actor demo-qa
```

预期结果：

```text
GATE_WORKFLOW_RECORDED gate=qa-test-gate verdict=PASS workflowId=demo-self-pass changeSnapshot=demo-snapshot
```

验证下一道门可以开始：

```powershell
.\bin\formal-gates.exe workflow verify-admission `
  --worktree .artifacts/tmp/minimal-self-pass-demo/worktree `
  --gate complexity-gate `
  --workflow-id demo-self-pass `
  --change-snapshot demo-snapshot
```

预期结果：

```text
GATE_WORKFLOW_ADMISSION_PASS gate=complexity-gate workflowId=demo-self-pass changeSnapshot=demo-snapshot
```

## 5. 清理

这个 demo 只会在 `.artifacts/tmp/minimal-self-pass-demo` 下写入一次性临时文件。

```powershell
.\bin\formal-gates.exe workflow cleanup --worktree . --path .artifacts/tmp/minimal-self-pass-demo --execute
```

这个 demo 使用 `.artifacts/tmp/minimal-self-pass-demo/worktree` 作为临时 worktree，所以不会把 gate state 写到仓库根目录。
