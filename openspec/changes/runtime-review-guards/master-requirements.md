# Slice 4 Master Requirements

Status: confirmed
Date: 2026-07-26

## Confirmed outcome

Restore the lightweight runtime protections that were lost when the historical
evidence-heavy workflow was removed. Keep one workflow state file and the
existing action and gate commands. Do not restore the old evidence, receipt,
policy, closure, or artifact graph.

The delivered workflow must:

1. create a unique binding for every prepared independent review;
2. bind the returned result to that dispatch, its exact prompt, review wave,
   requirement revision, catalog revision, and VCS snapshot;
3. reject reviewer or session reuse across QA Review and post-development gate
   reviews in the same run;
4. obtain immutable snapshot identities through the native VCS selected by the
   main agent instead of requiring repeated handwritten identities;
5. support Git, SVN, and P4 without treating Git as the universal environment;
6. freeze every registered requirements and solution document when development
   starts, use the frozen documents only as acceptance input after that point,
   and exclude them from post-development review targets;
7. return any later requirement-document change to requirement clarification or
   requirement change instead of accepting it as ordinary repair; and
8. require the approved QA set and QA Execution to contain both fast static
   checks and real execution through documented public entrypoints; and
9. let QA Review approve cases individually so unchanged passing cases are not
   reviewed again while failed, new, or changed cases remain reviewable.

Dry-run behavior is not part of this slice. QA Design, QA Execution, Carry, and
development workers do not require zero-context identity isolation. QA Review
and every discovered post-development review gate do.

## Confirmed solution boundaries

- The main agent chooses the repository's actual VCS once when the run starts.
- The CLI invokes that selected VCS through a small native resolver for Git,
  SVN, or P4. It does not implement a second VCS or silently fall back.
- The primary requirement source is registered automatically. Additional PRD,
  OpenSpec, design, specification, or ordinary Markdown paths are registered
  explicitly before development; the CLI computes and owns their revisions.
- Product documentation such as README and SKILL files remains normal delivery
  content unless it was explicitly registered as a requirement artifact.
- QA cases gain one structured kind: `STATIC` or `LIVE`. The complete approved
  set must contain both kinds, without mechanically repeating every
  deterministic rule at both layers.
- QA Review returns one outcome for every case assigned in that attempt. The
  CLI stores those outcomes on the existing cases, preserves PASS only for
  semantically unchanged cases retained by incremental QA Design, and sends
  only failed, new, or changed cases for decision on the next attempt. Prior
  passing case descriptions may be shown as accepted coverage context but are
  not reopened for review.
- Existing run state, prompt composition, transition validation, and result
  recording remain the owners. No provider SDK, receipt hook, identity service,
  evidence database, compatibility path, or adversarial hardening is added.
