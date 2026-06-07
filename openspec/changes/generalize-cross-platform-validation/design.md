# Design

## Overview

The change splits `formal-gates` portability into three bounded layers:

1. **Core skill layer**: Markdown and JSON package content that Agent Skill compatible runtimes can read.
2. **Portable validation layer**: a Go CLI that performs deterministic package and artifact checks on Windows, macOS, and Linux.
3. **Host adaptation layer**: host-specific installation and hook wording that states what is readable, installable, and live-canary-proven.

OpenSpec becomes one adapter in the requirement-document layer. The core workflow should use generic language such as requirement document, design, tasks, spec, acceptance evidence, and requirement source.

## Go Validation Entrypoint

The portable CLI should provide a small command surface focused on current validation needs:

- package structure checks;
- required skill and reference file checks;
- manifest and example artifact checks;
- requirements-clarification and formal gate artifact schema checks through portable Go logic or platform-appropriate wrappers that do not require PowerShell on macOS or Linux.

The CLI must not become a general workflow engine. PowerShell can remain for Windows compatibility, but macOS and Linux validation must not depend on PowerShell.

## CI Matrix

The CI matrix must run the portable validation path on:

- Windows;
- macOS;
- Linux.

The matrix is acceptance evidence for cross-platform validation. It does not prove host hook enforcement.

## Requirement Document Adapters

Core docs should describe requirements generically. Adapter references should explain format-specific coverage:

- OpenSpec adapter: proposal, design, tasks, spec, and change path coverage.
- Generic document adapter: PRD, SDD, issue, design brief, or markdown requirement bundle coverage.

Adapters may map local document terms to the common requirement fields, but they must not narrow user requirements or replace user-confirmed intent.

## Host Capability Wording

Host documentation must use separate capability categories:

- readable skill support;
- install guidance;
- hook configuration path;
- hook blocking proven by live canary.

Hook blocking can only be claimed for a specific host after live canary proof on that host.

## Phase 2 Release Trust

Phase 2 must be documented as follow-up work for release trust:

- checksums;
- GitHub artifact attestation or equivalent build provenance;
- npm provenance or equivalent package provenance if an npm package is introduced;
- signed release or equivalent verification guidance.

Phase 1 must not claim these features as delivered.

## Risks

- Over-generalizing host support would recreate the same false enforcement problem the package already tries to avoid.
- Replacing PowerShell with Node would move the runtime dependency rather than remove it.
- Letting the Go CLI grow into a workflow engine would turn a portability change into a platform project.
