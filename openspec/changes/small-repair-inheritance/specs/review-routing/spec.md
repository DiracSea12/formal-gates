## ADDED Requirements

### Requirement: Small repairs may inherit all prior PASS results

The workflow SHALL allow the main agent to skip the independent Carry dispatch
after inspecting the immediate native repair diff when it can establish that
every prior PASS result is unaffected. The shortcut SHALL reuse the existing
Carry transition, record its reason and origin, inherit prior PASS QA and
discovered gates, and leave only non-passing results for rerun.

#### Scenario: Bounded repair affects only a failed result

- **WHEN** the main agent establishes from the immediate repair diff that no
  prior PASS verification can be affected
- **THEN** the main-agent shortcut rebinds every prior PASS QA and gate result
  to the repair snapshot
- **AND** only FAIL, RUNTIME_ERROR, and PENDING selected results remain to run

#### Scenario: One prior PASS may be affected

- **WHEN** any causal impact on a prior PASS is possible or uncertain
- **THEN** the shortcut is not used and independent Carry decides each eligible
  discovered gate while QA follows the normal rerun path

#### Scenario: Shortcut reason is missing

- **WHEN** an operator invokes the main-agent shortcut without a reason
- **THEN** the CLI rejects it without mutating workflow state
