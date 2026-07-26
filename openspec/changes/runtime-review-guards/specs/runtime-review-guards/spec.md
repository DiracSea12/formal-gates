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

#### Scenario: An interrupted reviewer returned no result

- **WHEN** the host claimed a reviewer identity for a dispatch that was later
  interrupted before result recording
- **THEN** a later QA Review or gate dispatch cannot claim that identity

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

### Requirement: QA Review preserves unchanged case approvals

QA Review SHALL return one outcome for every case assigned in its current
dispatch. The CLI SHALL store those outcomes on the existing QA cases and SHALL
derive the aggregate QA Review result without direct reviewer edits to workflow
state or a separate case file.

#### Scenario: Some cases fail review

- **WHEN** one QA Review attempt passes some assigned cases and fails others
- **THEN** the CLI retains PASS on the passing cases and returns the candidate
  set to QA Design with the failed cases and findings
- **AND** the next QA Review requests decisions only for failed, new, or changed
  cases

#### Scenario: QA Design preserves a passing case

- **WHEN** QA Design returns a case whose kind, description, procedure, and
  oracle exactly match a previously passing case retained for an unaffected
  requirement
- **THEN** the CLI preserves that case's PASS marker

#### Scenario: QA Design changes a passing case

- **WHEN** any semantic field of a passing case changes
- **THEN** the resulting case is pending and must be reviewed again

#### Scenario: Review finds missing coverage

- **WHEN** the reviewer passes every assigned case but reports a set-level
  missing-coverage finding
- **THEN** QA Review remains failed until QA Design adds or revises the needed
  case and that pending case passes review

#### Scenario: Requirements change incrementally

- **WHEN** a requirement revision adds, changes, or removes scope and QA Design
  returns the revised complete candidate set
- **THEN** exact retained cases for unaffected requirements keep PASS
- **AND** new or changed cases are pending while omitted obsolete cases are
  removed
