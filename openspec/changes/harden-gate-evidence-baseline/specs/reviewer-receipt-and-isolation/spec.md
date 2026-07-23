## Delivery Applicability

Phase 1 reuses the existing dispatch receipt chain and completes the
parts required by its enabled reviewer roles: exact completed-output hash
binding, workflow/gate/stage/snapshot and, where the provider supplies usable
lifecycle events, actual subagent consistency; it also requires distinct
open output-path reservation, closure inclusion, and receipt-first,
registration-last finalization. It does not add restricted-path enforcement,
or a Carry Arbiter role, and it makes no host file-access-capture claim. Phase 2
adds restricted-path and Carry rules and applies the existing receipt chain to
each newly enabled role in that role's policy, validator, and tests. Host
capture claims remain conditional on a successful same-host live canary.

## ADDED Requirements

### Requirement: Formal reviewer results are receipt-bound

Every formal reviewer result and Carry-Forward Arbiter result SHALL reuse the
existing dispatch-registration, artifact-hash, and receipt-validation chain.
Providers with usable lifecycle events SHALL also bind subagent start/stop and
subagent ID. Before dispatch, registration
SHALL validate and bind the exact final-send prompt path and hash. Validation
SHALL bind workflow, gate, stage, target snapshot, exact final-send prompt,
semantic submission proof, and output hash. Lifecycle-capable providers SHALL
also bind host-captured subagent identity and lifecycle. Plain actor labels,
provider names, or dispatch IDs
SHALL NOT substitute for a matching receipt.

Reviewer JSON SHALL NOT self-report a session ID. Parallel reviews SHALL be
distinguished by the system-generated dispatch ID, distinct review-artifact
path, exact output hash, workflow, gate, stage, and snapshot. Providers with
usable lifecycle events SHALL additionally bind the host-captured subagent ID.
Two open dispatches SHALL NOT reserve the same review-artifact path;
registration or finalization SHALL reject the ambiguity rather than select or
combine a result. Before any lifecycle event is captured on a capable provider, registration MAY
replace the single open reservation for that output path with a changed prompt
or route. Once lifecycle capture starts, the reservation SHALL NOT be rebound.

Mechanical QA Execution is not a reviewer result: it binds independent
QA-owned execution evidence and SHALL NOT require reviewer dispatch or a
reviewer receipt. Design Review, complexity, architecture, code quality, and
Carry-Forward Arbiter remain reviewer results when their stages are enabled.

Codex SHALL retain its existing `SubagentStart` / `SubagentStop` hook
installation, but SHALL NOT require those events in receipt finalization or
validation. Its receipt SHALL retain every non-lifecycle binding above. No replacement
agent tracker, session manager, event emulation, compatibility alias, or manual
capture fallback SHALL be added.

The ordinary CLI receipt path proves consistency among local records. It SHALL
remain capable of formal PASS and SHALL NOT claim resistance to an operator who
controls local files and CLI execution.

Receipt finalization SHALL compute completed registration and receipt bytes
before mutating either file, write the receipt completely through a temporary
file first, and atomically replace the open registration last. The registration
replacement SHALL be the commit point. Any earlier failure SHALL leave the
registration open; an unreferenced receipt SHALL NOT authorize PASS.

Schema/version, artifact role, registered workflow/snapshot, gate/stage,
policy/check catalog, context bundle, evidence references, paths, hashes,
bindings, and aggregate verdict are machine-owned fields. Registration SHALL
generate the complete static projection and an immutable catalog proof before
dispatch. The reviewer SHALL pass only ordered status, message, finding, and
location values to `receipt submit`; a Carry Arbiter SHALL pass only ordered
decision and reason values. Reviewer submission SHALL reject Carry and QA
Design fields; Carry submission SHALL reject reviewer and QA Design fields;
QA Design submission SHALL reject reviewer and Carry fields. Submission SHALL reject unknown, duplicate,
missing, wrongly typed, or illegal values before changing the artifact, then
atomically record the composed PENDING artifact hash/proof. Finalization SHALL
require that proof and mechanically derive the verdict. No reviewer SHALL edit
formal JSON, transcribe, or confirm a machine-owned field.
While the same dispatch remains open and unfinalized, every submission role
MAY use the existing `receipt submit` command to replace its complete semantic
submission when the existing valid `SemanticSubmissionSHA` exactly matches the
current artifact bytes. The CLI SHALL rebuild from the original static catalog,
rerun all validation, and atomically replace the artifact and dispatch proof.
A manually changed artifact, missing or mismatched submission SHA, finalized
dispatch, incomplete input, or other rejection SHALL leave both files
byte-for-byte unchanged. No reset/reopen command or state SHALL be added.

#### Scenario: Receipt is missing or mismatched

- **WHEN** any required receipt is absent or binds a different reviewer output,
  workflow, gate, stage, snapshot, prompt, submission, or provider-required lifecycle
- **THEN** formal PASS is rejected.

#### Scenario: Receipt write fails before commit

