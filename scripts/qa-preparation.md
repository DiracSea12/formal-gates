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

## Public operation handoff index (stage 4a, Git regular, no split)

Use the current action receipt for root/run/action/identity/candidate; the
candidate in a historical example is never an input. Resume the assigned native
identity with its original request ID. A changed candidate requires the engine's
new binding; retained completed cases are not rerun to make an example work.

| Entry | Dependencies and parameters | Applicable version / adaptation |
| --- | --- | --- |
| `qa.prepare` | Current action binding, approved case and shared-step commands; uses candidate plus registered repair overlay, toolchain and args | Stage 4a; preferred preparation entry. Do not call historical fixture `prepare`. |
| `qa.run` | Current action binding, stable requestId; empty caseId for approved pending set, or one approved case; empty prompt | Stage 4a; per-operation receipts and pending semantic judgments remain authoritative. |
| `qa.checkpoint` | Same native identity; empty caseId; JSON delta cases, exact remaining, message | Stage 4a; copies legal old rows through existing writer. Never rewrite retained PASS. |
| `phase3-qa-fixture.sh build` | POSIX sh, Go; `--source` engine-prepared candidate directory, `--output` binary path | Stage 4a build entry includes prepared uncommitted overlays. Other fixture entries are historical and are not legal stage-4 preparation recipes. |
| `phase3-qa-codex-run.py` | Historical Python 3 + installed Codex; `--action`, `--dispatch`, `--root`, `--binary`, `--evidence`, optional `--continue-action` | Stage 3.5c fixed comparison only; contains historical run/case assumptions and starts native work at module scope. Requires adaptation and an approved current task-operation entry before stage-4 use; never import it or execute it for discovery. |
| `qa-static-check.py` | Python 3; one or more `.py` / `.sh` paths; shell files additionally require POSIX sh | Syntax only: AST parse / `sh -n`; never import or source inspected helpers. Other languages report unsupported and require explicit adaptation. Does not verify runtime dependencies or semantic oracles. |
| `workflow show --summary` | Current `--root`, `--run-id`; optional `--after-revision` from previous response | Stage 4a; read-only incremental operation results, message paths, raw evidence roots, cost and current observations. No scheduler/result authority. Full `show` remains available. |

Native prompt files listed by the summary are the actual text sent for each
start/resume. They retain complete mandatory text for text-only roles. The
requirement's repeated derived projection is omitted from the message; the
frozen document text, path and revision remain. Task operation directories hold
request, process and observed receipt plus separate stdout/stderr; use these to
investigate an exception without copying all old passing results.

Counts refer to issued actions, not estimates of future dynamically issued
work. `newResults` counts receipt completion rather than semantic PASS. The
cursor is the accepted state revision, so an operation started before a cursor
and accepted after it is still new. Older records with no completion revision
are included only in the initial view. Cost totals are measured subtotals;
unavailable entries contain null measurements and a source reason when known.
Command wall intervals include waits and must not be added across concurrent
work. Missing timestamps remain unavailable. Preparation reuse counts only
prepare receipts, never the same marker copied into target/collect receipts.

Before handing off a helper, locate its dependencies and syntax-check its source.
Read its argument declarations rather than importing it to discover functions.
For example a helper that writes an output at module scope must not run during
inspection. A real end-to-end target still executes through qa.target after
approval; syntax PASS is never target or oracle PASS.

`observedNativeCalls` counts only retained process observations; `nativeCallSourcesUnavailable` lists claimed task journals that cannot establish a call count. Repeated-operation entries bind previous/current operation IDs and report candidate/input/checkpoint changes; when no explicit reason is recorded they say unavailable. These do not authorize another execution.
