# formal-gates

**A lightweight development and review workflow built on strict separation of the implementer, reviewer, and tester: from requirements clarification to seal, every step has a standardized process, gates, and records**

[English](README_EN.md) | [中文](README.md)

[![CI](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml/badge.svg)](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

formal-gates is a complete AI-assisted development workflow: requirements clarification, route selection, QA design, development, snapshot, QA testing, independent review, rework, and seal — every step has a standardized process, gates, and records.

The review step runs in mutually invisible independent sessions: a reviewer receives only the requirement text and the native VCS diff (zero context), so it cannot fill gaps from implementation memory. The CLI keeps the only record; at seal the whole round collapses into a summary that depends on no session memory.

```text
User
  │
  ▼
Main agent ──► CLI record (.gates/)
  │
  ├─► Dev Worker    independent session · zero-context · requirement + VCS diff
  ├─► Gate A / B    independent session · zero-context · VCS diff
  └─► QA Executor   independent session · runs approved cases
```

In one line: **take the writer, the reviewer, and the tester out of the same head, and write the conclusions into a record that depends on no anchored memory.**

---

## Table of Contents

- [Why](#why)
- [Features](#features)
- [How It Works](#how-it-works)
- [Formal Flow](#formal-flow)
- [Example Artifacts](#example-artifacts)
- [Installation And Uninstall](#installation-and-uninstall)
- [Usage](#usage)
- [FAQ](#faq)
- [Current Status](#current-status)
- [Local Validation, Contributing, And License](#local-validation-contributing-and-license)

---

## Why

An AI agent that implements code and then tests and reviews its own work has two problems:

- **A structural blind spot.** The reviewer and the implementer share one memory and one context. The AI can fill gaps with "I wrote it, so I know what I meant" instead of judging the change on its own — so real defects get rationalized away.
- **No records left.** There is no independent review and nothing traceable. When a session ends, the reasoning behind the implementation, the review conclusions, and the approvals are all lost.

formal-gates provides a complete AI-coding development workflow and puts implementation, QA, and review into **mutually invisible** independent sessions. A reviewer session receives only the requirement text and the native VCS diff — zero context: no chain of thought, opinions, chat history, or live state from the implementer. It cannot fall back on implementation memory; it faces the change like a stranger would. That is what a reviewer should be.

The CLI keeps the only record. Every semantic result — requirement confirmation, route choice, gate verdicts, QA cases, snapshots — is recorded under `.gates/` and collapses into a single summary at seal. Sealing means: freeze the round's results, write one summary, and clear the temporary state. Anyone can open the repository afterwards and see exactly what happened and why each gate passed.

In one line: **take the writer and the reviewer out of the same head, and write the conclusions into a record that depends on no session memory.**

---

## Features

- **A complete flow** — requirements clarification, route selection, QA design, development, snapshot, independent review, rework, and seal: none skipped, every step recorded.
- **Implementation and review are strictly separated** — the dev worker, review gates, and QA each run in mutually invisible independent sessions; reviewers see only the requirement plus the VCS diff. What this gives you: verdicts rest on the change alone and cannot be colored by implementation memory.
- **The CLI is the only record** — during a run there is one temporary state file, `.gates/tmp/<run-id>/state.json`; after seal only the summary `.gates/results/<run-id>.json` remains. The CLI stores no diffs and copies no project files. What this gives you: no second dataset to keep in sync, and no lost state when a session ends.
- **A gate is just a file; add or remove them freely** — each `gates/*.md` is one review gate; add a file for a new gate, delete one to drop it, with no limit. What this gives you: the gate set is exactly what the `gates/` directory contains — no registry, YAML, or weights table.
- **Gates and prompts are customizable** — gate review logic lives in `gates/*.md` and each action's prompt (requirements clarification, QA design/review/execution, the development worker, carry) lives in `prompts/actions/*.md`, all plain Markdown files inside the installed package. What this gives you: write a file to add a gate, or reword how a review step is expressed by editing its prompt — no code changes.
- **QA is designed and independently reviewed before development** — a complete candidate case set (STATIC direct checks plus LIVE real execution) is produced first, and an independent QA Review must pass before any code is written; rework keeps unchanged PASS cases. What this gives you: the behavior contract is frozen before coding, and repairs don't re-test what didn't change.
- **One route** — lightweight, full, or custom is chosen once after the requirement is aligned; later requirements and task slices keep that route. Custom picks any non-empty subset of QA and the discovered gates — leave QA out, or run QA alone. What this gives you: one decision fixes the shape of the entire formal run.
- **Native VCS** — Git, SVN, and P4 drive snapshots and diffs directly. What this gives you: the repository itself is the whole truth, with no intermediate version data. A snapshot is the native commit identity of that semantic result (a commit in git).

---

## How It Works

- **A gate is a file** — every independent review gate is a `gates/*.md` file whose filename is its ID. Want to review something? Write a file. Don't want it? Delete it. Want to change the review logic? Edit the file's prompt. QA is built in and doesn't occupy the gate directory.
- **The CLI is the only record** — all semantic conclusions live under `.gates/` and collapse into one summary at seal. It stores no diffs and no evidence; snapshots and diffs live in the repository's own VCS.
- **QA lifecycle** — before development, "what counts as correct" is written down: each behavior to verify becomes candidate cases (static checks plus real execution), and an independent QA Review must pass before code is written. Rework re-checks only failed or changed cases; unchanged PASS cases are kept.
- **One route** — after the requirement is confirmed, choose once from lightweight, full, or custom. Lightweight is the minimal flow; full adds complete QA and every gate; custom freely picks any non-empty subset of QA and the gates — QA included or not.
- **State and persistence** — a running run has exactly one temporary state file, and any interruption can be resumed from it; after seal or abort that temporary state is deleted and only an immutable summary remains.
- **Task slicing and the overall run** — large formal work is split by dependency, ownership, risk, and verification surface into independent slice runs, each developing in its own VCS worktree; one overall run keeps the original base, the complete requirement, and the route, and does the integration review of merged results.
- **Repairs and inheritance** — findings can be sent back for repair and re-run; a PASS or FAIL already recorded against an immutable snapshot stays authoritative. A bounded repair that provably cannot affect any previously passing verification may inherit all prior PASS results, including QA; otherwise an independent carry decision is used.

---

## Formal Flow

A formal run advances from requirements clarification to seal in the order below. **The gate set is dynamic**: every file under `gates/*.md` is one gate — add a file for a new gate, delete one to drop it; nothing is hardcoded into the flow.

1. **Start** — pick the repository's VCS (Git / SVN / P4), freeze the baseline, and register the requirement and its documents.
2. **Requirements clarification registration** — register the confirmed requirement and solution and record PASS; this is the sole basis for every later judgment.
3. **Bind the route** — choose once from lightweight / full / custom; custom freely combines QA with any subset of the discovered gates.
4. **Before development** — write down "what counts as correct" first: produce the full QA candidate case set (STATIC static checks + LIVE real execution), and an independent QA Review must pass before any code is written.
5. **Development** — the dev worker implements within the confirmed scope in an independent session; new delivery paths are added to the VCS first.
6. **Freeze the snapshot** — create an immutable identity with the VCS; later reviews target only this snapshot.
7. **Post-development review** — QA execution and every gate review run in parallel; gates are discovered dynamically from `gates/*.md`, and each gate reviews only the complete base-to-current diff.
8. **Rework** — P0/P1 findings or a QA FAIL send the round back for repair; rework re-checks only failed or changed cases, unchanged PASS results are kept, and a repair whose scope can be determined may inherit prior PASS results directly.
9. **Seal** — once all required results pass, collapse the round's conclusions into one immutable summary and clear the temporary state.

---

## Example Artifacts

**A review gate** is a Markdown file under `gates/`; its filename is the gate ID. To add a gate, write a file and reinstall:

```markdown
# Naming Gate (example)

Review naming and readability in the change; report only identifiers that are
ambiguous or misleading. Each finding gives a repository-relative location.
P0/P1 findings block PASS; P2 is advisory only.
```

P0/P1 are finding severities that block seal; P2 is advisory only and does not block.

**After seal**, the round's conclusions are collapsed into one immutable summary at `.gates/results/<run-id>.json`: which gates passed, each finding's severity and location, and the QA case results. Just open it when you want to look — there is no schema to memorize.

---

## Installation And Uninstall

### Install from a release (recommended)

No Go toolchain needed. Download the latest release source archive, extract it, and run the install script inside it (`install.command` on macOS/Linux, `install.bat` on Windows). The script downloads the matching native binary, canary, and SHA256 checksums, verifies them, assembles a local package, and calls the same native installer:

```bash
./install.command --host claude --scope global
# Windows: install.bat
```

### Build from source

Build the native binary in the source checkout, then pick a host and scope:

```bash
go build -o bin/formal-gates ./cmd/formal-gates

bin/formal-gates install --source . --host claude --scope global --force
bin/formal-gates install --source . --host codex --scope project --project <project> --force
bin/formal-gates install --source . --host cursor --scope project --project <project> --force

bin/formal-gates uninstall --host claude --scope global
bin/formal-gates uninstall --host codex --scope project --project <project>
```

On Windows use `bin\formal-gates.exe`.

### Where things get installed

| Host | Global | Project |
| --- | --- | --- |
| Claude Code | `~/.claude/skills/formal-gates` | corresponding directory under the selected project |
| Codex | `~/.codex/skills/formal-gates` | corresponding directory under the selected project |
| Cursor | `~/.cursor/formal-gates` | corresponding directory under the selected project |

Installation merges formal-gates' own host hooks into the host config: Claude Code's `~/.claude/settings.json`, Codex's `~/.codex/hooks.json`, and Cursor's `~/.cursor/hooks.json` (project-level installs write the corresponding files under the selected project). Existing non-formal-gates hooks are preserved.

Installation also manages the intake rule: Claude Code uses global `~/.claude/CLAUDE.md` or
project `CLAUDE.md`, Codex uses global `~/.codex/AGENTS.md` or project `AGENTS.md`, and
project Cursor uses `.cursor/rules/formal-gates.mdc`. Cursor global installs do not create a
rules file; they retain the existing runtime and hook integration. Repeated installs collapse
all known historical and duplicate rules to one latest rule.

### Native uninstall

Uninstall uses the same host, scope, and project resolution and removes the formal-gates
runtime, installer-owned hook entries, and every historical managed rule while preserving
other document content and hooks:

```bash
bin/formal-gates uninstall --host claude --scope global
bin/formal-gates uninstall --host cursor --scope project --project <project>
```

When the runtime directory is already missing, add `--source <formal-gates>` pointing to a
package containing `references/managed-rules.json`.

### Flag semantics

- `--force` — replace an existing target.
- `--skip-hooks` — install the package without touching host hooks (only when the host hook config must stay byte-for-byte unchanged).
- `uninstall --source` — optional catalog source used when uninstalling a target whose runtime is already missing.

---

## Usage

As a user you only need to do three things:

1. **Install** — set it up for one of your AI hosts (claude, codex, or cursor) as described above.
2. **Let your AI agent drive the formal flow** — the installed skill (`SKILL.md` and `references/`) is the agent's operating manual. It reads those files and runs the formal flow for your requirement: clarify the requirement, pick a route, dispatch the independent worker and reviewers, record QA, and seal. You don't need to remember any commands.
3. **Review the outcome** — the main agent summarizes each round for you: which gates passed, which findings need action, and what the seal summary looks like. You can always open `.gates/results/<run-id>.json` for the full conclusions.

The `formal-gates workflow ...` commands are run by the flow driver (your AI agent), not typed by humans.

---

## FAQ

**Why not just open a new window to review — why do I need formal-gates?**
A new window solves one thing: independent zero-context review. formal-gates provides the complete workflow — requirements clarification, confirmation, QA design, development, snapshot, independent review, rework, and seal — it runs every time, records every step, and rework re-checks only the cases that changed.

**How is this different from a review bot that has AI review its own work?**
A review bot typically runs in the same context that produced the code, so the AI can fill gaps from its implementation memory. formal-gates puts implementation, QA, and review into mutually invisible independent sessions; reviewers see only the requirement plus the VCS diff, with no implementation memory to lean on, so they can only judge the change itself. It also leaves a CLI record.

**What are the prerequisites?**
Building from source needs Go 1.22+ and one host: claude, codex, or cursor. A formal run needs a Git, SVN, or P4 repository; projects without a VCS don't enter the formal flow.

**How do I add a review gate?**
Create `gates/<id>.md` and reinstall. The filename is the gate ID; no registry, YAML, or weights table needs changing. Delete the file to drop the gate; there is no limit.

**Can a custom route skip QA?**
Yes. Custom picks any non-empty subset of QA and the discovered gates: one gate only, QA only, or QA plus some gates. The only constraint is that it selects at least one item and not the full set — pick everything with full.

**Is every review result final?**
No. Every independent result is candidate input. Before recording or presenting it, the main agent checks requirement fit, the normal-use boundary, and the result contract; a FAIL or blocker is also independently reproduced through its documented public path. A result that fails any check is discarded.

**What is left after seal?**
The run's entire temporary directory is deleted; only the summary `.gates/results/<run-id>.json` remains. No prompt copies, evidence graphs, or detailed state trees are kept.

---

## Current Status

This is a v0.1.0 prerelease; the documented workflow is authoritative in this repository. Releases are published on [GitHub Releases](https://github.com/DiracSea12/formal-gates/releases).

---

## Local Validation, Contributing, And License

**Local validation** (run from the repository root):

```bash
go test ./...
go test -race ./internal/validate ./internal/cli
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

**Contributing**:
- To add or adjust a review gate, create or edit `gates/*.md`; reinstall to apply.
- Update [CHANGELOG.md](CHANGELOG.md) for behavior changes.
- Report bugs and improvement ideas via GitHub issues.

**License**: [MIT](LICENSE).
