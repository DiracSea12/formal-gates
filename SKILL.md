---
name: formal-gates
description: Use when the user explicitly asks for formal requirements alignment, development readiness, formal development, independent post-development gates, release, an explicit formal-gates run seal, or formal-gates installation and diagnosis. Do not activate for ordinary delivery wrap-up, implementation, review, explanation, or tiny edits unless explicitly requested.
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

## Activation

Activate the formal workflow only when the user asks for formal requirements
alignment, start-readiness review, formal development, post-development gates,
release, or an explicit formal-gates run seal. Routine coding, informal review,
ordinary delivery wrap-up, explanations, typo fixes, and other small low-risk
work stay outside this workflow.

When requirement-like text changes meaning, clarify the consequential gap
before writing it as confirmed requirement text. Non-semantic edits need no
formal run or artifact. Questions must explain the real user-visible choice in
plain language; if a question cannot be explained without internal jargon, do
not ask it yet.

## Single Formal Flow

`SKILL.md` is the only owner of this order. Run only the stages the user
requested; omitted review stages are not backfilled during seal:

1. **Start.** Select an external VCS, freeze its native base identity, and run
   `formal-gates workflow start`. No-VCS formal runs are unsupported.
2. **Requirements.** The main agent uses `requirements-clarification`
   interactively, one consequential decision at a time, and remains read-only
   until the user confirms the outcome and consequential solution choices.
   Record PASS, then bind the exact revision with `workflow requirement
   --confirmed`. After a meaning-changing revision, QA Design receives every
   prior case as unapproved review input and returns a newly approved complete
   set, retaining unaffected cases when impact is reliably bounded.
3. **Route once.** Read `workflow route-candidates`, present QA first followed
   by every discovered gate, and record one `none`, `full`, or `custom` route.
   Custom omissions receive route skip authorization. Later additions require
   explicit user direction; QA cannot be added after development begins.
4. **Before formal development.** For `full` and `custom`, run
   `start-readiness`; the `none` route omits it. Only when QA is selected, run
   blind `qa-design`. These selected actions may run in parallel. Record QA
   cases only through `workflow qa-design`.
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
   orchestrator records returned semantic results. A semantic PASS or FAIL is
   authoritative for its snapshot; only PENDING and RUNTIME_ERROR are retryable.
8. **Repair.** A wave with QA FAIL or a P0/P1 gate finding returns to repair and
   includes every P2 finding from that wave. Before editing, freeze the current
   VCS identity. After the worker
   finishes, freeze the new identity and call `workflow snapshot`. Run independent
   `carry` against only the immediate pre-repair-to-current native comparison.
   Carry returns `INHERIT` or `RERUN` for every previously passing gate. Always
   rerun QA Execution; rerun only gates marked `RERUN`, and give each rerun gate
   the complete base-to-current comparison. QA and every selected gate share
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
   not block Seal. When a post-development addition makes an initial none route
   non-empty, run deferred Start Readiness before preparing that gate or Seal;
   retain the current immutable development snapshot.

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
  --vcs <vcs> --base-snapshot <base>

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
  --run-id <id> --meaning <preserved|changed>
formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <none|full|custom> [--gate <gate-id> ...]
formal-gates workflow route-add --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id>
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness
formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

# QA Design, then development worker.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-design
formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --case '<description>' --procedure '<public procedure>' --oracle '<expected result>'
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

# Record one group for every approved case and every discovered gate.
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

# After a repair snapshot, prepare and record Carry when prior PASS gates exist.
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

The main agent itself follows the prepared Requirements Clarification task.
For an independently dispatched action or gate, call `workflow prepare-action`
or `workflow prepare-gate`, then send stdout as the complete task through the
host's native independent-agent channel. Send the development task only to the
separate worker. Do not append chat history, findings, repair explanations,
another reviewer's result, expected verdicts, or focus instructions.

Every gate task is assembled in memory from exactly one shared
`prompts/reviewer-base.md`, exactly one selected `gates/<gate-id>.md`, current
requirement routing, native VCS routing, and the result contract. Do not bypass
the CLI composer or invoke a gate prompt directly. Missing or invalid prompt
files are runtime errors and stop dispatch.

An independent result is either:

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

## Review-Wave And Repair Limit

The orchestrator validates findings against the confirmed scope, discards
unsupported findings, groups one root cause once, and repairs all P0, P1, and
P2 findings in a blocking wave together. QA and all selected gates share at
most three completed automatic review waves per delivery attempt. The initial
post-development wave and each post-repair wave count once after all required
verification completes, regardless of semantic outcome. Each snapshot counts
at most once. Dispatch failure, interruption, missing verification, PENDING,
and RUNTIME_ERROR do not count. After exhaustion, present remaining blockers
once; the user may authorize named Seal skips, additional repair, or a
requirement change.

## Gate Files

Independent post-development gates are the valid direct Markdown children of
the installed package's `gates/` directory. The filename stem is the gate ID;
files are sorted lexically. Adding a valid file and reinstalling adds a gate.
Deleting it and reinstalling removes the gate. Do not add a gate registry,
manifest, front matter, weight, dependency graph, or project-local overlay.

QA, requirements clarification, start readiness, Carry, development, and seal
are workflow actions, not gate files.

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
