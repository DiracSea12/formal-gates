# formal-gates

formal-gates is a lightweight AI development review workflow. It separates the
implementation worker from independent reviewers, uses the project's existing
Git, SVN, or P4 for diffs, and lets the CLI retain the results of whichever
reviews the user selected.

It activates only when the user explicitly requests formal-gates requirements
alignment, start-readiness review, formal development, post-development review,
release, or formal run seal. Ordinary delivery wrap-up, development,
explanation, informal review, and tiny changes do not enter the workflow
automatically.

## Design

- `prompts/reviewer-base.md` contains the shared independent-review contract.
- Each `gates/*.md` file is one independent review gate; its filename is its ID.
- `prompts/actions/*.md` contains requirements, readiness, QA, worker, and Carry
  roles.
- `.gates/tmp/<run-id>/state.json` is the only temporary run-state file.
- `.gates/results/<run-id>.json` is the only retained seal or abort result.

Adding or removing a valid `gates/*.md` file and reinstalling adds or removes a
gate. There is no Go registry, gate manifest, YAML front matter, weight,
dependency graph, or ordering table. After clarification, the user makes one
none, full, or custom selection from QA followed by the lexical gate list.

QA is not part of the prompt-gate catalog. When selected, QA Design first
returns the complete candidate set and an independent QA Review must pass
before development. Review failure returns the retained cases to Design rework
without consuming a post-development review wave. After development, QA
Execution and review gates may run in one parallel wave.
Start Readiness runs before development for full and custom routes. A none
route omits it unless a post-development gate addition makes the selected set
non-empty; that deferred readiness must pass before review or Seal and keeps
the current development snapshot. Preparing the development worker freezes
pre-development results and prevents QA from being added after development
starts.

After a meaning-changing requirement revision, previously approved QA cases
remain as unapproved input to a complete coverage review. The next QA Design
retains unaffected cases when impact is bounded and replaces the complete set
when it is not, then sends the complete set through independent QA Review
again. Prepared development tasks can be recomposed after normal interruption,
while a recorded semantic PASS or FAIL remains authoritative for its immutable
snapshot.

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

Start and inspect a run:

```bash
formal-gates workflow start \
  --root <repo> --package-root <installed-formal-gates> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  --vcs <git|svn|p4> --base-snapshot <base>

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
  --run-id <id> --action <requirements-clarification|start-readiness|qa-design|qa-review|development-worker|qa-execution|carry> \
  --live-snapshot <current>

formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <discovered-gate-id> --live-snapshot <current>
```

Record semantic results:

```bash
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> --confirmed

formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <none|full|custom> [--gate <gate-id> ...]

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --status PASS \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --case '<behavior>' --procedure '<public procedure>' --oracle '<expected result>'

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> \
  --live-snapshot <current> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --current-snapshot <new-current> --live-snapshot <new-current>
```

Use `formal-gates help` and `SKILL.md` for QA Execution, Carry, repair
authorization, and Seal parameters. A changed requirement revision first needs
`workflow requirement --meaning preserved|changed`; the CLI does not infer its
semantic effect.

## Diffs And Repairs

formal-gates neither reads nor stores diff bytes and never copies project
files. The worker, QA executor, and reviewers invoke the on-site VCS directly:

- The total diff is base to current. Every fresh or rerun gate reviews it.
- The repair diff is immediate pre-repair to current. Only Carry uses it to
  decide which prior gate results remain valid.
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
