# formal-gates

> AI Code Quality Gates: 1 pre-work gate for alignment + 4 post-work gates for quality. Before AI starts coding, align on requirements. After completion, independent review AI validates through each checkpoint before release.

This is an Agent Skill package. The core skill documents can be read by any Agent Skill compatible runtime; the bundled installer and hook paths currently declare support for Claude Code, Codex, and Cursor. Any host claiming hooks block bad gate flow must prove it with a live canary on that host.

Chinese README: [README.md](README.md)

It doesn't write code for you—instead, it judges "is the AI's direction correct? Can the completed code/documentation be released?"

## Problems It Solves

AI code generation has common pitfalls that this gate system specifically catches:

- **Direction drift**—Starting work without aligning on goals, scope, and acceptance criteria means even rigorous post-review is just polishing the wrong solution.
- **Over-engineering**—Constantly creating Manager / Service / Provider / various abstractions and "frameworks."
- **Fake tests**—Only asserting "field exists," "non-empty string," "log contains a line" instead of verifying actual behavior.
- **Silent scope reduction**—Shrinking the user's requested scope without declaration.
- **Self-endorsement**—Writing code and then saying "looks good" without independent validation.

## Pre-work Gate: Requirements Clarification Gate (The only pre-coding gate, most cost-effective)

Before writing OpenSpec / PRD / SDD or other specification documents, first align on **goals, user value, scope, non-goals, acceptance criteria, architecture boundaries, and requirement details**. If any item is missing to the point where the document would rely on "guessing," it stops at `DRAFT_BLOCKED`—no silent default values allowed.

Requirement details include: specific business rules, boundary conditions, exception cases, data constraints, scenario details, non-functional metrics. High-level alignment alone is insufficient—discovering detail misalignment mid-development has even higher rework costs.

This is the **only gate actively triggered by the skill** (automatically runs when writing specification documents, without user request), and the only gate that intercepts **before** AI starts coding—since direction errors have the highest rework cost, this gate is most critical.

## Four Post-work Gates (Review after AI completion, in sequence—cannot proceed to next gate until previous passes)

1. **qa-test-gate (Test Gate)**—Are test cases and acceptance criteria trustworthy? Does QA have real, owned evidence?
2. **complexity-gate (Complexity Gate)**—Did the change bloat? Over-engineered? Created unnecessary systems?
3. **architecture-health-gate (Architecture Gate)**—Are module boundaries, ownership, dependency directions, state/cache lifecycles sound?
4. **code-quality-gate (Code Quality Gate)**—Correctness, edge cases, dead code, fake tests, overfitting, maintainability.

## Core Mechanism: Preventing AI Self-endorsement

- Pass verdicts must come from **zero-context independent review AI**—it doesn't know the main AI's conclusions or suspicions, avoiding echo chambers.
- Each gate's verdict is recorded as an **artifact**, with **machine-side mandatory validation** via PowerShell scripts: missing fields, placeholders (`<...>`/`todo`/`tbd`), or reused stale conclusions are rejected by validators. Configured and live-tested hooks can block them at command time.
- With configured hooks or using `gate-workflow.ps1` for recording, the main AI cannot "self-stamp approval"—the machine layer blocks it. Without hooks, explicit script validation is still required.

## When to Use / Not Use

| ✅ Use | ❌ Don't Use |
|------|--------|
| Major refactors, new systems, full module development | UI position tweaks, small bug fixes |
| Final validation before release/seal | Casual chat, code browsing, wording adjustments |
| Requirements clarification before writing spec docs | Single-file typos |

For routine small changes, it stays silent and doesn't interfere.

## Requirements

- Windows + PowerShell 5 or 7
- Version control: Git / SVN / No VCS (file hash snapshots) all supported
- Complexity scripts require Python 3.x or 2.7

## Installation

Use the included scripts to copy the entire directory (don't cherry-pick just SKILL.md):

```powershell
# Install to global Claude skill
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# Install to global Claude skill and configure/update command hook
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# Install to global Codex skill
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# Install Cursor hook support for a project
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# Or install to a specific Claude project locally
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

`-RunCanary` runs portability self-checks after copying, verifying skill document readability, core rule completeness, and path accessibility. If it fails, don't treat this installation as usable.

Claude Code, Codex, and Cursor have different hook/config surfaces, so each host must be installed and verified on its own. A passing canary on one host does not mean another host enforces hooks. Other compatible runtimes can read the core skill documents, but they need their own install path, hook integration, and canary proof. Hook and script integration details live in `references/install-and-hooks.md`.

## Getting Started

Tell the AI "run four gates," "do formal gate review," or "validate before seal" to trigger the process. When writing specification documents, it will automatically run the requirements clarification gate first. For routine small changes, no action needed.

## Skill Behavior Checks

`examples/skill-behavior-prompts.json` contains read-only prompts for checking whether the skill changes agent behavior in the intended way. They cover requirements clarification before OpenSpec work, blocking direct main-agent implementation, rejecting self-issued PASS, preventing focused evidence from becoming Final QA PASS, avoiding over-triggering on routine chat or tiny edits. They also include negative cases for unavailable subagents, dirty snapshots, manifest extension gates, and inactive hooks.

Use these prompts with automated skill review tools or a human reviewer. They are behavior checks for the skill itself, not formal release gates and not a replacement for `scripts\test-portable-openspec-canary.ps1` portability validation.

## Package Structure

```
formal-gates/
  SKILL.md                  # Entry point (for AI): routing, red lines, four-gate sequence, GateWorkflow essentials
  references/               # Gate-specific rules, loaded on demand
    requirements-clarification-gate.md   # Requirements clarification gate
    requirements-clarification-artifacts.md # Requirements clarification recording fields
    qa-test-gate.md                      # Test gate
    complexity-gate.md                   # Complexity gate (includes Complexity Contract, budgets)
    architecture-health-gate.md          # Architecture gate
    code-quality-gate.md                 # Code quality gate
    post-development-artifacts.md        # Formal post-development recording fields and commands
    install-and-hooks.md                 # Installation, hooks, canaries, manifests, and multi-host integration
  scripts/                  # PowerShell + Python gate scripts
  hooks/                    # enforce-gate-sequence.ps1 (machine-side sequence and field enforcement)
  agents/                   # Independent gate agent prompts and optional host config
  examples/                 # GateWorkflow, behavior-check prompts, and other samples
  formal-gates.manifest.json # Package index, host support caveats, install and verification commands
```

Humans read this README to get started; AI enters through `SKILL.md`. Gate-specific criteria are loaded from `references/` as needed.
