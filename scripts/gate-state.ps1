param(
    [ValidateSet('record', 'verify', 'verify-admission', 'show', 'help')]
    [string]$Action = 'verify',

    [string]$StatePath = (Join-Path (Get-Location) '.claude/gates/gate-state.json'),
    [string]$ManifestPath,
    [string]$Gate,
    [string]$Verdict,
    [string]$Mode,
    [string]$Stage,
    [string]$Artifact,
    [string]$Actor,
    [string]$Reason,
    [string]$WorkflowId,
    [string]$ChangeSnapshot,
    [string]$ManifestHash,
    [string]$Worktree,
    [string]$BaseRef,
    [string]$BaseCommit,
    [string]$HeadRef,
    [string]$HeadCommit,
    [string]$AttemptId,

    [string]$RequireVerdict = 'PASS',
    [string]$RequireMode,
    [string]$RequireStage,
    [string]$RequireWorkflowId,
    [string]$RequireManifestHash,
    [switch]$RequireArtifactExists,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
$StandaloneSingleGateHint = 'hint="Standalone advisory single-gate only: omit WorkflowId/ChangeSnapshot and use GateWorkflow.singleGateAuthorized=true. Formal four-gate workflows must keep WorkflowId/ChangeSnapshot and sequencing; do not use standalone mode to bypass gates."'

function Show-Usage {
    @'
gate-state.ps1 -Action <record|verify|verify-admission|show|help> [options]

Actions:
  record            Record a gate verdict. PASS requires an existing artifact.
  verify            Verify one gate entry matches required verdict/mode/stage/workflow/snapshot.
  verify-admission  Verify all prerequisite gates for the requested gate.
  show              Print the current state JSON.
  help              Print this usage text.

Common options:
  -StatePath <path>          Defaults to .claude/gates/gate-state.json under the current directory.
  -ManifestPath <path>       Optional generic workflow manifest; default built-in order is used when omitted.
  -Gate <name>               qa-test-gate | complexity-gate | architecture-health-gate | code-quality-gate by default.
  -WorkflowId <id>           Required for multi-gate admission checks.
  -ChangeSnapshot <hash>     Optional change snapshot; when supplied, admission verifies it matches recorded PASS entries.
  -Artifact <path>           Required when recording PASS.

Examples:
  pwsh <formal-gates>/scripts/gate-state.ps1 -Action record -Gate qa-test-gate -Verdict PASS -Mode formal -Stage Execution -Artifact .artifacts/qa.txt -Actor qa -WorkflowId wf-1 -ChangeSnapshot snap-1
  pwsh <formal-gates>/scripts/gate-state.ps1 -Action verify-admission -Gate complexity-gate -WorkflowId wf-1 -ChangeSnapshot snap-1
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

function New-GateState {
    [ordered]@{
        schemaVersion = 1
        gates = [ordered]@{}
        history = @()
    }
}

function Read-GateState([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return New-GateState
    }

    if ((Get-Item -LiteralPath $Path).PSIsContainer) {
        Write-Host "GATE_STATE_BLOCKED statePathIsDirectory=$(Format-GatePath $Path)"
        exit 1
    }

    $Raw = Get-Content -LiteralPath $Path -Raw -Encoding UTF8
    if ([string]::IsNullOrWhiteSpace($Raw)) {
        return New-GateState
    }

    $Parsed = $Raw | ConvertFrom-Json
    $Gates = [ordered]@{}
    if ($Parsed.PSObject.Properties.Name -contains 'gates' -and $null -ne $Parsed.gates) {
        foreach ($Property in $Parsed.gates.PSObject.Properties) {
            $Gates[$Property.Name] = $Property.Value
        }
    }
    $History = @()
    if ($Parsed.PSObject.Properties.Name -contains 'history' -and $null -ne $Parsed.history) {
        $History = @($Parsed.history)
    }

    return [ordered]@{
        schemaVersion = 1
        gates = $Gates
        history = $History
    }
}

function Write-GateState([string]$Path, $State) {
    $Parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($Parent) -and -not (Test-Path -LiteralPath $Parent)) {
        New-Item -ItemType Directory -Path $Parent | Out-Null
    }
    $State | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $Path -Encoding UTF8
}

function Resolve-ArtifactPath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    if ([System.IO.Path]::IsPathRooted($Path)) { return $Path }
    return (Join-Path (Get-Location) $Path)
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

function Test-ContextBundleExists([string]$Value) {
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
        $path = Resolve-ArtifactPath $candidate
        if (Test-Path -LiteralPath $path) { return $true }
    }
    return $false
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-ContextBundleValidationErrors([string]$Value) {
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
        $path = Resolve-ArtifactPath $pathText
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

function Test-ArtifactReferenceExists([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $false }
    $path = Resolve-ArtifactPath $pathText
    return (Test-Path -LiteralPath $path -PathType Leaf)
}

function Get-ArtifactReferencePath([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $null }
    return Resolve-ArtifactPath $pathText
}

function Assert-ArtifactHashMatches($Entry, [string]$GateName, [string]$RequiredFor, [string]$ArtifactPath) {
    if ($null -eq $Entry -or [string]::IsNullOrWhiteSpace($ArtifactPath)) { return }
    if (-not ($Entry.PSObject.Properties.Name -contains 'artifactHash') -or [string]::IsNullOrWhiteSpace([string]$Entry.artifactHash)) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName artifactHashMissing=$($Entry.artifact) requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }
    $actual = Get-Sha256 $ArtifactPath
    if ($actual -ne ([string]$Entry.artifactHash).ToLowerInvariant()) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName artifactHashMismatch=$($Entry.artifact) requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }
}

