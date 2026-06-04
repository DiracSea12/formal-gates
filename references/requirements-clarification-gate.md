# Requirements Clarification Gate

Use before writing or modifying OpenSpec, PRD, SDD, phase docs, or start-readiness material. Also use when the user explicitly asks for formal requirement clarification.

Do not trigger this for ordinary chat, brainstorming, small tasks, wording edits, explanations, or casual idea discussion that is not entering document work. If unclear, ask whether the user wants document work or ordinary discussion.

## Pattern

Borrow the useful parts, not the weight: Spec Kit's clarify/checklist before planning, BMAD's idea-to-brief routing, and OpenSpec's what/why before implementation details.

## Mode Check

Classify before asking questions:
- `DISCUSSION_ONLY`: user is exploring an idea. Do not write formal docs or gate verdicts.
- `CLARIFY_FOR_DOC`: user wants to write/modify a document and requirements need alignment.
- `READY_TO_DRAFT`: core answers are known; write the document as draft/unsealed if minor gaps remain.
- `BLOCKED`: missing answers would make scope, acceptance, or architecture guesswork.
Light tasks stay `DISCUSSION_ONLY` unless they write/modify OpenSpec/PRD/SDD/start-readiness material or the user explicitly says to run formal gates.

## Questions To Cover

Ask focused questions until wrong-direction risk is low. Do not spam filler questions.

Required coverage:
- Goal: what outcome must change?
- User/value: who benefits, and why now?
- Scope: whole phase, feature, bugfix, or slice?
- Non-goals: what must not be included?
- Acceptance: how will success be judged?
- Evidence: what proof is required, especially for user-visible behavior?
- Constraints: platform, compatibility, performance, security, schedule, or tooling limits.
- Architecture boundary: which layer/module owns the behavior?
- Unknowns: what is still uncertain enough to block drafting?

Useful prompts:
- "Is this the whole phase or one implementation slice?"
- "Which behavior is explicitly out of scope?"
- "What evidence would convince you this is done?"
- "If we must cut scope, what cannot be cut?"

## Stop Rules

Return `DRAFT_BLOCKED` when any answer is missing and would make the document guess:
- product goal
- acceptance standard
- scope/non-goal
- architecture boundary
- evidence requirement
- compatibility promise

If the user asks to skip clarification, record `SKIPPED_BY_USER` and list the risk. Do not silently fill defaults.

## Output

```text
Requirements Clarification Gate
Mode: DISCUSSION_ONLY / CLARIFY_FOR_DOC / READY_TO_DRAFT / BLOCKED
Verdict: READY_FOR_DRAFT / DRAFT_BLOCKED / SKIPPED_BY_USER / DISCUSSION_ONLY
Confirmed answers:
Open questions:
Blocking gaps:
Assumptions not allowed:
Draft status: none / draft-only / ready-to-draft
Next action:
```

`READY_FOR_DRAFT` is not development approval. It only means the writing workflow may start.
