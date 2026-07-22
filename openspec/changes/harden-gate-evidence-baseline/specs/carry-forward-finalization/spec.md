## Delivery Applicability

Phase 1 mechanical FinalExecution supports four current-snapshot gate evidence
packages only; each matrix row contains just `gate` and `gateEvidence`, where
`gateEvidence` references that gate's immutable closure. It does not define
result kind, source/target snapshots, Carry Arbiter JSON, a carried matrix row,
or a disabled carry role.

Phase 2 adds Carry-Forward Arbiter, `CARRIED_PASS`, transition-chain admission,
and extends each FinalExecution matrix row with result kind, source/target
snapshots, and carry-decision evidence together with their JSON content, policy,
validators, and positive and negative tests. The common envelope remains
unchanged.

The Arbiter uses policy `carry.arbiter.v2`, role `CARRY_ARBITER`, gate
`qa-test-gate`, stage `Carry`, and flow `carry`. It requires a receipt and uses
the closed payload below rather than the shared reviewer checks:

| Field | Type | Rule |
|---|---|---|
| `contextBundle` | `EvidenceRef` | required machine-only binding; never reviewer input |
| `reviewPolicyId` | string | exactly `carry.arbiter.v2` |
| `transitionChain` | `EvidenceRef` | required machine-only chain; typed below and validated by CLI |
| `decisions` | array of `CarryDecision` | required; one per eligible prior PASS gate |

The Carry payload SHALL reject legacy `dispatch` and prompt evidence fields.
Its external receipt SHALL bind and revalidate the exact final-send prompt.

Each `CarryDecision` contains exactly `gate`, `sourceSnapshot`,
`sourceGateEvidence`, `decision`, and `reason`.
`decision` is `ACCEPT_CARRY`, `RERUN_REQUIRED`, or `BLOCKED`.
The target snapshot is the envelope `changeSnapshot` and is not repeated in
each decision.

Receipt registration SHALL generate the Carry catalog and immutable proof.
It SHALL populate the envelope, policy, transition reference, eligible gate
catalog, source snapshots, and source evidence from CLI-validated inputs. The
caller SHALL provide only repeatable source closure paths; registration SHALL
verify each closure and derive its fixed gate and source snapshot rather than
accepting a caller-authored gate/path binding. Transition-chain composition
SHALL receive one ordered group of source snapshot, target snapshot,
changed-files path, and verification path per hop and SHALL generate the typed
chain. Missing, mismatched, duplicate, or invalid input SHALL be rejected before
the chain or proof is written. The
Arbiter SHALL pass one ordered `decision` and `reason` pair per generated gate
to `receipt submit`; it SHALL NOT edit Carry JSON or repeat the gate/static
bindings. Submission SHALL reject invalid or incomplete values before changing
the artifact and SHALL atomically record its hash/proof. Finalization SHALL
require that proof and mechanically write the top-level verdict and final
artifact; no AI actor may author or repeat the static fields.

The referenced transition chain contains exactly `schemaVersion=2`,
`workflowId`, `targetSnapshot`, and non-empty `hops`. Each hop contains exactly
`fromSnapshot`, `toSnapshot`, `changedFiles`, and `verification`; the last
`toSnapshot` equals the target, adjacent hops are contiguous, and both evidence
fields are hashed `EvidenceRef` values. This material stays under `restricted/`
and is validated by the CLI outside every reviewer prompt. The CLI owns the
workflow chain rather than project changes. The Arbiter receives only the
worktree and exact native VCS comparison target for the newest adjacent repair,
excluding unrelated local worktree changes, instead of the hop, receipt, prior
Carry decisions, or repair-history files.

For successive repairs, the machine chain retains every contiguous snapshot hop
and accepted decision needed to prove how earlier gate evidence
reaches the new target. The CLI validates that provenance outside the prompt;
the new Arbiter judges only the newest adjacent hop. `CarryDecision.sourceSnapshot`
continues to identify the source gate evidence snapshot and MAY predate the
current hop's `fromSnapshot`; those two meanings SHALL NOT be conflated.

Top-level verdict is `PASS` when every decision is terminal
(`ACCEPT_CARRY` or `RERUN_REQUIRED`) and `BLOCKED` when any decision is
`BLOCKED`; an incomplete decision set is invalid. The existing
`workflow record-transition` entrypoint is restored with typed JSON input: it
accepts only a receipt-bound terminal `PASS` Arbiter artifact, validates every
source gate closure and the complete chain, creates one Arbiter closure, and
records that closure for the target snapshot without recording a fifth gate
PASS. A conflicting transition for the same workflow and target is rejected.

## ADDED Requirements

### Requirement: Final composition distinguishes fresh and carried gates

Finalization SHALL contain one row per fixed post-development gate for the
target snapshot, marked `FRESH_PASS` or `CARRIED_PASS`. Each row SHALL bind its
source and target snapshots and evidence references. A fresh-only composition
SHALL require no Carry-Forward Arbiter.

Each Phase 2 matrix row contains exactly `gate`, `resultKind`,
`sourceSnapshot`, `targetSnapshot`, `gateEvidence`, and optional
`carryDecision`. `FRESH_PASS` requires equal source and target snapshots, a
current-target gate closure, and omitted `carryDecision`. `CARRIED_PASS`
requires different source and target snapshots, the source gate closure, and a
`carryDecision` reference to the accepted Arbiter closure that names the same
gate, source evidence, and target snapshot.

#### Scenario: All four gates are fresh

- **WHEN** every gate has valid reviewer evidence on the target snapshot
- **THEN** composition records four `FRESH_PASS` rows.

