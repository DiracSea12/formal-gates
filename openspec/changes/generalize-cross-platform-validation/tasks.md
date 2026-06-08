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

- [x] Run a Claude Code live canary before claiming Claude Code hook blocking works for this package.
  - Evidence: Claude Code 2.1.168 negative canary produced 1 PreToolUse payload, blocked the invalid formal PASS command, and left the marker uncreated at `.artifacts/ai/formal-gates-hook-client-tests/claude-live-canary-20260608-215339/summary.json`.
  - Evidence: Claude Code 2.1.168 normal-operation canary produced 1 PreToolUse payload and allowed a larger read/search/ordinary-note workflow with literal `> openspec/...` and `writeFileSync(...)` strings at `.artifacts/ai/formal-gates-hook-client-tests/claude-normal-canary-20260608-215545/summary.json`.
- [ ] Run a Codex live canary before claiming Codex hook blocking works for this package.
  - Current result: Codex CLI 0.137.0 did not prove hook blocking. Even with a temporary `~/.codex/hooks.json` matcher of `*`, the invalid formal PASS command executed, the marker was created, and 0 hook payloads were captured at `.artifacts/ai/formal-gates-hook-client-tests/codex-globalhook-negative-20260608-215826/summary.json`.
  - Current result: Codex normal-operation run completed and was not falsely blocked, but it also captured 0 hook payloads; this is normal-command evidence only, not hook allow-path proof, at `.artifacts/ai/formal-gates-hook-client-tests/codex-globalhook-normal-20260608-220036/summary.json`.
- [ ] Run a Cursor live canary before claiming Cursor hook blocking works for this package.
  - Current result: native Windows Cursor 3.4.20 exposes `cursor.cmd`, but `cursor.cmd agent ...` only forwards unknown options to Electron/Chromium and did not start a terminal agent. WSL is not installed/usable on this machine, and the downloaded Cursor Agent installer script supports only `Linux` and `Darwin`.
  - Current result: direct Cursor-shaped hook payload tests passed for invalid formal PASS blocking and normal-operation allow at `.artifacts/ai/formal-gates-hook-client-tests/cursor-direct-hook-20260608-2203/summary.json`, but this is not live runtime proof.
- [x] Keep each live canary result scoped to the specific host and environment that produced it.

## Phase 2C Conditional Release Trust

- [ ] Add checksums for published artifacts if release artifacts are introduced.
- [ ] Add build provenance or artifact attestation if binary or generated release artifacts are introduced.
- [ ] Add package provenance if npm or another package-manager distribution is introduced.
- [ ] Add signing or equivalent verification guidance only when the chosen distribution channel requires it.
