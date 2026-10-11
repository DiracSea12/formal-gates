# 安装与 Hook

这份参考只用于包维护、安装、hook 和 canary。推进顺序、签发与接纳由 engine/CLI 持有，`SKILL.md` 只约束受理与真实用户决定。

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

活动 workflow run 不阻断安装：安装器不扫描或等待它们，也不改写、迁移或
兼容旧 run。安装后旧 generation 的 run 由正常 admission/版本校验决定能否继续。
卸载仍在存在受影响的活动 run 时拒绝，避免直接移除它正在使用的运行时、hook 和规则。

引导文件 `install.command` 和 `install.bat` 会下载匹配的 release 源码与二进制、
校验已发布的 checksum、组装本地包，把 binary 首次放到固定 stable launcher，再由该
launcher 调用唯一原生安装事务。既有 launcher 拒绝 release 安装参数时，`--force`
引导安装由已校验源码包内的实际 `bin/formal-gates` 执行同一原生事务，只允许更新默认
固定 launcher，原 journal 在替换前保存原 launcher。源码 binary 不接受普通安装、
bootstrap、卸载或 workflow 写入；引导脚本不备份或覆盖既有 launcher。

## 安装位置

典型的全局目标是：

- Claude Code：`~/.claude/skills/formal-gates`
- Codex：`~/.codex/skills/formal-gates`
- Cursor：`~/.cursor/formal-gates`
- DeepSeek Harness：`$DSH_HOME/skills/formal-gates`（默认 `~/.dsh`）
- ZCode：`~/.zcode/skills/formal-gates`
- WorkBuddy：`~/.codebuddy/skills/formal-gates`；项目级同对应目录，仅安装 CODEBUDDY.md 指令，不写 global hook。

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
写 hook 补丁，需要 hook 集成时使用 global。项目级安装不能代替绑定 DeepSeek provider 与隔离
Cordis bridge 的完整原生 canary。
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

hook 的 write_block 是辅助写隔离，业务推进与接纳由 engine 门禁拥有。它读取当前
engine state 的已签发开发 Batch、OwnerSession/OwnerTranscript、确认材料与
SpawnReceipts/lifecycle 身份，限制主线程和审查角色直接写代码或 run 状态。开发/返修与
QA 设计角色按已绑定身份工作；主代理编辑已确认需求/方案材料仍须走需求变更流程。
作用域限定承载活动 run 的仓库根、owner 对话与开发阶段；未进入开发或终态解除写墙。
角色分类是只读投影，不签发任务、不接纳结果、不改变状态。真实自动拦截仍须同一宿主
和安装副本的 live canary 证明。代理职责见 [角色定义](../agents/agent-types.md)。

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

派发与原生身份绑定由 engine 的已签发 action 和 SpawnReceipt 持有；lifecycle capture
只登记原生观测，drive/submit 经同一执行与对账路径处理。UNKNOWN 先查原始生命周期
事实，不能盲目重派，也不能从配置文件推断代理已经完成。实际调用形式与结果提交见
[正式流程公共合同](formal-flow.md#原生运行时派发)。

维护面的只读核验可使用：

```bash
$HOME/.local/bin/formal-gates lifecycle verify --root <repo> --run-id <id> \
  --dispatch <issued-action-id>
```

provider、bridge、原生会话、请求/轮次与完成证据分别绑定。缺少 transcript/usage 的
宿主如实记 UNAVAILABLE；安装目录或 hook 配置存在不等于原生执行可用，更不等于
Git 非分片完整 canary 通过。不同宿主、版本和安装副本的证据不能互相代用。

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
candidate-binary package validation against its own source package
  -> lock(admission/install -> registry)
  -> reconcile any unfinished install journal
  -> journal intent(old/new runtime, pointer/config, registry digests)
  -> temp + backups
  -> copy manifest-selected native subset + Lstat/realpath/digest validation
  -> switch release/installed target (journal=switched; old registry remains authoritative)
  -> actual installed-binary package validation + --version smoke
  -> hook/managed-rule writes + atomic stable-launcher publication
  -> actual stable-launcher --version smoke
  -> registry record commit
  -> journal committed + install receipt
```

这里的 post-switch 指 release/installed target 已切换但公共 pointer/config 和 registry 尚未
提交；候选自身 CLI 核其包合同，native owner 按同一 manifest 核复制集合与字节。
source 与 target 分别由各自包内 binary 执行 package validation；实际 installed binary
执行 `--version` smoke。hook/规则写入后原子发布固定 launcher，并从其实际路径执行
`--version` smoke，通过后提交 registry。
任一 runtime、pointer/config 或 registry 提交失败，都由同一 owner
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
