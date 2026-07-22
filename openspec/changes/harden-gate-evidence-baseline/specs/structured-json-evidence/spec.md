## Delivery Applicability

Phase 1 migrates only the formal roles that already exist and are enabled:
requirements PASS, QA Execution, complexity, architecture, code quality, and
mechanical FinalExecution. Design Review, White-box Adequacy, and
Carry-Forward Arbiter are not registered, accepted, or represented by
placeholder payloads in Phase 1. Phase 2 adds Design Review and Carry-Forward
Arbiter only with their JSON content, policy, domain validator, and positive and
negative tests in the same task. White-box Adequacy remains unsupported by this
change. The envelope remains common.

## ADDED Requirements

### Requirement: CLI generation owns every deterministic formal field

Every static formal-workflow value SHALL be script-generated. This includes
schema/version, artifact role, workflow/snapshot, gate/stage, policy and check
catalogs, context and evidence paths, hashes, bindings, verification
envelopes, receipts, closures, state, and aggregate verdicts. AI SHALL provide
only semantic requirement judgments, reviewer statuses/messages/findings,
Carry decisions/reasons, and QA execution observations. A public composition or
registration path SHALL generate the static template or final artifact before
that artifact can be validated or recorded. There SHALL be no compatibility
path for an AI-authored complete formal JSON artifact.

Receipt registration SHALL accept only role-specific source paths for
Design Review case-set/Design-receipt binding and Carry source closures. It
SHALL generate the fixed check IDs
and bindings and derive every Carry gate from a verified source closure.
Transition-chain composition SHALL accept ordered scalar snapshot and evidence
path values for each hop and generate the hop fields, objects, and array. A
generic caller-authored check-ID map, gate/path binding, or hop string DSL SHALL
NOT be a supported production input.

#### Scenario: Old complete producer artifact is supplied

- **WHEN** an AI actor supplies a complete reviewer, requirements, QA
  Execution, Carry, or verification formal artifact without its
  required CLI generation/proof step
- **THEN** validation or finalization rejects it and records no PASS.

#### Scenario: Semantic owner changes a static field

- **WHEN** a semantic owner changes a generated schema field, check ID,
  evidence reference, hash, binding, source gate, or verdict
- **THEN** the immutable projection check rejects the output before receipt,
  closure, or state mutation.

#### Scenario: Semantic-only input completes normally

- **WHEN** a generated template keeps its static projection and the semantic
  owner completes every required semantic slot
- **THEN** the CLI derives the verdict and writes the final formal artifact and
  downstream machine bindings.

#### Scenario: Caller supplies a removed static mini-language

- **WHEN** a public caller supplies a check ID/path pair, a Carry gate/path
  pair, or a key/value hop string instead of role-specific paths and ordered
  hop scalars
- **THEN** the CLI rejects the unsupported input before writing an output or
  composition/dispatch proof.

### Requirement: Public and internal JSON ownership stays narrow

Public reviewer, requirements, QA Execution, context-bundle, FinalExecution, and `policy show --format json` structures SHALL have closed machine contracts.

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
`PASS`, `REVIEW`, `FAIL`, or `BLOCKED`. `REQUIREMENTS_PASS`, `QA_EXECUTION`,
and `FINAL_EXECUTION` SHALL accept only `PASS`.

The Phase 1 role combinations SHALL be exactly:

| `artifactRole` | `gate` | `stage` | payload |
|---|---|---|---|
| `REQUIREMENTS_PASS` | `requirements-clarification-gate` | `""` | requirements |
| `QA_EXECUTION` | `qa-test-gate` | `Execution` | QA execution |
| `COMPLEXITY_REVIEW` | `complexity-gate` | `""` | reviewer |
| `ARCHITECTURE_REVIEW` | `architecture-health-gate` | `""` | reviewer |
| `CODE_QUALITY_REVIEW` | `code-quality-gate` | `""` | reviewer |
| `FINAL_EXECUTION` | `qa-test-gate` | `FinalExecution` | final execution |

