# Post-Development Artifacts

Phase 1 machine truth is a closed schema-version-2 JSON envelope. Reviewers write their judgments directly; the main agent writes only the mechanical QA Execution envelope over completed QA-owned evidence. Markdown may explain findings but cannot complete or override JSON. Schema version 1 and Markdown machine artifacts are rejected.

## Reviewer JSON

Enabled reviewer combinations are complexity, architecture health, and code quality. The envelope contains exactly `schemaVersion`, `artifactRole`, `workflowId`, `changeSnapshot`, `gate`, `stage`, `verdict`, and `payload`. Reviewer verdicts are `PASS`, `REVIEW`, `FAIL`, or `BLOCKED`; only PASS can enter formal state.

The shared reviewer payload contains only `dispatch`, `contextBundle`, `reviewPolicyId`, `checks`, and, for post-development reviews, `changedFiles` and `verification`. Each check contains exactly `id`, `status`, `message`, `evidenceRefs`, and `findings`. Each finding contains a message and locations; each location contains repository-relative path, start line, and end line.

`policy show --format json` is the executable source for policy IDs, prerequisites, check catalogs, permitted `NOT_APPLICABLE`, and required fields. Every required check appears exactly once. The top verdict must equal machine aggregation of the checks.

The context bundle is strict JSON containing exactly bundle version 1, workflow, snapshot, and a non-empty `inputs` array of evidence references. The CLI verifies the bundle and every listed file. No second input manifest is supported.

Post-development complexity attaches a fresh statistics-only result with no budget, `budget_source=none`, and false overrides to `complexity.statistics`.

## Mechanical QA Execution

After an independent QA executor writes complete QA-owned results and case-result binding, the main agent writes `QA_EXECUTION` at `qa-test-gate / Execution`. Its payload contains exactly five evidence references: `approvedCaseSet`, `qaOwnedResults`, `caseResultBinding`, `changedFiles`, and `verification`.

QA-owned results are closed JSON containing exactly `owner`, `workflowId`, `changeSnapshot`, `stage`, `status`, `overallOutcome`, `executions`, and `caseResults`. Each execution contains exactly `id`, `outcome`, `procedure`, and `result`; each case result contains exactly `caseId`, `status`, `procedures`, and `oracle`. Case-result binding is closed JSON containing exactly `workflowId`, `changeSnapshot`, `approvedCaseSet`, `qaOwnedResults`, `complete`, and `bindings`; each binding contains exactly `caseId`, `resultPointer`, `status`, `executionRefs`, `procedures`, and `oracle`. `oracleBound` is not part of the contract.

The CLI verifies every path and hash, matching workflow and snapshot, exact approved-case coverage, PASS executions and case results, and exact result/oracle/procedure binding. It then creates the normal gate closure and records PASS. QA Execution has no reviewer dispatch, context bundle, checks, findings, or reviewer receipt.

## Receipt And Closure

Reviewer JSON contains no self-reported identity or receipt field. Before reviewer dispatch, register the meaningful target snapshot and reserve a distinct run-local reviewer output path that does not yet exist. After lifecycle start the reviewer writes that JSON, lifecycle stop is captured, and receipt finalization validates the completed output's workflow, gate, stage, snapshot, and exact bytes. Existing output and duplicate open reservations are rejected. Receipt finalization writes the completed receipt first and atomically finalizes the still-open registration last.

Recording reviewer PASS requires that external receipt and builds one deterministic recursive closure containing the reviewer artifact, receipt, context inputs, and all other typed evidence. Recording QA Execution builds its closure directly from the mechanical artifact and its five evidence references. Both bind state only to the closure path and hash. Rejection never changes authoritative state.

## Recording

```bash
formal-gates artifact validate --root <repo> --file <review.json> \
  --gate <gate-id> --stage <stage-if-any> \
  --workflow-id <id> --change-snapshot <snapshot>

formal-gates workflow record-stage --worktree <repo> --run-dir <run-dir> \
  --gate <gate-id> --stage <stage-if-any> --mode <formal-or-start-readiness> \
  --verdict PASS --artifact <review.json> \
  --workflow-id <id> --change-snapshot <snapshot>
```

## Mechanical FinalExecution

After four current-snapshot post-development closures and final verification exist, the existing final-verification command generates deterministic FinalExecution JSON at `--final-qa-artifact` when `--record-final-qa` is set. Its payload contains only `mode=MECHANICAL_CLOSEOUT`, four `gateMatrix` rows with `gate` and immutable `gateEvidence`, `finalVerification`, and `releaseJudgment=SEAL`.

The CLI re-verifies all four closures and final verification, records the exact generated FinalExecution path and hash, creates no fifth closure, and never treats finalization as a gate prerequisite. Phase 1 accepts no carried rows or cross-snapshot reuse.
