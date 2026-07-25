# Phase 5: Clarification And Review Routing

Phase 5 makes formal development start with adaptive alignment of both the
requested outcome and consequential technical choices. After confirmation,
the user chooses once from a unified dynamic list containing QA and every
discovered gate. The main agent orchestrates but does not edit delivery code;
the CLI releases a separate development worker only when required
pre-development results are complete.

Selected QA and gates share three completed automatic review waves, beginning
with the initial complete post-development wave.
Discovered-gate findings use P0/P1/P2, QA failures block directly, Carry stays
limited to previously passing selected gates, interrupted work remains
resumable, and Seal records explicit route or final skip authorization.
Meaning-changing revisions recheck complete QA coverage while retaining
unaffected cases and changing only the impacted set when impact is bounded.

See `alignment.md` for confirmed requirements, `design.md` for the minimal
state and guard design, `specs/review-routing/spec.md` for observable behavior,
and `tasks.md` for implementation work.
