# Requirements Clarification Artifacts

Formal requirements PASS is one closed schema-version-2 JSON artifact. Markdown may explain the result but has no admission authority. Old Markdown and schema-version-1 workflows restart.

The envelope contains exactly `schemaVersion`, `artifactRole`, `workflowId`, `changeSnapshot`, `gate`, `stage`, `verdict`, and `payload`. For requirements PASS use role `REQUIREMENTS_PASS`, gate `requirements-clarification-gate`, empty stage, and verdict `PASS`.

The payload contains exactly:

- `requirementSource`, `alignment`, `totalAlignmentItems`, optional `previousAlignment`
- `openQuestionIds`, `openBlockers`, `droppedQuestionIds`, `droppedQuestionApproval`
- `userConfirmation`, `coverageScan`, `scopePreservation`, `taskProof`
- `dimensionCoverage`, `decision`, `coveredTargets`, `downstreamPermission`

Every evidence reference contains exactly a run-relative `/` path and lowercase SHA-256. Required arrays are present even when empty. Optional fields are omitted, never `null`.

The alignment JSON contains exactly schema version, workflow, snapshot, and non-empty `items`. Each item contains stable `RQ-###` ID, requirement or question, source, why it matters, status, user answer, downstream effect, document impact, and evidence needed. Legal statuses are `OPEN`, `CONFIRMED`, `DEFERRED`, `DROPPED`, and `WITHDRAWN`; PASS admits no open or unapproved item.

The decision JSON contains exactly schema version, workflow, snapshot, `USER_CONFIRMATION`, boolean confirmation, original user text, alignment reference, approved current IDs, approved removed IDs, and approval scope. Current IDs, removed IDs, payload counts, and continuity must agree exactly.

`dimensionCoverage` contains `DIM-01` through `DIM-13` exactly once. Each row contains `id`, `status`, `alignmentIds`, and `message`. Covered targets are precise repository-relative documents or bundles and are not evidence references.

Validate and record through the existing CLI:

```bash
formal-gates artifact validate --root <repo> --file <requirements.json> \
  --gate requirements-clarification-gate --workflow-id <id> --change-snapshot <snapshot>

formal-gates workflow record-stage --worktree <repo> --run-dir <run-dir> \
  --gate requirements-clarification-gate --verdict PASS --artifact <requirements.json> \
  --workflow-id <id> --change-snapshot <snapshot>
```

Recording validates every typed rule first, creates one deterministic evidence closure without a reviewer receipt, and atomically replaces authoritative state. Any rejection leaves prior state byte-identical.
