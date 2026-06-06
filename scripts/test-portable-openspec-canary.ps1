param(
    [string]$SkillPath,
    [string]$OutputDir,
    [ValidateSet('.claude', '.codex', '.cursor')]
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

function Run-PowerShellStdinExpect([string]$WorkingDirectory, [string]$Script, [string]$InputText, [int]$ExpectedExitCode = 0) {
    $launchArgs = Get-FormalGatesPowerShellFileArgs $Script
    $previousErrorActionPreference = $ErrorActionPreference
    $previousLocation = (Get-Location).Path
    $ErrorActionPreference = 'Continue'
    try {
        Set-Location -LiteralPath $WorkingDirectory
        $output = $InputText | & (Get-FormalGatesPowerShellExe) @launchArgs 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        Set-Location -LiteralPath $previousLocation
        $ErrorActionPreference = $previousErrorActionPreference
    }
    $text = (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
    if ($exitCode -ne $ExpectedExitCode) {
        throw "PowerShell -File $Script expected exit $ExpectedExitCode but got ${exitCode}:`n$text"
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

function Get-CanaryPromptSource([string]$Title) {
    if ($Title -match '(?i)\bQA\b') { return 'agents/qa-test-gate.md' }
    if ($Title -match '(?i)Architecture') { return 'agents/architecture-health-gate.md' }
    if ($Title -match '(?i)Code Quality') { return 'agents/code-quality-gate.md' }
    return 'agents/complexity-gate.md'
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
        'Review mode: ZERO_CONTEXT_FORMAL',
        'Prompt contamination check: PASS',
        'Semantic anti-anchor check: PASS',
        "Prompt source: $(Get-CanaryPromptSource $Title)",
        'Zero-context reviewer: YES',
        'Independent agent: YES',
        "Reviewer agent id: $ReviewerId",
        "Context bundle: $ContextBundle",
        "Dispatch prompt artifact: $dispatchPromptRef",
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

function Test-AgentTemplateIntegrity([string]$SkillPath) {
    $errors = @()
    $processViolationText = 'PROCESS_VIOLATION: ' + [string]::new([char[]]@([char]0x4E3B, [char]0x4EE3, [char]0x7406, [char]0x8D8A, [char]0x754C, [char]0x6C61, [char]0x67D3, [char]0x5BA1, [char]0x67E5))
    $agentFiles = @(
        'agents/qa-test-gate.md',
        'agents/complexity-gate.md',
        'agents/architecture-health-gate.md',
        'agents/code-quality-gate.md',
        'agents/cold-water-review.md'
    )
    foreach ($relative in $agentFiles) {
        $path = Join-Path $SkillPath $relative
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            $errors += "missing $relative"
            continue
        }
        $text = Get-Content -LiteralPath $path -Raw -Encoding UTF8
        foreach ($required in @(
                'Role:',
                'Allowed prompt fields:',
                'Forbidden prompt fields include',
                $processViolationText,
                'Do not continue review. Do not output PASS, FAIL, or REVIEW.',
                'Semantic anti-anchor check: PASS',
                'Prompt source: ' + $relative,
                'Dispatch prompt artifact:'
            )) {
            if ($text -notmatch [regex]::Escape($required)) {
                $errors += "$relative missing required text: $required"
            }
        }
        foreach ($forbidden in @('Known issues', 'Previous findings', 'Just fixed', 'Expected answer', 'Expected PASS/FAIL', 'Focus items')) {
            if ($text -notmatch [regex]::Escape($forbidden)) {
                $errors += "$relative missing forbidden-field guard: $forbidden"
            }
        }
    }

    $requirementsAgent = Join-Path $SkillPath 'agents/requirements-clarification-gate.md'
    if (-not (Test-Path -LiteralPath $requirementsAgent -PathType Leaf)) {
        $errors += 'missing agents/requirements-clarification-gate.md'
    }
    else {
        $text = Get-Content -LiteralPath $requirementsAgent -Raw -Encoding UTF8
        foreach ($required in @('Role:', 'pre-document requirement alignment agent', 'must not use OpenSpec, tasks, commits, gate artifacts, validation reports, or implementation as the requirement source of truth', 'Requirements Clarification Gate')) {
            if ($text -notmatch [regex]::Escape($required)) {
                $errors += "agents/requirements-clarification-gate.md missing required text: $required"
            }
        }
    }

    $openAiPath = Join-Path $SkillPath 'agents/openai.yaml'
    if (Test-Path -LiteralPath $openAiPath -PathType Leaf) {
        $openAiText = Get-Content -LiteralPath $openAiPath -Raw -Encoding UTF8
        if ($openAiText -match '(?m)^[ \t]*agent_templates[ \t]*:') { $errors += 'agents/openai.yaml must not define agent_templates' }
        if ($openAiText -match '(?m)^[ \t]*prompt_template[ \t]*:') { $errors += 'agents/openai.yaml must not define prompt_template' }
    }
    return @($errors)
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
    if ($normalized -match '/\.cursor(?:/|$)') {
        return '.cursor'
    }
    return '.claude'
}

function Get-PortableSkillPackageEntries {
    return @(
        'SKILL.md',
        'agents',
        'examples',
        'hooks',
        'references',
        'scripts'
    )
}

function Copy-PortableSkillPackage([string]$SourcePath, [string]$TargetPath) {
    New-Dir (Split-Path -Parent $TargetPath)
    New-Dir $TargetPath
    foreach ($entry in Get-PortableSkillPackageEntries) {
        $candidate = Join-Path $SourcePath $entry
        if (-not (Test-Path -LiteralPath $candidate)) {
            throw "portable skill source is incomplete; missing $entry under $SourcePath"
        }
        Copy-Item -LiteralPath $candidate -Destination $TargetPath -Recurse -Force
    }
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
$uniqueSuffix = ([guid]::NewGuid().ToString('N')).Substring(0, 8)
$runId = "portable-formal-gates-canary-$timestamp-$PID-$uniqueSuffix"
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
    $dispatchPromptRel = '.claude/gates/artifacts/dispatch-prompt.txt'
    $dispatchPromptPath = Join-Path $tempRepo $dispatchPromptRel
    Set-Utf8File $dispatchPromptPath @'
Worktree: portable canary repo
Base commit or snapshot: portable canary snapshot
Context bundle: .claude/bundles/canary-bundle.txt
Diff or changed-files artifact: .claude/gates/artifacts/changed-files.txt
User request and acceptance criteria: run portable formal-gates canary
Forbidden files: none
Allowed prompt guard note: Forbidden prompt fields include Known issues and Focus items.
Output template: formal gate artifact
'@
    $dispatchPromptHash = Get-Sha256 $dispatchPromptPath
    $dispatchPromptRef = "$dispatchPromptRel sha256=$dispatchPromptHash"
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

    $targetSkillPath = if ($installRoot -eq '.cursor') {
        Join-Path $tempRepo "$installRoot/formal-gates"
    }
    else {
        Join-Path $tempRepo "$installRoot/skills/formal-gates"
    }
    Copy-PortableSkillPackage $resolvedSkillPath $targetSkillPath
    $summary.copiedSkillPath = Format-Path $targetSkillPath

    $workflowScript = Join-Path $targetSkillPath 'scripts/gate-workflow.ps1'
    $hookScript = Join-Path $targetSkillPath 'hooks/enforce-gate-sequence.ps1'
    if (-not (Test-Path -LiteralPath $workflowScript)) {
        throw "Copied skill is missing gate-workflow.ps1: $workflowScript"
    }
    if (-not (Test-Path -LiteralPath $hookScript)) {
        throw "Copied skill is missing enforce-gate-sequence.ps1: $hookScript"
    }
    $requiredPackageFiles = @(
        'SKILL.md',
        'agents/openai.yaml',
        'agents/qa-test-gate.md',
        'agents/complexity-gate.md',
        'agents/architecture-health-gate.md',
        'agents/code-quality-gate.md',
        'agents/cold-water-review.md',
        'agents/requirements-clarification-gate.md',
        'references/requirements-clarification-gate.md',
        'references/requirements-clarification-artifacts.md',
        'references/post-development-artifacts.md',
        'references/qa-test-gate.md',
        'references/complexity-gate.md',
        'references/architecture-health-gate.md',
        'references/code-quality-gate.md',
        'references/install-and-hooks.md',
        'hooks/enforce-gate-sequence.ps1',
        'scripts/gate-artifact-validation.ps1',
        'scripts/powershell-host.ps1',
        'scripts/run-complexity-gate.ps1',
        'scripts/gate-state.ps1',
        'scripts/gate-workflow.ps1'
    )
    $missingPackageFiles = @($requiredPackageFiles | Where-Object { -not (Test-Path -LiteralPath (Join-Path $targetSkillPath $_) -PathType Leaf) })
    Add-Check ([ref]$summary) 'copied-package-structure-complete' ($missingPackageFiles.Count -eq 0) ($missingPackageFiles -join ', ')

    $agentTemplateErrors = @(Test-AgentTemplateIntegrity $targetSkillPath)
    Add-Check ([ref]$summary) 'agent-template-integrity-enforced' ($agentTemplateErrors.Count -eq 0) ($agentTemplateErrors -join "`n")

    $installScript = Join-Path $targetSkillPath 'scripts/install-formal-gates.ps1'
    $installHookOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $installScript,
        '-HostName', 'Claude',
        '-Scope', 'Project',
        '-ProjectPath', $tempRepo,
        '-SourcePath', $targetSkillPath,
        '-ConfigureHook'
    )
    $claudeSettingsPath = Join-Path $tempRepo '.claude/settings.json'
    $claudeSettings = Get-Content -LiteralPath $claudeSettingsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $formalClaudeMatchers = @(
        foreach ($entry in @($claudeSettings.hooks.PreToolUse)) {
            foreach ($hook in @($entry.hooks)) {
                if (([string]$hook.command) -like '*enforce-gate-sequence.ps1*') {
                    [string]$entry.matcher
                }
            }
        }
    )
    $formalClaudeCommands = @(
        foreach ($entry in @($claudeSettings.hooks.PreToolUse)) {
            foreach ($hook in @($entry.hooks)) {
                if (([string]$hook.command) -like '*enforce-gate-sequence.ps1*') {
                    [string]$hook.command
                }
            }
        }
    )
    $claudeMatcherPassed = ($formalClaudeMatchers -contains '*') -and ($formalClaudeCommands.Count -gt 0)
    Add-Check ([ref]$summary) 'claude-install-hook-matcher-covers-document-tools' $claudeMatcherPassed (($formalClaudeMatchers -join ', ') + "`n" + ($formalClaudeCommands -join ', ') + "`n" + $installHookOutput)

    $cursorInstallOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $installScript,
        '-HostName', 'Cursor',
        '-Scope', 'Project',
        '-ProjectPath', $tempRepo,
        '-SourcePath', $targetSkillPath,
        '-Force',
        '-ConfigureHook'
    )
    $cursorHooksPath = Join-Path $tempRepo '.cursor/hooks.json'
    $cursorHooks = Get-Content -LiteralPath $cursorHooksPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $formalCursorCommands = @(
        foreach ($entry in @($cursorHooks.hooks.preToolUse)) {
            if (([string]$entry.command) -like '*enforce-gate-sequence.ps1*') {
                [string]$entry.command
            }
        }
    )
    $formalCursorFailClosed = @(
        foreach ($entry in @($cursorHooks.hooks.preToolUse)) {
            if (([string]$entry.command) -like '*enforce-gate-sequence.ps1*') {
                [bool]$entry.failClosed
            }
        }
    )
    $cursorHookPassed = ($formalCursorCommands.Count -gt 0) -and ($formalCursorFailClosed -contains $true)
    Add-Check ([ref]$summary) 'cursor-install-hook-configured' $cursorHookPassed (($formalCursorCommands -join ', ') + "`n" + $cursorInstallOutput)

    $complexityWrapper = Join-Path $targetSkillPath 'scripts/run-complexity-gate.ps1'
    $complexityWrapperOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $complexityWrapper,
        '--task-type', 'bugfix',
        '--max-net', '2000',
        '--max-new-prod-files', '99',
        '--max-prod-insertions', '2000',
        '--worktree', $tempRepo
    )
    Add-Check ([ref]$summary) 'complexity-wrapper-finds-supported-python' ($complexityWrapperOutput -match 'Complexity Gate: PASS') $complexityWrapperOutput

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

    $alignmentRel = '.claude/gates/artifacts/requirements-alignment.md'
    $alignmentPath = Join-Path $tempRepo $alignmentRel
    Set-Utf8File $alignmentPath @'
# Requirements Alignment

ID: RQ-001
Requirement or question: Verify copied formal-gates can record a machine-checked workflow.
Source: user-confirmed canary brief
Why it matters: This is the behavior the portable canary is proving.
Status: confirmed
User answer: Keep the check scoped to formal-gates package behavior.
Downstream effect: OpenSpec docs may be drafted for this canary.
OpenSpec impact: proposal/design/tasks/spec describe the portable canary only.
Evidence needed: gate-workflow record-stage succeeds with matching workflow and snapshot.

ID: RQ-002
Requirement or question: Formal document edits must not proceed before requirement alignment is recorded.
Source: user-confirmed canary brief
Why it matters: This proves the pre-document gate is not chat-only.
Status: confirmed
User answer: Block formal document writes until clarification PASS exists.
Downstream effect: hook must deny OpenSpec markdown writes without clarification PASS.
OpenSpec impact: formal document edits depend on requirements-clarification-gate PASS.
Evidence needed: hook negative test blocks a simulated OpenSpec proposal write.
'@
    $decisionRel = '.claude/gates/artifacts/requirements-user-decision.md'
    $decisionPath = Join-Path $tempRepo $decisionRel
    Set-Utf8File $decisionPath @"
# User Decision

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "Proceed with the recorded alignment table for this portable canary."
Approved alignment IDs: all
Approved alignment artifact: .claude/gates/artifacts/requirements-alignment.md
Approved workflow id: $workflowId
Approved change snapshot: $changeSnapshot
Approval scope: requirements-clarification-gate
"@
    $requirementsClarificationDimensionCoverage = @'
Dimension coverage:
  DIM-01 Goal: covered | RQ-001
  DIM-02 User/value: covered | RQ-001
  DIM-03 Scope: covered | RQ-001
  DIM-04 Non-goals: NA | RQ-001
  DIM-05 Acceptance: covered | RQ-001
  DIM-06 Evidence: covered | RQ-001,RQ-002
  DIM-07 Constraints: covered | RQ-002
  DIM-08 Architecture boundary: NA | RQ-001
  DIM-09 Unknowns: NA | RQ-001
  DIM-10 Task status: covered | RQ-002
  DIM-11 Phase dependency: NA | RQ-001
  DIM-12 Must-not-cut scope: NA | RQ-001
'@
    $clarificationRel = '.claude/gates/artifacts/requirements-clarification-pass.md'
    $clarificationPath = Join-Path $tempRepo $clarificationRel
    Set-Utf8File $clarificationPath @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: FIRST_RUN
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $clarificationRel,
        '-Actor', 'requirements-clarification-canary',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) | Out-Null
    $summary.artifactPaths.requirementsAlignment = Format-Path $alignmentPath
    $summary.artifactPaths.requirementsClarification = Format-Path $clarificationPath
    $summary.artifactPaths.requirementsDecision = Format-Path $decisionPath
    Add-Check ([ref]$summary) 'requirements-clarification-pass-recorded' $true $clarificationRel

    $staleDocWritePayload = @{
        tool_name = 'Write'
        cwd = $tempRepo
        tool_input = @{
            file_path = (Join-Path $tempRepo 'openspec/changes/stale/proposal.md')
            content = '# Stale'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $staleDocWriteOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $staleDocWritePayload 2
    $staleDocWritePassed = $staleDocWriteOutput -match 'workflowId and GateWorkflow.changeSnapshot are required'
    Add-Check ([ref]$summary) 'formal-document-write-stale-pass-without-workflow-blocked' $staleDocWritePassed $staleDocWriteOutput

    $fakeConfirmationRel = '.claude/gates/artifacts/bad-requirements-clarification-inline-decision.md'
    Set-Utf8File (Join-Path $tempRepo $fakeConfirmationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: INLINE
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-fake-confirmation
  change_snapshot: snap-fake-confirmation
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $fakeConfirmationOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $fakeConfirmationRel,
        '-Actor', 'negative-fake-confirmation',
        '-WorkflowId', 'wf-fake-confirmation',
        '-ChangeSnapshot', 'snap-fake-confirmation'
    ) 1
    $fakeConfirmationPassed = $fakeConfirmationOutput -match 'Decision record must point to a dedicated'
    Add-Check ([ref]$summary) 'requirements-clarification-inline-decision-blocked' $fakeConfirmationPassed $fakeConfirmationOutput

    $ordinaryDecisionRel = 'notes/requirements-user-decision.md'
    Set-Utf8File (Join-Path $tempRepo $ordinaryDecisionRel) @'
# Ordinary Note

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "This ordinary note must not unlock requirements clarification."
Approved alignment IDs: all
Approval scope: requirements-clarification-gate
'@
    $ordinaryDecisionClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-ordinary-decision.md'
    Set-Utf8File (Join-Path $tempRepo $ordinaryDecisionClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $ordinaryDecisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-ordinary-decision
  change_snapshot: snap-ordinary-decision
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $ordinaryDecisionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $ordinaryDecisionClarificationRel,
        '-Actor', 'negative-ordinary-decision',
        '-WorkflowId', 'wf-ordinary-decision',
        '-ChangeSnapshot', 'snap-ordinary-decision'
    ) 1
    $ordinaryDecisionPassed = $ordinaryDecisionOutput -match 'Decision record must point to a dedicated'
    Add-Check ([ref]$summary) 'requirements-clarification-ordinary-decision-blocked' $ordinaryDecisionPassed $ordinaryDecisionOutput

    $openspecDecisionRel = 'openspec/changes/x/requirements-user-decision.md'
    Set-Utf8File (Join-Path $tempRepo $openspecDecisionRel) @'
# OpenSpec Decision Impostor

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "This OpenSpec file must not unlock requirements clarification."
Approved alignment IDs: all
Approval scope: requirements-clarification-gate
'@
    $openspecDecisionClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-openspec-decision.md'
    Set-Utf8File (Join-Path $tempRepo $openspecDecisionClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $openspecDecisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-openspec-decision
  change_snapshot: snap-openspec-decision
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $openspecDecisionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $openspecDecisionClarificationRel,
        '-Actor', 'negative-openspec-decision',
        '-WorkflowId', 'wf-openspec-decision',
        '-ChangeSnapshot', 'snap-openspec-decision'
    ) 1
    $openspecDecisionPassed = $openspecDecisionOutput -match 'Decision record must point to a dedicated'
    Add-Check ([ref]$summary) 'requirements-clarification-openspec-decision-blocked' $openspecDecisionPassed $openspecDecisionOutput

    $wrongNameDecisionRel = '.claude/gates/artifacts/alignment-user-confirmation.md'
    Set-Utf8File (Join-Path $tempRepo $wrongNameDecisionRel) @'
# Wrong Name Decision

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "This wrong-name artifact must not unlock requirements clarification."
Approved alignment IDs: all
Approval scope: requirements-clarification-gate
'@
    $wrongNameDecisionClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-wrong-name-decision.md'
    Set-Utf8File (Join-Path $tempRepo $wrongNameDecisionClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $wrongNameDecisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-wrong-name-decision
  change_snapshot: snap-wrong-name-decision
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $wrongNameDecisionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $wrongNameDecisionClarificationRel,
        '-Actor', 'negative-wrong-name-decision',
        '-WorkflowId', 'wf-wrong-name-decision',
        '-ChangeSnapshot', 'snap-wrong-name-decision'
    ) 1
    $wrongNameDecisionPassed = $wrongNameDecisionOutput -match 'Decision record must point to a dedicated'
    Add-Check ([ref]$summary) 'requirements-clarification-wrong-name-decision-blocked' $wrongNameDecisionPassed $wrongNameDecisionOutput

    $firstRunResetRel = '.claude/gates/artifacts/bad-requirements-clarification-first-run-reset.md'
    Set-Utf8File (Join-Path $tempRepo $firstRunResetRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: FIRST_RUN
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $firstRunResetOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $firstRunResetRel,
        '-Actor', 'negative-first-run-reset',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $firstRunResetPassed = $firstRunResetOutput -match 'Previous alignment artifact cannot be FIRST_RUN'
    Add-Check ([ref]$summary) 'requirements-clarification-first-run-reset-blocked' $firstRunResetPassed $firstRunResetOutput

    $fakePreviousAlignmentRel = '.claude/gates/artifacts/bad-requirements-alignment-fake-previous.md'
    Set-Utf8File (Join-Path $tempRepo $fakePreviousAlignmentRel) @'
# Fake Previous Requirements Alignment

ID: RQ-001
Requirement or question: Keep only one current question and pretend this is the previous alignment.
Source: user-confirmed canary brief
Why it matters: Previous alignment cannot be forged by pointing at the current table.
Status: confirmed
User answer: This is a fake previous alignment negative canary.
Downstream effect: Old RQ IDs could disappear if this were accepted.
OpenSpec impact: proposal would silently lose old requirements.
Evidence needed: record-stage must reject this fake previous artifact.
'@
    $fakePreviousDecisionRel = '.claude/gates/artifacts/requirements-user-decision-fake-previous.md'
    Set-Utf8File (Join-Path $tempRepo $fakePreviousDecisionRel) @'
# User Decision

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "Approve only RQ-001 for the fake previous alignment negative canary."
Approved alignment IDs: RQ-001
Approval scope: requirements-clarification-gate
'@
    $fakePreviousClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-fake-previous-alignment.md'
    Set-Utf8File (Join-Path $tempRepo $fakePreviousClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $fakePreviousAlignmentRel
Total alignment items: 1
Previous alignment artifact: $fakePreviousAlignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $fakePreviousDecisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $fakePreviousOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $fakePreviousClarificationRel,
        '-Actor', 'negative-fake-previous-alignment',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $fakePreviousPassed = $fakePreviousOutput -match 'Previous alignment artifact must match the latest historical Alignment table artifact'
    Add-Check ([ref]$summary) 'requirements-clarification-fake-previous-alignment-blocked' $fakePreviousPassed $fakePreviousOutput

    $oldDecisionAlignmentRel = '.claude/gates/artifacts/bad-requirements-alignment-old-decision-reuse.md'
    Set-Utf8File (Join-Path $tempRepo $oldDecisionAlignmentRel) @'
# Requirements Alignment

ID: RQ-003
Requirement or question: A new alignment item must not reuse an old all-approved decision file.
Source: user-confirmed canary brief
Why it matters: Old dedicated decision records must be bound to the current alignment.
Status: confirmed
User answer: Old decision files are not reusable unless they bind to the current approval scope.
Downstream effect: Draft unlock must depend on the current alignment approval.
OpenSpec impact: proposal cannot proceed on stale user confirmation.
Evidence needed: record-stage rejects the old dedicated decision record reuse.
'@
    $oldDecisionClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-old-decision-reuse.md'
    Set-Utf8File (Join-Path $tempRepo $oldDecisionClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $oldDecisionAlignmentRel
Total alignment items: 1
Previous alignment artifact: FIRST_RUN
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-old-decision-reuse
  change_snapshot: snap-old-decision-reuse
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $oldDecisionOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $oldDecisionClarificationRel,
        '-Actor', 'negative-old-decision-reuse',
        '-WorkflowId', 'wf-old-decision-reuse',
        '-ChangeSnapshot', 'snap-old-decision-reuse'
    ) 1
    $oldDecisionPassed = $oldDecisionOutput -match 'Decision record must bind to the current alignment artifact' -and $oldDecisionOutput -match 'Decision record must bind to the current workflow and change snapshot'
    Add-Check ([ref]$summary) 'requirements-clarification-old-dedicated-decision-reuse-blocked' $oldDecisionPassed $oldDecisionOutput

    $droppedFakeYesDecisionRel = '.claude/gates/artifacts/requirements-user-decision-dropped-fake-yes.md'
    Set-Utf8File (Join-Path $tempRepo $droppedFakeYesDecisionRel) @'
# User Decision

Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: "Approve current RQ-001, but do not approve dropping any old IDs."
Approved alignment IDs: RQ-001
Approval scope: requirements-clarification-gate
'@
    $droppedFakeYesRel = '.claude/gates/artifacts/bad-requirements-clarification-dropped-fake-yes.md'
    Set-Utf8File (Join-Path $tempRepo $droppedFakeYesRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $fakePreviousAlignmentRel
Total alignment items: 1
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: RQ-002
Dropped question approval: YES
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $droppedFakeYesDecisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-dropped-fake-yes
  change_snapshot: snap-dropped-fake-yes
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $droppedFakeYesOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $droppedFakeYesRel,
        '-Actor', 'negative-dropped-fake-yes',
        '-WorkflowId', 'wf-dropped-fake-yes',
        '-ChangeSnapshot', 'snap-dropped-fake-yes'
    ) 1
    $droppedFakeYesPassed = $droppedFakeYesOutput -match 'Dropped question IDs require explicit decision record approval'
    Add-Check ([ref]$summary) 'requirements-clarification-dropped-ids-fake-yes-blocked' $droppedFakeYesPassed $droppedFakeYesOutput

    $deferredAlignmentRel = '.claude/gates/artifacts/bad-requirements-alignment-deferred-no-approval.md'
    Set-Utf8File (Join-Path $tempRepo $deferredAlignmentRel) @'
# Requirements Alignment

ID: RQ-001
Requirement or question: Defer a blocking choice without user approval.
Source: user-confirmed canary brief
Why it matters: Deferred items must not pass without explicit user approval.
Status: deferred-by-user
User answer: decide later
Downstream effect: Draft would proceed with a known gap.
OpenSpec impact: proposal leaves one choice unresolved.
Evidence needed: explicit user approval for the defer decision.
'@
    $deferredClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-deferred-no-approval.md'
    Set-Utf8File (Join-Path $tempRepo $deferredClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $deferredAlignmentRel
Total alignment items: 1
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: RQ-002
Dropped question approval: YES
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-deferred-no-approval
  change_snapshot: snap-deferred-no-approval
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $deferredClarificationOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $deferredClarificationRel,
        '-Actor', 'negative-deferred-no-approval',
        '-WorkflowId', 'wf-deferred-no-approval',
        '-ChangeSnapshot', 'snap-deferred-no-approval'
    ) 1
    $deferredClarificationPassed = $deferredClarificationOutput -match 'requires per-item user approval evidence'
    Add-Check ([ref]$summary) 'requirements-clarification-deferred-without-user-approval-blocked' $deferredClarificationPassed $deferredClarificationOutput

    $badClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-open-question.md'
    Set-Utf8File (Join-Path $tempRepo $badClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: RQ-002
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-bad-clarification
  change_snapshot: snap-bad-clarification
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $badClarificationOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $badClarificationRel,
        '-Actor', 'negative-requirements-clarification',
        '-WorkflowId', 'wf-bad-clarification',
        '-ChangeSnapshot', 'snap-bad-clarification'
    ) 1
    $badClarificationPassed = $badClarificationOutput -match 'Open question IDs must be none'
    Add-Check ([ref]$summary) 'requirements-clarification-open-question-blocked' $badClarificationPassed $badClarificationOutput

    $incompleteAlignmentRel = '.claude/gates/artifacts/incomplete-requirements-alignment.md'
    Set-Utf8File (Join-Path $tempRepo $incompleteAlignmentRel) @'
# Incomplete Requirements Alignment

ID: RQ-001
'@
    $incompleteClarificationRel = '.claude/gates/artifacts/bad-requirements-clarification-incomplete-alignment.md'
    Set-Utf8File (Join-Path $tempRepo $incompleteClarificationRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $incompleteAlignmentRel
Total alignment items: 1
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
Covered formal targets: openspec/changes/portable-formal-gates-canary/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-incomplete-clarification
  change_snapshot: snap-incomplete-clarification
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $incompleteClarificationOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'requirements-clarification-gate',
        '-Verdict', 'PASS',
        '-Artifact', $incompleteClarificationRel,
        '-Actor', 'negative-incomplete-requirements-clarification',
        '-WorkflowId', 'wf-incomplete-clarification',
        '-ChangeSnapshot', 'snap-incomplete-clarification'
    ) 1
    $incompleteClarificationPassed = $incompleteClarificationOutput -match 'missing meaningful'
    Add-Check ([ref]$summary) 'requirements-clarification-incomplete-alignment-blocked' $incompleteClarificationPassed $incompleteClarificationOutput

    $badCoveredTargetCases = @(
        @{ Name = 'missing-covered-targets'; Line = $null },
        @{ Name = 'root-covered-targets'; Line = 'Covered formal targets: .' },
        @{ Name = 'wildcard-covered-targets'; Line = 'Covered formal targets: *' }
    )
    foreach ($badCoveredTargetCase in $badCoveredTargetCases) {
        $coveredLine = if ([string]::IsNullOrWhiteSpace([string]$badCoveredTargetCase.Line)) { '' } else { "$($badCoveredTargetCase.Line)`n" }
        $badCoveredTargetRel = ".claude/gates/artifacts/bad-requirements-clarification-$($badCoveredTargetCase.Name).md"
        Set-Utf8File (Join-Path $tempRepo $badCoveredTargetRel) @"
# Requirements Clarification Gate

Requirement source: user-confirmed portable canary brief
Alignment table artifact: $alignmentRel
Total alignment items: 2
Previous alignment artifact: $alignmentRel
Open question IDs: none
Dropped question IDs: none
Dropped question approval: NOT_APPLICABLE: none dropped
User confirmation: YES
Open blockers: none
Coverage scan: PASS
Scope preservation check: NOT_APPLICABLE: no draft written yet
Task proof check: NOT_APPLICABLE: no tasks completed yet
$requirementsClarificationDimensionCoverage
Decision record: $decisionRel
$($coveredLine)Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: wf-$($badCoveredTargetCase.Name)
  change_snapshot: snap-$($badCoveredTargetCase.Name)
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
        $badCoveredTargetOutput = Run-PowerShellExpect $tempRepo @(
            '-File', $workflowScript,
            '-Action', 'record-stage',
            '-Worktree', $tempRepo,
            '-Gate', 'requirements-clarification-gate',
            '-Verdict', 'PASS',
            '-Artifact', $badCoveredTargetRel,
            '-Actor', "negative-$($badCoveredTargetCase.Name)",
            '-WorkflowId', "wf-$($badCoveredTargetCase.Name)",
            '-ChangeSnapshot', "snap-$($badCoveredTargetCase.Name)"
        ) 1
        $badCoveredTargetPassed = $badCoveredTargetOutput -match 'Covered formal targets'
        Add-Check ([ref]$summary) "requirements-clarification-$($badCoveredTargetCase.Name)-blocked" $badCoveredTargetPassed $badCoveredTargetOutput
    }

    $docWritePayloadBlocked = @{
        tool_name = 'Write'
        cwd = $plainRepo
        tool_input = @{
            file_path = (Join-Path $plainRepo 'openspec/changes/blocked/proposal.md')
            content = '# Proposal'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $docWriteBlockedOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $docWritePayloadBlocked 2
    $docWriteBlockedPassed = $docWriteBlockedOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-write-before-clarification-blocked' $docWriteBlockedPassed $docWriteBlockedOutput

    $applyPatchBlockedPayload = @{
        tool_name = 'apply_patch'
        cwd = $plainRepo
        tool_input = @{
            command = @'
*** Begin Patch
*** Add File: openspec/changes/x/proposal.md
+# Proposal
*** End Patch
'@
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $applyPatchBlockedOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $applyPatchBlockedPayload 2
    $applyPatchBlockedPassed = $applyPatchBlockedOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-apply-patch-before-clarification-blocked' $applyPatchBlockedPassed $applyPatchBlockedOutput

    $applyPatchPatchFieldBlockedPayload = @{
        tool_name = 'apply_patch'
        cwd = $plainRepo
        tool_input = @{
            patch = @'
*** Begin Patch
*** Add File: docs/requirements/new-requirement.md
+# Requirement
*** End Patch
'@
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $applyPatchPatchFieldBlockedOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $applyPatchPatchFieldBlockedPayload 2
    $applyPatchPatchFieldBlockedPassed = $applyPatchPatchFieldBlockedOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-apply-patch-patch-field-blocked' $applyPatchPatchFieldBlockedPassed $applyPatchPatchFieldBlockedOutput

    $directoryDocPaths = @(
        'docs/prd/index.md',
        'docs/sdd/design.md',
        'docs/phases/31.md',
        'docs/start-readiness/index.md',
        'docs/requirements/index.md',
        'docs/specs/index.md',
        'docs/requirements/product-requirements.txt',
        'feature-specs.txt',
        'PRD.txt',
        'requirements/index.md',
        'specs/index.md'
    )
    foreach ($directoryDocPath in $directoryDocPaths) {
        $directoryDocWritePayload = @{
            tool_name = 'Write'
            cwd = $plainRepo
            tool_input = @{
                file_path = (Join-Path $plainRepo $directoryDocPath)
                content = '# Formal Document'
            }
        } | ConvertTo-Json -Depth 8 -Compress
        $directoryDocWriteOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $directoryDocWritePayload 2
        $directoryDocWritePassed = $directoryDocWriteOutput -match 'Formal document write blocked before requirements clarification PASS'
        Add-Check ([ref]$summary) "formal-document-write-directory-path-blocked-$($directoryDocPath.Replace('/', '-').Replace('.', '-'))" $directoryDocWritePassed $directoryDocWriteOutput
    }

    $docWritePayloadAllowed = @{
        tool_name = 'Write'
        cwd = $tempRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = $workflowId
                changeSnapshot = $changeSnapshot
                worktree = $tempRepo
            }
            file_path = (Join-Path $tempRepo 'openspec/changes/portable-formal-gates-canary/proposal.md')
            content = '# Proposal'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $docWriteAllowedOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $docWritePayloadAllowed 0
    Add-Check ([ref]$summary) 'formal-document-write-after-clarification-allowed' $true $docWriteAllowedOutput

    $applyPatchAllowedPayload = @{
        tool_name = 'apply_patch'
        cwd = $tempRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = $workflowId
                changeSnapshot = $changeSnapshot
                worktree = $tempRepo
            }
            command = @'
*** Begin Patch
*** Update File: openspec/changes/portable-formal-gates-canary/proposal.md
@@
+# Proposal
*** End Patch
'@
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $applyPatchAllowedOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $applyPatchAllowedPayload 0
    Add-Check ([ref]$summary) 'formal-document-apply-patch-covered-target-allowed' $true $applyPatchAllowedOutput

    $uncoveredDocWritePayload = @{
        tool_name = 'Write'
        cwd = $tempRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = $workflowId
                changeSnapshot = $changeSnapshot
                worktree = $tempRepo
            }
            file_path = (Join-Path $tempRepo 'docs/prd/unrelated-new-prd.md')
            content = '# Unrelated PRD'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $uncoveredDocWriteOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $uncoveredDocWritePayload 2
    $uncoveredDocWritePassed = $uncoveredDocWriteOutput -match 'targetNotCovered'
    Add-Check ([ref]$summary) 'formal-document-write-uncovered-target-blocked' $uncoveredDocWritePassed $uncoveredDocWriteOutput

    $uncoveredShellWritePayload = @{
        tool_name = 'Shell'
        cwd = $tempRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = $workflowId
                changeSnapshot = $changeSnapshot
                worktree = $tempRepo
            }
            command = 'Set-Content -LiteralPath "docs/requirements/uncovered.md" -Value "# Requirement"'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $uncoveredShellWriteOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $uncoveredShellWritePayload 2
    $uncoveredShellWritePassed = $uncoveredShellWriteOutput -match 'targetNotCovered'
    Add-Check ([ref]$summary) 'formal-document-shell-uncovered-target-blocked' $uncoveredShellWritePassed $uncoveredShellWriteOutput

    $coveredShellWritePayload = @{
        tool_name = 'Shell'
        cwd = $tempRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = $workflowId
                changeSnapshot = $changeSnapshot
                worktree = $tempRepo
            }
            command = 'Set-Content -LiteralPath "openspec/changes/portable-formal-gates-canary/proposal.md" -Value "# Proposal"'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $coveredShellWriteOutput = Run-PowerShellStdinExpect $tempRepo $hookScript $coveredShellWritePayload 0
    Add-Check ([ref]$summary) 'formal-document-shell-covered-target-allowed' $true $coveredShellWriteOutput

    $joinPathShellPayload = @{
        tool_name = 'Shell'
        cwd = $plainRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = 'wf-join-path-write'
                changeSnapshot = 'snap-join-path-write'
                worktree = $plainRepo
            }
            command = 'Set-Content -LiteralPath (Join-Path "openspec" "changes/x/proposal.md") -Value "# Proposal"'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $joinPathShellOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $joinPathShellPayload 2
    $joinPathShellPassed = $joinPathShellOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-shell-join-path-write-blocked' $joinPathShellPassed $joinPathShellOutput

    $pythonInlinePayload = @{
        tool_name = 'Shell'
        cwd = $plainRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = 'wf-python-inline-write'
                changeSnapshot = 'snap-python-inline-write'
                worktree = $plainRepo
            }
            command = 'python -c "from pathlib import Path; Path(''openspec/changes/x/proposal.md'').write_text(''# Proposal'')"'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $pythonInlineOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $pythonInlinePayload 2
    $pythonInlinePassed = $pythonInlineOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-python-inline-write-blocked' $pythonInlinePassed $pythonInlineOutput

    $nodeInlinePayload = @{
        tool_name = 'Shell'
        cwd = $plainRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = 'wf-node-inline-write'
                changeSnapshot = 'snap-node-inline-write'
                worktree = $plainRepo
            }
            command = 'node -e "require(''fs'').writeFileSync(''openspec/changes/x/proposal.md'', ''# Proposal'')"'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $nodeInlineOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $nodeInlinePayload 2
    $nodeInlinePassed = $nodeInlineOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-node-inline-write-blocked' $nodeInlinePassed $nodeInlineOutput

    $dotNetInlinePayload = @{
        tool_name = 'Shell'
        cwd = $plainRepo
        tool_input = @{
            GateWorkflow = @{
                gate = 'requirements-clarification-gate'
                workflowId = 'wf-dotnet-inline-write'
                changeSnapshot = 'snap-dotnet-inline-write'
                worktree = $plainRepo
            }
            command = '[System.IO.File]::WriteAllText((Join-Path "openspec" "changes/x/proposal.md"), "# Proposal")'
        }
    } | ConvertTo-Json -Depth 8 -Compress
    $dotNetInlineOutput = Run-PowerShellStdinExpect $plainRepo $hookScript $dotNetInlinePayload 2
    $dotNetInlinePassed = $dotNetInlineOutput -match 'Formal document write blocked before requirements clarification PASS'
    Add-Check ([ref]$summary) 'formal-document-dotnet-writealltext-blocked' $dotNetInlinePassed $dotNetInlineOutput

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

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/qa-test-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: hash-qa-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRef
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

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/qa-test-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: manual-final-qa-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRef
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

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
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
    $negativePassed = $negativeOutput -match 'PASS blocked|review artifact is incomplete|Reviewer agent id'
    Add-Check ([ref]$summary) 'invalid-pass-artifact-blocked' $negativePassed $negativeOutput
    $summary.artifactPaths.invalidComplexity = Format-Path $invalidArtifactPath

    $contaminatedArtifactRel = '.claude/gates/artifacts/contaminated-complexity-pass.md'
    $contaminatedArtifactPath = Join-Path $tempRepo $contaminatedArtifactRel
    New-FormalArtifact $contaminatedArtifactPath 'Contaminated Complexity Gate' 'contaminated-complexity-agent' $bundleRef @(
        'Script result: PASS',
        'Diff shape judgment: contaminated prompt negative canary',
        'Impact surface health: contaminated prompt negative canary',
        'Public/config surface: contaminated prompt negative canary',
        'New concepts: contaminated prompt negative canary',
        'Shrink opportunities: contaminated prompt negative canary',
        'Decision evidence: contaminated prompt negative canary',
        'Known issues: main agent supplied previous findings',
        "Changed files artifact: $changedFilesRel",
        "Verification artifact: $verificationRel"
    )
    $contaminatedOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $contaminatedArtifactRel,
        '-Actor', 'negative-contaminated-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $contaminatedPassed = $contaminatedOutput -match 'prompt contamination'
    Add-Check ([ref]$summary) 'contaminated-review-pass-blocked' $contaminatedPassed $contaminatedOutput
    $summary.artifactPaths.contaminatedComplexity = Format-Path $contaminatedArtifactPath

    $missingPromptFieldsRel = '.claude/gates/artifacts/missing-prompt-fields-complexity-pass.md'
    $missingPromptFieldsPath = Join-Path $tempRepo $missingPromptFieldsRel
    Set-Utf8File $missingPromptFieldsPath @"
# Complexity Gate

Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: missing-prompt-fields-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRef
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: missing prompt fields negative canary
Impact surface health: missing prompt fields negative canary
Public/config surface: missing prompt fields negative canary
New concepts: missing prompt fields negative canary
Shrink opportunities: missing prompt fields negative canary
Decision evidence: missing prompt fields negative canary
Changed files artifact: $changedFilesRel
Verification artifact: $verificationRel

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $missingPromptFieldsOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $missingPromptFieldsRel,
        '-Actor', 'negative-missing-prompt-fields-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $missingPromptFieldsPassed = $missingPromptFieldsOutput -match 'Review mode' -and $missingPromptFieldsOutput -match 'Prompt contamination check' -and $missingPromptFieldsOutput -match 'Prompt source'
    Add-Check ([ref]$summary) 'prompt-integrity-fields-required-blocked' $missingPromptFieldsPassed $missingPromptFieldsOutput
    $summary.artifactPaths.missingPromptFields = Format-Path $missingPromptFieldsPath

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

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: route-mismatch-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRef
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

    $missingDispatchArtifactRel = '.claude/gates/artifacts/missing-dispatch-prompt-complexity-pass.md'
    $missingDispatchArtifactPath = Join-Path $tempRepo $missingDispatchArtifactRel
    Set-Utf8File $missingDispatchArtifactPath @"
# Complexity Gate

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: missing-dispatch-prompt-agent
Context bundle: $bundleRef
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: missing dispatch prompt negative canary
Impact surface health: missing dispatch prompt negative canary
Public/config surface: missing dispatch prompt negative canary
New concepts: missing dispatch prompt negative canary
Shrink opportunities: missing dispatch prompt negative canary
Decision evidence: missing dispatch prompt negative canary
Changed files artifact: $changedFilesRel
Verification artifact: $verificationRel

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $missingDispatchOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $missingDispatchArtifactRel,
        '-Actor', 'negative-missing-dispatch-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $missingDispatchPassed = $missingDispatchOutput -match 'Dispatch prompt artifact'
    Add-Check ([ref]$summary) 'dispatch-prompt-artifact-required-blocked' $missingDispatchPassed $missingDispatchOutput
    $summary.artifactPaths.missingDispatchPrompt = Format-Path $missingDispatchArtifactPath

    $badDispatchHashArtifactRel = '.claude/gates/artifacts/bad-dispatch-hash-complexity-pass.md'
    $badDispatchHashArtifactPath = Join-Path $tempRepo $badDispatchHashArtifactRel
    Set-Utf8File $badDispatchHashArtifactPath @"
# Complexity Gate

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: bad-dispatch-hash-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRel sha256=0000000000000000000000000000000000000000000000000000000000000000
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: bad dispatch hash negative canary
Impact surface health: bad dispatch hash negative canary
Public/config surface: bad dispatch hash negative canary
New concepts: bad dispatch hash negative canary
Shrink opportunities: bad dispatch hash negative canary
Decision evidence: bad dispatch hash negative canary
Changed files artifact: $changedFilesRel
Verification artifact: $verificationRel

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
    $badDispatchHashOutput = Run-PowerShellExpect $tempRepo @(
        '-File', $workflowScript,
        '-Action', 'record-stage',
        '-Worktree', $tempRepo,
        '-Gate', 'complexity-gate',
        '-Verdict', 'PASS',
        '-Mode', 'formal',
        '-Artifact', $badDispatchHashArtifactRel,
        '-Actor', 'negative-dispatch-hash-agent',
        '-WorkflowId', $workflowId,
        '-ChangeSnapshot', $changeSnapshot
    ) 1
    $badDispatchHashPassed = $badDispatchHashOutput -match 'Dispatch prompt artifact sha256 mismatch'
    Add-Check ([ref]$summary) 'dispatch-prompt-hash-mismatch-blocked' $badDispatchHashPassed $badDispatchHashOutput
    $summary.artifactPaths.badDispatchPromptHash = Format-Path $badDispatchHashArtifactPath

    $anchoredDispatchCases = @(
        [pscustomobject]@{ Name = 'list-focus-items'; Line = '- Focus items: verify the issue the main agent already suspects'; Expected = 'Focus items' },
        [pscustomobject]@{ Name = 'quote-what-to-verify'; Line = '> what to verify: confirm the main agent suspicion'; Expected = 'what to verify' },
        [pscustomobject]@{ Name = 'heading-focus-items'; Line = '### Focus items: verify the issue the main agent already suspects'; Expected = 'Focus items' },
        [pscustomobject]@{ Name = 'quote-chinese-focus'; Line = '> ' + [string]::new([char[]]@([char]0x91CD, [char]0x70B9, [char]0x590D, [char]0x67E5)) + ': ' + [string]::new([char[]]@([char]0x4E3B, [char]0x4EE3, [char]0x7406, [char]0x6307, [char]0x5B9A, [char]0x7684, [char]0x98CE, [char]0x9669)); Expected = [string]::new([char[]]@([char]0x91CD, [char]0x70B9, [char]0x590D, [char]0x67E5)) }
    )
    foreach ($anchoredCase in $anchoredDispatchCases) {
        $anchoredDispatchPromptRel = ".claude/gates/artifacts/anchored-dispatch-prompt-$($anchoredCase.Name).txt"
        $anchoredDispatchPromptPath = Join-Path $tempRepo $anchoredDispatchPromptRel
        Set-Utf8File $anchoredDispatchPromptPath @"
Worktree: portable canary repo
$($anchoredCase.Line)
Output template: formal gate artifact
"@
        $anchoredDispatchPromptHash = Get-Sha256 $anchoredDispatchPromptPath
        $anchoredDispatchArtifactRel = ".claude/gates/artifacts/anchored-dispatch-prompt-$($anchoredCase.Name)-complexity-pass.md"
        $anchoredDispatchArtifactPath = Join-Path $tempRepo $anchoredDispatchArtifactRel
        Set-Utf8File $anchoredDispatchArtifactPath @"
# Complexity Gate

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/complexity-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: anchored-dispatch-prompt-$($anchoredCase.Name)-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $anchoredDispatchPromptRel sha256=$anchoredDispatchPromptHash
No-anchor prompt: YES
Script result: PASS
Diff shape judgment: anchored dispatch prompt $($anchoredCase.Name) negative canary
Impact surface health: anchored dispatch prompt $($anchoredCase.Name) negative canary
Public/config surface: anchored dispatch prompt $($anchoredCase.Name) negative canary
New concepts: anchored dispatch prompt $($anchoredCase.Name) negative canary
Shrink opportunities: anchored dispatch prompt $($anchoredCase.Name) negative canary
Decision evidence: anchored dispatch prompt $($anchoredCase.Name) negative canary
Changed files artifact: $changedFilesRel
Verification artifact: $verificationRel

gate_route:
  workflow_id: $workflowId
  change_snapshot: $changeSnapshot
  next_action: proceed
  rework_owner: none
  rerun_from: none
"@
        $anchoredDispatchOutput = Run-PowerShellExpect $tempRepo @(
            '-File', $workflowScript,
            '-Action', 'record-stage',
            '-Worktree', $tempRepo,
            '-Gate', 'complexity-gate',
            '-Verdict', 'PASS',
            '-Mode', 'formal',
            '-Artifact', $anchoredDispatchArtifactRel,
            '-Actor', "negative-anchored-dispatch-$($anchoredCase.Name)-agent",
            '-WorkflowId', $workflowId,
            '-ChangeSnapshot', $changeSnapshot
        ) 1
        $anchoredDispatchPassed = $anchoredDispatchOutput -match 'dispatch prompt contamination' -and $anchoredDispatchOutput -match [regex]::Escape([string]$anchoredCase.Expected)
        Add-Check ([ref]$summary) "dispatch-prompt-anchoring-field-$($anchoredCase.Name)-blocked" $anchoredDispatchPassed $anchoredDispatchOutput
        $summary.artifactPaths["anchoredDispatchPrompt_$($anchoredCase.Name)"] = Format-Path $anchoredDispatchArtifactPath
    }

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

Review mode: ZERO_CONTEXT_FORMAL
Prompt contamination check: PASS
Semantic anti-anchor check: PASS
Prompt source: agents/qa-test-gate.md
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id: conditional-qa-agent
Context bundle: $bundleRef
Dispatch prompt artifact: $dispatchPromptRef
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
