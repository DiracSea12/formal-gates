# Tasks

- [x] Add a bounded Go validation CLI for portable package and artifact checks.
- [x] Preserve the existing Windows PowerShell canary path or wrap it without regression.
- [x] Add a Windows, macOS, and Linux CI matrix for the portable validation path.
- [x] Update README.md and README_EN.md to describe generic requirement-document support and cross-platform validation boundaries.
- [x] Update `SKILL.md` and relevant references so OpenSpec-specific wording moves to adapter guidance.
- [x] Add OpenSpec and generic requirement-document adapter references.
- [x] Update manifest host-support fields to separate readable skill support, install guidance, hook configuration, and live-canary-proven blocking.
- [x] Document Phase 2 release-trust scope without claiming it is delivered in Phase 1.
- [x] Document Phase 2 hook/runtime scope without claiming it is delivered in Phase 1.
- [x] Verify the change does not introduce a unified agent runtime, unified hook framework, broad host-path table, or platform-sized artifact/report/state system.
- [x] Run the existing Windows canary and the new cross-platform validation path.
- [x] Fix current Windows/Claude formal-document Write gating so content-carried `GateWorkflow` can unlock a covered OpenSpec target after requirements clarification PASS.

## Phase 2 Follow-up Tasks

- [ ] Add a Go-based hook core for cross-platform allow/deny decisions without requiring PowerShell on macOS or Linux.
- [ ] Add fixture-based hook logic tests for Claude/Codex/Cursor payload shapes on Windows, macOS, and Linux.
- [ ] Add cross-platform installer or copy-verifier coverage for non-Windows package installation paths.
- [ ] Add separate live canaries for Claude Code, Codex, and Cursor runtime interception.
- [ ] Add release-trust evidence: checksums, artifact attestation, package provenance if package distribution is introduced, and signing or equivalent verification guidance.
