## Context

Formal-gates already has gate order, workflow state, receipt records, artifact
validation, QA guidance, and transition records. The local diff started closing
gaps between those references and the Go validator, but the current draft mixed
that repair with several new evidence mechanisms and OpenSpec-specific task
state.

`alignment.md` is the confirmed source for this design. Deterministic work is
owned by the existing Go CLI. Model reviewers remain responsible only for
semantic judgments that static code cannot make.

## Design Constraints

- Keep `internal/cli` and `internal/validate`; do not add a package, service,
  provider, verifier, policy runtime, or orchestration framework.
- Modify or remove existing structures before adding concepts.
- Formal-gates must work with OpenSpec, PRD, SDD, issues, or ordinary Markdown.
  No document format or checkbox is a runtime prerequisite.
- Formal development and review may use Git, SVN, P4, or another available VCS.
  A formal flow stops before development when no VCS can produce a complete
  delivery comparison. The worker, orchestrator, and reviewer invoke that VCS
  directly; the handoff VCS value and caller-provided snapshot identities are
  external-tool metadata for the workflow.
- New workflow evidence has one host-neutral owner at `.gates/runs/`. Host-
  specific `.claude`, `.codex`, and `.cursor` paths remain installation and hook
  concerns only.
- Native install uses the existing host target and hook-merge owners as one
  complete operation. It configures each selected host's supported hooks by default,
  preserves unrelated hook entries, and fails when hook configuration fails.
  `--skip-hooks` is the only opt-out; there is no hook opt-in or compatibility
  alias.
- The worker explicitly submits every delivery path. The CLI only validates
  repository-relative path syntax, rejects `.gates`, sorts and deduplicates the
  values, and generates changed-files evidence plus its composition proof. It
  does not scan the worktree, infer intent, parse the external diff, or capture
  file content. The worker adds each new delivery file to the named VCS
  immediately and adds an existing untracked delivery file before modifying or
  deleting it. Only explicit delivery paths may be added; before return, every
  delivery path is tracked and present in the complete VCS diff while unrelated
  untracked files remain untouched.
- Before repair, every affected delivery path is tracked and the on-site VCS
  fixes the pre-repair snapshot. The Carry reviewer compares that snapshot with
  the post-repair snapshot directly; if it cannot, Carry is unavailable and
  affected gates cannot enter a new-snapshot rerun without terminal
  `RERUN_REQUIRED`.
- Every review role that can report a blocker, failure, concern, or decision gap
  performs a completeness sweep before returning: it searches the allowed
  requirement and current-change inputs for same-pattern instances, follows the
  same causal/behavioral/data/dependency/architecture chain until all related
  in-scope consequences caused by the current change are identified, and reports
  independent problems together in one result. Same-root manifestations share
  one finding with all affected locations. The sweep excludes unrelated history,
  other gate responsibilities, and unapproved QA cases; Carry remains a
  per-gate inheritance decision, not a defect-discovery review.
- Every related agent, reference, fixture, and validator moves to the new
  vocabulary together. There is no legacy compatibility path.
- Reviewer findings become requirements only when they need a user decision,
  followed by clarification and persistence in the current alignment record.
- A finding blocks only when it shows a concrete current-change violation of a
  confirmed requirement, observable behavior, or existing mandatory rule.
  Advisory-only reviews PASS; the main agent filters duplicates and preferences,
  and stops each gate's automatic review-repair after three completed cycles. The existing
  receipt-registration choke point rejects a fourth finalized review for the
  same workflow, gate, and stage unless the user explicitly authorizes it. A cycle starts
  only with a complete formal result and counts only after that result, its
  accepted repairs, and required re-verification have been processed as one
  unit. Developer self-check fixes, failed dispatches, interrupted runs, and
  incomplete results do not count. Any remaining evidenced blockers then require
  a user decision before another round or delivery action.

## Ownership And Call Direction

Calls remain one-way:

1. `internal/cli` parses commands and renders deterministic results.
2. Existing workflow code coordinates validation and is the only owner that
   writes authoritative workflow or gate state.
