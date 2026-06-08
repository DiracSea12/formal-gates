# Generalize Cross-Platform Validation

## Why

`formal-gates` is currently packaged as a reusable Agent Skill, but its machine-side validation and installation story is still too tied to Windows PowerShell and OpenSpec-first wording. A reusable public package needs a portable validation entrypoint, clear host capability wording, and requirement-document language that works beyond OpenSpec.

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
- Explicitly document Phase 2 release-trust and cross-platform hook/runtime work without claiming it is delivered in Phase 1.

## What Changes

- Introduce a Go CLI command for cross-platform validation.
- Add CI tasks that run the portable validation path on Windows, macOS, and Linux.
- Update README, manifest, skill entrypoint, and references so OpenSpec is no longer the only named document path.
- Add adapter documentation for OpenSpec and generic requirement documents.
- Update host support wording to distinguish readable skill support, installation guidance, and live-canary-proven hook blocking.
- Add Phase 2 follow-up scope for checksums, artifact attestation, npm provenance, or equivalent release-trust evidence.
- Add Phase 2 follow-up scope for a Go-based hook core, cross-platform installer verification, and per-host runtime live canaries.
- Fix the existing Windows/Claude document-write gate path so a covered OpenSpec document write can pass after requirements clarification PASS when `GateWorkflow` is carried in the Write tool content.

## Non-goals

- Do not build a unified agent runtime.
- Do not build a unified hook framework across Claude, Codex, Cursor, Gemini, OpenCode, or Windsurf.
- Do not maintain a large host-path table for dozens of agents.
- Do not add a large artifact, report, cache, or state platform.
- Do not require PowerShell on macOS or Linux.
- Do not make Node, Python, or PowerShell the universal validation runtime.
- Do not implement Phase 2 release-trust features in Phase 1.
- Do not implement the Phase 2 Go hook core or cross-platform installer/runtime canary suite in Phase 1.
- Do not weaken the named gate structure or host-specific live-canary proof requirement.

## Acceptance

- A Go validation entrypoint runs package and artifact validation without requiring PowerShell on macOS or Linux.
- Existing Windows PowerShell canary behavior still passes or remains explicitly supported by a compatible wrapper path.
- CI runs the portable validation path on Windows, macOS, and Linux.
- README, manifest, `SKILL.md`, and references describe requirement documents generically and route OpenSpec-specific behavior through adapter guidance.
- Host support wording separates readable skill support, installation guidance, and live-canary-proven hook blocking.
- Documentation clearly states Phase 2 will cover release trust, including checksums, artifact attestation, npm provenance, or equivalent evidence, and that those features are not Phase 1 deliverables.
- Documentation clearly states Phase 2 will cover cross-platform hook logic, installer verification, and host runtime live canaries, and that those features are not Phase 1 deliverables.
- Existing Windows/Claude formal-document write gating allows a covered OpenSpec target after a recorded requirements-clarification PASS when the Write tool content contains the matching `GateWorkflow`.
- The change does not introduce a unified agent runtime, unified hook framework, broad host-path table, or new platform-sized artifact/report/state system.
