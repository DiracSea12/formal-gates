param(
    [string]$SkillPath,
    [string]$OutputDir,
    [ValidateSet('.claude', '.codex')]
    [string]$TargetHost,
    [switch]$KeepTemp
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'powershell-host.ps1')

function Format-Path([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    try {
        return [System.IO.Path]::GetFullPath($Path).Replace('\\', '/')
    }
    catch {
        return $Path.Replace('\\', '/')
    }
}

function New-Dir([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        New-Item -ItemType Directory -Path $Path | Out-Null
    }
}

function Run-Git([string]$Repo, [string[]]$Arguments) {
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & git -C $Repo @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
    if ($exitCode -ne 0) {
        throw "git $($Arguments -join ' ') failed:`n$text"
    }
    return $text
}

function Run-PowerShellJson([string]$WorkingDirectory, [string[]]$Arguments) {
    if ($Arguments.Count -lt 2 -or $Arguments[0] -ne '-File') { throw 'Run-PowerShellJson expects arguments beginning with -File <script>.' }
    $script = $Arguments[1]
    $remaining = @()
    if ($Arguments.Count -gt 2) { $remaining = $Arguments[2..($Arguments.Count - 1)] }
    $launchArgs = (Get-FormalGatesPowerShellFileArgs $script) + @($remaining)
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & (Get-FormalGatesPowerShellExe) @launchArgs 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($exitCode -ne 0) {
        throw "PowerShell $($Arguments -join ' ') failed:`n$((($output | ForEach-Object { [string]$_ }) -join "`n"))"
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
    return $text | ConvertFrom-Json
}

function Run-PowerShellExpect([string]$WorkingDirectory, [string[]]$Arguments, [int]$ExpectedExitCode = 0) {
    if ($Arguments.Count -lt 2 -or $Arguments[0] -ne '-File') { throw 'Run-PowerShellExpect expects arguments beginning with -File <script>.' }
    $script = $Arguments[1]
    $remaining = @()
    if ($Arguments.Count -gt 2) { $remaining = $Arguments[2..($Arguments.Count - 1)] }
    $launchArgs = (Get-FormalGatesPowerShellFileArgs $script) + @($remaining)
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & (Get-FormalGatesPowerShellExe) @launchArgs 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
    if ($exitCode -ne $ExpectedExitCode) {
        throw "PowerShell $($Arguments -join ' ') expected exit $ExpectedExitCode but got ${exitCode}:`n$text"
    }
    return $text
}

function Set-Utf8File([string]$Path, [string]$Content) {
    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Dir $parent
    }
    [System.IO.File]::WriteAllText($Path, $Content, [System.Text.UTF8Encoding]::new($false))
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function New-FormalArtifact(
    [string]$Path,
    [string]$Title,
    [string]$ReviewerId,
    [string]$ContextBundle,
    [string[]]$ExtraLines
) {
    $lines = @(
        "# $Title",
        '',
        'Zero-context reviewer: YES',
        'Independent agent: YES',
        "Reviewer agent id: $ReviewerId",
        "Context bundle: $ContextBundle",
        'No-anchor prompt: YES',
        '',
        'gate_route:',
        "  workflow_id: $workflowId",
        "  change_snapshot: $changeSnapshot",
        '  next_action: proceed',
        '  rework_owner: none',
        '  rerun_from: none',
        ''
    ) + $ExtraLines
    Set-Utf8File $Path ($lines -join "`n")
}

function Add-Check([ref]$Summary, [string]$Name, [bool]$Passed, [string]$Detail) {
    $entry = [ordered]@{
        name = $Name
        passed = $Passed
        detail = $Detail
    }
    if ($Passed) {
        $Summary.Value.passedChecks += @($entry)
    }
    else {
        $Summary.Value.failedChecks += @($entry)
    }
}

function Get-SkillInstallRoot([string]$SkillPath, [string]$TargetHost) {
    if (-not [string]::IsNullOrWhiteSpace($TargetHost)) {
        return $TargetHost
    }

    $normalized = Format-Path $SkillPath
    if ($normalized -match '/\.codex/skills(?:/|$)') {
        return '.codex'
    }
    if ($normalized -match '/\.claude/skills(?:/|$)') {
        return '.claude'
    }
    return '.claude'
}

$resolvedSkillPath = if ([string]::IsNullOrWhiteSpace($SkillPath)) {
    [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
}
else {
    [System.IO.Path]::GetFullPath($SkillPath)
}

if (-not (Test-Path -LiteralPath $resolvedSkillPath)) {
    throw "SkillPath does not exist: $resolvedSkillPath"
}

$skillLeaf = Split-Path -Leaf $resolvedSkillPath
if ($skillLeaf -ne 'formal-gates') {
    throw "SkillPath must point to the formal-gates directory: $resolvedSkillPath"
}

$installRoot = Get-SkillInstallRoot $resolvedSkillPath $TargetHost

$repoParent = if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    Join-Path ([System.IO.Path]::GetTempPath()) 'portable-formal-gates-canary'
}
else {
    [System.IO.Path]::GetFullPath($OutputDir)
}
New-Dir $repoParent

$timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$runId = "portable-formal-gates-canary-$timestamp"
$tempRepo = Join-Path $repoParent $runId
$plainRepo = Join-Path $repoParent "$runId-plain"
$summaryPath = Join-Path $repoParent ("$runId-summary.json")

$summary = [ordered]@{
    status = 'FAIL'
    repoPath = (Format-Path $tempRepo)
    copiedSkillPath = $null
    workflowId = $null
    changeSnapshot = $null
    passedChecks = @()
    failedChecks = @()
    artifactPaths = [ordered]@{}
}

try {
    New-Dir $tempRepo

    $changeName = 'portable-formal-gates-canary'
    $workflowId = 'wf-portable-canary'
    $summary.workflowId = $workflowId

    $changeRoot = Join-Path $tempRepo "openspec/changes/$changeName"
    $specDir = Join-Path $changeRoot 'specs/portable-skill/spec.md'
    Set-Utf8File (Join-Path $changeRoot 'proposal.md') @'
# Proposal

验证 common formal-gates skill 在项目级 Windows OpenSpec 仓库中的复制与 formal gate 机器检查。
'@
    Set-Utf8File (Join-Path $changeRoot 'design.md') @'
# Design

使用最小 OpenSpec change 和 project-local formal-gates copy 运行 gate-workflow canary。
'@
    Set-Utf8File (Join-Path $changeRoot 'tasks.md') @'
# Tasks

- [x] 创建最小 OpenSpec change
- [x] 运行 common formal-gates canary
'@
    Set-Utf8File $specDir @'
# Requirement

## Scenario: Copied formal-gates skill can record formal gate workflow
- WHEN a project-local formal-gates copy runs gate workflow checks
- THEN the minimal OpenSpec repo records the expected formal stage chain
'@

    New-Dir (Join-Path $tempRepo '.claude/bundles')
    New-Dir (Join-Path $tempRepo '.claude/gates/artifacts')
    $bundlePath = Join-Path $tempRepo '.claude/bundles/canary-bundle.txt'
    Set-Utf8File $bundlePath @'
repo: portable formal gates canary
scope: minimal OpenSpec skill verification
sha256: sample
'@
    $bundleHash = Get-Sha256 $bundlePath
    $bundleRef = ".claude/bundles/canary-bundle.txt sha256=$bundleHash"
    $changedFilesRel = '.claude/gates/artifacts/changed-files.txt'
    Set-Utf8File (Join-Path $tempRepo $changedFilesRel) "portable-skill change files`nopenspec/changes/portable-formal-gates-canary/tasks.md"
    $verificationRel = '.claude/gates/artifacts/developer-self-test.txt'
    Set-Utf8File (Join-Path $tempRepo $verificationRel) 'developer self-test: portable canary fixture'

    Run-Git $tempRepo @('init') | Out-Null
    Run-Git $tempRepo @('config', 'user.name', 'portable-canary') | Out-Null
    Run-Git $tempRepo @('config', 'user.email', 'portable-canary@example.invalid') | Out-Null
    Run-Git $tempRepo @('add', '.') | Out-Null
    Run-Git $tempRepo @('commit', '-m', 'baseline') | Out-Null
    $baseCommit = Run-Git $tempRepo @('rev-parse', 'HEAD')

    Set-Utf8File (Join-Path $changeRoot 'tasks.md') @'
# Tasks

- [x] 创建最小 OpenSpec change
- [x] 运行 common formal-gates canary
- [x] 记录 formal gate artifacts
'@
    Run-Git $tempRepo @('add', '.') | Out-Null
    Run-Git $tempRepo @('commit', '-m', 'feature') | Out-Null

    $targetSkillPath = Join-Path $tempRepo "$installRoot/skills/formal-gates"
    New-Dir (Split-Path -Parent $targetSkillPath)
    Copy-Item -LiteralPath $resolvedSkillPath -Destination $targetSkillPath -Recurse -Force
    $summary.copiedSkillPath = Format-Path $targetSkillPath

    $workflowScript = Join-Path $targetSkillPath 'scripts/gate-workflow.ps1'
    if (-not (Test-Path -LiteralPath $workflowScript)) {
        throw "Copied skill is missing gate-workflow.ps1: $workflowScript"
    }
    $requiredPackageFiles = @(
        'SKILL.md',
        'references/requirements-clarification-gate.md',
        'references/qa-test-gate.md',
        'references/install-and-hooks.md',
        'hooks/enforce-gate-sequence.ps1',
        'scripts/powershell-host.ps1',
        'scripts/run-complexity-gate.ps1',
        'scripts/gate-state.ps1',
        'scripts/gate-workflow.ps1'
    )
    $missingPackageFiles = @($requiredPackageFiles | Where-Object { -not (Test-Path -LiteralPath (Join-Path $targetSkillPath $_) -PathType Leaf) })
    Add-Check ([ref]$summary) 'copied-package-structure-complete' ($missingPackageFiles.Count -eq 0) ($missingPackageFiles -join ', ')

    $complexityWrapper = Join-Path $targetSkillPath 'scripts/run-complexity-gate.ps1'
    $complexityWrapperOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $complexityWrapper,
        '--task-type', 'bugfix',
        '--max-net', '999',
        '--max-new-prod-files', '99',
        '--max-prod-insertions', '999',
        '--worktree', $tempRepo
    )
    Add-Check ([ref]$summary) 'complexity-wrapper-finds-python3' ($complexityWrapperOutput -match 'Complexity Gate: PASS') $complexityWrapperOutput

    New-Dir $plainRepo
    Set-Utf8File (Join-Path $plainRepo 'plain.txt') 'plain project without git or svn'
    $plainSnapshot = Run-PowerShellJson $plainRepo @(
        '-File', $workflowScript,
        '-Action', 'snapshot',
        '-Worktree', $plainRepo,
        '-Vcs', 'auto'
    )
    Add-Check ([ref]$summary) 'non-git-file-hash-snapshot-created' ([string]$plainSnapshot.changeSnapshot -match '^files\.') ([string]$plainSnapshot.changeSnapshot)
    $plainComplexityOutput = Run-PowerShellExpect $plainRepo @(
        '-File', $complexityWrapper,
        '--task-type', 'bugfix',
        '--max-net', '10',
        '--max-new-prod-files', '1',
        '--max-prod-insertions', '10',
        '--worktree', $plainRepo,
        '--vcs', 'auto'
    ) 2
    Add-Check ([ref]$summary) 'non-git-complexity-requires-manual-evidence' ($plainComplexityOutput -match 'no git or svn working copy detected') $plainComplexityOutput

    $snapshot = Run-PowerShellJson $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'snapshot',
        '-Worktree', $tempRepo,
        '-BaseRef', $baseCommit,
        '-HeadRef', 'HEAD',
        '-IncludeWorkingTree'
    )
    $changeSnapshot = [string]$snapshot.changeSnapshot
    $summary.changeSnapshot = $changeSnapshot
    $summary.artifactPaths.snapshot = Format-Path (Join-Path $repoParent ("$runId-snapshot.json"))
    Set-Utf8File $summary.artifactPaths.snapshot (($snapshot | ConvertTo-Json -Depth 8))
    Add-Check ([ref]$summary) 'snapshot-created' $true $changeSnapshot

    $qaArtifactRel = '.claude/gates/artifacts/qa-execution.md'
    $qaArtifactPath = Join-Path $tempRepo $qaArtifactRel
    New-FormalArtifact $qaArtifactPath 'QA Execution' 'qa-canary-agent' $bundleRef @(
        'Approved case set: portable canary approved cases',
        'QA-owned evidence: portable canary verification evidence',
        'Case-to-artifact binding: portable case maps to qa-execution.md',
        'Verification evidence: minimal formal QA execution artifact'
    )
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $qaArtifactRel,
        '-Actor', 'qa-canary-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $summary.artifactPaths.qaExecution = Format-Path $qaArtifactPath
    Add-Check ([ref]$summary) 'qa-execution-pass-recorded' $true $qaArtifactRel

    $hashWorkflowId = 'wf-artifact-hash-canary'
    $hashSnapshot = 'snap-artifact-hash-canary'
    $hashArtifactRel = '.claude/gates/artifacts/hash-qa-execution.md'
    $hashArtifactPath = Join-Path $tempRepo $hashArtifactRel
    Set-Utf8File $hashArtifactPath @"
