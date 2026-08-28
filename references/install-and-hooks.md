# 安装与 Hook

这份参考只用于包维护、安装、hook 和 canary。流程顺序只由 `SKILL.md` 拥有。

- [构建与验证](#构建与验证)
- [原生安装](#原生安装)
- [安装位置](#安装位置)
- [Hook 边界](#hook-边界)
- [候选包检查](#候选包检查)
- [Release 边界](#release-边界)

## 构建与验证

已安装的包使用原生 Go 二进制。从源码检出安装之前先构建它：

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

Windows 上构建并调用 `bin\formal-gates.exe`。

`package validate` 检查所维护的包结构和基于文件的提示词目录。`canary portable`
检查不需要实机 AI host 的包、流程、hook 决策和安装行为。这两个命令都不是独立审查
或正式 Seal 结果。

## 原生安装

```bash
./install.command --host <host> --scope global --force
./install.command --host <host> --scope project --project <project> --force
# Windows: install.bat ...

$HOME/.local/bin/formal-gates uninstall \
  --host <host> --scope global

$HOME/.local/bin/formal-gates uninstall \
  --host <host> --scope project --project <project>
```

安装器复制一份运行时包，包含 `SKILL.md`、CLI、`prompts/`、`gates/` 和所维护的参
考文档。它默认配置所选 host 的 formal-gates 命令和子代理生命周期 hook。既有的无
关 hook 条目会被保留，并在适用的 host 指导文件中维护最新的受理规则。只有当 host 的 hook 配置和受理规则都必须逐字节保持不变时，才使用
`--skip-hooks`。

只有带 `--force` 时，安装才会替换一个已存在的 formal-gates 目标。它不得把另一个
host 的全局安装当作回退。

引导文件 `install.command` 和 `install.bat` 会下载匹配的 release 源码与二进制、
校验已发布的 checksum、组装本地包，把 binary 首次放到固定 stable launcher，再由该
launcher 调用唯一原生安装事务。源码目录的 `bin/formal-gates` 是候选，只能做只读验证
和隔离 canary，不能安装、bootstrap、卸载或写 workflow state；引导脚本不是第二个安装器。

## 安装位置

典型的全局目标是：

- Claude Code：`~/.claude/skills/formal-gates`
- Codex：`~/.codex/skills/formal-gates`
- Cursor：`~/.cursor/formal-gates`
- DeepSeek Harness：`$DSH_HOME/skills/formal-gates`（默认 `~/.dsh`）
- ZCode：`~/.zcode/skills/formal-gates`

项目级安装使用所选项目下的对应目录。hook 配置始终写固定 stable launcher 的绝对路径，
不会指向可替换 target 内的 candidate binary。

安装器维护的规则文件是：

- Claude Code 全局：`~/.claude/CLAUDE.md`；项目：`<project>/CLAUDE.md`
- Codex 全局：`~/.codex/AGENTS.md`；项目：`<project>/AGENTS.md`
- Cursor 项目：`<project>/.cursor/rules/formal-gates.mdc`
- DeepSeek Harness 全局：`$DSH_HOME/AGENTS.md`；项目：`<project>/AGENTS.md`
- ZCode 全局：`~/.zcode/AGENTS.md`；项目：`<project>/AGENTS.md`

Cursor 全局只安装 `~/.cursor/formal-gates` 运行时和 `hooks.json` hook，不创建全局
规则文件。当前规则直接取自 `SKILL.md` 中
`<formal-gates:host-instructions:start>` 与 `<formal-gates:host-instructions:end>` 之间的
唯一宿主指令区块；安装器把同一区块写入宿主指令文件。重复安装会替换区块内容并把
重复区块收敛为一个，同时保留区块外内容。

从旧版（包括旧 target/bin hook 或旧 marker）升级时，直接从 release 引导脚本加
`--force` 安装；事务只识别并迁移安装器拥有的精确旧 hook 形状，不保留旧 writer 或入口。

DeepSeek Harness 的 hook 补丁是 home 级 `cordis.patch.yml`；DSH 不自动加载项目目录
下的补丁文件，所以 `--host dsh --scope project` 只安装 skill 与 `AGENTS.md` 指令，不
写 hook 补丁，需要 hook 集成时使用 global。项目级 DSH 二进制因此保持宽松默认
provider，lifecycle verify 为 `UNAVAILABLE`，既有派发与身份检查仍是权威依据。
其他兼容 Agent Skill 的 host 可以手工阅读这些 Markdown，但本包不声明为它们提供
安装器或 hook 集成。

## 原生卸载

卸载使用与安装相同的 host、scope 和 project 解析：

```bash
$HOME/.local/bin/formal-gates uninstall --host claude --scope global
$HOME/.local/bin/formal-gates uninstall --host codex --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host cursor --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host dsh --scope global
$HOME/.local/bin/formal-gates uninstall --host dsh --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host zcode --scope global
$HOME/.local/bin/formal-gates uninstall --host zcode --scope project --project <project>
```

它会删除所选 host 的 formal-gates 运行时目录、安装器拥有的 hook 条目和完整 marker
规则区块，同时保留规则区块外内容与非 formal-gates hook；若另一个已安装 host 仍共享同一
项目规则文件，则保留该文件直到最后一个 host 卸载。规则清理由 marker 独立完成，
即使运行时目录已经不存在也不需要规则源码。卸载只能由 registry 登记的 stable launcher
执行，不接受旧 `--source` 旁路。

## Hook 边界

原生 hook 入口是：

```bash
$HOME/.local/bin/formal-gates hook decide
```

Codex 的安装命令会附加 `--provider codex`。Codex 要求阻断结果通过 JSON 的
`decision: "block"` 返回，同时 hook 进程退出码必须为 0；Claude Code、Cursor 和
DeepSeek Harness 的 Cordis 插件使用原有的拒绝退出码与通用 JSON 决策。ZCode 使用
嵌套的 `hookSpecificOutput`/`permissionDecision` 决策对象，允许时不输出 JSON，拒绝时
使用文档化的退出码 2 快速阻断。

它从 stdin 接收 host 的 JSON 载荷，并返回与 host 兼容的 allow/block 决策。它是围
绕 formal-gates 命令的护栏，既不是代码质量的证明，也不能替代显式的流程状态检查。

进入正式开发后，hook 还承担 RQ-011 主代理/审查类代理写阻断：以
`development-worker` 从 `PENDING` 进入 `PREPARED` 为开发开始边界；在此之前的产品审、
技术审及文档修订阶段不启用写阻断。开发开始后，PreToolUse 对代码与 run 状态的直接写入
（Edit/Write/MultiEdit、git commit、写文件 Bash）按调用者身份阻断——主线程（payload
无 agent 身份）与审查类代理被阻断，formal-gates CLI 命令、只读命令、development-worker、
qa-design 与主代理对已登记需求/设计文档的编辑放行；未进入开发的活动 run 与无活动 run
均放行。写墙只在 `status=ACTIVE && development-worker.status!=PENDING` 的区间内生效；
Seal / Abort 先持久化 `SEALED` / `ABORTED` 再收尾删除临时状态，因此即使收尾失败留下终态
文件，也会立即解除写阻断，不会永久锁住仓库。角色权限按身份（`agent_type`）而非静态
文件白名单判定；路径只用于限定活动仓库根的空间边界。代理类型定义见
`agents/agent-types.md`。Bash 写入判定只看**真实写目标**：命令文本只是
提到 `.gates` 但只读（grep/ls/cat/find/python3 读、只读 git 查询如 `git status`
`git log -- <path>`）一律放行；真实写（`git add`/`git commit`、`> .gates/...`、
`tee .gates/...`）仍阻断，主线程与审查类代理一致。实机阻断仍须在同一 host 上经 live
canary 验证。

写墙的空间范围只覆盖承载该活动 run 的仓库根，不是进程级或全局文件锁。即使窗口 cwd
仍位于活动仓库，Edit/Write/MultiEdit 的目标明确位于仓库外时也直接放行；简单 Bash 写入
（重定向、tee、touch、mkdir、rm、mv、cp、install）在全部目标都能解析且均位于仓库外时
同样放行。包含仓库内目标或无法可靠归类的复合 Bash 写入继续按角色墙处理，避免一条命令
同时修改仓库内外时绕过保护。

安装器还会为 Claude Code 和 Codex 配置 `SubagentStart` 与 `SubagentStop`，为
Cursor 配置 `subagentStart` 与 `subagentStop`。这些 hook 把 host 载荷经 stdin 发
送给已安装的原生二进制：

ZCode 的全局 CLI 配置使用 `PreToolUse`、`PostToolUse` 和 `PostToolUseFailure`，并以
`Agent|Task` matcher 观察工具调用；当前运行时不读取项目级 hook 配置，因此项目安装
只写 skill 与 `AGENTS.md` 指令。工具调用生命周期的精确子代理完成语义仍须同一 ZCode
版本的实机 canary 才能确认。

```bash
$HOME/.local/bin/formal-gates lifecycle capture \
  --provider <claude-code|codex|cursor|deepseek-harness|zcode> --event <provider-event-name>
```

capture 命令从正常的 host 载荷推导项目根（必要时使用 host 的项目目录环境变量），
因此全局 hook 不依赖其配置目录作为工作目录。`--root` 仍然可用，作为显式的命令行
覆盖。

生命周期观测只在该项目中至少有一个正式 run 处于活动状态时保留。每个 run 在
`.gates/tmp/<run-id>/lifecycle` 下拥有自己的待定与已认领观测，因此正常的 Seal 或
Abort 清理会把它们与该 run 的其余部分一并退役。在没有活动正式 run 时触发的 hook
不会创建生命周期日志。

在 `workflow claim-dispatch` 绑定 host 身份之后，可以在不改变流程状态的情况下检
视推导出的结果。固定 stable launcher 同时服务多个 host 时，认领必须显式给出
`--provider <claude-code|codex|cursor|deepseek-harness|zcode>`；同一项目路径下存在冲突的
active registry host 时不会猜测第一个记录：

```bash
$HOME/.local/bin/formal-gates workflow claim-dispatch --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --reviewer <identity> --provider codex
```

省略 `--provider` 只适用于没有歧义的正常开发构建/已登记 host；共享 launcher 的歧义
会硬阻断，避免把生命周期证据绑定到错误 host。

```bash
$HOME/.local/bin/formal-gates lifecycle verify --root <repo> --run-id <id> \
  --dispatch <dispatch-id>
```

全局安装的 Claude Code、Cursor、Codex、DeepSeek Harness 和 ZCode 都要求其适配器的生命周期事件配对；
项目级 DSH/ZCode 因宿主不读取项目 hook 配置而保持宽松默认 provider，验证结果为 `UNAVAILABLE`。Codex
派发独立代理走原生 `spawn_agent`，用返回的 `agent_id` 认领；DeepSeek Harness 的
Cordis 插件把 `subagent/start`/`subagent/end` 转成同一身份对，claim → lifecycle 配对
验证即可通过。ZCode 以 `tool_use_id`（或同等调用 ID）关联 Agent/Task 工具调用；这
证明的是工具调用闭环，子代理“完成”语义仍需同版本实机 canary。未安装二进制（go test、canary portable、本地开发构建）解析为宽松的
默认 provider，生命周期验证为 `UNAVAILABLE`，此时既有的派发与身份检查仍是权威依据。

一个设置文件或直接的 `hook decide` 单元测试，只能证明本地决策逻辑。只有当实际目
标 host 发送了实机 `PreToolUse` 载荷、并且 hook 阻塞了测试命令之后，才可以声明自
动阻塞。在一个 host 上做的 canary 不能证明另一个 host。

Codex 实机检查：

```bash
bin/formal-gates canary codex-hook --worktree <repo>
```

失败意味着该客户端/版本没有证明闭环的自动拦截。显式的 `formal-gates workflow ...`
命令仍然是该 run 的正常权威。

## 候选包检查

测试候选版本时，把该确切源码安装进测试项目，并记录两个路径：

```text
source: <candidate>/formal-gates
installed: <test-project>/<host-path>/formal-gates
```

不要一边测试过期的全局包，一边把结果报告成候选版本的结果。包、提示词目录、安装
和实机 hook 的所有声明，都必须指明实际使用的那份副本。

首次使用固定 stable driver 时，先从冻结的已安装 artifact 调用受支持的
`install --bootstrap` 维护入口。bootstrap 只登记 target、host、project/state/resource
root、runtime sibling、epoch/generation 和 lease/token，并写 bootstrap receipt；它不创建
workflow state。registry 不存在时允许这一次创建，已有记录缺失、冲突或无法对账时只留下
disabled/`UNREGISTERED_INSTALL` receipt 并停止。bootstrap receipt 提交后，stable launcher
才允许第一次 `workflow start`；候选 binary 和裸旧绝对 binary 都不能执行 bootstrap 或写
stable registry。

安装事务会在每个非幂等边界持久化同一份 journal，并把完整机器 receipt 写入 registry
旁的 `<registry>.install.json`。Go installer、admission bridge、Shell 和 PowerShell
共享 native owner、install/uninstall lock、registry lock、generation/token 和 recovery
receipt schema。事务的可观察顺序固定为：

```text
lock(admission/install -> registry)
  -> journal intent(old/new runtime, pointer/config, registry digests)
  -> temp + backups
  -> copy + manifest/Lstat/realpath/digest validation
  -> switch release/installed target (journal=switched; old registry remains authoritative)
  -> installed-binary package validation + post-switch/pre-commit smoke
  -> atomic current/pointer/config + registry record commit
  -> journal committed + install receipt
```

这里的 post-switch 指 release/installed target 已切换但公共 pointer/config 和 registry 尚未
提交；smoke 必须从实际 installed binary path 启动。只有 smoke 通过后才共同提交 runtime
和 registry；任一 runtime、pointer/config 或 registry 提交失败，都由同一 owner
observe/reconcile 恢复旧 runtime/config/registry bytes，再写 recovery receipt。receipt 至少
包含 source/package 与 installed digest、逐输入 `Lstat`/realpath manifest、hook/config 与
managed-rule 路径和 digest、事务 smoke 结果以及 registry/state/resource canonical paths。
重复安装时如果 hook 或 managed-rule 内容已经相同，receipt 的 action 会明确记录
`SKIPPED_UNCHANGED`，不会用另一个兼容 writer 改写字节；`--skip-hooks` 同时跳过 hook
和受理规则更新，不绕过 runtime、receipt 或 registry transaction。故障矩阵可用同一原生入口复现：

```bash
FORMAL_GATES_INSTALL_FAULT=journal-boundary|intent|registry|copy-component:runtime|prepared|switched|post-switch-smoke|pointer|hook|managed-rule|registry-commit|copy-component:prompts|copy-component:gates|verify-stage:installed-target|verify-stage:manifest|verify-stage:realpath|verify-stage:digest \
  <stable-launcher> install --source <candidate> --host claude --scope project \
  --project <project> --binary-target <stable-launcher> --force
```

也可以用公开 fixture 一次覆盖复制、切换、installed-binary smoke、pointer、hook、规则、
registry commit 和 journal 边界，并验证每次失败没有留下已提交 target/release/launcher。每个
注入边界只暴露一个 canonical fixture 名称（`hook`、`managed-rule` 不再有 `hooks`/`rules`
等别名，同一边界不重复执行）；唯一保留的旧输入 `runtime` 在入口归一化为
`copy-component:runtime`。复制 fixture 可按组件选择，
installed-target 校验 fixture 可按阶段选择：

```bash
<stable-launcher> canary fault-matrix --root <candidate> --fixture copy-component:prompts
<stable-launcher> canary fault-matrix --root <candidate> --fixture verify-stage:installed-target
```

真实 hook JSON、managed-rule、pointer/config、registry 和 post-switch/pre-commit
installed-binary smoke 失败也会回滚到旧 runtime/config/registry，并在 registry 旁留下
`<registry>.transaction.json.failure.json`；崩溃后下次 install/uninstall 会对账同一 journal，
清理临时/备份路径并写 `<registry>.transaction.json.receipt.json`。smoke 失败时 journal
仍处于 `switched`，不会伪装成 committed；脚本不得自行删除 release、切换 pointer 或写
registry。

## Release 边界

CI 流程构建 Windows、macOS 和 Linux 二进制、portable-canary 输出和 SHA256
checksum 文件，并可以把它们附加到 GitHub Release。checksum 说明下载到的字节与已
发布的 CI 产物一致。它们不提供签名、attestation、provenance、registry、
marketplace 或 `npx` 分发路径。
