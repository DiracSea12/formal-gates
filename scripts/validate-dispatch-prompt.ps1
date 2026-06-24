# Shared dispatch prompt pollution validation
# Used by both hook (pre-dispatch) and artifact validation (post-review)

param(
    [Parameter(Mandatory=$true)]
    [AllowEmptyString()]
    [string]$PromptText,

    [string]$ConfigPath
)

$ErrorActionPreference = 'Stop'

# Default config path if not provided
if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
    $ConfigPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'hooks/pollution-patterns.json'
}

if (-not (Test-Path -LiteralPath $ConfigPath)) {
    Write-Error "Pollution patterns config not found: $ConfigPath"
    exit 1
}

try {
    $config = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
}
catch {
    Write-Error "Cannot parse pollution patterns config: $_"
    exit 1
}

$violations = @()

$patternChecks = @()
if ($config.english -and $config.english.patternGroups) {
    foreach ($group in $config.english.patternGroups) {
        foreach ($pattern in @($group.patterns)) {
            $patternChecks += [pscustomobject]@{
                Pattern = $pattern
                Label = $group.label
                Description = $group.description
            }
        }
    }
}
$termChecks = @()
if ($config.chinese -and $config.chinese.termGroups) {
    foreach ($group in $config.chinese.termGroups) {
        foreach ($term in @($group.terms)) {
            $termChecks += [pscustomobject]@{
                Term = $term
                Label = $group.label
                Description = $group.description
            }
        }
    }
}
# Check prohibited patterns (regex)
foreach ($check in $patternChecks) {
    $pattern = $check.Pattern
    if ($PromptText -match $pattern) {
        $matched = [regex]::Match($PromptText, $pattern).Value
        $violations += [pscustomobject]@{
            Type = 'pattern'
            Matched = $matched
            Label = $check.label
            Description = $check.description
        }
    }
}

# Check prohibited terms (exact match)
foreach ($check in $termChecks) {
    $term = $check.Term
    if ($PromptText.Contains($term)) {
        $violations += [pscustomobject]@{
            Type = 'term'
            Matched = $term
            Label = $check.label
            Description = $check.description
        }
    }
}

if ($violations.Count -gt 0) {
    # Output violations as JSON for programmatic consumption
    $violations | ConvertTo-Json -Depth 3 -Compress
    exit 1
}

exit 0
