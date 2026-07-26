# formal-gates

formal-gates is a lightweight AI development review workflow. It separates the
implementation worker from independent reviewers, uses the project's existing
Git, SVN, or P4 for diffs, and lets the CLI retain the results of whichever
reviews the user selected.

Its intake activates for every request that creates, edits, moves, or deletes
project content, regardless of repository, product, file type, or estimated
size. Read-only questions, explanations, diagnostics, and reviews remain outside
automatic modification intake unless the user explicitly requests formal
execution.

Before any write or implementation dispatch, the main agent inspects workspace
facts, clarifies the outcome and consequential technical choices one at a time,
then presents the complete requirements and solution for explicit confirmation.
It recommends and asks once for lightweight, full, or custom execution using the
stateless package route-candidate query. Lightweight creates no formal run;
full selects QA and every discovered gate; custom shows the complete list for a
non-empty subset choice.

## Design

- `prompts/reviewer-base.md` contains the shared independent-review contract.
- Each `gates/*.md` file is one independent review gate; its filename is its ID.
  `qa` is reserved for the built-in QA flow and cannot be a file ID.
- `prompts/actions/*.md` contains requirements, readiness, QA, worker, and Carry
  roles.
- `.gates/tmp/<run-id>/state.json` is the only temporary run-state file.
- `.gates/results/<run-id>.json` is the only retained seal or abort result.

Adding or removing a valid `gates/*.md` file and reinstalling adds or removes a
gate. There is no Go registry, gate manifest, YAML front matter, weight,
dependency graph, or ordering table. After clarification, the user makes one
lightweight, full, or custom selection from QA followed by the lexical gate
list. Formal workflow state starts only for full or custom.

QA is not part of the prompt-gate catalog. When selected, QA Design first
returns a complete set containing STATIC direct-owner checks and LIVE public
execution. An independent QA Review must pass before development. Per-case
outcomes preserve exact unchanged passing cases across Design rework, while the
next review receives only failed, new, or changed cases. This loop does not
consume a post-development review wave. After development, QA Execution and
review gates may run in one parallel wave.
Start Readiness runs before development for every formal route. Preparing the
development worker freezes the registered requirement and solution artifact set
as acceptance input and prevents QA from being added after development starts.
Post-development prompts exclude those frozen paths as ordinary review targets.
Full and custom require the complete confirmed requirements and technical
solution in any stable available document format; no named plugin is required.

The chosen route covers later requirements and task slices. Added scope pauses
related writes for clarification and confirmation of a refreshed complete
summary, without another route question unless the user requests one. Large
formal work is split by dependency, ownership, risk, and verification surface.
One overall run, started with `--retained-overall`, retains the original base,
complete requirement, and route. Independent slices use separate VCS worktrees
and formal runs and own implementation. After merge and conflict resolution,
`workflow snapshot` records the merged commit directly in the retained overall
run, which reviews it from the original base. Integration findings return to
their owning slice runs; after sealed slice repairs are merged, the retained
overall run records the new merged snapshot directly without preparing its own
development worker.

After a meaning-changing requirement revision, the next QA Design retains exact
passing cases for unaffected requirements and makes new or changed cases
pending. Prepared development tasks can be recomposed after normal interruption,
while a recorded semantic PASS or FAIL remains authoritative for its immutable
snapshot. Every QA Review and gate attempt uses a unique dispatch and newly
claimed zero-context reviewer identity, including retries.

After repair, the main agent inspects the immediate native repair diff. It may
use `workflow carry --main-agent --main-reason '<reason>'` only when the repair
cannot affect any previously passing selected verification; this inherits all
prior PASS results, including QA. Shared behavior, configuration, dependencies,
cross-gate ownership, or uncertain impact uses independent Carry as before,
with QA rerun normally.

## Installation

Build the native binary in the source checkout:

```bash
go build -o bin/formal-gates ./cmd/formal-gates
```

Then select a host and scope:

```bash
bin/formal-gates install --source . --host claude --scope global --force
bin/formal-gates install --source . --host codex --scope project --project <project> --force
bin/formal-gates install --source . --host cursor --scope project --project <project> --force
```

Installation merges formal-gates' own host hook by default. Add `--skip-hooks`
only when hook configuration must remain unchanged. Existing unrelated hooks
are preserved.

Use `bin\formal-gates.exe` on Windows. `install.command` and `install.bat` can
download matching release assets and invoke the same installer.

## Workflow Commands

These examples show command entrypoints only. [SKILL.md](SKILL.md) is the sole
owner of workflow order.

