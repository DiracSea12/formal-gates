# Requirements Clarification Artifacts

This is the cold-path machine reference for `requirements-clarification-gate`. Read it only when recording PASS, building the alignment artifacts, or diagnosing validator failures. For interactive requirement alignment, read `references/requirements-clarification-gate.md` instead.

The machine check cannot prove the user's business intent is correct. It can prove the agent did not claim PASS without the required user-confirmed alignment evidence.

## Machine-Enforced PASS

PASS is allowed only when the clarification result is recorded as an artifact and validated by `formal-gates workflow record-stage`.

The validator blocks these failures:

- no alignment artifact;
- missing required fields;
- placeholder text like `<...>`, `todo`, or `tbd`;
- no stable `RQ-###` IDs;
- declared item count does not match the IDs in the alignment artifact;
- open question IDs are not `none`;
- open blockers are not `none`;
- user confirmation is not `YES`;
- coverage scan is not `PASS`;
- scope preservation check is not `PASS` or `NOT_APPLICABLE: <reason>`;
- task proof check is not `PASS` or `NOT_APPLICABLE: <reason>`;
- dropped question IDs lack explicit approval;
- decision record does not point to a dedicated `.claude/gates/artifacts/requirements-user-decision*.md` or `.claude/gates/artifacts/user-requirements-decision*.md` artifact;
- decision record lacks `Decision record type: USER_CONFIRMATION`, `User confirmation: YES`, `Approved alignment IDs: all` or a `RQ-###` list, and `Approval scope: requirements-clarification-gate`;
- `Covered formal targets` is missing, empty, rooted at the whole repository, absolute, too broad, or uses a wildcard;
- `Dimension coverage:` field is missing or empty;
- `gate_route` is missing, does not match the current workflow/snapshot, or routes somewhere other than proceed.

When a project explicitly enables document-write hook enforcement, the hook blocks writes to requirement documents such as OpenSpec/PRD/SDD/start-readiness/phase Markdown files and formal PRD/requirements/specs `.txt` files when the current worktree has no recorded `requirements-clarification-gate` PASS covering that target path. This opt-in pre-document hard stop is separate from the later post-development gates.

## Recording Command

```bash
bin/formal-gates workflow record-stage \
  --worktree <repo> \
  --gate requirements-clarification-gate \
  --verdict PASS \
  --artifact <clarification-pass-artifact> \
  --actor <main-agent-or-orchestrator> \
  --workflow-id <workflow-id> \
  --change-snapshot <snapshot>
```

## PASS Artifact Required Fields

```text
Requirement source:
Alignment table artifact:
Total alignment items:
Previous alignment artifact: FIRST_RUN / .claude/gates/artifacts/<previous-alignment>.md
Open question IDs: none
Open blockers: none
Dropped question IDs: none / RQ-###
Dropped question approval: none / YES
User confirmation: YES
Coverage scan: PASS
Scope preservation check: PASS / NOT_APPLICABLE: <reason>
Task proof check: PASS / NOT_APPLICABLE: <reason>
Dimension coverage:
  DIM-01 Goal: covered/deferred/NA | RQ-###
  DIM-02 User/value: covered/deferred/NA | RQ-###
  DIM-03 Scope: covered/deferred/NA | RQ-###
  DIM-04 Non-goals: covered/deferred/NA | RQ-###
  DIM-05 Acceptance: covered/deferred/NA | RQ-###
  DIM-06 Evidence: covered/deferred/NA | RQ-###
  DIM-07 Constraints: covered/deferred/NA | RQ-###
  DIM-08 Architecture boundary: covered/deferred/NA | RQ-###
  DIM-09 Requirement details: covered/deferred/NA | RQ-###
  DIM-10 Unknowns: covered/deferred/NA | RQ-###
  DIM-11 Task status: covered/deferred/NA | RQ-###
  DIM-12 Phase dependency: covered/deferred/NA | RQ-###
  DIM-13 Must-not-cut scope: covered/deferred/NA | RQ-###
Decision record: .claude/gates/artifacts/requirements-user-decision.md
Covered formal targets: openspec/changes/<change>/
Downstream permission: READY_TO_DRAFT

gate_route:
  workflow_id: <workflow-id>
  change_snapshot: <snapshot>
  next_action: proceed
  rework_owner: none
  rerun_from: none
```

## Alignment Artifact Required Fields

The alignment artifact path must exist and contain stable `RQ-###` IDs.

```text
ID:
Requirement or question:
Source:
Why it matters:
Status:
User answer:
Downstream effect:
OpenSpec impact:
Evidence needed:
```

`OpenSpec impact:` is a legacy machine-field name kept for artifact schema compatibility. For generic requirement documents, treat it as the format-specific document impact field and record the covered target precisely. Adapter mapping rules live in `references/requirement-document-adapters.md`.

The alignment artifact must not contain placeholders, open items, inferred items, or doc-derived items when recording PASS. If there was an earlier PASS for the same workflow, `Previous alignment artifact` must point to that latest historical alignment artifact unless this is the first run.

## Decision Record Required Fields

```text
Decision record type: USER_CONFIRMATION
User confirmation: YES
User original: <verbatim or faithful user approval text>
Approved alignment IDs: all / RQ-001,RQ-002
Approved alignment artifact: <current alignment artifact when Approved alignment IDs is all>
Approved workflow id: <current workflow id, alternative binding for all>
Approved change snapshot: <current change snapshot, alternative binding for all>
Approved dropped IDs: none / RQ-###
Approval scope: requirements-clarification-gate
```

## Covered Formal Targets Rules

`Covered formal targets` is a comma-separated list of relative file paths or directory prefixes. Do not use `.`, `/`, `*`, an absolute path, or a broad directory that can cover unrelated documents. For an OpenSpec change, prefer `openspec/changes/<change>/`; for a generic document bundle, name the concrete PRD/SDD/issue/design-brief path or bundle directory.

## SKIPPED_BY_USER Rules

`SKIPPED_BY_USER` is not a PASS. If the user explicitly skips clarification, record the skip and risks in narrative output, but do not record a PASS artifact unless the user has also confirmed the remaining alignment table and accepted every listed open risk.
