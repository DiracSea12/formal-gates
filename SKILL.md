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
2. **Requirements.** Use `requirements-clarification` when meaning is not
   already confirmed. Record the result, then bind the exact confirmed
   requirement revision with `workflow requirement --confirmed`.
3. **Before formal development.** Run the independent `start-readiness` action
   and blind `qa-design` action. They may run in parallel after the requirement
   is confirmed. Record all QA cases through `workflow qa-design`.
4. **Development.** Prepare `development-worker` and dispatch a worker separate
   from formal reviewers. Do not send it the QA cases. The worker implements
   only the confirmed scope, adds each new or previously untracked delivery
   path explicitly to the named VCS before continuing, and verifies the full
   native base-to-current comparison before returning.
5. **Fix the current snapshot.** Use the native VCS to create an immutable
   identity for the completed implementation and record it with `workflow
   snapshot`. Never send mutable working-tree state to review.
6. **Post-development review, when requested.** Dispatch the user-selected
   combination of QA Execution and discovered gates. Selected independent
   actions may run in one parallel wave. QA receives the approved cases. Every
   selected gate receives the complete base-to-current VCS route and
   independently inspects that diff. Agents never write workflow state; the
   orchestrator records returned semantic results.
7. **Repair.** Before editing, freeze the current VCS identity. After the worker
   finishes, freeze the new identity and call `workflow snapshot`. Run independent
   `carry` against only the immediate pre-repair-to-current native comparison.
   Carry returns `INHERIT` or `RERUN` for every previously passing gate. Always
   rerun QA Execution; rerun only gates marked `RERUN`, and give each rerun gate
   the complete base-to-current comparison.
8. **Seal.** Confirm the live native VCS identity immediately before and after
   aggregation. `workflow seal` requires the confirmed requirement and prompt
   catalog to be unchanged, but it does not require QA, start-readiness, or any
   gate to have run or passed. It records the current QA and gate statuses,
   including pending or non-passing results, writes one retained summary, and
   removes the run's temporary directory.

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
  --live-snapshot <current> [--finding '<message>' --location '<path:line>']

# After a repair snapshot, prepare and record Carry when prior PASS gates exist.
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action carry --live-snapshot <current>
formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> --live-snapshot <current> \
  --gate <gate-id> --decision <INHERIT|RERUN> --reason '<reason>'

# Seal with any selected combination of review results, including none.
formal-gates workflow seal --root <repo> --package-root <package> --run-id <id> \
  --live-snapshot-before <current> --live-snapshot-after <current>
```

Repeat `--case`, `--case-result`, findings, and Carry gate groups as needed.
Use the command's `--runtime-error` or `--status RUNTIME_ERROR --message ...`
form when an agent or native comparison cannot run; do not fabricate semantic
results.

## Independent Dispatch

For an action or gate, call `workflow prepare-action` or
`workflow prepare-gate`, then send stdout as the complete task through the
host's native independent-agent channel. Do not append chat history, findings,
repair explanations, another reviewer's result, expected verdicts, or focus
instructions.

Every gate task is assembled in memory from exactly one shared
`prompts/reviewer-base.md`, exactly one selected `gates/<gate-id>.md`, current
requirement routing, native VCS routing, and the result contract. Do not bypass
the CLI composer or invoke a gate prompt directly. Missing or invalid prompt
files are runtime errors and stop dispatch.

An independent result is either:

- `PASS` with no findings;
- `FAIL` with one or more evidence-backed findings; or
- `RUNTIME_ERROR` when dispatch, context, VCS comparison, or result parsing
  fails.

Runtime errors are not review findings. They remain visible in the run and seal
summary but do not block seal. A reviewer must finish every safe in-scope check.
After finding a defect, it scans the whole current
change for the same defect pattern and follows the same causal, behavioral,
data, ownership, or dependency chain so one result reports the complete related
set instead of one symptom at a time. Advisory comments do not block PASS.

Never hurry a reviewer. A status request may ask only for progress and must say
to continue until all assigned checks are complete.

## Review And Repair Limit

The orchestrator validates findings against the confirmed scope, discards
advisory or unsupported findings, groups one root cause once, and repairs all
accepted findings from the same chain together. Each discovered gate gets at
most three completed automatic review-repair cycles per delivery attempt. A
cycle counts only after one complete independent result, accepted repairs, and
re-verification. Dispatch failures and interrupted reviews do not count. After
the third completed cycle for one gate, stop that gate and ask the user how to
proceed; other gates keep their own counts.

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
