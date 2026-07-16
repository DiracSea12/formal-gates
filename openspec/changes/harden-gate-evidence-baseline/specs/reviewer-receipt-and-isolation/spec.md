## Delivery Applicability

Phase 1 reuses the existing dispatch/lifecycle receipt chain and completes the
parts required by its enabled reviewer roles: exact completed-output hash
binding, workflow/gate/stage/snapshot and actual subagent consistency, distinct
open output-path reservation, closure inclusion, and receipt-first,
registration-last finalization. It does not add restricted-path enforcement,
host file-access capture, or a Carry Arbiter role. Phase 2 adds those deferred
rules and applies the existing receipt chain to each newly enabled role in that
role's policy, validator, and tests.

## ADDED Requirements

### Requirement: Formal reviewer results are receipt-bound

Every formal reviewer result and Carry-Forward Arbiter result SHALL reuse the
existing dispatch-registration, subagent start/stop, subagent-ID,
artifact-hash, and receipt-validation chain. Validation SHALL bind workflow,
gate, stage, target snapshot, exact dispatch, host-captured subagent identity,
lifecycle, and output hash. Plain actor labels, provider names, or dispatch IDs
SHALL NOT substitute for a matching receipt.

Reviewer JSON SHALL NOT self-report a session ID. Parallel reviews SHALL be
distinguished by the system-generated dispatch ID, host-captured subagent ID,
distinct review-artifact path, exact output hash, workflow, gate, stage, and
snapshot. Two open dispatches SHALL NOT reserve the same review-artifact path;
registration or finalization SHALL reject the ambiguity rather than select or
combine a result.

Mechanical QA Execution is not a reviewer result: it binds independent
QA-owned execution evidence and SHALL NOT require reviewer dispatch or a
reviewer receipt. Design Review, optional White-box Adequacy, complexity,
architecture, code quality, and Carry-Forward Arbiter remain reviewer results
when their stages are enabled.

The ordinary CLI receipt path proves consistency among local records. It SHALL
remain capable of formal PASS and SHALL NOT claim resistance to an operator who
controls local files and CLI execution.

Receipt finalization SHALL compute completed registration and receipt bytes
before mutating either file, write the receipt completely through a temporary
file first, and atomically replace the open registration last. The registration
replacement SHALL be the commit point. Any earlier failure SHALL leave the
registration open; an unreferenced receipt SHALL NOT authorize PASS.

#### Scenario: Receipt is missing or mismatched

- **WHEN** any required receipt is absent or binds a different lifecycle,
  reviewer, output, workflow, gate, stage, or snapshot
- **THEN** formal PASS is rejected.

#### Scenario: Receipt write fails before commit

- **WHEN** receipt creation or the final registration replacement fails
- **THEN** the dispatch registration remains open and no partial receipt state
  can authorize PASS.

#### Scenario: Parallel reviews use distinct outputs

- **WHEN** two reviews run concurrently with distinct dispatches, subagent IDs,
  and output paths
- **THEN** each output validates only with its own lifecycle events and receipt.

#### Scenario: Parallel reviews reuse one output path

- **WHEN** more than one open dispatch reserves the same review-artifact path
- **THEN** receipt processing rejects the ambiguous path without selecting or
  combining either result.

#### Scenario: Ordinary CLI receipt is valid

- **WHEN** every required receipt binding validates through the existing CLI
- **THEN** formal PASS may proceed without another provider or verifier.

#### Scenario: Host auto-capture is claimed

- **WHEN** documentation claims a host automatically captured lifecycle or file
  access
- **THEN** a same-host live canary must prove that capability; otherwise the
  claim remains unproven while ordinary CLI validation stays available.

### Requirement: Four-gate inputs exclude restricted process history

Sensitive process history SHALL be stored under
`.claude/gates/runs/<workflow-id>/restricted/`. This includes prior verdicts and
findings, repair narratives, old dispatch/context bundles, transition and carry
material, main-agent summaries, chat records, and development-time complexity
budget material.

Four-gate typed context bundles and transitive evidence references SHALL reject
all `restricted/` paths across every run after normalization and symlink
resolution. Current task-related repository material outside that path SHALL
remain broadly readable; the initial reading list SHALL NOT become a repository
whitelist.

#### Scenario: Restricted path is referenced

- **WHEN** a four-gate input directly or transitively references any restricted
  path
- **THEN** the review is invalid and cannot record PASS.

#### Scenario: Reviewer expands current scope

- **WHEN** a reviewer needs another task-related current file outside
  `restricted/`
- **THEN** the reviewer may read it and report the expanded scope.

#### Scenario: Actual file-read visibility is unavailable

- **WHEN** ordinary CLI validation can inspect declared inputs but the host has
  no canaried file-access capture
- **THEN** the system validates declared paths without claiming knowledge of
  every out-of-band file read.

### Requirement: Current confirmed decisions remain visible and binding

Reviewer context and request fields SHALL reference current approved requirement
documents that incorporate every confirmed user decision relevant to the
review. Reviewers MAY report conflicts or implementation mismatch but SHALL NOT
reopen a decision from preference. Missing a decision that can change the
conclusion SHALL yield BLOCKED.

Current requirements, acceptance criteria, scope, and neutral current facts are
allowed. Prior verdicts, findings, repair narratives, target conclusions,
directed focus, and formal-gates rule overrides remain forbidden reviewer
process context.

#### Scenario: Relevant decision is omitted

- **WHEN** supplied current requirement sources omit a decision that can change
  the review
- **THEN** the reviewer returns BLOCKED instead of a complete verdict.

#### Scenario: Historical anchor is supplied

- **WHEN** dispatch or typed reviewer input includes a prior finding, repair
  narrative, expected conclusion, or directed focus
- **THEN** prompt/input validation rejects formal review.

### Requirement: Post-development complexity cannot read development budgets

Post-development complexity SHALL accept a fresh statistics-only report for the
current diff. The report SHALL omit budget, set `budget_source` to `none`, and
leave every budget override false. Development handoff, budget reports, budget
checks, expansion requests, and anti-complexity decisions SHALL remain
restricted and unreachable through transitive evidence references.

#### Scenario: Budget history enters post-development evidence

- **WHEN** post-development complexity input directly or transitively references
  development-time budget material
- **THEN** PASS is rejected.

### Requirement: Carry Arbiter receives the complete repair chain

Carry-Forward Arbiter SHALL use a separate receipt-bound role policy that may
read the complete transition and repair chain because inheritance is its review
object. That material SHALL remain restricted from the four fixed gate
reviewers.

#### Scenario: Carried result needs arbitration

- **WHEN** a carried prerequisite is proposed
- **THEN** a fresh Arbiter receives the full chain while four-gate inputs remain
  isolated from it.
