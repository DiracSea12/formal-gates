# Requirement Document Adapters

This reference maps local requirement-document formats into the formal-gates workflow. Keep core gate language generic; use this file only when a specific document format needs routing.

## Common Requirement Fields

Every supported format must provide or map to:

- goal and user value;
- scope and non-goals;
- acceptance criteria and evidence required;
- architecture boundary and constraints;
- requirement details that can change implementation or validation;
- tasks or completion status, when the format has task tracking.

The user-confirmed requirement source stays authoritative. A document adapter can organize evidence, but it cannot narrow, delete, or replace confirmed user intent.

## OpenSpec Adapter

Use this adapter when the requirement source is under `openspec/changes/<change>/`.

Map OpenSpec files this way:

- `proposal.md`: goal, why, scope, non-goals, acceptance summary.
- `design.md`: architecture boundary, design constraints, risks, migration notes.
- `tasks.md`: task checklist and completion status; checkboxes are routing hints, not proof.
- `specs/**/spec.md`: requirements, scenarios, and acceptance oracles.
- change directory path: concrete covered target for requirements-clarification artifacts.

When machine artifacts still require the compatibility field `OpenSpec impact:`, use it for OpenSpec changes. For non-OpenSpec formats, write the equivalent document impact in the alignment item and keep the covered target precise.

## Generic Markdown Adapter

Use this adapter for PRD, SDD, issue, design brief, phase note, or a markdown requirement bundle.

Map generic documents this way:

- PRD or issue: goal, user value, scope, acceptance, business rules.
- SDD or design brief: architecture boundary, ownership, constraints, failure semantics.
- phase note or task list: task status and phase dependencies.
- linked evidence files: validation standard and required artifacts.

If the bundle lacks a field that can change direction, ask a clarification question before drafting or dispatching development. Do not infer missing acceptance, compatibility, or evidence requirements from implementation or prior gate artifacts.

## Adapter Limits

- Do not treat an adapter as a new gate.
- Do not add a new artifact platform for each document format.
- Do not rename the four fixed post-development gate IDs.
- Do not claim a document is complete just because its adapter mapping exists.
