$ErrorActionPreference = 'Stop'

function Resolve-FormalGateArtifactPath([string]$BasePath, [string]$MaybeRelativePath) {
    if ([string]::IsNullOrWhiteSpace($MaybeRelativePath)) { return $null }
    if ([System.IO.Path]::IsPathRooted($MaybeRelativePath)) { return $MaybeRelativePath }
    if ([string]::IsNullOrWhiteSpace($BasePath)) { $BasePath = (Get-Location).Path }
    return Join-Path $BasePath $MaybeRelativePath
}

function Get-FormalGateSha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-FormalGateArtifactFieldValue([string]$Text, [string]$FieldName) {
    $match = [regex]::Match($Text, "(?im)^[ \t]*" + [regex]::Escape($FieldName) + "[ \t]*:[ \t]*(.*?)[ \t]*$")
    if (-not $match.Success) { return $null }
    return $match.Groups[1].Value.Trim()
}

function Test-FormalGateMeaningfulArtifactField([string]$Text, [string]$FieldName) {
    $value = Get-FormalGateArtifactFieldValue $Text $FieldName
    if ([string]::IsNullOrWhiteSpace($value)) { return $false }
    if ($value -match '[<>]') { return $false }
    return $value -notmatch '(?i)^(unavailable|unknown|none|null|n/a|na|todo|tbd|placeholder|sample|example)$'
}

function Get-FormalGateArtifactReferencePath([string]$BasePath, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $null }
    return Resolve-FormalGateArtifactPath $BasePath $pathText
}

function Test-FormalGateArtifactReferenceExists([string]$BasePath, [string]$Value) {
    $path = Get-FormalGateArtifactReferencePath $BasePath $Value
    return (-not [string]::IsNullOrWhiteSpace($path) -and (Test-Path -LiteralPath $path -PathType Leaf))
}

function Test-FormalGateAnyMeaningfulArtifactField([string]$Text, [string[]]$FieldNames) {
    foreach ($field in $FieldNames) {
        if (Test-FormalGateMeaningfulArtifactField $Text $field) { return $true }
    }
    return $false
}

function Get-FormalGateContextBundleValidationErrors([string]$BasePath, [string]$Value) {
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
        $path = Resolve-FormalGateArtifactPath $BasePath $pathText
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            $errors += "Context bundle path does not exist: $pathText"
            continue
        }
        if (-not $shaMatch.Success) {
            $errors += "Context bundle sha256 missing: $pathText"
            continue
        }
        $expected = $shaMatch.Groups[1].Value.ToLowerInvariant()
        $actual = Get-FormalGateSha256 $path
        if ($actual -ne $expected) {
            $errors += "Context bundle sha256 mismatch: $pathText"
        }
    }
    if ($errors.Count -eq 0 -and [string]::IsNullOrWhiteSpace(($Value -split ',' | Where-Object { -not [string]::IsNullOrWhiteSpace($_.Trim()) } | Select-Object -First 1))) {
        $errors += 'Context bundle: <non-empty bundle path>'
    }
    return $errors
}

function Get-FormalGateImplementationEvidenceMissing([string]$Text, [string]$GateName, [string]$BasePath) {
    if ($GateName -notin @('complexity-gate', 'architecture-health-gate', 'code-quality-gate')) {
        return @()
    }
    $missing = @()
    if (-not (Test-FormalGateAnyMeaningfulArtifactField $Text @('Raw diff artifact', 'Changed files artifact'))) {
        $missing += 'Raw diff artifact or Changed files artifact'
    }
    else {
        $diffValue = Get-FormalGateArtifactFieldValue $Text 'Raw diff artifact'
        if ([string]::IsNullOrWhiteSpace($diffValue)) { $diffValue = Get-FormalGateArtifactFieldValue $Text 'Changed files artifact' }
        if (-not (Test-FormalGateArtifactReferenceExists $BasePath $diffValue)) { $missing += 'Raw diff/changed-files artifact path must exist' }
    }
    if (-not (Test-FormalGateAnyMeaningfulArtifactField $Text @('Developer self-test artifact', 'Verification artifact'))) {
        $missing += 'Developer self-test artifact or Verification artifact'
    }
    else {
        $verificationValue = Get-FormalGateArtifactFieldValue $Text 'Developer self-test artifact'
        if ([string]::IsNullOrWhiteSpace($verificationValue)) { $verificationValue = Get-FormalGateArtifactFieldValue $Text 'Verification artifact' }
        if (-not (Test-FormalGateArtifactReferenceExists $BasePath $verificationValue)) { $missing += 'Developer self-test/verification artifact path must exist' }
    }
    return $missing
}

