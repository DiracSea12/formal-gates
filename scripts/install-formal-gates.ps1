param(
    [ValidateSet('Claude', 'Codex', 'Cursor', 'Both')]
    [string]$HostName = 'Claude',

    [ValidateSet('Global', 'Project')]
    [string]$Scope = 'Global',

    [string]$ProjectPath,

    [string]$SourcePath,

    [switch]$Force,

    [switch]$RunCanary,

    [switch]$ConfigureHook
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'powershell-host.ps1')

function Resolve-FullPath([string]$Path) {
    return [System.IO.Path]::GetFullPath($Path)
}

function Get-DefaultSourcePath {
    return Resolve-FullPath (Split-Path -Parent $PSScriptRoot)
}

function Assert-SkillPackage([string]$Path) {
    $required = @(
        'SKILL.md',
        'agents',
        'agents/qa-test-gate.md',
        'agents/complexity-gate.md',
        'agents/architecture-health-gate.md',
        'agents/code-quality-gate.md',
        'agents/cold-water-review.md',
        'agents/requirements-clarification-gate.md',
        'examples',
        'hooks',
        'references',
        'scripts',
        'references/requirements-clarification-gate.md',
        'references/requirements-clarification-artifacts.md',
        'references/post-development-artifacts.md',
        'references/qa-test-gate.md',
        'references/complexity-gate.md',
        'references/architecture-health-gate.md',
        'references/code-quality-gate.md',
        'references/install-and-hooks.md',
        'scripts/gate-artifact-validation.ps1',
        'scripts/gate-proof-receipt.ps1',
        'scripts/powershell-host.ps1',
        'scripts/run-complexity-gate.ps1',
        'scripts/validate-dispatch-prompt.ps1',
        'scripts/gate-state.ps1',
        'scripts/gate-workflow.ps1',
        'scripts/test-portable-openspec-canary.ps1',
        'hooks/enforce-gate-sequence.ps1',
        'hooks/capture-subagent-receipt.ps1',
        'hooks/test-subagent-receipt-canary-common.ps1',
        'hooks/test-claude-subagent-receipt-canary.ps1',
        'hooks/test-codex-subagent-receipt-canary.ps1',
        'hooks/test-cursor-subagent-receipt-canary.ps1',
        'hooks/pollution-patterns.json'
    )

    foreach ($relative in $required) {
        $candidate = Join-Path $Path $relative
        if (-not (Test-Path -LiteralPath $candidate)) {
            throw "formal-gates package is incomplete; missing $relative under $Path"
        }
    }
}

function ConvertTo-QuotedCommandArgument([string]$Value) {
    return '"' + $Value.Replace('"', '\"') + '"'
}

function ConvertTo-HookScriptArgument([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return '""' }
    return '"' + $Value.Replace('"', '\"') + '"'
}

function Get-FormalGatesHookCommand([string]$HookScriptPath) {
    $powerShellExe = ConvertTo-QuotedCommandArgument (Get-FormalGatesPowerShellExe)
    $args = @(Get-FormalGatesPowerShellFileArgs $HookScriptPath | ForEach-Object { ConvertTo-QuotedCommandArgument ([string]$_) })
    return (@($powerShellExe) + @($args)) -join ' '
}

function Get-FormalGatesReceiptHookCommand(
    [string]$HookScriptPath,
    [string]$Provider,
    [string]$EventName
) {
    $base = Get-FormalGatesHookCommand $HookScriptPath
    return "$base -ReceiptProvider $(ConvertTo-HookScriptArgument $Provider) -ReceiptEventName $(ConvertTo-HookScriptArgument $EventName)"
}

function Test-FormalGatesHookCommand([string]$Command) {
    return ([string]$Command) -like '*formal-gates*' -or
        ([string]$Command) -like '*enforce-gate-sequence.ps1*' -or
        ([string]$Command) -like '*capture-subagent-receipt.ps1*'
}

function Test-FormalGatesHookObject($Hook) {
    if ($null -eq $Hook) { return $false }
    $text = @(
        [string]$Hook.command
        foreach ($arg in @($Hook.args)) { [string]$arg }
    ) -join ' '
    return Test-FormalGatesHookCommand $text
}

function Write-FormalGatesJsonFile([string]$Path, [object]$Value) {
    $json = $Value | ConvertTo-Json -Depth 12
    [System.IO.File]::WriteAllText($Path, $json, [System.Text.UTF8Encoding]::new($false))
}

