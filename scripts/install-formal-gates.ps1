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
        'scripts/powershell-host.ps1',
        'scripts/run-complexity-gate.ps1',
        'scripts/gate-state.ps1',
        'scripts/gate-workflow.ps1',
        'scripts/test-portable-openspec-canary.ps1',
        'hooks/enforce-gate-sequence.ps1'
    )

    foreach ($relative in $required) {
        $candidate = Join-Path $Path $relative
        if (-not (Test-Path -LiteralPath $candidate)) {
            throw "formal-gates package is incomplete; missing $relative under $Path"
        }
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

function Set-FormalGatesHook([string]$SettingsPath, [string]$HookScriptPath) {
    # Read, merge, and write back only the formal-gates hook; preserve other hooks. Idempotent, with backup before write.

    $settings = $null
    if (Test-Path -LiteralPath $SettingsPath) {
        $raw = Get-Content -LiteralPath $SettingsPath -Raw -Encoding UTF8
        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            try {
                $settings = $raw | ConvertFrom-Json
            }
            catch {
                throw "Existing settings.json is not valid JSON; refusing to touch it: $SettingsPath"
            }
        }
        Copy-Item -LiteralPath $SettingsPath -Destination "$SettingsPath.bak" -Force
        Write-Host "Backed up existing settings to $SettingsPath.bak"
    }
    if ($null -eq $settings) { $settings = [pscustomobject]@{} }

    if (-not ($settings.PSObject.Properties.Name -contains 'hooks') -or $null -eq $settings.hooks) {
        $settings | Add-Member -NotePropertyName 'hooks' -NotePropertyValue ([pscustomobject]@{}) -Force
    }
    if (-not ($settings.hooks.PSObject.Properties.Name -contains 'PreToolUse') -or $null -eq $settings.hooks.PreToolUse) {
        $settings.hooks | Add-Member -NotePropertyName 'PreToolUse' -NotePropertyValue (@()) -Force
    }

    $preToolUse = @($settings.hooks.PreToolUse)
    $formalHookFound = $false
    $formalHookUpdated = $false
    foreach ($entry in $preToolUse) {
        foreach ($h in @($entry.hooks)) {
            if (([string]$h.command) -like "*enforce-gate-sequence.ps1*") {
                $formalHookFound = $true
                if ($entry.PSObject.Properties.Name -contains 'matcher') {
                    if ([string]$entry.matcher -ne '*') {
                        $entry.matcher = '*'
                        $formalHookUpdated = $true
                    }
                }
                else {
                    $entry | Add-Member -NotePropertyName 'matcher' -NotePropertyValue '*' -Force
                    $formalHookUpdated = $true
                }
                if ([string]$h.command -ne ('powershell -NoProfile -ExecutionPolicy Bypass -File "' + [string]$HookScriptPath + '"')) {
                    if ($h.PSObject.Properties.Name -contains 'type') {
                        $h.type = 'command'
                    }
                    else {
                        $h | Add-Member -NotePropertyName 'type' -NotePropertyValue 'command' -Force
                    }
                    if ($h.PSObject.Properties.Name -contains 'command') {
                        $h.command = 'powershell -NoProfile -ExecutionPolicy Bypass -File "' + [string]$HookScriptPath + '"'
                    }
                    else {
                        $h | Add-Member -NotePropertyName 'command' -NotePropertyValue ('powershell -NoProfile -ExecutionPolicy Bypass -File "' + [string]$HookScriptPath + '"') -Force
                    }
                    $formalHookUpdated = $true
                }
            }
        }
    }
    if ($formalHookFound) {
        if ($formalHookUpdated) {
            $settings | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $SettingsPath -Encoding UTF8
            Write-Host "formal-gates PreToolUse hook updated in $SettingsPath"
        }
        else {
            Write-Host "formal-gates hook already present in $SettingsPath; left unchanged."
        }
        return
    }

    $newEntry = [pscustomobject]@{
        matcher = '*'
        hooks   = @(
            [pscustomobject]@{
                type    = 'command'
                command = 'powershell -NoProfile -ExecutionPolicy Bypass -File "' + [string]$HookScriptPath + '"'
            }
        )
    }
    $settings.hooks.PreToolUse = @($preToolUse + $newEntry)

    $parent = Split-Path -Parent $SettingsPath
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $settings | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $SettingsPath -Encoding UTF8
    Write-Host "formal-gates PreToolUse hook added to $SettingsPath"
}

