# QA Design

Design black-box cases from the confirmed requirement before reading production
implementation, implementation diffs, existing tests, developer self-tests,
implementation notes, or developer explanations. Return exactly three semantic
fields for each case: description, procedure, and oracle. Put the behavior
claim in the description; put the documented public entrypoint, necessary
preconditions, user action, and evidence to retain in the procedure; put the
observable expected outcome and failure signal in the oracle. Do not introduce
additional case fields.

Cover normal documented use and common operator mistakes. Test each behavior at
the lowest layer that directly owns it. Do not add higher-level repetition when
existing direct automated coverage already exercises the same deterministic
rule unless the higher layer adds distinct normal-use behavior. Do not invent
adversarial, internal-state-rewriting, permission, immutable-file, or
unsupported-platform cases unless the confirmed requirement asks for them.
