# Install And Hooks

This file covers installation, hooks, canaries, manifests, and host integration. Do not load it for ordinary four-gate work.

## Package Structure

Source candidate package directories must keep this shape:

```text
formal-gates/
  SKILL.md
  go.mod
  cmd/
  internal/
  agents/
  examples/
  hooks/
  references/
```

Native host skill installs copy the verifiable installable package subset: `SKILL.md`, `README.md`, `README_EN.md`, `formal-gates.manifest.json`, `go.mod`, `.github/workflows/portable-validation.yml`, `bin/`, `cmd/`, `internal/`, `agents/`, `examples/`, `references/`, and `hooks/pollution-patterns.json`. They do not copy PS1, Python, shell, JavaScript, batch, or command-script runtime files. Dispatch prompt pollution validation is implemented by the Go CLI and reads `hooks/pollution-patterns.json`.

Installed packages must include `bin/formal-gates.exe` on Windows and `bin/formal-gates` on Linux/macOS. Source checkouts may use `go run ./cmd/formal-gates` for development tests, but installed hook and validation paths must call the packaged binary.

## Native Hook and Receipt Paths

Maintenance local self-check commands live in [`local-validation.md`](local-validation.md).

To make one portable hook decision from a host-provided JSON payload:

```bash
bin/formal-gates hook decide < payload.json
```

To use the native receipt proof foundation:

```bash
bin/formal-gates receipt register --provider codex --worktree <repo> --artifact <review.md> --gate <gate-id> --workflow-id <workflow-id> --stage <stage>
bin/formal-gates receipt capture --provider codex --event SubagentStart --worktree <repo> < start-payload.json
bin/formal-gates receipt capture --provider codex --event SubagentStop --worktree <repo> < stop-payload.json
bin/formal-gates receipt finalize --provider codex --worktree <repo> --artifact <review.md> --gate <gate-id> --workflow-id <workflow-id> --stage <stage>
bin/formal-gates receipt validate --worktree <repo> --receipt <receipt.json> --artifact <review.md> --gate <gate-id> --workflow-id <workflow-id> --change-snapshot <snapshot> --stage <stage>
bin/formal-gates receipt preflight --host codex --worktree <repo>
```

Use `bin/formal-gates.exe` on Windows. Source checkout development tests may use `go run ./cmd/formal-gates`, but installed hook and validation paths must use `bin/formal-gates(.exe)`.

The `hook` command returns compact JSON with `decision`, `reason`, and host-compatible allow/deny fields. Top-level `decision` uses `block` / `approve` for Claude Code and Cursor compatibility; `permission` and `permissionDecision` remain `deny` / `allow` for hosts or checks that use those fields. It rejects legacy PowerShell formal-gates commands and blocks missing-artifact PASS recording for native `formal-gates ... record...` commands. It only decides whether a command-like payload should be allowed or denied; it does not prove that any host actually invokes hooks.

To install the runtime subset without PowerShell:

```bash
bin/formal-gates install --source <formal-gates> --host claude --scope global --force
bin/formal-gates install --source <formal-gates> --host codex --scope project --project <project> --force --configure-hooks
bin/formal-gates install --source <formal-gates> --host cursor --scope project --project <project> --force --configure-hooks
```

The native installer requires `bin/formal-gates(.exe)` under `--source`; build it first with `go build -o bin/formal-gates ./cmd/formal-gates` or the Windows `.exe` equivalent. `--configure-hooks` writes native hook commands: `hook decide` for PreToolUse/preToolUse and `receipt capture` for subagent lifecycle events. It preserves non-formal-gates hook entries and replaces only formal-gates hook commands.

This Go path is the portable CLI entrypoint for Windows, macOS, and Linux. It now includes basic gate-state recording, admission checks for the fixed post-development order, deterministic state display, native install, a native workflow foundation for file-hash/git snapshots, record-stage, admission wrappers, final verification aggregation, FinalExecution recording from a supplied artifact, dry-run-first cleanup, a native receipt foundation for dispatch registration, lifecycle event capture, receipt finalization, receipt validation, diagnostic preflight, and a native Codex hook live canary. It is not a persistent report system, cache, receipt-sensitive full workflow, or release-trust mechanism.

## Maintained Source

The maintained source is the GitHub checkout, or the candidate package source directory explicitly passed through `--source`. Global `.claude` and optional `.codex` directories are installation snapshots, not the maintained source.

When changing the package, edit the maintained source first, verify it, then commit and push if needed before installing it into a target host. Do not treat a global same-name skill as current just because it exists. In A/B or old-version comparison runs, leave unnamed hosts untouched.

## Source Host Canaries

