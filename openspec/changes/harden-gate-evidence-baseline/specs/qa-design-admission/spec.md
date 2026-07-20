## Delivery Applicability

Phase 1 migrates the currently enabled QA Execution artifact to a small
mechanical payload that directly references the approved case set, QA-owned
results, case binding, changed files, and verification. The main agent and CLI
check those inputs without a second QA reviewer or receipt. It does not register Design Review, White-box
Adequacy, or stronger approved-chain admission.

Phase 2 delivers Design Review and its pre-development approved-chain admission.
Each newly enabled stage includes its JSON content, policy, domain
validator, and positive and negative tests in that phase. Design Rework remains
a semantic revision action through another Design registration/submission, not
a machine role or stage. Phase 2 does not implement or
register White-box Adequacy.

Phase 2 adds policy `qa.design-review.v2` with role `QA_REVIEW`, gate
`qa-test-gate`, stage `Design Review`, and flow `pre-development`. It requires
same-workflow, same-snapshot requirements PASS, a reviewer receipt, no changed
files or verification fields, no `NOT_APPLICABLE`, and exactly these checks in
the shared reviewer payload:

- `review.prompt-semantics`
- `qa.design.requirement-coverage`
- `qa.design.executability`
- `qa.design.oracles`
- `qa.design.evidence-binding`
- `qa.design.independence`
- `qa.design.case-set-binding`

`qa.design.case-set-binding` SHALL reference exactly the reviewed case set and
its Design-stage lifecycle receipt. That receipt reuses the existing lifecycle
registration, start/stop, subagent-ID, snapshot, and exact-output-hash chain,
but Design itself records no gate PASS and requires no reviewer-prompt binding.
Design registration SHALL generate the title, stable Case IDs, seven-field
catalog, separators, and newline layout. The designer SHALL submit exactly the
ordered Claim, Source, Action, Oracle, Failure signal, Evidence, and Gap values
for each generated case position through `receipt submit`; it SHALL NOT edit
the generated Markdown. The CLI SHALL reject missing, duplicate, unknown,
incomplete, empty, `PENDING`, or multiline values before changing the case
artifact, then write canonical case order and atomically record the exact
artifact hash in the open dispatch. Finalization SHALL require that submission
proof. Design Rework SHALL use another Design registration and semantic
submission rather than rewriting a submitted or finalized case set.
Receipt registration generates the Design Review catalog and exact
case-set/Design-receipt evidence binding. The reviewer supplies only ordered
semantic status, message, finding, and location values through `receipt submit`;
the CLI constructs the JSON and submission proof, and finalization mechanically
derives the verdict. The Design Review's own external receipt binds
its exact final-send prompt and finalized JSON result. A PASS review makes that
exact case-set hash approved; no copied or rewritten approved-case artifact is
created.

## ADDED Requirements

### Requirement: Formal QA case design precedes implementation

The system SHALL require QA Design before development for a user-authorized
four-gate, release, or seal flow declared before code. QA Design SHALL derive
cases from confirmed requirements and public contracts. The designer SHALL NOT
inspect implementation or diff to invent cases. The case set SHALL bind the
designer receipt but SHALL NOT record a gate PASS.

The CLI SHALL generate the formal development-handoff template and populate
its field catalog, workflow/snapshot, approved-case and Design Review evidence
references, paths/hashes, and complexity-check command shape. The orchestrator
MAY supply only semantic scope, verification expectations, budget choices, stop
conditions, and residual-risk text. A hand-authored static handoff SHALL NOT
satisfy admission.

The orchestrator SHALL select one existing supported complexity task type. The
CLI SHALL reject a missing or unsupported type before writing the handoff or
composition proof, and SHALL generate an executable complexity command using
that type and the same three numeric limits as the budget field.

#### Scenario: Development handoff has approved cases

- **WHEN** formal implementation handoff is validated
- **THEN** it binds the exact case-set hash, designer receipt, and accepted
  independent Design Review.

#### Scenario: Handoff lacks the approved chain

- **WHEN** any required case, receipt, review, or hash binding is absent
- **THEN** handoff is rejected before implementation.

#### Scenario: Generated complexity command is executable

- **WHEN** a handoff is composed with a supported complexity task type and
  numeric budget
- **THEN** the handoff validates and its generated complexity command executes
  with that exact type and budget.

#### Scenario: Handoff task type is unsupported

- **WHEN** handoff composition receives a task type outside the existing
  complexity checker enum
- **THEN** composition is rejected without a partial handoff or proof.

#### Scenario: Handoff uses an explicit workflow run directory

- **WHEN** handoff composition receives a valid `--run-dir` whose directory name
  differs from the workflow ID
- **THEN** composition and subsequent validation use that same resolved run,
  validate its generated proof, and retain the handoff.

### Requirement: Existing code starts a new blind-design workflow

