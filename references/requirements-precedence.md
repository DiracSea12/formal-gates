# Requirements precedence

This is the complete stage-0 document inventory. Every listed requirement
source has one explicit status and priority; there is no catch-all or unnamed
OpenSpec source. “Priority” is the priority within this implementation scope,
not a new severity rule. The project boundary and P0–P3 interpretation remain
owned by `prompts/reviewer-base.md`.

| Status | Priority | Document | Authority in this work |
| --- | --- | --- | --- |
| current-authority | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md` | Complete stage-0 requirements and acceptance contract. |
| current-authority | P0 | `refactor-plan/stage-0-requirements.md` | Confirmed stage-0 scope, gates, and normal-use boundary. |
| current-authority | P0 | `refactor-plan/stage-0-solution.md` | Confirmed implementation choices for this stage. |
| current-authority | P1 | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Parent engine requirements, used where they do not conflict with the stage-0 slice. |
| current-authority | P1 | `refactor-plan/incremental-seal-plan.md` | Confirmed sequencing, isolation, and exit conditions. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/alignment.md` | Alignment evidence; it cannot override the two confirmed requirement documents. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/design.md` | Design rationale and constraints; implementation choices above win on conflict. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/proposal.md` | Proposal context only; not a public semantic source. |
| reference | P0 | `openspec/changes/orchestration-pipeline-engine-phase-0/tasks.md` | Task evidence only; it does not create a requirement or public entrypoint. |
| reference | P1 | `refactor-plan/final-implementation-draft.md` | Later architecture reference; later-stage behavior does not flow backward into stage 0. |
| orthogonal | P2 | `openspec/changes/blackbox-parallel-seal-squash-qa-mode/master-requirements.md` | Parallel/seal/squash QA scope; not part of this repair. |
| orthogonal | P2 | `openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md` | Lifecycle/deadlock scope; only shared constraints are references. |
| orthogonal | P2 | `openspec/changes/fix-existing-defects/master-requirements.md` | Separate defect-remediation scope. |
| orthogonal | P2 | `openspec/changes/host-rule-management-and-codex-hook/master-requirements.md` | Host-rule and hook scope outside the stage-0 authority unless explicitly cited above. |
| orthogonal | P2 | `openspec/changes/p1-qa-decouple-and-carries-fix/master-requirements.md` | QA/carry scope outside this repair. |
| orthogonal | P2 | `openspec/changes/qa-execution-rerun-scope/master-requirements.md` | QA rerun scope outside this repair. |
| orthogonal | P2 | `openspec/changes/runtime-review-guards/master-requirements.md` | Runtime-review scope outside this repair. |
| orthogonal | P2 | `openspec/changes/sliced-runs-confirmation-and-qa-refactor/master-requirements.md` | Sliced-run and QA scope outside this repair. |
| orthogonal | P2 | `openspec/changes/two-phase-pre-development-review/master-requirements.md` | Pre-development-review scope outside this repair. |
| orthogonal | P2 | `P2-FIX-REQUIREMENT.md` | Root-level P2 historical/parallel defect scope; does not redefine stage-0 priority. |
| orthogonal | P2 | `QA-INCREMENTAL-ISOLATION-REQUIREMENT.md` | Root-level QA isolation scope; stage-0 uses it only as a non-conflicting reference. |
| orthogonal | P2 | `TRIGGER-MODEL-REQUIREMENT.md` | Root-level trigger-model scope outside the install/workflow repair. |
| orthogonal | P2 | `TRIGGER-MODEL-V2-REQUIREMENT.md` | Root-level trigger-model successor scope outside the install/workflow repair. |
| superseded | P3 | `openspec/changes/universal-modification-intake/master-requirements.md` | Retained for traceability; its universal-intake precedence and retired command surface are superseded by the current-authority rows. |
| historical | P3 | `CHANGELOG.md` and archived gate results | Historical evidence only; never a source for new public semantics. |

When a later stage changes a current contract, it must add a dated inventory
entry identifying the superseded source. The stage-0 implementation must not
add later-stage workflow behavior to stable `SKILL.md`, README files, or action
prompts.
