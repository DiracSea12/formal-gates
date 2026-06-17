# Tests for validate-dispatch-prompt.ps1

$ErrorActionPreference = 'Stop'

$scriptPath = Join-Path $PSScriptRoot '../scripts/validate-dispatch-prompt.ps1'
$configPath = Join-Path $PSScriptRoot '../hooks/pollution-patterns.json'

function Test-ValidationPass {
    param([string]$Name, [string]$PromptText)

    $result = & pwsh -NoProfile -File $scriptPath -PromptText $PromptText -ConfigPath $configPath 2>&1
    $exitCode = $LASTEXITCODE

    if ($exitCode -eq 0) {
        Write-Host "✓ PASS: $Name"
        return $true
    }
    else {
        Write-Host "✗ FAIL: $Name (expected exit 0, got $exitCode)"
        Write-Host "  Output: $result"
        return $false
    }
}

function Test-ValidationFail {
    param([string]$Name, [string]$PromptText, [string]$ExpectedLabel)

    $result = & pwsh -NoProfile -File $scriptPath -PromptText $PromptText -ConfigPath $configPath 2>&1
    $exitCode = $LASTEXITCODE

    if ($exitCode -ne 0) {
        try {
            $violations = $result | ConvertFrom-Json
            $first = if ($violations -is [array]) { $violations[0] } else { $violations }
            if ($first.Label -like "*$ExpectedLabel*") {
                Write-Host "✓ PASS: $Name (detected $($first.Label))"
                return $true
            }
            else {
                Write-Host "✗ FAIL: $Name (expected label containing '$ExpectedLabel', got '$($first.Label)')"
                return $false
            }
        }
        catch {
            Write-Host "✗ FAIL: $Name (JSON parse error: $_)"
            Write-Host "  Output: $result"
            return $false
        }
    }
    else {
        Write-Host "✗ FAIL: $Name (expected failure, got exit 0)"
        return $false
    }
}

Write-Host "Running validate-dispatch-prompt.ps1 tests..."
Write-Host ""

$passed = 0
$failed = 0

# Test 1: Clean prompt
if (Test-ValidationPass "Clean prompt" "Please review this implementation and provide feedback") {
    $passed++
} else {
    $failed++
}

# Test 2: Empty prompt
if (Test-ValidationPass "Empty prompt" "") {
    $passed++
} else {
    $failed++
}

# Test 3: English pattern - previous findings
if (Test-ValidationFail "English pattern: previous findings" "The previous findings show that there is an issue" "previous review") {
    $passed++
} else {
    $failed++
}

# Test 4: English pattern - known issue
if (Test-ValidationFail "English pattern: known issue" "This addresses the known issue with validation" "known issue") {
    $passed++
} else {
    $failed++
}

# Test 5: English pattern - just fixed
if (Test-ValidationFail "English pattern: just fixed" "I just fixed the bug in the code" "fix reference") {
    $passed++
} else {
    $failed++
}

# Test 6: English pattern - expected outcome
if (Test-ValidationFail "English pattern: expected outcome" "The expected answer is PASS" "expected outcome") {
    $passed++
} else {
    $failed++
}

# Test 7: English pattern - focus direction
if (Test-ValidationFail "English pattern: focus on" "Please focus on the performance issues" "focus direction") {
    $passed++
} else {
    $failed++
}

# Test 8: Chinese term - 刚修了
if (Test-ValidationFail "Chinese term: 刚修了" "请审查这段代码，刚修了一个bug" "fix reference") {
    $passed++
} else {
    $failed++
}

# Test 9: Chinese term - 重点复查
if (Test-ValidationFail "Chinese term: 重点复查" "请重点复查这部分逻辑" "focus direction") {
    $passed++
} else {
    $failed++
}

# Test 10: Chinese term - 已知问题
if (Test-ValidationFail "Chinese term: 已知问题" "这个改动解决了已知问题" "known issue") {
    $passed++
} else {
    $failed++
}

# Test 11: Multiple violations
$multiPrompt = "The previous findings show issues. Please focus on validating the fix."
$result = & pwsh -NoProfile -File $scriptPath -PromptText $multiPrompt -ConfigPath $configPath 2>&1
$exitCode = $LASTEXITCODE
if ($exitCode -ne 0) {
    try {
        $violations = $result | ConvertFrom-Json
        if ($violations -is [array] -and $violations.Count -ge 2) {
            Write-Host "✓ PASS: Multiple violations (detected $($violations.Count) violations)"
            $passed++
        }
        else {
            Write-Host "✗ FAIL: Multiple violations (expected 2+, got $($violations.Count))"
            $failed++
        }
    }
    catch {
        Write-Host "✗ FAIL: Multiple violations (parse error: $_)"
        $failed++
    }
}
else {
    Write-Host "✗ FAIL: Multiple violations (expected failure, got exit 0)"
    $failed++
}

# Test 12: Legitimate use of pattern words
if (Test-ValidationPass "Legitimate 'expected'" "The expected behavior is to validate input correctly") {
    $passed++
} else {
    $failed++
}

# Test 13: Special characters in prompt
if (Test-ValidationPass "Special characters" "Check `$variable and [options] for correctness") {
    $passed++
} else {
    $failed++
}

# Test 14: Large prompt
$largePrompt = "Review this implementation. " * 1000
if (Test-ValidationPass "Large prompt" $largePrompt) {
    $passed++
} else {
    $failed++
}

# Test 15: Mixed English and Chinese
if (Test-ValidationPass "Mixed language" "Please review this 实现 and provide feedback on the 逻辑") {
    $passed++
} else {
    $failed++
}

Write-Host ""
Write-Host "Test Results: $passed passed, $failed failed"

if ($failed -gt 0) {
    exit 1
}
