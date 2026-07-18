## Delivery Applicability

Phase 1 reuses the existing dispatch/lifecycle receipt chain and completes the
parts required by its enabled reviewer roles: exact completed-output hash
binding, workflow/gate/stage/snapshot and actual subagent consistency, distinct
open output-path reservation, closure inclusion, and receipt-first,
registration-last finalization. It does not add restricted-path enforcement,
or a Carry Arbiter role, and it makes no host file-access-capture claim. Phase 2
adds restricted-path and Carry rules and applies the existing receipt chain to
each newly enabled role in that role's policy, validator, and tests. Host
capture claims remain conditional on a successful same-host live canary.

## ADDED Requirements

### Requirement: Formal reviewer results are receipt-bound

Every formal reviewer result and Carry-Forward Arbiter result SHALL reuse the
existing dispatch-registration, subagent start/stop, subagent-ID,
artifact-hash, and receipt-validation chain. Before dispatch, registration
SHALL validate and bind the exact final-send prompt path and hash. Validation
SHALL bind workflow, gate, stage, target snapshot, exact final-send prompt,
host-captured subagent identity, lifecycle, and output hash. Plain actor labels,
provider names, or dispatch IDs
SHALL NOT substitute for a matching receipt.

Reviewer JSON SHALL NOT self-report a session ID. Parallel reviews SHALL be
distinguished by the system-generated dispatch ID, host-captured subagent ID,
distinct review-artifact path, exact output hash, workflow, gate, stage, and
snapshot. Two open dispatches SHALL NOT reserve the same review-artifact path;
registration or finalization SHALL reject the ambiguity rather than select or
combine a result. Before any lifecycle event is captured, registration MAY
replace the single open reservation for that output path with a changed prompt
or route. Once lifecycle capture starts, the reservation SHALL NOT be rebound.

Mechanical QA Execution is not a reviewer result: it binds independent
QA-owned execution evidence and SHALL NOT require reviewer dispatch or a
reviewer receipt. Design Review, complexity, architecture, code quality, and
Carry-Forward Arbiter remain reviewer results when their stages are enabled.

The ordinary CLI receipt path proves consistency among local records. It SHALL
remain capable of formal PASS and SHALL NOT claim resistance to an operator who
controls local files and CLI execution.

Receipt finalization SHALL compute completed registration and receipt bytes
before mutating either file, write the receipt completely through a temporary
file first, and atomically replace the open registration last. The registration
replacement SHALL be the commit point. Any earlier failure SHALL leave the
registration open; an unreferenced receipt SHALL NOT authorize PASS.

Schema version, artifact role, registered workflow, snapshot, gate, stage, and
context-bundle reference are machine-owned fields. Before hashing the completed
reviewer envelope, finalization SHALL write those values from policy and the
dispatch registration. A reviewer transcription error in those fields SHALL NOT
require repeating the review or changing its judgment.

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

#### Scenario: Unstarted dispatch is rebound

- **WHEN** an operator corrects the prompt or route before any lifecycle event
  and registers the same still-empty output path again
- **THEN** the single open reservation is replaced with a new dispatch binding.

#### Scenario: Started dispatch is not rebound

- **WHEN** lifecycle capture has started for an open reservation
- **THEN** another registration for that output path is rejected.

#### Scenario: Ordinary CLI receipt is valid

- **WHEN** every required receipt binding validates through the existing CLI
- **THEN** formal PASS may proceed without another provider or verifier.

#### Scenario: Host auto-capture is claimed

- **WHEN** documentation claims a host automatically captured lifecycle or file
  access
- **THEN** a same-host live canary must prove that capability; otherwise the
  claim remains unproven while ordinary CLI validation stays available.

### Requirement: Every review file is restricted and never reviewer context

Every review-related workflow file SHALL be stored under
`.claude/gates/runs/<workflow-id>/restricted/` without exception. This includes
current and old dispatch copies, context bundles or manifests, reviewer and QA
outputs, receipts, lifecycle events, closures, state, reports, logs,
statistics, verification records, repair history, transition and Carry
material, and main-agent summaries.