- **WHEN** receipt creation or the final registration replacement fails
- **THEN** the dispatch registration remains open and no partial receipt state
  can authorize PASS.

#### Scenario: Parallel reviews use distinct outputs

- **WHEN** two reviews run concurrently with distinct dispatches and output paths
- **THEN** each output validates only with its own receipt and, for a
  lifecycle-capable provider, its own lifecycle events and subagent ID.

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

#### Scenario: Codex finalizes without lifecycle events

- **WHEN** a Codex dispatch, exact prompt, semantic submission, artifact hash,
  and all other non-lifecycle receipt bindings validate without start/stop events
- **THEN** receipt finalization and formal validation proceed without claiming
  or synthesizing lifecycle evidence.

### Requirement: Every review file is restricted and never reviewer context

Every review-related workflow file SHALL be stored under
`.gates/runs/<workflow-id>/restricted/` without exception. This includes
current and old dispatch copies, context bundles or manifests, reviewer and QA
outputs, receipts, lifecycle events, closures, state, reports, logs,
verification records, repair history, transition and Carry
material, and main-agent summaries.

The reviewer SHALL receive only the confirmed current requirement, its current
role, and the current diff or proposed change. Worktree, base revision, output
location, and output format MAY be supplied as operational routing only. The
reviewer SHALL read current requirements and changed files directly from the
repository, run its own checks, and under the workflow run read only its
assigned CLI-generated catalog and write the result through `receipt submit`.
It SHALL NOT edit formal JSON or open
referenced evidence or any other workflow-run artifact. The main agent and CLI MAY read
restricted machine evidence to verify prerequisites, receipts, and recording,
but SHALL NOT expose that evidence to the reviewer.

For a post-development target, the orchestrator SHALL generate `Current diff`
with the on-site external VCS and provide it as operational reviewer input. It
SHALL NOT expose an internal workflow-run evidence file as the project diff.
Formal-gates SHALL retain the workflow snapshot identity and static review
evidence.

The `.gates` root SHALL be shared by Claude Code, Codex, Cursor, and any other
supported host for the same project. Host-specific install, skill, and hook
files MAY remain under the locations required by each host.

Formal closure evidence remains in the active run's `restricted/` directory
after seal. Every disposable operational file from a development-readiness or
post-development gate flow SHALL instead use one shared
`.gates/tmp/<flow-id>/` directory. The flow ID is one directory name selected
for that flow. `workflow cleanup` SHALL remove only `.gates/tmp` or one named
child; it SHALL not delete formal closure evidence. Superseded flows use their
own temporary directory and are removed as whole directories after the later
flow seals. Successful `workflow final-verification --record-final-qa` SHALL
remove the shared temporary root after it records FinalExecution. The Codex
hook canary SHALL always use `.gates/tmp/codex-hook-canary/` and SHALL NOT
offer a caller-selected output directory.

Before dispatch, the orchestrator SHALL use `prompt prepare` to generate the
exact seven-field message. Receipt registration SHALL be the single full
pre-dispatch static check and catalog generator and SHALL reject any mismatch
in role, seven fields, worktree, snapshot, gate/stage output contract, output
path, context-bundle schema/path/hash, policy-specific input placement,
unresolved `PENDING`, or pollution. It SHALL bind the exact-send prompt and
generate all static catalog fields, including evidence references supplied by
role-specific CLI path flags. Design Review SHALL accept separate case-set and
Design-receipt paths, and Carry SHALL accept source closure paths from which the CLI derives
fixed gates. Registration SHALL NOT accept caller-authored check-ID/path or
gate/path bindings. The orchestrator SHALL send those exact prompt bytes
and append nothing. The reviewer SHALL make no prompt-field confirmation;
`review.prompt-semantics` remains its only prompt-owned semantic check. The
reviewer SHALL call `receipt submit`; successful submission SHALL record the
artifact hash/proof and finalization SHALL require it, aggregate the semantic
values, and write the final artifact and receipt before finalizing the registration. Reviewer and
Carry payloads SHALL contain no dispatch or prompt evidence field. QA Design
makes no review judgment and SHALL NOT be forced to register a reviewer prompt,
but its formal static content SHALL still be generated by the CLI. Its semantic
owner SHALL submit seven ordered values per generated case through `receipt
submit`; the owner SHALL NOT edit Case IDs, field labels, separators, or newline
layout, and finalization SHALL require the CLI-recorded submission hash. Receipt
registration SHALL reject a fourth
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

#### Scenario: Reviewer changes a static template field

- **WHEN** a reviewer changes an envelope field, policy/check ID, evidence
  reference, path/hash/binding, source gate, or other generated value
- **THEN** finalization rejects the template, writes no receipt or final
  artifact, and leaves the dispatch open.

#### Scenario: Semantic owner submits only complete semantic values

- **WHEN** every required role-specific semantic value is supplied through
  `receipt submit`, either initially or as a valid full replacement on the same
  open, unfinalized dispatch
- **THEN** finalization derives the verdict and writes the final formal
  artifact and receipt without AI-authored static content.