Core package checks, workflow recording, hook decisions, install, receipt checks, prompt checks, complexity checks, and the Codex hook live canary use the native binary. `formal-gates receipt preflight` reads Claude/Codex/Cursor hook JSON and reports missing lifecycle proof, but it is diagnostic only. Hook blocking is claimed only after a same-host live canary captures a real payload and blocks an invalid command.

## Host Capability Levels

Use these four capability categories when describing host support:

- readable skill support;
- install guidance;
- hook configuration;
- hook blocking proven by live canary.

Current package scope:

| Host | Readable skill support | Install guidance | Hook configuration | Hook blocking proven by live canary |
|---|---|---|---|---|
| Claude Code | supported | bundled installer | bundled command-hook guidance | project-local proven on Claude Code 2.1.193; global Windows path still has known UX risk |
| Codex | supported | bundled installer | bundled command-hook guidance | not proven on Codex CLI 0.142.0 |
| Cursor | project-rule or markdown guidance | bundled installer | bundled `hooks.json` guidance | project-local proven on Cursor headless 2026.06.26-7079533 |
| Gemini | manual markdown adaptation | not bundled | not bundled | not claimed |
| OpenCode | manual markdown adaptation | not bundled | not bundled | not claimed |
| Windsurf | manual markdown adaptation | not bundled | not bundled | not claimed |

This is a compact capability statement, not a broad host-path registry. Do not add per-version path tables unless a specific host integration is implemented and canaried.

Generated hook commands use slash paths inside quoted command strings. This is intentional for Windows: the same hook command may be interpreted by a shell that treats backslashes as escapes. Do not rewrite generated commands back to raw Windows backslash paths.

## Release Evidence

The portable validation workflow builds native binaries on Windows, macOS arm64, macOS amd64, and Linux, writes platform-specific `portable-canary-*.json`, writes matching `SHA256SUMS-*.txt`, and uploads those files as CI artifacts. It is configured to upload the same files to a GitHub Release when a release is published:

- `formal-gates-windows-amd64.exe`, `portable-canary-windows-amd64.json`, `SHA256SUMS-windows-amd64.txt`
- `formal-gates-macos-arm64`, `portable-canary-macos-arm64.json`, `SHA256SUMS-macos-arm64.txt`
- `formal-gates-macos-amd64`, `portable-canary-macos-amd64.json`, `SHA256SUMS-macos-amd64.txt`
- `formal-gates-linux-amd64`, `portable-canary-linux-amd64.json`, `SHA256SUMS-linux-amd64.txt`

Here, an artifact is a saved build or evidence file from CI. The binary is what the user can run. The canary JSON is the package's self-check result for that platform. The checksum file lets a user verify that the downloaded binary and canary file match what CI produced. `PASS` means the package-local checks in that canary passed for that platform; it does not prove a third-party signature, provenance, attestation, or host hook interception.

## Maintenance Self-Checks

The repository-local self-check chain lives in [`local-validation.md`](local-validation.md). Keep this install-and-hooks file focused on package shape, install flows, host hooks, and release evidence.

`formal-gates behavior evaluate` reads behavior cases and optional model answers:

```bash
bin/formal-gates behavior evaluate --root . --cases examples/skill-behavior-prompts.json
bin/formal-gates behavior evaluate --root . --cases examples/skill-behavior-prompts.json --answers examples/skill-behavior-answers.json
```

Without answers, cases are reported as `PENDING`. With answers, the harness checks explicit `must_include` and `must_avoid` markers when present, or derives a small set of key terms from the expected behavior. This is a repeatable local harness for behavior evidence; it is not a replacement for a human or model judge on nuanced semantic quality.

`examples/skill-behavior-prompts.json` is the portable marker fixture used by `package validate` and `canary portable`. `examples/skill-behavior-answers.json` is the checked answer fixture and must make all 24 portable cases pass. Root `test-prompts.json` is a broader 20-case prompt set for manual or agent-level evaluation; it is intentionally not used as the fixed package self-check fixture.

## Install To Claude

Use the native installer to copy the installable skill subset. Do not cherry-pick files:

```bash
bin/formal-gates install --source <formal-gates> --host claude --scope global --force
bin/formal-gates install --source <formal-gates> --host claude --scope global --force --configure-hooks
bin/formal-gates install --source <formal-gates> --host claude --scope project --project <project> --force
```

The installer copies the installable skill subset, refuses to replace targets outside `skills\formal-gates`, and removes script-runtime files from the installed target. Native `--configure-hooks` writes native commands and only merges or updates formal-gates hooks.

Claude manual install targets:

- Global Claude: `%USERPROFILE%\.claude\skills\formal-gates`
- Project-local Claude: `<project>\.claude\skills\formal-gates`

Do not copy only `SKILL.md`. A native install also needs the packaged binary, Go command sources used by the CLI, references, agents, examples, and `hooks/pollution-patterns.json`.

