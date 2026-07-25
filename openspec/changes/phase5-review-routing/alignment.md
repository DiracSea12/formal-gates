# Requirements Alignment

Date: 2026-07-25
Status: confirmed for Phase 5 implementation

## RQ-001 - Adaptive requirement and solution alignment

Requirements Clarification SHALL run before formal development. The main agent
SHALL conduct it interactively, one consequential decision at a time. It SHALL
inspect facts available from the repository instead of asking the user, and
SHALL adapt its questions to the actual request rather than use a fixed scope
or questionnaire.

Clarification SHALL cover both the requested outcome and technical decisions
that materially affect public behavior, acceptance, or architecture. The main
agent SHALL explain and align consequential solution choices; it SHALL decide
minor implementation details unless they become consequential. Before the user
confirms shared understanding, the main agent SHALL remain read-only and SHALL
NOT prepare development, dispatch a development worker, or modify delivery
code.

The workflow SHALL bind only the requirement source supplied for the current
run and its revision. It SHALL NOT know or route by a product, feature, business
domain, or project-specific document path.

## RQ-002 - One gate selection

After the confirmed requirement is recorded, the main agent SHALL present one
ordered gate list containing QA followed by every valid gate dynamically
discovered from the current package. The user SHALL make one routing decision:
no gate flow, the full gate flow, or a custom selection from that list. Gate
count SHALL remain dynamic; Phase 5 SHALL reuse the existing discovery owner.

Full flow SHALL select QA and every discovered gate. Custom flow SHALL require
every selected gate and SHALL record every unselected gate as user-authorized
to skip. Selecting QA SHALL require pre-development QA Design and
post-development QA Execution. Omitting QA SHALL omit both actions.

Start Readiness SHALL remain a separate binary pre-development action. It
SHALL run automatically for full and custom gate flows and SHALL NOT require a
separate user selection. It SHALL not use finding severity or consume a repair
cycle.

## RQ-003 - Role and transition enforcement

During a formal run, the main agent SHALL own clarification, routing,
orchestration, and result recording but SHALL NOT modify delivery code. A
separate development worker SHALL be the only formal-flow role that modifies
delivery code. The CLI SHALL compose its task only after the current
requirement is confirmed, routing is confirmed, Start Readiness passes, and QA
Design is complete when QA is selected.

Every prepare and record entrypoint SHALL enforce the same direct transition
preconditions. A command SHALL reject an unselected gate, a skipped
prerequisite, a missing immutable development snapshot, pending repair
verification, or another attempt to cross the documented order. This policy
SHALL be uniform; Phase 5 SHALL NOT add a legacy, compatibility, or relaxed
path. Each transition guard SHALL inspect only the minimum workflow state
needed to prove that operation's direct ordering prerequisites. This minimum
check rule applies only to workflow sequence enforcement; it does not narrow
clarification, readiness, QA, review, or verification scope.

## RQ-004 - Route changes and invalidation

Only an explicit user decision may change the selected gate set. A gate may be
added only before its required workflow node has passed. A selected gate SHALL
not be silently removed; it must pass or receive user skip authorization at
Seal. QA cannot be added after development begins because its design action is
pre-development. A post-development gate may be added after development but
before Seal. The confirmed none, full, or custom route SHALL remain the run's
single routing decision across requirement revisions; the workflow SHALL NOT
ask the user to choose the route again.

When the bound requirement revision changes, Resume SHALL pause without
preserving or invalidating dependent results until the main agent explicitly
classifies the change by meaning. A meaning-preserving change leaves the
confirmed outcome, acceptance, public behavior, and consequential solution
boundaries unchanged and SHALL preserve dependent results. A meaning-changing
classification SHALL invalidate the confirmed requirement and dependent
readiness, QA, development, and review results and return to
Requirements Clarification. Classification SHALL depend on semantic effect,
not an enumerated list of edit types. If the main agent cannot establish that
meaning is preserved, it SHALL clarify with the user instead of guessing.

## RQ-005 - Severity and QA routing

Every discovered-gate finding SHALL contain exactly one severity: `P0`, `P1`,
or `P2`. A reviewer SHALL complete the related-chain scan and SHALL not receive
the downstream blocking policy or remaining repair count.

Severity SHALL describe impact rather than an enumerated defect type. P0 means
a systemic severe consequence or unusable core capability. P1 means a
confirmed requirement, acceptance, or architecture-boundary violation. P2
means an improvement without a confirmed behavior violation.

A discovered gate SHALL return `PASS` with no findings or only P2 findings,
`FAIL` with at least one P0 or P1 finding and optional P2 findings, or
`RUNTIME_ERROR` with no findings. P0 and P1 SHALL block normal delivery. When
either exists in a review wave, the repair SHALL also address P2 findings from
that wave. P2-only findings SHALL remain visible as non-blocking
recommendations for the user's final decision. A QA case failure SHALL always
block and SHALL not be converted to P2.

## RQ-006 - One shared repair limit

One delivery attempt SHALL share at most three completed automatic repair and
review cycles across selected QA and all selected discovered gates. A cycle
SHALL count only after an accepted repair produces a new immutable snapshot
and every required verification result is recorded. Dispatch failure,
interruption, missing verification, and a result with no repair SHALL not
count.

Before three completed cycles are exhausted, remaining blocking results SHALL
return to repair and SHALL NOT offer Seal skip. After exhaustion, the user may
change the requirement, authorize additional repair, or authorize Seal skips.
An explicitly requested repair for a P2-only recommendation SHALL use the same
shared limit. `PENDING` and `RUNTIME_ERROR` SHALL NOT consume a repair cycle,
and Phase 5 SHALL NOT add a separate runtime retry counter.

## RQ-007 - Narrow Carry responsibility

After repair, selected QA SHALL always rerun. Carry SHALL use only the named
native VCS immediate pre-repair-to-current comparison and SHALL decide
`INHERIT` or `RERUN` only for previously passing selected discovered gates.
Requirements Clarification, Start Readiness, QA Design, skipped gates, and QA
Execution SHALL not be Carry inputs.

## RQ-008 - Explicit Seal authorization

The custom route decision SHALL persist skip authorization for unselected
gates. A selected `PENDING` result means dispatch is incomplete or interrupted;
Resume SHALL continue that dispatch and Seal SHALL reject it. A selected
`RUNTIME_ERROR` result SHALL remain visible and allow retry or explicit user
skip authorization without consuming a repair cycle.

Only repairable blockers, meaning QA FAIL or discovered-gate P0/P1, SHALL wait
for the shared automatic repair limit to be exhausted before Seal skip is
offered. At exhaustion the main agent SHALL present every remaining repairable
blocker once. The user may authorize all or a named subset to skip, authorize
more repair, or change the requirement. P2-only recommendations SHALL be
presented for the user's final decision without blocking Seal.

Seal SHALL reject any selected `PENDING` result and every other required result
without matching pass or user authorization. The retained summary SHALL record
each skip, whether it came from route selection or Seal, and the result status
when authorized.

## RQ-009 - Lightweight generic implementation

Phase 5 SHALL extend the existing run state, lock, prompt catalog, native VCS
routing, action results, QA results, gate results, Carry state, and snapshots.
It SHALL add only state that cannot be derived from those owners: confirmed
gate routing, skip authorization, shared completed-cycle count, extra-cycle
authorization, and finding severity.

It SHALL NOT add a scheduler, dependency graph, second state or evidence
store, custom diff engine, project-specific router, or duplicated phase state.

For this Phase 5 delivery, existing documentation and code SHALL be edited,
consolidated, or removed before new files or parallel abstractions are added.
