# Public Release Readiness

## ADDED Requirements

### Requirement: Public release package metadata and documentation shall not overstate host support

The public release package MUST include repository hygiene and package metadata needed for an open-source candidate: a clear license, git ignore rules for local gate/review artifacts, a compact package manifest or index, and valid `agents/openai.yaml` metadata starting with `interface:`.

The public release package MAY include read-only behavior-check prompt examples under `examples/`, including `examples/skill-behavior-prompts.json`, as samples for human or Darwin-style skill review. These examples MUST be documented as behavior checks for the skill itself and MUST NOT be described or enforced as formal QA, release, seal, or four-gate verdicts.

Public README, promotional, and installation/hook documentation, including `references/install-and-hooks.md`, MUST describe Claude Code, Codex, and Cursor as separate host targets where host support is described. Any hook enforcement claim MUST be tied to hook/config setup and live canary evidence on the specific target host, and MUST NOT claim that a passing canary on one host proves another host. Public examples MUST NOT expose maintainer-local absolute paths such as a specific Windows user profile.

#### Scenario: Open-source candidate package has required release hygiene

- **GIVEN** the `harden-public-release` change is applied
- **WHEN** a maintainer inspects the package root and host metadata
- **THEN** the package includes a license file, git ignore rules for local gate/review artifacts, a compact manifest or index, and an `agents/openai.yaml` file that starts with `interface:`

#### Scenario: Behavior prompt examples are public samples, not formal gates

- **GIVEN** the `harden-public-release` change is applied
- **WHEN** a maintainer inspects README, the package manifest, and `examples/skill-behavior-prompts.json`
- **THEN** the behavior prompts are covered as read-only public examples
- **AND** they are described as skill behavior checks, not formal QA, release, seal, or four-gate verdicts

#### Scenario: Public platform support wording requires per-host proof

- **GIVEN** the `harden-public-release` change is applied
- **WHEN** a maintainer inspects README, public promotional documents, and `references/install-and-hooks.md`
- **THEN** Claude Code, Codex, and Cursor are described as host targets
- **AND** hook enforcement claims require hooks/config and live canary evidence on the specific host
- **AND** public docs do not claim that one host's canary proves another host or claim full Codex plugin distribution
- **AND** public examples use portable user-profile placeholders instead of maintainer-local absolute paths

#### Scenario: Public release validation commands pass

- **GIVEN** the `harden-public-release` change is applied
- **WHEN** the maintainer runs the portable OpenSpec canary and local skill validation
- **THEN** both verification commands pass