3. The strict decoder creates typed values and selects the owning domain
   validator. It does not decide domain semantics.
4. The typed policy is read-only input shared by validation and `policy show`.
5. Requirements, closure/path, receipt/isolation, QA admission, and carry
   validators return results; they do not call CLI code or write state.
6. Existing workflow code binds the caller-provided external VCS snapshot and
   changed-files evidence. Changed-files composition owns only deterministic
   path normalization and proof generation. The worker, orchestrator, reviewer,
   and Carry Arbiter invoke the on-site VCS directly to view native changes;
   formal-gates owns workflow metadata and static evidence.

Serialized ownership is intentionally narrow. Reviewer semantic slots,
requirements/QA positioned scalar submissions, context-bundle and FinalExecution outputs, and
`policy show --format json` shapes are public machine contracts and are closed
in the owning specifications. Their static formal fields are CLI-generated;
callers supply only documented semantic values and source paths. Receipt files,
closure manifests, gate state, and final-verification records are run-local CLI
implementation artifacts: users do not author them, only the CLI reads or
writes them, and old workflows restart after the cutover. Their typed Go
structures and behavior tests define internal bytes; the requirements specify
their integrity, lifecycle, and failure guarantees without creating an external
field-by-field compatibility promise.

## Decisions

### 0. Review Must Converge Before Product Development

Phase 0 changes existing instructions and behavior cases only. Reviewers keep
their gate-specific ownership, but wording, naming, formatting, equivalent
design preferences, unrequested hardening, and unsupported hypothetical risks
cannot affect a verdict. The main agent checks whether each reported problem is
evidenced, in scope, and distinct before creating a repair task or clarification
item. This uses no new schema field, severity model, disposition record,
fingerprint, reviewer role, command, or state machine.

### 1. Delivery Is Split By Independent Value

Phase 1 replaces Markdown machine truth with strict JSON for requirements, QA
Execution, complexity, architecture, code quality, and mechanical
FinalExecution. The requirements PASS parity and typed-policy behavior defects
already present in the local diff are fixed directly in that final contract;
there is no separately deliverable current-format repair or retained schema-v1
path. The phase also adds deterministic closure/path validation, exact completed
review-output binding through the existing receipt chain, unique open output
reservation, and safe receipt/state replacement, and switches every producer
and consumer to the same typed contract in one deliverable snapshot.

Phase 2 adds restricted-path isolation and applies the existing receipt chain to
Design Review, stronger QA admission, and carry on that foundation; each added
feature includes its JSON content, policy, domain validator, and tests in one
task. It does not implement White-box Adequacy.

Phase 2.5 implements the confirmed schedule. The three pre-development
start-readiness checks (`complexity-gate`, `architecture-health-gate`, and
cold-water review) run concurrently. QA Design, Design Review, and Design
Rework retain their own case-set/review/rework dependencies but may overlap
isolated candidate development. QA Execution and the three post-development
gates also run concurrently. After each repair, a new independent zero-context agent
reviews the cumulative diff produced by that repair, from the pre-repair
snapshot to the post-repair snapshot, and decides per gate whether to rerun it
or inherit its prior result. Unrelated local worktree changes are excluded.
The main agent only dispatches and records that decision. During a repair, the
orchestrator MAY prepare source-closure selection, context inputs, and immutable
command shape in parallel with the worker, but Carry registration, dispatch,
and judgment wait for the worker's exact post-repair VCS snapshot and the
CLI-composed exact transition. No mutable future ref, waiting Arbiter, or
two-phase judgment is used. Concurrent candidate development cannot see QA drafts or review
findings, and it gains no formal acceptance before the case set is approved. A
requirement ambiguity pauses only the affected development slice for user
clarification. Phase 2.5 does not run a duplicate experiment or add an A/B/C
   selector. Phase 3 re-runs the Phase 1 stale-vocabulary scan as a regression
   audit, uses `.gates` for workflow evidence, requires external VCS snapshot
   identities and explicit delivery paths, removes proven obsolete or duplicate
   code/document owners across the repository, and completes broad
