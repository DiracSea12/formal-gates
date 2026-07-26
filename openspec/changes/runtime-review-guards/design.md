# Design

## Dispatch binding in the existing run state

Add one compact dispatch record owned by each review-capable action or gate.
The record contains a generated ID, target, attempt, review wave, prompt SHA-256,
requirement revision, catalog revision, and current snapshot. Keep a run-local
set of reviewer identities already used for QA Review and gate reviews.

`prepare-action qa-review` and `prepare-gate` always create a fresh review
dispatch. Their composed prompts include the dispatch ID in the result contract.
After the host creates the zero-context reviewer and obtains its reviewer/session
identity, the main agent immediately claims the open dispatch through one small
CLI transition. Claiming reserves that identity in the run before any result can
be accepted. A claim rejects an identity reserved by any earlier dispatch,
including an interrupted dispatch that never returned a result.

Recording accepts the returned dispatch ID and reads the claimed identity and
all static bindings from the open dispatch. The mutation lock checks the claim,
dispatch, snapshot, and existing authoritative result together before recording.
An unclaimed dispatch cannot record a result. Runtime-error or interrupted retry
creates a new dispatch and requires a new claim with a fresh review identity.
The CLI stores only the identity string and claim state; no provider SDK,
session transcript, receipt, or external identity service is introduced.

Non-review actions may use the same compact prepared binding to remove repeated
source arguments, but do not enter the reviewer-identity uniqueness set. Do not
store prompt bytes or agent output in workflow state.

## Native VCS resolution

Keep the run's existing `vcs` field as the sole selection made by the main
agent. Add a small resolver interface with Git, SVN, and P4 implementations.
Each implementation shells out to the named native CLI, resolves the working
copy or client root, returns its native immutable identity, and verifies an
identity when a transition requires it.

Workflow commands resolve the live identity immediately inside the command and
compare it with the run boundary under the existing state mutation lock.
Snapshot and Seal commands no longer require the operator to repeat native
identity flags. Prepared result bindings retain the resolved identity. Native
comparison commands remain in reviewer prompts; the CLI stores neither their
output nor changed file bytes.

Use an injectable command runner for direct resolver tests. Public integration
coverage exercises the available native Git path; SVN and P4 command shapes and
failure behavior are tested at the resolver owner without requiring those
servers in normal package validation.

## Requirement artifact set

Extend requirement registration with repeatable artifact paths. Normalize paths
under the repository root, include the primary source automatically, reject
duplicates and missing files, and compute a content revision for each path.
Store the set in stable lexical order.

The first development preparation freezes the set. Every later command that
depends on confirmed requirements validates the live bytes before transition.
If any artifact changed, ordinary work stops and the existing meaning-changing
requirement flow must reopen clarification. A meaning-preserved update remains
available only before development starts.

Prompt routing presents frozen documents as acceptance inputs and separately
lists their paths as excluded VCS review targets. This is format-neutral and
does not infer OpenSpec or PRD directory conventions.

## Static and live QA cases

Add `Kind` to the existing QA case input, state record, CLI grouped flags, and
prompt rendering. Accept only `STATIC` and `LIVE`. QA Design recording rejects
an incomplete set before marking design PASS. QA Review receives kind with each
case and checks whether the complete requirement is covered at the appropriate
lowest layers. QA Execution preserves the approved kind when recording results
and still requires one result for every approved case.

Update public workflow documentation and behavior cases so `LIVE` means actual
public-entrypoint execution, not textual review or developer self-test evidence.
No dry-run case kind or phase is introduced.

## Scope and sequencing

The capabilities share run state, preparation, result recording, and prompt
routing, so this remains one bounded slice. Implement dispatch bindings first,
then native VCS resolution, then requirement freezing and QA kinds. Reuse
historical code only at the level of small ID, prompt-hash, and native-command
helpers; do not restore historical subsystems.