#### Scenario: One gate is carried

- **WHEN** a gate reuses an earlier PASS
- **THEN** its row identifies the source snapshot, source artifact, closure, and
  accepted carry decision.

### Requirement: Carry is arbitrated per gate before final composition

After the target snapshot is fixed, every eligible prior PASS gate SHALL receive
a fresh Carry-Forward Arbiter decision before final composition. The main agent
MAY supply transition source paths to the CLI, but the CLI SHALL generate their
static chain and bindings. The main agent MUST NOT author the formal chain or
authorize carry or rerun.

Before `receipt register` admits a post-development gate at a new target
snapshot, the CLI SHALL check the active workflow for an older-snapshot PASS for
that same gate. If one exists, a recorded terminal Carry transition for the
target SHALL explicitly decide `RERUN_REQUIRED` for that gate; `ACCEPT_CARRY`,
`BLOCKED`, or no decision SHALL reject registration before its artifact or
proof is written. If no prior PASS exists for that gate in the active workflow,
the first execution MAY register normally. Mechanical QA Execution composition
at a new snapshot SHALL apply the same rule to an older-snapshot QA Execution
PASS before writing any output or proof.

During repair, the orchestrator MAY prepare source-closure selection, context
inputs, and immutable command shape in parallel with the worker. Carry
registration, dispatch, and judgment SHALL wait until the worker fixes the exact
post-repair VCS snapshot and the CLI composes the exact transition. A mutable
future reference, waiting Arbiter, or two-phase judgment SHALL NOT be used.

#### Scenario: Final composition starts before arbitration

- **WHEN** final composition depends on an undecided carried gate
- **THEN** finalization blocks that workflow.

#### Scenario: New-snapshot rerun lacks terminal Carry permission

- **WHEN** an active workflow has an older-snapshot PASS for a gate and the
  caller registers that gate, or composes QA Execution, at a new snapshot
  without a terminal target transition deciding `RERUN_REQUIRED` for it
- **THEN** the CLI rejects before writing the new artifact or proof; an
  `ACCEPT_CARRY`, `BLOCKED`, or absent decision does not permit rerun.

#### Scenario: Repair preparation overlaps implementation safely

- **WHEN** the worker is still repairing while the orchestrator prepares source
  closures, context inputs, and immutable command shape
- **THEN** no Carry registration, dispatch, or judgment occurs until the exact
  post-repair VCS snapshot and CLI-composed transition exist.

#### Scenario: Accepted decision reaches finalization

- **WHEN** the target snapshot remains unchanged after arbitration
- **THEN** finalization reuses the same accepted per-gate decisions without
  another model review.

#### Scenario: Deliverable changes later

- **WHEN** repair creates a new target snapshot
- **THEN** the old carry decision is stale and new arbitration is required
  before final composition.

### Requirement: Arbiter decides every carried gate from a native VCS comparison

The CLI SHALL validate every hop and accepted prior transition in the machine
chain. The receipt-bound Arbiter SHALL receive only the worktree and exact
pre-repair and post-repair snapshot identities, invoke the on-site VCS directly
to compare them, and decide each eligible prior PASS gate as `ACCEPT_CARRY`,
`RERUN_REQUIRED`, or `BLOCKED`. It SHALL NOT receive the transition, receipt,
prior decision, repair history, or unrelated local worktree changes. The main
agent SHALL NOT override or aggregate those
individual decisions.

Before changing a delivery path for a repair, the worker or orchestrator SHALL
ensure the path is tracked and use the named external VCS's native state or
checkpoint facility to fix the pre-repair snapshot. After the repair, it SHALL
fix the post-repair snapshot and verify that the same VCS can compare those two
states directly. Formal-gates SHALL retain only workflow snapshots, static
evidence, and decisions. If the exact native comparison is unavailable, Carry SHALL NOT be
proposed and an affected gate SHALL NOT enter a new-snapshot rerun without a
terminal `RERUN_REQUIRED` decision.

#### Scenario: Multi-hop machine history is incomplete

- **WHEN** the machine transition chain omits an intermediate snapshot, change
  surface, or proof reference
- **THEN** CLI validation rejects the carry record without exposing that chain
  to the Arbiter.

#### Scenario: Gate evidence predates the current repair source

- **WHEN** a gate passed fresh on `T1`, was validly carried to `T2`, and the
  current repair creates `T3`
- **THEN** the CLI validates the accepted `T1`-to-`T2` provenance while the new
  Arbiter directly compares native VCS snapshots `T2` and `T3`.

#### Scenario: Different gates receive different decisions

- **WHEN** the Arbiter accepts carry for some gates and requires reruns for
  others
- **THEN** the accepted gates remain eligible for `CARRIED_PASS` and only the
  named gates record fresh target-snapshot results.

#### Scenario: Arbiter changes a generated source binding

- **WHEN** the Arbiter edits an eligible gate, source snapshot, source closure,
  transition reference, policy, or aggregate verdict in its generated template
- **THEN** finalization rejects the static projection and records no Carry
  closure or transition.

### Requirement: FinalExecution is mechanical and format-neutral

Mechanical FinalExecution SHALL bind only the final gate matrix, any required
accepted Arbiter decisions, final verification, and release judgment for the
unchanged target snapshot. It SHALL NOT add reviewer judgment.

#### Scenario: Final gate evidence is incomplete

- **WHEN** any required gate row, accepted carry decision, or final verification
  is absent or stale
- **THEN** FinalExecution cannot seal the workflow.
