# Proposal

Phase 5 prevents formal development from starting before the user and main
agent align on both the requested outcome and consequential technical choices.
It then records one full, custom, or empty gate selection and makes every
selected stage mandatory in the existing lightweight workflow.

The same change completes review routing: selected QA and discovered gates
share three automatic complete review waves beginning with the initial
post-development wave, discovered-gate findings use P0/P1/P2,
P2 joins a repair when blocking findings exist, and Seal requires explicit
authorization for repairable blockers after the review-wave limit. Interrupted
dispatch remains resumable and cannot Seal; runtime errors may be retried or
explicitly skipped without completing a review wave.

The implementation reuses the current state and transition owners. Dynamic
gate discovery already exists and is preserved. No scheduler, compatibility
path, project-specific router, second state store, or host capability claim is
introduced. Meaning-changing revisions re-review complete QA coverage while
retaining unaffected cases and editing only the affected set; full redesign is
reserved for changes whose impact cannot be bounded reliably or spans the
overall workflow.
