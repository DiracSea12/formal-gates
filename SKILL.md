---
name: formal-gates
description: Use for every request that creates, edits, moves, or deletes project content, regardless of repository, product, file type, or estimated size, and for explicitly requested formal-gates alignment, review, release, seal, installation, or diagnosis. Read-only questions, explanations, diagnostics, and reviews stay outside automatic modification intake unless the user requests formal execution.
---

# Formal Gates

Run one lightweight, VCS-backed workflow. The CLI keeps one temporary state
file; the host starts independent agents; the repository's native VCS owns all
snapshots and diffs.

## Scope Boundary

Guarantee documented normal use and common operator mistakes. Unless the user
explicitly asks for hardening, adversarial inputs, malicious edits, manual
state rewriting, permission failures, immutable-file failures, unsupported
platforms, and other contrived workflow violations are advisory and cannot
block PASS or cause new mechanisms, compatibility paths, or tests.

Every blocking finding must reproduce from a documented public entrypoint with
normal user actions or a common mistake. Test a deterministic rule at the
lowest layer that owns it; do not add tests whose main purpose is to retest
another test or validator.

## Universal Modification Intake

Activate this intake for every request that creates, edits, moves, or deletes
project content. Apply the same rules in the formal-gates source repository and
every other project. Read-only questions, explanations, diagnostics, and reviews
that request no modification remain outside automatic intake unless the user
explicitly requests formal execution.

Before any file write or implementation dispatch, the main agent MUST:

1. Inspect the workspace for facts it can determine directly.
2. Clarify the requested outcome and every technical choice that materially
   changes public behavior, acceptance, or architecture. Ask one consequential
   decision at a time in plain language and decide minor implementation details.
3. Present the complete consolidated requirements and technical solution, then
   wait for explicit user confirmation.
4. Assess total size, coupling, risk, and verification complexity; explain the
   recommendation; query `formal-gates package route-candidates --root
   <package>`; and present exactly one combined choice:
   - lightweight execution with no formal run;
   - `full`, meaning QA plus every discovered gate; or
   - `custom`, showing QA and the complete discovered gate list for subset
     selection.

When no earlier clarification question was needed, the complete-summary
confirmation and route choice may be one user response. Ask for the route only
once. A later requirement pauses related writes for clarification, a refreshed
complete summary, and explicit confirmation, but retains the chosen route unless
the user explicitly requests reconsideration.

Lightweight execution creates no workflow state and no formal task slices. It
may omit PRD, OpenSpec, design, and task-plan artifacts, but the user may request
any of them as ordinary deliverables without selecting formal gates.

For `full` or `custom`, persist the complete confirmed requirements and
technical solution in any stable document format available in the environment
before development. Do not require a named format or document plugin. Then run
the formal flow below. Both routes always run Start Readiness.

When the total confirmed formal request cannot be implemented and verified as
one coherent bounded unit, split it by dependency, ownership, risk, and
verification surface. Before any slice development, start and retain one overall
formal run at the complete request's original base with the complete requirement
and chosen route by passing `--retained-overall` to `workflow start`. Give each
slice a separate formal run and native VCS worktree; run independent slices
concurrently when capacity allows and wait on dependency edges. After merging
sealed slice branches and resolving conflicts, use `workflow snapshot` to record
the merged identity as completed development in the retained overall run, then
execute its integration QA and gates from the original base to the merged
snapshot. Do not create a second overall run, replace its base, prepare or
dispatch an overall development worker, or repeat clarification or routing after
the merge.

## Single Formal Flow

`SKILL.md` is the only owner of this order. The intake has already selected
`full` or `custom`; omitted review stages are not backfilled during seal:

1. **Start.** Select an external VCS, freeze its native base identity, and run
   `formal-gates workflow start`. No-VCS formal runs are unsupported.
2. **Requirements.** The main agent follows `requirements-clarification`,
   records the already confirmed complete outcome and solution as PASS, then
   binds the exact durable revision with `workflow requirement --confirmed`.
   After a meaning-changing revision, QA Design receives every
   prior case as unapproved review input and returns a revised complete
   candidate set, retaining unaffected cases when impact is reliably bounded.
   Classifying any changed revision also binds the current immutable live VCS
   identity that contains it.
3. **Bind the chosen route.** Read `workflow route-candidates` to verify the
   run-bound catalog and record the already chosen `full` or `custom` route
   without asking again. Custom omissions receive route skip authorization.
   Later additions require explicit user direction; QA cannot be added after
   development begins.
4. **Before formal development.** Run `start-readiness`. Only when QA is selected, run
   blind `qa-design`. These selected actions may run in parallel. Record the
   complete candidate cases only through `workflow qa-design`, then dispatch
   independent `qa-review`. PASS approves the cases and unlocks development.
   FAIL retains them as unapproved rework input and returns to QA Design;
   RUNTIME_ERROR retries QA Review without reopening design. This loop does
   not consume post-development review waves.
