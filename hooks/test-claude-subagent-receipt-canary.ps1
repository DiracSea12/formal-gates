param(
    [ValidateSet('preflight', 'positive')]
    [string]$Mode = 'preflight',
    [string]$SkillPath,
    [string]$Worktree,
    [string]$OutputDir,
    [string]$ClaudeCommand = 'claude'
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'test-subagent-receipt-canary-common.ps1')

Invoke-HostSubagentReceiptCanary `
    -Mode $Mode `
    -SkillPath $SkillPath `
    -Worktree $Worktree `
    -OutputDir $OutputDir `
    -HostDisplayName 'Claude Code' `
    -Provider 'claude-code' `
    -DiagnosticSlug 'claude' `
    -ProjectConfigRelativePath '.claude/settings.json' `
    -GlobalConfigRelativePath '.claude/settings.json' `
    -MissingConfigMessage 'Claude Code settings.json with SubagentStart/SubagentStop receipt hooks' `
    -ConfigReadErrorPrefix 'readable Claude Code hook config JSON' `
    -HookShape 'nested' `
    -EventDefinitions @(
        [pscustomobject]@{
            ConfigEventName = 'SubagentStart'
            ReceiptEventName = 'SubagentStart'
            OutputName = 'SubagentStart'
            HookMissing = 'Claude Code SubagentStart receipt capture hook'
            PayloadMissing = 'real Claude Code host-emitted SubagentStart payload artifact'
        },
        [pscustomobject]@{
            ConfigEventName = 'SubagentStop'
            ReceiptEventName = 'SubagentStop'
            OutputName = 'SubagentStop'
            HookMissing = 'Claude Code SubagentStop receipt capture hook'
            PayloadMissing = 'real Claude Code host-emitted SubagentStop payload artifact'
        }
    ) `
    -CommandPropertyName 'claudeCommand' `
    -CommandValue $ClaudeCommand
