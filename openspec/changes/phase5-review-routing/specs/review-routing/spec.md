## ADDED Requirements

This specification uses the current routing contract from
`../../../universal-modification-intake/alignment.md`: lightweight execution
creates no formal run, and formal routing accepts only `full` or `custom`.

### Requirement: Adaptive clarification blocks development

Requirements Clarification SHALL adapt to the actual request and SHALL align
both the requested outcome and consequential technical choices. The main agent
SHALL remain read-only until the user confirms shared understanding. The CLI
SHALL not prepare development until the confirmed requirement and required
pre-development results are recorded.

#### Scenario: Unconfirmed solution cannot enter development

- **WHEN** the outcome is known but a consequential technical choice remains
  unconfirmed
- **THEN** development preparation is rejected and clarification continues

#### Scenario: Repository facts are not user questions

- **WHEN** a possible clarification can be answered from the repository
- **THEN** the main agent inspects that fact and asks only the remaining user
  decision

### Requirement: One unified dynamic gate selection

After clarification, the main agent SHALL present QA and every dynamically
discovered gate in one ordered list. A formal run SHALL choose the full list or
a non-empty custom subset in one routing decision. Selected gates SHALL be
required; custom omissions SHALL receive route skip authorization.

#### Scenario: QA selection owns the complete QA flow

- **WHEN** QA is selected
- **THEN** QA Design and independent QA Review are required in that order before
  development, and QA Execution is required after development

#### Scenario: QA omission removes the complete QA flow

- **WHEN** QA is not selected
- **THEN** QA Design, QA Review, and QA Execution are all omitted

#### Scenario: Discovered gate count changes

- **WHEN** the current valid package gate files change before a new run starts
- **THEN** the full-flow list reflects the existing dynamic catalog without a
  fixed count or registry

### Requirement: Direct transition enforcement

The main agent SHALL not modify delivery code during a formal run. The CLI
SHALL compose a separate development-worker task only after the requirement,
route, Start Readiness, and selected QA Review prerequisites pass. Every
prepare and record entrypoint SHALL independently reject a transition whose
direct prerequisite is not satisfied. A transition guard SHALL inspect only
the minimum workflow state needed to establish those direct ordering
prerequisites; this rule SHALL NOT reduce any clarification, readiness, QA,
review, or verification scope.

#### Scenario: Direct recording cannot bypass preparation order

- **WHEN** a normal operator calls a record command before its required stage
- **THEN** the CLI rejects it without changing run state

#### Scenario: Non-QA development does not require QA cases

- **WHEN** a confirmed route omits QA and Start Readiness passes
- **THEN** development can be prepared without QA cases

#### Scenario: Formal routing always runs readiness

- **WHEN** a formal run selects full or custom routing
- **THEN** Start Readiness passes before development and a later discovered-gate
  addition keeps that result and the current immutable development snapshot

#### Scenario: Completed semantic result is immutable on its snapshot

- **WHEN** QA or a discovered gate already has PASS or FAIL recorded for the
  current immutable snapshot
- **THEN** duplicate preparation or recording cannot replace that result, while
  PENDING and RUNTIME_ERROR remain retryable

#### Scenario: Prepared development dispatch resumes

- **WHEN** normal interruption occurs after a development or repair task is
  prepared but before the host retains or completes its dispatch
- **THEN** Resume can recompose the current prepared task without advancing or
  resetting its workflow boundary

### Requirement: Independent QA Review gates development

When QA is selected, the CLI SHALL compose QA Review only after QA Design has
recorded a complete candidate case set. A separate reviewer SHALL inspect the
confirmed requirement and complete candidate set without production
implementation, implementation diffs, tests, developer explanations,
post-development results, or another reviewer's conclusion. It SHALL check
complete coverage, necessity and duplication, documented public procedures,
observable oracles, the normal-use project boundary, and lowest-layer
ownership. QA Review PASS SHALL be required before development and SHALL not
consume the post-development review-wave limit.

#### Scenario: QA Review cannot run before design

- **WHEN** QA is selected but QA Design has not recorded a complete candidate
  set
- **THEN** QA Review preparation and recording are rejected without state
  mutation

#### Scenario: QA Review PASS unlocks development

- **WHEN** an independent QA Review passes the complete candidate case set and
  all other direct prerequisites pass
- **THEN** development preparation succeeds without incrementing a review wave

#### Scenario: QA Review FAIL returns to design rework

- **WHEN** QA Review returns FAIL with findings
- **THEN** the candidate cases remain as unapproved rework input, QA Design
  reopens, development stays blocked, and the revised complete set requires a
  fresh independent QA Review

#### Scenario: QA Review interruption is retryable

- **WHEN** QA Review is PENDING or returns RUNTIME_ERROR
- **THEN** that review can be resumed or retried without reopening QA Design or
  consuming a review wave

