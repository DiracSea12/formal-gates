function Get-FormalGatesPowerShellExe {
    $processPath = $null
    try {
        $processPath = (Get-Process -Id $PID).Path
    }
    catch {}

    if (-not [string]::IsNullOrWhiteSpace($processPath) -and (Test-Path -LiteralPath $processPath)) {
        return $processPath
    }

    $candidates = @()
    if (-not [string]::IsNullOrWhiteSpace($PSHOME)) {
        $candidates += (Join-Path $PSHOME 'pwsh.exe')
        $candidates += (Join-Path $PSHOME 'powershell.exe')
        $candidates += (Join-Path $PSHOME 'pwsh')
        $candidates += (Join-Path $PSHOME 'powershell')
    }

    foreach ($candidate in ($candidates | Select-Object -Unique)) {
        if (Test-Path -LiteralPath $candidate) { return $candidate }
    }

    foreach ($name in @('pwsh', 'powershell')) {
        $command = Get-Command $name -ErrorAction SilentlyContinue
        if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
            return $command.Source
        }
    }

    throw 'PowerShell executable not found. Run this script from Windows PowerShell 5 or PowerShell 7.'
}

function Get-FormalGatesPowerShellFileArgs([string]$ScriptPath) {
    $args = @('-NoProfile')
    $edition = if ($PSVersionTable.ContainsKey('PSEdition')) { [string]$PSVersionTable.PSEdition } else { 'Desktop' }
    if ($edition -eq 'Desktop') {
        $args += @('-ExecutionPolicy', 'Bypass')
    }
    $args += @('-File', $ScriptPath)
    return $args
}