For copy-then-verify runs, copy the candidate into the project-local `.claude/skills/formal-gates` first. Do not accidentally test a global stale package.

### Cross-platform bootstrap

Use the double-click bootstrap entry points when you want a single entry point that downloads the release source snapshot and the matching native binary, verifies checksums, assembles a local package copy, and optionally runs host configuration:

```bash
open install.command
```

```powershell
install.bat
```

Bootstrap entry points are platform shims. They do not replace `formal-gates install`; they prepare a local package copy and then hand off to the same CLI installer.

The bootstrap scripts only request release assets that this repository publishes: macOS arm64, macOS amd64, Windows amd64, and Linux amd64. Other OS/architecture combinations stop before download instead of guessing an unpublished asset name.

## Install To Codex

Use the native installer to copy the installable skill subset. Do not cherry-pick files:

```bash
bin/formal-gates install --source <formal-gates> --host codex --scope global --force
bin/formal-gates install --source <formal-gates> --host both --scope project --project <project> --force
```

Codex manual install targets:

- Global Codex: `%USERPROFILE%\.codex\skills\formal-gates`
- Project-local Codex: `<project>\.codex\skills\formal-gates`

When Claude and Codex are both installed, the two mirrors must be byte-identical or the run record must state their version/hash difference. Never use one host's install or canary result as proof for another host.

### Codex on macOS

Use the native `cursor-agent`/`claude` style paths for macOS and do not follow Windows examples that use `%USERPROFILE%` or `.exe` filenames. For example, a project-local macOS hook command should point at the installed native binary under the project tree:

```bash
"<project>/.codex/skills/formal-gates/bin/formal-gates" hook decide
```

## Cursor Hook Integration

Cursor uses its Hooks surface, not a Claude-style skill loader. The installer can write hook config:

```bash
bin/formal-gates install --source <formal-gates> --host cursor --scope project --project <project> --force --configure-hooks
bin/formal-gates install --source <formal-gates> --host cursor --scope global --force --configure-hooks
```

The installer copies the installable skill subset to `.cursor\formal-gates` and writes a `preToolUse` command hook into `.cursor\hooks.json` or `%USERPROFILE%\.cursor\hooks.json`.

### Cursor on macOS

Use the native installed binary path under the project directory:

```bash
"<project>/.cursor/formal-gates/bin/formal-gates" hook decide
```

The hook payload remains JSON over stdin; the command must return `permission: "deny"` or exit code `2` when it blocks a PASS without artifact.

## Candidate Package A/B Testing

Do not test a candidate package by using a global same-name skill. First copy the user-provided candidate path to the test workspace's `.claude/skills/formal-gates`, preferably through `formal-gates install`. Only copy to `.codex/skills/formal-gates` when explicitly testing Codex compatibility.

Each A/B record must state:

```text
Skill source path: <candidate>\formal-gates
Copied skill path: <test-workspace>\.claude\skills\formal-gates
```

If evidence only says `formal-gates`, it is impossible to tell whether the candidate or global original was tested, so the evidence is invalid.

## Hook Rules

The native hook entry point is the installed binary:

```text
bin/formal-gates hook decide
```

Candidate hooks must resolve only the same package's native binary and `hooks/pollution-patterns.json` config. They must not fall back to a global original package.

Do not fall back to:

- `%USERPROFILE%\.codex\skills\formal-gates\bin\formal-gates.exe` from another package copy
- `%USERPROFILE%\.claude\skills\formal-gates\bin\formal-gates.exe` from another package copy
- old loose skill directories
- any global original path

In A/B testing, a hook that silently uses the global binary tests the old package while pretending to test the candidate.

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
            "command": "\"%USERPROFILE%\\.claude\\skills\\formal-gates\\bin\\formal-gates.exe\" hook decide"
          }
        ]
      }
    ]
  }
}
```

For project-local installs, change the `command` path to `<project>\\.claude\\skills\\formal-gates\\bin\\formal-gates.exe hook decide` on Windows, or the matching `bin/formal-gates hook decide` path on Linux/macOS. Whether a client or hosted gateway actually executes command hooks must be proven with a live canary on the target machine. Inspecting settings files is not proof.

Codex may load hooks from `~/.codex/hooks.json`, `[hooks]` in `~/.codex/config.toml`, or project-local `.codex` config. Do not route through Codex plugin packaging just to load hooks.

Minimal Codex `config.toml` shape:

```toml
[features]
hooks = true