# QA Execution

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: hash-qa-agent
Context bundle: $bundleRef
No-anchor prompt: YES
Approved case set: artifact hash canary cases
QA-owned evidence: artifact hash canary evidence
Case-to-artifact binding: hash case maps to hash-qa-execution.md

gate_route:
  workflow_id: $hashWorkflowId
  change_snapshot: $hashSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $hashArtifactRel,
        '-Actor', 'hash-qa-agent',
        '-WorkflowId', $hashWorkflowId,
        '-ChangeSnapshot', $hashSnapshot
    ) | Out-Null
    Set-Utf8File $hashArtifactPath 'tampered after PASS record'
    $hashAdmissionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-WorkflowId', $hashWorkflowId,
        '-ChangeSnapshot', $hashSnapshot
    ) 1
    $hashAdmissionPassed = $hashAdmissionOutput -match 'artifactHashMismatch'
    Add-Check ([ref]$summary) 'artifact-hash-mismatch-blocked' $hashAdmissionPassed $hashAdmissionOutput

    $complexityAdmission = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    )
    Add-Check ([ref]$summary) 'complexity-admission-pass' ($complexityAdmission -match 'GATE_STATE_ADMISSION_PASS') $complexityAdmission

    $missingSnapshotAdmissionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-WorkflowId', $workflowId
    ) 1
    $missingSnapshotAdmissionPassed = $missingSnapshotAdmissionOutput -match 'ChangeSnapshot is required|changeSnapshotRequired'
    Add-Check ([ref]$summary) 'admission-requires-change-snapshot' $missingSnapshotAdmissionPassed $missingSnapshotAdmissionOutput

    $complexityArtifactRel = '.claude/gates/artifacts/complexity-pass.md'
    $complexityArtifactPath = Join-Path $tempRepo $complexityArtifactRel
    New-FormalArtifact $complexityArtifactPath 'Complexity Gate' 'complexity-canary-agent' $bundleRef @(
        'Script result: PASS',
        'Diff shape judgment: minimal isolated canary change',
        'Impact surface health: contained to project-local formal gate files',
        'Public/config surface: no new gate ids or schema changes',
        'New concepts: portable canary only',
        'Shrink opportunities: none within stated scope',
        'Decision evidence: snapshot and QA execution artifact recorded',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $complexityArtifactRel,
        '-Actor', 'complexity-canary-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $summary.artifactPaths.complexity = Format-Path $complexityArtifactPath
    Add-Check ([ref]$summary) 'complexity-pass-recorded' $true $complexityArtifactRel

    $architectureAdmission = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'architecture-health-gate',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    )
    Add-Check ([ref]$summary) 'architecture-admission-pass' ($architectureAdmission -match 'GATE_STATE_ADMISSION_PASS') $architectureAdmission

    $architectureArtifactRel = '.claude/gates/artifacts/architecture-pass.md'
    $architectureArtifactPath = Join-Path $tempRepo $architectureArtifactRel
    New-FormalArtifact $architectureArtifactPath 'Architecture Gate' 'architecture-canary-agent' $bundleRef @(
        'Boundary review: copied formal-gates skill remains project-local and script-scoped',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'architecture-health-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $architectureArtifactRel,
        '-Actor', 'architecture-canary-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $summary.artifactPaths.architecture = Format-Path $architectureArtifactPath
    Add-Check ([ref]$summary) 'architecture-pass-recorded' $true $architectureArtifactRel

    $codeQualityAdmission = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'code-quality-gate',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    )
    Add-Check ([ref]$summary) 'code-quality-admission-pass' ($codeQualityAdmission -match 'GATE_STATE_ADMISSION_PASS') $codeQualityAdmission

    $codeQualityArtifactRel = '.claude/gates/artifacts/code-quality-pass.md'
    $codeQualityArtifactPath = Join-Path $tempRepo $codeQualityArtifactRel
    New-FormalArtifact $codeQualityArtifactPath 'Code Quality Gate' 'code-quality-canary-agent' $bundleRef @(
        'Quality review: canary chain stayed within declared scope and preserved schema',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'code-quality-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $codeQualityArtifactRel,
        '-Actor', 'code-quality-canary-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $summary.artifactPaths.codeQuality = Format-Path $codeQualityArtifactPath
    Add-Check ([ref]$summary) 'code-quality-pass-recorded' $true $codeQualityArtifactRel

    $attempts = @(
        [ordered]@{
            status = 'PASS'
            accepted = $true
            artifact = $qaArtifactRel
            reviewerAgentId = 'qa-canary-agent'
            contextBundle = $bundleRef
        }
    ) | ConvertTo-Json -Depth 8 -Compress
    $attemptsRel = '.claude/gates/artifacts/final-verification-attempts.json'
    Set-Utf8File (Join-Path $tempRepo $attemptsRel) $attempts
    $finalVerificationRel = '.claude/gates/artifacts/final-verification.json'
    $finalQaRel = '.claude/gates/artifacts/final-qa-execution.md'
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-final-verification',
        '-Worktree', $tempRepo,
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot,
        '-AttemptsJsonFile', $attemptsRel,
        '-OutputArtifact', $finalVerificationRel,
        '-FinalQaArtifact', $finalQaRel,
        '-RecordFinalQa',
        '-Actor', 'qa-final-canary-agent'
    ) | Out-Null
    $summary.artifactPaths.finalVerification = Format-Path (Join-Path $tempRepo $finalVerificationRel)
    $summary.artifactPaths.finalQa = Format-Path (Join-Path $tempRepo $finalQaRel)
    Add-Check ([ref]$summary) 'final-verification-recorded' $true $finalVerificationRel

    $manualFinalQaRel = '.claude/gates/artifacts/manual-final-qa-without-aggregate.md'
    $manualFinalQaPath = Join-Path $tempRepo $manualFinalQaRel
    Set-Utf8File $manualFinalQaPath @"
