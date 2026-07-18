# Post-Development Artifacts

Machine truth is a closed schema-version-2 JSON envelope. Reviewers write their judgments directly; the main agent writes only the mechanical QA Execution envelope over completed QA-owned evidence. Markdown may explain findings but cannot complete or override JSON. Schema version 1 and Markdown machine artifacts are rejected.

## Reviewer JSON

Enabled reviewer combinations are QA Design Review, complexity, architecture health, and code quality. The envelope contains exactly `schemaVersion`, `artifactRole`, `workflowId`, `changeSnapshot`, `gate`, `stage`, `verdict`, and `payload`. Reviewer verdicts are `PASS`, `REVIEW`, `FAIL`, or `BLOCKED`; only PASS can enter formal state.

The shared reviewer payload contains only `contextBundle`, `reviewPolicyId`, `checks`, and, for post-development reviews, `changedFiles` and `verification`. It contains no dispatch or prompt evidence field. Each check contains exactly `id`, `status`, `message`, `evidenceRefs`, and `findings`. Each finding contains a message and locations; each location contains repository-relative path, start line, and end line.

`policy show --format json` is the executable source for policy IDs, prerequisites, check catalogs, permitted `NOT_APPLICABLE`, and required fields. Every required check appears exactly once. The top verdict must equal machine aggregation of the checks.

The context bundle is strict JSON containing exactly bundle version 1, workflow, snapshot, and a non-empty `inputs` array of evidence references. It is a machine-only binding: the CLI verifies the bundle and every listed file, while the reviewer does not read it. No second input manifest is supported.

Post-development complexity attaches a fresh statistics-only result with no budget, `budget_source=none`, and false overrides to `complexity.statistics`.

`QA_REVIEW` uses policy `qa.design-review.v2` at `qa-test-gate / Design Review`, flow `pre-development`. Its eight checks are `review.prompt-fields`, `review.prompt-semantics`, `qa.design.requirement-coverage`, `qa.design.executability`, `qa.design.oracles`, `qa.design.evidence-binding`, `qa.design.independence`, and `qa.design.case-set-binding`. The last check references exactly the case document and its Design-stage lifecycle receipt. Recording requires a separate reviewer receipt and same-workflow, same-snapshot requirements PASS. The resulting closure is the approval; no copied approved-case artifact is created.

## Mechanical QA Execution

After an independent QA executor writes complete QA-owned results and case-result binding, the main agent writes `QA_EXECUTION` at `qa-test-gate / Execution`. Its payload contains exactly six evidence references: `approvedCaseSet`, `designReview`, `qaOwnedResults`, `caseResultBinding`, `changedFiles`, and `verification`.

QA-owned results are closed JSON containing exactly `owner`, `workflowId`, `changeSnapshot`, `stage`, `status`, `overallOutcome`, `executions`, and `caseResults`. Each execution contains exactly `id`, `outcome`, `procedure`, and `result`; each case result contains exactly `caseId`, `status`, `procedures`, and `oracle`. Case-result binding is closed JSON containing exactly `workflowId`, `changeSnapshot`, `approvedCaseSet`, `qaOwnedResults`, `complete`, and `bindings`; each binding contains exactly `caseId`, `resultPointer`, `status`, `executionRefs`, `procedures`, and `oracle`. `oracleBound` is not part of the contract.

The CLI verifies every path and hash, the same workflow, exact Design Review case-set binding, current QA snapshot, complete approved-case coverage, PASS executions and case results, and exact result/oracle/procedure binding. The Design Review closure may use its earlier pre-development snapshot only when its case reference remains exact. It then creates the normal gate closure and records PASS. QA Execution has no reviewer dispatch, context bundle, checks, findings, or reviewer receipt.

## Receipt And Closure

Reviewer JSON contains no self-reported identity, dispatch, prompt, or receipt field. First use `formal-gates prompt prepare` to build the unsealed seven-field reviewer message from a generation-only template and current machine-only bindings. After its one full static check passes, `receipt register` writes the machine-generated `static-validation=PASS` binding and binds the resulting exact-send bytes.

Before reviewer dispatch, register the meaningful target snapshot, generated prompt, context bundle, and absent run-local output path. `receipt register` is the single full pre-dispatch static check: it validates every prompt and routing field, the output role/policy contract, exact bundle path/hash and policy-specific input placement before reserving capacity. The reviewer must confirm the static PASS binding in its prompt-field check but must not read bound files. Registration rejects a fourth finalized review unless the user explicitly approved it. After lifecycle stop, finalization writes dispatch-owned route fields in memory and validates the complete reviewer JSON before writing the receipt or finalizing the registration; validation failure leaves the registration open and the original reviewer bytes unchanged. QA Design omits the reviewer prompt binding.

Recording reviewer PASS requires that external receipt and builds one deterministic recursive closure containing the reviewer artifact, receipt, context inputs, and all other typed evidence. Recording QA Execution builds its closure directly from the mechanical artifact, its six references, and the recursively revalidated Design Review chain. Both bind state only to the closure path and hash. Rejection never changes authoritative state.

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

## Carry Transition

Carry arbitration uses direct schema-version-2 `CARRY_ARBITER` JSON with policy
`carry.arbiter.v2`, gate `qa-test-gate`, and stage `Carry`. Keep the Arbiter
artifact, machine-only context bundle, and complete typed transition
chain under the active run's `restricted/` directory. The reviewer reads only
the current requirement and the cumulative diff produced by the repair from the
pre-repair snapshot to the post-repair snapshot; unrelated local worktree
changes are excluded. The CLI validates
the bundle, chain, source closures, and receipts outside reviewer context. The
payload contains only `contextBundle`, `reviewPolicyId`,
`transitionChain`, and per-gate `decisions`.

After a finalized matching receipt exists, record an accepted transition with:

```bash
formal-gates workflow record-transition --worktree <repo> --run-dir <run-dir> \
  --artifact <restricted/carry-arbiter.json> \
  --workflow-id <id> --change-snapshot <target-snapshot>
```

The command derives rejection rerun boundaries from typed decisions. It accepts
no caller-authored from/to/rerun/reason fields, records no fifth gate PASS, and
records the accepted Arbiter closure for that exact workflow and target. A
fresh downstream post-development gate may consume an accepted carried
prerequisite only while that target remains unchanged.

## Mechanical FinalExecution

After four fresh or accepted-carried post-development results and final verification exist, the existing final-verification command generates deterministic FinalExecution JSON at `--final-qa-artifact` when `--record-final-qa` is set. Its payload contains only `mode=MECHANICAL_CLOSEOUT`, four fixed-order `gateMatrix` rows, `finalVerification`, and `releaseJudgment=SEAL`.

Each matrix row contains exactly `gate`, `resultKind`, `sourceSnapshot`, `targetSnapshot`, `gateEvidence`, and optional `carryDecision`. `FRESH_PASS` uses equal source and target snapshots, the current-target gate closure, and no Carry decision. `CARRIED_PASS` uses different snapshots, the source gate closure, and the accepted Arbiter closure whose decision exactly names that gate, source evidence, and target.

The CLI re-verifies all gate and Carry closures, the Arbiter receipt, and final verification; records the exact generated FinalExecution path and hash; creates no fifth closure or second Carry review; and never treats finalization as a gate prerequisite. A later target snapshot invalidates carried admission and closeout.
