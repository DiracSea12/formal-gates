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

## Phase 2A Core Cross-platform Follow-up

- [x] Add a small Go-based hook decision core for cross-platform allow/deny decisions without requiring PowerShell on macOS or Linux.
- [x] Expose the hook decision core through the existing Go validation command surface instead of adding a new runtime framework.
- [x] Add fixture-based hook logic tests for representative Claude Code, Codex, and Cursor payload shapes on Windows, macOS, and Linux.
- [x] Document thin host wiring guidance that copies or invokes the shared hook decision logic without introducing an installer framework.
- [x] Verify Phase 2A does not introduce a unified agent runtime, host registry, installer verifier, report/cache/state platform, daemon, service, or broad host-path table.

## Phase 2B Claim-scoped Host Proof

- [ ] Run a Claude Code live canary before claiming Claude Code hook blocking works for this package.
- [ ] Run a Codex live canary before claiming Codex hook blocking works for this package.
- [ ] Run a Cursor live canary before claiming Cursor hook blocking works for this package.
- [ ] Keep each live canary result scoped to the specific host and environment that produced it.

## Phase 2C Conditional Release Trust

- [ ] Add checksums for published artifacts if release artifacts are introduced.
- [ ] Add build provenance or artifact attestation if binary or generated release artifacts are introduced.
- [ ] Add package provenance if npm or another package-manager distribution is introduced.
- [ ] Add signing or equivalent verification guidance only when the chosen distribution channel requires it.
