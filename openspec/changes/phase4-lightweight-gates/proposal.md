# Proposal

The current workflow has accumulated a large internal evidence and lifecycle
system around a small number of semantic reviews. That system consumes time and
tokens, produces many temporary files, and duplicates responsibilities already
provided by the external VCS and the independent reviewer.

Phase 4 keeps the quality-bearing parts: a confirmed requirement, native VCS
comparison, an independent zero-context reviewer, requirement-bound QA cases,
a real QA execution, same-chain completeness sweeps, and final aggregation on
one immutable snapshot. It removes the internal version-management and
proof-management layers that do not improve the review decision for this
personal project.

The review architecture becomes file-driven. `prompts/reviewer-base.md` is the
single shared reviewer contract. Every installed `gates/<gate-id>.md` is one
post-development independent review gate. The CLI discovers, validates, sorts,
and composes these files; the host skill starts the independent agents. Adding
or removing a gate therefore means adding or removing one package prompt file
and reinstalling; no Go gate registry, YAML manifest, weight, dependency graph,
provider adapter, or duplicated agent catalog is required.

## Non-goals

- No custom version-control system, snapshot store, backup store, or no-VCS
  fallback.
- No all-gates-final-rerun feature in this phase.
- No adversarial tamper resistance beyond the documented normal workflow.
- No compatibility reader for the removed internal artifacts.
- No project-local gate overlay or automatic semantic linting of prompt prose.
