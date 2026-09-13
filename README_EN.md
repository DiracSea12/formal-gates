# formal-gates

An explicit, versioned workflow for requirements, independent development and review, QA, repair,
and a candidate-bound Seal. The Git, no-split regular route supports **Codex, WorkBuddy, ZCode and DeepSeek Harness**, with required native capabilities checked before launch.
Other supported hosts retain their installation and maintenance capabilities. Split and additional
regular host/VCS adapters remain later-stage work; old workflow commands and the legacy writer
have been removed. Historical requirements, raw results and Seals keep their original identity.

The current [public contract](references/engine-regular.md) describes every retained command,
typed event, dynamic binding, model/QA operation and recovery observation. See also
[configuration](references/user-configuration.md), [workflow](references/formal-flow.md) and
[an end-to-end example](references/example-run.md).

The workflow starts only when requested. A single full requirement is prechecked and shown with
its exact revision for confirmation. Product review precedes technical review and plan/route choice.
Development and isolated blackbox design/review run concurrently. Blackbox failure follows its
bounded retry or recovery Ask while independent development continues.

QA uses frozen declarations and program-owned prepare → target → collect. Post-development QA
and gates share one candidate/wave; failures do not stop independent targets. Findings are
adjudicated before one repair is created. Ordinary repairs use a fixed affected QA subset; an active
all required QA and gates passing, including valid inherited PASS, permit Seal without an extra final FULL run. Gate P2 fixes preserve valid gate PASS through
explicit carry; P3 remains advisory. A meaning-changing revision needs a confirmed impact preview.
Use guard-preview to inspect the single cost/allowance projection. Seal binds the actual promoted
candidate; it does not create another candidate.

## When it runs

One default reminder accompanies a content-change request: you may directly request the formal-gates flow.
After clarification and confirmation of a complex request, a second reminder identifies detected complex requirements.
The assistant never self-triggers. A confirmed intake can initially select lightweight for a record-only,
unverified terminal; an existing regular run cannot be downgraded to lightweight.

## Installation And Uninstall

### Install from a release (recommended)

No Go toolchain needed. Download the latest release source archive, extract it, and run the install script inside it (`install.command` on macOS/Linux, `install.bat` on Windows). The script downloads the matching native binary, canary, and SHA256 checksums, verifies them, assembles a local package, and calls the same native installer:

```bash
./install.command --host claude --scope global
# Windows: install.bat
```

### Build from source (read-only validation)

A source-built binary is a candidate. It may run package validation and the isolated canary, but
it cannot install, bootstrap, uninstall, or write workflow state. Production installation always
uses the release bootstrap above, which stages the verified binary at the fixed stable launcher
before entering the single native transaction:

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

On Windows use `bin\formal-gates.exe`. `bin\formal-gates.exe` is a Windows-only asset: do not try to run it on macOS/Linux (it fails with `exec format error`); use that platform's `bin/formal-gates` instead.

### Where things get installed

| Host | Global | Project |
| --- | --- | --- |
| Claude Code | `~/.claude/skills/formal-gates` | corresponding directory under the selected project |
| Codex | `~/.codex/skills/formal-gates` | corresponding directory under the selected project |
| Cursor | `~/.cursor/formal-gates` | corresponding directory under the selected project |
| DeepSeek Harness | `~/.dsh/skills/formal-gates` (under `$DSH_HOME`) | `.dsh/skills/formal-gates` under the selected project |
| ZCode | `~/.zcode/skills/formal-gates` | `.zcode/skills/formal-gates` under the selected project |
| WorkBuddy | `~/.codebuddy/skills/formal-gates` | `.codebuddy/skills/formal-gates` in the selected project |

Installation merges formal-gates' own host hooks into the host config: Claude Code's `~/.claude/settings.json`, Codex's `~/.codex/hooks.json`, and Cursor's `~/.cursor/hooks.json` (project-level installs write the corresponding files under the selected project); a DeepSeek Harness global install writes `$DSH_HOME/cordis.patch.yml` and mounts the bundled minimal Cordis plugin, while a project install writes only the skill and `AGENTS.md` instructions (DSH auto-loads only the home-level patch); a ZCode global install writes `~/.zcode/cli/config.json`, while a project install writes only the skill and `AGENTS.md` instructions (current ZCode ignores project-level hook configuration). Existing non-formal-gates hooks and plugin rows are preserved; when multiple hosts share a project `AGENTS.md`, the managed marker is cleaned only after the last active owner is uninstalled.

Installation also manages the intake rule: Claude Code uses global `~/.claude/CLAUDE.md` or project
`CLAUDE.md`, Codex uses global `~/.codex/AGENTS.md` or project `AGENTS.md`, project Cursor
uses `.cursor/rules/formal-gates.mdc`, DeepSeek Harness uses `$DSH_HOME/AGENTS.md` or project
`AGENTS.md`, and ZCode uses `~/.zcode/AGENTS.md` or project `AGENTS.md`. Cursor global installs do not create a rules file; they
retain the existing runtime and hook integration. The current rule is enclosed by the host
instructions block `<formal-gates:host-instructions:start>` and
`<formal-gates:host-instructions:end>`; repeated installs replace the block content and collapse
duplicate blocks to one.
When upgrading from an older release, including an old target/bin hook or marker, run the release
bootstrap with `--force`. The single transaction migrates installer-owned hooks to the fixed
launcher and converges the managed instruction block.

### Native uninstall

Uninstall uses the same host, scope, and project resolution and removes the formal-gates
runtime, installer-owned hook entries, and the complete marker-delimited rule block while
preserving content outside the block and other hooks:

```bash
$HOME/.local/bin/formal-gates uninstall --host claude --scope global
$HOME/.local/bin/formal-gates uninstall --host cursor --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host dsh --scope global
$HOME/.local/bin/formal-gates uninstall --host dsh --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host zcode --scope global
$HOME/.local/bin/formal-gates uninstall --host zcode --scope project --project <project>
# Windows: %LOCALAPPDATA%\formal-gates\bin\formal-gates.exe uninstall ...
```

Marker-based rule cleanup does not need the runtime or a rule source directory.

### Flag semantics

- `--force` — replace an existing target.
- `--skip-hooks` — install the package without touching host hooks (only when the host hook config must stay byte-for-byte unchanged).

---

## Validation and release status

A development candidate or an isolated canary is not a global release. Follow the independent
QA/Seal, integration, required revalidation and install transaction before switching the global entry.
The [local validation checklist](references/local-validation.md) lists build, full tests, race, tagged
whitebox, vet, package validation, portable canary and diff checks. Licensed under [MIT](LICENSE).