# Manual Final QA Execution

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: manual-final-qa-agent
Context bundle: $bundleRef
No-anchor prompt: YES
Approved case set: manual final QA negative canary
QA-owned evidence: .claude/gates/artifacts/missing-final-verification-aggregate.json
Case-to-artifact binding: final case maps to missing aggregate

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: seal
  rework_owner: none
  rerun_from: none
"@
    $manualFinalQaOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Stage', 'FinalExecution',
        '-Artifact', $manualFinalQaRel,
        '-Actor', 'negative-manual-final-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $manualFinalQaPassed = $manualFinalQaOutput -match 'FinalExecution QA-owned evidence'
    Add-Check ([ref]$summary) 'manual-final-execution-requires-aggregate' $manualFinalQaPassed $manualFinalQaOutput

    $invalidArtifactRel = '.claude/gates/artifacts/invalid-complexity-pass.md'
    $invalidArtifactPath = Join-Path $tempRepo $invalidArtifactRel
    Set-Utf8File $invalidArtifactPath @'
# Complexity Gate

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id:
Context bundle:
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: incomplete artifact
Impact surface health: incomplete artifact
Public/config surface: incomplete artifact
New concepts: incomplete artifact
Shrink opportunities: incomplete artifact
Decision evidence: incomplete artifact
'@
    $negativeOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $invalidArtifactRel,
        '-Actor', 'negative-canary-agent',
        '-WorkflowId', 'wf-negative',
        '-ChangeSnapshot', 'snap-negative'
    ) 1
    $negativePassed = $negativeOutput -match 'artifact lacks required formal independent zero-context review fields'
    Add-Check ([ref]$summary) 'invalid-pass-artifact-blocked' $negativePassed $negativeOutput
    $summary.artifactPaths.invalidComplexity = Format-Path $invalidArtifactPath

    $qaWithoutWorkflowOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Artifact', $qaArtifactRel,
        '-Actor', 'negative-qa-agent'
    ) 1
    $qaWithoutWorkflowPassed = $qaWithoutWorkflowOutput -match 'qaPassRequiresWorkflowId'
    Add-Check ([ref]$summary) 'qa-pass-without-workflow-blocked' $qaWithoutWorkflowPassed $qaWithoutWorkflowOutput

    $qaMissingEvidenceRel = '.claude/gates/artifacts/qa-missing-evidence-fields.md'
    $qaMissingEvidencePath = Join-Path $tempRepo $qaMissingEvidenceRel
    New-FormalArtifact $qaMissingEvidencePath 'QA Missing Evidence' 'qa-missing-evidence-agent' $bundleRef @(
        'Verification evidence: missing required QA machine fields negative canary'
    )
    $qaMissingEvidenceOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $qaMissingEvidenceRel,
        '-Actor', 'negative-qa-evidence-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $qaMissingEvidencePassed = $qaMissingEvidenceOutput -match 'Approved case set'
    Add-Check ([ref]$summary) 'qa-evidence-fields-required-blocked' $qaMissingEvidencePassed $qaMissingEvidenceOutput

    $placeholderArtifactRel = '.claude/gates/artifacts/placeholder-reviewer-complexity-pass.md'
    $placeholderArtifactPath = Join-Path $tempRepo $placeholderArtifactRel
    New-FormalArtifact $placeholderArtifactPath 'Placeholder Reviewer Complexity Gate' '<independent-reviewer-id>' $bundleRef @(
        'Script result: PASS',
        'Diff shape judgment: placeholder reviewer negative canary',
        'Impact surface health: placeholder reviewer negative canary',
        'Public/config surface: placeholder reviewer negative canary',
        'New concepts: placeholder reviewer negative canary',
        'Shrink opportunities: placeholder reviewer negative canary',
        'Decision evidence: placeholder reviewer negative canary',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    $placeholderOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $placeholderArtifactRel,
        '-Actor', 'negative-placeholder-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $placeholderPassed = $placeholderOutput -match 'Reviewer agent id'
    Add-Check ([ref]$summary) 'placeholder-reviewer-id-blocked' $placeholderPassed $placeholderOutput
    $summary.artifactPaths.placeholderReviewer = Format-Path $placeholderArtifactPath

    $badAttempts = @(
        [ordered]@{
            status = 'PASS'
            accepted = $true
            artifact = '.claude/gates/artifacts/missing-final-attempt.md'
            reviewerAgentId = 'qa-negative-agent'
            contextBundle = $bundleRef
        }
    ) | ConvertTo-Json -Depth 8 -Compress
    $badAttemptsRel = '.claude/gates/artifacts/bad-final-verification-attempts.json'
    Set-Utf8File (Join-Path $tempRepo $badAttemptsRel) $badAttempts
    $badAttemptOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-final-verification',
        '-Worktree', $tempRepo,
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot,
        '-AttemptsJsonFile', $badAttemptsRel,
        '-OutputArtifact', '.claude/gates/artifacts/bad-final-verification.json',
        '-FinalQaArtifact', '.claude/gates/artifacts/bad-final-qa.md',
        '-RecordFinalQa',
        '-Actor', 'negative-final-qa-agent'
    ) 1
    $badAttemptPassed = $badAttemptOutput -match 'finalVerificationAcceptedAttemptArtifactMissing'
    Add-Check ([ref]$summary) 'final-verification-missing-attempt-artifact-blocked' $badAttemptPassed $badAttemptOutput

    $routeMismatchArtifactRel = '.claude/gates/artifacts/route-mismatch-complexity-pass.md'
    $routeMismatchArtifactPath = Join-Path $tempRepo $routeMismatchArtifactRel
    Set-Utf8File $routeMismatchArtifactPath @"
