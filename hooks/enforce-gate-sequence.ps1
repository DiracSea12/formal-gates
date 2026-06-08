$ErrorActionPreference = 'Stop'

$formalGatesPowerShellHost = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/powershell-host.ps1'
if (Test-Path -LiteralPath $formalGatesPowerShellHost) {
    . $formalGatesPowerShellHost
}
$formalGatesArtifactValidation = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/gate-artifact-validation.ps1'
if (-not (Test-Path -LiteralPath $formalGatesArtifactValidation)) {
    throw "Bundled formal-gates gate-artifact-validation.ps1 was not found: $formalGatesArtifactValidation"
}
. $formalGatesArtifactValidation

$inputJson = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($inputJson)) { exit 0 }

try {
    $payload = $inputJson | ConvertFrom-Json
}
catch {
    exit 0
}

$toolName = [string]$payload.tool_name
$toolInput = $payload.tool_input
$inputProperties = @{}
if ($null -ne $toolInput) {
    foreach ($property in $toolInput.PSObject.Properties) {
        $inputProperties[$property.Name] = $property.Value
    }
}
$commandText = [string]$inputProperties['command']
$rawIntentText = (($inputProperties['args'], $inputProperties['prompt'], $inputProperties['description'], $commandText) | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) }) -join ' '
$script:UnresolvedFormalDocumentWrite = $false

$gateAliases = @{
    'requirements-clarification-gate' = 'requirements-clarification-gate'
    'qa-test-gate' = 'qa-test-gate'
    'complexity-gate' = 'complexity-gate'
    'architecture-health-gate' = 'architecture-health-gate'
    'code-quality-gate' = 'code-quality-gate'
}
$routerSkillNames = @('formal-gates')

