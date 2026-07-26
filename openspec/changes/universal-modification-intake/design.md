# Design

## Hard intake before Single Formal Flow

Place a mandatory Universal Modification Intake section before formal-run
instructions and make the skill description activate for all modification
requests. Use explicit prohibition language: no file write or implementation
dispatch before outcome and consequential solution clarification, consolidated
summary presentation, and user confirmation.

The route choice follows clarification. Use the stateless package candidate
command to build the same-question `full/custom` list. Start CLI workflow state
only for `full` or `custom`; lightweight work remains conversational and
stateless.

## Route simplification

Remove `none` from route-mode validation and public command documentation. New
runs accept `full` and `custom`; custom still allows any non-empty selected
subset from QA plus discovered gates. Keep Start Readiness automatic for both.
Remove tests and branches whose sole purpose is a none route, and adjust tests
that need an empty route to use the appropriate pre-route state instead.

## Conversation-owned continuity

The main agent retains route choice and task-slice plan in the active
conversation. Do not add CLI state for lightweight work. Added requirements
reopen semantic clarification and complete-summary confirmation, not route
selection. Explicit user direction may reopen the route choice.

Before formal development, write the complete confirmed requirements,
technical solution, and slice dependencies in any stable document form the
environment supports. Do not depend on a PRD/OpenSpec plugin. Lightweight work
does not create formal slices or require this formal artifact.

For independent formal slices, create separate native VCS worktrees and runs.
Before dispatching them, start one overall formal run at the complete request's
original VCS base with `workflow start --retained-overall` and keep it active
throughout sliced development. Slice workers alone own implementation. Dispatch
independent slices concurrently when host capacity allows. Slice runs provide
their own bounded verification but do not replace the overall run.

Merge sealed slice branches and resolve conflicts, then use the existing
`workflow snapshot` transition to record that merged VCS identity directly as
the retained overall run's completed development snapshot. Run the final
integration QA and gates in that retained overall run against its original
base-to-merged comparison. Do not prepare or dispatch an overall development
task, create a new integration run after the merge, move the original base, or
repeat clarification or routing. Dependent slices start only after their
upstream contracts are integrated. Integration findings return to their owning
slice runs. Merge sealed slice repairs, then record the new merged snapshot
directly in the retained overall run without preparing an overall development
worker.

## Documentation consistency

Update `SKILL.md`, Chinese and English READMEs, agent metadata, changelog, and
behavior examples. Remove routine/tiny modification exemptions and the
source-repository maintenance special case. Preserve operational installation
and host-canary instructions that describe formal-gates capabilities without
changing intake based on repository identity.

Integrate the small-repair shortcut and stateless candidate command already
delivered by prior slices. Do not add a second catalog, project detector, size
registry, lightweight state file, or another Carry owner.

## Result validation hard stop

Treat every independently returned result as unrecorded input. Before invoking
any workflow command that records FAIL or a blocking finding, the main agent
must check the finding against the complete confirmed requirement, verify that
its premise matches the actual retained workflow state, reproduce its public
normal-use path independently, and inspect the cited evidence. Reject the
finding if any check fails.

This validation is orchestration, not another review gate or another agent.
Reuse the existing result-recording commands only after validation. Do not add
an evidence database, approval state, second verdict, or compatibility layer.
