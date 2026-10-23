# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### 面板选项与选择状态（2026-10-22）

- 改动：平台、模型选项共用缓存，手动刷新和级联刷新只更新下游选项；模型及推理强度跟随实际默认。强化已选择/未选择样式；全部门单独展示并随具体门状态变化；识别提示词中的中英文简介并换行；增加黑盒 QA 消耗提示和配置列表中的“全量”路线。
- 验证与限制：面板 Node 测试通过，真实 DOM 的流程画布及移动边界用例通过。缓存与展示检查属于前端验证，实际执行效果结合后续批门及真实项目记录核验。

本组及以下七项从原汇总提交 `59ef3a3f` 按功能重组，共涉及原 139 个文件；除本变更日志外，最终跟踪文件内容保持相同。累计原实现的独立复杂度门、实现质量门均 PASS，冻结输入 SHA256 为 `20a6e841a24c01f52e6f5293f1007dd005b260939acf79d25dfb76596c0d18cd`。这些事实不代表 CI 全绿：[原推送流水线](https://github.com/DiracSea12/formal-gates/actions/runs/39882713458) 的三项 Unix race 检查已失败，其余结果以 Actions 为准；新提交继续跟进 CI。原始日志和运行证据保留本地，不将临时状态、登录资料或无关材料提交入库。

### 每批质量门与统一配置（2026-10-22）

- 改动：每个开发批次具有独立执行职责，是否启用及所选门由命名配置统一控制；依赖批次等待前批所选门。处理分片、独立批次并行、取消后重开和返修配置恢复；合并评审携带各分片原始 base/head 比较。技审前弹出配置面板的时机保持原产品策略。
- 验证与限制：真实 Git/引擎边界、配置反复保存与恢复回归通过；真实项目为两分片各两批、八次普通门通过，原执行时间戳证明并行。返修关闭时业务 FAIL 阻断由协议负控验证，真实模型运行未产生该 FAIL finding。

### 对话启动、宿主确认与原生子代理（2026-10-22）

- 改动：启动采用对话明确决定，删除启动强制弹窗；流程内操作确认遵循宿主原生权限。免交互宿主不额外索取真人输入，询问宿主保留原生确认；拒绝/取消不执行，修改后可以重提。公共操作确认不再附加必填数据字段，DSH 删除原生工具放行后的第二次提问。限制确认展示篇幅；同平台 Codex 采用真实原生子代理，继续和恢复复用原身份与接纳路径。
- 验证与限制：公共确认的接受/拒绝/取消/过期及普通数据表单回归通过；真实 DSH 原生权限对照通过。实际 Codex 子代理的父子来源、never/full 策略和控制候选接纳已核验。七平台已审计，但没有全部进行真实模型完整运行，未支持的原生子代理能力仍明确报错。

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
