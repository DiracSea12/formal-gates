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
- [Contributing](#contributing)
- [License](#license)
- [Changelog](#changelog)

---

## One-Line Quick Start

From the repo root, run the following to try the read-only validation:

```powershell
# Validate package structure (requires Go 1.22+, go in PATH)
go run ./cmd/formal-gates-validate package --root .

# Install formal-gates to current Claude Code project (project-local)
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath . -Force -RunCanary

# Tell AI: "run four gates" or "validate before seal"
```

---

## What Can I Do

| What you want to do | Which gate to use |
|---------------------|-------------------|
| Before writing OpenSpec / PRD / SDD | **Requirements Clarification Gate** (with skill installed) |
| After writing code, verify test coverage | **qa-test-gate** |
| Check if the change is over-engineered | **complexity-gate** |
| Check module boundaries and dependency direction | **architecture-health-gate** |
| Check code correctness, dead code, fake tests | **code-quality-gate** |
| Final validation before release/seal | Run all four gates in sequence |

Tell the AI "**run four gates**", "**do formal gate review**", or "**validate before seal**" and it will follow the installed skill rules. Machine-level enforcement still depends on the target host's hook config and passing live canary.

| Scenario | Gate required? |
|----------|---------------|
| Major refactors, new systems, pre-release/seal | Yes |
| Before writing OpenSpec / PRD / SDD | Yes (with skill installed) |
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

### Requirements Clarification Gate (only pre-coding gate)

Before writing OpenSpec / PRD / SDD or other specification documents, first align on **goals, user value, scope, non-goals, acceptance criteria, architecture boundaries, and requirement details**. If any item is missing to the point where the document would rely on "guessing," it stops at `DRAFT_BLOCKED`—no silent default values allowed.

Requirement details include: specific business rules, boundary conditions, exception cases, data constraints, scenario details, non-functional metrics. High-level alignment alone is insufficient—discovering detail misalignment mid-development has even higher rework costs.

This is the **only gate that should run before AI starts coding**—since direction errors have the highest rework cost, this gate is most critical.

### Four Post-work Gates (review after completion, in sequence—cannot proceed to next until previous passes)

1. **qa-test-gate** — Are test cases and acceptance criteria trustworthy? Does QA have real, owned evidence?
2. **complexity-gate** — Did the change bloat? Over-engineered? Created unnecessary systems?
3. **architecture-health-gate** — Are module boundaries, ownership, dependency directions, state/cache lifecycles sound?
4. **code-quality-gate** — Correctness, edge cases, dead code, fake tests, maintainability.

---

## Core Mechanism

- Pass verdicts must come from **zero-context independent review AI**—it doesn't know the main AI's conclusions or suspicions, avoiding echo chambers.
- Each gate's verdict is recorded as an **artifact**, checked by the Go validator for field completeness. Missing fields, placeholders (`<...>`/`todo`/`tbd`), or reused stale conclusions are rejected.
- With configured and same-host live-tested hooks, or using `gate-workflow.ps1` for recording, the main AI cannot "self-stamp approval"—the machine layer blocks it.

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

This package can install a Codex skill, but `-ConfigureHook` on Codex is a no-op that prompts to consult `references/install-and-hooks.md`. Codex hook config must be done manually, and hooks only work as an auxiliary guardrail—not a hard enforcement gate. Formal gates must explicitly run `gate-workflow.ps1` and verify artifacts.

---

## Requirements

- **Go 1.22+**: portable package and artifact validation
- **Windows + PowerShell 5 or 7**: install, hook, and canary scripts
- **Python 3.x or 2.7**: complexity analysis scripts (optional)

macOS and Linux need only Go for package and artifact validation. Install, hook, and canary currently require Windows PowerShell.

---

## Portable Validation

> **Prerequisite**: Go 1.22+, with `go` in PATH (verify with `go version`).

A rerunnable local demo is available at [`examples/package-validation-demo.md`](examples/package-validation-demo.md). It runs Go package validation plus the portable OpenSpec canary, then writes local output to `examples/package-validation-demo-output.txt`.

> `examples/package-validation-demo-output.txt` is the bundled sample output—running the demo on your machine will overwrite it with your local results.

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

## Contributing

Issues and Pull Requests are welcome. Before submitting, please ensure:

- Non-trivial source / script / test / config changes pass all gate reviews (four gates in sequence)
- Documentation-only fixes pass relevant checks (format, links, spelling) only
- New or changed behavior is reflected in the corresponding `references/` gate rule documents
- Go code passes `go build ./...` and `go test ./...`
- PowerShell scripts pass validation under `-RunCanary`

How to contribute:

1. Fork the repository, create a feature branch
2. Validate your changes in your own project or test environment (run `gate-workflow.ps1`)
3. Submit a Pull Request describing the change rationale and validation results

---

## License

This project is open source under the **MIT License**. See [LICENSE](LICENSE) for details.

---

## Changelog

For full version history and detailed changelog, see [CHANGELOG.md](CHANGELOG.md).
