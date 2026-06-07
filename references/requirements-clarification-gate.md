# Requirements Clarification Gate

Run this before writing or modifying requirement documents such as OpenSpec, PRD, SDD, phase documents, issue briefs, design briefs, or start-readiness material. Requirement alignment happens before drafting. Also run it when the user explicitly asks for formal requirement clarification, or when existing document status, seal, redo scope, task status, or phase dependency must be checked against the original requirements.

Do not trigger this for ordinary chat, brainstorming, small tasks, wording edits, explanations, or casual idea discussion that is not entering document work. If intent is unclear, ask whether the user wants formal document work or ordinary discussion.

`requirements-clarification-gate` is a built-in pre-document gate. It is not one of the four post-development review gates. Its PASS evidence is user-confirmed requirement alignment, not an independent zero-context reviewer verdict.

## Source Of Truth

Use only the user's requirement brief, explicit user decisions, approved requirement notes, and user-confirmed answers as requirement truth.

If a requirement document already exists, review it against that source. Do not treat OpenSpec, PRD, SDD, tasks, commits, gate artifacts, validation reports, implementation, or prior agent summaries as self-confirming requirements.

## Hard Stop Rules

- Before drafting or changing requirement documents, first produce or obtain a requirement brief with goal, scope, non-goals, acceptance, evidence, constraints, architecture boundary, and requirement details.
- Requirement details include: specific business rules, boundary conditions, exception cases, data constraints, detailed user scenarios, non-functional requirement metrics, and other details that affect requirement understanding and acceptance judgment.
- Before judging existing document status, redo scope, seal, task status, or phase dependency, build a detail alignment table against the requirement brief.
- Do not dispatch complexity, architecture, cold-water, QA, or development agents until the alignment table is user-confirmed.
- User-accepted risk is not a shortcut. It must be recorded per item as `deferred-by-user` or `out-of-scope-by-user` with explicit approval and still produce a machine-recorded PASS before drafting or gates proceed.
- Every alignment item and every open question must have a stable `RQ-###` ID. Do not merge, delete, renumber, or compress open questions after showing them to the user unless the user explicitly approves that exact change.
- Do not replace many open questions with a smaller summary list. Summaries may be added only after the full numbered list. If the list is long, split it into batches, but keep all IDs alive until answered, deferred, or explicitly cut by the user.
- Chat-only answers are not enough. Record each accepted answer in the alignment table before any downstream document or gate uses it.
- `confirmed` means user-confirmed or approved requirement-note-confirmed. Agent summaries are `doc-derived` or `inferred`, not confirmed.
- If an inferred or doc-derived item can change scope, acceptance, task status, architecture boundary, evidence, or phase dependency, it blocks drafting and gates until confirmed or explicitly deferred by the user.
- Task checkboxes are not proof. A checked task must trace to current evidence. If evidence was reverted, invalidated, or never confirmed, mark the task as `needs re-proof` or leave it unchecked.
- If a draft already exists, the draft is the object being checked. It cannot be used as its own proof.

## Mode Check

Classify before asking questions:

- `DISCUSSION_ONLY`: The user is exploring an idea. Do not write formal docs or gate verdicts.
- `CLARIFY_FOR_DOC`: The user wants to write or modify a document and requirements need alignment.
- `READY_TO_DRAFT`: Core answers are known; write the document as draft/unsealed if only minor non-blocking gaps remain.
- `BLOCKED`: Missing answers would make scope, acceptance, evidence, task status, phase dependency, or architecture boundary guesswork.

Light tasks stay `DISCUSSION_ONLY` unless they write or modify requirement documents or the user explicitly says to run formal gates.

## Alignment Table

Before any draft, review, gate, QA, or development handoff, produce an alignment table with these fields:

```text
ID:
Requirement or question:
Source:
Why it matters:
Status: confirmed / doc-derived / inferred / open / deferred-by-user / out-of-scope-by-user
User answer:
Downstream effect:
Document impact:
Evidence needed:
```

Rules:

