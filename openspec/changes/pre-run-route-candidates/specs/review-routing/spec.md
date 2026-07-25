## ADDED Requirements

### Requirement: Route candidates are available before a run

The package SHALL expose a read-only route-candidate query before a workflow
run exists. It SHALL return built-in QA followed by every dynamically
discovered gate and SHALL reuse the existing prompt catalog as the only gate
discovery and ordering owner.

#### Scenario: Valid package is queried before workflow start

- **WHEN** an operator runs `formal-gates package route-candidates --root
  <package>` against a valid installed package
- **THEN** stdout is a JSON array containing `qa` first and every discovered
  gate ID afterward in lexical order
- **AND** no workflow state or other file is created

#### Scenario: Package has no dynamic gates

- **WHEN** a valid package contains no direct gate Markdown files
- **THEN** the shared prompt catalog remains valid
- **AND** the query returns `["qa"]`

#### Scenario: Unrelated gate-directory entries are present

- **WHEN** `gates/` contains ordinary subdirectories or non-Markdown files in
  addition to direct gate Markdown files
- **THEN** the unrelated entries are ignored
- **AND** only the direct gate Markdown files appear after `qa`

#### Scenario: Gate-like entry is not a regular file

- **WHEN** a direct entry in `gates/` has a `.md` name but is not a regular file
- **THEN** the query returns the shared direct-file validation error

#### Scenario: Invalid catalog is queried

- **WHEN** the package has a catalog error already rejected by
  `LoadPromptCatalog`
- **THEN** the query returns that validation error instead of a partial list or
  a separately discovered result