Phase 2 adds exactly `QA_REVIEW` / `qa-test-gate` / `Design Review` with the
shared reviewer payload and `CARRY_ARBITER` / `qa-test-gate` / `Carry` with the
typed Carry payload. Their fields and domain rules are owned by
`qa-design-admission` and `carry-forward-finalization`; the envelope decoder
only dispatches the typed roles. White-box Adequacy remains unsupported.

Phase 1 SHALL NOT contain `route`, `nextAction`, `reworkOwner`, or `rerunFrom`.
The CLI SHALL derive admission from `artifactRole` and `verdict`. Unresolved
requirements SHALL continue clarification without producing a
`REQUIREMENTS_PASS` artifact. A failed final closeout SHALL return an error
without producing `FINAL_EXECUTION`. Reviewer `REVIEW`, `FAIL`, and `BLOCKED`
results MAY report findings but SHALL NOT create formal PASS state. Findings
and human-readable output MAY describe rework, but SHALL NOT supply a machine
routing field. Rerun-boundary behavior is outside Phase 1 and SHALL be defined
only with Phase 2 Carry arbitration and transitions.

The CLI SHALL generate every
deterministic field in reviewer, requirements, QA Execution, Carry,
verification binding, receipt, closure, state, and finalization
artifacts. Reviewer and Carry semantic owners SHALL submit only ordered values
through `receipt submit`; confirmed requirement and QA execution owners use
documented compose commands with 1-based positions and pure semantic scalar
arguments. Reviewer submission SHALL reject Carry and QA Design fields; Carry
submission SHALL reject reviewer and QA Design fields; QA Design submission
SHALL reject reviewer and Carry fields. Cross-role rejection SHALL happen
before either the assigned artifact or dispatch proof changes. Requirements composition SHALL derive every RQ/DIM mapping and JSON
structure. QA-owned evidence composition SHALL read Case IDs from the approved
case set and derive every Execution ID, procedure reference, result array, and
binding. No AI-authored semantic JSON or editable generated template SHALL be
a production input. No semantic owner SHALL handwrite a complete formal
artifact or repeat schema/version, role, workflow/snapshot, gate/stage,
policy/check catalog, evidence path/hash/binding, or aggregate verdict. The CLI
SHALL NOT generate or rewrite semantic judgments; it SHALL validate the
semantic values, leave the artifact unchanged on rejection, atomically record
the composed reviewer/Carry artifact hash and submission proof, and
mechanically compose the final artifact.
For every `receipt submit` role, the caller MAY fully resubmit all role-specific
semantic values while the same dispatch remains open and unfinalized, but only
when its existing valid `SemanticSubmissionSHA` exactly matches the current
artifact bytes. The CLI SHALL rebuild the projection from the original static
catalog, rerun every validation, and atomically replace the artifact and
dispatch proof. A manually changed artifact, missing or mismatched submission
SHA, finalized dispatch, incomplete input, or other validation failure SHALL be
rejected before either file changes. This SHALL use the existing submission and
dispatch lifecycle without a reset/reopen command or state.
The Requirements and QA-owned evidence composers SHALL reject missing,
duplicate, out-of-range, empty, `PENDING`, or illegal semantic values before
writing any target or sibling composition proof. Existing inputs and targets
SHALL remain byte-identical after rejection.
For a Phase 3 target, changed-files composition SHALL accept repeatable explicit
delivery paths from the worker/orchestrator. It SHALL validate repository-
relative paths, reject `.gates`, sort and deduplicate them, and generate the
evidence artifact plus composition proof. It SHALL NOT rediscover Git range,
staged, unstaged, or untracked paths, parse a diff, read project content, or infer
which unrelated worktree files belong to the delivery. The worker SHALL add a
new delivery file to the named external VCS immediately and SHALL add an
existing untracked delivery file before modifying or deleting it. Only explicit
delivery paths may be added; whole-worktree add commands are forbidden. Before
the worker returns, every delivery path SHALL be tracked and present in the
complete external VCS diff, while unrelated untracked files remain untouched.
Typed Go structs and the standard library SHALL define deterministic output
bytes. The decoder SHALL reject invalid UTF-8, duplicate keys, unknown fields
at every level, wrong types, trailing JSON values, old schema, and
role/gate/stage conflicts before domain validation. Required arrays SHALL be
present even when empty; `null` SHALL be rejected; and optional fields SHALL be
omitted rather than emitted as `null`. A reviewer artifact receipt SHALL hash
the exact submitted bytes without canonicalizing key order. Markdown MAY
explain a result but MUST NOT satisfy, complete, or override a machine field.
Reviewer registration and `receipt submit` SHALL be the only supported judgment path;
old full-artifact producer input is unsupported and SHALL NOT receive a
compatibility path.