- Use stable IDs in the form `RQ-001`, `RQ-002`, and so on. Do not renumber old IDs after the user has seen them.
- `confirmed`: explicit user answer or approved requirement note.
- `doc-derived`: extracted from a requirement brief, but not directly confirmed in this run.
- `inferred`: agent guess. Cannot unlock drafting or gates when it affects scope, acceptance, evidence, task status, architecture, or phase dependency.
- `open`: blocks drafting and gates when it can change scope, acceptance, evidence, task status, architecture, or phase dependency.
- `deferred-by-user`: include the risk accepted by the user and the later step that must revisit it.
- `out-of-scope-by-user`: preserve the original requirement text and the user's explicit cut decision.
- `deferred-by-user` and `out-of-scope-by-user` require per-item user approval evidence in the alignment item itself.
- If a previous alignment artifact exists, compare ID sets before recording PASS. Any missing old ID must appear under `Dropped question IDs` and have explicit user approval in the decision record or in a per-ID user quote field.
- For current PowerShell PASS artifact compatibility, the machine alignment artifact still accepts the legacy field name `OpenSpec impact:`. Use `Document impact:` in narrative alignment, and map it through `references/requirement-document-adapters.md` when recording format-specific artifacts.

## Question Quality Standard

A high-quality clarification question must:

1. Eliminate work branches: the answer changes what gets built, how it is validated, or what counts as success.
2. Be falsifiable: the answer is concrete and verifiable.
3. Block wrong-direction risk: leaving it unanswered would let the agent proceed in a direction the user may not intend.

Good questions:

- "Is this the whole phase or one implementation slice?"
- "Does backward compatibility with version 1.x need to be preserved?"
- "What evidence would convince you this is done?"
- "If scope must be cut, what cannot be cut?"

Weak questions do not count toward coverage unless they affect acceptance, constraints, or architecture boundary:

- "What color should the button be?"
- "Would you like detailed logging?"
- "Do you prefer approach A or B?"

Ask 1-5 high-quality questions per round. Do not ask 10+ questions at once unless they are tightly coupled. After the user answers, synthesize and ask follow-ups if needed.

## Completeness And Conflict Scan

Before `READY_TO_DRAFT`, scan the confirmed alignment items for conflicts and missing essentials. Keep this as a lightweight check, not a new framework.

- Acceptance must be testable. Use a concrete condition/result form such as `given / when / then` when it helps, but do not force that format for every requirement.
- Check for conflicts between confirmed requirements, constraints, and evidence. Examples: "real-time" versus "low CPU", "backward compatible" versus "delete old path", or "offline" versus "cloud-only evidence".
- When relevant, make sure the alignment covers inputs, outputs, error cases, security or performance constraints, external dependencies, and assumptions.
- **Requirement detail alignment**: When the requirement involves non-trivial scope, check that key requirement details are aligned—specific business rules, boundary conditions, exception cases, data constraints, detailed scenarios. Missing detail alignment on questions that would change requirement understanding, acceptance outcomes, or verification approach must block drafting.
- Unresolved conflicts or missing essentials stay in `Blocking gaps`; do not hide them as inferred defaults.

## Stop Rules

Return `DRAFT_BLOCKED` when any missing answer would make the document guess:

- product goal
- acceptance standard
- scope or non-goal
- architecture boundary
- evidence requirement
- unresolved requirement conflict
- compatibility promise
- task completion status
- phase dependency
- requirement details that would change requirement understanding, acceptance outcomes, or verification approach

If the user asks to skip clarification, record `SKIPPED_BY_USER` and list the risk. This does not unlock document writes, gate dispatch, QA, or development. Do not silently fill defaults.

## Output

```text
Requirements Clarification Gate
Mode: DISCUSSION_ONLY / CLARIFY_FOR_DOC / READY_TO_DRAFT / BLOCKED
Verdict: READY_FOR_DRAFT / DRAFT_BLOCKED / SKIPPED_BY_USER / DISCUSSION_ONLY
Confirmed answers:
Alignment table:
Scope preservation check:
Task proof check:
Open questions:
Blocking gaps:
Assumptions not allowed:
Draft status: none / draft-only / ready-to-draft
Next action:
```

`READY_FOR_DRAFT` is not development approval. It only means the writing workflow may start.

## Machine Recording

PASS must be recorded through `gate-workflow.ps1` and validated by `gate-artifact-validation.ps1`; a chat-only claim is not enough.

For PASS artifact fields, decision record fields, validator blockers, recording commands, and covered-target rules, read `references/requirements-clarification-artifacts.md` only when recording PASS or diagnosing artifact validation.
