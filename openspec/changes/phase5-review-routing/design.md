# Design

## Reuse existing state owners

Do not store a second phase value. Derive allowed transitions from the existing
requirement confirmation, action results, QA cases and result, gate results,
current and pre-repair snapshots, and Carry decisions.

Add only:

- confirmed route mode and selected gate IDs, where `qa` is the user-facing QA
  gate and other IDs come from the existing catalog;
- skip authorizations with route-selection or Seal origin;
- one completed repair-cycle count and explicit extra-cycle authorization;
- severity on discovered-gate findings.

The state schema changes in place. Do not migrate or silently default older
active run state.

## Main-agent and worker boundary

Requirements Clarification is an interactive main-agent action. Its prompt
drives one consequential question at a time and prohibits delivery writes or
development dispatch until the user confirms shared understanding. The CLI
continues to bind the run's supplied requirement source and revision; it has no
knowledge of the source's business meaning or file convention.

The main agent remains the orchestrator for the formal run and never edits
delivery code. The existing independently dispatched development worker is the
write owner. `prepare-action development-worker` applies the complete entry
guard before composing a task.

When Resume detects a changed requirement revision, it returns a pending
classification and does not mutate dependent state. The existing requirement
update owner accepts one explicit semantic-effect decision. A
meaning-preserving decision rebinds the revision while retaining results; a
meaning-changing decision uses the existing invalidation path. The main agent
makes that decision from the confirmed meaning and asks the user when it cannot
establish preservation. The CLI does not infer meaning from filenames, changed
lines, edit categories, or project vocabulary.

## Gate selection and direct guards

Expose the current route candidates through the existing run/catalog view:
`qa` first, then lexically sorted discovered gate IDs. One route mutation
records none, full, or the exact custom set. A custom complement becomes
route-authorized skips. Later additions use the same mutation owner and reject
nodes whose prerequisite point has passed. Requirement rebinding and
invalidation preserve this route and do not emit another route prompt.

Use one central validation function from every prepare and record path. Each
operation reads only the minimum workflow state needed for its direct ordering
prerequisites. In particular, development requires confirmed routing, passing
Start Readiness, and QA cases only when QA is selected. Post-development
review requires a bound immutable snapshot and selection of the requested
gate. This optimization is limited to sequence guards and does not reduce the
scope of clarification, readiness, QA, review, or repository verification.

## Review and repair aggregation

Gate recording validates severity without exposing routing policy to the
reviewer. QA retains case PASS/FAIL results. Once every selected result for a
wave is present, aggregate it under the existing state lock:

- QA FAIL or any P0/P1 starts repair and includes all P2 from that wave;
- a discovered gate records `PASS` with no findings or P2-only findings,
  `FAIL` with at least one P0/P1 and optional P2, or `RUNTIME_ERROR` with no
  findings;
- P2-only results remain visible recommendations for final user disposition;
- no findings reaches the Seal-ready condition.

A repair snapshot resets selected QA, retains prior passing selected gates for
Carry, and resets other selected gates for rerun. After the last required QA,
Carry, or gate verification is recorded, atomically increment the shared count
once. Existing snapshot bindings distinguish an initial review from a repaired
wave; no repair-attempt database is needed.

## Seal authorization

The existing `PENDING` status continues to mean incomplete or interrupted
dispatch. Resume continues it and Seal rejects it. `RUNTIME_ERROR` remains
distinct from a semantic result; it may be retried or covered by explicit user
skip authorization. Neither status consumes a repair cycle, and no runtime
retry counter is added.

Before the effective cycle limit is exhausted, repairable blockers reject
Seal and cannot receive skip authorization. After exhaustion, repeated Seal
skip inputs must exactly cover the remaining QA FAIL and discovered-gate P0/P1
results the user authorized. Route-authorized skips cover custom omissions.
P2-only recommendations are shown for the user's final decision but do not
block Seal. Persist both skip origins in the retained summary. Explicit extra
repair authorization increases only this run's effective limit and returns it
to the normal repair path.
