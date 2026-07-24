# Requirements Alignment

Date: 2026-07-23
Status: confirmed Phase 4 scope

This is a new requirement round. IDs start at `RQ-001`; earlier phase IDs are
not reused as Phase 4 requirements.

## RQ-001 - Prompt-file gate discovery

Requirement: Post-development independent AI review gates SHALL be discovered
from the installed formal-gates package's `gates/*.md`. The filename stem is
the stable gate ID. Files are sorted deterministically. Adding one valid file
to the package and reinstalling adds one available gate; deleting one and
reinstalling removes that gate. Project worktrees do not provide an overlay or
second gate directory.

User decision: confirmed.

## RQ-002 - Shared base prompt injection

Requirement: Every independent gate dispatch SHALL use the GateRunner contract.
The Go CLI owns package discovery, prompt composition, result recording, and
aggregation. The installed host skill owns the actual independent-agent start
and return. GateRunner SHALL load `prompts/reviewer-base.md` and the selected
gate file, validate both, and compose one deterministic complete task payload
containing the base once followed by the gate-specific prompt once, then the
current requirement and VCS routing. The host sends that payload through its
native subagent task channel; no dynamic system-message API is required.
Missing or invalid files stop before the host starts an agent. There is no
provider SDK or agent runtime inside the Go CLI and no caller-supplied
full-prompt override.

User decision: confirmed in principle; implementation uses the researched
minimal runner composition.

## RQ-003 - No prompt registry or extra metadata

Requirement: Gate discovery SHALL NOT require a gate manifest, YAML front
matter, weights, dependency graph, ordering table, or extra registration code.
Gate order is lexical by validated filename. A targeted review or rerun may
select a discovered gate ID. Seal records the current status of the discovered
set but does not require any gate to have run or passed. The
initial gate files are `complexity-gate.md`,
`architecture-health-gate.md`, and `code-quality-gate.md`. `qa-test-gate` is
replaced by the QA workflow action rather than kept as an AI prompt gate.

User decision: confirmed.

## RQ-004 - Lightweight workflow state

Requirement: The workflow SHALL retain one atomic
`.gates/tmp/<run-id>/state.json` containing only the run identity and flow,
requirement source/content revision and confirmation status, installed base
prompt and gate-catalog content revision, VCS name, base/current and optional
most-recent pre-repair snapshot, named action results, approved QA cases, QA
execution result, discovered gate IDs and their results, lightweight Carry
decisions, and current lifecycle status. `start`, `show`, `resume`, and `abort` are the
only run-lifecycle operations. Cleanup happens after successful seal or
explicit abort, never merely because a process was interrupted. The final seal
summary is retained outside the temporary run directory. This is not a second
evidence or version-control system.

User decision: confirmed.

## RQ-005 - External VCS is the diff authority

Requirement: Git, SVN, P4, or another available VCS SHALL provide the delivery
and repair diffs through its own native commands. The host/worker records
caller-provided immutable snapshot identities; each reviewer invokes the named
VCS directly to read the base-to-current or pre-repair-to-current comparison.
Every delivery file SHALL be tracked before the snapshot is fixed; a worker
adds a newly created delivery path immediately and adds an existing untracked
delivery path before modifying or deleting it. If the VCS cannot compare the
two supplied identities, the action returns a runtime error. formal-gates does
not parse or store diff contents and adds no provider adapter, content backup,
custom diff engine, untracked-file scanner, or no-VCS mode.

User decision: confirmed.

## RQ-006 - Keep quality-bearing workflow actions

Requirement: Requirements clarification, one combined start-readiness review,
QA case design, real QA execution,
Carry's lightweight rerun/inherit decision, and final aggregation remain
explicit workflow actions. QA Design writes its semantic cases into the one run
state, bound to the confirmed requirement revision; no second Design Review,
receipt, or closure remains. A requirement change invalidates the cases. QA
Execution covers every approved case on the current snapshot and records the
procedure, observation, and oracle result. Independent reviewer results use
one shared PASS/FAIL plus findings contract. Every semantic result is bound to
the CLI-generated requirement content revision and installed prompt/catalog
revision. A requirement change invalidates readiness, QA, gate, and Carry
results. A package prompt/catalog change blocks resume and requires a new run.
Malformed/missing model output is a runtime error distinct from review FAIL. It
is retained in the run state and summary but does not block seal.

User decision: confirmed.

## RQ-007 - Repository convergence

Requirement: The implementation SHALL remove obsolete receipt, lifecycle,
context-bundle, prompt-copy, recursive-closure, heavy Carry, detailed
gate-state, duplicate policy/catalog, and legacy compatibility paths. Each
remaining rule has one owner. Existing host-specific installation and hook
responsibilities remain separate where their behavior differs.

User decision: confirmed.

## Flow matrix

The following matrix is the single Phase 4 applicability contract. It is owned
by the workflow code and the maintained skill; individual gate files do not
declare stages or dependencies.

| Flow | Required actions |
|---|---|
| Start readiness | confirmed requirement; one independent `start-readiness` action covering solution simplicity, architecture, and cold-water checks |
| Development start | confirmed requirement; requirement-bound QA cases |
| Post-development review | the user-selected combination of QA Execution and discovered `gates/*.md`, in parallel when multiple are selected, on the current requirement revision and current snapshot |
| Repair | new current snapshot; QA Execution reruns; one independent Carry action decides `INHERIT` or `RERUN` for each prior passing AI gate |
| Seal | unchanged confirmed requirement revision and installed prompt/catalog revision; host-native VCS identity matches before and after aggregation; QA, start-readiness, and gates may be any combination of `PENDING`, `PASS`, `FAIL`, or `RUNTIME_ERROR`, including no executed review at all |

An inherited gate result is flattened onto the new current snapshot with its
immediate source snapshot and decision. A later repair judges that current
effective result again; no transition chain is retained. Requirements and QA
case approval are bound to the requirement revision rather than the code
snapshot, so they do not become stale merely because implementation changes.

Parallel agents never write state. The host records completed semantic results
through the CLI. The CLI takes the existing cross-process lock around the full
reload, terminal-state check, mutation, and atomic replacement. Late writes
after abort or seal are rejected. No in-flight `RUNNING` state is persisted;
an interrupted dispatch remains `PENDING` and may be dispatched again on
resume.

## Gate file contract

A gate file is a direct regular child of `gates/`, has a filename matching
`[a-z0-9]+(?:-[a-z0-9]+)*.md`, contains valid UTF-8, and is non-empty after
trimming whitespace. No Markdown parser or semantic linter is added. The shared
base states that its cross-gate rules take precedence; a gate file owns only
its role-specific checks. Duplicate shared rules are removed from maintained
gate files during migration.

## Boundary

Only normal documented use and common operator mistakes are in scope. Findings
that require manually rewriting internal artifacts, permissions, immutable
files, malicious local edits, or attack-style inputs are advisory and SHALL
NOT block this phase.
