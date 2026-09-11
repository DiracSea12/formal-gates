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
考文档。它默认配置所选 host 的 formal-gates 命令，并为支持 hook 的 host 配置子代理生命周期 hook。既有的无
关 hook 条目会被保留，并在适用的 host 指导文件中维护最新的受理规则。只有当 host 的 hook 配置和受理规则都必须逐字节保持不变时，才使用
`--skip-hooks`。

只有带 `--force` 时，安装才会替换一个已存在的 formal-gates 目标。它不得把另一个
host 的全局安装当作回退。

活动 workflow run 不阻断安装：安装器不扫描或等待它们，也不改写、迁移或
兼容旧 run。安装后旧 generation 的 run 由正常 admission/版本校验决定能否继续。
卸载仍在存在受影响的活动 run 时拒绝，避免直接移除它正在使用的运行时、hook 和规则。

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

Codex 的安装命令会附加 `--provider codex`，Claude Code 附加 `--provider claude-code`；
两者的 owner 计量由此绑定同一启动 transcript 的解析器。既有 Claude 安装通过普通更新
替换旧 hook 后获得该来源字段；缺 provider 的 run 仍将 owner 记为 unavailable。
Codex 要求阻断结果通过 JSON 的
`decision: "block"` 返回，同时 hook 进程退出码必须为 0；Claude Code、Cursor 和
DeepSeek Harness 的 Cordis 插件使用原有的拒绝退出码与通用 JSON 决策。ZCode 使用
嵌套的 `hookSpecificOutput`/`permissionDecision` 决策对象，允许时不输出 JSON，拒绝时
使用文档化的退出码 2 快速阻断。

它从 stdin 接收 host 的 JSON 载荷，并返回与 host 兼容的 allow/block 决策。它是围
绕 formal-gates 命令的护栏，既不是代码质量的证明，也不能替代显式的流程状态检查。

当前写阻断读取 `.gates/engine/<run-id>/state.json` 的 engine envelope；只接纳当前
schema，不读取旧 workflow state。至少一个 `development-worker` execution 已认领且
`regular.terminal` 为空时，对仓库内代码与状态的直接写入按调用者身份阻断。此前的
产品审、技术审和需求确认阶段不启用该写阻断；终态写入后解除，即使后续 cleanup 尚未完成。

主线程与审查类代理的直接写入被阻断；formal-gates CLI、只读命令、已绑定开发/QA
设计角色，以及对当前 `regular.requirement.path` 的文档编辑按现有规则处理。原生 identity
从当前 `regularExecutions` 解析，不再依赖 `claim-dispatch` 或旧派发 sidecar。角色定义见
[代理类型](../agents/agent-types.md)。真实写入包括 `git add`、`git commit` 和文件重定向；
只读命令中提到 `.gates` 不构成写入。实机阻断仍须由同一 host 的 live canary 证明。

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

`lifecycle capture` 和 `lifecycle verify` 保留为独立宿主维护/观测命令，参数见各自
`--help`。它们不签发、认领或推进当前 regular action；没有已有维护 binding 时，verify
返回 `REJECTED` 与 `dispatch has no lifecycle binding`。没有活动观测上下文时 capture
不创建日志。维护观测的旧 `.gates/tmp/<run-id>/lifecycle` 目录不作为 engine 状态、动作
授权或 regular 完成证据，不能通过手工创建目录来启动当前流程。

当前 Codex/Git regular 使用原生 app-server bridge。执行方式为：

```bash
formal-gates workflow drive --root <repo> --run-id <id> --action-id <issued-action-id>
# 已持久化原生身份的受支持中断恢复：
formal-gates workflow drive --root <repo> --run-id <id> --action-id <issued-action-id> --resume
```