verification. Each implementation phase has its own snapshot and
start-readiness review before handoff. A later phase that is not yet start-ready
blocks only its own handoff, not an earlier phase; its exact contracts are
completed and reviewed against the snapshot that exists when that phase is
about to start.

### 2. Strict JSON Is The Only Machine Truth

Phase 1 uses one closed version-2 JSON envelope for requirements PASS, QA
Execution, the three independent post-development reviewers, and mechanical FinalExecution. The structured-evidence capability
owns the exact fields, role combinations, payloads, and rejection scenarios;
this design does not duplicate that public contract.

Semantic owners provide only positioned scalar statuses, reasons, findings,
confirmed answers, and execution observations through typed commands. They do
not author semantic JSON or edit generated templates. The existing Go CLI generates every
deterministic envelope field, policy/check catalog, evidence reference, hash
binding, and aggregate verdict, then validates and records the composed result.
It never generates or rewrites semantic judgment. Receipts, closures, state,
finalization, and the static portions of requirements, reviewer, QA, and Carry
artifacts are CLI-owned typed output rather than AI-authored JSON.

Markdown may explain a result but cannot satisfy, complete, or override machine
evidence. Phase 1 has no route object or reviewer self-certification, accepts no
legacy format, and restarts old workflows instead of adding compatibility.

### 3. Reviewer Judgment Uses Stable Checks

Complexity, architecture, code quality, QA Design Review, and Carry use a
CLI-generated judgment catalog. Registration writes all route fields, policy
IDs, check IDs, and machine-only evidence bindings before dispatch. The
reviewer submits only ordered semantic status, reason, finding, and location
values through `receipt submit`; it never edits JSON, reads bound evidence
files, or handwrites a static field. Submission validates all values before an
atomic artifact/proof write. Finalization requires that proof, computes the
aggregate verdict, and emits the final formal JSON.

Registration accepts machine evidence only through role-specific path flags:
Design Review supplies its case set and Design receipt, and Carry supplies
source closure paths. The CLI generates the fixed check bindings and derives each Carry gate
from the verified closure. Transition composition accepts ordered scalar groups
for each hop and generates every hop field and object. No generic check-ID map,
gate/path mini-language, or hop string DSL remains as a production input.

The CLI owns the complete required check set and computes the aggregate
verdict. Reviewer messages remain explanatory text rather than machine rules.
The matching external receipt binds the exact final-send prompt and each
completed reviewer output, and the gate closure contains both; reviewer JSON
does not self-report identity, prompt evidence, or receipt proof.

QA Execution is different: the QA executor owns the approved-case results and
binding, while the main agent supplies the six source paths to
`artifact compose-qa-execution` for the approved case set, accepted Design
Review, QA results, binding, changed files, and verification. The CLI generates
their evidence references and the complete `QA_EXECUTION` envelope, checks case
coverage, PASS results, hashes, workflow, snapshot, and binding, and records the
gate without reviewer checks or a reviewer receipt.

Requirements PASS and FinalExecution keep dedicated typed payloads because they
carry operational data rather than reviewer judgment. Their exact fields, the
reviewer check catalog, and the one-time Markdown migration mapping remain in
the owning capability specifications rather than this design rationale.

### 4. Requirements PASS Is Enforced Per Item

The requirements payload binds a structured alignment artifact and typed user
decision record. Validation checks every stable RQ ID, legal status, required
field, prior-round ID continuity, explicit approval for dropped/deferred items,
declared count, open blockers, coverage dimensions, scope preservation, precise
covered targets, workflow/snapshot, and PASS eligibility. One valid item cannot
hide another invalid item.

The narrative alignment vocabulary uses `Document impact:`. Requirement source
paths are precise scope identifiers, not PASS evidence and not limited to an
`openspec/` tree.

Interaction guidance stays in the existing requirements reference/agent:
inspect repository facts first, ask one consequential decision with a
recommendation, and write each user decision to the current alignment record
before asking the next question. No runtime interaction state is added.

### 5. One Typed Policy Drives Validation And Reporting

