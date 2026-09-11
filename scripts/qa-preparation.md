# Approved QA preparation

`qa.prepare` can execute `sharedSteps[].command` from the approved manifest.
A command declares `executable`, `args` and `expectedExitCode`, like the target
command. `procedure` remains the human-readable preparation description.
Steps run in the order of the case's `sharedSteps` references. A text-only step
has no executable interpretation; its procedure is not a shell command.

Preparation uses the candidate worktree, or the registered repair worktree's
tracked, deleted and untracked changes. The completion observation binds the
input view, preparation commands and tool environment. Simultaneous identical
preparations share the existing path lock and reuse the completed workspace.
An interrupted preparation without its completion observation remains UNKNOWN;
it does not silently invoke the build again.

Set `independent: true` for a step whose build, installation or cache operation
must really occur independently. The operation identity then separates its
preparation from otherwise identical inputs. This does not copy an installed
project or an active run.

The target receives `FORMAL_GATES_QA_SHARED`, pointing at the shared preparation
workspace. Target input and mutable work remain in the case's own workspace.
Shared prepared files are reusable inputs; do not mutate them in an independent
case. The inherited Go dependency/tool caches are shared without copying HOME.

For this repository's fixture, an approved shared command can use:

    sh scripts/phase3-qa-fixture.sh build --source . --output bin/formal-gates

This requires POSIX shell and Go, `go.mod` and `cmd/formal-gates` in
the prepared candidate. It performs the actual build using the prepared source,
including repair overlays. It does not call legacy workflow commands. The
historical fixture `prepare` entry still targets the old workflow and must not
be selected for a stage-4 run. The `stage` entry is a committed-only export;
use the engine's prepared source for an uncommitted repair.

A completed target/collect receipt is process evidence, not a semantic PASS.
Dependencies need an accepted checkpoint PASS (or valid inherited PASS).
Evidence requiring semantic judgment must be returned through the existing
verification result contract before dependent cases can start.

`workspaceMode: "isolated"` on a case allocates an empty independent working
folder instead of another full source worktree. Use `candidate` when the case
requires its own source or Git operations (the default for older declarations).
Declare reusable output files in a shared step's `artifacts` list; missing or
changed artifacts reject reuse instead of silently treating stale files as valid.

`qa.run` is available through the same native operation tool and the CLI's
`workflow drive --operation-request` JSON. It requires the current action binding,
requestId, and empty prompt. Empty caseId selects the approved remaining set;
a caseId restricts it to that approved case. `qaCapacity` in the existing editable
configuration bounds running QA operations (default 4, range 1–64). Queueing
creates no failed case or extra attempt. Returned receipts are new observations;
pending identifies semantic judgment, dependency, resource, or recovery waits.

CHECKPOINT/final submissions may contain just newly judged cases and the exact
remaining set. The existing writer merges retained rows and findings unchanged.
New rows bind to the actual command, operation ID, stream files and exit code;
missing or conflicting evidence is rejected. Judgment remains the QA worker's
responsibility, including semantic failure when the command exits zero.

`qa.checkpoint` submits a semantic delta while the same native worker remains
attached. Use empty caseId and a prompt containing one JSON object with cases,
remaining and message (plus optional findings/invalidated). The bridge supplies
the immutable action binding; callers cannot replace it in the delta. Malformed
or conflicting deltas fail before closing the worker. Repair self-tests use the
same checkpoint storage under their repair scope; these observations do not
replace a formal QA wave. Their final RepairResult retains checkpointed cases.

## Run-owned resources (stage 4a)

The `qa.prepare` intent binds its run, candidate, action and case to `resources`.
Work, temporary and output directories live below the run's resource root;
recovery uses the same intent and never allocates a replacement for a claimed
operation. Commands and fixture subprocesses inherit:

- `FORMAL_GATES_QA_WORK`, `FORMAL_GATES_QA_TMP` and `FORMAL_GATES_QA_OUTPUT`:
  case work, temporary files and explicit deliverables.
- `FORMAL_GATES_QA_RESOURCE_ROOT`, `FORMAL_GATES_QA_RUN`,
  `FORMAL_GATES_QA_CANDIDATE`, `FORMAL_GATES_QA_TASK`: allocation and binding.
- `FORMAL_GATES_QA_SHARED` and `FORMAL_GATES_QA_CACHE`: shared preparation and
  tool cache. Ordinary steps inherit run-shared `GOCACHE`/`GOMODCACHE`;
  `independent` preparation retains the original cache configuration for real
  isolation operations. `TMPDIR`, `TMP` and `TEMP` point at allocated temporary
  space. Fixture arguments remain explicit and can use these environment paths.

Use the output directory only for intended delivery files. Before removing a
case scene, the adapter saves these files under
`artifacts/qa-output/<prepare-operation-id-without-prefix>/`. Standard streams,
exit observations and operation receipts remain under `artifacts/task-operations`; shared
preparation command evidence is in that operation's `preparation` subdirectory.
These are the durable evidence surfaces; work and temporary paths are not.

After collection and completion of the owning execution, a case scene is released only when its declared consumers
have collected and no operation on that case remains pending. Missing consumers
retain the scene. Shared preparation and caches remain available through the
run and are removed at terminal reconciliation. Seal/abort preserve the above
formal evidence and deliveries, remove run resources, and keep cleanup pending
on errors. Retrying cleanup preserves the same source record; it does not rerun
the QA command or copy an active run.
