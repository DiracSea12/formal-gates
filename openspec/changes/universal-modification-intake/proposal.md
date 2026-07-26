# Proposal

Make formal-gates the universal intake for every modification request. Before
any write or implementation dispatch, the main agent clarifies the requested
outcome and consequential technical choices, presents the complete consolidated
requirements and solution, recommends a route from size and risk, and waits for
one explicit user confirmation and route choice.

The route choices become lightweight execution, `full`, or `custom`. A
lightweight choice creates no formal run and may omit formal planning artifacts,
while still allowing the user to request PRD, OpenSpec, or other planning work.
`full` and `custom` create formal runs and automatically include Start
Readiness. The obsolete formal `none` route is removed completely.

One route choice covers the total request, later task slices, and requirements
added during development. Oversized work is split into coherent formal runs,
but the route question is not repeated unless the user explicitly reopens it.
No rule distinguishes the formal-gates source repository from another project.

Independent formal slices may execute concurrently in separate VCS worktrees.
One overall formal run starts before sliced development and retains the complete
request's original base, requirements, and route. After slice code is merged and
conflicts are resolved, the merged snapshot is recorded in that same overall
run, which then executes the integration QA and gates. No new run, base,
clarification, or route choice is created after merging.
Integration findings return to their owning slice runs. Sealed slice repairs
are merged and recorded directly as repaired snapshots in the same retained
overall run, which never prepares its own development or repair worker.

Independent agent conclusions remain candidate inputs until the main agent
validates their requirement premise, normal public-entrypoint reproduction, and
evidence. A blocker that does not survive that independent validation is
discarded and is never recorded or presented as a formal blocker.
