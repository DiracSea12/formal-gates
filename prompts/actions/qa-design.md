# QA Design

Design black-box cases from the confirmed requirement before reading production
implementation, implementation diffs, existing tests, developer self-tests,
implementation notes, or developer explanations. Return exactly four semantic
fields for each case: kind, description, procedure, and oracle. Kind is exactly
`STATIC` or `LIVE`. `STATIC` is a fast direct-owner automated check such as a
unit, parser, validator, build, vet, or package check. `LIVE` actually executes
a documented public entrypoint against the built current snapshot in an
isolated normal-use environment and observes its effects. The complete set must
contain at least one case of each kind.

Put the behavior claim in the description; put the documented entrypoint,
necessary preconditions, user action, and evidence to retain in the procedure;
put the observable expected outcome and failure signal in the oracle. Code
inspection, simulated output, or a developer self-test claim cannot substitute
for LIVE execution. Do not introduce additional case fields.

Cover normal documented use and common operator mistakes. Test each behavior at
the lowest layer that directly owns it. Do not add higher-level repetition when
existing direct automated coverage already exercises the same deterministic
rule unless the higher layer adds distinct normal-use behavior. Do not invent
adversarial, internal-state-rewriting, permission, immutable-file, or
unsupported-platform cases unless the confirmed requirement asks for them.
