# formal-gates

> Stop AI from writing, reviewing, testing, and then declaring its own PASS.

**formal-gates** is an evidence gate system for AI development workflows. Before AI starts, requirements are aligned. After completion, independent review and machine-checkable artifacts decide whether the work can proceed or release. It does not write code for you; it judges whether the direction is right, the evidence is enough, and the result can be released.

**Built-in install targets:** Claude Code · Codex · Cursor

Automatic blocking varies by tool. When a tool cannot block commands automatically, use the explicit validation commands.

**Current boundary:** This repository currently supports local install and local validation. CI is configured to upload binaries, `portable canary` output, and SHA256 checksums when a GitHub Release is published. It does not implement public registry, marketplace, `npx`, signing, provenance, attestation, or third-party-verifiable release-trust distribution.

---

## Table of Contents

- [One-Line Quick Start](#one-line-quick-start)
- [What Does It Block?](#what-does-it-block)
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

## What Does It Block?

The common AI failure is simple: the AI writes the code, says it tested it, and then declares its own work as "PASS."

**formal-gates keeps one rule: no evidence, no PASS record.**

![No evidence, no PASS](assets/showcase/no-evidence-no-pass.svg)

### What Do These Words Mean?

- **PASS**: a gate verdict that allows work to move forward.
- **Evidence**: a real test, review, or verification result.
- **Artifact**: the file that stores that evidence, such as a QA report, code-quality review, or final verification record.
- **Gate**: one review step, such as QA, complexity, architecture health, or code quality.

### Terminology Split

- **Four gates**: `qa-test-gate`, `complexity-gate`, `architecture-health-gate`, `code-quality-gate`
- **hook**: host interception capability, not the four gates

In plain terms, formal-gates does not trust "I tested it." The AI supplies semantic judgments or observations, scripts generate the static formal evidence, and the command layer checks whether the result can support a PASS.

### Why Is That Useful?

It turns "the AI thinks it is done" into three checkable questions:

1. Is there an evidence file?
2. Are the evidence fields complete?
3. Does it match the current workflow and snapshot?

If evidence is missing, incomplete, or reused from an old snapshot, the PASS record is rejected.

To reproduce the smallest case, run this demo: [Minimal Self-PASS Block Demo](examples/minimal-self-pass-block-demo.en.md).

> Note: allowing a command to continue does not mean formal PASS has been recorded. The artifact still has to exist and pass formal-gates validation.

---

## What Can I Do

| What you want to do | Which gate to use |
|---------------------|-------------------|
| Align requirements before writing OpenSpec / PRD / SDD | **Requirements Clarification Gate** (optional) |
| After writing code, verify test coverage | **qa-test-gate** |
| Check if the change is over-engineered | **complexity-gate** |
| Check module boundaries and dependency direction | **architecture-health-gate** |
| Check code correctness, dead code, fake tests | **code-quality-gate** |
| Final validation before release/seal | Run all four gates on the same snapshot; they may execute in parallel |

Only after you tell the AI "**run four gates**", "**do formal gate review**", or "**validate before seal**" will it follow the installed skill rules. Whether invalid commands are blocked automatically depends on the tool you use; if automatic blocking is unavailable, run the validation commands explicitly.

| Scenario | Gate required? |
|----------|---------------|
| Major refactors, new systems | No, unless the user asks for gate review |
| Pre-release/seal validation | Yes, when the user asks to seal or run four gates |
| Before writing OpenSpec / PRD / SDD | No formal gate by default; first do lightweight routing. Non-semantic edits ask no questions and create no artifacts. Semantic changes need clarification before formal requirement text. |
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

For requirement-like document edits, formal-gates uses lightweight semantic routing before editing. This covers OpenSpec, PRD, SDD, phase docs, development plans, technical plans, implementation plans, handoff docs, and roadmap or milestone sections when they define scope or acceptance. The routing check is not a formal gate: typo or formatting edits proceed with 0 questions and 0 artifacts; low-risk clarification uses confirmed sources; semantic changes need confirmation before the document states them as requirements.

**Candidate development overlap**: once the requirement is frozen, candidate code may be developed alongside QA Design, Design Review, and Design Rework. The lanes are mutually blind: candidate development must not read QA drafts, conclusions, or repair records, while QA design, review, and case editing must not read production implementation, diffs, existing tests, developer self-tests, implementation notes, or developer explanations. Candidate code cannot claim formal acceptance; after the case set is approved, it may be adopted, modified, or deleted without a rewrite.

### Four Post-work Gates (run only when the user asks; same-snapshot reviews may run in parallel)

1. **qa-test-gate** — Are test cases and acceptance criteria trustworthy? Does QA have real, owned evidence?
2. **complexity-gate** — Did the change bloat? Is it the minimum sufficient implementation? Over-engineered? Created unnecessary systems?
3. **architecture-health-gate** — Are module boundaries, ownership, dependency directions, state/cache lifecycles, and performance shape sound?
4. **code-quality-gate** — Correctness, edge cases, performance, dead code, fake tests, maintainability.

All four review the same workflow and external VCS change snapshot independently. Reviewers may finish in parallel, while the main agent mechanically validates and serializes shared-state commits; the CLI cross-process lock prevents lost updates after an accidental concurrent commit. Finalization still requires all four target-bound PASS results or accepted per-gate Carry decisions. Before repair, every affected path is tracked and the on-site VCS fixes the pre-repair and post-repair snapshots for direct Carry comparison. If that comparison is unreliable, Carry is unavailable and affected gates cannot enter a new-snapshot rerun without terminal `RERUN_REQUIRED`.

QA has two separate responsibilities: an independent subagent reviews test-case design, and a QA executor independent from the developer runs the post-development cases. The main agent and CLI only check execution evidence, hashes, snapshot, and case binding; they do not add a second QA reviewer.

---

## Core Mechanism

- Pass verdicts that require quality judgment must come from **zero-context independent review AI**—it doesn't know the main AI's conclusions or suspicions, avoiding echo chambers. QA Execution is the exception: it uses independent QA-owned execution evidence and main-agent/CLI mechanical validation, not a reviewer of the reviewer.
- All static formal content is script-generated. `prompt prepare` generates the seven-field message, and `receipt register` accepts only role-specific source paths and creates a read-only reviewer or Carry catalog with every static field populated; callers never supply check IDs or gate/path bindings. Reviewers, Carry, and QA Design submit only ordered semantic scalars. Requirements owners and QA executors likewise submit only pure semantic scalars with 1-based positions to compose commands; the CLI generates DIM/Case/Execution IDs, JSON keys/objects/arrays, references, and bindings. AI never edits formal JSON or QA Design Markdown and never repeats static content. Compose and submit reject duplicate, missing, out-of-range, empty, or illegal values before writing; failure leaves no partial artifact or proof, and success writes and validates the result atomically.
- `receipt register` blocks a fourth finalized review for the same workflow, gate, and stage. `--user-authorized-extra-review` is permitted only after explicit user approval; the main agent cannot authorize itself.
- Each enabled gate verdict is a closed schema-version-2 JSON **artifact** checked by the Go validator. Markdown may explain a result but cannot supply machine truth; missing fields, unknown fields, invalid evidence, or stale conclusions are rejected.
- Cross-workflow isolation is enforced: prerequisite gates must belong to the same `workflowId` and `changeSnapshot`; extension gates also bind prerequisites to the same manifest path and hash.
- Configured and tested hooks can block invalid commands; when using `formal-gates workflow` / `formal-gates gate`, the command layer validates evidence and rejects invalid records.
- Maintainers can inspect the built-in machine policy without authorizing any PASS: `bin/formal-gates policy show --format json`.

---

## Visible Evidence

For a first verification pass, check two result types:

```bash
# Local package, prompt, hook decide, workflow, receipt, and install self-check
bin/formal-gates canary portable --root . --format json

# Read-only report of the Go validator's built-in policy
bin/formal-gates policy show --format json

# Automatically checked behavior cases; expect all 25 to PASS
bin/formal-gates behavior evaluate --root . --cases examples/skill-behavior-prompts.json --answers examples/skill-behavior-answers.json

# Run only when validating Codex host auto-interception; failure does not mean native validation failed
bin/formal-gates canary codex-hook --worktree .
```

`portable canary` is the main proof for capabilities controlled by this package. `codex-hook` only proves whether the current Codex client actually invokes hooks and blocks invalid commands. If it fails, the host's automatic interception is not closed-loop; keep using explicit `formal-gates workflow` / `formal-gates gate` evidence validation and do not claim Codex hook blocking proven.

`examples/skill-behavior-prompts.json` and `examples/skill-behavior-answers.json` are the 25 automatically checked behavior cases used by package validation and the portable canary. Root `test-prompts.json` is the broader manual/model evaluation prompt set with 22 scenarios, not the fixed package self-check fixture.

The full maintenance local self-check chain lives in [`references/local-validation.md`](references/local-validation.md).

Current support can be described this way:

| Tool | How to use it |
|------|---------------|
| Claude Code / Cursor | Project-local installs have been verified to block "record PASS without evidence." |
| Codex | Install the rules and run explicit formal-gates validation commands. Codex exposes no usable subagent start/stop events, so receipts do not block on or synthesize them. Claim automatic command blocking only after the `codex-hook` live canary passes on that host. |

Detailed host evidence and version boundaries live in [`references/install-and-hooks.md`](references/install-and-hooks.md).

---

## Release Trust Boundary

The current package is suitable for local installs, local validation, and candidate package checks. CI is configured to upload per-platform binaries, `portable canary` output, and SHA256 checksums when a GitHub Release is published. Checksums only prove downloaded files match CI artifacts; do not describe the current repository state as having:

- public registry or marketplace distribution;
- `npx` remote one-command installation;
- binary signatures, provenance, or attestations;
- a third-party-verifiable release-trust chain.

Before public release, add signatures or provenance. After a real release workflow succeeds, release binaries, checksums, and `portable canary` output make the build result easier to verify, but they do not replace signing or provenance.

---

## Installation

Prefer the native CLI for installs. Do not copy only `SKILL.md`; the installer copies the runtime skill subset and configures each selected host's complete native hook set by default. Pass `--skip-hooks` only when hook configuration is intentionally out of scope.

```bash
# Install to global Claude Code and configure native command hooks
bin/formal-gates install --source . --host claude --scope global --force

# Install Codex support for a project and configure its complete native hook set
bin/formal-gates install --source . --host codex --scope project --project <project> --force

# Install Cursor hook support for a project
bin/formal-gates install --source . --host cursor --scope project --project <project> --force

# Install only the runtime and leave existing host hooks unchanged
bin/formal-gates install --source . --host claude --scope global --force --skip-hooks
```

On Windows, use `bin/formal-gates.exe`. Maintenance local self-check commands live in [`references/local-validation.md`](references/local-validation.md).

Each host must be installed and verified on its own. A passing canary on one host does not mean another host enforces hooks.

### Codex Note

Codex users should not rely only on automatic blocking. Unless `formal-gates canary codex-hook --worktree <repo>` passes on the same machine and Codex client, explicitly run `formal-gates workflow` / `formal-gates gate` to record and validate PASS evidence after installation. Codex may emit no usable `SubagentStart` / `SubagentStop`, so installation still writes the existing complete hook set while receipt finalization does not require those events; dispatch, exact-prompt, CLI semantic-submission, artifact/hash, and closure checks remain mandatory.

---

## Requirements

- **User runtime**: the platform `formal-gates` binary and the host application. Core commands do not require PowerShell, Bash, Python, Node, or Git Bash.
- **Development / CI**: Go 1.22+ to build, test, and package native binaries.

---

## Portable Validation

> **Prerequisite**: Go 1.22+, with `go` in PATH (verify with `go version`).

Maintenance local self-check commands live in [`references/local-validation.md`](references/local-validation.md). This section keeps the other cross-platform validation commands.

```bash
# Generate requirements alignment, decision, and PASS from positioned semantic scalars
bin/formal-gates artifact compose-requirements \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id <workflow-id> --change-snapshot <snapshot> \
  --requirement-source openspec/changes/CHANGE \
  --alignment-id RQ-064 \
  --alignment 1 \
  --alignment-value '<requirement or question>' --alignment-value '<source>' \
  --alignment-value '<why it matters>' --alignment-value CONFIRMED \
  --alignment-value '<user answer>' --alignment-value '<downstream effect>' \
  --alignment-value '<document impact>' --alignment-value '<evidence needed>' \
  --user-original '<confirmed original user text>' --coverage-scan PASS \
  --scope-status PASS --scope-message '<scope judgment>' \
  --task-status PASS --task-message '<task proof judgment>' \
  --dimension 1 --dimension-status COVERED \
  --dimension-message '<coverage judgment>' \
  --dimension-ref 1 --dimension-ref-item 1 \
  --covered-target openspec/changes/CHANGE/alignment.md \
  --covered-target openspec/changes/CHANGE/tasks.md \
  --output-dir restricted/requirements

# Repeat one positioned value group per alignment. Dimension groups must cover
# positions 1 through 13 and bind at least one alignment position each.

# Generate changed-files content, hash, and proof from explicit delivery paths
bin/formal-gates artifact compose-changed-files \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id <workflow-id> --change-snapshot <external-vcs-snapshot> \
  --path internal/a.go --path README.md \
  --output restricted/changed-files.txt

# The worker adds a new delivery file to the on-site VCS immediately before
# further edits and adds an existing untracked delivery file before modifying
# or deleting it. Add only
# explicit delivery paths, never git add . / git add -A, and leave unrelated
# untracked files untouched. Before return, every delivery path must be tracked
# and present in the complete VCS diff.

# Generate a context bundle from run-local source paths; do not author paths or hashes
bin/formal-gates artifact compose-context-bundle \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id <workflow-id> --change-snapshot <snapshot> \
  --output restricted/complexity/context-bundle.json \
  --input restricted/requirements/requirements.json \
  --input restricted/changed-files.txt

# QA Design registration generates fixed Case IDs and the field catalog
bin/formal-gates artifact compose-context-bundle \
  --root . --run-dir .gates/runs/RUN_ID \
  --workflow-id <workflow-id> --change-snapshot <design-snapshot> \
  --output restricted/qa-design/context-bundle.json \
  --input restricted/requirements/requirements.json
bin/formal-gates receipt register --provider codex --worktree . \
  --run-dir .gates/runs/RUN_ID \
  --context-bundle restricted/qa-design/context-bundle.json \
  --qa-case-count 6 \
  --artifact .gates/runs/RUN_ID/restricted/qa-design/cases.md \
  --gate qa-test-gate --stage Design \
  --workflow-id <workflow-id> --change-snapshot <design-snapshot>

# The designer submits semantics only; the CLI writes the title, Case IDs,
# labels, separators, and final newlines. For each --design-case, pass exactly
# seven --case-value flags in Claim/Source/Action/Oracle/Failure signal/
# Evidence/Gap order.
bin/formal-gates receipt submit --worktree . \
  --artifact .gates/runs/RUN_ID/restricted/qa-design/cases.md \
  --design-case 1 \
  --case-value '<claim>' --case-value '<source>' \
  --case-value '<action>' --case-value '<oracle>' \
  --case-value '<failure signal>' --case-value '<evidence>' \
  --case-value '<gap>'

# Generate the handoff from the approved cases and Design Review closure
bin/formal-gates handoff compose --root . \
  --run-dir .gates/runs/RUN_ID \
  --workflow-id <workflow-id> --change-snapshot <snapshot> --vcs <git|svn|p4|other> \
  --output restricted/development-handoff.md \
  --requirement-target openspec/changes/CHANGE \
  --verification-requirements 'go test ./... && go vet ./...' \
  --forbidden-context 'QA drafts, review conclusions, and repair history' \
  --formal-flow-mode four-gate --trigger-source 'explicit user request' \
  --qa-case-set restricted/qa-design/cases.md \
  --design-review restricted/closures/design-review.json

# Formal development requires an external VCS. Dispatch the worker only after this passes.
bin/formal-gates handoff validate --root . \
  --file .gates/runs/RUN_ID/restricted/development-handoff.md \
  --workflow-id <workflow-id> --change-snapshot <snapshot>

# Validate a run-local reviewer JSON artifact generated by CLI finalization
bin/formal-gates artifact validate \
  --root . \
  --file .gates/runs/RUN_ID/restricted/complexity-review.json \
  --gate complexity-gate \
  --workflow-id <workflow-id> \
  --change-snapshot <external-vcs-snapshot>

# Before a repair, use the on-site VCS's native state/checkpoint facility to fix
# the pre-repair boundary; after it, use the same VCS for the exact comparison.
# Generate and validate the exact reviewer message
bin/formal-gates prompt prepare --root . \
  --output .gates/runs/RUN_ID/restricted/complexity/prompt.txt \
  --gate complexity-gate --current-requirement openspec/changes/CHANGE \
  --current-diff '<external VCS command that emits the complete delivery diff>' --worktree . --change-snapshot <external-vcs-snapshot> \
  --review-artifact .gates/runs/RUN_ID/restricted/complexity/review.json \
  --policy-id complexity.post-development.v2 \
  --context-bundle .gates/runs/RUN_ID/restricted/complexity/context-bundle.json

# Revalidate the exact-send file; send it verbatim after PASS
bin/formal-gates prompt validate --root . --file .gates/runs/RUN_ID/restricted/complexity/prompt.txt

# Bind prompt and machine evidence before dispatch and generate the read-only reviewer catalog
bin/formal-gates receipt register --provider codex --worktree . --run-dir .gates/runs/RUN_ID --context-bundle restricted/complexity/context-bundle.json --prompt restricted/complexity/prompt.txt --changed-files restricted/changed-files.txt --verification restricted/verification.json --artifact .gates/runs/RUN_ID/restricted/complexity-review.json --gate complexity-gate --workflow-id <workflow-id> --change-snapshot <snapshot>

# Submit semantic values only; the CLI generates nested JSON, PENDING verdict, and submission proof
bin/formal-gates receipt submit --worktree . --artifact .gates/runs/RUN_ID/restricted/complexity-review.json --check 1 --status PASS --message '<semantic judgment>' --check 2 --status REVIEW --message '<semantic judgment>' --finding-check 2 --finding-message '<finding>' --location-finding 1 --location-path internal/example.go --location-start 10 --location-end 12

# Generate QA-owned results and case binding from the QA executor's semantic observations
bin/formal-gates artifact compose-qa-owned-evidence --root . --run-dir .gates/runs/RUN_ID --workflow-id <workflow-id> --change-snapshot <snapshot> --approved-case-set restricted/qa-design/cases.md --case 1 --outcome PASS --procedure '<procedure>' --observation '<observation>' --oracle-result '<oracle result>' --output-dir restricted/qa-execution

# Generate mechanical QA_EXECUTION from six existing sources; the main agent does not author its envelope or bindings
bin/formal-gates artifact compose-qa-execution --root . --run-dir .gates/runs/RUN_ID --workflow-id <workflow-id> --change-snapshot <snapshot> --output restricted/qa-execution.json --approved-case-set restricted/qa-design/cases.md --design-review restricted/closures/design-review.json --qa-owned-results restricted/qa-execution/qa-results.json --case-result-binding restricted/qa-execution/case-result-binding.json --changed-files restricted/changed-files.txt --verification restricted/verification.json

# Generate the Carry transition chain from script-generated hop evidence; repeat all four hop scalars as an ordered group
bin/formal-gates artifact compose-transition-chain --root . --run-dir .gates/runs/RUN_ID --workflow-id <workflow-id> --target-snapshot <snapshot-2> --output restricted/carry/transition-chain.json --hop-from <snapshot-1> --hop-to <snapshot-2> --hop-changed-files restricted/changed-files.txt --hop-verification restricted/verification.json

# The worker/orchestrator first ensures repair paths are tracked, then fixes the
# pre-repair and post-repair snapshots with the on-site VCS. The Carry reviewer
# compares those snapshots directly; if comparison fails, affected gates cannot
# enter a new-snapshot rerun without terminal RERUN_REQUIRED.
bin/formal-gates workflow record-stage --worktree <repo> --run-dir .gates/runs/RUN_ID --gate complexity-gate --verdict PASS --artifact .gates/runs/RUN_ID/restricted/complexity-review.json --workflow-id <workflow-id> --change-snapshot <external-vcs-snapshot>
bin/formal-gates workflow verify-admission --worktree <repo> --run-dir .gates/runs/RUN_ID --gate architecture-health-gate --workflow-id <workflow-id> --change-snapshot <external-vcs-snapshot>
bin/formal-gates workflow final-verification --worktree <repo> --run-dir .gates/runs/RUN_ID --attempt-artifact .gates/runs/RUN_ID/restricted/final-verification-go-test.txt --attempt-artifact .gates/runs/RUN_ID/restricted/final-verification-go-vet.txt --output .gates/runs/RUN_ID/restricted/final-verification.json --record-final-qa --final-qa-artifact .gates/runs/RUN_ID/restricted/final-execution.json --workflow-id <workflow-id> --change-snapshot <external-vcs-snapshot>
```

Each `--attempt-artifact` must be a run-local PASS output generated by the verification runner; the flag is repeatable. The runner must pass the path only after the command exits successfully. The CLI rejects output with clear `FAIL`, `FAILED`, `FAILURE`, `ERROR`, `FATAL`, or `PANIC` lines, a `COMMAND FAILED`/non-zero exit-status marker, or a Go compiler/vet diagnostic; empty output remains valid for successful commands such as `go build` and `go vet`. Rejected attempts produce a `FAIL` aggregate. The CLI writes the aggregate and every accepted attempt's path, hash, status, and accepted value; AI must not fill them. `--output` and `--final-qa-artifact` are never overwritten, so a rerun must use new output paths.

On Windows, use `bin/formal-gates.exe`. For development tests from a source checkout, `go run ./cmd/formal-gates` is acceptable. Installed hook and validation paths must use `bin/formal-gates(.exe)`.

This native CLI now has package validation, artifact field checks, prompt pollution checks, install support, command-blocking decisions, gate-state checks, workflow recording and cleanup, receipt recording, portable canary, Codex canary, and a behavior-case evaluation entrypoint. It is still not a complete workflow engine, agent runtime, persistent report system, cache system, or release-trust system.

---

## Package Structure

```
formal-gates/
  SKILL.md                  # Entry point (for AI): routing, red lines, fixed gate IDs, final aggregation
  references/               # Gate-specific rules (loaded on demand)
    requirements-clarification-gate.md
    qa-test-gate.md
    complexity-gate.md
    architecture-health-gate.md
    code-quality-gate.md
    install-and-hooks.md
  bin/                      # Locally built native CLI, not tracked by git
  cmd/                      # Go native CLI source
  internal/                 # Go core implementation
  hooks/                    # Dispatch prompt pollution rules
  agents/                   # Independent gate review agent prompts
  examples/                 # CLI demos and behavior-check cases
  formal-gates.manifest.json # Package index and install config
```

Humans read this README to get started; AI enters through `SKILL.md`. Gate-specific criteria are loaded from `references/` as needed.
`examples/sample-*.json` and `examples/sample-*.md` are structural references only. Formal records must be generated through `formal-gates gate` / `formal-gates workflow`; do not copy sample files directly as state or artifacts.

---

## License

This project is open source under the **MIT License**. See [LICENSE](LICENSE) for details.

---

## Changelog

For full version history and detailed changelog, see [CHANGELOG.md](CHANGELOG.md).
