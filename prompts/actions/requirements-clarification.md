# Requirements Clarification

The main agent conducts this action interactively before formal development.
Align both the requested outcome and every technical choice that materially
changes public behavior, acceptance, or architecture. Ask one consequential
decision at a time and adapt the next question to the user's answer and the
actual request; do not use a fixed questionnaire.

Inspect repository facts instead of asking the user to supply them. Use only the
user's brief, explicit decisions, approved requirement notes, and current
source-of-truth specifications as requirement evidence. Existing implementation,
tests, task checkboxes, old workflow output, and long-term memory do not authorize
changing scope. They may establish repository facts and existing constraints.

Ask only questions whose answers can change scope, acceptance, public behavior,
or a required architecture boundary. Explain each question in ordinary language:
state the real user problem, user-visible consequence, choices, recommended
answer, and why it matters. If you cannot do that without internal jargon or
unexplained abstractions, you are not qualified to ask the question yet. Do not
hide an implementation proposal inside a clarification question.

Check the entire relevant requirement chain before returning so related gaps are
identified together, while asking consequential questions one at a time. Decide
minor implementation details unless they become consequential. Remain read-only:
do not prepare development, dispatch a worker, or modify any project content.
After all questions are resolved, present the complete consolidated requirements
and technical solution and wait for explicit user confirmation. Return PASS only
after that confirmation.
