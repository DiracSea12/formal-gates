## ADDED Requirements

### Requirement: Gate files are the sole independent-review gate source

The implementation SHALL discover every valid direct regular child of the
installed package's `gates/`, use its filename stem as the gate ID, and execute
the discovered set in deterministic lexical order. A filename SHALL match
`[a-z0-9]+(?:-[a-z0-9]+)*.md`. Its content SHALL be valid UTF-8 and non-empty
after trimming whitespace. No Markdown syntax parser is required. Project
worktrees SHALL NOT provide a second discovery root or overlay.

#### Scenario: Adding a gate file

- **WHEN** a new valid Markdown file is added to `gates/`
- **THEN** the next installed package discovers and includes it without a Go
  gate registry, gate manifest, or additional registration change

#### Scenario: Invalid gate file

- **WHEN** a gate file is empty, unreadable, invalid UTF-8, not a direct regular
  child, or has an invalid ID
- **THEN** discovery fails before any reviewer dispatch or seal aggregation

### Requirement: Shared base prompt is mandatory and cannot be bypassed

The CLI side of GateRunner SHALL load `prompts/reviewer-base.md` for every
independent gate, append exactly one selected gate prompt, then append current
requirement and VCS routing plus the result contract. It SHALL return that one
deterministic complete task payload to the host skill. The host SHALL start the
independent agent through its native task channel with those exact bytes and
return only the semantic gate result. Missing, empty, or invalid base prompt
SHALL stop before the host starts an agent. No caller override SHALL replace or
omit the base. Go SHALL NOT embed a host provider SDK or generic agent runtime.

#### Scenario: Normal assembly

- **WHEN** GateRunner dispatches a discovered gate
- **THEN** the captured task payload contains the base once, the gate prompt
  once, and no prior finding, repair explanation, or target verdict

#### Scenario: Missing base

- **WHEN** `prompts/reviewer-base.md` is absent or empty
- **THEN** the runner returns a configuration error and makes no agent call

### Requirement: Seal records any selected review combination

The final aggregator SHALL NOT require QA Execution, start-readiness, or any
discovered gate to have run or passed. It SHALL seal with any combination of
`PENDING`, `PASS`, `FAIL`, and `RUNTIME_ERROR`, including a run with no executed
reviews, while preserving existing QA and gate statuses in the retained
summary. Requirements confirmation and the installed prompt/catalog revision
SHALL remain current, and the supplied native VCS identities SHALL match the
run's current snapshot. Reviewer FAIL and runtime error SHALL remain distinct;
a runtime error SHALL NOT be represented as a reviewer finding.

#### Scenario: No reviews were executed

- **WHEN** an active run has confirmed current definitions and matching live VCS
  identities but QA, readiness, and every gate remain `PENDING`
- **THEN** seal succeeds, retains the pending statuses, and cleans the temporary
  run directory

#### Scenario: Selected reviews did not pass

- **WHEN** an active run contains any mixture of PASS, FAIL, RUNTIME_ERROR, and
  PENDING review results
- **THEN** seal succeeds and retains those distinct statuses without converting
  or suppressing them

### Requirement: One minimal run state supports interruption and repair

The workflow SHALL atomically replace one temporary run-state file while
holding the existing cross-process lock across reload, terminal-state check,
mutation, and replacement. It SHALL contain the requirement revision,
base/catalog revision, VCS identities, named action records, QA cases and
execution, discovered gate results, immediate Carry decisions, and lifecycle
status defined by this change. Dispatch SHALL NOT persist `RUNNING`; an
interrupted dispatch remains `PENDING`. Temporary state SHALL be cleaned only after a
successful retained seal summary or explicit abort summary is written.

#### Scenario: Resume after interrupted reviewer

- **WHEN** a host is interrupted before recording a dispatched gate result
- **THEN** that gate remains `PENDING`, other completed action results are
  preserved, and the operator may dispatch the affected gate again

#### Scenario: Repair after gate PASS

- **WHEN** the current VCS snapshot changes after one or more gate PASS results
- **THEN** QA Execution is cleared and the Carry action records `INHERIT` or
  `RERUN` for every prior passing AI gate using the immediate native VCS
  comparison

#### Scenario: Parallel results complete together

- **WHEN** two normally parallel actions submit results concurrently
- **THEN** the CLI serializes the complete state transactions and preserves
  both results without preventing the agents themselves from running in parallel

#### Scenario: Requirement or installed prompts change

- **WHEN** the requirement content revision changes during an active run
- **THEN** all requirement-dependent semantic results become pending
- **AND WHEN** the installed base or gate catalog revision changes
- **THEN** resume and seal reject the mixed-definition run and require restart

### Requirement: Native VCS remains the only diff implementation

The workflow SHALL require a named VCS and caller-provided comparable base,
current, and when applicable pre-repair snapshot identities. The host and
reviewer SHALL invoke the native VCS directly. Every delivery path SHALL be
tracked before its snapshot is fixed. formal-gates SHALL NOT parse diff bytes,
store project content, scan untracked files, implement VCS adapters, or support
a no-VCS fallback.

#### Scenario: Newly created delivery file

- **WHEN** a worker creates a delivery file during documented development
- **THEN** the worker adds that explicit path to the named VCS before further
  edits and the native base-to-current comparison includes it

#### Scenario: Native comparison is unavailable

- **WHEN** the named VCS cannot compare the supplied immutable identities
- **THEN** the affected action returns `RUNTIME_ERROR`, which is retained without
  blocking seal

#### Scenario: Seal observes a different live identity

- **WHEN** the host-supplied native identity before or after aggregation differs
  from the run's current snapshot
- **THEN** the CLI rejects seal without accepting stale gate or QA results

### Requirement: Carry understands the dynamic gate catalog

The lightweight Carry action SHALL use its maintained action prompt, the
current installed catalog revision and gate definitions, and the immediate
pre-repair-to-current VCS routing. It SHALL return exactly one `INHERIT` or
`RERUN` decision for every prior passing discovered gate. Every fresh or rerun
gate SHALL independently inspect the complete base-to-current comparison.

#### Scenario: Custom gate exists during repair

- **WHEN** a valid custom discovered gate has a prior PASS and a repair changes
  the current snapshot
- **THEN** Carry reads that gate's current definition and returns a decision
  before the prior result can be recorded as inherited on the new snapshot