function Format-HookPath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $Path }
    try {
        $full = [System.IO.Path]::GetFullPath($Path)
        $home = [System.IO.Path]::GetFullPath($HOME).TrimEnd('\', '/')
        if ($full.StartsWith($home, [System.StringComparison]::OrdinalIgnoreCase)) {
            return ('~' + $full.Substring($home.Length)).Replace('\', '/')
        }
        return $full.Replace('\', '/')
    }
    catch {
        return $Path.Replace('\', '/')
    }
}

function Block-Gate([string]$Message, [string]$Reason) {
    $displayReason = $Reason
    if (-not [string]::IsNullOrWhiteSpace($Message) -and $Message -ne $Reason) {
        $displayReason = "$Message $Reason"
    }
    [pscustomobject]@{
        permission = 'deny'
        user_message = $displayReason
        agent_message = $displayReason
        decision = 'block'
        reason = $displayReason
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            permissionDecision = 'deny'
            permissionDecisionReason = $displayReason
        }
    } | ConvertTo-Json -Depth 6 -Compress
    exit 2
}

function Invoke-GateState([string[]]$Arguments, [string]$WorkingDirectory) {
    $stdoutPath = [System.IO.Path]::GetTempFileName()
    $stderrPath = [System.IO.Path]::GetTempFileName()
    try {
        $process = Start-Process -FilePath (Get-FormalGatesPowerShellExe) -ArgumentList $Arguments -WorkingDirectory $WorkingDirectory -NoNewWindow -PassThru -Wait -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
        $stdout = Get-Content -LiteralPath $stdoutPath -Raw -ErrorAction SilentlyContinue
        $stderr = Get-Content -LiteralPath $stderrPath -Raw -ErrorAction SilentlyContinue
        return [pscustomobject]@{
            ExitCode = $process.ExitCode
            Output = (($stdout, $stderr) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }) -join "`n"
        }
    }
    finally {
        Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Test-IntentTerm([string]$Pattern, [int[]]$ChineseCodePoints) {
    if ($rawIntentText -match $Pattern) { return $true }
    $term = -join ($ChineseCodePoints | ForEach-Object { [char]$_ })
    return $rawIntentText.Contains($term)
}

function Get-ToolGateIntent {
    if ($toolName -eq 'Skill') {
        $skillName = [string]$inputProperties['skill']
        if ($gateAliases.ContainsKey($skillName)) { return $gateAliases[$skillName] }
        if ($routerSkillNames -contains $skillName) { return $skillName }
        return $null
    }
    if ($toolName -in @('Agent', 'Task')) {
        if (Test-IntentTerm '(?i)(requirements-clarification-gate|requirement\s+clarification|requirements\s+clarification|clarification\s+gate)' @(0x6F84, 0x6E05, 0x95E8)) { return 'requirements-clarification-gate' }
        if (Test-IntentTerm '(?i)(requirements-clarification-gate|requirement\s+alignment|requirements\s+alignment|alignment\s+gate)' @(0x9700, 0x6C42, 0x5BF9, 0x9F50)) { return 'requirements-clarification-gate' }
        if (Test-IntentTerm '(?i)(architecture-health-gate|architecture\s+gate)' @(0x67B6, 0x6784, 0x95E8)) { return 'architecture-health-gate' }
        if (Test-IntentTerm '(?i)(code-quality-gate|code\s+quality\s+gate)' @(0x4EE3, 0x7801, 0x8D28, 0x91CF, 0x95E8)) { return 'code-quality-gate' }
        if (Test-IntentTerm '(?i)(qa-test-gate|qa\s+gate)' @(0x6D4B, 0x8BD5, 0x95E8)) { return 'qa-test-gate' }
        if (Test-IntentTerm '(?i)(complexity-gate|complexity\s+gate)' @(0x590D, 0x6742, 0x5EA6, 0x95E8)) { return 'complexity-gate' }
    }
    return $null
}

function Find-JsonObjectAfterKey([string]$Text, [string[]]$Keys) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $null }
    foreach ($key in $Keys) {
        $match = [regex]::Match($Text, '(?i)(^|[\s,;])' + [regex]::Escape($key) + '\s*[:=]\s*\{')
        if (-not $match.Success) { continue }
        $openIndex = $match.Index + $match.Value.LastIndexOf('{')
        $depth = 0
        $inString = $false
        $escaped = $false
        for ($index = $openIndex; $index -lt $Text.Length; ++$index) {
            $ch = $Text[$index]
            if ($inString) {
                if ($escaped) { $escaped = $false; continue }
                if ($ch -eq '\') { $escaped = $true; continue }
                if ($ch -eq '"') { $inString = $false; continue }
                continue
            }
            if ($ch -eq '"') { $inString = $true; continue }
            if ($ch -eq '{') { ++$depth; continue }
            if ($ch -eq '}') {
                --$depth
                if ($depth -eq 0) { return $Text.Substring($openIndex, $index - $openIndex + 1) }
            }
        }
    }
    return $null
}

function Convert-ToWorkflowObject($Value) {
    if ($null -eq $Value) { return $null }
    if ($Value -is [string]) {
        if ([string]::IsNullOrWhiteSpace($Value)) { return $null }
        try { return $Value | ConvertFrom-Json }
        catch { Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.' }
    }
    return $Value
}

function Get-StructuredWorkflow {
    foreach ($name in @('GateWorkflow', 'Workflow')) {
        if ($inputProperties.ContainsKey($name)) {
            return Convert-ToWorkflowObject $inputProperties[$name]
        }
    }
    $hasStructuredKey = $false
    foreach ($name in @('GateWorkflow', 'Workflow')) {
        if ($rawIntentText -match '(?i)(^|[\s,;])' + [regex]::Escape($name) + '\s*[:=]') {
            $hasStructuredKey = $true
            break
        }
    }
    $jsonText = Find-JsonObjectAfterKey $rawIntentText @('GateWorkflow', 'Workflow')
    if (-not [string]::IsNullOrWhiteSpace($jsonText)) { return Convert-ToWorkflowObject $jsonText }
    if ($hasStructuredKey) {
        Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.'
    }
    return $null
}

function Get-WorkflowFromText([string]$Text, [string[]]$Keys = @('GateWorkflow', 'Workflow')) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $null }
    $hasStructuredKey = $false
    foreach ($name in $Keys) {
        if ($Text -match '(?i)(^|[\s,;])' + [regex]::Escape($name) + '\s*[:=]') {
            $hasStructuredKey = $true
            break
        }
    }
    $jsonText = Find-JsonObjectAfterKey $Text $Keys
    if (-not [string]::IsNullOrWhiteSpace($jsonText)) { return Convert-ToWorkflowObject $jsonText }
    if ($hasStructuredKey) {
        Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.'
    }
    return $null
}

function Get-FormalDocumentWriteWorkflow([object[]]$Targets) {
    $workflow = Get-StructuredWorkflow
    if ($null -ne $workflow) { return $workflow }
    if ($Targets.Count -eq 0) { return $null }
    if ($toolName -notin @('Write', 'Edit', 'MultiEdit', 'NotebookEdit')) { return $null }

    foreach ($fieldName in @('content', 'new_string', 'text')) {
        if ($inputProperties.ContainsKey($fieldName)) {
            $workflow = Get-WorkflowFromText ([string]$inputProperties[$fieldName]) @('GateWorkflow')
            if ($null -ne $workflow) { return $workflow }
        }
    }

    if ($inputProperties.ContainsKey('edits') -and $null -ne $inputProperties['edits']) {
        foreach ($edit in @($inputProperties['edits'])) {
            if ($null -eq $edit) { continue }
            foreach ($fieldName in @('new_string', 'content', 'text')) {
                $property = $edit.PSObject.Properties[$fieldName]
                if ($null -ne $property) {
                    $workflow = Get-WorkflowFromText ([string]$property.Value) @('GateWorkflow')
                    if ($null -ne $workflow) { return $workflow }
                }
            }
        }
    }

    return $null
}

function Get-WorkflowField($Workflow, [string[]]$Names) {
    if ($null -eq $Workflow) { return $null }
    foreach ($prop in $Workflow.PSObject.Properties) {
        foreach ($name in $Names) {
            if ($prop.Name -ieq $name) { return $prop.Value }
        }
    }
    return $null
}

function Test-Truthy($Value) {
    if ($null -eq $Value) { return $false }
    if ($Value -is [bool]) { return [bool]$Value }
    return ([string]$Value) -match '(?i)^(true|yes|1)$'
}

function Allow-AdvisoryGate([string]$Gate) {
    [pscustomobject]@{
        permission = 'allow'
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            additionalContext = "UNSTRUCTURED GATE ENTRY = ADVISORY ONLY for $Gate. This is a standalone gate review only. It is not four-stage sequencing, not release/seal approval, and not permission to output, record, or reuse a workflow PASS. If the user asks for formal four-gate, release, seal, final approval, or recordable PASS, stop inside the skill with GATE_SEQUENCE_ERROR and require structured GateWorkflow metadata plus gate-state admission."
        }
    } | ConvertTo-Json -Depth 5 -Compress
    exit 0
}

