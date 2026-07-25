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

#### Scenario: Invalid catalog is queried

- **WHEN** the package has a catalog error already rejected by
  `LoadPromptCatalog`
- **THEN** the query returns that validation error instead of a partial list or
  a separately discovered result
