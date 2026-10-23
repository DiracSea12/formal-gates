# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### 分片审查夹具的逐节点执行平台（2026-10-22）

- 改动：共享分片夹具按保存配置中每个审查节点的执行选择确定预期 provider，仅在未配置时回落到原启动宿主；原材料、事件、原生来源和原始审查结果的平台身份比较继续保留。新增跨平台回归，验证主运行及两个分片重新打开后仍保留产品审查和技术审查各自的执行选择，启动宿主不随之改变。
- 验证与限制：`53276703` 的 Linux race 实际记录证实前一项修复使原四个失败中的三个通过；剩余分片用例暴露此共享夹具仍混同宿主和执行平台。本机独立、相同的混合平台用例修前失败、修后通过；仅修改测试，不改业务执行选择或同平台子代理规则。完整 Unix/race 流水线仍待后续 CI，未宣称全绿。

### Unix 独立执行夹具的平台身份（2026-10-22）

- 改动：独立 supervisor 用例明确区分 Codex 宿主与 WorkBuddy 执行平台，在启动前通过原配置保存执行选择，并核对实际签发材料和 supervisor 原生请求的 provider；分片沿原配置继承执行平台。显式测试同宿主绑定的用例保留原含义。
- 原因：[流水线 39884120576](https://github.com/DiracSea12/formal-gates/actions/runs/39884120576) 的 Unix race 检查中，四个用例把宿主和执行平台都设为 WorkBuddy，却预期独立会话启动。该输入应触发已有的同平台原生子代理能力检查，不能作为跨平台独立执行的夹具。
- 验证与限制：保留原启动次数、归属、取消、需求替换、分片与恢复断言；本机同平台拒绝独立会话的负控通过。仅调整测试，不恢复同平台独立会话回退，不改变业务调度或权限规则。本机没有 Unix 执行环境，Unix 实际执行及 race 结果继续由后续 CI 核实，当前不宣称 CI 通过。

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

### WorkBuddy 桌面认证与模型目录（2026-10-22）

- 改动：通过 WorkBuddy 原生桌面存储及进程内桥接初始化原 CLI 所需认证，复用现有登录态，不另建登录存储或明文凭据副本；模型目录从原生元数据发现，安装包包含对应 helper。受管工具列表沿用公共归属。
- 验证与限制：固定启动器真实元数据查询获得 22 个模型，使用现有桌面登录态的 Hy3 实际调用成功；安装资产哈希和 WorkBuddy 相关回归通过。异常 socket 断开只有源码核查，未宣称已有独立断开负控实跑；其他平台的认证实测范围分别记录，未要求重新登录。

### Git 候选与工作区逻辑一致性（2026-10-22）

- 改动：跟踪及就绪判断使用 Git 原生修改文件与逻辑差异，正确处理 Windows CRLF、索引 stat 刷新与原生过滤；不将仅物理换行或 stat 差异当作新业务候选，不制造空提交。
- 验证与限制：真实 Windows Git 红绿回归、重复只读与 HEAD/index/逻辑内容不变检查通过；原真实项目 UNKNOWN 执行通过原观察接纳路径继续，候选快照保持，没有手工重写状态或索引。

### Windows 状态发布窗口（2026-10-22）

- 改动：持久化状态的替换与读取、诊断读取复用现有 atomicfile 能力；正常读取和恢复保持同一权威状态及原校验路径，不新增第二份状态或恢复账本。
- 验证与限制：加入并执行 Windows 原生发布窗口相关回归，核对并发读取及发布后的实际内容；本次提交拆分另核编译与最终树一致性，不把 Windows 检查写作 Unix 实测。

### 完整终态结果读取（2026-10-22）

- 改动：COMPLETE/ABORTED 不再刷新已清理的活动子状态，保留历史投影；完全退休结果读取提取原引擎唯一判据，供 Drive、CLI 和控制视图共用。安装更新后可只读原完整终态，活动运行、未完成清理及真正写入仍受原安装身份限制。
- 验证与限制：隔离安装更新后的 show/status/next/drive 重复读取、原文件树字节不变，以及活动运行/未完清理/新写入拒绝负控通过。最新版稳定入口实际重读原项目返回 COMPLETE、修订 198、exit 0、无 stderr；原主运行和两个子运行活动状态未重建。

### Codex MCP 安装幂等与配置保留（2026-10-22）

- 改动：以原生 TOML 解码和已有工具参数所有权判据识别自有未标记 MCP 配置；匹配时重复安装保持原字节，外来条目拒绝覆盖。卸载仅处理实际自有表，排除多行说明文字中的伪表头。新增 BurntSushi/toml 依赖，避免自行实现 TOML 语法。
- 验证与限制：自有/外来/裸参数/多行说明伪表头及安装回滚回归通过；正常全局安装成功，注册表仅保留一个全局记录。宿主配置和登录文件摘要保持，源构建、稳定入口、全局安装二进制摘要一致。

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
