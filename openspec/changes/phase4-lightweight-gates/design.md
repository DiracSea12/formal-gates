# Design

## Prompt assembly

GateRunner is a contract split across the existing owners, not a new agent
runtime. The Go CLI discovers package-local `gates/*.md`, validates direct
child filenames, sorts by filename, composes the prompt, records the semantic
result, and aggregates state. The installed host skill starts the independent
agent with that exact complete task payload and returns its semantic result.
The host task channel is the transport; a dynamic system-message role is not
required. The CLI does not
link a Codex, Claude, Cursor, or generic provider SDK.

For gate `complexity-gate`, the task payload is:

```text
[Shared reviewer contract]
<reviewer-base.md>

[Gate: complexity-gate]

<gates/complexity-gate.md>

[Current requirement]
<requirement path and CLI-generated content revision>

[Current change]
<worktree, VCS name, base snapshot, current snapshot>

[Result contract]
<PASS/FAIL and findings syntax>
```

The base and gate text each occur exactly once. The reviewer reads the current
requirement from the named source and invokes the named VCS directly for the
specified snapshots. Diff bytes, prior findings, repair explanations, target
verdicts, and run-local artifacts are not supplied.

The host must use the CLI-composed prompt for every formal independent gate;
old direct agent files and full-prompt overrides are removed. The same result
type is used by targeted runs and seal. A gate result contains `gateId`,
`requirementRevision`, `catalogRevision`, `snapshot`, `status` (`PASS`, `FAIL`,
or `RUNTIME_ERROR`), and `findings`.
Each finding contains a message and zero or more repository-relative
locations. PASS requires an empty findings list; FAIL requires at least one
finding. Invalid or missing model output becomes `RUNTIME_ERROR`, not FAIL.

The base contract states that shared independence, context, completeness, and
result rules take precedence over role-specific prose. Static validation does
not try to understand prompt semantics. Migration removes copied shared rules
from gate files so the base is their only maintained owner.

## Package and applicability

`prompts/reviewer-base.md` and `gates/` live in the formal-gates package root
and are copied by the existing installer for every host. Package manifest,
package validation, installed-target tests, and cleanup/convergence checks are
updated together. Project worktrees do not override or extend this directory.

The initial post-development gate inventory is:

- `complexity-gate.md`
- `architecture-health-gate.md`
- `code-quality-gate.md`

QA is a workflow action. Start readiness is one independent action using
`prompts/actions/start-readiness.md`; it combines solution-simplicity,
architecture, and cold-water checks without invoking or hardcoding gate IDs.
Seal records every discovered gate's current status but does not require any
gate, QA, or readiness action to have run or passed. This applicability matrix
has one workflow owner and is not repeated inside individual gate files.

## VCS and snapshots

The caller freezes comparable native VCS snapshot identities. Carry invokes
the named VCS directly to inspect immediate pre-repair to current. Every fresh
or rerun gate inspects the complete base-to-current comparison. The worker tracks every delivery
file before freezing the relevant snapshot, including new files and existing
untracked files it will modify or delete. If the native VCS cannot reproduce
the exact comparison, the action returns `RUNTIME_ERROR`. formal-gates stores
only the VCS name and identities; it does not parse diff bytes, copy content,
or implement a VCS adapter. Immediately before parallel review, and again
immediately before and after seal aggregation, the host obtains the delivery's
native immutable identity using the documented VCS command and submits it to
the CLI. The CLI compares that supplied identity with the run's current
snapshot and rejects a mismatch. Native command recipes for Git, SVN, and P4
live in one VCS reference; they are instructions, not adapters.

## Run state and lifecycle

One atomically replaced `.gates/tmp/<run-id>/state.json` contains:

- run ID, flow, lifecycle status, requirement source and content revision;
- shared-base revision and the ordered gate catalog/content revision;
- VCS name, base/current and optional immediate pre-repair snapshot;
- named action records, including start-readiness;
- approved QA cases and QA execution result;
- the discovered gate set and each gate result;
- per-gate immediate Carry decision and source snapshot.

