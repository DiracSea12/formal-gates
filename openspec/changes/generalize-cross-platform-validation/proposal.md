# Generalize Cross-Platform Validation

## Why

`formal-gates` is currently packaged as a reusable Agent Skill, but its machine-side validation and installation story is still too tied to Windows PowerShell and OpenSpec-first wording. A reusable public package needs a portable validation entrypoint, clear host capability wording, and requirement-document language that works beyond OpenSpec.

Comparable high-star skill packages keep the package shape small: a skill directory, `SKILL.md`, optional scripts or resources, host-specific setup notes, and focused tests. They do not usually build a unified hook runtime, canary platform, installer framework, or release-trust system just to be cross-platform as a skill package. Phase 2 should follow that lighter shape.

## Goal

Make `formal-gates` usable as a cross-platform Agent Skill package with a Go-based validation entrypoint, Windows/macOS/Linux validation evidence, and OpenSpec treated as one requirement-document adapter rather than the only formal path.

## Scope

- Add a Go-based machine validation entrypoint for portable package and artifact checks.
- Keep existing Windows PowerShell validation usable and non-regressed.
- Add a Windows, macOS, and Linux CI matrix for the portable validation entrypoint.
- Change core documentation from OpenSpec-only language to generic requirement-document language.
- Move OpenSpec-specific behavior into adapter guidance.
- Clarify host support levels for Claude, Codex, Cursor, Gemini, OpenCode, and Windsurf.
- Preserve existing live-canary discipline for hook enforcement claims.
- Re-scope Phase 2 into a lightweight default path:
  - Phase 2A: portable hook decision logic and fixture tests.
  - Phase 2B: claim-scoped live canary proof only for hosts where blocking is claimed.
  - Phase 2C: release-trust work only when binary or package distribution is introduced.

## What Changes

- Introduce a Go CLI command for cross-platform validation.
- Add CI tasks that run the portable validation path on Windows, macOS, and Linux.
- Update README, manifest, skill entrypoint, and references so OpenSpec is no longer the only named document path.
- Add adapter documentation for OpenSpec and generic requirement documents.
- Update host support wording to distinguish readable skill support, installation guidance, and live-canary-proven hook blocking.
- Replace the broad Phase 2 follow-up with a skill-package-scale plan:
  - a small Go hook decision core for cross-platform allow/deny decisions;
  - fixture-based tests for Claude Code, Codex, and Cursor payload examples;
  - simple source-copy or thin-script install guidance instead of an installer framework;
  - per-host live canaries only before making host-specific blocking claims;
  - conditional release-trust evidence only if this project starts shipping release artifacts or package-manager distributions.
- Fix the existing Windows/Claude document-write gate path so a covered OpenSpec document write can pass after requirements clarification PASS when `GateWorkflow` is carried in the Write tool content.

## Non-goals

- Do not build a unified agent runtime.
- Do not build a unified hook framework across Claude, Codex, Cursor, Gemini, OpenCode, or Windsurf.
- Do not maintain a large host-path table for dozens of agents.
- Do not add a large artifact, report, cache, or state platform.
- Do not require a live canary platform, installer verifier, or release-trust system to complete the core cross-platform skill-package path.
- Do not require PowerShell on macOS or Linux.
- Do not make Node, Python, or PowerShell the universal validation runtime.
- Do not require checksums, attestation, signing, package provenance, or npm provenance unless release artifacts or package-manager distribution are introduced.
- Do not treat Phase 2B host canaries as proof for any host that was not canaried.
- Do not implement the Phase 2A Go hook decision core or host canaries in Phase 1.
- Do not weaken the named gate structure or host-specific live-canary proof requirement.

## Acceptance

- A Go validation entrypoint runs package and artifact validation without requiring PowerShell on macOS or Linux.
- Existing Windows PowerShell canary behavior still passes or remains explicitly supported by a compatible wrapper path.
- CI runs the portable validation path on Windows, macOS, and Linux.
- README, manifest, `SKILL.md`, and references describe requirement documents generically and route OpenSpec-specific behavior through adapter guidance.
- Host support wording separates readable skill support, installation guidance, and live-canary-proven hook blocking.
- Documentation clearly states Phase 2A is the core cross-platform follow-up: portable hook decision logic plus fixture tests, not a runtime platform.
- Documentation clearly states Phase 2B live canaries are required only before claiming hook blocking for a specific host.
- Documentation clearly states Phase 2C release-trust evidence is conditional on shipping release artifacts or package-manager distributions.
- Existing Windows/Claude formal-document write gating allows a covered OpenSpec target after a recorded requirements-clarification PASS when the Write tool content contains the matching `GateWorkflow`.
- The change does not introduce a unified agent runtime, unified hook framework, broad host-path table, or new platform-sized artifact/report/state system.
