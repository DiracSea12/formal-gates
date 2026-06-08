# Design

## Small Fix Strategy

This change fixes specific regressions in the existing PowerShell hook and gate scripts. It should reuse the current files:

- `hooks/enforce-gate-sequence.ps1`
- `scripts/gate-artifact-validation.ps1`
- `scripts/gate-state.ps1`
- `scripts/gate-workflow.ps1`
- `SKILL.md`
- `agents/*.md`
- `scripts/test-portable-openspec-canary.ps1`

No new subsystem is needed.

## Shell Document-Write Detection

Shell tools must be classified in two steps:

1. decide whether the command contains a supported write-like operation;
2. only then extract formal document targets from the write operation.

Read-only commands must not be blocked just because they mention `GateWorkflow`, `.md`, `openspec`, or a formal path.

Supported write-like shell operations remain narrow: redirection, PowerShell content writes, known file-write APIs, and explicit copy/move operations. Copy-like operations should be treated as writes only when the destination can be parsed as a formal document target.

## Structured Tool Target Detection

For Write/Edit/MultiEdit/NotebookEdit, formal document targets must come from explicit path-like fields, not from `content`, `new_string`, or generic text.

The content fields may still be scanned for `GateWorkflow={...}` so users can attach workflow metadata to a document write payload. That scan must not turn arbitrary prose into a target path or command intent.

## UTF-8 Validation

All validation reads for Markdown, JSON, dispatch prompts, and gate artifacts must use `-Encoding UTF8`. This matches existing writers and avoids GBK misread on Chinese Windows.

## Workflow Route Isolation

The state file may keep its current compact top-level `gates` map, but validation must select matching entries from history by workflow and snapshot before comparing gate details.

Recording a gate for workflow B must not compare workflow B's artifact route against workflow A's stale same-gate entry. Verification must fail only when the matching route is missing, stale, or invalid.

## Shared Requirements Gate For Document Writes

Document writes sometimes update a docs repository while implementation work uses another workflow or snapshot. The hook needs a narrow override for the requirements-clarification lookup only.

The `GateWorkflow` object may include a nested shared requirements reference:

```json
{
  "workflowId": "current-implementation-workflow",
  "changeSnapshot": "current-implementation-snapshot",
  "requirementsWorkflowId": "parent-requirements-workflow",
  "requirementsChangeSnapshot": "parent-doc-snapshot",
  "requirementsWorktree": "path-to-doc-repo"
}
```

Only the document-write requirements check may use these `requirements*` fields. Downstream QA, complexity, architecture, and code-quality admission must keep using the current workflow fields.

## Review Subagent Isolation

The frontmatter `description` should describe formal-gates as a main-orchestrator workflow skill. It must not invite auto-loading for ordinary review prompts just because the prompt mentions OpenSpec, code review, or a gate name.

Each review agent prompt should state that the reviewer is not the formal-gates orchestrator and must not load or execute skills. The dispatch guidance in `SKILL.md` should require this no-skill-loading line in zero-context review prompts.

## Bilingual Support

This change provides lightweight Chinese and English support:

- existing English contamination labels remain blocked;
- Chinese focus/recheck/just-fixed labels remain blocked;
- review-subagent isolation wording appears in English with a concise Chinese equivalent where users and agents most often see it;
- error and documentation wording should be readable in both Chinese and English where this change touches user-facing instructions.

This is not a full translation framework.

## Risks

- Over-broad shell detection could keep blocking read-only commands. Tests must include read-only probes.
- Over-loose shared requirements matching could let stale parent gates unlock unrelated document writes. The override must remain explicit.
- Skill trigger wording that is too narrow could make main agents forget formal-gates. The description should still trigger on explicit formal-gates orchestration requests.
