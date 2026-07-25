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
After their code is merged and conflicts are resolved, the combined result
receives one integration formal run using the original full or custom route.