### Requirement: User-owned route changes

Only explicit user direction SHALL add a gate after routing. A gate SHALL be
addable only before its prerequisite node has passed. A selected gate SHALL
remain required until it passes or the user authorizes a Seal skip. A semantic
requirement or consequential solution change SHALL invalidate dependent
results except the confirmed route and SHALL return the run to clarification.
The workflow SHALL ask for routing only once. Lightweight execution SHALL NOT
create a formal run, and a formal run SHALL accept only full or custom routing.
A changed requirement revision SHALL first pause for an explicit
semantic-effect classification. A
meaning-preserving classification SHALL rebind the revision and preserve
dependent results. Both semantic classifications SHALL bind the current native
VCS identity containing the changed revision so later development and Seal use
the live snapshot. Classification SHALL depend on whether confirmed meaning
changed, not an enumerated set of edit types; uncertainty SHALL return to user
clarification.

#### Scenario: QA cannot be inserted after development starts

- **WHEN** the user requests QA after the pre-development QA Design point
- **THEN** the current run rejects the insertion instead of backfilling QA

#### Scenario: Prepared work freezes the selected set

- **WHEN** a development or repair worker has been prepared but its snapshot
  has not yet been recorded
- **THEN** a route addition is rejected without changing the selected set

#### Scenario: A completed wave accepts a late discovered gate

- **WHEN** the current review wave completed and the user explicitly adds an
  omitted discovered gate before Seal
- **THEN** only that gate is reviewed on the unchanged snapshot, previously
  completed work does not rerun, and the wave count remains unchanged
- **AND** its RUNTIME_ERROR requires retry or matching explicit authorization
  before repair or Seal
- **WHEN** a repair snapshot is awaiting verification
- **THEN** a route addition is rejected without changing the selected set

#### Scenario: Semantic change preserves the one route decision

- **WHEN** the bound requirement changes meaning
- **THEN** prior confirmation, readiness, QA, development, and review results
  cannot authorize continued delivery, while the confirmed route remains and
  no second routing prompt is shown

#### Scenario: Changed meaning revalidates QA incrementally

- **WHEN** a meaning-changing revision has a reliably bounded effect on some
  previously approved QA cases
- **THEN** QA Design reviews the complete current requirement and every prior
  case, preserves confirmed unaffected cases, changes only affected cases, and
  blocks development until the resulting complete set passes QA Review

#### Scenario: Unbounded QA impact triggers full redesign

- **WHEN** QA Design cannot reliably bound which prior cases a semantic change
  affects or the change alters the overall workflow
- **THEN** it replaces the complete QA case set instead of assuming unaffected
  coverage

#### Scenario: Meaning-preserving change keeps routing

- **WHEN** the bound revision changes but the confirmed outcome, acceptance,
  public behavior, and consequential solution boundaries remain unchanged
- **THEN** Resume pauses for classification and an explicit
  meaning-preserving decision rebinds the revision and live VCS snapshot without
  discarding results

#### Scenario: Built-in QA ID cannot collide with a gate

- **WHEN** the installed gate directory contains a direct `qa.md` file
- **THEN** catalog loading rejects it before routing can expose duplicate IDs

#### Scenario: Uncertain meaning is not guessed

- **WHEN** the main agent cannot establish that a changed revision preserves
  the confirmed meaning
- **THEN** it asks the user instead of selecting the preserving path

### Requirement: Severity and QA have explicit routing

Every discovered-gate finding SHALL carry exactly one of P0, P1, or P2 based
on impact. P0 means a systemic severe consequence or unusable core capability;
P1 means a confirmed requirement, acceptance, or architecture-boundary
violation; P2 means an improvement without a confirmed behavior violation.
A gate SHALL return PASS with no findings or P2-only findings, FAIL with at
least one P0/P1 and optional P2, or RUNTIME_ERROR with no findings. QA case
failure SHALL always block without being converted to P2.

#### Scenario: Blocking wave includes its recommendations

- **WHEN** one review wave contains P1 and P2 findings
- **THEN** the repair input contains both findings

#### Scenario: P2-only wave reaches user decision

- **WHEN** all selected executable results pass except for P2 recommendations
- **THEN** the PASS recommendations remain visible for the user's final
  decision and do not automatically start repair or block Seal

#### Scenario: FAIL cannot contain only recommendations

- **WHEN** a discovered gate result contains only P2 findings
- **THEN** the CLI rejects FAIL and accepts the findings only with PASS

#### Scenario: Invalid severity is rejected

- **WHEN** a discovered-gate finding has another severity value
- **THEN** the result is rejected without mutating run state

### Requirement: One shared completed review-wave limit