#### Scenario: Receipt submission mixes role semantics

- **WHEN** reviewer or Carry submission also supplies QA Design case values, or
  any role supplies another role's semantic fields
- **THEN** submission rejects before changing the artifact or open dispatch.

#### Scenario: Open semantic submission is fully replaced

- **WHEN** a caller fully resubmits its role semantics to the same open,
  unfinalized dispatch whose `SemanticSubmissionSHA` matches the current
  artifact bytes
- **THEN** the CLI rebuilds from the original static catalog, reruns all
  validation, and atomically updates the artifact and dispatch proof.

#### Scenario: Changed-files evidence is composed from explicit paths

- **WHEN** the worker submits the delivery paths for a Phase 3 target
- **THEN** the CLI validates, sorts, deduplicates, writes, and binds the generated
  artifact and proof.

#### Scenario: Caller supplies an invalid delivery path

- **WHEN** a caller supplies an absolute, escaping, empty, backslash-containing,
  control-character-containing, or `.gates` path
- **THEN** composition rejects it without writing an artifact or proof.

#### Scenario: Markdown cannot complete JSON

- **WHEN** required JSON evidence is missing but matching Markdown labels exist
- **THEN** recording rejects the artifact without reading those labels as truth.

#### Scenario: Unsupported or future stage is sent

- **WHEN** QA evidence names White-box Adequacy, or names Design Review or Carry
  Arbiter before its owning Phase 2 task is delivered
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

- **WHEN** `REQUIREMENTS_PASS`, `QA_EXECUTION`, or `FINAL_EXECUTION` has
  verdict `REVIEW`, `FAIL`, or `BLOCKED`
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

### Requirement: Reviewer gates use one reviewer payload

Complexity, architecture, and code quality SHALL use the same CLI-generated
reviewer payload containing exactly:

| Field | Type | Rule |
|---|---|---|
| `contextBundle` | `EvidenceRef` | required machine binding under `restricted/`; never supplied as reviewer context |
| `reviewPolicyId` | string | required and non-empty; known by typed policy |
| `checks` | array of `Check` | required |
| `changedFiles` | `EvidenceRef` | required post-development machine evidence under `restricted/`; the reviewer inspects the live diff instead |
| `verification` | `EvidenceRef` | required post-development machine evidence under `restricted/`; not a pre-read reviewer summary |

Each `Check` SHALL contain exactly `id`, `status`, `message`, `evidenceRefs`,
and `findings`. Registration SHALL generate `id` and `evidenceRefs`; the
reviewer SHALL pass ordered `status`, `message`, `findings`, and locations to
`receipt submit`, and the CLI SHALL construct the complete `Check`. `id` and `message`
SHALL be non-empty. `status` SHALL be
`PASS`, `REVIEW`, `FAIL`, `BLOCKED`, or `NOT_APPLICABLE`. `evidenceRefs[]` and
`findings[]` SHALL always be present. A matching reviewer receipt SHALL NOT be
a reviewer-payload field: the CLI hashes completed reviewer JSON first, then
the receipt binds that hash, and the closure contains both.

The reviewer payload SHALL NOT contain `reviewSessionId` or another
self-reported identity field. It SHALL also reject legacy `dispatch` and prompt
evidence fields. The external receipt SHALL bind the exact final-send prompt,
system-generated dispatch ID, distinct review output path, exact output hash,
workflow, gate, stage, and snapshot. Providers with usable lifecycle events
SHALL additionally bind the host-captured subagent ID and start/stop events;
Codex SHALL keep the other bindings without requiring unavailable events.

