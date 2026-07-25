## ADDED Requirements

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
discovered gate in one ordered list. The user SHALL choose no gate flow, the
full list, or a custom subset in one routing decision. Selected gates SHALL be
required; custom omissions SHALL receive route skip authorization.

#### Scenario: QA selection owns both QA actions

- **WHEN** QA is selected
- **THEN** QA Design is required before development and QA Execution is
  required after development

#### Scenario: QA omission removes both QA actions

- **WHEN** QA is not selected
- **THEN** neither QA Design nor QA Execution blocks the selected flow

#### Scenario: Discovered gate count changes

- **WHEN** the current valid package gate files change before a new run starts
- **THEN** the full-flow list reflects the existing dynamic catalog without a
  fixed count or registry

### Requirement: Direct transition enforcement

The main agent SHALL not modify delivery code during a formal run. The CLI
SHALL compose a separate development-worker task only after the requirement,
route, Start Readiness, and selected QA Design prerequisites pass. Every
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

### Requirement: User-owned route changes

Only explicit user direction SHALL add a gate after routing. A gate SHALL be
addable only before its prerequisite node has passed. A selected gate SHALL
remain required until it passes or the user authorizes a Seal skip. A semantic
requirement or consequential solution change SHALL invalidate dependent
results except the confirmed route and SHALL return the run to clarification.
The workflow SHALL ask for none, full, or custom routing only once per run. A
changed requirement revision
SHALL first pause for an explicit semantic-effect classification. A
meaning-preserving classification SHALL rebind the revision and preserve
dependent results. Classification SHALL depend on whether confirmed meaning
changed, not an enumerated set of edit types; uncertainty SHALL return to user
clarification.

#### Scenario: QA cannot be inserted after development starts

- **WHEN** the user requests QA after the pre-development QA Design point
- **THEN** the current run rejects the insertion instead of backfilling QA

#### Scenario: Semantic change preserves the one route decision

- **WHEN** the bound requirement changes meaning
- **THEN** prior confirmation, readiness, QA, development, and review results
  cannot authorize continued delivery, while the confirmed route remains and
  no second routing prompt is shown

#### Scenario: Meaning-preserving change keeps routing

- **WHEN** the bound revision changes but the confirmed outcome, acceptance,
  public behavior, and consequential solution boundaries remain unchanged
- **THEN** Resume pauses for classification and an explicit
  meaning-preserving decision rebinds the revision without discarding results

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

### Requirement: One shared completed-cycle limit

Selected QA and selected discovered gates SHALL share three completed
automatic repair-review cycles. A cycle SHALL count once only after a repair
produces a new immutable snapshot and all required verification for that
snapshot is recorded. Failed dispatch, interruption, missing verification,
and no-repair results SHALL not count. PENDING and RUNTIME_ERROR SHALL not
consume the limit, and the workflow SHALL not maintain a separate runtime
retry counter.

#### Scenario: QA and different gates share one count

- **WHEN** repairs originating from QA and two different gates complete
  verification
- **THEN** all three consume the same delivery-level count

#### Scenario: Incomplete verification does not count

- **WHEN** a repair snapshot exists but one required selected result remains
  pending
- **THEN** the completed-cycle count remains unchanged

### Requirement: Carry remains selected-gate routing

After repair, selected QA SHALL rerun. Carry SHALL use the native immediate
repair comparison and SHALL decide INHERIT or RERUN only for previously passing
selected discovered gates.

#### Scenario: Skipped gate is not a Carry input

- **WHEN** a repair follows a custom route with an unselected gate
- **THEN** Carry omits that gate regardless of its catalog presence

### Requirement: Seal requires explicit unresolved authorization

Seal SHALL enforce explicit authorization for unresolved selected results.
Before three completed cycles are exhausted, a selected QA FAIL or
discovered-gate P0/P1 result SHALL return to repair and SHALL not offer Seal
skip. After exhaustion, Seal SHALL require authorization for every remaining
repairable blocker. Custom omissions SHALL use their route authorization. A
selected PENDING result SHALL block Seal until Resume completes its dispatch.
A selected RUNTIME_ERROR SHALL allow retry or immediate explicit user skip
authorization. P2-only PASS recommendations SHALL remain visible for final
user decision without blocking Seal. The retained summary SHALL identify each
authorization source and result status.

#### Scenario: Early skip is rejected

- **WHEN** a selected gate remains blocking before three completed cycles
- **THEN** Seal rejects skip authorization and returns the run to repair

#### Scenario: Partial exhausted-limit authorization is rejected

- **WHEN** three cycles are exhausted and the user authorizes only some of the
  selected non-passing gates
- **THEN** Seal remains blocked by every unapproved result

#### Scenario: Interrupted dispatch cannot be sealed

- **WHEN** a selected QA or discovered gate remains PENDING
- **THEN** Seal rejects it and Resume continues the unfinished dispatch

#### Scenario: Runtime error does not wait for repair exhaustion

- **WHEN** a selected QA or discovered gate returns RUNTIME_ERROR
- **THEN** the user may retry it or explicitly authorize its skip without
  changing the completed repair-cycle count

#### Scenario: Additional repair is user-authorized

- **WHEN** three cycles are exhausted and the user authorizes another repair
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
