param(
    [ValidateSet('preflight', 'positive')]
    [string]$Mode = 'preflight',
    [string]$SkillPath,
    [string]$Worktree,
    [string]$OutputDir,
    [string]$CursorCommand = 'cursor'
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'test-subagent-receipt-canary-common.ps1')

Invoke-HostSubagentReceiptCanary `
    -Mode $Mode `
    -SkillPath $SkillPath `
    -Worktree $Worktree `
    -OutputDir $OutputDir `
    -HostDisplayName 'Cursor' `
    -Provider 'cursor' `
    -DiagnosticSlug 'cursor' `
    -ProjectConfigRelativePath '.cursor/hooks.json' `
    -GlobalConfigRelativePath '.cursor/hooks.json' `
    -MissingConfigMessage 'Cursor hooks.json with subagentStart/subagentStop receipt hooks' `
    -ConfigReadErrorPrefix 'readable Cursor hook config JSON' `
    -HookShape 'flat' `
    -EventDefinitions @(
        [pscustomobject]@{
            ConfigEventName = 'subagentStart'
            ReceiptEventName = 'SubagentStart'
            OutputName = 'subagentStart'
            HookMissing = 'Cursor subagentStart receipt capture hook'
            PayloadMissing = 'real Cursor host-emitted subagentStart payload artifact'
        },
        [pscustomobject]@{
            ConfigEventName = 'subagentStop'
            ReceiptEventName = 'SubagentStop'
            OutputName = 'subagentStop'
            HookMissing = 'Cursor subagentStop receipt capture hook'
            PayloadMissing = 'real Cursor host-emitted subagentStop payload artifact'
        }
    ) `
    -CommandPropertyName 'cursorCommand' `
    -CommandValue $CursorCommand
