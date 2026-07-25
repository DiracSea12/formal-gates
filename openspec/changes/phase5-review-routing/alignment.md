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
to skip. Selecting QA SHALL require pre-development QA Design, an independent
pre-development QA Review, and post-development QA Execution. Omitting QA SHALL
omit all three actions. QA Review SHALL be automatic when QA is selected and
SHALL NOT appear as another route choice or discovered gate.

Start Readiness SHALL remain a separate binary readiness action. It SHALL run
automatically for full and custom gate flows before development and SHALL NOT
require a separate user selection. It SHALL not use finding severity or consume
a review wave. When an initial none route first becomes non-empty after
development, Start Readiness SHALL run at that point and SHALL pass before the
newly selected gate is prepared or Seal may proceed.

## RQ-003 - Role and transition enforcement

During a formal run, the main agent SHALL own clarification, routing,
orchestration, and result recording but SHALL NOT modify delivery code. A
separate development worker SHALL be the only formal-flow role that modifies
delivery code. The CLI SHALL compose its task only after the current
requirement is confirmed, routing is confirmed, Start Readiness passes, and QA
Review passes after QA Design when QA is selected.

Every prepare and record entrypoint SHALL enforce the same direct transition
preconditions. A command SHALL reject an unselected gate, a skipped
prerequisite, a missing immutable development snapshot, pending repair
verification, or another attempt to cross the documented order. This policy
SHALL be uniform; Phase 5 SHALL NOT add a legacy, compatibility, or relaxed
path. Each transition guard SHALL inspect only the minimum workflow state
needed to prove that operation's direct ordering prerequisites. This minimum
check rule applies only to workflow sequence enforcement; it does not narrow
clarification, readiness, QA, review, or verification scope.

A semantic QA or discovered-gate PASS or FAIL recorded for an immutable
snapshot SHALL remain authoritative for that snapshot and SHALL not be replaced
by duplicate dispatch or recording. Retry SHALL remain available for PENDING
and RUNTIME_ERROR. Resume SHALL be able to recompose a prepared development or
repair task after normal interruption because prepared prompts are not retained.

## RQ-004 - Route changes and invalidation

Only an explicit user decision may change the selected gate set. A gate may be
added only before its required workflow node has passed. A selected gate SHALL
not be silently removed; it must pass or receive user skip authorization at
Seal. QA cannot be added after development begins because its design action is
pre-development. A post-development gate may be added after development but
before Seal. If that addition changes an initial none route into a non-empty
selection, the workflow SHALL require the deferred Start Readiness action; it
SHALL not discard the current immutable development snapshot merely because
readiness ran after it. The confirmed none, full, or custom route SHALL remain
the run's single routing decision across requirement revisions; the workflow
SHALL NOT ask the user to choose the route again.

When the bound requirement revision changes, Resume SHALL pause without
preserving or invalidating dependent results until the main agent explicitly
classifies the change by meaning. A meaning-preserving change leaves the
confirmed outcome, acceptance, public behavior, and consequential solution
boundaries unchanged and SHALL preserve dependent results. A meaning-changing
classification SHALL invalidate the confirmed requirement and dependent
readiness, QA approval and execution, development, and review results and return
to Requirements Clarification. Previously approved QA cases SHALL remain only
as unapproved input to the next QA Design. That QA Design SHALL inspect the
complete current requirement and every prior case, retain confirmed unaffected
cases, and add, modify, or remove only affected cases. It SHALL replace the
complete case set only when the impact cannot be bounded reliably or the change
alters the overall workflow. Development SHALL remain blocked until this
coverage pass produces a complete case set and independent QA Review passes it.
Classification and QA impact SHALL depend on semantic effect, not an enumerated
list of edit types. If the main agent cannot establish that meaning is
preserved, it SHALL clarify with the user instead of guessing.

## RQ-005 - Independent QA Review

When QA is selected, the CLI SHALL compose QA Review only after QA Design has
recorded a complete candidate case set. A separate reviewer SHALL read the
current confirmed requirement and that complete case set, but SHALL NOT receive
production implementation, implementation diffs, tests, developer explanation,
post-development results, or another reviewer's conclusion. It SHALL check
complete requirement coverage, case necessity and duplication, documented
public procedures, observable oracles, the normal-use project boundary, and
lowest-layer ownership.

