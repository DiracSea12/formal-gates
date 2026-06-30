# Requirements Clarification Gate

Run this when the user asks for formal requirement clarification, pre-development review, start-readiness review, seal/status review against requirements, or a project has explicitly opted into pre-document gate enforcement. Requirement alignment happens before formal drafting inside that chosen flow.

Do not trigger this for ordinary chat, brainstorming, small tasks, wording edits, explanations, casual idea discussion, or normal document work where the user did not ask for formal gates. If intent is unclear, ask whether the user wants formal document work or ordinary discussion.

`requirements-clarification-gate` is a built-in pre-document gate. It is not one of the four post-development review gates. Its PASS evidence is user-confirmed requirement alignment, not an independent zero-context reviewer verdict.

## Lightweight Document Routing

Before editing OpenSpec, PRD, SDD, phase docs, requirements, specs, requirement proposals, development plans, technical plans, implementation plans, handoff documents, or roadmap/milestone sections with concrete scope and acceptance, do a lightweight semantic routing check.

Classify by document role and content, not filename alone. A requirement-like edit is any edit that defines or changes product goal, user value, scope, non-scope, acceptance criteria, architecture boundary, compatibility promise, evidence standard, phase status, development order, dependency, business rule, edge case, data constraint, or requirement detail.

Lightweight routing is not `requirements-clarification-gate`. It must not create PASS records, gate artifacts, reviewer dispatch artifacts, or machine gate state.

Edit classes:

- `NON_SEMANTIC`: typo, formatting, link update, heading numbering, or wording cleanup that does not change meaning. Ask 0 questions, create 0 gate artifacts, and edit directly.
- `LOW_RISK_CLARIFICATION`: clearer wording that restates confirmed requirements without adding or removing meaning. Usually ask 0 questions; ask at most 1 confirmation question if the source is unclear. Create 0 gate artifacts.
- `SEMANTIC_CHANGE`: an edit that changes or may change requirement meaning. Clarify before writing the change as formal requirement text. If the flow becomes formal, record confirmed answers as `RQ-###` alignment.
- `BLOCKED`: missing answers would force guessing about goal, scope, acceptance, architecture boundary, compatibility, evidence, phase status, dependency order, or requirement details. Return `DRAFT_BLOCKED` unless the user asked for exploration/spike work with assumptions clearly marked as pending.

Exploration, spike, and brainstorming drafts may continue without confirmed formal requirements only when assumptions are visibly marked as pending confirmation and are not written as acceptance standards.

## Source Of Truth

Use only the user's requirement brief, explicit user decisions, approved requirement notes, confirmed `RQ-###` items, and user-confirmed answers as requirement truth.

Source hierarchy:

1. current explicit user decisions;
2. approved requirement notes or confirmed `RQ-###` items;
3. current approved and not-deprecated source-of-truth specs or PRDs;
4. ordinary old documents, implementation, tests, validation output, gate artifacts, and agent summaries.

Current approved and not-deprecated source-of-truth specs or PRDs may prove current requirement state. They do not authorize adding, deleting, or changing requirements.

If a requirement document already exists, review it against that source. Do not treat old OpenSpec, PRD, SDD, tasks, commits, gate artifacts, validation reports, implementation, tests, or prior agent summaries as self-confirming requirements.

Long-term memory such as `CONTEXT.md`, ADRs, or `.out-of-scope` files is auxiliary context. Treat it as `doc-derived` unless the user explicitly confirms it. If memory affects scope, acceptance, architecture boundary, compatibility, or development direction, ask for confirmation before turning it into requirement truth.

## Hard Stop Rules

- Before formal drafting or formal review of requirement documents, first produce or obtain a requirement brief with goal, scope, non-goals, acceptance, evidence, constraints, architecture boundary, and requirement details.
- Requirement details include: specific business rules, boundary conditions, exception cases, data constraints, detailed user scenarios, non-functional requirement metrics, and other details that affect requirement understanding and acceptance judgment.
- Before formal judgment of existing document status, redo scope, seal, task status, or phase dependency, build a detail alignment table against the requirement brief.
- Inside a formal flow, do not dispatch complexity, architecture, cold-water, QA, or development agents until the alignment table is user-confirmed.
- User-accepted risk is not a shortcut inside a formal flow. It must be recorded per item as `deferred-by-user` or `out-of-scope-by-user` with explicit approval and still produce a machine-recorded PASS before formal drafting or gates proceed.
- Every alignment item and every open question must have a stable `RQ-###` ID. Do not merge, delete, renumber, or compress open questions after showing them to the user unless the user explicitly approves that exact change.
- Do not replace many open questions with a smaller summary list. Summaries may be added only after the full numbered list. If the list is long, split it into batches, but keep all IDs alive until answered, deferred, or explicitly cut by the user.
- Chat-only answers are not enough for formal PASS. Record each accepted answer in the alignment table before any downstream formal document or gate uses it.
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

Light tasks stay `DISCUSSION_ONLY` unless the user explicitly says to run formal gates or the project has explicitly opted into pre-document gate enforcement.

## Alignment Table

Before any formal draft, review, gate, QA, or development handoff, produce an alignment table with these fields:

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
- For current PASS artifact schema compatibility, the machine alignment artifact still accepts the legacy field name `OpenSpec impact:`. Use `Document impact:` in narrative alignment, and map it through `references/requirement-document-adapters.md` when recording format-specific artifacts.

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

Question budget is risk-based:

- Non-semantic edit: 0 questions.
- Low-risk clarification with confirmed source: 0 questions; at most 1 confirmation if the source is unclear.
- Ordinary semantic change: usually 1-3 high-impact questions.
- Complex requirement or development plan: up to 5 questions per round.
- Additional rounds are allowed only when unresolved answers still affect goal, scope, acceptance, architecture boundary, compatibility, evidence, phase state, or dependency order.

Each question should include a stable `RQ-###` ID when it may enter formal alignment, the question, a recommended answer, why the answer matters, and concrete choices when useful.

Default to one question at a time. Closely related questions may be asked in a batch of 2-5. Do not dump a broad questionnaire.

Stop asking when critical ambiguity is resolved, remaining questions are low impact, the user stops or defers them, or the flow must return `DRAFT_BLOCKED`.

Ask 1-5 high-quality questions per round in formal clarification. Do not ask 10+ questions at once unless they are tightly coupled. After the user answers, synthesize and ask follow-ups only when needed.

## File Budget

Lightweight routing and informal clarification must not create gate artifacts.

Do not create one file per question, one file per clarification round, default issue tracker files, or `.scratch` output by default.

Formal requirements PASS may create only the required alignment artifact, user decision record, and normal gate state.

After a semantic clarification is confirmed outside formal PASS recording, leave a minimal source trace in the target document instead of creating a separate gate artifact by default. Examples: an `RQ-###` ID, confirmation date, `Clarifications` section, or change note.

For requirements that affect acceptance or broad scope, keep lightweight traceability from requirement ID to affected document or module to observable acceptance condition or evidence entrypoint.

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

PASS must be recorded through `formal-gates workflow record-stage` and validated by the native artifact checks; a chat-only claim is not enough.

For PASS artifact fields, decision record fields, validator blockers, recording commands, and covered-target rules, read `references/requirements-clarification-artifacts.md` only when recording PASS or diagnosing artifact validation.
