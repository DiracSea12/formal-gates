## Why

The current local changes strengthen formal-gates, but several documented PASS
rules are still weaker in Go than in the references. Formal evidence also lacks
one consistent machine contract for reviewer results, transitive evidence,
carry-forward decisions, and pre-development QA admission.

This change closes those gaps without making OpenSpec a runtime dependency,
without making AI parse deterministic evidence, and without forcing every gate
to rerun after a small repair. `alignment.md` is the requirement source.

## What Changes

- Make formal reviews converge before product development: only concrete current-change defects can block, advisory-only reviews PASS, the main agent filters and deduplicates findings, and each gate's automatic review-repair stops after three completed cycles.
- Complete the requirements-clarification PASS checks already started in the
  local diff and prove `policy show` from the same typed Go rules that validators
  execute.
- Replace Markdown machine truth with strict typed JSON read and validated by
  the existing Go CLI. Old workflows restart; there is no compatibility path.
- Represent independent reviewer judgments as stable policy-owned `checks[]`
  results, while QA Execution uses a small mechanical payload over QA-owned evidence.
- Generate every deterministic artifact field, policy/check catalog, path/hash
  binding, and aggregate verdict in the CLI; reviewers submit only semantic
  statuses, reasons, findings, and locations through the CLI, which constructs
  and proves the formal judgment JSON.
- Run the confirmed pre-development checks and post-development gates
  independently in parallel without duplicate experimental runs. Record results
  as they arrive through a cross-process single-writer state mutation that
  reloads the latest state before each atomic replacement.
- Bind each requirements and post-development gate PASS to its own run-local recursive
  evidence closure, keep that identity separate from the deliverable
  `changeSnapshot`, and keep mechanical FinalExecution out of a fifth closure.
- Reuse the existing reviewer receipt chain for actual reviewer judgments and
  reject mismatched reviewer output, workflow, gate, stage, snapshot, prompt,
  submission, and artifact data. Require lifecycle evidence only from providers
  that expose usable lifecycle events; Codex does not, so its normal receipt
  path keeps every non-lifecycle check without requiring the host to emit
  `SubagentStart` / `SubagentStop`; the existing complete hook installation is
  unchanged.
- Put every review-related workflow file under the existing per-run
  `restricted/` path. Formal reviewers, including Carry-Forward Arbiter, receive
  only the current requirement and current diff or proposed change as
  substantive context. Reviewer and QA Design semantic owners submit ordered
  values through the CLI; they never edit assigned formal artifacts. The CLI validates process history,
  evidence bindings, and transition chains outside reviewer context.
- Allow final composition to mix explicit `FRESH_PASS` and independently
  accepted `CARRIED_PASS` rows. Carry arbitration records a complete per-gate
  decision set; it is not a prerequisite ordering or downstream suffix rule.
- Enforce pre-implementation QA case design, independent Design Review,
  development handoff binding, and QA Execution admission without creating a
  gate or machine role for every editing step.
- Validate all inputs before state mutation and replace gate state through a
  completed same-directory temporary file.
- Preserve an explicitly selected workflow run directory through generated
  handoff validation. Before a repair, ensure every affected delivery path is
  tracked and use the named external VCS's native state or checkpoint facility
  to fix the pre-repair snapshot. The Carry reviewer compares that snapshot with
  the post-repair snapshot directly; when the exact comparison is unavailable,
  the affected gate cannot enter a new-snapshot rerun without terminal
  `RERUN_REQUIRED`. During repair, the orchestrator may prepare source
  closures, context inputs, and immutable command shape in parallel, but it
  must wait for the worker's exact post-repair VCS snapshot and CLI-composed
  transition before Carry registration, dispatch, or judgment.
- Store every new workflow run under the host-neutral `.gates/runs/` root.
  Claude Code, Codex, and Cursor keep their required host-specific install and
  hook directories, but they do not own separate gate evidence trees.
