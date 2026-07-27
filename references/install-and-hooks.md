# Install And Hooks

Use this reference only for package maintenance, installation, hooks, and
canaries. Workflow order lives only in `SKILL.md`.

## Build And Validate

The installed package uses the native Go binary. Build it before installing
from a source checkout:

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

On Windows, build and call `bin\formal-gates.exe`.

`package validate` checks the maintained package shape and file-driven prompt
catalog. `canary portable` checks package, workflow, hook-decision, and install
behavior that does not require a live AI host. Neither command is an
independent review or formal seal result.

## Native Installation

```bash
bin/formal-gates install --source <formal-gates> \
  --host <claude|codex|cursor> --scope global --force

bin/formal-gates install --source <formal-gates> \
  --host <claude|codex|cursor> --scope project --project <project> --force
```

The installer copies one runtime package, including `SKILL.md`, the CLI,
`prompts/`, `gates/`, and maintained references. It configures the selected
host's formal-gates command and subagent lifecycle hooks by default. Existing
unrelated hook entries are preserved. Use `--skip-hooks` only when the host's
hook configuration must remain byte-for-byte unchanged.

Installation replaces only an existing formal-gates target when `--force` is
present. It must not use another host's global installation as fallback.

The bootstrap files `install.command` and `install.bat` download the matching
release source and binary, verify the published checksum, assemble a local
package, and call this same native installer. They are not a second installer.

## Installed Locations

Typical global targets are:

- Claude Code: `~/.claude/skills/formal-gates`
- Codex: `~/.codex/skills/formal-gates`
- Cursor: `~/.cursor/formal-gates`

Project installs use the matching directory below the selected project. The
installer writes absolute native binary paths into hook configuration where the
host requires them.

Other Agent Skill compatible hosts may read the Markdown manually, but this
package does not claim an installer or hook integration for them.

## Hook Boundary

The native hook entrypoint is:

```bash
bin/formal-gates hook decide
```

It accepts the host's JSON payload on stdin and returns the host-compatible
allow/block decision. It is a guardrail around formal-gates commands, not proof
of code quality and not a replacement for explicit workflow state checks.

The installer also configures `SubagentStart` and `SubagentStop` for Claude
Code and Codex, and `subagentStart` and `subagentStop` for Cursor. Those hooks
send the host payload on stdin to the installed native binary:

```bash
bin/formal-gates lifecycle capture \
  --provider <claude-code|codex|cursor> --event <provider-event-name>
```

The capture command derives the project root from the normal host payload (or
the host's project-directory environment when needed), so global hooks do not
depend on their configuration directory as the working directory. `--root`
remains available as an explicit command-line override.

Lifecycle observations are retained only while at least one formal run is
active in that project. Each run owns its pending and claimed observations
under `.gates/tmp/<run-id>/lifecycle`, so normal Seal or Abort cleanup retires
them with the rest of the run. Hooks fired when no formal run is active do not
create a lifecycle journal.

After `workflow claim-dispatch` binds the host identity, inspect the derived
outcome without changing workflow state:

```bash
bin/formal-gates lifecycle verify --root <repo> --run-id <id> \
  --dispatch <dispatch-id>
```

Claude Code and Cursor require matching start and stop events. Codex reports
`UNAVAILABLE`, so the existing dispatch and identity checks remain authoritative.

A settings file or direct `hook decide` unit test proves only local decision
logic. Claim automatic blocking only after the actual target host sends a live
`PreToolUse` payload and the hook blocks the test command. A canary on one host
does not prove another host.

Codex live check:

```bash
bin/formal-gates canary codex-hook --worktree <repo>
```

Failure means that client/version did not prove closed-loop automatic
interception. Explicit `formal-gates workflow ...` commands still remain the
normal authority for the run.

## Candidate Package Checks

When testing a candidate version, install that exact source into the test
project and record both paths:

```text
source: <candidate>/formal-gates
installed: <test-project>/<host-path>/formal-gates
```

Do not test a stale global package while reporting the candidate. Package,
prompt catalog, install, and live-hook claims must all name the copy actually
used.

## Release Boundary

The CI workflow builds Windows, macOS, and Linux binaries, portable-canary
output, and SHA256 checksum files and can attach them to a GitHub Release.
Checksums show that downloaded bytes match the published CI artifact. They do
not provide signing, attestation, provenance, a registry, a marketplace, or an
`npx` distribution path.

Repository maintenance commands are in `references/local-validation.md`.
