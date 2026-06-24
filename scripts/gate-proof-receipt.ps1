param(
    [ValidateSet('', 'register-dispatch', 'capture-hook', 'finalize')]
    [Alias('Action')]
    [string]$ReceiptAction = '',
    [string]$ReceiptProvider,
    [string]$ReceiptWorktree,
    [string]$ReceiptWorkflowId,
    [string]$ReceiptGate,
    [string]$ReceiptStage,
    [string]$ReceiptReviewArtifact,
    [string]$ReceiptEventName,
    [string]$ReceiptSubagentId,
    [string]$ReceiptStatus,
    [string]$ReceiptDispatchId,
    [string]$ReceiptDispatchRegistrationArtifact,
    [string]$ReceiptPayloadJson
)

$ErrorActionPreference = 'Stop'

$script:FormalGateProofReceiptAction = $ReceiptAction
$script:FormalGateProofReceiptProvider = $ReceiptProvider
$script:FormalGateProofReceiptWorktree = $ReceiptWorktree
$script:FormalGateProofReceiptWorkflowId = $ReceiptWorkflowId
$script:FormalGateProofReceiptGate = $ReceiptGate
$script:FormalGateProofReceiptStage = $ReceiptStage
$script:FormalGateProofReceiptReviewArtifact = $ReceiptReviewArtifact
$script:FormalGateProofReceiptEventName = $ReceiptEventName
$script:FormalGateProofReceiptSubagentId = $ReceiptSubagentId
$script:FormalGateProofReceiptStatus = $ReceiptStatus
$script:FormalGateProofReceiptDispatchId = $ReceiptDispatchId
$script:FormalGateProofReceiptDispatchRegistrationArtifact = $ReceiptDispatchRegistrationArtifact
$script:FormalGateProofReceiptPayloadJson = $ReceiptPayloadJson

if ($MyInvocation.InvocationName -eq '.') {
    Remove-Variable -Name ReceiptAction,ReceiptProvider,ReceiptWorktree,ReceiptWorkflowId,ReceiptGate,ReceiptStage,ReceiptReviewArtifact,ReceiptEventName,ReceiptSubagentId,ReceiptStatus,ReceiptDispatchId,ReceiptDispatchRegistrationArtifact,ReceiptPayloadJson -Scope Local -ErrorAction SilentlyContinue
}

function Resolve-FormalGateReceiptPath([string]$BasePath, [string]$MaybeRelativePath) {
    if ([string]::IsNullOrWhiteSpace($MaybeRelativePath)) { return $null }
    if ([System.IO.Path]::IsPathRooted($MaybeRelativePath)) { return $MaybeRelativePath }
    if ([string]::IsNullOrWhiteSpace($BasePath)) { $BasePath = (Get-Location).Path }
    return Join-Path $BasePath $MaybeRelativePath
}

function Get-FormalGateReceiptSha256([string]$Path) {
    $bytes = [System.Security.Cryptography.SHA256]::Create().ComputeHash([System.IO.File]::ReadAllBytes($Path))
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}

