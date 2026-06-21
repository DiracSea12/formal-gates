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

function Allow-Hook([string]$Reason) {
    [pscustomobject]@{
        permission = 'allow'
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            permissionDecision = 'allow'
            permissionDecisionReason = $Reason
        }
    } | ConvertTo-Json -Depth 5 -Compress
    exit 0
}

$inputJson = [Console]::In.ReadToEnd()
if ([string]::IsNullOrWhiteSpace($inputJson)) { Allow-Hook 'No hook payload provided.' }

try {
    $payload = $inputJson | ConvertFrom-Json
}
catch {
    Allow-Hook 'Hook payload is not JSON.'
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

# Unified gate dispatch entry check
# For Agent/Task tools: require explicit formal_gate_dispatch field
# For Skill tools: check if it's a gate-related skill
$formalGateDispatch = $null
if ($toolName -in @('Agent', 'Task')) {
    $formalGateDispatch = [string]$inputProperties['formal_gate_dispatch']
    if ([string]::IsNullOrWhiteSpace($formalGateDispatch)) {
        Allow-Hook 'Not a formal gate dispatch.'
    }
}
elseif ($toolName -eq 'Skill') {
    $skillName = [string]$inputProperties['skill']
    if ($gateAliases.ContainsKey($skillName)) {
        $formalGateDispatch = $gateAliases[$skillName]
    }
    elseif ($routerSkillNames -contains $skillName) {
        $formalGateDispatch = $skillName
    }
    else {
        Allow-Hook 'Not a formal gate skill.'
    }
}
else {
    # Shell/write tools may still need command prechecks below.
}

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

function Test-JsonObjectIntentAfterKey([string]$Text, [string[]]$Keys) {
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    foreach ($key in $Keys) {
        if ($Text -match '(?i)(^|[\s,;])' + [regex]::Escape($key) + '\s*[:=]\s*\{') {
            return $true
        }
    }
    return $false
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
    $jsonText = Find-JsonObjectAfterKey $rawIntentText @('GateWorkflow', 'Workflow')
    if (-not [string]::IsNullOrWhiteSpace($jsonText)) { return Convert-ToWorkflowObject $jsonText }
    if (Test-JsonObjectIntentAfterKey $rawIntentText @('GateWorkflow', 'Workflow')) {
        Block-Gate 'Gate sequence blocked: structured GateWorkflow JSON is malformed.' 'Malformed GateWorkflow JSON.'
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

function Get-CommandSwitchValues([string]$Command, [string]$Name) {
    $values = @()
    if ([string]::IsNullOrWhiteSpace($Command)) { return @() }
    $tokens = @(Split-CommandArguments (Normalize-HookCommandText $Command))
    for ($index = 0; $index -lt $tokens.Count; ++$index) {
        if ($tokens[$index] -ieq "-$Name") {
            if ($index -lt ($tokens.Count - 1)) { $values += $tokens[$index + 1] }
            continue
        }
        if ($tokens[$index] -match ('(?i)^-' + [regex]::Escape($Name) + ':(.+)$')) {
            $values += $Matches[1]
        }
    }
    return @($values)
}

function Get-CommandSwitchValue([string]$Command, [string]$Name) {
    $values = @(Get-CommandSwitchValues $Command $Name)
    if ($values.Count -gt 0) { return [string]$values[-1] }
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
    return (Normalize-HookCommandText $Command) -match '(?i)(^|[\\/''"])gate-workflow\.ps1([''"\s]|$)'
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

function Assert-RequirementsClarificationPassForDocumentWrite {
    # Intentionally no-op by design. Do not add automatic document-write blocking here
    # unless the user has been asked and explicitly permits changing that design.
    return
}

function Enforce-FormalGatePassArtifact {
    if ($toolName -notin @('Bash', 'Shell')) { return }
    $command = [string]$inputProperties['command']
    if ([string]::IsNullOrWhiteSpace($command)) { return }
    if (-not (Test-CommandMentionsGateWorkflowScript $command)) { return }

    $actionValues = @(Get-CommandSwitchValues $command 'Action')
    if ($actionValues -notcontains 'record-stage') { return }

    $gateValue = Get-CommandSwitchValue $command 'Gate'
    $verdictValues = @(Get-CommandSwitchValues $command 'Verdict')
    if ($verdictValues -notcontains 'PASS') { return }

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

function Enforce-AgentDispatchPromptValidation {
    if ($toolName -notin @('Agent', 'Task')) { return }

    $validGates = @('requirements-clarification-gate', 'qa-test-gate', 'complexity-gate', 'architecture-health-gate', 'code-quality-gate', 'cold-water-review')
    if ($formalGateDispatch -notin $validGates) { return }

    $dispatchPrompt = [string]$inputProperties['prompt']
    if ([string]::IsNullOrWhiteSpace($dispatchPrompt)) { return }

    # Call shared validation script
    $validatorScript = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts/validate-dispatch-prompt.ps1'
    if (-not (Test-Path -LiteralPath $validatorScript)) {
        Block-Gate "Gate dispatch validation failed: validator script not found at $validatorScript" "Missing validator script"
    }

    $configPath = Join-Path $PSScriptRoot 'pollution-patterns.json'
    if (-not (Test-Path -LiteralPath $configPath)) {
        Block-Gate "Gate dispatch validation failed: pollution patterns config not found at $configPath" "Missing config"
    }

    try {
        $ps = Get-FormalGatesPowerShellExe
        $result = & $ps -NoProfile -File $validatorScript -PromptText $dispatchPrompt -ConfigPath $configPath 2>&1
        $exitCode = $LASTEXITCODE
    }
    catch {
        Block-Gate "Gate dispatch validation failed: validator script error: $_" "Validator error"
    }

    if ($exitCode -ne 0) {
        $resultText = $result | Out-String
        if ($resultText -match '^\s*[\[{]') {
            try {
                $violations = $resultText | ConvertFrom-Json
                $first = if ($violations -is [array]) { $violations[0] } else { $violations }
                if ($null -ne $first) {
                    $matched = $first.Matched
                    $label = $first.Label
                    $description = $first.Description
                    Block-Gate "Gate dispatch blocked: prompt contains prohibited anchoring $($first.Type) '$matched' ($label). $description. Zero-context formal review prompts must not include references to previous reviews, fixes, expected outcomes, focus direction, or any context that anchors the reviewer's judgment." "Anchoring detected: $matched"
                }
            }
            catch {
                Block-Gate "Gate dispatch blocked: prompt validation failed with JSON parse error. Raw output: $resultText" "Validation JSON parse failed"
            }
        }
        else {
            Block-Gate "Gate dispatch blocked: prompt validation failed. $resultText" "Validation failed"
        }
    }
}

Enforce-AgentDispatchPromptValidation

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

$workflow = Get-StructuredWorkflow
$hasStructuredWorkflow = $null -ne $workflow

if ($null -eq $formalGateDispatch -and -not $hasStructuredWorkflow) { Allow-Hook 'No structured formal gate workflow found.' }

if (-not $hasStructuredWorkflow) {
    Allow-AdvisoryGate $formalGateDispatch
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
if ($toolName -eq 'Skill' -and -not [string]::IsNullOrWhiteSpace($formalGateDispatch) -and $formalGateDispatch -ne 'formal-gates' -and $formalGateDispatch -ne $gate) {
    Block-Gate "Gate sequence blocked: invoked gate '$formalGateDispatch' does not match GateWorkflow.gate '$gate'." 'Structured gate mismatch.'
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
        permission = 'allow'
        hookSpecificOutput = @{
            hookEventName = 'PreToolUse'
            permissionDecision = 'allow'
            permissionDecisionReason = 'Standalone single-gate mode allowed.'
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
    Allow-Hook 'QA gate sequence precheck passed.'
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