function Split-CommandArguments([string]$Command) {
    $tokens = New-Object System.Collections.Generic.List[string]
    $current = [System.Text.StringBuilder]::new()
    $quote = [char]0
    for ($index = 0; $index -lt $Command.Length; ++$index) {
        $ch = $Command[$index]
        if ($quote -ne [char]0) {
            if ($ch -eq $quote) { $quote = [char]0; continue }
            [void]$current.Append($ch)
            continue
        }
        if ($ch -eq '"' -or $ch -eq "'") { $quote = $ch; continue }
        if ([char]::IsWhiteSpace($ch)) {
            if ($current.Length -gt 0) {
                $tokens.Add($current.ToString())
                [void]$current.Clear()
            }
            continue
        }
        [void]$current.Append($ch)
    }
    if ($current.Length -gt 0) { $tokens.Add($current.ToString()) }
    return @($tokens)
}

function Get-CommandSwitchValue([string]$Command, [string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return $null }
    $tokens = @(Split-CommandArguments (Normalize-HookCommandText $Command))
    for ($index = 0; $index -lt ($tokens.Count - 1); ++$index) {
        if ($tokens[$index] -ieq "-$Name") { return $tokens[$index + 1] }
    }
    return $null
}

function Normalize-HookCommandText([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return '' }
    $normalized = $Command
    for ($i = 0; $i -lt 3; ++$i) {
        $next = $normalized -replace '\\+"', '"'
        if ($next -eq $normalized) { break }
        $normalized = $next
    }
    return $normalized
}

function Test-CommandMentionsGateWorkflowScript([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return $false }
    return (Normalize-HookCommandText $Command) -match '(?i)(^|[\\/"])gate-workflow\.ps1(["\s]|$)'
}

function Resolve-HookRelativePath([string]$BasePath, [string]$MaybeRelativePath) {
    if ([string]::IsNullOrWhiteSpace($MaybeRelativePath)) { return $null }
    if ([System.IO.Path]::IsPathRooted($MaybeRelativePath)) { return $MaybeRelativePath }
    if ([string]::IsNullOrWhiteSpace($BasePath)) { $BasePath = (Get-Location).Path }
    return Join-Path $BasePath $MaybeRelativePath
}

function Test-ManifestDefinesGate([string]$GateName, [string]$ManifestPath, [string]$BasePath) {
    if ([string]::IsNullOrWhiteSpace($GateName) -or [string]::IsNullOrWhiteSpace($ManifestPath)) { return $false }
    $resolvedManifest = Resolve-HookRelativePath $BasePath $ManifestPath
    if (-not (Test-Path -LiteralPath $resolvedManifest)) {
        Block-Gate "Gate sequence blocked: manifest was not found for gate '$GateName'." "Manifest missing: $(Format-HookPath $resolvedManifest)"
    }
    try {
        $manifest = Get-Content -LiteralPath $resolvedManifest -Raw -Encoding UTF8 | ConvertFrom-Json
    }
    catch {
        Block-Gate "Gate sequence blocked: manifest is malformed for gate '$GateName'." "Manifest malformed: $(Format-HookPath $resolvedManifest)"
    }
    if ($null -eq $manifest -or $manifest.PSObject.Properties.Name -notcontains 'stages' -or $null -eq $manifest.stages) { return $false }
    foreach ($property in $manifest.stages.PSObject.Properties) {
        if ($gateAliases.ContainsKey($property.Name)) {
            Block-Gate "Gate sequence blocked: manifest must not redefine built-in gate '$($property.Name)'." 'Manifest extension gates may add new gate ids only.'
        }
        if ($property.Name -eq $GateName) { return $true }
    }
    return $false
}

function Normalize-GateStage([string]$StageValue) {
    if ([string]::IsNullOrWhiteSpace($StageValue)) { return '' }
    return ([regex]::Replace($StageValue.Trim(), '[\s_-]+', '')).ToLowerInvariant()
}