function ConvertTo-FormalGateReceiptRelativePath([string]$BasePath, [string]$Path) {
    $fullBase = [System.IO.Path]::GetFullPath($BasePath).TrimEnd('\', '/')
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    if ($fullPath.StartsWith($fullBase + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        return $fullPath.Substring($fullBase.Length + 1).Replace('\', '/')
    }
    return $fullPath.Replace('\', '/')
}

function New-FormalGateReceiptDirectory([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        New-Item -ItemType Directory -Path $Path | Out-Null
    }
}

function Get-FormalGateReviewArtifactCanonicalText([string]$Text) {
    $normalized = $Text.Replace("`r`n", "`n").Replace("`r", "`n")
    $lines = @($normalized.Split([string[]]@("`n"), [System.StringSplitOptions]::None))
    $kept = @()
    foreach ($line in $lines) {
        if ($line -match '(?i)^[ \t]*Reviewer proof receipt[ \t]*:') { continue }
        $kept += $line.TrimEnd(" ", "`t")
    }
    return ($kept -join "`n")
}

function Get-FormalGateReviewArtifactCanonicalSha256([string]$Text) {
    $canonical = Get-FormalGateReviewArtifactCanonicalText $Text
    $bytes = [System.Text.UTF8Encoding]::new($false).GetBytes($canonical)
    $hash = [System.Security.Cryptography.SHA256]::Create().ComputeHash($bytes)
    return [BitConverter]::ToString($hash).Replace('-', '').ToLowerInvariant()
}

function Add-FormalGateReceiptLine([string]$Text, [string]$ReceiptRef) {
    $lines = @($Text.Replace("`r`n", "`n").Replace("`r", "`n").Split([string[]]@("`n"), [System.StringSplitOptions]::None))
    $updated = New-Object System.Collections.Generic.List[string]
    $replaced = $false
    foreach ($line in $lines) {
        if ($line -match '(?i)^[ \t]*Reviewer proof receipt[ \t]*:') {
            if (-not $replaced) {
                $updated.Add($ReceiptRef)
                $replaced = $true
            }
            continue
        }
        $updated.Add($line)
    }
    if (-not $replaced) {
        $insert = [Math]::Min(7, $updated.Count)
        $withReceipt = New-Object System.Collections.Generic.List[string]
        for ($i = 0; $i -lt $insert; $i++) { $withReceipt.Add($updated[$i]) }
        $withReceipt.Add($ReceiptRef)
        for ($i = $insert; $i -lt $updated.Count; $i++) { $withReceipt.Add($updated[$i]) }
        $updated = $withReceipt
    }
    return ($updated.ToArray() -join "`n")
}

function Get-FormalGateProofReceiptValueParts([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
    $trimmed = $Value.Trim().Trim('"', "'")
    $shaMatch = [regex]::Match($trimmed, '(?i)\bsha(?:256)?\s*[:=]\s*([a-f0-9]{64})\b')
    if (-not $shaMatch.Success) { return $null }
    $pathText = [regex]::Replace($trimmed, '(?i)\s+sha(?:256)?\s*[:=]\s*[a-f0-9]{64}\b', '').Trim()
    $pathText = [regex]::Replace($pathText, '\s+\(.*$', '').Trim()
    if ([string]::IsNullOrWhiteSpace($pathText)) { return $null }
    return [pscustomobject]@{ Path = $pathText; Sha256 = $shaMatch.Groups[1].Value.ToLowerInvariant() }
}

function Get-FormalGateProofReceiptDispatchValidationErrors([string]$BasePath, [object]$Receipt, [string]$ReceiptPath) {
    $errors = @()
    $dispatchPathText = [string]$Receipt.dispatchRegistrationArtifact
    $dispatchHash = [string]$Receipt.dispatchRegistrationSha256
    if ([string]::IsNullOrWhiteSpace($dispatchPathText) -or $dispatchHash -notmatch '^[a-f0-9]{64}$') {
        return @('Reviewer proof receipt dispatch registration path/hash is missing')
    }
    $dispatchPath = Resolve-FormalGateReceiptPath $BasePath $dispatchPathText
    if (-not (Test-Path -LiteralPath $dispatchPath -PathType Leaf)) {
        return @("Reviewer proof receipt dispatch registration path does not exist: $dispatchPathText")
    }
    if ((Get-FormalGateReceiptSha256 $dispatchPath) -ne $dispatchHash) {
        return @("Reviewer proof receipt dispatch registration sha256 mismatch: $dispatchPathText")
    }
    try {
        $dispatchRecord = Get-Content -LiteralPath $dispatchPath -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        return @('Reviewer proof receipt dispatch registration is not valid JSON')
    }
    if ([int]$dispatchRecord.proofVersion -ne 1) { $errors += 'Reviewer proof receipt dispatch registration proofVersion must be 1' }
    if ([string]$dispatchRecord.dispatchId -ne [string]$Receipt.dispatchId) { $errors += 'Reviewer proof receipt dispatch registration dispatchId must match receipt' }
    if (-not (Test-FormalGateReceiptKnownProvider ([string]$dispatchRecord.provider))) { $errors += 'Reviewer proof receipt dispatch registration provider is unsupported' }
    if ([string]$dispatchRecord.provider -ne [string]$Receipt.provider) { $errors += 'Reviewer proof receipt dispatch registration provider must match receipt' }
    if ([string]$dispatchRecord.workflowId -ne [string]$Receipt.workflowId) { $errors += 'Reviewer proof receipt dispatch registration workflowId must match receipt' }
    if ([string]$dispatchRecord.gate -ne [string]$Receipt.gate) { $errors += 'Reviewer proof receipt dispatch registration gate must match receipt' }
    if (-not (Test-FormalGateReceiptStageMatch ([string]$dispatchRecord.stage) ([string]$Receipt.stage))) { $errors += 'Reviewer proof receipt dispatch registration stage must match receipt' }
    if ((Resolve-FormalGateReceiptPath $BasePath ([string]$dispatchRecord.reviewArtifact)) -ne (Resolve-FormalGateReceiptPath $BasePath ([string]$Receipt.reviewArtifact))) { $errors += 'Reviewer proof receipt dispatch registration reviewArtifact must match receipt' }
    $dispatchReceiptArtifact = [string]$dispatchRecord.receiptArtifact
    if ([string]::IsNullOrWhiteSpace($dispatchReceiptArtifact)) {
        $errors += 'Reviewer proof receipt dispatch registration receiptArtifact must be finalized'
    }
    elseif ([System.IO.Path]::GetFullPath((Resolve-FormalGateReceiptPath $BasePath $dispatchReceiptArtifact)) -ne [System.IO.Path]::GetFullPath($ReceiptPath)) {
        $errors += 'Reviewer proof receipt dispatch registration receiptArtifact must match receipt'
    }
    return $errors
}

function Test-FormalGateReceiptKnownProvider([string]$Provider) {
    return $Provider -in @('codex', 'claude-code', 'cursor')
}

function Get-FormalGateReceiptEventValue([object]$Event, [string]$Primary, [string[]]$Fallbacks) {
    foreach ($name in @($Primary) + $Fallbacks) {
        if ($Event.PSObject.Properties.Name -contains $name) {
            $value = [string]$Event.$name
            if (-not [string]::IsNullOrWhiteSpace($value)) { return $value }
        }
    }
    return $null
}

function Test-FormalGateReceiptStageMatch([string]$Actual, [string]$Expected) {
    return ([string]$Actual).Trim() -eq ([string]$Expected).Trim()
}

function Get-FormalGateReviewerProofReceiptValidationErrors(
    [string]$BasePath,
    [string]$ArtifactPath,
    [string]$Text,
    [string]$ExpectedWorkflowId,
    [string]$GateName,
    [string]$StageValue
) {
    $errors = @()
    $value = $null
    $match = [regex]::Match($Text, '(?im)^[ \t]*Reviewer proof receipt[ \t]*:[ \t]*(.*?)[ \t]*$')
    if ($match.Success) { $value = $match.Groups[1].Value.Trim() }
    $parts = Get-FormalGateProofReceiptValueParts $value
    if ($null -eq $parts) { return @('Reviewer proof receipt: <path> sha256=<sha256>') }

    $receiptPath = Resolve-FormalGateReceiptPath $BasePath $parts.Path
    if (-not (Test-Path -LiteralPath $receiptPath -PathType Leaf)) {
        return @("Reviewer proof receipt path does not exist: $($parts.Path)")
    }
    $actualReceiptHash = Get-FormalGateReceiptSha256 $receiptPath
    if ($actualReceiptHash -ne $parts.Sha256) {
        return @("Reviewer proof receipt sha256 mismatch: $($parts.Path)")
    }
    try {
        $receipt = Get-Content -LiteralPath $receiptPath -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        return @('Reviewer proof receipt is not valid JSON')
    }

    if ([int]$receipt.proofVersion -ne 1) { $errors += 'Reviewer proof receipt proofVersion must be 1' }
    if (-not (Test-FormalGateReceiptKnownProvider ([string]$receipt.provider))) { $errors += 'Reviewer proof receipt provider is unsupported' }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and [string]$receipt.workflowId -ne $ExpectedWorkflowId) { $errors += 'Reviewer proof receipt workflowId must match WorkflowId' }
    if ([string]$receipt.gate -ne $GateName) { $errors += 'Reviewer proof receipt gate must match gate' }
    if (-not (Test-FormalGateReceiptStageMatch ([string]$receipt.stage) $StageValue)) { $errors += 'Reviewer proof receipt stage must match stage' }
    $events = @($receipt.normalizedEvents)
    if ($events -notcontains 'subagent_start' -or $events -notcontains 'subagent_stop') { $errors += 'Reviewer proof receipt must include subagent_start and subagent_stop' }
    $errors += @(Get-FormalGateProofReceiptDispatchValidationErrors $BasePath $receipt $receiptPath)

    $artifactFull = [System.IO.Path]::GetFullPath($ArtifactPath)
    $receiptArtifactFull = [System.IO.Path]::GetFullPath((Resolve-FormalGateReceiptPath $BasePath ([string]$receipt.reviewArtifact)))
    if ($receiptArtifactFull -ne $artifactFull) { $errors += 'Reviewer proof receipt reviewArtifact must match artifact path' }
    $canonical = Get-FormalGateReviewArtifactCanonicalSha256 $Text
    if ([string]$receipt.reviewArtifactCanonicalSha256 -ne $canonical) { $errors += 'Reviewer proof receipt reviewArtifactCanonicalSha256 mismatch' }

    $start = Get-FormalGateReceiptEventValidationErrors $BasePath $ArtifactPath $receipt 'start' 'subagent_start' $ExpectedWorkflowId $GateName $StageValue
    $stop = Get-FormalGateReceiptEventValidationErrors $BasePath $ArtifactPath $receipt 'stop' 'subagent_stop' $ExpectedWorkflowId $GateName $StageValue
    $errors += @($start.Errors)
    $errors += @($stop.Errors)
    if (-not [string]::IsNullOrWhiteSpace($start.SubagentId) -and -not [string]::IsNullOrWhiteSpace($stop.SubagentId) -and $start.SubagentId -ne $stop.SubagentId) {
        $errors += 'Reviewer proof receipt start/stop subagentId mismatch'
    }
    return $errors
}

function Get-FormalGateReceiptEventValidationErrors(
    [string]$BasePath,
    [string]$ArtifactPath,
    [object]$Receipt,
    [string]$Prefix,
    [string]$ExpectedEvent,
    [string]$ExpectedWorkflowId,
    [string]$GateName,
    [string]$StageValue
) {
    $errors = @()
    $pathName = "${Prefix}EventArtifact"
    $shaName = "${Prefix}EventSha256"
    $pathText = [string]$Receipt.$pathName
    $expectedHash = [string]$Receipt.$shaName
    if ([string]::IsNullOrWhiteSpace($pathText) -or $expectedHash -notmatch '^[a-f0-9]{64}$') {
        return [pscustomobject]@{ Errors = @("Reviewer proof receipt $ExpectedEvent event path/hash is missing"); SubagentId = $null }
    }
    $path = Resolve-FormalGateReceiptPath $BasePath $pathText
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return [pscustomobject]@{ Errors = @("Reviewer proof receipt $ExpectedEvent event path does not exist: $pathText"); SubagentId = $null }
    }
    if ((Get-FormalGateReceiptSha256 $path) -ne $expectedHash) {
        $errors += "Reviewer proof receipt $ExpectedEvent event sha256 mismatch: $pathText"
    }
    try {
        $event = Get-Content -LiteralPath $path -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        return [pscustomobject]@{ Errors = @("Reviewer proof receipt $ExpectedEvent event is not valid JSON"); SubagentId = $null }
    }
    $eventWorkflow = Get-FormalGateReceiptEventValue $event 'workflowId' @('formalWorkflowId')
    $eventGate = Get-FormalGateReceiptEventValue $event 'gate' @('gateId')
    $eventStage = Get-FormalGateReceiptEventValue $event 'stage' @()
    $eventKind = Get-FormalGateReceiptEventValue $event 'normalizedEvent' @('kind', 'event')
    $subagentIdValue = Get-FormalGateReceiptEventValue $event 'subagentId' @('subagent_id')
    $eventDispatchId = Get-FormalGateReceiptEventValue $event 'dispatchId' @()
    $eventDispatchPath = Get-FormalGateReceiptEventValue $event 'dispatchRegistrationArtifact' @()
    if (-not [string]::IsNullOrWhiteSpace($ExpectedWorkflowId) -and $eventWorkflow -ne $ExpectedWorkflowId) { $errors += "Reviewer proof receipt $ExpectedEvent event workflowId must match WorkflowId" }
    if ($eventGate -ne $GateName) { $errors += "Reviewer proof receipt $ExpectedEvent event gate must match gate" }
    if (-not (Test-FormalGateReceiptStageMatch $eventStage $StageValue)) { $errors += "Reviewer proof receipt $ExpectedEvent event stage must match stage" }
    if ($eventKind -ne $ExpectedEvent) { $errors += "Reviewer proof receipt event kind must be $ExpectedEvent" }
    if (-not [string]::IsNullOrWhiteSpace([string]$Receipt.dispatchId) -and $eventDispatchId -ne [string]$Receipt.dispatchId) {
        $errors += "Reviewer proof receipt $ExpectedEvent event dispatchId must match dispatch registration"
    }
    if (-not [string]::IsNullOrWhiteSpace([string]$Receipt.dispatchRegistrationArtifact)) {
        $receiptDispatchFull = [System.IO.Path]::GetFullPath((Resolve-FormalGateReceiptPath $BasePath ([string]$Receipt.dispatchRegistrationArtifact)))
        if ([string]::IsNullOrWhiteSpace($eventDispatchPath)) {
            $errors += "Reviewer proof receipt $ExpectedEvent event dispatchRegistrationArtifact must match receipt"
        }
        else {
            $eventDispatchFull = [System.IO.Path]::GetFullPath((Resolve-FormalGateReceiptPath $BasePath $eventDispatchPath))
            if ($eventDispatchFull -ne $receiptDispatchFull) {
                $errors += "Reviewer proof receipt $ExpectedEvent event dispatchRegistrationArtifact must match receipt"
            }
        }
    }
    elseif (-not [string]::IsNullOrWhiteSpace($eventDispatchPath)) {
        $errors += "Reviewer proof receipt $ExpectedEvent event dispatchRegistrationArtifact must match receipt"
    }
    if ([string]::IsNullOrWhiteSpace([string]$Receipt.dispatchId) -and -not [string]::IsNullOrWhiteSpace($eventDispatchId)) {
        $errors += "Reviewer proof receipt $ExpectedEvent event dispatchId must match dispatch registration"
    }
    return [pscustomobject]@{ Errors = $errors; SubagentId = $subagentIdValue }
}

function New-FormalGateReceiptId {
    return ([Guid]::NewGuid().ToString('N'))
}

function Write-FormalGateReceiptJson([string]$Path, [object]$Object) {
    $parent = Split-Path -Parent $Path
    if (-not [string]::IsNullOrWhiteSpace($parent)) { New-FormalGateReceiptDirectory $parent }
    $json = $Object | ConvertTo-Json -Depth 12
    [System.IO.File]::WriteAllText($Path, $json, [System.Text.UTF8Encoding]::new($false))
}

function Register-FormalGateProofDispatch {
    $repo = [System.IO.Path]::GetFullPath($script:FormalGateProofReceiptWorktree)
    $dispatchDir = Join-Path $repo '.claude/gates/proofs/dispatch'
    New-FormalGateReceiptDirectory $dispatchDir
    $id = New-FormalGateReceiptId
    $path = Join-Path $dispatchDir "$id.json"
    $record = [ordered]@{
        proofVersion = 1
        dispatchId = $id
        provider = $script:FormalGateProofReceiptProvider
        workflowId = $script:FormalGateProofReceiptWorkflowId
        gate = $script:FormalGateProofReceiptGate
        stage = if ($null -eq $script:FormalGateProofReceiptStage) { '' } else { $script:FormalGateProofReceiptStage }
        worktree = $repo.Replace('\', '/')
        reviewArtifact = (ConvertTo-FormalGateReceiptRelativePath $repo (Resolve-FormalGateReceiptPath $repo $script:FormalGateProofReceiptReviewArtifact))
        status = 'open'
        registeredAtUtc = [DateTime]::UtcNow.ToString('o')
    }
    Write-FormalGateReceiptJson $path $record
    Write-Output (@{ dispatchId = $id; dispatchRegistrationArtifact = ConvertTo-FormalGateReceiptRelativePath $repo $path; dispatchRegistrationSha256 = Get-FormalGateReceiptSha256 $path } | ConvertTo-Json -Compress)
}

function Save-FormalGateProofEvent {
    $repo = [System.IO.Path]::GetFullPath($script:FormalGateProofReceiptWorktree)
    $eventDir = Join-Path $repo '.claude/gates/proofs/events'
    New-FormalGateReceiptDirectory $eventDir
    $normalized = if ($script:FormalGateProofReceiptEventName -match '(?i)stop|end|finish') { 'subagent_stop' } else { 'subagent_start' }
    $id = New-FormalGateReceiptId
    $path = Join-Path $eventDir "$id.json"
    $record = [ordered]@{
        provider = $script:FormalGateProofReceiptProvider
        workflowId = $script:FormalGateProofReceiptWorkflowId
        gate = $script:FormalGateProofReceiptGate
        stage = if ($null -eq $script:FormalGateProofReceiptStage) { '' } else { $script:FormalGateProofReceiptStage }
        normalizedEvent = $normalized
        rawEventName = $script:FormalGateProofReceiptEventName
        subagentId = $script:FormalGateProofReceiptSubagentId
        status = $script:FormalGateProofReceiptStatus
        capturedAtUtc = [DateTime]::UtcNow.ToString('o')
    }
    if (-not [string]::IsNullOrWhiteSpace($script:FormalGateProofReceiptDispatchId)) {
        $record.dispatchId = $script:FormalGateProofReceiptDispatchId
    }
    if (-not [string]::IsNullOrWhiteSpace($script:FormalGateProofReceiptDispatchRegistrationArtifact)) {
        $record.dispatchRegistrationArtifact = $script:FormalGateProofReceiptDispatchRegistrationArtifact
    }
    if (-not [string]::IsNullOrWhiteSpace($script:FormalGateProofReceiptPayloadJson)) {
        try { $record.rawPayload = ($script:FormalGateProofReceiptPayloadJson | ConvertFrom-Json) } catch { $record.rawPayloadText = $script:FormalGateProofReceiptPayloadJson }
    }
    Write-FormalGateReceiptJson $path $record
    Write-Output (@{ eventArtifact = ConvertTo-FormalGateReceiptRelativePath $repo $path; eventSha256 = Get-FormalGateReceiptSha256 $path; normalizedEvent = $normalized } | ConvertTo-Json -Compress)
}

function Complete-FormalGateProofReceipt {
    $repo = [System.IO.Path]::GetFullPath($script:FormalGateProofReceiptWorktree)
    $reviewPath = Resolve-FormalGateReceiptPath $repo $script:FormalGateProofReceiptReviewArtifact
    if (-not (Test-Path -LiteralPath $reviewPath -PathType Leaf)) { throw "ReviewArtifact not found: $script:FormalGateProofReceiptReviewArtifact" }
    $stageValue = if ($null -eq $script:FormalGateProofReceiptStage) { '' } else { $script:FormalGateProofReceiptStage }
    $reviewRel = ConvertTo-FormalGateReceiptRelativePath $repo $reviewPath
    $dispatches = @(Get-ChildItem -LiteralPath (Join-Path $repo '.claude/gates/proofs/dispatch') -Filter '*.json' -ErrorAction SilentlyContinue | ForEach-Object {
        $d = Get-Content -LiteralPath $_.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
        $dispatchReviewPath = Resolve-FormalGateReceiptPath $repo ([string]$d.reviewArtifact)
        if ([string]$d.provider -eq $script:FormalGateProofReceiptProvider -and [string]$d.workflowId -eq $script:FormalGateProofReceiptWorkflowId -and [string]$d.gate -eq $script:FormalGateProofReceiptGate -and [string]$d.stage -eq $stageValue -and [string]$d.status -eq 'open' -and [System.IO.Path]::GetFullPath($dispatchReviewPath) -eq [System.IO.Path]::GetFullPath($reviewPath)) {
            $_
        }
    })
    if ($dispatches.Count -ne 1) { throw "Receipt finalization requires exactly one open dispatch registration; found $($dispatches.Count)." }
    $dispatch = Get-Content -LiteralPath $dispatches[0].FullName -Raw -Encoding UTF8 | ConvertFrom-Json
    $dispatchId = [string]$dispatch.dispatchId
    $dispatchFull = [System.IO.Path]::GetFullPath($dispatches[0].FullName)
    $events = @(Get-ChildItem -LiteralPath (Join-Path $repo '.claude/gates/proofs/events') -Filter '*.json' -ErrorAction SilentlyContinue | ForEach-Object {
        $e = Get-Content -LiteralPath $_.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
        $eventDispatchId = [string]$e.dispatchId
        $eventDispatchPath = Resolve-FormalGateReceiptPath $repo ([string]$e.dispatchRegistrationArtifact)
        $matchesDispatch = (-not [string]::IsNullOrWhiteSpace($dispatchId) -and $eventDispatchId -eq $dispatchId)
        if (-not $matchesDispatch -and -not [string]::IsNullOrWhiteSpace($eventDispatchPath)) {
            $matchesDispatch = [System.IO.Path]::GetFullPath($eventDispatchPath) -eq $dispatchFull
        }
        if ($matchesDispatch -and [string]$e.provider -eq $script:FormalGateProofReceiptProvider -and [string]$e.workflowId -eq $script:FormalGateProofReceiptWorkflowId -and [string]$e.gate -eq $script:FormalGateProofReceiptGate -and [string]$e.stage -eq $stageValue) {
            [pscustomobject]@{ Path = $_.FullName; Event = $e }
        }
    })
    $starts = @($events | Where-Object { [string]$_.Event.normalizedEvent -eq 'subagent_start' })
    $stops = @($events | Where-Object { [string]$_.Event.normalizedEvent -eq 'subagent_stop' })
    if ($starts.Count -ne 1 -or $stops.Count -ne 1) { throw "Receipt finalization requires exactly one matching start and one matching stop event; found start=$($starts.Count) stop=$($stops.Count)." }
    $startId = [string]$starts[0].Event.subagentId
    $stopId = [string]$stops[0].Event.subagentId
    if (-not [string]::IsNullOrWhiteSpace($startId) -and -not [string]::IsNullOrWhiteSpace($stopId) -and $startId -ne $stopId) {
        throw 'Receipt finalization blocked: start/stop subagent ids mismatch.'
    }

    $artifactText = Get-Content -LiteralPath $reviewPath -Raw -Encoding UTF8
    $canonicalHash = Get-FormalGateReviewArtifactCanonicalSha256 $artifactText
    $proofDir = Join-Path $repo '.claude/gates/proofs'
    New-FormalGateReceiptDirectory $proofDir
    $receiptPath = Join-Path $proofDir ((New-FormalGateReceiptId) + '.json')
    $dispatchRel = ConvertTo-FormalGateReceiptRelativePath $repo $dispatches[0].FullName
    $startRel = ConvertTo-FormalGateReceiptRelativePath $repo $starts[0].Path
    $stopRel = ConvertTo-FormalGateReceiptRelativePath $repo $stops[0].Path
    $dispatch.status = 'finalized'
    $dispatch | Add-Member -NotePropertyName receiptArtifact -NotePropertyValue (ConvertTo-FormalGateReceiptRelativePath $repo $receiptPath) -Force
    $dispatchJson = $dispatch | ConvertTo-Json -Depth 12
    [System.IO.File]::WriteAllText($dispatches[0].FullName, $dispatchJson, [System.Text.UTF8Encoding]::new($false))
    $receipt = [ordered]@{
        proofVersion = 1
        provider = $script:FormalGateProofReceiptProvider
        workflowId = $script:FormalGateProofReceiptWorkflowId
        gate = $script:FormalGateProofReceiptGate
        stage = $stageValue
        worktree = $repo.Replace('\', '/')
        dispatchId = $dispatchId
        dispatchRegistrationArtifact = $dispatchRel
        dispatchRegistrationSha256 = Get-FormalGateReceiptSha256 $dispatches[0].FullName
        subagentId = $startId
        normalizedEvents = @('subagent_start', 'subagent_stop')
        rawEventNames = @([string]$starts[0].Event.rawEventName, [string]$stops[0].Event.rawEventName)
        startEventArtifact = $startRel
        startEventSha256 = Get-FormalGateReceiptSha256 $starts[0].Path
        stopEventArtifact = $stopRel
        stopEventSha256 = Get-FormalGateReceiptSha256 $stops[0].Path
        reviewArtifact = $reviewRel
        reviewArtifactCanonicalSha256 = $canonicalHash
        status = [string]$stops[0].Event.status
    }
    Write-FormalGateReceiptJson $receiptPath $receipt
    $receiptRel = ConvertTo-FormalGateReceiptRelativePath $repo $receiptPath
    $receiptRef = "Reviewer proof receipt: $receiptRel sha256=$(Get-FormalGateReceiptSha256 $receiptPath)"
    [System.IO.File]::WriteAllText($reviewPath, (Add-FormalGateReceiptLine $artifactText $receiptRef), [System.Text.UTF8Encoding]::new($false))
    Write-Output (@{ reviewerProofReceipt = "$receiptRel sha256=$(Get-FormalGateReceiptSha256 $receiptPath)" } | ConvertTo-Json -Compress)
}

if ($MyInvocation.InvocationName -eq '.') { return }
if ($script:FormalGateProofReceiptAction -eq '') { return }
if ([string]::IsNullOrWhiteSpace($script:FormalGateProofReceiptWorktree)) { $script:FormalGateProofReceiptWorktree = (Get-Location).Path }

switch ($script:FormalGateProofReceiptAction) {
    'register-dispatch' { Register-FormalGateProofDispatch; break }
    'capture-hook' { Save-FormalGateProofEvent; break }
    'finalize' { Complete-FormalGateProofReceipt; break }
}