- Make native install complete by default: every selected Claude Code, Codex,
  or Cursor target receives the runtime subset and the native hooks that host
  actually supports.
  Skipping hook configuration requires explicit `--skip-hooks`; hook merge
  failure fails installation, and unrelated host hooks remain intact.
- Require a usable VCS for formal development, four-gate, Carry, and seal flows.
  The worker invokes the available Git, SVN, P4, or equivalent tool outside
  formal-gates to produce the complete delivery diff and, after a repair, the
  exact repair comparison. The complexity reviewer invokes that same on-site VCS
  directly to inspect its native diff, stat, and changed contents. formal-gates
  owns only external-tool metadata, workflow snapshots, static evidence, and
  decisions. If the VCS cannot make the exact repair comparison, Carry is
  unavailable and affected gates cannot enter a new-snapshot rerun without
  terminal `RERUN_REQUIRED` instead of using Carry.
- Require the worker to submit every delivery path explicitly. The CLI validates
  repository-relative paths, rejects `.gates`, sorts and deduplicates them, then
  generates changed-files evidence and its composition proof. A worker adds each
  new delivery file to the named VCS immediately before further edits and adds
  an existing untracked delivery file before modifying or deleting it. Before a
  repair, every path it may touch is tracked before the native comparison
  boundary is fixed. Only explicit delivery paths may be added, never the whole
  worktree. Before return, every delivery path is
  tracked and present in the complete VCS diff, while unrelated untracked files
  remain untouched. The CLI does not scan the VCS or worktree or infer intent.
  No backend adapter, no-VCS backup, content store, draft/freeze lifecycle, or
  best-effort fallback is added.
- Audit the complete repository for obsolete contracts, conflicting docs, and
  duplicated ownership. Delete obsolete paths and consolidate each proven
  duplicate rule behind one existing or narrowly chosen owner without merging
  host/platform responsibilities that have different behavior.
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
   existing receipt chain to Design Review and carry-forward arbitration,
   with each feature's JSON,
   policy, validator, and tests delivered together on the shared envelope.
2.5. **Review scheduling and static generation:** use the confirmed schedule:
   run the three pre-development checks in parallel, run QA Execution and the
   three post-development gates in parallel, and after each repair use a new
   independent zero-context agent to compare the pre-repair and post-repair VCS
   snapshots and decide per gate whether the repair requires a rerun or permits
   inheritance; do not include unrelated local worktree changes. Add no A/B/C selector or
   duplicate experiment, selector, or additional reviewer layer. Move every
   static formal field and artifact envelope to CLI generation, leaving AI only
   semantic judgment and observation slots.
3. **Operational convergence and external VCS evidence:** move workflow evidence
   to `.gates`, require an external VCS and explicit worker-owned delivery path
   input, let workers and reviewers inspect native VCS comparisons directly,
   consolidate proven duplicate owners and obsolete documentation
   repository-wide, require every review role to sweep same-pattern findings and
   their related causal chains before returning, re-run the Phase 1
   stale-vocabulary scan, complete broad verification, and obtain fresh review
   before delivery.

Each phase has its own implementation snapshot and review. Later phases do not
silently enter an earlier phase, and Phase 1 has no separately deliverable
Markdown or schema-v1 intermediate state. Until Phase 2 implements Carry, any
deliverable change still invalidates earlier-snapshot PASS results.

## Impact

- Existing Go CLI and validator packages gain stricter typed validation; no new
  package or framework layer is introduced.
- Formal evidence format is breaking and old workflows restart.
- The workflow-run root is `.gates` for every host.
- Formal workflows require caller-generated VCS diffs and explicit delivery
  paths. Formal-gates does not add built-in Git/SVN/P4 acquisition, an
  independent snapshot command, backend adapters, project-content storage, or a
  no-VCS source backup.
- Agent and reference templates move from Markdown field detection to the same
  JSON/check vocabulary used by the validator.
- Document checkboxes remain non-authoritative progress hints outside formal
  admission and finalization.