# Complexity Gate

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: route-mismatch-agent
Context bundle: $bundleRef
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: route mismatch negative canary
Impact surface health: route mismatch negative canary
Public/config surface: route mismatch negative canary
New concepts: route mismatch negative canary
Shrink opportunities: route mismatch negative canary
Decision evidence: route mismatch negative canary
Changed files artifact: $changedFilesRel
Verification artifact: $verificationRel

gate_route:
  workflow_id: wrong-workflow
  change_snapshot: wrong-snapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $routeMismatchOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $routeMismatchArtifactRel,
        '-Actor', 'negative-route-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $routeMismatchPassed = $routeMismatchOutput -match 'gate_route.workflow_id must match WorkflowId'
    Add-Check ([ref]$summary) 'gate-route-mismatch-blocked' $routeMismatchPassed $routeMismatchOutput
    $summary.artifactPaths.routeMismatch = Format-Path $routeMismatchArtifactPath

    $badBundleHashArtifactRel = '.claude/gates/artifacts/bad-bundle-hash-complexity-pass.md'
    $badBundleHashArtifactPath = Join-Path $tempRepo $badBundleHashArtifactRel
    New-FormalArtifact $badBundleHashArtifactPath 'Bad Bundle Hash Complexity Gate' 'bad-bundle-hash-agent' '.claude/bundles/canary-bundle.txt sha256=0000000000000000000000000000000000000000000000000000000000000000' @(
        'Script result: PASS',
        'Diff shape judgment: bad bundle hash negative canary',
        'Impact surface health: bad bundle hash negative canary',
        'Public/config surface: bad bundle hash negative canary',
        'New concepts: bad bundle hash negative canary',
        'Shrink opportunities: bad bundle hash negative canary',
        'Decision evidence: bad bundle hash negative canary',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    $badBundleHashOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $badBundleHashArtifactRel,
        '-Actor', 'negative-bundle-hash-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $badBundleHashPassed = $badBundleHashOutput -match 'Context bundle sha256 mismatch'
    Add-Check ([ref]$summary) 'context-bundle-hash-mismatch-blocked' $badBundleHashPassed $badBundleHashOutput

    $missingImplementationEvidenceRel = '.claude/gates/artifacts/missing-implementation-evidence-complexity-pass.md'
    $missingImplementationEvidencePath = Join-Path $tempRepo $missingImplementationEvidenceRel
    New-FormalArtifact $missingImplementationEvidencePath 'Missing Implementation Evidence Complexity Gate' 'missing-implementation-evidence-agent' $bundleRef @(
        'Script result: PASS',
        'Diff shape judgment: missing implementation evidence negative canary',
        'Impact surface health: missing implementation evidence negative canary',
        'Public/config surface: missing implementation evidence negative canary',
        'New concepts: missing implementation evidence negative canary',
        'Shrink opportunities: missing implementation evidence negative canary',
        'Decision evidence: missing implementation evidence negative canary'
    )
    $missingImplementationEvidenceOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $missingImplementationEvidenceRel,
        '-Actor', 'negative-implementation-evidence-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $missingImplementationEvidencePassed = $missingImplementationEvidenceOutput -match 'Raw diff artifact or Changed files artifact'
    Add-Check ([ref]$summary) 'implementation-evidence-required-blocked' $missingImplementationEvidencePassed $missingImplementationEvidenceOutput

    $untrackedRel = 'untracked-evidence.txt'
    $untrackedPath = Join-Path $tempRepo $untrackedRel
    Set-Utf8File $untrackedPath 'first untracked content'
    $untrackedSnapshotA = Run-PowerShellJson $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'snapshot',
        '-Worktree', $tempRepo,
        '-BaseRef', $baseCommit,
        '-HeadRef', 'HEAD',
        '-IncludeWorkingTree'
    )
    Set-Utf8File $untrackedPath 'second untracked content'
    $untrackedSnapshotB = Run-PowerShellJson $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'snapshot',
        '-Worktree', $tempRepo,
        '-BaseRef', $baseCommit,
        '-HeadRef', 'HEAD',
        '-IncludeWorkingTree'
    )
    $untrackedHashPassed = [string]$untrackedSnapshotA.changeSnapshot -ne [string]$untrackedSnapshotB.changeSnapshot
    Add-Check ([ref]$summary) 'untracked-content-affects-snapshot' $untrackedHashPassed "$($untrackedSnapshotA.changeSnapshot) -> $($untrackedSnapshotB.changeSnapshot)"

    $manifestRel = '.claude/gates/manifests/security-gate.json'
    $manifestPath = Join-Path $tempRepo $manifestRel
    Set-Utf8File $manifestPath @'
{
  "stages": {
    "security-gate": {
      "requires": [
        { "gate": "qa-test-gate", "verdict": "PASS", "mode": "formal", "stage": "Execution", "artifact": true }
      ]
    }
  }
}
'@
    $manifestAdmissionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'security-gate',
        '-ManifestPath', $manifestRel,
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $manifestAdmissionPassed = $manifestAdmissionOutput -match 'missing route|manifestHash'
    Add-Check ([ref]$summary) 'manifest-admission-rejects-unhashed-old-pass' $manifestAdmissionPassed $manifestAdmissionOutput

    $manifestOverrideRel = '.claude/gates/manifests/override-built-in.json'
    $manifestOverridePath = Join-Path $tempRepo $manifestOverrideRel
    Set-Utf8File $manifestOverridePath @'
{
  "stages": {
    "complexity-gate": {
      "requires": []
    }
  }
}
'@
    $manifestOverrideOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-ManifestPath', $manifestOverrideRel,
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $manifestOverridePassed = $manifestOverrideOutput -match 'manifestOverridesBuiltInGate'
    Add-Check ([ref]$summary) 'manifest-cannot-override-built-in-gates' $manifestOverridePassed $manifestOverrideOutput

    $failedAttempts = @(
        [ordered]@{
            status = 'FAIL'
            accepted = $false
            artifact = $qaArtifactRel
            reviewerAgentId = 'qa-final-fail-agent'
            contextBundle = $bundleRef
        }
    ) | ConvertTo-Json -Depth 8 -Compress
    $failedAttemptsRel = '.claude/gates/artifacts/final-verification-fail-attempts.json'
    Set-Utf8File (Join-Path $tempRepo $failedAttemptsRel) $failedAttempts
    $failedFinalQaRel = '.claude/gates/artifacts/final-qa-fail-execution.md'
    $failedFinalOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-final-verification',
        '-Worktree', $tempRepo,
        '-WorkflowId', 'wf-final-fail-canary',
        '-ChangeSnapshot', 'snap-final-fail-canary',
        '-AttemptsJsonFile', $failedAttemptsRel,
        '-OutputArtifact', '.claude/gates/artifacts/final-verification-fail.json',
        '-FinalQaArtifact', $failedFinalQaRel,
        '-RecordFinalQa',
        '-Actor', 'negative-final-fail-agent'
    ) 1
    $failedFinalQaText = Get-Content -LiteralPath (Join-Path $tempRepo $failedFinalQaRel) -Raw -ErrorAction SilentlyContinue
    $failedFinalPassed = $failedFinalOutput -match 'status=FAIL' -and $failedFinalQaText -match 'next_action: blocked' -and $failedFinalQaText -notmatch 'next_action: seal'
    Add-Check ([ref]$summary) 'final-qa-fail-blocks-seal' $failedFinalPassed $failedFinalOutput

    $conditionalWorkflowId = 'wf-conditional-canary'
    $conditionalSnapshot = 'snap-conditional-canary'
    $conditionalArtifactRel = '.claude/gates/artifacts/conditional-qa-execution.md'
    $conditionalArtifactPath = Join-Path $tempRepo $conditionalArtifactRel
    Set-Utf8File $conditionalArtifactPath @"