The reviewer SHALL receive only the confirmed current requirement, its current
role, and the current diff or proposed change. Worktree, base revision, output
location, and output format MAY be supplied as operational routing only. The
reviewer SHALL read current requirements and changed files directly from the
repository, run its own checks, read no workflow-run artifact, and write only
its assigned output under `restricted/`. The main agent and CLI MAY read
restricted machine evidence to verify prerequisites, receipts, and recording,
but SHALL NOT expose that evidence to the reviewer.

Before dispatch, the orchestrator SHALL use `prompt prepare` to generate the
exact seven-field message. Receipt registration SHALL be the single full
pre-dispatch static check and SHALL reject any mismatch in role, seven fields,
worktree, snapshot, gate/stage output contract, output path, context-bundle
schema/path/hash, policy-specific input placement, unresolved `PENDING`, or
pollution. Only after all checks pass SHALL registration write one machine-generated
`static-validation=PASS` binding into the final prompt and bind those bytes. The reviewer SHALL verify that binding in its
prompt-field check without reading bound files. The orchestrator SHALL send
those exact bytes and append nothing. Finalization SHALL validate the complete
reviewer artifact with dispatch-owned fields applied in memory before writing a
receipt or finalizing the registration. Reviewer and Carry payloads SHALL contain no dispatch or prompt
evidence field. QA Design makes no review judgment and SHALL NOT be forced to
register a reviewer prompt. Receipt registration SHALL reject a fourth
finalized review for the same workflow, gate, and stage unless the user has
explicitly authorized the extra round.

#### Scenario: Prepared prompt is changed before dispatch

- **WHEN** an operator appends or replaces content in the generated prompt
  before the review lifecycle is finalized
- **THEN** registration or finalization rejects the prompt and no receipt or
  formal PASS can use that review.

#### Scenario: Dispatch bundle violates its gate contract

- **WHEN** a context bundle is structurally valid but places a material type
  where the selected gate policy forbids it
- **THEN** receipt registration rejects the dispatch before capacity is
  reserved and no reviewer receives it.

#### Scenario: Reviewer output is invalid before finalization

- **WHEN** the completed reviewer JSON violates its closed schema or selected
  policy
- **THEN** finalization writes no receipt, leaves the dispatch unfinalized, and
  does not rewrite the reviewer bytes.

#### Scenario: Review file is outside restricted

- **WHEN** any review-related workflow file resolves outside the active run's
  `restricted/` directory
- **THEN** validation rejects the workflow without creating an exception.

#### Scenario: Extra reviewer material is supplied

- **WHEN** a reviewer prompt includes a workflow-run artifact, copied project
  rule or requirement file, prior gate result, verification summary, repair
  history, or any substantive input beyond requirement, role, and diff
- **THEN** the review is invalid and cannot record PASS.

#### Scenario: Actual file-read visibility is unavailable

- **WHEN** ordinary CLI validation can inspect declared inputs but the host has
  no canaried file-access capture
- **THEN** the system validates declared paths without claiming knowledge of
  every out-of-band file read.

### Requirement: Current confirmed decisions remain visible and binding

The reviewer prompt SHALL identify current repository requirement documents
that incorporate every confirmed user decision relevant to the review.
Reviewers MAY report conflicts or implementation mismatch but SHALL NOT reopen
a decision from preference. Missing a decision that can change the conclusion
SHALL yield BLOCKED. Requirements SHALL be read from the current repository,
not copied into a workflow run for reviewer consumption.

#### Scenario: Relevant decision is omitted

- **WHEN** supplied current requirement sources omit a decision that can change
  the review
- **THEN** the reviewer returns BLOCKED instead of a complete verdict.

#### Scenario: Historical anchor is supplied

- **WHEN** the reviewer prompt includes a prior finding, repair narrative,
  expected conclusion, directed focus, verification summary, or workflow-run
  artifact
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

### Requirement: Carry Arbiter receives only the cumulative diff

Carry-Forward Arbiter SHALL use a separate receipt-bound role policy and judge
whether the cumulative source-to-target diff invalidates each proposed carried
gate. The transition and repair chain SHALL remain under `restricted/` and be
validated by the CLI outside the Arbiter prompt, exactly like other workflow
material.

#### Scenario: Carried result needs arbitration

- **WHEN** a carried prerequisite is proposed
- **THEN** a fresh Arbiter receives the cumulative diff while the CLI validates
  the full chain outside reviewer context.