Lifecycle status is `ACTIVE`, `SEALED`, or `ABORTED`; action status is
`PENDING`, `PASS`, `FAIL`, or `RUNTIME_ERROR`. Dispatch is in-memory host
activity and does not create a persisted `RUNNING` state. `start` creates a
unique run, `show` reads it, `resume` preserves completed results and leaves an
interrupted dispatch `PENDING`, and `abort` marks it terminal and cleans its
temporary directory after writing an abort summary. Successful seal
atomically writes the retained final summary before cleaning the temporary run.

All state mutations use the existing cross-process lock around the complete
read, terminal-state check, mutation, and atomic replacement. Agents may run in
parallel, but only the host records their completed results. A result submitted
after seal or abort is rejected. This preserves parallel review without lost
updates or a new scheduler.

The CLI computes the requirement revision from source bytes and the catalog
revision from the shared base, every installed action prompt, and ordered gate
IDs and contents. QA Design stores cases directly in state with those
revisions. A requirement revision
change clears readiness, QA, gate, and Carry results. If an installed prompt or
catalog revision changes during an active run, resume and seal reject with a
clear restart requirement rather than mixing definitions. A repair clears
QA execution and invokes one lightweight independent Carry action. Its only
per-gate decisions are `INHERIT` or `RERUN`; a runtime failure is retained as
an action result without blocking seal. An
inherited PASS is recorded for the new current snapshot with only its immediate
source, so multiple repairs do not create a retained transition chain.

Carry uses `prompts/actions/carry.md`. Its task payload names the installed
catalog revision and every current gate file; the independent Carry agent reads
those role-specific definitions and uses the immediate native VCS comparison
to return one decision per prior passing gate. The host records those decisions
through the same locked CLI state mutation.

## Prompt and agent inventory

The old `agents/` gate dispatch files are removed. Their role-specific content
moves to `gates/*.md`; non-gate semantic actions move to
`prompts/actions/{requirements-clarification,start-readiness,qa-design,qa-execution,carry,development-worker}.md`.
The old QA Design Review prompt is deleted. Development start no longer creates
a closure-bound handoff file: the CLI admits development when the requirement
and QA cases in state share the current requirement revision, and the host
sends the worker the requirement/VCS routing and changed-file tracking rule.
QA cases are not sent to the worker. SKILL.md is the only workflow-order owner;
README files explain its public commands without duplicating policy tables.

## Temporary data

All disposable run data is under one `.gates/tmp/<run-id>/` directory. It is
removed only after successful seal or explicit abort. The final seal or abort
summary is the only retained workflow output. No receipt graph, recursive
closure, prompt copy, or parallel evidence tree is created.

## Validation and tests

- Gate discovery tests cover lexical ordering, the exact ASCII ID grammar,
  direct-child regular files, UTF-8, trimmed non-empty content, and add/remove
  behavior. There is no Markdown parser test.
- Prompt assembly tests capture the final task payload and assert one base,
  one selected gate prompt, and no prior finding text.
- Runner tests prove a missing base/gate stops before dispatch and that all
  discovered gates are included in final aggregation.
- State tests cover concurrent result recording under the retained lock, late
  terminal writes, interruption with `PENDING`, requirement revision
  invalidation, and package/catalog-change restart errors.
- VCS behavior tests verify that exact caller-provided identities are retained
  and routed to the host; native Git/SVN/P4 correctness remains owned by those
  tools. A normal end-to-end test uses an available VCS and includes a newly
  tracked file. Unsupported no-VCS operation fails clearly.
- Repository convergence checks prove removed names and duplicate owners are
  absent from maintained code, docs, and tests.
- Installer/package tests prove the shared prompt and every gate file reach
  each supported installed host. The maintained README and SKILL contain one
  replacement start/resume/review/repair/seal workflow, and removed commands
  are absent rather than documented as compatibility aliases.
