param(
    [string]$Worktree = (Get-Location).Path,
    [string]$OutputDir,
    [string]$CodexCommand = 'codex',
    [int]$TimeoutSeconds = 180,
    [switch]$KeepTemp
)

$ErrorActionPreference = 'Stop'
. (Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/powershell-host.ps1')

function New-Directory([string]$Path) {
    New-Item -ItemType Directory -Force -Path $Path | Out-Null
    return [System.IO.Path]::GetFullPath($Path)
}

function Get-CodexHome {
    if (-not [string]::IsNullOrWhiteSpace($env:CODEX_HOME)) {
        return [System.IO.Path]::GetFullPath($env:CODEX_HOME)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $HOME '.codex'))
}

function Get-CodexVersion([string]$Command) {
    $versionJob = $null
    try {
        $versionJob = Start-Job -ScriptBlock {
            param($CommandToRun)
            & $CommandToRun --version 2>$null
        } -ArgumentList $Command
        if (Wait-Job -Job $versionJob -Timeout 15) {
            $output = @(Receive-Job -Job $versionJob -ErrorAction SilentlyContinue)
            if ($output.Count -gt 0) { return [string]$output[0] }
            return 'unavailable: empty version output'
        }
        Stop-Job -Job $versionJob -ErrorAction SilentlyContinue
        return 'unavailable: version command timed out'
    }
    catch {
        return "unavailable: $($_.Exception.Message)"
    }
    finally {
        if ($null -ne $versionJob) { Remove-Job -Job $versionJob -Force -ErrorAction SilentlyContinue }
    }
}

function Get-CodexProfileFlag([string]$Command) {
    $help = [string](& $Command exec --help 2>&1)
    if ($help -match '--profile-v2') { return '--profile-v2' }
    if ($help -match '(^|\s)--profile(\s|,)') { return '--profile' }
    throw "Codex command '$Command' does not expose --profile or --profile-v2 for temporary hook config."
}

function ConvertTo-SingleQuotedPowerShellLiteral([string]$Value) {
    return "'" + ($Value -replace "'", "''") + "'"
}

function Get-ProcessLaunch([string]$Command) {
    $resolved = Get-Command $Command -ErrorAction Stop
    if ($resolved.CommandType -eq 'ExternalScript' -and $resolved.Path.EndsWith('.ps1', [System.StringComparison]::OrdinalIgnoreCase)) {
        $scriptDir = Split-Path -Parent $resolved.Path
        $codexJs = Join-Path $scriptDir 'node_modules/@openai/codex/bin/codex.js'
        if (Test-Path -LiteralPath $codexJs -PathType Leaf) {
            $nodeCommand = Get-Command 'node.exe' -ErrorAction SilentlyContinue
            if ($null -eq $nodeCommand) { $nodeCommand = Get-Command 'node' -ErrorAction Stop }
            return [pscustomobject]@{
                FilePath = $nodeCommand.Source
                PrefixArguments = @($codexJs)
            }
        }
        return [pscustomobject]@{
            FilePath = Get-FormalGatesPowerShellExe
            PrefixArguments = Get-FormalGatesPowerShellFileArgs $resolved.Path
        }
    }
    return [pscustomobject]@{
        FilePath = $resolved.Source
        PrefixArguments = @()
    }
}

$timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$resolvedWorktree = [System.IO.Path]::GetFullPath($Worktree)
$artifactRoot = if ([string]::IsNullOrWhiteSpace($OutputDir)) {
    Join-Path $resolvedWorktree '.artifacts/ai/formal-gates-hook-client-tests'
}
else {
    $OutputDir
}
$artifactRoot = New-Directory $artifactRoot
$caseName = "codex-hook-client-canary-$timestamp"
$caseDir = New-Directory (Join-Path $artifactRoot $caseName)
$diagDir = New-Directory (Join-Path $caseDir 'payloads')
$codexHome = Get-CodexHome
$codexHome = New-Directory $codexHome
$profileName = "formal-gates-hook-canary-$timestamp"
$profilePath = Join-Path $codexHome "$profileName.config.toml"
$hookPath = Join-Path $caseDir 'diag-hook.ps1'
$formalHookPath = Join-Path $PSScriptRoot 'enforce-gate-sequence.ps1'
$gateWorkflowPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/gate-workflow.ps1'
$formalHookOutputPath = Join-Path $caseDir 'formal-hook-output.txt'
$stdoutPath = Join-Path $caseDir 'codex.stdout.jsonl'
$stderrPath = Join-Path $caseDir 'codex.stderr.txt'
$finalPath = Join-Path $caseDir 'codex.final.txt'
$promptPath = Join-Path $caseDir 'prompt.txt'
$markerPath = Join-Path $caseDir 'marker.txt'
$summaryPath = Join-Path $artifactRoot "$caseName.summary.json"

