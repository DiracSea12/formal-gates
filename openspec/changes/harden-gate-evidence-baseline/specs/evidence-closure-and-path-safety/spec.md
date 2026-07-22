## ADDED Requirements

Receipt files, closure manifests, gate state, and final-verification records are
run-local CLI implementation artifacts. Users do not author them, only the CLI
reads or writes them, and their typed Go structures own internal bytes without
an external field-by-field compatibility promise. The requirements below define
their integrity, lifecycle, and failure behavior. Old workflow files are
rejected after the Phase 1 cutover.

### Requirement: Each gate or requirements PASS owns one recursive evidence closure

Every requirements PASS and reviewer gate PASS SHALL own one independent
run-local closure root containing the top-level artifact, every transitive typed
evidence dependency used by that PASS, and the matching receipt when required
by the role. Typed evidence references, every `inputs[]` reference in a typed
context bundle, and role-required receipt bindings SHALL create graph edges.
Untyped text SHALL NOT create an edge. Gate state SHALL bind those PASS records
only to the closure manifest path and hash, and finalization SHALL re-verify each
accepted gate root without creating another aggregate hash root.

`FINAL_EXECUTION` is the sole exception because it is a CLI-owned mechanical
closeout, not another gate PASS. It SHALL NOT own a closure. Its `gateEvidence`
and `finalVerification` references are revalidation inputs rather than edges in
a fifth aggregate closure. The finalization state entry SHALL bind the exact
CLI-generated FinalExecution artifact path and hash and identify it as
finalization; seal SHALL re-hash that artifact and re-verify all referenced gate
closures and final verification. A FinalExecution entry SHALL NOT satisfy a gate
prerequisite.

The top-level artifact SHALL NOT refer to its receipt or closure manifest. The
CLI SHALL hash the completed artifact first, finalize a receipt that binds that
hash, then build the closure containing both. No hash computation may omit or
normalize away a receipt field because that field is absent from the artifact.

#### Scenario: Nested evidence is tampered

- **WHEN** any file reachable from a recorded evidence reference changes after
  PASS
- **THEN** downstream admission and finalization reject the dependent PASS even
  when the top-level report is unchanged.

#### Scenario: Context input is tampered

- **WHEN** the typed context bundle still matches its own hash but one listed
  input file is missing or no longer matches its declared hash
- **THEN** closure validation rejects the dependent PASS.

#### Scenario: Later gate leaves earlier closure unchanged

- **WHEN** a later gate records its own report and dependencies
- **THEN** it receives a separate closure and does not rewrite the earlier one.

#### Scenario: Receipt binds completed output without a cycle

- **WHEN** a reviewer result is finalized
- **THEN** the receipt binds the exact completed result hash and the single
  closure root subsequently includes both result and receipt.

#### Scenario: Mechanical closeout is recorded

- **WHEN** the CLI creates and records FinalExecution after all four gate roots
  and final verification pass revalidation
- **THEN** state binds the exact FinalExecution artifact hash without creating a
  fifth closure or aggregate root.

### Requirement: Closure bytes are deterministic

The closure manifest SHALL use typed Go structures with a fixed schema version,
workflow, snapshot, root role, root artifact path, and sorted entries. Each
entry SHALL contain normalized path, lowercase SHA-256, and sorted references.
Standard-library encoding SHALL produce fixed UTF-8 bytes locked by golden
fixtures. Maps, missing hashes, conflicting aliases, and reference cycles SHALL
be rejected.

#### Scenario: Equivalent input produces stable bytes

- **WHEN** the same typed closure fixture is generated repeatedly on supported
  platforms
- **THEN** its manifest bytes and SHA-256 match the golden fixture.

#### Scenario: Reference cycle is supplied

- **WHEN** typed evidence references form a cycle
- **THEN** closure construction fails before PASS state is written.

### Requirement: Evidence paths stay inside the active run

Formal evidence paths SHALL be UTF-8 relative logical paths using `/`. Empty,
`.` and `..` segments, absolute or volume-qualified paths, URI schemes,
backslashes, cross-run references, and symlink escape SHALL be rejected. The CLI
SHALL validate the logical form, resolve it beneath the active run directory,
and verify the resolved target remains inside that directory.

For every workflow, the active run directory SHALL resolve beneath
`.gates/runs/<workflow-id>`.

