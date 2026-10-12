# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- 新增 DeepSeek Harness (`dsh`) 宿主目标：global 安装把 skill 装到 `$DSH_HOME/skills/formal-gates`，维护 `$DSH_HOME/AGENTS.md` 规则，并把包内最小 Cordis 插件挂进 home 级 `cordis.patch.yml`，把 `tools/pre-execute` 转发到原生 `hook decide`；project 安装只装 skill 与 `AGENTS.md`（DSH 只自动加载 home 级补丁）。写墙通过 lifecycle claim 绑定反查子代理类型，保证 development-worker / qa-design 放行、审查类代理阻断。实机阻断仍须同宿主 live canary。

### Changed

- runWorkflow 拆为子命令表驱动 runner。
- Extend native installation with ordered managed intake-rule migration and
  symmetric uninstall cleanup for runtime directories, hooks, and host guidance
  files across Claude Code, Codex, and Cursor project installs.
- Accept an ancestor (or equal) `--base-snapshot` at `workflow start`, making
  already-committed in-flight work fall inside the base-to-current review diff.
- Strengthen the implementation-quality gate's performance review for observable
  regressions.
- Discover independent review gates from package-local `gates/*.md` files.
- Compose every gate task from one shared reviewer base and one gate-specific
  prompt.
- Replace the receipt/closure workflow with one resumable run-state file and
  one retained seal or abort summary.
- Run selected QA Execution and selected independent gates in parallel on the
  current native VCS snapshot.
- Resolve and verify immutable identities through the selected Git, SVN, or P4
  command for preparation, snapshots, results, Carry, and Seal.
- Reserve `qa` for the built-in QA flow so a discovered gate cannot collide
  with its routing and result ownership.
- Use the selected Git, SVN, or P4 command directly for cumulative and repair
  comparisons.
- Reject Seal while selected work is pending or lacks a matching PASS or
  permitted user skip authorization.

### Removed

- The formal `none` route and its empty-run branches.
- Fixed four-gate registries, extension manifests, prompt copies, context
  bundles, receipt and closure graphs, recursive Carry chains, generated
  handoffs, detailed gate-state trees, and their compatibility paths.
- The duplicate `formal-gates-validate` command, old evidence demos, and the
  standalone prompt-pollution pattern catalog.

---

## [0.1.0] — 2026-06-13

### Added
- Portable `formal-gates-validate` Go CLI for cross-platform package and artifact validation
- Host canary results for Claude / Codex / Cursor
- Darwin (macOS) strict test prompts
- `requirements-clarification-gate` with `DRAFT_BLOCKED` enforcement
- `complexity-gate` for scope creep and over-engineering prevention
- `architecture-health-gate` for module boundary and dependency health checks
- `code-quality-gate` for correctness, dead code, and test quality review
- `qa-test-gate` for test case design and evidence validation
- `enforce-gate-sequence.ps1` hook for machine-layer gate enforcement
- `gate-workflow.ps1` for recording gate workflow artifacts
- English translations for README and SKILL

### Changed
- Trim non-runtime package materials from distribution
- Improve public validation summary output
- Optimize formal-gates skill workflow convergence stop rule
- Harden formal gates hook validation logic
- Clarify Codex hook enforcement boundary (auxiliary guardrail only)
- Refine formal gates packaging for public release
- Bind gate reviews to dispatch prompts

### Fixed
- Formal gates hook and workflow regressions
- Document write gate for OpenSpec proposal phase

---

## [0.0.1] — 2026-06-05

### Added
- Initial release with core four-gate system
- SKILL.md entry point for AI routing
- Gate-specific reference documents (`references/`)
- PowerShell installation and canary scripts
- `examples/` with GateWorkflow and behavior-check prompt samples
