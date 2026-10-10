# Requirements precedence

This is the current requirements inventory. Explicit user decisions and the
parent requirements govern the product. Architecture choices are subordinate to
them; runtime instructions present the same contract. Priority is precedence,
not severity: project boundaries and P0–P3 definitions belong to
`prompts/reviewer-base.md`.

| Status | Priority | Document | Authority |
| --- | --- | --- | --- |
| current-authority | P0 | `openspec/changes/orchestration-pipeline-engine/master-requirements.md` | Product scope, public entries, unique workflow state, human decisions, typed execution and recovery. |
| current-authority | P1 | `refactor-plan/final-implementation-draft.md` | Current architecture and its implementation constraints. |
| current-authority | P1 | `refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md` | Typed authoring, closed registry and canonical definition generation. |
| current-authority | P1 | `refactor-plan/adr-002-development-batching-and-verification-tiers.md` | Batch/Subtask contracts and validation tiers, with no second Batch lifecycle. |
| current-authority | P1 | `references/formal-flow.md` | Intake confirmation, selected-host automatic execution, review/QA duties and completion qualification. |
| current-authority | P1 | `references/install-and-hooks.md` | Installation transactions, host guidance, hooks and actual capability qualification. |
| current-authority | P1 | `references/qa-operations.md` | Approved QA recipes, actual execution receipts and exact operation authority. |

Structural precheck and full-file/revision confirmation precede development-run
start. Start binds the confirmed typed intake receipt; first drive registers it
without repeating confirmation. Formal execution consumes issued work on the
selected platform automatically; user choices remain typed Ask decisions.

`state.json` is the authoritative workflow projection. The approved product
excludes SQLite and event sourcing. Native resources, installation registry,
receipts and raw provider evidence retain their existing separate owners.

Validation records the actual candidate, input, source and affected capability.
Unexecuted hosts and unavailable quota are reported as unverified or skipped,
never PASS. A change only invalidates evidence for its affected behavior; current
source checks and original execution proofs establish what remains applicable.
