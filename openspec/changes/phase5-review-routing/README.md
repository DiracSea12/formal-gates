# Phase 5: Clarification And Review Routing

This implementation record is amended by
`../universal-modification-intake/alignment.md`. Current routing is:

- lightweight execution creates no formal run;
- a formal run accepts only `full` or `custom`;
- `full` selects QA and every discovered gate;
- `custom` selects a non-empty proper subset.

The alignment, design, specification, and tasks in this directory use that
current contract while retaining the rest of the Phase 5 behavior.
