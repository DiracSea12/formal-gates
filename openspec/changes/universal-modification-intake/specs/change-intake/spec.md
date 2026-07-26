## ADDED Requirements

### Requirement: Every modification uses one confirmed intake

Every modification request SHALL clarify outcome and consequential solution
choices before writes. The main agent SHALL then present the complete
requirements and solution and wait for explicit confirmation together with one
lightweight, full, or custom route choice.

#### Scenario: Routine edit has no ambiguity

- **WHEN** a modification request has no unresolved consequential choice
- **THEN** the main agent presents its complete interpretation, technical
  solution, size recommendation, and route choices before writing
- **AND** the user's route selection confirms the summary

#### Scenario: Lightweight work requests OpenSpec

- **WHEN** the user selects lightweight execution and separately requests an
  OpenSpec artifact
- **THEN** the artifact is created without starting formal gates

#### Scenario: Formal work has no document plugin

- **WHEN** the user selects full or custom and no PRD/OpenSpec plugin is
  available
- **THEN** the main agent persists the complete requirements and solution in
  another stable document format before development

### Requirement: One route choice survives additions and slices

The chosen route SHALL apply to the total request, every task slice, and later
requirements unless the user explicitly reopens it. Added requirements SHALL
still receive clarification and complete-summary confirmation.

#### Scenario: User adds scope during development

- **WHEN** added scope changes the complete requirement or technical solution
- **THEN** related writes pause for clarification and confirmation of the
  refreshed complete summary
- **AND** no route question is repeated

#### Scenario: Oversized formal request is split

- **WHEN** one bounded implementation and verification unit is not reasonable
- **THEN** the main agent creates dependency-ordered slices with independent
  formal runs that inherit the original route

#### Scenario: Independent slices run concurrently

- **WHEN** two formal slices have no dependency edge
- **THEN** an overall formal run already retains the complete request's original
  base, requirements, and route
- **AND** the slices may run concurrently in separate native VCS worktrees and
  independent slice runs
- **AND** after their branches are merged and conflicts are resolved, the merged
  snapshot is recorded as completed development in the retained overall run
- **AND** that same overall run reviews the original base-to-merged comparison
  with the inherited route without another clarification or route decision

#### Scenario: Lightweight request is large

- **WHEN** the user selected lightweight execution
- **THEN** the workflow does not create formal task slices or require a formal
  requirements artifact

## REMOVED Requirements

### Requirement: Formal runs may select none

**Reason**: Lightweight execution now occurs before workflow state is created;
a formal run with no selected route contradicts the combined route contract.

**Migration**: New documented runs choose `full` or a non-empty `custom` route.
No migration is provided for temporary state outside the documented workflow.