Admission, role/stage rules, required check catalogs, permitted
NOT_APPLICABLE checks, and active role exceptions are declared once in typed Go
data. Validators execute those objects and `policy show --format json` projects
the schema owned by `policy-baseline`: schema version, fixed gate ID/display order, the
eight Phase 1 policies, and the two Phase 2 policies when their complete domain
features are enabled. It does not export human prose, maps, future-role
placeholders, or a properties bag. Every exported policy or check ID has
accepting and rejecting behavior tests. Comparing field counts or keywords is
not parity evidence.

The selected policy's flow is also executable data. Existing record/admission
mode and persisted state must match it: start-readiness cannot satisfy
post-development, and any declared prerequisite must match gate, stage, and
flow. The four post-development gates have no gate-to-gate PASS prerequisites;
the finalization policy alone aggregates all four target-bound results.
Standalone artifact validation is not authority to record PASS. This reuses the
existing mode input and adds no artifact field or CLI flag.

### 6. Each Gate Or Requirements PASS Owns A Recursive Evidence Closure

Typed evidence-reference fields create a run-local directed graph. This includes
every `inputs[]` entry in the typed context bundle. Markdown and untyped text
never create an edge. Each reference contains a normalized relative path and
lowercase SHA-256. Validation rejects missing files, hash mismatch, absolute
paths, URI paths, traversal, backslashes, symlink escape, cross-run references,
conflicting aliases, and cycles.

Each requirements PASS and post-development gate PASS owns one deterministic closure
manifest containing its top-level artifact, its matching receipt when the role
requires one, and every transitive input. Reviewer output never refers back to
the receipt or closure. For reviewer gates, the receipt binds the exact output
hash and the manifest includes both files and their dependencies. QA Execution
has no receipt; its closure directly includes the six referenced inputs. Entries and
references are sorted by normalized path, and typed Go structs and fixed
encoding produce the bytes locked by golden tests. Gate state binds those PASS
records only to the closure manifest path and hash.

FinalExecution is a CLI-owned mechanical closeout, not a fifth gate PASS root.
It owns no closure. Its state entry binds the exact deterministic FinalExecution
artifact path and hash; seal re-hashes it and re-verifies its four gate closures
and final-verification reference. The entry cannot satisfy a gate prerequisite,
and no separate report root or final aggregate root is created.

### 7. Deliverable Snapshot And Evidence Identity Are Separate

`changeSnapshot` covers deliverable source, tests, configuration, requirements,
and tracked project documentation. Generated reports, dispatches, receipts, and
run logs are excluded and protected by evidence closure. A deliverable edit
creates a new snapshot and invalidates old PASS results for that target unless
an independent Carry Arbiter accepts the individual gate. Before registering a
post-development gate at that new snapshot, or composing mechanical QA
Execution there, the CLI requires the target's terminal Carry transition to
decide `RERUN_REQUIRED` for that gate when an older-snapshot PASS exists;
`ACCEPT_CARRY`, `BLOCKED`, or no decision rejects before output/proof writes.
With no prior same-gate PASS, first execution remains allowed. A bound evidence
edit invalidates the dependent PASS on the same deliverable snapshot.

Document checkboxes are ordinary, non-authoritative content. A tracked document
edit follows the same snapshot rule as any other edit; there is no general
checkbox normalization, FinalExecution binding, or admission rule.

### 8. Reviewer Identity Reuses Existing Receipts

Formal reviewer and Carry-Forward Arbiter results use the current
dispatch registration, artifact hash, and receipt chain, plus subagent
start/stop and identity only when the selected provider exposes usable
lifecycle events. The generation-only prompt template is not evidence and its own
hash is never embedded in the final prompt. Registration is the single full
pre-dispatch static check and judgment-template generator: it validates the
seven fields, route values, output contract, context-bundle bindings, evidence
references, and policy-specific input placement, then generates the immutable
reviewer/Carry static projection and proof. The reviewer does not confirm or
repeat that projection. Validation binds workflow, gate, stage, target snapshot,
final-send prompt, submission proof, and exact output. Lifecycle-capable
providers additionally bind host-captured subagent identity and start/stop.
Missing or mismatched required receipt data blocks PASS; actor labels and
provider names are not substitutes. Codex supplies no usable subagent lifecycle
events, so those fields are absent rather than emulated. Mechanical QA Execution
is excluded because it contains no second review judgment.

