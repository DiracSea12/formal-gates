## Delivery Applicability

Phase 1 migrates the currently enabled QA Execution artifact to a small
mechanical payload that directly references the approved case set, QA-owned
results, case binding, changed files, and verification. The main agent and CLI
check those inputs without a second QA reviewer or receipt. It does not register Design Review, White-box
Adequacy, or stronger approved-chain admission.

Phase 2 delivers Design Review and its pre-development approved-chain admission.
Each newly enabled stage includes its JSON content, policy, domain
validator, and positive and negative tests in that phase. Optional White-box
Adequacy follows the same rule. Design Rework remains an editing action, not a
machine role or stage.

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
policy-owned checks, binding the candidate and accepted case-set hashes and its
reviewer receipt. Changing any case invalidates the prior review and requires a
new Design Review.

#### Scenario: Case changes after review

- **WHEN** a reviewed case, oracle, or case ID changes
- **THEN** the old review no longer satisfies handoff or QA admission.

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

#### Scenario: Existing tests are named without case binding

- **WHEN** evidence only says existing tests passed without mapping approved
  cases and oracles to results
- **THEN** QA Execution cannot record PASS.

#### Scenario: Complete approved chain is supplied

- **WHEN** approved cases, independent review, QA-owned results, and all
  case-to-result bindings validate
- **THEN** QA Execution may be mechanically validated and recorded without a
  second reviewer.

### Requirement: Optional and mechanical QA stages do not add gates

FinalExecution SHALL remain a mechanical closeout over existing gate evidence
and final verification. White-box Adequacy, when required, SHALL reuse the QA
reviewer payload and checks. Neither stage SHALL add a gate or new framework.

#### Scenario: FinalExecution adds QA judgment

- **WHEN** mechanical closeout supplies new QA judgment
- **THEN** role validation rejects it.
