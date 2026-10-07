# Requirements precedence

This is the single requirements inventory for the current engine implementation.
The current parent requirements and explicit user decisions govern stages 5–7;
a stage slice cannot reintroduce a conflicting public command or a second writer.
“Priority” is requirement precedence, not a severity rule. Project boundary and
P0–P3 interpretation remain owned by `prompts/reviewer-base.md`.

Current decision recorded 2026-10-07: structural precheck and full-file/revision
confirmation precede development-run start. Start binds the confirmed typed intake
receipt; first drive registers it automatically without repeating that confirmation.
This supersedes the start-before-precheck order in intake instructions. The product
retains its human confirmation nodes; this development uses independent gates
without a formal run, and does not authorize global installation or push.

| Status | Priority | Document | Authority in this work |
| --- | --- | --- | --- |
| reference | P1 | `openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md` | Stage-0 delivery contract; non-conflicting installation and identity constraints remain references. |
| reference | P1 | `refactor-plan/stage-0-requirements.md` | Confirmed stage-0 scope; transitional workflow behavior does not override the parent final surface. |
| reference | P1 | `refactor-plan/stage-0-solution.md` | Stage-0 installation choices; superseded runtime routing is not a current product capability. |
| current-authority | P0 | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Complete engine product and acceptance contract; eight public workflow entries, unique writer, human nodes, typed execution and recovery. |
| current-authority | P1 | `refactor-plan/incremental-seal-plan.md` | Confirmed sequencing, isolation, and exit conditions. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/alignment.md` | Alignment evidence; it cannot override the current parent requirements. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/design.md` | Design rationale and constraints; implementation choices above win on conflict. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/proposal.md` | Proposal context only; not a public semantic source. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/tasks.md` | Task evidence only; it does not create a requirement or public entrypoint. |
| current-authority | P1 | `refactor-plan/final-implementation-draft.md` | Confirmed architecture, subordinate to parent requirements and explicit later decisions. |
| current-authority | P1 | `refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md` | Typed authoring, closed registry, canonical definition generation and lockstep activation. |
| current-authority | P1 | `refactor-plan/adr-002-development-batching-and-verification-tiers.md` | Batch/Subtask design and validation tiers; no separate Batch lifecycle. |
| reference | P1 | `openspec/changes/orchestration-pipeline-engine-phase-1/master-requirements.md` | Kernel delivery requirements; parent final contract wins on public runtime conflicts. |
| reference | P1 | `openspec/changes/orchestration-pipeline-engine-phase-2/master-requirements.md` | Protocol delivery requirements; current parent controls final writer and public surface. |
| reference | P1 | `openspec/changes/stage0-1-delivery-repair/master-requirements.md` | Delivery repair record; no independent authority for retired commands. |
| reference | P1 | `refactor-plan/stage-3-requirements.md` | Confirmed regular execution slice; non-conflicting receipt and semantic constraints remain applicable. |
| reference | P1 | `refactor-plan/stage-3-solution.md` | Regular execution design reference, subordinate to current parent and ADRs. |
| orthogonal | P2 | `openspec/changes/blackbox-parallel-seal-squash-qa-mode/master-requirements.md` | Parallel/seal/squash QA scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md` | Lifecycle/deadlock scope; non-conflicting lifecycle constraints remain applicable. |
| orthogonal | P2 | `openspec/changes/fix-existing-defects/master-requirements.md` | Separate defect-remediation scope. |
| orthogonal | P2 | `openspec/changes/host-rule-management-and-codex-hook/master-requirements.md` | Host-rule and hook maintenance remains applicable; workflow writer and authority follow the current parent. |
| orthogonal | P2 | `openspec/changes/p1-qa-decouple-and-carries-fix/master-requirements.md` | QA/carry scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `openspec/changes/qa-execution-rerun-scope/master-requirements.md` | QA rerun scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `openspec/changes/runtime-review-guards/master-requirements.md` | Runtime-review scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `openspec/changes/sliced-runs-confirmation-and-qa-refactor/master-requirements.md` | Sliced-run and QA scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `openspec/changes/two-phase-pre-development-review/master-requirements.md` | Pre-development-review scope; non-conflicting behavior remains applicable. |
| orthogonal | P2 | `P2-REPAIR-AND-COMMENT-CLEANUP-REQUIREMENT.md` | Root-level P2 historical/parallel defect scope; does not override the current engine contract. |
| orthogonal | P2 | `QA-WORKTREE-ISOLATION-AND-INCREMENTAL-REQUIREMENT.md` | Root-level QA isolation scope; non-conflicting isolation requirements remain applicable. |
| superseded | P2 | `TRIGGER-MODEL-EXPLICIT-REQUEST-REQUIREMENT.md` | Retained for traceability; its intake yes/no wording is superseded by the current on-request trigger contract. |
| superseded | P2 | `TRIGGER-MODEL-DEFAULT-REMINDER-REQUIREMENT.md` | Retained for traceability; its intake yes/no wording is superseded by the current on-request trigger contract. |
| superseded | P3 | `openspec/changes/universal-modification-intake/master-requirements.md` | Retained for traceability; its universal-intake precedence and retired command surface are superseded by the current-authority rows. |
| historical | P3 | `CHANGELOG.md` and archived gate results | Historical evidence only; never a source for new public semantics. |

Supersession is precise: the parent §3.1–3.2 replaces public workflow
prepare/record/claim/route/snapshot/carry/seal/resume/reset/abort/cleanup commands,
compatibility aliases, legacy writer/routing/migration, legacy QA aggregate state,
legacy guard and workflow hook gates wherever the reference or orthogonal documents
above describe them. Their other, non-conflicting requirements remain in scope.
`write_block` stays an auxiliary repository/owner/development-scope write boundary;
engine admission and typed submission own business advancement.

The parent §5.9 explicitly retains `state.json` as the authoritative workflow
projection and excludes SQLite/event sourcing. The general database preference in
AGENTS does not authorize a database migration for this work. Registry installation
state and raw receipts/evidence remain with their existing owners.

Sequencing and final cutover conditions in the incremental plan remain acceptance
requirements. This inventory does not claim final-stage completion: the final VCS
and host canaries, post-integration candidate identity, final Seal and actual global
cutover each require their own evidence. Unexecuted host cells cannot be PASS.