The reviewer and Carry payloads contain no dispatch, prompt, or self-reported
session ID. Parallel reviews are
distinguished by the system-generated dispatch ID, distinct review-artifact
path, and exact output hash; lifecycle-capable providers also bind the
host-captured subagent ID. Two open dispatches may
not reserve the same review-artifact path; ambiguity is rejected rather than
resolved by choosing or combining a result. An operator may replace the single
open reservation in place when correcting its prompt or route before lifecycle
capture starts on a lifecycle-capable provider. Finalization locks the dispatch.

Receipt submission first combines semantic values with the assigned catalog,
rejects unknown, duplicate, missing, wrongly typed, or illegal values, and
atomically records the PENDING artifact hash/proof. Receipt finalization
requires that proof, derives the verdict, and runs the complete artifact policy
validator. While the same dispatch remains open and unfinalized, a caller may
fully resubmit its role semantics only when the existing
`SemanticSubmissionSHA` exactly matches the current artifact bytes. The CLI
rebuilds from the original static catalog, reruns every validation, and
atomically replaces the artifact and dispatch proof. A missing or mismatched
SHA, manually changed artifact, finalized dispatch, incomplete input, or other
validation failure leaves both byte-for-byte unchanged. This uses the existing
submission and dispatch lifecycle; it adds no reset/reopen command or state.
Valid finalization generates the formal artifact and computes the completed
registration and receipt bytes before changing either file. It writes the receipt completely first, then
atomically replaces the still-open registration with its finalized form. The
registration is the commit point: if either write fails before that replacement,
the registration remains open and no formal PASS may use an orphan receipt.

QA Design retains output binding, plus lifecycle binding when the provider
supports it, but is not a review judgment and
does not require a reviewer prompt. Its registration creates the static case
template, and the designer submits exactly seven ordered semantic values per
generated case position through `receipt submit`. The CLI writes the complete
canonical Markdown, commits its hash to the dispatch proof, and finalization
requires that proof. The designer never writes Case IDs, labels, separators,
or newline layout. Ordinary CLI receipt validation proves record consistency. It does not claim
resistance to an operator who controls local files and CLI execution. A host may
claim automatic lifecycle or file-access capture only after a same-host live
canary demonstrates it. Hosts without such hooks can still record formal PASS
through the ordinary receipt path. Codex still installs the existing
`SubagentStart` / `SubagentStop` hooks for host compatibility, but its receipt
path does not require events the host may not emit; hook installation and merge
behavior remain unchanged.

### 9. Reviewers Receive Only Requirement, Role, And Diff

Every review-related file is written under
`.gates/runs/<workflow-id>/restricted/`, including current and old
dispatch copies, bundles, reviewer output, QA output, receipts, lifecycle
events, closures, state, reports, logs, verification records,
repair history, Carry material, and summaries. There is no outside-restricted
review-artifact exception.

The reviewer receives only the confirmed current requirement, its current role,
and the current diff or pre-development proposed change. Worktree, base
revision, output location, and output format are operational routing only.
Requirements and changed files are read directly from the current repository;
the reviewer runs its own checks. Within the workflow run it may read only the
assigned CLI-generated catalog and writes the result only through `receipt
submit`; it never edits formal JSON. It receives no other workflow-run file, copied
requirement or project-rule file, prior gate result, verification summary,
receipt, closure, state, repair history, or main-agent conclusion. The main
agent and CLI validate prerequisites and machine evidence outside reviewer
context. The assigned catalog is not substantive reviewer context because its
static projection is immutable and its bound files remain unread. An
implementation that cannot keep this boundary must stop instead of adding
another reviewer input or path exception.