Selected QA and selected discovered gates SHALL share three completed automatic
review waves. The initial post-development wave and every post-repair wave SHALL
each count once when every selected result for its immutable snapshot is
recorded and none is PENDING or RUNTIME_ERROR. A complete PASS, P2-only, QA
FAIL, or discovered-gate P0/P1 wave SHALL count; QA and all selected discovered
gates in that wave SHALL produce only one increment. Each immutable snapshot
SHALL count at most once. Failed dispatch, interruption, missing verification,
PENDING, and RUNTIME_ERROR SHALL leave the wave incomplete and SHALL not consume
the limit. The workflow SHALL not maintain a separate runtime retry counter.

#### Scenario: Initial wave counts

- **WHEN** every selected review result for the initial immutable development
  snapshot completes without PENDING or RUNTIME_ERROR
- **THEN** the delivery-level completed-wave count increases from zero to one

#### Scenario: QA and different gates share one wave count

- **WHEN** QA and multiple discovered gates all complete for one immutable
  snapshot
- **THEN** that complete wave increases the delivery-level count only once,
  regardless of whether its semantic result passes or requires repair

#### Scenario: Incomplete verification does not count

- **WHEN** an initial or repair snapshot exists but one required selected result
  remains pending or has RUNTIME_ERROR
- **THEN** the completed-wave count remains unchanged

#### Scenario: Duplicate submission does not recount a snapshot

- **WHEN** a complete wave has already incremented the count for its immutable
  snapshot
- **THEN** retrying or repeating result submission cannot increment it again

### Requirement: Carry remains selected-gate routing

After repair, selected QA SHALL rerun. Carry SHALL use the native immediate
repair comparison and SHALL decide INHERIT or RERUN only for previously passing
selected discovered gates.

#### Scenario: Skipped gate is not a Carry input

- **WHEN** a repair follows a custom route with an unselected gate
- **THEN** Carry omits that gate regardless of its catalog presence

### Requirement: Seal requires explicit unresolved authorization

Seal SHALL enforce explicit authorization for unresolved selected results.
Before three completed review waves are exhausted, a selected QA FAIL or
discovered-gate P0/P1 result SHALL return to repair and SHALL not offer Seal
skip. After exhaustion, Seal SHALL require authorization for every remaining
repairable blocker. Custom omissions SHALL use their route authorization. A
selected PENDING result SHALL block Seal until Resume completes its dispatch.
A selected RUNTIME_ERROR SHALL allow retry or immediate explicit user skip
authorization. P2-only PASS recommendations SHALL remain visible for final
user decision without blocking Seal. The retained summary SHALL identify each
authorization source and result status.

#### Scenario: Early skip is rejected

- **WHEN** a selected gate remains blocking before three completed review waves
- **THEN** Seal rejects skip authorization and returns the run to repair

#### Scenario: Partial exhausted-limit authorization is rejected

- **WHEN** three review waves are exhausted and the user authorizes only some
  of the selected non-passing gates
- **THEN** Seal remains blocked by every unapproved result

#### Scenario: Partial authorization persists

- **WHEN** the user authorizes a named subset but the same Seal attempt remains
  blocked by another selected result
- **THEN** the named authorization is saved and remains effective after normal
  continuation or Resume

#### Scenario: Repair snapshot clears prior Seal authorization

- **WHEN** a named Seal authorization is recorded for one snapshot and the user
  then authorizes another repair that produces a new immutable snapshot
- **THEN** the prior Seal authorization cannot cover a result from the new
  snapshot, while route-origin authorization remains unchanged

#### Scenario: Interrupted dispatch cannot be sealed

- **WHEN** a selected QA or discovered gate remains PENDING
- **THEN** Seal rejects it and Resume continues the unfinished dispatch

#### Scenario: Runtime error does not wait for repair exhaustion

- **WHEN** a selected QA or discovered gate returns RUNTIME_ERROR
- **THEN** the user may retry it or explicitly authorize its skip without
  completing the current review wave

#### Scenario: Authorized runtime error does not strand another blocker

- **WHEN** one selected result has an authorized RUNTIME_ERROR and another
  selected result on the same snapshot requires repair
- **THEN** repair may proceed without counting the incomplete review wave

#### Scenario: Additional repair is user-authorized

- **WHEN** three review waves are exhausted and the user authorizes another repair
- **THEN** the effective limit for that run increases and the normal repair
  path resumes

### Requirement: Implementation stays generic and minimal

The implementation SHALL reuse the existing run state, lock, prompt catalog,
native VCS routing, results, Carry, and snapshots. It SHALL not add a scheduler,
dependency graph, duplicate phase state, compatibility path, project-specific
router, second state store, or custom diff engine.

#### Scenario: Different project vocabulary does not change routing

- **WHEN** the workflow is used with another requirement source, language, or
  repository layout
- **THEN** the same generic route and transition rules apply without a project
  lookup table
