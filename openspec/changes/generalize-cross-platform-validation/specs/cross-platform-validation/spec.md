# Cross-Platform Validation

## ADDED Requirements

### Requirement: Portable validation entrypoint

`formal-gates` MUST provide a Go-based validation entrypoint that can run on Windows, macOS, and Linux without requiring PowerShell on macOS or Linux.

The validation entrypoint MUST stay focused on deterministic package and artifact checks. It MUST NOT become a unified agent runtime, workflow engine, or host hook framework.

#### Scenario: Portable validation runs without PowerShell on non-Windows systems

- **GIVEN** the `generalize-cross-platform-validation` change is applied
- **WHEN** a maintainer runs the portable validation entrypoint on macOS or Linux
- **THEN** validation runs without requiring PowerShell
- **AND** the command reports package or artifact validation failures with actionable output

#### Scenario: Windows validation does not regress

- **GIVEN** the existing Windows PowerShell canary path is available
- **WHEN** the maintainer runs the existing Windows validation path after this change
- **THEN** it still passes or remains supported through an explicitly documented compatible wrapper

### Requirement: Cross-platform CI evidence

The package MUST include CI validation for the portable validation path on Windows, macOS, and Linux.

This CI evidence MUST NOT be described as proof that any host hook blocks commands unless a live canary has run on that specific host.

#### Scenario: CI validates the portable path on three operating systems

- **GIVEN** the repository CI runs for this change
- **WHEN** the cross-platform validation job executes
- **THEN** the portable validation path runs on Windows, macOS, and Linux
- **AND** failures on any platform fail the job

### Requirement: Requirement-document adapters

Core `formal-gates` documentation MUST describe requirement sources generically rather than treating OpenSpec as the only formal path.

OpenSpec-specific behavior MUST be documented as an adapter for OpenSpec proposal, design, tasks, spec, and change-path coverage. Generic requirement documents such as PRD, SDD, issue, design brief, or markdown requirement bundle MUST be mappable to common requirement fields without weakening user-confirmed requirements.

#### Scenario: OpenSpec is one adapter rather than the only supported path

- **GIVEN** the change is applied
- **WHEN** a maintainer inspects README, `SKILL.md`, and requirement references
- **THEN** the core wording refers to requirement documents generically
- **AND** OpenSpec-specific behavior is routed through adapter guidance

### Requirement: Host capability claims

Host support documentation MUST distinguish readable skill support, install guidance, hook configuration, and hook blocking proven by live canary.

Hook enforcement claims MUST be limited to the specific host where live canary proof exists. A passing canary on one host MUST NOT be used as evidence for another host.

#### Scenario: Host support wording does not overstate enforcement

- **GIVEN** the change is applied
- **WHEN** a maintainer inspects README, manifest, and installation references
- **THEN** Claude, Codex, Cursor, Gemini, OpenCode, and Windsurf support is described by capability level
- **AND** unverified hook blocking is not claimed

### Requirement: Phase 2 release-trust scope

The package MUST document Phase 2 release-trust work as future scope covering checksums, artifact attestation, npm provenance if npm packaging is introduced, or equivalent release verification evidence.

Phase 1 documentation MUST NOT claim Phase 2 release-trust features as delivered.

#### Scenario: Release trust is visible but not falsely claimed

- **GIVEN** the Phase 1 change is applied
- **WHEN** a maintainer inspects the proposal, design, and public documentation
- **THEN** release-trust work is identified as Phase 2
- **AND** Phase 1 acceptance does not require or claim delivered release signing, artifact attestation, npm provenance, or equivalent release-trust evidence