Carry-Forward Arbiter uses a separate role policy because it must invoke the
on-site VCS to compare the exact pre-repair and post-repair snapshots and decide
whether the repair invalidates each eligible prior PASS gate. Unrelated local
worktree changes are excluded.
The complete transition chain remains machine-only: the CLI validates its hops,
old PASS closures, receipts, and references outside reviewer context. Only a
canaried host may claim it observed every actual file read.

Reviewer `Current diff` is produced by the orchestrator with the on-site VCS.
The complexity reviewer invokes that VCS directly to inspect the native diff,
stat, and changed contents and semantically judges whether the solution and code
volume match the requirement. formal-gates owns workflow metadata, static
evidence, and decisions.

### 10. Carry Is Decided Per Gate Before Final Composition

Final composition has one row per fixed gate, marked `FRESH_PASS` or
`CARRIED_PASS`, with target/source snapshots and evidence references. A fresh-only
composition needs no Arbiter.

After the target snapshot is fixed, the CLI validates the complete transition
chain and one fresh Carry-Forward Arbiter directly compares the exact pre-repair
and post-repair snapshots with the on-site VCS. The Arbiter decides every
eligible prior PASS gate independently as `ACCEPT_CARRY`, `RERUN_REQUIRED`, or
`BLOCKED`. A completed arbitration may contain a mixture of accepted carries
and gates that must rerun. There is no carried prefix, earliest rerun boundary,
or automatic downstream suffix rerun, and the main agent cannot override an
individual decision.

The Arbiter judges the native repair comparison against each gate's
responsibility, checks, evidence, and observable behavior. Change size alone
cannot approve or reject carry.

When another repair follows an accepted Carry transition, the next chain starts
at the latest fresh-PASS evidence and includes the accepted decisions through the
current target. The CLI validates provenance outside the prompt, but the Arbiter
receives only the newest snapshot pair and worktree needed for the native VCS
comparison, not the full delivery history. A gate evidence source may therefore
predate the current hop source; the two identities remain separate. This
preserves hop-by-hop review without adding a prefix or earliest-rerun selector.
When the on-site VCS cannot compare that exact pair, Carry is unavailable.

The accepted per-gate decisions may be reused at finalization only while the
target snapshot remains unchanged. Any deliverable edit invalidates them and
requires a new decision set before final composition.

### 11. QA Admission Records Only Meaningful Boundaries

If formal intent is declared before code, QA Design derives cases from confirmed
requirements and public contracts before development. If code already exists,
a new workflow treats it as an unaccepted candidate and keeps implementation,
diff, existing tests, and prior self-test claims from blind case design and
independent Design Review.

Design creates a case set and binds it to the designer receipt; it is not a
PASS or a gate. Independent Design Review uses the shared reviewer envelope and
the `qa.design-*` checks, binding the exact case-set hash and Design-stage
receipt. Design Rework is not a machine role: changing cases changes the hash
and requires another Design Review. After Design finalization, rework uses
another Design registration and semantic submission; it never manually edits
the case artifact. The accepted Design Review closure is the
approval; no second copy of the case set is created.

Development handoff binds the approved case set and accepted review. A worker
may adopt, modify, or delete pre-existing candidate code after that handoff and
must produce current-snapshot verification. Developers may add cases but cannot
remove or weaken approved cases.

QA Execution requires the approved chain, QA-owned results, and complete
case-to-result binding. Its six-reference payload contains the original five
execution references plus the accepted Design Review closure. That
pre-development closure remains valid
across the normal implementation snapshot change only for the same workflow
and exact case-set hash; it is not a current-snapshot gate prerequisite. The
main agent and CLI mechanically validate and record those inputs without
another QA reviewer. FinalExecution is a
mechanical closeout over existing gate evidence and final verification, not
another QA judgment.

### 12. Validation Completes Before State Replacement

All supplied artifacts, paths, hashes, receipts, role data, verdict admission,
policy checks, and prerequisites validate before authoritative state changes.
Any rejection leaves prior state byte-identical.

