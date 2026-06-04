param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = 'Stop'

function Test-Python3Candidate([string]$Command, [string[]]$PrefixArguments) {
    $versionOutput = $null
    try {
        $versionOutput = & $Command @PrefixArguments -c "import sys; print(sys.version_info[0])" 2>$null
    }
    catch {
        return $false
    }
    return (($versionOutput | Select-Object -First 1) -eq '3')
}

function Get-Python3Launch {
    $candidates = @(
        [pscustomobject]@{ Command = 'python3'; PrefixArguments = @() },
        [pscustomobject]@{ Command = 'py'; PrefixArguments = @('-3') },
        [pscustomobject]@{ Command = 'python'; PrefixArguments = @() }
    )

    foreach ($candidate in $candidates) {
        $resolved = Get-Command $candidate.Command -ErrorAction SilentlyContinue
        if ($null -eq $resolved) { continue }
        if (Test-Python3Candidate $candidate.Command $candidate.PrefixArguments) {
            return $candidate
        }
    }

    throw 'Python 3 was not found. Install python3, py -3, or make python point to Python 3 for formal-gates complexity checks.'
}

$scriptPath = Join-Path $PSScriptRoot 'complexity_gate.py'
if (-not (Test-Path -LiteralPath $scriptPath)) {
    throw "complexity_gate.py not found next to run-complexity-gate.ps1: $scriptPath"
}

$python = Get-Python3Launch
$pythonArgs = @($python.PrefixArguments) + @($scriptPath) + @($Arguments)
& $python.Command @pythonArgs
exit $LASTEXITCODE
