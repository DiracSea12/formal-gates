# Formal Gates Runtime

## ADDED Requirements

### Requirement: Shell document-write detection is write-first

The hook MUST NOT treat a shell command as a formal document write unless the command contains a supported write-like operation.

#### Scenario: Read-only command mentions GateWorkflow

- **GIVEN** a shell command such as `rg GateWorkflow openspec/changes/example/spec.md`
- **WHEN** the hook evaluates the command
- **THEN** it does not require `GateWorkflow.workflowId`

#### Scenario: Shell write to formal document still requires gate evidence

- **GIVEN** a shell command that writes to `openspec/changes/example/spec.md`
- **WHEN** the hook evaluates the command without matching requirements-clarification evidence
- **THEN** it blocks the write

### Requirement: Structured edit targets come from path fields

For Write/Edit/MultiEdit/NotebookEdit tools, the hook MUST detect formal document targets from explicit path-like fields only.

Content fields such as `content`, `new_string`, and `text` MAY be scanned for `GateWorkflow`, but MUST NOT be used as formal document target discovery.

#### Scenario: Edit prose does not change target detection

- **GIVEN** an Edit tool call with an explicit formal document path
- **WHEN** `new_string` contains ordinary Chinese or English prose, code-like tokens, or field names
- **THEN** the document-write gate behavior depends on the explicit path and workflow evidence, not on those prose tokens

### Requirement: Gate artifacts are read as UTF-8

Gate artifact validation MUST read Markdown, JSON, dispatch prompt, and route artifacts as UTF-8.

#### Scenario: Chinese artifact text does not corrupt field detection

- **GIVEN** a formal gate artifact containing Chinese text before an ASCII field such as `Case-to-artifact binding:`
- **WHEN** artifact validation runs on Chinese Windows
- **THEN** the ASCII field is still detected

### Requirement: Gate state is workflow-route isolated

Gate state verification MUST use entries matching the requested workflow and change snapshot before comparing gate details.

#### Scenario: Stale same-gate entry from another workflow does not block current route

- **GIVEN** a state file has an old `code-quality-gate` entry for workflow A
- **AND** workflow B records a valid `code-quality-gate` artifact
- **WHEN** workflow B records or verifies its gate state
- **THEN** workflow A's stale same-gate record does not cause a workflow or snapshot mismatch for workflow B

### Requirement: Document writes may explicitly use a shared requirements gate

Document-write requirements checks MUST allow an explicit shared requirements-clarification route for the requirements gate only.

The shared route MUST include both workflow and snapshot. It MAY include a worktree or state path for the document repository.

#### Scenario: Child workflow writes document using parent requirements gate

- **GIVEN** a parent workflow has a recorded requirements-clarification PASS covering a formal document target
- **AND** a child implementation workflow writes that covered document
- **WHEN** the tool payload includes explicit `requirementsWorkflowId` and `requirementsChangeSnapshot`
- **THEN** the hook checks the parent requirements route for document-write admission
- **AND** downstream implementation gates still use the child workflow route

### Requirement: Review subagents remain isolated from formal-gates orchestration

Formal review dispatch MUST instruct independent review subagents not to load or execute skills. The formal-gates skill description MUST be scoped to main orchestration and explicit formal workflow requests, not ordinary review-only prompts.

#### Scenario: Review prompt names a gate without activating orchestration

- **GIVEN** a zero-context review dispatch prompt mentions `complexity-gate`
- **WHEN** the reviewer is acting only as an independent gate reviewer
- **THEN** the prompt tells it not to load skills or run the formal-gates orchestration flow

### Requirement: Review contamination guards support English and Chinese labels

Formal review prompt validation MUST reject known anchoring labels in English and Chinese.

#### Scenario: Chinese focus label is blocked

- **GIVEN** a dispatch prompt contains a line-leading Chinese focus or just-fixed label
- **WHEN** a formal gate PASS artifact references that dispatch prompt
- **THEN** validation reports dispatch prompt contamination
