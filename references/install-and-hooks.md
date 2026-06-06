# Install And Hooks

This file covers installation, hooks, canaries, manifests, and host integration. Do not load it for ordinary four-gate work.

## Package Structure

Candidate package directories must keep this shape:

```text
formal-gates/
  SKILL.md
  agents/
  examples/
  hooks/
  references/
  scripts/
```

`scripts/` and `hooks/` must be copied with the package. Do not depend on stale loose skills or global original paths.

## Maintained Source

The maintained source is the GitHub checkout, or the candidate package source directory explicitly passed through `-SourcePath`. Global `.claude` and optional `.codex` directories are installation snapshots, not the maintained source.

When changing the package, edit the maintained source first, verify it, then commit and push if needed before installing it into a target host. Do not treat a global same-name skill as current just because it exists. In A/B or old-version comparison runs, leave unnamed hosts untouched.

## PowerShell Entry

Scripts support Windows PowerShell 5 and PowerShell 7. Examples use `<ps>` as the launch prefix:

- PowerShell 5: `powershell -NoProfile -ExecutionPolicy Bypass`
- PowerShell 7: `pwsh -NoProfile`

Bundled scripts continue under the current PowerShell. PowerShell 7 is not required.

## Install To Claude

Use the bundled installer to copy the full directory. Do not cherry-pick files:

```powershell
# Install or replace the global Claude skill
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# Install and create/update the Claude settings.json command hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# Install to a project-local Claude skill path
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

The installer copies the full `formal-gates` directory, refuses to replace targets outside `skills\formal-gates`, and removes `__pycache__`. `-RunCanary` runs the portable canary after copying; a failed canary means the install is not usable. `-ConfigureHook` writes Claude `settings.json` or Cursor `hooks.json`; it creates a `.bak` backup and only merges or updates formal-gates hooks.

Claude manual install targets:

- Global Claude: `%USERPROFILE%\.claude\skills\formal-gates`
- Project-local Claude: `<project>\.claude\skills\formal-gates`

Do not copy only `SKILL.md`. Without `scripts/gate-state.ps1` or `hooks/enforce-gate-sequence.ps1`, formal workflows lose machine checks.

For copy-then-verify runs, copy the candidate into the project-local `.claude/skills/formal-gates` first. Do not accidentally test a global stale package.

## Install To Codex

Use the bundled installer to copy the full directory. Do not cherry-pick files:

```powershell
# Install to the global Codex skill path
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# Install to both Claude and Codex project-local skill paths
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Both -Scope Project -ProjectPath <project> -Force -RunCanary
```

Codex manual install targets:

- Global Codex: `%USERPROFILE%\.codex\skills\formal-gates`
- Project-local Codex: `<project>\.codex\skills\formal-gates`

When Claude and Codex are both installed, the two mirrors must be byte-identical or the run record must state their version/hash difference. Never use one host's install or canary result as proof for another host.

## Cursor Hook Integration

Cursor uses its Hooks surface, not a Claude-style skill loader. The installer can write hook config:

```powershell
# Project-level Cursor hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# Global Cursor hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Cursor -Scope Global -Force -RunCanary -ConfigureHook
```

The installer copies the package to `.cursor\formal-gates` and writes a `preToolUse` command hook into `.cursor\hooks.json` or `%USERPROFILE%\.cursor\hooks.json`.

## Candidate Package A/B Testing

Do not test a candidate package by using a global same-name skill. First copy the user-provided candidate path to the test workspace's `.claude/skills/formal-gates`, preferably through `install-formal-gates.ps1`. Only copy to `.codex/skills/formal-gates` when explicitly testing Codex compatibility.

Each A/B record must state:

```text
Skill source path: <candidate>\formal-gates
Copied skill path: <test-workspace>\.claude\skills\formal-gates
```

If evidence only says `formal-gates`, it is impossible to tell whether the candidate or global original was tested, so the evidence is invalid.

## Hook Rules

The hook entry point is:

```text
hooks/enforce-gate-sequence.ps1
```

Candidate hooks must resolve only the same package's:

```text
scripts/gate-state.ps1
```

Do not fall back to:

- `%USERPROFILE%\.codex\skills\formal-gates\scripts\gate-state.ps1`
- `%USERPROFILE%\.claude\skills\formal-gates\scripts\gate-state.ps1`
- old loose skill directories
- any global original path

In A/B testing, a hook that silently uses the global script tests the old package while pretending to test the candidate.

Claude Code loads hooks from Claude settings. Global example:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "powershell -NoProfile -ExecutionPolicy Bypass -Command \"& (Join-Path $env:USERPROFILE '.claude\\skills\\formal-gates\\hooks\\enforce-gate-sequence.ps1')\""
          }
        ]
      }
    ]
  }
}
```

For project-local installs, change the `command` path to `<project>\\.claude\\skills\\formal-gates\\hooks\\enforce-gate-sequence.ps1`. Whether a client or hosted gateway actually executes command hooks must be proven with a live canary on the target machine. Inspecting settings files is not proof.

Codex may load hooks from `~/.codex/hooks.json`, `[hooks]` in `~/.codex/config.toml`, or project-local `.codex` config. Do not route through Codex plugin packaging just to load hooks.

Minimal Codex `config.toml` shape:

```toml
[features]
hooks = true

[[hooks.PreToolUse]]
matcher = "*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "powershell -NoProfile -ExecutionPolicy Bypass -Command \"& (Join-Path $env:USERPROFILE '.codex\\skills\\formal-gates\\hooks\\enforce-gate-sequence.ps1')\""
timeout = 30
```

