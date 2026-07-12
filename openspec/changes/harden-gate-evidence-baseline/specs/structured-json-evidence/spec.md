## Delivery Applicability

Phase 1 migrates only the formal roles that already exist and are enabled:
requirements PASS, QA Execution, complexity, architecture, code quality, and
mechanical FinalExecution. Design Review, optional White-box Adequacy, and
Carry-Forward Arbiter are not registered, accepted, or represented by
placeholder payloads in Phase 1. A later phase adds each role or stage only
with its JSON content, policy, domain validator, and positive and negative
tests in the same task. The envelope remains common.

## ADDED Requirements

### Requirement: Public and internal JSON ownership stays narrow

Public reviewer, requirements, context-bundle, FinalExecution, and `policy show --format json` structures SHALL have closed machine contracts.

Their owning specifications define closed fields. Receipt files, closure manifests,
gate state, and final-verification records SHALL be run-local CLI implementation
artifacts: users SHALL NOT author them, only the CLI SHALL read or write them,
and the cutover SHALL reject old workflow files. Typed Go structures and
behavior tests SHALL own their internal bytes. Requirements for those internal
files SHALL describe integrity, lifecycle, and failure behavior without
creating field-by-field external compatibility.

#### Scenario: User supplies a CLI-internal artifact

- **WHEN** a caller attempts to hand-author or import a receipt, closure, gate
  state, or final-verification record as a supported public input
- **THEN** the CLI rejects that unsupported path rather than treating its wire
  fields as a public compatibility contract.

### Requirement: Formal machine truth is one closed JSON envelope

Every Phase 1 formal artifact SHALL use schema version `2` and contain exactly
`schemaVersion`, `artifactRole`, `workflowId`, `changeSnapshot`, `gate`,
`stage`, `verdict`, and `payload`. Object key order SHALL NOT affect
admission. Every envelope field is required. Reviewer-role `verdict` SHALL be
`PASS`, `REVIEW`, `FAIL`, or `BLOCKED`. `REQUIREMENTS_PASS` and
`FINAL_EXECUTION` SHALL accept only `PASS`.

The Phase 1 role combinations SHALL be exactly:

| `artifactRole` | `gate` | `stage` | payload |
|---|---|---|---|
| `REQUIREMENTS_PASS` | `requirements-clarification-gate` | `""` | requirements |
| `QA_REVIEW` | `qa-test-gate` | `Execution` | reviewer |
| `COMPLEXITY_REVIEW` | `complexity-gate` | `""` | reviewer |
| `ARCHITECTURE_REVIEW` | `architecture-health-gate` | `""` | reviewer |
| `CODE_QUALITY_REVIEW` | `code-quality-gate` | `""` | reviewer |
| `FINAL_EXECUTION` | `qa-test-gate` | `FinalExecution` | final execution |

Phase 1 SHALL NOT contain `route`, `nextAction`, `reworkOwner`, or `rerunFrom`.
The CLI SHALL derive admission from `artifactRole` and `verdict`. Unresolved
requirements SHALL continue clarification without producing a
`REQUIREMENTS_PASS` artifact. A failed final closeout SHALL return an error
without producing `FINAL_EXECUTION`. Reviewer `REVIEW`, `FAIL`, and `BLOCKED`
results MAY report findings but SHALL NOT create formal PASS state. Findings
and human-readable output MAY describe rework, but SHALL NOT supply a machine
routing field. Rerun-boundary behavior is outside Phase 1 and SHALL be defined
only with Phase 2 Carry arbitration and transitions.

