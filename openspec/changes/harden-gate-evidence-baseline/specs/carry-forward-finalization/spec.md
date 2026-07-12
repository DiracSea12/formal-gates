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

## ADDED Requirements

### Requirement: Final composition distinguishes fresh and carried gates

Finalization SHALL contain one row per fixed post-development gate for the
target snapshot, marked `FRESH_PASS` or `CARRIED_PASS`. Each row SHALL bind its
source and target snapshots and evidence references. A fresh-only composition
SHALL require no Carry-Forward Arbiter.

#### Scenario: All four gates are fresh

- **WHEN** every gate has valid reviewer evidence on the target snapshot
- **THEN** composition records four `FRESH_PASS` rows.

#### Scenario: One gate is carried

- **WHEN** a gate reuses an earlier PASS
- **THEN** its row identifies the source snapshot, source artifact, closure, and
  accepted carry decision.

### Requirement: Carry is arbitrated before downstream reliance

After the target snapshot is fixed, every proposed carried prerequisite SHALL
receive a fresh Carry-Forward Arbiter decision before a fresh downstream gate
uses it. The main agent MAY propose the earliest rerun boundary but MUST NOT
authorize carry.

#### Scenario: Downstream gate starts before arbitration

- **WHEN** a fresh downstream gate depends on an undecided carried prerequisite
- **THEN** admission blocks that gate.

#### Scenario: Accepted decision reaches finalization

- **WHEN** downstream gates complete on the unchanged target snapshot
- **THEN** finalization reuses the same accepted decision without another model
  review.

#### Scenario: Deliverable changes later

- **WHEN** repair creates a new target snapshot
- **THEN** the old carry decision is stale and new arbitration is required
  before downstream reliance.

### Requirement: Arbiter decides every carried gate from the full chain

The receipt-bound Arbiter SHALL receive every hop in the transition chain and
decide each carried gate as `ACCEPT_CARRY`, `RERUN_REQUIRED`, or `BLOCKED`.
`RERUN_REQUIRED` SHALL identify the earliest fixed gate that must run on the
target snapshot. The main agent SHALL NOT override rejection.

#### Scenario: Multi-hop history is compressed

- **WHEN** an Arbiter input omits an intermediate snapshot, change surface, or
  proof reference
- **THEN** the carry decision is invalid.

#### Scenario: Different gates require different rerun points

- **WHEN** carried rows are rejected at different gates
- **THEN** the workflow reruns from the earliest rejected gate and every
  downstream gate records a fresh result.

### Requirement: FinalExecution is mechanical and format-neutral

Mechanical FinalExecution SHALL bind only the final gate matrix, any required
accepted Arbiter decisions, final verification, and release judgment for the
unchanged target snapshot. It SHALL NOT add reviewer judgment.

#### Scenario: Final gate evidence is incomplete

- **WHEN** any required gate row, accepted carry decision, or final verification
  is absent or stale
- **THEN** FinalExecution cannot seal the workflow.
