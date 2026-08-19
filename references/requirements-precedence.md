# Requirements precedence

This inventory is the stage-0 evidence index. It records which documents are
authoritative for the orchestration-pipeline-engine work and keeps older plans
traceable without treating their retired command names as a current contract.

| Status | Document | Use |
| --- | --- | --- |
| current-authority | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Overall engine requirements and non-negotiable boundaries. |
| current-authority | `openspec/changes/orchestration-pipeline-engine-phase-0/master-requirements.md` | Stage-0 requirements for this implementation run. |
| current-authority | `refactor-plan/incremental-seal-plan.md` | Stage ordering, environment isolation, and stage exit conditions. |
| current-authority | `refactor-plan/stage-0-requirements.md` | Confirmed stage-0 scope and acceptance requirements. |
| current-authority | `refactor-plan/stage-0-solution.md` | Confirmed stage-0 implementation choices. |
| reference | `openspec/changes/orchestration-pipeline-engine-phase-0/alignment.md` | Stage-0 alignment record; does not override the confirmed requirements. |
| reference | `openspec/changes/orchestration-pipeline-engine-phase-0/design.md` | Stage-0 design detail; does not override the confirmed requirements. |
| reference | `openspec/changes/orchestration-pipeline-engine-phase-0/proposal.md` | Stage-0 proposal context; does not override the confirmed requirements. |
| reference | `openspec/changes/orchestration-pipeline-engine-phase-0/tasks.md` | Stage-0 implementation-task evidence; does not create public semantics. |
| reference | `refactor-plan/final-implementation-draft.md` | Target architecture; later-stage behavior does not flow backward into stage 0. |
| orthogonal | `openspec/changes/blackbox-parallel-seal-squash-qa-mode/master-requirements.md` | Applies only to its named QA/seal feature scope. |
| orthogonal | `openspec/changes/deadlock-recovery-and-codex-lifecycle/master-requirements.md` | Applies only to its named lifecycle/recovery feature scope. |
| orthogonal | `openspec/changes/fix-existing-defects/master-requirements.md` | Applies only to its named defect-remediation scope. |
| orthogonal | `openspec/changes/host-rule-management-and-codex-hook/master-requirements.md` | Applies only to its named host-rule and hook feature scope. |
| orthogonal | `openspec/changes/p1-qa-decouple-and-carries-fix/master-requirements.md` | Applies only to its named QA/carry feature scope. |
| orthogonal | `openspec/changes/qa-execution-rerun-scope/master-requirements.md` | Applies only to its named QA-rerun feature scope. |
| orthogonal | `openspec/changes/runtime-review-guards/master-requirements.md` | Applies only to its named runtime-review feature scope. |
| orthogonal | `openspec/changes/sliced-runs-confirmation-and-qa-refactor/master-requirements.md` | Applies only to its named sliced-run and QA feature scope. |
| orthogonal | `openspec/changes/two-phase-pre-development-review/master-requirements.md` | Applies only to its named pre-development-review feature scope. |
| superseded | `openspec/changes/universal-modification-intake/master-requirements.md` | Retained for traceability only; its universal-intake precedence and retired command surface are superseded by the current stage-0 authorities above. |
| historical | `CHANGELOG.md` and archived gate results | Traceability and prior behavior only; not a source for new public semantics. |

The stage-0 implementation must not add future `drive`/`submit` behavior to the
stable `SKILL.md`, README files, or action prompts. When a later stage changes a
current contract, it must add a new dated entry and identify the superseded
document explicitly rather than deleting history.
