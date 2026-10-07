# formal-gates

**An AI-assisted development workflow, from confirmed requirements to Seal.**

[English](README_EN.md) | [中文](README.md)

formal-gates separates development, review, and testing into independent sessions. The engine
owns deterministic advancement and records raw evidence and acceptance. The main agent clarifies
requirements, verifies facts, and carries user decisions; workflow state does not depend on chat memory.

Ordinary project changes receive one non-blocking reminder that the user may request formal-gates.
Complex work receives a further recommendation after requirements and the plan are settled. Formal
intake begins only when the user explicitly requests it.

## The formal flow

1. **Clarify and confirm.** Read repository facts and resolve major choices. Write the complete
   requirement and solution into one formal document. Run the deterministic structural precheck,
   show its full content and exact revision, then wait for explicit user confirmation.
2. **Start and register.** After confirmation, choose regular/lightweight and start with a typed
   intake receipt. The first drive registers that confirmation automatically. Product review,
   technical review, development, and QA consume the same confirmed revision.
3. **Independent reviews.** Product review precedes technical review. The Operator verifies each
   raw finding and the user decides valid findings. Technical review produces Batch→Subtask,
   dependencies, DoD, verification, and recovery contracts.
4. **Plan and route.** Bind exact split topology or the no-split reason after technical review,
   then choose full/custom. Start does not predict split; the engine creates child runs.
5. **Concurrent development.** Development runs alongside blackbox design/review in a baseline
   workspace. Developers and reviewers remain isolated. Each Batch starts a new development
   session; retries and recovery within the Batch retain the original identity.
6. **Freeze the candidate.** The adapter validates the manifest, tracks paths, commits, and fixes
   a snapshot. Whitebox QA independently assesses coverage and tests. All FROZEN case sets and
   deliverables belong to the final candidate; Git squash precedes the last required validation.
7. **Parallel validation.** Freeze one complete expected set and run QA and gates up to capacity,
   refilling slots promptly. Gates read the full baseline-to-candidate diff; independent QA case
   text and execution evidence are excluded from gate input.
8. **One repair.** Collect the wave, handle errors, and adjudicate problems before creating one
   repair. Ordinary repair uses a fixed affected QA subset; requirement guarantee requires FULL
   on the final candidate before Seal. Gate P2 fixes carry PASS without another gate review;
   gate P3 remains Seal advisory.
9. **Promotion and Seal.** Verify the same candidate and its equivalence receipt, clean registered
   resources, and prove no leftovers before Complete. Seal, integration, actual canaries, and
   global cutover have distinct evidence.

lightweight records an explicit unverified result without development, snapshots, QA, or gates.
A regular run cannot downgrade to lightweight during development.

The [public workflow contract](references/formal-flow.md) owns receipt fields and invocation forms.
See the [example](references/example-run.md), [slicing](references/sliced-runs.md), and
[VCS boundaries](references/vcs-snapshots.md) for the corresponding mechanisms.

## Usage

After installation, tell your AI: “Use formal-gates for this change.” Respond to actual unresolved
choices and confirm at the required human nodes. The exhaustive public workflow surface is:

`start` / `drive` / `submit` / `show` / `status` / `next` / `diagnose` / `guard-preview`.

Start bootstraps, drive advances deterministically and recovers, and submit is the only external
event writer. The other operations are read-only. Requirement changes, route changes, reset, abort,
waiver/skip, and HUMAN legal-node jumps use availableActions and typed Ask decisions. Reconcile
UNKNOWN before another spawn or VCS side effect.

Version, definition/package digest, or installation identity mismatch rejects ordinary reads and
writes. Diagnose can inspect raw state; unsupported runs are neither migrated nor continued. Create
a current run with a new ID. Terminal summaries remain available for read-only inspection.

`workflow guard-preview --input <basis.json>` is a non-authoritative read-only calculation using the
production guard. It creates no run and authenticates neither user confirmation nor task legitimacy.
See the [input and algorithm contract](references/guard-preview.md).

## Nodes, hosts, and evidence

An ordinary internal node requires an implementation, typed registration, and inclusion in the
compiled definition. Execution and recovery share submission and acceptance. This is not an
arbitrary dynamic plugin SDK; a new control semantic still needs an explicit design boundary.

Host adapters own native launch, execution, observation, recovery, and evidence transport. The
engine and user typed Ask own business decisions. Installation and execution capabilities are
separate. Doctor, fixtures, or configuration presence do not prove a real canary. The current
candidate has not completed final stage-7 evidence, release, or global cutover. Five-host and
Git/SVN/P4 unsliced/sliced acceptance must bind the same candidate and actual installed copy;
unexecuted cells are reported as unverified or skipped.

The [development harness](references/langgraph-harness.md) documents native capability and recovery.
Its demo/fixture result or HARNESS_COMPLETE is not a formal Seal.

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

## Local validation and contributions

See [local validation](references/local-validation.md) for package, prompt, and deterministic checks.
Real canaries need per-host execution, recovery, receipt, and cleanup evidence; an unexecuted check
cannot be reported as PASS.

Gate review prose lives in `gates/*.md`; executable tasks still require registration and inclusion
in the current definition. Update CHANGELOG for behavior changes and use GitHub issues for reports.

[MIT](LICENSE) · [GitHub Releases](https://github.com/DiracSea12/formal-gates/releases)
