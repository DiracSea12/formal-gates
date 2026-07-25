# Tasks

## Alignment and routing

- [x] Make Requirements Clarification an adaptive main-agent hard gate for
  requirement and consequential solution alignment.
- [x] Add one none/full/custom selection over QA plus discovered gates, route
  authorization for omissions, and guarded later additions.
- [x] Pause changed requirement revisions until an explicit semantic-effect
  decision preserves or invalidates dependent state.
- [x] Apply one direct transition guard to every prepare and record owner.
- [x] Keep the main agent read-only and make the development worker the formal
  delivery-code write owner.

## Review, repair, and Seal

- [x] Add P0/P1/P2 validation to discovered-gate findings and aggregate P2
  with a wave containing blockers; allow PASS to retain P2-only findings.
- [x] Share three completed repair cycles across selected QA and gates; count
  only after the repair snapshot and required verification complete.
- [x] Keep Carry limited to previously passing selected gates and rerun
  selected QA after repair.
- [x] Reject Seal for selected PENDING results; allow retry or explicit skip
  authorization for RUNTIME_ERROR without consuming a repair cycle.
- [x] Require persisted route or Seal authorization for every skipped or
  unresolved required result and retain it in the summary.

## Focused verification

- [x] Update direct workflow tests for routing, transition rejection, shared
  cycle completion, severity, Carry scope, and Seal authorization.
- [x] Update only CLI parsing tests with unique coverage, then run the existing
  repository validation commands and the selected post-development gates.
- [x] Remove or consolidate superseded code and documentation before adding
  any new file or abstraction.
