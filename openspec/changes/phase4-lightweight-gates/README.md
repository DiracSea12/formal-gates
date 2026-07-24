# Phase 4: Lightweight Gate Workflow

Phase 4 reduces the workflow to the smallest mechanism that preserves normal
documented review quality. Independent AI review gates are discovered from
`gates/*.md` and run through one GateRunner with a mandatory shared base
prompt. QA, requirements clarification, Carry decisions, and seal remain
workflow actions rather than independently registered prompt gates.

This change is a breaking cleanup. Old receipt, closure, context-bundle,
prompt-copy, recursive-closure, detailed gate-state, and legacy compatibility
paths are removed instead of maintained in parallel.
