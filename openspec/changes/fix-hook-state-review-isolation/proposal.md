# Fix Hook State Review Isolation

## Why

Recent downstream runs found several formal-gates regressions that undermine daily use and zero-context review trust:

- formal document write detection can treat read-only shell commands as writes;
- structured Edit-style tools can let edited content influence target detection;
- artifact validation can misread UTF-8 gate artifacts on Chinese Windows;
- workflow state checks can confuse old gate entries across workflows;
- document-write requirements checks cannot intentionally use a shared parent requirements gate;
- review subagents can be polluted when the formal-gates skill auto-loads in a review-only context.

These are correctness bugs in the existing gate workflow. The fix must stay small and must not become a hook runtime, installer framework, state platform, or localization system.

## Goal

Make formal-gates safer and more predictable across Chinese and English usage while preserving the lightweight skill-package shape.

## Scope

- Ensure shell document-write gating only triggers on actual write-like shell operations.
- Ensure Write/Edit/MultiEdit/NotebookEdit target detection uses explicit path fields, while edited content is used only to extract `GateWorkflow`.
- Ensure artifact, prompt, attempt, and route validation reads UTF-8 content explicitly.
- Ensure gate state lookup and verification use matching workflow routes and do not let stale records for another workflow block the current workflow.
- Add a narrow way for a document write to reference an already-recorded shared requirements-clarification gate.
- Reduce formal-gates skill auto-trigger pollution in review subagents and require review dispatch prompts to explicitly disable skill loading.
- Support both English and Chinese wording for review-contamination guards and user-facing gate guidance where this workflow already accepts natural language.

## Non-goals

- Do not build a general shell parser.
- Do not build a unified hook runtime.
- Do not add a host registry, report/cache/state platform, daemon, service, or installer verifier.
- Do not turn shared requirements gates into a generic workflow inheritance system.
- Do not add a full localization framework or translated copies of every reference document.
- Do not weaken the four fixed gate names or zero-context evidence requirements.

## Acceptance

- Read-only shell commands such as `rg`, `grep`, `cat`, and `echo` that mention formal document paths or `GateWorkflow` are not blocked as document writes.
- Shell commands that clearly write formal documents, including redirection, PowerShell content writes, supported file APIs, and copy-like commands to formal document targets, still require requirements-clarification evidence.
- Edit-style tool target detection does not depend on `new_string`, `content`, or prose text except for extracting a content-carried `GateWorkflow`.
- Gate artifact validation reads UTF-8 consistently and accepts Chinese text without corrupting later ASCII field detection.
- Recording or verifying one workflow does not fail because another workflow has a stale same-gate record.
- A document write can explicitly point to a shared requirements-clarification workflow/snapshot without changing the current implementation workflow identity.
- Formal review dispatch guidance includes an explicit no-skill-loading instruction for review subagents.
- The formal-gates skill description no longer auto-triggers simply because a subagent prompt mentions OpenSpec, code review, or a gate name.
- English and Chinese contamination labels are both covered by validation or prompt rules where applicable.
