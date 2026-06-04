param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = 'Stop'

function Get-PythonVersionCandidate([string]$Command, [string[]]$PrefixArguments) {
    $versionOutput = $null
    try {
        $versionOutput = & $Command @PrefixArguments -c "import sys; print('%s.%s' % (sys.version_info[0], sys.version_info[1]))" 2>$null
    }
    catch {
        return $null
    }
    return [string]($versionOutput | Select-Object -First 1)
}

function Test-SupportedPythonVersion([string]$Version) {
    if ([string]::IsNullOrWhiteSpace($Version)) { return $false }
    if ($Version -match '^3\.') { return $true }
    return ($Version -eq '2.7')
}

function Get-PythonLaunch {
    $candidates = @(
        [pscustomobject]@{ Command = 'python3'; PrefixArguments = @() },
        [pscustomobject]@{ Command = 'py'; PrefixArguments = @('-3') },
        [pscustomobject]@{ Command = 'python'; PrefixArguments = @() },
        [pscustomobject]@{ Command = 'py'; PrefixArguments = @('-2') }
    )

    foreach ($candidate in $candidates) {
        $resolved = Get-Command $candidate.Command -ErrorAction SilentlyContinue
        if ($null -eq $resolved) { continue }
        $version = Get-PythonVersionCandidate $candidate.Command $candidate.PrefixArguments
        if (Test-SupportedPythonVersion $version) {
            return $candidate
        }
    }

    throw 'Supported Python was not found. Install Python 3.x or Python 2.7 for formal-gates complexity checks.'
}

$scriptPath = Join-Path $PSScriptRoot 'complexity_gate.py'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw "complexity_gate.py not found next to run-complexity-gate.ps1: $scriptPath"
}

$python = Get-PythonLaunch
$pythonArgs = @($python.PrefixArguments) + @($scriptPath) + @($Arguments)
& $python.Command @pythonArgs
exit $LASTEXITCODE
