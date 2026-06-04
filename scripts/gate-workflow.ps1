param(
    [ValidateSet('record-stage', 'verify-admission', 'snapshot', 'record-final-verification', 'help')]
    [string]$Action = 'help',

    [string]$Worktree = (Get-Location).Path,
    [string]$StatePath,
    [string]$ManifestPath,
    [string]$Gate,
    [string]$Verdict = 'PASS',
    [string]$Mode,
    [string]$Stage,
    [string]$Artifact,
    [string]$Actor = 'gate-workflow',
    [string]$Reason,
    [string]$WorkflowId,
    [string]$ChangeSnapshot,
    [string]$BaseRef,
    [string]$HeadRef = 'HEAD',
    [switch]$IncludeWorkingTree,
    [string]$AttemptId,
    [string]$AttemptsJson,
    [string]$OutputArtifact,
    [string]$FinalQaArtifact,
    [switch]$RecordFinalQa,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'

function Show-Usage {
    @'
gate-workflow.ps1 -Action <record-stage|verify-admission|snapshot|record-final-verification> [options]

Generic helper around gate-state.ps1. It resolves the worktree/state path, verifies artifacts,
records state, and writes machine-readable final verification attempt aggregates.

Examples:
  pwsh <formal-gates>/scripts/gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate complexity-gate -WorkflowId wf -ChangeSnapshot snap
  pwsh <formal-gates>/scripts/gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate complexity-gate -Verdict PASS -Artifact .claude/gates/artifacts/scope.txt -WorkflowId wf -ChangeSnapshot snap
  pwsh <formal-gates>/scripts/gate-workflow.ps1 -Action snapshot -Worktree <repo> -BaseRef main -HeadRef HEAD
'@
}

if ($Help -or $Action -eq 'help') {
    Show-Usage
    exit 0
}

function Format-GatePath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    try {
        $full = [System.IO.Path]::GetFullPath($Path)
        $cwd = [System.IO.Path]::GetFullPath((Get-Location).Path).TrimEnd('\', '/')
        if ($full.StartsWith($cwd, [System.StringComparison]::OrdinalIgnoreCase)) {
            $suffix = $full.Substring($cwd.Length).TrimStart('\', '/')
            if ([string]::IsNullOrWhiteSpace($suffix)) { return '.' }
            return $suffix.Replace('\', '/')
        }
        $homeRoot = if (-not [string]::IsNullOrWhiteSpace($HOME)) { $HOME } else { $env:USERPROFILE }
        if (-not [string]::IsNullOrWhiteSpace($homeRoot)) {
            $home = [System.IO.Path]::GetFullPath($homeRoot).TrimEnd('\', '/')
            if ($full.StartsWith($home, [System.StringComparison]::OrdinalIgnoreCase)) {
                return ('~' + $full.Substring($home.Length)).Replace('\', '/')
            }
        }
        return $full.Replace('\', '/')
    }
    catch {
        return $Path.Replace('\', '/')
    }
}

function Resolve-GateStateScript([string]$Repo) {
    $bundled = Join-Path $PSScriptRoot 'gate-state.ps1'
    if (Test-Path -LiteralPath $bundled) { return $bundled }
    throw "Bundled formal-gates gate-state.ps1 was not found next to gate-workflow.ps1: $bundled"
}

function Resolve-StatePath([string]$Repo, [string]$Path) {
    if (-not [string]::IsNullOrWhiteSpace($Path)) {
        if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
        return Join-Path $Repo $Path
    }
    return Join-Path $Repo '.claude/gates/gate-state.json'
}

function Resolve-ArtifactPath([string]$Repo, [string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
    return Join-Path $Repo $Path
}

function Get-ArtifactFieldValue([string]$Text, [string]$FieldName) {
    $match = [regex]::Match($Text, "(?im)^[ \t]*" + [regex]::Escape($FieldName) + "[ \t]*:[ \t]*(.*?)[ \t]*$")
    if (-not $match.Success) { return $null }
    return $match.Groups[1].Value.Trim()
}

function Test-MeaningfulArtifactField([string]$Text, [string]$FieldName) {
    $value = Get-ArtifactFieldValue $Text $FieldName
    if ([string]::IsNullOrWhiteSpace($value)) { return $false }
    if ($value -match '[<>]') { return $false }
    return $value -notmatch '(?i)^(unavailable|unknown|none|null|n/a|na|todo|tbd|placeholder|sample|example)$'
}

function Test-ContextBundleExists([string]$Repo, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    $candidates = @()
    foreach ($part in ($Value -split ',')) {
        $trimmed = $part.Trim().Trim('"', "'")
        if ([string]::IsNullOrWhiteSpace($trimmed)) { continue }
        $candidates += $trimmed
        $candidates += ($trimmed -replace '\s+sha(256)?\s*[:=].*$', '').Trim()
        $candidates += ($trimmed -replace '\s+\(.*$', '').Trim()
    }
    foreach ($candidate in ($candidates | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)) {
        $path = if ([System.IO.Path]::IsPathRooted($candidate)) { $candidate } else { Join-Path $Repo $candidate }
        if (Test-Path -LiteralPath $path) { return $true }
    }
    return $false
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-ContextBundleValidationErrors([string]$Repo, [string]$Value) {
    $errors = @()
    if ([string]::IsNullOrWhiteSpace($Value)) {
        return @('Context bundle: <non-empty bundle path>')
    }
    foreach ($part in ($Value -split ',')) {
        $trimmed = $part.Trim().Trim('"', "'")
        if ([string]::IsNullOrWhiteSpace($trimmed)) { continue }
        $shaMatch = [regex]::Match($trimmed, '(?i)\bsha(?:256)?\s*[:=]\s*([a-f0-9]{64})\b')
        $pathText = [regex]::Replace($trimmed, '(?i)\s+sha(?:256)?\s*[:=]\s*[a-f0-9]{64}\b', '').Trim()
        $pathText = [regex]::Replace($pathText, '\s+\(.*$', '').Trim()
        if ([string]::IsNullOrWhiteSpace($pathText)) {
            $errors += 'Context bundle path is empty'
            continue
        }
        $path = Resolve-ArtifactPath $Repo $pathText
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            $errors += "Context bundle path does not exist: $pathText"
            continue
        }
        if (-not $shaMatch.Success) {
            $errors += "Context bundle sha256 missing: $pathText"
            continue
        }
        $expected = $shaMatch.Groups[1].Value.ToLowerInvariant()
        $actual = Get-Sha256 $path
        if ($actual -ne $expected) {
            $errors += "Context bundle sha256 mismatch: $pathText"
        }
    }
    if ($errors.Count -eq 0 -and [string]::IsNullOrWhiteSpace(($Value -split ',' | Where-Object { -not [string]::IsNullOrWhiteSpace($_.Trim()) } | Select-Object -First 1))) {
        $errors += 'Context bundle: <non-empty bundle path>'
    }
    return $errors
}

function Test-ArtifactReferenceExists([string]$Repo, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $false }
    $path = Resolve-ArtifactPath $Repo $pathText
    return (Test-Path -LiteralPath $path -PathType Leaf)
}

function Get-ArtifactReferencePath([string]$Repo, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $null }
    return Resolve-ArtifactPath $Repo $pathText
}

function Test-AnyMeaningfulArtifactField([string]$Text, [string[]]$FieldNames) {
    foreach ($field in $FieldNames) {
        if (Test-MeaningfulArtifactField $Text $field) { return $true }
    }
    return $false
}

function Get-ImplementationEvidenceMissing([string]$Text, [string]$GateName, [string]$Repo) {
    if ($GateName -notin @('complexity-gate', 'architecture-health-gate', 'code-quality-gate')) {
        return @()
    }
    $missing = @()
    if (-not (Test-AnyMeaningfulArtifactField $Text @('Raw diff artifact', 'Changed files artifact'))) {
        $missing += 'Raw diff artifact or Changed files artifact'
    }
    else {
        $diffValue = Get-ArtifactFieldValue $Text 'Raw diff artifact'
        if ([string]::IsNullOrWhiteSpace($diffValue)) { $diffValue = Get-ArtifactFieldValue $Text 'Changed files artifact' }
        if (-not (Test-ArtifactReferenceExists $Repo $diffValue)) { $missing += 'Raw diff/changed-files artifact path must exist' }
    }
    if (-not (Test-AnyMeaningfulArtifactField $Text @('Developer self-test artifact', 'Verification artifact'))) {
        $missing += 'Developer self-test artifact or Verification artifact'
    }
    else {
        $verificationValue = Get-ArtifactFieldValue $Text 'Developer self-test artifact'
        if ([string]::IsNullOrWhiteSpace($verificationValue)) { $verificationValue = Get-ArtifactFieldValue $Text 'Verification artifact' }
        if (-not (Test-ArtifactReferenceExists $Repo $verificationValue)) { $missing += 'Developer self-test/verification artifact path must exist' }
    }
    return $missing
}

function Get-QaEvidenceMissing([string]$Text, [string]$GateName) {
    if ($GateName -ne 'qa-test-gate') { return @() }
    $missing = @()
    foreach ($field in @('Approved case set', 'QA-owned evidence', 'Case-to-artifact binding')) {
        if (-not (Test-MeaningfulArtifactField $Text $field)) {
            $missing += "${field}: <non-empty>"
        }
    }
    return $missing
}

function Get-FinalExecutionEvidenceMissing([string]$Text, [string]$GateName, [string]$StageValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$Repo) {
    if ($GateName -ne 'qa-test-gate' -or $StageValue -ne 'FinalExecution') { return @() }
    $missing = @()
    $evidenceValue = Get-ArtifactFieldValue $Text 'QA-owned evidence'
    $evidencePath = Get-ArtifactReferencePath $Repo $evidenceValue
    if ([string]::IsNullOrWhiteSpace($evidencePath) -or -not (Test-Path -LiteralPath $evidencePath -PathType Leaf)) {
        return @('FinalExecution QA-owned evidence must point to an existing final verification aggregate')
    }
    try {
        $aggregate = Get-Content -LiteralPath $evidencePath -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        return @("FinalExecution QA-owned evidence is not valid JSON: $evidenceValue")
    }
    if ([string]$aggregate.status -ne 'PASS') { $missing += 'FinalExecution final verification aggregate status must be PASS' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and [string]$aggregate.workflowId -ne $ExpectedWorkflowId) {
        $missing += 'FinalExecution final verification aggregate workflowId must match WorkflowId'
    }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -and [string]$aggregate.changeSnapshot -ne $ExpectedChangeSnapshot) {
        $missing += 'FinalExecution final verification aggregate changeSnapshot must match ChangeSnapshot'
    }
    $acceptedCount = @($aggregate.acceptedAttempts).Count
    if ($acceptedCount -lt 1) { $missing += 'FinalExecution final verification aggregate needs at least one accepted PASS attempt' }
    return $missing
}

function Get-GateRouteFieldValue([string]$Text, [string]$FieldName) {
    $lines = $Text -split "`r?`n"
    $inside = $false
    foreach ($line in $lines) {
        if ($line -match '^[ \t]*gate_route[ \t]*:[ \t]*$') {
            $inside = $true
            continue
        }
        if (-not $inside) { continue }
        if ($line -match '^[^ \t#][^:]*:') { break }
        $match = [regex]::Match($line, '^[ \t]+' + [regex]::Escape($FieldName) + '[ \t]*:[ \t]*(.*?)[ \t]*$')
        if ($match.Success) {
            return $match.Groups[1].Value.Trim().Trim('"', "'")
        }
    }
    return $null
}

function Test-AllowedGateRouteValue([string]$Value, [string[]]$AllowedValues) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    foreach ($allowed in $AllowedValues) {
        if ($Value -ieq $allowed) { return $true }
    }
    return $false
}

function Get-GateRouteMissingForPass([string]$Text, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$GateName, [string]$StageValue) {
    $missing = @()
    $workflow = Get-GateRouteFieldValue $Text 'workflow_id'
    $snapshot = Get-GateRouteFieldValue $Text 'change_snapshot'
    $nextAction = Get-GateRouteFieldValue $Text 'next_action'
    $reworkOwner = Get-GateRouteFieldValue $Text 'rework_owner'
    $rerunFrom = Get-GateRouteFieldValue $Text 'rerun_from'
    if ([string]::IsNullOrWhiteSpace($workflow)) { $missing += 'gate_route.workflow_id' }
    if ([string]::IsNullOrWhiteSpace($snapshot)) { $missing += 'gate_route.change_snapshot' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and $workflow -ne $ExpectedWorkflowId) { $missing += 'gate_route.workflow_id must match WorkflowId' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -and $snapshot -ne $ExpectedChangeSnapshot) { $missing += 'gate_route.change_snapshot must match ChangeSnapshot' }
    $isSealStage = $GateName -eq 'qa-test-gate' -and $StageValue -eq 'FinalExecution'
    if ($isSealStage) {
        if (-not (Test-AllowedGateRouteValue $nextAction @('seal'))) { $missing += 'gate_route.next_action must be seal for FinalExecution PASS' }
    }
    elseif (-not (Test-AllowedGateRouteValue $nextAction @('proceed'))) {
        $missing += 'gate_route.next_action must be proceed for non-final PASS'
    }
    if (-not (Test-AllowedGateRouteValue $reworkOwner @('none'))) { $missing += 'gate_route.rework_owner must be none for PASS' }
    if (-not (Test-AllowedGateRouteValue $rerunFrom @('none'))) { $missing += 'gate_route.rerun_from must be none for PASS' }
    return $missing
}

function Test-ArtifactFields([string]$ArtifactPath, [string[]]$RequiredFields, [string]$Repo, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$GateName, [string]$StageValue) {
    if (-not (Test-Path -LiteralPath $ArtifactPath)) {
        return [pscustomobject]@{ Ok = $false; Missing = @("artifact missing: $(Format-GatePath $ArtifactPath)") }
    }
    $text = [string](Get-Content -LiteralPath $ArtifactPath -Raw)
    $missing = @($RequiredFields | Where-Object { $text -notmatch [regex]::Escape($_) })
    if (-not (Test-MeaningfulArtifactField $text 'Reviewer agent id')) {
        $missing += 'Reviewer agent id: <non-empty independent agent id>'
    }
    if (-not (Test-MeaningfulArtifactField $text 'Context bundle')) {
        $missing += 'Context bundle: <non-empty bundle path>'
    }
    else {
        $missing += @(Get-ContextBundleValidationErrors $Repo (Get-ArtifactFieldValue $text 'Context bundle'))
    }
    $missing += @(Get-QaEvidenceMissing $text $GateName)
    $missing += @(Get-FinalExecutionEvidenceMissing $text $GateName $StageValue $ExpectedWorkflowId $ExpectedChangeSnapshot $Repo)
    $missing += @(Get-ImplementationEvidenceMissing $text $GateName $Repo)
    $missing += @(Get-GateRouteMissingForPass $text $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue)
    return [pscustomobject]@{ Ok = ($missing.Count -eq 0); Missing = $missing }
}

function Get-FormalGatePassRequiredFields([string]$GateName) {
    $zeroContextFields = @(
        'Zero-context reviewer: YES',
        'Independent agent: YES',
        'Reviewer agent id:',
        'Context bundle:',
        'No-anchor prompt: YES',
        'gate_route:'
    )
    if ($GateName -eq 'complexity-gate') {
        return @(
            'Script result',
            'Diff shape judgment',
            'Impact surface health',
            'Public/config surface',
            'New concepts',
            'Shrink opportunities',
            'Decision evidence'
        ) + $zeroContextFields
    }
    if ($GateName -in @('qa-test-gate', 'architecture-health-gate', 'code-quality-gate')) {
        if ($GateName -eq 'qa-test-gate') {
            return @(
                'Approved case set:',
                'QA-owned evidence:',
                'Case-to-artifact binding:'
            ) + $zeroContextFields
        }
        return $zeroContextFields
    }
    if ([string]::IsNullOrWhiteSpace($GateName)) { return @() }
    return $zeroContextFields
}

function Assert-FormalPassArtifact([string]$GateName, [string]$ArtifactPath, [string]$Repo, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$StageValue) {
    $requiredFields = @(Get-FormalGatePassRequiredFields $GateName)
    if ($requiredFields.Count -eq 0) { return }
    $check = Test-ArtifactFields $ArtifactPath $requiredFields $Repo $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue
    if (-not $check.Ok) {
        Write-Host "$GateName PASS blocked: artifact lacks required formal independent zero-context review fields: $($check.Missing -join ', ')"
        exit 1
    }
}

function Write-FinalQaArtifact([string]$Path, [string]$Status, [object[]]$Attempts, [object[]]$AcceptedAttempts) {
    $reviewerIds = @($AcceptedAttempts | ForEach-Object { [string]$_.reviewerAgentId } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)
    $contextBundles = @($AcceptedAttempts | ForEach-Object { [string]$_.contextBundle } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)
    $reviewerId = if ($reviewerIds.Count -gt 0) { $reviewerIds -join ', ' } else { 'unavailable' }
    $contextBundle = if ($contextBundles.Count -gt 0) { $contextBundles -join ', ' } else { 'unavailable' }
    $attemptSummary = @($Attempts | ForEach-Object {
        "- status=$($_.status) accepted=$($_.accepted) artifact=$($_.artifact)"
    })
    $routeNextAction = if ($Status -eq 'PASS') { 'seal' } else { 'blocked' }
    $routeReworkOwner = if ($Status -eq 'PASS') { 'none' } else { 'qa-cases' }
    $routeRerunFrom = if ($Status -eq 'PASS') { 'none' } else { 'final-verification' }
    $acceptedArtifacts = @($AcceptedAttempts | ForEach-Object { [string]$_.artifact } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $caseBinding = if ($acceptedArtifacts.Count -gt 0) { $acceptedArtifacts -join ', ' } else { 'no accepted PASS attempts' }

    $content = @(
        '# Final QA Execution'
        ''
        'Zero-context reviewer: YES'
        'Independent agent: YES'
        "Reviewer agent id: $reviewerId"
        "Context bundle: $contextBundle"
        'No-anchor prompt: YES'
        ''
        "Workflow id: $WorkflowId"
        "Change snapshot: $ChangeSnapshot"
        "Final verification status: $Status"
        'Approved case set: final verification accepted attempts'
        "QA-owned evidence: $OutputArtifact"
        "Case-to-artifact binding: $caseBinding"
        ''
        'gate_route:'
        "  workflow_id: $WorkflowId"
        "  change_snapshot: $ChangeSnapshot"
        "  next_action: $routeNextAction"
        "  rework_owner: $routeReworkOwner"
        "  rerun_from: $routeRerunFrom"
        ''
        '## Attempts'
    ) + $attemptSummary

    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent) -and -not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Path $parent | Out-Null
    }
    $content -join "`n" | Set-Content -LiteralPath $Path -Encoding UTF8
}

function Assert-AcceptedFinalVerificationAttempts([object[]]$AcceptedAttempts, [string]$Repo, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot) {
    foreach ($attempt in $AcceptedAttempts) {
        $artifactValue = [string]$attempt.artifact
        if ([string]::IsNullOrWhiteSpace($artifactValue)) {
            Write-Host 'GATE_WORKFLOW_BLOCKED finalVerificationAcceptedAttemptMissingArtifact=true'
            exit 1
        }
        $contextBundleErrors = @(Get-ContextBundleValidationErrors $Repo ([string]$attempt.contextBundle))
        if ($contextBundleErrors.Count -gt 0) {
            Write-Host "GATE_WORKFLOW_BLOCKED finalVerificationAcceptedAttemptContextBundleInvalid=$($contextBundleErrors -join ', ')"
            exit 1
        }
        $attemptPath = Resolve-ArtifactPath $Repo $artifactValue
        if (-not (Test-Path -LiteralPath $attemptPath)) {
            Write-Host "GATE_WORKFLOW_BLOCKED finalVerificationAcceptedAttemptArtifactMissing=$(Format-GatePath $attemptPath)"
            exit 1
        }
        $attemptText = [string](Get-Content -LiteralPath $attemptPath -Raw)
        if ([string]::IsNullOrWhiteSpace($attemptText)) {
            Write-Host "GATE_WORKFLOW_BLOCKED finalVerificationAcceptedAttemptArtifactEmpty=$(Format-GatePath $attemptPath)"
            exit 1
        }
        $routeWorkflow = Get-GateRouteFieldValue $attemptText 'workflow_id'
        $routeSnapshot = Get-GateRouteFieldValue $attemptText 'change_snapshot'
        if (-not [string]::IsNullOrWhiteSpace($routeWorkflow) -and $routeWorkflow -ne $ExpectedWorkflowId) {
            Write-Host "GATE_WORKFLOW_BLOCKED finalVerificationAttemptWorkflowMismatch artifact=$(Format-GatePath $attemptPath)"
            exit 1
        }
        if (-not [string]::IsNullOrWhiteSpace($routeSnapshot) -and $routeSnapshot -ne $ExpectedChangeSnapshot) {
            Write-Host "GATE_WORKFLOW_BLOCKED finalVerificationAttemptSnapshotMismatch artifact=$(Format-GatePath $attemptPath)"
            exit 1
        }
    }
}

function Invoke-GateState([string[]]$Arguments, [string]$Repo) {
    $gateState = Resolve-GateStateScript $Repo
    $stdoutPath = [System.IO.Path]::GetTempFileName()
    $stderrPath = [System.IO.Path]::GetTempFileName()
    try {
        $processArgs = @('-NoProfile', '-File', $gateState) + @($Arguments)
        $process = Start-Process -FilePath 'pwsh' -ArgumentList $processArgs -WorkingDirectory $Repo -NoNewWindow -PassThru -Wait -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $stdout = Get-Content -LiteralPath $stdoutPath -Raw -ErrorAction SilentlyContinue
        $stderr = Get-Content -LiteralPath $stderrPath -Raw -ErrorAction SilentlyContinue
        $output = (($stdout, $stderr) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) -join "`n"
        if (-not [string]::IsNullOrWhiteSpace($output)) { Write-Host $output }
        return $process.ExitCode
    }
    finally {
        Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-GitText([string]$Repo, [string[]]$Arguments) {
    $output = & git -C $Repo @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw (($output | ForEach-Object { [string]$_ }) -join "`n")
    }
    return (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
}

function Get-TreeHash([string]$Text) {
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        return (($sha.ComputeHash($bytes) | ForEach-Object { $_.ToString('x2') }) -join '')
    }
    finally {
        $sha.Dispose()
    }
}

function Get-UntrackedContentDigest([string]$Repo) {
    $output = & git -C $Repo ls-files --others --exclude-standard 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw (($output | ForEach-Object { [string]$_ }) -join "`n")
    }
    $entries = @()
    foreach ($relative in @($output | ForEach-Object { [string]$_ } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Sort-Object)) {
        $path = Join-Path $Repo $relative
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        $entries += "$relative sha256=$(Get-Sha256 $path)"
    }
    return ($entries -join "`n")
}

function New-Snapshot($Repo, [string]$Base, [string]$Head, [bool]$IncludeDirty) {
    if ([string]::IsNullOrWhiteSpace($Base)) { throw 'BaseRef is required for snapshot.' }
    if ([string]::IsNullOrWhiteSpace($Head)) { $Head = 'HEAD' }

    $baseCommit = Invoke-GitText $Repo @('rev-parse', $Base)
    $headCommit = Invoke-GitText $Repo @('rev-parse', $Head)
    $status = Invoke-GitText $Repo @('status', '--short')
    if (-not [string]::IsNullOrWhiteSpace($status) -and -not $IncludeDirty) {
        Write-Host "GATE_WORKFLOW_BLOCKED dirtyWorktree=true worktree=$(Format-GatePath $Repo)"
        exit 1
    }

    $rangeDiff = Invoke-GitText $Repo @('diff', '--binary', "$baseCommit..$headCommit")
    $rangeHash = Get-TreeHash $rangeDiff
    $workingHash = $null
    if ($IncludeDirty) {
        $workingDiff = Invoke-GitText $Repo @('diff', '--binary')
        $cachedDiff = Invoke-GitText $Repo @('diff', '--binary', '--cached')
        $untrackedDigest = Get-UntrackedContentDigest $Repo
        $workingHash = Get-TreeHash ($status + "`n" + $cachedDiff + "`n" + $workingDiff + "`n" + $untrackedDigest)
    }

    $snapshot = [ordered]@{
        baseRef = $Base
        baseCommit = $baseCommit
        headRef = $Head
        headCommit = $headCommit
        rangeHash = $rangeHash
        includeWorkingTree = $IncludeDirty
        workingTreeHash = $workingHash
        changeSnapshot = if ($IncludeDirty) { "$($baseCommit.Substring(0, 12))..$($headCommit.Substring(0, 12))+wt.$($workingHash.Substring(0, 12))" } else { "$($baseCommit.Substring(0, 12))..$($headCommit.Substring(0, 12))+$($rangeHash.Substring(0, 12))" }
    }
    return $snapshot
}

$RepoRoot = [System.IO.Path]::GetFullPath($Worktree)
if (-not (Test-Path -LiteralPath $RepoRoot)) {
    Write-Host "GATE_WORKFLOW_BLOCKED worktreeMissing=$(Format-GatePath $RepoRoot)"
    exit 1
}
$ResolvedStatePath = Resolve-StatePath $RepoRoot $StatePath
$ManifestHash = $null
if (-not [string]::IsNullOrWhiteSpace($ManifestPath)) {
    $ResolvedManifestPath = Resolve-ArtifactPath $RepoRoot $ManifestPath
    if (-not (Test-Path -LiteralPath $ResolvedManifestPath)) {
        Write-Host "GATE_WORKFLOW_BLOCKED manifestMissing=$(Format-GatePath $ResolvedManifestPath)"
        exit 1
    }
    $ManifestHash = (Get-FileHash -LiteralPath $ResolvedManifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
}
else {
    $ResolvedManifestPath = $null
}

if ($Action -eq 'snapshot') {
    $snapshot = New-Snapshot $RepoRoot $BaseRef $HeadRef $IncludeWorkingTree.IsPresent
    $snapshot | ConvertTo-Json -Depth 8
    exit 0
}

if ($Action -eq 'verify-admission') {
    if ([string]::IsNullOrWhiteSpace($Gate)) { throw 'Gate is required.' }
    if ([string]::IsNullOrWhiteSpace($WorkflowId)) {
        Write-Host "GATE_WORKFLOW_BLOCKED gate=$Gate workflowIdRequired"
        exit 1
    }
    if ([string]::IsNullOrWhiteSpace($ChangeSnapshot)) {
        Write-Host "GATE_WORKFLOW_BLOCKED gate=$Gate changeSnapshotRequired"
        exit 1
    }
    $args = @('-Action', 'verify-admission', '-StatePath', $ResolvedStatePath, '-Gate', $Gate)
    $args += @('-WorkflowId', $WorkflowId, '-ChangeSnapshot', $ChangeSnapshot)
    if (-not [string]::IsNullOrWhiteSpace($ResolvedManifestPath)) { $args += @('-ManifestPath', $ResolvedManifestPath) }
    $code = Invoke-GateState $args $RepoRoot
    exit $code
}

if ($Action -eq 'record-stage') {
    if ([string]::IsNullOrWhiteSpace($Gate)) { throw 'Gate is required.' }
    if ([string]::IsNullOrWhiteSpace($Artifact)) { throw 'Artifact is required.' }
    $artifactPath = Resolve-ArtifactPath $RepoRoot $Artifact
    if (-not (Test-Path -LiteralPath $artifactPath)) {
        Write-Host "GATE_WORKFLOW_BLOCKED artifactMissing=$(Format-GatePath $artifactPath)"
        exit 1
    }
    if ($Verdict -eq 'PASS') {
        Assert-FormalPassArtifact $Gate $artifactPath $RepoRoot $WorkflowId $ChangeSnapshot $Stage
    }

    $snapshot = $null
    if (-not [string]::IsNullOrWhiteSpace($BaseRef)) {
        $snapshot = New-Snapshot $RepoRoot $BaseRef $HeadRef $IncludeWorkingTree.IsPresent
        if ([string]::IsNullOrWhiteSpace($ChangeSnapshot)) { $ChangeSnapshot = $snapshot.changeSnapshot }
        $BaseCommit = $snapshot.baseCommit
        $HeadCommit = $snapshot.headCommit
    }

    $recordArgs = @('-Action', 'record', '-StatePath', $ResolvedStatePath, '-Gate', $Gate, '-Verdict', $Verdict, '-Artifact', $Artifact, '-Actor', $Actor, '-Worktree', (Format-GatePath $RepoRoot))
    foreach ($pair in @(
        @{ name = 'Mode'; value = $Mode },
        @{ name = 'Stage'; value = $Stage },
        @{ name = 'Reason'; value = $Reason },
        @{ name = 'WorkflowId'; value = $WorkflowId },
        @{ name = 'ChangeSnapshot'; value = $ChangeSnapshot },
        @{ name = 'ManifestHash'; value = $ManifestHash },
        @{ name = 'BaseRef'; value = $BaseRef },
        @{ name = 'BaseCommit'; value = $BaseCommit },
        @{ name = 'HeadRef'; value = $HeadRef },
        @{ name = 'HeadCommit'; value = $HeadCommit },
        @{ name = 'AttemptId'; value = $AttemptId }
    )) {
        if (-not [string]::IsNullOrWhiteSpace([string]$pair.value)) { $recordArgs += @('-' + $pair.name, [string]$pair.value) }
    }
    if (-not [string]::IsNullOrWhiteSpace($ResolvedManifestPath)) { $recordArgs += @('-ManifestPath', $ResolvedManifestPath) }
    $recordCode = Invoke-GateState $recordArgs $RepoRoot
    if ($recordCode -ne 0) { exit $recordCode }

    $verifyArgs = @('-Action', 'verify', '-StatePath', $ResolvedStatePath, '-Gate', $Gate, '-RequireVerdict', $Verdict, '-RequireArtifactExists')
    if (-not [string]::IsNullOrWhiteSpace($WorkflowId)) { $verifyArgs += @('-RequireWorkflowId', $WorkflowId) }
    if (-not [string]::IsNullOrWhiteSpace($ChangeSnapshot)) { $verifyArgs += @('-ChangeSnapshot', $ChangeSnapshot) }
    if (-not [string]::IsNullOrWhiteSpace($ManifestHash)) { $verifyArgs += @('-RequireManifestHash', $ManifestHash) }
    if (-not [string]::IsNullOrWhiteSpace($ResolvedManifestPath)) { $verifyArgs += @('-ManifestPath', $ResolvedManifestPath) }
    $verifyCode = Invoke-GateState $verifyArgs $RepoRoot
    exit $verifyCode
}

if ($Action -eq 'record-final-verification') {
    if ([string]::IsNullOrWhiteSpace($AttemptsJson)) { throw 'AttemptsJson is required.' }
    $attempts = @($AttemptsJson | ConvertFrom-Json)
    if ($attempts.Count -eq 0) { throw 'At least one attempt is required.' }
    $accepted = @($attempts | Where-Object { $_.accepted -eq $true -and [string]$_.status -eq 'PASS' })
    $finalStatus = if ($accepted.Count -gt 0) { 'PASS' } else { 'FAIL' }
    if ($finalStatus -eq 'PASS') {
        Assert-AcceptedFinalVerificationAttempts $accepted $RepoRoot $WorkflowId $ChangeSnapshot
    }
    if ([string]::IsNullOrWhiteSpace($OutputArtifact)) {
        $OutputArtifact = Join-Path '.claude/gates/artifacts' ("final-verification-" + $(if ([string]::IsNullOrWhiteSpace($WorkflowId)) { 'workflow' } else { $WorkflowId }) + '.json')
    }
    $outputPath = Resolve-ArtifactPath $RepoRoot $OutputArtifact
    $parent = Split-Path -Parent $outputPath
    if (-not [string]::IsNullOrWhiteSpace($parent) -and -not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Path $parent | Out-Null
    }
    $aggregate = [ordered]@{
        schemaVersion = 1
        workflowId = $WorkflowId
        changeSnapshot = $ChangeSnapshot
        status = $finalStatus
        generatedAtUtc = [DateTime]::UtcNow.ToString('o')
        attempts = $attempts
        acceptedAttempts = $accepted
    }
    $aggregate | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $outputPath -Encoding UTF8
    Write-Host "GATE_WORKFLOW_FINAL_VERIFICATION status=$finalStatus artifact=$(Format-GatePath $outputPath) accepted=$($accepted.Count) attempts=$($attempts.Count)"

    if ($RecordFinalQa) {
        if ([string]::IsNullOrWhiteSpace($FinalQaArtifact)) {
            $FinalQaArtifact = Join-Path '.claude/gates/artifacts' ("final-qa-execution-" + $(if ([string]::IsNullOrWhiteSpace($WorkflowId)) { 'workflow' } else { $WorkflowId }) + '.md')
        }
        $finalQaPath = Resolve-ArtifactPath $RepoRoot $FinalQaArtifact
        Write-FinalQaArtifact $finalQaPath $finalStatus $attempts $accepted
        if ($finalStatus -eq 'PASS') {
            Assert-FormalPassArtifact 'qa-test-gate' $finalQaPath $RepoRoot $WorkflowId $ChangeSnapshot 'FinalExecution'
        }
        $recordArgs = @('-Action', 'record-stage', '-Worktree', $RepoRoot, '-StatePath', $ResolvedStatePath, '-Gate', 'qa-test-gate', '-Verdict', $finalStatus, '-Mode', 'formal', '-Stage', 'FinalExecution', '-Artifact', $FinalQaArtifact, '-Actor', $Actor, '-WorkflowId', $WorkflowId, '-ChangeSnapshot', $ChangeSnapshot)
        if (-not [string]::IsNullOrWhiteSpace($ResolvedManifestPath)) { $recordArgs += @('-ManifestPath', $ResolvedManifestPath) }
        & pwsh -NoProfile -File $PSCommandPath @recordArgs
        $recordExitCode = $LASTEXITCODE
        if ($recordExitCode -ne 0) { exit $recordExitCode }
        exit $(if ($finalStatus -eq 'PASS') { 0 } else { 1 })
    }
    exit $(if ($finalStatus -eq 'PASS') { 0 } else { 1 })
}