The CLI SHALL generate and validate `contextBundle` under `restricted/` before
dispatch from caller-supplied source paths, and registration SHALL place that
exact unchanged machine binding in the reviewer template. The reviewer SHALL
not transcribe it or read the bound files. The
JSON referenced by `contextBundle` SHALL contain exactly `bundleVersion`,
`workflowId`, `changeSnapshot`, and `inputs`. `bundleVersion` SHALL be integer
`1`; workflow and snapshot SHALL match the reviewer envelope; and `inputs`
SHALL be a non-empty array of `EvidenceRef`. Unknown or duplicate fields and
duplicate normalized input paths SHALL be rejected. The CLI SHALL verify every
listed path and hash, and every `inputs[]` item SHALL become an evidence-closure
edge. Every referenced file SHALL also be under the active run's `restricted/`
directory. No bundle path or content is included in the reviewer prompt.

The typed Go policy catalog for the selected gate SHALL define and generate
required check IDs and which checks, if any, allow `NOT_APPLICABLE`. The CLI
SHALL reject any static template change, including missing, unknown, reordered,
or duplicate IDs or modified evidence references, plus unknown status and unapproved
`NOT_APPLICABLE`. `NOT_APPLICABLE` SHALL require a reason in `message`. Any
`REVIEW`, `FAIL`, or `BLOCKED` SHALL prevent aggregate PASS. The CLI SHALL
compute and write the aggregate verdict; the semantic owner SHALL not supply it
and messages SHALL never determine it.

`reviewPolicyId` SHALL select one of the reviewer policies exported by
`policy show`; its artifact role, gate, and stage SHALL match the envelope.
Unknown or mismatched policy IDs SHALL be rejected before check aggregation.

Every reviewer policy SHALL require `review.prompt-semantics` in addition to
the gate-specific IDs below. Purely static prompt-field validation SHALL remain
CLI-owned and SHALL NOT appear as a reviewer check. Reviewer checks SHALL NOT
use `NOT_APPLICABLE`.

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

### Requirement: Reviewer check catalogs are policy-owned

The CLI SHALL generate each reviewer's static envelope, context binding,
policy, check catalog, evidence references, and aggregate verdict. Reviewers
SHALL submit only the ordered semantic status, message, finding, and location
values accepted by their role.

The reviewer gate-specific check IDs SHALL be:

| Gate | Check IDs |
|---|---|
| Complexity | `complexity.diff-shape`, `complexity.impact-surface`, `complexity.public-config-surface`, `complexity.new-concepts`, `complexity.minimum-sufficient`, `complexity.shrink-opportunities` |
| Architecture | `architecture.boundaries`, `architecture.ownership`, `architecture.public-surface`, `architecture.state-lifecycle`, `architecture.dependencies`, `architecture.failure-semantics`, `architecture.performance`, `architecture.decoupling` |
| Code quality | `code-quality.correctness`, `code-quality.maintainability`, `code-quality.performance`, `code-quality.test-quality`, `code-quality.dead-code`, `code-quality.overfitting`, `code-quality.validation-encoding`, `code-quality.verification`, `code-quality.residual-risk` |

The orchestrator supplies proof source paths to registration; the CLI generates
the matching `evidenceRefs`. The CLI-generated envelope verdict SHALL determine
machine admission; findings and human-readable output SHALL describe required
rework. No gate-specific judgment or evidence field SHALL be added to the
reviewer payload.

#### Scenario: Reviewer submits semantic judgments

- **WHEN** a reviewer submits one valid ordered semantic result for every
  generated check
- **THEN** the CLI writes the policy-owned check IDs, evidence references,
  findings, locations, and aggregate verdict.

### Requirement: Phase 1 operational payloads retain only current data

The Phase 1 `QA_EXECUTION` payload SHALL contain exactly:

| Field | Type | Rule |
|---|---|---|
| `approvedCaseSet` | `EvidenceRef` | required; approved case document with unique case IDs |
| `qaOwnedResults` | `EvidenceRef` | required; QA-owned, complete, PASS, and bound to the envelope workflow and snapshot |
| `caseResultBinding` | `EvidenceRef` | required; binds every approved case to the exact QA result and PASS procedures |
| `changedFiles` | `EvidenceRef` | required |
| `verification` | `EvidenceRef` | required |

The CLI SHALL verify all five paths and hashes. QA results SHALL exactly cover
the approved case IDs; every referenced execution and case result SHALL PASS;
and the binding SHALL reference the exact approved-case and QA-results hashes,
exactly cover the same IDs, point to the matching result, and bind its oracle
and execution references. Missing cases, extra cases, failed results, stale
workflow or snapshot, wrong hashes, or mismatched bindings SHALL reject PASS
without state mutation. `QA_EXECUTION` SHALL NOT contain reviewer dispatch,
context-bundle, policy-check, finding, or receipt fields.

The `artifact compose-qa-owned-evidence` entrypoint SHALL read approved Case
IDs in document order and accept only one group of 1-based case position,
PASS/FAIL outcome, procedure, observation, and oracle-result scalars per case.
The CLI SHALL generate QA-owned results, Execution IDs, procedure references,
case-result binding, static workflow/snapshot fields, paths, hashes, arrays,
and pair proof. Missing, duplicate, out-of-range, empty, `PENDING`, or illegal
submissions SHALL be rejected before any output or proof is written. The
`artifact compose-qa-execution` entrypoint SHALL hash the six Phase 2 sources,
generate the complete `QA_EXECUTION` envelope and payload, validate it, refuse
overwrite, and remove any partial output on failure. No AI actor SHALL author
either envelope, evidence references, semantic JSON, or a static template.

#### Scenario: QA Execution evidence is complete

- **WHEN** approved cases, QA-owned PASS results, complete result bindings,
  changed files, and verification all match the envelope workflow and snapshot
- **THEN** the main agent may invoke CLI composition and recording without
  authoring `QA_EXECUTION` or dispatching a second QA reviewer.

#### Scenario: QA Execution tries to self-review

- **WHEN** a `QA_EXECUTION` payload supplies reviewer checks, findings,
  dispatch, context bundle, or receipt data
- **THEN** strict role validation rejects the payload.

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
gate, PASS verdict, workflow, envelope snapshot, any role-required receipt, and all
transitive evidence. It SHALL NOT reference the mutable gate-state file or
create a separate gate-record artifact. FinalExecution SHALL NOT own another
closure: its `gateEvidence` and `finalVerification` references are revalidation
inputs for mechanical closeout, not edges in an aggregate root. The finalization
state entry SHALL bind the exact CLI-generated FinalExecution artifact path and
hash and SHALL NOT satisfy a gate prerequisite.

Phase 1 SHALL NOT define a Carry Arbiter payload, a carried matrix row, a Design
Review payload, or a White-box payload. Phase 2 adds Carry Arbiter and Design
Review only with their complete domain features; White-box Adequacy remains
unsupported by this change. FinalExecution MUST NOT require or accept
task-checkbox evidence.

#### Scenario: Operational role impersonates reviewer

- **WHEN** requirements, QA Execution, or FinalExecution supplies reviewer fields
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

### Requirement: Operational surfaces share the current typed owner

Phase 1 SHALL atomically update every producer and consumer of requirements,
QA Execution, complexity, architecture, code quality, and mechanical
FinalExecution: agent templates, references, examples, canaries, fixtures,
policy output, artifact tests, and operational documentation. All of those
surfaces SHALL use the same current typed contracts.

A later role or stage SHALL update its own producing and consuming surfaces in
the phase that enables it; its future vocabulary SHALL NOT be required from the
Phase 1 cutover.

#### Scenario: Operational surfaces disagree on a typed contract

- **WHEN** one enabled producer or consumer uses a different field or policy
  contract from the owning typed implementation
- **THEN** the migration phase is incomplete and cannot be delivered.