The gate-state writer writes complete JSON to a same-directory temporary file
and then replaces the formal state file. Phase 2.5 also serializes each
cross-process state mutation with a same-directory lock covering reload,
state-dependent validation, mutation, and replacement. Parallel reviewers and
QA execution remain independent; only their shared commit point is serialized.
This adds no state schema or persistence layer.

## Evidence Roles

Phase 1 activates only:

- `REQUIREMENTS_PASS`: alignment, user decision, continuity, coverage, precise
  targets, and PASS eligibility.
- `QA_EXECUTION` at `Execution`: a mechanical payload with approved case set,
  QA-owned results, case-result binding, changed files, and verification.
- `COMPLEXITY_REVIEW`: common reviewer payload and policy-owned semantic checks.
- `ARCHITECTURE_REVIEW` and `CODE_QUALITY_REVIEW`: common reviewer payload and
  their policy-owned checks.
- `FINAL_EXECUTION`: mechanical mode, final gate matrix, final verification,
  and release judgment over four fresh results.

Phase 2 adds `QA_REVIEW` at Design Review and `CARRY_ARBITER`. Each becomes an
accepted role/stage only in the same task that delivers its JSON content,
policy, domain validator, and tests. Phase 1 does not register disabled versions
of those roles or stages, and Phase 2 does not register White-box Adequacy.

Phase 2.5 adds no evidence role or payload. Its scheduling and repair-impact
decision must reuse the existing roles, envelopes, receipts, and state; the
per-gate rerun/inheritance decision is recorded through the existing Carry
evidence rather than a new reviewer layer. It does add composition paths and
template proofs so every existing role's static formal content is CLI-generated.
Those paths accept only role-specific source paths and ordered semantic or
per-hop scalars; callers do not transcribe static IDs, gates, keys, or mini
languages.

Phase 2.5 preserves an explicitly resolved workflow run directory through
handoff composition and its immediate validation instead of deriving another
directory from the workflow ID. In Phase 3, workers, orchestrators, and
reviewers invoke the on-site VCS directly for delivery and repair comparisons;
formal-gates records workflow snapshots and decisions.

## Verification Strategy

- Behavior tests for each exported rule/check ID, including missing, duplicate,
  unknown, and non-PASS results.
- Strict JSON tests for duplicate/unknown fields, types, UTF-8, trailing values,
  role conflicts, old schema, and deterministic bytes.
- Template/composition tests proving semantic-only success, rejection of every
  static projection change, no overwrite, and no partial formal output.
- Closure/path tests for nested tampering, cycles, aliases, cross-run and
  symlink escape, plus Windows and macOS/Linux path fixtures.
- Receipt tests for every provider-required lifecycle dimension, Codex
  lifecycle-free finalization, and honest host capability reporting.
- Restricted-input tests, including transitive references.
- QA tests for case/review hash binding, case changes, weakened cases, and
  Execution evidence.
- Carry tests for fresh-only, accepted carry, mixed per-gate decisions,
  multi-hop chains, rejected transitions, reuse, invalidation, native VCS
  comparison, and the comparison-unavailable blocked-rerun path.
- Changed-files tests for deterministic sorting/deduplication, repository-relative
  path validation, `.gates` rejection, and no worktree/VCS scanning. Handoff tests
  reject missing or `none` VCS for formal modes.
- Repository convergence scans proving the active workflow uses `.gates`,
  external VCS snapshots, current vocabulary, consistent documentation, and one
  authoritative owner per rule.
- Review completeness checks proving each enabled review role reports all
  same-pattern and same-chain consequences in one result without broadening
  scope.
- State tests proving rejected writes preserve bytes and successful replacement
  produces complete valid JSON.

## Migration And Rollback

Phase 1 is a clean break: old machine evidence and the partial schema-v1 path
are rejected and the workflow
restarts. Generated templates, examples, canaries, agents, references, tests,
and validators change together so no mixed vocabulary is accepted.

Rollback discards incomplete new workflows. It does not translate evidence
backward.

Phase 3 is another direct cutover. New workflows use `.gates` and external VCS
snapshot identities. A rollback discards incomplete Phase 3 workflows.