function Test-AnyMeaningfulArtifactField([string]$Text, [string[]]$FieldNames) {
    foreach ($field in $FieldNames) {
        if (Test-MeaningfulArtifactField $Text $field) { return $true }
    }
    return $false
}

function Get-ImplementationEvidenceMissing([string]$Text, [string]$GateName) {
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
        if (-not (Test-ArtifactReferenceExists $diffValue)) { $missing += 'Raw diff/changed-files artifact path must exist' }
    }
    if (-not (Test-AnyMeaningfulArtifactField $Text @('Developer self-test artifact', 'Verification artifact'))) {
        $missing += 'Developer self-test artifact or Verification artifact'
    }
    else {
        $verificationValue = Get-ArtifactFieldValue $Text 'Developer self-test artifact'
        if ([string]::IsNullOrWhiteSpace($verificationValue)) { $verificationValue = Get-ArtifactFieldValue $Text 'Verification artifact' }
        if (-not (Test-ArtifactReferenceExists $verificationValue)) { $missing += 'Developer self-test/verification artifact path must exist' }
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

function Get-FinalExecutionEvidenceMissing([string]$Text, [string]$GateName, [string]$StageValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot) {
    if ($GateName -ne 'qa-test-gate' -or $StageValue -ne 'FinalExecution') { return @() }
    $missing = @()
    $evidenceValue = Get-ArtifactFieldValue $Text 'QA-owned evidence'
    $evidencePath = Get-ArtifactReferencePath $evidenceValue
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

function Assert-FormalPassArtifact([string]$GateName, [string]$ArtifactPath, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$StageValue) {
    $requiredFields = @(Get-FormalGatePassRequiredFields $GateName)
    if ($requiredFields.Count -eq 0) { return }
    $text = [string](Get-Content -LiteralPath $ArtifactPath -Raw)
    $missing = @($requiredFields | Where-Object { $text -notmatch [regex]::Escape($_) })
    if (-not (Test-MeaningfulArtifactField $text 'Reviewer agent id')) {
        $missing += 'Reviewer agent id: <non-empty independent agent id>'
    }
    if (-not (Test-MeaningfulArtifactField $text 'Context bundle')) {
        $missing += 'Context bundle: <non-empty bundle path>'
    }
    else {
        $missing += @(Get-ContextBundleValidationErrors (Get-ArtifactFieldValue $text 'Context bundle'))
    }
    $missing += @(Get-QaEvidenceMissing $text $GateName)
    $missing += @(Get-FinalExecutionEvidenceMissing $text $GateName $StageValue $ExpectedWorkflowId $ExpectedChangeSnapshot)
    $missing += @(Get-ImplementationEvidenceMissing $text $GateName)
    $missing += @(Get-GateRouteMissingForPass $text $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue)
    if ($missing.Count -gt 0) {
        Write-Host "$GateName PASS blocked: artifact lacks required formal independent zero-context review fields: $($missing -join ', ')"
        exit 1
    }
}

function Get-ManifestHash([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    if (-not (Test-Path -LiteralPath $Path)) {
        Write-Host "GATE_STATE_BLOCKED manifestMissing=$(Format-GatePath $Path) state=$(Format-GatePath $StatePath)"
        exit 1
    }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Read-GateManifest([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    if (-not (Test-Path -LiteralPath $Path)) {
        Write-Host "GATE_STATE_BLOCKED manifestMissing=$(Format-GatePath $Path) state=$(Format-GatePath $StatePath)"
        exit 1
    }
    try {
        return Get-Content -LiteralPath $Path -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        Write-Host "GATE_STATE_BLOCKED manifestMalformed=$(Format-GatePath $Path) state=$(Format-GatePath $StatePath)"
        exit 1
    }
}

$Manifest = Read-GateManifest $ManifestPath
if ([string]::IsNullOrWhiteSpace($ManifestHash) -and -not [string]::IsNullOrWhiteSpace($ManifestPath)) {
    $ManifestHash = Get-ManifestHash $ManifestPath
}
if ([string]::IsNullOrWhiteSpace($RequireManifestHash) -and -not [string]::IsNullOrWhiteSpace($ManifestPath)) {
    $RequireManifestHash = $ManifestHash
}

function Get-ManifestStage($ManifestObject, [string]$GateName) {
    if ($null -eq $ManifestObject -or [string]::IsNullOrWhiteSpace($GateName)) { return $null }
    if ($ManifestObject.PSObject.Properties.Name -contains 'stages' -and $null -ne $ManifestObject.stages) {
        foreach ($Property in $ManifestObject.stages.PSObject.Properties) {
            if ($Property.Name -eq $GateName) { return $Property.Value }
        }
    }
    return $null
}

function Assert-ManifestDoesNotOverrideBuiltIns($ManifestObject) {
    if ($null -eq $ManifestObject -or $ManifestObject.PSObject.Properties.Name -notcontains 'stages' -or $null -eq $ManifestObject.stages) { return }
    $builtIns = @('qa-test-gate', 'complexity-gate', 'architecture-health-gate', 'code-quality-gate')
    foreach ($Property in $ManifestObject.stages.PSObject.Properties) {
        if ($builtIns -contains $Property.Name) {
            Write-Host "GATE_STATE_BLOCKED manifestOverridesBuiltInGate=$($Property.Name) state=$(Format-GatePath $StatePath)"
            exit 1
        }
    }
}

Assert-ManifestDoesNotOverrideBuiltIns $Manifest

function Test-KnownGate([string]$GateName) {
    $KnownGates = @('qa-test-gate', 'complexity-gate', 'architecture-health-gate', 'code-quality-gate')
    if ($KnownGates -contains $GateName) { return }
    if ($null -ne (Get-ManifestStage $Manifest $GateName)) { return }
    Write-Host "GATE_STATE_BLOCKED gate=$GateName unknownGate state=$(Format-GatePath $StatePath)"
    exit 1
}

function Test-KnownVerdict([string]$VerdictValue) {
    $KnownVerdicts = @('PASS', 'CONDITIONAL_PASS', 'REVIEW', 'FAIL', 'BLOCKED')
    if ($KnownVerdicts -notcontains $VerdictValue) {
        Write-Host "GATE_STATE_BLOCKED verdict=$VerdictValue unknownVerdict state=$(Format-GatePath $StatePath)"
        exit 1
    }
}

function Test-GateEntry($State, [string]$GateName, [string]$RequiredFor, [string]$ExpectedVerdict, [string]$ExpectedMode, [string]$ExpectedStage, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [bool]$ArtifactMustExist, [string]$ExpectedManifestHash) {
    $Entries = @()
    $History = @($State.history)
    for ($Index = $History.Count - 1; $Index -ge 0; --$Index) {
        $HistoryEntry = $History[$Index]
        if ($HistoryEntry.PSObject.Properties.Name -contains 'gate' -and $HistoryEntry.gate -eq $GateName) {
            $Entries += $HistoryEntry
        }
    }
    $CurrentEntry = $State.gates[$GateName]
    if ($Entries.Count -eq 0 -and $null -ne $CurrentEntry) { $Entries += $CurrentEntry }
    if ($Entries.Count -eq 0) {
        Write-Host "GATE_STATE_BLOCKED missing gate=$GateName requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    $RouteEntries = @($Entries | Where-Object {
        ([string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -or $_.workflowId -eq $ExpectedWorkflowId) -and
        ([string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -or $_.changeSnapshot -eq $ExpectedChangeSnapshot) -and
        ([string]::IsNullOrWhiteSpace($ExpectedManifestHash) -or (($_.PSObject.Properties.Name -contains 'manifestHash') -and -not [string]::IsNullOrWhiteSpace([string]$_.manifestHash) -and $_.manifestHash -eq $ExpectedManifestHash))
    })
    if ($RouteEntries.Count -eq 0) {
        Write-Host "GATE_STATE_BLOCKED missing route gate=$GateName requiredFor=$RequiredFor workflowId=$ExpectedWorkflowId changeSnapshot=$ExpectedChangeSnapshot state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    foreach ($Entry in $RouteEntries) {
        if ($ExpectedVerdict -eq 'PASS' -and $Entry.verdict -in @('CONDITIONAL_PASS', 'REVIEW', 'FAIL', 'BLOCKED')) {
            Write-Host "GATE_STATE_BLOCKED gate=$GateName verdict=$($Entry.verdict) required=$ExpectedVerdict requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
            exit 1
        }
        if ($Entry.verdict -ne $ExpectedVerdict) { continue }
        if (-not [string]::IsNullOrWhiteSpace($ExpectedMode) -and $Entry.mode -ne $ExpectedMode) { continue }
        if (-not [string]::IsNullOrWhiteSpace($ExpectedStage) -and $Entry.stage -ne $ExpectedStage) { continue }
        if ($ArtifactMustExist) {
            $ArtifactPath = Resolve-ArtifactPath $Entry.artifact
            if ([string]::IsNullOrWhiteSpace($ArtifactPath) -or -not (Test-Path -LiteralPath $ArtifactPath)) {
                Write-Host "GATE_STATE_BLOCKED gate=$GateName artifactMissing=$($Entry.artifact) requiredFor=$RequiredFor state=$(Format-GatePath $StatePath)"
                exit 1
            }
            Assert-ArtifactHashMatches $Entry $GateName $RequiredFor $ArtifactPath
        }
        return
    }

    $Latest = $RouteEntries[0]
    if ($Latest.verdict -ne $ExpectedVerdict) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName verdict=$($Latest.verdict) required=$ExpectedVerdict requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedMode) -and $Latest.mode -ne $ExpectedMode) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName mode=$($Latest.mode) requiredMode=$ExpectedMode requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedStage) -and $Latest.stage -ne $ExpectedStage) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName stage=$($Latest.stage) requiredStage=$ExpectedStage requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and $Latest.workflowId -ne $ExpectedWorkflowId) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName workflowId=$($Latest.workflowId) requiredWorkflowId=$ExpectedWorkflowId requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -and $Latest.changeSnapshot -ne $ExpectedChangeSnapshot) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName changeSnapshot=$($Latest.changeSnapshot) requiredChangeSnapshot=$ExpectedChangeSnapshot requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if (-not [string]::IsNullOrWhiteSpace($ExpectedManifestHash) -and (-not ($Latest.PSObject.Properties.Name -contains 'manifestHash') -or [string]::IsNullOrWhiteSpace([string]$Latest.manifestHash) -or $Latest.manifestHash -ne $ExpectedManifestHash)) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName manifestHash=$($Latest.manifestHash) requiredManifestHash=$ExpectedManifestHash requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    if ($ArtifactMustExist) {
        $ArtifactPath = Resolve-ArtifactPath $Latest.artifact
        if ([string]::IsNullOrWhiteSpace($ArtifactPath) -or -not (Test-Path -LiteralPath $ArtifactPath)) {
            Write-Host "GATE_STATE_BLOCKED gate=$GateName artifactMissing=$($Latest.artifact) requiredFor=$RequiredFor state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
            exit 1
        }
        Assert-ArtifactHashMatches $Latest $GateName $RequiredFor $ArtifactPath
    }
}

