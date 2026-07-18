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

Serialized ownership is intentionally narrow. Reviewer, requirements, QA
Execution, context-bundle, FinalExecution, and `policy show --format json` shapes are public
machine contracts and are closed in the owning specifications. Receipt files,
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
and consumer before removing the old parser in the same deliverable snapshot.

Phase 2 adds restricted-path isolation and applies the existing receipt chain to
Design Review, stronger QA admission, and carry on that foundation; each added
feature includes its JSON content, policy, domain validator, and tests in one
task. It does not implement White-box Adequacy.

Phase 2.5 uses the natural Phase 2 development and formal run as the project
sample for two separate scheduling decisions. The pre-development comparison
is serial QA case approval versus QA Design/Design Review/Design Rework running
concurrently with candidate development from the same frozen requirements. The
post-development comparison is serial gates, QA followed by three parallel
reviews, or one four-gate wave. Concurrent candidate development cannot see QA
drafts or review findings, and it gains no formal acceptance before the case
set is approved. A requirement ambiguity pauses only the affected development
slice for user clarification. Phase 2.5 does not duplicate work for an
experiment or preselect the post-development option. The main agent summarizes
existing run-local facts, the user chooses the future schedule, and only then
are exact policy and implementation requirements written. Phase 3 re-runs the
Phase 1 stale-vocabulary scan as a regression audit and completes broad
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

Semantic owners write their own JSON judgments or confirmed requirement data.
The existing Go CLI is the authoritative decoder, validator, aggregate checker,
and state-recording entrypoint, but it does not generate or rewrite semantic
judgments. CLI-owned receipts, closures, state, and finalization files remain
typed internal artifacts rather than expanded public schemas.

Markdown may explain a result but cannot satisfy, complete, or override machine
evidence. Phase 1 has no route object or reviewer self-certification, accepts no
legacy format, and restarts old workflows instead of adding compatibility.

### 3. Reviewer Judgment Uses Stable Checks

Complexity, architecture, and code quality use one reviewer payload without a
dispatch or prompt evidence field and stable checks from the typed policy catalog. The main agent creates the single typed
context bundle before dispatch as a machine-only binding. Its fixed reference
may appear only in the output format; the reviewer does not read the bundle or
its files. The CLI verifies those files and hashes outside reviewer context.
Gate-specific evidence attaches to the relevant check instead of creating
another top-level schema.

The CLI validates the complete required check set and recomputes the aggregate
verdict. Reviewer messages remain explanatory text rather than machine rules.
The matching external receipt binds the exact final-send prompt and each
completed reviewer output, and the gate closure contains both; reviewer JSON
does not self-report identity, prompt evidence, or receipt proof.

QA Execution is different: the QA executor owns the approved-case results and
binding, while the main agent submits the six typed references for the approved
case set, accepted Design Review, QA results, binding, changed files, and
verification. The CLI
checks case coverage, PASS results, hashes, workflow, snapshot, and binding and
records the gate without reviewer checks or a reviewer receipt.

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
the schema owned by `policy-baseline`: schema version, fixed gate order, the
eight Phase 1 policies, and the two Phase 2 policies when their complete domain
features are enabled. It does not export human prose, maps, future-role
placeholders, or a properties bag. Every exported policy or check ID has
accepting and rejecting behavior tests. Comparing field counts or keywords is
not parity evidence.

The selected policy's flow is also executable data. Existing record/admission
mode and persisted state must match it: start-readiness cannot satisfy
post-development, and prerequisites match gate, stage, and flow. Standalone
artifact validation is not authority to record PASS. This reuses the existing
mode input and adds no artifact field or CLI flag.

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
run logs are excluded and protected by evidence closure. In Phase 1, a
deliverable edit creates a new snapshot, unconditionally invalidates old PASS
results, and requires fresh gate execution. Phase 2 may evaluate carry and the
earliest rerun boundary only after Carry arbitration is implemented. A bound
evidence edit invalidates the dependent PASS on the same deliverable snapshot.

Document checkboxes are ordinary, non-authoritative content. A tracked document
edit follows the same snapshot rule as any other edit; there is no general
checkbox normalization, FinalExecution binding, or admission rule.

### 8. Reviewer Identity Reuses Existing Receipts

Formal reviewer and Carry-Forward Arbiter results use the current
dispatch registration, subagent start/stop, subagent ID, artifact hash, and
receipt chain. The generation-only prompt template is not evidence and its own
hash is never embedded in the final prompt. Registration is the single full
pre-dispatch static check: it validates the seven fields, route values, output
contract, context-bundle bindings and policy-specific input placement, then
writes and verifies the reviewer-visible static PASS binding into the final
bytes. Validation binds workflow, gate, stage, target snapshot,
final-send prompt, host-captured subagent identity, lifecycle, and exact output. Missing
or mismatched receipt data blocks PASS; actor labels and provider names are not
substitutes. Mechanical QA Execution is excluded because it contains no second
review judgment.

The reviewer and Carry payloads contain no dispatch, prompt, or self-reported
session ID. Parallel reviews are
distinguished by the system-generated dispatch ID, host-captured subagent ID,
distinct review-artifact path, and exact output hash. Two open dispatches may
not reserve the same review-artifact path; ambiguity is rejected rather than
resolved by choosing or combining a result. An operator may replace the single
open reservation in place when correcting its prompt or route before lifecycle
capture starts. Started and finalized dispatches remain immutable.

Receipt finalization first applies dispatch-owned fields in memory and runs the
complete artifact policy validator. Invalid reviewer output leaves its original
bytes and open registration unchanged. Valid finalization computes the completed
registration and receipt bytes before changing either file. It writes the receipt completely first, then
atomically replaces the still-open registration with its finalized form. The
registration is the commit point: if either write fails before that replacement,
the registration remains open and no formal PASS may use an orphan receipt.

