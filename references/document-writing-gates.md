# Document Writing Gates

Use this reference before writing or changing OpenSpec proposals, designs, specs, tasks, PRDs, SDDs, phase documents, requirement notes, or any document that defines scope, acceptance, architecture direction, or development readiness.

This is a writing workflow, not an implementation approval, release seal, or final QA verdict.

## Gate 1: Requirement Clarification

Run `requirements-clarification-gate.md` before drafting. Carry its confirmed answers, open questions, and draft status into this writing workflow.

Do not let the document guess the user's goal, acceptance standard, non-goal, architecture boundary, evidence requirement, or intended scope. If the clarification gate returns `DRAFT_BLOCKED`, the document may only be draft/unsealed.

## Gate 2: Architecture Shape Review

Check architecture shape before treating the document as ready:

- Does the document put responsibilities in the right layer?
- Does it preserve ownership boundaries and dependency direction?
- Does it introduce public API, config, state, cache, provider, registry, manager, or service concepts without current need?
- Does it hide runtime behavior in infrastructure wording?
- Does it leave the development agent guessing the architecture boundary?

Catch wrong direction and unclear ownership, not sentence polish.

## Gate 3: Complexity and Scope Review

Review scope before development readiness:

- Proposal/spec is the requirement source of truth.
- Plans, tasks, Complexity Contracts, handoffs, gate verdicts, and implementation slices may split or constrain delivery, but must not rewrite or shrink the user's original requirement.
- Only the user may approve changing the actual requirement goal or acceptance standard.
- A focused implementation must be marked as `slice`, `partial`, or `focused implementation`, and must list uncovered proposal/spec requirements.
- Do not add new platforms, reports, caches, state machines, managers, services, registries, or frameworks unless the current requirement proves they are needed.
- Remove stale implementation paths, wrong technical choices, deprecated compatibility chains, and descriptions that conflict with current code facts. Do not remove user goals just because they are hard.

If unauthorized scope shrinkage is found, block with:

```text
REQUIREMENTS_SCOPE_MISMATCH
Original requirement:
Unauthorized shrink:
Where it happened:
Required action:
```

## Gate 4: Cold-Water Start-Readiness Review

Review to the standard of "development can proceed without direction errors or blockers." Do not turn this into sentence polishing.

Block only for issues that would make a developer guess the main direction, architecture boundary, acceptance standard, required evidence, or actual scope.

Non-blocking wording issues, minor phrasing, or details that can safely be handled during implementation or later gates should be recorded as risks or follow-up checks, not used to bounce the document forever.

## Required Output

For formal document work, record:

```text
Document Writing Gates
Document/change:
Gate 1 Requirement Clarification: PASS / DRAFT_BLOCKED / SKIPPED_BY_USER
User answers captured:
Open questions:
Draft/seal status:
Gate 2 Architecture Shape: PASS / REVIEW / FAIL
Gate 3 Complexity and Scope: PASS / REVIEW / FAIL
Gate 4 Cold-Water Start-Readiness: PASS / REVIEW / FAIL
Verdict: DRAFT_ONLY / READY_FOR_ZERO_CONTEXT_REVIEW / BLOCKED
Required next action:
```

`READY_FOR_ZERO_CONTEXT_REVIEW` is not development approval. Formal OpenSpec/start-readiness still requires independent zero-context complexity review, architecture-health review, and cold-water review before any "can develop" conclusion.