function Convert-ManifestRequirement($Requirement) {
    @{
        gate = [string]$Requirement.gate
        verdict = if ($Requirement.PSObject.Properties.Name -contains 'verdict' -and -not [string]::IsNullOrWhiteSpace([string]$Requirement.verdict)) { [string]$Requirement.verdict } else { 'PASS' }
        mode = if ($Requirement.PSObject.Properties.Name -contains 'mode') { [string]$Requirement.mode } else { $null }
        stage = if ($Requirement.PSObject.Properties.Name -contains 'stage') { [string]$Requirement.stage } else { $null }
        artifact = if ($Requirement.PSObject.Properties.Name -contains 'artifact') { [bool]$Requirement.artifact } else { $true }
    }
}

function Get-AdmissionRequirements([string]$GateName) {
    $ManifestStage = Get-ManifestStage $Manifest $GateName
    if ($null -ne $ManifestStage) {
        if ($ManifestStage.PSObject.Properties.Name -contains 'requires' -and $null -ne $ManifestStage.requires) {
            return @($ManifestStage.requires | ForEach-Object { Convert-ManifestRequirement $_ })
        }
        return @()
    }

    switch ($GateName) {
        'qa-test-gate' {
            return @()
        }
        'complexity-gate' {
            return @(
                @{ gate = 'qa-test-gate'; verdict = 'PASS'; mode = 'formal'; stage = 'Execution'; artifact = $true }
            )
        }
        'architecture-health-gate' {
            return @(
                @{ gate = 'qa-test-gate'; verdict = 'PASS'; mode = 'formal'; stage = 'Execution'; artifact = $true },
                @{ gate = 'complexity-gate'; verdict = 'PASS'; mode = $null; stage = $null; artifact = $true }
            )
        }
        'code-quality-gate' {
            return @(
                @{ gate = 'qa-test-gate'; verdict = 'PASS'; mode = 'formal'; stage = 'Execution'; artifact = $true },
                @{ gate = 'complexity-gate'; verdict = 'PASS'; mode = $null; stage = $null; artifact = $true },
                @{ gate = 'architecture-health-gate'; verdict = 'PASS'; mode = $null; stage = $null; artifact = $true }
            )
        }
        default {
            Write-Host "GATE_STATE_BLOCKED gate=$GateName admissionProfileMissing state=$(Format-GatePath $StatePath)"
            exit 1
        }
    }
}