The semantic owner SHALL write its own typed JSON; in particular, a reviewer
SHALL write its review judgment. The existing Go CLI SHALL be the only
authoritative decoder, validator, and state-recording entrypoint and SHALL NOT
generate or rewrite reviewer judgments. Typed Go structs and the standard
library SHALL define output bytes for deterministic mechanical artifacts owned
by the CLI, including receipts, closure manifests, state, and finalization
output. The decoder SHALL reject invalid UTF-8, duplicate keys, unknown fields
at every level, wrong types, trailing JSON values, old schema, and
role/gate/stage conflicts before domain validation. Required arrays SHALL be
present even when empty; `null` SHALL be rejected; and optional fields SHALL be
omitted rather than emitted as `null`. A semantic owner's artifact receipt SHALL
hash the exact submitted bytes without canonicalizing key order. Markdown MAY
explain a result but MUST NOT satisfy, complete, or override a machine field.
Reviewer output SHALL NOT require a separate artifact-generation command,
intermediate schema, or conversion layer.

#### Scenario: Markdown cannot complete JSON

- **WHEN** required JSON evidence is missing but matching Markdown labels exist
- **THEN** recording rejects the artifact without reading those labels as truth.

#### Scenario: Future stage is sent during Phase 1

- **WHEN** QA evidence names Design Review or White-box Adequacy, or an artifact
  names Carry Arbiter, before its owning Phase 2 task is delivered
- **THEN** role dispatch rejects the unknown role/stage without a disabled-role
  or compatibility branch.

#### Scenario: Ambiguous JSON is rejected

- **WHEN** an envelope contains a duplicate key, unknown field, wrong type,
  invalid UTF-8, trailing value, old schema, `null`, or role conflict
- **THEN** the CLI rejects the whole artifact before state mutation.

#### Scenario: Removed route is supplied

- **WHEN** an envelope or payload supplies `route`, `nextAction`, `reworkOwner`,
  or `rerunFrom`
- **THEN** strict unknown-field validation rejects the artifact rather than
  accepting a compatibility form.

#### Scenario: Reviewer non-PASS result cannot create PASS state

- **WHEN** a valid Phase 1 reviewer artifact has verdict `REVIEW`, `FAIL`, or
  `BLOCKED`
- **THEN** the result may report findings but cannot record formal PASS.

#### Scenario: Operational pass role uses a non-PASS verdict

- **WHEN** `REQUIREMENTS_PASS` or `FINAL_EXECUTION` has verdict `REVIEW`,
  `FAIL`, or `BLOCKED`
- **THEN** role validation rejects the artifact without producing a pass
  artifact or changing state.

### Requirement: Evidence references and findings have one machine form

Every `EvidenceRef` SHALL contain exactly a normalized run-relative `path` and
lowercase 64-hex `sha256`. Every `Location` SHALL contain exactly a non-empty
repository-relative `path`, positive integer `startLine`, and integer `endLine`
not before `startLine`. Every `Finding` SHALL contain exactly a non-empty
`message` and `locations[]`. The array SHALL be present and MAY be empty when a
finding is not tied to a source line.

Paths and closure semantics remain owned by
`evidence-closure-and-path-safety`; this capability only decodes the typed
objects.

#### Scenario: Free-form evidence text is not a reference

- **WHEN** a message or Markdown explanation names a file without an
  `EvidenceRef`
- **THEN** the decoder does not treat the text as proof or a closure edge.

### Requirement: Four-gate reviews use one reviewer payload

QA Execution, complexity, architecture, and code quality SHALL use the same
reviewer payload containing exactly:

| Field | Type | Rule |
|---|---|---|
| `dispatch` | `EvidenceRef` | required |
| `contextBundle` | `EvidenceRef` | required; points to the typed initial-input bundle defined below |
| `reviewPolicyId` | string | required and non-empty; known by typed policy |
| `checks` | array of `Check` | required |
| `changedFiles` | `EvidenceRef` | required post-development; otherwise omitted |
| `verification` | `EvidenceRef` | required post-development; otherwise omitted |

Each `Check` SHALL contain exactly `id`, `status`, `message`, `evidenceRefs`,
and `findings`. `id` and `message` SHALL be non-empty. `status` SHALL be
`PASS`, `REVIEW`, `FAIL`, `BLOCKED`, or `NOT_APPLICABLE`. `evidenceRefs[]` and
`findings[]` SHALL always be present. A matching reviewer receipt SHALL NOT be
a reviewer-payload field: the CLI hashes completed reviewer JSON first, then
the receipt binds that hash, and the closure contains both.