function Read-FormalGatesJsonFile([string]$Path, [string]$InvalidJsonMessage, [string]$BackupName) {
    $value = $null
    if (Test-Path -LiteralPath $Path) {
        $raw = Get-Content -LiteralPath $Path -Raw -Encoding UTF8
        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            try {
                $value = $raw | ConvertFrom-Json
            }
            catch {
                throw "${InvalidJsonMessage}: $Path"
            }
        }
        Copy-Item -LiteralPath $Path -Destination "$Path.bak" -Force
        Write-Host "Backed up existing $BackupName to $Path.bak"
    }
    if ($null -eq $value) { $value = [pscustomobject]@{} }
    return $value
}

function Ensure-FormalGatesObjectProperty([object]$Object, [string]$Name, [object]$DefaultValue) {
    if (-not ($Object.PSObject.Properties.Name -contains $Name) -or $null -eq $Object.$Name) {
        $Object | Add-Member -NotePropertyName $Name -NotePropertyValue $DefaultValue -Force
    }
}

function New-ClaudeHookEntry([string]$Matcher, [string]$Command) {
    return [pscustomobject]@{
        matcher = $Matcher
        hooks   = @(
            [pscustomobject]@{
                type    = 'command'
                command = $Command
            }
        )
    }
}

function New-CodexHookEntry([string]$Matcher, [string]$Command) {
    return [pscustomobject]@{
        matcher = $Matcher
        hooks   = @(
            [pscustomobject]@{
                type    = 'command'
                command = $Command
                timeout = 30
            }
        )
    }
}

function New-CursorHookEntry([string]$Command) {
    return [pscustomobject]@{
        command    = $Command
        timeout    = 30
        failClosed = $true
    }
}

function Get-SkillPackageEntries {
    return @(
        'SKILL.md',
        'agents',
        'examples',
        'hooks',
        'references',
        'scripts'
    )
}

function Get-InstallTargets([string]$HostName, [string]$Scope, [string]$ProjectPath) {
    $hosts = if ($HostName -eq 'Both') { @('Claude', 'Codex') } else { @($HostName) }
    $targets = @()

    foreach ($targetHost in $hosts) {
        if ($Scope -eq 'Global') {
            if ($targetHost -eq 'Claude') { $base = Join-Path $HOME '.claude/skills' }
            elseif ($targetHost -eq 'Codex') { $base = Join-Path $HOME '.codex/skills' }
            else { $base = Join-Path $HOME '.cursor' }
        }
        else {
            if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
                throw '-ProjectPath is required when -Scope Project is used.'
            }
            $project = Resolve-FullPath $ProjectPath
            if ($targetHost -eq 'Claude') { $base = Join-Path $project '.claude/skills' }
            elseif ($targetHost -eq 'Codex') { $base = Join-Path $project '.codex/skills' }
            else { $base = Join-Path $project '.cursor' }
        }

        $targets += [pscustomobject]@{
            Host = $targetHost
            Path = Resolve-FullPath (Join-Path $base 'formal-gates')
        }
    }

    return $targets
}

function Remove-ExistingTarget([string]$TargetPath) {
    $resolved = Resolve-FullPath $TargetPath
    $leaf = Split-Path -Leaf $resolved
    $parent = Split-Path -Parent $resolved
    $parentLeaf = Split-Path -Leaf $parent

    if ($leaf -ne 'formal-gates' -or $parentLeaf -notin @('skills', '.cursor')) {
        throw "Refusing to replace unexpected target path: $resolved"
    }

    Remove-Item -LiteralPath $resolved -Recurse -Force
}

function Copy-SkillPackage([string]$SourcePath, [string]$TargetPath, [bool]$Force) {
    $source = Resolve-FullPath $SourcePath
    $target = Resolve-FullPath $TargetPath

    if ($source.TrimEnd('\', '/') -ieq $target.TrimEnd('\', '/')) {
        Write-Host "formal-gates already installed at $target"
        return
    }

    if (Test-Path -LiteralPath $target) {
        if (-not $Force) {
            throw "Target already exists: $target. Re-run with -Force to replace it."
        }
        Remove-ExistingTarget $target
    }

    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    foreach ($entry in Get-SkillPackageEntries) {
        $candidate = Join-Path $source $entry
        if (-not (Test-Path -LiteralPath $candidate)) {
            throw "formal-gates package is incomplete; missing $entry under $source"
        }
        Copy-Item -LiteralPath $candidate -Destination $target -Recurse -Force
    }

    Get-ChildItem -LiteralPath $target -Recurse -Force -Directory -Filter '__pycache__' -ErrorAction SilentlyContinue |
        Remove-Item -Recurse -Force

    Assert-SkillPackage $target
    Write-Host "formal-gates installed: $target"
}

function Get-ClaudeSettingsPath([string]$Scope, [string]$ProjectPath) {
    if ($Scope -eq 'Global') {
        return Join-Path $HOME '.claude/settings.json'
    }
    if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
        throw '-ProjectPath is required to configure a project-local hook.'
    }
    return Join-Path (Resolve-FullPath $ProjectPath) '.claude/settings.json'
}

