# Requirements Alignment

Date: 2026-07-26
Status: confirmed

## RQ-001 - Universal modification activation

Every request that creates, edits, moves, or deletes project content SHALL
activate formal-gates intake regardless of repository, product, file type, or
estimated size. The skill SHALL NOT distinguish changes to formal-gates from
changes to another project.

Read-only questions, explanations, diagnostics, and reviews that request no
modification remain outside automatic modification intake unless the user
explicitly requests formal execution.

## RQ-002 - Outcome and solution clarification precede writes

Before any write or implementation dispatch, the main agent SHALL clarify the
requested outcome and every consequential technical choice. It SHALL inspect
workspace facts rather than ask the user, ask one consequential decision at a
time, and remain read-only until clarification completes.

After clarification and immediately before writes, the main agent SHALL present
the complete consolidated requirements and technical solution and wait for
explicit user confirmation. When no earlier question was needed, this final
confirmation MAY be combined with the route choice.

## RQ-003 - One combined route choice

The main agent SHALL assess total size, coupling, and risk, explain its
recommendation, and present exactly these choices once:

- lightweight execution with no formal run;
- `full`, meaning QA plus every dynamically discovered gate;
- `custom`, showing QA and the complete dynamic gate list for subset selection.

The main agent SHALL obtain the list before a run through the stateless package
route-candidate query. `full` and `custom` are formal execution. Both SHALL run
Start Readiness automatically before development; Start Readiness is not a
custom gate choice and does not consume a post-development review wave.

## RQ-004 - Planning artifacts are independent

When the user chooses lightweight execution, formal run state and independent
formal gates SHALL be omitted. PRD, OpenSpec, design documents, or task plans
MAY also be omitted without any additional small-change classification.

The user MAY request any planning artifact while retaining lightweight
execution. Creating a PRD or OpenSpec SHALL NOT automatically select formal
gates.

For `full` or `custom`, a durable complete requirements and technical solution
document SHALL exist before development. The workflow SHALL not require PRD,
OpenSpec, or any other named format or plugin; any stable document form
available in the current environment is valid.

## RQ-005 - One choice covers additions

The chosen route SHALL cover the total request and requirements added during
development. Added requirements SHALL receive necessary outcome and solution
clarification, followed by a refreshed complete summary and explicit
confirmation before related writes. The route question SHALL not repeat unless
the user explicitly requests reconsideration.

## RQ-006 - Oversized requests are sliced

When the confirmed total request cannot be implemented and verified coherently
as one bounded unit, the main agent SHALL split it into dependency-ordered task
slices with clear outcomes, ownership, and verification surfaces. Sizing SHALL
use engineering judgment over scope, coupling, risk, and verification
complexity, not a fixed line, file, or token threshold.

Under `full` or `custom`, each slice SHALL use an independent formal run and
inherit the one route choice. Cross-slice dependencies and the complete outcome
SHALL remain explicit.

Before slice development begins, the main agent SHALL start and retain one
overall formal run for the complete request. That overall run owns the original
base snapshot, complete confirmed requirements, selected route, and final
integration review. Slice runs supplement the overall run; they do not replace,
restart, or close it.

Independent slices SHALL use separate VCS worktrees and MAY run concurrently.
Dependent slices SHALL wait for their prerequisites. After parallel slices are
merged and conflicts are resolved, the main agent SHALL record the merged VCS
identity as the completed development snapshot of the retained overall run.
That same overall run SHALL then execute one integration post-development wave
using its original base-to-merged comparison and inherited route. The workflow
SHALL NOT start a second overall or integration run after merging, replace the
original base, repeat Requirements Clarification, or repeat the route question.

Lightweight execution SHALL NOT introduce formal task slices.

## RQ-007 - Formal none route is removed

Lightweight execution happens before a formal run exists. The CLI, state
validation, tests, skill, READMEs, examples, and metadata SHALL remove the
formal `none` route and every description of a run with no selected formal
route. New formal runs SHALL accept only `full` or `custom`.

No compatibility or migration path is required for manually altered or old
temporary run state outside the documented active workflow.

## RQ-008 - Repair guidance includes the fast path

The rewritten skill and public documentation SHALL describe the confirmed
small-repair shortcut: the main agent may inherit prior PASS QA and gates only
after bounding the immediate repair diff; otherwise independent Carry remains
required. This slice SHALL consume the already implemented Carry command and
state contract rather than add another shortcut.