function Test-FormalDocumentPath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    $normalized = $Path.Replace('\', '/').ToLowerInvariant()
    if ($normalized -notmatch '\.(md|markdown|txt)$') { return $false }
    if ($normalized -match '\.txt$') {
        return $normalized -match '(prd|requirements|specs)'
    }
    if ($normalized -match '(^|/)openspec/') { return $true }
    if ($normalized -match '(^|/)(prd|sdd|phase|phases|start-readiness|requirements|specs)/') { return $true }
    if ($normalized -match '(^|/)docs/(prd|sdd|phase|phases|start-readiness|requirements|specs)/') { return $true }
    if ($normalized -match '(^|/)[^/]*(prd|sdd|start-readiness)[^/]*\.(md|markdown)$') { return $true }
    if ($normalized -match '(^|/)phase[0-9a-z._-]*\.(md|markdown)$') { return $true }
    return $false
}

function Get-HookStringValues($Value, [int]$Depth = 0) {
    if ($Depth -gt 6 -or $null -eq $Value) { return @() }
    if ($Value -is [string]) { return @([string]$Value) }
    $strings = @()
    if ($Value -is [System.Collections.IEnumerable] -and -not ($Value -is [string])) {
        foreach ($item in $Value) {
            $strings += @(Get-HookStringValues $item ($Depth + 1))
        }
        return $strings
    }
    if ($Value.PSObject -and $Value.PSObject.Properties) {
        foreach ($property in $Value.PSObject.Properties) {
            $strings += @(Get-HookStringValues $property.Value ($Depth + 1))
        }
    }
    return $strings
}

function Get-HookPathLikeStrings($Value, [int]$Depth = 0) {
    if ($Depth -gt 5 -or $null -eq $Value) { return @() }
    $paths = @()
    if ($Value -is [string]) {
        if ($Value -match '\.(md|markdown|txt)(["''\s]|$)') { return @([string]$Value) }
        return @()
    }
    if ($Value -is [System.Collections.IEnumerable] -and -not ($Value -is [string])) {
        foreach ($item in $Value) {
            $paths += @(Get-HookPathLikeStrings $item ($Depth + 1))
        }
        return $paths
    }
    if ($Value.PSObject -and $Value.PSObject.Properties) {
        foreach ($property in $Value.PSObject.Properties) {
            if ($property.Name -match '(?i)(path|file)') {
                if ($property.Value -is [string]) {
                    $paths += [string]$property.Value
                    continue
                }
            }
            $paths += @(Get-HookPathLikeStrings $property.Value ($Depth + 1))
        }
    }
    return $paths
}

function Get-ApplyPatchTargetPaths([string]$PatchText) {
    if ([string]::IsNullOrWhiteSpace($PatchText)) { return @() }
    $paths = @()
    foreach ($line in ($PatchText -split "`r?`n")) {
        $match = [regex]::Match($line, '^\*\*\* (Add File|Update File|Delete File|Move to):\s*(.+?)\s*$')
        if ($match.Success) {
            $paths += $match.Groups[2].Value.Trim()
        }
    }
    return @($paths | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Select-Object -Unique)
}

function Get-ApplyPatchTargetsFromToolInput {
    $targets = @()
    foreach ($text in @(Get-HookStringValues $toolInput)) {
        if ($text -notmatch '^\s*\*\*\* Begin Patch\b' -and $text -notmatch '(?m)^\*\*\* (Add File|Update File|Delete File|Move to):') { continue }
        $targets += @(Get-ApplyPatchTargetPaths $text)
    }
    return @($targets | Where-Object { Test-FormalDocumentPath $_ } | Select-Object -Unique)
}