function Get-CursorHooksPath([string]$Scope, [string]$ProjectPath) {
    if ($Scope -eq 'Global') {
        return Join-Path $HOME '.cursor/hooks.json'
    }
    if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
        throw '-ProjectPath is required to configure a project-local Cursor hook.'
    }
    return Join-Path (Resolve-FullPath $ProjectPath) '.cursor/hooks.json'
}

function Remove-FormalGatesHookEntries(
    [object[]]$Entries,
    [ValidateSet('nested', 'flat')]
    [string]$HookShape
) {
    $kept = @()
    foreach ($entry in @($Entries)) {
        if ($null -eq $entry) { continue }

        if ($HookShape -eq 'nested' -and $entry.PSObject.Properties.Name -contains 'hooks') {
            $remainingHooks = @(
                foreach ($hook in @($entry.hooks)) {
                    if (-not (Test-FormalGatesHookObject $hook)) { $hook }
                }
            )
            if ($remainingHooks.Count -gt 0) {
                $entry.hooks = $remainingHooks
                $kept += $entry
            }
            continue
        }

        if (-not (Test-FormalGatesHookObject $entry)) {
            $kept += $entry
        }
    }
    return @($kept)
}

function Set-FormalGatesHookEvents(
    [object]$Config,
    [System.Collections.Specialized.OrderedDictionary]$DesiredHooks,
    [ValidateSet('nested', 'flat')]
    [string]$HookShape
) {
    Ensure-FormalGatesObjectProperty $Config 'hooks' ([pscustomobject]@{})

    foreach ($eventName in $DesiredHooks.Keys) {
        Ensure-FormalGatesObjectProperty $Config.hooks $eventName (@())
        $existing = @(Remove-FormalGatesHookEntries @($Config.hooks.($eventName)) $HookShape)
        $Config.hooks.($eventName) = @($existing + @($DesiredHooks[$eventName]))
    }

    foreach ($property in @($Config.hooks.PSObject.Properties)) {
        if ($DesiredHooks.Keys -contains $property.Name) { continue }
        $value = $property.Value
        if ($value -is [System.Array]) {
            $Config.hooks.($property.Name) = Remove-FormalGatesHookEntries @($value) $HookShape
        }
    }
}

function Set-FormalGatesHookFile(
    [string]$Path,
    [string]$InvalidJsonMessage,
    [string]$BackupName,
    [System.Collections.Specialized.OrderedDictionary]$DesiredHooks,
    [ValidateSet('nested', 'flat')]
    [string]$HookShape,
    [bool]$EnsureVersion,
    [string]$WrittenMessage,
    [string]$LifecycleMessage
) {
    $config = Read-FormalGatesJsonFile $Path $InvalidJsonMessage $BackupName
    if ($EnsureVersion) {
        Ensure-FormalGatesObjectProperty $config 'version' 1
    }
    Set-FormalGatesHookEvents $config $DesiredHooks $HookShape

    $parent = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    Write-FormalGatesJsonFile $Path $config
    Write-Host $WrittenMessage
    Write-Host $LifecycleMessage
}

function Set-FormalGatesHook([string]$SettingsPath, [string]$GateHookScriptPath, [string]$ReceiptHookScriptPath) {
    $desired = [ordered]@{
        PreToolUse    = New-ClaudeHookEntry '*' (Get-FormalGatesHookCommand $GateHookScriptPath)
        SubagentStart = New-ClaudeHookEntry '*' (Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'claude-code' 'SubagentStart')
        SubagentStop  = New-ClaudeHookEntry '*' (Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'claude-code' 'SubagentStop')
    }
    Set-FormalGatesHookFile `
        -Path $SettingsPath `
        -InvalidJsonMessage 'Existing settings.json is not valid JSON; refusing to touch it' `
        -BackupName 'settings' `
        -DesiredHooks $desired `
        -HookShape 'nested' `
        -EnsureVersion $false `
        -WrittenMessage "formal-gates Claude lifecycle hooks written to $SettingsPath" `
        -LifecycleMessage 'formal-gates Claude receipt capture lifecycle hooks enabled: SubagentStart, SubagentStop'
}

function Get-CodexHooksPath([string]$Scope, [string]$ProjectPath) {
    if ($Scope -eq 'Global') {
        return Join-Path $HOME '.codex/hooks.json'
    }
    if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
        throw '-ProjectPath is required to configure a project-local Codex hook.'
    }
    return Join-Path (Resolve-FullPath $ProjectPath) '.codex/hooks.json'
}

