# formal-gates

> Stop AI from writing, reviewing, testing, and then declaring its own PASS.

**formal-gates** is an evidence gate system for AI development workflows. Before AI starts, requirements are aligned. After completion, independent review and machine-checkable artifacts decide whether the work can proceed or release. It does not write code for you; it judges whether the direction is right, the evidence is enough, and the result can be released.

**Built-in install targets:** Claude Code · Codex · Cursor

Per-host integration varies; actual behavior is determined by live canary.

**Current boundary:** This repository currently supports local install and local validation. It does not implement public registry, marketplace, `npx`, signing, provenance, checksum, attestation, or release-trust distribution.

---

## Table of Contents

- [One-Line Quick Start](#one-line-quick-start)
- [What Can I Do](#what-can-i-do)
- [Problems It Solves](#problems-it-solves)
- [How the Four Gates Work](#how-the-four-gates-work)
- [Core Mechanism](#core-mechanism)
- [Installation](#installation)
- [Requirements](#requirements)
- [Portable Validation](#portable-validation)
- [Package Structure](#package-structure)
- [License](#license)
- [Changelog](#changelog)

---

## One-Line Quick Start

Stop AI from writing, reviewing, testing, and then declaring its own PASS.

---

## What Can I Do

| What you want to do | Which gate to use |
|---------------------|-------------------|
| Align requirements before writing OpenSpec / PRD / SDD | **Requirements Clarification Gate** (optional) |
| After writing code, verify test coverage | **qa-test-gate** |
| Check if the change is over-engineered | **complexity-gate** |
| Check module boundaries and dependency direction | **architecture-health-gate** |
| Check code correctness, dead code, fake tests | **code-quality-gate** |
| Final validation before release/seal | Run all four gates in sequence |

Only after you tell the AI "**run four gates**", "**do formal gate review**", or "**validate before seal**" will it follow the installed skill rules. Whether the machine layer can block commands depends on the target host's hook config and a same-host live canary.

| Scenario | Gate required? |
|----------|---------------|
| Major refactors, new systems | No, unless the user asks for gate review |
| Pre-release/seal validation | Yes, when the user asks to seal or run four gates |
| Before writing OpenSpec / PRD / SDD | No; requirements clarification is optional pre-development review |
| UI tweaks, small bug fixes | No |
| Casual chat, wording adjustments | No |

---

## Problems It Solves

AI code generation has common pitfalls that this gate system specifically catches:

- **Direction drift**—Starting work without aligning on goals, scope, and acceptance criteria means even rigorous post-review is just polishing the wrong solution.
- **Over-engineering**—Constantly creating Manager / Service / Provider / various abstractions and "frameworks."
- **Fake tests**—Only asserting "field exists," "non-empty string," "log contains a line" instead of verifying actual behavior.
- **Silent scope reduction**—Shrinking the user's requested scope without declaration.
- **Self-endorsement**—Writing code and then saying "looks good" without independent validation.

---

## How the Four Gates Work

### Requirements Clarification Gate (optional pre-coding gate)

When the user asks for formal requirements clarification, first align on **goals, user value, scope, non-goals, acceptance criteria, architecture boundaries, and requirement details**. If any item is missing to the point where the document would rely on "guessing," it stops at `DRAFT_BLOCKED`—no silent default values allowed.

Requirement details include: specific business rules, boundary conditions, exception cases, data constraints, scenario details, non-functional metrics. High-level alignment alone is insufficient—discovering detail misalignment mid-development has even higher rework costs.

This is the best gate to run before AI starts coding, because direction errors have the highest rework cost. It is still optional and user-authorized, not automatic.

### Four Post-work Gates (run only when the user asks, in sequence)

1. **qa-test-gate** — Are test cases and acceptance criteria trustworthy? Does QA have real, owned evidence?
2. **complexity-gate** — Did the change bloat? Is it the minimum sufficient implementation? Over-engineered? Created unnecessary systems?
3. **architecture-health-gate** — Are module boundaries, ownership, dependency directions, state/cache lifecycles, and performance shape sound?
4. **code-quality-gate** — Correctness, edge cases, performance, dead code, fake tests, maintainability.

---

## Core Mechanism

- Pass verdicts must come from **zero-context independent review AI**—it doesn't know the main AI's conclusions or suspicions, avoiding echo chambers.
- Each gate's verdict is recorded as an **artifact**, checked by the Go validator for field completeness. Missing fields, placeholders (`<...>`/`todo`/`tbd`), or reused stale conclusions are rejected.
- Cross-workflow isolation is enforced: prerequisite gates must belong to the same `workflowId` and `changeSnapshot`; extension gates also bind prerequisites to the same manifest path and hash.
- Configured and same-host live-tested hooks can block invalid commands; when using `gate-workflow.ps1` for records, the machine layer validates evidence and rejects invalid gate records.

---

## Installation

The local install entry point is `scripts\install-formal-gates.ps1`. Do not copy only `SKILL.md`; the installer copies the runtime skill subset.

```powershell
# Install to global Claude Code
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# Install to global Claude Code and configure command hook
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# Install to global Codex
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# Install Cursor hook support for a project
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# Install to a specific Claude Code project locally
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

`-RunCanary` runs the canary after copying, verifying skill document readability and path accessibility. If it fails, don't treat this installation as usable.

Each host must be installed and verified on its own. A passing canary on one host does not mean another host enforces hooks.

### Codex Note

This package can install a Codex skill; with `-ConfigureHook`, the installer writes Codex `hooks.json`. Codex hook files must keep only the top-level `hooks` object; do not add extra top-level fields such as `version` or `description`.

Codex hooks are only an auxiliary guardrail, not a hard enforcement gate. In the current local Windows + Codex CLI 0.142.0 test, `PreToolUse` appeared active/trusted in `/hooks`, but `codex exec` and script-launched Codex command execution still used `command_execution` and did not prove closed-loop command blocking. Formal gates must explicitly run `gate-workflow.ps1` and verify artifacts; mark Codex hook blocking as proven only after a same-host live canary observes a `PreToolUse` payload and blocks the invalid command.

---

## Requirements

- **Go 1.22+**: portable package and artifact validation
- **Windows + PowerShell 5 or 7**: install, hook, and canary scripts
- **Python 3.x or 2.7**: complexity analysis scripts (optional)

macOS and Linux need only Go for package and artifact validation. Install, hook, and canary currently require Windows PowerShell.

---

## Portable Validation

> **Prerequisite**: Go 1.22+, with `go` in PATH (verify with `go version`).

A rerunnable local demo is available at [`examples/package-validation-demo.md`](examples/package-validation-demo.md). It runs Go package validation plus the portable OpenSpec canary, then writes local output to ignored `.artifacts/tmp/package-validation-demo-output.txt`.

> Demo output is a local generated artifact and is not tracked in the package. Pass `-OutputPath` to write somewhere else.

```bash
# Validate package structure
go run ./cmd/formal-gates-validate package --root .

# Validate a specific artifact
go run ./cmd/formal-gates-validate artifact \
  --root . \
  --file .claude/gates/artifacts/<artifact>.md \
  --gate complexity-gate \
  --workflow-id <workflow-id> \
  --change-snapshot <snapshot>
```

This validator performs deterministic package and artifact field checks only. It is not a workflow engine, agent runtime, or hook framework.

---

## Package Structure

```
formal-gates/
  SKILL.md                  # Entry point (for AI): routing, red lines, four-gate sequence
  references/               # Gate-specific rules (loaded on demand)
    requirements-clarification-gate.md
    qa-test-gate.md
    complexity-gate.md
    architecture-health-gate.md
    code-quality-gate.md
    install-and-hooks.md
  scripts/                  # PowerShell + Python gate scripts
  cmd/                      # Go portable validation CLI
  internal/                 # Go validation implementation
  hooks/                    # enforce-gate-sequence.ps1
  agents/                   # Independent gate review agent prompts
  examples/                 # GateWorkflow and behavior-check prompt samples
  formal-gates.manifest.json # Package index and install config
```

Humans read this README to get started; AI enters through `SKILL.md`. Gate-specific criteria are loaded from `references/` as needed.

> This package currently supports local install and local validation only; it does not provide public registry, marketplace, `npx`, signing, provenance, checksum, attestation, or release-trust distribution.

---

## License

This project is open source under the **MIT License**. See [LICENSE](LICENSE) for details.

---

## Changelog

For full version history and detailed changelog, see [CHANGELOG.md](CHANGELOG.md).