The reviewer payload SHALL NOT contain `reviewSessionId` or another
self-reported identity field. The external receipt SHALL bind the
system-generated dispatch ID, host-captured subagent ID, distinct review output
path, exact output hash, workflow, gate, stage, and snapshot.

The orchestrating caller SHALL write and validate `contextBundle` before
dispatch, and the reviewer SHALL reference that exact unchanged bundle. The
JSON referenced by `contextBundle` SHALL contain exactly `bundleVersion`,
`workflowId`, `changeSnapshot`, and `inputs`. `bundleVersion` SHALL be integer
`1`; workflow and snapshot SHALL match the reviewer envelope; and `inputs`
SHALL be a non-empty array of `EvidenceRef`. Unknown or duplicate fields and
duplicate normalized input paths SHALL be rejected. The CLI SHALL verify every
listed path and hash, and every `inputs[]` item SHALL become an evidence-closure
edge. No second input manifest, untyped text-list parser, or context-bundle
generation command SHALL be added.

The typed Go policy catalog for the selected gate SHALL define required check
IDs and which checks, if any, allow `NOT_APPLICABLE`. The CLI SHALL reject
missing, unknown, or duplicate IDs, unknown status, and unapproved
`NOT_APPLICABLE`. `NOT_APPLICABLE` SHALL require a reason in `message`. Any
`REVIEW`, `FAIL`, or `BLOCKED` SHALL prevent aggregate PASS. The CLI SHALL
recompute the aggregate and compare the envelope verdict; messages SHALL never
determine it.

`reviewPolicyId` SHALL select one of the reviewer policies exported by
`policy show`; its artifact role, gate, and stage SHALL match the envelope.
Unknown or mismatched policy IDs SHALL be rejected before check aggregation.

Every reviewer policy SHALL require `review.prompt-fields` and
`review.prompt-semantics` in addition to the gate-specific IDs below. The only
Phase 1 check allowed to use `NOT_APPLICABLE` SHALL be
`complexity.statistics` under policy `complexity.start-readiness.v2`.

#### Scenario: One dispatched input changes

- **WHEN** a file listed in the typed context bundle no longer matches its hash
- **THEN** the reviewer artifact and every PASS closure depending on it are
  rejected even when the context-bundle file itself is unchanged.

#### Scenario: Required check is missing

- **WHEN** a reviewer artifact omits a check required by its gate policy
- **THEN** the artifact is invalid and cannot record PASS.

#### Scenario: Review result contradicts top-level PASS

- **WHEN** any check is `REVIEW`, `FAIL`, or `BLOCKED` while the envelope says
  `PASS`
- **THEN** aggregation rejects the artifact before recording.

### Requirement: Existing reviewer fields have one explicit migration

The Phase 1 cutover SHALL map existing Markdown machine fields as follows:

| Existing field | JSON destination |
|---|---|
| workflow, snapshot, gate, stage, verdict | envelope |
| `gate_route` action, owner, and rerun fields | deleted; behavior is derived from `artifactRole + verdict` |
| `Dispatch prompt artifact` | `payload.dispatch` |
| declared hashed initial review inputs / `Context bundle` | `payload.contextBundle` |
| `Prompt source` and formal mode | `payload.reviewPolicyId` |
| changed-files/raw-diff field | `payload.changedFiles` |
| verification/developer-self-test field | `payload.verification` |
| prompt label scan | `review.prompt-fields` check |
| semantic anti-anchor result | `review.prompt-semantics` check |
| reviewer receipt | closure dependency outside reviewer JSON |

`Zero-context reviewer`, `Independent agent`, and `No-anchor prompt` SHALL be
deleted as self-certification fields and SHALL NOT receive replacement JSON
booleans.

The gate-specific fields SHALL map to these policy-owned check IDs:

| Gate | Check IDs |
|---|---|
| QA Execution | `qa.approved-case-set`, `qa.owned-results`, `qa.case-result-binding` |
| Complexity | `complexity.statistics`, `complexity.diff-shape`, `complexity.impact-surface`, `complexity.public-config-surface`, `complexity.new-concepts`, `complexity.minimum-sufficient`, `complexity.shrink-opportunities` |
| Architecture | `architecture.boundaries`, `architecture.ownership`, `architecture.public-surface`, `architecture.state-lifecycle`, `architecture.dependencies`, `architecture.failure-semantics`, `architecture.performance`, `architecture.decoupling` |
| Code quality | `code-quality.correctness`, `code-quality.maintainability`, `code-quality.performance`, `code-quality.test-quality`, `code-quality.dead-code`, `code-quality.overfitting`, `code-quality.validation-encoding`, `code-quality.verification`, `code-quality.residual-risk` |

The old field's explanatory text SHALL become the matching check `message`,
its proof SHALL become `evidenceRefs`, and its concrete issues SHALL become
`findings`. Complexity `Decision evidence` SHALL be distributed to the
affected checks' evidence references. The envelope verdict SHALL determine
machine admission; findings and human-readable output SHALL describe required
rework. No gate-specific judgment or
evidence field SHALL be added to the reviewer payload.

#### Scenario: Old judgment field is supplied

- **WHEN** a reviewer payload supplies a superseded top-level blocker, risk,
  minimum-sufficiency, QA binding, or self-certification field
- **THEN** strict unknown-field validation rejects it rather than accepting a
  compatibility form.

#### Scenario: Complexity report contains development budget data

- **WHEN** the `complexity.statistics` check references a report containing
  budget data, a non-`none` budget source, or any budget override
- **THEN** the owning complexity validator rejects PASS.

Direct or transitive `restricted/` path enforcement, including development-time
budget material referenced outside the statistics report, is enabled by the
Phase 2 reviewer-isolation task rather than this Phase 1 contract.

### Requirement: Phase 1 operational payloads retain only current data

The Phase 1 requirements payload SHALL contain exactly:

| Field | Type | Rule |
|---|---|---|
| `requirementSource` | string | required and non-empty |
| `alignment` | `EvidenceRef` | required |
| `totalAlignmentItems` | positive integer | required |
| `previousAlignment` | `EvidenceRef` | omitted only on the first run |
| `openQuestionIds` | array of strings | required |
| `openBlockers` | array of strings | required |
| `droppedQuestionIds` | array of strings | required |
| `droppedQuestionApproval` | boolean | required |
| `userConfirmation` | boolean | required |
| `coverageScan` | `PASS` | required for PASS |
| `scopePreservation` | `PassOrNA` | required |
| `taskProof` | `PassOrNA` | required |
| `dimensionCoverage` | array of `DimensionCoverage` | required |
| `decision` | `EvidenceRef` | required |
| `coveredTargets` | array of strings | required |
| `downstreamPermission` | `READY_TO_DRAFT` | required for PASS |

`PassOrNA` SHALL contain exactly `status` and `message`; status SHALL be `PASS`
or `NOT_APPLICABLE`, and the latter SHALL require a non-empty message.
`DimensionCoverage` SHALL contain exactly `id`, `status`, `alignmentIds`, and
`message`; status SHALL be `COVERED`, `DEFERRED`, or `NOT_APPLICABLE`, and
`alignmentIds[]` SHALL be present. `dimensionCoverage` SHALL contain each of
these IDs exactly once and no other ID: `DIM-01` goal, `DIM-02` user/value,
`DIM-03` scope, `DIM-04` non-goals, `DIM-05` acceptance, `DIM-06` evidence,
`DIM-07` constraints, `DIM-08` architecture boundary, `DIM-09` requirement
details, `DIM-10` unknowns, `DIM-11` task status, `DIM-12` phase dependency, and
`DIM-13` must-not-cut scope. Every `alignmentIds` array SHALL be non-empty,
contain unique IDs, and reference current alignment items; one alignment item
may cover more than one dimension. `message` SHALL be a string and SHALL be
non-empty for `DEFERRED` or `NOT_APPLICABLE`. An empty, incomplete, duplicate,
or unknown dimension catalog SHALL be rejected. The requirements capability
owns the typed alignment and decision contents and their domain rules.

