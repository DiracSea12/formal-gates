# Tasks

## Alignment and routing

- [x] Make Requirements Clarification an adaptive main-agent hard gate for
  requirement and consequential solution alignment.
- [x] Add one none/full/custom selection over QA plus discovered gates, route
  authorization for omissions, and guarded later additions.
- [x] Pause changed requirement revisions until an explicit semantic-effect
  decision preserves or invalidates dependent state.
- [x] On meaning-changing revisions, invalidate QA Design approval but retain
  prior cases as review input, recheck complete coverage, and replace only the
  affected cases unless impact cannot be bounded reliably.
- [x] Apply one direct transition guard to every prepare and record owner,
  including deferred readiness when a none route becomes non-empty, immutable
  same-snapshot semantic results, and resumable prepared development or repair
  dispatch.
- [x] Keep the main agent read-only and make the development worker the formal
  delivery-code write owner.

## Review, repair, and Seal

- [x] Add P0/P1/P2 validation to discovered-gate findings and aggregate P2
  with a wave containing blockers; allow PASS to retain P2-only findings.
- [x] Share three completed review waves across selected QA and gates; count
  the initial post-development wave and every complete post-repair wave once,
  regardless of semantic outcome, only after all required results complete.
- [x] Keep Carry limited to previously passing selected gates and rerun
  selected QA after repair.
- [x] Reject Seal for selected PENDING results; allow retry or explicit skip
  authorization for RUNTIME_ERROR without completing a review wave.
- [x] Require persisted route or Seal authorization for every skipped or
  unresolved required result, save named subsets before reporting other
  blockers, clear Seal-origin authorization on a new repair snapshot, and
  retain it in the summary.

## Focused verification

- [x] Update direct workflow tests for routing, transition rejection, shared
  wave completion, severity, Carry scope, and Seal authorization.
- [x] Update only CLI parsing tests with unique coverage, then run the existing
  repository validation commands and the selected post-development gates.
- [x] Remove or consolidate superseded code and documentation before adding
  any new file or abstraction.