QA Design retains lifecycle/output binding but is not a review judgment and
does not require a reviewer prompt. Ordinary CLI receipt validation proves record consistency. It does not claim
resistance to an operator who controls local files and CLI execution. A host may
claim automatic lifecycle or file-access capture only after a same-host live
canary demonstrates it. Hosts without such hooks can still record formal PASS
through the ordinary receipt path.

### 9. Reviewers Receive Only Requirement, Role, And Diff

Every review-related file is written under
`.claude/gates/runs/<workflow-id>/restricted/`, including current and old
dispatch copies, bundles, reviewer output, QA output, receipts, lifecycle
events, closures, state, reports, logs, statistics, verification records,
repair history, Carry material, and summaries. There is no outside-restricted
review-artifact exception.

The reviewer receives only the confirmed current requirement, its current role,
and the current diff or pre-development proposed change. Worktree, base
revision, output location, and output format are operational routing only.
Requirements and changed files are read directly from the current repository;
the reviewer runs its own checks. It receives no workflow-run file, copied
requirement or project-rule file, prior gate result, verification summary,
receipt, closure, state, repair history, or main-agent conclusion. The main
agent and CLI validate prerequisites and machine evidence outside reviewer
context. An implementation that cannot keep this boundary must stop instead of
adding another reviewer input or path exception.

Carry-Forward Arbiter uses a separate role policy because it must decide whether
the cumulative source-to-target diff invalidates each proposed carried gate.
The complete transition chain remains machine-only: the CLI validates its hops,
old PASS closures, receipts, and references outside reviewer context. Only a
canaried host may claim it observed every actual file read.

Post-development complexity accepts only a fresh statistics-only report for the
current diff. Development handoff, numeric budgets, budget checks, expansion
requests, and anti-complexity decisions remain restricted and cannot be reached
through transitive evidence references.

### 10. Carry Is Decided Before Downstream Reliance

Final composition has one row per fixed gate, marked `FRESH_PASS` or
`CARRIED_PASS`, with target/source snapshots and evidence references. A fresh-only
composition needs no Arbiter.

When carry is proposed, the main agent may suggest a rerun boundary but cannot
approve it. After the target snapshot is fixed and before the first fresh
downstream gate relies on a carried prerequisite, the CLI validates the complete
uncompressed transition chain and one fresh Carry-Forward Arbiter reviews the
cumulative source-to-target diff. It decides every carried gate as
`ACCEPT_CARRY`, `RERUN_REQUIRED`, or `BLOCKED`. Rejection names the earliest gate
to rerun; downstream gates rerun from that point and the main agent cannot
override the decision.

The Arbiter judges the full repair set against each gate's responsibility,
checks, evidence, and observable behavior. Diff line count alone cannot approve
or reject carry.

The accepted decision may be reused at finalization only while the target
snapshot remains unchanged. Any deliverable edit invalidates it and requires a
new decision before downstream reliance.

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
and requires another Design Review. The accepted Design Review closure is the
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

The existing gate-state writer writes complete JSON to a same-directory
temporary file and then replaces the formal state file. This is a local
write-safety change, not a new persistence abstraction or a broader concurrency
guarantee.

## Evidence Roles

Phase 1 activates only:

- `REQUIREMENTS_PASS`: alignment, user decision, continuity, coverage, precise
  targets, and PASS eligibility.
- `QA_EXECUTION` at `Execution`: a mechanical payload with approved case set,
  QA-owned results, case-result binding, changed files, and verification.
- `COMPLEXITY_REVIEW`: common reviewer payload plus fresh statistics-only
  evidence on its statistics check.
- `ARCHITECTURE_REVIEW` and `CODE_QUALITY_REVIEW`: common reviewer payload and
  their policy-owned checks.
- `FINAL_EXECUTION`: mechanical mode, final gate matrix, final verification,
  and release judgment over four fresh results.

Phase 2 adds `QA_REVIEW` at Design Review and `CARRY_ARBITER`. Each becomes an
accepted role/stage only in the same task that delivers its JSON content,
policy, domain validator, and tests. Phase 1 does not register disabled versions
of those roles or stages, and Phase 2 does not register White-box Adequacy.

Phase 2.5 adds no evidence role or payload. Before the user chooses a scheduling
schedule from the Phase 2 sample, it also changes no prerequisite. Any later
selected schedule must reuse the existing roles, envelopes, receipts, and
state.

## Verification Strategy

- Behavior tests for each exported rule/check ID, including missing, duplicate,
  unknown, and non-PASS results.
- Strict JSON tests for duplicate/unknown fields, types, UTF-8, trailing values,
  role conflicts, old schema, and deterministic bytes.
- Closure/path tests for nested tampering, cycles, aliases, cross-run and
  symlink escape, plus Windows and macOS/Linux path fixtures.
- Receipt tests for every bound lifecycle dimension and honest host capability
  reporting.
- Restricted-input tests, including transitive references and development-time
  budget material.
- QA tests for case/review hash binding, case changes, weakened cases, and
  Execution evidence.
- Carry tests for fresh-only, accepted carry, multi-hop chains, early admission,
  rejection, earliest rerun, reuse, and invalidation.
- State tests proving rejected writes preserve bytes and successful replacement
  produces complete valid JSON.

## Migration And Rollback

Phase 1 is a clean break: old machine evidence and the partial schema-v1 path
are rejected and the workflow
restarts. Generated templates, examples, canaries, agents, references, tests,
and validators change together so no mixed vocabulary is accepted.

Rollback discards incomplete new workflows. It does not translate evidence
backward.
