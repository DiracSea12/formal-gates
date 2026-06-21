function Resolve-CanaryFullPath([string]$Path, [string]$Fallback) {
    if ([string]::IsNullOrWhiteSpace($Path)) { $Path = $Fallback }
    return [System.IO.Path]::GetFullPath($Path)
}

function New-CanaryDirectory([string]$Path) {
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
}

function Get-JsonPropertyValue([object]$Object, [string]$Name) {
    if ($null -eq $Object) { return $null }
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) { return $null }
    return $property.Value
}

function Get-HostCanaryHookCommands(
    [object]$Config,
    [string]$EventName,
    [ValidateSet('nested', 'flat')]
    [string]$HookShape
) {
    $commands = @()
    $hooksRoot = Get-JsonPropertyValue $Config 'hooks'
    $entries = Get-JsonPropertyValue $hooksRoot $EventName
    foreach ($entry in @($entries)) {
        if ($HookShape -eq 'nested') {
            foreach ($hook in @((Get-JsonPropertyValue $entry 'hooks'))) {
                $command = [string](Get-JsonPropertyValue $hook 'command')
                if (-not [string]::IsNullOrWhiteSpace($command)) { $commands += $command }
            }
            continue
        }

        $command = [string](Get-JsonPropertyValue $entry 'command')
        if (-not [string]::IsNullOrWhiteSpace($command)) { $commands += $command }
    }
    return $commands
}

function Test-ReceiptCommand([string[]]$Commands, [string]$Provider, [string]$EventName) {
    foreach ($command in @($Commands)) {
        if ($command -like '*capture-subagent-receipt.ps1*' -and
            $command -like "*-ReceiptProvider `"$Provider`"*" -and
            $command -like "*-ReceiptEventName `"$EventName`"*") {
            return $true
        }
    }
    return $false
}

function Write-UnsupportedReceiptDiagnostic([object]$Payload, [string]$DiagnosticPath) {
    $json = $Payload | ConvertTo-Json -Depth 8
    New-CanaryDirectory (Split-Path -Parent $DiagnosticPath)
    Set-Content -LiteralPath $DiagnosticPath -Value $json -Encoding UTF8
    Write-Output $json
    exit 2
}

function Invoke-HostSubagentReceiptCanary(
    [ValidateSet('preflight', 'positive')]
    [string]$Mode,
    [string]$SkillPath,
    [string]$Worktree,
    [string]$OutputDir,
    [string]$HostDisplayName,
    [string]$Provider,
    [string]$DiagnosticSlug,
    [string]$ProjectConfigRelativePath,
    [string]$GlobalConfigRelativePath,
    [string]$MissingConfigMessage,
    [string]$ConfigReadErrorPrefix,
    [ValidateSet('nested', 'flat')]
    [string]$HookShape,
    [object[]]$EventDefinitions,
    [string]$CommandPropertyName,
    [string]$CommandValue
) {
    $resolvedWorktree = Resolve-CanaryFullPath $Worktree (Get-Location).Path
    $resolvedSkillPath = Resolve-CanaryFullPath $SkillPath (Join-Path $PSScriptRoot '..')
    $resolvedOutputDir = Resolve-CanaryFullPath $OutputDir (Join-Path $resolvedWorktree '.claude/gates/proofs/host-receipt-canaries')
    New-CanaryDirectory $resolvedOutputDir

    $checkedConfigPaths = @(
        (Join-Path $resolvedWorktree $ProjectConfigRelativePath),
        (Join-Path $HOME $GlobalConfigRelativePath)
    )
    $configPath = $null
    foreach ($candidate in $checkedConfigPaths) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            $configPath = [System.IO.Path]::GetFullPath($candidate)
            break
        }
    }

    $missing = @()
    $configuredLifecycleHooks = [ordered]@{}
    if ([string]::IsNullOrWhiteSpace($configPath)) {
        $missing += $MissingConfigMessage
        foreach ($event in @($EventDefinitions)) {
            $configuredLifecycleHooks[[string]$event.OutputName] = @()
        }
    }
    else {
        try {
            $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
            foreach ($event in @($EventDefinitions)) {
                $commands = @(Get-HostCanaryHookCommands $config ([string]$event.ConfigEventName) $HookShape)
                $configuredLifecycleHooks[[string]$event.OutputName] = $commands
                if (-not (Test-ReceiptCommand $commands $Provider ([string]$event.ReceiptEventName))) {
                    $missing += [string]$event.HookMissing
                }
            }
        }
        catch {
            $missing += "${ConfigReadErrorPrefix}: $($_.Exception.Message)"
        }
    }

    foreach ($event in @($EventDefinitions)) {
        $missing += [string]$event.PayloadMissing
    }
    $missing += 'usable host correlation fields tying both payloads to one dispatch registration'
    if ($Mode -eq 'positive') {
        $missing += "positive mode requires a real $HostDisplayName subagent lifecycle run; script-direct payloads are not accepted"
    }
    else {
        $missing += 'preflight is diagnostic only and does not prove host lifecycle emission'
    }

    $diagnosticPath = Join-Path $resolvedOutputDir "$DiagnosticSlug-subagent-receipt-$Mode-diagnostic.json"
    $payload = [ordered]@{
        status = 'UNSUPPORTED_HOST_RECEIPT'
        host = $HostDisplayName
        mode = $Mode
        configPath = $configPath
        checkedConfigPath = @($checkedConfigPaths | ForEach-Object { [System.IO.Path]::GetFullPath($_) })
        missing = $missing
        requiredLifecycleEvents = @($EventDefinitions | ForEach-Object { [string]$_.OutputName })
        usableCorrelationFields = @()
        rawPayloadArtifacts = @()
        diagnosticArtifact = $diagnosticPath
        skillPath = $resolvedSkillPath
        worktree = $resolvedWorktree
    }
    $payload[$CommandPropertyName] = $CommandValue
    $payload['configuredLifecycleHooks'] = $configuredLifecycleHooks

    Write-UnsupportedReceiptDiagnostic $payload $diagnosticPath
}
