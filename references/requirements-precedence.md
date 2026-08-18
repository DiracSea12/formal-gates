# Requirements precedence

This inventory is the stage-0 evidence index. It records which documents are
authoritative for the orchestration-pipeline-engine work and keeps older plans
traceable without treating their retired command names as a current contract.

| Status | Document family | Use |
| --- | --- | --- |
| current-authority | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Overall engine requirements and non-negotiable boundaries. |
| current-authority | `refactor-plan/incremental-seal-plan.md` | Stage ordering, environment isolation, and stage exit conditions. |
| current-authority | `refactor-plan/stage-0-requirements.md` and `refactor-plan/stage-0-solution.md` | This run's stage-0 scope and implementation choices. |
| current-authority | `openspec/changes/orchestration-pipeline-engine-phase-0/` | Stage-0 requirements, alignment, design, and task evidence. |
| reference | `refactor-plan/final-implementation-draft.md` | Target architecture; later-stage behavior does not flow backward into stage 0. |
| orthogonal | Other `openspec/changes/**/master-requirements.md` files | Continue to apply only to their own named feature or defect scope. |
| historical | `CHANGELOG.md`, archived gate results, and superseded plan prose | Traceability and prior behavior only; not a source for new public semantics. |

The stage-0 implementation must not add future `drive`/`submit` behavior to the
stable `SKILL.md`, README files, or action prompts. When a later stage changes a
current contract, it must add a new dated entry and identify the superseded
document explicitly rather than deleting history.