function Get-FormalGateQaEvidenceMissing([string]$Text, [string]$GateName) {
    if ($GateName -ne 'qa-test-gate') { return @() }
    $missing = @()
    foreach ($field in @('Approved case set', 'QA-owned evidence', 'Case-to-artifact binding')) {
        if (-not (Test-FormalGateMeaningfulArtifactField $Text $field)) {
            $missing += "${field}: <non-empty>"
        }
    }
    return $missing
}

function Get-FormalGateFinalExecutionEvidenceMissing([string]$Text, [string]$GateName, [string]$StageValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$BasePath) {
    if ($GateName -ne 'qa-test-gate' -or $StageValue -ne 'FinalExecution') { return @() }
    $missing = @()
    $evidenceValue = Get-FormalGateArtifactFieldValue $Text 'QA-owned evidence'
    $evidencePath = Get-FormalGateArtifactReferencePath $BasePath $evidenceValue
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

function Get-FormalGateRouteFieldValue([string]$Text, [string]$FieldName) {
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

function Test-FormalGateAllowedRouteValue([string]$Value, [string[]]$AllowedValues) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    foreach ($allowed in $AllowedValues) {
        if ($Value -ieq $allowed) { return $true }
    }
    return $false
}

function Get-FormalGateRouteMissingForPass([string]$Text, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$GateName, [string]$StageValue) {
    $missing = @()
    $workflow = Get-FormalGateRouteFieldValue $Text 'workflow_id'
    $snapshot = Get-FormalGateRouteFieldValue $Text 'change_snapshot'
    $nextAction = Get-FormalGateRouteFieldValue $Text 'next_action'
    $reworkOwner = Get-FormalGateRouteFieldValue $Text 'rework_owner'
    $rerunFrom = Get-FormalGateRouteFieldValue $Text 'rerun_from'
    if ([string]::IsNullOrWhiteSpace($workflow)) { $missing += 'gate_route.workflow_id' }
    if ([string]::IsNullOrWhiteSpace($snapshot)) { $missing += 'gate_route.change_snapshot' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and $workflow -ne $ExpectedWorkflowId) { $missing += 'gate_route.workflow_id must match WorkflowId' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -and $snapshot -ne $ExpectedChangeSnapshot) { $missing += 'gate_route.change_snapshot must match ChangeSnapshot' }
    $isSealStage = $GateName -eq 'qa-test-gate' -and $StageValue -eq 'FinalExecution'
    if ($isSealStage) {
        if (-not (Test-FormalGateAllowedRouteValue $nextAction @('seal'))) { $missing += 'gate_route.next_action must be seal for FinalExecution PASS' }
    }
    elseif (-not (Test-FormalGateAllowedRouteValue $nextAction @('proceed'))) {
        $missing += 'gate_route.next_action must be proceed for non-final PASS'
    }
    if (-not (Test-FormalGateAllowedRouteValue $reworkOwner @('none'))) { $missing += 'gate_route.rework_owner must be none for PASS' }
    if (-not (Test-FormalGateAllowedRouteValue $rerunFrom @('none'))) { $missing += 'gate_route.rerun_from must be none for PASS' }
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

function Test-FormalGateArtifactFields([string]$ArtifactPath, [string[]]$RequiredFields, [string]$BasePath, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$GateName, [string]$StageValue) {
    if (-not (Test-Path -LiteralPath $ArtifactPath)) {
        return [pscustomobject]@{ Ok = $false; Missing = @("artifact missing: $ArtifactPath") }
    }
    $text = [string](Get-Content -LiteralPath $ArtifactPath -Raw)
    $missing = @($RequiredFields | Where-Object { $text -notmatch [regex]::Escape($_) })
    if (-not (Test-FormalGateMeaningfulArtifactField $text 'Reviewer agent id')) {
        $missing += 'Reviewer agent id: <non-empty independent agent id>'
    }
    if (-not (Test-FormalGateMeaningfulArtifactField $text 'Context bundle')) {
        $missing += 'Context bundle: <non-empty bundle path>'
    }
    else {
        $missing += @(Get-FormalGateContextBundleValidationErrors $BasePath (Get-FormalGateArtifactFieldValue $text 'Context bundle'))
    }
    $missing += @(Get-FormalGateQaEvidenceMissing $text $GateName)
    $missing += @(Get-FormalGateFinalExecutionEvidenceMissing $text $GateName $StageValue $ExpectedWorkflowId $ExpectedChangeSnapshot $BasePath)
    $missing += @(Get-FormalGateImplementationEvidenceMissing $text $GateName $BasePath)
    $missing += @(Get-FormalGateRouteMissingForPass $text $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue)
    return [pscustomobject]@{ Ok = ($missing.Count -eq 0); Missing = $missing }
}
