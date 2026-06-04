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
    [ValidateSet('auto', 'git', 'svn', 'file-hash')]
    [string]$Vcs = 'auto',
    [string]$AttemptId,
    [string]$AttemptsJson,
    [string]$AttemptsJsonFile,
    [string]$OutputArtifact,
    [string]$FinalQaArtifact,
    [switch]$RecordFinalQa,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'powershell-host.ps1')
. (Join-Path $PSScriptRoot 'gate-artifact-validation.ps1')

function Show-Usage {
    @'
gate-workflow.ps1 -Action <record-stage|verify-admission|snapshot|record-final-verification> [options]

Generic helper around gate-state.ps1. It resolves the worktree/state path, verifies artifacts,
records state, and writes machine-readable final verification attempt aggregates.

Examples:
  <ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate complexity-gate -WorkflowId wf -ChangeSnapshot snap
  <ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate complexity-gate -Verdict PASS -Artifact .claude/gates/artifacts/scope.txt -WorkflowId wf -ChangeSnapshot snap
  <ps> -File <formal-gates>/scripts/gate-workflow.ps1 -Action snapshot -Worktree <repo> -BaseRef main -HeadRef HEAD
  powershell -NoProfile -ExecutionPolicy Bypass -File <formal-gates>/scripts/gate-workflow.ps1 -Action snapshot -Worktree <svn-or-plain-project> -Vcs file-hash
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

function Invoke-NativeText([string]$FilePath, [string[]]$Arguments) {
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = & $FilePath @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    return [pscustomobject]@{
        ExitCode = $exitCode
        Text = (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
    }
}

function Resolve-StatePath([string]$Repo, [string]$Path) {
    if (-not [string]::IsNullOrWhiteSpace($Path)) {
        if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
        return Join-Path $Repo $Path
    }
    return Join-Path $Repo '.claude/gates/gate-state.json'
}

function Resolve-ArtifactPath([string]$Repo, [string]$Path) {
    return Resolve-FormalGateArtifactPath $Repo $Path
}

function Get-Sha256([string]$Path) {
    return Get-FormalGateSha256 $Path
}

function Get-ContextBundleValidationErrors([string]$Repo, [string]$Value) {
    return Get-FormalGateContextBundleValidationErrors $Repo $Value
}

function Get-GateRouteFieldValue([string]$Text, [string]$FieldName) {
    return Get-FormalGateRouteFieldValue $Text $FieldName
}

function Assert-FormalPassArtifact([string]$GateName, [string]$ArtifactPath, [string]$Repo, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$StageValue) {
    $requiredFields = @(Get-FormalGatePassRequiredFields $GateName)
    if ($requiredFields.Count -eq 0) { return }
    $check = Test-FormalGateArtifactFields $ArtifactPath $requiredFields $Repo $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue
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
        $processArgs = (Get-FormalGatesPowerShellFileArgs $gateState) + @($Arguments)
        $process = Start-Process -FilePath (Get-FormalGatesPowerShellExe) -ArgumentList $processArgs -WorkingDirectory $Repo -NoNewWindow -PassThru -Wait -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
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
    $result = Invoke-NativeText 'git' (@('-C', $Repo) + @($Arguments))
    if ($result.ExitCode -ne 0) {
        throw $result.Text
    }
    return $result.Text
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
    $result = Invoke-NativeText 'git' @('-C', $Repo, 'ls-files', '--others', '--exclude-standard')
    if ($result.ExitCode -ne 0) {
        throw $result.Text
    }
    $entries = @()
    foreach ($relative in @($result.Text -split '\r?\n' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Sort-Object)) {
        $path = Join-Path $Repo $relative
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        $entries += "$relative sha256=$(Get-Sha256 $path)"
    }
    return ($entries -join "`n")
}

function Test-GitWorktree([string]$Repo) {
    $git = Get-Command git -ErrorAction SilentlyContinue
    if ($null -eq $git) { return $false }
    $result = Invoke-NativeText 'git' @('-C', $Repo, 'rev-parse', '--is-inside-work-tree')
    return ($result.ExitCode -eq 0 -and (($result.Text -split '\r?\n' | Select-Object -First 1) -eq 'true'))
}

function Test-SvnWorktree([string]$Repo) {
    $current = [System.IO.Path]::GetFullPath($Repo)
    while (-not [string]::IsNullOrWhiteSpace($current)) {
        if (Test-Path -LiteralPath (Join-Path $current '.svn')) { return $true }
        $parent = Split-Path -Parent $current
        if ($parent -eq $current) { break }
        $current = $parent
    }
    $svn = Get-Command svn -ErrorAction SilentlyContinue
    if ($null -eq $svn) { return $false }
    $result = Invoke-NativeText 'svn' @('info', $Repo)
    return ($result.ExitCode -eq 0)
}

function Test-SnapshotExcludedPath([string]$RelativePath) {
    $normalized = $RelativePath.Replace('\', '/')
    if ($normalized -match '(^|/)(\.git|\.svn|\.hg|node_modules|__pycache__)(/|$)') { return $true }
    if ($normalized -match '^(\.claude|\.codex)/gates(/|$)') { return $true }
    return $false
}

function Get-FileTreeDigest([string]$Repo) {
    $root = [System.IO.Path]::GetFullPath($Repo).TrimEnd('\', '/')
    $entries = @()
    foreach ($file in Get-ChildItem -LiteralPath $root -Recurse -File -Force -ErrorAction SilentlyContinue) {
        $relative = $file.FullName.Substring($root.Length).TrimStart('\', '/')
        if (Test-SnapshotExcludedPath $relative) { continue }
        $entries += "$($relative.Replace('\', '/')) sha256=$(Get-Sha256 $file.FullName)"
    }
    return ($entries | Sort-Object) -join "`n"
}

function New-FileHashSnapshot($Repo, [string]$DetectedVcs) {
    $digest = Get-FileTreeDigest $Repo
    $treeHash = Get-TreeHash $digest
    $prefix = if ($DetectedVcs -eq 'svn') { 'svn-files' } else { 'files' }
    return [ordered]@{
        vcs = $DetectedVcs
        baseRef = $null
        baseCommit = $null
        headRef = $null
        headCommit = $null
        rangeHash = $treeHash
        includeWorkingTree = $true
        workingTreeHash = $treeHash
        changeSnapshot = "$prefix.$($treeHash.Substring(0, 12))"
    }
}

function New-Snapshot($Repo, [string]$Base, [string]$Head, [bool]$IncludeDirty, [string]$RequestedVcs) {
    $detectedVcs = $RequestedVcs
    if ($detectedVcs -eq 'auto') {
        if (Test-GitWorktree $Repo) { $detectedVcs = 'git' }
        elseif (Test-SvnWorktree $Repo) { $detectedVcs = 'svn' }
        else { $detectedVcs = 'file-hash' }
    }

    if ($detectedVcs -ne 'git') {
        return New-FileHashSnapshot $Repo $detectedVcs
    }

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
        vcs = 'git'
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
    $snapshot = New-Snapshot $RepoRoot $BaseRef $HeadRef $IncludeWorkingTree.IsPresent $Vcs
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
    if ([string]::IsNullOrWhiteSpace($AttemptsJson) -and [string]::IsNullOrWhiteSpace($AttemptsJsonFile)) { throw 'AttemptsJson or AttemptsJsonFile is required.' }
    if (-not [string]::IsNullOrWhiteSpace($AttemptsJsonFile)) {
        $attemptsJsonPath = Resolve-ArtifactPath $RepoRoot $AttemptsJsonFile
        if (-not (Test-Path -LiteralPath $attemptsJsonPath)) { throw "AttemptsJsonFile not found: $attemptsJsonPath" }
        $attemptsJsonText = Get-Content -LiteralPath $attemptsJsonPath -Raw -Encoding UTF8
    }
    else {
        $attemptsJsonText = $AttemptsJson
    }
    $attempts = @($attemptsJsonText | ConvertFrom-Json)
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
        $selfArgs = (Get-FormalGatesPowerShellFileArgs $PSCommandPath) + @($recordArgs)
        & (Get-FormalGatesPowerShellExe) @selfArgs
        $recordExitCode = $LASTEXITCODE
        if ($recordExitCode -ne 0) { exit $recordExitCode }
        exit $(if ($finalStatus -eq 'PASS') { 0 } else { 1 })
    }
    exit $(if ($finalStatus -eq 'PASS') { 0 } else { 1 })
}