# QA Execution

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: conditional-qa-agent
Context bundle: $bundleRef
No-anchor prompt: YES

gate_route:
  workflow_id: $conditionalWorkflowId
  change_snapshot: $conditionalSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none

Verification evidence: conditional pass negative canary
Approved case set: conditional canary approved cases
QA-owned evidence: conditional canary evidence
Case-to-artifact binding: conditional case maps to conditional-qa-execution.md
"@
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $conditionalArtifactRel,
        '-Actor', 'conditional-pass-agent',
        '-WorkflowId', $conditionalWorkflowId,
        '-ChangeSnapshot', $conditionalSnapshot
    ) | Out-Null
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'CONDITIONAL_PASS',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $conditionalArtifactRel,
        '-Actor', 'conditional-block-agent',
        '-WorkflowId', $conditionalWorkflowId,
        '-ChangeSnapshot', $conditionalSnapshot
    ) | Out-Null
    $conditionalAdmissionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-WorkflowId', $conditionalWorkflowId,
        '-ChangeSnapshot', $conditionalSnapshot
    ) 1
    $conditionalAdmissionPassed = $conditionalAdmissionOutput -match 'verdict=CONDITIONAL_PASS'
    Add-Check ([ref]$summary) 'conditional-pass-invalidates-old-pass' $conditionalAdmissionPassed $conditionalAdmissionOutput

    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'qa-test-gate',
        '-Verdict', 'REVIEW',
        '-Mode', 'formal',
        '-Stage', 'Execution',
        '-Artifact', $qaArtifactRel,
        '-Actor', 'negative-stale-review-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $staleAdmissionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'verify-admission',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $staleAdmissionPassed = $staleAdmissionOutput -match 'verdict=REVIEW'
    Add-Check ([ref]$summary) 'stale-pass-invalidated-by-review' $staleAdmissionPassed $staleAdmissionOutput

    if ($summary.failedChecks.Count -eq 0) {
        $summary.status = 'PASS'
    }

    Set-Utf8File $summaryPath (($summary | ConvertTo-Json -Depth 12))
    if ($summary.status -eq 'PASS' -and -not $KeepTemp.IsPresent) {
        Remove-Item -LiteralPath $tempRepo -Recurse -Force
        Remove-Item -LiteralPath $plainRepo -Recurse -Force -ErrorAction SilentlyContinue
    }
}
catch {
    Add-Check ([ref]$summary) 'exception' $false $_.Exception.Message
    Set-Utf8File $summaryPath (($summary | ConvertTo-Json -Depth 12))
    throw
}

$summary | ConvertTo-Json -Depth 12
if ($summary.status -ne 'PASS') {
    exit 1
}
exit 0