Repository requirement targets MAY identify review scope but MUST NOT serve as
PASS evidence unless a run-local hashed artifact records the observation.

#### Scenario: Unsafe path is rejected

- **WHEN** evidence uses an absolute path, URI, traversal, backslash, another
  workflow run, or a symlink resolving outside the active run
- **THEN** validation rejects the reference before recording state.

#### Scenario: Supported platform fixtures agree

- **WHEN** Windows and macOS/Linux path fixtures represent the same safe or
  unsafe logical case
- **THEN** the validator produces the same boundary decision.

### Requirement: Deliverable and evidence identities are separate

`changeSnapshot` SHALL cover deliverable source, tests, configuration,
requirements, and tracked project documentation. Generated gate reports,
dispatches, receipts, and run logs SHALL be excluded and protected by evidence
closure.

For Phase 3 formal flows, `changeSnapshot` SHALL be the caller-provided external
VCS identity. Formal-gates SHALL NOT store project versions. A changed-files
artifact SHALL NOT become a second target identity. Before the worker returns,
every explicitly declared delivery path SHALL be tracked by the external VCS
and present in its complete delivery diff. Generated workflow evidence remains
excluded.

A new delivery path SHALL be added to that VCS immediately before further
edits, and an existing untracked delivery path SHALL be added before it is
modified or deleted. Before a repair, every delivery path it may touch SHALL be
tracked so the VCS can fix the native pre-repair comparison boundary.

Document progress state SHALL have no authority over admission or finalization.
Editing a tracked document or its checkboxes SHALL follow the ordinary
deliverable snapshot rule; the system MUST NOT add general checkbox snapshot
normalization.

Phase 2 Design Review is a pre-development approval, not a post-development
gate PASS. Its immutable closure MAY be referenced by later-snapshot QA
Execution only for the same workflow and exact reviewed case-set hash. Changing
a case, oracle, or Case ID invalidates that approval and requires another Design
Review. This expected development boundary does not use Carry and does not make
other cross-snapshot PASS reusable.

#### Scenario: Deliverable edit changes snapshot

- **WHEN** deliverable source, test, configuration, requirement, or project
  documentation changes
- **THEN** Phase 1 requires a new snapshot, invalidates every old PASS, and
  requires fresh gate execution; carry/rerun evaluation becomes available only
  after Phase 2 implements Carry arbitration.

#### Scenario: External snapshot binding is missing

- **WHEN** a Phase 3 gate artifact omits or mismatches the caller-provided
  external VCS snapshot
- **THEN** closure construction and admission reject it before PASS state is
  written.

#### Scenario: Development follows an approved Design Review

- **WHEN** implementation changes the deliverable snapshot but the reviewed
  case set remains byte-identical
- **THEN** Phase 2 may admit that Design Review closure only through the
  same-workflow, exact-case-hash QA rule; fixed gate PASS results remain subject
  to ordinary fresh-or-carried admission.

#### Scenario: Evidence edit invalidates on same snapshot

- **WHEN** bound generated evidence changes without a deliverable edit
- **THEN** the dependent PASS is invalidated without inventing a new
  deliverable snapshot.

### Requirement: Rejection preserves prior state

The system SHALL finish all artifact, path, hash, receipt, role, verdict,
policy, and prerequisite validation before the existing authoritative state
writer is called. Any rejection SHALL leave prior state absent or
byte-identical.

The existing gate-state writer SHALL write complete JSON to a same-directory
temporary file and replace the formal state file only after that write succeeds.
Every mutating gate-state command SHALL take a cross-process same-directory
lock before loading authoritative state, reload under that lock, perform its
state-dependent validation and mutation, and keep the lock through replacement.
This requirement MUST NOT add a new persistence abstraction.

#### Scenario: Validation fails

- **WHEN** any supplied evidence or admission rule fails
- **THEN** no authoritative state byte changes.

#### Scenario: Temporary write fails

- **WHEN** writing or replacing the temporary state file fails
- **THEN** the prior formal state remains readable and is not replaced by
  partial JSON.

#### Scenario: Successful state replacement is complete

- **WHEN** all validation and the replacement succeed
- **THEN** the formal state is complete valid JSON referencing only already
  verified artifacts.

#### Scenario: Independent results finish together

- **WHEN** valid gate or Carry results concurrently record against one workflow
  state
- **THEN** every command reloads under the shared lock and no completed gate,
  history entry, or transition is lost.
