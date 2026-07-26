# Requirements Alignment

Date: 2026-07-26
Status: confirmed

## RQ-001 - Independent review dispatches are uniquely bound

Every QA Review and discovered post-development gate preparation SHALL create a
new unique dispatch record. The record SHALL bind the action or gate, current
review wave and attempt, exact composed prompt hash, current requirement and
catalog revisions, and immutable current VCS identity.

The result contract SHALL return the dispatch identifier. Recording SHALL bind
one host-provided reviewer or session identity to that dispatch and reject a
missing, unknown, stale, already completed, or mismatched dispatch before
changing semantic state.

## RQ-002 - Review identities are not reused

QA Review and every post-development review gate SHALL use a zero-context
reviewer identity that has not been used by any prior QA Review or gate review
in the same run. This applies across gates, retries, repair waves, and runtime
error retries. The CLI SHALL reject reuse before recording the result.

QA Design, QA Execution, Carry, requirements clarification, Start Readiness,
and development workers SHALL NOT require this review-identity uniqueness.

## RQ-003 - The CLI owns static source bindings

Prepared dispatch state SHALL be the single owner of requirement revision,
catalog revision, prompt hash, review attempt, review wave, and source snapshot.
Normal result-recording commands SHALL obtain those values from the named open
dispatch instead of requiring the operator to transcribe them independently.

The existing semantic result fields remain explicit. Removing repeated static
arguments SHALL NOT let a result replace an authoritative semantic result or
bypass current transition checks.

## RQ-004 - The selected native VCS owns snapshot identities

The main agent SHALL select the repository's actual VCS when starting the run.
For Git, SVN, and P4, the CLI SHALL use the selected VCS's native command to
resolve and verify the immutable base, current, pre-repair, and Seal identities
needed by the existing workflow. It SHALL NOT hard-code Git behavior, silently
detect a different VCS, switch VCS during a run, guess an identity, or retain
diff bytes.

If the selected native VCS cannot reproduce the required identity under normal
documented use, the command SHALL fail without mutating workflow state. Other
VCS implementations require an explicit supported resolver; there is no
fallback snapshot engine.

## RQ-005 - Requirement documents freeze at development start

Before development, the run SHALL register the primary requirement source and
every additional requirements or solution document for the task, independent
of whether those files use OpenSpec, PRD, or ordinary Markdown conventions.
The CLI SHALL compute and store their revisions. Preparing the first development
worker SHALL freeze that exact set and revision.

After development starts, ordinary development, QA, review, Carry, repair, and
Seal SHALL reject a changed frozen artifact. A change SHALL return to the
existing requirement clarification or meaning-changing requirement path and
establish a newly confirmed development boundary before related writes resume.
Meaning-preserved rebinding SHALL NOT update frozen requirement artifacts after
development has started.

## RQ-006 - Frozen requirement documents are acceptance input, not review targets

Post-development QA and reviewers MAY read the frozen requirement and solution
documents as their acceptance standard. The VCS route and prompt SHALL identify
the frozen artifact paths as excluded review targets, and findings SHALL NOT
request ordinary repair edits to those files.

README, SKILL, changelog, examples, and other product documentation remain
normal delivery and review targets unless a path was explicitly registered as
a requirement artifact for the run.

## RQ-007 - Formal QA contains static and live execution

Every QA case SHALL declare exactly one kind: `STATIC` or `LIVE`. `STATIC`
means a fast direct-owner automated check such as a unit, parser, validator,
build, vet, or package check. `LIVE` means actual execution through a documented
public entrypoint against the built current snapshot in an isolated normal-use
environment, with observable effects and an oracle.

The complete candidate set SHALL contain at least one case of each kind. QA
Review SHALL reject a set that omits either kind or substitutes code inspection,
developer self-test claims, or simulated output for live execution. QA
Execution SHALL run every approved case and record the actual procedure,
observation, and oracle comparison.

The workflow SHALL test each deterministic rule at its lowest owning layer and
SHALL NOT require the same rule to be mechanically repeated at both kinds when
the higher layer adds no distinct normal-use behavior.

## RQ-008 - The lightweight architecture remains intact

The implementation SHALL extend the existing single temporary run state,
action and gate preparation, prompt composition, transition checks, and semantic
result records. It SHALL reuse small native ideas from historical dispatch and
snapshot code where compatible.

It SHALL NOT restore or add an evidence graph, receipt subsystem, policy engine,
closure graph, provider adapter, hook-dependent identity proof, second VCS
model, project-specific route, dry-run phase, or compatibility path for
manually rewritten temporary state.

