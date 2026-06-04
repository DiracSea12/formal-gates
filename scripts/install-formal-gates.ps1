param(
    [ValidateSet('Claude', 'Codex', 'Both')]
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
        'examples',
        'hooks',
        'references',
        'scripts',
        'references/requirements-clarification-gate.md',
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

function Get-InstallTargets([string]$HostName, [string]$Scope, [string]$ProjectPath) {
    $hosts = if ($HostName -eq 'Both') { @('Claude', 'Codex') } else { @($HostName) }
    $targets = @()

    foreach ($targetHost in $hosts) {
        if ($Scope -eq 'Global') {
            $base = if ($targetHost -eq 'Claude') {
                Join-Path $HOME '.claude/skills'
            }
            else {
                Join-Path $HOME '.codex/skills'
            }
        }
        else {
            if ([string]::IsNullOrWhiteSpace($ProjectPath)) {
                throw '-ProjectPath is required when -Scope Project is used.'
            }
            $project = Resolve-FullPath $ProjectPath
            $base = if ($targetHost -eq 'Claude') {
                Join-Path $project '.claude/skills'
            }
            else {
                Join-Path $project '.codex/skills'
            }
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

    if ($leaf -ne 'formal-gates' -or $parentLeaf -ne 'skills') {
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
    Get-ChildItem -LiteralPath $source -Force |
        Where-Object { $_.Name -notin @('.git', '.github', '__pycache__') } |
        ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $target -Recurse -Force }

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

function Set-FormalGatesHook([string]$SettingsPath, [string]$HookScriptPath) {
    # 读取-合并-写回，只新增/更新 formal-gates 自己的 hook；不覆盖其它 hook；幂等；写前备份。
    $matcher = 'Bash|Agent|Skill'
    $command = 'powershell -NoProfile -ExecutionPolicy Bypass -File "{0}"' -f $HookScriptPath

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
                if ([string]$h.command -ne $command) {
                    if ($h.PSObject.Properties.Name -contains 'type') {
                        $h.type = 'command'
                    }
                    else {
                        $h | Add-Member -NotePropertyName 'type' -NotePropertyValue 'command' -Force
                    }
                    if ($h.PSObject.Properties.Name -contains 'command') {
                        $h.command = $command
                    }
                    else {
                        $h | Add-Member -NotePropertyName 'command' -NotePropertyValue $command -Force
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
        matcher = $matcher
        hooks   = @(
            [pscustomobject]@{
                type    = 'command'
                command = $command
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
        else {
            Write-Host "Skipping -ConfigureHook for $($target.Host): auto hook config supports Claude settings.json only. See references/install-and-hooks.md for Codex."
        }
    }
}
