## Why

The current local changes strengthen formal-gates, but several documented PASS
rules are still weaker in Go than in the references. Formal evidence also lacks
one consistent machine contract for reviewer results, transitive evidence,
carry-forward decisions, and pre-development QA admission.

This change closes those gaps without making OpenSpec a runtime dependency,
without making AI parse deterministic evidence, and without forcing every gate
to rerun after a small repair. `alignment.md` is the requirement source.

## What Changes

- Make formal reviews converge before product development: only concrete current-change defects can block, advisory-only reviews PASS, the main agent filters and deduplicates findings, and automatic review-repair stops after three completed cycles.
- Complete the requirements-clarification PASS checks already started in the
  local diff and prove `policy show` from the same typed Go rules that validators
  execute.
- Replace Markdown machine truth with strict typed JSON read and validated by
  the existing Go CLI. Old workflows restart; there is no compatibility path.
- Represent independent reviewer judgments as stable policy-owned `checks[]`
  results, while QA Execution uses a small mechanical payload over QA-owned evidence.
- Bind each requirements and post-development gate PASS to its own run-local recursive
  evidence closure, keep that identity separate from the deliverable
  `changeSnapshot`, and keep mechanical FinalExecution out of a fifth closure.
- Reuse the existing reviewer receipt chain for actual reviewer judgments and reject mismatched lifecycle,
  reviewer, output, workflow, gate, stage, or snapshot data. Host auto-capture
  is claimed only after a same-host live canary.
- Put anchoring process history under the existing per-run `restricted/` path.
  Formal reviewers retain broad access to current task material outside that
  path, while Carry-Forward Arbiter reviews may read the full repair chain.
- Allow final composition to mix explicit `FRESH_PASS` and independently
  accepted `CARRIED_PASS` rows. Arbitration happens before a fresh downstream
  gate relies on carried prerequisites.
- Enforce pre-implementation QA case design, independent Design Review,
  development handoff binding, and QA Execution admission without creating a
  gate or machine role for every editing step.
- Validate all inputs before state mutation and replace gate state through a
  completed same-directory temporary file.
- Keep requirement document formats interchangeable. OpenSpec, PRD, SDD,
  issues, and ordinary Markdown are adapters, not formal-gates dependencies;
  document checkboxes are non-authoritative progress hints.

## Capabilities

Phase 1 is the atomic machine-evidence cutover. It absorbs the requirements PASS
and typed-policy defects already present in the local diff into the final JSON
contract instead of producing a separate current-format checkpoint or retaining
the partial schema-v1 path.

### New Capabilities

- `policy-baseline`: executable typed policy and behavior-tested stable IDs.
- `requirements-clarification-pass-parity`: complete per-item requirements PASS
  enforcement.
- `structured-json-evidence`: strict machine evidence, reviewer `checks[]`, and
  role dispatch.
- `evidence-closure-and-path-safety`: deterministic run-local proof closure,
  safe paths, snapshot separation, and state-preserving rejection.
- `reviewer-receipt-and-isolation`: receipt consistency, restricted-path
  isolation, and neutral reviewer inputs.
- `carry-forward-finalization`: per-gate carry arbitration and final
  composition.
- `qa-design-admission`: approved pre-development cases and QA Execution
  prerequisites.

## Delivery Phases

0. **Review convergence:** tighten existing skill, reviewer, clarification, and behavior guidance so wording or design preferences cannot prolong formal review. This phase adds no product code, schema, command, role, state, or file.
1. **Machine evidence foundation and cutover:** finish the requirements PASS
   parity and typed-policy behavior gaps already exposed in the local diff, then
   deliver strict JSON decoding, `checks[]`, role dispatch, deterministic paths
   and hashes, exact existing-receipt output binding, safe receipt/state writes,
   and every producing and consuming agent, reference, example, canary, fixture,
   and operation guide while removing Markdown machine acceptance for
   requirements, QA Execution, complexity, architecture, code-quality, and
   mechanical FinalExecution in the same phase.
2. **Formal review chain:** add restricted-path enforcement and apply the
   existing receipt chain to Design Review, optional White-box Adequacy,
   and carry-forward arbitration, with each feature's JSON,
   policy, validator, and tests delivered together on the shared envelope.
3. **Operational verification:** re-run the Phase 1 stale-vocabulary scan as a
   regression audit, complete broad verification, and obtain fresh review before
   delivery.

Each phase has its own implementation snapshot and review. Later phases do not
silently enter Phase 1, and Phase 1 has no separately deliverable Markdown or
schema-v1 intermediate state.

## Impact

- Existing Go CLI and validator packages gain stricter typed validation; no new
  package or framework layer is introduced.
- Formal evidence format is breaking and old workflows restart.
- Agent and reference templates move from Markdown field detection to the same
  JSON/check vocabulary used by the validator.
- Document checkboxes remain non-authoritative progress hints outside formal
  admission and finalization.