5. **Development.** Prepare `development-worker`; preparation records that
   development started and freezes pre-development results and late QA routing.
   After normal interruption, preparing the same PREPARED development or repair
   task recomposes it without moving the recorded start boundary.
   Dispatch a worker separate from formal reviewers. Do not send it the QA
   cases. The worker implements only the confirmed scope, adds each new or
   previously untracked delivery path explicitly to the named VCS before
   continuing, and verifies the full native base-to-current comparison before
   returning.
6. **Fix the current snapshot.** Use the native VCS to create an immutable
   identity for the completed implementation and record it with `workflow
   snapshot`. Never send mutable working-tree state to review.
7. **Post-development review.** Dispatch every selected
   combination of QA Execution and discovered gates. Selected independent
   actions may run in one parallel wave. QA receives the approved cases. Every
   selected gate receives the complete base-to-current VCS route and
   independently inspects that diff. Agents never write workflow state; the
   orchestrator validates and records returned semantic results. A recorded
   semantic PASS or FAIL is authoritative for its snapshot; only PENDING and
   RUNTIME_ERROR are retryable.
8. **Repair.** A wave with QA FAIL or a P0/P1 gate finding returns to repair and
   includes every P2 finding from that wave. Before editing, freeze the current
   VCS identity. After the worker
   finishes, freeze the new identity and call `workflow snapshot`. The main
   agent inspects the immediate pre-repair-to-current native comparison. Only
   when it can bound the repair so no previously passing selected verification
   can be affected, it may use main-agent Carry to inherit every prior PASS,
   including QA, with a non-empty reason. Otherwise dispatch independent Carry
   for every previously passing selected discovered gate and rerun QA. Shared
   APIs, public behavior, configuration, dependencies, cross-gate ownership,
   and uncertain causal chains always use independent Carry. Rerun only gates
   marked `RERUN`, and give each rerun gate the complete base-to-current
   comparison. QA and every selected gate share
   three completed review waves, counting the initial complete post-development
   wave and every complete post-repair wave once. Incomplete or runtime-error
   waves do not consume one. Additional waves require explicit user
   authorization.
9. **Seal.** Confirm the live native VCS identity immediately before and after
   aggregation. Selected PENDING work blocks Seal. RUNTIME_ERROR requires an
   explicit user skip, and QA FAIL or P0/P1 requires repair until the shared
   limit is exhausted before a skip may be authorized. Route and Seal skips are
   retained in the summary. Named Seal authorizations persist if another result
   still blocks that attempt, apply only to the current snapshot, and clear on
   a later repair snapshot. P2-only PASS recommendations remain visible but do
   not block Seal.

Use `workflow show` to inspect a run and `workflow resume` after interruption.
An interrupted dispatch remains `PENDING`; preserve completed results. Use
`workflow abort` to retain an abort summary and remove that run's temporary
directory.

## CLI Command Map

Use the installed `formal-gates` binary. The examples below omit only repeated
finding or case groups.

```bash
# Start. --current-snapshot defaults to the base.
formal-gates workflow start --root <repo> --package-root <package> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  --vcs <vcs> --base-snapshot <base> [--retained-overall]

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <package> --run-id <id>
formal-gates workflow abort --root <repo> --run-id <id>

# Prepare and record requirements/readiness actions.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action requirements-clarification
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action requirements-clarification --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> --confirmed
# After Resume reports a changed revision, classify its semantic effect.
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --meaning <preserved|changed> --live-snapshot <current>
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

# QA Design, independent QA Review, then development worker.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-design
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --case '<description>' --procedure '<public procedure>' --oracle '<expected result>'
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  [--finding '<message>' --location '<path:line>']
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action development-worker --live-snapshot <current>

# Record the immutable post-development or post-repair identity.
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --current-snapshot <new-current> --live-snapshot <new-current>

# Prepare QA Execution and every gate in parallel.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-execution --live-snapshot <current>
formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --live-snapshot <current>

# Record one group for every approved QA case and every selected discovered gate.
formal-gates workflow qa-execution --root <repo> --package-root <package> \
  --run-id <id> --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> --live-snapshot <current> \
  --case-result CASE-001 --outcome <PASS|FAIL> --procedure '<actual>' \
  --observation '<observed>' --oracle-result '<comparison>'
formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> \
  --live-snapshot <current> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

# For a bounded repair, inherit every prior selected PASS with no agent dispatch.
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --main-agent --main-reason '<reason>' \
  --live-snapshot <current>

# Otherwise prepare and record independent Carry when prior PASS gates exist.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action carry --live-snapshot <current>
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> --live-snapshot <current> \
  --gate <gate-id> --decision <INHERIT|RERUN> --reason '<reason>'

formal-gates workflow authorize-repair --root <repo> --package-root <package> \
  --run-id <id> --cycles 1

# Seal only after every selected result passes or has permitted authorization.
formal-gates workflow seal --root <repo> --package-root <package> --run-id <id> \
  --live-snapshot-before <current> --live-snapshot-after <current> \
  [--skip <selected-non-passing-gate> ...]
```

