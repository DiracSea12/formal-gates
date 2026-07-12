## Delivery Applicability

Phase 1 absorbs the requirements PASS defects already present in the current
local diff into the final strict-JSON cutover. It does not produce a separate
current-format checkpoint, retain the partial schema-v1 path, or add a
compatibility branch.

## ADDED Requirements

### Requirement: Requirements PASS is enforced from typed per-item data

The system SHALL reject a requirements-clarification PASS whose machine-decoded
payload or referenced alignment leaves open questions or blockers, lacks user
confirmation, fails coverage or scope preservation, names imprecise targets, or
does not have a PASS verdict for the requested workflow and snapshot.
After the Phase 1 cutover, Markdown labels SHALL NOT supply or override these
values.

#### Scenario: One invalid item is hidden among valid items

- **WHEN** any alignment item lacks a stable RQ ID or required field, has an
  illegal status, or lacks required approval
- **THEN** the complete PASS is rejected and identifies that item.

#### Scenario: Count or continuity is wrong

- **WHEN** the declared count differs from unique IDs or a prior ID disappears
  without an approved dropped decision
- **THEN** PASS is rejected before state mutation.

#### Scenario: Open blocker remains

- **WHEN** open questions or blockers are non-empty
- **THEN** the requirements gate cannot record PASS.

### Requirement: User decision evidence binds approved items

The typed decision record SHALL bind the current workflow, snapshot, alignment
artifact, approved RQ IDs, and explicit user confirmation. Dropped, deferred,
or out-of-scope IDs SHALL require item-level user approval.

The Phase 1 alignment JSON SHALL contain exactly `schemaVersion`, `workflowId`,
`changeSnapshot`, and `items`. Each item SHALL contain exactly `id`,
`requirementOrQuestion`, `source`, `whyItMatters`, `status`, `userAnswer`,
`downstreamEffect`, `documentImpact`, and `evidenceNeeded`, directly replacing
the existing Markdown item labels. `status` SHALL be `OPEN`, `CONFIRMED`,
`DEFERRED`, `DROPPED`, or `WITHDRAWN`. Every field is required; an empty
`userAnswer` is allowed only while status is `OPEN`, which cannot enter PASS.
`schemaVersion` SHALL be the integer `2`; `workflowId` and `changeSnapshot`
SHALL be non-empty strings matching the referencing requirements envelope; and
`items` SHALL be a non-empty array. Every item field other than `status` SHALL
be a string, `id` SHALL match `RQ-[0-9]{3}`, IDs SHALL be unique, and every
string SHALL be non-empty for PASS.

The Phase 1 decision JSON SHALL contain exactly `schemaVersion`, `workflowId`,
`changeSnapshot`, `decisionType`, `userConfirmation`, `userOriginal`,
`alignment`, `approvedAlignmentIds`, `approvedDroppedIds`, and
`approvalScope`. `decisionType` SHALL be `USER_CONFIRMATION`; `alignment` SHALL
be an `EvidenceRef`; both ID arrays SHALL be present; and `approvalScope` SHALL
be `requirements-clarification-gate`. The decision SHALL enumerate approved IDs
instead of retaining the Markdown `all` shorthand or alternate binding fields.
`schemaVersion` SHALL be the integer `2`; `workflowId` and `changeSnapshot`
SHALL be non-empty strings matching both the alignment and the requirements
envelope; `userConfirmation` SHALL be the boolean `true` for PASS;
`userOriginal` SHALL be a non-empty string; and both ID arrays SHALL contain
unique `RQ-[0-9]{3}` strings. Neither object accepts unknown fields or `null`.

For PASS, `approvedAlignmentIds` SHALL equal the complete set of IDs still
present in `items`, including items whose approved disposition is `DEFERRED`,
`DROPPED`, or `WITHDRAWN`. `DEFERRED` means the user postponed the item,
`DROPPED` means the user placed it out of scope, and `WITHDRAWN` means the
clarification question was retracted; each requires a non-empty `userAnswer`
recording that disposition. `approvedDroppedIds` has a different purpose: it
SHALL equal the set of IDs present in `previousAlignment` but absent from the
current alignment, and SHALL be empty on the first run or when no prior ID was
removed. No current item ID may appear in `approvedDroppedIds`.

The requirements payload's `droppedQuestionIds` SHALL equal that same removed-ID
set. `droppedQuestionApproval` SHALL be `true` exactly when the set is non-empty
and `false` when it is empty.

#### Scenario: Decision approval is partial

- **WHEN** one required alignment ID is absent from the user decision
- **THEN** PASS is rejected even if the remaining IDs are approved.

#### Scenario: Dropped ID lacks approval

- **WHEN** an alignment omits a prior ID without explicit approval in the
  decision record
- **THEN** continuity validation rejects the artifact.

#### Scenario: One structured item is malformed

- **WHEN** an alignment item omits one former Markdown field, uses an unknown
  field, or has an illegal status
- **THEN** the complete alignment is rejected before requirements PASS.

### Requirement: Requirement targets are format-neutral and precise

Covered targets SHALL be non-empty, repository-relative, precise document or
bundle paths. Absolute paths, wildcards, repository roots, and broad top-level
directories SHALL be rejected. OpenSpec, PRD, SDD, issue exports, and ordinary
Markdown SHALL use the same target rules.

Narrative alignment items SHALL use `Document impact:` and SHALL remain neutral
to the source document format.

#### Scenario: Generic requirement bundle is accepted

- **WHEN** a precise relative PRD or Markdown bundle target is supplied and all
  other typed requirements evidence is valid
- **THEN** the target is accepted without an OpenSpec path.

#### Scenario: Broad target is rejected

- **WHEN** a target is `.`, a repository root, a wildcard, an absolute path, or
  an unrelated top-level directory
- **THEN** PASS is rejected as imprecise.

#### Scenario: Scope target is used as proof

- **WHEN** a repository document target is supplied where a run-local hashed
  evidence reference is required
- **THEN** the reference is rejected even when the target exists.

### Requirement: Old requirements machine formats are not compatible

Structured alignment and decision data SHALL be the only requirements machine
truth. The system MUST NOT parse the former Markdown field contract or migrate
an old workflow in place.

#### Scenario: Old workflow is presented

- **WHEN** requirements evidence uses the old machine format
- **THEN** validation requires a new workflow and records no synthesized PASS.
