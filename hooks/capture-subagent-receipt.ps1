param(
    [string]$ReceiptProvider,
    [string]$ReceiptWorktree,
    [string]$ReceiptWorkflowId,
    [string]$ReceiptGate,
    [string]$ReceiptStage,
    [string]$ReceiptEventName,
    [string]$ReceiptSubagentId,
    [string]$ReceiptStatus,
    [string]$ReceiptDispatchId,
    [string]$ReceiptDispatchRegistrationArtifact,
    [string]$ReceiptPayloadJson
)

$ErrorActionPreference = 'Stop'

$receiptScript = Join-Path $PSScriptRoot '..\scripts\gate-proof-receipt.ps1'
if (-not (Test-Path -LiteralPath $receiptScript -PathType Leaf)) {
    throw "gate-proof-receipt.ps1 not found: $receiptScript"
}

if ($MyInvocation.InvocationName -eq '.') {
    return
}

function Get-FormalGateHookStdinJson {
    if (-not [string]::IsNullOrWhiteSpace($ReceiptPayloadJson)) { return $ReceiptPayloadJson }
    try {
        if ([Console]::IsInputRedirected) {
            $text = [Console]::In.ReadToEnd()
            if (-not [string]::IsNullOrWhiteSpace($text)) { return $text }
        }
    }
    catch {
        return ''
    }
    return ''
}

function ConvertFrom-FormalGateHookJson([string]$Json) {
    if ([string]::IsNullOrWhiteSpace($Json)) { return $null }
    try { return ($Json | ConvertFrom-Json) }
    catch { return $null }
}

function Get-FormalGateHookProperty([object]$Object, [string]$Name) {
    if ($null -eq $Object) { return $null }
    foreach ($property in @($Object.PSObject.Properties)) {
        if ($property.Name -ieq $Name) { return $property.Value }
    }
    return $null
}

function ConvertTo-FormalGateHookScalar([object]$Value) {
    if ($null -eq $Value) { return $null }
    if ($Value -is [string]) { $text = $Value }
    elseif ($Value -is [ValueType]) { $text = [string]$Value }
    else { return $null }
    if ([string]::IsNullOrWhiteSpace($text)) { return $null }
    return $text
}

function Get-FormalGateHookPayloadValue([object]$Object, [string[]]$Names, [int]$Depth = 0) {
    foreach ($name in $Names) {
        $value = ConvertTo-FormalGateHookScalar (Get-FormalGateHookProperty $Object $name)
        if (-not [string]::IsNullOrWhiteSpace($value)) { return $value }
    }
    if ($Depth -ge 3) { return $null }
    foreach ($containerName in @('payload', 'event', 'data', 'hook', 'tool_input', 'toolInput', 'input')) {
        $container = Get-FormalGateHookProperty $Object $containerName
        if ($null -ne $container -and -not ($container -is [string])) {
            $value = Get-FormalGateHookPayloadValue $container $Names ($Depth + 1)
            if (-not [string]::IsNullOrWhiteSpace($value)) { return $value }
        }
    }
    return $null
}

function Resolve-FormalGateHookPath([string]$BasePath, [string]$MaybeRelativePath) {
    if ([string]::IsNullOrWhiteSpace($MaybeRelativePath)) { return $null }
    if ([System.IO.Path]::IsPathRooted($MaybeRelativePath)) { return $MaybeRelativePath }
    if ([string]::IsNullOrWhiteSpace($BasePath)) { $BasePath = (Get-Location).Path }
    return Join-Path $BasePath $MaybeRelativePath
}

