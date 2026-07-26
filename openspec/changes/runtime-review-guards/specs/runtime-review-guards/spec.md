## ADDED Requirements

### Requirement: Review results bind to one fresh independent dispatch

QA Review and every post-development gate review SHALL have one unique prepared
dispatch and one previously unused reviewer/session identity. The dispatch
SHALL bind the exact prompt, review wave and attempt, current requirement and
catalog revisions, and current native VCS identity.

#### Scenario: A gate result matches its preparation

- **WHEN** the host records a result with the open dispatch ID and a fresh
  reviewer identity
- **THEN** the CLI obtains every static source binding from that dispatch and
  records the semantic result

#### Scenario: A reviewer is reused

- **WHEN** a QA Review or gate result uses a reviewer/session identity already
  used by another review in the run
- **THEN** the CLI rejects the result without changing semantic state

#### Scenario: A review retries

- **WHEN** an interrupted or runtime-error review is prepared again
- **THEN** the CLI creates a new dispatch and requires a fresh reviewer identity

### Requirement: The selected native VCS supplies immutable identities

The main agent SHALL select the actual VCS once. The CLI SHALL resolve and
verify workflow identities through the selected Git, SVN, or P4 native command
without handwritten repeated snapshot fields, silent VCS switching, guessed
identities, retained diffs, or a fallback version-control engine.

#### Scenario: A supported VCS resolves the live identity

- **WHEN** a workflow command needs the current immutable identity
- **THEN** the CLI invokes the run's selected native VCS and uses the returned
  identity in the existing transition and dispatch binding

#### Scenario: Native identity resolution fails

- **WHEN** the selected VCS cannot return or verify the required identity
- **THEN** the command fails without mutating workflow state

### Requirement: Requirement artifacts freeze before development

The workflow SHALL register and hash the complete format-neutral requirement
and solution document set, freeze it at first development preparation, and
reject ordinary post-development work when any frozen file changes.

#### Scenario: Post-development review reads frozen requirements

- **WHEN** QA or a reviewer is prepared after development
- **THEN** it receives the frozen documents as acceptance input
- **AND** their paths are excluded from the VCS review targets

#### Scenario: A frozen requirement document changes

- **WHEN** any registered artifact changes after development starts
- **THEN** ordinary development, repair, QA, review, Carry, and Seal stop
- **AND** the run returns to requirement clarification or requirement change
  before a new development boundary can be established

### Requirement: QA combines static checks and real execution

Every complete formal QA case set SHALL contain `STATIC` and `LIVE` cases.
Static cases SHALL exercise fast direct-owner checks. Live cases SHALL actually
execute documented public entrypoints against the built current snapshot in an
isolated normal-use environment and compare observable results with an oracle.

#### Scenario: QA Design omits one kind

- **WHEN** the candidate set contains only static cases or only live cases
- **THEN** it cannot be approved for development

#### Scenario: QA Execution completes

- **WHEN** QA Execution reports the approved set
- **THEN** every static and live case has an actual procedure, observation, and
  oracle comparison
- **AND** code inspection, simulated output, or developer self-test claims do
  not substitute for live execution

