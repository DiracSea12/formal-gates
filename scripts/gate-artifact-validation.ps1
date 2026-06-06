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
    if ($value -match '<[^>\r\n]+>') { return $false }
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

function Get-FormalGateStableRequirementIds([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    return @([regex]::Matches($Text, '\bRQ-\d{3,}\b', [System.Text.RegularExpressions.RegexOptions]::IgnoreCase) |
        ForEach-Object { $_.Value.ToUpperInvariant() } |
        Sort-Object -Unique)
}

function Get-FormalGateDeclaredIdList([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return @() }
    if ($Value -match '(?i)^\s*(none|first_run|not_applicable)\s*$') { return @() }
    return Get-FormalGateStableRequirementIds $Value
}

function Test-FormalGateNoneValue([string]$Value) {
    return (-not [string]::IsNullOrWhiteSpace($Value) -and $Value -match '(?i)^\s*none\s*$')
}

function Test-FormalGateYesValue([string]$Value) {
    return (-not [string]::IsNullOrWhiteSpace($Value) -and $Value -match '(?i)^\s*(yes|true|user_confirmed|confirmed)\s*$')
}

function Test-FormalGatePassOrNotApplicableValue([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    return ($Value -match '(?i)^\s*PASS\s*$' -or $Value -match '(?i)^\s*NOT_APPLICABLE\s*:\s*\S.+$')
}

function Test-FormalGateMeaningfulPlainValue([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    if ($Value -match '<[^>\r\n]+>') { return $false }
    return $Value.Trim() -notmatch '(?i)^(unavailable|unknown|none|null|n/a|na|todo|tbd|placeholder|sample|example)$'
}

function ConvertTo-FormalGateRelativePath([string]$BasePath, [string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    $trimmed = $Path.Trim().Trim('"', "'").Replace('\', '/')
    if ([string]::IsNullOrWhiteSpace($trimmed)) { return $null }
    try {
        if ([System.IO.Path]::IsPathRooted($trimmed)) {
            $baseFull = [System.IO.Path]::GetFullPath($BasePath).TrimEnd('\', '/')
            $pathFull = [System.IO.Path]::GetFullPath($trimmed)
            if ($pathFull.Equals($baseFull, [System.StringComparison]::OrdinalIgnoreCase)) { return '.' }
            if ($pathFull.StartsWith($baseFull + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
                $trimmed = $pathFull.Substring($baseFull.Length).TrimStart('\', '/').Replace('\', '/')
            }
        }
    }
    catch {
    }
    $normalized = $trimmed.TrimStart('/')
    while ($normalized.StartsWith('./')) { $normalized = $normalized.Substring(2) }
    return $normalized
}

function Get-FormalGateCoveredFormalTargets([string]$Text) {
    $value = Get-FormalGateArtifactFieldValue $Text 'Covered formal targets'
    if ([string]::IsNullOrWhiteSpace($value)) { return @() }
    return @($value -split ',' | ForEach-Object { $_.Trim().Trim('"', "'").Replace('\', '/') } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

function Get-FormalGateCoveredFormalTargetsValidationErrors([string]$Text) {
    $errors = @()
    $targets = @(Get-FormalGateCoveredFormalTargets $Text)
    if ($targets.Count -eq 0) {
        return @('Covered formal targets: <non-empty relative path or directory prefix>')
    }
    foreach ($target in $targets) {
        $normalized = $target.Trim()
        while ($normalized.StartsWith('./')) { $normalized = $normalized.Substring(2) }
        $normalizedKey = $normalized.TrimEnd('/').ToLowerInvariant()
        if ([string]::IsNullOrWhiteSpace($normalized) -or $normalized -in @('.', '/', '*')) {
            $errors += 'Covered formal targets must not be repository root or wildcard'
            continue
        }
        if ($normalizedKey -in @('docs', 'openspec/changes', 'docs/prd', 'docs/sdd', 'docs/phases', 'docs/requirements', 'docs/specs')) {
            $errors += "Covered formal targets must name a concrete document scope, not a broad directory: $target"
        }
        if ($normalized -match '[*?]') {
            $errors += "Covered formal targets must not contain wildcards: $target"
        }
        if ([System.IO.Path]::IsPathRooted($normalized)) {
            $errors += "Covered formal targets must be relative paths: $target"
        }
    }
    return $errors
}

function Test-FormalGateCoveredFormalTarget([string]$Text, [string]$BasePath, [string]$TargetPath) {
    $target = ConvertTo-FormalGateRelativePath $BasePath $TargetPath
    if ([string]::IsNullOrWhiteSpace($target) -or $target -eq '.') { return $false }
    $target = $target.TrimEnd('/')
    foreach ($covered in @(Get-FormalGateCoveredFormalTargets $Text)) {
        $prefix = (ConvertTo-FormalGateRelativePath $BasePath $covered)
        if ([string]::IsNullOrWhiteSpace($prefix) -or $prefix -eq '.' -or $prefix -match '[*?]') { continue }
        $prefix = $prefix.TrimEnd('/')
        if ($target.Equals($prefix, [System.StringComparison]::OrdinalIgnoreCase)) { return $true }
        if ($target.StartsWith($prefix + '/', [System.StringComparison]::OrdinalIgnoreCase)) { return $true }
    }
    return $false
}

function Test-FormalGateAnyMeaningfulArtifactField([string]$Text, [string[]]$FieldNames) {
    foreach ($field in $FieldNames) {
        if (Test-FormalGateMeaningfulArtifactField $Text $field) { return $true }
    }
    return $false
}

function Get-FormalGateDecisionBindingErrors([string]$BasePath, [string]$DecisionText, [string]$AlignmentArtifactValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot) {
    $errors = @()
    $approvedAlignmentArtifact = Get-FormalGateArtifactFieldValue $DecisionText 'Approved alignment artifact'
    $approvedWorkflowId = Get-FormalGateArtifactFieldValue $DecisionText 'Approved workflow id'
    $approvedChangeSnapshot = Get-FormalGateArtifactFieldValue $DecisionText 'Approved change snapshot'
    $currentAlignmentPath = Get-FormalGateArtifactReferencePath $BasePath $AlignmentArtifactValue
    $approvedAlignmentPath = Get-FormalGateArtifactReferencePath $BasePath $approvedAlignmentArtifact

    if ([string]::IsNullOrWhiteSpace($approvedAlignmentArtifact) -or [string]::IsNullOrWhiteSpace($approvedAlignmentPath) -or [string]::IsNullOrWhiteSpace($currentAlignmentPath) -or
        -not [System.IO.Path]::GetFullPath($approvedAlignmentPath).Equals([System.IO.Path]::GetFullPath($currentAlignmentPath), [System.StringComparison]::OrdinalIgnoreCase)) {
        $errors += 'Decision record must bind to the current alignment artifact'
    }

    if ([string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -or [string]::IsNullOrWhiteSpace($ExpectedChangeSnapshot) -or
        $approvedWorkflowId -ne $ExpectedWorkflowId -or $approvedChangeSnapshot -ne $ExpectedChangeSnapshot) {
        $errors += 'Decision record must bind to the current workflow and change snapshot'
    }

    return $errors
}

function Test-FormalGateUserApprovalText([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    return $Text -match '(?im)^[ \t]*(User confirmation|User approved|User approval|User original|User quote|User decision)[ \t]*:[ \t]*(YES|confirmed|approved|".+"|''.+''|\S.+)[ \t]*$'
}

function Get-FormalGateUserApprovalArtifactValidationErrors([string]$BasePath, [string]$Value, [string[]]$AlignmentIds, [string]$AlignmentArtifactValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string[]]$DroppedIds) {
    $errors = @()
    $path = Get-FormalGateArtifactReferencePath $BasePath $Value
    if ([string]::IsNullOrWhiteSpace($path) -or -not (Test-Path -LiteralPath $path -PathType Leaf)) { return @('Decision record must point to a dedicated .claude/gates/artifacts requirements-user-decision artifact with USER_CONFIRMATION fields') }
    $baseFull = [System.IO.Path]::GetFullPath((Resolve-FormalGateArtifactPath $BasePath '.claude/gates/artifacts')).TrimEnd('\', '/')
    $pathFull = [System.IO.Path]::GetFullPath($path)
    if (-not $pathFull.StartsWith($baseFull + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) { return @('Decision record must point to a dedicated .claude/gates/artifacts requirements-user-decision artifact with USER_CONFIRMATION fields') }
    $fileName = [System.IO.Path]::GetFileName($pathFull)
    if ($fileName -notmatch '(?i)^(requirements-user-decision|user-requirements-decision)[^\\/]*\.(md|markdown)$') { return @('Decision record must point to a dedicated .claude/gates/artifacts requirements-user-decision artifact with USER_CONFIRMATION fields') }
    $text = [string](Get-Content -LiteralPath $path -Raw -Encoding UTF8)
    if ((Get-FormalGateArtifactFieldValue $text 'Decision record type') -notmatch '(?i)^\s*USER_CONFIRMATION\s*$') { $errors += 'Decision record type must be USER_CONFIRMATION' }
    if (-not (Test-FormalGateYesValue (Get-FormalGateArtifactFieldValue $text 'User confirmation'))) { $errors += 'Decision record User confirmation must be YES' }
    $userOriginal = Get-FormalGateArtifactFieldValue $text 'User original'
    $userQuote = Get-FormalGateArtifactFieldValue $text 'User quote'
    if (-not (Test-FormalGateMeaningfulPlainValue $userOriginal) -and -not (Test-FormalGateMeaningfulPlainValue $userQuote)) { $errors += 'Decision record must include User original or User quote' }
    $approvedIds = Get-FormalGateArtifactFieldValue $text 'Approved alignment IDs'
    if ($approvedIds -notmatch '(?i)^\s*(all|RQ-\d{3,}(\s*,\s*RQ-\d{3,})*)\s*$') {
        $errors += 'Decision record Approved alignment IDs must be all or a RQ-### list'
    }
    else {
        $errors += @(Get-FormalGateDecisionBindingErrors $BasePath $text $AlignmentArtifactValue $ExpectedWorkflowId $ExpectedChangeSnapshot)
        if ($approvedIds -notmatch '(?i)^\s*all\s*$') {
            $approvedIdSet = @{}
            foreach ($id in @(Get-FormalGateDeclaredIdList $approvedIds)) { $approvedIdSet[$id] = $true }
            $unapproved = @($AlignmentIds | Where-Object { -not $approvedIdSet.ContainsKey($_) })
            if ($unapproved.Count -gt 0) {
                $errors += "Decision record Approved alignment IDs does not cover current alignment IDs: $($unapproved -join ',')"
            }
        }
    }
    if ((Get-FormalGateArtifactFieldValue $text 'Approval scope') -notmatch '(?i)^\s*requirements-clarification-gate\s*$') { $errors += 'Decision record Approval scope must be requirements-clarification-gate' }

    if ($DroppedIds.Count -gt 0) {
        $approvedDroppedSet = @{}
        foreach ($id in @(Get-FormalGateDeclaredIdList (Get-FormalGateArtifactFieldValue $text 'Approved dropped IDs'))) { $approvedDroppedSet[$id] = $true }
        $missingDropped = @()
        foreach ($id in $DroppedIds) {
            if ($approvedDroppedSet.ContainsKey($id)) { continue }
            $droppedApproval = $null
            foreach ($fieldName in @("Dropped $id", "Dropped ID $id")) {
                $droppedApproval = Get-FormalGateArtifactFieldValue $text $fieldName
                if (Test-FormalGateMeaningfulPlainValue $droppedApproval) { break }
            }
            if (-not (Test-FormalGateMeaningfulPlainValue $droppedApproval)) {
                $missingDropped += $id
            }
        }
        if ($missingDropped.Count -gt 0) {
            $errors += "Dropped question IDs require explicit decision record approval: $($missingDropped -join ',')"
        }
    }
    return $errors
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

function Get-FormalGateExpectedPromptSource([string]$GateName) {
    if ($GateName -eq 'qa-test-gate') { return 'agents/qa-test-gate.md' }
    if ($GateName -eq 'complexity-gate') { return 'agents/complexity-gate.md' }
    if ($GateName -eq 'architecture-health-gate') { return 'agents/architecture-health-gate.md' }
    if ($GateName -eq 'code-quality-gate') { return 'agents/code-quality-gate.md' }
    if ([string]::IsNullOrWhiteSpace($GateName)) { return 'agents/<gate>.md' }
    return "agents/$GateName.md"
}

function Get-FormalGatePromptIntegrityErrors([string]$Text, [string]$GateName) {
    if ($GateName -eq 'requirements-clarification-gate') { return @() }
    $errors = @()
    $requiredValues = [ordered]@{
        'Review mode' = 'ZERO_CONTEXT_FORMAL'
        'Prompt contamination check' = 'PASS'
        'Prompt source' = (Get-FormalGateExpectedPromptSource $GateName)
    }
    foreach ($field in $requiredValues.Keys) {
        $value = Get-FormalGateArtifactFieldValue $Text $field
        if ([string]::IsNullOrWhiteSpace($value)) {
            $errors += "${field}: $($requiredValues[$field])"
            continue
        }
        if ($value -cne $requiredValues[$field]) {
            $errors += "${field} must be $($requiredValues[$field])"
        }
    }

    $contaminationLabels = @(
        'Known issues',
        'Previous findings',
        'Just fixed',
        'Expected answer',
        'Expected PASS/FAIL',
        'Focus items',
        ([string]::new([char[]]@([char]0x91CD, [char]0x70B9, [char]0x590D, [char]0x67E5))),
        ([string]::new([char[]]@([char]0x521A, [char]0x4FEE, [char]0x4E86)))
    )
    foreach ($label in $contaminationLabels) {
        $pattern = '(?im)^[ \t]*' + [regex]::Escape($label) + '[ \t]*:'
        if ($Text -match $pattern) {
            $errors += "prompt contamination: forbidden anchoring field present: $label"
        }
    }
    return $errors
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

function Get-FormalGateAlignmentBlockRecords([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $matches = @([regex]::Matches($Text, '(?im)^[ \t]*ID[ \t]*:[ \t]*(RQ-\d{3,})[ \t]*$'))
    $records = @()
    for ($index = 0; $index -lt $matches.Count; ++$index) {
        $start = $matches[$index].Index
        $end = if ($index + 1 -lt $matches.Count) { $matches[$index + 1].Index } else { $Text.Length }
        $records += [pscustomobject]@{
            Id = $matches[$index].Groups[1].Value.ToUpperInvariant()
            Text = $Text.Substring($start, $end - $start)
        }
    }
    return $records
}

function Get-FormalGateAlignmentTableRows([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $rows = @()
    $header = $null
    foreach ($line in ($Text -split "`r?`n")) {
        if ($line -notmatch '^\s*\|') { continue }
        $cells = @($line.Trim().Trim('|') -split '\|' | ForEach-Object { $_.Trim() })
        if ($cells.Count -lt 2) { continue }
        if (($cells -join '|') -match '^-+$|^\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*$') { continue }
        if ($null -eq $header -and ($cells | Where-Object { $_ -match '(?i)^ID$' }).Count -gt 0 -and ($cells | Where-Object { $_ -match '(?i)^Status$' }).Count -gt 0) {
            $header = $cells
            continue
        }
        if ($null -eq $header) { continue }
        $idIndex = -1
        for ($i = 0; $i -lt $header.Count; ++$i) {
            if ($header[$i] -match '(?i)^ID$') {
                $idIndex = $i
                break
            }
        }
        if ($idIndex -lt 0 -or $idIndex -ge $cells.Count -or $cells[$idIndex] -notmatch '(?i)\bRQ-\d{3,}\b') { continue }
        $map = @{}
        for ($i = 0; $i -lt $header.Count -and $i -lt $cells.Count; ++$i) {
            $map[$header[$i]] = $cells[$i]
        }
        $rows += [pscustomobject]@{
            Id = ([regex]::Match($cells[$idIndex], '(?i)\bRQ-\d{3,}\b').Value.ToUpperInvariant())
            Fields = $map
        }
    }
    return $rows
}

function Get-FormalGateAlignmentRecordValidationErrors([string]$Text) {
    $errors = @()
    $ids = @(Get-FormalGateStableRequirementIds $Text)
    if ($ids.Count -eq 0) { return @() }

    $requiredLabels = @('Requirement or question', 'Source', 'Why it matters', 'Status', 'User answer', 'Downstream effect', 'OpenSpec impact', 'Evidence needed')
    $allowedPassStatuses = @('confirmed', 'deferred-by-user', 'out-of-scope-by-user')
    $blockRecords = @(Get-FormalGateAlignmentBlockRecords $Text)
    $tableRows = @(Get-FormalGateAlignmentTableRows $Text)
    $seen = @{}

    foreach ($record in $blockRecords) {
        $seen[$record.Id] = $true
        foreach ($label in $requiredLabels) {
            $value = Get-FormalGateArtifactFieldValue $record.Text $label
            if (-not (Test-FormalGateMeaningfulPlainValue $value)) {
                $errors += "Alignment item $($record.Id) missing meaningful '$label'"
            }
        }
        $statusValue = Get-FormalGateArtifactFieldValue $record.Text 'Status'
        $status = if ($null -eq $statusValue) { '' } else { $statusValue.Trim().ToLowerInvariant() }
        if ($allowedPassStatuses -notcontains $status) {
            $errors += "Alignment item $($record.Id) status must be confirmed/deferred-by-user/out-of-scope-by-user for PASS"
        }
        if ($status -in @('deferred-by-user', 'out-of-scope-by-user')) {
            if (-not (Test-FormalGateUserApprovalText $record.Text)) {
                $errors += "Alignment item $($record.Id) status $status requires per-item user approval evidence"
            }
        }
    }

    foreach ($row in $tableRows) {
        if ($seen.ContainsKey($row.Id)) { continue }
        $seen[$row.Id] = $true
        foreach ($label in $requiredLabels) {
            $value = $null
            foreach ($key in $row.Fields.Keys) {
                if ($key -ieq $label) {
                    $value = [string]$row.Fields[$key]
                    break
                }
            }
            if (-not (Test-FormalGateMeaningfulPlainValue $value)) {
                $errors += "Alignment item $($row.Id) missing meaningful '$label'"
            }
        }
        $status = $null
        foreach ($key in $row.Fields.Keys) {
            if ($key -ieq 'Status') {
                $status = ([string]$row.Fields[$key]).Trim().ToLowerInvariant()
                break
            }
        }
        if ($allowedPassStatuses -notcontains $status) {
            $errors += "Alignment item $($row.Id) status must be confirmed/deferred-by-user/out-of-scope-by-user for PASS"
        }
        if ($status -in @('deferred-by-user', 'out-of-scope-by-user')) {
            $hasApproval = $false
            foreach ($key in $row.Fields.Keys) {
                if ($key -match '(?i)^(User confirmation|User approved|User approval|User original|User quote|User decision)$' -and (Test-FormalGateMeaningfulPlainValue ([string]$row.Fields[$key]))) {
                    $hasApproval = $true
                    break
                }
            }
            if (-not $hasApproval) {
                $errors += "Alignment item $($row.Id) status $status requires per-item user approval evidence"
            }
        }
    }

    $unparsed = @($ids | Where-Object { -not $seen.ContainsKey($_) })
    if ($unparsed.Count -gt 0) {
        $errors += "Alignment table has RQ IDs without parseable complete records: $($unparsed -join ',')"
    }
    return $errors
}

function Get-FormalGateLatestHistoricalRequirementsClarificationPass([string]$BasePath, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot) {
    $statePath = Resolve-FormalGateArtifactPath $BasePath '.claude/gates/gate-state.json'
    if ([string]::IsNullOrWhiteSpace($statePath) -or -not (Test-Path -LiteralPath $statePath -PathType Leaf)) { return $null }
    try {
        $state = Get-Content -LiteralPath $statePath -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        return $null
    }
    $entries = @()
    if ($state.PSObject.Properties.Name -contains 'history' -and $null -ne $state.history) {
        $entries += @($state.history)
    }
    if ($state.PSObject.Properties.Name -contains 'gates' -and $null -ne $state.gates -and $state.gates.PSObject.Properties.Name -contains 'requirements-clarification-gate') {
        $entries += @($state.gates.'requirements-clarification-gate')
    }
    for ($index = $entries.Count - 1; $index -ge 0; --$index) {
        $entry = $entries[$index]
        if ($entry.gate -ne 'requirements-clarification-gate' -or $entry.verdict -ne 'PASS') { continue }
        if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and $entry.workflowId -ne $ExpectedWorkflowId) { continue }
        return $entry
    }
    return $null
}

function Get-FormalGateRequirementsClarificationMissing([string]$Text, [string]$GateName, [string]$BasePath, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot) {
    if ($GateName -ne 'requirements-clarification-gate') { return @() }

    $missing = @()
    foreach ($field in @('Requirement source', 'Alignment table artifact', 'Total alignment items', 'Previous alignment artifact', 'User confirmation', 'Coverage scan', 'Scope preservation check', 'Task proof check', 'Decision record', 'Covered formal targets', 'Downstream permission')) {
        if (-not (Test-FormalGateMeaningfulArtifactField $Text $field)) {
            $missing += "${field}: <non-empty>"
        }
    }
    $missing += @(Get-FormalGateCoveredFormalTargetsValidationErrors $Text)

    $alignmentValue = Get-FormalGateArtifactFieldValue $Text 'Alignment table artifact'
    $alignmentPath = Get-FormalGateArtifactReferencePath $BasePath $alignmentValue
    $alignmentText = $null
    if ([string]::IsNullOrWhiteSpace($alignmentPath) -or -not (Test-Path -LiteralPath $alignmentPath -PathType Leaf)) {
        $missing += 'Alignment table artifact path must exist'
    }
    else {
        $alignmentText = [string](Get-Content -LiteralPath $alignmentPath -Raw -Encoding UTF8)
        foreach ($label in @('ID', 'Requirement or question', 'Source', 'Why it matters', 'Status', 'User answer', 'Downstream effect', 'OpenSpec impact', 'Evidence needed')) {
            if ($alignmentText -notmatch [regex]::Escape($label)) {
                $missing += "Alignment table missing column/field: $label"
            }
        }
        if ($alignmentText -match '<[^>\r\n]+>') {
            $missing += 'Alignment table contains placeholder text'
        }
        if ($alignmentText -match '(?im)^[ \t]*Status[ \t]*:[ \t]*(open|inferred|doc-derived)\b' -or $alignmentText -match '(?im)\|[ \t]*(open|inferred|doc-derived)[ \t]*\|') {
            $missing += 'Alignment table still contains open/inferred/doc-derived items'
        }
        $missing += @(Get-FormalGateAlignmentRecordValidationErrors $alignmentText)

        $alignmentIds = @(Get-FormalGateStableRequirementIds $alignmentText)
        if ($alignmentIds.Count -eq 0) {
            $missing += 'Alignment table must use stable RQ-### IDs'
        }

        $declaredCountValue = Get-FormalGateArtifactFieldValue $Text 'Total alignment items'
        $declaredCount = 0
        if (-not [int]::TryParse($declaredCountValue, [ref]$declaredCount)) {
            $missing += 'Total alignment items must be an integer'
        }
        elseif ($alignmentIds.Count -ne $declaredCount) {
            $missing += "Total alignment items must match unique RQ-### IDs in alignment table: declared=$declaredCount actual=$($alignmentIds.Count)"
        }

        $previousValue = Get-FormalGateArtifactFieldValue $Text 'Previous alignment artifact'
        $historicalPass = Get-FormalGateLatestHistoricalRequirementsClarificationPass $BasePath $ExpectedWorkflowId $ExpectedChangeSnapshot
        $historicalAlignmentPath = $null
        if ($null -ne $historicalPass) {
            $historicalPassPath = Get-FormalGateArtifactReferencePath $BasePath ([string]$historicalPass.artifact)
            if ([string]::IsNullOrWhiteSpace($historicalPassPath) -or -not (Test-Path -LiteralPath $historicalPassPath -PathType Leaf)) {
                $missing += 'Historical requirements-clarification PASS artifact path must exist before recording another PASS for the same workflow'
            }
            else {
                $historicalPassText = [string](Get-Content -LiteralPath $historicalPassPath -Raw -Encoding UTF8)
                $historicalAlignmentValue = Get-FormalGateArtifactFieldValue $historicalPassText 'Alignment table artifact'
                $historicalAlignmentPath = Get-FormalGateArtifactReferencePath $BasePath $historicalAlignmentValue
                if ([string]::IsNullOrWhiteSpace($historicalAlignmentPath) -or -not (Test-Path -LiteralPath $historicalAlignmentPath -PathType Leaf)) {
                    $missing += 'Historical requirements-clarification PASS must point to an existing Alignment table artifact'
                }
            }
        }
        if ($previousValue -match '(?i)^\s*FIRST_RUN\s*$') {
            if ($null -ne $historicalPass) {
                $missing += 'Previous alignment artifact cannot be FIRST_RUN when a historical requirements-clarification PASS exists for the same workflow'
            }
        }
        else {
            $previousPath = Get-FormalGateArtifactReferencePath $BasePath $previousValue
            if ([string]::IsNullOrWhiteSpace($previousPath) -or -not (Test-Path -LiteralPath $previousPath -PathType Leaf)) {
                $missing += 'Previous alignment artifact must be FIRST_RUN or an existing file'
            }
            else {
                if (-not [string]::IsNullOrWhiteSpace($historicalAlignmentPath) -and
                    -not [System.IO.Path]::GetFullPath($previousPath).Equals([System.IO.Path]::GetFullPath($historicalAlignmentPath), [System.StringComparison]::OrdinalIgnoreCase)) {
                    $missing += 'Previous alignment artifact must match the latest historical Alignment table artifact for the same workflow'
                }
                $previousText = [string](Get-Content -LiteralPath $previousPath -Raw -Encoding UTF8)
                $previousIds = @(Get-FormalGateStableRequirementIds $previousText)
                $currentIdSet = @{}
                foreach ($id in $alignmentIds) { $currentIdSet[$id] = $true }
                $declaredDropped = @(Get-FormalGateDeclaredIdList (Get-FormalGateArtifactFieldValue $Text 'Dropped question IDs'))
                $declaredDroppedSet = @{}
                foreach ($id in $declaredDropped) { $declaredDroppedSet[$id] = $true }
                $silentlyRemoved = @($previousIds | Where-Object { -not $currentIdSet.ContainsKey($_) -and -not $declaredDroppedSet.ContainsKey($_) })
                if ($silentlyRemoved.Count -gt 0) {
                    $missing += "Previous alignment IDs removed without listing Dropped question IDs: $($silentlyRemoved -join ',')"
                }
            }
        }
    }

    $openQuestionValue = Get-FormalGateArtifactFieldValue $Text 'Open question IDs'
    if (-not (Test-FormalGateNoneValue $openQuestionValue)) {
        $missing += 'Open question IDs must be none for PASS'
    }

    $openBlockersValue = Get-FormalGateArtifactFieldValue $Text 'Open blockers'
    if (-not (Test-FormalGateNoneValue $openBlockersValue)) {
        $missing += 'Open blockers must be none for PASS'
    }

    $droppedValue = Get-FormalGateArtifactFieldValue $Text 'Dropped question IDs'
    $droppedApproval = Get-FormalGateArtifactFieldValue $Text 'Dropped question approval'
    if (-not (Test-FormalGateNoneValue $droppedValue) -and -not (Test-FormalGateYesValue $droppedApproval)) {
        $missing += 'Dropped question IDs require Dropped question approval: YES'
    }

    if (-not (Test-FormalGateYesValue (Get-FormalGateArtifactFieldValue $Text 'User confirmation'))) {
        $missing += 'User confirmation must be YES for PASS'
    }

    $coverage = Get-FormalGateArtifactFieldValue $Text 'Coverage scan'
    if ($coverage -notmatch '(?i)^\s*PASS\s*$') {
        $missing += 'Coverage scan must be PASS'
    }

    if (-not (Test-FormalGatePassOrNotApplicableValue (Get-FormalGateArtifactFieldValue $Text 'Scope preservation check'))) {
        $missing += 'Scope preservation check must be PASS or NOT_APPLICABLE: <reason>'
    }

    if (-not (Test-FormalGatePassOrNotApplicableValue (Get-FormalGateArtifactFieldValue $Text 'Task proof check'))) {
        $missing += 'Task proof check must be PASS or NOT_APPLICABLE: <reason>'
    }

    $decisionValue = Get-FormalGateArtifactFieldValue $Text 'Decision record'
    $decisionDroppedIds = @(Get-FormalGateDeclaredIdList (Get-FormalGateArtifactFieldValue $Text 'Dropped question IDs'))
    $decisionAlignmentIds = if ($null -eq $alignmentText) { @() } else { @(Get-FormalGateStableRequirementIds $alignmentText) }
    $missing += @(Get-FormalGateUserApprovalArtifactValidationErrors $BasePath $decisionValue $decisionAlignmentIds $alignmentValue $ExpectedWorkflowId $ExpectedChangeSnapshot $decisionDroppedIds)

    $downstream = Get-FormalGateArtifactFieldValue $Text 'Downstream permission'
    if ($downstream -notmatch '(?i)^\s*READY_TO_DRAFT\s*$') {
        $missing += 'Downstream permission must be READY_TO_DRAFT for PASS'
    }

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
    if ($GateName -eq 'requirements-clarification-gate') {
        return @(
            'Requirement source:',
            'Alignment table artifact:',
            'Total alignment items:',
            'Open question IDs:',
            'User confirmation:',
            'Dimension coverage:',
            'Decision record:',
            'Covered formal targets:',
            'Downstream permission:',
            'gate_route:'
        )
    }

    $zeroContextFields = @(
        'Review mode: ZERO_CONTEXT_FORMAL',
        'Prompt contamination check: PASS',
        "Prompt source: $(Get-FormalGateExpectedPromptSource $GateName)",
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
    if ($GateName -ne 'requirements-clarification-gate') {
        $missing += @(Get-FormalGatePromptIntegrityErrors $text $GateName)
        if (-not (Test-FormalGateMeaningfulArtifactField $text 'Reviewer agent id')) {
            $missing += 'Reviewer agent id: <non-empty independent agent id>'
        }
        if (-not (Test-FormalGateMeaningfulArtifactField $text 'Context bundle')) {
            $missing += 'Context bundle: <non-empty bundle path>'
        }
        else {
            $missing += @(Get-FormalGateContextBundleValidationErrors $BasePath (Get-FormalGateArtifactFieldValue $text 'Context bundle'))
        }
    }
    $missing += @(Get-FormalGateRequirementsClarificationMissing $text $GateName $BasePath $ExpectedWorkflowId $ExpectedChangeSnapshot)
    $missing += @(Get-FormalGateQaEvidenceMissing $text $GateName)
    $missing += @(Get-FormalGateFinalExecutionEvidenceMissing $text $GateName $StageValue $ExpectedWorkflowId $ExpectedChangeSnapshot $BasePath)
    $missing += @(Get-FormalGateImplementationEvidenceMissing $text $GateName $BasePath)
    $missing += @(Get-FormalGateRouteMissingForPass $text $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue)
    return [pscustomobject]@{ Ok = ($missing.Count -eq 0); Missing = $missing }
}
