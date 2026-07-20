## ADDED Requirements

### Requirement: One typed policy drives behavior and reporting

The system SHALL define gate order, prerequisites, role/stage rules, required
review check catalogs, permitted `NOT_APPLICABLE` checks, and active role
exceptions once in typed Go data consumed by admission, artifact validation,
and `formal-gates policy show --format json`.

The implementation MUST NOT maintain a separately authored export model or add
an external policy engine.

#### Scenario: Validator and export share a rule object

- **WHEN** a typed rule or required check changes
- **THEN** validation and `policy show` observe that same object without a
  second table update.

#### Scenario: Policy export cannot authorize PASS

- **WHEN** an invalid artifact matches the shape of exported policy output
- **THEN** the owning executable validator still rejects it.

### Requirement: Exported dynamic rules have stable tested IDs

Every exported dynamic rule and reviewer check SHALL have a stable ID and at
least one accepting and one rejecting behavior test that executes its real
validation path. Count, field-name, or keyword comparisons SHALL NOT satisfy
behavior parity.

#### Scenario: Check ID has behavior coverage

- **WHEN** policy coverage is evaluated for a required reviewer check
- **THEN** tests demonstrate both an accepted result and a result that blocks or
  invalidates PASS.

#### Scenario: Shape-only test is supplied

- **WHEN** a test compares only exported keys, lengths, or words
- **THEN** the affected rule remains uncovered.

### Requirement: Policy reporting is read-only and deterministic

`formal-gates policy show --format json` SHALL emit deterministic JSON and SHALL
not mutate workflow state or artifacts. Unsupported formats SHALL fail without
mutation.

The output SHALL contain exactly `schemaVersion`,
`postDevelopmentGateOrder`, and `artifactPolicies`. `schemaVersion` SHALL be
integer `2`. `postDevelopmentGateOrder` SHALL contain the four fixed
post-development gate IDs in this order: `qa-test-gate`, `complexity-gate`,
`architecture-health-gate`, `code-quality-gate`. `artifactPolicies` SHALL be
sorted by `id` and contain exactly these Phase 1 policy IDs:

| Policy ID | Artifact role | Gate / stage | Flow |
|---|---|---|---|
| `requirements.pass.v2` | `REQUIREMENTS_PASS` | `requirements-clarification-gate` / `""` | `requirements` |
| `qa.execution.v2` | `QA_EXECUTION` | `qa-test-gate` / `Execution` | `post-development` |
| `complexity.start-readiness.v2` | `COMPLEXITY_REVIEW` | `complexity-gate` / `""` | `start-readiness` |
| `complexity.post-development.v2` | `COMPLEXITY_REVIEW` | `complexity-gate` / `""` | `post-development` |
| `architecture.start-readiness.v2` | `ARCHITECTURE_REVIEW` | `architecture-health-gate` / `""` | `start-readiness` |
| `architecture.post-development.v2` | `ARCHITECTURE_REVIEW` | `architecture-health-gate` / `""` | `post-development` |
| `code-quality.post-development.v2` | `CODE_QUALITY_REVIEW` | `code-quality-gate` / `""` | `post-development` |
| `final-execution.v2` | `FINAL_EXECUTION` | `qa-test-gate` / `FinalExecution` | `finalization` |

Phase 2 adds exactly these policies without changing the fixed post-development
gate order:

| Policy ID | Artifact role | Gate / stage | Flow |
|---|---|---|---|
| `qa.design-review.v2` | `QA_REVIEW` | `qa-test-gate` / `Design Review` | `pre-development` |
| `carry.arbiter.v2` | `CARRY_ARBITER` | `qa-test-gate` / `Carry` | `carry` |

Each `ArtifactPolicy` SHALL contain exactly `id`, `artifactRole`, `gate`,
`stage`, `flow`, `prerequisites`, `requiredCheckIds`,
`allowedNotApplicableCheckIds`, `receiptRequired`, `changedFilesRequired`,
`verificationRequired`, and `mechanical`. Each prerequisite SHALL contain
exactly `gate`, `stage`, and `flow`.

Artifact validation and authoritative recording SHALL select the same policy
object. For reviewer artifacts, `reviewPolicyId` SHALL identify that object;
requirements, QA Execution, and FinalExecution SHALL use their fixed role policy. The selected
policy's `artifactRole`, `gate`, and `stage` SHALL match the envelope, and its
`flow` SHALL match the existing recording request and persisted state: mode
`start-readiness` maps to flow `start-readiness`, formal reviewer recording maps
to `post-development`, requirements recording maps to `requirements`, and the
CLI-owned FinalExecution path maps to `finalization`. Admission SHALL match every
prerequisite by gate, stage, and flow. Standalone artifact validation without a
recording flow SHALL NOT authorize PASS or state mutation. No public artifact
field or new CLI flag is added for this check.

After Phase 2, mode `pre-development` maps to the Design Review flow and
`workflow record-transition` selects the Carry policy directly. Design Review
requires requirements PASS. Carry validates source closures through its domain
payload instead of declaring current-target gate prerequisites in the policy.

Requirements and Phase 1 QA Execution have no gate prerequisite. Phase 2
Design Review requires requirements PASS on its pre-development snapshot.
Phase 2 QA Execution validates its `designReview` closure by same workflow and
exact case-set hash in the QA domain validator; Design Review is not a
current-snapshot gate-state prerequisite. Both start-readiness reviewer
policies require requirements PASS and are independently recordable. The three
start-readiness conclusions are collected in parallel and aggregated by the
readiness workflow. All four post-development gate policies are independently
recordable on the same workflow and target snapshot; none requires another
post-development gate. FinalExecution requires all four fixed gate results.
Every shared-payload reviewer policy requires `review.prompt-semantics` and its
gate-specific check IDs. Static prompt structure and bindings are validated by
the CLI before dispatch and SHALL NOT be exported as a reviewer check. Only
`complexity.start-readiness.v2` allows `complexity.statistics` to be
`NOT_APPLICABLE`; every other allowed list is empty.

`receiptRequired` SHALL be true only for reviewer and Carry Arbiter policies.
`changedFilesRequired` and `verificationRequired` SHALL be true for QA
Execution and post-development reviewer policies. `mechanical` SHALL be true
only for `qa.execution.v2` and `final-execution.v2`. Requirements, QA
Execution, Carry Arbiter, and FinalExecution SHALL have empty check-ID arrays.
The output SHALL NOT contain free-form rule descriptions, maps, future
role placeholders, or a properties bag.

#### Scenario: Current policy is exported

- **WHEN** a maintainer requests JSON policy output
- **THEN** the command reports the exact schema, gate order, policy IDs,
  prerequisites, checks, and flags above from current executable data.

#### Scenario: Unsupported format is requested

- **WHEN** a maintainer requests another format
- **THEN** the command returns an error and writes no formal state.

#### Scenario: Policy flow does not match recording flow

- **WHEN** a start-readiness policy is supplied to post-development recording,
  or any prerequisite has the right gate and stage but the wrong flow
- **THEN** validation rejects it before authoritative state mutation.
