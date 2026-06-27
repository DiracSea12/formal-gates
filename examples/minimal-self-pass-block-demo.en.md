# Minimal Self-PASS Block Demo

This demo shows the smallest native proof that formal-gates blocks a self-stamped PASS unless evidence is supplied.

Run commands from the repository root. On Windows, use `bin\formal-gates.exe`. On macOS or Linux, use `bin/formal-gates`.

## What This Proves

- A formal PASS recording command without an artifact is denied by the native hook decision.
- The same command shape is allowed when an artifact argument is present.
- A valid QA Execution artifact can be recorded and used as the prerequisite for the next gate.

## What This Does Not Prove

- It does not prove Claude Code, Codex, Cursor, or any other host actually calls the hook.
- It does not prove code quality or a full release/seal decision.
- It does not replace independent gate review artifacts.

## 1. Baseline Check

```powershell
.\bin\formal-gates.exe canary portable --root . --format json
```

Expected: every check reports `PASS`.

## 2. Bad Path: PASS Without Artifact

```powershell
$payload = '{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --workflow-id demo --change-snapshot snap"}'
$payload | .\bin\formal-gates.exe hook decide
```

Expected:

```json
{"decision":"block","reason":"formal gate PASS recording requires an artifact","permission":"deny","permissionDecision":"deny","permissionDecisionReason":"formal gate PASS recording requires an artifact"}
```

The process exits with code `2`, which means deny.

## 3. Good Hook Path: Artifact Argument Present

```powershell
$payload = '{"command":"formal-gates workflow record-stage --gate qa-test-gate --verdict PASS --artifact qa.md --workflow-id demo --change-snapshot snap"}'
$payload | .\bin\formal-gates.exe hook decide
```

Expected:

```json
{"decision":"approve","reason":"command allowed","permission":"allow","permissionDecision":"allow","permissionDecisionReason":"command allowed"}
```

The process exits with code `0`, which means allow. This only proves the command clears the hook decision. The artifact still has to exist and pass validation before a formal record can be made.

## 4. Good Workflow Path: Record Real Evidence

Create a disposable demo artifact set:

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
Set-Content -Path $qaEvidence -Encoding UTF8 -Value "Observed block decision for PASS without artifact and approve/allow decision with artifact argument."
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

Record QA Execution:

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

Expected:

```text
GATE_WORKFLOW_RECORDED gate=qa-test-gate verdict=PASS workflowId=demo-self-pass changeSnapshot=demo-snapshot
```

Verify that the next gate may start:

```powershell
.\bin\formal-gates.exe workflow verify-admission `
  --worktree .artifacts/tmp/minimal-self-pass-demo/worktree `
  --gate complexity-gate `
  --workflow-id demo-self-pass `
  --change-snapshot demo-snapshot
```

Expected:

```text
GATE_WORKFLOW_ADMISSION_PASS gate=complexity-gate workflowId=demo-self-pass changeSnapshot=demo-snapshot
```

## 5. Cleanup

The demo writes only disposable scratch files under `.artifacts/tmp/minimal-self-pass-demo`.

```powershell
.\bin\formal-gates.exe workflow cleanup --worktree . --path .artifacts/tmp/minimal-self-pass-demo --execute
```

The demo uses a scratch worktree under `.artifacts/tmp/minimal-self-pass-demo/worktree`, so it does not write gate state into the repository root.