function Set-CursorFormalGatesHook([string]$HooksPath, [string]$HookScriptPath) {
    $command = 'powershell -NoProfile -ExecutionPolicy Bypass -File "{0}"' -f $HookScriptPath

    $config = $null
    if (Test-Path -LiteralPath $HooksPath) {
        $raw = Get-Content -LiteralPath $HooksPath -Raw -Encoding UTF8
        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            try {
                $config = $raw | ConvertFrom-Json
            }
            catch {
                throw "Existing Cursor hooks.json is not valid JSON; refusing to touch it: $HooksPath"
            }
        }
        Copy-Item -LiteralPath $HooksPath -Destination "$HooksPath.bak" -Force
        Write-Host "Backed up existing Cursor hooks to $HooksPath.bak"
    }
    if ($null -eq $config) { $config = [pscustomobject]@{} }

    if (-not ($config.PSObject.Properties.Name -contains 'version')) {
        $config | Add-Member -NotePropertyName 'version' -NotePropertyValue 1 -Force
    }
    if (-not ($config.PSObject.Properties.Name -contains 'hooks') -or $null -eq $config.hooks) {
        $config | Add-Member -NotePropertyName 'hooks' -NotePropertyValue ([pscustomobject]@{}) -Force
    }
    if (-not ($config.hooks.PSObject.Properties.Name -contains 'preToolUse') -or $null -eq $config.hooks.preToolUse) {
        $config.hooks | Add-Member -NotePropertyName 'preToolUse' -NotePropertyValue (@()) -Force
    }

    $preToolUse = @($config.hooks.preToolUse)
    foreach ($entry in $preToolUse) {
        if (([string]$entry.command) -like "*enforce-gate-sequence.ps1*") {
            $entry.command = $command
            $entry.timeout = 30
            if ($entry.PSObject.Properties.Name -contains 'failClosed') {
                $entry.failClosed = $true
            }
            else {
                $entry | Add-Member -NotePropertyName 'failClosed' -NotePropertyValue $true -Force
            }
            if ($entry.PSObject.Properties.Name -contains 'matcher') {
                $entry.PSObject.Properties.Remove('matcher')
            }
            $config | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $HooksPath -Encoding UTF8
            Write-Host "formal-gates Cursor preToolUse hook updated in $HooksPath"
            return
        }
    }

    $newEntry = [pscustomobject]@{
        command    = $command
        timeout    = 30
        failClosed = $true
    }
    $config.hooks.preToolUse = @($preToolUse + $newEntry)

    $parent = Split-Path -Parent $HooksPath
    if (-not (Test-Path -LiteralPath $parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $config | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $HooksPath -Encoding UTF8
    Write-Host "formal-gates Cursor preToolUse hook added to $HooksPath"
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
            Set-FormalGatesHook $settingsPath $hookPath
        }
        elseif ($target.Host -eq 'Cursor') {
            $hooksPath = Get-CursorHooksPath $Scope $ProjectPath
            $hookCommandPath = if ($Scope -eq 'Project') {
                '.cursor/formal-gates/hooks/enforce-gate-sequence.ps1'
            }
            else {
                $hookPath
            }
            Set-CursorFormalGatesHook $hooksPath $hookCommandPath
        }
        else {
            Write-Host "Skipping -ConfigureHook for $($target.Host): see references/install-and-hooks.md for Codex hook config."
        }
    }
}