function Assert-RecordAdmission($State, [string]$GateName, [string]$Workflow, [string]$Snapshot) {
    $Requirements = @(Get-AdmissionRequirements $GateName)
    if ($Requirements.Count -eq 0) { return }

    if ([string]::IsNullOrWhiteSpace($Workflow)) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName recordRequiresWorkflowId state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }
    if ([string]::IsNullOrWhiteSpace($Snapshot)) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName recordRequiresChangeSnapshot state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    foreach ($Requirement in $Requirements) {
        Test-GateEntry $State $Requirement.gate $GateName $Requirement.verdict $Requirement.mode $Requirement.stage $Workflow $Snapshot $Requirement.artifact $RequireManifestHash
    }
}

function Assert-FinalQaAdmission($State, [string]$Workflow, [string]$Snapshot) {
    if ([string]::IsNullOrWhiteSpace($Workflow)) {
        Write-Host "GATE_STATE_BLOCKED gate=qa-test-gate finalExecutionRequiresWorkflowId state=$(Format-GatePath $StatePath)"
        exit 1
    }
    if ([string]::IsNullOrWhiteSpace($Snapshot)) {
        Write-Host "GATE_STATE_BLOCKED gate=qa-test-gate finalExecutionRequiresChangeSnapshot state=$(Format-GatePath $StatePath)"
        exit 1
    }
    Test-GateEntry $State 'code-quality-gate' 'qa-test-gate' 'PASS' $null $null $Workflow $Snapshot $true $RequireManifestHash
}