function ConvertTo-HookCandidatePath([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $null }
    $path = $Text.Trim()
    if ($path.StartsWith('(') -and $path.EndsWith(')')) { $path = $path.Substring(1, $path.Length - 2).Trim() }
    $path = $path.Trim('"', "'").Trim()
    $path = $path.TrimEnd(',', ';', ')', ']', '}')
    $path = $path.Replace('\', '/')
    while ($path.StartsWith('./')) { $path = $path.Substring(2) }
    return $path
}

function Get-JoinPathTargets([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $targets = @()
    foreach ($match in [regex]::Matches($Text, '(?i)Join-Path\s+(?<left>"[^"]+"|''[^'']+''|[^\s\)]+)\s+(?<right>"[^"]+"|''[^'']+''|[^\s\)]+)')) {
        $left = ConvertTo-HookCandidatePath $match.Groups['left'].Value
        $right = ConvertTo-HookCandidatePath $match.Groups['right'].Value
        if (-not [string]::IsNullOrWhiteSpace($left) -and -not [string]::IsNullOrWhiteSpace($right)) {
            $targets += (($left.TrimEnd('/')) + '/' + ($right.TrimStart('/')))
        }
    }
    return $targets
}

function Get-FormalDocumentPathsFromText([string]$Text) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $normalized = (Normalize-HookCommandText $Text).Replace('\', '/')
    $paths = @()
    $paths += @(Get-JoinPathTargets $normalized)

    foreach ($match in [regex]::Matches($normalized, '(?i)(?:^|[\s"'',;=\(\[])(?<path>(?:[A-Za-z]:)?(?:\.?/)?(?:[^"'',;\s\|\)\]]+/)+[^"'',;\s\|\)\]]+\.(?:md|markdown|txt))(?=$|[\s"'',;\)\]])')) {
        $paths += (ConvertTo-HookCandidatePath $match.Groups['path'].Value)
    }
    foreach ($match in [regex]::Matches($normalized, '(?i)(?:^|[\s"'',;=\(\[])(?<path>[^"'',;\s\|\)\]]*(?:prd|sdd|start-readiness|requirements|specs)[^"'',;\s\|\)\]]*\.(?:md|markdown|txt)|phase[0-9a-z._-]*\.(?:md|markdown))(?=$|[\s"'',;\)\]])')) {
        $paths += (ConvertTo-HookCandidatePath $match.Groups['path'].Value)
    }
    return @($paths | Where-Object { Test-FormalDocumentPath $_ } | Select-Object -Unique)
}

function Test-CommandWritesFormalDocument([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return $false }
    $normalized = (Normalize-HookCommandText $Command).Replace('\', '/')
    $writesFile = $normalized -match '(?i)(Set-Content|Add-Content|Out-File|New-Item|Copy-Item|Move-Item|>>|>\s|tee\b)' -or
        $normalized -match '(?i)(writeFileSync|writeFile|createWriteStream|\.write_text\s*\(|\.writeText\s*\(|open\s*\([^)]*["''][wa]\b)' -or
        $normalized -match '(?i)(\[(System\.)?IO\.File\]::|\b(System\.)?IO\.File::)\s*(WriteAllText|AppendAllText|WriteAllLines|AppendAllLines|WriteAllBytes)\s*\(' -or
        $normalized -match '(?i)\b(WriteAllText|AppendAllText|WriteAllLines|AppendAllLines|WriteAllBytes)\s*\(' -or
        $normalized -match '(?i)(StreamWriter|FileStream)'
    if (-not $writesFile) { return $false }
    if ($normalized -match '(?i)(^|[\s"''])(openspec/[^"'']+\.(md|markdown)|[^"'']*(prd|sdd|start-readiness|requirements|specs)[^"'']*\.(md|markdown|txt)|phase[0-9a-z._-]*\.(md|markdown))') { return $true }
    if ($normalized -match '(?i)\bopenspec\b' -and $normalized -match '(?i)\.(md|markdown)\b') { return $true }
    if ($normalized -match '(?i)\b(prd|sdd|start-readiness|requirements|specs)\b' -and $normalized -match '(?i)\.(md|markdown|txt)\b') { return $true }
    if ($normalized -match '(?i)\bphase[0-9a-z._-]*\b' -and $normalized -match '(?i)\.(md|markdown)\b') { return $true }
    return $false
}

function Get-ShellFormalDocumentWriteTargets([string]$Command) {
    if ([string]::IsNullOrWhiteSpace($Command)) { return @() }
    $normalized = (Normalize-HookCommandText $Command).Replace('\', '/')
    $targets = @(Get-FormalDocumentPathsFromText $normalized)
    foreach ($match in [regex]::Matches($normalized, '(?i)\b(?:Set-Content|Add-Content|Out-File|Tee-Object)\b[^\r\n;|]*\s-(?:LiteralPath|FilePath|Path)\s+(?<path>\([^)]+\)|"[^"]+"|''[^'']+''|[^\s;|]+)')) {
        $targetText = $match.Groups['path'].Value
        $targets += @(Get-FormalDocumentPathsFromText $targetText)
        $targets += (ConvertTo-HookCandidatePath $targetText)
    }
    foreach ($match in [regex]::Matches($normalized, '(?m)(?:^|[^-])>{1,2}\s*(?<path>"[^"]+"|''[^'']+''|[^\s;&|]+)')) {
        $targets += (ConvertTo-HookCandidatePath $match.Groups['path'].Value)
    }
    return @($targets | Where-Object { Test-FormalDocumentPath $_ } | Select-Object -Unique)
}

function Get-FormalDocumentWriteTargets {
    if ($toolName -in @('Bash', 'Shell')) {
        $command = [string]$inputProperties['command']
        $targets = @(Get-ShellFormalDocumentWriteTargets $command)
        if ($targets.Count -gt 0) { return $targets }
        if (Test-CommandWritesFormalDocument $command) {
            $script:UnresolvedFormalDocumentWrite = $true
        }
        return @()
    }
    if ($toolName -in @('apply_patch', 'ApplyPatch')) {
        return @(Get-ApplyPatchTargetsFromToolInput)
    }
    if ($toolName -notin @('Write', 'Edit', 'MultiEdit', 'NotebookEdit')) { return @() }
    $paths = @(Get-HookPathLikeStrings $toolInput | Where-Object { Test-FormalDocumentPath $_ } | Select-Object -Unique)
    return $paths
}

function Assert-RequirementsClarificationPassForDocumentWrite {
    $targets = @(Get-FormalDocumentWriteTargets)
    if ($script:UnresolvedFormalDocumentWrite) {
        Block-Gate 'Formal document write blocked before requirements clarification PASS.' 'Shell command appears to write a formal document, but the target path could not be parsed for Covered formal targets verification. Use a direct file path or a supported shell write form.'
    }
    if ($targets.Count -eq 0) { return }

    $workflowForDoc = Get-FormalDocumentWriteWorkflow $targets
    $docWorkflowId = [string](Get-WorkflowField $workflowForDoc @('workflowId', 'WorkflowId', 'workflow_id'))
    $docSnapshot = [string](Get-WorkflowField $workflowForDoc @('changeSnapshot', 'ChangeSnapshot', 'snapshot'))
    $docWorktree = [string](Get-WorkflowField $workflowForDoc @('worktree', 'Worktree', 'repo', 'Repo', 'cwd', 'Cwd'))
    $docStatePath = [string](Get-WorkflowField $workflowForDoc @('statePath', 'StatePath'))
    if ([string]::IsNullOrWhiteSpace($docWorktree) -and -not [string]::IsNullOrWhiteSpace($docStatePath)) {
        $docWorktree = Resolve-WorktreeFromStatePath $docStatePath
    }
    if ([string]::IsNullOrWhiteSpace($docWorktree)) { $docWorktree = [string]$payload.cwd }
    if ([string]::IsNullOrWhiteSpace($docWorktree)) { $docWorktree = (Get-Location).Path }

    if ([string]::IsNullOrWhiteSpace($docWorkflowId) -or [string]::IsNullOrWhiteSpace($docSnapshot)) {
        Block-Gate 'Formal document write blocked before requirements clarification PASS.' 'GateWorkflow.workflowId and GateWorkflow.changeSnapshot are required so stale requirements-clarification PASS entries cannot unlock new document writes.'
    }

    $localSkillRoot = Split-Path -Parent $PSScriptRoot
    $localGateStateScript = Join-Path $localSkillRoot 'scripts/gate-state.ps1'
    if (-not (Test-Path -LiteralPath $localGateStateScript)) {
        Block-Gate 'Formal document write blocked: requirements clarification gate cannot be checked.' "Missing gate-state.ps1: $(Format-HookPath $localGateStateScript)"
    }

    $verifyArgs = (Get-FormalGatesPowerShellFileArgs $localGateStateScript) + @(
        '-Action', 'verify',
        '-Gate', 'requirements-clarification-gate',
        '-RequireVerdict', 'PASS',
        '-RequireArtifactExists'
    )
    if (-not [string]::IsNullOrWhiteSpace($docStatePath)) { $verifyArgs += @('-StatePath', $docStatePath) }
    if (-not [string]::IsNullOrWhiteSpace($docWorkflowId)) { $verifyArgs += @('-RequireWorkflowId', $docWorkflowId) }
    if (-not [string]::IsNullOrWhiteSpace($docSnapshot)) { $verifyArgs += @('-ChangeSnapshot', $docSnapshot) }
    if ($targets.Count -gt 0) { $verifyArgs += @('-RequireCoveredTarget') + @($targets) }

    $result = Invoke-GateState $verifyArgs $docWorktree
    if ($result.ExitCode -ne 0) {
        $detail = if ([string]::IsNullOrWhiteSpace($result.Output)) { 'requirements-clarification-gate PASS is missing.' } else { $result.Output }
        Block-Gate "Formal document write blocked before requirements clarification PASS. Targets: $($targets -join ', ')." $detail
    }
}

function Enforce-FormalGatePassArtifact {
    if ($toolName -notin @('Bash', 'Shell')) { return }
    $command = [string]$inputProperties['command']
    if ([string]::IsNullOrWhiteSpace($command)) { return }
    if (-not (Test-CommandMentionsGateWorkflowScript $command)) { return }

    $actionValue = Get-CommandSwitchValue $command 'Action'
    if ($actionValue -ne 'record-stage') { return }

    $gateValue = Get-CommandSwitchValue $command 'Gate'
    $verdictValue = Get-CommandSwitchValue $command 'Verdict'
    if ($verdictValue -ne 'PASS') { return }

    $requiredFields = @(Get-FormalGatePassRequiredFields $gateValue)
    if ($requiredFields.Count -eq 0) { return }

    $artifactValue = Get-CommandSwitchValue $command 'Artifact'
    if ([string]::IsNullOrWhiteSpace($artifactValue)) {
        Block-Gate "$gateValue PASS blocked: gate-workflow record-stage must provide an artifact." 'Missing gate artifact for machine validation.'
    }

    $worktreeValue = Get-CommandSwitchValue $command 'Worktree'
    if ([string]::IsNullOrWhiteSpace($worktreeValue)) { $worktreeValue = [string]$payload.cwd }
    if ([string]::IsNullOrWhiteSpace($worktreeValue)) { $worktreeValue = (Get-Location).Path }
    $artifactPath = Resolve-HookRelativePath $worktreeValue $artifactValue
    $workflowValue = Get-CommandSwitchValue $command 'WorkflowId'
    $snapshotValue = Get-CommandSwitchValue $command 'ChangeSnapshot'
    $stageValue = Get-CommandSwitchValue $command 'Stage'
    $reviewCheck = Test-FormalGateArtifactFields $artifactPath $requiredFields $worktreeValue $workflowValue $snapshotValue $gateValue $stageValue
    if (-not $reviewCheck.Ok) {
        $missingText = ($reviewCheck.Missing -join ', ')
        if ($gateValue -eq 'requirements-clarification-gate') {
            Block-Gate "$gateValue PASS blocked: clarification artifact is incomplete: $missingText" 'Requirements clarification PASS requires user-confirmed alignment evidence, not a chat-only claim.'
        }
        else {
            Block-Gate "$gateValue PASS blocked: review artifact is incomplete: $missingText" 'Formal review PASS requires an independent zero-context reviewer artifact, not main-thread self-review.'
        }
    }
}

Enforce-FormalGatePassArtifact

function Resolve-WorktreeFromStatePath([string]$Path) {
    if ([string]::IsNullOrWhiteSpace($Path)) { return $null }
    try {
        $full = [System.IO.Path]::GetFullPath($Path)
        $fileName = [System.IO.Path]::GetFileName($full)
        $gatesDir = Split-Path -Parent $full
        $claudeDir = Split-Path -Parent $gatesDir
        if ($fileName -eq 'gate-state.json' -and [System.IO.Path]::GetFileName($gatesDir) -eq 'gates' -and [System.IO.Path]::GetFileName($claudeDir) -eq '.claude') {
            return Split-Path -Parent $claudeDir
        }
    }
    catch {
        return $null
    }
    return $null
}

Assert-RequirementsClarificationPassForDocumentWrite

$toolGateIntent = Get-ToolGateIntent
$workflow = Get-StructuredWorkflow
$hasStructuredWorkflow = $null -ne $workflow

if ($null -eq $toolGateIntent -and -not $hasStructuredWorkflow) { exit 0 }

if (-not $hasStructuredWorkflow) {
    Allow-AdvisoryGate $toolGateIntent
}

$gate = [string](Get-WorkflowField $workflow @('gate', 'Gate'))
if ([string]::IsNullOrWhiteSpace($gate)) {
    Block-Gate 'Gate sequence blocked: GateWorkflow.gate is required.' 'Missing structured gate field.'
}
$explicitWorktree = [string](Get-WorkflowField $workflow @('worktree', 'Worktree', 'repo', 'Repo', 'cwd', 'Cwd'))
$explicitStatePath = [string](Get-WorkflowField $workflow @('statePath', 'StatePath'))
if ([string]::IsNullOrWhiteSpace($explicitWorktree) -and -not [string]::IsNullOrWhiteSpace($explicitStatePath)) {
    $explicitWorktree = Resolve-WorktreeFromStatePath $explicitStatePath
}
$manifestPath = [string](Get-WorkflowField $workflow @('manifestPath', 'ManifestPath'))
$manifestBasePath = if (-not [string]::IsNullOrWhiteSpace($explicitWorktree)) { $explicitWorktree } elseif (-not [string]::IsNullOrWhiteSpace([string]$payload.cwd)) { [string]$payload.cwd } else { (Get-Location).Path }
$isManifestGate = Test-ManifestDefinesGate $gate $manifestPath $manifestBasePath
if (-not $gateAliases.ContainsKey($gate) -and -not $isManifestGate) {
    Block-Gate "Gate sequence blocked: unknown gate '$gate'." "Unknown structured gate '$gate'."
}
if ($toolName -eq 'Skill' -and -not [string]::IsNullOrWhiteSpace($toolGateIntent) -and $toolGateIntent -ne 'formal-gates' -and $toolGateIntent -ne $gate) {
    Block-Gate "Gate sequence blocked: invoked gate '$toolGateIntent' does not match GateWorkflow.gate '$gate'." 'Structured gate mismatch.'
}

$workflowId = [string](Get-WorkflowField $workflow @('workflowId', 'WorkflowId', 'workflow_id'))
$changeSnapshot = [string](Get-WorkflowField $workflow @('changeSnapshot', 'ChangeSnapshot', 'snapshot'))
$singleGateAuthorized = Test-Truthy (Get-WorkflowField $workflow @('singleGateAuthorized', 'SingleGateAuthorized', 'standalone'))
$stage = [string](Get-WorkflowField $workflow @('stage', 'Stage'))
$normalizedStage = Normalize-GateStage $stage
$mode = [string](Get-WorkflowField $workflow @('mode', 'Mode'))
$isFinal = (Test-Truthy (Get-WorkflowField $workflow @('final', 'Final'))) -or $normalizedStage -in @('whiteboxadequacy', 'finalexecution', 'final', 'release', 'seal')
$isWhiteBox = $normalizedStage -eq 'whiteboxadequacy'

$isStandaloneSingleGateRequest = $singleGateAuthorized -and -not $isFinal -and -not ($mode -match '(?i)formal|release|seal')
if ($isStandaloneSingleGateRequest) {
    if (-not [string]::IsNullOrWhiteSpace($workflowId) -or -not [string]::IsNullOrWhiteSpace($changeSnapshot)) {
        Block-Gate 'Gate sequence blocked: standalone single-gate mode cannot carry WorkflowId or ChangeSnapshot.' "Standalone $gate tried to use workflow/snapshot fields."
    }
    [pscustomobject]@{
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            additionalContext = 'USER-AUTHORIZED STANDALONE SINGLE-GATE MODE ONLY. This is advisory only, not four-stage sequencing, not release/seal approval, and not permission to enter downstream gates. Do not record or reuse this result as a workflow PASS.'
        }
    } | ConvertTo-Json -Depth 5 -Compress
    exit 0
}

$isQaExecution = $gate -eq 'qa-test-gate' -and $normalizedStage -in @('execution', 'finalexecution')
$requiresWorkflow = $isManifestGate -or $gate -in @('requirements-clarification-gate', 'complexity-gate', 'architecture-health-gate', 'code-quality-gate') -or ($gate -eq 'qa-test-gate' -and ($isWhiteBox -or $isFinal -or $isQaExecution))
if ($requiresWorkflow) {
    if ([string]::IsNullOrWhiteSpace($workflowId)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.workflowId." "Missing workflowId for $gate."
    }
    if ([string]::IsNullOrWhiteSpace($changeSnapshot)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.changeSnapshot." "Missing changeSnapshot for $gate."
    }
    if ([string]::IsNullOrWhiteSpace($explicitWorktree) -and [string]::IsNullOrWhiteSpace($explicitStatePath)) {
        Block-Gate "Gate sequence blocked: entering $gate requires GateWorkflow.worktree or GateWorkflow.statePath." "Missing explicit worktree/statePath for $gate."
    }
}

$cwd = if (-not [string]::IsNullOrWhiteSpace($explicitWorktree)) { $explicitWorktree } else { [string]$payload.cwd }
if ([string]::IsNullOrWhiteSpace($cwd)) { $cwd = (Get-Location).Path }

$skillRoot = Split-Path -Parent $PSScriptRoot
if ((Split-Path -Leaf $skillRoot) -ne 'formal-gates') {
    Block-Gate "Gate sequence blocked: hook is not running from a formal-gates package root; cannot enter $gate without machine-verifiable admission." "Invalid formal-gates hook package root: $(Format-HookPath $skillRoot)"
}

$gateStateScript = Join-Path $skillRoot 'scripts/gate-state.ps1'
if (-not (Test-Path -LiteralPath $gateStateScript)) {
    Block-Gate "Gate sequence blocked: package-local gate-state.ps1 was not found; cannot enter $gate without machine-verifiable admission." "Missing package-local gate-state.ps1 for ${gate}: $(Format-HookPath $gateStateScript)"
}

function Add-CommonGateStateArgs {
    param([string[]]$Arguments)
    $Result = @($Arguments)
    if (-not [string]::IsNullOrWhiteSpace($explicitStatePath)) { $Result += @('-StatePath', $explicitStatePath) }
    if (-not [string]::IsNullOrWhiteSpace($manifestPath)) { $Result += @('-ManifestPath', $manifestPath) }
    return $Result
}

if ($gate -eq 'qa-test-gate') {
    if ($isFinal -or $isWhiteBox) {
        $verifyArgs = (Get-FormalGatesPowerShellFileArgs $gateStateScript) + @(
            '-Action', 'verify',
            '-Gate', 'code-quality-gate',
            '-RequireVerdict', 'PASS',
            '-RequireWorkflowId', $workflowId,
            '-ChangeSnapshot', $changeSnapshot,
            '-RequireArtifactExists'
        )
        $verifyArgs = Add-CommonGateStateArgs $verifyArgs
        $result = Invoke-GateState $verifyArgs $cwd
        if ($result.ExitCode -ne 0) {
            $detail = if ([string]::IsNullOrWhiteSpace($result.Output)) { 'code-quality gate PASS is missing.' } else { $result.Output }
            Block-Gate "Gate sequence blocked before final QA/seal. $detail" $detail
        }
    }
    exit 0
}

$admissionArgs = (Get-FormalGatesPowerShellFileArgs $gateStateScript) + @(
    '-Action', 'verify-admission',
    '-Gate', $gate,
    '-WorkflowId', $workflowId,
    '-ChangeSnapshot', $changeSnapshot
)
$admissionArgs = Add-CommonGateStateArgs $admissionArgs
$result = Invoke-GateState $admissionArgs $cwd
if ($result.ExitCode -ne 0) {
    $detail = if ([string]::IsNullOrWhiteSpace($result.Output)) { "gate-state admission check failed with exit code $($result.ExitCode)." } else { $result.Output }
    Block-Gate "Gate sequence blocked before entering $gate. $detail" $detail
}

[pscustomobject]@{
    permission = 'allow'
    hookSpecificOutput = @{
        hookEventName = 'PreToolUse'
        additionalContext = "Gate sequence admission passed for $gate. $($result.Output)"
    }
} | ConvertTo-Json -Depth 5 -Compress
exit 0
