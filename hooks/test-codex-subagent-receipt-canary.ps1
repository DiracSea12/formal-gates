param(
    [ValidateSet('preflight', 'positive')]
    [string]$Mode = 'preflight',
    [string]$SkillPath,
    [string]$Worktree,
    [string]$OutputDir,
    [string]$CodexCommand = 'codex'
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'test-subagent-receipt-canary-common.ps1')

Invoke-HostSubagentReceiptCanary `
    -Mode $Mode `
    -SkillPath $SkillPath `
    -Worktree $Worktree `
    -OutputDir $OutputDir `
    -HostDisplayName 'Codex' `
    -Provider 'codex' `
    -DiagnosticSlug 'codex' `
    -ProjectConfigRelativePath '.codex/hooks.json' `
    -GlobalConfigRelativePath '.codex/hooks.json' `
    -MissingConfigMessage 'Codex hooks.json with SubagentStart/SubagentStop receipt hooks' `
    -ConfigReadErrorPrefix 'readable Codex hook config JSON' `
    -HookShape 'nested' `
    -EventDefinitions @(
        [pscustomobject]@{
            ConfigEventName = 'SubagentStart'
            ReceiptEventName = 'SubagentStart'
            OutputName = 'SubagentStart'
            HookMissing = 'Codex SubagentStart receipt capture hook'
            PayloadMissing = 'real Codex host-emitted SubagentStart payload artifact'
        },
        [pscustomobject]@{
            ConfigEventName = 'SubagentStop'
            ReceiptEventName = 'SubagentStop'
            OutputName = 'SubagentStop'
            HookMissing = 'Codex SubagentStop receipt capture hook'
            PayloadMissing = 'real Codex host-emitted SubagentStop payload artifact'
        }
    ) `
    -CommandPropertyName 'codexCommand' `
    -CommandValue $CodexCommand
