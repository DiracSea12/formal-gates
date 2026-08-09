# formal-gates

**The complete AI-assisted development workflow, from requirements clarification to seal: every step has a standardized process, gates, and records**

[English](README_EN.md) | [中文](README.md)

[![CI](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml/badge.svg)](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

formal-gates is a complete AI-assisted development workflow. It manages a single development effort from start to finish: clarify the requirement and confirm the plan first, have independent reviewers check before development, keep writing, reviewing, and testing mutually isolated during development, run the checks one by one after development, send failures back for repair, and finally freeze the whole round into a record. Every step has a standard procedure, every step leaves a record, and every conclusion is traceable.

**You don't need to understand the terms below before using it.** Once installed, just tell your AI "help me handle this change with formal-gates" and it will walk you through the whole flow step by step: it asks about your requirement, presents the plan for your confirmation, executes, and gives you a report at the end. All you do is answer its questions and confirm at the key points. The rest of this document explains how it works internally, for people who want to know.

```text
User
  │
  ▼
Main agent (your AI, orchestrator) ──► record (.gates/)
  │
  ├─► Dev agent      independent session · requirement + change only
  ├─► Review gates   independent session · full change only
  └─► Test agent     independent session · runs approved cases
```

In one line: keep the writer, the reviewer, and the tester out of each other's sight, so nobody can see anyone else's thinking; every step's conclusion is recorded, and the round finally collapses into a summary that depends on no conversation memory.

---

## Table of Contents

- [Why](#why)
- [Features](#features)
- [The Formal Flow: from requirements clarification to seal](#the-formal-flow-from-requirements-clarification-to-seal)
- [Example Artifacts](#example-artifacts)
- [Installation And Uninstall](#installation-and-uninstall)
- [Usage](#usage)
- [FAQ](#faq)
- [Current Status](#current-status)
- [Local Validation, Contributing, And License](#local-validation-contributing-and-license)

---

## Why

When the same AI writes code, checks it, and tests its own work, two problems appear:

- **It can't catch its own mistakes.** The reviewer and the writer share one memory, so the AI can fall back on "I wrote it, so I know what I meant" instead of judging the change on its own — real defects get rationalized away.
- **No records are left.** There is no independent review and nothing traceable. When a session ends, why it was done this way, what the review concluded, and why it passed are all lost.

formal-gates answers both problems with a complete, standardized workflow:

- **Before development** — clarify the requirement, confirm the plan, have independent reviewers check first, and write down "what counts as correct" up front.
- **During development** — the writer is fully isolated from reviewers and works only within the confirmed scope, with every step recorded in the VCS.
- **After development** — real testing and per-gate review run, and the conclusions are frozen into a record that depends on no conversation memory.

Review runs in mutually invisible independent sessions: a reviewer receives only the requirement text and the change's diff (zero context), sees nothing from the coding conversation, and can only judge the change itself. The CLI keeps the only record; seal (freezing the round's conclusions into a record) depends on no one's memory.

---

## Features

- **The complete flow** — requirements confirmation, slicing and route, pre-development review, test design, development, snapshot, independent review, repair, and seal. None skipped, every step recorded.
- **Requirements are confirmed before any code** — before development the main agent asks about each consequential decision one at a time, then presents the integrated plan for your confirmation; it continues only after you say yes.
- **Few decisions, made once** — first choose **lightweight** (plain vibe-coding, no formal flow) or **formal** (the complete flow); inside the formal flow, choose how complete: full or custom. After that the choices carry forward.
- **"What counts as correct" is fixed before coding** — before development the behaviors to verify are written as cases (real execution), and an independent review must pass before any code is written; later, conclusions that didn't change are kept instead of being re-tested.
- **Writing, reviewing, and testing are mutually isolated** — the three roles work in invisible independent sessions; reviewers see only the requirement and the change, so they can't be colored by the writer's memory.
- **A snapshot pins the progress** — when implementation finishes, the VCS gets an immutable marker, and every later review targets only that marker, never the drifting working tree.
- **A review gate is just a file** — every independent review gate (called a "gate" here) is one Markdown file, and the filename is its ID. Add a file to add a gate, delete one to drop it, edit the file to change what it checks.
- **Repair and inheritance** — failures are sent back for repair; PASS conclusions that the repair can't affect are inherited directly, while any gate that does re-run always sees the complete change, and testing re-runs all approved cases on the new snapshot.
- **The CLI is the only record** — during a run there is one temporary state file; when it ends only an immutable record remains. The CLI copies no project files; the VCS itself is the whole truth.
- **Native VCS** — Git, SVN, and P4 drive snapshots and diffs directly; projects without a VCS don't enter the formal flow.

---

## The Formal Flow: from requirements clarification to seal

Your change request goes through **intake** first, then enters the formal flow. **The formal flow has nine phases** (1–9); intake comes before it.

> **The entire flow runs only when you actively choose the formal flow.** With "lightweight", your AI handles the change directly in a vibe-coding manner, with no gates and no formal records. The nine-phase formal flow is heavier because every step carries gates and records; it need not be entered when those are not required.

Every phase has a standard procedure, an explicit executor, and a written record. Below, "main agent" means your AI orchestrator.

### Intake: clarify and confirm the requirement (before the formal flow)

**What happens** — every change request starts here. The main agent looks at the current state, then asks about each consequential decision one at a time (in plain language, explaining the consequences of each option), and presents the integrated plan for your explicit confirmation. After confirmation it assesses the workload and asks you to **route** this change by selecting the track it will follow: **lightweight** (a normal development flow, relatively fast and light, with no formal records or gates; suitable for small changes and documentation edits) or **formal** (the complete flow, heavier, but with gates and records at every step; choose it when full guarantees are required).
**Who does it** — the main agent directly; no independent agent is needed.
**What is recorded** — the confirmed requirement, the basis for every later judgment.
**In one line** — route first: small changes take the lightweight vibe-coding path, while changes that require gates and records take the formal one; inside the formal flow, full / custom then determines completeness (see phase 3).

### 1. Start

**What happens** — pick the repository's VCS type (Git / SVN / P4), freeze its baseline, and register the requirement and its documents. Projects without a VCS don't enter the formal flow.
**Who does it** — the main agent starts a formal run through the CLI.
**What is recorded** — the run's temporary state file and the frozen baseline.

### 2. Register the requirement

**What happens** — the requirement confirmed during intake is formally registered as the sole basis for later judgments; when the requirement changes, the test cases are adjusted accordingly (unaffected ones stay).
**Who does it** — the main agent registers it through the CLI.
**What is recorded** — the confirmed requirement and its documents are bound into the flow state.

### 3. Decide whether to split, and how complete the flow runs

**What happens** — first decide whether to **split** the work (break a large task into independent parts that are developed separately and reviewed together after merging — only very large work needs this; usually it does not). Then decide how complete the flow runs: **full** (complete testing plus every review gate — the most complete, and the heaviest) or **custom** (select your own subset — for example, only testing, or only a few gates; at least one item must be selected, and not the full set; the full set is full). If full is too heavy, custom can trim the scope.
**Who does it** — the main agent gives a recommendation; you decide.
**What is recorded** — the slicing decision and choice are written into the flow state and carried forward.
**In one line** — decide how to split first, then how strictly to review.

### 4. Before development: independent review + "what counts as correct"

**What happens** — two independent reviews run before development. The first checks whether the requirement is sound; the second checks whether the plan is minimal and ready to build. Findings are graded by severity and go to you to adjudicate one by one. Before development the behaviors to verify are also written as test cases (real execution) and independently approved, so "what counts as correct" is fixed before coding; structural checks are designed and run after development by an independent agent reading the code.
**Who does it** — one brand-new independent reviewer per review; the main agent relays findings for you to adjudicate.
**What is recorded** — adjudicated findings and the approved test cases.
**In one line** — before coding, review "whether it should be done, whether it is minimal, and what counts as correct."

### 5. Development

**What happens** — a dev agent implements within the confirmed scope in an independent session, fully isolated from every reviewer; new or changed delivery paths are added to the VCS first, the complete base-to-current change is checked before returning, and the agent self-tests before delivery.
**Who does it** — an independent dev agent session, mutually invisible to reviewers.
**What is recorded** — the implementation itself enters the VCS and becomes the content of later snapshots and diffs.

### 6. Freeze the snapshot

**What happens** — when implementation finishes, the VCS gets an immutable marker (a commit in git) recording the version this step produced.
**Who does it** — the main agent records it through the CLI.
**What is recorded** — an immutable snapshot. Later reviews target only this snapshot, never the working-tree state, so conclusions stay reproducible.

### 7. Post-development review

**What happens** — testing and every gate review run in parallel. Testing runs only the approved cases; each gate (a file under `gates/`, one gate per file) reviews the complete change in a brand-new independent session, without interference. Results are validated by the main agent and recorded as PASS / FAIL.
**Who does it** — an independent test executor plus one brand-new independent reviewer per gate.
**What is recorded** — gate verdicts and test results, validated by the main agent.
**In one line** — every gate faces the same complete change with a fresh session; nobody can coast on memory.

### 8. Repair

**What happens** — a test failure or a P0/P1 finding (a severe problem that must be fixed) sends the whole round back for repair (the round's P2/P3 minor suggestions are handled together). PASS conclusions that the repair can't affect are inherited directly; any gate that is re-run always sees the **complete change** (the full start-to-current delivery, never just the small repair delta), and testing re-runs all approved cases on the new snapshot. At most three automatic review rounds; after that every repair needs your explicit authorization.
**Who does it** — the main agent pins the pre-repair marker, dispatches a dev agent to repair, then re-snapshots and re-reviews.
**What is recorded** — the repair change and a new snapshot; the pre-repair version is kept as the basis for inheritance decisions.
**In one line** — small repairs don't re-test what didn't change; big ones go through an independent judgment.

### 9. Seal

**What happens** — once all required results pass, the round's conclusions are frozen into one immutable record and the temporary state is cleared. An outstanding result blocks seal; a runtime error needs your manual skip or a retry; a test failure or P0/P1 must be fixed first unless you explicitly authorize a skip.
**Who does it** — the main agent summarizes and confirms the current VCS marker.
**What is recorded** — one immutable record at `.gates/results/<run-id>.json` — which gates passed, each finding's location and severity, and the test results. Anyone opening the repository afterwards can see exactly what happened.
**In one line** — after seal, the round's conclusions depend on no one's memory.

### Advanced: task slicing and the overall run

By default a request is handled as a single run. Large work can be split by dependency, ownership, and risk into independent parts, each developed in its own worktree; one "overall run" keeps the original baseline, the complete requirement, and the choice, and does the integration review of the merged results. With two or more slices, a merge gate and merge testing are attached automatically. Whether and how to slice is your decision in phase 3.

### An end-to-end example

Suppose you want the AI to add a "retry login failures automatically" feature:

- **Intake** — the main agent asks a few questions (how many retries, how long between retries, whether to log), presents the plan, and you confirm and choose the formal flow.
1. **Start** — a formal run starts in the repository.
2. **Register** — the requirement is registered.
3. **Slicing and route** — the main agent recommends no split (reason recorded) and you confirm full.
4. **Before development** — independent reviewers examine the requirement and plan, findings go to you; test cases like "report an error after three failed retries" are written and approved.
5. **Development** — a dev agent implements; the change enters the VCS.
6. **Snapshot** — an immutable commit is frozen; later reviews target only it.
7. **Review** — testing runs the approved cases and each gate reviews the complete change in a fresh session.
8. **Repair** — if there's a failure or a severe finding, the round is repaired, re-snapshotted, and re-reviewed.
9. **Seal** — everything passes and the round collapses into a record.

---

## Example Artifacts

**A review gate** is a Markdown file under `gates/`; its filename is the gate ID. To add a gate, write a file and reinstall:

```markdown
# Naming Gate (example)

Review naming and readability in the change; report only identifiers that are
ambiguous or misleading. Each finding gives a repository-relative location.
P0/P1 findings block PASS; P2/P3 are advisory only.
```

Findings are graded by severity: P0/P1 are severe problems that block PASS; P2/P3 are advisory only and do not block.

This repository ships two gates by default (each a file under `gates/`):

- **Complexity gate** — checks whether the implementation is "sufficient and minimal": any new abstraction, file, type, state, or configuration must be attributable to an existing capability; otherwise it constitutes over-engineering.
- **Implementation quality gate** — reviews implementation quality across three dimensions: whether the requirement is fully covered, whether module boundaries and ownership are sound, and correctness and maintainability (including test quality, dead code, and observable performance regressions).

The **merge gate** is attached automatically in sliced runs (two or more slices) and reviews the merged change (conflict resolution, cross-slice seams) and cross-slice architecture.

**After seal**, the round's conclusions are frozen into one immutable record at `.gates/results/<run-id>.json`: which gates passed, each finding's location and severity, and the test results. Just open it when you want to look — there is no schema to memorize.

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

Installation also manages the intake rule: Claude Code uses global `~/.claude/CLAUDE.md` or project
`CLAUDE.md`, Codex uses global `~/.codex/AGENTS.md` or project `AGENTS.md`, and project Cursor
uses `.cursor/rules/formal-gates.mdc`. Cursor global installs do not create a rules file; they
retain the existing runtime and hook integration. Repeated installs collapse all known historical
and duplicate rules to one latest rule.

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
2. **Let your AI walk you through the whole flow** — after installing, tell your AI "help me handle this change with formal-gates" and it will guide you: ask about your requirement, present the plan for confirmation, execute, and report the result. You don't need to remember any commands or understand the terms in this document first.
3. **Review the outcome** — the main agent summarizes each round for you: which gates passed, which findings need action, and what the seal record looks like. You can always open `.gates/results/<run-id>.json` for the full conclusions.

The `formal-gates workflow ...` commands are run by the flow driver (your AI agent), not typed by humans.

---

## FAQ

**Do I need to understand these terms before using it?**
No. After installing, tell your AI "help me handle this change with formal-gates" and it will walk you through the whole flow; you just answer its confirmation questions. The terms in this document are for people who want to understand the internals.

**Is formal-gates just a post-development review/QA tool?**
No. Post-development review is only the seventh of the nine phases in the formal flow. The complete flow also includes the intake-phase requirements clarification and confirmation, the pre-development slicing and route, independent review, and test design, the development and snapshot phases, and the repair and seal phases that follow. This README gives every phase equal space; review is just one phase.

**Why not just open a new window to review — why do I need formal-gates?**
A new window solves one thing: independent review. formal-gates provides the complete workflow — requirements confirmation, independent review, test design, development, snapshot, independent review, repair, and seal — it runs every time, records every step; repair keeps what didn't change and any re-run gate still sees the complete change.

**How is this different from a review bot that has AI review its own work?**
A review bot typically runs in the same context that produced the code, so the AI can fill gaps from its implementation memory. formal-gates puts implementation, testing, and review into mutually invisible independent sessions; reviewers see only the requirement plus the change, with no implementation memory to lean on, so they can only judge the change itself. It also leaves a CLI record.

**What is the relationship between lightweight and formal?**
The intake phase asks you to choose **lightweight** or **formal**: lightweight is a normal development flow with no formal records; formal enters the nine phases above. Note that how complete the formal flow runs (full / custom) is not chosen at intake — it is confirmed inside the formal flow, after the slicing decision.

**Is the formal flow expensive?**
Yes. A formal run dispatches several independent reviewers and runs real tests, so it costs more time and tokens than ordinary development. If full is too heavy, choose **custom** inside the formal flow to trim the scope (omit testing, or omit some gates); small changes can go straight to lightweight vibe-coding; very large work can use the **slicing** mode, split into independent parts developed in parallel and reviewed together after merging. Review gates can also be added, removed, and customized freely — if a gate is unsuitable, delete it or change what it checks, which likewise affects the weight.

**What are the prerequisites?**
Building from source needs Go 1.22+ and one host: claude, codex, or cursor. A formal run needs a Git, SVN, or P4 repository; projects without a VCS don't enter the formal flow.

**How do I add or remove a review gate?**
Create or delete `gates/<id>.md` and reinstall. The filename is the gate ID; no config table needs changing. Delete the file to drop the gate; there is no limit.

**Is every review result final?**
No. Every independent result is candidate input. Before recording or presenting it, the main agent checks requirement fit, the normal-use boundary, and the result format; a failure or blocker is also independently reproduced through its documented public path. A result that fails any check is discarded.

**What is left after seal?**
The run's entire temporary directory is deleted; only the record `.gates/results/<run-id>.json` remains. No prompt copies, evidence graphs, or detailed state trees are kept.

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
