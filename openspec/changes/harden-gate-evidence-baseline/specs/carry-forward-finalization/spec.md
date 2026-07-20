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
changed-files path, verification path, and repair-evidence path per hop and
SHALL generate the typed chain. Missing, mismatched, duplicate, or invalid path
input SHALL be rejected before the chain or proof is written. The
Arbiter SHALL pass one ordered `decision` and `reason` pair per generated gate
to `receipt submit`; it SHALL NOT edit Carry JSON or repeat the gate/static
bindings. Submission SHALL reject invalid or incomplete values before changing
the artifact and SHALL atomically record its hash/proof. Finalization SHALL
require that proof and mechanically write the top-level verdict and final
artifact; no AI actor may author or repeat the static fields.

The referenced transition chain contains exactly `schemaVersion=2`,
`workflowId`, `targetSnapshot`, and non-empty `hops`. Each hop contains exactly
`fromSnapshot`, `toSnapshot`, `changedFiles`,
`verification`, and `repairEvidence`; the last `toSnapshot` equals the target,
adjacent hops are contiguous, and all three evidence fields are hashed
`EvidenceRef` values. This material stays under `restricted/` and is validated
by the CLI outside every reviewer prompt. The Arbiter receives the cumulative
repair diff from the pre-repair snapshot to the post-repair snapshot, excluding
unrelated local worktree changes, instead of the hop, receipt, or repair-history
files.

For successive repairs, the chain starts at the most recent fresh-PASS baseline
and includes every hop through the new target. A target-to-new-target-only chain
cannot carry a gate whose source evidence predates that hop; the complete
cumulative chain is required for the next independent per-gate decision.

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

#### Scenario: Final composition starts before arbitration

- **WHEN** final composition depends on an undecided carried gate
- **THEN** finalization blocks that workflow.

#### Scenario: Accepted decision reaches finalization

- **WHEN** the target snapshot remains unchanged after arbitration
- **THEN** finalization reuses the same accepted per-gate decisions without
  another model review.

#### Scenario: Deliverable changes later

- **WHEN** repair creates a new target snapshot
- **THEN** the old carry decision is stale and new arbitration is required
  before final composition.

### Requirement: Arbiter decides every carried gate from the repair diff

The CLI SHALL validate every hop in the transition chain. The receipt-bound
Arbiter SHALL receive the cumulative diff produced by the repair from the
pre-repair snapshot to the post-repair snapshot, not the transition, receipt,
or repair-history files and not unrelated local worktree changes, and decide
each eligible prior PASS gate as `ACCEPT_CARRY`, `RERUN_REQUIRED`, or `BLOCKED`.
The main agent SHALL NOT override or aggregate those individual decisions.

Phase 2.5 relies on an exact pre-repair source that was preserved before the
repair. If that source is unavailable, Carry SHALL NOT be proposed and the
affected gate SHALL run fresh. Phase 3 SHALL replace this operational
precondition with CLI-persisted source/target dirty-tree identities and an
exact diff mechanically derived and revalidated from those trees; old hash-only
snapshots SHALL NOT be represented as tree-bound.

#### Scenario: Multi-hop machine history is incomplete

- **WHEN** the machine transition chain omits an intermediate snapshot, change
  surface, or proof reference
- **THEN** CLI validation rejects the carry record without exposing that chain
  to the Arbiter.

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