$diagLiteral = ConvertTo-SingleQuotedPowerShellLiteral $diagDir
$formalHookLiteral = ConvertTo-SingleQuotedPowerShellLiteral $formalHookPath
$formalHookOutputLiteral = ConvertTo-SingleQuotedPowerShellLiteral $formalHookOutputPath
$powerShellExeLiteral = ConvertTo-SingleQuotedPowerShellLiteral (Get-FormalGatesPowerShellExe)
$edition = if ($PSVersionTable.ContainsKey('PSEdition')) { [string]$PSVersionTable.PSEdition } else { 'Desktop' }
$powerShellPolicyArgs = if ($edition -eq 'Desktop') { "    `$formalArgs += @('-ExecutionPolicy', 'Bypass')`n" } else { '' }
$hookContent = @"
`$ErrorActionPreference = 'Stop'
`$payload = [Console]::In.ReadToEnd()
`$eventName = 'unknown'
`$toolName = 'unknown'
try {
    `$json = `$payload | ConvertFrom-Json -ErrorAction Stop
    if (`$json.hook_event_name) { `$eventName = [string]`$json.hook_event_name }
    if (`$json.tool_name) { `$toolName = [string]`$json.tool_name }
}
catch {}
`$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-ffff'
`$outDir = $diagLiteral
`$out = Join-Path `$outDir ("hook-`$eventName-`$toolName-`$stamp.json")
Set-Content -LiteralPath `$out -Value `$payload -Encoding UTF8
if (`$eventName -eq 'PreToolUse') {
    `$formalHook = $formalHookLiteral
    `$formalOutputPath = $formalHookOutputLiteral
    `$formalPowerShell = $powerShellExeLiteral
    `$formalArgs = @('-NoProfile')
$powerShellPolicyArgs    `$formalArgs += @('-File', `$formalHook)
    `$formalOutput = `$payload | & `$formalPowerShell @formalArgs 2>&1
    `$formalExit = `$LASTEXITCODE
    Add-Content -LiteralPath `$formalOutputPath -Value ("exit=`$formalExit") -Encoding UTF8
    if (`$formalOutput) {
        Add-Content -LiteralPath `$formalOutputPath -Value ((`$formalOutput | ForEach-Object { [string]`$_ }) -join "`n") -Encoding UTF8
        `$formalOutput
    }
    exit `$formalExit
}
"@
Set-Content -LiteralPath $hookPath -Value $hookContent -Encoding UTF8

$hookForToml = $hookPath.Replace('\', '/').Replace('"', '\"')
$powerShellForToml = (Get-FormalGatesPowerShellExe).Replace('\', '/').Replace('"', '\"')
$powerShellCommandPrefix = '"' + $powerShellForToml + '" -NoProfile'
if ($edition -eq 'Desktop') { $powerShellCommandPrefix += ' -ExecutionPolicy Bypass' }
$profileContent = @"
[features]
hooks = true

[[hooks.UserPromptSubmit]]
[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = '$powerShellCommandPrefix -File "$hookForToml"'
timeout = 30
statusMessage = "formal-gates Codex hook canary user prompt"

[[hooks.PreToolUse]]
matcher = "*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = '$powerShellCommandPrefix -File "$hookForToml"'
timeout = 30
statusMessage = "formal-gates Codex hook canary pre tool"

[[hooks.PostToolUse]]
matcher = "*"
[[hooks.PostToolUse.hooks]]
type = "command"
command = '$powerShellCommandPrefix -File "$hookForToml"'
timeout = 30
statusMessage = "formal-gates Codex hook canary post tool"

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = '$powerShellCommandPrefix -File "$hookForToml"'
timeout = 30
statusMessage = "formal-gates Codex hook canary stop"
"@
Set-Content -LiteralPath $profilePath -Value $profileContent -Encoding UTF8

$profileFlag = $null
$exitCode = $null
$timedOut = $false
$process = $null
try {
    $profileFlag = Get-CodexProfileFlag $CodexCommand
    $gateWorkflowForPrompt = $gateWorkflowPath.Replace('\', '/')
    $caseDirForPrompt = $caseDir.Replace('\', '/')
    $markerForPrompt = $markerPath.Replace('\', '/')
    $powerShellForPrompt = (Get-FormalGatesPowerShellExe).Replace('\', '/')
    $prompt = 'Run exactly this shell command once, then stop: & ''' + $powerShellForPrompt + ''' -NoProfile'
    if ($edition -eq 'Desktop') { $prompt += ' -ExecutionPolicy Bypass' }
    $prompt += ' -File ''' + $gateWorkflowForPrompt + ''' -Action record-stage -Worktree ''' + $caseDirForPrompt + ''' -Gate complexity-gate -Verdict PASS -Mode formal -Artifact .claude/gates/artifacts/missing-hook-canary.md -Actor hook-canary -WorkflowId hook-canary -ChangeSnapshot hook-snapshot; Set-Content -LiteralPath ''' + $markerForPrompt + ''' -Value HIT'
    Set-Content -LiteralPath $promptPath -Value $prompt -Encoding UTF8
    $arguments = @(
        'exec',
        '--json',
        $profileFlag,
        $profileName,
        '--enable',
        'hooks',
        '--dangerously-bypass-hook-trust',
        '--sandbox',
        'danger-full-access',
        '--skip-git-repo-check',
        '-c',
        'approval_policy="never"',
        '-o',
        $finalPath,
        '-'
    )

    $launch = Get-ProcessLaunch $CodexCommand
    $commandArgs = @($launch.PrefixArguments) + $arguments
    $process = Start-Process -FilePath $launch.FilePath -ArgumentList $commandArgs -WorkingDirectory $resolvedWorktree -NoNewWindow -PassThru -RedirectStandardInput $promptPath -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
        $timedOut = $true
        try { $process.Kill($true) } catch {}
        $process.WaitForExit()
    }
    else {
        $exitCode = $process.ExitCode
    }
}
catch {
    $exitCode = -1
    Add-Content -LiteralPath $stderrPath -Value $_.Exception.Message -Encoding UTF8
}
finally {
    Remove-Item -LiteralPath $profilePath -Force -ErrorAction SilentlyContinue
}

$payloads = @(Get-ChildItem -LiteralPath $diagDir -Filter 'hook-*.json' -ErrorAction SilentlyContinue)
$preToolUsePayloads = @($payloads | Where-Object { $_.Name -like 'hook-PreToolUse-*' })
$markerExists = Test-Path -LiteralPath $markerPath
$formalHookOutput = if (Test-Path -LiteralPath $formalHookOutputPath) { Get-Content -LiteralPath $formalHookOutputPath -Raw -ErrorAction SilentlyContinue } else { '' }
$formalHookBlocked = $formalHookOutput -match 'permissionDecision"\s*:\s*"deny"|decision"\s*:\s*"block"|PASS blocked|GATE_SEQUENCE|Gate sequence blocked|review artifact is incomplete|clarification artifact is incomplete'
$status = if ($timedOut) {
    'TIMED_OUT'
}
elseif ($preToolUsePayloads.Count -gt 0 -and -not $markerExists -and $formalHookBlocked) {
    'PASS'
}
else {
    'FAIL'
}

$summary = [ordered]@{
    status = $status
    case = $caseName
    codexCommand = $CodexCommand
    codexVersion = Get-CodexVersion $CodexCommand
    profileFlag = $profileFlag
    timeoutSeconds = $TimeoutSeconds
    exitCode = $exitCode
    markerExists = $markerExists
    hookPayloadCount = $payloads.Count
    preToolUsePayloadCount = $preToolUsePayloads.Count
    artifactDir = $caseDir
    stdout = $stdoutPath
    stderr = $stderrPath
    final = $finalPath
    prompt = $promptPath
    payloadDir = $diagDir
    formalHookOutput = $formalHookOutputPath
    expectedPassCondition = 'At least one PreToolUse hook payload exists, enforce-gate-sequence.ps1 denies the invalid formal PASS command, and marker.txt was not created.'
}

$summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $summaryPath -Encoding UTF8
$summary | ConvertTo-Json -Depth 5

if (-not $KeepTemp -and $status -eq 'PASS') {
    Remove-Item -LiteralPath $caseDir -Recurse -Force -ErrorAction SilentlyContinue
}

if ($status -eq 'PASS') { exit 0 }
exit 1