function Set-CodexFormalGatesHook([string]$HooksPath, [string]$GateHookScriptPath, [string]$ReceiptHookScriptPath) {
    $gateCommand = Get-FormalGatesHookCommand $GateHookScriptPath
    $startCommand = Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'codex' 'SubagentStart'
    $stopCommand = Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'codex' 'SubagentStop'

    $desired = [ordered]@{
        PreToolUse    = New-CodexHookEntry '*' $gateCommand
        SubagentStart = New-CodexHookEntry '*' $startCommand
        SubagentStop  = New-CodexHookEntry '*' $stopCommand
    }
    Set-FormalGatesHookFile `
        -Path $HooksPath `
        -InvalidJsonMessage 'Existing Codex hooks.json is not valid JSON; refusing to touch it' `
        -BackupName 'Codex hooks' `
        -DesiredHooks $desired `
        -HookShape 'nested' `
        -EnsureVersion $false `
        -WrittenMessage "formal-gates Codex lifecycle hooks written to $HooksPath" `
        -LifecycleMessage 'formal-gates Codex receipt capture lifecycle hooks enabled: SubagentStart, SubagentStop'
}

function Set-CursorFormalGatesHook([string]$HooksPath, [string]$GateHookScriptPath, [string]$ReceiptHookScriptPath) {
    $gateCommand = Get-FormalGatesHookCommand $GateHookScriptPath
    $startCommand = Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'cursor' 'SubagentStart'
    $stopCommand = Get-FormalGatesReceiptHookCommand $ReceiptHookScriptPath 'cursor' 'SubagentStop'

    $desired = [ordered]@{
        preToolUse    = New-CursorHookEntry $gateCommand
        subagentStart = New-CursorHookEntry $startCommand
        subagentStop  = New-CursorHookEntry $stopCommand
    }
    Set-FormalGatesHookFile `
        -Path $HooksPath `
        -InvalidJsonMessage 'Existing Cursor hooks.json is not valid JSON; refusing to touch it' `
        -BackupName 'Cursor hooks' `
        -DesiredHooks $desired `
        -HookShape 'flat' `
        -EnsureVersion $true `
        -WrittenMessage "formal-gates Cursor lifecycle hooks written to $HooksPath" `
        -LifecycleMessage 'formal-gates Cursor receipt capture lifecycle hooks enabled: subagentStart, subagentStop'
}

$source = if ([string]::IsNullOrWhiteSpace($SourcePath)) {
    Get-DefaultSourcePath
}
else {
    Resolve-FullPath $SourcePath
}

Assert-SkillPackage $source
$targets = Get-InstallTargets $HostName $Scope $ProjectPath

foreach ($target in $targets) {
    Copy-SkillPackage $source $target.Path ([bool]$Force)
    $hookPath = Join-Path $target.Path 'hooks/enforce-gate-sequence.ps1'
    $receiptHookPath = Join-Path $target.Path 'hooks/capture-subagent-receipt.ps1'
    Write-Host "$($target.Host) hook path: $hookPath"

    if ($RunCanary) {
        $canary = Join-Path $target.Path 'scripts/test-portable-openspec-canary.ps1'
        $canaryArgs = (Get-FormalGatesPowerShellFileArgs $canary) + @('-SkillPath', $target.Path)
        & (Get-FormalGatesPowerShellExe) @canaryArgs
        if ($LASTEXITCODE -ne 0) {
            throw "Portable canary failed for $($target.Path)"
        }
    }

    if ($ConfigureHook) {
        if ($target.Host -eq 'Claude') {
            $settingsPath = Get-ClaudeSettingsPath $Scope $ProjectPath
            Set-FormalGatesHook $settingsPath $hookPath $receiptHookPath
        }
        elseif ($target.Host -eq 'Codex') {
            $hooksPath = Get-CodexHooksPath $Scope $ProjectPath
            Set-CodexFormalGatesHook $hooksPath $hookPath $receiptHookPath
        }
        elseif ($target.Host -eq 'Cursor') {
            $hooksPath = Get-CursorHooksPath $Scope $ProjectPath
            $gateHookCommandPath = if ($Scope -eq 'Project') {
                '.cursor/formal-gates/hooks/enforce-gate-sequence.ps1'
            }
            else {
                $hookPath
            }
            $receiptHookCommandPath = if ($Scope -eq 'Project') {
                '.cursor/formal-gates/hooks/capture-subagent-receipt.ps1'
            }
            else {
                $receiptHookPath
            }
            Set-CursorFormalGatesHook $hooksPath $gateHookCommandPath $receiptHookCommandPath
        }
    }
}
