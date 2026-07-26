# Universal Modification Workflow

Date: 2026-07-26
Status: confirmed
Route for this delivery: full

## Complete Requirements

1. Every modification request uses the same intake in every repository. Before
   writes, the main agent clarifies the requested outcome and consequential
   technical choices, then presents the complete consolidated requirement and
   solution and waits for explicit confirmation.
2. The main agent recommends and asks once for lightweight, full, or custom
   execution. Full means QA plus every discovered gate. Custom presents QA and
   the complete gate list in the same question. Added requirements retain the
   original choice unless the user explicitly reopens it.
3. Lightweight execution creates no formal run and requires no formal
   requirements document, but users may still request PRD, OpenSpec, design, or
   other documents as ordinary deliverables.
4. Full and custom execution require a durable complete requirements and
   solution document before development. No document format or plugin is
   mandatory. Both routes run Start Readiness automatically.
5. Formal work that is too large for one coherent unit is split by dependency,
   ownership, risk, and verification boundaries. Independent slices use
   separate VCS worktrees and formal runs in parallel. Dependent slices wait
   for their prerequisites. Before sliced development, one overall formal run
   fixes the complete request's original base, requirements, and route and stays
   active until integration finishes. Lightweight work has no formal task
   slices.
6. After parallel formal slices pass, merge their code and resolve conflicts.
   Record the merged code as the retained overall run's completed development
   snapshot, then run its integration QA and gates from the original base to the
   merged result. Do not start another run, replace the base, or repeat
   clarification or routing after the merge.
7. A read-only package command lists QA and dynamic gates before a run by
   reusing the existing prompt catalog. The obsolete formal none route is
   removed from CLI, state validation, tests, skill text, and public docs.
8. For a narrowly bounded repair, the main agent may inspect the immediate
   repair diff, record its reason, skip the independent Carry agent, and inherit
   every prior PASS QA and gate result. Any non-PASS remains for normal handling.
   Shared or uncertain impact requires independent Carry.
9. No intake, maintenance, or execution rule changes based on whether the
   current repository is formal-gates itself.

## Confirmed Technical Solution

- Add `formal-gates package route-candidates --root <package>` as a stateless
  JSON query backed by `LoadPromptCatalog` and the existing gate ordering.
- Extend the current `workflow carry` command and Carry state owner with an
  all-or-nothing main-agent inheritance mode. Preserve independent Carry as the
  fallback and record structured decision origin, snapshots, and reason.
- Rewrite `SKILL.md` around a mandatory pre-write intake, one combined route
  choice, durable formal requirements, dependency-aware parallel worktrees,
  and post-merge integration formal review.
- Remove the `none` route rather than retaining a compatibility path. Do not add
  lightweight workflow state, a second gate scanner, a size registry, a second
  Carry state machine, or project detection.

## Delivery Slices

1. `pre-run-route-candidates`: stateless package candidate query.
2. `small-repair-inheritance`: main-agent Carry/QA inheritance shortcut.
3. `universal-modification-intake`: mandatory intake, durable formal
   requirements, route simplification, parallel slicing, none removal, and
   documentation alignment.
4. Integration: keep the overall run that began before sliced development,
   merge the sealed slices, resolve conflicts, record the merged snapshot, and
   run that same overall run's full post-development review on the combined
   result.

Slices 1 and 2 are independent and run in parallel. Slice 3 depends on their
public contracts and begins after both are integrated.