Before starting a run, query the installed package's route candidates without
a repository, requirement, run ID, VCS snapshot, or workflow state. This
read-only command does not create workflow state:

```bash
formal-gates package route-candidates --root <package>
```

The JSON array starts with `qa`, followed by the dynamically discovered gates
in filename ID order. After starting a run and confirming its requirement, use
the run-bound `workflow route-candidates` command shown below instead.

Start and inspect a run:

```bash
formal-gates workflow start \
  --root <repo> --package-root <installed-formal-gates> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  [--requirement-artifact <requirement-or-solution-file> ...] \
  --vcs <git|svn|p4> [--base-snapshot <identity-to-verify>] [--retained-overall]

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <installed-formal-gates> --run-id <id>
formal-gates workflow abort --root <repo> --run-id <id>
```

Prompt preparation writes the complete task to stdout. The main agent follows
Requirements Clarification itself; send independently dispatched action and gate
tasks verbatim without appending chat history, prior conclusions, or another
gate's result:

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action <requirements-clarification|start-readiness|qa-design|qa-review|development-worker|qa-execution|carry>

formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <discovered-gate-id>

formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <reviewer-or-session-id>
```

Record semantic results:

```bash
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> \
  [--requirement-artifact <file> ... | --clear-requirement-artifacts] --confirmed

formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --dispatch <dispatch-id> --status PASS

formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --dispatch <dispatch-id> --case '<behavior>' --kind <STATIC|LIVE> \
  --procedure '<public procedure>' --oracle '<expected result>'

formal-gates workflow qa-review --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --case CASE-001 \
  --outcome <PASS|FAIL> [--reason '<required for FAIL>]

formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --dispatch <dispatch-id> \
  --status <PASS|FAIL|RUNTIME_ERROR> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id>

formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --main-agent --main-reason '<bounded repair reason>'
```

Use `formal-gates help` and `SKILL.md` for QA Execution, Carry, repair
authorization, and Seal parameters. A changed requirement revision first needs
`workflow requirement --meaning preserved|changed`; the CLI resolves the native
VCS identity containing that revision but does not infer its semantic effect.

## Diffs And Repairs

formal-gates neither reads nor stores diff bytes and never copies project
files. The worker, QA executor, and reviewers invoke the on-site VCS directly:

- The total diff is base to current. Every fresh or rerun gate reviews it.
- The repair diff is immediate pre-repair to current. The main agent uses it
  only for the all-PASS bounded-repair shortcut; otherwise independent Carry
  decides which prior gate results remain valid.
- A new delivery file, or an existing untracked delivery file in scope, must be
  added by explicit path before further edits. Never run `git add .` or touch
  unrelated untracked files.

See [references/vcs-snapshots.md](references/vcs-snapshots.md) for Git, SVN, and
P4 commands. Formal runs do not support a no-VCS worktree.

## Results And Interruption

Every gate finding has P0, P1, or P2 impact. A gate returns `PASS` with no
findings or P2-only recommendations, `FAIL` with at least one P0/P1 finding,
or `RUNTIME_ERROR` with no findings. Selected `PENDING` work blocks Seal.
Runtime errors require retry or explicit skip. QA FAIL and P0/P1 require repair
until the shared three-review-wave limit is exhausted before Seal skip is
available. P2-only recommendations remain visible without blocking Seal.

Every independent result is candidate input. Before recording or presenting any
PASS or FAIL, the main agent explicitly validates it against the complete
confirmed requirement, normal-use boundary, and result contract. For a FAIL or
blocker, the main agent also checks retained workflow state, independently
reproduces its documented normal-use public path, and verifies its evidence,
scope, severity, and causal claim. A finding that fails any check is discarded
without changing workflow state, requirements, or implementation.

After successful seal or explicit abort, the CLI writes one summary and removes
that run's entire temporary directory. It does not retain prompt copies,
layered evidence graphs, or a detailed state tree.

## Local Validation

```bash
go test ./...
go test -race ./internal/validate ./internal/cli
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

Hook configuration is not proof that the host invoked it. Claim hook blocking
only after a live canary on that host. See
[references/install-and-hooks.md](references/install-and-hooks.md).

## Scope

This project guarantees documented normal use and common operator mistakes.
Unless explicitly required, malicious internal-state edits, permission or
immutable-file fault injection, attack-style inputs, and other workflow
violations are out of scope and must not create extra defensive systems.

License: [MIT](LICENSE). See [CHANGELOG.md](CHANGELOG.md) for changes.