#### Scenario: Semantic submission is invalid or no longer replaceable

- **WHEN** semantic input is incomplete, duplicated, unknown, wrongly typed, or
  violates the selected policy, the artifact bytes no longer match the recorded
  `SemanticSubmissionSHA`, that SHA is missing or invalid, or the dispatch is
  finalized
- **THEN** submission leaves the assigned artifact and dispatch proof
  byte-for-byte unchanged and creates no reset/reopen state.

#### Scenario: Review file is outside restricted

- **WHEN** any review-related workflow file resolves outside the active run's
  `restricted/` directory
- **THEN** validation rejects the workflow without creating an exception.

#### Scenario: Workflow uses a host-named run root

- **WHEN** a workflow supplies a host-specific evidence root
- **THEN** validation rejects it and requires `.gates`.

#### Scenario: Seal cleanup preserves formal evidence

- **WHEN** an operator cleans a sealed workflow's temporary files
- **THEN** the CLI removes only `.gates/tmp` or the requested flow ID below it,
  leaving the sealed run's `restricted/` closure unchanged.

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

### Requirement: Native install configures selected host hooks by default

The native installer SHALL copy the runtime/skill subset and configure native
hooks for every selected Claude Code, Codex, or Cursor target by default in
both global and project scopes. A caller MAY skip hook configuration only with
the explicit `--skip-hooks` flag. The installer SHALL reject the obsolete
`--configure-hooks` flag and SHALL NOT provide a compatibility alias.

Hook configuration SHALL preserve unrelated non-formal-gates entries and
replace only formal-gates entries. A hook configuration read, merge, directory,
backup, or write failure SHALL fail the installation command; the command SHALL
NOT print a successful install report after that failure.

#### Scenario: Default install includes native hooks

- **WHEN** a caller installs any supported selected host without hook flags
- **THEN** the runtime/skill target and that host's native hook configuration are installed

#### Scenario: Explicit opt-out preserves hook configuration

- **WHEN** a caller installs with `--skip-hooks`
- **THEN** the runtime/skill target is installed and the existing hook configuration remains byte-identical

#### Scenario: Hook failure fails installation

- **WHEN** default hook configuration cannot read, merge, back up, or write the selected host config
- **THEN** installation returns failure and emits no successful install claim

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

### Requirement: Reviewers complete related-problem sweeps before returning

Every enabled review role SHALL complete all safe checks it owns before
returning when it can report a blocker, failure, concern, or decision gap.
After finding one such problem, it SHALL inspect every allowed part of the
confirmed requirement and current change for other instances of the same defect
pattern, then trace the same causal, behavioral, data, dependency, or
architecture chain until every related in-scope consequence caused by the current
change is identified. It SHALL report every independently actionable problem in
the same result. Multiple manifestations of one root cause SHALL be one finding
that names every affected location or case.

The sweep SHALL NOT include unrelated pre-existing problems, another gate's
responsibilities, or unapproved QA cases. QA Execution SHALL execute and report
all approved cases but SHALL NOT invent additional cases. Carry-Forward Arbiter
is excluded from defect discovery: it SHALL still decide every eligible prior
PASS gate independently as its own carry, rerun, or blocked decision.

The complexity role SHALL preserve its solution-first order: it must finish the
solution-level sweep before returning; if the solution fails, dependent
code-level checks are blocked rather than reviewed as if the solution were
accepted. Output concision SHALL never reduce the number of reported findings.

#### Scenario: A finding has related instances and downstream consequences

- **WHEN** a review discovers one blocker caused by the current change and the
  same defect pattern or its causal chain affects other in-scope locations
- **THEN** the review continues through those locations and reports all
  independently actionable consequences in one result before returning.

#### Scenario: One root cause has multiple manifestations

- **WHEN** the same current-change defect appears in multiple files, checks, or
  cases
- **THEN** the result contains one finding with every affected location or case,
  rather than one partial finding per manifestation.

#### Scenario: Related problem is outside the review boundary

- **WHEN** a discovered issue is historical and unrelated, belongs to another
  gate, or would require an unapproved QA case
- **THEN** it is not reported as an in-scope blocker or used to broaden the
  current review.

### Requirement: Carry Arbiter receives only the native comparison target

Carry-Forward Arbiter SHALL use a separate receipt-bound role policy and judge
whether the exact repair comparison it obtains directly from the on-site VCS
invalidates each eligible prior PASS gate. Formal-gates SHALL retain only the
workflow snapshots, static evidence, and decisions.
Unrelated local worktree changes SHALL be excluded. Earlier hops, accepted Carry
decisions, source gate evidence, and the transition chain SHALL remain under
`restricted/` and be validated by the CLI outside the Arbiter prompt, exactly
like other workflow material.

#### Scenario: Carried result needs arbitration

- **WHEN** a carried prerequisite is proposed
- **THEN** a fresh Arbiter directly compares the native pre-repair and
  post-repair snapshots while the CLI validates the full chain outside reviewer
  context.