When formal intent is first declared after code exists, the system SHALL start
a new workflow and treat that code as an unaccepted candidate. QA Design and
independent Design Review SHALL not read candidate implementation, diff,
existing tests, or prior self-test claims.

After approval, the formal worker MAY adopt, modify, or delete candidate code
and SHALL produce current-snapshot verification. Adoption MAY produce no code
delta when justified, but no old gate or approval status carries into the new
workflow.

#### Scenario: Worker adopts valid candidate code

- **WHEN** the approved cases show candidate code already satisfies part of the
  requirement
- **THEN** the worker may record adoption rather than rewriting it.

### Requirement: Design Review is the only pre-development review record

Independent Design Review SHALL use the shared reviewer envelope and
policy-owned checks, binding the exact reviewed case-set hash, its Design-stage
receipt, and the reviewer receipt. Changing any case invalidates the prior
review and requires a new Design Review.

#### Scenario: Case changes after review

- **WHEN** a reviewed case, oracle, or case ID changes
- **THEN** the old review no longer satisfies handoff or QA admission.

#### Scenario: Designer or reviewer receipt does not bind the case set

- **WHEN** either lifecycle receipt is absent, stale, or hashes different bytes
- **THEN** Design Review cannot record PASS and the case set is not approved.

### Requirement: Approved cases cannot be weakened by development

The developer MAY add cases but MUST NOT delete, replace, weaken, or loosen an
approved case or oracle. Such a change SHALL return to independent Design
Review.

#### Scenario: Approved oracle is weakened

- **WHEN** the development case set accepts behavior rejected by an approved
  oracle
- **THEN** QA admission fails and requires a newly reviewed case set.

### Requirement: QA Execution binds cases to QA-owned results

Formal QA Execution PASS SHALL require the approved case chain, QA-owned
execution evidence, and complete case-to-result binding. Developer self-test or
field labels alone SHALL NOT satisfy admission.

The independent QA executor SHALL call the QA-owned evidence composer with only
one approved case's 1-based position, PASS/FAIL outcome, procedure,
observation, and oracle-result scalars per repeated group. The CLI SHALL read
approved Case IDs and generate QA results, Execution IDs, procedure references,
paths, hashes, arrays, pair proof, and complete case-result binding. It SHALL
reject missing, duplicate, out-of-range, empty, `PENDING`, or illegal
submissions before writing any target or proof. The CLI SHALL then hash the six
evidence sources and generate the complete `QA_EXECUTION` envelope; neither the
QA executor nor the main agent may author semantic JSON, edit a static
template, or handwrite static formal content.

#### Scenario: QA submits positioned scalar observations

- **WHEN** every approved case position appears exactly once with a legal
  outcome and non-empty resolved procedure, observation, and oracle result
- **THEN** the CLI derives Case/Execution IDs and writes a complete QA-owned
  result and binding pair that the existing QA Execution validator accepts.

#### Scenario: QA positioned submission is incomplete

- **WHEN** a case position is missing, duplicated, out of range, empty,
  `PENDING`, or carries an illegal outcome
- **THEN** the CLI rejects before changing any target, proof, or approved case
  source.

In Phase 2 the `QA_EXECUTION` payload contains the Phase 1 five evidence
references plus `designReview`, which references the accepted Design Review
closure. The closure SHALL contain the exact `approvedCaseSet` hash and its
designer and reviewer receipts. Formal development handoff SHALL bind the same
case-set hash and Design Review closure. The Design Review keeps its
pre-development snapshot; QA Execution MAY have the later implementation
snapshot and SHALL validate the closure by same workflow and exact case-set
hash rather than as a current-snapshot gate prerequisite. No status label or
task checkbox can replace this chain.

#### Scenario: Existing tests are named without case binding

- **WHEN** evidence only says existing tests passed without mapping approved
  cases and oracles to results
- **THEN** QA Execution cannot record PASS.

#### Scenario: Complete approved chain is supplied

- **WHEN** approved cases, independent review, QA-owned results, and all
  case-to-result bindings validate
- **THEN** QA Execution may be mechanically validated and recorded without a
  second reviewer.

#### Scenario: QA Execution names a different approved case set

- **WHEN** its `approvedCaseSet` hash differs from the case set in the accepted
  Design Review closure
- **THEN** QA Execution is rejected before state mutation.

#### Scenario: Implementation changes after Design Review

- **WHEN** implementation produces a later snapshot without changing any case,
  oracle, or Case ID
- **THEN** QA Execution may reference the immutable pre-development Design
  Review closure for the same workflow and exact case-set hash without another
  Design Review.

### Requirement: Mechanical QA closeout does not add a gate

FinalExecution SHALL remain a mechanical closeout over existing gate evidence
and final verification. It SHALL NOT add a gate or new QA judgment.

#### Scenario: FinalExecution adds QA judgment

- **WHEN** mechanical closeout supplies new QA judgment
- **THEN** role validation rejects it.
