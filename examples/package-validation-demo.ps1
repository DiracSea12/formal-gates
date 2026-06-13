param(
    [string]$OutputPath = (Join-Path $PSScriptRoot 'package-validation-demo-output.txt')
)

$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$resolvedOutputPath = [System.IO.Path]::GetFullPath($OutputPath)
$outputDir = Split-Path -Parent $resolvedOutputPath
if (-not (Test-Path -LiteralPath $outputDir)) {
    New-Item -ItemType Directory -Path $outputDir | Out-Null
}

$lines = New-Object System.Collections.Generic.List[string]

function Add-Line([string]$Text = '') {
    $script:lines.Add($Text) | Out-Null
}

function Resolve-CommandPath([string]$Name, [string[]]$Fallbacks = @()) {
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($command) {
        return $command.Source
    }

    foreach ($fallback in $Fallbacks) {
        if (Test-Path -LiteralPath $fallback) {
            return $fallback
        }
    }

    return $Name
}

function Invoke-DemoCommand([string]$Label, [string]$FilePath, [string[]]$Arguments) {
    Add-Line "## $Label"
    Add-Line "Command: $FilePath $($Arguments -join ' ')"
    Add-Line ''

    Push-Location $script:repoRoot
    try {
        $output = & $FilePath @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    }
    finally {
        Pop-Location
    }

    if ($null -ne $output) {
        foreach ($line in $output) {
            Add-Line ([string]$line)
        }
    }

    Add-Line ''
    Add-Line "ExitCode: $exitCode"
    Add-Line ''

    if ($exitCode -ne 0) {
        throw "$Label failed with exit code $exitCode"
    }
}

Add-Line "# Package Validation Demo Output"
Add-Line "Repository: $repoRoot"
Add-Line "GeneratedAt: $((Get-Date).ToString('o'))"
Add-Line ''

$goExe = Resolve-CommandPath 'go' @('C:\Program Files\Go\bin\go.exe', 'C:\Go\bin\go.exe')

Invoke-DemoCommand 'Go package validation' $goExe @('run', './cmd/formal-gates-validate', 'package', '--root', '.')
Invoke-DemoCommand 'Portable OpenSpec canary' 'powershell' @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', 'scripts\test-portable-openspec-canary.ps1', '-SkillPath', '.')

Add-Line 'Result: PASS'

Set-Content -LiteralPath $resolvedOutputPath -Value $lines -Encoding UTF8
Write-Host "Wrote $resolvedOutputPath"