Repeat `--case`, `--case-result`, findings, and Carry gate groups as needed.
Use the command's `--runtime-error` or `--status RUNTIME_ERROR --message ...`
form when an agent or native comparison cannot run; do not fabricate semantic
results.

## Independent Dispatch

The main agent itself follows the prepared Requirements Clarification task and
records justified main-agent Carry directly without preparing a Carry prompt.
For an independently dispatched action or gate, call `workflow prepare-action`
or `workflow prepare-gate`, then send stdout as the complete task through the
host's native independent-agent channel. Send the development task only to the
separate worker. Do not append chat history, findings, repair explanations,
another reviewer's result, expected verdicts, or focus instructions.

QA Review receives only the confirmed requirement and complete candidate case
set assembled by the CLI. It must not inspect production implementation,
implementation diffs, tests, developer explanations, later results, or another
reviewer's conclusion.

Every gate task is assembled in memory from exactly one shared
`prompts/reviewer-base.md`, exactly one selected `gates/<gate-id>.md`, current
requirement routing, native VCS routing, and the result contract. Do not bypass
the CLI composer or invoke a gate prompt directly. Missing or invalid prompt
files are runtime errors and stop dispatch.

Every returned independent result is candidate input until the main agent
validates it against the complete confirmed requirement, the documented
normal-use boundary, and the result contract. The main agent records and
presents no semantic result before making that validation explicit. The result
contract is one of:

- `PASS` with no findings or only P2 findings;
- `FAIL` with at least one P0/P1 finding and optional P2 findings; or
- `RUNTIME_ERROR` when dispatch, context, VCS comparison, or result parsing
  fails.

Runtime errors are not review findings. They remain visible and require retry or
explicit user skip authorization. A reviewer must finish every safe in-scope check.
After finding a defect, it scans the whole current
change for the same defect pattern and follows the same causal, behavioral,
data, ownership, or dependency chain so one result reports the complete related
set instead of one symptom at a time. Advisory comments do not block PASS.

Never hurry a reviewer. A status request may ask only for progress and must say
to continue until all assigned checks are complete.

## Result Validation And Repair Limit

Before recording or presenting any semantic PASS or FAIL, the main agent makes
its complete-requirement, normal-use-boundary, and result-contract validation
explicit. For a FAIL or blocker it additionally checks retained workflow state,
scope, severity, causal claim, and cited evidence, and MUST reproduce the
end-to-end failure from a documented public entrypoint using normal user actions
or a common mistake. Discard a finding if any premise, reproduction, or evidence
check fails: do not record it, present it as a blocker, or change requirements or
implementation because of it. This is orchestration validation, not a second
verdict, gate, agent, or evidence store.

After validation, the orchestrator groups one root cause once and repairs all
P0, P1, and P2 findings in a blocking wave together. QA and all selected gates
share at most three completed automatic review waves per delivery attempt. The initial
post-development wave and each post-repair wave count once after all required
verification completes, regardless of semantic outcome. Each snapshot counts
at most once. Dispatch failure, interruption, missing verification, PENDING,
and RUNTIME_ERROR do not count. After exhaustion, present remaining blockers
once; the user may authorize named Seal skips, additional repair, or a
requirement change.

## Gate Files

Independent post-development gates are the valid direct Markdown children of
the installed package's `gates/` directory. The filename stem is the gate ID;
files are sorted lexically. `qa` is reserved for the built-in QA flow, so
`gates/qa.md` is invalid. Adding any other valid file and reinstalling adds a
gate.
Deleting it and reinstalling removes the gate. Do not add a gate registry,
manifest, front matter, weight, dependency graph, or project-local overlay.

QA Design, QA Review, QA Execution, requirements clarification, start
readiness, Carry, development, and seal are workflow actions, not gate files.

## State And VCS

The only temporary workflow state is `.gates/tmp/<run-id>/state.json`. Do not
hand-edit it. The CLI owns atomic updates, prompt/catalog revisions, action and
gate statuses, approved cases, and current snapshot bindings. It does not store
diff bytes, project file contents, an evidence graph, layered run artifacts, or
a second version-control model.

The host and agents use Git, SVN, P4, or another named VCS directly. The base
to current comparison is the total delivery diff. The immediate pre-repair to
current comparison is the repair diff used only by Carry. If the VCS cannot
reproduce a supplied immutable comparison, record `RUNTIME_ERROR`; do not
guess or implement a fallback diff engine. See `references/vcs-snapshots.md`.

## Installation And Maintenance

Read `references/install-and-hooks.md` only for installation, host hooks,
canaries, packaging, or release checks. Read `references/local-validation.md`
only when maintaining this repository. Hook configuration is not proof that a
host executed it; claim hook blocking only after a same-host live canary.

Never claim formal PASS from chat, developer self-tests, main-agent self-review,
hook configuration, or partial results. If an independent agent is unavailable,
report that review as blocked instead of issuing PASS yourself.
