param(
    [ValidateSet('Claude', 'Codex', 'Both')]
    [string]$HostName = 'Claude',

    [ValidateSet('Global', 'Project')]
    [string]$Scope = 'Global',

    [string]$ProjectPath,

    [string]$SourcePath,

    [switch]$Force,

    [switch]$RunCanary
)

$ErrorActionPreference = 'Stop'

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
    Copy-Item -LiteralPath $source -Destination $target -Recurse -Force

    Get-ChildItem -LiteralPath $target -Recurse -Force -Directory -Filter '__pycache__' -ErrorAction SilentlyContinue |
        Remove-Item -Recurse -Force

    Assert-SkillPackage $target
    Write-Host "formal-gates installed: $target"
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
        & pwsh -NoProfile $canary -SkillPath $target.Path
        if ($LASTEXITCODE -ne 0) {
            throw "Portable canary failed for $($target.Path)"
        }
    }
}
