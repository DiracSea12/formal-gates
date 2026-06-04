$ErrorActionPreference = 'Stop'

$formalGatesPowerShellHost = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/powershell-host.ps1'
if (Test-Path -LiteralPath $formalGatesPowerShellHost) {
    . $formalGatesPowerShellHost
}

$inputJson = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($inputJson)) { exit 0 }

try {
    $payload = $inputJson | ConvertFrom-Json
}
catch {
    exit 0
}

$toolName = [string]$payload.tool_name
$toolInput = $payload.tool_input
$inputProperties = @{}
if ($null -ne $toolInput) {
    foreach ($property in $toolInput.PSObject.Properties) {
        $inputProperties[$property.Name] = $property.Value
    }
}
$commandText = [string]$inputProperties['command']
$rawIntentText = (($inputProperties['args'], $inputProperties['prompt'], $inputProperties['description'], $commandText) | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }) -join ' '

$gateAliases = @{
    'qa-test-gate' = 'qa-test-gate'
    'complexity-gate' = 'complexity-gate'
    'architecture-health-gate' = 'architecture-health-gate'
    'code-quality-gate' = 'code-quality-gate'
}
$routerSkillNames = @('formal-gates')

function Format-HookPath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    try {
        $full = [System.IO.Path]::GetFullPath($Path)
        $home = [System.IO.Path]::GetFullPath($HOME).TrimEnd('\', '/')
        if ($full.StartsWith($home, [System.StringComparison]::OrdinalIgnoreCase)) {
            return ('~' + $full.Substring($home.Length)).Replace('\', '/')
        }
        return $full.Replace('\', '/')
    }
    catch {
        return $Path.Replace('\', '/')
    }
}

function Block-Gate([string]$Message, [string]$Reason) {
    $displayReason = $Reason
    if (-not [string]::IsNullOrWhiteSpace($Message) -and $Message -ne $Reason) {
        $displayReason = "$Message $Reason"
    }
    [pscustomobject]@{
        decision = 'block'
        reason = $displayReason
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            permissionDecision = 'deny'
            permissionDecisionReason = $displayReason
        }
    } | ConvertTo-Json -Depth 6 -Compress
    exit 2
}

function Invoke-GateState([string[]]$Arguments, [string]$WorkingDirectory) {
    $stdoutPath = [System.IO.Path]::GetTempFileName()
    $stderrPath = [System.IO.Path]::GetTempFileName()
    try {
        $process = Start-Process -FilePath (Get-FormalGatesPowerShellExe) -ArgumentList $Arguments -WorkingDirectory $WorkingDirectory -NoNewWindow -PassThru -Wait -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $stdout = Get-Content -LiteralPath $stdoutPath -Raw -ErrorAction SilentlyContinue
        $stderr = Get-Content -LiteralPath $stderrPath -Raw -ErrorAction SilentlyContinue
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            Output = (($stdout, $stderr) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) -join "`n"
        }
    }
    finally {
        Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Get-ToolGateIntent {
    if ($toolName -eq 'Skill') {
        $skillName = [string]$inputProperties['skill']
        if ($gateAliases.ContainsKey($skillName)) { return $gateAliases[$skillName] }
        if ($routerSkillNames -contains $skillName) { return $skillName }
        return $null
    }
    if ($toolName -eq 'Agent') {
        if ($rawIntentText -match '(?i)(architecture-health-gate|architecture\s+gate|架构门)') { return 'architecture-health-gate' }
        if ($rawIntentText -match '(?i)(code-quality-gate|code\s+quality\s+gate|代码质量门)') { return 'code-quality-gate' }
        if ($rawIntentText -match '(?i)(qa-test-gate|qa\s+gate|测试门)') { return 'qa-test-gate' }
        if ($rawIntentText -match '(?i)(complexity-gate|complexity\s+gate|复杂度门)') { return 'complexity-gate' }
    }
    return $null
}

function Find-JsonObjectAfterKey([string]$Text, [string[]]$Keys) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $null }
    foreach ($key in $Keys) {
        $match = [regex]::Match($Text, '(?i)(^|[\s,;])' + [regex]::Escape($key) + '\s*[:=]\s*\{')
        if (-not $match.Success) { continue }
        $openIndex = $match.Index + $match.Value.LastIndexOf('{')
        $depth = 0
        $inString = $false
        $escaped = $false
        for ($index = $openIndex; $index -lt $Text.Length; ++$index) {
            $ch = $Text[$index]
            if ($inString) {
                if ($escaped) { $escaped = $false; continue }
                if ($ch -eq '\') { $escaped = $true; continue }
                if ($ch -eq '"') { $inString = $false; continue }
                continue
            }
            if ($ch -eq '"') { $inString = $true; continue }
            if ($ch -eq '{') { ++$depth; continue }
            if ($ch -eq '}') {
                --$depth
                if ($depth -eq 0) { return $Text.Substring($openIndex, $index - $openIndex + 1) }
            }
        }
    }
    return $null
}

function Convert-ToWorkflowObject($Value) {
    if ($null -eq $Value) { return $null }
    if ($Value -is [string]) {
        if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
        try { return $Value | ConvertFrom-Json }
        catch { Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.' }
    }
    return $Value
}

function Get-StructuredWorkflow {
    foreach ($name in @('GateWorkflow', 'Workflow')) {
        if ($inputProperties.ContainsKey($name)) {
            return Convert-ToWorkflowObject $inputProperties[$name]
        }
    }
    $hasStructuredKey = $false
    foreach ($name in @('GateWorkflow', 'Workflow')) {
        if ($rawIntentText -match '(?i)(^|[\s,;])' + [regex]::Escape($name) + '\s*[:=]') {
            $hasStructuredKey = $true
            break
        }
    }
    $jsonText = Find-JsonObjectAfterKey $rawIntentText @('GateWorkflow', 'Workflow')
    if (-not [string]::IsNullOrWhiteSpace($jsonText)) { return Convert-ToWorkflowObject $jsonText }
    if ($hasStructuredKey) {
        Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.'
    }
    return $null
}

function Get-WorkflowField($Workflow, [string[]]$Names) {
    if ($null -eq $Workflow) { return $null }
    foreach ($prop in $Workflow.PSObject.Properties) {
        foreach ($name in $Names) {
            if ($prop.Name -ieq $name) { return $prop.Value }
        }
    }
    return $null
}

function Test-Truthy($Value) {
    if ($null -eq $Value) { return $false }
    if ($Value -is [bool]) { return [bool]$Value }
    return ([string]$Value) -match '(?i)^(true|yes|1)$'
}

function Allow-AdvisoryGate([string]$Gate) {
    [pscustomobject]@{
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            additionalContext = "UNSTRUCTURED GATE ENTRY = ADVISORY ONLY for $Gate. 检测到未按照门禁标准顺序运行，将视为你是在单跑门禁，只能用于用户显式要求单跑特定门禁。如果是正式的自动化开发流程，请走标准的四门流程，此次门禁不具备任何自动化开发推进效力。 This is not four-stage sequencing, not release/seal approval, and not permission to output, record, or reuse a workflow PASS. If the user asks for formal four-gate, release, seal, final approval, or recordable PASS, stop inside the skill with GATE_SEQUENCE_ERROR and require structured GateWorkflow metadata plus gate-state admission."
        }
    } | ConvertTo-Json -Depth 5 -Compress
    exit 0
}

function Split-CommandArguments([string]$Command) {
    $tokens = New-Object System.Collections.Generic.List[string]
    $current = [System.Text.StringBuilder]::new()
    $quote = [char]0
    for ($index = 0; $index -lt $Command.Length; ++$index) {
        $ch = $Command[$index]
        if ($quote -ne [char]0) {
            if ($ch -eq $quote) { $quote = [char]0; continue }
            [void]$current.Append($ch)
            continue
        }
        if ($ch -eq '"' -or $ch -eq "'") { $quote = $ch; continue }
        if ([char]::IsWhiteSpace($ch)) {
            if ($current.Length -gt 0) {
                $tokens.Add($current.ToString())
                [void]$current.Clear()
            }
            continue
        }
        [void]$current.Append($ch)
    }
    if ($current.Length -gt 0) { $tokens.Add($current.ToString()) }
    return @($tokens)
}

function Get-CommandSwitchValue([string]$Command, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return $null }
    $tokens = @(Split-CommandArguments (Normalize-HookCommandText $Command))
    for ($index = 0; $index -lt ($tokens.Count - 1); ++$index) {
        if ($tokens[$index] -ieq "-$Name") { return $tokens[$index + 1] }
    }
    return $null
}

function Normalize-HookCommandText([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return '' }
    $normalized = $Command
    for ($i = 0; $i -lt 3; ++$i) {
        $next = $normalized -replace '\\+"', '"'
        if ($next -eq $normalized) { break }
        $normalized = $next
    }
    return $normalized
}

function Test-CommandMentionsGateWorkflowScript([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return $false }
    return (Normalize-HookCommandText $Command) -match '(?i)(^|[\\/"])gate-workflow\.ps1(["\s]|$)'
}

function Resolve-HookRelativePath([string]$BasePath, [string]$MaybeRelativePath) {
    if ([string]::IsNullOrWhiteSpace($MaybeRelativePath)) { return $null }
    if ([System.IO.Path]::IsPathRooted($MaybeRelativePath)) { return $MaybeRelativePath }
    if ([string]::IsNullOrWhiteSpace($BasePath)) { $BasePath = (Get-Location).Path }
    return Join-Path $BasePath $MaybeRelativePath
}

function Test-ManifestDefinesGate([string]$GateName, [string]$ManifestPath, [string]$BasePath) {
    if ([string]::IsNullOrWhiteSpace($GateName) -or [string]::IsNullOrWhiteSpace($ManifestPath)) { return $false }
    $resolvedManifest = Resolve-HookRelativePath $BasePath $ManifestPath
    if (-not (Test-Path -LiteralPath $resolvedManifest)) {
        Block-Gate "Gate sequence blocked: manifest was not found for gate '$GateName'." "Manifest missing: $(Format-HookPath $resolvedManifest)"
    }
    try {
        $manifest = Get-Content -LiteralPath $resolvedManifest -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        Block-Gate "Gate sequence blocked: manifest is malformed for gate '$GateName'." "Manifest malformed: $(Format-HookPath $resolvedManifest)"
    }
    if ($null -eq $manifest -or $manifest.PSObject.Properties.Name -notcontains 'stages' -or $null -eq $manifest.stages) { return $false }
    foreach ($property in $manifest.stages.PSObject.Properties) {
        if ($gateAliases.ContainsKey($property.Name)) {
            Block-Gate "Gate sequence blocked: manifest must not redefine built-in gate '$($property.Name)'." 'Manifest extension gates may add new gate ids only.'
        }
        if ($property.Name -eq $GateName) { return $true }
    }
    return $false
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

function Test-ContextBundleExists([string]$BasePath, [string]$Value) {
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
        $path = Resolve-HookRelativePath $BasePath $candidate
        if (Test-Path -LiteralPath $path) { return $true }
    }
    return $false
}

function Get-Sha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-ContextBundleValidationErrors([string]$BasePath, [string]$Value) {
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
        $path = Resolve-HookRelativePath $BasePath $pathText
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

function Test-ArtifactReferenceExists([string]$BasePath, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $false }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $false }
    $path = Resolve-HookRelativePath $BasePath $pathText
    return (Test-Path -LiteralPath $path -PathType Leaf)
}