On Windows, let PowerShell expand `$env:USERPROFILE` inside the Codex `command` string. Do not rely on the outer host to expand `%USERPROFILE%`.

Unmanaged Codex command hooks must be reviewed and trusted through `/hooks` before ordinary interactive use. `--dangerously-bypass-hook-trust` is only for automated canaries after the hook source has been reviewed; do not use it as a daily bypass.

Cursor loads hooks from `~/.cursor/hooks.json` or project `.cursor/hooks.json`. Minimal project-level shape:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "powershell -NoProfile -ExecutionPolicy Bypass -File \".cursor/formal-gates/hooks/enforce-gate-sequence.ps1\"",
        "timeout": 30,
        "failClosed": true
      }
    ]
  }
}
```

The Cursor hook script reads JSON from stdin and blocks by returning `permission: "deny"` on stdout or exit code 2. The example leaves `matcher` out so the script can filter only formal-gates commands internally and avoid Cursor-version matcher-shape drift. Inspecting `hooks.json` still is not a live canary.

## GateWorkflow And Gate State

Formal gates require structured `GateWorkflow`. Minimum fields:

- `workflowId`
- `changeSnapshot`
- `worktree` or `statePath`
- `gate`
- `stage` for the QA gate or manifest extension gates; built-in complexity/architecture/code-quality gates can omit `stage`.

`scripts/gate-state.ps1` records and validates gate state. `scripts/gate-workflow.ps1` is the common wrapper and calls the same-directory `gate-state.ps1`.

`GateWorkflow` is JSON. Escape Windows backslashes as double backslashes, or use forward slashes:

```text
GateWorkflow={"gate":"complexity-gate","workflowId":"wf","changeSnapshot":"snap","worktree":"C:\\Users\\me\\repo"}
GateWorkflow={"gate":"complexity-gate","workflowId":"wf","changeSnapshot":"snap","worktree":"C:/Users/me/repo"}
```

Do not write `"worktree":"C:\Users\me\repo"`; `\U` is not valid JSON escaping and the hook reports `Malformed GateWorkflow JSON`.

## Manifest Extension Gates

Custom extension gates must provide `manifestPath` and define dependencies under `stages.<gate-id>` in the manifest. Example: `GateWorkflow={"gate":"security-gate","workflowId":"...","changeSnapshot":"...","worktree":"...","manifestPath":"gate-manifest.json"}`.

Manifests can define only extension gates. They cannot define or override `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, or `code-quality-gate`. The four built-in gate IDs and order are fixed.

Manifest extension gates bind to the manifest hash. Their prerequisite gates must also be recorded with the same `-ManifestPath`; old records or records without `manifestHash` do not satisfy extension-gate admission. After adding a manifest to an existing flow, re-record prerequisites under that manifest instead of reusing old built-in PASS records.

## Formal Gate Artifacts And Recording Commands

Formal post-development gate artifact fields, `gate_route`, recording commands, PowerShell prefixes, and final-QA wrapper commands live in `references/post-development-artifacts.md`.

This file keeps only installation, hooks, manifests, canaries, multi-host paths, and runtime validation rules. Do not turn install documentation into an artifact-template repository.

If Claude and Codex are both installed but their skill mirrors or hooks are not the same package version, do not claim "the same formal-gates package is active." Record the actual paths and hashes.

## Git / SVN / Non-VCS Snapshot

Git projects can use a commit range snapshot:

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action snapshot -Worktree <repo> -BaseRef <base> -HeadRef HEAD -IncludeWorkingTree
```

SVN or non-git projects can use the file-tree hash fallback without `BaseRef`:

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action snapshot -Worktree <project> -Vcs auto
```

This excludes `.git`, `.svn`, `.hg`, `node_modules`, `__pycache__`, and local gate state, then produces `svn-files.<hash>` or `files.<hash>`. The hash binds artifacts to a file snapshot, but it does not replace human confirmation of SVN branch/version provenance.

## Portable Canary

Run this script to check that the package can be copied into a temporary project and execute the basic gate flow:

```powershell
<ps> -File <formal-gates>\scripts\test-portable-openspec-canary.ps1 -SkillPath <formal-gates>
```

It validates package portability, script execution, and basic gate-state recording. It is not final QA for a real project.

## Codex Hook Canary

Run this only when validating Codex compatibility:

```powershell
<ps> -File <formal-gates>\hooks\test-codex-hook-client.ps1 -Worktree <repo> -KeepTemp
```

The PASS condition is strict: real `codex exec` must write at least one `PreToolUse` hook payload, the bundled `hooks/enforce-gate-sequence.ps1` must block a formal PASS command that lacks an artifact, and the canary marker file must not be created. `FAIL` or `TIMED_OUT` means this Codex client/version does not have closed-loop hook interception, or the formal-gates hook is not wired correctly.

Do not use script-direct tests, Claude hook success, or a target command failing inside `codex exec` as proof of Codex hook closure. The Codex hook is an optional guardrail; formal non-interactive workflows still rely on `gate-workflow.ps1` / `gate-state.ps1` admission and artifact recording.

## Quick Structure Validation

After candidate package changes, run:

```powershell
$validator = "<path-to-skill-creator>\scripts\quick_validate.py"
if (-not (Test-Path $validator)) { throw "skill-creator quick_validate.py not found; pass the local validator path." }
py -3 -X utf8 $validator <formal-gates>
```

Then read the skill entrypoint with PowerShell:

```powershell
Get-Content -Encoding UTF8 <formal-gates>\SKILL.md
```

If encoding is corrupted, BOM handling is wrong, or frontmatter lacks `name` / `description`, fix package structure before discussing gate quality.