QA Review SHALL return `PASS`, `FAIL` with findings, or `RUNTIME_ERROR`. `PASS`
is required before development. `FAIL` SHALL reopen QA Design with the current
cases as unapproved rework input, and the revised complete set SHALL undergo a
new independent QA Review. `RUNTIME_ERROR` and interrupted `PENDING` work SHALL
remain retryable without reopening QA Design. This pre-development loop SHALL
not consume or alter the post-development review-wave count.

## RQ-006 - Severity and QA routing

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

## RQ-007 - One shared review-wave limit

One delivery attempt SHALL share at most three completed automatic review waves
across selected QA and all selected discovered gates. The initial complete
post-development wave SHALL count once, and each complete post-repair wave
SHALL count once. A wave SHALL count only after every result required for the
current immutable snapshot is recorded and none is `PENDING` or
`RUNTIME_ERROR`. Its semantic outcome does not affect counting: PASS, P2-only,
QA FAIL, and discovered-gate P0/P1 waves all count when complete. QA and all
selected discovered gates in the same wave SHALL share that one increment.

Each immutable snapshot SHALL be counted at most once. Dispatch failure,
interruption, missing verification, `PENDING`, and `RUNTIME_ERROR` SHALL leave
the wave incomplete and SHALL not count. Before three completed waves are
exhausted, remaining blocking results SHALL return to repair and SHALL NOT
offer Seal skip. After exhaustion, the user may change the requirement,
authorize additional repair, or authorize Seal skips. An explicitly requested
repair for a P2-only recommendation SHALL use the same shared limit, and Phase
5 SHALL NOT add a separate runtime retry counter.

## RQ-008 - Narrow Carry responsibility

After repair, selected QA SHALL always rerun. Carry SHALL use only the named
native VCS immediate pre-repair-to-current comparison and SHALL decide
`INHERIT` or `RERUN` only for previously passing selected discovered gates.
Requirements Clarification, Start Readiness, QA Design, QA Review, skipped
gates, and QA Execution SHALL not be Carry inputs.

## RQ-009 - Explicit Seal authorization

The custom route decision SHALL persist skip authorization for unselected
gates. A selected `PENDING` result means dispatch is incomplete or interrupted;
Resume SHALL continue that dispatch and Seal SHALL reject it. A selected
`RUNTIME_ERROR` result SHALL remain visible and allow retry or explicit user
skip authorization without completing a review wave.

Only repairable blockers, meaning QA FAIL or discovered-gate P0/P1, SHALL wait
for the shared automatic review-wave limit to be exhausted before Seal skip is
offered. At exhaustion the main agent SHALL present every remaining repairable
blocker once. The user may authorize all or a named subset to skip, authorize
more repair, or change the requirement. P2-only recommendations SHALL be
presented for the user's final decision without blocking Seal.

Seal SHALL reject any selected `PENDING` result and every other required result
without matching pass or user authorization. The retained summary SHALL record
each skip, whether it came from route selection or Seal, and the result status
when authorized. A named Seal authorization SHALL persist even when the same
Seal attempt remains blocked by another unresolved selected result. Seal-origin
authorization SHALL apply only to the immutable snapshot whose result the user
authorized and SHALL be cleared when a later repair snapshot is recorded.
Route-origin authorization SHALL remain unchanged across snapshots.

## RQ-010 - Lightweight generic implementation

Phase 5 SHALL extend the existing run state, lock, prompt catalog, native VCS
routing, action results, QA results, gate results, Carry state, and snapshots.
It SHALL add only state that cannot be derived from those owners: confirmed
gate routing, skip authorization, shared completed-wave count, extra-wave
authorization, and finding severity. Seal skip authorization SHALL reuse the
existing snapshot identity. Existing action state SHALL mark whether the current
immutable snapshot's wave has already been counted; Phase 5 SHALL not add a
second per-wave state owner.

It SHALL NOT add a scheduler, dependency graph, second state or evidence
store, custom diff engine, project-specific router, or duplicated phase state.

For this Phase 5 delivery, existing documentation and code SHALL be edited,
consolidated, or removed before new files or parallel abstractions are added.