[[hooks.PreToolUse]]
matcher = "*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "\"%USERPROFILE%\\.codex\\skills\\formal-gates\\bin\\formal-gates.exe\" hook decide"
timeout = 30
```

On Windows, use the absolute installed binary path when the host does not expand `%USERPROFILE%`.

Unmanaged Codex command hooks must be reviewed and trusted through `/hooks` before ordinary interactive use. `--dangerously-bypass-hook-trust` is only for automated canaries after the hook source has been reviewed; do not use it as a daily bypass.

Cursor loads hooks from `~/.cursor/hooks.json` or project `.cursor/hooks.json`. Minimal project-level shape:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "\".cursor/formal-gates/bin/formal-gates.exe\" hook decide",
        "timeout": 30,
        "failClosed": true
      }
    ]
  }
}
```

The Cursor hook command reads JSON from stdin and blocks by returning `permission: "deny"` on stdout or exit code 2. The example leaves `matcher` out so the command can filter only formal-gates commands internally and avoid Cursor-version matcher-shape drift. Inspecting `hooks.json` still is not a live canary.

## GateWorkflow And Gate State

Formal gates require structured `GateWorkflow`. Minimum fields:

- `workflowId`
- `changeSnapshot`
- `worktree` or `statePath`
- `gate`
- `stage` for the QA gate or manifest extension gates; built-in complexity/architecture/code-quality gates can omit `stage`.

`formal-gates gate record`, `formal-gates gate verify-admission`, and `formal-gates gate show` provide the native gate-state foundation for `.claude/gates/gate-state.json`. `formal-gates workflow snapshot`, `formal-gates workflow record-stage`, `formal-gates workflow verify-admission`, `formal-gates workflow final-verification`, `formal-gates workflow cleanup`, and `formal-gates workflow show` provide the current native workflow foundation. `formal-gates install` copies the verifiable installable package subset and can write native host hook config. `formal-gates receipt register`, `receipt capture`, `receipt finalize`, `receipt validate`, and `receipt preflight` provide the current native receipt foundation. Installed `SubagentStart` and `SubagentStop` hooks call `formal-gates receipt capture` directly.

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

Formal post-development gate artifact fields, `gate_route`, recording commands, and final-QA native commands live in `references/post-development-artifacts.md`.

This file keeps only installation, hooks, manifests, canaries, multi-host paths, and runtime validation rules. Do not turn install documentation into an artifact-template repository.

If Claude and Codex are both installed but their skill mirrors or hooks are not the same package version, do not claim "the same formal-gates package is active." Record the actual paths and hashes.

## Git / SVN / Non-VCS Snapshot

Git projects can use a commit range snapshot:

```bash
bin/formal-gates workflow snapshot --worktree <repo> --vcs git --base-ref <base> --head-ref HEAD --include-working-tree
```

SVN or non-git projects can use the file-tree hash fallback without `BaseRef`:

```bash
bin/formal-gates workflow snapshot --worktree <project> --vcs file-hash
```

This excludes `.git`, `.svn`, `.hg`, `node_modules`, `__pycache__`, and local gate state, then produces `svn-files.<hash>` or `files.<hash>`. The hash binds artifacts to a file snapshot, but it does not replace human confirmation of SVN branch/version provenance.

## Portable Canary

Keep maintainer self-check commands in [`local-validation.md`](local-validation.md). Keep this file focused on install, hooks, host integration, and release evidence.

## Phase 2 Release Trust

Phase 1 ships CI artifacts and platform-specific SHA256 checksum files. It is configured to upload release evidence when a GitHub Release is published, but that still does not provide artifact attestation, npm provenance, signing, or equivalent third-party release-trust evidence. Those controls are Phase 2 work. Do not claim release provenance, signed artifacts, npm package provenance, or equivalent trust guarantees from the Phase 1 validator or CI matrix.

## Codex Hook Canary

Run this only when validating Codex compatibility:

```bash
bin/formal-gates canary codex-hook --worktree <repo> --keep-temp
```

Use `bin/formal-gates.exe` on Windows. If `codex` resolves to a PowerShell wrapper, pass a script-free Codex executable path with `--codex-command <codex.exe-or-codex.cmd>`.

The PASS condition is strict: real `codex exec` must write at least one `PreToolUse` hook payload, the native hook must block a formal PASS command that lacks an artifact, and the canary marker file must not be created. `FAIL` or `TIMED_OUT` means this Codex client/version does not have closed-loop hook interception, or the formal-gates hook is not wired correctly. Treat that as a host auto-blocking failure, not a package-local validation failure. Failed summaries include `failureReason` and `nextAction` so the result says whether the likely blocker was timeout, missing hook payload, marker creation, or missing deny output.

Do not use direct hook-decision tests, Claude hook success, or a target command failing inside `codex exec` as proof of Codex hook closure. The Codex hook is an optional guardrail; formal non-interactive workflows still rely on native `formal-gates workflow` / `formal-gates gate` admission and artifact recording.

## Quick Structure Check

See [`local-validation.md`](local-validation.md) for package validation and portable-canary checks.