function Assert-FormalWorkflowSnapshot([string]$GateName, [string]$Workflow, [string]$Snapshot, [string]$ModeValue, [string]$StageValue) {
    if ([string]::IsNullOrWhiteSpace($Workflow)) { return }
    $isWorkflowBound = $GateName -ne 'qa-test-gate' -or $ModeValue -eq 'formal' -or $StageValue -in @('Execution', 'FinalExecution')
    if ($isWorkflowBound -and [string]::IsNullOrWhiteSpace($Snapshot)) {
        Write-Host "GATE_STATE_BLOCKED gate=$GateName workflowPassRequiresChangeSnapshot state=$(Format-GatePath $StatePath)"
        exit 1
    }
}

function Assert-QaPassWorkflow([string]$Workflow, [string]$Snapshot, [string]$ModeValue, [string]$StageValue) {
    if ([string]::IsNullOrWhiteSpace($Workflow)) {
        Write-Host "GATE_STATE_BLOCKED gate=qa-test-gate qaPassRequiresWorkflowId state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }
    if ([string]::IsNullOrWhiteSpace($Snapshot)) {
        Write-Host "GATE_STATE_BLOCKED gate=qa-test-gate qaPassRequiresChangeSnapshot state=$(Format-GatePath $StatePath)"
        exit 1
    }
    if ($ModeValue -ne 'formal' -or $StageValue -notin @('Execution', 'FinalExecution')) {
        Write-Host "GATE_STATE_BLOCKED gate=qa-test-gate qaPassRequiresFormalExecution mode=$ModeValue stage=$StageValue state=$(Format-GatePath $StatePath)"
        exit 1
    }
}

if ($Action -eq 'show') {
    $State = Read-GateState $StatePath
    $State | ConvertTo-Json -Depth 12
    exit 0
}

if ([string]::IsNullOrWhiteSpace($Gate)) {
    Write-Error 'Gate is required.'
}
Test-KnownGate $Gate

$State = Read-GateState $StatePath

if ($Action -eq 'record') {
    if ([string]::IsNullOrWhiteSpace($Verdict)) {
        Write-Error 'Verdict is required when recording gate state.'
    }
    Test-KnownVerdict $Verdict

    if ($Verdict -eq 'PASS') {
        $ArtifactPath = Resolve-ArtifactPath $Artifact
        if ([string]::IsNullOrWhiteSpace($ArtifactPath)) {
            Write-Host "GATE_STATE_BLOCKED gate=$Gate passArtifactRequired state=$(Format-GatePath $StatePath)"
            exit 1
        }
        if (-not (Test-Path -LiteralPath $ArtifactPath)) {
            Write-Host "GATE_STATE_BLOCKED gate=$Gate passArtifactMissing=$Artifact state=$(Format-GatePath $StatePath)"
            exit 1
        }
        Assert-FormalPassArtifact $Gate $ArtifactPath $WorkflowId $ChangeSnapshot $Stage
        if ($Gate -eq 'qa-test-gate') {
            Assert-QaPassWorkflow $WorkflowId $ChangeSnapshot $Mode $Stage
            if ($Stage -eq 'FinalExecution') {
                Assert-FinalQaAdmission $State $WorkflowId $ChangeSnapshot
            }
        }
        Assert-FormalWorkflowSnapshot $Gate $WorkflowId $ChangeSnapshot $Mode $Stage
        Assert-RecordAdmission $State $Gate $WorkflowId $ChangeSnapshot
    }

    $ArtifactHash = $null
    $ResolvedArtifactForHash = Resolve-ArtifactPath $Artifact
    if (-not [string]::IsNullOrWhiteSpace($ResolvedArtifactForHash) -and (Test-Path -LiteralPath $ResolvedArtifactForHash -PathType Leaf)) {
        $ArtifactHash = Get-Sha256 $ResolvedArtifactForHash
    }

    $Entry = [ordered]@{
        gate = $Gate
        verdict = $Verdict
        mode = $Mode
        stage = $Stage
        artifact = $Artifact
        artifactHash = $ArtifactHash
        actor = $Actor
        reason = $Reason
        workflowId = $WorkflowId
        changeSnapshot = $ChangeSnapshot
        manifestHash = $ManifestHash
        worktree = $Worktree
        baseRef = $BaseRef
        baseCommit = $BaseCommit
        headRef = $HeadRef
        headCommit = $HeadCommit
        attemptId = $AttemptId
        updatedAtUtc = [DateTime]::UtcNow.ToString('o')
    }

    $State.gates[$Gate] = $Entry
    $State.history = @($State.history) + @($Entry)
    Write-GateState $StatePath $State
    Write-Host "GATE_STATE_RECORDED gate=$Gate verdict=$Verdict workflowId=$WorkflowId state=$(Format-GatePath $StatePath)"
    exit 0
}

if ($Action -eq 'verify-admission') {
    $Requirements = @(Get-AdmissionRequirements $Gate)
    if ($Requirements.Count -gt 0 -and [string]::IsNullOrWhiteSpace($WorkflowId)) {
        Write-Host "GATE_STATE_BLOCKED gate=$Gate workflowIdRequired state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }
    if ($Requirements.Count -gt 0 -and [string]::IsNullOrWhiteSpace($ChangeSnapshot)) {
        Write-Host "GATE_STATE_BLOCKED gate=$Gate changeSnapshotRequired state=$(Format-GatePath $StatePath) $StandaloneSingleGateHint"
        exit 1
    }

    foreach ($Requirement in $Requirements) {
        Test-GateEntry $State $Requirement.gate $Gate $Requirement.verdict $Requirement.mode $Requirement.stage $WorkflowId $ChangeSnapshot $Requirement.artifact $RequireManifestHash
    }
    Write-Host "GATE_STATE_ADMISSION_PASS gate=$Gate workflowId=$WorkflowId changeSnapshot=$ChangeSnapshot prerequisites=$($Requirements.Count) state=$(Format-GatePath $StatePath)"
    exit 0
}

Test-GateEntry $State $Gate $Gate $RequireVerdict $RequireMode $RequireStage $RequireWorkflowId $ChangeSnapshot $RequireArtifactExists.IsPresent $RequireManifestHash

Write-Host "GATE_STATE_PASS gate=$Gate verdict=$($State.gates[$Gate].verdict) workflowId=$($State.gates[$Gate].workflowId) state=$(Format-GatePath $StatePath)"
exit 0