function ConvertTo-FormalGateHookRelativePath([string]$BasePath, [string]$Path) {
    $fullBase = [System.IO.Path]::GetFullPath($BasePath).TrimEnd('\', '/')
    $fullPath = [System.IO.Path]::GetFullPath($Path)
    if ($fullPath.StartsWith($fullBase + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        return $fullPath.Substring($fullBase.Length + 1).Replace('\', '/')
    }
    return $fullPath.Replace('\', '/')
}

function Get-FormalGateHookDispatchRegistration(
    [string]$Worktree,
    [string]$Provider,
    [string]$DispatchId,
    [string]$DispatchRegistrationArtifact
) {
    if ([string]::IsNullOrWhiteSpace($Worktree)) { $Worktree = (Get-Location).Path }
    $repo = [System.IO.Path]::GetFullPath($Worktree)
    if (-not [string]::IsNullOrWhiteSpace($DispatchRegistrationArtifact)) {
        $path = Resolve-FormalGateHookPath $repo $DispatchRegistrationArtifact
        if (Test-Path -LiteralPath $path -PathType Leaf) {
            $record = Get-Content -LiteralPath $path -Raw -Encoding UTF8 | ConvertFrom-Json
            if (-not [string]::IsNullOrWhiteSpace($Provider) -and [string]$record.provider -ne $Provider) {
                return $null
            }
            return [pscustomobject]@{
                Record = $record
                Path = $path
                RelativePath = ConvertTo-FormalGateHookRelativePath $repo $path
            }
        }
    }
    if ([string]::IsNullOrWhiteSpace($DispatchId)) { return $null }
    $dispatchDir = Join-Path $repo '.claude/gates/proofs/dispatch'
    $matches = @(Get-ChildItem -LiteralPath $dispatchDir -Filter '*.json' -ErrorAction SilentlyContinue | ForEach-Object {
        $record = Get-Content -LiteralPath $_.FullName -Raw -Encoding UTF8 | ConvertFrom-Json
        if ([string]$record.dispatchId -eq $DispatchId -and ([string]::IsNullOrWhiteSpace($Provider) -or [string]$record.provider -eq $Provider)) {
            [pscustomobject]@{
                Record = $record
                Path = $_.FullName
                RelativePath = ConvertTo-FormalGateHookRelativePath $repo $_.FullName
            }
        }
    })
    if ($matches.Count -eq 1) { return $matches[0] }
    return $null
}

$payloadJson = Get-FormalGateHookStdinJson
$payload = ConvertFrom-FormalGateHookJson $payloadJson

if ($null -ne $payload) {
    if ([string]::IsNullOrWhiteSpace($ReceiptWorktree)) { $ReceiptWorktree = Get-FormalGateHookPayloadValue $payload @('worktree', 'workspace', 'workspacePath', 'cwd', 'repo', 'repository', 'repositoryPath', 'projectPath') }
    if ([string]::IsNullOrWhiteSpace($ReceiptWorkflowId)) { $ReceiptWorkflowId = Get-FormalGateHookPayloadValue $payload @('workflowId', 'formalWorkflowId', 'workflow_id') }
    if ([string]::IsNullOrWhiteSpace($ReceiptGate)) { $ReceiptGate = Get-FormalGateHookPayloadValue $payload @('gate', 'gateId', 'gate_id') }
    if ([string]::IsNullOrWhiteSpace($ReceiptStage)) { $ReceiptStage = Get-FormalGateHookPayloadValue $payload @('stage', 'gateStage', 'stageName') }
    if ([string]::IsNullOrWhiteSpace($ReceiptEventName)) { $ReceiptEventName = Get-FormalGateHookPayloadValue $payload @('eventName', 'event', 'hookEvent', 'type', 'lifecycleEvent') }
    if ([string]::IsNullOrWhiteSpace($ReceiptSubagentId)) { $ReceiptSubagentId = Get-FormalGateHookPayloadValue $payload @('subagentId', 'subagent_id', 'agentId', 'agent_id', 'taskId', 'task_id') }
    if ([string]::IsNullOrWhiteSpace($ReceiptStatus)) { $ReceiptStatus = Get-FormalGateHookPayloadValue $payload @('status', 'result', 'outcome', 'stopStatus', 'stop_status', 'reason') }
    if ([string]::IsNullOrWhiteSpace($ReceiptDispatchId)) { $ReceiptDispatchId = Get-FormalGateHookPayloadValue $payload @('dispatchId', 'dispatch_id') }
    if ([string]::IsNullOrWhiteSpace($ReceiptDispatchRegistrationArtifact)) { $ReceiptDispatchRegistrationArtifact = Get-FormalGateHookPayloadValue $payload @('dispatchRegistrationArtifact', 'dispatch_registration_artifact', 'dispatchPath', 'dispatchRegistrationPath') }
}

if ([string]::IsNullOrWhiteSpace($ReceiptWorktree)) { $ReceiptWorktree = (Get-Location).Path }

$dispatchRegistration = Get-FormalGateHookDispatchRegistration $ReceiptWorktree $ReceiptProvider $ReceiptDispatchId $ReceiptDispatchRegistrationArtifact
if ($null -ne $dispatchRegistration) {
    $dispatch = $dispatchRegistration.Record
    if ([string]::IsNullOrWhiteSpace($ReceiptProvider)) { $ReceiptProvider = [string]$dispatch.provider }
    if ([string]::IsNullOrWhiteSpace($ReceiptWorkflowId)) { $ReceiptWorkflowId = [string]$dispatch.workflowId }
    if ([string]::IsNullOrWhiteSpace($ReceiptGate)) { $ReceiptGate = [string]$dispatch.gate }
    if ([string]::IsNullOrWhiteSpace($ReceiptStage)) { $ReceiptStage = [string]$dispatch.stage }
    if ([string]::IsNullOrWhiteSpace($ReceiptDispatchId)) { $ReceiptDispatchId = [string]$dispatch.dispatchId }
    if ([string]::IsNullOrWhiteSpace($ReceiptDispatchRegistrationArtifact)) { $ReceiptDispatchRegistrationArtifact = [string]$dispatchRegistration.RelativePath }
}

& $receiptScript -ReceiptAction capture-hook -ReceiptProvider $ReceiptProvider -ReceiptWorktree $ReceiptWorktree -ReceiptWorkflowId $ReceiptWorkflowId -ReceiptGate $ReceiptGate -ReceiptStage $ReceiptStage -ReceiptEventName $ReceiptEventName -ReceiptSubagentId $ReceiptSubagentId -ReceiptStatus $ReceiptStatus -ReceiptDispatchId $ReceiptDispatchId -ReceiptDispatchRegistrationArtifact $ReceiptDispatchRegistrationArtifact -ReceiptPayloadJson $payloadJson
