# Design

## Reuse existing state owners

Do not store a second phase value. Derive allowed transitions from the existing
requirement confirmation, action results, QA cases and result, gate results,
current and pre-repair snapshots, and Carry decisions.

Add only:

- confirmed route mode and selected gate IDs, where `qa` is the user-facing QA
  gate and other IDs come from the existing catalog;
- skip authorizations with route-selection or Seal origin;
- one completed review-wave count and explicit extra-wave authorization;
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

Preparation is a persisted dispatch state, not a one-shot prompt. Repeating
preparation for a `PREPARED` or `REPAIR_PREPARED` development action recomposes
the same current task after interruption while preserving its start boundary.

When Resume detects a changed requirement revision, it returns a pending
classification and does not mutate dependent state. The existing requirement
update owner accepts one explicit semantic-effect decision. A
meaning-preserving decision rebinds the revision while retaining results; a
meaning-changing decision uses the existing invalidation path. The main agent
makes that decision from the confirmed meaning and asks the user when it cannot
establish preservation. The CLI does not infer meaning from filenames, changed
lines, edit categories, or project vocabulary.

On a meaning-changing decision, reset QA Design completion and QA Review
approval but retain the prior approved cases in the existing case collection
as unapproved review input.
Compose QA Design from the complete current requirement plus those cases, while
continuing to withhold production implementation, diffs, and tests. The agent
checks complete coverage, carries unaffected cases forward, edits only affected
cases, and returns the complete resulting case set. The existing `qa-design`
record owner atomically replaces the collection and restores approval. If the
agent cannot bound impact reliably or the workflow changed as a whole, it
returns a fully redesigned complete set. No case-history store, change-kind
registry, or second approval state is added.

## QA Design Review loop

Add `qa-review` as an installed workflow action, not a discovered gate. QA
selection activates QA Design, QA Review, and QA Execution together. Start
Readiness and QA Design may run in parallel, but QA Review is composed only
after QA Design records the complete candidate set. Its prompt contains the
confirmed requirement and candidate cases and excludes production code, diffs,
tests, developer explanations, later results, and other reviewer conclusions.

Record QA Review through the existing action-result owner. PASS unlocks
development. FAIL retains the candidate cases as unapproved input, resets QA
Design to PENDING, and permits a new design task; recording the revised complete
set resets QA Review to PENDING for a fresh independent dispatch. RUNTIME_ERROR
and PENDING remain on QA Review and can be recomposed without reopening design.
The loop uses existing action statuses and QA cases, adds no review-cycle state,
and never touches the post-development completed-wave count.

## Gate selection and direct guards

Expose the current route candidates through the existing run/catalog view:
`qa` first, then lexically sorted discovered gate IDs. One route mutation
records none, full, or the exact custom set. A custom complement becomes
route-authorized skips. Later additions use the same mutation owner and reject
nodes whose prerequisite point has passed. Requirement rebinding and
invalidation preserve this route and do not emit another route prompt.
Start Readiness is keyed to the effective selected set rather than only the
original route label. When a post-development addition changes an initial none
route into a non-empty selection, set readiness to its required pending state
and block preparation of the added gate and Seal until it passes. Keep the
current immutable development snapshot; readiness itself does not create a
snapshot or complete a review wave.

Use one central validation function from every prepare and record path. Each
operation reads only the minimum workflow state needed for its direct ordering
prerequisites. In particular, development requires confirmed routing, passing
Start Readiness, and passing QA Review only when QA is selected.
Post-development review requires a bound immutable snapshot and selection of
the requested gate. This optimization is limited to sequence guards and does
not reduce the scope of clarification, readiness, QA, review, or repository
verification.

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

Once QA or a discovered gate has a semantic PASS or FAIL for the current
snapshot, prepare and record reject a same-snapshot semantic replacement.
PENDING and RUNTIME_ERROR remain retryable.

A development or repair snapshot starts one uncounted review wave. A repair
snapshot resets selected QA, retains prior passing selected gates for Carry,
and resets other selected gates for rerun. After the last required QA, Carry,
or gate verification is recorded, complete the wave only when no selected
result is `PENDING` or `RUNTIME_ERROR`, then atomically increment the shared
count once regardless of semantic outcome. Reuse the existing development
action status to mark that the current immutable snapshot has been verified,
so duplicate result submission cannot count it twice. Snapshot bindings and
that marker distinguish an uncounted wave from a counted one; no repair-attempt
or separate wave database is needed.

## Seal authorization

The existing `PENDING` status continues to mean incomplete or interrupted
dispatch. Resume continues it and Seal rejects it. `RUNTIME_ERROR` remains
distinct from a semantic result; it may be retried or covered by explicit user
skip authorization. Neither status completes a review wave, and no runtime
retry counter is added. When that snapshot also has a repairable blocker, an
authorized runtime error no longer prevents repair of the blocker, but the
incomplete wave still does not increment the shared count.

Before the effective review-wave limit is exhausted, repairable blockers reject
Seal and cannot receive skip authorization. After exhaustion, repeated Seal
skip inputs must exactly cover the remaining QA FAIL and discovered-gate P0/P1
results the user authorized. Route-authorized skips cover custom omissions.
P2-only recommendations are shown for the user's final decision but do not
block Seal. Persist both skip origins in the retained summary. Explicit extra
repair authorization increases only this run's effective wave limit and
returns it to the normal repair path. Apply and save every named Seal
authorization before reporting any still-unresolved result, so a partially
authorized attempt survives normal continuation and Resume. Bind Seal-origin
authorization to the current snapshot and clear it when `workflow snapshot`
advances after repair; keep route-origin authorization because it represents
the user's unchanged selection rather than a reviewed result.