The Phase 1 FinalExecution payload SHALL contain exactly `mode`, `gateMatrix`,
`finalVerification`, and `releaseJudgment`. `mode` SHALL be
`MECHANICAL_CLOSEOUT`; `finalVerification` SHALL be an `EvidenceRef`; and
`releaseJudgment` SHALL be `SEAL` for PASS. `gateMatrix` SHALL contain exactly
one row for each fixed post-development gate. Each Phase 1 row SHALL contain
exactly `gate` and `gateEvidence`; `gateEvidence` SHALL be an `EvidenceRef` to
that gate's immutable closure. The CLI SHALL re-verify that closure's top-level
gate, PASS verdict, workflow, envelope snapshot, role-required receipt, and all
transitive evidence. It SHALL NOT reference the mutable gate-state file or
create a separate gate-record artifact. FinalExecution SHALL NOT own another
closure: its `gateEvidence` and `finalVerification` references are revalidation
inputs for mechanical closeout, not edges in an aggregate root. The finalization
state entry SHALL bind the exact CLI-generated FinalExecution artifact path and
hash and SHALL NOT satisfy a gate prerequisite.

Phase 1 SHALL NOT define a Carry Arbiter payload, a carried matrix row, a Design
Review payload, or a White-box payload. Phase 2 adds each only with its complete
domain feature. FinalExecution MUST NOT require or accept task-checkbox
evidence.

#### Scenario: Operational role impersonates reviewer

- **WHEN** requirements or FinalExecution supplies reviewer fields
- **THEN** strict role validation rejects the payload.

#### Scenario: Phase 1 FinalExecution contains carry fields

- **WHEN** a Phase 1 matrix row supplies `resultKind`, `sourceSnapshot`,
  `targetSnapshot`, a Carry Arbiter decision, or any carried result
- **THEN** strict role validation rejects the unimplemented feature.

#### Scenario: Phase 1 reuses a gate from another snapshot

- **WHEN** a Phase 1 gate row or transition attempts to reuse PASS evidence
  bound to a different snapshot
- **THEN** admission rejects it until Phase 2 delivers Carry arbitration and
  cross-snapshot admission.

### Requirement: Format dispatch does not own domain rules

After strict decoding, role dispatch SHALL invoke only the owning validators
for roles enabled in the current phase. A structurally valid envelope MUST NOT
authorize PASS by itself. Adding a later role or stage SHALL update its typed
payload, policy, domain validator, and tests in one phase; the decoder SHALL NOT
pre-register it.

#### Scenario: Domain rule rejects valid JSON shape

- **WHEN** JSON structure is valid but an owning domain rule fails
- **THEN** the domain validator rejects the artifact with its stable rule ID and
  no state is written.

### Requirement: Operational surfaces move with their enabled role

Phase 1 SHALL atomically update every producer and consumer of requirements,
QA Execution, complexity, architecture, code quality, and mechanical
FinalExecution: agent templates, references, examples, canaries, fixtures,
policy output, artifact tests, and operational documentation. Superseded fields
and parsers MUST NOT remain as compatibility paths.

A later role or stage SHALL update its own producing and consuming surfaces in
the phase that enables it; its future vocabulary SHALL NOT be required from the
Phase 1 cutover.

#### Scenario: Phase 1 completion scan retains old machine vocabulary

- **WHEN** the authoritative Phase 1 completion scan finds an old judgment field or Markdown PASS
  parser still accepted by a Phase 1 operational surface
- **THEN** the migration phase is incomplete and cannot be delivered.
