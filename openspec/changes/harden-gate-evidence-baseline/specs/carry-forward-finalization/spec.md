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
| `decisions` | array of `CarryDecision` | required; one per proposed carried gate |

The Carry payload SHALL reject legacy `dispatch` and prompt evidence fields.
Its external receipt SHALL bind and revalidate the exact final-send prompt.

Each `CarryDecision` contains exactly `gate`, `sourceSnapshot`,
`sourceGateEvidence`, `decision`, `rerunFromGate`, and `reason`.
`decision` is `ACCEPT_CARRY`, `RERUN_REQUIRED`, or `BLOCKED`.
`rerunFromGate` is a fixed gate ID only for `RERUN_REQUIRED` and is otherwise
the empty string. The target snapshot is the envelope `changeSnapshot` and is
not repeated in each decision.

The referenced transition chain contains exactly `schemaVersion=2`,
`workflowId`, `targetSnapshot`, `proposedCarriedGates`, and non-empty `hops`.
Proposed carried gates SHALL be a unique prefix of the fixed four-gate order.
Each hop contains exactly `fromSnapshot`, `toSnapshot`, `changedFiles`,
`verification`, and `repairEvidence`; the last `toSnapshot` equals the target,
adjacent hops are contiguous, and all three evidence fields are hashed
`EvidenceRef` values. This material stays under `restricted/` and is validated
by the CLI outside every reviewer prompt. The Arbiter receives the cumulative
source-to-target diff instead of the hop or repair files.

Top-level verdict is `PASS` only when every decision is `ACCEPT_CARRY`,
`BLOCKED` when any decision is `BLOCKED`, and otherwise `REVIEW`. The existing
`workflow record-transition` entrypoint is restored with typed JSON input: it
accepts only a receipt-bound PASS Arbiter artifact, validates every source gate
closure and the complete chain, creates one Arbiter closure, and records that
closure for the target snapshot without recording a fifth gate PASS. A
conflicting transition for the same workflow and target is rejected.

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

### Requirement: Arbiter decides every carried gate from the cumulative diff

The CLI SHALL validate every hop in the transition chain. The receipt-bound
Arbiter SHALL receive the complete cumulative source-to-target diff, not the
transition or repair files, and decide each carried gate as `ACCEPT_CARRY`,
`RERUN_REQUIRED`, or `BLOCKED`.
`RERUN_REQUIRED` SHALL identify the earliest fixed gate that must run on the
target snapshot. The main agent SHALL NOT override rejection.

The CLI SHALL derive the aggregate rerun point as the earliest
`rerunFromGate` in the decisions; it SHALL NOT trust a separate main-agent
summary field.

#### Scenario: Multi-hop machine history is incomplete

- **WHEN** the machine transition chain omits an intermediate snapshot, change
  surface, or proof reference
- **THEN** CLI validation rejects the carry record without exposing that chain
  to the Arbiter.

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