function Get-ArtifactReferencePath([string]$BasePath, [string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
    $trimmed = $Value.Trim().Trim('"', "'")
    $pathText = [regex]::Replace($trimmed, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $null }
    return Resolve-HookRelativePath $BasePath $pathText
}

function Test-AnyMeaningfulArtifactField([string]$Text, [string[]]$FieldNames) {
    foreach ($field in $FieldNames) {
        if (Test-MeaningfulArtifactField $Text $field) { return $true }
    }
    return $false
}

function Get-ImplementationEvidenceMissing([string]$Text, [string]$GateName, [string]$BasePath) {
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
        if (-not (Test-ArtifactReferenceExists $BasePath $diffValue)) { $missing += 'Raw diff/changed-files artifact path must exist' }
    }
    if (-not (Test-AnyMeaningfulArtifactField $Text @('Developer self-test artifact', 'Verification artifact'))) {
        $missing += 'Developer self-test artifact or Verification artifact'
    }
    else {
        $verificationValue = Get-ArtifactFieldValue $Text 'Developer self-test artifact'
        if ([string]::IsNullOrWhiteSpace($verificationValue)) { $verificationValue = Get-ArtifactFieldValue $Text 'Verification artifact' }
        if (-not (Test-ArtifactReferenceExists $BasePath $verificationValue)) { $missing += 'Developer self-test/verification artifact path must exist' }
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

function Get-FinalExecutionEvidenceMissing([string]$Text, [string]$GateName, [string]$StageValue, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$BasePath) {
    if ($GateName -ne 'qa-test-gate' -or $StageValue -ne 'FinalExecution') { return @() }
    $missing = @()
    $evidenceValue = Get-ArtifactFieldValue $Text 'QA-owned evidence'
    $evidencePath = Get-ArtifactReferencePath $BasePath $evidenceValue
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

function Test-GateArtifactFields([string]$ArtifactPath, [string[]]$RequiredFields, [string]$BasePath, [string]$ExpectedWorkflowId, [string]$ExpectedChangeSnapshot, [string]$GateName, [string]$StageValue) {
    if (-not (Test-Path -LiteralPath $ArtifactPath)) {
        return [pscustomobject]@{ Ok = $false; Missing = @("artifact missing: $(Format-HookPath $ArtifactPath)") }
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
        $missing += @(Get-ContextBundleValidationErrors $BasePath (Get-ArtifactFieldValue $text 'Context bundle'))
    }
    $missing += @(Get-QaEvidenceMissing $text $GateName)
    $missing += @(Get-FinalExecutionEvidenceMissing $text $GateName $StageValue $ExpectedWorkflowId $ExpectedChangeSnapshot $BasePath)
    $missing += @(Get-ImplementationEvidenceMissing $text $GateName $BasePath)
    $missing += @(Get-GateRouteMissingForPass $text $ExpectedWorkflowId $ExpectedChangeSnapshot $GateName $StageValue)
    return [pscustomobject]@{ Ok = ($missing.Count -eq 0); Missing = $missing }
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

function Normalize-GateStage([string]$StageValue) {
    if ([string]::IsNullOrWhiteSpace($StageValue)) { return '' }
    return ([regex]::Replace($StageValue.Trim(), '[\s_-]+', '')).ToLowerInvariant()
}

function Get-FormalGatePassRequiredFields([string]$Gate) {
    $zeroContextFields = @(
        'Zero-context reviewer: YES',
        'Independent agent: YES',
        'Reviewer agent id:',
        'Context bundle:',
        'No-anchor prompt: YES',
        'gate_route:'
    )
    if ($Gate -eq 'complexity-gate') {
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
    if ($Gate -in @('qa-test-gate', 'architecture-health-gate', 'code-quality-gate')) {
        if ($Gate -eq 'qa-test-gate') {
            return @(
                'Approved case set:',
                'QA-owned evidence:',
                'Case-to-artifact binding:'
            ) + $zeroContextFields
        }
        return $zeroContextFields
    }
    if ([string]::IsNullOrWhiteSpace($Gate)) { return @() }
    return $zeroContextFields
}

function Enforce-FormalGatePassArtifact {
    if ($toolName -ne 'Bash') { return }
    $command = [string]$inputProperties['command']
    if ([string]::IsNullOrWhiteSpace($command)) { return }
    if (-not (Test-CommandMentionsGateWorkflowScript $command)) { return }

    $actionValue = Get-CommandSwitchValue $command 'Action'
    if ($actionValue -ne 'record-stage') { return }

    $gateValue = Get-CommandSwitchValue $command 'Gate'
    $verdictValue = Get-CommandSwitchValue $command 'Verdict'
    if ($verdictValue -ne 'PASS') { return }

    $requiredFields = @(Get-FormalGatePassRequiredFields $gateValue)
    if ($requiredFields.Count -eq 0) { return }

    $artifactValue = Get-CommandSwitchValue $command 'Artifact'
    if ([string]::IsNullOrWhiteSpace($artifactValue)) {
        Block-Gate "$gateValue PASS blocked: gate-workflow record-stage must provide an artifact." 'Missing gate artifact for formal independent zero-context review enforcement.'
    }

    $worktreeValue = Get-CommandSwitchValue $command 'Worktree'
    if ([string]::IsNullOrWhiteSpace($worktreeValue)) { $worktreeValue = [string]$payload.cwd }
    if ([string]::IsNullOrWhiteSpace($worktreeValue)) { $worktreeValue = (Get-Location).Path }
    $artifactPath = Resolve-HookRelativePath $worktreeValue $artifactValue
    $workflowValue = Get-CommandSwitchValue $command 'WorkflowId'
    $snapshotValue = Get-CommandSwitchValue $command 'ChangeSnapshot'
    $stageValue = Get-CommandSwitchValue $command 'Stage'
    $reviewCheck = Test-GateArtifactFields $artifactPath $requiredFields $worktreeValue $workflowValue $snapshotValue $gateValue $stageValue
    if (-not $reviewCheck.Ok) {
        $missingText = ($reviewCheck.Missing -join ', ')
        Block-Gate "$gateValue PASS blocked: artifact lacks required formal independent zero-context review fields: $missingText" 'Formal gate PASS requires an independent zero-context reviewer artifact, not main-thread self-review.'
    }
}

Enforce-FormalGatePassArtifact

function Resolve-WorktreeFromStatePath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    try {
        $full = [System.IO.Path]::GetFullPath($Path)
        $fileName = [System.IO.Path]::GetFileName($full)
        $gatesDir = Split-Path -Parent $full
        $claudeDir = Split-Path -Parent $gatesDir
        if ($fileName -eq 'gate-state.json' -and [System.IO.Path]::GetFileName($gatesDir) -eq 'gates' -and [System.IO.Path]::GetFileName($claudeDir) -eq '.claude') {
            return Split-Path -Parent $claudeDir
        }
    }
    catch {
        return $null
    }
    return $null
}

$toolGateIntent = Get-ToolGateIntent
$workflow = Get-StructuredWorkflow
$hasStructuredWorkflow = $null -ne $workflow

if ($null -eq $toolGateIntent -and -not $hasStructuredWorkflow) { exit 0 }

if (-not $hasStructuredWorkflow) {
    Allow-AdvisoryGate $toolGateIntent
}

$gate = [string](Get-WorkflowField $workflow @('gate', 'Gate'))
if ([string]::IsNullOrWhiteSpace($gate)) {
    Block-Gate 'Gate sequence blocked: GateWorkflow.gate is required.' 'Missing structured gate field.'
}
$explicitWorktree = [string](Get-WorkflowField $workflow @('worktree', 'Worktree', 'repo', 'Repo', 'cwd', 'Cwd'))
$explicitStatePath = [string](Get-WorkflowField $workflow @('statePath', 'StatePath'))
if ([string]::IsNullOrWhiteSpace($explicitWorktree) -and -not [string]::IsNullOrWhiteSpace($explicitStatePath)) {
    $explicitWorktree = Resolve-WorktreeFromStatePath $explicitStatePath
}
$manifestPath = [string](Get-WorkflowField $workflow @('manifestPath', 'ManifestPath'))
$manifestBasePath = if (-not [string]::IsNullOrWhiteSpace($explicitWorktree)) { $explicitWorktree } elseif (-not [string]::IsNullOrWhiteSpace([string]$payload.cwd)) { [string]$payload.cwd } else { (Get-Location).Path }
$isManifestGate = Test-ManifestDefinesGate $gate $manifestPath $manifestBasePath
if (-not $gateAliases.ContainsKey($gate) -and -not $isManifestGate) {
    Block-Gate "Gate sequence blocked: unknown gate '$gate'." "Unknown structured gate '$gate'."
}
if ($toolName -eq 'Skill' -and -not [string]::IsNullOrWhiteSpace($toolGateIntent) -and $toolGateIntent -ne 'formal-gates' -and $toolGateIntent -ne $gate) {
    Block-Gate "Gate sequence blocked: invoked gate '$toolGateIntent' does not match GateWorkflow.gate '$gate'." 'Structured gate mismatch.'
}

$workflowId = [string](Get-WorkflowField $workflow @('workflowId', 'WorkflowId', 'workflow_id'))
$changeSnapshot = [string](Get-WorkflowField $workflow @('changeSnapshot', 'ChangeSnapshot', 'snapshot'))
$singleGateAuthorized = Test-Truthy (Get-WorkflowField $workflow @('singleGateAuthorized', 'SingleGateAuthorized', 'standalone'))
$stage = [string](Get-WorkflowField $workflow @('stage', 'Stage'))
$normalizedStage = Normalize-GateStage $stage
$mode = [string](Get-WorkflowField $workflow @('mode', 'Mode'))
$isFinal = (Test-Truthy (Get-WorkflowField $workflow @('final', 'Final'))) -or $normalizedStage -in @('whiteboxadequacy', 'finalexecution', 'final', 'release', 'seal')
$isWhiteBox = $normalizedStage -eq 'whiteboxadequacy'

$isStandaloneSingleGateRequest = $singleGateAuthorized -and -not $isFinal -and -not ($mode -match '(?i)formal|release|seal')
if ($isStandaloneSingleGateRequest) {
    if (-not [string]::IsNullOrWhiteSpace($workflowId) -or -not [string]::IsNullOrWhiteSpace($changeSnapshot)) {
        Block-Gate 'Gate sequence blocked: standalone single-gate mode cannot carry WorkflowId or ChangeSnapshot.' "Standalone $gate tried to use workflow/snapshot fields."
    }
    [pscustomobject]@{
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            additionalContext = 'USER-AUTHORIZED STANDALONE SINGLE-GATE MODE ONLY. This is advisory only, not four-stage sequencing, not release/seal approval, and not permission to enter downstream gates. Do not record or reuse this result as a workflow PASS.'
        }
    } | ConvertTo-Json -Depth 5 -Compress
    exit 0
}

$isQaExecution = $gate -eq 'qa-test-gate' -and $normalizedStage -in @('execution', 'finalexecution')
$requiresWorkflow = $isManifestGate -or $gate -in @('complexity-gate', 'architecture-health-gate', 'code-quality-gate') -or ($gate -eq 'qa-test-gate' -and ($isWhiteBox -or $isFinal -or $isQaExecution))
if ($requiresWorkflow) {
    if ([string]::IsNullOrWhiteSpace($workflowId)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.workflowId." "Missing workflowId for $gate."
    }
    if ([string]::IsNullOrWhiteSpace($changeSnapshot)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.changeSnapshot." "Missing changeSnapshot for $gate."
    }
    if ([string]::IsNullOrWhiteSpace($explicitWorktree) -and [string]::IsNullOrWhiteSpace($explicitStatePath)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.worktree or GateWorkflow.statePath." "Missing explicit worktree/statePath for $gate."
    }
}

$cwd = if (-not [string]::IsNullOrWhiteSpace($explicitWorktree)) { $explicitWorktree } else { [string]$payload.cwd }
if ([string]::IsNullOrWhiteSpace($cwd)) { $cwd = (Get-Location).Path }

$skillRoot = Split-Path -Parent $PSScriptRoot
if ((Split-Path -Leaf $skillRoot) -ne 'formal-gates') {
    Block-Gate "Gate sequence blocked: hook is not running from a formal-gates package root; cannot enter $gate without machine-verifiable admission." "Invalid formal-gates hook package root: $(Format-HookPath $skillRoot)"
}

$gateStateScript = Join-Path $skillRoot 'scripts/gate-state.ps1'
if (-not (Test-Path -LiteralPath $gateStateScript)) {
    Block-Gate "Gate sequence blocked: package-local gate-state.ps1 was not found; cannot enter $gate without machine-verifiable admission." "Missing package-local gate-state.ps1 for ${gate}: $(Format-HookPath $gateStateScript)"
}

function Add-CommonGateStateArgs {
    param([string[]]$Arguments)
    $Result = @($Arguments)
    if (-not [string]::IsNullOrWhiteSpace($explicitStatePath)) { $Result += @('-StatePath', $explicitStatePath) }
    if (-not [string]::IsNullOrWhiteSpace($manifestPath)) { $Result += @('-ManifestPath', $manifestPath) }
    return $Result
}

if ($gate -eq 'qa-test-gate') {
    if ($isFinal -or $isWhiteBox) {
        $verifyArgs = (Get-FormalGatesPowerShellFileArgs $gateStateScript) + @(
            '-Action', 'verify',
            '-Gate', 'code-quality-gate',
            '-RequireVerdict', 'PASS',
            '-RequireWorkflowId', $workflowId,
            '-ChangeSnapshot', $changeSnapshot,
            '-RequireArtifactExists'
        )
        $verifyArgs = Add-CommonGateStateArgs $verifyArgs
        $result = Invoke-GateState $verifyArgs $cwd
        if ($result.ExitCode -ne 0) {
            $detail = if ([string]::IsNullOrWhiteSpace($result.Output)) { 'code-quality gate PASS is missing.' } else { $result.Output }
            Block-Gate "Gate sequence blocked before final QA/seal. $detail" $detail
        }
    }
    exit 0
}

$admissionArgs = (Get-FormalGatesPowerShellFileArgs $gateStateScript) + @(
    '-Action', 'verify-admission',
    '-Gate', $gate,
    '-WorkflowId', $workflowId,
    '-ChangeSnapshot', $changeSnapshot
)
$admissionArgs = Add-CommonGateStateArgs $admissionArgs
$result = Invoke-GateState $admissionArgs $cwd
if ($result.ExitCode -ne 0) {
    $detail = if ([string]::IsNullOrWhiteSpace($result.Output)) { "gate-state admission check failed with exit code $($result.ExitCode)." } else { $result.Output }
    Block-Gate "Gate sequence blocked before entering $gate. $detail" $detail
}

[pscustomobject]@{
    hookSpecificOutput = @{
        hookEventName = 'PreToolUse'
        additionalContext = "Gate sequence admission passed for $gate. $($result.Output)"
    }
} | ConvertTo-Json -Depth 5 -Compress
exit 0
