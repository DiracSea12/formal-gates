## Delivery Applicability

Phase 1 migrates the currently enabled QA Execution artifact to a small
mechanical payload that directly references the approved case set, QA-owned
results, case binding, changed files, and verification. The main agent and CLI
check those inputs without a second QA reviewer or receipt. It does not register Design Review, White-box
Adequacy, or stronger approved-chain admission.

Phase 2 delivers Design Review and its pre-development approved-chain admission.
Each newly enabled stage includes its JSON content, policy, domain
validator, and positive and negative tests in that phase. Design Rework remains
an editing action, not a machine role or stage. Phase 2 does not implement or
register White-box Adequacy.

Phase 2 adds policy `qa.design-review.v2` with role `QA_REVIEW`, gate
`qa-test-gate`, stage `Design Review`, and flow `pre-development`. It requires
same-workflow, same-snapshot requirements PASS, a reviewer receipt, no changed
files or verification fields, no `NOT_APPLICABLE`, and exactly these checks in
the shared reviewer payload:

- `review.prompt-fields`
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
The Design Review's own external receipt binds its exact final-send prompt and
exact JSON result. A PASS review makes that exact case-set hash approved; no copied or
rewritten approved-case artifact is created.

## ADDED Requirements

### Requirement: Formal QA case design precedes implementation

The system SHALL require QA Design before development for a user-authorized
four-gate, release, or seal flow declared before code. QA Design SHALL derive
cases from confirmed requirements and public contracts. The designer SHALL NOT
inspect implementation or diff to invent cases. The case set SHALL bind the
designer receipt but SHALL NOT record a gate PASS.

#### Scenario: Development handoff has approved cases

- **WHEN** formal implementation handoff is validated
- **THEN** it binds the exact case-set hash, designer receipt, and accepted
  independent Design Review.

#### Scenario: Handoff lacks the approved chain

- **WHEN** any required case, receipt, review, or hash binding is absent
- **THEN** handoff is rejected before implementation.

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