`action-id` 来自 `workflow show` 的 `executions`。执行前由 engine 认领，bridge 从原生
thread/turn 及本次 process journal 收集启动、完成和 source 事实；调用者不自行填
`agent_id` 代替原生观察。任务内模型与声明 QA 操作使用同一 action/TaskKey/Attempt
及动态工具。完整 argv、typed 字段、动态绑定和恢复证据见
[regular 使用参考](engine-regular.md#任务内部-model-与声明-qa-操作)。
其他已安装宿主的 hook 维护能力不意味着该宿主已支持阶段 4 regular。

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

完整源码包的版本文件清单由该包自带 binary 校验。stable owner 固定执行
`<source>/bin/formal-gates package validate --root <source>`；这是只读检查，source binary
不执行安装。复制、installed-path smoke、runtime/pointer/registry 提交和失败恢复仍全部
由当前 stable owner 的既有事务持有。这样新版恢复旧包时不会要求旧包包含新版源码文件。

`97eef33d866301a2985f510dd486a21196eea3f9` 的旧安装器仍用自身文件清单校验其他版本，
不能直接安装删除了旧 workflow 文件的阶段 4 包。先从精确旧源码构建维护版本，应用
[通用安装器补丁](phase4-installer-maintenance.patch)，再由原 stable launcher 安装维护包：

```bash
git -C <old-source-copy> checkout --detach 97eef33d866301a2985f510dd486a21196eea3f9
git -C <old-source-copy> apply --unidiff-zero <stage4-source>/references/phase4-installer-maintenance.patch
git -C <old-source-copy> add internal/validate/install.go
git -C <old-source-copy> commit -m "Validate install sources with their own package contract"
cd <old-source-copy>
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
```

在独立 HOME、project、release root 中按下列顺序演练；`<stable-launcher>` 必须是该隔离
目标已安装的固定入口。每一步保留精确源码、package/installed identity、registry generation
和 `<registry>.install.json`，下一步不得覆盖上一步的留存副本。

```bash
<stable-launcher> install --source <old-source-copy> --host codex --scope project --project <project> --binary-target <stable-launcher> --release-root <release-root> --force
<stable-launcher> install --source <committed-stage4-package> --host codex --scope project --project <project> --binary-target <stable-launcher> --release-root <release-root> --force
<stable-launcher> install --source <original-stable-package> --host codex --scope project --project <project> --binary-target <stable-launcher> --release-root <release-root> --force
```

最后一步是提交后恢复原稳定包，仍是一笔新安装事务，generation 不倒退；失败提交则
按下述原 journal 自动恢复。维护补丁与阶段 4 的 `internal/validate/install.go` 修改相同，
不增加命令或 workflow 兼容路径。真实全局的维护升级只能在本阶段固定 driver 完成 Seal、
主线集成及必要重验后执行；Seal 前保持该 driver 及运行绑定不变。

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


## WorkBuddy 原生入口（阶段 4a）

已有 WorkBuddy 应用包 CLI 可直接使用，不必因为 codebuddy 不在 PATH 再装一份。
使用已有安装事务：`./install.command --host workbuddy --scope project --project <project> --force`
或 `--scope global`。项目/全局技能目录为 `.codebuddy/skills/formal-gates`；指导文件分别为
项目 `CODEBUDDY.md` 与 `~/.codebuddy/CODEBUDDY.md`。安装器不为 WorkBuddy 写全局 hook；
引擎动作的启动、结束、身份、结果与工具边界由原生 runtime adapter 记录。安装成功
不代表已经实测平台完整 regular 流程，也不承诺原生流程以外的全局 hook 拦截。

ZCode 与 DSH 继续复用原来的安装位置、宿主配置与生命周期能力；本阶段只增加 regular
驱动和 action 内工具配置，不替换用户模型/认证设置。可执行入口与模型选项见
[user-configuration.md](user-configuration.md)。困难平台实跑按用户决定延期，明确未验证；
公共与协议组件验证继续执行，延期不免除实现，也不作为本轮 Seal 阻塞。
